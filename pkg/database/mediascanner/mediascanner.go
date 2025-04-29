package mediascanner

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/pkg/utils"
	"github.com/rs/zerolog/log"
)

type PathResult struct {
	System systemdefs.System
	Path   string
}

// FindPath case-insensitively finds a file/folder at a path.
func FindPath(path string) (string, error) {
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}

	parent := filepath.Dir(path)
	name := filepath.Base(path)

	files, err := os.ReadDir(parent)
	if err != nil {
		return "", err
	}

	for _, file := range files {
		target := file.Name()

		if len(target) != len(name) {
			continue
		} else if strings.EqualFold(target, name) {
			return filepath.Join(parent, target), nil
		}
	}

	return "", fmt.Errorf("file match not found: %s", path)
}

func GetSystemPaths(pl platforms.Platform, rootFolders []string, systems []systemdefs.System) []PathResult {
	var matches []PathResult

	for _, system := range systems {
		var launchers []platforms.Launcher
		for _, l := range pl.Launchers() {
			if l.SystemID == system.ID {
				launchers = append(launchers, l)
			}
		}

		var folders []string
		for _, l := range launchers {
			for _, folder := range l.Folders {
				if !utils.Contains(folders, folder) {
					folders = append(folders, folder)
				}
			}
		}

		for _, folder := range rootFolders {
			gf, err := FindPath(folder)
			if err != nil {
				continue
			}

			for _, folder := range folders {
				systemFolder := filepath.Join(gf, folder)
				path, err := FindPath(systemFolder)
				if err != nil {
					continue
				}

				matches = append(matches, PathResult{system, path})
			}
		}
	}

	return matches
}

type resultsStack [][]string

func (r *resultsStack) new() {
	*r = append(*r, []string{})
}

func (r *resultsStack) pop() {
	if len(*r) == 0 {
		return
	}
	*r = (*r)[:len(*r)-1]
}

func (r *resultsStack) get() (*[]string, error) {
	if len(*r) == 0 {
		return nil, fmt.Errorf("nothing on stack")
	}
	return &(*r)[len(*r)-1], nil
}

// GetFiles searches for all valid games in a given path and return a list of
// files. This function deep searches .zip files and handles symlinks at all
// levels.
func GetFiles(
	cfg *config.Instance,
	platform platforms.Platform,
	systemId string,
	path string,
) ([]string, error) {
	var allResults []string
	var stack resultsStack
	visited := make(map[string]struct{})

	system, err := systemdefs.GetSystem(systemId)
	if err != nil {
		return nil, err
	}

	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}

	var scanner func(path string, file fs.DirEntry, err error) error
	scanner = func(path string, file fs.DirEntry, _ error) error {
		// avoid recursive symlinks
		if file.IsDir() {
			if _, ok := visited[path]; ok {
				return filepath.SkipDir
			} else {
				visited[path] = struct{}{}
			}
		}

		// handle symlinked directories
		if file.Type()&os.ModeSymlink != 0 {
			err = os.Chdir(filepath.Dir(path))
			if err != nil {
				return err
			}

			realPath, err := filepath.EvalSymlinks(path)
			if err != nil {
				return err
			}

			file, err := os.Stat(realPath)
			if err != nil {
				return err
			}

			if file.IsDir() {
				err = os.Chdir(path)
				if err != nil {
					return err
				}

				stack.new()
				defer stack.pop()

				err = filepath.WalkDir(realPath, scanner)
				if err != nil {
					return err
				}

				results, err := stack.get()
				if err != nil {
					return err
				}

				for i := range *results {
					allResults = append(allResults, strings.Replace((*results)[i], realPath, path, 1))
				}

				return nil
			}
		}

		results, err := stack.get()
		if err != nil {
			return err
		}

		if utils.IsZip(path) && platform.ZipsAsDirs() {
			// zip files
			zipFiles, err := utils.ListZip(path)
			if err != nil {
				// skip invalid zip files
				return nil
			}

			for i := range zipFiles {
				abs := filepath.Join(path, zipFiles[i])
				if utils.MatchSystemFile(cfg, platform, (*system).ID, abs) {
					*results = append(*results, abs)
				}
			}
		} else {
			// regular files
			if utils.MatchSystemFile(cfg, platform, (*system).ID, path) {
				*results = append(*results, path)
			}
		}

		return nil
	}

	stack.new()
	defer stack.pop()

	root, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}

	err = os.Chdir(filepath.Dir(path))
	if err != nil {
		return nil, err
	}

	// handle symlinks on root game folder because WalkDir fails silently on them
	var realPath string
	if root.Mode()&os.ModeSymlink == 0 {
		realPath = path
	} else {
		realPath, err = filepath.EvalSymlinks(path)
		if err != nil {
			return nil, err
		}
	}

	realRoot, err := os.Stat(realPath)
	if err != nil {
		return nil, err
	}

	if !realRoot.IsDir() {
		return nil, fmt.Errorf("root is not a directory")
	}

	err = filepath.WalkDir(realPath, scanner)
	if err != nil {
		return nil, err
	}

	results, err := stack.get()
	if err != nil {
		return nil, err
	}

	allResults = append(allResults, *results...)

	// change root back to symlink
	if realPath != path {
		for i := range allResults {
			allResults[i] = strings.Replace(allResults[i], realPath, path, 1)
		}
	}

	err = os.Chdir(cwd)
	if err != nil {
		return nil, err
	}

	return allResults, nil
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
	fdb *database.Database,
	update func(IndexStatus),
) (int, error) {
	db := fdb.MediaDB
	status := IndexStatus{
		Total: len(systems) + 2, // estimate steps
		Step:  1,
	}

	scanState := database.ScanState{
		Systems:    make([]database.System, 1, 128),
		SystemIds:  make(map[string]int, 128),
		Titles:     make([]database.MediaTitle, 1, 131072),
		TitleIds:   make(map[string]int, 131072),
		Media:      make([]database.Media, 1, 131072),
		MediaIds:   make(map[string]int, 131072),
		TagTypes:   make([]database.TagType, 1, 16),
		TagTypeIds: make(map[string]int, 16),
		Tags:       make([]database.Tag, 1, 64),
		TagIds:     make(map[string]int, 64),
		MediaTags:  make([]database.MediaTag, 262144),
	}
	seedKnownTags(&scanState)

	filteredIds := make([]string, 0)
	for _, s := range systems {
		filteredIds = append(filteredIds, s.ID)
	}

	// Reset Media DB instead of selective rebuild
	db.Allocate()

	update(status)
	systemPaths := make(map[string][]string)
	for _, v := range GetSystemPaths(platform, platform.RootDirs(cfg), systems) {
		systemPaths[v.System.ID] = append(systemPaths[v.System.ID], v.Path)
	}

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
			if l.SystemID == k && l.Scanner != nil {
				log.Debug().Msgf("running %s scanner for system: %s", l.Id, systemId)
				var err error
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

		for _, p := range files {
			AddMediaPath(&scanState, systemId, p.Path)
		}
	}

	// run each custom scanner at least once, even if there are no paths
	// defined or results from regular index
	for _, l := range platform.Launchers() {
		systemId := l.SystemID
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
				for _, p := range results {
					AddMediaPath(&scanState, systemId, p.Path)
				}
			}
		}
	}

	// launcher scanners with no system defined are run against every system
	var anyScanners []platforms.Launcher
	for _, l := range platform.Launchers() {
		if l.SystemID == "" && l.Scanner != nil {
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

				for _, p := range results {
					AddMediaPath(&scanState, systemId, p.Path)
				}
			}
		}
	}

	status.Step++
	status.SystemId = ""
	update(status)

	err := db.ReindexFromScanState(&scanState)
	if err != nil {
		log.Error().Err(err).Msg("MediaDB sqlite bulk insert failed")
	}

	indexedSystems := make([]string, 0)
	log.Debug().Msgf("scanned systems: %v", scanned)
	for k, v := range scanned {
		if v {
			indexedSystems = append(indexedSystems, k)
		}
	}
	log.Debug().Msgf("indexed systems: %v", indexedSystems)

	return status.Files, nil
}
