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
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMediaRecursivePathPrefixNormalizesNativeSeparators(t *testing.T) {
	t.Parallel()

	path := filepath.Join("roms", "SNES")
	assert.Equal(t, filepath.ToSlash(path)+"/", mediaRecursivePathPrefix(path))
}

func TestSearchMediaWithFilters_PathPrefixIsRecursiveAndBoundarySafe(t *testing.T) {
	t.Parallel()

	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()
	ctx := context.Background()

	root := filepath.ToSlash(filepath.Join("roms", "SNES"))
	insidePath := filepath.ToSlash(filepath.Join(root, "inside.sfc"))
	nestedPath := filepath.ToSlash(filepath.Join(root, "nested", "inside-too.sfc"))
	siblingPath := filepath.ToSlash(filepath.Join("roms", "SNES2", "outside.sfc"))
	literalRoot := filepath.ToSlash(filepath.Join("roms", "100%_Games"))
	literalPath := filepath.ToSlash(filepath.Join(literalRoot, "literal.sfc"))
	wildcardLookalikePath := filepath.ToSlash(filepath.Join("roms", "100XXGames", "outside.sfc"))

	system := insertSystemWithMedia(t, mediaDB, "SNES", "Inside", insidePath)
	insertSystemMedia(t, mediaDB, system, "Nested", nestedPath)
	insertSystemMedia(t, mediaDB, system, "Sibling", siblingPath)
	insertSystemMedia(t, mediaDB, system, "Literal", literalPath)
	insertSystemMedia(t, mediaDB, system, "Wildcard Lookalike", wildcardLookalikePath)
	insertSystemMedia(t, mediaDB, system, "Steam Inside", "steam://44/inside")
	insertSystemMedia(t, mediaDB, system, "Steam Sibling", "steam://440/outside")

	snes, err := systemdefs.GetSystem(systemdefs.SystemSNES)
	require.NoError(t, err)
	search := func(pathPrefix string) []database.SearchResultWithCursor {
		results, searchErr := mediaDB.SearchMediaWithFilters(ctx, &database.SearchFilters{
			Systems:    []systemdefs.System{*snes},
			PathPrefix: pathPrefix,
			Sort:       "filename-asc",
			Limit:      10,
		})
		require.NoError(t, searchErr)
		return results
	}

	results := search(root)
	require.Len(t, results, 2)
	assert.Equal(t, []string{insidePath, nestedPath}, []string{results[0].Path, results[1].Path})

	results = search(literalRoot)
	require.Len(t, results, 1)
	assert.Equal(t, literalPath, results[0].Path)

	results = search("steam://44")
	require.Len(t, results, 1)
	assert.Equal(t, "steam://44/inside", results[0].Path)
}

func TestSearchMediaWithFilters_PathPrefixIncludesIndexedVirtualSystems(t *testing.T) {
	t.Parallel()

	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()
	ctx := context.Background()

	virtualSystem, err := mediaDB.FindOrInsertSystem(database.System{
		SystemID: "virtual-system",
		Name:     "Virtual System",
	})
	require.NoError(t, err)
	require.NoError(t, mediaDB.BeginTransaction(false))
	title, err := mediaDB.InsertMediaTitle(&database.MediaTitle{
		SystemDBID: virtualSystem.DBID,
		Slug:       "virtualgame",
		Name:       "Virtual Game",
	})
	require.NoError(t, err)
	virtualPath := "zaparoo://virtual-system/virtual-game"
	_, err = mediaDB.InsertMedia(database.Media{
		SystemDBID:     virtualSystem.DBID,
		MediaTitleDBID: title.DBID,
		Path:           virtualPath,
		ParentDir:      "zaparoo://",
		SortName:       "Virtual Game",
	})
	require.NoError(t, err)
	require.NoError(t, mediaDB.CommitTransaction())

	results, err := mediaDB.SearchMediaWithFilters(ctx, &database.SearchFilters{
		Systems:    systemdefs.AllSystems(),
		PathPrefix: "zaparoo://virtual-system",
		Query:      "Virtual Game",
		Sort:       "name-asc",
		Limit:      10,
	})
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "virtual-system", results[0].SystemID)
	assert.Equal(t, virtualPath, results[0].Path)
}

func TestSearchMediaWithFilters_VirtualPathKeepsMediaTypeVariantsScoped(t *testing.T) {
	t.Parallel()

	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()
	ctx := context.Background()

	nesSystem := insertSystemWithMedia(t, mediaDB, systemdefs.SystemNES, "Mario Kart", "steam://nes/mario-kart")
	insertSystemMedia(t, mediaDB, nesSystem, "R-Type", "steam://nes/r-type")
	insertSystemWithMedia(t, mediaDB, systemdefs.SystemMovie, "R-Type", "steam://movies/r-type")
	mediaDB.slugSearchCache.Store(nil)

	results, err := mediaDB.SearchMediaWithFilters(ctx, &database.SearchFilters{
		Systems:    systemdefs.AllSystems(),
		PathPrefix: "steam://",
		Query:      "R-Type",
		Sort:       "filename-asc",
		Limit:      10,
	})
	require.NoError(t, err)
	require.Len(t, results, 2)

	resultSystems := []string{results[0].SystemID, results[1].SystemID}
	assert.ElementsMatch(t, []string{systemdefs.SystemNES, systemdefs.SystemMovie}, resultSystems)
	for i := range results {
		assert.Equal(t, "R-Type", results[i].Name)
	}
}

func TestSearchMediaWithFilters_PathPrefixComposesAcrossCacheAndSQL(t *testing.T) {
	t.Parallel()

	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()
	ctx := context.Background()

	root := filepath.ToSlash(filepath.Join("roms", "SNES", "favorites"))
	alphaPath := filepath.ToSlash(filepath.Join(root, "target-alpha.sfc"))
	betaPath := filepath.ToSlash(filepath.Join(root, "nested", "target-beta.sfc"))
	notFavoritePath := filepath.ToSlash(filepath.Join(root, "target-other.sfc"))
	outsidePath := filepath.ToSlash(filepath.Join("roms", "SNES", "outside", "target-outside.sfc"))

	system := insertSystemWithMedia(t, mediaDB, "SNES", "Target Alpha", alphaPath)
	insertSystemMedia(t, mediaDB, system, "Target Beta", betaPath)
	insertSystemMedia(t, mediaDB, system, "Target Other", notFavoritePath)
	insertSystemMedia(t, mediaDB, system, "Target Outside", outsidePath)

	for _, mediaPath := range []string{alphaPath, betaPath, outsidePath} {
		media, err := mediaDB.FindMedia(database.Media{SystemDBID: system.DBID, Path: mediaPath})
		require.NoError(t, err)
		require.NoError(t, mediaDB.UpdateMediaTags(ctx, media.DBID, nil, []database.MediaTagRef{{
			Type: "user",
			Tag:  "favorite",
		}}))
	}
	require.NoError(t, mediaDB.RebuildSlugSearchCache())

	snes, err := systemdefs.GetSystem(systemdefs.SystemSNES)
	require.NoError(t, err)
	letter := "T"
	filters := database.SearchFilters{
		Systems:    []systemdefs.System{*snes},
		PathPrefix: root,
		Query:      "Target",
		Sort:       "name-asc",
		Tags: []zapscript.TagFilter{{
			Type:  "user",
			Value: "favorite",
		}},
		Letter: &letter,
		Limit:  1,
	}

	firstPage, err := mediaDB.SearchMediaWithFilters(ctx, &filters)
	require.NoError(t, err)
	require.Len(t, firstPage, 1)
	assert.Equal(t, "Target Alpha", firstPage[0].Name)

	filters.SortCursor = &database.SearchCursor{
		SortValue: firstPage[0].SortValue,
		Sort:      firstPage[0].SortMode,
		LastID:    firstPage[0].MediaID,
	}
	secondPage, err := mediaDB.SearchMediaWithFilters(ctx, &filters)
	require.NoError(t, err)
	require.Len(t, secondPage, 1)
	assert.Equal(t, "Target Beta", secondPage[0].Name)

	mediaDB.slugSearchCache.Store(nil)
	filters.SortCursor = nil
	sqlResults, err := mediaDB.SearchMediaWithFilters(ctx, &filters)
	require.NoError(t, err)
	require.Len(t, sqlResults, 1)
	assert.Equal(t, "Target Alpha", sqlResults[0].Name)
}
