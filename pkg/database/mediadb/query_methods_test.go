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

package mediadb_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/scantest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMediaDB_Exists(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := helpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)

	assert.True(t, mediaDB.Exists(), "a MediaDB set up for testing must report itself as open")
}

func TestMediaDB_HasAnyMedia(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := helpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)

	has, err := mediaDB.HasAnyMedia()
	require.NoError(t, err)
	assert.False(t, has, "a freshly created database has no media")

	scantest.IndexMediaPaths(t, mediaDB, "SNES",
		filepath.Join(string(filepath.Separator), "roms", "SNES", "Game.sfc"))

	has, err = mediaDB.HasAnyMedia()
	require.NoError(t, err)
	assert.True(t, has)
}

func TestMediaDB_SystemIndexed(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := helpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)

	snes, err := systemdefs.GetSystem("SNES")
	require.NoError(t, err)

	assert.False(t, mediaDB.SystemIndexed(snes), "system has no rows yet")

	scantest.IndexMediaPaths(t, mediaDB, "SNES",
		filepath.Join(string(filepath.Separator), "roms", "SNES", "Game.sfc"))

	assert.True(t, mediaDB.SystemIndexed(snes))
}

func TestMediaDB_GetAllSystems(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := helpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)

	systems, err := mediaDB.GetAllSystems()
	require.NoError(t, err)
	assert.Empty(t, systems)

	scantest.IndexMediaPaths(t, mediaDB, "SNES",
		filepath.Join(string(filepath.Separator), "roms", "SNES", "Game.sfc"))

	systems, err = mediaDB.GetAllSystems()
	require.NoError(t, err)
	require.Len(t, systems, 1)
	assert.Equal(t, "SNES", systems[0].SystemID)
}

// TestMediaDB_SearchMediaPathGlob_MultiSystemVariants exercises the public
// wrapper's slug-variant generation (one variant per distinct MediaType
// across the queried systems, deduplicated) rather than the already-tested
// internal sqlSearchMediaPathParts it delegates to.
func TestMediaDB_SearchMediaPathGlob_MultiSystemVariants(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := helpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)

	gamePath := filepath.ToSlash(filepath.Join(
		string(filepath.Separator), "roms", "SNES", "Super Mario World.sfc",
	))
	scantest.IndexMediaPaths(t, mediaDB, "SNES", gamePath)

	snes, err := systemdefs.GetSystem("SNES")
	require.NoError(t, err)

	results, err := mediaDB.SearchMediaPathGlob([]systemdefs.System{*snes}, "*mario*")
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, gamePath, results[0].Path)
}

// TestMediaDB_SearchMediaPathGlob_NoGlobPartsReturnsRandomGame pins the
// wrapper's fallback branch: a query with no non-empty parts between "*"s
// (nothing to build a slug variant from) returns a single random game rather
// than an empty result or error.
func TestMediaDB_SearchMediaPathGlob_NoGlobPartsReturnsRandomGame(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := helpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)

	gamePath := filepath.ToSlash(filepath.Join(string(filepath.Separator), "roms", "SNES", "Only Game.sfc"))
	scantest.IndexMediaPaths(t, mediaDB, "SNES", gamePath)

	snes, err := systemdefs.GetSystem("SNES")
	require.NoError(t, err)

	results, err := mediaDB.SearchMediaPathGlob([]systemdefs.System{*snes}, "*")
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, gamePath, results[0].Path)
}

// TestMediaDB_BrowseDirCount_FromMediaFallback exercises the public wrapper
// on a fresh database, where the browse cache is never populated so it must
// fall back to computing the count directly from Media.
func TestMediaDB_BrowseDirCount_FromMediaFallback(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := helpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)

	scantest.IndexMediaPaths(t, mediaDB, "SNES", filepath.ToSlash(filepath.Join(
		string(filepath.Separator), "roms", "SNES", "USA", "Game.sfc",
	)))

	systemDir := filepath.ToSlash(filepath.Join(string(filepath.Separator), "roms", "SNES")) + "/"
	count, err := mediaDB.BrowseDirCount(context.Background(), database.BrowseDirCountOptions{
		PathPrefix: systemDir,
	})
	require.NoError(t, err)
	assert.Equal(t, 1, count, "the SNES directory contains one subdirectory (USA)")
}

func TestMediaDB_SearchSkipTagsThenAttach(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mediaDB, cleanup := helpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)

	root := filepath.Join(string(filepath.Separator), "roms", "NES")
	scantest.IndexMediaPaths(t, mediaDB, "NES",
		filepath.Join(root, "Mario (USA).nes"),
		filepath.Join(root, "Mario (Japan).nes"),
		filepath.Join(root, "Zelda (Europe).nes"))
	nes, err := systemdefs.GetSystem("NES")
	require.NoError(t, err)

	for _, query := range []string{"", "mario"} {
		filters := database.SearchFilters{
			Systems: []systemdefs.System{*nes}, Query: query, Sort: "name-asc", Limit: 10,
		}
		want, err := mediaDB.SearchMediaWithFilters(ctx, &filters)
		require.NoError(t, err)
		require.NotEmpty(t, want)

		filters.SkipTags = true
		bare, err := mediaDB.SearchMediaWithFilters(ctx, &filters)
		require.NoError(t, err)
		require.Len(t, bare, len(want))
		for i := range bare {
			assert.Equal(t, want[i].MediaID, bare[i].MediaID, "query %q", query)
			assert.Empty(t, bare[i].Tags, "query %q reads no tags", query)
		}

		require.NoError(t, mediaDB.AttachSearchTags(ctx, bare))
		assert.Equal(t, want, bare, "query %q: attached tags match a search's own", query)
	}
	require.NoError(t, mediaDB.AttachSearchTags(ctx, nil))
}
