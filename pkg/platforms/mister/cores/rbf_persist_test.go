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
			Path:      filepath.Join(string(filepath.Separator), "media", "fat", "_Console", "SNES_20240101.rbf"),
			Filename:  "SNES_20240101.rbf",
			ShortName: "SNES",
			MglName:   filepath.Join("_Console", "SNES"),
		},
		{
			Path:      filepath.Join(string(filepath.Separator), "media", "fat", "_Console", "NES_20240101.rbf"),
			Filename:  "NES_20240101.rbf",
			ShortName: "NES",
			MglName:   filepath.Join("_Console", "NES"),
		},
	}
}

func TestPersistedRBFCache_RoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, RBFCacheFileName)

	rbfs := sampleRBFs()
	manifest := []string{
		filepath.Join("_Console", "NES_20240101.rbf"),
		filepath.Join("_Console", "SNES_20240101.rbf"),
		"MyCustomCore.rbf",
	}

	require.NoError(t, writePersistedRBFCache(path, rbfs, manifest))

	loaded, ok, err := loadPersistedRBFCache(path)
	require.NoError(t, err)
	require.True(t, ok, "expected to load the file we just wrote")
	require.NotNil(t, loaded)
	assert.Equal(t, rbfCacheFileMagic, loaded.Magic)
	assert.Equal(t, rbfCacheFileVersion, loaded.Version)
	assert.Equal(t, rbfs, loaded.Files)
	assert.Equal(t, manifest, loaded.Manifest)
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

	require.NoError(t, writePersistedRBFCache(path, sampleRBFs(), []string{
		filepath.Join("_Console", "NES_20240101.rbf"),
		filepath.Join("_Console", "SNES_20240101.rbf"),
	}))

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

	require.NoError(t, writePersistedRBFCache(path, sampleRBFs(), []string{
		filepath.Join("_Console", "NES_20240101.rbf"),
		filepath.Join("_Console", "SNES_20240101.rbf"),
	}))

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

	require.NoError(t, writePersistedRBFCache(path, sampleRBFs()[:1], []string{"first.rbf"}))
	loaded, ok, err := loadPersistedRBFCache(path)
	require.NoError(t, err)
	require.True(t, ok)
	require.Len(t, loaded.Files, 1)

	require.NoError(t, writePersistedRBFCache(path, sampleRBFs(), []string{"second.rbf"}))
	loaded, ok, err = loadPersistedRBFCache(path)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Len(t, loaded.Files, 2, "second write should overwrite first")
	assert.Equal(t, []string{"second.rbf"}, loaded.Manifest)
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

func TestSnapshotDirMtimes_IncludesSpecialCoreDirs(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	raCoreDir := filepath.Join(root, "_RA_Cores", "Cores")
	lightGunDir := filepath.Join(root, "Light Gun")
	require.NoError(t, os.MkdirAll(raCoreDir, 0o750))
	require.NoError(t, os.MkdirAll(lightGunDir, 0o750))

	snapshot, err := snapshotDirMtimesAt(root)
	require.NoError(t, err)
	assert.Contains(t, snapshot, raCoreDir)
	assert.Contains(t, snapshot, lightGunDir)
}

func TestSnapshotRBFManifest_ShallowScanScope(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	consoleDir := filepath.Join(root, "_Console")
	customDir := filepath.Join(root, "_Custom")
	lightGunDir := filepath.Join(root, "Light Gun")
	raCoreDir := filepath.Join(root, "_RA_Cores", "Cores")
	ignoredDir := filepath.Join(root, "Games")
	for _, dir := range []string{consoleDir, customDir, lightGunDir, raCoreDir, ignoredDir} {
		require.NoError(t, os.MkdirAll(dir, 0o750))
	}

	writeRBF := func(path string) {
		require.NoError(t, os.WriteFile(path, nil, 0o600))
	}
	writeRBF(filepath.Join(root, "Arcade.rbf"))
	writeRBF(filepath.Join(consoleDir, "Saturn_20251003.rbf"))
	writeRBF(filepath.Join(customDir, "Custom.rbf"))
	writeRBF(filepath.Join(lightGunDir, "Sinden.rbf"))
	writeRBF(filepath.Join(raCoreDir, "RASNES.rbf"))
	writeRBF(filepath.Join(ignoredDir, "Ignored.rbf"))
	require.NoError(t, os.MkdirAll(filepath.Join(consoleDir, "Nested"), 0o750))
	writeRBF(filepath.Join(consoleDir, "Nested", "TooDeep.rbf"))
	require.NoError(t, os.WriteFile(filepath.Join(consoleDir, "readme.txt"), nil, 0o600))

	manifest, err := snapshotRBFManifestAt(root)
	require.NoError(t, err)
	assert.Equal(t, []string{
		"Arcade.rbf",
		filepath.Join("Light Gun", "Sinden.rbf"),
		filepath.Join("_Console", "Saturn_20251003.rbf"),
		filepath.Join("_Custom", "Custom.rbf"),
		filepath.Join("_RA_Cores", "Cores", "RASNES.rbf"),
	}, manifest)
}

func TestSnapshotRBFManifest_TracksSymlinkTarget(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	consoleDir := filepath.Join(root, "_Console")
	require.NoError(t, os.MkdirAll(consoleDir, 0o750))
	require.NoError(t, os.Symlink("Saturn_20251003.rbf", filepath.Join(consoleDir, "Saturn.rbf")))

	manifest, err := snapshotRBFManifestAt(root)
	require.NoError(t, err)
	assert.Equal(t, []string{
		filepath.Join("_Console", "Saturn.rbf") + " -> Saturn_20251003.rbf",
	}, manifest)
}

func TestRBFManifestsMatch(t *testing.T) {
	t.Parallel()

	assert.True(t, rbfManifestsMatch(nil, nil))
	saturn := filepath.Join("_Console", "Saturn.rbf")
	assert.True(t, rbfManifestsMatch([]string{saturn}, []string{saturn}))
	assert.False(t, rbfManifestsMatch(
		[]string{filepath.Join("_Console", "Saturn_old.rbf")},
		[]string{filepath.Join("_Console", "Saturn_new.rbf")},
	))
	assert.False(t, rbfManifestsMatch(nil, []string{saturn}))
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
	assert.True(t, cache.needsRescan, "unavailable manifest keeps scan marked stale")
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

func TestRBFCache_LoadFromDisk_PopulatesMaps(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		rootExists  bool
		storedMatch bool
		needsRescan bool
	}{
		{name: "matching manifest", rootExists: true, storedMatch: true},
		{name: "mismatching manifest", rootExists: true, needsRescan: true},
		{name: "snapshot error", needsRescan: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			root := filepath.Join(dir, "sd")
			manifest := []string(nil)
			if tt.rootExists {
				require.NoError(t, os.MkdirAll(root, 0o750))
				if !tt.storedMatch {
					manifest = []string{"missing.rbf"}
				}
			}
			path := filepath.Join(dir, RBFCacheFileName)
			require.NoError(t, writePersistedRBFCache(path, sampleRBFs(), manifest))

			cache := &RBFCache{sdRoot: root}
			cache.SetPersistPath(path)
			cache.Refresh()

			rbf, ok := cache.GetByShortName("snes")
			require.True(t, ok)
			assert.Equal(t, filepath.Join("_Console", "SNES"), rbf.MglName)
			assert.Equal(t, tt.needsRescan, cache.NeedsRescan())
		})
	}
}
