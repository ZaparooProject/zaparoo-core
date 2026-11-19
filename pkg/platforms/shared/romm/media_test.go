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

package romm

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRomPathRoundTrip(t *testing.T) {
	t.Parallel()

	p := RomPath(42, "Game: The Sequel (USA) [!]")
	assert.True(t, IsRomPath(p))
	assert.True(t, IsRomPath("ROMM://1/x"))
	assert.False(t, IsRomPath("/roms/romm/x.nes"))
	id, err := ParseRomPath(p)
	require.NoError(t, err)
	assert.Equal(t, 42, id)

	for _, bad := range []string{
		"romm://0/x", "romm://-1/x", "romm://+5/x", "romm://007/x", "romm:///x", "steam://5/x",
	} {
		_, err = ParseRomPath(bad)
		require.ErrorIs(t, err, ErrInvalidPath, bad)
	}
}

func TestLayoutFor(t *testing.T) {
	t.Parallel()

	single := &Rom{ID: 1, FSName: "Folder", Files: []RomFile{{FileName: "Game (USA).sfc"}}}
	l, err := LayoutFor(single)
	require.NoError(t, err)
	assert.Equal(t, Layout{Name: "Game (USA).sfc"}, l)
	assert.Equal(t, "Game (USA)", Title(l))
	assert.Equal(t, []string{"game (usa).sfc"}, LocalNames(single, l))

	multi := &Rom{
		ID: 2, FSName: "Disc Game", FSNameNoExt: "Disc Game",
		Files: []RomFile{{FileName: "Disc Game (Disc 1).cue"}, {FileName: "sub/Disc Game (Disc 1).bin"}},
	}
	l, err = LayoutFor(multi)
	require.NoError(t, err)
	assert.Equal(t, Layout{Name: "Disc Game", Archive: true}, l)
	assert.Equal(t, "Disc Game", Title(l))
	assert.Equal(t, []string{"disc game (disc 1).cue", "disc game (disc 1).bin", "disc game.m3u"},
		LocalNames(multi, l))
	assert.Equal(t, []string{".cue", ".bin"}, extensions(multi, l))

	bare := &Rom{ID: 3, FSName: "Bare.gb"}
	l, err = LayoutFor(bare)
	require.NoError(t, err)
	assert.Equal(t, Layout{Name: "Bare.gb"}, l)
	assert.Equal(t, []string{"bare.gb"}, LocalNames(bare, l))

	for _, name := range []string{"", "..", "a/b", `a\b`, "c:x", "x\x00"} {
		_, err = LayoutFor(&Rom{ID: 4, Files: []RomFile{{FileName: name}}})
		require.Error(t, err, "%q", name)
	}
}

func TestDownloadable(t *testing.T) {
	t.Parallel()

	yes, no := true, false
	assert.True(t, (&Rom{}).Downloadable())
	assert.True(t, (&Rom{HasFileOnDisk: &yes}).Downloadable())
	assert.False(t, (&Rom{HasFileOnDisk: &no}).Downloadable())
	assert.False(t, (&Rom{MissingFromFS: true}).Downloadable())
}
