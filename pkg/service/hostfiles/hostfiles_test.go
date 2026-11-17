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

package hostfiles_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/hostfiles"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeFile(t *testing.T, path string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	require.NoError(t, os.WriteFile(path, []byte("x"), 0o600))
}

// tree is two browse roots and a folder outside both.
type tree struct {
	browser *hostfiles.Browser
	games   string
	images  string
	outside string
}

func buildTree(t *testing.T) tree {
	t.Helper()
	// Resolved, so the paths compared below are the real ones on hosts whose
	// temp folder sits behind a symlink.
	base, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	games, images := filepath.Join(base, "games"), filepath.Join(base, "images")
	outside := filepath.Join(base, "private")
	writeFile(t, filepath.Join(games, "SNES", "covers", "b.PNG"))
	writeFile(t, filepath.Join(games, "SNES", "covers", "a.jpg"))
	writeFile(t, filepath.Join(games, "SNES", "covers", "notes.txt"))
	writeFile(t, filepath.Join(games, "SNES", "covers", ".hidden.png"))
	writeFile(t, filepath.Join(games, "SNES", "covers", "Zeta", "c.webp"))
	writeFile(t, filepath.Join(games, "SNES", "game.sfc"))
	writeFile(t, filepath.Join(images, "thumb.gif"))
	writeFile(t, filepath.Join(outside, "secret.png"))
	browser := hostfiles.NewBrowser([]string{games, games, filepath.Join(base, "missing"), "relative"}, images)
	return tree{browser: browser, games: games, images: images, outside: outside}
}

func rootIDFor(t *testing.T, browser *hostfiles.Browser, path string) string {
	t.Helper()
	for _, root := range browser.Roots() {
		if root.Path == path {
			return root.ID
		}
	}
	require.FailNowf(t, "no root", "for %s", path)
	return ""
}

func TestNewBrowserKeepsExistingFoldersOnce(t *testing.T) {
	t.Parallel()
	tr := buildTree(t)
	browser, games, images, _ := tr.browser, tr.games, tr.images, tr.outside

	roots := browser.Roots()
	require.Len(t, roots, 2, "a missing folder, a relative path and a duplicate are left out")
	assert.Equal(t, hostfiles.ImagesRootID, roots[0].ID)
	assert.Equal(t, images, roots[0].Path)
	assert.Equal(t, games, roots[1].Path)
	assert.NotContains(t, roots[1].ID, "games", "a root's ID is not a path")
}

func TestListImagesKeepsFoldersAndImages(t *testing.T) {
	t.Parallel()
	tr := buildTree(t)
	browser, games, _, _ := tr.browser, tr.games, tr.images, tr.outside
	root := rootIDFor(t, browser, games)

	listing, err := browser.ListImages(context.Background(), root, "SNES/covers")
	require.NoError(t, err)
	assert.False(t, listing.Truncated)
	names := make([]string, 0, len(listing.Entries))
	for _, entry := range listing.Entries {
		names = append(names, entry.Name)
	}
	assert.Equal(t, []string{"Zeta", "a.jpg", "b.PNG"}, names, "folders first, then images by name")
	assert.True(t, listing.Entries[0].IsDir)
	assert.Equal(t, "SNES/covers/a.jpg", listing.Entries[1].Path)
	assert.Equal(t, int64(1), listing.Entries[1].Size)

	top, err := browser.ListImages(context.Background(), root, "")
	require.NoError(t, err)
	require.Len(t, top.Entries, 1)
	assert.Equal(t, "SNES", top.Entries[0].Path)

	_, err = browser.ListImages(context.Background(), root, "SNES/game.sfc")
	require.ErrorIs(t, err, hostfiles.ErrNotDir)
	_, err = browser.ListImages(context.Background(), "nope", "")
	require.ErrorIs(t, err, hostfiles.ErrUnknownRoot)
}

func TestResolveRefusesPathsThatLeaveTheRoot(t *testing.T) {
	t.Parallel()
	tr := buildTree(t)
	browser, games, _, outside := tr.browser, tr.games, tr.images, tr.outside
	root := rootIDFor(t, browser, games)

	for _, rel := range []string{
		"..", "../private", "SNES/../../private/secret.png", "/etc", filepath.ToSlash(outside),
		`SNES\covers`, "SNES/\x00",
	} {
		_, err := browser.Resolve(root, rel)
		require.ErrorIs(t, err, hostfiles.ErrInvalidPath, "path %q", rel)
	}
	_, err := browser.Resolve(root, "SNES/nothing")
	require.ErrorIs(t, err, hostfiles.ErrNotFound)

	path, err := browser.Resolve(root, "SNES/./covers")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(games, "SNES", "covers"), path)
}

func TestSymlinksOutOfTheRootAreNeitherListedNorResolved(t *testing.T) {
	t.Parallel()
	tr := buildTree(t)
	browser, games, _, outside := tr.browser, tr.games, tr.images, tr.outside
	root := rootIDFor(t, browser, games)
	covers := filepath.Join(games, "SNES", "covers")
	if err := os.Symlink(outside, filepath.Join(covers, "escape")); err != nil {
		t.Skipf("symlinks are not available: %v", err)
	}
	require.NoError(t, os.Symlink(filepath.Join(outside, "secret.png"), filepath.Join(covers, "leak.png")))
	require.NoError(t, os.Symlink(filepath.Join(covers, "a.jpg"), filepath.Join(covers, "inside.jpg")))

	listing, err := browser.ListImages(context.Background(), root, "SNES/covers")
	require.NoError(t, err)
	names := make([]string, 0, len(listing.Entries))
	for _, entry := range listing.Entries {
		names = append(names, entry.Name)
	}
	assert.Equal(t, []string{"Zeta", "a.jpg", "b.PNG", "inside.jpg"}, names)

	for _, rel := range []string{"SNES/covers/escape", "SNES/covers/escape/secret.png", "SNES/covers/leak.png"} {
		_, err = browser.Resolve(root, rel)
		require.ErrorIs(t, err, hostfiles.ErrInvalidPath, "path %q", rel)
	}
}

func TestResolveImageNeedsAnImageFile(t *testing.T) {
	t.Parallel()
	tr := buildTree(t)
	browser, games, images, _ := tr.browser, tr.games, tr.images, tr.outside
	root := rootIDFor(t, browser, games)

	path, err := browser.ResolveImage(hostfiles.ImagesRootID, "thumb.gif")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(images, "thumb.gif"), path)

	for _, rel := range []string{"SNES/game.sfc", "SNES/covers", "SNES/covers/notes.txt"} {
		_, err = browser.ResolveImage(root, rel)
		require.ErrorIs(t, err, hostfiles.ErrNotFile, "path %q", rel)
	}
}
