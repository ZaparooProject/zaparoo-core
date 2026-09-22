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
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/scantest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The same file, named the way the indexer stores it and the way a Windows
// caller hands it over. The backslash form is written out rather than built
// with filepath.Join so this runs as a real regression everywhere, not only on
// the one OS where the separator happens to differ.
const (
	slashPath     = "roms/SNES/Super Mario World (USA).sfc"
	backslashPath = `roms\SNES\Super Mario World (USA).sfc`
	// A second file sharing the first's title slug, so the indexer has a
	// reason to record disambiguating tags on both. Without it the tag query
	// answers empty for every path and could not tell the two apart.
	hackSlashPath     = "roms/SNES/Super Mario World (USA) (Hack).sfc"
	hackBackslashPath = `roms\SNES\Super Mario World (USA) (Hack).sfc`
)

// TestExactPathQueriesAcceptNativeSeparators pins that a media path looks the
// same to every exact-path query however the caller spells it. Media.Path is
// stored canonically by the indexing pipeline, so a query that compared the
// raw argument found nothing on Windows, where paths arrive with backslashes —
// decks could not be composed and title resolution could not name a file.
func TestExactPathQueriesAcceptNativeSeparators(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	mediaDB, cleanup := testhelpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)
	scantest.IndexMediaPaths(t, mediaDB, "SNES", slashPath, hackSlashPath)

	system, err := systemdefs.LookupSystem("SNES")
	require.NoError(t, err)

	t.Run("SearchMediaPathExact", func(t *testing.T) {
		t.Parallel()
		for _, path := range []string{slashPath, backslashPath} {
			results, searchErr := mediaDB.SearchMediaPathExact(
				ctx, []systemdefs.System{*system}, path)
			require.NoError(t, searchErr, path)
			require.Len(t, results, 1, path)
			assert.Equal(t, slashPath, results[0].Path, path)
		}
	})

	t.Run("LookupMediaIdentity", func(t *testing.T) {
		t.Parallel()
		for _, path := range []string{slashPath, backslashPath} {
			identity, found, idErr := database.LookupMediaIdentity(ctx, mediaDB, "SNES", path)
			require.NoError(t, idErr, path)
			require.True(t, found, path)
			assert.Equal(t, "Super Mario World", identity.DisplayName, path)
		}
	})

	t.Run("GetZapScriptTagsBySystemAndPath", func(t *testing.T) {
		t.Parallel()
		// The hack variant is the one with a tag to disambiguate it from the
		// plain release; the plain release has nothing to distinguish.
		want, err := mediaDB.GetZapScriptTagsBySystemAndPath(ctx, "SNES", hackSlashPath)
		require.NoError(t, err)
		require.NotEmpty(t, want, "fixture must produce disambiguating tags or this proves nothing")
		got, err := mediaDB.GetZapScriptTagsBySystemAndPath(ctx, "SNES", hackBackslashPath)
		require.NoError(t, err)
		assert.Equal(t, want, got)
	})

	t.Run("GetLaunchCommandForMedia", func(t *testing.T) {
		t.Parallel()
		want, err := mediaDB.GetLaunchCommandForMedia(ctx, "SNES", slashPath)
		require.NoError(t, err)
		got, err := mediaDB.GetLaunchCommandForMedia(ctx, "SNES", backslashPath)
		require.NoError(t, err)
		assert.Equal(t, want, got)
	})

	t.Run("FindMediaBySystemAndPath", func(t *testing.T) {
		t.Parallel()
		want, err := mediaDB.FindMediaBySystemAndPath(ctx, 1, slashPath)
		require.NoError(t, err)
		require.NotNil(t, want)
		got, err := mediaDB.FindMediaBySystemAndPath(ctx, 1, backslashPath)
		require.NoError(t, err)
		require.NotNil(t, got, "a native path must find the same row")
		assert.Equal(t, want.DBID, got.DBID)

		folded, err := mediaDB.FindMediaBySystemAndPathFold(ctx, 1, backslashPath)
		require.NoError(t, err)
		require.NotNil(t, folded, "a native path must find the same row when folded")
		assert.Equal(t, want.DBID, folded.DBID)
	})
}
