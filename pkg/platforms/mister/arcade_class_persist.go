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

package mister

import (
	"encoding/gob"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/rs/zerolog/log"
)

// arcadeClassCacheFileName is the on-disk filename for the persisted MRA
// classification cache, resolved against the data cache directory.
const arcadeClassCacheFileName = "arcade_class_cache.gob"

const (
	arcadeClassCacheFileMagic   = "zarc"
	arcadeClassCacheFileVersion = 1
)

// arcadeClassCacheMaxBytes caps gob input at load time. ~10K MRAs × ~150 B
// per record ≈ 1.5 MiB realistic; the cap is generous for future fields.
// Var (not const) so tests can lower it to exercise the oversize fallback.
var arcadeClassCacheMaxBytes int64 = 8 << 20

// arcadeClassCacheEntry records what a previous run learned about one MRA
// file. Size and MtimeNs pin the entry to the file contents it was parsed
// from; SetName is empty when the file parsed but had no setname, or failed
// to parse — either way there is no point re-reading an unchanged file.
type arcadeClassCacheEntry struct {
	SetName string
	Size    int64
	MtimeNs int64
}

// persistedArcadeClassCache is the on-disk shape of the classification cache.
type persistedArcadeClassCache struct {
	Entries map[string]arcadeClassCacheEntry
	Magic   string
	Version int
}

// loadArcadeClassCache reads the persisted classification cache. Returns an
// empty map for a missing, truncated, wrong-magic, or wrong-version file —
// classification then just re-parses every MRA once and rewrites the cache.
func loadArcadeClassCache(path string) map[string]arcadeClassCacheEntry {
	if path == "" {
		return map[string]arcadeClassCacheEntry{}
	}
	f, err := os.Open(path) //nolint:gosec // path is derived from the data dir
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			log.Warn().Err(err).Str("path", path).Msg("failed to open arcade classification cache")
		}
		return map[string]arcadeClassCacheEntry{}
	}
	defer func() { _ = f.Close() }()

	var stored persistedArcadeClassCache
	if decErr := gob.NewDecoder(io.LimitReader(f, arcadeClassCacheMaxBytes)).Decode(&stored); decErr != nil {
		log.Warn().Err(decErr).Str("path", path).Msg("arcade classification cache unreadable, re-parsing MRA files")
		return map[string]arcadeClassCacheEntry{}
	}
	if stored.Magic != arcadeClassCacheFileMagic || stored.Version != arcadeClassCacheFileVersion {
		log.Info().Str("path", path).Msg("arcade classification cache format changed, re-parsing MRA files")
		return map[string]arcadeClassCacheEntry{}
	}
	if stored.Entries == nil {
		return map[string]arcadeClassCacheEntry{}
	}
	return stored.Entries
}

// saveArcadeClassCache encodes the cache and renames it into place, atomic
// against concurrent readers. Best-effort: a lost file only means MRA files
// are re-parsed on the next classification.
func saveArcadeClassCache(path string, entries map[string]arcadeClassCacheEntry) error {
	if path == "" {
		return nil
	}
	dir := filepath.Dir(path)
	if mkErr := os.MkdirAll(dir, 0o750); mkErr != nil {
		return fmt.Errorf("mkdir for arcade classification cache: %w", mkErr)
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("create arcade classification cache temp file: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpPath) }

	payload := persistedArcadeClassCache{
		Magic:   arcadeClassCacheFileMagic,
		Version: arcadeClassCacheFileVersion,
		Entries: entries,
	}
	if encErr := gob.NewEncoder(tmp).Encode(&payload); encErr != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("encode arcade classification cache: %w", encErr)
	}
	if syncErr := tmp.Sync(); syncErr != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("sync arcade classification cache: %w", syncErr)
	}
	if closeErr := tmp.Close(); closeErr != nil {
		cleanup()
		return fmt.Errorf("close arcade classification cache: %w", closeErr)
	}
	if renameErr := os.Rename(tmpPath, path); renameErr != nil {
		cleanup()
		return fmt.Errorf("rename arcade classification cache: %w", renameErr)
	}
	return nil
}
