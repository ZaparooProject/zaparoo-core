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
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTreeListingState_MovesWithAnyNameAtAnyDepth(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	root := filepath.Join("games", "media", "boxart")
	require.NoError(t, afero.WriteFile(fs, filepath.Join(root, "a.png"), []byte("x"), 0o600))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(root, "sub", "b.png"), []byte("x"), 0o600))
	state := func(dir string) string {
		t.Helper()
		described, ok := TreeListingState(fs, dir)
		require.True(t, ok)
		return described
	}
	base := state(root)
	assert.Equal(t, base, state(root))

	require.NoError(t, afero.WriteFile(fs, filepath.Join(root, "sub", "deeper", "c.png"), []byte("x"), 0o600))
	added := state(root)
	assert.NotEqual(t, base, added)

	require.NoError(t, fs.Rename(filepath.Join(root, "a.png"), filepath.Join(root, "sub", "a.png")))
	assert.NotEqual(t, added, state(root), "a file moved between folders is a change")

	assert.Equal(t, "unreadable", state(filepath.Join("games", "missing")))
}

// A lookup follows a symlinked folder and this walk does not, so a tree that
// has one cannot be described well enough to skip a scrape on.
func TestTreeListingState_RefusesTreeWithSymlinkedDirectory(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	root := filepath.Join(base, "media", "boxart")
	elsewhere := filepath.Join(base, "elsewhere")
	require.NoError(t, os.MkdirAll(root, 0o750))
	require.NoError(t, os.MkdirAll(elsewhere, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(root, "a.png"), []byte("x"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(elsewhere, "b.png"), []byte("x"), 0o600))
	fs := afero.NewOsFs()

	_, ok := TreeListingState(fs, root)
	require.True(t, ok)

	// A symlink to a file is only a name, which the description covers.
	if err := os.Symlink(filepath.Join(elsewhere, "b.png"), filepath.Join(root, "b.png")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	_, ok = TreeListingState(fs, root)
	require.True(t, ok)

	require.NoError(t, os.Symlink(elsewhere, filepath.Join(root, "sub")))
	_, ok = TreeListingState(fs, root)
	assert.False(t, ok)
}
