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

	"github.com/rs/zerolog/log"
)

// SlugSearchCacheFileSuffix is appended to the media DB path to derive the
// on-disk slug search cache filename.
const SlugSearchCacheFileSuffix = ".slug_search_cache.gob"

// slugSearchCacheFileVersion bumps when the on-disk format changes in a
// backwards-incompatible way. Mismatched versions fall back to a SQL rebuild.
const slugSearchCacheFileVersion = 1

const slugSearchCacheFileMagic = "zpsl"

// slugSearchCacheKind is the human label used in log messages for this cache type.
const slugSearchCacheKind = "slug search"

// slugSearchCacheMaxBytes caps the size of the on-disk slug search cache
// that gob will decode. Anyone with write access to the data dir could
// otherwise craft a file with a huge length prefix and OOM the daemon at
// startup; the magic/version check runs after decode so it can't reject
// early. Declared as a var so tests can lower the cap to validate the
// LimitReader wiring without generating 100 MiB files.
var slugSearchCacheMaxBytes int64 = 100 << 20 // 100 MiB

// persistedSlugSearchCache mirrors SlugSearchCache with exported fields so
// gob can serialize it. Fields are converted 1:1 in both directions; this
// struct lives only on disk. The header fields are not part of SlugSearchCache
// itself so the in-memory shape can change without touching readers of older
// files (the version check rejects them first).
type persistedSlugSearchCache struct {
	SystemDBIDToID  map[int64]string
	SystemIDToDBID  map[string]int64
	SystemRanges    map[int64][2]int
	CoveredSystems  map[string]struct{}
	Magic           string
	SecSlugOffsets  []uint32
	SlugOffsets     []uint32
	SecSlugData     []byte
	SlugData        []byte
	TitleDBIDs      []int64
	SystemDBIDs     []int64
	TrigramOffsets  []uint32
	TrigramPostings []uint32
	TrigramCapped   []bool
	Version         int
	IndexGeneration int64
	EntryCount      int
	Complete        bool
}

func (p *persistedSlugSearchCache) header() persistedHeader {
	return persistedHeader{
		Magic:           p.Magic,
		Version:         p.Version,
		IndexGeneration: p.IndexGeneration,
	}
}

// slugSearchCachePath returns the on-disk path for the slug cache file.
// Returns the empty string when the DB has no path (in-memory test DBs).
func (db *MediaDB) slugSearchCachePath() string {
	if db.dbPath == "" {
		return ""
	}
	return db.dbPath + SlugSearchCacheFileSuffix
}

// LoadCachedSlugSearchCache reads the persisted slug search cache from disk
// and installs it into the atomic pointer. Verifies magic, version, and
// index generation against the live DB before accepting the file. Returns
// (false, nil) when the file is missing or stale, so the caller can fall
// back to a SQL rebuild; (true, nil) when a usable cache was loaded;
// (false, err) on unrecoverable I/O or decode errors.
func (db *MediaDB) LoadCachedSlugSearchCache() (bool, error) {
	path := db.slugSearchCachePath()
	if path == "" {
		return false, nil
	}
	gen, err := db.IndexGeneration()
	if err != nil {
		return false, fmt.Errorf("failed to read index generation: %w", err)
	}

	var stored persistedSlugSearchCache
	loaded, err := loadPersistedCacheFile(
		path, slugSearchCacheMaxBytes, &stored, slugSearchCacheKind,
		slugSearchCacheFileMagic, slugSearchCacheFileVersion, gen,
	)
	if err != nil {
		return false, err
	}
	if !loaded {
		return false, nil
	}
	if stored.EntryCount == 0 {
		return false, nil
	}
	cache := &SlugSearchCache{
		systemDBIDToID:  stored.SystemDBIDToID,
		systemIDToDBID:  stored.SystemIDToDBID,
		systemRanges:    stored.SystemRanges,
		coveredSystems:  stored.CoveredSystems,
		secSlugOffsets:  stored.SecSlugOffsets,
		slugOffsets:     stored.SlugOffsets,
		secSlugData:     stored.SecSlugData,
		slugData:        stored.SlugData,
		titleDBIDs:      stored.TitleDBIDs,
		systemDBIDs:     stored.SystemDBIDs,
		trigramOffsets:  stored.TrigramOffsets,
		trigramPostings: stored.TrigramPostings,
		trigramCapped:   stored.TrigramCapped,
		entryCount:      stored.EntryCount,
		complete:        stored.Complete,
	}
	db.slugSearchCache.Store(cache)
	log.Info().
		Int("entries", cache.entryCount).
		Int("systems", len(cache.systemRanges)).
		Int64("generation", gen).
		Str("path", path).
		Msg("slug search cache loaded from disk")
	return true, nil
}

// PersistSlugSearchCache writes the current in-memory slug search cache to
// disk via temp file + rename. Atomic against concurrent readers but not
// durable against a hard power-off — see writePersistedCacheFile for the
// contract.
func (db *MediaDB) PersistSlugSearchCache() error {
	path := db.slugSearchCachePath()
	if path == "" {
		return nil
	}
	cache := db.slugSearchCache.Load()
	if cache == nil || cache.entryCount == 0 {
		return removePersistedCacheFile(path, slugSearchCacheKind)
	}
	gen, err := db.IndexGeneration()
	if err != nil {
		return fmt.Errorf("failed to read index generation: %w", err)
	}

	stored := persistedSlugSearchCache{
		Magic:           slugSearchCacheFileMagic,
		Version:         slugSearchCacheFileVersion,
		IndexGeneration: gen,
		SystemDBIDToID:  cache.systemDBIDToID,
		SystemIDToDBID:  cache.systemIDToDBID,
		SystemRanges:    cache.systemRanges,
		CoveredSystems:  cache.coveredSystems,
		SecSlugOffsets:  cache.secSlugOffsets,
		SlugOffsets:     cache.slugOffsets,
		SecSlugData:     cache.secSlugData,
		SlugData:        cache.slugData,
		TitleDBIDs:      cache.titleDBIDs,
		SystemDBIDs:     cache.systemDBIDs,
		TrigramOffsets:  cache.trigramOffsets,
		TrigramPostings: cache.trigramPostings,
		TrigramCapped:   cache.trigramCapped,
		EntryCount:      cache.entryCount,
		Complete:        cache.complete,
	}
	if err := writePersistedCacheFile(path, &stored); err != nil {
		return err
	}
	log.Info().
		Int("entries", cache.entryCount).
		Int("systems", len(cache.systemRanges)).
		Int64("generation", gen).
		Str("path", path).
		Msg("slug search cache persisted")
	return nil
}
