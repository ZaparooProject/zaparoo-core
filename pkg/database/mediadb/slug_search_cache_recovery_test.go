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
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestSlugCacheRecoveryCoalescesInvalidations(t *testing.T) {
	t.Parallel()
	db, cleanup := setupCleanOrphansDB(t)
	defer cleanup()
	require.NoError(t, db.RebuildSlugSearchCache())

	// Hold the shared build gate so multiple mutations accumulate before SQL
	// starts. The same worker must own all requests, not queue one per mutation.
	func() {
		db.slugCacheState.buildMu.Lock()
		defer db.slugCacheState.buildMu.Unlock()
		db.invalidateCaches(invalidationScope{AllSystems: true})
		db.slugCacheState.mu.Lock()
		worker := db.slugCacheState.worker
		db.slugCacheState.mu.Unlock()
		require.NotNil(t, worker)
		_, err := db.sql.Load().ExecContext(t.Context(),
			"INSERT INTO MediaTitles (DBID, SystemDBID, Slug, Name) VALUES (4, 1, 'newtitle', 'New Title')")
		require.NoError(t, err)
		for range 8 {
			db.invalidateCaches(invalidationScope{AllSystems: true})
			db.slugCacheState.mu.Lock()
			current := db.slugCacheState.worker
			db.slugCacheState.mu.Unlock()
			require.Same(t, worker, current)
		}
		require.Nil(t, db.slugSearchCache.Load())
	}()
	db.WaitForBackgroundOperations()
	cache := db.slugSearchCache.Load()
	require.NotNil(t, cache)
	require.True(t, cache.complete)
	require.Equal(t, []int64{1, 4, 2, 3}, cache.titleDBIDs, "rebuilt entries must include the new NES title")
	require.False(t, db.HasBackgroundOperations())
}

func TestSlugCacheRecoveryRejectsSupersededPublication(t *testing.T) {
	t.Parallel()
	for _, preserve := range []bool{false, true} {
		t.Run(map[bool]string{false: "invalidated", true: "indexing_commit"}[preserve], func(t *testing.T) {
			t.Parallel()
			db, cleanup := setupCleanOrphansDB(t)
			defer cleanup()
			require.NoError(t, db.RebuildSlugSearchCache())
			old := db.slugSearchCache.Load()
			func() {
				db.slugCacheState.buildMu.Lock()
				defer db.slugCacheState.buildMu.Unlock()
				source, generation := db.slugCacheBuildSnapshot()
				db.invalidateCaches(invalidationScope{AllSystems: true, PreserveSlugSearchCache: preserve})
				publishErr := db.publishSlugCache(context.Background(), source, generation, old)
				require.ErrorIs(t, publishErr, errSlugCacheSuperseded)
				if !preserve {
					require.Nil(t, db.slugSearchCache.Load())
				}
			}()
			db.WaitForBackgroundOperations()
		})
	}
}

func TestSlugCacheRecoveryDefersToIndexing(t *testing.T) {
	t.Parallel()
	for _, status := range []string{IndexingStatusRunning, IndexingStatusPending} {
		t.Run(status, func(t *testing.T) {
			t.Parallel()
			db, cleanup := setupCleanOrphansDB(t)
			defer cleanup()
			require.NoError(t, db.RebuildSlugSearchCache())
			require.NoError(t, db.SetIndexingStatus(status))
			db.invalidateCaches(invalidationScope{AllSystems: true})
			db.WaitForBackgroundOperations()
			require.Nil(t, db.slugSearchCache.Load())
			require.False(t, db.HasBackgroundOperations())
			// The indexer's explicit publication remains allowed.
			require.NoError(t, db.RebuildSlugSearchCache())
			require.NotNil(t, db.slugSearchCache.Load())
		})
	}
}

func TestSlugCacheRecoveryDoesNotWarmInitialDatabase(t *testing.T) {
	t.Parallel()
	db, cleanup := setupCleanOrphansDB(t)
	defer cleanup()
	db.invalidateCaches(invalidationScope{AllSystems: true})
	db.WaitForBackgroundOperations()
	require.Nil(t, db.slugSearchCache.Load())
	require.False(t, db.HasBackgroundOperations())
}

func TestSlugCacheRecoveryCloseCancelsSQLWait(t *testing.T) {
	t.Parallel()
	db, cleanup := setupCleanOrphansDB(t)
	defer cleanup()
	require.NoError(t, db.RebuildSlugSearchCache())

	// Withhold the sole connection: recovery cannot complete SQL work unless
	// shutdown cancels it. Test timeout is the failure guard, not a sleep.
	db.sql.Load().SetMaxOpenConns(1)
	conn, err := db.sql.Load().Conn(t.Context())
	require.NoError(t, err)
	defer func() { require.NoError(t, conn.Close()) }()
	db.slugCacheState.mu.Lock()
	db.slugSearchCache.Store(nil)
	db.slugCacheInvalidatedLocked()
	worker := db.slugCacheState.worker
	db.slugCacheState.mu.Unlock()
	require.NotNil(t, worker)
	require.NoError(t, db.Close())
	<-worker.done
	require.Nil(t, db.slugSearchCache.Load())
	require.False(t, db.HasBackgroundOperations())

	// A late invalidation cannot register work after Close began draining.
	db.slugCacheState.mu.Lock()
	db.slugCacheInvalidatedLocked()
	worker = db.slugCacheState.worker
	db.slugCacheState.mu.Unlock()
	require.Nil(t, worker)
}

func TestSlugCacheRecoveryGateResumesPendingInvalidation(t *testing.T) {
	t.Parallel()
	db, cleanup := setupCleanOrphansDB(t)
	defer cleanup()
	require.NoError(t, db.RebuildSlugSearchCache())
	db.BeginRecovery()
	db.invalidateCaches(invalidationScope{AllSystems: true})
	require.False(t, db.HasBackgroundOperations())
	require.Nil(t, db.slugSearchCache.Load())
	db.EndRecovery()
	db.WaitForBackgroundOperations()
	require.NotNil(t, db.slugSearchCache.Load())
}

func TestSlugCacheRecoveryFailureAllowsNextInvalidation(t *testing.T) {
	t.Parallel()
	db, cleanup := setupCleanOrphansDB(t)
	defer cleanup()
	require.NoError(t, db.RebuildSlugSearchCache())
	_, err := db.sql.Load().ExecContext(t.Context(), "ALTER TABLE MediaTitles RENAME TO UnavailableTitles")
	require.NoError(t, err)
	db.invalidateCaches(invalidationScope{AllSystems: true})
	db.WaitForBackgroundOperations()
	require.Nil(t, db.slugSearchCache.Load())
	require.False(t, db.HasBackgroundOperations(), "a failed build must release its worker, not spin")

	_, err = db.sql.Load().ExecContext(t.Context(), "ALTER TABLE UnavailableTitles RENAME TO MediaTitles")
	require.NoError(t, err)
	db.invalidateCaches(invalidationScope{AllSystems: true})
	db.WaitForBackgroundOperations()
	require.NotNil(t, db.slugSearchCache.Load(), "a later invalidation must retry even with a nil cache")
}

func TestSlugCacheRecoveryRetriesInvalidationDuringBuild(t *testing.T) {
	t.Parallel()
	started, resume := make(chan struct{}), make(chan struct{})
	var blocked atomic.Bool
	const systemsSQL = "SELECT DBID, SystemID FROM Systems"
	matcher := sqlmock.QueryMatcherFunc(func(expected, actual string) error {
		if err := sqlmock.QueryMatcherEqual.Match(expected, actual); err != nil {
			return fmt.Errorf("unexpected recovery query: %w", err)
		}
		if expected == systemsSQL && blocked.CompareAndSwap(false, true) {
			close(started)
			<-resume
		}
		return nil
	})
	source, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(matcher))
	require.NoError(t, err)
	defer func() { _ = source.Close() }()
	for _, id := range []int64{1, 2} {
		mock.ExpectQuery("SELECT Value FROM DBConfig WHERE Name = ?").WithArgs(DBConfigIndexingStatus).
			WillReturnRows(sqlmock.NewRows([]string{"Value"}).AddRow(""))
		mock.ExpectQuery(systemsSQL).
			WillReturnRows(sqlmock.NewRows([]string{"DBID", "SystemID"}).AddRow(1, "NES"))
		mock.ExpectQuery("SELECT DBID, SystemDBID, Slug, SecondarySlug FROM MediaTitles ORDER BY DBID").
			WillReturnRows(sqlmock.NewRows([]string{"DBID", "SystemDBID", "Slug", "SecondarySlug"}).
				AddRow(id, 1, "title", nil))
	}
	db := &MediaDB{ctx: t.Context()}
	db.sql.Store(source)
	db.slugCacheState.enabled = true
	func() {
		defer close(resume)
		db.slugCacheState.mu.Lock()
		db.slugCacheInvalidatedLocked()
		worker := db.slugCacheState.worker
		db.slugCacheState.mu.Unlock()
		<-started
		// The first build already captured its generation. No second worker
		// may start; its first result must be discarded and rebuilt instead.
		db.slugCacheState.mu.Lock()
		db.slugCacheInvalidatedLocked()
		current := db.slugCacheState.worker
		db.slugCacheState.mu.Unlock()
		require.Same(t, worker, current)
	}()
	db.WaitForBackgroundOperations()
	require.NoError(t, mock.ExpectationsWereMet())
	cache := db.slugSearchCache.Load()
	require.NotNil(t, cache)
	require.Equal(t, []int64{2}, cache.titleDBIDs)
}

func TestSlugCacheRecoveryRejectsReplacedSource(t *testing.T) {
	t.Parallel()
	db, cleanup := setupCleanOrphansDB(t)
	defer cleanup()
	other, otherCleanup := setupTempMediaDB(t)
	defer otherCleanup()
	require.NoError(t, db.RebuildSlugSearchCache())
	source, generation := db.slugCacheBuildSnapshot()
	old := db.slugSearchCache.Load()
	db.resetSlugCacheSource(other.sql.Load())
	defer db.resetSlugCacheSource(source)
	require.ErrorIs(t, db.publishSlugCache(context.Background(), source, generation, old), errSlugCacheSuperseded)
	require.Nil(t, db.slugSearchCache.Load())
}
