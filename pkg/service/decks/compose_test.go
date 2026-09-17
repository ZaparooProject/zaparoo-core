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

package decks

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/scantest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTitleLaunchScript(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "**launch.title:SNES/Super Metroid", TitleLaunchScript("SNES", "Super Metroid", nil))

	// Every script must parse back to one launch.title command whose single
	// argument is the system, title and tags as composed.
	tests := []struct {
		name string
		want string
		tags []database.TagInfo
	}{
		{
			name: "Super Mario World",
			tags: []database.TagInfo{{Type: "unlicensed", Tag: "hack"}},
			want: "SNES/Super Mario World (unlicensed:hack)",
		},
		{name: "Who Wants to Be a Millionaire?", want: "SNES/Who Wants to Be a Millionaire?"},
		{name: "Rock || Roll", want: "SNES/Rock || Roll"},
		{
			name: "Game",
			tags: []database.TagInfo{{Type: "region", Tag: "eu"}, {Type: "region", Tag: "us"}},
			want: "SNES/Game (region:eu, region:us)",
		},
		{name: `Say "Hi"`, want: `SNES/Say "Hi"`},
		{name: "Hello [[World]]", want: "SNES/Hello [[World]]"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			script := TitleLaunchScript("SNES", tc.name, tc.tags)
			parsed, err := zapscript.NewParser(script).ParseScript()
			require.NoError(t, err, script)
			require.Len(t, parsed.Cmds, 1, script)
			assert.Equal(t, zapscript.ZapScriptCmdLaunchTitle, parsed.Cmds[0].Name)
			require.Len(t, parsed.Cmds[0].Args, 1, script)
			assert.Equal(t, tc.want, parsed.Cmds[0].Args[0])
			assert.True(t, parsed.Cmds[0].AdvArgs.IsEmpty(), script)
		})
	}
}

// A game added from this device composes a title launch that names the same
// game on any device: the hack tag rides along even though the plain release
// is what the title matcher would otherwise pick, and the anchor keeps the
// exact file for this device.
func TestComposeMediaItem(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mediaDB, cleanup := testhelpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)

	hackPath := filepath.Join("roms", "SNES", "Super Mario World (USA) (Hack).sfc")
	plainPath := filepath.Join("roms", "SNES", "Super Mario World (USA).sfc")
	scantest.IndexMediaPaths(t, mediaDB, "SNES", hackPath, plainPath)

	hack, err := ComposeMediaItem(ctx, mediaDB, "SNES", hackPath)
	require.NoError(t, err)
	assert.Equal(t, database.DeckItemKindScript, hack.Kind)
	assert.Equal(t, "Super Mario World", hack.Name)
	parsed, err := zapscript.NewParser(hack.ZapScript).ParseScript()
	require.NoError(t, err)
	require.Len(t, parsed.Cmds, 1)
	assert.Equal(t, zapscript.ZapScriptCmdLaunchTitle, parsed.Cmds[0].Name)
	require.Len(t, parsed.Cmds[0].Args, 1)
	assert.True(t, strings.HasPrefix(parsed.Cmds[0].Args[0], "SNES/Super Mario World"))
	assert.Contains(t, parsed.Cmds[0].Args[0], "(unlicensed:hack)")
	assert.Equal(t, "SNES", hack.Anchor.SystemID)
	assert.Equal(t, filepath.ToSlash(hackPath), hack.Anchor.Path)
	assert.Equal(t, "Super Mario World", hack.Anchor.MediaName)
	assert.Contains(t, hack.Anchor.Tags, "unlicensed:hack")
	assert.Contains(t, hack.Anchor.Tags, "region:us")

	plain, err := ComposeMediaItem(ctx, mediaDB, "SNES", plainPath)
	require.NoError(t, err)
	assert.NotContains(t, plain.ZapScript, "hack")
	assert.NotEqual(t, hack.ZapScript, plain.ZapScript, "the hack and the plain release compose different scripts")

	_, err = ComposeMediaItem(ctx, mediaDB, "SNES", filepath.Join("roms", "SNES", "Missing.sfc"))
	require.ErrorIs(t, err, ErrMediaNotIndexed)
}

// Right after an upgrade that changed the variant rule, a title's stored
// disambiguating types can still be the old ones until the one-time recompute
// runs. The old rule only marked types that differ between a title's files, so
// a lone homebrew, or a hack kept in two folders, had none. A game added to a
// deck in that window must still carry its variant tag, because the deck keeps
// the script.
func TestComposeMediaItem_StaleDisambiguation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mediaDB, cleanup := testhelpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)

	hackPath := filepath.Join("roms", "SNES", "Hacks", "Super Mario World (USA) (Hack).sfc")
	hackCopyPath := filepath.Join("roms", "SNES", "All", "Super Mario World (USA) (Hack).sfc")
	homebrewPath := filepath.Join("roms", "SNES", "Nova the Squirrel (World) (Homebrew).sfc")
	plainPath := filepath.Join("roms", "SNES", "Donkey Kong Country (USA).sfc")
	paths := []string{hackPath, hackCopyPath, homebrewPath, plainPath}
	scantest.IndexMediaPaths(t, mediaDB, "SNES", paths...)

	current := make(map[string]string, len(paths))
	for _, path := range paths {
		item, err := ComposeMediaItem(ctx, mediaDB, "SNES", path)
		require.NoError(t, err)
		current[path] = item.ZapScript
	}
	assert.Equal(t, `**launch.title:"SNES/Super Mario World (unlicensed:hack)"`, current[hackPath])
	assert.Equal(t, current[hackPath], current[hackCopyPath])
	assert.Contains(t, current[homebrewPath], "(release:homebrew)")
	assert.Equal(t, "**launch.title:SNES/Donkey Kong Country", current[plainPath])

	_, err := mediaDB.UnsafeGetSQLDb().ExecContext(ctx, `update MediaTitles set DisambiguationTypes = '';`)
	require.NoError(t, err)
	stale, err := mediaDB.GetZapScriptTagsBySystemAndPath(ctx, "SNES", hackPath)
	require.NoError(t, err)
	require.Empty(t, stale, "the stored types are stale")

	for _, path := range paths {
		item, composeErr := ComposeMediaItem(ctx, mediaDB, "SNES", path)
		require.NoError(t, composeErr)
		assert.Equal(t, current[path], item.ZapScript, "stale stored types still compose the current script")
	}
}
