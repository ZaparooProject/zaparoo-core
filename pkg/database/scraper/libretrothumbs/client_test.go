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
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

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

// index.txt saved by a Windows editor ends every line in \r\n, and a stray
// space is easy to leave. Neither may stop a name matching.
func TestIndexTrimsEachCachedLine(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	c := newClient(fs, "/d", "http://example.invalid")
	cache := filepath.Join(c.dir, nesPlaylist, "index.txt")
	require.NoError(t, afero.WriteFile(fs, cache,
		[]byte("Super Smash Bros. - Smash Remix\r\nTrailing Space (USA) \r\n\r\n"), 0o644))

	ix, err := c.index(t.Context(), nesPlaylist)

	require.NoError(t, err)
	name, ok := ix.match("Super Smash Bros. - Smash Remix", "")
	require.True(t, ok)
	assert.Equal(t, "Super Smash Bros. - Smash Remix", name)
	name, ok = ix.match("Trailing Space (USA)", "")
	require.True(t, ok)
	assert.Equal(t, "Trailing Space (USA)", name)
}

// A listing the server can no longer give is not worth losing the one already
// cached just because a week has passed.
func TestIndexKeepsAnExpiredCacheWhenTheRefreshFails(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	c := newClient(fs, "/d", "http://127.0.0.1:1")
	cache := filepath.Join(c.dir, nesPlaylist, "index.txt")
	require.NoError(t, afero.WriteFile(fs, cache, []byte("Old Game (USA)\n"), 0o644))
	c.now = func() time.Time { return time.Now().Add(indexMaxAge + time.Hour) }

	ix, err := c.index(t.Context(), nesPlaylist)

	require.NoError(t, err)
	_, ok := ix.match("Old Game (USA)", "")
	assert.True(t, ok)
}

func TestIndexFailsWhenNothingIsCachedOrLocal(t *testing.T) {
	t.Parallel()
	c := newClient(afero.NewMemMapFs(), "/d", "http://127.0.0.1:1")

	_, err := c.index(t.Context(), nesPlaylist)

	require.Error(t, err)
}

// Local names join the listing without being written into the cache, which
// stays a mirror of the server.
func TestIndexAddsLocalThumbnailsWithoutCachingThem(t *testing.T) {
	t.Parallel()
	server := &fakeServer{images: map[string]bool{"Named_Boxarts/A (USA)": true}}
	ts := httptest.NewServer(server)
	t.Cleanup(ts.Close)
	fs := afero.NewMemMapFs()
	c := newClient(fs, "/d", ts.URL)
	require.NoError(t, afero.WriteFile(fs, filepath.Join(c.dir, nesPlaylist, "Named_Snaps", "Mine.png"),
		[]byte("\x89PNG\r\n\x1a\n"), 0o644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(c.dir, nesPlaylist, "Named_Snaps", "Empty.png"),
		nil, 0o644))

	ix, err := c.index(t.Context(), nesPlaylist)

	require.NoError(t, err)
	for _, name := range []string{"A (USA)", "Mine"} {
		_, ok := ix.exact[name]
		assert.True(t, ok, name)
	}
	_, ok := ix.exact["Empty"]
	assert.False(t, ok, "an empty file is not a thumbnail")
	cached, err := afero.ReadFile(fs, filepath.Join(c.dir, nesPlaylist, "index.txt"))
	require.NoError(t, err)
	assert.Equal(t, "A (USA)\n", string(cached))
}

// A user's copy of a thumbnail is the listed one even when the file name
// differs from the listing in letter case; it must be used, not downloaded
// again over the user's copy or, offline, not at all.
func TestImageUsesLocalFileThatDiffersOnlyInCase(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	c := newClient(fs, "/d", "http://127.0.0.1:1")
	local := filepath.Join(c.dir, nesPlaylist, "Named_Boxarts", "foo bar (usa).png")
	require.NoError(t, afero.WriteFile(fs, local, []byte("\x89PNG\r\n\x1a\nmine"), 0o644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(c.dir, nesPlaylist, "index.txt"),
		[]byte("Foo Bar (USA)\n"), 0o644))
	ix, err := c.index(t.Context(), nesPlaylist)
	require.NoError(t, err)
	name, ok := ix.match("Foo Bar (USA)", "")
	require.True(t, ok)
	require.Equal(t, "Foo Bar (USA)", name, "the listed spelling wins the match")

	got, err := c.image(t.Context(), nesPlaylist, "Named_Boxarts", name, false)

	require.NoError(t, err, "the unreachable server is never asked")
	assert.Equal(t, local, got)

	_, err = c.image(t.Context(), nesPlaylist, "Named_Snaps", name, false)
	require.Error(t, err, "the case-folded lookup stays inside the one kind's folder")
}
