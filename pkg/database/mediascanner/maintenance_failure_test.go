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
	"errors"
	"fmt"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediadb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const (
	failCreateSecondaryIndexes = "create_secondary_indexes"
	failAnalyzeApproximate     = "analyze_approximate"
	failUserDataProjection     = "user_data_projection"
	failSystemTagsCache        = "system_tags_cache"
	failSlugSearchCache        = "slug_search_cache"
	failTagCache               = "tag_cache"
	failGenerationBump         = "generation_bump"
	failPersistTagCache        = "persist_tag_cache"
	failPersistSlugCache       = "persist_slug_cache"
	failCountCache             = "count_cache"
)

type maintenanceFailureMediaDB struct {
	database.MediaDBI
	failure  error
	failAt   string
	statuses []string
	marked   []string
}

func (db *maintenanceFailureMediaDB) injected(name string) error {
	if db.failAt == name {
		return db.failure
	}
	return nil
}

//nolint:wrapcheck // Test decorator deliberately forwards underlying behavior.
func (db *maintenanceFailureMediaDB) CreateSecondaryIndexes() error {
	if err := db.injected(failCreateSecondaryIndexes); err != nil {
		return err
	}
	return db.MediaDBI.CreateSecondaryIndexes()
}

//nolint:wrapcheck // Test decorator deliberately forwards underlying behavior.
func (db *maintenanceFailureMediaDB) AnalyzeApproximate() error {
	if err := db.injected(failAnalyzeApproximate); err != nil {
		return err
	}
	return db.MediaDBI.AnalyzeApproximate()
}

//nolint:wrapcheck // Test decorator deliberately forwards underlying behavior.
func (db *maintenanceFailureMediaDB) FindSystemBySystemID(
	systemID string,
) (database.System, error) {
	if err := db.injected(failUserDataProjection); err != nil {
		return database.System{}, err
	}
	return db.MediaDBI.FindSystemBySystemID(systemID)
}

//nolint:wrapcheck // Test decorator deliberately forwards underlying behavior.
func (db *maintenanceFailureMediaDB) PopulateSystemTagsCache(
	ctx context.Context,
) error {
	if err := db.injected(failSystemTagsCache); err != nil {
		return err
	}
	return db.MediaDBI.PopulateSystemTagsCache(ctx)
}

//nolint:wrapcheck // Test decorator deliberately forwards underlying behavior.
func (db *maintenanceFailureMediaDB) RebuildSlugSearchCache() error {
	if err := db.injected(failSlugSearchCache); err != nil {
		return err
	}
	return db.MediaDBI.RebuildSlugSearchCache()
}

//nolint:wrapcheck // Test decorator deliberately forwards underlying behavior.
func (db *maintenanceFailureMediaDB) RebuildTagCache() error {
	if err := db.injected(failTagCache); err != nil {
		return err
	}
	return db.MediaDBI.RebuildTagCache()
}

//nolint:wrapcheck // Test decorator deliberately forwards underlying behavior.
func (db *maintenanceFailureMediaDB) BumpIndexGeneration() (int64, error) {
	if err := db.injected(failGenerationBump); err != nil {
		return 0, err
	}
	return db.MediaDBI.BumpIndexGeneration()
}

//nolint:wrapcheck // Test decorator deliberately forwards underlying behavior.
func (db *maintenanceFailureMediaDB) PersistTagCache() error {
	if err := db.injected(failPersistTagCache); err != nil {
		return err
	}
	return db.MediaDBI.PersistTagCache()
}

//nolint:wrapcheck // Test decorator deliberately forwards underlying behavior.
func (db *maintenanceFailureMediaDB) PersistSlugSearchCache() error {
	if err := db.injected(failPersistSlugCache); err != nil {
		return err
	}
	return db.MediaDBI.PersistSlugSearchCache()
}

//nolint:wrapcheck // Test decorator deliberately forwards underlying behavior.
func (db *maintenanceFailureMediaDB) InvalidateCountCache() error {
	if err := db.injected(failCountCache); err != nil {
		return err
	}
	return db.MediaDBI.InvalidateCountCache()
}

//nolint:wrapcheck // Test decorator deliberately forwards underlying behavior.
func (db *maintenanceFailureMediaDB) SetIndexingStatus(status string) error {
	db.statuses = append(db.statuses, status)
	return db.MediaDBI.SetIndexingStatus(status)
}

func (db *maintenanceFailureMediaDB) MarkCorrupt(reason string) {
	db.marked = append(db.marked, reason)
	db.MediaDBI.MarkCorrupt(reason)
}

type projectedUserDataDB struct {
	database.UserDBI
}

func (*projectedUserDataDB) ListMediaUserData() ([]database.MediaUserData, error) {
	return []database.MediaUserData{{SystemID: "NES", Path: "game.nes", IsFavorite: true}}, nil
}

func runMaintenanceFailureIndex(
	t *testing.T, failAt string, failure error,
) (*maintenanceFailureMediaDB, error) {
	t.Helper()

	db, cleanup := testhelpers.NewTestDatabase(t)
	t.Cleanup(cleanup)
	wrapped := &maintenanceFailureMediaDB{MediaDBI: db.MediaDB, failAt: failAt, failure: failure}
	db.MediaDB = wrapped
	if failAt == failUserDataProjection {
		db.UserDB = &projectedUserDataDB{UserDBI: db.UserDB}
	}

	platform := mocks.NewMockPlatform()
	platform.On("ID").Return("test-platform")
	platform.On("Launchers", mock.Anything).Return([]platforms.Launcher{})
	platform.On("RootDirs", mock.Anything).Return([]string{})

	_, err := NewNamesIndex(
		context.Background(), platform, &config.Instance{}, nil, db, func(IndexStatus) {}, nil,
	)
	platform.AssertExpectations(t)
	return wrapped, err
}

func TestNewNamesIndex_MaintenanceCorruptionAbortsBeforeCompletion(t *testing.T) {
	corruptErr := fmt.Errorf("wrapped maintenance failure: %w", sqlite3.Error{Code: sqlite3.ErrCorrupt})
	phases := []string{
		failCreateSecondaryIndexes,
		failAnalyzeApproximate,
		failUserDataProjection,
		failSystemTagsCache,
		failSlugSearchCache,
		failTagCache,
		failGenerationBump,
		failPersistTagCache,
		failPersistSlugCache,
		failCountCache,
	}

	for _, phase := range phases {
		t.Run(phase, func(t *testing.T) {
			wrapped, err := runMaintenanceFailureIndex(t, phase, corruptErr)
			require.Error(t, err)
			require.ErrorIs(t, err, corruptErr)
			assert.True(t, database.IsCorruptionError(err))
			assert.NotEmpty(t, wrapped.marked)
			assert.NotContains(t, wrapped.statuses, mediadb.IndexingStatusCompleted)

			status, statusErr := wrapped.GetIndexingStatus()
			require.NoError(t, statusErr)
			assert.Equal(t, mediadb.IndexingStatusCorrupt, status)
		})
	}
}

func TestNewNamesIndex_EarlyAnalyzeCorruptionAbortsRun(t *testing.T) {
	db, cleanup := testhelpers.NewTestDatabase(t)
	t.Cleanup(cleanup)
	corruptErr := fmt.Errorf("early analyze: %w", sqlite3.Error{Code: sqlite3.ErrCorrupt})
	wrapped := &maintenanceFailureMediaDB{
		MediaDBI: db.MediaDB,
		failAt:   failAnalyzeApproximate,
		failure:  corruptErr,
	}
	db.MediaDB = wrapped

	platform := mocks.NewMockPlatform()
	platform.On("ID").Return("test-platform")
	platform.On("Launchers", mock.Anything).Return([]platforms.Launcher{{
		ID:                 "nes-test",
		SystemID:           "NES",
		SkipFilesystemScan: true,
		Scanner: func(
			context.Context, *config.Instance, string, []platforms.ScanResult,
		) ([]platforms.ScanResult, error) {
			return []platforms.ScanResult{{Path: "game.nes"}}, nil
		},
	}})
	platform.On("RootDirs", mock.Anything).Return([]string{})

	_, err := NewNamesIndex(
		context.Background(), platform, &config.Instance{}, []systemdefs.System{{ID: "NES"}},
		db, func(IndexStatus) {}, nil,
	)
	require.Error(t, err)
	require.ErrorIs(t, err, corruptErr)
	assert.NotEmpty(t, wrapped.marked)
	assert.NotContains(t, wrapped.statuses, mediadb.IndexingStatusCompleted)
	platform.AssertExpectations(t)
}

func TestNewNamesIndex_OrdinaryCacheFailureRemainsBestEffort(t *testing.T) {
	wrapped, err := runMaintenanceFailureIndex(t, failSystemTagsCache, errors.New("cache unavailable"))
	require.NoError(t, err)
	assert.Empty(t, wrapped.marked)
	assert.Contains(t, wrapped.statuses, mediadb.IndexingStatusCompleted)
}

func TestNewNamesIndex_SecondaryIndexFailureAlwaysFails(t *testing.T) {
	wrapped, err := runMaintenanceFailureIndex(t, failCreateSecondaryIndexes, errors.New("disk full"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create secondary indexes")
	assert.Empty(t, wrapped.marked)
	assert.NotContains(t, wrapped.statuses, mediadb.IndexingStatusCompleted)

	status, statusErr := wrapped.GetIndexingStatus()
	require.NoError(t, statusErr)
	assert.Equal(t, mediadb.IndexingStatusFailed, status)
}

func TestNewNamesIndex_MaintenanceCancellationPersistsCancelledStatus(t *testing.T) {
	wrapped, err := runMaintenanceFailureIndex(t, failAnalyzeApproximate, context.Canceled)
	require.ErrorIs(t, err, context.Canceled)
	assert.Contains(t, wrapped.statuses, mediadb.IndexingStatusCancelled)
	assert.NotContains(t, wrapped.statuses, mediadb.IndexingStatusCompleted)

	status, statusErr := wrapped.GetIndexingStatus()
	require.NoError(t, statusErr)
	assert.Equal(t, mediadb.IndexingStatusCancelled, status)
}

func TestRefreshMidScanCaches_PropagatesCorruptionFromEveryPhase(t *testing.T) {
	corruptErr := fmt.Errorf("wrapped cache corruption: %w", sqlite3.Error{Code: sqlite3.ErrCorrupt})

	for _, phase := range []string{"slug", "tags", "browse"} {
		t.Run(phase, func(t *testing.T) {
			db := &testhelpers.MockMediaDBI{}
			if phase == "slug" {
				db.On("RefreshSlugSearchCacheForSystems", mock.Anything, []string{"NES"}).Return(corruptErr).Once()
			} else {
				db.On("RefreshSlugSearchCacheForSystems", mock.Anything, []string{"NES"}).Return(nil).Once()
				var tagErr error
				if phase == "tags" {
					tagErr = corruptErr
				}
				db.On("PopulateSystemTagsCacheForSystems", mock.Anything, mock.Anything).Return(tagErr).Once()
				if phase == "browse" {
					db.On("PopulateBrowseCacheForSystems", mock.Anything, []string{"NES"}).Return(corruptErr).Once()
				}
			}

			// populateTagsCache=true so all three phases run; this test is about
			// corruption propagation, not about when the tags phase is skipped.
			err := refreshMidScanCaches(context.Background(), db, []string{"NES"}, true)
			require.Error(t, err)
			assert.True(t, database.IsCorruptionError(err))
			db.AssertExpectations(t)
		})
	}
}

// TestRefreshMidScanCaches_SkipsTagsCacheWhenItWouldNotSurvive covers #1279: on a
// run large enough that every commit invalidates all systems, SystemTagsCache is
// dropped wholesale by the next commit, so populating it per system is wasted
// work. The slug search and browse caches are scoped per system and survive, so
// they must still run either way.
func TestRefreshMidScanCaches_SkipsTagsCacheWhenItWouldNotSurvive(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name              string
		populateTagsCache bool
	}{
		{name: "skips tags cache on all-systems runs", populateTagsCache: false},
		{name: "populates tags cache on selective runs", populateTagsCache: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			db := &testhelpers.MockMediaDBI{}
			db.On("RefreshSlugSearchCacheForSystems", mock.Anything, []string{"NES"}).Return(nil).Once()
			db.On("PopulateBrowseCacheForSystems", mock.Anything, []string{"NES"}).Return(nil).Once()
			if tc.populateTagsCache {
				db.On("PopulateSystemTagsCacheForSystems", mock.Anything, mock.Anything).Return(nil).Once()
			}

			err := refreshMidScanCaches(context.Background(), db, []string{"NES"}, tc.populateTagsCache)
			require.NoError(t, err)
			if !tc.populateTagsCache {
				db.AssertNotCalled(t, "PopulateSystemTagsCacheForSystems", mock.Anything, mock.Anything)
			}
			db.AssertExpectations(t)
		})
	}
}

// TestMidScanSystemTagsCacheSurvivesCommit_MatchesInvalidationScope guards the
// pairing between the predicate and invalidationScopeForSystemIDs: if the
// selective-invalidation threshold moves, the mid-scan skip must move with it or
// it starts skipping populations that would in fact have survived.
func TestMidScanSystemTagsCacheSurvivesCommit_MatchesInvalidationScope(t *testing.T) {
	t.Parallel()

	assert.False(t, mediadb.MidScanSystemTagsCacheSurvivesCommit(0), "no systems invalidates all")
	assert.True(t, mediadb.MidScanSystemTagsCacheSurvivesCommit(1))
	assert.True(t, mediadb.MidScanSystemTagsCacheSurvivesCommit(32), "at the threshold, still selective")
	assert.False(t, mediadb.MidScanSystemTagsCacheSurvivesCommit(33), "past the threshold, all systems")
	assert.False(t, mediadb.MidScanSystemTagsCacheSurvivesCommit(131), "a full MiSTer library")
}

func TestBestEffortMaintenanceError_PreservesWrappedCorruption(t *testing.T) {
	corruptErr := sqlite3.Error{Code: sqlite3.ErrNotADB}
	wrappedErr := fmt.Errorf("cache query: %w", corruptErr)

	err := bestEffortMaintenanceError(wrappedErr, "cache rebuild failed")
	require.Error(t, err)
	require.ErrorIs(t, err, corruptErr)
	assert.True(t, database.IsCorruptionError(err))
}
