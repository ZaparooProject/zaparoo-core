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
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/scantest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTitleLaunchScript(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "**launch.title:SNES/Super Metroid", TitleLaunchScript("SNES", "Super Metroid", nil))
	assert.Equal(t, "**launch.title:SNES/Super Mario World (unlicensed:hack)",
		TitleLaunchScript("SNES", "Super Mario World", []database.TagInfo{{Type: "unlicensed", Tag: "hack"}}))
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
	assert.Contains(t, hack.ZapScript, "**launch.title:SNES/Super Mario World")
	assert.Contains(t, hack.ZapScript, "(unlicensed:hack)")
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
