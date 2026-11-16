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
)

// dbConfigScanMarkerPrefix prefixes the per-system record of what the scanner
// last reconciled. A system whose walk produces the same record again has
// nothing for a reconcile to change, so the scanner leaves it alone.
//
// That only holds while the scanner is the one writer of the rows a reconcile
// owns. The record is stored in this database, so replacing the database
// drops it, and the few writers that do change those rows from outside the
// scanner remove it (see sqlClearScanMarkers and
// sqlClearScanMarkersForMedia).
const dbConfigScanMarkerPrefix = "ScanWalk:"

// ScanMarker returns the record the scanner stored for a system when it last
// reconciled it, or "" when it has stored none.
func (db *MediaDB) ScanMarker(ctx context.Context, systemID string) (string, error) {
	if db.sql.Load() == nil {
		return "", ErrNullSQL
	}
	var value string
	err := db.conn().QueryRowContext(ctx,
		"SELECT Value FROM DBConfig WHERE Name = ?", dbConfigScanMarkerPrefix+systemID,
	).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("failed to get scan marker: %w", err)
	}
	return value, nil
}

// SetScanMarker records what the scanner reconciled a system from, or forgets
// it when marker is empty. It runs in the open indexing transaction, so the
// record and the rows it describes commit together.
func (db *MediaDB) SetScanMarker(ctx context.Context, systemID, marker string) error {
	if db.sql.Load() == nil {
		return ErrNullSQL
	}
	if marker == "" {
		return sqlClearScanMarkers(ctx, db.conn(), []string{systemID})
	}
	_, err := db.conn().ExecContext(ctx,
		"INSERT OR REPLACE INTO DBConfig (Name, Value) VALUES (?, ?)",
		dbConfigScanMarkerPrefix+systemID, marker,
	)
	if err != nil {
		return fmt.Errorf("failed to set scan marker: %w", err)
	}
	return nil
}

// sqlClearScanMarkers forgets the scan record of the given systems, or of
// every system when none are named.
func sqlClearScanMarkers(ctx context.Context, db sqlQueryable, systemIDs []string) error {
	if len(systemIDs) == 0 {
		if _, err := db.ExecContext(ctx,
			"DELETE FROM DBConfig WHERE Name LIKE ?", dbConfigScanMarkerPrefix+"%",
		); err != nil {
			return fmt.Errorf("failed to clear scan markers: %w", err)
		}
		return nil
	}
	for _, systemID := range systemIDs {
		if _, err := db.ExecContext(ctx,
			"DELETE FROM DBConfig WHERE Name = ?", dbConfigScanMarkerPrefix+systemID,
		); err != nil {
			return fmt.Errorf("failed to clear scan marker for %s: %w", systemID, err)
		}
	}
	return nil
}

// sqlClearScanMarkersForMedia forgets the scan record of every system that
// owns one of the media rows selected by mediaIDsSQL, a list or subquery of
// Media.DBID values taking args. A scraper calls it after it removed tag
// links from those rows that the scanner may have written: the next index
// has to reconcile the system to put them back.
func sqlClearScanMarkersForMedia(
	ctx context.Context, db sqlQueryable, mediaIDsSQL string, args ...any,
) error {
	//nolint:gosec // mediaIDsSQL is built by callers from placeholders only.
	_, err := db.ExecContext(ctx, `
		DELETE FROM DBConfig WHERE Name IN (
			SELECT '`+dbConfigScanMarkerPrefix+`' || s.SystemID
			FROM Media m JOIN Systems s ON s.DBID = m.SystemDBID
			WHERE m.DBID IN (`+mediaIDsSQL+`)
		)`, args...)
	if err != nil {
		return fmt.Errorf("failed to clear scan markers for scraped media: %w", err)
	}
	return nil
}

// sqlClearScanMarkersForScannerLinks is called just before a scraper replaces
// every link of one exclusive tag type on the given media. Where one of those
// links was written by the scanner, the scan record of the owning system is
// forgotten. args are the media IDs for mediaIDPlaceholders followed by the
// tag type's DBID.
func sqlClearScanMarkersForScannerLinks(
	ctx context.Context, db sqlQueryable, mediaIDPlaceholders string, args ...any,
) error {
	return sqlClearScanMarkersForMedia(ctx, db, `
		SELECT mt.MediaDBID FROM MediaTags mt
		WHERE mt.MediaDBID IN (`+mediaIDPlaceholders+`)
		  AND mt.Scraped = 0
		  AND mt.TagDBID IN (SELECT DBID FROM Tags WHERE TypeDBID = ?)`, args...)
}
