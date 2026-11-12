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
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScrapeFingerprint_RoundTripsPerScraperAndSystem(t *testing.T) {
	t.Parallel()
	db, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := t.Context()

	stored, err := db.GetScrapeFingerprint(ctx, "test", "NES")
	require.NoError(t, err)
	assert.Empty(t, stored, "nothing stored yet")

	require.NoError(t, db.SetScrapeFingerprint(ctx, "test", "NES", "first"))
	require.NoError(t, db.SetScrapeFingerprint(ctx, "test", "SNES", "other system"))
	require.NoError(t, db.SetScrapeFingerprint(ctx, "other", "NES", "other scraper"))
	require.NoError(t, db.SetScrapeFingerprint(ctx, "test", "NES", "second"))

	stored, err = db.GetScrapeFingerprint(ctx, "test", "NES")
	require.NoError(t, err)
	assert.Equal(t, "second", stored)
	stored, err = db.GetScrapeFingerprint(ctx, "other", "NES")
	require.NoError(t, err)
	assert.Equal(t, "other scraper", stored)
}

func TestLibraryRevision_CountsBumpsPerSystem(t *testing.T) {
	t.Parallel()
	db, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := t.Context()

	revision, err := db.LibraryRevision(ctx, "NES")
	require.NoError(t, err)
	assert.Zero(t, revision, "no index has changed the system yet")

	require.NoError(t, sqlBumpLibraryRevision(ctx, db.sql.Load(), "NES"))
	require.NoError(t, sqlBumpLibraryRevision(ctx, db.sql.Load(), "NES"))
	require.NoError(t, sqlBumpLibraryRevision(ctx, db.sql.Load(), "SNES"))

	revision, err = db.LibraryRevision(ctx, "NES")
	require.NoError(t, err)
	assert.Equal(t, int64(2), revision)
	revision, err = db.LibraryRevision(ctx, "SNES")
	require.NoError(t, err)
	assert.Equal(t, int64(1), revision)
}

func TestTruncate_ForgetsScrapeFingerprintsOfTheRowsItRemoves(t *testing.T) {
	t.Parallel()
	db, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := t.Context()
	for _, system := range []string{"NES", "SNES"} {
		require.NoError(t, db.SetScrapeFingerprint(ctx, "mister-docs", system, "stored"))
	}

	require.NoError(t, db.TruncateSystems([]string{"NES"}))
	stored, err := db.GetScrapeFingerprint(ctx, "mister-docs", "NES")
	require.NoError(t, err)
	assert.Empty(t, stored, "its rows and their metadata are gone")
	stored, err = db.GetScrapeFingerprint(ctx, "mister-docs", "SNES")
	require.NoError(t, err)
	assert.Equal(t, "stored", stored)

	require.NoError(t, db.Truncate())
	stored, err = db.GetScrapeFingerprint(ctx, "mister-docs", "SNES")
	require.NoError(t, err)
	assert.Empty(t, stored)
}

func TestGetTitlesByDBIDs_ReturnsOnlyTheRowsAsked(t *testing.T) {
	t.Parallel()
	db, cleanup := setupScrapeBulkTestDB(t)
	defer cleanup()

	titles, err := db.GetTitlesByDBIDs(t.Context(), []int64{3, 1, 99})
	require.NoError(t, err)
	assert.ElementsMatch(t, []database.TitleWithSystem{
		{DBID: 1, SystemDBID: 1, Slug: "mario", Name: "Mario"},
		{DBID: 3, SystemDBID: 1, Slug: "metroid", Name: "Metroid"},
	}, titles)

	none, err := db.GetTitlesByDBIDs(t.Context(), nil)
	require.NoError(t, err)
	assert.Empty(t, none)
}
