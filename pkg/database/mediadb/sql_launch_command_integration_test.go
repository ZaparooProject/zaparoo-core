// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
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

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/slugs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMediaDB_GetLaunchCommandForMedia_Integration tests the launch command generation
// against a real SQLite database to catch schema-related issues that mocks cannot detect.
func TestMediaDB_GetLaunchCommandForMedia_Integration(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	ctx := context.Background()

	// Create tag type before transaction
	yearTagType, err := mediaDB.FindOrInsertTagType(database.TagType{Type: "year"})
	require.NoError(t, err)

	// Begin transaction for test data
	err = mediaDB.BeginTransaction(false)
	require.NoError(t, err)

	nesSystem, err := systemdefs.GetSystem("NES")
	require.NoError(t, err)

	// Insert System
	system := database.System{
		SystemID: nesSystem.ID,
		Name:     "Nintendo Entertainment System",
	}
	insertedSystem, err := mediaDB.InsertSystem(system)
	require.NoError(t, err)

	// Insert MediaTitle with year tag
	marioTitle := database.MediaTitle{
		SystemDBID: insertedSystem.DBID,
		Slug:       slugs.SlugifyString("Super Mario Bros."),
		Name:       "Super Mario Bros.",
	}
	insertedMarioTitle, err := mediaDB.InsertMediaTitle(marioTitle)
	require.NoError(t, err)

	// Insert Media
	marioMedia := database.Media{
		MediaTitleDBID: insertedMarioTitle.DBID,
		SystemDBID:     insertedSystem.DBID,
		Path:           "/games/super-mario-bros.nes",
	}
	insertedMarioMedia, err := mediaDB.InsertMedia(marioMedia)
	require.NoError(t, err)

	// Insert year tag
	yearTag := database.Tag{
		TypeDBID: yearTagType.DBID,
		Tag:      "1985",
	}
	insertedYearTag, err := mediaDB.FindOrInsertTag(yearTag)
	require.NoError(t, err)

	// Link Media to year Tag
	marioMediaTag := database.MediaTag{
		MediaDBID: insertedMarioMedia.DBID,
		TagDBID:   insertedYearTag.DBID,
	}
	_, err = mediaDB.InsertMediaTag(marioMediaTag)
	require.NoError(t, err)

	// Insert MediaTitle without year tag
	zeldaTitle := database.MediaTitle{
		SystemDBID: insertedSystem.DBID,
		Slug:       slugs.SlugifyString("The Legend of Zelda"),
		Name:       "The Legend of Zelda",
	}
	insertedZeldaTitle, err := mediaDB.InsertMediaTitle(zeldaTitle)
	require.NoError(t, err)

	zeldaMedia := database.Media{
		MediaTitleDBID: insertedZeldaTitle.DBID,
		SystemDBID:     insertedSystem.DBID,
		Path:           "/games/zelda.nes",
	}
	_, err = mediaDB.InsertMedia(zeldaMedia)
	require.NoError(t, err)

	err = mediaDB.CommitTransaction()
	require.NoError(t, err)

	t.Run("returns launch command with year tag", func(t *testing.T) {
		result, err := mediaDB.GetLaunchCommandForMedia(ctx, nesSystem.ID, "/games/super-mario-bros.nes")
		require.NoError(t, err)
		assert.Equal(t, "@NES/Super Mario Bros. (year:1985)", result)
	})

	t.Run("returns launch command without year for different media", func(t *testing.T) {
		result, err := mediaDB.GetLaunchCommandForMedia(ctx, nesSystem.ID, "/games/zelda.nes")
		require.NoError(t, err)
		assert.Equal(t, "@NES/The Legend of Zelda", result)
	})

	t.Run("returns empty string for non-existent media", func(t *testing.T) {
		result, err := mediaDB.GetLaunchCommandForMedia(ctx, nesSystem.ID, "/games/nonexistent.nes")
		require.NoError(t, err)
		assert.Empty(t, result)
	})
}
