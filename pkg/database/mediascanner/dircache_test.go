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
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDirCache_CachesResults(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "file.txt"), []byte("x"), 0o600))

	cache := newDirCache()
	ctx := context.Background()

	entries1, err := cache.list(ctx, dir)
	require.NoError(t, err)
	require.Len(t, entries1, 1)

	// Add another file — cached result should still show 1 entry
	require.NoError(t, os.WriteFile(filepath.Join(dir, "file2.txt"), []byte("y"), 0o600))

	entries2, err := cache.list(ctx, dir)
	require.NoError(t, err)
	assert.Len(t, entries2, 1, "should return cached result, not re-read directory")
}

func TestDirCache_FindEntry_ExactMatch(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, "NES"), 0o750))

	cache := newDirCache()
	ctx := context.Background()

	name, err := cache.findEntry(ctx, dir, "NES")
	require.NoError(t, err)
	assert.Equal(t, "NES", name)
}

func TestDirCache_FindEntry_CaseInsensitive(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, "SNES"), 0o750))

	cache := newDirCache()
	ctx := context.Background()

	name, err := cache.findEntry(ctx, dir, "snes")
	require.NoError(t, err)
	assert.Equal(t, "SNES", name)
}

func TestDirCache_FindEntry_PrefersExactOnLinux(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("exact-match preference only applies on Linux")
	}

	dir := t.TempDir()
	// Create both cases — Linux allows this on case-sensitive filesystems
	require.NoError(t, os.Mkdir(filepath.Join(dir, "Games"), 0o750))
	require.NoError(t, os.Mkdir(filepath.Join(dir, "games"), 0o750))

	cache := newDirCache()
	ctx := context.Background()

	name, err := cache.findEntry(ctx, dir, "Games")
	require.NoError(t, err)
	assert.Equal(t, "Games", name)

	name, err = cache.findEntry(ctx, dir, "games")
	require.NoError(t, err)
	assert.Equal(t, "games", name)
}

func TestDirCache_FindEntry_NotFound(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, "NES"), 0o750))

	cache := newDirCache()
	ctx := context.Background()

	name, err := cache.findEntry(ctx, dir, "GENESIS")
	require.NoError(t, err)
	assert.Empty(t, name)
}

func TestDirCache_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	cache := newDirCache()

	_, err := cache.list(ctx, t.TempDir())
	require.Error(t, err)
	require.ErrorIs(t, err, context.Canceled)

	_, err = cache.findEntry(ctx, t.TempDir(), "anything")
	require.Error(t, err)
	require.ErrorIs(t, err, context.Canceled)
}
