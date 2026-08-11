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

func TestSearchMediaWithFilters_ExplicitSortPagination(t *testing.T) {
	t.Parallel()

	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()
	ctx := context.Background()

	system := insertSystemWithMedia(
		t, mediaDB, "NES", "charlie", filepath.Join("roms", "nes", "a-charlie.nes"))
	insertSystemMedia(t, mediaDB, system, "Alpha", filepath.Join("roms", "nes", "z-alpha.nes"))
	insertSystemMedia(t, mediaDB, system, "bravo", filepath.Join("roms", "nes", "m-bravo.nes"))
	insertSystemMedia(t, mediaDB, system, "alpha", filepath.Join("roms", "nes", "b-alpha-lower.nes"))

	nes, err := systemdefs.GetSystem("NES")
	require.NoError(t, err)
	firstPage, err := mediaDB.SearchMediaWithFilters(ctx, &database.SearchFilters{
		Systems: []systemdefs.System{*nes},
		Sort:    "name-asc",
		Limit:   2,
	})
	require.NoError(t, err)
	require.Len(t, firstPage, 2)
	assert.Equal(t, []string{"Alpha", "alpha"}, []string{firstPage[0].Name, firstPage[1].Name})
	assert.Equal(t, "Alpha", firstPage[0].SortValue)
	assert.Equal(t, "name-asc", firstPage[0].SortMode)

	secondPage, err := mediaDB.SearchMediaWithFilters(ctx, &database.SearchFilters{
		Systems: []systemdefs.System{*nes},
		Sort:    "name-asc",
		SortCursor: &database.SearchCursor{
			Sort:      "name-asc",
			SortValue: firstPage[1].SortValue,
			LastID:    firstPage[1].MediaID,
		},
		Limit: 2,
	})
	require.NoError(t, err)
	require.Len(t, secondPage, 2)
	assert.Equal(t, []string{"bravo", "charlie"}, []string{secondPage[0].Name, secondPage[1].Name})

	descending, err := mediaDB.SearchMediaWithFilters(ctx, &database.SearchFilters{
		Systems: []systemdefs.System{*nes},
		Sort:    "name-desc",
		Limit:   4,
	})
	require.NoError(t, err)
	require.Len(t, descending, 4)
	assert.Equal(t, []string{"charlie", "bravo", "alpha", "Alpha"}, []string{
		descending[0].Name,
		descending[1].Name,
		descending[2].Name,
		descending[3].Name,
	})

	byFilename, err := mediaDB.SearchMediaWithFilters(ctx, &database.SearchFilters{
		Systems: []systemdefs.System{*nes},
		Sort:    "filename-asc",
		Limit:   4,
	})
	require.NoError(t, err)
	require.Len(t, byFilename, 4)
	assert.Equal(t, []string{"charlie", "alpha", "bravo", "Alpha"}, []string{
		byFilename[0].Name,
		byFilename[1].Name,
		byFilename[2].Name,
		byFilename[3].Name,
	})
}

func TestSearchMediaWithFilters_CombinesTagLetterAndSort(t *testing.T) {
	t.Parallel()

	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()
	ctx := context.Background()

	bravoPath := filepath.Join("roms", "nes", "bravo.nes")
	system := insertSystemWithMedia(t, mediaDB, "NES", "Bravo", bravoPath)
	insertSystemMedia(t, mediaDB, system, "Beta", filepath.Join("roms", "nes", "beta.nes"))
	insertSystemMedia(t, mediaDB, system, "Alpha", filepath.Join("roms", "nes", "alpha.nes"))
	bravo, err := mediaDB.FindMedia(database.Media{SystemDBID: system.DBID, Path: bravoPath})
	require.NoError(t, err)
	require.NoError(t, mediaDB.UpdateMediaTags(ctx, bravo.DBID, nil, []database.MediaTagRef{{
		Type: "user",
		Tag:  "favorite",
	}}))

	nes, err := systemdefs.GetSystem("NES")
	require.NoError(t, err)
	letter := "B"
	results, err := mediaDB.SearchMediaWithFilters(ctx, &database.SearchFilters{
		Systems: []systemdefs.System{*nes},
		Tags:    []zapscript.TagFilter{{Type: "user", Value: "favorite"}},
		Letter:  &letter,
		Sort:    "name-asc",
		Limit:   10,
	})
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "Bravo", results[0].Name)
}
