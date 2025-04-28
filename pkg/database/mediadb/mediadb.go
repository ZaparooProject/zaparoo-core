package mediadb

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/pkg/utils"
	_ "modernc.org/sqlite"
)

const (
	BucketNames       = "names"
	indexedSystemsKey = "meta:indexedSystems"
)

var ERROR_NULL_SQL = errors.New("MediaDB is not connected")

type MediaDB struct {
	sql *sql.DB
	pl  platforms.Platform
}

func OpenMediaDB(pl platforms.Platform) (*MediaDB, error) {
	db := &MediaDB{sql: nil, pl: pl}
	err := db.Open()
	return db, err
}

func (db *MediaDB) Open() error {
	exists := true
	dbPath := filepath.Join(db.pl.DataDir(), config.MediaDbFile)
	fmt.Println(dbPath)
	_, err := os.Stat(dbPath)
	if err != nil {
		exists = false
		err := os.MkdirAll(filepath.Dir(dbPath), 0755)
		if err != nil {
			return err
		}
	}
	sql, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return err
	}
	db.sql = sql
	if !exists {
		return db.Allocate()
	}
	return nil
}

func (db *MediaDB) Exists() bool {
	return db.sql != nil
}

func (db *MediaDB) UnsafeGetSqlDb() *sql.DB {
	return db.sql
}

func (db *MediaDB) Truncate() error {
	if db.sql == nil {
		return ERROR_NULL_SQL
	}
	return sqlTruncate(db.sql)
}

func (db *MediaDB) Allocate() error {
	if db.sql == nil {
		return ERROR_NULL_SQL
	}
	return sqlAllocate(db.sql)
}

func (db *MediaDB) Vacuum() error {
	if db.sql == nil {
		return ERROR_NULL_SQL
	}
	return sqlVacuum(db.sql)
}

func (db *MediaDB) Close() error {
	if db.sql == nil {
		return nil
	}
	return db.sql.Close()
}

// Update the names index with the given files.
func (db *MediaDB) ReindexFromScanState(ss *database.ScanState) error {
	if db.sql == nil {
		return ERROR_NULL_SQL
	}

	// clear DB
	db.Allocate()

	// Clear unneeded state maps for GC
	ss.SystemIds = make(map[string]int)
	ss.TitleIds = make(map[string]int)
	ss.MediaIds = make(map[string]int)
	ss.TagTypeIds = make(map[string]int)
	ss.TagIds = make(map[string]int)

	var err error
	err = sqlBulkInsertSystems(db.sql, ss)
	if err != nil {
		return err
	}
	ss.Systems = make([]database.System, 0)

	err = sqlBulkInsertTitles(db.sql, ss)
	if err != nil {
		return err
	}
	ss.Titles = make([]database.MediaTitle, 0)

	err = sqlBulkInsertMedia(db.sql, ss)
	if err != nil {
		return err
	}
	ss.Media = make([]database.Media, 0)

	err = sqlBulkInsertTagTypes(db.sql, ss)
	if err != nil {
		return err
	}
	ss.TagTypes = make([]database.TagType, 0)

	err = sqlBulkInsertTags(db.sql, ss)
	if err != nil {
		return err
	}
	ss.Tags = make([]database.Tag, 0)

	err = sqlBulkInsertMediaTags(db.sql, ss)
	if err != nil {
		return err
	}
	ss.MediaTags = make([]database.MediaTag, 0)

	// Apply indexes
	sqlIndexTables(db.sql)

	return nil
}

// Return indexed names matching exact query (case insensitive).
func (db *MediaDB) SearchMediaPathExact(systems []systemdefs.System, query string) ([]database.SearchResult, error) {
	if db.sql == nil {
		return make([]database.SearchResult, 0), ERROR_NULL_SQL
	}
	return sqlSearchMediaPathExact(db.sql, systems, query)
}

// Return indexed names that include every word in query (case insensitive).
func (db *MediaDB) SearchMediaPathWords(systems []systemdefs.System, query string) ([]database.SearchResult, error) {
	if db.sql == nil {
		return make([]database.SearchResult, 0), ERROR_NULL_SQL
	}
	qWords := strings.Fields(strings.ToLower(query))
	return sqlSearchMediaPathParts(db.sql, systems, qWords)
}

// Glob pattern matching unclear on some patterns
func (db *MediaDB) SearchMediaPathGlob(systems []systemdefs.System, query string) ([]database.SearchResult, error) {
	// query == path like with possible *
	var nullResults []database.SearchResult
	if db.sql == nil {
		return nullResults, ERROR_NULL_SQL
	}
	var parts []string
	for _, part := range strings.Split(query, "*") {
		if part != "" {
			parts = append(parts, part)
		}
	}
	if len(parts) == 0 {
		// return random instead
		rnd, err := db.RandomGame(systems)
		if err != nil {
			return nullResults, err
		}
		return []database.SearchResult{rnd}, nil
	}

	return sqlSearchMediaPathParts(db.sql, systems, parts)
	// TODO since we approximated a glob, we should actually check
	// result paths against base glob to confirm
}

// Return true if a specific system is indexed in the gamesdb
func (db *MediaDB) SystemIndexed(system systemdefs.System) bool {
	if db.sql == nil {
		return false
	}
	return sqlSystemIndexed(db.sql, system)
}

// Return all systems indexed in the gamesdb
func (db *MediaDB) IndexedSystems() ([]string, error) {
	// JBONE: return string map of Systems.Key, Systems.Indexed
	var systems []string
	if db.sql == nil {
		return systems, ERROR_NULL_SQL
	}
	return sqlIndexedSystems(db.sql)
}

// Return a random game from specified systems.
func (db *MediaDB) RandomGame(systems []systemdefs.System) (database.SearchResult, error) {
	var result database.SearchResult
	if db.sql == nil {
		return result, ERROR_NULL_SQL
	}

	system, err := utils.RandomElem(systems)
	if err != nil {
		return result, err
	}

	return sqlRandomGame(db.sql, system)
}
