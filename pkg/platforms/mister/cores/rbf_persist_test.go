//go:build linux

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

package cores

import (
	"encoding/gob"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func sampleRBFs() []RBFInfo {
	return []RBFInfo{
		{
			Path:      "/media/fat/_Console/SNES_20240101.rbf",
			Filename:  "SNES_20240101.rbf",
			ShortName: "SNES",
			MglName:   "_Console/SNES",
		},
		{
			Path:      "/media/fat/_Console/NES_20240101.rbf",
			Filename:  "NES_20240101.rbf",
			ShortName: "NES",
			MglName:   "_Console/NES",
		},
	}
}

func TestPersistedRBFCache_RoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, RBFCacheFileName)

	rbfs := sampleRBFs()
	mtimes := map[string]int64{
		"/media/fat/_Console":  1234567891,
		"/media/fat/_Computer": 1234567892,
	}
	rootRBFs := []string{"MyCustomCore.rbf", "Userport.rbf"}

	require.NoError(t, writePersistedRBFCache(path, rbfs, mtimes, rootRBFs))

	loaded, ok, err := loadPersistedRBFCache(path)
	require.NoError(t, err)
	require.True(t, ok, "expected to load the file we just wrote")
	require.NotNil(t, loaded)
	assert.Equal(t, rbfCacheFileMagic, loaded.Magic)
	assert.Equal(t, rbfCacheFileVersion, loaded.Version)
	assert.Equal(t, rbfs, loaded.Files)
	assert.Equal(t, mtimes, loaded.DirMtimes)
	assert.Equal(t, rootRBFs, loaded.RootRBFs)
}

func TestPersistedRBFCache_MissingFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, RBFCacheFileName)

	loaded, ok, err := loadPersistedRBFCache(path)
	require.NoError(t, err)
	assert.False(t, ok)
	assert.Nil(t, loaded)
}

func TestPersistedRBFCache_BadMagic(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, RBFCacheFileName)

	f, err := os.Create(path) //nolint:gosec // test-controlled path
	require.NoError(t, err)
	require.NoError(t, gob.NewEncoder(f).Encode(&persistedRBFCache{
		Magic:   "WRONG",
		Version: rbfCacheFileVersion,
	}))
	require.NoError(t, f.Close())

	loaded, ok, err := loadPersistedRBFCache(path)
	require.NoError(t, err)
	assert.False(t, ok, "wrong magic should be a graceful fallback")
	assert.Nil(t, loaded)
}

func TestPersistedRBFCache_VersionMismatch(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, RBFCacheFileName)

	f, err := os.Create(path) //nolint:gosec // test-controlled path
	require.NoError(t, err)
	require.NoError(t, gob.NewEncoder(f).Encode(&persistedRBFCache{
		Magic:   rbfCacheFileMagic,
		Version: rbfCacheFileVersion + 99,
		Files:   sampleRBFs(),
	}))
	require.NoError(t, f.Close())

	loaded, ok, err := loadPersistedRBFCache(path)
	require.NoError(t, err)
	assert.False(t, ok, "version mismatch should be a graceful fallback")
	assert.Nil(t, loaded)
}

// TestPersistedRBFCache_TruncatedFile simulates a crash mid-write.
func TestPersistedRBFCache_TruncatedFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, RBFCacheFileName)

	require.NoError(t, writePersistedRBFCache(path, sampleRBFs(), map[string]int64{
		"/media/fat/_Console": 1,
	}, nil))

	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Greater(t, info.Size(), int64(8))
	require.NoError(t, os.Truncate(path, 8))

	loaded, ok, err := loadPersistedRBFCache(path)
	require.NoError(t, err, "truncated file should be a graceful fallback")
	assert.False(t, ok)
	assert.Nil(t, loaded)
}

// TestPersistedRBFCache_OversizedFile validates the io.LimitReader cap.
func TestPersistedRBFCache_OversizedFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, RBFCacheFileName)

	require.NoError(t, writePersistedRBFCache(path, sampleRBFs(), map[string]int64{
		"/media/fat/_Console": 1,
	}, nil))

	originalCap := rbfCacheMaxBytes
	rbfCacheMaxBytes = 16
	t.Cleanup(func() { rbfCacheMaxBytes = originalCap })

	loaded, ok, err := loadPersistedRBFCache(path)
	require.NoError(t, err, "oversized file should fall back gracefully")
	assert.False(t, ok)
	assert.Nil(t, loaded)
}

func TestPersistedRBFCache_AtomicRenameOverwrites(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, RBFCacheFileName)

	require.NoError(t, writePersistedRBFCache(path, sampleRBFs()[:1], map[string]int64{
		"/media/fat/_Console": 1,
	}, []string{"first.rbf"}))
	loaded, ok, err := loadPersistedRBFCache(path)
	require.NoError(t, err)
	require.True(t, ok)
	require.Len(t, loaded.Files, 1)

	require.NoError(t, writePersistedRBFCache(path, sampleRBFs(), map[string]int64{
		"/media/fat/_Console": 2,
	}, []string{"second.rbf"}))
	loaded, ok, err = loadPersistedRBFCache(path)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Len(t, loaded.Files, 2, "second write should overwrite first")
	assert.Equal(t, int64(2), loaded.DirMtimes["/media/fat/_Console"])
	assert.Equal(t, []string{"second.rbf"}, loaded.RootRBFs)
}

func TestDirMtimesMatch_EmptySnapshot(t *testing.T) {
	t.Parallel()
	// An empty snapshot is always treated as stale so we don't trust a
	// half-written file.
	assert.False(t, dirMtimesMatch(nil))
	assert.False(t, dirMtimesMatch(map[string]int64{}))
}

func TestDirMtimesMatch_MissingPath(t *testing.T) {
	t.Parallel()
	// A snapshot referencing a path that doesn't exist signals drift.
	assert.False(t, dirMtimesMatch(map[string]int64{
		"/this/path/should/not/exist/zaparoo-rbf-test": 1,
	}))
}

func TestDiffDirMtimes_AllMatching(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	info, err := os.Stat(dir)
	require.NoError(t, err)

	diffs := diffDirMtimes(map[string]int64{dir: info.ModTime().UnixNano()})
	assert.Empty(t, diffs)
}

func TestDiffDirMtimes_DriftedPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	info, err := os.Stat(dir)
	require.NoError(t, err)
	now := info.ModTime().UnixNano()

	// Pretend the snapshot was taken 5 seconds before the live mtime.
	snapshotMtime := now - 5_000_000_000
	diffs := diffDirMtimes(map[string]int64{dir: snapshotMtime})

	require.Len(t, diffs, 1)
	assert.Equal(t, dir, diffs[0].Path)
	assert.Equal(t, "drifted", diffs[0].Status)
	assert.Equal(t, snapshotMtime, diffs[0].SnapshotMtimeNs)
	assert.Equal(t, now, diffs[0].CurrentMtimeNs)
	// Allow a tiny rounding window — the value is integer ns / 1e6.
	assert.InDelta(t, 5000, diffs[0].DeltaMs, 1)
}

func TestDiffDirMtimes_MissingPath(t *testing.T) {
	t.Parallel()
	missing := "/this/path/should/not/exist/zaparoo-rbf-diff-test"
	diffs := diffDirMtimes(map[string]int64{missing: 12345})

	require.Len(t, diffs, 1)
	assert.Equal(t, missing, diffs[0].Path)
	assert.Equal(t, "missing", diffs[0].Status)
	assert.Equal(t, int64(12345), diffs[0].SnapshotMtimeNs)
	assert.Zero(t, diffs[0].CurrentMtimeNs)
}

func TestSnapshotDirMtimes_IncludesRetroAchievementsCoreDir(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	raCoreDir := filepath.Join(root, "_RA_Cores", "Cores")
	require.NoError(t, os.MkdirAll(raCoreDir, 0o750))

	snapshot, err := snapshotDirMtimesAt(root)
	require.NoError(t, err)
	assert.Contains(t, snapshot, raCoreDir)
}

func TestRootRBFsMatch_NoFilesEqualsEmptySnapshot(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	assert.True(t, rootRBFsMatchAt(root, nil), "empty live + empty snapshot should match")
	assert.True(t, rootRBFsMatchAt(root, []string{}), "empty live + empty snapshot should match")
}

func TestRootRBFsMatch_AddedAtRoot(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "NewCore.rbf"), []byte{}, 0o600))
	assert.False(t, rootRBFsMatchAt(root, nil), "a new RBF at root should be detected")
}

func TestRootRBFsMatch_RemovedFromRoot(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	assert.False(t, rootRBFsMatchAt(root, []string{"GoneCore.rbf"}),
		"a removed RBF should be detected")
}

func TestRootRBFsMatch_IgnoresNonRBFRootFiles(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "MiSTer.ini"), []byte{}, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "boot.log"), []byte{}, 0o600))
	assert.True(t, rootRBFsMatchAt(root, nil),
		"unrelated files at SD root must not invalidate the cache")
}

func TestDiffRootRBFs_AddedAndRemoved(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "Kept.rbf"), []byte{}, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "Added.rbf"), []byte{}, 0o600))

	diff := diffRootRBFsAt(root, []string{"Kept.rbf", "Removed.rbf"})
	assert.Equal(t, []string{"Added.rbf"}, diff.Added)
	assert.Equal(t, []string{"Removed.rbf"}, diff.Removed)
}

// TestRBFCache_NoPersistPath_ScanFlowsThrough verifies that with no persist
// path the existing scan-only behaviour still works (regardless of whether
// /media/fat exists on the test host).
func TestRBFCache_NoPersistPath_ScanFlowsThrough(t *testing.T) {
	t.Parallel()
	cache := &RBFCache{}
	cache.Refresh()

	cache.mu.RLock()
	defer cache.mu.RUnlock()
	assert.True(t, cache.initialized, "Refresh must mark cache initialized")
	assert.False(t, cache.needsRescan, "scan path resets needsRescan")
	assert.Empty(t, cache.persistPath, "no persist path was configured")
}

// TestRBFCache_SetPersistPath sets and reads back the path under the lock.
func TestRBFCache_SetPersistPath(t *testing.T) {
	t.Parallel()
	cache := &RBFCache{}
	cache.SetPersistPath("/tmp/example.gob")

	cache.mu.RLock()
	got := cache.persistPath
	cache.mu.RUnlock()
	assert.Equal(t, "/tmp/example.gob", got)
}

// TestRBFCache_LoadFromDisk_PopulatesMaps verifies that a persisted file is
// decoded and BuildFromRBFs is run, populating bySystemID for known systems.
func TestRBFCache_LoadFromDisk_PopulatesMaps(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, RBFCacheFileName)

	rbfs := sampleRBFs()
	require.NoError(t, writePersistedRBFCache(path, rbfs, map[string]int64{
		"/this/path/does/not/exist": 1, // forces dirMtimesMatch=false → needsRescan=true
	}, nil))

	cache := &RBFCache{}
	cache.SetPersistPath(path)
	cache.Refresh()

	// We don't assert specific systems — that depends on the Systems table
	// containing entries with RBF "_Console/SNES" or "_Console/NES". What we
	// CAN assert is that byShortName was populated from the persisted file
	// and that the drift was detected.
	rbf, ok := cache.GetByShortName("snes")
	assert.True(t, ok, "SNES should be loaded from the persisted file")
	assert.Equal(t, "_Console/SNES", rbf.MglName)

	assert.True(t, cache.NeedsRescan(),
		"bogus snapshot path must mark needsRescan=true")
}
