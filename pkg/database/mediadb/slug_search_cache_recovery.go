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
	"context"
	"database/sql"
	"errors"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/rs/zerolog/log"
)

var errSlugCacheSuperseded = errors.New("slug search cache build superseded")

// mu protects publication and worker registration, never SQL work. buildMu
// serializes full builds, selective refreshes and persisted loads. Invalidation
// can proceed during a build; generation prevents its old snapshot resurfacing.
type slugCacheLifecycle struct {
	worker     *slugCacheRecovery
	buildMu    syncutil.Mutex
	mu         syncutil.Mutex
	generation uint64
	enabled    bool
	dirty      bool
	closed     bool
	paused     bool
}

type slugCacheRecovery struct {
	cancel context.CancelFunc
	done   chan struct{}
}

// slugCacheInvalidatedLocked coalesces work without making a mutator wait for
// SQL reads. Never-warmed databases retain the existing startup/indexing policy.
func (db *MediaDB) slugCacheInvalidatedLocked() {
	s := &db.slugCacheState
	s.generation++
	s.dirty = true
	db.startSlugCacheRecoveryLocked()
}

func (db *MediaDB) startSlugCacheRecoveryLocked() {
	s := &db.slugCacheState
	if !s.enabled || !s.dirty || s.closed || s.paused || s.worker != nil ||
		db.recreating.Load() || db.ctx.Err() != nil {
		return
	}
	ctx, cancel := context.WithCancel(db.ctx)
	worker := &slugCacheRecovery{cancel: cancel, done: make(chan struct{})}
	s.worker = worker
	go db.recoverSlugCache(ctx, worker)
}

func (db *MediaDB) recoverSlugCache(ctx context.Context, worker *slugCacheRecovery) {
	s := &db.slugCacheState
	defer worker.cancel()
	for {
		s.buildMu.Lock()
		s.mu.Lock()
		generation, source := s.generation, db.sql.Load()
		cache := db.slugSearchCache.Load()
		needed := ctx.Err() == nil && !s.closed && !s.paused && !db.recreating.Load() &&
			source != nil && (cache == nil || !cache.complete)
		s.mu.Unlock()

		var err error
		if needed {
			// Indexing already refreshes each committed system and the final
			// catalog. Do not add a competing full scan between its batches.
			var status string
			status, err = sqlGetIndexingStatus(ctx, source)
			if err == nil && status != IndexingStatusRunning && status != IndexingStatusPending {
				cache, err = buildSlugSearchCache(ctx, source)
				if err == nil {
					err = db.publishSlugCache(ctx, source, generation, cache)
				}
			}
		}
		s.buildMu.Unlock()

		s.mu.Lock()
		retry := generation != s.generation && ctx.Err() == nil && !s.closed && !s.paused
		if !retry {
			s.worker = nil
			close(worker.done)
		}
		s.mu.Unlock()
		if !retry {
			if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, errSlugCacheSuperseded) {
				log.Warn().Err(err).Msg("failed to recover invalidated slug search cache")
			}
			return
		}
	}
}

func (db *MediaDB) slugCacheBuildSnapshot() (source *sql.DB, generation uint64) {
	db.slugCacheState.mu.Lock()
	defer db.slugCacheState.mu.Unlock()
	return db.sql.Load(), db.slugCacheState.generation
}

func (db *MediaDB) publishSlugCache(
	ctx context.Context, source *sql.DB, generation uint64, cache *SlugSearchCache,
) error {
	s := &db.slugCacheState
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if generation != s.generation || source != db.sql.Load() || s.closed || db.recreating.Load() {
		return errSlugCacheSuperseded
	}
	db.slugSearchCache.Store(cache)
	s.enabled = true
	if cache.complete {
		s.dirty = false
	}
	return nil
}

func (db *MediaDB) stopSlugCacheRecovery(closing bool) {
	s := &db.slugCacheState
	s.mu.Lock()
	if closing {
		s.closed = true
	} else {
		s.paused = true
	}
	worker := s.worker
	if worker != nil {
		worker.cancel()
	}
	s.mu.Unlock()
	if worker != nil {
		<-worker.done
	}
}

func (db *MediaDB) waitForSlugCacheRecovery() {
	db.slugCacheState.mu.Lock()
	worker := db.slugCacheState.worker
	db.slugCacheState.mu.Unlock()
	if worker != nil {
		<-worker.done
	}
}

func (db *MediaDB) resetSlugCacheSource(source *sql.DB) {
	s := &db.slugCacheState
	s.mu.Lock()
	defer s.mu.Unlock()
	db.sql.Store(source)
	db.slugSearchCache.Store(nil)
	s.generation++
	s.enabled = false
	s.dirty = false
	s.closed = false
}
