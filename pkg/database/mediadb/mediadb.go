// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later
//
// This file is part of Zaparoo Core.
//
// Zaparoo Core is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// Zaparoo Core is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with Zaparoo Core.  If not, see <http://www.gnu.org/licenses/>.

package mediadb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	_ "github.com/mattn/go-sqlite3"
	"github.com/rs/zerolog/log"
)

var ErrNullSQL = errors.New("MediaDB is not connected")

const sqliteConnParams = "?_journal_mode=WAL&_synchronous=NORMAL&_busy_timeout=5000" +
	"&_cache_size=-64000&_temp_store=MEMORY"

type MediaDB struct {
	sql *sql.DB
	pl  platforms.Platform
	ctx context.Context
	// Prepared statements for batch operations during scanning
	tx                   *sql.Tx
	stmtInsertSystem     *sql.Stmt
	stmtInsertMediaTitle *sql.Stmt
	stmtInsertMedia      *sql.Stmt
	stmtInsertTag        *sql.Stmt
	stmtInsertMediaTag   *sql.Stmt
}

func OpenMediaDB(ctx context.Context, pl platforms.Platform) (*MediaDB, error) {
	db := &MediaDB{sql: nil, pl: pl, ctx: ctx}
	err := db.Open()
	return db, err
}

func (db *MediaDB) Open() error {
	exists := true
	dbPath := db.GetDBPath()
	_, err := os.Stat(dbPath)
	if err != nil {
		exists = false
		mkdirErr := os.MkdirAll(filepath.Dir(dbPath), 0o750)
		if mkdirErr != nil {
			return fmt.Errorf("failed to create database directory: %w", mkdirErr)
		}
	}
	sqlInstance, err := sql.Open("sqlite3", dbPath+sqliteConnParams)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	db.sql = sqlInstance
	if !exists {
		return db.Allocate()
	}
	return nil
}

func (db *MediaDB) GetDBPath() string {
	return filepath.Join(helpers.DataDir(db.pl), config.MediaDbFile)
}

func (db *MediaDB) Exists() bool {
	return db.sql != nil
}

func (db *MediaDB) UpdateLastGenerated() error {
	if db.sql == nil {
		return ErrNullSQL
	}
	return sqlUpdateLastGenerated(db.ctx, db.sql)
}

func (db *MediaDB) GetLastGenerated() (time.Time, error) {
	if db.sql == nil {
		return time.Time{}, ErrNullSQL
	}
	return sqlGetLastGenerated(db.ctx, db.sql)
}

func (db *MediaDB) UnsafeGetSQLDb() *sql.DB {
	return db.sql
}

func (db *MediaDB) Truncate() error {
	if db.sql == nil {
		return ErrNullSQL
	}
	return sqlTruncate(db.ctx, db.sql)
}

func (db *MediaDB) Allocate() error {
	if db.sql == nil {
		return ErrNullSQL
	}
	return sqlAllocate(db.sql)
}

func (db *MediaDB) MigrateUp() error {
	if db.sql == nil {
		return ErrNullSQL
	}
	return sqlMigrateUp(db.sql)
}

func (db *MediaDB) Vacuum() error {
	if db.sql == nil {
		return ErrNullSQL
	}
	return sqlVacuum(db.ctx, db.sql)
}

func (db *MediaDB) Close() error {
	if db.sql == nil {
		return nil
	}
	err := db.sql.Close()
	if err != nil {
		return fmt.Errorf("failed to close database: %w", err)
	}
	return nil
}

// SetSQLForTesting allows injection of a sql.DB instance for testing purposes.
// This method should only be used in tests to set up in-memory databases.
func (db *MediaDB) SetSQLForTesting(ctx context.Context, sqlDB *sql.DB, platform platforms.Platform) error {
	db.sql = sqlDB
	db.ctx = ctx
	db.pl = platform

	// Initialize the database schema
	return db.Allocate()
}

// closeAllPreparedStatements closes all prepared statements and sets them to nil
func (db *MediaDB) closeAllPreparedStatements() {
	if db.stmtInsertSystem != nil {
		if closeErr := db.stmtInsertSystem.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close prepared statement: stmtInsertSystem")
		}
		db.stmtInsertSystem = nil
	}
	if db.stmtInsertMediaTitle != nil {
		if closeErr := db.stmtInsertMediaTitle.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close prepared statement: stmtInsertMediaTitle")
		}
		db.stmtInsertMediaTitle = nil
	}
	if db.stmtInsertMedia != nil {
		if closeErr := db.stmtInsertMedia.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close prepared statement: stmtInsertMedia")
		}
		db.stmtInsertMedia = nil
	}
	if db.stmtInsertTag != nil {
		if closeErr := db.stmtInsertTag.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close prepared statement: stmtInsertTag")
		}
		db.stmtInsertTag = nil
	}
	if db.stmtInsertMediaTag != nil {
		if closeErr := db.stmtInsertMediaTag.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close prepared statement: stmtInsertMediaTag")
		}
		db.stmtInsertMediaTag = nil
	}
}

// RollbackTransaction rolls back the current transaction and cleans up resources
func (db *MediaDB) RollbackTransaction() error {
	if db.tx == nil {
		return nil // No active transaction
	}

	// Clean up prepared statements first
	db.closeAllPreparedStatements()

	// Rollback the transaction
	err := db.tx.Rollback()
	db.tx = nil
	if err != nil {
		return fmt.Errorf("failed to rollback transaction: %w", err)
	}

	return nil
}

// rollbackAndLogError helper function to handle rollback with error logging
func (db *MediaDB) rollbackAndLogError() {
	if rbErr := db.RollbackTransaction(); rbErr != nil {
		log.Error().Err(rbErr).Msg("failed to rollback transaction during prepared statement setup")
	}
}

func (db *MediaDB) BeginTransaction() error {
	if db.sql == nil {
		return ErrNullSQL
	}

	// Begin a proper transaction
	tx, err := db.sql.BeginTx(db.ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	db.tx = tx

	// Prepare statements for batch operations - clean up on any error
	if db.stmtInsertSystem, err = tx.PrepareContext(db.ctx, insertSystemSQL); err != nil {
		db.rollbackAndLogError()
		return fmt.Errorf("failed to prepare insert system statement: %w", err)
	}

	if db.stmtInsertMediaTitle, err = tx.PrepareContext(db.ctx, insertMediaTitleSQL); err != nil {
		db.rollbackAndLogError()
		return fmt.Errorf("failed to prepare insert media title statement: %w", err)
	}

	if db.stmtInsertMedia, err = tx.PrepareContext(db.ctx, insertMediaSQL); err != nil {
		db.rollbackAndLogError()
		return fmt.Errorf("failed to prepare insert media statement: %w", err)
	}

	if db.stmtInsertTag, err = tx.PrepareContext(db.ctx, insertTagSQL); err != nil {
		db.rollbackAndLogError()
		return fmt.Errorf("failed to prepare insert tag statement: %w", err)
	}

	if db.stmtInsertMediaTag, err = tx.PrepareContext(db.ctx, insertMediaTagSQL); err != nil {
		db.rollbackAndLogError()
		return fmt.Errorf("failed to prepare insert media tag statement: %w", err)
	}

	return nil
}

func (db *MediaDB) CommitTransaction() error {
	if db.tx == nil {
		return nil // No active transaction
	}

	// Always clean up prepared statements first
	db.closeAllPreparedStatements()

	// Commit the transaction
	err := db.tx.Commit()
	if err != nil {
		// Try to rollback and combine errors if both fail
		if rbErr := db.tx.Rollback(); rbErr != nil {
			db.tx = nil
			return fmt.Errorf("commit failed: %w; rollback also failed: %w", err, rbErr)
		}
		db.tx = nil
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	db.tx = nil
	return nil
}

func (db *MediaDB) reindexTablesWithTransaction() error {
	return sqlIndexTablesWithTransaction(db.ctx, db.tx)
}

func (db *MediaDB) insertSystemWithPreparedStmt(row database.System) (database.System, error) {
	return sqlInsertSystemWithPreparedStmt(db.ctx, db.stmtInsertSystem, row)
}

func (db *MediaDB) insertMediaTitleWithPreparedStmt(row database.MediaTitle) (database.MediaTitle, error) {
	return sqlInsertMediaTitleWithPreparedStmt(db.ctx, db.stmtInsertMediaTitle, row)
}

func (db *MediaDB) insertMediaWithPreparedStmt(row database.Media) (database.Media, error) {
	return sqlInsertMediaWithPreparedStmt(db.ctx, db.stmtInsertMedia, row)
}

func (db *MediaDB) insertTagWithPreparedStmt(row database.Tag) (database.Tag, error) {
	return sqlInsertTagWithPreparedStmt(db.ctx, db.stmtInsertTag, row)
}

func (db *MediaDB) insertMediaTagWithPreparedStmt(row database.MediaTag) (database.MediaTag, error) {
	return sqlInsertMediaTagWithPreparedStmt(db.ctx, db.stmtInsertMediaTag, row)
}

func (db *MediaDB) ReindexTables() error {
	// Use transaction if active, otherwise use direct database connection
	if db.tx != nil {
		return db.reindexTablesWithTransaction()
	}
	return sqlIndexTables(db.ctx, db.sql)
}

// SearchMediaPathExact returns indexed names matching an exact query (case-insensitive).
func (db *MediaDB) SearchMediaPathExact(systems []systemdefs.System, query string) ([]database.SearchResult, error) {
	if db.sql == nil {
		return make([]database.SearchResult, 0), ErrNullSQL
	}
	return sqlSearchMediaPathExact(db.ctx, db.sql, systems, query)
}

// SearchMediaPathWords returns indexed names that include every word in a query (case-insensitive).
func (db *MediaDB) SearchMediaPathWords(systems []systemdefs.System, query string) ([]database.SearchResult, error) {
	if db.sql == nil {
		return make([]database.SearchResult, 0), ErrNullSQL
	}
	qWords := strings.Fields(strings.ToLower(query))
	return sqlSearchMediaPathParts(db.ctx, db.sql, systems, qWords)
}

func (db *MediaDB) SearchMediaPathGlob(systems []systemdefs.System, query string) ([]database.SearchResult, error) {
	// TODO: glob pattern matching unclear on some patterns
	// query == path like with possible *
	var nullResults []database.SearchResult
	if db.sql == nil {
		return nullResults, ErrNullSQL
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
	return sqlSearchMediaPathParts(db.ctx, db.sql, systems, parts)
}

// SystemIndexed returns true if a specific system is indexed in the media database.
func (db *MediaDB) SystemIndexed(system systemdefs.System) bool {
	if db.sql == nil {
		return false
	}
	return sqlSystemIndexed(db.ctx, db.sql, system)
}

// IndexedSystems returns all systems indexed in the media database.
func (db *MediaDB) IndexedSystems() ([]string, error) {
	// TODO: what is a JBONE??
	// JBONE: return string map of Systems.Key, Systems.Indexed
	var systems []string
	if db.sql == nil {
		return systems, ErrNullSQL
	}
	return sqlIndexedSystems(db.ctx, db.sql)
}

// RandomGame returns a random game from specified systems.
func (db *MediaDB) RandomGame(systems []systemdefs.System) (database.SearchResult, error) {
	var result database.SearchResult
	if db.sql == nil {
		return result, ErrNullSQL
	}

	system, err := helpers.RandomElem(systems)
	if err != nil {
		return result, fmt.Errorf("failed to select random system: %w", err)
	}

	return sqlRandomGame(db.ctx, db.sql, system)
}

func (db *MediaDB) FindSystem(row database.System) (database.System, error) {
	return sqlFindSystem(db.ctx, db.sql, row)
}

func (db *MediaDB) InsertSystem(row database.System) (database.System, error) {
	// Use prepared statement if in transaction, otherwise fall back to original method
	if db.stmtInsertSystem != nil {
		return db.insertSystemWithPreparedStmt(row)
	}
	return sqlInsertSystem(db.ctx, db.sql, row)
}

func (db *MediaDB) FindOrInsertSystem(row database.System) (database.System, error) {
	system, err := db.FindSystem(row)
	if errors.Is(err, sql.ErrNoRows) {
		system, err = db.InsertSystem(row)
	}
	return system, err
}

func (db *MediaDB) FindMediaTitle(row database.MediaTitle) (database.MediaTitle, error) {
	return sqlFindMediaTitle(db.ctx, db.sql, row)
}

func (db *MediaDB) InsertMediaTitle(row database.MediaTitle) (database.MediaTitle, error) {
	// Use prepared statement if in transaction, otherwise fall back to original method
	if db.stmtInsertMediaTitle != nil {
		return db.insertMediaTitleWithPreparedStmt(row)
	}
	return sqlInsertMediaTitle(db.ctx, db.sql, row)
}

func (db *MediaDB) FindOrInsertMediaTitle(row database.MediaTitle) (database.MediaTitle, error) {
	system, err := db.FindMediaTitle(row)
	if errors.Is(err, sql.ErrNoRows) {
		system, err = db.InsertMediaTitle(row)
	}
	return system, err
}

func (db *MediaDB) FindMedia(row database.Media) (database.Media, error) {
	return sqlFindMedia(db.ctx, db.sql, row)
}

func (db *MediaDB) InsertMedia(row database.Media) (database.Media, error) {
	// Use prepared statement if in transaction, otherwise fall back to original method
	if db.stmtInsertMedia != nil {
		return db.insertMediaWithPreparedStmt(row)
	}
	return sqlInsertMedia(db.ctx, db.sql, row)
}

func (db *MediaDB) FindOrInsertMedia(row database.Media) (database.Media, error) {
	system, err := db.FindMedia(row)
	if errors.Is(err, sql.ErrNoRows) {
		system, err = db.InsertMedia(row)
	}
	return system, err
}

func (db *MediaDB) FindTagType(row database.TagType) (database.TagType, error) {
	return sqlFindTagType(db.ctx, db.sql, row)
}

func (db *MediaDB) InsertTagType(row database.TagType) (database.TagType, error) {
	return sqlInsertTagType(db.ctx, db.sql, row)
}

func (db *MediaDB) FindOrInsertTagType(row database.TagType) (database.TagType, error) {
	system, err := db.FindTagType(row)
	if errors.Is(err, sql.ErrNoRows) {
		system, err = db.InsertTagType(row)
	}
	return system, err
}

func (db *MediaDB) FindTag(row database.Tag) (database.Tag, error) {
	return sqlFindTag(db.ctx, db.sql, row)
}

func (db *MediaDB) InsertTag(row database.Tag) (database.Tag, error) {
	// Use prepared statement if in transaction, otherwise fall back to original method
	if db.stmtInsertTag != nil {
		return db.insertTagWithPreparedStmt(row)
	}
	return sqlInsertTag(db.ctx, db.sql, row)
}

func (db *MediaDB) FindOrInsertTag(row database.Tag) (database.Tag, error) {
	system, err := db.FindTag(row)
	if errors.Is(err, sql.ErrNoRows) {
		system, err = db.InsertTag(row)
	}
	return system, err
}

func (db *MediaDB) FindMediaTag(row database.MediaTag) (database.MediaTag, error) {
	return sqlFindMediaTag(db.ctx, db.sql, row)
}

func (db *MediaDB) InsertMediaTag(row database.MediaTag) (database.MediaTag, error) {
	// Use prepared statement if in transaction, otherwise fall back to original method
	if db.stmtInsertMediaTag != nil {
		return db.insertMediaTagWithPreparedStmt(row)
	}
	return sqlInsertMediaTag(db.ctx, db.sql, row)
}

func (db *MediaDB) FindOrInsertMediaTag(row database.MediaTag) (database.MediaTag, error) {
	system, err := db.FindMediaTag(row)
	if errors.Is(err, sql.ErrNoRows) {
		system, err = db.InsertMediaTag(row)
	}
	return system, err
}

func (db *MediaDB) FindMediaTitleTag(row database.MediaTitleTag) (database.MediaTitleTag, error) {
	return sqlFindMediaTitleTag(db.ctx, db.sql, row)
}

func (db *MediaDB) InsertMediaTitleTag(row database.MediaTitleTag) (database.MediaTitleTag, error) {
	return sqlInsertMediaTitleTag(db.ctx, db.sql, row)
}

func (db *MediaDB) FindOrInsertMediaTitleTag(row database.MediaTitleTag) (database.MediaTitleTag, error) {
	result, err := db.FindMediaTitleTag(row)
	if errors.Is(err, sql.ErrNoRows) {
		result, err = db.InsertMediaTitleTag(row)
	}
	return result, err
}

// Scraper metadata methods

func (db *MediaDB) GetGamesWithoutMetadata(systemID string, limit int) ([]database.MediaTitle, error) {
	if db.sql == nil {
		return nil, ErrNullSQL
	}

	// Find games that don't have a 'scraper_source' tag, which indicates they haven't been scraped
	query := `SELECT mt.DBID, mt.SystemDBID, mt.Slug, mt.Name
		FROM MediaTitles mt
		JOIN Systems s ON s.DBID = mt.SystemDBID
		LEFT JOIN (
			SELECT DISTINCT mtt.MediaTitleDBID
			FROM MediaTitleTags mtt
			JOIN Tags t ON t.DBID = mtt.TagDBID
			JOIN TagTypes tt ON tt.DBID = t.TypeDBID
			WHERE tt.Type = 'scraper_source'
		) scraped_titles ON scraped_titles.MediaTitleDBID = mt.DBID
		WHERE s.SystemID = ? AND scraped_titles.MediaTitleDBID IS NULL
		LIMIT ?`

	rows, err := db.sql.QueryContext(db.ctx, query, systemID, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query media: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("Failed to close rows")
		}
	}()

	titles := make([]database.MediaTitle, 0, limit)
	for rows.Next() {
		var title database.MediaTitle
		err := rows.Scan(&title.DBID, &title.SystemDBID, &title.Slug, &title.Name)
		if err != nil {
			return nil, fmt.Errorf("failed to scan media title row: %w", err)
		}
		titles = append(titles, title)
	}

	return titles, rows.Err()
}

func (db *MediaDB) HasScraperMetadata(mediaTitleDBID int64) (bool, error) {
	if db.sql == nil {
		return false, ErrNullSQL
	}

	query := `SELECT EXISTS(
		SELECT 1 FROM MediaTitleTags mtt
		JOIN Tags t ON t.DBID = mtt.TagDBID
		JOIN TagTypes tt ON tt.DBID = t.TypeDBID
		WHERE mtt.MediaTitleDBID = ? AND tt.Type = 'scraper_source'
	)`

	var exists bool
	err := db.sql.QueryRowContext(db.ctx, query, mediaTitleDBID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("failed to check scraper metadata existence: %w", err)
	}

	return exists, nil
}

// GetTagsForMediaTitle retrieves all tags for a MediaTitle as a map of tagType -> tagValue
func (db *MediaDB) GetTagsForMediaTitle(mediaTitleDBID int64) (map[string]string, error) {
	if db.sql == nil {
		return nil, ErrNullSQL
	}

	query := `SELECT tt.Type, t.Tag
		FROM MediaTitleTags mtt
		JOIN Tags t ON t.DBID = mtt.TagDBID
		JOIN TagTypes tt ON tt.DBID = t.TypeDBID
		WHERE mtt.MediaTitleDBID = ?`

	rows, err := db.sql.QueryContext(db.ctx, query, mediaTitleDBID)
	if err != nil {
		return nil, fmt.Errorf("failed to query tags for media title: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("Failed to close rows")
		}
	}()

	tags := make(map[string]string)
	for rows.Next() {
		var tagType, tagValue string
		if err := rows.Scan(&tagType, &tagValue); err != nil {
			return nil, fmt.Errorf("failed to scan tag row: %w", err)
		}
		tags[tagType] = tagValue
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return tags, nil
}

func (db *MediaDB) GetMediaTitlesBySystem(systemID string) ([]database.MediaTitle, error) {
	if db.sql == nil {
		return nil, ErrNullSQL
	}

	query := `SELECT mt.DBID, mt.SystemDBID, mt.Slug, mt.Name
		FROM MediaTitles mt
		JOIN Systems s ON s.DBID = mt.SystemDBID
		WHERE s.SystemID = ?`

	rows, err := db.sql.QueryContext(db.ctx, query, systemID)
	if err != nil {
		return nil, fmt.Errorf("failed to query media titles by system: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("Failed to close rows")
		}
	}()

	titles := make([]database.MediaTitle, 0, 100)
	for rows.Next() {
		var title database.MediaTitle
		err := rows.Scan(&title.DBID, &title.SystemDBID, &title.Slug, &title.Name)
		if err != nil {
			return nil, fmt.Errorf("failed to scan media title row in GetMediaTitlesBySystem: %w", err)
		}
		titles = append(titles, title)
	}

	return titles, rows.Err()
}

func (db *MediaDB) GetMediaByID(mediaDBID int64) (*database.Media, error) {
	if db.sql == nil {
		return nil, ErrNullSQL
	}

	query := `SELECT DBID, MediaTitleDBID, Path FROM Media WHERE DBID = ?`
	row := db.sql.QueryRowContext(db.ctx, query, mediaDBID)

	var media database.Media
	err := row.Scan(&media.DBID, &media.MediaTitleDBID, &media.Path)
	if err != nil {
		return nil, fmt.Errorf("failed to scan media row: %w", err)
	}

	return &media, nil
}

func (db *MediaDB) GetMediaTitleByID(mediaTitleDBID int64) (*database.MediaTitle, error) {
	if db.sql == nil {
		return nil, ErrNullSQL
	}

	query := `SELECT DBID, SystemDBID, Slug, Name FROM MediaTitles WHERE DBID = ?`
	row := db.sql.QueryRowContext(db.ctx, query, mediaTitleDBID)

	var title database.MediaTitle
	err := row.Scan(&title.DBID, &title.SystemDBID, &title.Slug, &title.Name)
	if err != nil {
		return nil, fmt.Errorf("failed to scan media title by ID: %w", err)
	}

	return &title, nil
}

func (db *MediaDB) GetSystemByID(systemDBID int64) (*database.System, error) {
	if db.sql == nil {
		return nil, ErrNullSQL
	}

	query := `SELECT DBID, SystemID, Name FROM Systems WHERE DBID = ?`
	row := db.sql.QueryRowContext(db.ctx, query, systemDBID)

	var system database.System
	err := row.Scan(&system.DBID, &system.SystemID, &system.Name)
	if err != nil {
		return nil, fmt.Errorf("failed to scan system row: %w", err)
	}

	return &system, nil
}

// Game hash methods

func (db *MediaDB) SaveGameHashes(hashes *database.GameHashes) error {
	if db.sql == nil {
		return ErrNullSQL
	}

	query := `INSERT OR REPLACE INTO MediaHashes
		(SystemID, MediaPath, ComputedAt, FileSize, CRC32, MD5, SHA1)
		VALUES (?, ?, ?, ?, ?, ?, ?)`

	_, err := db.sql.ExecContext(db.ctx, query,
		hashes.SystemID,
		hashes.MediaPath,
		hashes.ComputedAt.Unix(),
		hashes.FileSize,
		hashes.CRC32,
		hashes.MD5,
		hashes.SHA1,
	)
	return fmt.Errorf("failed to save game hashes: %w", err)
}

func (db *MediaDB) GetGameHashes(systemID, mediaPath string) (*database.GameHashes, error) {
	if db.sql == nil {
		return nil, ErrNullSQL
	}

	query := `SELECT DBID, SystemID, MediaPath, ComputedAt, FileSize, CRC32, MD5, SHA1
		FROM MediaHashes WHERE SystemID = ? AND MediaPath = ?`

	row := db.sql.QueryRowContext(db.ctx, query, systemID, mediaPath)

	var hashes database.GameHashes
	var computedAtUnix int64

	err := row.Scan(
		&hashes.DBID,
		&hashes.SystemID,
		&hashes.MediaPath,
		&computedAtUnix,
		&hashes.FileSize,
		&hashes.CRC32,
		&hashes.MD5,
		&hashes.SHA1,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to scan game hashes row: %w", err)
	}

	hashes.ComputedAt = time.Unix(computedAtUnix, 0)
	return &hashes, nil
}

func (db *MediaDB) FindGameByHash(crc32, md5, sha1 string) ([]database.Media, error) {
	if db.sql == nil {
		return nil, ErrNullSQL
	}

	query := `SELECT m.DBID, m.MediaTitleDBID, m.Path
		FROM Media m
		JOIN MediaTitles mt ON mt.DBID = m.MediaTitleDBID
		JOIN Systems s ON s.DBID = mt.SystemDBID
		JOIN MediaHashes mh ON mh.SystemID = s.SystemID AND mh.MediaPath = m.Path
		WHERE (? != '' AND mh.CRC32 = ?)
		   OR (? != '' AND mh.MD5 = ?)
		   OR (? != '' AND mh.SHA1 = ?)`

	rows, err := db.sql.QueryContext(db.ctx, query, crc32, crc32, md5, md5, sha1, sha1)
	if err != nil {
		return nil, fmt.Errorf("failed to query games by hash: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("Failed to close rows")
		}
	}()

	media := make([]database.Media, 0, 10)
	for rows.Next() {
		var m database.Media
		err := rows.Scan(&m.DBID, &m.MediaTitleDBID, &m.Path)
		if err != nil {
			return nil, fmt.Errorf("failed to scan media row in FindGameByHash: %w", err)
		}
		media = append(media, m)
	}

	return media, rows.Err()
}
