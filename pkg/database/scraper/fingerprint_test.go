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
	base := TreeListingState(fs, root)
	assert.Equal(t, base, TreeListingState(fs, root))

	require.NoError(t, afero.WriteFile(fs, filepath.Join(root, "sub", "deeper", "c.png"), []byte("x"), 0o600))
	added := TreeListingState(fs, root)
	assert.NotEqual(t, base, added)

	require.NoError(t, fs.Rename(filepath.Join(root, "a.png"), filepath.Join(root, "sub", "a.png")))
	assert.NotEqual(t, added, TreeListingState(fs, root), "a file moved between folders is a change")

	assert.Equal(t, "unreadable", TreeListingState(fs, filepath.Join("games", "missing")))
}
