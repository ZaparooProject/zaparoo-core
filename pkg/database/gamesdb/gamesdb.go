package gamesdb

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	bolt "go.etcd.io/bbolt"
	"golang.org/x/sync/errgroup"

	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/pkg/utils"
	"github.com/gobwas/glob"
	"github.com/rs/zerolog/log"
)

const (
	BucketNames       = "names"
	indexedSystemsKey = "meta:indexedSystems"
)

// NameKey returns the key for a file name in the names index.
func NameKey(systemId string, name string) string {
	return systemId + ":" + name
}

// Exists returns true if the media database exists on disk.
func Exists(platform platforms.Platform) bool {
	_, err := os.Stat(filepath.Join(platform.DataDir(), config.GamesDbFile))
	return err == nil
}

// Open the gamesdb with the given options. If the database does not exist it
// will be created and the buckets will be initialized.
func open(platform platforms.Platform, options *bolt.Options) (*bolt.DB, error) {
	err := os.MkdirAll(filepath.Dir(filepath.Join(platform.DataDir(), config.GamesDbFile)), 0755)
	if err != nil {
		return nil, err
	}

	db, err := bolt.Open(filepath.Join(platform.DataDir(), config.GamesDbFile), 0600, options)
	if err != nil {
		return nil, err
	}

	err = db.Update(func(txn *bolt.Tx) error {
		for _, bucket := range []string{BucketNames} {
			_, err := txn.CreateBucketIfNotExists([]byte(bucket))
			if err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return db, nil
}

// Open the gamesdb with default options for generating names index.
func openForGenerate(platform platforms.Platform) (*bolt.DB, error) {
	return open(platform, &bolt.Options{
		Timeout:        1 * time.Second,
		NoSync:         true,
		NoFreelistSync: true,
	})
}

func readIndexedSystems(db *bolt.DB) ([]string, error) {
	var systems []string

	err := db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(BucketNames))
		v := b.Get([]byte(indexedSystemsKey))
		if v != nil {
			systems = strings.Split(string(v), ",")
		}
		return nil
	})

	return systems, err
}

func writeIndexedSystems(db *bolt.DB, systems []string) error {
	return db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(BucketNames))
		v := b.Get([]byte(indexedSystemsKey))
		if v == nil {
			v = []byte(strings.Join(systems, ","))
			return b.Put([]byte(indexedSystemsKey), v)
		} else {
			existing := strings.Split(string(v), ",")
			for _, s := range systems {
				if !utils.Contains(existing, s) {
					existing = append(existing, s)
				}
			}
			return b.Put([]byte(indexedSystemsKey), []byte(strings.Join(existing, ",")))
		}
	})
}

type fileInfo struct {
	SystemId string
	Path     string
	Name     string
}

// Delete all names in index for the given system.
func deleteSystemNames(db *bolt.DB, systemId string) (int, error) {
	deleted := 0
	err := db.Batch(func(tx *bolt.Tx) error {
		bns := tx.Bucket([]byte(BucketNames))
		c := bns.Cursor()
		p := []byte(systemId + ":")
		for k, _ := c.Seek(p); k != nil && strings.HasPrefix(string(k), string(p)); k, _ = c.Next() {
			err := bns.Delete(k)
			if err != nil {
				return err
			}
			deleted++
		}
		return nil
	})
	return deleted, err
}

// Update the names index with the given files.
func updateNames(db *bolt.DB, files []fileInfo) error {
	return db.Batch(func(tx *bolt.Tx) error {
		bns := tx.Bucket([]byte(BucketNames))

		for _, file := range files {
			base := filepath.Base(file.Path)
			name := file.Name
			if name == "" {
				name = strings.TrimSuffix(base, filepath.Ext(base))
			}

			nk := NameKey(file.SystemId, name)
			err := bns.Put([]byte(nk), []byte(file.Path))
			if err != nil {
				return err
			}
		}

		return nil
	})
}

type IndexStatus struct {
	Total    int
	Step     int
	SystemId string
	Files    int
}

// Given a list of systems, index all valid game files on disk and write a
// names index to the DB. Overwrites any existing names index, but does not
// clean up old missing files.
//
// Takes a function which will be called with the current status of the index
// during key steps.
//
// Returns the total number of files indexed.
func NewNamesIndex(
	platform platforms.Platform,
	cfg *config.Instance,
	systems []systemdefs.System,
	update func(IndexStatus),
) (int, error) {
	status := IndexStatus{
		Total: len(systems) + 2, // estimate steps
		Step:  1,
	}

	db, err := openForGenerate(platform)
	if err != nil {
		return status.Files, fmt.Errorf("error opening gamesdb: %s", err)
	}
	defer func(db *bolt.DB) {
		err := db.Close()
		if err != nil {
			log.Warn().Err(err).Msg("closing gamesdb")
		}
	}(db)

	filteredIds := make([]string, 0)
	for _, s := range systems {
		filteredIds = append(filteredIds, s.ID)
	}

	indexed, err := readIndexedSystems(db)
	if err != nil {
		log.Info().Msg("no indexed systems found")
	}

	for _, v := range indexed {
		if !utils.Contains(filteredIds, v) {
			continue
		}

		count, err := deleteSystemNames(db, v)
		if err != nil {
			return status.Files, fmt.Errorf("error deleting system names: %s", err)
		} else {
			log.Debug().Msgf("deleted names for %s: %d", v, count)
		}
	}

	update(status)
	systemPaths := make(map[string][]string)
	for _, v := range GetSystemPaths(platform, platform.RootDirs(cfg), systems) {
		systemPaths[v.System.ID] = append(systemPaths[v.System.ID], v.Path)
	}

	g := new(errgroup.Group)
	scanned := make(map[string]bool)
	for _, s := range systemdefs.AllSystems() {
		scanned[s.ID] = false
	}

	sysPathIds := utils.AlphaMapKeys(systemPaths)
	// update steps with true count
	status.Total = len(sysPathIds) + 2

	for _, k := range sysPathIds {
		systemId := k
		files := make([]platforms.ScanResult, 0)

		status.SystemId = systemId
		status.Step++
		update(status)

		// scan using standard folder + extensions
		for _, path := range systemPaths[k] {
			pathFiles, err := GetFiles(cfg, platform, k, path)
			if err != nil {
				log.Error().Err(err).Msgf("error getting files for system: %s", systemId)
				continue
			}
			for _, f := range pathFiles {
				files = append(files, platforms.ScanResult{Path: f})
			}
		}

		// for each system launcher in platform, run the results through its
		// custom scan function if one exists
		for _, l := range platform.Launchers() {
			if l.SystemId == k && l.Scanner != nil {
				log.Debug().Msgf("running %s scanner for system: %s", l.Id, systemId)
				files, err = l.Scanner(cfg, systemId, files)
				if err != nil {
					log.Error().Err(err).Msgf("error running %s scanner for system: %s", l.Id, systemId)
					continue
				}
			}
		}

		if len(files) == 0 {
			log.Debug().Msgf("no files found for system: %s", systemId)
			continue
		}

		status.Files += len(files)
		log.Debug().Msgf("scanned %d files for system: %s", len(files), systemId)
		scanned[systemId] = true

		g.Go(func() error {
			fis := make([]fileInfo, 0)
			for _, p := range files {
				fis = append(fis, fileInfo{SystemId: systemId, Path: p.Path, Name: p.Name})
			}
			return updateNames(db, fis)
		})
	}

	// run each custom scanner at least once, even if there are no paths
	// defined or results from regular index
	for _, l := range platform.Launchers() {
		systemId := l.SystemId
		if !scanned[systemId] && l.Scanner != nil {
			log.Debug().Msgf("running %s scanner for system: %s", l.Id, systemId)
			results, err := l.Scanner(cfg, systemId, []platforms.ScanResult{})
			if err != nil {
				log.Error().Err(err).Msgf("error running %s scanner for system: %s", l.Id, systemId)
				continue
			}

			log.Debug().Msgf("scanned %d files for system: %s", len(results), systemId)

			status.Files += len(results)
			scanned[systemId] = true

			if len(results) > 0 {
				g.Go(func() error {
					fis := make([]fileInfo, 0)
					for _, p := range results {
						fis = append(fis, fileInfo{SystemId: systemId, Path: p.Path, Name: p.Name})
					}
					log.Debug().Msgf("updating names for system: %s", systemId)
					return updateNames(db, fis)
				})
			}
		}
	}

	// launcher scanners with no system defined are run against every system
	var anyScanners []platforms.Launcher
	for _, l := range platform.Launchers() {
		if l.SystemId == "" && l.Scanner != nil {
			anyScanners = append(anyScanners, l)
		}
	}

	for _, l := range anyScanners {
		for _, s := range systems {
			log.Debug().Msgf("running %s scanner for system: %s", l.Id, s.ID)
			results, err := l.Scanner(cfg, s.ID, []platforms.ScanResult{})
			if err != nil {
				log.Error().Err(err).Msgf("error running %s scanner for system: %s", l.Id, s.ID)
				continue
			}

			log.Debug().Msgf("scanned %d files for system: %s", len(results), s.ID)

			if len(results) > 0 {
				status.Files += len(results)
				scanned[s.ID] = true

				systemId := s.ID
				g.Go(func() error {
					fis := make([]fileInfo, 0)
					for _, p := range results {
						fis = append(fis, fileInfo{SystemId: systemId, Path: p.Path, Name: p.Name})
					}
					log.Debug().Msgf("updating names for system: %s", systemId)
					return updateNames(db, fis)
				})
			}
		}
	}

	status.Step++
	status.SystemId = ""
	update(status)

	err = g.Wait()
	if err != nil {
		return status.Files, fmt.Errorf("error updating names index: %s", err)
	}

	indexedSystems := make([]string, 0)
	log.Debug().Msgf("scanned systems: %v", scanned)
	for k, v := range scanned {
		if v {
			indexedSystems = append(indexedSystems, k)
		}
	}
	log.Debug().Msgf("indexed systems: %v", indexedSystems)

	err = writeIndexedSystems(db, indexedSystems)
	if err != nil {
		return status.Files, fmt.Errorf("error writing indexed systems: %s", err)
	}

	err = db.Sync()
	if err != nil {
		return status.Files, fmt.Errorf("error syncing database: %s", err)
	}

	return status.Files, nil
}

type SearchResult struct {
	SystemId string
	Name     string
	Path     string
}

// Iterate all indexed names and return matches to test func against query.
func searchNamesGeneric(
	platform platforms.Platform,
	systems []systemdefs.System,
	query string,
	test func(string, string) bool,
) ([]SearchResult, error) {
	if !Exists(platform) {
		return nil, fmt.Errorf("gamesdb does not exist")
	}

	db, err := open(platform, &bolt.Options{})
	if err != nil {
		return nil, err
	}
	defer func(db *bolt.DB) {
		err := db.Close()
		if err != nil {
			log.Warn().Err(err).Msg("closing database")
		}
	}(db)

	var results []SearchResult

	err = db.View(func(tx *bolt.Tx) error {
		bn := tx.Bucket([]byte(BucketNames))

		for _, system := range systems {
			pre := []byte(system.ID + ":")
			nameIdx := bytes.Index(pre, []byte(":"))

			c := bn.Cursor()
			for k, v := c.Seek(pre); k != nil && bytes.HasPrefix(k, pre); k, v = c.Next() {
				keyName := string(k[nameIdx+1:])

				if test(query, keyName) {
					results = append(results, SearchResult{
						SystemId: system.ID,
						Name:     keyName,
						Path:     string(v),
					})
				}
			}
		}

		return nil
	})

	if err != nil {
		log.Debug().Err(err).Msg("search names")
		return nil, err
	}

	return results, nil
}

// Return indexed names matching exact query (case insensitive).
func SearchNamesExact(platform platforms.Platform, systems []systemdefs.System, query string) ([]SearchResult, error) {
	return searchNamesGeneric(platform, systems, query, func(query, keyName string) bool {
		return strings.EqualFold(query, keyName)
	})
}

// Return indexed names partially matching query (case insensitive).
func SearchNamesPartial(platform platforms.Platform, systems []systemdefs.System, query string) ([]SearchResult, error) {
	return searchNamesGeneric(platform, systems, query, func(query, keyName string) bool {
		return strings.Contains(strings.ToLower(keyName), strings.ToLower(query))
	})
}

// Return indexed names that include every word in query (case insensitive).
func SearchNamesWords(platform platforms.Platform, systems []systemdefs.System, query string) ([]SearchResult, error) {
	return searchNamesGeneric(platform, systems, query, func(query, keyName string) bool {
		qWords := strings.Fields(strings.ToLower(query))

		for _, word := range qWords {
			if !strings.Contains(strings.ToLower(keyName), word) {
				return false
			}
		}

		return true
	})
}

// Return indexed names matching query using regular expression.
func SearchNamesRegexp(platform platforms.Platform, systems []systemdefs.System, query string) ([]SearchResult, error) {
	return searchNamesGeneric(platform, systems, query, func(query, keyName string) bool {
		// TODO: this should be cached
		r, err := regexp.Compile(query)
		if err != nil {
			return false
		}

		return r.MatchString(keyName)
	})
}

var globCache = make(map[string]glob.Glob)
var globCacheMutex = &sync.RWMutex{}

func SearchNamesGlob(platform platforms.Platform, systems []systemdefs.System, query string) ([]SearchResult, error) {
	return searchNamesGeneric(platform, systems, query, func(query, keyName string) bool {
		globCacheMutex.RLock()
		cached, ok := globCache[query]
		if !ok {
			globCacheMutex.RUnlock()

			g, err := glob.Compile(query)
			if err != nil {
				return false
			}

			globCacheMutex.Lock()
			globCache[query] = g
			globCacheMutex.Unlock()
			cached = g
		} else {
			globCacheMutex.RUnlock()
		}

		return cached.Match(strings.ToLower(keyName))
	})
}

// Return true if a specific system is indexed in the gamesdb
func SystemIndexed(platform platforms.Platform, system systemdefs.System) bool {
	if !Exists(platform) {
		return false
	}

	db, err := open(platform, &bolt.Options{})
	if err != nil {
		return false
	}
	defer func(db *bolt.DB) {
		err := db.Close()
		if err != nil {
			log.Warn().Err(err).Msg("closing database")
		}
	}(db)

	systems, err := readIndexedSystems(db)
	if err != nil {
		return false
	}

	return utils.Contains(systems, system.ID)
}

// Return all systems indexed in the gamesdb
func IndexedSystems(platform platforms.Platform) ([]string, error) {
	if !Exists(platform) {
		return nil, fmt.Errorf("gamesdb does not exist")
	}

	db, err := open(platform, &bolt.Options{})
	if err != nil {
		return nil, err
	}
	defer func(db *bolt.DB) {
		err := db.Close()
		if err != nil {
			log.Warn().Err(err).Msg("closing database")
		}
	}(db)

	systems, err := readIndexedSystems(db)
	if err != nil {
		return nil, err
	}

	return systems, nil
}

// Return a random game from specified systems.
func RandomGame(platform platforms.Platform, systems []systemdefs.System) (SearchResult, error) {
	if !Exists(platform) {
		return SearchResult{}, fmt.Errorf("gamesdb does not exist")
	}

	db, err := open(platform, &bolt.Options{})
	if err != nil {
		return SearchResult{}, err
	}
	defer func(db *bolt.DB) {
		err := db.Close()
		if err != nil {
			log.Warn().Err(err).Msg("closing database")
		}
	}(db)

	var result SearchResult

	system, err := utils.RandomElem(systems)
	if err != nil {
		return result, err
	}

	possible := make([]SearchResult, 0)

	err = db.View(func(tx *bolt.Tx) error {
		bn := tx.Bucket([]byte(BucketNames))

		pre := []byte(system.ID + ":")
		nameIdx := bytes.Index(pre, []byte(":"))

		c := bn.Cursor()
		for k, v := c.Seek(pre); k != nil && bytes.HasPrefix(k, pre); k, v = c.Next() {
			keyName := string(k[nameIdx+1:])
			possible = append(possible, SearchResult{
				SystemId: system.ID,
				Name:     keyName,
				Path:     string(v),
			})
		}

		return nil
	})
	if err != nil {
		return result, err
	}

	if len(possible) == 0 {
		return result, fmt.Errorf("no games found for system: %s", system.ID)
	}

	result, err = utils.RandomElem(possible)
	if err != nil {
		return result, err
	}

	return result, nil
}
