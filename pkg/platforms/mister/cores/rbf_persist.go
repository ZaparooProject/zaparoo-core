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
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/config"
	"github.com/rs/zerolog/log"
)

// RBFCacheFileName is the on-disk filename used by the persisted cache,
// resolved against the data directory by the caller of SetPersistPath.
const RBFCacheFileName = "rbf_cache.gob"

const (
	rbfCacheFileMagic   = "zrbf"
	rbfCacheFileVersion = 1
)

// rbfCacheMaxBytes caps gob input at load time. ~200 RBFs × ~256 B per
// record ≈ 50 KiB realistic; the cap is generous to absorb future field
// additions without forcing a version bump. Var (not const) so tests can
// lower it to exercise the oversize-fallback path.
var rbfCacheMaxBytes int64 = 2 << 20

// persistedRBFCache is the on-disk shape. DirMtimes maps the SD root and
// each scanned `_*` subdirectory to its ModTime().UnixNano() at scan time;
// drift in any of these signals a core was added or removed since the
// last scan and the cache should be rebuilt.
type persistedRBFCache struct {
	DirMtimes map[string]int64
	Magic     string
	Files     []RBFInfo
	Version   int
}

// snapshotDirMtimes captures the current mtime for the SD root and each of
// its `_*` subdirectories. These are the directories `shallowScanRBF`
// actually visits, so any add/remove/rename of a core (or a `_*` dir
// itself) mutates one of these mtimes.
func snapshotDirMtimes() (map[string]int64, error) {
	snapshot := make(map[string]int64)

	rootInfo, err := os.Stat(config.SDRootDir)
	if err != nil {
		return nil, fmt.Errorf("stat SD root: %w", err)
	}
	snapshot[config.SDRootDir] = rootInfo.ModTime().UnixNano()

	files, err := os.ReadDir(config.SDRootDir)
	if err != nil {
		return nil, fmt.Errorf("readdir SD root: %w", err)
	}
	for _, f := range files {
		if !f.IsDir() || !strings.HasPrefix(f.Name(), "_") {
			continue
		}
		sub := filepath.Join(config.SDRootDir, f.Name())
		info, statErr := os.Stat(sub)
		if statErr != nil {
			continue
		}
		snapshot[sub] = info.ModTime().UnixNano()
	}
	return snapshot, nil
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
func writePersistedRBFCache(path string, files []RBFInfo, dirMtimes map[string]int64) error {
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
