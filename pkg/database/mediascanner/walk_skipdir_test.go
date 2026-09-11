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

package mediascanner

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/charlievieth/fastwalk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWalkSkipDirFromFileKeepsSiblings pins the semantics the symlink-alias
// branch in GetFiles depends on. That branch returns filepath.SkipDir for an
// alias, and an alias normally sits in a directory alongside real media —
// Arcade Organizer output written straight into _Arcade, for instance. Under
// filepath.WalkDir, SkipDir returned for a non-directory skips the rest of the
// containing directory, which would silently drop every sibling after it.
//
// This asserts fastwalk does not do that, because the alias skip would
// otherwise be a media-loss bug rather than a de-duplication.
func TestWalkSkipDirFromFileKeepsSiblings(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	target := filepath.Join(root, "target.rom")
	require.NoError(t, os.WriteFile(target, []byte("x"), 0o600))
	// Sorts before the real entries, so a "skip the rest of this directory"
	// implementation would drop all of them.
	require.NoError(t, os.Symlink(target, filepath.Join(root, "00-alias.rom")))

	realNames := []string{"a-real.rom", "b-real.rom", "c-real.rom", "d-real.rom"}
	for _, n := range realNames {
		require.NoError(t, os.WriteFile(filepath.Join(root, n), []byte("x"), 0o600))
	}

	for _, workers := range []int{1, 4} {
		t.Run("workers", func(t *testing.T) {
			t.Parallel()
			var mu syncutil.Mutex
			var seen []string

			conf := &fastwalk.Config{Follow: true, NumWorkers: workers}
			err := fastwalk.Walk(conf, root, func(p string, d fs.DirEntry, walkErr error) error {
				// The fixture is a fresh temp dir, so a walk error here means
				// the test itself is broken rather than a condition to skip.
				require.NoError(t, walkErr)
				rel, relErr := filepath.Rel(root, p)
				require.NoError(t, relErr)
				if rel == "." {
					return nil
				}
				mu.Lock()
				seen = append(seen, rel)
				mu.Unlock()
				if d.Type()&os.ModeSymlink != 0 {
					return filepath.SkipDir
				}
				return nil
			})
			require.NoError(t, err)

			mu.Lock()
			sort.Strings(seen)
			mu.Unlock()

			for _, n := range realNames {
				assert.Contains(t, seen, n,
					"skipping an alias must not drop its siblings")
			}
			assert.Contains(t, seen, "target.rom")
		})
	}
}

// TestSkipEntry covers the three shapes an entry reaches the walk callback as.
// The regular-file case is the one that matters: on exFAT and FAT a symlink
// arrives typed as a regular file, and only nil skips it without taking the
// rest of its directory with it.
func TestSkipEntry(t *testing.T) {
	t.Parallel()

	tests := []struct {
		want error
		name string
		mode fs.FileMode
	}{
		{name: "directory", mode: fs.ModeDir, want: filepath.SkipDir},
		{name: "symlink", mode: fs.ModeSymlink, want: filepath.SkipDir},
		{name: "regular file", mode: 0, want: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, skipEntry(fakeDirEntry{name: "entry", mode: tt.mode}))
		})
	}
}

// walkReturningForAlias walks root, calling decide for the entry named
// aliasName and returning nil for everything else. It reports the entries the
// callback saw and any error handed back to the callback. SortLexical makes
// readdir order deterministic; the sentinel propagates identically under
// SortNone, which is what GetFiles uses.
func walkReturningForAlias(
	t *testing.T,
	root, aliasName string,
	workers int,
	decide func(fs.DirEntry) error,
) ([]string, []error) {
	t.Helper()

	var mu syncutil.Mutex
	var seen []string
	var walkErrs []error

	conf := &fastwalk.Config{Follow: true, NumWorkers: workers, Sort: fastwalk.SortLexical}
	err := fastwalk.Walk(conf, root, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			mu.Lock()
			walkErrs = append(walkErrs, walkErr)
			mu.Unlock()
			return nil
		}
		rel, relErr := filepath.Rel(root, p)
		require.NoError(t, relErr)
		if rel == "." {
			return nil
		}
		mu.Lock()
		seen = append(seen, rel)
		mu.Unlock()
		if rel == aliasName {
			return decide(d)
		}
		return nil
	})
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()
	sort.Strings(seen)
	return append([]string(nil), seen...), append([]error(nil), walkErrs...)
}

// aliasDirFixture builds a directory holding an entry that sorts first,
// followed by real media. On exFAT the alias would be a symlink the dirent
// reports as a regular file; a plain file reproduces the type fastwalk sees.
func aliasDirFixture(t *testing.T) (root, alias string, realNames []string) {
	t.Helper()

	root = t.TempDir()
	alias = "00-alias.rom"
	require.NoError(t, os.WriteFile(filepath.Join(root, alias), []byte("x"), 0o600))

	realNames = []string{"a-real.rom", "b-real.rom", "c-real.rom", "d-real.rom"}
	for _, n := range realNames {
		require.NoError(t, os.WriteFile(filepath.Join(root, n), []byte("x"), 0o600))
	}
	return root, alias, realNames
}

// TestWalkSkipDirFromUntypedEntryTruncatesDirectory pins the fastwalk
// behaviour skipEntry exists to work around. When the callback returns
// filepath.SkipDir for an entry fastwalk typed as a regular file, the sentinel
// escapes readDir, the rest of the directory is never visited, and it comes
// back to the callback as a walk error. If fastwalk ever handles this the way
// it handles typed symlinks, this test fails and skipEntry can go.
func TestWalkSkipDirFromUntypedEntryTruncatesDirectory(t *testing.T) {
	t.Parallel()

	root, alias, realNames := aliasDirFixture(t)

	seen, walkErrs := walkReturningForAlias(t, root, alias, 1, func(fs.DirEntry) error {
		return filepath.SkipDir
	})

	for _, n := range realNames {
		assert.NotContains(t, seen, n,
			"raw SkipDir on an untyped entry is expected to drop the rest of the directory")
	}
	require.Len(t, walkErrs, 1)
	assert.ErrorIs(t, walkErrs[0], filepath.SkipDir)
}

// TestSkipEntryKeepsSiblingsWhenDirentUntyped is the exFAT regression: the
// symlink-alias branch skips one entry and every sibling after it still gets
// scanned.
func TestSkipEntryKeepsSiblingsWhenDirentUntyped(t *testing.T) {
	t.Parallel()

	for _, workers := range []int{1, 4} {
		t.Run("workers", func(t *testing.T) {
			t.Parallel()

			root, alias, realNames := aliasDirFixture(t)
			seen, walkErrs := walkReturningForAlias(t, root, alias, workers, skipEntry)

			for _, n := range realNames {
				assert.Contains(t, seen, n,
					"skipping an alias must not drop its siblings")
			}
			assert.Empty(t, walkErrs)
		})
	}
}
