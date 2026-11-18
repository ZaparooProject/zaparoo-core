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

package misterarcade

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper/mra"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/slugs"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestAlternativesFolder(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		path   string
		folder string
		ok     bool
	}{
		{
			path:   filepath.Join(arcadeRoot, "_alternatives", "_Green Beret", "Rush'n Attack (US).mra"),
			folder: "Green Beret", ok: true,
		},
		{path: filepath.Join(arcadeRoot, "_Alternatives", "_1942", "1942 (Revision A).mra"), folder: "1942", ok: true},
		{path: `C:\games\_Arcade\_alternatives\_ASO\Alpha Mission.mra`, folder: "ASO", ok: true},
		{
			path:   filepath.Join(arcadeRoot, "_alternatives", "_ASO", "extra", "Alpha Mission.mra"),
			folder: "ASO", ok: true,
		},
		{path: filepath.Join(arcadeRoot, "1942 (Revision B).mra")},
		// A descriptor directly inside the tree is filed under no game.
		{path: filepath.Join(arcadeRoot, "_alternatives", "Loose.mra")},
		{path: filepath.Join(arcadeRoot, "_alternatives", "_", "Nameless.mra")},
	} {
		folder, ok := alternativesFolder(tc.path)
		assert.Equal(t, tc.ok, ok, tc.path)
		assert.Equal(t, tc.folder, folder, tc.path)
	}
}

func key(keyType, value string) database.VersionKey {
	return database.VersionKey{Type: keyType, Key: value}
}

func TestVersionKeys(t *testing.T) {
	t.Parallel()
	greenBeret := slugs.Slugify(slugs.MediaTypeGame, "Green Beret")
	altDir := filepath.Join(arcadeRoot, "_alternatives", "_Green Beret")

	t.Run("a main descriptor answers to its own set and title only", func(t *testing.T) {
		t.Parallel()
		row := &descriptorRow{path: filepath.Join(arcadeRoot, "Green Beret.mra"), titleSlug: greenBeret}
		// The parent set of a main game names its hardware, not a game it
		// is a version of.
		keys := versionKeys(row, mra.Identity{SetName: "gberet", Parent: "scotrsht"})
		assert.Equal(t, []database.VersionKey{
			key(database.VersionKeyMAMESet, "gberet"),
			key(database.VersionKeyAltFolder, greenBeret),
		}, keys)
	})

	t.Run("an alternate answers to its set, its parent and its folder", func(t *testing.T) {
		t.Parallel()
		row := &descriptorRow{
			path:      filepath.Join(altDir, "Rush'n Attack (US).mra"),
			titleSlug: slugs.Slugify(slugs.MediaTypeGame, "Rush'n Attack"),
		}
		keys := versionKeys(row, mra.Identity{SetName: "rushatck", Parent: "gberet"})
		assert.Equal(t, []database.VersionKey{
			key(database.VersionKeyMAMESet, "rushatck"),
			key(database.VersionKeyMAMESet, "gberet"),
			key(database.VersionKeyAltFolder, greenBeret),
		}, keys, "its own title is not a name other games are filed under")
	})

	t.Run("an alternate with no set still has its folder", func(t *testing.T) {
		t.Parallel()
		row := &descriptorRow{path: filepath.Join(arcadeRoot, "_alternatives", "_ASO", "Arian Mission.mra")}
		assert.Equal(t, []database.VersionKey{
			key(database.VersionKeyAltFolder, slugs.Slugify(slugs.MediaTypeGame, "ASO")),
		}, versionKeys(row, mra.Identity{}))
	})

	t.Run("an alternate that is a game of its own is not filed under the folder", func(t *testing.T) {
		t.Parallel()
		row := &descriptorRow{
			path:      filepath.Join(arcadeRoot, "_alternatives", "_Tron", "Satan's Hollow (Tron Hardware).mra"),
			otherGame: true,
		}
		assert.Equal(t, []database.VersionKey{
			key(database.VersionKeyMAMESet, "shollow"),
		}, versionKeys(row, mra.Identity{SetName: "shollow"}))
	})

	t.Run("a parent that repeats the set is written once", func(t *testing.T) {
		t.Parallel()
		row := &descriptorRow{path: filepath.Join(altDir, "Green Beret (bootleg).mra")}
		keys := versionKeys(row, mra.Identity{SetName: "gberetb", Parent: "gberetb"})
		assert.Equal(t, []string{database.VersionKeyMAMESet, database.VersionKeyAltFolder}, keyTypes(keys))
	})

	t.Run("a set name that is not a plain romset name is not a key", func(t *testing.T) {
		t.Parallel()
		row := &descriptorRow{path: filepath.Join(arcadeRoot, "Odd.mra")}
		assert.Empty(t, versionKeys(row, mra.Identity{SetName: "not a set"}))
	})
}

func TestMainIndexOtherGame(t *testing.T) {
	t.Parallel()
	tron := slugs.Slugify(slugs.MediaTypeGame, "Tron")
	hollow := slugs.Slugify(slugs.MediaTypeGame, "Satan's Hollow")
	rushn := slugs.Slugify(slugs.MediaTypeGame, "Rush'n Attack - Green Beret")
	inTron := filepath.Join(arcadeRoot, "_alternatives", "_Tron")
	inGreenBeret := filepath.Join(arcadeRoot, "_alternatives", "_Green Beret")
	rows := []descriptorRow{
		{path: filepath.Join(arcadeRoot, "Tron.mra"), titleDBID: 1, titleSlug: tron},
		{path: filepath.Join(arcadeRoot, "Satan's Hollow.mra"), titleDBID: 2, titleSlug: hollow},
		{path: filepath.Join(arcadeRoot, "Rush'n Attack - Green Beret.mra"), titleDBID: 3, titleSlug: rushn},
		{path: filepath.Join(inTron, "Tron (older).mra"), titleDBID: 1},
		{path: filepath.Join(inTron, "Satan's Hollow (Tron Hardware).mra"), titleDBID: 2},
		{path: filepath.Join(inTron, "Tron Bootleg.mra"), titleDBID: 4},
		{path: filepath.Join(inGreenBeret, "Rush'n Attack - Green Beret (older).mra"), titleDBID: 3},
	}
	mains := indexMains(rows)
	assert.False(t, mains.otherGame(&rows[0]), "a main is its own game")
	assert.False(t, mains.otherGame(&rows[3]), "an older build of the folder's game")
	assert.True(t, mains.otherGame(&rows[4]), "a game with a main of its own, filed under another game")
	assert.False(t, mains.otherGame(&rows[5]), "an alternate with no main is the folder's")
	assert.False(t, mains.otherGame(&rows[6]),
		"the folder is named after no main, so this alternate is what ties it to its game")
}

func keyTypes(keys []database.VersionKey) []string {
	types := make([]string, 0, len(keys))
	for _, k := range keys {
		types = append(types, k.Type)
	}
	return types
}

// The platform's cache answers the parent where it recorded one, and a
// descriptor it has no parent answer for is not read to find out.
func TestBuildTargetsWritesVersionKeys(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	main := descriptor(t, fs, "1941 - Counter Attack (World)", "1941")
	altDir := filepath.Join(arcadeRoot, "_alternatives", "_1941 - Counter Attack")
	require.NoError(t, fs.MkdirAll(altDir, 0o750))
	alternate := filepath.Join(altDir, "1941 - Counter Attack (Japan).mra")
	require.NoError(t, afero.WriteFile(fs, alternate, []byte(
		"<misterromdescription><setname>1941j</setname><parent>1941</parent></misterromdescription>"), 0o600))
	uncatalogued := filepath.Join(altDir, "Bootleg Attack.mra")
	require.NoError(t, afero.WriteFile(fs, uncatalogued, []byte(
		"<misterromdescription><setname>1941boot</setname><parent>1941</parent></misterromdescription>"), 0o600))

	slug := slugs.Slugify(slugs.MediaTypeGame, "1941 - Counter Attack")
	rows := []descriptorRow{
		{path: main, dbid: 100, titleDBID: 1, titleSlug: slug},
		{path: alternate, dbid: 101, titleDBID: 1, titleSlug: slug},
		{path: uncatalogued, dbid: 102, titleDBID: 2, titleSlug: "bootlegattack"},
	}
	impl := &scraperImpl{fs: fs, entries: index([]Entry{cps1Entry()}), unmapped: &scraper.UnmappedValues{}}

	built, err := impl.buildTargets(
		context.Background(), rows, indexMains(rows), map[int64]struct{}{}, scraper.ScrapeOptions{})
	require.NoError(t, err)
	targets, keyTargets := built.matched, built.keysOnly
	assert.Equal(t, matchStats{Processed: 3, Matched: 1, Skipped: 2}, built.stats)

	require.Len(t, targets, 1)
	assert.Equal(t, int64(100), targets[0].MediaDBID)
	assert.Equal(t, scraperID, targets[0].Write.VersionKeySource)
	assert.Contains(t, targets[0].Write.VersionKeys, key(database.VersionKeyMAMESet, "1941"))
	assert.Contains(t, targets[0].Write.VersionKeys, key(database.VersionKeyAltFolder, slug))
	assert.NotEmpty(t, targets[0].Write.Sentinel.Type)

	require.Len(t, keyTargets, 2)
	for i, want := range []struct {
		setName string
		dbid    int64
	}{{dbid: 101, setName: "1941j"}, {dbid: 102, setName: "1941boot"}} {
		write := keyTargets[i].Write
		assert.Equal(t, want.dbid, keyTargets[i].MediaDBID)
		assert.Empty(t, write.Sentinel.Type, "a row with no metadata is not marked scraped")
		assert.Equal(t, scraperID, write.VersionKeySource)
		assert.Equal(t, []database.VersionKey{
			key(database.VersionKeyMAMESet, want.setName),
			key(database.VersionKeyMAMESet, "1941"),
			key(database.VersionKeyAltFolder, slug),
		}, write.VersionKeys)
	}

	t.Run("cached identities are used without reading", func(t *testing.T) {
		t.Parallel()
		cached := &scraperImpl{
			fs: afero.NewMemMapFs(), entries: index([]Entry{cps1Entry()}), unmapped: &scraper.UnmappedValues{},
			identities: func(path string) (string, string, bool) {
				if path == alternate {
					return "1941J", "1941", true
				}
				return "", "", false
			},
			cache: func(string) (string, bool) { return "1941boot", true },
		}
		cachedBuilt, buildErr := cached.buildTargets(
			context.Background(), rows[1:], indexMains(rows), map[int64]struct{}{}, scraper.ScrapeOptions{})
		require.NoError(t, buildErr)
		keysOnly := cachedBuilt.keysOnly
		require.Len(t, keysOnly, 2)
		assert.Contains(t, keysOnly[0].Write.VersionKeys, key(database.VersionKeyMAMESet, "1941"),
			"the cache recorded the parent")
		assert.NotContains(t, keysOnly[1].Write.VersionKeys, key(database.VersionKeyMAMESet, "1941"),
			"a set name cached without a parent leaves the parent unknown")
		assert.Contains(t, keysOnly[1].Write.VersionKeys, key(database.VersionKeyMAMESet, "1941boot"))
	})
}

// The media walk carries no title slug, so a main descriptor's is read from
// its title. Without it the main never answers to its alternates' folder.
func TestBuildTargetsReadsMainTitleSlug(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	main := descriptor(t, fs, "Solo Flight", "soloflt")
	altDir := filepath.Join(arcadeRoot, "_alternatives", "_Solo Flight")
	require.NoError(t, fs.MkdirAll(altDir, 0o750))
	alternate := filepath.Join(altDir, "Night Sortie.mra")
	require.NoError(t, afero.WriteFile(fs, alternate, []byte(
		"<misterromdescription><setname>nightsrt</setname></misterromdescription>"), 0o600))

	slug := slugs.Slugify(slugs.MediaTypeGame, "Solo Flight")
	mediaDB := testhelpers.NewMockMediaDBI()
	mediaDB.On("GetTitlesByDBIDs", mock.Anything, []int64{1}).
		Return([]database.TitleWithSystem{{DBID: 1, Slug: slug}}, nil).Once()
	impl := &scraperImpl{fs: fs, db: mediaDB, entries: index(nil), unmapped: &scraper.UnmappedValues{}}

	rows := []descriptorRow{
		{path: main, dbid: 100, titleDBID: 1},
		{path: alternate, dbid: 101, titleDBID: 2},
	}
	built, err := impl.buildTargets(
		context.Background(), rows, indexMains(rows), map[int64]struct{}{}, scraper.ScrapeOptions{})
	require.NoError(t, err)
	require.Len(t, built.keysOnly, 2)
	folder := key(database.VersionKeyAltFolder, slug)
	assert.Contains(t, built.keysOnly[0].Write.VersionKeys, folder, "the main answers to its own title")
	assert.Contains(t, built.keysOnly[1].Write.VersionKeys, folder, "the alternate answers to its folder")
	mediaDB.AssertExpectations(t)
}
