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

func TestFindFileFS_NotFound(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	root := filepath.Join("roms", "nes")
	boxartDir := filepath.Join(root, "media", "boxart")
	require.NoError(t, fs.MkdirAll(boxartDir, 0o750))

	file := FindFileFS(fs, []string{"Missing.png"}, []string{"boxart"}, map[string]string{"boxart": boxartDir})

	assert.Nil(t, file)
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

func TestFindFileAcrossRootsFS_FirstRootInOrderWins(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	rootA := filepath.Join("media", "usb0", "nes")
	rootB := filepath.Join("media", "fat", "nes")
	boxartA := filepath.Join(rootA, "media", "boxart")
	boxartB := filepath.Join(rootB, "media", "boxart")
	require.NoError(t, fs.MkdirAll(boxartA, 0o750))
	require.NoError(t, fs.MkdirAll(boxartB, 0o750))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(boxartA, "Game.png"), []byte("a"), 0o600))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(boxartB, "Game.png"), []byte("b"), 0o600))

	file := FindFileAcrossRootsFS(
		fs,
		[]string{"Game.png"},
		[]string{"boxart"},
		[]map[string]string{
			{"boxart": boxartA},
			{"boxart": boxartB},
		},
	)

	require.NotNil(t, file)
	assert.Equal(t, filepath.ToSlash(filepath.Join(boxartA, "Game.png")), file.Path)
}

func TestFindFileAcrossRootsFS_FallsThroughToLaterRoot(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	romRoot := filepath.Join("media", "fat", "cifs", "nes")
	artRoot := filepath.Join("media", "fat", "nes")
	boxartArt := filepath.Join(artRoot, "media", "boxart")
	require.NoError(t, fs.MkdirAll(filepath.Join(romRoot, "media"), 0o750))
	require.NoError(t, fs.MkdirAll(boxartArt, 0o750))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(boxartArt, "Game.png"), []byte("art"), 0o600))

	file := FindFileAcrossRootsFS(
		fs,
		[]string{"Game.png"},
		[]string{"boxart"},
		[]map[string]string{
			StatMediaDirsFS(fs, romRoot),
			StatMediaDirsFS(fs, artRoot),
		},
	)

	require.NotNil(t, file)
	assert.Equal(t, filepath.ToSlash(filepath.Join(boxartArt, "Game.png")), file.Path)
}

func TestFindFileAcrossRootsFS_NotFound(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	boxart := filepath.Join("media", "fat", "nes", "media", "boxart")
	require.NoError(t, fs.MkdirAll(boxart, 0o750))

	file := FindFileAcrossRootsFS(
		fs,
		[]string{"Missing.png"},
		[]string{"boxart"},
		[]map[string]string{{"boxart": boxart}},
	)

	assert.Nil(t, file)
}

func TestResolvePath(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	resolved := ResolvePath(filepath.Join("Subdir", "Game.nes"), root)
	assert.Equal(t, filepath.Join(root, "Subdir", "Game.nes"), resolved)

	escaped := ResolvePath(filepath.Join("..", "Other", "Game.nes"), root)
	assert.Empty(t, escaped)
}

func TestResolvePathAbs_Empty(t *testing.T) {
	t.Parallel()

	resolved, ok := ResolvePathAbs("", t.TempDir())
	assert.False(t, ok)
	assert.Empty(t, resolved)
}

func TestResolvePathAbs_Absolute(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, "Assets", "Game.png")
	resolved, ok := ResolvePathAbs(path, t.TempDir())
	require.True(t, ok)
	assert.Equal(t, path, resolved)
}

func TestResolvePathAbs_Home(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	resolved, ok := ResolvePathAbs(filepath.Join("~", "Assets", "Game.png"), t.TempDir())
	require.True(t, ok)
	assert.Equal(t, filepath.Join(home, "Assets", "Game.png"), resolved)
}

func TestResolvePathAbs_HomeBackslash(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	resolved, ok := ResolvePathAbs(`~\Assets\Game.png`, t.TempDir())
	require.True(t, ok)
	expected := filepath.Clean(home + string(filepath.Separator) + `Assets\Game.png`)
	assert.Equal(t, expected, resolved)
	assert.True(t, IsHomeRelativePath(`~\Assets\Game.png`))
}

func TestResolvePath_HomeBackslashInsideRoot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	resolved := ResolvePath(`~\games\mario.nes`, home)
	expected := filepath.Clean(home + string(filepath.Separator) + `games\mario.nes`)
	assert.Equal(t, expected, resolved)
}

func TestPathWithinRoot(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	assert.True(t, PathWithinRoot(filepath.Join(root, "Subdir", "Game.nes"), root))
	assert.False(t, PathWithinRoot(filepath.Join(filepath.Dir(root), "Other", "Game.nes"), root))
}

func TestMimeFromExt(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"Game.PNG":      "image/png",
		"Game.jpeg":     "image/jpeg",
		"Animation.gif": "image/gif",
		"Game.webp":     "image/webp",
		"Video.mp4":     "video/mp4",
		"Video.mkv":     "video/x-matroska",
		"Video.avi":     "video/avi",
		"Manual.pdf":    "application/pdf",
		"Song.mp3":      "audio/mpeg",
		"Song.m4a":      "audio/mp4",
		"Book.m4b":      "audio/mp4",
		"Video.mpg":     "video/mpeg",
		"Video.mpeg":    "video/mpeg",
		"Video.m4v":     "video/mp4",
		"Game.unknown":  "application/octet-stream",
	}
	for path, want := range cases {
		assert.Equal(t, want, MimeFromExt(path), path)
	}
}

func TestArtworkDirCandidates_ContainsExpectedESDirs(t *testing.T) {
	t.Parallel()

	assert.Contains(t, ArtworkDirCandidates[string(tags.TagPropertyImageBoxart)], "covers")
	assert.Contains(t, ArtworkDirCandidates[string(tags.TagPropertyImageBoxart3D)], "3dboxes")
	assert.Contains(t, ArtworkDirCandidates[string(tags.TagPropertyImageScreenshot)], "screenshots")
	assert.Contains(t, ArtworkDirCandidates[string(tags.TagPropertyImageTitleshot)], "titlescreens")
}
