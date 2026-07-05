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

package mediascanner

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/browseprefix"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediascanner/testdata"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSelectiveIndexingPreservesTagTypes verifies that truncating and
// re-indexing one system leaves the shared tag vocabulary untouched.
func TestSelectiveIndexingPreservesTagTypes(t *testing.T) {
	ctx := context.Background()
	mediaDB, cleanup := testhelpers.NewInMemoryMediaDB(t)
	defer cleanup()

	testSystems := []string{"NES", "SNES", "Amiga"}
	batch := testdata.CreateReproducibleBatch(testSystems, 3)

	for _, systemID := range testSystems {
		paths := make([]string, 0, len(batch.Entries[systemID]))
		for _, entry := range batch.Entries[systemID] {
			paths = append(paths, entry.Path)
		}
		indexMediaPaths(t, mediaDB, systemID, paths...)
	}

	countTagTypes := func() int {
		var n int
		require.NoError(t, mediaDB.UnsafeGetSQLDb().
			QueryRowContext(ctx, "SELECT COUNT(*) FROM TagTypes").Scan(&n))
		return n
	}
	initialTagTypeCount := countTagTypes()
	require.Positive(t, initialTagTypeCount, "Should have TagTypes after full index")

	// Selective reindex: truncate Amiga, then re-index it from scratch.
	require.NoError(t, mediaDB.TruncateSystems([]string{"Amiga"}))
	assert.Equal(t, initialTagTypeCount, countTagTypes(),
		"TagTypes should be preserved during selective truncation")

	amigaPaths := make([]string, 0, len(batch.Entries["Amiga"]))
	for _, entry := range batch.Entries["Amiga"] {
		amigaPaths = append(amigaPaths, entry.Path)
	}
	indexMediaPaths(t, mediaDB, "Amiga", amigaPaths...)

	assert.Equal(t, initialTagTypeCount, countTagTypes(),
		"TagTypes should remain unchanged after selective reindex")

	allSystems, err := mediaDB.GetAllSystems()
	require.NoError(t, err)
	assert.Len(t, allSystems, 3, "Should still have all 3 systems")
}

// TestReindexSameSystemTwice tests that reindexing the same system multiple
// times works correctly without crashes, duplicates, or tag corruption.
func TestReindexSameSystemTwice(t *testing.T) {
	ctx := context.Background()
	mediaDB, cleanup := testhelpers.NewInMemoryMediaDB(t)
	defer cleanup()

	batch := testdata.CreateReproducibleBatch([]string{"Amiga"}, 5)
	amigaPaths := make([]string, 0, 5)
	for _, entry := range batch.Entries["Amiga"] {
		amigaPaths = append(amigaPaths, entry.Path)
	}

	indexAmiga := func() {
		indexMediaPaths(t, mediaDB, "Amiga", amigaPaths...)
	}

	indexAmiga()
	var mediaCount int
	require.NoError(t, mediaDB.UnsafeGetSQLDb().
		QueryRowContext(ctx, "SELECT COUNT(*) FROM Media").Scan(&mediaCount))
	assert.Equal(t, 5, mediaCount, "Should have 5 media entries")

	for range 2 {
		require.NoError(t, mediaDB.TruncateSystems([]string{"Amiga"}))
		indexAmiga()
	}

	allSystems, err := mediaDB.GetAllSystems()
	require.NoError(t, err)
	assert.Len(t, allSystems, 1, "Should have 1 system (Amiga)")

	require.NoError(t, mediaDB.UnsafeGetSQLDb().
		QueryRowContext(ctx, "SELECT COUNT(*) FROM Media").Scan(&mediaCount))
	assert.Equal(t, 5, mediaCount, "re-indexing must not duplicate media rows")

	// TagTypes must not be duplicated across seed + reindex cycles.
	rows, err := mediaDB.UnsafeGetSQLDb().QueryContext(ctx,
		"SELECT Type, COUNT(*) FROM TagTypes GROUP BY Type HAVING COUNT(*) > 1")
	require.NoError(t, err)
	defer func() { require.NoError(t, rows.Close()) }()
	for rows.Next() {
		var typeName string
		var n int
		require.NoError(t, rows.Scan(&typeName, &n))
		t.Errorf("TagType %s duplicated %d times", typeName, n)
	}
	require.NoError(t, rows.Err())
}

// TestAutomaticNumberStrippingDetection tests the automatic detection of numbered playlists.
// This tests the threshold-based heuristic that analyzes directories to determine if leading
// numbers should be stripped (e.g., "01. ", "02 - ") based on how many files match the pattern.
func TestAutomaticNumberStrippingDetection(t *testing.T) {
	tests := []struct {
		name              string
		description       string
		files             []platforms.ScanResult
		expectedDetection bool
	}{
		{
			name: "numbered playlist - exceeds threshold",
			files: []platforms.ScanResult{
				{Path: "/roms/nes/favorites/01. Super Mario Bros.nes"},
				{Path: "/roms/nes/favorites/02. Zelda.nes"},
				{Path: "/roms/nes/favorites/03. Metroid.nes"},
				{Path: "/roms/nes/favorites/04. Mega Man.nes"},
				{Path: "/roms/nes/favorites/05. Castlevania.nes"},
			},
			expectedDetection: true,
			description:       "5/5 files match (100% > 50% threshold, ≥5 files)",
		},
		{
			name: "numbered playlist with dash separator",
			files: []platforms.ScanResult{
				{Path: "/roms/snes/01 - Super Mario World.snes"},
				{Path: "/roms/snes/02 - Zelda ALTTP.snes"},
				{Path: "/roms/snes/03 - Super Metroid.snes"},
				{Path: "/roms/snes/04 - Chrono Trigger.snes"},
				{Path: "/roms/snes/05 - Final Fantasy VI.snes"},
				{Path: "/roms/snes/06 - Earthbound.snes"},
			},
			expectedDetection: true,
			description:       "6/6 files match (100% > 50% threshold, ≥5 files)",
		},
		{
			name: "mixed - just over threshold",
			files: []platforms.ScanResult{
				{Path: "/roms/nes/01. Game One.nes"},
				{Path: "/roms/nes/02. Game Two.nes"},
				{Path: "/roms/nes/03. Game Three.nes"},
				{Path: "/roms/nes/1942.nes"},          // Legitimate game name
				{Path: "/roms/nes/Contra.nes"},        // Regular name
				{Path: "/roms/nes/04. Game Four.nes"}, // Added 4th numbered to tip over 50%
			},
			expectedDetection: true,
			description:       "4/6 files match (67% > 50% threshold, ≥5 files)",
		},
		{
			name: "mixed - below threshold",
			files: []platforms.ScanResult{
				{Path: "/roms/nes/01. Game One.nes"},
				{Path: "/roms/nes/02. Game Two.nes"},
				{Path: "/roms/nes/1942.nes"},
				{Path: "/roms/nes/Contra.nes"},
				{Path: "/roms/nes/Castlevania.nes"},
				{Path: "/roms/nes/Metroid.nes"},
			},
			expectedDetection: false,
			description:       "2/6 files match (33% < 50% threshold)",
		},
		{
			name: "no numbered files",
			files: []platforms.ScanResult{
				{Path: "/roms/nes/Super Mario Bros.nes"},
				{Path: "/roms/nes/Zelda.nes"},
				{Path: "/roms/nes/Metroid.nes"},
				{Path: "/roms/nes/Mega Man.nes"},
				{Path: "/roms/nes/Castlevania.nes"},
			},
			expectedDetection: false,
			description:       "0/5 files match (0% < 50% threshold)",
		},
		{
			name: "too few files - should not detect",
			files: []platforms.ScanResult{
				{Path: "/roms/nes/01. Game.nes"},
				{Path: "/roms/nes/02. Game.nes"},
				{Path: "/roms/nes/03. Game.nes"},
				{Path: "/roms/nes/04. Game.nes"},
			},
			expectedDetection: false,
			description:       "4/4 files match but <5 files (below minFiles threshold)",
		},
		{
			name: "legitimate number games",
			files: []platforms.ScanResult{
				{Path: "/roms/nes/1942.nes"},
				{Path: "/roms/nes/007.nes"},
				{Path: "/roms/nes/3D Worldrunner.nes"},
				{Path: "/roms/nes/720 Degrees.nes"},
				{Path: "/roms/nes/8 Eyes.nes"},
			},
			expectedDetection: false,
			description:       "filename extensions are ignored before prefix detection",
		},
		{
			name: "exactly at threshold boundary",
			files: []platforms.ScanResult{
				{Path: "/roms/nes/01. Game.nes"},
				{Path: "/roms/nes/02. Game.nes"},
				{Path: "/roms/nes/03. Game.nes"},
				{Path: "/roms/nes/Contra.nes"},
				{Path: "/roms/nes/Metroid.nes"},
				{Path: "/roms/nes/Castlevania.nes"},
			},
			expectedDetection: false,
			description:       "3/6 files match (50% = 50%, NOT > 50%, so returns false)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test with threshold=0.5, minFiles=5 (production values)
			result := detectNumberingPattern(tt.files, 0.5, 5)
			assert.Equal(t, tt.expectedDetection, result,
				"Detection failed: %s", tt.description)
		})
	}
}

// TestSlugGenerationPipeline tests the complete slug generation from file path to database.
// This integration test ensures that the context-aware leading number stripping works correctly
// throughout the entire indexing pipeline (file path → parsed title → slug → database storage).
func TestGetPathFragments_DatePrefixPolicy(t *testing.T) {
	t.Parallel()

	fragments := GetPathFragments(&PathFragmentParams{
		Path:         filepath.Join("roms", "genesis", "history", "1991-06-23 - Sonic the Hedgehog (USA).gen"),
		SystemID:     "Genesis",
		PrefixPolicy: browseprefix.Policy{Kind: browseprefix.KindDate, Enabled: true},
	})

	assert.Equal(t, "Sonic the Hedgehog", fragments.Title)
	assert.Equal(t, "sonicthehedgehog", fragments.Slug)
}

// TestSlugGenerationPipeline verifies title and slug derivation end-to-end
// through the staging pipeline for representative filename shapes.
func TestSlugGenerationPipeline(t *testing.T) {
	tests := []struct {
		name                string
		systemID            string
		path                string
		expectedTitle       string
		expectedSlug        string
		stripLeadingNumbers bool
	}{
		{
			name:                "disc metadata stripped",
			systemID:            "PS1",
			path:                filepath.Join("roms", "ps1", "Final Fantasy VII (USA) (Disc 1).bin"),
			stripLeadingNumbers: false,
			expectedTitle:       "Final Fantasy VII",
			expectedSlug:        "finalfantasy7",
		},
		{
			name:                "trailing article format - preserved in title, stripped from slug",
			systemID:            "NES",
			path:                filepath.Join("roms", "nes", "Legend of Zelda, The (USA).nes"),
			stripLeadingNumbers: false,
			expectedTitle:       "Legend of Zelda, The",
			expectedSlug:        "legendofzelda",
		},
		{
			name:                "subtitle with colon",
			systemID:            "NES",
			path:                filepath.Join("roms", "nes", "Zelda: Link's Awakening (USA).nes"),
			stripLeadingNumbers: false,
			expectedTitle:       "Zelda: Link's Awakening",
			expectedSlug:        "zeldalinksawakening",
		},
		{
			name:                "subtitle with dash - preserved as-is",
			systemID:            "NES",
			path:                filepath.Join("roms", "nes", "Zelda - Link's Awakening (USA).nes"),
			stripLeadingNumbers: false,
			expectedTitle:       "Zelda - Link's Awakening",
			expectedSlug:        "zeldalinksawakening",
		},
		{
			name:                "multiple digit prefix stripped",
			systemID:            "NES",
			path:                filepath.Join("roms", "nes", "123. Game Title (USA).nes"),
			stripLeadingNumbers: true,
			expectedTitle:       "Game Title",
			expectedSlug:        "gametitle",
		},
		{
			name:                "zero-padded number stripped",
			systemID:            "SNES",
			path:                filepath.Join("roms", "snes", "003 - Chrono Trigger (USA).snes"),
			stripLeadingNumbers: true,
			expectedTitle:       "Chrono Trigger",
			expectedSlug:        "chronotrigger",
		},
	}

	ctx := context.Background()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mediaDB, cleanup := testhelpers.NewInMemoryMediaDB(t)
			defer cleanup()

			policy := browseprefix.Policy{}
			if tt.stripLeadingNumbers {
				policy = browseprefix.Policy{Kind: browseprefix.KindRank, Enabled: true}
			}

			require.NoError(t, SeedCanonicalTags(ctx, mediaDB))
			require.NoError(t, mediaDB.BeginTransaction(true))
			require.NoError(t, mediaDB.ClearScanStage())
			require.NoError(t, StageMediaPath(&StageMediaPathParams{
				DB:           mediaDB,
				SystemID:     tt.systemID,
				Path:         tt.path,
				PrefixPolicy: policy,
			}))
			_, err := mediaDB.ReconcileStagedSystem(ctx, tt.systemID, database.ScanReconcileOpts{})
			require.NoError(t, err)
			require.NoError(t, mediaDB.CommitTransaction())

			titles, err := mediaDB.GetTitlesBySystemID(tt.systemID)
			require.NoError(t, err)
			require.Len(t, titles, 1)
			assert.Equal(t, tt.expectedTitle, titles[0].Name, "Title name mismatch")
			assert.Equal(t, tt.expectedSlug, titles[0].Slug, "Slug mismatch")

			media, err := mediaDB.GetMediaBySystemID(tt.systemID)
			require.NoError(t, err)
			require.Len(t, media, 1)
			assert.Equal(t, titles[0].DBID, media[0].MediaTitleDBID, "Media should point to correct title")
			assert.Equal(t, tt.path, media[0].Path, "Media path should match")
		})
	}
}
