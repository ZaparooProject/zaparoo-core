package mediadb

import (
	"database/sql"
	"errors"
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

var ErrorNullSql = errors.New("MediaDB is not connected")

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
	dbPath := db.GetDBPath()
	_, err := os.Stat(dbPath)
	if err != nil {
		exists = false
		err := os.MkdirAll(filepath.Dir(dbPath), 0755)
		if err != nil {
			return err
		}
	}
	sqlInstance, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return err
	}
	db.sql = sqlInstance
	if !exists {
		return db.Allocate()
	}
	return nil
}

func (db *MediaDB) GetDBPath() string {
	return filepath.Join(db.pl.DataDir(), config.MediaDbFile)
}

func (db *MediaDB) Exists() bool {
	return db.sql != nil
}

func (db *MediaDB) UnsafeGetSqlDb() *sql.DB {
	return db.sql
}

func (db *MediaDB) Truncate() error {
	if db.sql == nil {
		return ErrorNullSql
	}
	return sqlTruncate(db.sql)
}

func (db *MediaDB) Allocate() error {
	if db.sql == nil {
		return ErrorNullSql
	}
	return sqlAllocate(db.sql)
}

func (db *MediaDB) Vacuum() error {
	if db.sql == nil {
		return ErrorNullSql
	}
	return sqlVacuum(db.sql)
}

func (db *MediaDB) Close() error {
	if db.sql == nil {
		return nil
	}
	return db.sql.Close()
}

func (db *MediaDB) BeginTransaction() error {
	return sqlBeginTransaction(db.sql)
}

func (db *MediaDB) CommitTransaction() error {
	return sqlCommitTransaction(db.sql)
}

func (db *MediaDB) ReindexTables() error {
	return sqlIndexTables(db.sql)
}

// SearchMediaPathExact returns indexed names matching an exact query (case-insensitive).
func (db *MediaDB) SearchMediaPathExact(systems []systemdefs.System, query string) ([]database.SearchResult, error) {
	if db.sql == nil {
		return make([]database.SearchResult, 0), ErrorNullSql
	}
	return sqlSearchMediaPathExact(db.sql, systems, query)
}

// SearchMediaPathWords returns indexed names that include every word in a query (case-insensitive).
func (db *MediaDB) SearchMediaPathWords(systems []systemdefs.System, query string) ([]database.SearchResult, error) {
	if db.sql == nil {
		return make([]database.SearchResult, 0), ErrorNullSql
	}
	qWords := strings.Fields(strings.ToLower(query))
	return sqlSearchMediaPathParts(db.sql, systems, qWords)
}

func (db *MediaDB) SearchMediaPathGlob(systems []systemdefs.System, query string) ([]database.SearchResult, error) {
	// TODO: glob pattern matching unclear on some patterns
	// query == path like with possible *
	var nullResults []database.SearchResult
	if db.sql == nil {
		return nullResults, ErrorNullSql
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

	// TODO: since we approximated a glob, we should actually check
	//       result paths against base glob to confirm
	return sqlSearchMediaPathParts(db.sql, systems, parts)
}

// SystemIndexed returns true if a specific system is indexed in the media database.
func (db *MediaDB) SystemIndexed(system systemdefs.System) bool {
	if db.sql == nil {
		return false
	}
	return sqlSystemIndexed(db.sql, system)
}

// IndexedSystems returns all systems indexed in the media database.
func (db *MediaDB) IndexedSystems() ([]string, error) {
	// TODO: what is a JBONE??
	// JBONE: return string map of Systems.Key, Systems.Indexed
	var systems []string
	if db.sql == nil {
		return systems, ErrorNullSql
	}
	return sqlIndexedSystems(db.sql)
}

// RandomGame returns a random game from specified systems.
func (db *MediaDB) RandomGame(systems []systemdefs.System) (database.SearchResult, error) {
	var result database.SearchResult
	if db.sql == nil {
		return result, ErrorNullSql
	}

	system, err := utils.RandomElem(systems)
	if err != nil {
		return result, err
	}

	return sqlRandomGame(db.sql, system)
}

func (db *MediaDB) FindSystem(row database.System) (database.System, error) {
	return sqlFindSystem(db.sql, row)
}

func (db *MediaDB) InsertSystem(row database.System) (database.System, error) {
	return sqlInsertSystem(db.sql, row)
}

func (db *MediaDB) FindOrInsertSystem(row database.System) (database.System, error) {
	system, err := db.FindSystem(row)
	if errors.Is(err, sql.ErrNoRows) {
		system, err = db.InsertSystem(row)
	}
	return system, err
}

func (db *MediaDB) FindMediaTitle(row database.MediaTitle) (database.MediaTitle, error) {
	return sqlFindMediaTitle(db.sql, row)
}

func (db *MediaDB) InsertMediaTitle(row database.MediaTitle) (database.MediaTitle, error) {
	return sqlInsertMediaTitle(db.sql, row)
}

func (db *MediaDB) FindOrInsertMediaTitle(row database.MediaTitle) (database.MediaTitle, error) {
	system, err := db.FindMediaTitle(row)
	if errors.Is(err, sql.ErrNoRows) {
		system, err = db.InsertMediaTitle(row)
	}
	return system, err
}

func (db *MediaDB) FindMedia(row database.Media) (database.Media, error) {
	return sqlFindMedia(db.sql, row)
}

func (db *MediaDB) InsertMedia(row database.Media) (database.Media, error) {
	return sqlInsertMedia(db.sql, row)
}

func (db *MediaDB) FindOrInsertMedia(row database.Media) (database.Media, error) {
	system, err := db.FindMedia(row)
	if errors.Is(err, sql.ErrNoRows) {
		system, err = db.InsertMedia(row)
	}
	return system, err
}

func (db *MediaDB) FindTagType(row database.TagType) (database.TagType, error) {
	return sqlFindTagType(db.sql, row)
}

func (db *MediaDB) InsertTagType(row database.TagType) (database.TagType, error) {
	return sqlInsertTagType(db.sql, row)
}

func (db *MediaDB) FindOrInsertTagType(row database.TagType) (database.TagType, error) {
	system, err := db.FindTagType(row)
	if errors.Is(err, sql.ErrNoRows) {
		system, err = db.InsertTagType(row)
	}
	return system, err
}

func (db *MediaDB) FindTag(row database.Tag) (database.Tag, error) {
	return sqlFindTag(db.sql, row)
}

func (db *MediaDB) InsertTag(row database.Tag) (database.Tag, error) {
	return sqlInsertTag(db.sql, row)
}

func (db *MediaDB) FindOrInsertTag(row database.Tag) (database.Tag, error) {
	system, err := db.FindTag(row)
	if errors.Is(err, sql.ErrNoRows) {
		system, err = db.InsertTag(row)
	}
	return system, err
}

func (db *MediaDB) FindMediaTag(row database.MediaTag) (database.MediaTag, error) {
	return sqlFindMediaTag(db.sql, row)
}

func (db *MediaDB) InsertMediaTag(row database.MediaTag) (database.MediaTag, error) {
	return sqlInsertMediaTag(db.sql, row)
}

func (db *MediaDB) FindOrInsertMediaTag(row database.MediaTag) (database.MediaTag, error) {
	system, err := db.FindMediaTag(row)
	if errors.Is(err, sql.ErrNoRows) {
		system, err = db.InsertMediaTag(row)
	}
	return system, err
}
