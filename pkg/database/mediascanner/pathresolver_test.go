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
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// countingResolver wraps a resolver's directory reads with a per-path counter.
func countingResolver() (resolver *pathResolver, counts *sync.Map) {
	resolver = newPathResolver()
	counts = &sync.Map{}
	base := resolver.readDirFn
	resolver.readDirFn = func(ctx context.Context, path string) ([]os.DirEntry, error) {
		count, _ := counts.LoadOrStore(path, &atomic.Int64{})
		if counter, ok := count.(*atomic.Int64); ok {
			counter.Add(1)
		}
		return base(ctx, path)
	}
	return resolver, counts
}

func readCount(counts *sync.Map, path string) int64 {
	count, ok := counts.Load(path)
	if !ok {
		return 0
	}
	counter, ok := count.(*atomic.Int64)
	if !ok {
		return 0
	}
	return counter.Load()
}

func TestPathResolverRetriesFailedDirectoryRead(t *testing.T) {
	t.Parallel()

	readErr := errors.New("transient read failure")
	resolver := newPathResolver()
	var calls atomic.Int64
	resolver.readDirFn = func(context.Context, string) ([]os.DirEntry, error) {
		if calls.Add(1) == 1 {
			return nil, readErr
		}
		return []os.DirEntry{}, nil
	}

	_, err := resolver.readDir(context.Background(), "transient")
	require.ErrorIs(t, err, readErr)
	entries, err := resolver.readDir(context.Background(), "transient")
	require.NoError(t, err)
	assert.Empty(t, entries)
	_, err = resolver.readDir(context.Background(), "transient")
	require.NoError(t, err)
	assert.EqualValues(t, 2, calls.Load(), "successful retry must be memoized")
}

// TestPathResolverMemoizesSharedParents pins the memoization: probing many
// sibling paths must list their shared parent directories exactly once.
func TestPathResolverMemoizesSharedParents(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	for _, name := range []string{"NES", "SNES", "Genesis"} {
		require.NoError(t, os.MkdirAll(filepath.Join(root, "games", name), 0o750))
	}

	resolver, counts := countingResolver()
	ctx := context.Background()
	for _, name := range []string{"NES", "SNES", "Genesis", "Missing"} {
		resolved, err := resolver.findPath(ctx, filepath.Join(root, "games", name))
		if name == "Missing" {
			require.Error(t, err)
			continue
		}
		require.NoError(t, err)
		assert.Equal(t, filepath.Join(root, "games", name), resolved)
	}

	assert.EqualValues(t, 1, readCount(counts, root), "shared root listed once")
	assert.EqualValues(t, 1, readCount(counts, filepath.Join(root, "games")), "shared parent listed once")

	// A repeated probe is served entirely from the resolution memo.
	before := readCount(counts, filepath.Join(root, "games"))
	_, err := resolver.findPath(ctx, filepath.Join(root, "games", "NES"))
	require.NoError(t, err)
	assert.Equal(t, before, readCount(counts, filepath.Join(root, "games")))
}

// TestPathResolverConcurrentProbesShareReads pins the concurrent contract used
// by root validation: parallel probes of paths under one parent produce one
// listing of that parent and consistent results. Run with -race in CI.
func TestPathResolverConcurrentProbesShareReads(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	names := []string{"a", "b", "c", "d", "e", "f", "g", "h"}
	for _, name := range names {
		require.NoError(t, os.MkdirAll(filepath.Join(root, name), 0o750))
	}

	resolver, counts := countingResolver()
	ctx := context.Background()
	var wg sync.WaitGroup
	errs := make([]error, len(names))
	for i, name := range names {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = resolver.findPath(ctx, filepath.Join(root, name))
		}()
	}
	wg.Wait()

	for i, err := range errs {
		require.NoError(t, err, names[i])
	}
	assert.EqualValues(t, 1, readCount(counts, root), "concurrent probes share one parent listing")
}

func skipUnlessCaseSensitive(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("needs a case-sensitive temp directory")
	}
}

// TestPathResolverFoldsCaseWhenExactPathMissing is the regression for system
// folders whose on-disk case differs from the launcher's: on a case-sensitive
// filesystem the exact stat of roms/snes fails, and discovery must still find
// roms/SNES through the memoized listings.
func TestPathResolverFoldsCaseWhenExactPathMissing(t *testing.T) {
	t.Parallel()
	skipUnlessCaseSensitive(t)

	root := t.TempDir()
	roms := filepath.Join(root, "roms")
	require.NoError(t, os.MkdirAll(filepath.Join(roms, "SNES"), 0o750))
	require.NoError(t, os.MkdirAll(filepath.Join(roms, "NES"), 0o750))

	resolver, counts := countingResolver()
	ctx := context.Background()

	resolved, err := resolver.findPath(ctx, filepath.Join(root, "ROMS", "snes"))
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(roms, "SNES"), resolved)

	resolved, err = resolver.findPath(ctx, filepath.Join(roms, "nes"))
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(roms, "NES"), resolved)

	_, err = resolver.findPath(ctx, filepath.Join(roms, "genesis"))
	require.Error(t, err)
	_, err = resolver.findPath(ctx, filepath.Join(roms, "missing", "deeper"))
	require.Error(t, err)

	assert.EqualValues(t, 1, readCount(counts, root), "folding walks reuse the memoized root listing")
	assert.EqualValues(t, 1, readCount(counts, roms), "folding walks reuse the memoized parent listing")
}

// TestFindPathOneOffKeepsExactExistenceCheck pins that only the discovery pass
// folds case: a direct FindPath caller still gets an error on a case mismatch.
func TestFindPathOneOffKeepsExactExistenceCheck(t *testing.T) {
	t.Parallel()
	skipUnlessCaseSensitive(t)

	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "roms", "SNES"), 0o750))

	_, err := FindPath(context.Background(), filepath.Join(root, "roms", "snes"))
	require.Error(t, err)

	resolved, err := FindPath(context.Background(), filepath.Join(root, "roms", "SNES"))
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(root, "roms", "SNES"), resolved)
}

// TestPathResolverCaseVariantsPreferExactThenDeterministic covers coexisting
// case variants: an exact match always wins, and a probe matching none of
// them exactly picks the same variant whatever order the listing returns.
func TestPathResolverCaseVariantsPreferExactThenDeterministic(t *testing.T) {
	t.Parallel()
	skipUnlessCaseSensitive(t)

	root := t.TempDir()
	for _, name := range []string{"snes", "SNES", "Snes"} {
		require.NoError(t, os.MkdirAll(filepath.Join(root, name), 0o750))
	}

	for _, reverse := range []bool{false, true} {
		resolver := newPathResolver()
		if reverse {
			resolver.readDirFn = func(ctx context.Context, path string) ([]os.DirEntry, error) {
				entries, err := readDirWithContext(ctx, path)
				slices.Reverse(entries)
				return entries, err
			}
		}
		ctx := context.Background()

		for _, name := range []string{"snes", "SNES", "Snes"} {
			resolved, err := resolver.findPath(ctx, filepath.Join(root, name))
			require.NoError(t, err)
			assert.Equal(t, filepath.Join(root, name), resolved, "exact match wins (reverse=%v)", reverse)
		}

		resolved, err := resolver.findPath(ctx, filepath.Join(root, "sNES"))
		require.NoError(t, err)
		assert.Equal(t, filepath.Join(root, "SNES"), resolved, "smallest variant wins (reverse=%v)", reverse)
	}
}

// TestGetSystemPathsMatchesFolderCaseInsensitively runs the same regression
// through path discovery: a launcher folder "snes" finds an on-disk "SNES".
func TestGetSystemPathsMatchesFolderCaseInsensitively(t *testing.T) {
	t.Parallel()
	skipUnlessCaseSensitive(t)

	root := t.TempDir()
	snesDir := filepath.Join(root, "SNES")
	require.NoError(t, os.MkdirAll(snesDir, 0o750))

	cache := &helpers.LauncherCache{}
	cache.InitializeFromSlice([]platforms.Launcher{{
		ID:         "snes-launcher",
		SystemID:   systemdefs.SystemSNES,
		Folders:    []string{"snes"},
		Extensions: []string{".sfc"},
	}})

	results := getSystemPathsForLauncherCache(
		context.Background(),
		[]string{root},
		[]systemdefs.System{{ID: systemdefs.SystemSNES}},
		cache,
	)
	require.Len(t, results, 1)
	assert.Equal(t, systemdefs.SystemSNES, results[0].System.ID)
	assert.Equal(t, snesDir, results[0].Path)
}
