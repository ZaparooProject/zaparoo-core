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

package esmedia

import (
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestArtworkFallbackNames_RootFile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	gamePath := filepath.Join(root, "Game.nes")

	assert.Equal(t, []string{
		"Game.png",
		"Game.jpg",
		"Game.jpeg",
		"Game.webp",
	}, ArtworkFallbackNames(gamePath, root))
}

func TestArtworkFallbackNames_NestedFileMirrorsThenFlat(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	gamePath := filepath.Join(root, "Subdir", "Game.nes")

	assert.Equal(t, []string{
		filepath.Join("Subdir", "Game.png"),
		filepath.Join("Subdir", "Game.jpg"),
		filepath.Join("Subdir", "Game.jpeg"),
		filepath.Join("Subdir", "Game.webp"),
		"Game.png",
		"Game.jpg",
		"Game.jpeg",
		"Game.webp",
	}, ArtworkFallbackNames(gamePath, root))
}

func TestArtworkFallbackNames_RejectsEscapedPath(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	outside := filepath.Join(filepath.Dir(root), "Other", "Game.nes")

	assert.Nil(t, ArtworkFallbackNames(outside, root))
}

func TestFallbackArtworkNames(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []string{"Game.png", "Game.jpg", "Game.jpeg", "Game.webp"}, FallbackArtworkNames("Game"))
	assert.Nil(t, FallbackArtworkNames(""))
	assert.Nil(t, FallbackArtworkNames("."))
}

func TestStatMediaDirsFS(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	root := filepath.Join("roms", "nes")
	require.NoError(t, fs.MkdirAll(filepath.Join(root, "media", "boxart"), 0o750))
	require.NoError(t, fs.MkdirAll(filepath.Join(root, "media", "screenshots"), 0o750))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(root, "media", "file.txt"), []byte("x"), 0o600))

	dirs := StatMediaDirsFS(fs, root)
	assert.Equal(t, filepath.Join(root, "media", "boxart"), dirs["boxart"])
	assert.Equal(t, filepath.Join(root, "media", "screenshots"), dirs["screenshots"])
	assert.NotContains(t, dirs, "file.txt")
}

func TestFindFileFS_UsesCandidateAndNameOrder(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	root := filepath.Join("roms", "nes")
	boxartDir := filepath.Join(root, "media", "boxart")
	coverDir := filepath.Join(root, "media", "covers")
	require.NoError(t, fs.MkdirAll(boxartDir, 0o750))
	require.NoError(t, fs.MkdirAll(coverDir, 0o750))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(boxartDir, "Game.jpg"), []byte("jpg"), 0o600))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(coverDir, "Game.png"), []byte("png"), 0o600))

	file := FindFileFS(
		fs,
		[]string{"Game.png", "Game.jpg"},
		[]string{"boxart", "covers"},
		map[string]string{"boxart": boxartDir, "covers": coverDir},
	)

	require.NotNil(t, file)
	assert.Equal(t, filepath.ToSlash(filepath.Join(boxartDir, "Game.jpg")), file.Path)
	assert.Equal(t, "image/jpeg", file.ContentType)
}

func TestFindFileFS_RejectsTraversalNames(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	dir := filepath.Join("roms", "nes", "media", "boxart")
	require.NoError(t, fs.MkdirAll(dir, 0o750))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "Game.png"), []byte("png"), 0o600))

	file := FindFileFS(fs, []string{filepath.Join("..", "Game.png"), "Game.png"}, []string{"boxart"}, map[string]string{
		"boxart": dir,
	})

	require.NotNil(t, file)
	assert.Equal(t, filepath.ToSlash(filepath.Join(dir, "Game.png")), file.Path)
}

func TestResolvePath(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	resolved := ResolvePath(filepath.Join("Subdir", "Game.nes"), root)
	assert.Equal(t, filepath.Join(root, "Subdir", "Game.nes"), resolved)

	escaped := ResolvePath(filepath.Join("..", "Other", "Game.nes"), root)
	assert.Empty(t, escaped)
}

func TestResolvePathAbs_Home(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	resolved, ok := ResolvePathAbs(filepath.Join("~", "Assets", "Game.png"), t.TempDir())
	require.True(t, ok)
	assert.Equal(t, filepath.Join(home, "Assets", "Game.png"), resolved)
}

func TestPathWithinRoot(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	assert.True(t, PathWithinRoot(filepath.Join(root, "Subdir", "Game.nes"), root))
	assert.False(t, PathWithinRoot(filepath.Join(filepath.Dir(root), "Other", "Game.nes"), root))
}

func TestMimeFromExt(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "image/png", MimeFromExt("Game.PNG"))
	assert.Equal(t, "image/jpeg", MimeFromExt("Game.jpeg"))
	assert.Equal(t, "image/webp", MimeFromExt("Game.webp"))
	assert.Equal(t, "application/pdf", MimeFromExt("Manual.pdf"))
	assert.Equal(t, "application/octet-stream", MimeFromExt("Game.unknown"))
}

func TestArtworkDirCandidates_ContainsExpectedESDirs(t *testing.T) {
	t.Parallel()

	assert.Contains(t, ArtworkDirCandidates[string(tags.TagPropertyImageBoxart)], "covers")
	assert.Contains(t, ArtworkDirCandidates[string(tags.TagPropertyImageBoxart3D)], "3dboxes")
	assert.Contains(t, ArtworkDirCandidates[string(tags.TagPropertyImageScreenshot)], "screenshots")
	assert.Contains(t, ArtworkDirCandidates[string(tags.TagPropertyImageTitleshot)], "titlescreens")
}
