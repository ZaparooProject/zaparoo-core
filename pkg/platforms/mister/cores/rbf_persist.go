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
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/config"
	"github.com/rs/zerolog/log"
)

// RBFCacheFileName is the on-disk filename used by the persisted cache,
// resolved against the data directory by the caller of SetPersistPath.
const RBFCacheFileName = "rbf_cache.gob"

const (
	rbfCacheFileMagic   = "zrbf"
	rbfCacheFileVersion = 2
)

// rbfCacheMaxBytes caps gob input at load time. ~200 RBFs × ~256 B per
// record ≈ 50 KiB realistic; the cap is generous to absorb future field
// additions without forcing a version bump. Var (not const) so tests can
// lower it to exercise the oversize-fallback path.
var rbfCacheMaxBytes int64 = 2 << 20

// persistedRBFCache is the on-disk shape. DirMtimes maps each scanned
// `_*` subdirectory to its ModTime().UnixNano() at scan time; drift
// signals a core was added or removed under that subdir. RootRBFs is the
// sorted list of `.rbf` filenames placed directly at SD root — used in
// place of an `/media/fat` mtime check, which proved too noisy because
// boot scripts and other writes touch the SD root unrelated to RBFs.
type persistedRBFCache struct {
	DirMtimes map[string]int64
	Magic     string
	Files     []RBFInfo
	RootRBFs  []string
	Version   int
}

// snapshotDirMtimes captures the current mtime for each `_*` subdirectory
// of the SD root. Those are the only directories whose mtime tracks core
// presence: any add/remove/rename of an RBF inside `_Console` (etc.)
// mutates the dir mtime. The SD root itself is intentionally excluded —
// its mtime drifts from boot scripts and other unrelated writes — and
// root-level RBFs are tracked separately via snapshotRootRBFs.
func snapshotDirMtimes() (map[string]int64, error) {
	return snapshotDirMtimesAt(config.SDRootDir)
}

func snapshotDirMtimesAt(root string) (map[string]int64, error) {
	snapshot := make(map[string]int64)

	files, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("readdir SD root: %w", err)
	}
	for _, f := range files {
		if !f.IsDir() || !strings.HasPrefix(f.Name(), "_") {
			continue
		}
		sub := filepath.Join(root, f.Name())
		info, statErr := os.Stat(sub)
		if statErr != nil {
			continue
		}
		snapshot[sub] = info.ModTime().UnixNano()
	}
	return snapshot, nil
}

// snapshotRootRBFs returns the sorted list of `.rbf` filenames placed
// directly at SD root. Replaces an mtime check on the SD root itself,
// which was too noisy to be useful.
//
// Only direct SD-root files are captured here; RBFs nested under `_*`
// directories are tracked transitively via snapshotDirMtimes. This matches
// MiSTer's convention that cores live at SD root or under top-level `_*`
// folders — a `.rbf` placed inside a brand-new non-`_*` top-level
// directory would not be detected.
func snapshotRootRBFs() ([]string, error) {
	return snapshotRootRBFsAt(config.SDRootDir)
}

func snapshotRootRBFsAt(root string) ([]string, error) {
	files, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("readdir SD root: %w", err)
	}
	rbfs := make([]string, 0)
	for _, f := range files {
		if f.IsDir() {
			continue
		}
		if filepath.Ext(strings.ToLower(f.Name())) != ".rbf" {
			continue
		}
		rbfs = append(rbfs, f.Name())
	}
	sort.Strings(rbfs)
	return rbfs, nil
}

// dirMtimeDiff describes a single divergence between a stored mtime
// snapshot and the live filesystem state. Status is one of "drifted"
// (path still exists with a different mtime), "missing" (path no longer
// exists), or "added" (a new `_*` dir present that wasn't in the
// snapshot). Mtimes are reported as Unix nanoseconds; SnapshotMtimeNs is
// zero for "added" entries and CurrentMtimeNs is zero for "missing".
type dirMtimeDiff struct {
	Path            string
	Status          string
	SnapshotMtimeNs int64
	CurrentMtimeNs  int64
	DeltaMs         int64
}

// rootRBFsDiff describes the divergence between a stored sorted list of
// root-level RBF filenames and the live state. Either or both slices may
// be empty when there is no divergence in that direction.
type rootRBFsDiff struct {
	Added   []string
	Removed []string
}

// rootRBFsMatch reports whether the stored sorted list of root-level RBF
// filenames is identical to the live filesystem state. A read failure on
// the SD root is treated as a mismatch.
func rootRBFsMatch(stored []string) bool {
	return rootRBFsMatchAt(config.SDRootDir, stored)
}

func rootRBFsMatchAt(root string, stored []string) bool {
	current, err := snapshotRootRBFsAt(root)
	if err != nil {
		return false
	}
	if len(current) != len(stored) {
		return false
	}
	for i, name := range stored {
		if current[i] != name {
			return false
		}
	}
	return true
}

// diffRootRBFs returns the names added to and removed from the live
// filesystem relative to the stored snapshot. Returns an empty diff when
// the live filesystem can't be read, so the caller can still log the
// stale state without crashing on the diagnostic path.
func diffRootRBFs(stored []string) rootRBFsDiff {
	return diffRootRBFsAt(config.SDRootDir, stored)
}

func diffRootRBFsAt(root string, stored []string) rootRBFsDiff {
	current, err := snapshotRootRBFsAt(root)
	if err != nil {
		return rootRBFsDiff{}
	}
	storedSet := make(map[string]struct{}, len(stored))
	for _, n := range stored {
		storedSet[n] = struct{}{}
	}
	currentSet := make(map[string]struct{}, len(current))
	for _, n := range current {
		currentSet[n] = struct{}{}
	}
	var diff rootRBFsDiff
	for _, n := range current {
		if _, ok := storedSet[n]; !ok {
			diff.Added = append(diff.Added, n)
		}
	}
	for _, n := range stored {
		if _, ok := currentSet[n]; !ok {
			diff.Removed = append(diff.Removed, n)
		}
	}
	return diff
}

// diffDirMtimes returns the per-path differences between snapshot and the
// live filesystem state. Returns nil when everything matches. Used as a
// diagnostic when dirMtimesMatch reports false so we can see *which*
// paths drifted and by how much.
func diffDirMtimes(snapshot map[string]int64) []dirMtimeDiff {
	var diffs []dirMtimeDiff
	for p, want := range snapshot {
		info, err := os.Stat(p)
		if err != nil {
			diffs = append(diffs, dirMtimeDiff{
				Path:            p,
				Status:          "missing",
				SnapshotMtimeNs: want,
			})
			continue
		}
		got := info.ModTime().UnixNano()
		if got != want {
			diffs = append(diffs, dirMtimeDiff{
				Path:            p,
				Status:          "drifted",
				SnapshotMtimeNs: want,
				CurrentMtimeNs:  got,
				DeltaMs:         (got - want) / 1_000_000,
			})
		}
	}
	files, err := os.ReadDir(config.SDRootDir)
	if err == nil {
		for _, f := range files {
			if !f.IsDir() || !strings.HasPrefix(f.Name(), "_") {
				continue
			}
			sub := filepath.Join(config.SDRootDir, f.Name())
			if _, ok := snapshot[sub]; ok {
				continue
			}
			info, statErr := os.Stat(sub)
			if statErr != nil {
				continue
			}
			diffs = append(diffs, dirMtimeDiff{
				Path:           sub,
				Status:         "added",
				CurrentMtimeNs: info.ModTime().UnixNano(),
			})
		}
	}
	return diffs
}

// dirMtimesMatch reports whether the live filesystem state matches the
// snapshot. Any of these makes the cache stale: a snapshotted dir is gone
// or has a different mtime; a new `_*` dir exists that wasn't in the
// snapshot. An empty snapshot is always considered stale so we don't trust
// a half-written file.
func dirMtimesMatch(snapshot map[string]int64) bool {
	if len(snapshot) == 0 {
		return false
	}
	for p, want := range snapshot {
		info, err := os.Stat(p)
		if err != nil {
			return false
		}
		if info.ModTime().UnixNano() != want {
			return false
		}
	}
	files, err := os.ReadDir(config.SDRootDir)
	if err != nil {
		return false
	}
	for _, f := range files {
		if !f.IsDir() || !strings.HasPrefix(f.Name(), "_") {
			continue
		}
		sub := filepath.Join(config.SDRootDir, f.Name())
		if _, ok := snapshot[sub]; !ok {
			return false
		}
	}
	return true
}

// loadPersistedRBFCache reads path and validates the magic and version.
// Returns (cache, true, nil) on a usable file, (nil, false, nil) for
// missing/truncated/wrong-magic/wrong-version files (caller should fall
// back to a scan), or (nil, false, err) on other I/O or decode errors.
func loadPersistedRBFCache(path string) (*persistedRBFCache, bool, error) {
	f, err := os.Open(path) //nolint:gosec // path is derived from the data dir
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("open RBF cache: %w", err)
	}
	defer func() { _ = f.Close() }()

	var stored persistedRBFCache
	if decErr := gob.NewDecoder(io.LimitReader(f, rbfCacheMaxBytes)).Decode(&stored); decErr != nil {
		if errors.Is(decErr, io.EOF) || errors.Is(decErr, io.ErrUnexpectedEOF) {
			log.Warn().Str("path", path).Msg("RBF cache file truncated, falling back to scan")
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("decode RBF cache: %w", decErr)
	}

	if stored.Magic != rbfCacheFileMagic {
		log.Warn().Str("path", path).Msg("RBF cache file has wrong magic, falling back to scan")
		return nil, false, nil
	}
	if stored.Version != rbfCacheFileVersion {
		log.Info().
			Int("file_version", stored.Version).
			Int("expected", rbfCacheFileVersion).
			Msg("RBF cache file version mismatch, falling back to scan")
		return nil, false, nil
	}
	return &stored, true, nil
}

// writePersistedRBFCache encodes the cache and renames into place. Atomic
// against concurrent readers; the directory entry change itself is not
// fsynced, so a hard power-off between rename and the next sync can lose
// the file. Recovery is automatic: a missing or truncated file falls back
// to a scan on the next boot.
func writePersistedRBFCache(
	path string, files []RBFInfo, dirMtimes map[string]int64, rootRBFs []string,
) error {
	if path == "" {
		return nil
	}
	dir := filepath.Dir(path)
	if mkErr := os.MkdirAll(dir, 0o750); mkErr != nil {
		return fmt.Errorf("mkdir for RBF cache: %w", mkErr)
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("create RBF cache temp file: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpPath) }

	payload := persistedRBFCache{
		Magic:     rbfCacheFileMagic,
		Version:   rbfCacheFileVersion,
		Files:     files,
		DirMtimes: dirMtimes,
		RootRBFs:  rootRBFs,
	}
	if encErr := gob.NewEncoder(tmp).Encode(&payload); encErr != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("encode RBF cache: %w", encErr)
	}
	if syncErr := tmp.Sync(); syncErr != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("sync RBF cache: %w", syncErr)
	}
	if closeErr := tmp.Close(); closeErr != nil {
		cleanup()
		return fmt.Errorf("close RBF cache: %w", closeErr)
	}
	if renameErr := os.Rename(tmpPath, path); renameErr != nil {
		cleanup()
		return fmt.Errorf("rename RBF cache: %w", renameErr)
	}
	return nil
}
