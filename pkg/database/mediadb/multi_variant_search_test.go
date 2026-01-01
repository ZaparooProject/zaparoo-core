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

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	testsqlmock "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSearchMediaWithFilters_MultipleSameMediaTypeSystems verifies that when searching
// across multiple systems with the SAME MediaType (e.g., NES + SNES both are Game),
// we properly deduplicate slug variants and don't generate duplicate SQL parameters.
//
// This test would have caught the deduplication bug where:
// if slugVariant != "" && seenVariants[slugVariant] == struct{}{} // Always true!
func TestSearchMediaWithFilters_MultipleSameMediaTypeSystems(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	// Two systems with SAME MediaType (both Game)
	nes, err := systemdefs.GetSystem("NES")
	require.NoError(t, err)
	snes, err := systemdefs.GetSystem("SNES")
	require.NoError(t, err)

	systems := []systemdefs.System{*nes, *snes}
	variantGroups := [][]string{{"mario"}} // Deduplication should result in single "mario"
	rawWords := []string{"mario"}

	// Expected SQL args with deduplication:
	// - "nes", "snes" (system IDs)
	// - "%mario%", "%mario%" (Slug LIKE, SecondarySlug LIKE) - ONE variant, not duplicated
	// - 10 (limit)
	// Total: 5 args
	//
	// WITHOUT deduplication (the bug):
	// - "nes", "snes"
	// - "%mario%", "%mario%" (first mario from NES)
	// - "%mario%", "%mario%" (duplicate mario from SNES)
	// - 10
	// Total: 7 args (bloated with duplicates)

	mock.ExpectPrepare("SELECT.*Systems\\.SystemID.*MediaTitles\\.Name.*Media\\.Path.*Media\\.DBID.*").
		ExpectQuery().
		WithArgs("NES", "SNES", "%mario%", "%mario%", 10). // Should be 5 args, not 7
		WillReturnRows(sqlmock.NewRows([]string{"SystemID", "Name", "Path", "DBID"}).
			AddRow("NES", "Super Mario Bros", "/games/mario.nes", int64(1)))

	// Mock tags query
	mock.ExpectPrepare("SELECT DISTINCT.*FROM Media.*WHERE Media.DBID IN").
		ExpectQuery().
		WithArgs(int64(1)).
		WillReturnRows(sqlmock.NewRows([]string{"MediaDBID", "Tag", "Type"}))

	results, err := sqlSearchMediaWithFilters(
		context.Background(), db, systems, variantGroups, rawWords, nil, nil, nil, 10, false,
	)

	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "Super Mario Bros", results[0].Name)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestSearchMediaWithFilters_DifferentMediaTypes verifies that when searching
// across systems with DIFFERENT MediaTypes, we generate separate slug variants
// for each type and include them all in the SQL query.
//
// Example: Game system vs TV system may parse titles differently
func TestSearchMediaWithFilters_DifferentMediaTypes(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	// Two systems with DIFFERENT MediaTypes
	ps2, err := systemdefs.GetSystem("PS2") // Game
	require.NoError(t, err)
	tvEpisode, err := systemdefs.GetSystem("TVEpisode") // TVShow
	require.NoError(t, err)

	systems := []systemdefs.System{*ps2, *tvEpisode}

	// For a TV show title like "Lost S01E05", different MediaTypes may produce different slugs
	// Game: "losts01e05" (basic normalization)
	// TVShow: "losts01e05" (normalized season/episode format)
	// In this case they're the same, so deduplication should work
	variantGroups := [][]string{{"losts01e05"}}
	rawWords := []string{"lost s01e05"}

	// Expected: deduplication means only ONE "losts01e05" variant
	// Args: PS2, TVEpisode, %losts01e05%, %losts01e05%, 10
	mock.ExpectPrepare("SELECT.*Systems\\.SystemID.*MediaTitles\\.Name.*Media\\.Path.*Media\\.DBID.*").
		ExpectQuery().
		WithArgs("PS2", "TVEpisode", "%losts01e05%", "%losts01e05%", 10).
		WillReturnRows(sqlmock.NewRows([]string{"SystemID", "Name", "Path", "DBID"}))

	// No tags query mock needed - no results means fetchAndAttachTags returns early

	results, err := sqlSearchMediaWithFilters(
		context.Background(), db, systems, variantGroups, rawWords, nil, nil, nil, 10, false,
	)

	require.NoError(t, err)
	assert.Empty(t, results)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestSearchMediaWithFilters_MultipleWords verifies deduplication works correctly
// when searching with multiple words across multiple same-type systems.
func TestSearchMediaWithFilters_MultipleWordsMultipleSystems(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	// Three game systems
	nes, err := systemdefs.GetSystem("NES")
	require.NoError(t, err)
	snes, err := systemdefs.GetSystem("SNES")
	require.NoError(t, err)
	genesis, err := systemdefs.GetSystem("Genesis")
	require.NoError(t, err)

	systems := []systemdefs.System{*nes, *snes, *genesis}

	// Two words, each should be deduplicated across the three systems
	variantGroups := [][]string{
		{"super"},
		{"mario"},
	}
	rawWords := []string{"super", "mario"}

	// Expected args (with deduplication):
	// System IDs: nes, snes, genesis (3)
	// Word 1 variants: %super%, %super% (Slug + SecondarySlug) (2)
	// Word 2 variants: %mario%, %mario% (Slug + SecondarySlug) (2)
	// Limit: 10 (1)
	// Total: 8 args
	//
	// WITHOUT deduplication (the bug):
	// System IDs: 3
	// Word 1: %super% x6 (3 systems x 2 LIKE clauses each) = 6
	// Word 2: %mario% x6 = 6
	// Limit: 1
	// Total: 16 args (bloated!)

	mock.ExpectPrepare("SELECT.*Systems\\.SystemID.*MediaTitles\\.Name.*Media\\.Path.*Media\\.DBID.*").
		ExpectQuery().
		WithArgs(
			"NES", "SNES", "Genesis", // System IDs
			"%super%", "%super%", // Word 1: Slug LIKE, SecondarySlug LIKE
			"%mario%", "%mario%", // Word 2: Slug LIKE, SecondarySlug LIKE
			10, // Limit
		). // Should be 8 args, not 16
		WillReturnRows(sqlmock.NewRows([]string{"SystemID", "Name", "Path", "DBID"}).
			AddRow("SNES", "Super Mario World", "/games/smw.sfc", int64(1)))

	// Mock tags query
	mock.ExpectPrepare("SELECT DISTINCT.*FROM Media.*WHERE Media.DBID IN").
		ExpectQuery().
		WithArgs(int64(1)).
		WillReturnRows(sqlmock.NewRows([]string{"MediaDBID", "Tag", "Type"}))

	results, err := sqlSearchMediaWithFilters(
		context.Background(), db, systems, variantGroups, rawWords, nil, nil, nil, 10, false,
	)

	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "Super Mario World", results[0].Name)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestSearchMediaPathGlob_Deduplication verifies that PathGlob searches
// properly deduplicate slug variants when multiple systems have the same MediaType.
func TestSearchMediaPathGlob_Deduplication(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	// Multiple game systems
	nes, err := systemdefs.GetSystem("NES")
	require.NoError(t, err)
	snes, err := systemdefs.GetSystem("SNES")
	require.NoError(t, err)

	systems := []systemdefs.System{*nes, *snes}

	// PathGlob pattern: "*mario*bros*"
	// Should generate variants for "mario" and "bros"
	variantGroups := [][]string{
		{"mario"},
		{"bros"},
	}

	// Expected SQL with deduplication:
	// System IDs: NES, SNES (2)
	// Part 1 (mario): %mario%, %mario% (Slug + SecondarySlug) (2)
	// Part 2 (bros): %bros%, %bros% (Slug + SecondarySlug) (2)
	// Total: 6 args
	// Note: sqlSearchMediaPathParts returns different columns than sqlSearchMediaWithFilters

	mock.ExpectPrepare("select Systems.SystemID, Media.Path from Systems.*").
		ExpectQuery().
		WithArgs(
			"NES", "SNES", // System IDs
			"%mario%", "%mario%", // Part 1: Slug LIKE, SecondarySlug LIKE
			"%bros%", "%bros%", // Part 2: Slug LIKE, SecondarySlug LIKE
		).
		WillReturnRows(sqlmock.NewRows([]string{"SystemID", "Path"}).
			AddRow("NES", "/games/mario.nes"))

	// sqlSearchMediaPathParts doesn't fetch tags, returns different struct
	results, err := sqlSearchMediaPathParts(
		context.Background(), db, systems, variantGroups,
	)

	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "/games/mario.nes", results[0].Path)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestBuildMediaQueryWhereClause_PathGlobDeduplication verifies that
// buildMediaQueryWhereClause properly deduplicates variants when building
// WHERE clauses for PathGlob queries (used by RandomGameWithQuery).
func TestBuildMediaQueryWhereClause_PathGlobDeduplication(t *testing.T) {
	t.Parallel()

	// Create a MediaQuery with multiple same-type systems and PathGlob
	_, err := systemdefs.GetSystem("NES")
	require.NoError(t, err)
	_, err = systemdefs.GetSystem("SNES")
	require.NoError(t, err)

	query := &database.MediaQuery{
		Systems:  []string{"NES", "SNES"},
		PathGlob: "*mario*",
	}

	// Build the WHERE clause
	whereClause, args := buildMediaQueryWhereClause(query)

	// Verify WHERE clause is not empty
	require.NotEmpty(t, whereClause)
	require.Contains(t, whereClause, "WHERE")

	// Verify we have the right number of args:
	// - Systems IN: NES, SNES (2)
	// - PathGlob "mario": %mario%, %mario% (Slug + SecondarySlug) (2)
	// Total: 4 args
	//
	// WITHOUT deduplication (the bug):
	// - Systems: 2
	// - mario x4 (2 systems x 2 LIKE clauses) = 4
	// Total: 6 args (bloated)

	assert.Len(t, args, 4, "Should have 4 args with deduplication (not 6 without)")

	// Verify args contain expected values
	assert.Equal(t, "NES", args[0])
	assert.Equal(t, "SNES", args[1])
	assert.Equal(t, "%mario%", args[2])
	assert.Equal(t, "%mario%", args[3])

	// Verify WHERE clause structure contains System filter and PathGlob conditions
	assert.Contains(t, whereClause, "Systems.SystemID IN")
	assert.Contains(t, whereClause, "MediaTitles.Slug LIKE ?")
	assert.Contains(t, whereClause, "MediaTitles.SecondarySlug LIKE ?")
}

// TestBuildMediaQueryWhereClause_ComplexPathGlob tests deduplication
// with a more complex PathGlob pattern containing multiple parts.
func TestBuildMediaQueryWhereClause_ComplexPathGlob(t *testing.T) {
	t.Parallel()

	// Three game systems
	_, err := systemdefs.GetSystem("NES")
	require.NoError(t, err)
	_, err = systemdefs.GetSystem("SNES")
	require.NoError(t, err)
	_, err = systemdefs.GetSystem("Genesis")
	require.NoError(t, err)

	query := &database.MediaQuery{
		Systems:  []string{"NES", "SNES", "Genesis"},
		PathGlob: "*super*mario*world*",
	}

	whereClause, args := buildMediaQueryWhereClause(query)

	require.NotEmpty(t, whereClause)

	// Expected args with deduplication:
	// - Systems: NES, SNES, Genesis (3)
	// - Part "super": %super%, %super% (2)
	// - Part "mario": %mario%, %mario% (2)
	// - Part "world": %world%, %world% (2)
	// Total: 9 args

	assert.Len(t, args, 9, "Should have 9 args with proper deduplication")

	// Verify system IDs
	assert.Equal(t, "NES", args[0])
	assert.Equal(t, "SNES", args[1])
	assert.Equal(t, "Genesis", args[2])

	// Verify glob parts (order matters for AND logic)
	assert.Equal(t, "%super%", args[3])
	assert.Equal(t, "%super%", args[4])
	assert.Equal(t, "%mario%", args[5])
	assert.Equal(t, "%mario%", args[6])
	assert.Equal(t, "%world%", args[7])
	assert.Equal(t, "%world%", args[8])

	// Verify WHERE clause has proper structure with AND groups
	assert.Contains(t, whereClause, "WHERE")
	assert.Contains(t, whereClause, "Systems.SystemID IN")
}
