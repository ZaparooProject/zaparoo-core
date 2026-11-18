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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A scraper that replaces a tag link the scanner wrote leaves the system
// different from what its files alone produce, so the system's scan record
// has to go. Replacing its own earlier link changes nothing a reconcile owns.
func TestScanMarker_ClearedOnlyWhenAScraperReplacesAScannerLink(t *testing.T) {
	t.Parallel()
	db := openScanScratchTestDB(t)
	ctx := context.Background()
	sqlDB := db.sql.Load()

	for _, stmt := range []string{
		`INSERT INTO Systems (DBID, SystemID, Name) VALUES (1, 'NES', 'NES'), (2, 'SNES', 'SNES')`,
		`INSERT INTO MediaTitles (DBID, SystemDBID, Slug, Name, SlugLength, SlugWordCount)
			VALUES (1, 1, 'a', 'A', 1, 1), (2, 2, 'b', 'B', 1, 1)`,
		`INSERT INTO Media (DBID, MediaTitleDBID, SystemDBID, Path, ParentDir, SortName, IsMissing)
			VALUES (10, 1, 1, '/nes/a.nes', '/nes/', 'A', 0), (20, 2, 2, '/snes/b.sfc', '/snes/', 'B', 0)`,
		`INSERT INTO TagTypes (DBID, Type) VALUES (900, 'testrev')`,
		`INSERT INTO Tags (DBID, TypeDBID, Tag) VALUES (901, 900, 'a')`,
		`INSERT INTO MediaTags (MediaDBID, TagDBID, Scraped) VALUES (10, 901, 0), (20, 901, 1)`,
	} {
		_, err := sqlDB.ExecContext(ctx, stmt)
		require.NoError(t, err, stmt)
	}
	require.NoError(t, db.SetScanMarker(ctx, "NES", "nes-record"))
	require.NoError(t, db.SetScanMarker(ctx, "SNES", "snes-record"))

	// SNES holds only the scraper's own link of this type.
	require.NoError(t, sqlClearScanMarkersForScannerLinks(ctx, sqlDB, "?", int64(20), int64(900)))
	marker, err := db.ScanMarker(ctx, "SNES")
	require.NoError(t, err)
	assert.Equal(t, "snes-record", marker)

	// NES holds a link the scanner wrote.
	require.NoError(t, sqlClearScanMarkersForScannerLinks(ctx, sqlDB, "?, ?", int64(10), int64(20), int64(900)))
	marker, err = db.ScanMarker(ctx, "NES")
	require.NoError(t, err)
	assert.Empty(t, marker)
	marker, err = db.ScanMarker(ctx, "SNES")
	require.NoError(t, err)
	assert.Equal(t, "snes-record", marker)

	// Truncating a system forgets its record with its rows.
	require.NoError(t, db.SetScanMarker(ctx, "NES", "nes-record"))
	require.NoError(t, db.TruncateSystems([]string{"NES"}))
	marker, err = db.ScanMarker(ctx, "NES")
	require.NoError(t, err)
	assert.Empty(t, marker)
}
