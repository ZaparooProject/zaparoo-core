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

package audio

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/audio/gme"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/audio/midi"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/audio/trackmeta"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/audio/vgm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLeadingYear(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   string
		want int
	}{
		{in: "1991", want: 1991},
		{in: " 1988 Nintendo", want: 1988},
		{in: "1994-11-02", want: 1994},
		{in: "19911"},
		{in: "Sega"},
		{in: ""},
		{in: "0001"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, leadingYear(tt.in), tt.in)
	}
}

func TestChipMeta(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	song := filepath.Join(dir, "song.vgm")

	assert.Equal(t, &trackmeta.Meta{Title: "Green Hill", Artist: "Masato Nakamura", Album: "Sonic", Year: 1991},
		vgmMeta(song, &vgm.Info{Title: "Green Hill", Author: "Masato Nakamura", Game: "Sonic", Year: "1991/06/23"}))
	assert.Equal(t, &trackmeta.Meta{Title: "Overworld", Album: "Zelda", Year: 1986},
		gmeMeta(song, &gme.Info{Title: "Overworld", Game: "Zelda", Copyright: "1986 Nintendo"}))
	assert.Nil(t, midiMeta(song, midi.Info{}), "no tags and no art is no metadata")

	require.NoError(t, os.WriteFile(filepath.Join(dir, "cover.png"), []byte("png"), 0o600))
	assert.Equal(t, &trackmeta.Meta{HasArt: true}, midiMeta(song, midi.Info{}))
}
