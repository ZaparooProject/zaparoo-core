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

package titles

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/slugs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/scantest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A canonical tag group holds a colon, which title splitting would take for
// the main/secondary separator. The title must be matched without its tag
// groups, or an article after " - " survives into the slug and the exact
// match misses: the 1943 hack below resolved to the plain 1943 through the
// progressive trim.
func TestResolveTitle_TagGroupsDoNotChangeTheSlug(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mediaDB, cleanup := helpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)
	cfg, err := helpers.NewTestConfig(nil, t.TempDir())
	require.NoError(t, err)

	dir := filepath.Join("roms", "NES")
	paths := []string{
		filepath.Join(dir, "1943 - The Battle of Midway (USA).nes"),
		filepath.Join(dir, "1943 - The Battle of Midway - 2011 Update (1943 Hack).nes"),
		filepath.Join(dir, "Zelda II - The Adventure of Link (USA).nes"),
		filepath.Join(dir, "Zelda II - The Adventure of Link (Europe).nes"),
		filepath.Join(dir, "Ninja Gaiden - The Dark Sword of Chaos (Hack).nes"),
	}
	scantest.IndexMediaPaths(t, mediaDB, "NES", paths...)
	require.NoError(t, mediaDB.RebuildSlugSearchCache())

	resolve := func(name string) *ResolveResult {
		t.Helper()
		result, resolveErr := ResolveTitle(ctx, &ResolveParams{
			MediaDB: mediaDB, Cfg: cfg, SystemID: "NES", GameName: name, MediaType: slugs.MediaTypeGame,
		})
		require.NoError(t, resolveErr, name)
		require.NotNil(t, result, name)
		return result
	}

	hack := resolve("1943 - The Battle of Midway - 2011 Update (unlicensed:hack)")
	assert.Equal(t, filepath.ToSlash(paths[1]), filepath.ToSlash(hack.Result.Path))
	assert.Equal(t, StrategyExactMatch, hack.Strategy)

	europe := resolve("Zelda II - The Adventure of Link (region:eu)")
	assert.Equal(t, filepath.ToSlash(paths[3]), filepath.ToSlash(europe.Result.Path))
	assert.Equal(t, StrategyExactMatch, europe.Strategy)

	// Every file's own title launch, as search and decks compose it, names
	// that file again.
	for _, path := range paths {
		tags, tagsErr := mediaDB.GetZapScriptTagsBySystemAndPath(ctx, "NES", path)
		require.NoError(t, tagsErr)
		identity, found, identityErr := database.LookupMediaIdentity(ctx, mediaDB, "NES", path)
		require.NoError(t, identityErr)
		require.True(t, found, path)
		script := database.BuildTitleZapScript("NES", identity.DisplayName, tags)
		name := strings.TrimPrefix(script, "@NES/")
		result := resolve(name)
		assert.Equal(t, filepath.ToSlash(path), filepath.ToSlash(result.Result.Path), script)
		assert.Equal(t, StrategyExactMatch, result.Strategy, script)
	}
}
