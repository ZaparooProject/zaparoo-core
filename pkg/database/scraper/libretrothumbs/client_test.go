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

package libretrothumbs

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A cached index is read verbatim from disk, so it is validated exactly like
// a fresh listing: a tampered file, or one a pre-fix run wrote, must not
// smuggle a traversal name past the check parseIndex applies to the server.
func TestIndexFiltersTraversalNamesFromACachedListing(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	c := newClient(fs, "/d", "http://example.invalid")
	cache := filepath.Join(c.dir, nesPlaylist, "index.txt")
	require.NoError(t, afero.WriteFile(fs, cache,
		[]byte("Good Game (USA)\n../../../etc/passwd\n"), 0o644))

	ix, err := c.index(t.Context(), nesPlaylist)

	require.NoError(t, err)
	_, ok := ix.exact["Good Game (USA)"]
	assert.True(t, ok, "a clean cached name is kept")
	_, ok = ix.exact["../../../etc/passwd"]
	assert.False(t, ok, "a traversal name from a tampered or pre-fix cache is dropped")
}

// A traversal name only appears once decoded: the raw href has no literal
// slash, so it must be rejected after unescaping, not before.
func TestParseIndexRejectsPathTraversal(t *testing.T) {
	t.Parallel()
	listing := []byte(`
<a href="Donkey%20Kong.png">Donkey Kong.png</a>
<a href="..%2F..%2F..%2Fetc%2Fpasswd.png">evil</a>
<a href="a%2Fb.png">evil2</a>
<a href="..png">dotdot</a>
`)

	names := parseIndex(listing)

	assert.Equal(t, []string{"Donkey Kong"}, names)
}

// Two rows that resolve to the same thumbnail name is not a corner case:
// region variants and title-slug matches often land on one name. Both
// downloads must succeed and the file must hold one writer's bytes whole,
// never a mix, and never leave the shared path unwritten.
func TestWriteFileHandlesConcurrentWritesToTheSamePath(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	c := newClient(fs, t.TempDir(), "http://example.invalid")
	path := filepath.Join(c.dir, "boxarts", "Donkey Kong.png")

	const writers = 8
	var wg sync.WaitGroup
	errs := make([]error, writers)
	for i := range writers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = c.writeFile(path, fmt.Appendf(nil, "payload-%d", i))
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		require.NoError(t, err, "writer %d", i)
	}
	data, err := afero.ReadFile(fs, path)
	require.NoError(t, err)
	assert.Regexp(t, `^payload-\d$`, string(data), "the file holds one writer's bytes whole, not a mix")

	entries, err := afero.ReadDir(fs, filepath.Dir(path))
	require.NoError(t, err)
	assert.Len(t, entries, 1, "no leftover .part files")
}
