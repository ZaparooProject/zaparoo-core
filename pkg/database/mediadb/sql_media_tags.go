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
	"fmt"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/rs/zerolog/log"
)

const insertMediaTagSQL = `INSERT OR IGNORE INTO MediaTags (MediaDBID, TagDBID) VALUES (?, ?)`

func sqlFindMediaTag(ctx context.Context, db sqlQueryable, mediaTag database.MediaTag) (database.MediaTag, error) {
	var row database.MediaTag
	stmt, err := db.PrepareContext(ctx, `
		select
		MediaDBID, TagDBID
		from MediaTags
		where MediaDBID = ?
		and TagDBID = ?
		LIMIT 1;
	`)
	if err != nil {
		return row, fmt.Errorf("failed to prepare find media tag statement: %w", err)
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()
	err = stmt.QueryRowContext(ctx,
		mediaTag.MediaDBID,
		mediaTag.TagDBID,
	).Scan(
		&row.MediaDBID,
		&row.TagDBID,
	)
	if err != nil {
		return row, fmt.Errorf("failed to scan media tag row: %w", err)
	}
	return row, nil
}

func sqlInsertMediaTagWithPreparedStmt(
	ctx context.Context, stmt *sql.Stmt, row database.MediaTag,
) (database.MediaTag, error) {
	res, err := stmt.ExecContext(ctx, row.MediaDBID, row.TagDBID)
	if err != nil {
		return row, fmt.Errorf("failed to execute prepared insert media tag statement: %w", err)
	}

	lastID, err := res.LastInsertId()
	if err != nil {
		return row, fmt.Errorf("failed to get last insert ID for media tag: %w", err)
	}

	if lastID == 0 {
		// INSERT OR IGNORE occurred - row already existed, no new ID generated
		log.Debug().Int64("MediaDBID", row.MediaDBID).Int64("TagDBID", row.TagDBID).
			Msg("MediaTag already exists, INSERT OR IGNORE executed")
		// Note: row.DBID remains as originally provided (usually 0)
	} else {
		row.DBID = lastID
	}

	return row, nil
}

func sqlInsertMediaTag(ctx context.Context, db *sql.DB, row database.MediaTag) (database.MediaTag, error) {
	stmt, err := db.PrepareContext(ctx, insertMediaTagSQL)
	if err != nil {
		return row, fmt.Errorf("failed to prepare insert media tag statement: %w", err)
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()

	res, err := stmt.ExecContext(ctx, row.MediaDBID, row.TagDBID)
	if err != nil {
		return row, fmt.Errorf("failed to execute insert media tag statement: %w", err)
	}

	lastID, err := res.LastInsertId()
	if err != nil {
		return row, fmt.Errorf("failed to get last insert ID for media tag: %w", err)
	}

	if lastID == 0 {
		// INSERT OR IGNORE occurred - row already existed, no new ID generated
		log.Debug().Int64("MediaDBID", row.MediaDBID).Int64("TagDBID", row.TagDBID).
			Msg("MediaTag already exists, INSERT OR IGNORE executed")
		// Note: row.DBID remains as originally provided (usually 0)
	} else {
		row.DBID = lastID
	}

	return row, nil
}

func sqlDeleteMediaTag(ctx context.Context, db sqlQueryable, mediaDBID, tagDBID int64) error {
	if _, err := db.ExecContext(
		ctx,
		`DELETE FROM MediaTags WHERE MediaDBID = ? AND TagDBID = ?`,
		mediaDBID,
		tagDBID,
	); err != nil {
		return fmt.Errorf("failed to delete media tag: %w", err)
	}

	return nil
}

func sqlDeleteMediaTagsByTagIDs(ctx context.Context, db sqlQueryable, mediaDBID int64, tagDBIDs []int) error {
	if len(tagDBIDs) == 0 {
		return nil
	}

	args := make([]any, 0, len(tagDBIDs)+1)
	args = append(args, mediaDBID)
	for _, tagDBID := range tagDBIDs {
		args = append(args, tagDBID)
	}

	//nolint:gosec // Safe: prepareVariadic only generates SQL placeholders like "?, ?, ?".
	query := `DELETE FROM MediaTags WHERE MediaDBID = ? AND TagDBID IN (` +
		prepareVariadic("?", ",", len(tagDBIDs)) + `)`
	if _, err := db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("failed to delete media tags by tag IDs: %w", err)
	}

	return nil
}

func sqlGetMediaTagsBySystemID(ctx context.Context, db *sql.DB, systemID string) ([]database.MediaTagLink, error) {
	query := `
		SELECT mt.MediaDBID, mt.TagDBID
		FROM Media m INDEXED BY media_system_path_idx
		CROSS JOIN MediaTags mt
		WHERE m.SystemDBID = (SELECT DBID FROM Systems WHERE SystemID = ?)
		  AND mt.MediaDBID = m.DBID
	`
	return sqlQueryMediaTagLinksBySystemID(ctx, db, query, systemID, systemID)
}

// sqlGetNonScannerTagDBIDs returns the DBIDs of every tag the filename scanner
// does NOT own: user tags, cover/scrape "property" tags, the scraper-exclusive
// metadata types the filename parser never emits ("rating", "genre", "gamefamily"),
// and scraper sentinel/run markers (dynamic "scraper.<id>" / "scraper-run.<id>"
// types). Reconcile diffs a media's stored tags against the tags re-derived from
// its filename; without this exclusion any tag written by a scraper or cover
// resolution would look "stale" and be deleted on every re-index, silently wiping
// scraped data (and marking the title touched, forcing a full tags-cache rebuild).
func sqlGetNonScannerTagDBIDs(ctx context.Context, db *sql.DB) (map[int64]struct{}, error) {
	query := `
		SELECT t.DBID
		FROM Tags t
		JOIN TagTypes tt ON t.TypeDBID = tt.DBID
		WHERE tt.Type IN (?, ?, ?, ?, ?)
		   OR tt.Type LIKE ?
		   OR tt.Type LIKE ?
	`
	rows, err := db.QueryContext(ctx, query,
		string(tags.TagTypeUser),
		string(tags.TagTypeProperty),
		string(tags.TagTypeRating),
		string(tags.TagTypeGenre),
		string(tags.TagTypeGameFamily),
		string(tags.ScraperType(""))+"%",
		string(tags.ScraperRunType(""))+"%",
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query non-scanner tag DBIDs: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close rows")
		}
	}()

	ids := make(map[int64]struct{})
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("failed to scan non-scanner tag DBID: %w", err)
		}
		ids[id] = struct{}{}
	}
	return ids, rows.Err()
}

// sqlGetScannerMediaTagsBySystemID returns media-tag links for the scanner-owned
// tags only, excluding non-scanner tags (user, cover/scrape property, scraper
// markers). The exclusion set is filtered in Go against a pre-fetched DBID set
// rather than joining Tags/TagTypes in SQL: the link query is the hottest scanner
// read (it walks every MediaTags row for the system), and the extra per-row B-tree
// probes dominated its cost.
func sqlGetScannerMediaTagsBySystemID(
	ctx context.Context, db *sql.DB, systemID string,
) ([]database.MediaTagLink, error) {
	nonScannerTagIDs, err := sqlGetNonScannerTagDBIDs(ctx, db)
	if err != nil {
		return nil, err
	}

	query := `
		SELECT mt.MediaDBID, mt.TagDBID
		FROM Media m INDEXED BY media_system_path_idx
		CROSS JOIN MediaTags mt
		WHERE m.SystemDBID = (SELECT DBID FROM Systems WHERE SystemID = ?)
		  AND mt.MediaDBID = m.DBID
	`
	links, err := sqlQueryMediaTagLinksBySystemID(ctx, db, query, systemID, systemID)
	if err != nil {
		return nil, err
	}
	if len(nonScannerTagIDs) == 0 {
		return links, nil
	}

	filtered := links[:0]
	for _, link := range links {
		if _, isNonScanner := nonScannerTagIDs[link.TagDBID]; isNonScanner {
			continue
		}
		filtered = append(filtered, link)
	}
	return filtered, nil
}

func sqlQueryMediaTagLinksBySystemID(
	ctx context.Context, db *sql.DB, query string, systemID string, args ...any,
) ([]database.MediaTagLink, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query media tags by system: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close rows")
		}
	}()

	links := make([]database.MediaTagLink, 0)
	for rows.Next() {
		var link database.MediaTagLink
		if err := rows.Scan(&link.MediaDBID, &link.TagDBID); err != nil {
			return nil, fmt.Errorf("failed to scan media tags for system %s: %w", systemID, err)
		}
		links = append(links, link)
	}

	return links, rows.Err()
}
