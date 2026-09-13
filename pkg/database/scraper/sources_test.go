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
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testSource(id int64, mediaPath, sourcePath string, unique bool) database.MediaSource {
	return database.MediaSource{
		MediaDBID: id, MediaPath: mediaPath, SourcePath: sourcePath,
		SourceKey: strings.ToLower(filepath.ToSlash(filepath.Clean(sourcePath))), SourceRoot: filepath.Dir(sourcePath),
		SourceKind: "directory", Unique: unique,
	}
}

func TestSourceIndex(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	first := testSource(1, "scummvm://monkey-vga/Old%20title", filepath.Join(root, "monkey.v1"), true)
	second := testSource(2, "scummvm://monkey-ega/Title", filepath.Join(root, "monkey.v2"), true)
	index := NewSourceIndex([]database.MediaSource{first, second, first})
	got, ok := index.ForMedia("SCUMMVM://monkey-vga/New%20title")
	require.True(t, ok)
	assert.Equal(t, first, got)
	got, ok = index.ForPath(first.SourcePath + string(filepath.Separator))
	require.True(t, ok)
	assert.Equal(t, first, got)
	assert.False(t, index.HasMedia("scummvm://unknown/Old%20title"))
	assert.False(t, index.HasMedia("scummvm://MONKEY-VGA/Old%20title"))
	_, ok = index.ForPath(filepath.Join(root, "unknown"))
	assert.False(t, ok)
}

func TestSourceIndexAmbiguity(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	shared := filepath.Join(root, "game")
	first := testSource(1, "scummvm://one/Title", shared, false)
	second := testSource(2, "scummvm://two/Title", shared, false)
	index := NewSourceIndex([]database.MediaSource{first, second})
	_, ok := index.ForPath(shared)
	assert.False(t, ok)
	_, ok = index.ForMedia(first.MediaPath)
	assert.True(t, ok, "ambiguous sources remain excluded from title guessing")

	conflict := testSource(3, first.MediaPath, filepath.Join(root, "different"), true)
	index = NewSourceIndex([]database.MediaSource{first, conflict})
	_, ok = index.ForMedia(first.MediaPath)
	assert.False(t, ok)
}

func TestVirtualMediaKey(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "scummvm://engine:game/", VirtualMediaKey("SCUMMVM://engine%3Agame/Changed%20name"))
	for _, path := range []string{"", "scummvm://", "scummvm:///Title", filepath.Join(t.TempDir(), "game.rom")} {
		assert.Empty(t, VirtualMediaKey(path))
	}
}
