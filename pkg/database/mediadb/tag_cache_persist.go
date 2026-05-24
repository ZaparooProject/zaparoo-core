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
	"fmt"
	"path/filepath"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/rs/zerolog/log"
)

// TagCacheFileName is the on-disk filename used by the persisted tag cache,
// resolved against the cache subdirectory of the data dir.
const TagCacheFileName = "tag_cache.gob"

// tagCacheFileVersion bumps when the on-disk format changes in a
// backwards-incompatible way. Mismatched versions fall back to a SQL rebuild.
const tagCacheFileVersion = 1

const tagCacheFileMagic = "zptg"

// tagCacheKind is the human label used in log messages for this cache type.
const tagCacheKind = "tag"

// tagCacheMaxBytes caps the size of the on-disk tag cache that gob will
// decode. Anyone with write access to the data dir could otherwise craft a
// file with a huge length prefix and OOM the daemon at startup; the magic/
// version check runs after decode so it can't reject early. Declared as a
// var so tests can lower the cap to validate the LimitReader wiring without
// generating 100 MiB files.
var tagCacheMaxBytes int64 = 100 << 20 // 100 MiB

// persistedTagCache is the serializable form of *tagCache. The header fields
// are not part of tagCache itself so the in-memory shape can change without
// touching readers of older files (the version check rejects them first).
type persistedTagCache struct {
	BySystem        map[string][]database.TagInfo
	Magic           string
	AllTags         []database.TagInfo
	Version         int
	IndexGeneration int64
}

func (p *persistedTagCache) header() persistedHeader {
	return persistedHeader{
		Magic:           p.Magic,
		Version:         p.Version,
		IndexGeneration: p.IndexGeneration,
	}
}

// tagCachePath returns the on-disk path for the tag cache file. Returns the
// empty string when the DB has no path (in-memory test DBs).
func (db *MediaDB) tagCachePath() string {
	if db.dbPath == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(db.dbPath), config.CacheDir, TagCacheFileName)
}

// LoadCachedTagCache reads the persisted tag cache from disk and installs it
// into the atomic pointer. Verifies magic, version, and index generation
// against the live DB before accepting the file. Returns (false, nil) when
// the file is missing or stale, so the caller can fall back to a SQL
// rebuild; (true, nil) when a usable cache was loaded; (false, err) on
// unrecoverable I/O or decode errors.
func (db *MediaDB) LoadCachedTagCache() (bool, error) {
	path := db.tagCachePath()
	if path == "" {
		return false, nil
	}
	gen, err := db.IndexGeneration()
	if err != nil {
		return false, fmt.Errorf("failed to read index generation: %w", err)
	}

	var stored persistedTagCache
	loaded, err := loadPersistedCacheFile(
		path, tagCacheMaxBytes, &stored, tagCacheKind,
		tagCacheFileMagic, tagCacheFileVersion, gen,
	)
	if err != nil {
		return false, err
	}
	if !loaded {
		return false, nil
	}
	if len(stored.AllTags) == 0 {
		return false, nil
	}
	cache := &tagCache{
		bySystem: stored.BySystem,
		allTags:  stored.AllTags,
	}
	db.inMemoryTagCache.Store(cache)
	log.Info().
		Int("tags", len(cache.allTags)).
		Int("systems", len(cache.bySystem)).
		Int64("generation", gen).
		Str("path", path).
		Msg("tag cache loaded from disk")
	return true, nil
}

// PersistTagCache writes the current in-memory tag cache to disk via temp
// file + rename. Atomic against concurrent readers but not durable against a
// hard power-off — see writePersistedCacheFile for the contract.
func (db *MediaDB) PersistTagCache() error {
	path := db.tagCachePath()
	if path == "" {
		return nil
	}
	cache := db.inMemoryTagCache.Load()
	if cache == nil || len(cache.allTags) == 0 {
		return removePersistedCacheFile(path, tagCacheKind)
	}
	gen, err := db.IndexGeneration()
	if err != nil {
		return fmt.Errorf("failed to read index generation: %w", err)
	}

	stored := persistedTagCache{
		Magic:           tagCacheFileMagic,
		Version:         tagCacheFileVersion,
		IndexGeneration: gen,
		BySystem:        cache.bySystem,
		AllTags:         cache.allTags,
	}
	if err := writePersistedCacheFile(path, &stored); err != nil {
		return err
	}
	log.Info().
		Int("tags", len(cache.allTags)).
		Int("systems", len(cache.bySystem)).
		Int64("generation", gen).
		Str("path", path).
		Msg("tag cache persisted")
	return nil
}
