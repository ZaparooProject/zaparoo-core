// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
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
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/rs/zerolog/log"
)

// FindMediaBySystemAndPath returns the Media row for the given system and path,
// or nil, nil when not found.
func (db *MediaDB) FindMediaBySystemAndPath(
	ctx context.Context, systemDBID int64, path string,
) (*database.Media, error) {
	if db.sql.Load() == nil {
		return nil, ErrNullSQL
	}
	stmt, err := db.sql.Load().PrepareContext(ctx, `
		SELECT DBID, MediaTitleDBID, SystemDBID, Path, ParentDir, IsMissing
		FROM Media
		WHERE SystemDBID = ? AND Path = ?
		LIMIT 1
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare FindMediaBySystemAndPath: %w", err)
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()

	var row database.Media
	err = stmt.QueryRowContext(ctx, systemDBID, path).Scan(
		&row.DBID,
		&row.MediaTitleDBID,
		&row.SystemDBID,
		&row.Path,
		&row.ParentDir,
		&row.IsMissing,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil //nolint:nilnil // sql.ErrNoRows means not found; nil result is the "not found" sentinel
	}
	if err != nil {
		return nil, fmt.Errorf("failed to scan FindMediaBySystemAndPath: %w", err)
	}
	return &row, nil
}

func (db *MediaDB) FindMediaBySystemAndPaths(
	ctx context.Context, systemDBID int64, paths []string,
) (map[string]database.Media, error) {
	results := make(map[string]database.Media, len(paths))
	if len(paths) == 0 {
		return results, nil
	}
	if db.sql.Load() == nil {
		return nil, ErrNullSQL
	}

	args := make([]any, 0, len(paths)+1)
	args = append(args, systemDBID)
	for _, path := range paths {
		args = append(args, path)
	}

	//nolint:gosec // Safe: prepareVariadic only generates SQL placeholders like "?, ?, ?".
	rows, err := db.sql.Load().QueryContext(ctx, `
		SELECT DBID, MediaTitleDBID, SystemDBID, Path, ParentDir, IsMissing
		FROM Media
		WHERE SystemDBID = ? AND Path IN (`+prepareVariadic("?", ",", len(paths))+`)
	`, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query FindMediaBySystemAndPaths: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close rows")
		}
	}()

	for rows.Next() {
		var row database.Media
		if err := rows.Scan(
			&row.DBID,
			&row.MediaTitleDBID,
			&row.SystemDBID,
			&row.Path,
			&row.ParentDir,
			&row.IsMissing,
		); err != nil {
			return nil, fmt.Errorf("failed to scan FindMediaBySystemAndPaths: %w", err)
		}
		results[row.Path] = row
	}
	return results, rows.Err()
}

func (db *MediaDB) FindMediaIDsByPaths(
	ctx context.Context, paths []string,
) ([]database.MediaPathID, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	if db.sql.Load() == nil {
		return nil, ErrNullSQL
	}

	uniquePaths := make([]string, 0, len(paths))
	seenPaths := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		if _, ok := seenPaths[path]; ok {
			continue
		}
		seenPaths[path] = struct{}{}
		uniquePaths = append(uniquePaths, path)
	}

	results := make([]database.MediaPathID, 0, len(uniquePaths))
	for start := 0; start < len(uniquePaths); start += sqliteMaxParams {
		end := min(start+sqliteMaxParams, len(uniquePaths))
		batchResults, err := findMediaIDsByPathBatch(ctx, db.sql.Load(), uniquePaths[start:end])
		if err != nil {
			return nil, err
		}
		results = append(results, batchResults...)
	}
	return results, nil
}

func findMediaIDsByPathBatch(ctx context.Context, db sqlQueryable, paths []string) ([]database.MediaPathID, error) {
	args := make([]any, 0, len(paths))
	for _, path := range paths {
		args = append(args, path)
	}

	//nolint:gosec // Safe: prepareVariadic only generates SQL placeholders like "?, ?, ?".
	rows, err := db.QueryContext(ctx, `
		SELECT s.SystemID, m.Path, m.DBID
		FROM Media m
		INNER JOIN Systems s ON m.SystemDBID = s.DBID
		WHERE m.Path IN (`+prepareVariadic("?", ",", len(paths))+`)
	`, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query FindMediaIDsByPaths: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close rows")
		}
	}()

	results := make([]database.MediaPathID, 0, len(paths))
	for rows.Next() {
		var row database.MediaPathID
		if err := rows.Scan(&row.SystemID, &row.Path, &row.DBID); err != nil {
			return nil, fmt.Errorf("failed to scan FindMediaIDsByPaths: %w", err)
		}
		results = append(results, row)
	}
	return results, rows.Err()
}

func (db *MediaDB) FindSingleContainerLaunchMedia(
	ctx context.Context, systemDBID int64, containerPath string,
) (*database.Media, error) {
	if db.sql.Load() == nil {
		return nil, ErrNullSQL
	}

	prefix := strings.TrimRight(containerPath, "/") + "/"
	hasNested, err := containerHasNestedMedia(ctx, db.sql.Load(), systemDBID, prefix)
	if err != nil {
		return nil, err
	}
	if hasNested {
		return nil, nil //nolint:nilnil // nested containers remain browseable, not launch aliases
	}

	rows, err := db.sql.Load().QueryContext(ctx, `
		SELECT DBID, MediaTitleDBID, SystemDBID, Path, ParentDir, IsMissing
		FROM Media
		WHERE SystemDBID = ? AND IsMissing = 0 AND ParentDir = ?
		ORDER BY Path ASC, DBID ASC
	`, systemDBID, prefix)
	if err != nil {
		return nil, fmt.Errorf("failed to query FindSingleContainerLaunchMedia: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close rows")
		}
	}()

	var matches []database.Media
	for rows.Next() {
		var row database.Media
		if scanErr := rows.Scan(
			&row.DBID,
			&row.MediaTitleDBID,
			&row.SystemDBID,
			&row.Path,
			&row.ParentDir,
			&row.IsMissing,
		); scanErr != nil {
			return nil, fmt.Errorf("failed to scan FindSingleContainerLaunchMedia: %w", scanErr)
		}
		matches = append(matches, row)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, fmt.Errorf("failed to iterate FindSingleContainerLaunchMedia: %w", rowsErr)
	}
	return selectContainerLaunchMedia(matches), nil
}

func containerHasNestedMedia(ctx context.Context, db sqlQueryable, systemDBID int64, prefix string) (bool, error) {
	upper := stringPrefixUpperBound(prefix)
	query := `
		SELECT 1
		FROM Media
		WHERE SystemDBID = ? AND IsMissing = 0 AND Path >= ? AND Path < ? AND ParentDir != ?
		LIMIT 1
	`
	args := []any{systemDBID, prefix, upper, prefix}
	if upper == "" {
		query = `
			SELECT 1
			FROM Media
			WHERE SystemDBID = ? AND IsMissing = 0 AND Path >= ?
				AND substr(Path, 1, length(?)) = ? AND ParentDir != ?
			LIMIT 1
		`
		args = []any{systemDBID, prefix, prefix, prefix, prefix}
	}

	var exists int
	err := db.QueryRowContext(ctx, query, args...).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to check nested container media: %w", err)
	}
	return true, nil
}

func selectContainerLaunchMedia(rows []database.Media) *database.Media {
	if len(rows) == 0 {
		return nil
	}
	if len(rows) == 1 {
		return &rows[0]
	}

	m3u := singleMediaWithExt(rows, ".m3u")
	if m3u != nil && allOtherExtsMatch(rows, m3u.DBID, isM3UCompanionExt) {
		return m3u
	}

	cue := singleMediaWithExt(rows, ".cue")
	if cue != nil && allOtherExtsMatch(rows, cue.DBID, isCueCompanionExt) {
		return cue
	}

	return nil
}

func singleMediaWithExt(rows []database.Media, ext string) *database.Media {
	var match *database.Media
	for i := range rows {
		if mediaExt(rows[i].Path) != ext {
			continue
		}
		if match != nil {
			return nil
		}
		match = &rows[i]
	}
	return match
}

func allOtherExtsMatch(rows []database.Media, mediaDBID int64, allowed func(string) bool) bool {
	for i := range rows {
		if rows[i].DBID == mediaDBID {
			continue
		}
		if !allowed(mediaExt(rows[i].Path)) {
			return false
		}
	}
	return true
}

func mediaExt(mediaPath string) string {
	name := mediaPath
	if idx := strings.LastIndex(name, "/"); idx >= 0 {
		name = name[idx+1:]
	}
	if idx := strings.LastIndex(name, "."); idx >= 0 {
		return strings.ToLower(name[idx:])
	}
	return ""
}

func isCueCompanionExt(ext string) bool {
	switch ext {
	case ".bin", ".wav", ".mp3", ".ogg", ".flac", ".ape":
		return true
	default:
		return false
	}
}

func isM3UCompanionExt(ext string) bool {
	if isCueCompanionExt(ext) {
		return true
	}
	switch ext {
	case ".cue", ".chd", ".iso":
		return true
	default:
		return false
	}
}

func stringPrefixUpperBound(prefix string) string {
	if prefix == "" {
		return ""
	}
	b := []byte(prefix)
	for i := len(b) - 1; i >= 0; i-- {
		if b[i] != 0xff {
			b[i]++
			return string(b[:i+1])
		}
	}
	return ""
}

// FindMediaBySystemAndPathFold returns the Media row for the given system and
// path using a case-insensitive path comparison, or nil, nil when not found.
// LOWER() in SQLite covers ASCII only, which is sufficient for filesystem paths.
func (db *MediaDB) FindMediaBySystemAndPathFold(
	ctx context.Context, systemDBID int64, path string,
) (*database.Media, error) {
	if db.sql.Load() == nil {
		return nil, ErrNullSQL
	}
	stmt, err := db.sql.Load().PrepareContext(ctx, `
		SELECT DBID, MediaTitleDBID, SystemDBID, Path, ParentDir, IsMissing
		FROM Media
		WHERE SystemDBID = ? AND LOWER(Path) = LOWER(?)
		LIMIT 1
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare FindMediaBySystemAndPathFold: %w", err)
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()

	var row database.Media
	err = stmt.QueryRowContext(ctx, systemDBID, path).Scan(
		&row.DBID,
		&row.MediaTitleDBID,
		&row.SystemDBID,
		&row.Path,
		&row.ParentDir,
		&row.IsMissing,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil //nolint:nilnil // sql.ErrNoRows means not found; nil result is the "not found" sentinel
	}
	if err != nil {
		return nil, fmt.Errorf("failed to scan FindMediaBySystemAndPathFold: %w", err)
	}
	return &row, nil
}

// FindMediaBySystemAndPathSuffix returns all Media rows for the given system
// whose Path ends with "/" + filename. LIKE wildcards in the filename are
// escaped so a literal '%' or '_' in the name does not expand.
func (db *MediaDB) FindMediaBySystemAndPathSuffix(
	ctx context.Context, systemDBID int64, filename string,
) ([]database.Media, error) {
	if db.sql.Load() == nil {
		return nil, ErrNullSQL
	}
	escaped := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(filename)
	pattern := "%/" + escaped
	rows, err := db.sql.Load().QueryContext(ctx, `
		SELECT DBID, MediaTitleDBID, SystemDBID, Path, ParentDir, IsMissing
		FROM Media
		WHERE SystemDBID = ? AND Path LIKE ? ESCAPE '\'
	`, systemDBID, pattern)
	if err != nil {
		return nil, fmt.Errorf("failed to query FindMediaBySystemAndPathSuffix: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close rows")
		}
	}()

	var result []database.Media
	for rows.Next() {
		var row database.Media
		if err := rows.Scan(
			&row.DBID,
			&row.MediaTitleDBID,
			&row.SystemDBID,
			&row.Path,
			&row.ParentDir,
			&row.IsMissing,
		); err != nil {
			return nil, fmt.Errorf("failed to scan FindMediaBySystemAndPathSuffix: %w", err)
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

// MediaHasTag returns true when the given Media record has a tag matching the
// "type:value" string tagValue. The string is split on the first colon: everything
// before the colon is matched against TagTypes.Type, everything after is matched
// against Tags.Tag (padded and unpadded forms are both checked).
func (db *MediaDB) MediaHasTag(ctx context.Context, mediaDBID int64, tagValue string) (bool, error) {
	if db.sql.Load() == nil {
		return false, ErrNullSQL
	}

	idx := strings.Index(tagValue, ":")
	if idx < 0 {
		return false, fmt.Errorf("MediaHasTag: tagValue %q is malformed — expected \"type:value\" format", tagValue)
	}

	tagType := tagValue[:idx]
	tagPart := tagValue[idx+1:]
	padded := tags.PadTagValue(tagPart)

	stmt, err := db.sql.Load().PrepareContext(ctx, `
		SELECT 1
		FROM MediaTags mt
		JOIN Tags t ON mt.TagDBID = t.DBID
		JOIN TagTypes tt ON t.TypeDBID = tt.DBID
		WHERE mt.MediaDBID = ?
		  AND tt.Type = ?
		  AND (t.Tag = ? OR t.Tag = ?)
		LIMIT 1
	`)
	if err != nil {
		return false, fmt.Errorf("failed to prepare MediaHasTag: %w", err)
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()

	var found int
	err = stmt.QueryRowContext(ctx, mediaDBID, tagType, tagPart, padded).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to scan MediaHasTag: %w", err)
	}
	return found == 1, nil
}

// GetScrapedMediaCount returns the number of distinct media rows marked as
// successfully scraped by the given scraper.
func (db *MediaDB) GetScrapedMediaCount(ctx context.Context, scraperID string) (int, error) {
	if db.sql.Load() == nil {
		return 0, ErrNullSQL
	}

	tagDBIDs, err := findScraperSentinelTagDBIDs(
		ctx, db.sql.Load(), string(tags.ScraperType(scraperID)), string(tags.TagScraperScraped),
	)
	if err != nil {
		return 0, fmt.Errorf("failed to find scraper sentinel tag for scraper %q: %w", scraperID, err)
	}
	count, err := countMediaTagsForTagDBIDs(ctx, db.sql.Load(), tagDBIDs)
	if err != nil {
		return 0, fmt.Errorf("failed to count scraped media for scraper %q: %w", scraperID, err)
	}
	return count, nil
}

// GetTotalScrapedMediaCount returns the number of distinct media rows marked
// as successfully scraped by any scraper sentinel tag.
func (db *MediaDB) GetTotalScrapedMediaCount(ctx context.Context) (int, error) {
	if db.sql.Load() == nil {
		return 0, ErrNullSQL
	}

	tagDBIDs, err := findAllScraperSentinelTagDBIDs(ctx, db.sql.Load())
	if err != nil {
		return 0, fmt.Errorf("failed to find scraper sentinel tags: %w", err)
	}
	count, err := countMediaTagsForTagDBIDs(ctx, db.sql.Load(), tagDBIDs)
	if err != nil {
		return 0, fmt.Errorf("failed to count scraped media: %w", err)
	}
	return count, nil
}

// GetScrapedMediaIDs returns media DBIDs in systemDBID already marked as scraped
// by scraperID.
func (db *MediaDB) GetScrapedMediaIDs(
	ctx context.Context, scraperID string, systemDBID int64,
) (map[int64]struct{}, error) {
	if db.sql.Load() == nil {
		return nil, ErrNullSQL
	}

	tagDBIDs, err := findScraperSentinelTagDBIDs(
		ctx, db.sql.Load(), string(tags.ScraperType(scraperID)), string(tags.TagScraperScraped),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to find scraper sentinel tag for scraper %q: %w", scraperID, err)
	}
	mediaIDs, err := getMediaIDsForTagDBIDs(ctx, db.sql.Load(), systemDBID, tagDBIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to query scraped media IDs for scraper %q: %w", scraperID, err)
	}
	return mediaIDs, nil
}

// GetScrapeRunMediaIDs returns media DBIDs in systemDBID completed during a
// specific persisted scraper run.
func (db *MediaDB) GetScrapeRunMediaIDs(
	ctx context.Context, scraperID, runID string, systemDBID int64,
) (map[int64]struct{}, error) {
	if db.sql.Load() == nil {
		return nil, ErrNullSQL
	}
	if runID == "" {
		return map[int64]struct{}{}, nil
	}

	tagDBIDs, err := findScraperSentinelTagDBIDs(ctx, db.sql.Load(), string(tags.ScraperRunType(scraperID)), runID)
	if err != nil {
		return nil, fmt.Errorf("failed to find scraper run tag for scraper %q run %q: %w", scraperID, runID, err)
	}
	mediaIDs, err := getMediaIDsForTagDBIDs(ctx, db.sql.Load(), systemDBID, tagDBIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to query scrape run media IDs for scraper %q run %q: %w", scraperID, runID, err)
	}
	return mediaIDs, nil
}

// ClearScrapeRunMarkers removes per-run completion markers after a scraper
// operation reaches a terminal state.
func (db *MediaDB) ClearScrapeRunMarkers(ctx context.Context, scraperID, runID string) error {
	if db.sql.Load() == nil {
		return ErrNullSQL
	}
	if runID == "" {
		return nil
	}

	tagDBIDs, err := findScraperSentinelTagDBIDs(ctx, db.sql.Load(), string(tags.ScraperRunType(scraperID)), runID)
	if err != nil {
		return fmt.Errorf("failed to find scraper run tag for scraper %q run %q: %w", scraperID, runID, err)
	}
	return clearMediaTagsForTagDBIDs(ctx, db.sql.Load(), tagDBIDs)
}

func getMediaIDsForTagDBIDs(
	ctx context.Context, db *sql.DB, systemDBID int64, tagDBIDs []int64,
) (map[int64]struct{}, error) {
	if len(tagDBIDs) == 0 {
		return map[int64]struct{}{}, nil
	}

	placeholders := prepareVariadic("?", ",", len(tagDBIDs))
	args := make([]any, 0, len(tagDBIDs)+1)
	args = append(args, systemDBID)
	for _, tagDBID := range tagDBIDs {
		args = append(args, tagDBID)
	}

	selectClause := "SELECT m.DBID"
	if len(tagDBIDs) > 1 {
		selectClause = "SELECT DISTINCT m.DBID"
	}
	//nolint:gosec // Safe: prepareVariadic only generates SQL placeholders.
	query := selectClause + `
		FROM Media m INDEXED BY media_system_path_idx
		CROSS JOIN MediaTags mt
		WHERE m.SystemDBID = ?
		  AND mt.MediaDBID = m.DBID
		  AND mt.TagDBID IN (` + placeholders + `)`
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query media IDs for tag DBIDs: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close rows")
		}
	}()

	mediaIDs := make(map[int64]struct{})
	for rows.Next() {
		var mediaDBID int64
		if err := rows.Scan(&mediaDBID); err != nil {
			return nil, fmt.Errorf("failed to scan tagged media ID: %w", err)
		}
		mediaIDs[mediaDBID] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate tagged media IDs: %w", err)
	}
	return mediaIDs, nil
}

func clearMediaTagsForTagDBIDs(ctx context.Context, db *sql.DB, tagDBIDs []int64) error {
	if len(tagDBIDs) == 0 {
		return nil
	}

	placeholders := prepareVariadic("?", ",", len(tagDBIDs))
	args := make([]any, 0, len(tagDBIDs))
	for _, tagDBID := range tagDBIDs {
		args = append(args, tagDBID)
	}

	//nolint:gosec // Safe: prepareVariadic only generates SQL placeholders.
	if _, err := db.ExecContext(ctx, `DELETE FROM MediaTags WHERE TagDBID IN (`+placeholders+`)`, args...); err != nil {
		return fmt.Errorf("failed to delete media tag links: %w", err)
	}
	//nolint:gosec // Safe: prepareVariadic only generates SQL placeholders.
	if _, err := db.ExecContext(ctx, `
		DELETE FROM Tags
		WHERE DBID IN (`+placeholders+`)
		  AND NOT EXISTS (SELECT 1 FROM MediaTags WHERE MediaTags.TagDBID = Tags.DBID)
	`, args...); err != nil {
		return fmt.Errorf("failed to delete unreferenced tags: %w", err)
	}
	return nil
}

func findScraperSentinelTagDBIDs(ctx context.Context, db *sql.DB, scraperType, tagValue string) ([]int64, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT t.DBID
		FROM Tags t
		JOIN TagTypes tt ON t.TypeDBID = tt.DBID
		WHERE tt.Type = ? AND t.Tag = ?
		ORDER BY t.DBID
	`, scraperType, tagValue)
	if err != nil {
		return nil, fmt.Errorf("failed to query scraper sentinel tag DBIDs: %w", err)
	}
	return scanInt64Rows(rows, "scraper sentinel tag DBID")
}

func findAllScraperSentinelTagDBIDs(ctx context.Context, db *sql.DB) ([]int64, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT t.DBID
		FROM Tags t
		JOIN TagTypes tt ON t.TypeDBID = tt.DBID
		WHERE tt.Type LIKE 'scraper.%' AND t.Tag = ?
		ORDER BY t.DBID
	`, string(tags.TagScraperScraped))
	if err != nil {
		return nil, fmt.Errorf("failed to query scraper sentinel tag DBIDs: %w", err)
	}
	return scanInt64Rows(rows, "scraper sentinel tag DBID")
}

func countMediaTagsForTagDBIDs(ctx context.Context, db *sql.DB, tagDBIDs []int64) (int, error) {
	if len(tagDBIDs) == 0 {
		return 0, nil
	}
	if len(tagDBIDs) == 1 {
		var count int
		if err := db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM MediaTags WHERE TagDBID = ?`, tagDBIDs[0],
		).Scan(&count); err != nil {
			return 0, fmt.Errorf("failed to count media tag rows: %w", err)
		}
		return count, nil
	}

	placeholders := prepareVariadic("?", ",", len(tagDBIDs))
	args := make([]any, 0, len(tagDBIDs))
	for _, tagDBID := range tagDBIDs {
		args = append(args, tagDBID)
	}
	var count int
	//nolint:gosec // Safe: prepareVariadic only generates SQL placeholders.
	query := `SELECT COUNT(DISTINCT MediaDBID) FROM MediaTags WHERE TagDBID IN (` + placeholders + `)`
	if err := db.QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
		return 0, fmt.Errorf("failed to count media tag rows: %w", err)
	}
	return count, nil
}

func findMediaTitlesBySystemDBID(ctx context.Context, db *sql.DB, systemDBID int64) ([]database.MediaTitle, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT DBID, SystemDBID, Slug, Name
		FROM MediaTitles
		WHERE SystemDBID = ?
	`, systemDBID)
	if err != nil {
		return nil, fmt.Errorf("failed to query media titles by system DBID: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close rows")
		}
	}()

	var titles []database.MediaTitle
	for rows.Next() {
		var title database.MediaTitle
		if err := rows.Scan(&title.DBID, &title.SystemDBID, &title.Slug, &title.Name); err != nil {
			return nil, fmt.Errorf("failed to scan media title: %w", err)
		}
		titles = append(titles, title)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate media titles: %w", err)
	}
	return titles, nil
}

func scanInt64Rows(rows *sql.Rows, label string) ([]int64, error) {
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Str("label", label).Msg("failed to close rows")
		}
	}()
	var values []int64
	for rows.Next() {
		var value int64
		if err := rows.Scan(&value); err != nil {
			return nil, fmt.Errorf("failed to scan %s: %w", label, err)
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate %s rows: %w", label, err)
	}
	return values, nil
}

// UpsertMediaTags writes tags to MediaTags for a specific Media row, respecting
// TagTypes.IsExclusive: exclusive types delete existing tags of that type first;
// additive types use INSERT OR IGNORE.
func (db *MediaDB) UpsertMediaTags(ctx context.Context, mediaDBID int64, tagInfos []database.TagInfo) error {
	if db.sql.Load() == nil {
		return ErrNullSQL
	}
	return upsertTags(ctx, db.sql.Load(), tagInfos, func(tx *sql.Tx, typeDBID int64) error {
		_, err := tx.ExecContext(ctx,
			`DELETE FROM MediaTags WHERE MediaDBID = ? AND TagDBID IN (SELECT DBID FROM Tags WHERE TypeDBID = ?)`,
			mediaDBID, typeDBID,
		)
		if err != nil {
			return fmt.Errorf("failed to delete media tags for type: %w", err)
		}
		return nil
	}, func(tx *sql.Tx, tagDBID int64) error {
		_, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO MediaTags (MediaDBID, TagDBID) VALUES (?, ?)`,
			mediaDBID, tagDBID,
		)
		if err != nil {
			return fmt.Errorf("failed to insert media tag link: %w", err)
		}
		return nil
	})
}

// UpsertMediaTitleTags writes tags to MediaTitleTags for a specific MediaTitle row.
func (db *MediaDB) UpsertMediaTitleTags(ctx context.Context, mediaTitleDBID int64, tagInfos []database.TagInfo) error {
	if db.sql.Load() == nil {
		return ErrNullSQL
	}
	return upsertTags(ctx, db.sql.Load(), tagInfos, func(tx *sql.Tx, typeDBID int64) error {
		const q = `DELETE FROM MediaTitleTags` +
			` WHERE MediaTitleDBID = ? AND TagDBID IN (SELECT DBID FROM Tags WHERE TypeDBID = ?)`
		_, err := tx.ExecContext(ctx, q, mediaTitleDBID, typeDBID)
		if err != nil {
			return fmt.Errorf("failed to delete media title tags for type: %w", err)
		}
		return nil
	}, func(tx *sql.Tx, tagDBID int64) error {
		_, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO MediaTitleTags (MediaTitleDBID, TagDBID) VALUES (?, ?)`,
			mediaTitleDBID, tagDBID,
		)
		if err != nil {
			return fmt.Errorf("failed to insert media title tag link: %w", err)
		}
		return nil
	})
}

const (
	sqliteMaxParams            = 999
	bulkTagInsertRowsPerStmt   = 400
	bulkPropUpsertRowsPerStmt  = 200
	bulkDeleteEntityIDsPerStmt = sqliteMaxParams - 1
)

type scrapeBatchSQLStats struct {
	Duration                      time.Duration
	Targets                       int
	TitleTagDeletes               int
	TitleTagInsertRows            int
	TitleTagInsertStatements      int
	TitlePropertyUpsertRows       int
	TitlePropertyUpsertStatements int
	SentinelDeleteStatements      int
	SentinelInsertRows            int
	SentinelInsertStatements      int
	MediaTagFallbackTargets       int
	MediaPropFallbackTargets      int
}

type tagTypeEntry struct {
	dbid        int64
	isExclusive bool
}

type tagTypeGroup struct {
	tags        []database.TagInfo
	dbid        int64
	isExclusive bool
}

type tagCacheKey struct {
	tag      string
	typeDBID int64
}

type scrapeWriteTxContext struct {
	tx               *sql.Tx
	tagTypes         map[string]tagTypeEntry
	tags             map[tagCacheKey]int64
	propertyTypeTags map[string]int64
}

func newScrapeWriteTxContext(tx *sql.Tx) *scrapeWriteTxContext {
	return &scrapeWriteTxContext{
		tx:               tx,
		tagTypes:         make(map[string]tagTypeEntry),
		tags:             make(map[tagCacheKey]int64),
		propertyTypeTags: make(map[string]int64),
	}
}

func (c *scrapeWriteTxContext) resolveTagType(
	ctx context.Context, tagType string,
) (typeDBID int64, isExclusive bool, err error) {
	if cached, ok := c.tagTypes[tagType]; ok {
		return cached.dbid, cached.isExclusive, nil
	}

	err = c.tx.QueryRowContext(ctx,
		`SELECT DBID, IsExclusive FROM TagTypes WHERE Type = ? LIMIT 1`,
		tagType,
	).Scan(&typeDBID, &isExclusive)
	if errors.Is(err, sql.ErrNoRows) {
		_, insertErr := c.tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO TagTypes (Type, IsExclusive) VALUES (?, 0)`,
			tagType,
		)
		if insertErr != nil {
			return 0, false, fmt.Errorf("failed to auto-create tag type %q: %w", tagType, insertErr)
		}
		err = c.tx.QueryRowContext(ctx,
			`SELECT DBID, IsExclusive FROM TagTypes WHERE Type = ? LIMIT 1`,
			tagType,
		).Scan(&typeDBID, &isExclusive)
	}
	if err != nil {
		return 0, false, fmt.Errorf("failed to look up tag type %q: %w", tagType, err)
	}

	c.tagTypes[tagType] = tagTypeEntry{dbid: typeDBID, isExclusive: isExclusive}
	return typeDBID, isExclusive, nil
}

func (c *scrapeWriteTxContext) resolveTag(
	ctx context.Context, typeDBID int64, typeName, tagValue, displayName string,
) (int64, error) {
	key := tagCacheKey{typeDBID: typeDBID, tag: tagValue}
	if cached, ok := c.tags[key]; ok {
		if err := c.setTagDisplayName(ctx, cached, displayName); err != nil {
			return 0, err
		}
		return cached, nil
	}

	var tagDBID int64
	err := c.tx.QueryRowContext(ctx,
		`SELECT DBID FROM Tags WHERE TypeDBID = ? AND Tag = ? LIMIT 1`,
		typeDBID, tagValue,
	).Scan(&tagDBID)
	if errors.Is(err, sql.ErrNoRows) {
		if _, insertErr := c.tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO Tags (TypeDBID, Tag, DisplayName) VALUES (?, ?, ?)`,
			typeDBID, tagValue, displayName,
		); insertErr != nil {
			return 0, fmt.Errorf("failed to insert tag %q:%q: %w", typeName, tagValue, insertErr)
		}
		if err = c.tx.QueryRowContext(ctx,
			`SELECT DBID FROM Tags WHERE TypeDBID = ? AND Tag = ? LIMIT 1`,
			typeDBID, tagValue,
		).Scan(&tagDBID); err != nil {
			return 0, fmt.Errorf("failed to re-query tag DBID for %q:%q: %w", typeName, tagValue, err)
		}
	} else if err != nil {
		return 0, fmt.Errorf("failed to look up tag DBID for %q:%q: %w", typeName, tagValue, err)
	}

	if err := c.setTagDisplayName(ctx, tagDBID, displayName); err != nil {
		return 0, err
	}
	c.tags[key] = tagDBID
	return tagDBID, nil
}

func (c *scrapeWriteTxContext) setTagDisplayName(ctx context.Context, tagDBID int64, displayName string) error {
	if displayName == "" {
		return nil
	}
	if _, err := c.tx.ExecContext(ctx,
		`UPDATE Tags SET DisplayName = ? WHERE DBID = ? AND DisplayName = ''`, displayName, tagDBID,
	); err != nil {
		return fmt.Errorf("failed to update tag display name: %w", err)
	}
	return nil
}

func (c *scrapeWriteTxContext) resolvePropertyTypeTag(ctx context.Context, typeTag string) (int64, error) {
	if cached, ok := c.propertyTypeTags[typeTag]; ok {
		return cached, nil
	}
	tagDBID, err := resolvePropertyTypeTag(ctx, c.tx, typeTag)
	if err != nil {
		return 0, err
	}
	c.propertyTypeTags[typeTag] = tagDBID
	return tagDBID, nil
}

func preloadScrapeWriteLookupCache(
	ctx context.Context, writeCtx *scrapeWriteTxContext, targets []database.ScrapeWriteTarget,
) error {
	tagTypes := make(map[string]struct{})
	propertyTypeTags := make(map[string]struct{})
	for _, target := range targets {
		write := target.Write
		for _, tag := range write.MediaTags {
			tagTypes[tag.Type] = struct{}{}
		}
		for _, tag := range write.TitleTags {
			tagTypes[tag.Type] = struct{}{}
		}
		if write.Sentinel.Type != "" {
			tagTypes[write.Sentinel.Type] = struct{}{}
		}
		for _, prop := range write.MediaProps {
			propertyTypeTags[prop.TypeTag] = struct{}{}
		}
		for _, prop := range write.TitleProps {
			propertyTypeTags[prop.TypeTag] = struct{}{}
		}
	}
	for typeTag := range propertyTypeTags {
		idx := strings.Index(typeTag, ":")
		if idx < 0 {
			return fmt.Errorf("property type tag %q is not in type:value format", typeTag)
		}
		tagTypes[typeTag[:idx]] = struct{}{}
	}
	if err := preloadTagTypes(ctx, writeCtx, tagTypes); err != nil {
		return err
	}
	if err := preloadWriteTags(ctx, writeCtx, targets); err != nil {
		return err
	}
	return preloadPropertyTypeTags(ctx, writeCtx, propertyTypeTags)
}

func preloadTagTypes(ctx context.Context, writeCtx *scrapeWriteTxContext, tagTypes map[string]struct{}) error {
	missing := make([]string, 0, len(tagTypes))
	for tagType := range tagTypes {
		if _, ok := writeCtx.tagTypes[tagType]; !ok {
			missing = append(missing, tagType)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	if err := queryTagTypesIntoCache(ctx, writeCtx, missing); err != nil {
		return err
	}
	inserted := false
	for _, tagType := range missing {
		if _, ok := writeCtx.tagTypes[tagType]; ok {
			continue
		}
		if _, err := writeCtx.tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO TagTypes (Type, IsExclusive) VALUES (?, 0)`, tagType,
		); err != nil {
			return fmt.Errorf("failed to auto-create tag type %q: %w", tagType, err)
		}
		inserted = true
	}
	if !inserted {
		return nil
	}
	return queryTagTypesIntoCache(ctx, writeCtx, missing)
}

func queryTagTypesIntoCache(ctx context.Context, writeCtx *scrapeWriteTxContext, tagTypes []string) error {
	const chunkSize = sqliteMaxParams
	for start := 0; start < len(tagTypes); start += chunkSize {
		end := start + chunkSize
		if end > len(tagTypes) {
			end = len(tagTypes)
		}
		chunk := tagTypes[start:end]
		args := make([]any, 0, len(chunk))
		for _, tagType := range chunk {
			args = append(args, tagType)
		}
		//nolint:gosec // Safe: prepareVariadic only generates SQL placeholders.
		query := `SELECT DBID, Type, IsExclusive FROM TagTypes WHERE Type IN (` +
			prepareVariadic("?", ",", len(chunk)) + `)`
		if err := queryTagTypeChunk(ctx, writeCtx, query, args); err != nil {
			return err
		}
	}
	return nil
}

func queryTagTypeChunk(ctx context.Context, writeCtx *scrapeWriteTxContext, query string, args []any) error {
	rows, err := writeCtx.tx.QueryContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("failed to query tag types: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close tag type rows")
		}
	}()
	for rows.Next() {
		var tagType string
		var entry tagTypeEntry
		if err := rows.Scan(&entry.dbid, &tagType, &entry.isExclusive); err != nil {
			return fmt.Errorf("failed to scan tag type: %w", err)
		}
		writeCtx.tagTypes[tagType] = entry
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("failed to iterate tag types: %w", err)
	}
	return nil
}

func preloadWriteTags(ctx context.Context, writeCtx *scrapeWriteTxContext, targets []database.ScrapeWriteTarget) error {
	valuesByType := make(map[int64]map[string]struct{})
	addTag := func(tag database.TagInfo) error {
		if tag.Type == "" {
			return nil
		}
		typeEntry, ok := writeCtx.tagTypes[tag.Type]
		if !ok {
			return fmt.Errorf("tag type %q missing from preload cache", tag.Type)
		}
		if _, ok := valuesByType[typeEntry.dbid]; !ok {
			valuesByType[typeEntry.dbid] = make(map[string]struct{})
		}
		valuesByType[typeEntry.dbid][tags.PadTagValue(tag.Tag)] = struct{}{}
		return nil
	}
	for _, target := range targets {
		for _, tag := range target.Write.MediaTags {
			if err := addTag(tag); err != nil {
				return err
			}
		}
		for _, tag := range target.Write.TitleTags {
			if err := addTag(tag); err != nil {
				return err
			}
		}
		if err := addTag(target.Write.Sentinel); err != nil {
			return err
		}
	}
	return preloadTagsByType(ctx, writeCtx, valuesByType)
}

func preloadTagsByType(
	ctx context.Context, writeCtx *scrapeWriteTxContext, valuesByType map[int64]map[string]struct{},
) error {
	for typeDBID, values := range valuesByType {
		missing := make([]string, 0, len(values))
		for value := range values {
			key := tagCacheKey{typeDBID: typeDBID, tag: value}
			if _, ok := writeCtx.tags[key]; !ok {
				missing = append(missing, value)
			}
		}
		if len(missing) == 0 {
			continue
		}
		if err := queryTagsIntoCache(ctx, writeCtx, typeDBID, missing); err != nil {
			return err
		}
		inserted := false
		for _, value := range missing {
			key := tagCacheKey{typeDBID: typeDBID, tag: value}
			if _, ok := writeCtx.tags[key]; ok {
				continue
			}
			if _, err := writeCtx.tx.ExecContext(ctx,
				`INSERT OR IGNORE INTO Tags (TypeDBID, Tag, DisplayName) VALUES (?, ?, '')`, typeDBID, value,
			); err != nil {
				return fmt.Errorf("failed to insert tag %q: %w", value, err)
			}
			inserted = true
		}
		if inserted {
			if err := queryTagsIntoCache(ctx, writeCtx, typeDBID, missing); err != nil {
				return err
			}
		}
	}
	return nil
}

func queryTagsIntoCache(ctx context.Context, writeCtx *scrapeWriteTxContext, typeDBID int64, values []string) error {
	const valuesPerQuery = sqliteMaxParams - 1
	for start := 0; start < len(values); start += valuesPerQuery {
		end := start + valuesPerQuery
		if end > len(values) {
			end = len(values)
		}
		chunk := values[start:end]
		args := make([]any, 0, len(chunk)+1)
		args = append(args, typeDBID)
		for _, value := range chunk {
			args = append(args, value)
		}
		//nolint:gosec // Safe: prepareVariadic only generates SQL placeholders.
		query := `SELECT DBID, Tag FROM Tags WHERE TypeDBID = ? AND Tag IN (` +
			prepareVariadic("?", ",", len(chunk)) + `)`
		if err := queryTagChunk(ctx, writeCtx, typeDBID, query, args); err != nil {
			return err
		}
	}
	return nil
}

func queryTagChunk(
	ctx context.Context, writeCtx *scrapeWriteTxContext, typeDBID int64, query string, args []any,
) error {
	rows, err := writeCtx.tx.QueryContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("failed to query tags: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close tag rows")
		}
	}()
	for rows.Next() {
		var tagDBID int64
		var value string
		if err := rows.Scan(&tagDBID, &value); err != nil {
			return fmt.Errorf("failed to scan tag: %w", err)
		}
		writeCtx.tags[tagCacheKey{typeDBID: typeDBID, tag: value}] = tagDBID
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("failed to iterate tags: %w", err)
	}
	return nil
}

func preloadPropertyTypeTags(
	ctx context.Context, writeCtx *scrapeWriteTxContext, propertyTypeTags map[string]struct{},
) error {
	missing := make([]string, 0, len(propertyTypeTags))
	valuesByType := make(map[string]map[string]struct{})
	for typeTag := range propertyTypeTags {
		if _, ok := writeCtx.propertyTypeTags[typeTag]; ok {
			continue
		}
		idx := strings.Index(typeTag, ":")
		if idx < 0 {
			return fmt.Errorf("property type tag %q is not in type:value format", typeTag)
		}
		missing = append(missing, typeTag)
		typeName := typeTag[:idx]
		rawValue := typeTag[idx+1:]
		if _, ok := valuesByType[typeName]; !ok {
			valuesByType[typeName] = make(map[string]struct{})
		}
		valuesByType[typeName][rawValue] = struct{}{}
		valuesByType[typeName][tags.PadTagValue(rawValue)] = struct{}{}
	}
	if len(missing) == 0 {
		return nil
	}
	for typeName, values := range valuesByType {
		vals := make([]string, 0, len(values))
		for value := range values {
			vals = append(vals, value)
		}
		if err := queryPropertyTypeTagsIntoCache(ctx, writeCtx, typeName, vals, missing); err != nil {
			return err
		}
	}
	for _, typeTag := range missing {
		if _, ok := writeCtx.propertyTypeTags[typeTag]; ok {
			continue
		}
		if _, err := writeCtx.resolvePropertyTypeTag(ctx, typeTag); err != nil {
			return err
		}
	}
	return nil
}

func queryPropertyTypeTagsIntoCache(
	ctx context.Context, writeCtx *scrapeWriteTxContext, typeName string, values, requested []string,
) error {
	const valuesPerQuery = sqliteMaxParams - 1
	for start := 0; start < len(values); start += valuesPerQuery {
		end := start + valuesPerQuery
		if end > len(values) {
			end = len(values)
		}
		chunk := values[start:end]
		args := make([]any, 0, len(chunk)+1)
		args = append(args, typeName)
		for _, value := range chunk {
			args = append(args, value)
		}
		//nolint:gosec // Safe: prepareVariadic only generates SQL placeholders.
		query := `
			SELECT t.DBID, t.Tag
			FROM Tags t
			JOIN TagTypes tt ON t.TypeDBID = tt.DBID
			WHERE tt.Type = ? AND t.Tag IN (` + prepareVariadic("?", ",", len(chunk)) + `)
		`
		if err := queryPropertyTypeTagChunk(ctx, writeCtx, typeName, requested, query, args); err != nil {
			return err
		}
	}
	return nil
}

func queryPropertyTypeTagChunk(
	ctx context.Context,
	writeCtx *scrapeWriteTxContext,
	typeName string,
	requested []string,
	query string,
	args []any,
) error {
	rows, err := writeCtx.tx.QueryContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("failed to query property type tags: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close property type tag rows")
		}
	}()
	for rows.Next() {
		var tagDBID int64
		var value string
		if err := rows.Scan(&tagDBID, &value); err != nil {
			return fmt.Errorf("failed to scan property type tag: %w", err)
		}
		for _, typeTag := range requested {
			idx := strings.Index(typeTag, ":")
			if idx < 0 || typeTag[:idx] != typeName {
				continue
			}
			rawValue := typeTag[idx+1:]
			if value == rawValue || value == tags.PadTagValue(rawValue) {
				writeCtx.propertyTypeTags[typeTag] = tagDBID
			}
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("failed to iterate property type tags: %w", err)
	}
	return nil
}

// upsertTags is the shared implementation for UpsertMediaTags and UpsertMediaTitleTags.
// deleteFn deletes existing tags of a type for the entity (called once per exclusive type).
// insertFn inserts the tag link for the entity.
// All operations run inside a single transaction for atomicity.
//
// Tags are grouped by type before processing. For each exclusive type the existing
// tags are deleted once (before any inserts for that type), preventing multiple tags
// of the same exclusive type from clobbering each other during the loop.
func upsertTags(
	ctx context.Context,
	db *sql.DB,
	tagInfos []database.TagInfo,
	deleteFn func(tx *sql.Tx, typeDBID int64) error,
	insertFn func(tx *sql.Tx, tagDBID int64) error,
) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("upsertTags: begin transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if err := upsertTagsInTx(ctx, tx, tagInfos, deleteFn, insertFn); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("upsertTags: commit: %w", err)
	}
	committed = true
	return nil
}

func upsertTagsInTx(
	ctx context.Context,
	tx *sql.Tx,
	tagInfos []database.TagInfo,
	deleteFn func(tx *sql.Tx, typeDBID int64) error,
	insertFn func(tx *sql.Tx, tagDBID int64) error,
) error {
	return upsertTagsWithContext(ctx, newScrapeWriteTxContext(tx), tagInfos, deleteFn, insertFn)
}

func upsertTagsWithContext(
	ctx context.Context,
	writeCtx *scrapeWriteTxContext,
	tagInfos []database.TagInfo,
	deleteFn func(tx *sql.Tx, typeDBID int64) error,
	insertFn func(tx *sql.Tx, tagDBID int64) error,
) error {
	tx := writeCtx.tx
	typeOrder := make([]string, 0, len(tagInfos)) // preserve insertion order
	byType := make(map[string]*tagTypeGroup, len(tagInfos))

	for _, ti := range tagInfos {
		e, exists := byType[ti.Type]
		if !exists {
			typeDBID, isExclusive, err := writeCtx.resolveTagType(ctx, ti.Type)
			if err != nil {
				return err
			}
			e = &tagTypeGroup{dbid: typeDBID, isExclusive: isExclusive}
			byType[ti.Type] = e
			typeOrder = append(typeOrder, ti.Type)
		}
		e.tags = append(e.tags, ti)
	}

	// Process each type: delete once for exclusive types, then insert all tags.
	for _, typeName := range typeOrder {
		e := byType[typeName]

		if e.isExclusive {
			seen := make(map[string]struct{}, len(e.tags))
			for _, ti := range e.tags {
				seen[tags.PadTagValue(ti.Tag)] = struct{}{}
			}
			if len(seen) > 1 {
				return fmt.Errorf("exclusive tag type %q received multiple values", typeName)
			}
		}

		// For exclusive types: delete all existing tags of this type for the entity once,
		// before inserting any new tags. This prevents subsequent tags of the same type
		// from clobbering each other within this call.
		if e.isExclusive {
			if err := deleteFn(tx, e.dbid); err != nil {
				return fmt.Errorf("failed to delete exclusive tags for type %q: %w", typeName, err)
			}
		}

		for _, ti := range e.tags {
			tagValue := tags.PadTagValue(ti.Tag)
			tagDBID, err := writeCtx.resolveTag(ctx, e.dbid, typeName, tagValue, ti.Label)
			if err != nil {
				return err
			}

			if err := insertFn(tx, tagDBID); err != nil {
				return fmt.Errorf("failed to insert tag link for %q:%q: %w", typeName, ti.Tag, err)
			}
		}
	}

	return nil
}

// UpsertMediaTitleProperties upserts properties into MediaTitleProperties.
// Conflicts on (MediaTitleDBID, TypeTagDBID) update data columns; DBID is preserved.
// p.TypeTag must be set to the full "type:value" string; TypeTagDBID is resolved
// from the Tags table automatically.
func (db *MediaDB) UpsertMediaTitleProperties(
	ctx context.Context, mediaTitleDBID int64, props []database.MediaProperty,
) error {
	if db.sql.Load() == nil {
		return ErrNullSQL
	}
	tx, err := db.sql.Load().BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("UpsertMediaTitleProperties: begin transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if err := upsertMediaTitleProperties(ctx, tx, mediaTitleDBID, props); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("UpsertMediaTitleProperties: commit: %w", err)
	}
	committed = true
	return nil
}

func upsertMediaTitleProperties(
	ctx context.Context, q sqlQueryable, mediaTitleDBID int64, props []database.MediaProperty,
) error {
	for _, p := range props {
		typeTagDBID, err := resolvePropertyTypeTag(ctx, q, p.TypeTag)
		if err != nil {
			return fmt.Errorf("failed to resolve property type tag %q: %w", p.TypeTag, err)
		}
		if err := upsertMediaTitleProperty(ctx, q, mediaTitleDBID, typeTagDBID, &p); err != nil {
			return err
		}
	}
	return nil
}

func upsertMediaTitlePropertiesWithContext(
	ctx context.Context, writeCtx *scrapeWriteTxContext, mediaTitleDBID int64, props []database.MediaProperty,
) error {
	for _, p := range props {
		typeTagDBID, err := writeCtx.resolvePropertyTypeTag(ctx, p.TypeTag)
		if err != nil {
			return fmt.Errorf("failed to resolve property type tag %q: %w", p.TypeTag, err)
		}
		if err := upsertMediaTitleProperty(ctx, writeCtx.tx, mediaTitleDBID, typeTagDBID, &p); err != nil {
			return err
		}
	}
	return nil
}

func upsertMediaTitleProperty(
	ctx context.Context, q sqlQueryable, mediaTitleDBID, typeTagDBID int64, p *database.MediaProperty,
) error {
	_, err := q.ExecContext(ctx, `
		INSERT INTO MediaTitleProperties (MediaTitleDBID, TypeTagDBID, Text, BlobDBID)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(MediaTitleDBID, TypeTagDBID) DO UPDATE SET
			Text    = excluded.Text,
			BlobDBID = excluded.BlobDBID
		WHERE MediaTitleProperties.Text IS NOT excluded.Text
		   OR MediaTitleProperties.BlobDBID IS NOT excluded.BlobDBID
	`, mediaTitleDBID, typeTagDBID, p.Text, p.BlobDBID)
	if err != nil {
		return fmt.Errorf("failed to upsert MediaTitleProperty (typeTag=%q): %w", p.TypeTag, err)
	}
	return nil
}

// UpsertMediaProperties upserts properties into MediaProperties.
// Conflicts on (MediaDBID, TypeTagDBID) update data columns; DBID is preserved.
// p.TypeTag must be set to the full "type:value" string; TypeTagDBID is resolved
// from the Tags table automatically.
//
// When called inside an open batch transaction, the write uses db.conn() instead
// of opening a second transaction. SQLite WAL allows one writer, so a nested
// BeginTx here would block behind the batch transaction until busy_timeout.
func (db *MediaDB) UpsertMediaProperties(ctx context.Context, mediaDBID int64, props []database.MediaProperty) error {
	if db.sql.Load() == nil {
		return ErrNullSQL
	}
	if db.inTransaction {
		return upsertMediaProperties(ctx, db.conn(), mediaDBID, props)
	}
	tx, err := db.sql.Load().BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("UpsertMediaProperties: begin transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if err := upsertMediaProperties(ctx, tx, mediaDBID, props); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("UpsertMediaProperties: commit: %w", err)
	}
	committed = true
	return nil
}

// SearchMediaByProperty finds media whose stored property value matches value,
// optionally scoped to systemID. Empty systemID matches any system.
func (db *MediaDB) SearchMediaByProperty(
	ctx context.Context, systemID, property, value string,
) ([]database.SearchResult, error) {
	if db.sql.Load() == nil {
		return nil, ErrNullSQL
	}
	typeTagDBID, err := resolvePropertyTypeTag(ctx, db.sql.Load(), tags.PropertyTypeTag(tags.TagValue(property)))
	if err != nil {
		return nil, fmt.Errorf("failed to resolve property %q: %w", property, err)
	}

	query := `
SELECT s.SystemID, mt.Name, m.Path, m.DBID
FROM Media m
JOIN Systems s ON s.DBID = m.SystemDBID
JOIN MediaTitles mt ON mt.DBID = m.MediaTitleDBID
JOIN MediaProperties mp ON mp.MediaDBID = m.DBID
WHERE mp.TypeTagDBID = ? AND mp.Text = ? AND m.IsMissing = 0`
	args := []any{typeTagDBID, value}
	if systemID != "" {
		query += " AND s.SystemID = ?"
		args = append(args, systemID)
	}
	query += " ORDER BY m.DBID"

	rows, err := db.sql.Load().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query media by property: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []database.SearchResult
	for rows.Next() {
		var result database.SearchResult
		if scanErr := rows.Scan(&result.SystemID, &result.Name, &result.Path, &result.MediaID); scanErr != nil {
			return nil, fmt.Errorf("scan media by property: %w", scanErr)
		}
		results = append(results, result)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, fmt.Errorf("iterate media by property: %w", rowsErr)
	}
	return results, nil
}

// HasMediaPropertyForPath reports whether systemID/path already has property.
func (db *MediaDB) HasMediaPropertyForPath(ctx context.Context, systemID, path, property string) (bool, error) {
	if db.sql.Load() == nil {
		return false, ErrNullSQL
	}
	typeTagDBID, err := resolvePropertyTypeTag(ctx, db.conn(), tags.PropertyTypeTag(tags.TagValue(property)))
	if err != nil {
		return false, fmt.Errorf("failed to resolve property %q: %w", property, err)
	}
	var exists int
	err = db.conn().QueryRowContext(ctx, `
SELECT 1
FROM Media m
JOIN Systems s ON s.DBID = m.SystemDBID
JOIN MediaProperties mp ON mp.MediaDBID = m.DBID
WHERE s.SystemID = ? AND m.Path = ? AND mp.TypeTagDBID = ?
LIMIT 1`, systemID, path, typeTagDBID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to check media property %q: %w", property, err)
	}
	return true, nil
}

// ApplyScrapeResult writes all scraper metadata for a match in one transaction.
// The sentinel tag is inserted last so interrupted writes remain retryable.
func (db *MediaDB) ApplyScrapeResult(
	ctx context.Context, mediaDBID, mediaTitleDBID int64, write *database.ScrapeWrite,
) (retErr error) {
	if db.sql.Load() == nil {
		return ErrNullSQL
	}
	// A malformed-page error while writing scraped properties (the table this corruption
	// class targets) flags the database corrupt so recovery rebuilds it.
	defer func() { db.NoteCorruption(retErr) }()
	target := database.ScrapeWriteTarget{MediaDBID: mediaDBID, MediaTitleDBID: mediaTitleDBID, Write: write}
	if err := validateScrapeWriteTarget("ApplyScrapeResult", target); err != nil {
		return err
	}

	tx, err := db.sql.Load().BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("ApplyScrapeResult: begin transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	writeCtx := newScrapeWriteTxContext(tx)
	if err := applyScrapeWriteTarget(ctx, writeCtx, target); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("ApplyScrapeResult: commit: %w", err)
	}
	committed = true
	// Scraped tags can change which tags distinguish a title's variants. Refresh
	// after commit (the scrape ran on its own tx, not db.tx). Non-fatal.
	if disErr := db.RecomputeTitleDisambiguation(ctx, []int64{mediaTitleDBID}); disErr != nil {
		log.Warn().Err(disErr).Int64("titleID", mediaTitleDBID).
			Msg("failed to recompute title disambiguation after scrape")
	}
	return nil
}

// ApplyScrapeResults writes multiple scraper payloads in one transaction.
// Each target's sentinel is written after its metadata, and the whole batch rolls
// back if any target fails.
func (db *MediaDB) ApplyScrapeResults(ctx context.Context, targets []database.ScrapeWriteTarget) (retErr error) {
	if db.sql.Load() == nil {
		return ErrNullSQL
	}
	defer func() { db.NoteCorruption(retErr) }()
	for _, target := range targets {
		if err := validateScrapeWriteTarget("ApplyScrapeResults", target); err != nil {
			return err
		}
	}
	if len(targets) == 0 {
		return nil
	}

	tx, err := db.sql.Load().BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("ApplyScrapeResults: begin transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	writeCtx := newScrapeWriteTxContext(tx)
	stats, err := applyScrapeWriteTargetsBulk(ctx, writeCtx, targets)
	if err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("ApplyScrapeResults: commit: %w", err)
	}
	committed = true
	// Scraped tags can change which tags distinguish a title's variants. Refresh
	// the affected titles after commit (the batch ran on its own tx). Non-fatal.
	titleIDs := make([]int64, 0, len(targets))
	seenTitles := make(map[int64]struct{}, len(targets))
	for i := range targets {
		id := targets[i].MediaTitleDBID
		if _, ok := seenTitles[id]; ok {
			continue
		}
		seenTitles[id] = struct{}{}
		titleIDs = append(titleIDs, id)
	}
	if disErr := db.RecomputeTitleDisambiguation(ctx, titleIDs); disErr != nil {
		log.Warn().Err(disErr).Msg("failed to recompute title disambiguation after scrape batch")
	}
	stats.Duration = stats.Duration.Round(time.Microsecond)
	log.Debug().
		Int("targets", stats.Targets).
		Int("title_tag_deletes", stats.TitleTagDeletes).
		Int("title_tag_insert_rows", stats.TitleTagInsertRows).
		Int("title_tag_insert_statements", stats.TitleTagInsertStatements).
		Int("title_property_upsert_rows", stats.TitlePropertyUpsertRows).
		Int("title_property_upsert_statements", stats.TitlePropertyUpsertStatements).
		Int("sentinel_delete_statements", stats.SentinelDeleteStatements).
		Int("sentinel_insert_rows", stats.SentinelInsertRows).
		Int("sentinel_insert_statements", stats.SentinelInsertStatements).
		Int("media_tag_fallback_targets", stats.MediaTagFallbackTargets).
		Int("media_property_fallback_targets", stats.MediaPropFallbackTargets).
		Dur("duration", stats.Duration).
		Msg("mediadb: applied scrape results bulk")
	return nil
}

func applyScrapeWriteTargetsBulk(
	ctx context.Context, writeCtx *scrapeWriteTxContext, targets []database.ScrapeWriteTarget,
) (scrapeBatchSQLStats, error) {
	start := time.Now()
	stats := scrapeBatchSQLStats{Targets: len(targets)}
	if err := preloadScrapeWriteLookupCache(ctx, writeCtx, targets); err != nil {
		return stats, fmt.Errorf("preload scrape write lookups: %w", err)
	}
	for _, target := range targets {
		write := target.Write
		if len(write.MediaTags) > 0 {
			stats.MediaTagFallbackTargets++
			if err := upsertMediaTagsWithContext(ctx, writeCtx, target.MediaDBID, write.MediaTags); err != nil {
				return stats, fmt.Errorf("upsert media tags: %w", err)
			}
		}
		if len(write.MediaProps) > 0 {
			stats.MediaPropFallbackTargets++
			if err := upsertMediaPropertiesWithContext(ctx, writeCtx, target.MediaDBID, write.MediaProps); err != nil {
				return stats, fmt.Errorf("upsert media properties: %w", err)
			}
		}
	}
	if err := upsertMediaTitleTagsBulkWithContext(ctx, writeCtx, targets, &stats); err != nil {
		return stats, fmt.Errorf("upsert title tags: %w", err)
	}
	if err := upsertMediaTitlePropertiesBulkWithContext(ctx, writeCtx, targets, &stats); err != nil {
		return stats, fmt.Errorf("upsert title properties: %w", err)
	}
	if err := upsertScrapeSentinelsBulkWithContext(ctx, writeCtx, targets, &stats); err != nil {
		return stats, fmt.Errorf("upsert sentinel tag: %w", err)
	}
	stats.Duration = time.Since(start)
	return stats, nil
}

func validateScrapeWriteTarget(method string, target database.ScrapeWriteTarget) error {
	if target.Write == nil {
		return fmt.Errorf("%s: write is nil", method)
	}
	return nil
}

func applyScrapeWriteTarget(
	ctx context.Context, writeCtx *scrapeWriteTxContext, target database.ScrapeWriteTarget,
) error {
	write := target.Write
	if len(write.MediaTags) > 0 {
		if err := upsertMediaTagsWithContext(ctx, writeCtx, target.MediaDBID, write.MediaTags); err != nil {
			return fmt.Errorf("upsert media tags: %w", err)
		}
	}
	if len(write.TitleTags) > 0 {
		if err := upsertMediaTitleTagsWithContext(ctx, writeCtx, target.MediaTitleDBID, write.TitleTags); err != nil {
			return fmt.Errorf("upsert title tags: %w", err)
		}
	}
	if len(write.TitleProps) > 0 {
		err := upsertMediaTitlePropertiesWithContext(ctx, writeCtx, target.MediaTitleDBID, write.TitleProps)
		if err != nil {
			return fmt.Errorf("upsert title properties: %w", err)
		}
	}
	if len(write.MediaProps) > 0 {
		if err := upsertMediaPropertiesWithContext(ctx, writeCtx, target.MediaDBID, write.MediaProps); err != nil {
			return fmt.Errorf("upsert media properties: %w", err)
		}
	}
	sentinel := []database.TagInfo{write.Sentinel}
	if err := upsertMediaTagsWithContext(ctx, writeCtx, target.MediaDBID, sentinel); err != nil {
		return fmt.Errorf("upsert sentinel tag: %w", err)
	}
	return nil
}

type titleTagTypeKey struct {
	mediaTitleDBID int64
	typeDBID       int64
}

type titleTagPair struct {
	mediaTitleDBID int64
	tagDBID        int64
}

type titlePropKey struct {
	mediaTitleDBID int64
	typeTagDBID    int64
}

type titlePropRow struct {
	p   database.MediaProperty
	key titlePropKey
}

func upsertMediaTitleTagsBulkWithContext(
	ctx context.Context,
	writeCtx *scrapeWriteTxContext,
	targets []database.ScrapeWriteTarget,
	stats *scrapeBatchSQLStats,
) error {
	exclusiveDeletes := make(map[int64]map[int64]struct{})
	exclusiveFinal := make(map[titleTagTypeKey][]int64)
	additivePairs := make(map[titleTagPair]struct{})

	for _, target := range targets {
		if len(target.Write.TitleTags) == 0 {
			continue
		}
		typeOrder := make([]string, 0, len(target.Write.TitleTags))
		byType := make(map[string][]database.TagInfo, len(target.Write.TitleTags))
		for _, ti := range target.Write.TitleTags {
			if _, ok := byType[ti.Type]; !ok {
				typeOrder = append(typeOrder, ti.Type)
			}
			byType[ti.Type] = append(byType[ti.Type], ti)
		}
		for _, typeName := range typeOrder {
			typeDBID, isExclusive, err := writeCtx.resolveTagType(ctx, typeName)
			if err != nil {
				return err
			}
			tagInfos := byType[typeName]
			if isExclusive {
				seen := make(map[string]struct{}, len(tagInfos))
				for _, ti := range tagInfos {
					seen[tags.PadTagValue(ti.Tag)] = struct{}{}
				}
				if len(seen) > 1 {
					return fmt.Errorf("exclusive tag type %q received multiple values", typeName)
				}
				if _, ok := exclusiveDeletes[typeDBID]; !ok {
					exclusiveDeletes[typeDBID] = make(map[int64]struct{})
				}
				exclusiveDeletes[typeDBID][target.MediaTitleDBID] = struct{}{}
			}

			resolved := make([]int64, 0, len(tagInfos))
			for _, ti := range tagInfos {
				tagValue := tags.PadTagValue(ti.Tag)
				tagDBID, err := writeCtx.resolveTag(ctx, typeDBID, typeName, tagValue, ti.Label)
				if err != nil {
					return err
				}
				resolved = append(resolved, tagDBID)
			}
			if isExclusive {
				exclusiveFinal[titleTagTypeKey{mediaTitleDBID: target.MediaTitleDBID, typeDBID: typeDBID}] = resolved
				continue
			}
			for _, tagDBID := range resolved {
				additivePairs[titleTagPair{mediaTitleDBID: target.MediaTitleDBID, tagDBID: tagDBID}] = struct{}{}
			}
		}
	}

	if err := deleteMediaTitleTagsByExclusiveType(ctx, writeCtx.tx, exclusiveDeletes, stats); err != nil {
		return err
	}

	pairs := make([]titleTagPair, 0, len(additivePairs)+len(exclusiveFinal))
	for pair := range additivePairs {
		pairs = append(pairs, pair)
	}
	for key, tagDBIDs := range exclusiveFinal {
		for _, tagDBID := range tagDBIDs {
			pairs = append(pairs, titleTagPair{mediaTitleDBID: key.mediaTitleDBID, tagDBID: tagDBID})
		}
	}
	return insertMediaTitleTagPairs(ctx, writeCtx.tx, pairs, stats)
}

func deleteMediaTitleTagsByExclusiveType(
	ctx context.Context, tx *sql.Tx, exclusiveDeletes map[int64]map[int64]struct{}, stats *scrapeBatchSQLStats,
) error {
	for typeDBID, titleIDs := range exclusiveDeletes {
		ids := make([]int64, 0, len(titleIDs))
		for id := range titleIDs {
			ids = append(ids, id)
		}
		for start := 0; start < len(ids); start += bulkDeleteEntityIDsPerStmt {
			end := start + bulkDeleteEntityIDsPerStmt
			if end > len(ids) {
				end = len(ids)
			}
			chunk := ids[start:end]
			args := make([]any, 0, len(chunk)+1)
			for _, id := range chunk {
				args = append(args, id)
			}
			args = append(args, typeDBID)
			//nolint:gosec // Safe: prepareVariadic only generates SQL placeholders.
			query := `DELETE FROM MediaTitleTags WHERE MediaTitleDBID IN (` +
				prepareVariadic("?", ",", len(chunk)) +
				`) AND EXISTS (` +
				`SELECT 1 FROM Tags WHERE Tags.DBID = MediaTitleTags.TagDBID AND Tags.TypeDBID = ?` +
				`)`
			if _, err := tx.ExecContext(ctx, query, args...); err != nil {
				return fmt.Errorf("failed to delete media title tags for type: %w", err)
			}
			stats.TitleTagDeletes++
		}
	}
	return nil
}

func insertMediaTitleTagPairs(
	ctx context.Context, tx *sql.Tx, pairs []titleTagPair, stats *scrapeBatchSQLStats,
) error {
	if len(pairs) == 0 {
		return nil
	}
	for start := 0; start < len(pairs); start += bulkTagInsertRowsPerStmt {
		end := start + bulkTagInsertRowsPerStmt
		if end > len(pairs) {
			end = len(pairs)
		}
		chunk := pairs[start:end]
		args := make([]any, 0, len(chunk)*2)
		for _, pair := range chunk {
			args = append(args, pair.mediaTitleDBID, pair.tagDBID)
		}
		//nolint:gosec // Safe: prepareVariadic only generates SQL placeholders.
		query := `INSERT OR IGNORE INTO MediaTitleTags (MediaTitleDBID, TagDBID) VALUES ` +
			prepareVariadic("(?, ?)", ",", len(chunk))
		if _, err := tx.ExecContext(ctx, query, args...); err != nil {
			return fmt.Errorf("failed to insert media title tag links: %w", err)
		}
		stats.TitleTagInsertRows += len(chunk)
		stats.TitleTagInsertStatements++
	}
	return nil
}

func upsertMediaTitlePropertiesBulkWithContext(
	ctx context.Context,
	writeCtx *scrapeWriteTxContext,
	targets []database.ScrapeWriteTarget,
	stats *scrapeBatchSQLStats,
) error {
	rowsByKey := make(map[titlePropKey]titlePropRow)
	for _, target := range targets {
		for _, p := range target.Write.TitleProps {
			typeTagDBID, err := writeCtx.resolvePropertyTypeTag(ctx, p.TypeTag)
			if err != nil {
				return fmt.Errorf("failed to resolve property type tag %q: %w", p.TypeTag, err)
			}
			key := titlePropKey{mediaTitleDBID: target.MediaTitleDBID, typeTagDBID: typeTagDBID}
			rowsByKey[key] = titlePropRow{key: key, p: p}
		}
	}
	if len(rowsByKey) == 0 {
		return nil
	}
	rows := make([]titlePropRow, 0, len(rowsByKey))
	for _, row := range rowsByKey {
		rows = append(rows, row)
	}
	for start := 0; start < len(rows); start += bulkPropUpsertRowsPerStmt {
		end := start + bulkPropUpsertRowsPerStmt
		if end > len(rows) {
			end = len(rows)
		}
		chunk := rows[start:end]
		args := make([]any, 0, len(chunk)*4)
		for _, row := range chunk {
			args = append(args, row.key.mediaTitleDBID, row.key.typeTagDBID, row.p.Text, row.p.BlobDBID)
		}
		//nolint:gosec // Safe: prepareVariadic only generates SQL placeholders.
		query := `
			INSERT INTO MediaTitleProperties (MediaTitleDBID, TypeTagDBID, Text, BlobDBID)
			VALUES ` + prepareVariadic("(?, ?, ?, ?)", ",", len(chunk)) + `
			ON CONFLICT(MediaTitleDBID, TypeTagDBID) DO UPDATE SET
				Text = excluded.Text,
				BlobDBID = excluded.BlobDBID
			WHERE MediaTitleProperties.Text IS NOT excluded.Text
			   OR MediaTitleProperties.BlobDBID IS NOT excluded.BlobDBID
		`
		if _, err := writeCtx.tx.ExecContext(ctx, query, args...); err != nil {
			return fmt.Errorf("failed to upsert MediaTitleProperties bulk: %w", err)
		}
		stats.TitlePropertyUpsertRows += len(chunk)
		stats.TitlePropertyUpsertStatements++
	}
	return nil
}

func upsertScrapeSentinelsBulkWithContext(
	ctx context.Context,
	writeCtx *scrapeWriteTxContext,
	targets []database.ScrapeWriteTarget,
	stats *scrapeBatchSQLStats,
) error {
	pairs := make([]mediaTagPair, 0, len(targets))
	seen := make(map[mediaTagPair]struct{}, len(targets))
	exclusiveDeletes := make(map[int64]map[int64]struct{})
	for _, target := range targets {
		sentinel := target.Write.Sentinel
		typeDBID, isExclusive, err := writeCtx.resolveTagType(ctx, sentinel.Type)
		if err != nil {
			return err
		}
		if isExclusive {
			if _, ok := exclusiveDeletes[typeDBID]; !ok {
				exclusiveDeletes[typeDBID] = make(map[int64]struct{})
			}
			exclusiveDeletes[typeDBID][target.MediaDBID] = struct{}{}
		}
		tagDBID, err := writeCtx.resolveTag(
			ctx, typeDBID, sentinel.Type, tags.PadTagValue(sentinel.Tag), sentinel.Label,
		)
		if err != nil {
			return err
		}
		pair := mediaTagPair{mediaDBID: target.MediaDBID, tagDBID: tagDBID}
		if _, ok := seen[pair]; ok {
			continue
		}
		seen[pair] = struct{}{}
		pairs = append(pairs, pair)
	}
	for typeDBID, mediaIDs := range exclusiveDeletes {
		ids := make([]int64, 0, len(mediaIDs))
		for id := range mediaIDs {
			ids = append(ids, id)
		}
		for start := 0; start < len(ids); start += bulkDeleteEntityIDsPerStmt {
			end := start + bulkDeleteEntityIDsPerStmt
			if end > len(ids) {
				end = len(ids)
			}
			chunk := ids[start:end]
			args := make([]any, 0, len(chunk)+1)
			for _, id := range chunk {
				args = append(args, id)
			}
			args = append(args, typeDBID)
			//nolint:gosec // Safe: prepareVariadic only generates SQL placeholders.
			query := `DELETE FROM MediaTags WHERE MediaDBID IN (` + prepareVariadic("?", ",", len(chunk)) +
				`) AND TagDBID IN (SELECT DBID FROM Tags WHERE TypeDBID = ?)`
			if _, err := writeCtx.tx.ExecContext(ctx, query, args...); err != nil {
				return fmt.Errorf("failed to delete media sentinel tags for type: %w", err)
			}
			stats.SentinelDeleteStatements++
		}
	}
	return insertMediaTagPairs(ctx, writeCtx.tx, pairs, stats)
}

type mediaTagPair struct {
	mediaDBID int64
	tagDBID   int64
}

func insertMediaTagPairs(
	ctx context.Context, tx *sql.Tx, pairs []mediaTagPair, stats *scrapeBatchSQLStats,
) error {
	if len(pairs) == 0 {
		return nil
	}
	for start := 0; start < len(pairs); start += bulkTagInsertRowsPerStmt {
		end := start + bulkTagInsertRowsPerStmt
		if end > len(pairs) {
			end = len(pairs)
		}
		chunk := pairs[start:end]
		args := make([]any, 0, len(chunk)*2)
		for _, pair := range chunk {
			args = append(args, pair.mediaDBID, pair.tagDBID)
		}
		//nolint:gosec // Safe: prepareVariadic only generates SQL placeholders.
		query := `INSERT OR IGNORE INTO MediaTags (MediaDBID, TagDBID) VALUES ` +
			prepareVariadic("(?, ?)", ",", len(chunk))
		if _, err := tx.ExecContext(ctx, query, args...); err != nil {
			return fmt.Errorf("failed to insert media tag links: %w", err)
		}
		stats.SentinelInsertRows += len(chunk)
		stats.SentinelInsertStatements++
	}
	return nil
}

func upsertMediaTagsWithContext(
	ctx context.Context, writeCtx *scrapeWriteTxContext, mediaDBID int64, tagInfos []database.TagInfo,
) error {
	return upsertTagsWithContext(ctx, writeCtx, tagInfos, func(tx *sql.Tx, typeDBID int64) error {
		_, err := tx.ExecContext(ctx,
			`DELETE FROM MediaTags WHERE MediaDBID = ? AND TagDBID IN (SELECT DBID FROM Tags WHERE TypeDBID = ?)`,
			mediaDBID, typeDBID,
		)
		if err != nil {
			return fmt.Errorf("failed to delete media tags for type: %w", err)
		}
		return nil
	}, func(tx *sql.Tx, tagDBID int64) error {
		_, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO MediaTags (MediaDBID, TagDBID) VALUES (?, ?)`,
			mediaDBID, tagDBID,
		)
		if err != nil {
			return fmt.Errorf("failed to insert media tag link: %w", err)
		}
		return nil
	})
}

func upsertMediaTitleTagsWithContext(
	ctx context.Context, writeCtx *scrapeWriteTxContext, mediaTitleDBID int64, tagInfos []database.TagInfo,
) error {
	return upsertTagsWithContext(ctx, writeCtx, tagInfos, func(tx *sql.Tx, typeDBID int64) error {
		const q = `DELETE FROM MediaTitleTags` +
			` WHERE MediaTitleDBID = ? AND TagDBID IN (SELECT DBID FROM Tags WHERE TypeDBID = ?)`
		_, err := tx.ExecContext(ctx, q, mediaTitleDBID, typeDBID)
		if err != nil {
			return fmt.Errorf("failed to delete media title tags for type: %w", err)
		}
		return nil
	}, func(tx *sql.Tx, tagDBID int64) error {
		_, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO MediaTitleTags (MediaTitleDBID, TagDBID) VALUES (?, ?)`,
			mediaTitleDBID, tagDBID,
		)
		if err != nil {
			return fmt.Errorf("failed to insert media title tag link: %w", err)
		}
		return nil
	})
}

func upsertMediaProperties(ctx context.Context, q sqlQueryable, mediaDBID int64, props []database.MediaProperty) error {
	for _, p := range props {
		typeTagDBID, err := resolvePropertyTypeTag(ctx, q, p.TypeTag)
		if err != nil {
			return fmt.Errorf("failed to resolve property type tag %q: %w", p.TypeTag, err)
		}
		if err := upsertMediaProperty(ctx, q, mediaDBID, typeTagDBID, &p); err != nil {
			return err
		}
	}
	return nil
}

func upsertMediaPropertiesWithContext(
	ctx context.Context, writeCtx *scrapeWriteTxContext, mediaDBID int64, props []database.MediaProperty,
) error {
	for _, p := range props {
		typeTagDBID, err := writeCtx.resolvePropertyTypeTag(ctx, p.TypeTag)
		if err != nil {
			return fmt.Errorf("failed to resolve property type tag %q: %w", p.TypeTag, err)
		}
		if err := upsertMediaProperty(ctx, writeCtx.tx, mediaDBID, typeTagDBID, &p); err != nil {
			return err
		}
	}
	return nil
}

func upsertMediaProperty(
	ctx context.Context, q sqlQueryable, mediaDBID, typeTagDBID int64, p *database.MediaProperty,
) error {
	_, err := q.ExecContext(ctx, `
		INSERT INTO MediaProperties (MediaDBID, TypeTagDBID, Text, BlobDBID)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(MediaDBID, TypeTagDBID) DO UPDATE SET
			Text    = excluded.Text,
			BlobDBID = excluded.BlobDBID
	`, mediaDBID, typeTagDBID, p.Text, p.BlobDBID)
	if err != nil {
		return fmt.Errorf("failed to upsert MediaProperty (typeTag=%q): %w", p.TypeTag, err)
	}
	return nil
}

// DeleteMediaTitleProperty removes the property row for (mediaTitleDBID, typeTagDBID)
// from MediaTitleProperties. It is a no-op when no matching row exists.
func (db *MediaDB) DeleteMediaTitleProperty(ctx context.Context, mediaTitleDBID, typeTagDBID int64) error {
	if db.sql.Load() == nil {
		return ErrNullSQL
	}
	_, err := db.sql.Load().ExecContext(ctx,
		`DELETE FROM MediaTitleProperties WHERE MediaTitleDBID = ? AND TypeTagDBID = ?`,
		mediaTitleDBID, typeTagDBID,
	)
	if err != nil {
		return fmt.Errorf(
			"failed to delete MediaTitleProperty (mediaTitleDBID=%d, typeTagDBID=%d): %w",
			mediaTitleDBID, typeTagDBID, err)
	}
	return nil
}

// DeleteMediaProperty removes the property row for (mediaDBID, typeTagDBID)
// from MediaProperties. It is a no-op when no matching row exists.
func (db *MediaDB) DeleteMediaProperty(ctx context.Context, mediaDBID, typeTagDBID int64) error {
	if db.sql.Load() == nil {
		return ErrNullSQL
	}
	_, err := db.sql.Load().ExecContext(ctx,
		`DELETE FROM MediaProperties WHERE MediaDBID = ? AND TypeTagDBID = ?`,
		mediaDBID, typeTagDBID,
	)
	if err != nil {
		return fmt.Errorf(
			"failed to delete MediaProperty (mediaDBID=%d, typeTagDBID=%d): %w",
			mediaDBID, typeTagDBID, err)
	}
	return nil
}

// resolvePropertyTypeTag looks up the DBID of the Tags row for the given full
// tag string (e.g. "property:description"). The tag must already exist in the DB
// (seeded by SeedCanonicalTags). Returns an error if not found.
func resolvePropertyTypeTag(ctx context.Context, db sqlQueryable, typeTag string) (int64, error) {
	// typeTag format: "type:value" — split on first colon.
	idx := strings.Index(typeTag, ":")
	if idx < 0 {
		return 0, fmt.Errorf("property type tag %q is not in type:value format", typeTag)
	}
	tagType := typeTag[:idx]
	tagValue := tags.PadTagValue(typeTag[idx+1:])
	unpadded := typeTag[idx+1:]

	var tagDBID int64
	err := db.QueryRowContext(ctx, `
		SELECT t.DBID
		FROM Tags t
		JOIN TagTypes tt ON t.TypeDBID = tt.DBID
		WHERE tt.Type = ? AND (t.Tag = ? OR t.Tag = ?)
		LIMIT 1
	`, tagType, tagValue, unpadded).Scan(&tagDBID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("property type tag %q not found in Tags table (run SeedCanonicalTags first)", typeTag)
	}
	if err != nil {
		return 0, fmt.Errorf("failed to resolve property type tag %q: %w", typeTag, err)
	}
	return tagDBID, nil
}

// FindMediaTitlesWithoutSentinel returns MediaTitle rows for the given system
// that have no Media row tagged with sentinelTag. sentinelTag must be in
// "type:value" format (e.g. "scraper.gamelist.xml:scraped").
func (db *MediaDB) FindMediaTitlesWithoutSentinel(
	ctx context.Context, systemDBID int64, sentinelTag string,
) ([]database.MediaTitle, error) {
	if db.sql.Load() == nil {
		return nil, ErrNullSQL
	}

	idx := strings.Index(sentinelTag, ":")
	if idx < 0 {
		return nil, fmt.Errorf("sentinelTag %q is not in type:value format", sentinelTag)
	}
	tagType := sentinelTag[:idx]
	tagPart := sentinelTag[idx+1:]
	tagDBIDs, err := findScraperSentinelTagDBIDs(ctx, db.sql.Load(), tagType, tagPart)
	if err != nil {
		return nil, fmt.Errorf("failed to find sentinel tag DBIDs: %w", err)
	}
	if len(tagDBIDs) == 0 {
		return findMediaTitlesBySystemDBID(ctx, db.sql.Load(), systemDBID)
	}

	placeholders := prepareVariadic("?", ",", len(tagDBIDs))
	args := make([]any, 0, len(tagDBIDs)+1)
	args = append(args, systemDBID)
	for _, tagDBID := range tagDBIDs {
		args = append(args, tagDBID)
	}

	//nolint:gosec // Safe: prepareVariadic only generates SQL placeholders.
	stmt, err := db.sql.Load().PrepareContext(ctx, `
		SELECT mt.DBID, mt.SystemDBID, mt.Slug, mt.Name
		FROM MediaTitles mt
		WHERE mt.SystemDBID = ?
		  AND NOT EXISTS (
			SELECT 1
			FROM Media m
			JOIN MediaTags mtag ON m.DBID = mtag.MediaDBID
			WHERE m.MediaTitleDBID = mt.DBID
			  AND mtag.TagDBID IN (`+placeholders+`)
		  )
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare FindMediaTitlesWithoutSentinel: %w", err)
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()

	rows, err := stmt.QueryContext(ctx, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query FindMediaTitlesWithoutSentinel: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close rows")
		}
	}()

	var titles []database.MediaTitle
	for rows.Next() {
		var t database.MediaTitle
		if err := rows.Scan(&t.DBID, &t.SystemDBID, &t.Slug, &t.Name); err != nil {
			return nil, fmt.Errorf("failed to scan MediaTitle: %w", err)
		}
		titles = append(titles, t)
	}
	return titles, rows.Err()
}

// FindMediaTitleByDBID returns the MediaTitle with the given DBID, or nil, nil
// when not found.
func (db *MediaDB) FindMediaTitleByDBID(ctx context.Context, dbid int64) (*database.MediaTitle, error) {
	if db.sql.Load() == nil {
		return nil, ErrNullSQL
	}
	stmt, err := db.sql.Load().PrepareContext(ctx, `
		SELECT DBID, SystemDBID, Slug, Name
		FROM MediaTitles
		WHERE DBID = ?
		LIMIT 1
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare FindMediaTitleByDBID: %w", err)
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()

	var t database.MediaTitle
	err = stmt.QueryRowContext(ctx, dbid).Scan(&t.DBID, &t.SystemDBID, &t.Slug, &t.Name)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil //nolint:nilnil // sql.ErrNoRows means not found; nil result is the "not found" sentinel
	}
	if err != nil {
		return nil, fmt.Errorf("failed to scan FindMediaTitleByDBID: %w", err)
	}
	return &t, nil
}

// FindMediaTitleBySystemAndSlug returns the MediaTitle matching systemDBID and
// slug, or nil, nil when not found.
func (db *MediaDB) FindMediaTitleBySystemAndSlug(
	ctx context.Context, systemDBID int64, slug string,
) (*database.MediaTitle, error) {
	if db.sql.Load() == nil {
		return nil, ErrNullSQL
	}
	stmt, err := db.sql.Load().PrepareContext(ctx, `
		SELECT DBID, SystemDBID, Slug, Name
		FROM MediaTitles
		WHERE SystemDBID = ? AND Slug = ?
		LIMIT 1
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare FindMediaTitleBySystemAndSlug: %w", err)
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()

	var t database.MediaTitle
	err = stmt.QueryRowContext(ctx, systemDBID, slug).Scan(&t.DBID, &t.SystemDBID, &t.Slug, &t.Name)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil //nolint:nilnil // sql.ErrNoRows means not found; nil result is the "not found" sentinel
	}
	if err != nil {
		return nil, fmt.Errorf("failed to scan FindMediaTitleBySystemAndSlug: %w", err)
	}
	return &t, nil
}

// GetMediaTitleProperties returns all MediaTitleProperties rows for the given
// title. TypeTag is populated as "type:value" from the joined Tags/TagTypes rows.
func (db *MediaDB) GetMediaTitleProperties(
	ctx context.Context, mediaTitleDBID int64,
) ([]database.MediaProperty, error) {
	return db.loadMediaTitleProperties(ctx, mediaTitleDBID)
}

func (db *MediaDB) loadMediaTitleProperties(
	ctx context.Context, mediaTitleDBID int64,
) ([]database.MediaProperty, error) {
	if db.sql.Load() == nil {
		return nil, ErrNullSQL
	}
	stmt, err := db.sql.Load().PrepareContext(ctx, mediaTitlePropertyQuery(
		"WHERE mtp.MediaTitleDBID = ?", propertyGroupOmit))
	if err != nil {
		return nil, fmt.Errorf("failed to prepare GetMediaTitleProperties: %w", err)
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()

	rows, err := stmt.QueryContext(ctx, mediaTitleDBID)
	if err != nil {
		return nil, fmt.Errorf("failed to query GetMediaTitleProperties: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close rows")
		}
	}()

	return scanProperties(rows)
}

// GetMediaTitlePropertiesByMediaTitleDBIDs returns properties grouped by
// MediaTitle DBID. Binary blobs are capped at MaxMediaPropertyBinaryBytes.
func (db *MediaDB) GetMediaTitlePropertiesByMediaTitleDBIDs(
	ctx context.Context, mediaTitleDBIDs []int64,
) (map[int64][]database.MediaProperty, error) {
	return db.loadMediaTitlePropertiesByMediaTitleDBIDs(ctx, mediaTitleDBIDs)
}

func (db *MediaDB) loadMediaTitlePropertiesByMediaTitleDBIDs(
	ctx context.Context, mediaTitleDBIDs []int64,
) (map[int64][]database.MediaProperty, error) {
	results := make(map[int64][]database.MediaProperty, len(mediaTitleDBIDs))
	if len(mediaTitleDBIDs) == 0 {
		return results, nil
	}
	if db.sql.Load() == nil {
		return nil, ErrNullSQL
	}

	args := int64Args(mediaTitleDBIDs)
	where := `WHERE mtp.MediaTitleDBID IN (` + prepareVariadic("?", ",", len(mediaTitleDBIDs)) + `)`
	//nolint:gosec // Safe: prepareVariadic only generates SQL placeholders like "?, ?, ?".
	rows, err := db.sql.Load().QueryContext(ctx, mediaTitlePropertyQuery(where, propertyGroupInclude), args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query GetMediaTitlePropertiesByMediaTitleDBIDs: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close rows")
		}
	}()

	return scanGroupedProperties(rows)
}

func (db *MediaDB) GetMediaTitlePropertyMetadata(
	ctx context.Context, mediaTitleDBID int64,
) ([]database.MediaProperty, error) {
	if db.sql.Load() == nil {
		return nil, ErrNullSQL
	}
	stmt, err := db.sql.Load().PrepareContext(ctx, mediaTitlePropertyMetadataQuery(
		"WHERE mtp.MediaTitleDBID = ?", propertyGroupOmit,
	))
	if err != nil {
		return nil, fmt.Errorf("failed to prepare GetMediaTitlePropertyMetadata: %w", err)
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()

	rows, err := stmt.QueryContext(ctx, mediaTitleDBID)
	if err != nil {
		return nil, fmt.Errorf("failed to query GetMediaTitlePropertyMetadata: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close rows")
		}
	}()

	return scanPropertyMetadata(rows)
}

func (db *MediaDB) GetMediaTitlePropertyMetadataByMediaTitleDBIDs(
	ctx context.Context, mediaTitleDBIDs []int64,
) (map[int64][]database.MediaProperty, error) {
	results := make(map[int64][]database.MediaProperty, len(mediaTitleDBIDs))
	if len(mediaTitleDBIDs) == 0 {
		return results, nil
	}
	if db.sql.Load() == nil {
		return nil, ErrNullSQL
	}

	args := int64Args(mediaTitleDBIDs)
	where := `WHERE mtp.MediaTitleDBID IN (` + prepareVariadic("?", ",", len(mediaTitleDBIDs)) + `)`
	//nolint:gosec // Safe: prepareVariadic only generates SQL placeholders like "?, ?, ?".
	rows, err := db.sql.Load().QueryContext(ctx, mediaTitlePropertyMetadataQuery(where, propertyGroupInclude), args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query GetMediaTitlePropertyMetadataByMediaTitleDBIDs: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close rows")
		}
	}()

	return scanGroupedPropertyMetadata(rows)
}

// GetMediaProperties returns all MediaProperties rows for the given Media record.
// TypeTag is populated as "type:value" from the joined Tags/TagTypes rows.
func (db *MediaDB) GetMediaProperties(ctx context.Context, mediaDBID int64) ([]database.MediaProperty, error) {
	return db.loadMediaProperties(ctx, mediaDBID)
}

func (db *MediaDB) loadMediaProperties(
	ctx context.Context, mediaDBID int64,
) ([]database.MediaProperty, error) {
	if db.sql.Load() == nil {
		return nil, ErrNullSQL
	}
	stmt, err := db.sql.Load().PrepareContext(ctx, mediaPropertyQuery("WHERE mp.MediaDBID = ?", propertyGroupOmit))
	if err != nil {
		return nil, fmt.Errorf("failed to prepare GetMediaProperties: %w", err)
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()

	rows, err := stmt.QueryContext(ctx, mediaDBID)
	if err != nil {
		return nil, fmt.Errorf("failed to query GetMediaProperties: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close rows")
		}
	}()

	return scanProperties(rows)
}

// GetMediaPropertiesByMediaDBIDs returns properties grouped by Media DBID.
// Binary blobs are capped at MaxMediaPropertyBinaryBytes.
func (db *MediaDB) GetMediaPropertiesByMediaDBIDs(
	ctx context.Context, mediaDBIDs []int64,
) (map[int64][]database.MediaProperty, error) {
	return db.loadMediaPropertiesByMediaDBIDs(ctx, mediaDBIDs)
}

func (db *MediaDB) loadMediaPropertiesByMediaDBIDs(
	ctx context.Context, mediaDBIDs []int64,
) (map[int64][]database.MediaProperty, error) {
	results := make(map[int64][]database.MediaProperty, len(mediaDBIDs))
	if len(mediaDBIDs) == 0 {
		return results, nil
	}
	if db.sql.Load() == nil {
		return nil, ErrNullSQL
	}

	args := int64Args(mediaDBIDs)
	where := `WHERE mp.MediaDBID IN (` + prepareVariadic("?", ",", len(mediaDBIDs)) + `)`
	//nolint:gosec // Safe: prepareVariadic only generates SQL placeholders like "?, ?, ?".
	rows, err := db.sql.Load().QueryContext(ctx, mediaPropertyQuery(where, propertyGroupInclude), args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query GetMediaPropertiesByMediaDBIDs: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close rows")
		}
	}()

	return scanGroupedProperties(rows)
}

func (db *MediaDB) GetMediaPropertyMetadata(ctx context.Context, mediaDBID int64) ([]database.MediaProperty, error) {
	if db.sql.Load() == nil {
		return nil, ErrNullSQL
	}
	query := mediaPropertyMetadataQuery("WHERE mp.MediaDBID = ?", propertyGroupOmit)
	stmt, err := db.sql.Load().PrepareContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare GetMediaPropertyMetadata: %w", err)
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()

	rows, err := stmt.QueryContext(ctx, mediaDBID)
	if err != nil {
		return nil, fmt.Errorf("failed to query GetMediaPropertyMetadata: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close rows")
		}
	}()

	return scanPropertyMetadata(rows)
}

func (db *MediaDB) GetMediaPropertyMetadataByMediaDBIDs(
	ctx context.Context, mediaDBIDs []int64,
) (map[int64][]database.MediaProperty, error) {
	results := make(map[int64][]database.MediaProperty, len(mediaDBIDs))
	if len(mediaDBIDs) == 0 {
		return results, nil
	}
	if db.sql.Load() == nil {
		return nil, ErrNullSQL
	}

	args := int64Args(mediaDBIDs)
	where := `WHERE mp.MediaDBID IN (` + prepareVariadic("?", ",", len(mediaDBIDs)) + `)`
	//nolint:gosec // Safe: prepareVariadic only generates SQL placeholders like "?, ?, ?".
	rows, err := db.sql.Load().QueryContext(ctx, mediaPropertyMetadataQuery(where, propertyGroupInclude), args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query GetMediaPropertyMetadataByMediaDBIDs: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close rows")
		}
	}()

	return scanGroupedPropertyMetadata(rows)
}

// GetMediaWithTitleAndSystem fetches a Media record together with its parent
// MediaTitle and System via a single JOIN query. Returns nil, nil when the
// mediaDBID does not exist. IsMissing is NOT filtered.
func (db *MediaDB) GetMediaWithTitleAndSystem(ctx context.Context, mediaDBID int64) (*database.MediaFullRow, error) {
	if db.sql.Load() == nil {
		return nil, ErrNullSQL
	}

	stmt, err := db.sql.Load().PrepareContext(ctx, `
		SELECT
			m.DBID, m.Path, m.ParentDir, m.SortName, m.IsMissing, m.MediaTitleDBID, m.SystemDBID,
			mt.DBID, mt.Slug, mt.SecondarySlug, mt.Name, mt.SlugLength, mt.SlugWordCount, mt.SystemDBID,
			mt.DisambiguationTypes,
			s.DBID, s.SystemID, s.Name
		FROM Media m
		INNER JOIN MediaTitles mt ON m.MediaTitleDBID = mt.DBID
		INNER JOIN Systems s ON mt.SystemDBID = s.DBID
		WHERE m.DBID = ?
		LIMIT 1
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare GetMediaWithTitleAndSystem: %w", err)
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()

	var row database.MediaFullRow
	err = stmt.QueryRowContext(ctx, mediaDBID).Scan(
		&row.DBID, &row.Path, &row.ParentDir, &row.SortName, &row.IsMissing,
		&row.MediaTitleDBID, &row.SystemDBID,
		&row.Title.DBID, &row.Title.Slug, &row.Title.SecondarySlug, &row.Title.Name,
		&row.Title.SlugLength, &row.Title.SlugWordCount, &row.Title.SystemDBID,
		&row.Title.DisambiguationTypes,
		&row.System.DBID, &row.System.SystemID, &row.System.Name,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil //nolint:nilnil // sql.ErrNoRows means not found; nil result is the "not found" sentinel
	}
	if err != nil {
		return nil, fmt.Errorf("failed to scan GetMediaWithTitleAndSystem: %w", err)
	}
	return &row, nil
}

func (db *MediaDB) GetMediaWithTitleAndSystemByIDs(
	ctx context.Context, mediaDBIDs []int64,
) (map[int64]database.MediaFullRow, error) {
	results := make(map[int64]database.MediaFullRow, len(mediaDBIDs))
	if len(mediaDBIDs) == 0 {
		return results, nil
	}
	if db.sql.Load() == nil {
		return nil, ErrNullSQL
	}

	args := int64Args(mediaDBIDs)
	//nolint:gosec // Safe: prepareVariadic only generates SQL placeholders like "?, ?, ?".
	rows, err := db.sql.Load().QueryContext(ctx, `
		SELECT
			m.DBID, m.Path, m.ParentDir, m.SortName, m.IsMissing, m.MediaTitleDBID, m.SystemDBID,
			mt.DBID, mt.Slug, mt.SecondarySlug, mt.Name, mt.SlugLength, mt.SlugWordCount, mt.SystemDBID,
			mt.DisambiguationTypes,
			s.DBID, s.SystemID, s.Name
		FROM Media m
		INNER JOIN MediaTitles mt ON m.MediaTitleDBID = mt.DBID
		INNER JOIN Systems s ON mt.SystemDBID = s.DBID
		WHERE m.DBID IN (`+prepareVariadic("?", ",", len(mediaDBIDs))+`)
	`, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query GetMediaWithTitleAndSystemByIDs: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close rows")
		}
	}()

	for rows.Next() {
		var row database.MediaFullRow
		if err := rows.Scan(
			&row.DBID, &row.Path, &row.ParentDir, &row.SortName, &row.IsMissing,
			&row.MediaTitleDBID, &row.SystemDBID,
			&row.Title.DBID, &row.Title.Slug, &row.Title.SecondarySlug, &row.Title.Name,
			&row.Title.SlugLength, &row.Title.SlugWordCount, &row.Title.SystemDBID,
			&row.Title.DisambiguationTypes,
			&row.System.DBID, &row.System.SystemID, &row.System.Name,
		); err != nil {
			return nil, fmt.Errorf("failed to scan GetMediaWithTitleAndSystemByIDs: %w", err)
		}
		results[row.DBID] = row
	}
	return results, rows.Err()
}

// GetMediaTagsByMediaDBID returns the file-level tags (MediaTags) for a single
// Media row, ordered by type then tag value.
func (db *MediaDB) GetMediaTagsByMediaDBID(ctx context.Context, mediaDBID int64) ([]database.TagInfo, error) {
	if db.sql.Load() == nil {
		return nil, ErrNullSQL
	}

	stmt, err := db.sql.Load().PrepareContext(ctx, `
		SELECT Tags.Tag, TagTypes.Type, Tags.DisplayName
		FROM MediaTags
		JOIN Tags ON MediaTags.TagDBID = Tags.DBID
		JOIN TagTypes ON Tags.TypeDBID = TagTypes.DBID
		WHERE MediaTags.MediaDBID = ?
		ORDER BY TagTypes.Type, Tags.Tag
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare GetMediaTagsByMediaDBID: %w", err)
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()

	rows, err := stmt.QueryContext(ctx, mediaDBID)
	if err != nil {
		return nil, fmt.Errorf("failed to query GetMediaTagsByMediaDBID: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close rows")
		}
	}()

	return scanTagInfos(rows)
}

func (db *MediaDB) GetMediaTagsByMediaDBIDs(
	ctx context.Context, mediaDBIDs []int64,
) (map[int64][]database.TagInfo, error) {
	results := make(map[int64][]database.TagInfo, len(mediaDBIDs))
	if len(mediaDBIDs) == 0 {
		return results, nil
	}
	if db.sql.Load() == nil {
		return nil, ErrNullSQL
	}

	args := int64Args(mediaDBIDs)
	//nolint:gosec // Safe: prepareVariadic only generates SQL placeholders like "?, ?, ?".
	rows, err := db.sql.Load().QueryContext(ctx, `
		SELECT MediaTags.MediaDBID, Tags.Tag, TagTypes.Type, Tags.DisplayName
		FROM MediaTags
		JOIN Tags ON MediaTags.TagDBID = Tags.DBID
		JOIN TagTypes ON Tags.TypeDBID = TagTypes.DBID
		WHERE MediaTags.MediaDBID IN (`+prepareVariadic("?", ",", len(mediaDBIDs))+`)
		ORDER BY MediaTags.MediaDBID, TagTypes.Type, Tags.Tag
	`, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query GetMediaTagsByMediaDBIDs: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close rows")
		}
	}()

	return scanGroupedTagInfos(rows)
}

// GetMediaTitleTagsByMediaTitleDBID returns the title-level tags (MediaTitleTags)
// for a single MediaTitle row, ordered by type then tag value.
func (db *MediaDB) GetMediaTitleTagsByMediaTitleDBID(
	ctx context.Context, mediaTitleDBID int64,
) ([]database.TagInfo, error) {
	if db.sql.Load() == nil {
		return nil, ErrNullSQL
	}

	stmt, err := db.sql.Load().PrepareContext(ctx, `
		SELECT Tags.Tag, TagTypes.Type, Tags.DisplayName
		FROM MediaTitleTags
		JOIN Tags ON MediaTitleTags.TagDBID = Tags.DBID
		JOIN TagTypes ON Tags.TypeDBID = TagTypes.DBID
		WHERE MediaTitleTags.MediaTitleDBID = ?
		ORDER BY TagTypes.Type, Tags.Tag
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare GetMediaTitleTagsByMediaTitleDBID: %w", err)
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()

	rows, err := stmt.QueryContext(ctx, mediaTitleDBID)
	if err != nil {
		return nil, fmt.Errorf("failed to query GetMediaTitleTagsByMediaTitleDBID: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close rows")
		}
	}()

	return scanTagInfos(rows)
}

func (db *MediaDB) GetMediaTitleTagsByMediaTitleDBIDs(
	ctx context.Context, mediaTitleDBIDs []int64,
) (map[int64][]database.TagInfo, error) {
	results := make(map[int64][]database.TagInfo, len(mediaTitleDBIDs))
	if len(mediaTitleDBIDs) == 0 {
		return results, nil
	}
	if db.sql.Load() == nil {
		return nil, ErrNullSQL
	}

	args := int64Args(mediaTitleDBIDs)
	//nolint:gosec // Safe: prepareVariadic only generates SQL placeholders like "?, ?, ?".
	rows, err := db.sql.Load().QueryContext(ctx, `
		SELECT MediaTitleTags.MediaTitleDBID, Tags.Tag, TagTypes.Type, Tags.DisplayName
		FROM MediaTitleTags
		JOIN Tags ON MediaTitleTags.TagDBID = Tags.DBID
		JOIN TagTypes ON Tags.TypeDBID = TagTypes.DBID
		WHERE MediaTitleTags.MediaTitleDBID IN (`+prepareVariadic("?", ",", len(mediaTitleDBIDs))+`)
		ORDER BY MediaTitleTags.MediaTitleDBID, TagTypes.Type, Tags.Tag
	`, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query GetMediaTitleTagsByMediaTitleDBIDs: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close rows")
		}
	}()

	return scanGroupedTagInfos(rows)
}

type propertyGroupMode int

const (
	propertyGroupOmit propertyGroupMode = iota
	propertyGroupInclude
)

func propertySelectColumns(entityIDColumn string, groupMode propertyGroupMode) string {
	parts := []string{}
	if groupMode == propertyGroupInclude {
		parts = append(parts, entityIDColumn)
	}
	dataColumn := fmt.Sprintf(
		"CASE WHEN mb.Data IS NOT NULL AND length(mb.Data) <= %d THEN mb.Data ELSE NULL END",
		database.MaxMediaPropertyBinaryBytes,
	)
	parts = append(parts,
		"tt.Type || ':' || t.Tag",
		"TypeTagDBID",
		"Text",
		"BlobDBID",
		"ContentType",
		"CASE WHEN mb.Data IS NOT NULL THEN length(mb.Data) ELSE NULL END",
		dataColumn,
	)
	return strings.Join(parts, ", ")
}

func propertyMetadataSelectColumns(entityIDColumn string, groupMode propertyGroupMode) string {
	parts := []string{}
	if groupMode == propertyGroupInclude {
		parts = append(parts, entityIDColumn)
	}
	parts = append(parts,
		"tt.Type || ':' || t.Tag",
		"TypeTagDBID",
		"Text",
		"BlobDBID",
		"ContentType",
		"CASE WHEN mb.Data IS NOT NULL THEN length(mb.Data) ELSE NULL END",
	)
	return strings.Join(parts, ", ")
}

func mediaTitlePropertyQuery(where string, groupMode propertyGroupMode) string {
	return `
		SELECT ` + propertySelectColumns("mtp.MediaTitleDBID", groupMode) + `
		FROM MediaTitleProperties mtp
		JOIN Tags t      ON mtp.TypeTagDBID = t.DBID
		JOIN TagTypes tt ON t.TypeDBID = tt.DBID
		LEFT JOIN MediaBlobs mb ON mtp.BlobDBID = mb.DBID
		` + where
}

func mediaTitlePropertyMetadataQuery(where string, groupMode propertyGroupMode) string {
	return `
		SELECT ` + propertyMetadataSelectColumns("mtp.MediaTitleDBID", groupMode) + `
		FROM MediaTitleProperties mtp
		JOIN Tags t      ON mtp.TypeTagDBID = t.DBID
		JOIN TagTypes tt ON t.TypeDBID = tt.DBID
		LEFT JOIN MediaBlobs mb ON mtp.BlobDBID = mb.DBID
		` + where
}

func mediaPropertyQuery(where string, groupMode propertyGroupMode) string {
	return `
		SELECT ` + propertySelectColumns("mp.MediaDBID", groupMode) + `
		FROM MediaProperties mp
		JOIN Tags t      ON mp.TypeTagDBID = t.DBID
		JOIN TagTypes tt ON t.TypeDBID = tt.DBID
		LEFT JOIN MediaBlobs mb ON mp.BlobDBID = mb.DBID
		` + where
}

func mediaPropertyMetadataQuery(where string, groupMode propertyGroupMode) string {
	return `
		SELECT ` + propertyMetadataSelectColumns("mp.MediaDBID", groupMode) + `
		FROM MediaProperties mp
		JOIN Tags t      ON mp.TypeTagDBID = t.DBID
		JOIN TagTypes tt ON t.TypeDBID = tt.DBID
		LEFT JOIN MediaBlobs mb ON mp.BlobDBID = mb.DBID
		` + where
}

func scanTagInfos(rows *sql.Rows) ([]database.TagInfo, error) {
	result := make([]database.TagInfo, 0)
	for rows.Next() {
		var t database.TagInfo
		if err := rows.Scan(&t.Tag, &t.Type, &t.Label); err != nil {
			return nil, fmt.Errorf("failed to scan TagInfo: %w", err)
		}
		t.Tag = tags.UnpadTagValue(t.Tag)
		result = append(result, t)
	}
	return result, rows.Err()
}

func scanGroupedTagInfos(rows *sql.Rows) (map[int64][]database.TagInfo, error) {
	result := make(map[int64][]database.TagInfo)
	for rows.Next() {
		var dbid int64
		var t database.TagInfo
		if err := rows.Scan(&dbid, &t.Tag, &t.Type, &t.Label); err != nil {
			return nil, fmt.Errorf("failed to scan grouped TagInfo: %w", err)
		}
		t.Tag = tags.UnpadTagValue(t.Tag)
		result[dbid] = append(result[dbid], t)
	}
	return result, rows.Err()
}

func scanProperties(rows *sql.Rows) ([]database.MediaProperty, error) {
	var props []database.MediaProperty
	for rows.Next() {
		var p database.MediaProperty
		var blobDBID sql.NullInt64
		var contentType sql.NullString
		var blobSize sql.NullInt64
		var binary []byte
		if err := rows.Scan(
			&p.TypeTag, &p.TypeTagDBID, &p.Text,
			&blobDBID, &contentType, &blobSize, &binary,
		); err != nil {
			return nil, fmt.Errorf("failed to scan MediaProperty: %w", err)
		}
		setPropertyBlobFields(&p, blobDBID, contentType, blobSize, binary)
		props = append(props, p)
	}
	if props == nil {
		props = []database.MediaProperty{}
	}
	return props, rows.Err()
}

func scanPropertyMetadata(rows *sql.Rows) ([]database.MediaProperty, error) {
	var props []database.MediaProperty
	for rows.Next() {
		prop, err := scanPropertyMetadataRow(rows)
		if err != nil {
			return nil, err
		}
		props = append(props, prop)
	}
	if props == nil {
		props = []database.MediaProperty{}
	}
	return props, rows.Err()
}

func scanGroupedProperties(rows *sql.Rows) (map[int64][]database.MediaProperty, error) {
	result := make(map[int64][]database.MediaProperty)
	for rows.Next() {
		var dbid int64
		prop, err := scanPropertyWithDBID(rows, &dbid)
		if err != nil {
			return nil, err
		}
		result[dbid] = append(result[dbid], prop)
	}
	return result, rows.Err()
}

func scanGroupedPropertyMetadata(rows *sql.Rows) (map[int64][]database.MediaProperty, error) {
	result := make(map[int64][]database.MediaProperty)
	for rows.Next() {
		var dbid int64
		prop, err := scanPropertyMetadataWithDBID(rows, &dbid)
		if err != nil {
			return nil, err
		}
		result[dbid] = append(result[dbid], prop)
	}
	return result, rows.Err()
}

func scanPropertyWithDBID(rows *sql.Rows, dbid *int64) (database.MediaProperty, error) {
	var p database.MediaProperty
	var blobDBID sql.NullInt64
	var contentType sql.NullString
	var blobSize sql.NullInt64
	var binary []byte
	if err := rows.Scan(
		dbid, &p.TypeTag, &p.TypeTagDBID, &p.Text,
		&blobDBID, &contentType, &blobSize, &binary,
	); err != nil {
		return database.MediaProperty{}, fmt.Errorf("failed to scan grouped MediaProperty: %w", err)
	}
	setPropertyBlobFields(&p, blobDBID, contentType, blobSize, binary)
	return p, nil
}

func scanPropertyMetadataRow(rows *sql.Rows) (database.MediaProperty, error) {
	var p database.MediaProperty
	var blobDBID sql.NullInt64
	var contentType sql.NullString
	var blobSize sql.NullInt64
	if err := rows.Scan(
		&p.TypeTag, &p.TypeTagDBID, &p.Text,
		&blobDBID, &contentType, &blobSize,
	); err != nil {
		return database.MediaProperty{}, fmt.Errorf("failed to scan MediaProperty metadata: %w", err)
	}
	setPropertyBlobFields(&p, blobDBID, contentType, blobSize, nil)
	return p, nil
}

func scanPropertyMetadataWithDBID(rows *sql.Rows, dbid *int64) (database.MediaProperty, error) {
	var p database.MediaProperty
	var blobDBID sql.NullInt64
	var contentType sql.NullString
	var blobSize sql.NullInt64
	if err := rows.Scan(
		dbid, &p.TypeTag, &p.TypeTagDBID, &p.Text,
		&blobDBID, &contentType, &blobSize,
	); err != nil {
		return database.MediaProperty{}, fmt.Errorf("failed to scan grouped MediaProperty metadata: %w", err)
	}
	setPropertyBlobFields(&p, blobDBID, contentType, blobSize, nil)
	return p, nil
}

func setPropertyBlobFields(
	p *database.MediaProperty,
	blobDBID sql.NullInt64,
	contentType sql.NullString,
	blobSize sql.NullInt64,
	binary []byte,
) {
	if blobDBID.Valid {
		p.BlobDBID = &blobDBID.Int64
	}
	p.ContentType = contentType.String
	if blobSize.Valid {
		p.BlobSize = blobSize.Int64
	}
	p.Binary = binary
}

func int64Args(values []int64) []any {
	args := make([]any, len(values))
	for i, value := range values {
		args[i] = value
	}
	return args
}

// ResolveSingletonContainerAliases implements MediaDBI.
// It fetches the direct media rows of every candidate child directory in a
// single ParentDir IN query and returns one SingletonContainerAlias per
// candidate that collapses to a single logical launch target. Candidates whose
// recursive FileCount exceeds their direct row count contain nested
// subdirectories and are omitted, as are ambiguous file sets. Tags and
// ZapScriptTags are populated via two batch queries plus in-memory
// disambiguation — the same approach used by the search path.
func (db *MediaDB) ResolveSingletonContainerAliases(
	ctx context.Context,
	systemDBID int64,
	dirCandidates []database.SingletonAliasCandidate,
) ([]database.SingletonContainerAlias, error) {
	if db.sql.Load() == nil {
		return nil, ErrNullSQL
	}
	if len(dirCandidates) == 0 {
		return nil, nil //nolint:nilnil // empty result is the "no aliases" sentinel, not an error
	}

	// Per-step timing, emitted once at debug level so the on-device breakdown of
	// a slow resolution is visible without changing behaviour.
	var inScanDur, fullRowsDur, tagsDur, zapDur, coverDur time.Duration
	var inScanRows int
	defer func() {
		log.Debug().
			Int("candidates", len(dirCandidates)).
			Int("inScanRows", inScanRows).
			Dur("inScanDuration", inScanDur).
			Dur("fullRowsDuration", fullRowsDur).
			Dur("tagsDuration", tagsDur).
			Dur("zapScriptDuration", zapDur).
			Dur("coverFlagsDuration", coverDur).
			Msg("resolve singleton aliases step timing")
	}()

	expectedCounts := make(map[string]int, len(dirCandidates))
	args := make([]any, 0, 1+len(dirCandidates))
	args = append(args, systemDBID)
	for _, c := range dirCandidates {
		childDir := c.ChildDir
		if !strings.HasSuffix(childDir, "/") {
			childDir += "/"
		}
		expectedCounts[childDir] = c.FileCount
		args = append(args, childDir)
	}

	// One query for the direct media rows of all candidate dirs, served by
	// idx_media_parentdir_system.
	inScanStart := time.Now()
	//nolint:gosec // Safe: prepareVariadic only generates SQL placeholders.
	rows, err := db.sql.Load().QueryContext(ctx, `
		SELECT DBID, MediaTitleDBID, SystemDBID, Path, ParentDir, IsMissing
		FROM Media
		WHERE SystemDBID = ? AND IsMissing = 0 AND ParentDir IN (`+
		prepareVariadic("?", ",", len(dirCandidates))+`)`, args...)
	if err != nil {
		return nil, fmt.Errorf("resolve singleton aliases query: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close rows")
		}
	}()

	childDirRows := make(map[string][]database.Media, len(dirCandidates))
	for rows.Next() {
		var m database.Media
		if scanErr := rows.Scan(
			&m.DBID, &m.MediaTitleDBID, &m.SystemDBID, &m.Path, &m.ParentDir, &m.IsMissing,
		); scanErr != nil {
			return nil, fmt.Errorf("resolve singleton aliases scan: %w", scanErr)
		}
		childDirRows[m.ParentDir] = append(childDirRows[m.ParentDir], m)
		inScanRows++
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, fmt.Errorf("resolve singleton aliases rows: %w", rowsErr)
	}
	inScanDur = time.Since(inScanStart)

	// For each candidate dir: skip if the recursive FileCount exceeds the
	// direct rows (media in nested subdirectories). Otherwise apply
	// selectContainerLaunchMedia to pick the launch target (mirrors the logic
	// in FindSingleContainerLaunchMedia).
	type resolved struct {
		childDir string
		media    database.Media
	}
	candidates := make([]resolved, 0, len(childDirRows))
	for childDir, directRows := range childDirRows {
		if len(directRows) != expectedCounts[childDir] {
			continue
		}
		chosen := selectContainerLaunchMedia(directRows)
		if chosen == nil {
			continue
		}
		candidates = append(candidates, resolved{childDir: childDir, media: *chosen})
	}
	if len(candidates) == 0 {
		return nil, nil //nolint:nilnil // empty result is the "no aliases" sentinel, not an error
	}

	// Batch-fetch full rows (title + system) and file-level tags.
	mediaDBIDs := make([]int64, 0, len(candidates))
	for _, c := range candidates {
		mediaDBIDs = append(mediaDBIDs, c.media.DBID)
	}
	fullRowsStart := time.Now()
	fullRows, err := db.GetMediaWithTitleAndSystemByIDs(ctx, mediaDBIDs)
	if err != nil {
		return nil, fmt.Errorf("resolve singleton aliases full rows: %w", err)
	}
	fullRowsDur = time.Since(fullRowsStart)
	tagsStart := time.Now()
	tagsMap, err := db.GetMediaTagsByMediaDBIDs(ctx, mediaDBIDs)
	if err != nil {
		return nil, fmt.Errorf("resolve singleton aliases tags: %w", err)
	}
	tagsDur = time.Since(tagsStart)

	// Build the alias list and a parallel synthetic results slice carrying each
	// title's stored DisambiguationTypes, which attachZapScriptTags reads to
	// populate ZapScriptTags (same path as the search/browse queries).
	aliases := make([]database.SingletonContainerAlias, 0, len(candidates))
	synthetic := make([]database.SearchResultWithCursor, 0, len(candidates))
	for _, c := range candidates {
		row, ok := fullRows[c.media.DBID]
		if !ok {
			continue
		}
		mediaTags := tagsMap[c.media.DBID]
		if mediaTags == nil {
			mediaTags = []database.TagInfo{}
		}
		aliases = append(aliases, database.SingletonContainerAlias{
			ChildDir: c.childDir,
			Row:      row,
			Tags:     mediaTags,
		})
		synthetic = append(synthetic, database.SearchResultWithCursor{
			SystemID:            row.System.SystemID,
			Name:                row.Title.Name,
			MediaID:             row.DBID,
			MediaTitleID:        row.Title.DBID,
			Tags:                mediaTags,
			DisambiguationTypes: row.Title.DisambiguationTypes,
		})
	}

	// Populate ZapScriptTags from each title's precomputed disambiguating types.
	zapStart := time.Now()
	if err := attachZapScriptTags(ctx, db.sql.Load(), synthetic); err != nil {
		return nil, fmt.Errorf("resolve singleton aliases disambiguation: %w", err)
	}
	zapDur = time.Since(zapStart)
	for i := range aliases {
		aliases[i].ZapScriptTags = synthetic[i].ZapScriptTags
	}

	// Batch cover-flag check: one indexed UNION ALL query for all alias media.
	// Populates HasCover so aliased directory entries show art in the grid.
	coverStart := time.Now()
	if err := fetchAndAttachCoverFlags(ctx, db.sql.Load(), synthetic); err != nil {
		return nil, fmt.Errorf("resolve singleton aliases cover flags: %w", err)
	}
	coverDur = time.Since(coverStart)
	for i := range aliases {
		aliases[i].HasCover = synthetic[i].HasCover
	}

	return aliases, nil
}
