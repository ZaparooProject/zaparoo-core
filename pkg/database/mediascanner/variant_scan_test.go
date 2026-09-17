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
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A scan must mark a single-file title carrying a game-variant tag for
// disambiguation, so the hack tag reaches its title launch without a plain
// sibling and without waiting for the next full recompute.
func TestScanRecomputesLoneVariantTitle(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mediaDB, cleanup := testhelpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)

	// Stored paths use forward slashes on every OS, so look them up that way.
	nesPath := func(name string) string {
		return filepath.ToSlash(filepath.Join("roms", "NES", name))
	}
	hackPath := nesPath("Contra (USA) (Hack).nes")
	plainPath := nesPath("Metroid (USA).nes")
	indexMediaPaths(t, mediaDB, "NES", hackPath, plainPath)

	hackTags, err := mediaDB.GetZapScriptTagsBySystemAndPath(ctx, "NES", hackPath)
	require.NoError(t, err)
	assert.Contains(t, hackTags, database.TagInfo{Type: "unlicensed", Tag: "hack"})

	plainTags, err := mediaDB.GetZapScriptTagsBySystemAndPath(ctx, "NES", plainPath)
	require.NoError(t, err)
	assert.Empty(t, plainTags, "a lone plain release still has nothing to disambiguate")

	// A later scan that adds a hack beside an existing lone title marks it.
	siblingHack := nesPath("Metroid (USA) (Hack).nes")
	indexMediaPaths(t, mediaDB, "NES", hackPath, plainPath, siblingHack)
	siblingTags, err := mediaDB.GetZapScriptTagsBySystemAndPath(ctx, "NES", siblingHack)
	require.NoError(t, err)
	assert.Contains(t, siblingTags, database.TagInfo{Type: "unlicensed", Tag: "hack"})

	// So does a later scan that adds a hack with no sibling at all.
	loneHack := nesPath("Kid Icarus (USA) (Hack).nes")
	stats := indexMediaPaths(t, mediaDB, "NES", hackPath, plainPath, siblingHack, loneHack)
	assert.Equal(t, int64(1), stats.TouchedTitles)
	loneTags, err := mediaDB.GetZapScriptTagsBySystemAndPath(ctx, "NES", loneHack)
	require.NoError(t, err)
	assert.Contains(t, loneTags, database.TagInfo{Type: "unlicensed", Tag: "hack"})

	// Rescanning the same files refreshes nothing.
	stats = indexMediaPaths(t, mediaDB, "NES", hackPath, plainPath, siblingHack, loneHack)
	assert.Equal(t, int64(0), stats.TouchedTitles)
}
