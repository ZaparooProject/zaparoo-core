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

package mediadb

import (
	"encoding/gob"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/rs/zerolog/log"
)

// persistedHeader is the common header used by every on-disk cache file in
// this package. The magic and version are checked to reject foreign or older
// files; IndexGeneration is checked against the live DB to reject stale
// caches from a previous indexing run. Each persisted struct declares the
// three header fields directly (rather than embedding this type) so the
// on-disk gob shape is independent of struct layout decisions.
type persistedHeader struct {
	Magic           string
	Version         int
	IndexGeneration int64
}

// headeredCache is implemented by every cache type that gob-encodes itself
// to disk. The header method exposes the three header fields so the shared
// load helper can validate them after decode without reflection.
type headeredCache interface {
	header() persistedHeader
}

// loadPersistedCacheFile opens path, gob-decodes into dst (which must be a
// pointer to a struct exposing header()), and validates the embedded
// magic/version/generation triple. dst must be a pointer for gob to populate.
//
// Return values:
//   - (true, nil) on a successful decode and valid header. Caller should
//     copy the decoded payload into the in-memory cache.
//   - (false, nil) when the file is missing, truncated, oversized
//     (LimitReader cap hit), or the header is rejected (wrong magic,
//     version mismatch, stale generation). All five are graceful "no cache"
//     and the caller should fall back to a SQL rebuild.
//   - (false, err) on any other I/O or decode error.
func loadPersistedCacheFile(
	path string,
	maxBytes int64,
	dst headeredCache,
	cacheKind string,
	expectedMagic string,
	expectedVersion int,
	expectedGen int64,
) (bool, error) {
	f, err := os.Open(path) //nolint:gosec // path is derived from the DB path
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("failed to open %s cache file: %w", cacheKind, err)
	}
	defer func() { _ = f.Close() }()

	if decodeErr := gob.NewDecoder(io.LimitReader(f, maxBytes)).Decode(dst); decodeErr != nil {
		if errors.Is(decodeErr, io.EOF) || errors.Is(decodeErr, io.ErrUnexpectedEOF) {
			log.Warn().Str("path", path).Msgf("%s cache file truncated, falling back to SQL", cacheKind)
			return false, nil
		}
		return false, fmt.Errorf("failed to decode %s cache: %w", cacheKind, decodeErr)
	}

	h := dst.header()
	if h.Magic != expectedMagic {
		log.Warn().Str("path", path).Msgf("%s cache file has wrong magic, falling back to SQL", cacheKind)
		return false, nil
	}
	if h.Version != expectedVersion {
		log.Info().
			Int("file_version", h.Version).
			Int("expected", expectedVersion).
			Msgf("%s cache file version mismatch, falling back to SQL", cacheKind)
		return false, nil
	}
	if h.IndexGeneration != expectedGen {
		log.Info().
			Int64("file_generation", h.IndexGeneration).
			Int64("db_generation", expectedGen).
			Msgf("%s cache file is stale, falling back to SQL", cacheKind)
		return false, nil
	}
	return true, nil
}

// writePersistedCacheFile encodes src (a pointer to a persisted struct) to a
// temp file in path's directory, fsyncs it, and renames into place. The
// rename is atomic against concurrent readers, but the parent directory
// entry change itself is not fsynced — a hard power-off between the rename
// and the next directory write can lose the new file. Crash recovery relies
// on the IndexGeneration check at load time: a missing or stale file just
// triggers a rebuild on the next boot.
func writePersistedCacheFile(path string, src any) error {
	dir := filepath.Dir(path)
	if mkErr := os.MkdirAll(dir, 0o750); mkErr != nil {
		return fmt.Errorf("failed to create cache dir: %w", mkErr)
	}
	base := filepath.Base(path)
	tmp, err := os.CreateTemp(dir, "."+base+".*.tmp")
	if err != nil {
		return fmt.Errorf("failed to create cache temp file: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpPath) }

	if encErr := gob.NewEncoder(tmp).Encode(src); encErr != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("failed to encode cache: %w", encErr)
	}
	if syncErr := tmp.Sync(); syncErr != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("failed to sync cache temp file: %w", syncErr)
	}
	if closeErr := tmp.Close(); closeErr != nil {
		cleanup()
		return fmt.Errorf("failed to close cache temp file: %w", closeErr)
	}
	if renameErr := os.Rename(tmpPath, path); renameErr != nil {
		cleanup()
		return fmt.Errorf("failed to rename cache file: %w", renameErr)
	}
	return nil
}

// removePersistedCacheFile deletes path if it exists. Used by Persist*Cache
// when the in-memory cache is empty so a future boot doesn't load a stale
// file back.
func removePersistedCacheFile(path, cacheKind string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("failed to remove stale %s cache file: %w", cacheKind, err)
	}
	return nil
}
