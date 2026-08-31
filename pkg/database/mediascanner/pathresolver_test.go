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
	"sync"
	"sync/atomic"
	"testing"

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
