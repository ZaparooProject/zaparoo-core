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
	"strconv"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
)

// dbConfigScrapeFingerprintPrefix prefixes the DBConfig rows that hold, per
// scraper and system, the state a scraper last completed that system in.
const dbConfigScrapeFingerprintPrefix = "ScrapeFingerprint:"

func scrapeFingerprintName(scraperID, systemID string) string {
	return dbConfigScrapeFingerprintPrefix + scraperID + ":" + systemID
}

// GetScrapeFingerprint returns the fingerprint a scraper stored for a system
// when it last completed it, or "" when it has stored none.
func (db *MediaDB) GetScrapeFingerprint(ctx context.Context, scraperID, systemID string) (string, error) {
	if db.sql.Load() == nil {
		return "", ErrNullSQL
	}
	var value string
	err := db.sql.Load().QueryRowContext(ctx,
		"SELECT Value FROM DBConfig WHERE Name = ?", scrapeFingerprintName(scraperID, systemID),
	).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("failed to get scrape fingerprint: %w", err)
	}
	return value, nil
}

// SetScrapeFingerprint records the state a scraper completed a system in.
func (db *MediaDB) SetScrapeFingerprint(ctx context.Context, scraperID, systemID, fingerprint string) error {
	if db.sql.Load() == nil {
		return ErrNullSQL
	}
	_, err := db.sql.Load().ExecContext(ctx,
		"INSERT OR REPLACE INTO DBConfig (Name, Value) VALUES (?, ?)",
		scrapeFingerprintName(scraperID, systemID), fingerprint,
	)
	if err != nil {
		return fmt.Errorf("failed to set scrape fingerprint: %w", err)
	}
	return nil
}

// dbConfigLibraryRevisionPrefix prefixes the DBConfig rows that count, per
// system, the index runs that changed that system's rows.
const dbConfigLibraryRevisionPrefix = "LibraryRevision:"

// LibraryRevision returns the system's library revision, 0 when indexing has
// not changed it since revisions were first kept.
func (db *MediaDB) LibraryRevision(ctx context.Context, systemID string) (int64, error) {
	if db.sql.Load() == nil {
		return 0, ErrNullSQL
	}
	return sqlLibraryRevision(ctx, db.sql.Load(), systemID)
}

func sqlLibraryRevision(ctx context.Context, db sqlQueryable, systemID string) (int64, error) {
	var value string
	err := db.QueryRowContext(ctx,
		"SELECT Value FROM DBConfig WHERE Name = ?", dbConfigLibraryRevisionPrefix+systemID,
	).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("failed to get library revision for %s: %w", systemID, err)
	}
	revision, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid library revision for %s: %w", systemID, err)
	}
	return revision, nil
}

// sqlBumpLibraryRevision records that indexing changed a system's rows. It
// runs in the transaction that made the change, so the two commit together.
func sqlBumpLibraryRevision(ctx context.Context, db sqlQueryable, systemID string) error {
	revision, err := sqlLibraryRevision(ctx, db, systemID)
	if err != nil {
		// An unreadable value is replaced: the revision only has to differ
		// from every earlier one.
		revision = time.Now().UnixNano()
	}
	_, err = db.ExecContext(ctx,
		"INSERT OR REPLACE INTO DBConfig (Name, Value) VALUES (?, ?)",
		dbConfigLibraryRevisionPrefix+systemID, strconv.FormatInt(revision+1, 10),
	)
	if err != nil {
		return fmt.Errorf("failed to bump library revision for %s: %w", systemID, err)
	}
	return nil
}

// sqlClearScrapeFingerprints forgets what scrapers recorded about the given
// systems, or about every system when none are named. Rows that were removed
// took their scraped metadata with them, and a library revision restarts when
// the database is rebuilt, so a fingerprint from before must not match after.
func sqlClearScrapeFingerprints(ctx context.Context, db sqlQueryable, systemIDs []string) error {
	if len(systemIDs) == 0 {
		if _, err := db.ExecContext(ctx,
			"DELETE FROM DBConfig WHERE Name LIKE ?", dbConfigScrapeFingerprintPrefix+"%",
		); err != nil {
			return fmt.Errorf("failed to clear scrape fingerprints: %w", err)
		}
		return nil
	}
	for _, systemID := range systemIDs {
		if _, err := db.ExecContext(ctx,
			"DELETE FROM DBConfig WHERE Name LIKE ? AND Name LIKE ?",
			dbConfigScrapeFingerprintPrefix+"%", "%:"+systemID,
		); err != nil {
			return fmt.Errorf("failed to clear scrape fingerprints for %s: %w", systemID, err)
		}
	}
	return nil
}

// GetTitlesByDBIDs returns the given MediaTitles rows. SystemID is left empty:
// the callers hold rows of one system they already know.
func (db *MediaDB) GetTitlesByDBIDs(ctx context.Context, titleDBIDs []int64) ([]database.TitleWithSystem, error) {
	if db.sql.Load() == nil {
		return nil, ErrNullSQL
	}
	titles := make([]database.TitleWithSystem, 0, len(titleDBIDs))
	for start := 0; start < len(titleDBIDs); start += batchLookupIDsPerQuery {
		chunk := titleDBIDs[start:min(start+batchLookupIDsPerQuery, len(titleDBIDs))]
		args := make([]any, 0, len(chunk))
		for _, id := range chunk {
			args = append(args, id)
		}
		//nolint:gosec // Safe: prepareVariadic only generates SQL placeholders.
		query := `SELECT DBID, Slug, Name, SystemDBID FROM MediaTitles WHERE DBID IN (` +
			prepareVariadic("?", ",", len(chunk)) + `)`
		var err error
		if titles, err = appendTitleRows(ctx, db.sql.Load(), query, args, titles); err != nil {
			return nil, err
		}
	}
	return titles, nil
}

func appendTitleRows(
	ctx context.Context, db *sql.DB, query string, args []any, titles []database.TitleWithSystem,
) ([]database.TitleWithSystem, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query titles by id: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var title database.TitleWithSystem
		if err := rows.Scan(&title.DBID, &title.Slug, &title.Name, &title.SystemDBID); err != nil {
			return nil, fmt.Errorf("failed to scan title: %w", err)
		}
		titles = append(titles, title)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate titles by id: %w", err)
	}
	return titles, nil
}
