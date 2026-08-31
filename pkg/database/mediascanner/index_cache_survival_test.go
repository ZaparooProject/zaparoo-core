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

package mediascanner

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediadb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// slugCacheProbe records the state of the in-memory slug search cache at a
// chosen point of a run, sampled from the status callback.
type slugCacheProbe struct {
	systems int
	loaded  bool
	derived bool
	ran     bool
}

// TestNewNamesIndex_SlugSearchCacheSurvivesRunStart pins the ordering between
// the indexing status write and the first commit of a run.
//
// MediaDB chooses its cache invalidation scope per commit from the persisted
// indexing status. A commit made while that status still reads "completed" is
// treated as an ordinary write and drops the whole slug search cache, so
// SeedCanonicalTags — which commits — has to run after the status says the run
// has started, not before.
//
// With the two in the wrong order the cache is gone from the first commit of
// every run onwards. On the #1279 device that put media.search on the grouped
// SQL LIKE path for the whole index, and every request hit the 30s API timeout.
func TestNewNamesIndex_SlugSearchCacheSurvivesRunStart(t *testing.T) {
	// Cannot use t.Parallel() - modifies shared GlobalLauncherCache.
	db, cleanup := testhelpers.NewTestDatabase(t)
	defer cleanup()

	mediaDB, ok := db.MediaDB.(*mediadb.MediaDB)
	require.True(t, ok, "test database must expose the concrete MediaDB")

	systemFiles := map[string][]string{
		systemdefs.SystemGameboy: {"pocket_quest.bin"},
		systemdefs.SystemSNES:    {"super_quest.bin"},
	}
	platform, cfg, _ := setupCustomLauncherSystems(t, systemFiles)
	ctx := context.Background()

	// Ask for every system, as a client-triggered re-index does. That is what
	// makes the run's invalidation scope all-systems, the shape the device runs
	// at; only the two with launchers hold any media.
	systems := systemdefs.AllSystems()

	// First run leaves a complete cache and a "completed" indexing status,
	// which is the state a device is in when a user asks for a re-index.
	_, err := NewNamesIndex(ctx, platform, cfg, systems, db, func(IndexStatus) {}, nil)
	require.NoError(t, err)

	loaded, coveredSystems, derived := mediaDB.SlugSearchCacheCoverageForTesting()
	require.True(t, loaded, "a finished run must leave a slug search cache")
	require.True(t, derived, "a finished full run's cache covers the whole library")
	require.Equal(t, 2, coveredSystems, "only systems holding media get cache entries")

	// Second run: sample at the first system, which is after the canonical tag
	// seeding commit and before any system has committed or refreshed.
	probe := &slugCacheProbe{}
	update := func(status IndexStatus) {
		if status.SystemID == "" || probe.ran {
			return
		}
		probe.ran = true
		probe.loaded, probe.systems, probe.derived = mediaDB.SlugSearchCacheCoverageForTesting()
	}

	_, err = NewNamesIndex(ctx, platform, cfg, systems, db, update, nil)
	require.NoError(t, err)
	require.True(t, probe.ran, "status callback never observed the first system starting")

	assert.True(t, probe.loaded,
		"the slug search cache must survive the start of a run; dropping it here "+
			"forces every search onto the SQL fallback until the run finishes")
	assert.True(t, probe.derived,
		"the surviving cache must still count as library-wide, so searches naming "+
			"systems that hold no media can be served from it")
	assert.Equal(t, 2, probe.systems,
		"an all-systems run leaves the cache intact at the start; systems are dropped "+
			"one at a time as the run reaches them")
}

// evictionRecordingMediaDB counts requests to evict systems from the slug
// search cache. The eviction window sits entirely inside one system's
// processing, between the status callback and that system's commit, so no
// probe driven by the callback can observe it — counting the calls is what
// makes the invariant testable.
type evictionRecordingMediaDB struct {
	database.MediaDBI
	evicted atomic.Int64
}

func (m *evictionRecordingMediaDB) DropSlugSearchCacheForSystems(systemIDs []string) {
	m.evicted.Add(1)
	if dropper, ok := m.MediaDBI.(interface {
		DropSlugSearchCacheForSystems(systemIDs []string)
	}); ok {
		dropper.DropSlugSearchCacheForSystems(systemIDs)
	}
}

// TestNewNamesIndex_KeepsInFlightSystemInSlugCache pins that a run never
// evicts the system it is about to rescan from the slug search cache.
//
// A client search with no system filter names every system Zaparoo knows
// about, so an evicted system makes the whole request unservable from memory
// and pushes it onto the grouped SQL LIKE path. Measured on the #1279 device
// mid-index, the same query took 242 ms across 28 covered systems and
// 27,205 ms across all of them.
//
// Keeping the entries is safe because the cache only nominates candidate title
// IDs and the rows come from a live query: entries whose rows have gone return
// nothing, and refreshMidScanCaches replaces the system's entries once it
// commits.
func TestNewNamesIndex_KeepsInFlightSystemInSlugCache(t *testing.T) {
	// Cannot use t.Parallel() - modifies shared GlobalLauncherCache.
	db, cleanup := testhelpers.NewTestDatabase(t)
	defer cleanup()

	recorder := &evictionRecordingMediaDB{MediaDBI: db.MediaDB}
	wrapped := &database.Database{MediaDB: recorder, UserDB: db.UserDB}

	systemFiles := map[string][]string{
		systemdefs.SystemGameboy: {"pocket_quest.bin"},
		systemdefs.SystemSNES:    {"super_quest.bin"},
	}
	platform, cfg, _ := setupCustomLauncherSystems(t, systemFiles)
	ctx := context.Background()
	systems := systemdefs.AllSystems()

	_, err := NewNamesIndex(ctx, platform, cfg, systems, wrapped, func(IndexStatus) {}, nil)
	require.NoError(t, err)
	// Re-index: the run that has previous entries to lose.
	_, err = NewNamesIndex(ctx, platform, cfg, systems, wrapped, func(IndexStatus) {}, nil)
	require.NoError(t, err)

	assert.Zero(t, recorder.evicted.Load(),
		"indexing must not evict systems from the slug search cache; a search naming "+
			"every system then falls back to the grouped SQL LIKE path for whichever "+
			"are in flight")

	mediaDB, ok := db.MediaDB.(*mediadb.MediaDB)
	require.True(t, ok, "test database must expose the concrete MediaDB")
	everySystemID := make([]string, len(systems))
	for i := range systems {
		everySystemID[i] = systems[i].ID
	}
	assert.True(t, mediaDB.CanServeSystemsFromSlugCacheForTesting(everySystemID),
		"a finished run must leave a cache that can answer a library-wide search")
}
