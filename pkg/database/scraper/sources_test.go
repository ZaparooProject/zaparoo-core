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

package scraper

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSourceIndex(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	first := MediaSource{MediaPath: "scummvm://monkey-vga/Old%20title", Directory: filepath.Join(root, "monkey.v1")}
	second := MediaSource{MediaPath: "scummvm://monkey-ega/Title", Directory: filepath.Join(root, "monkey.v2")}
	index := NewSourceIndex([]MediaSource{first, second, first})
	got, ok := index.ForMedia("SCUMMVM://monkey-vga/New%20title")
	require.True(t, ok)
	assert.Equal(t, first, got)
	got, ok = index.ForDirectory(first.Directory + string(filepath.Separator))
	require.True(t, ok)
	assert.Equal(t, first, got)
	assert.False(t, index.HasMedia("scummvm://unknown/Old%20title"))
	assert.False(t, index.HasMedia("scummvm://MONKEY-VGA/Old%20title"))
	_, ok = index.ForDirectory(filepath.Join(root, "unknown"))
	assert.False(t, ok)
}

func TestSourceIndexAmbiguity(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	first := MediaSource{MediaPath: "scummvm://one/Title", Directory: filepath.Join(root, "game")}
	for _, tc := range []struct {
		name          string
		second        MediaSource
		identityValid bool
	}{
		{
			name: "shared directory", identityValid: true,
			second: MediaSource{MediaPath: "scummvm://two/Title", Directory: first.Directory},
		},
		{
			name:   "conflicting target",
			second: MediaSource{MediaPath: first.MediaPath, Directory: filepath.Join(root, "different")},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// Repetition must never revive a key marked ambiguous.
			index := NewSourceIndex([]MediaSource{first, tc.second, first, tc.second})
			_, ok := index.ForDirectory(first.Directory)
			assert.False(t, ok)
			_, ok = index.ForDirectory(tc.second.Directory)
			assert.False(t, ok)
			_, ok = index.ForMedia(first.MediaPath)
			assert.Equal(t, tc.identityValid, ok)
			assert.True(t, index.HasMedia(first.MediaPath), "ambiguous targets remain excluded from title guessing")
		})
	}
}

func TestVirtualMediaKey(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "scummvm://engine:game/", VirtualMediaKey("SCUMMVM://engine%3Agame/Changed%20name"))
	for _, path := range []string{"", "scummvm://", "scummvm:///Title", filepath.Join(t.TempDir(), "game.rom")} {
		assert.Empty(t, VirtualMediaKey(path))
	}
}
