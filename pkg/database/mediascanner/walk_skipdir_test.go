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
