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

package service

import (
	"context"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediadb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/broker"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// The recovery happy path runs a real reindex via methods.GenerateMediaDB and is covered
// by the mediadb Recreate tests plus manual verification. These tests cover
// the guard/early-return branches, where recovery must NOT touch the database.

func TestCheckAndRecoverCorruptMediaDB_NoMarkerIsNoOp(t *testing.T) {
	mockDB := helpers.NewMockMediaDBI()
	mockDB.On("IsMarkedCorrupt").Return(false)
	mockDB.On("GetIndexingStatus").Return("", nil)

	checkAndRecoverCorruptMediaDB(nil, nil, &database.Database{MediaDB: mockDB}, nil, nil)

	mockDB.AssertNotCalled(t, "Recreate", true)
	mockDB.AssertNotCalled(t, "Recreate", false)
}

func TestCheckAndRecoverCorruptMediaDB_DefersWhenIndexingInFlight(t *testing.T) {
	mockDB := helpers.NewMockMediaDBI()
	mockDB.On("IsMarkedCorrupt").Return(true)
	mockDB.On("HasBackgroundOperations").Return(true)
	mockDB.On("GetIndexingStatus").Return(mediadb.IndexingStatusRunning, nil)

	checkAndRecoverCorruptMediaDB(nil, nil, &database.Database{MediaDB: mockDB}, nil, nil)

	mockDB.AssertNotCalled(t, "Recreate", true)
	mockDB.AssertNotCalled(t, "Recreate", false)
}

func TestCheckAndRecoverCorruptMediaDB_NilDatabaseIsNoOp(_ *testing.T) {
	// Must not panic with a nil database or nil MediaDB.
	checkAndRecoverCorruptMediaDB(nil, nil, nil, nil, nil)
	checkAndRecoverCorruptMediaDB(nil, nil, &database.Database{}, nil, nil)
}

// TestCheckAndResumeOptimization_FailedCorruptMarksAndSkips verifies the quick_check gate:
// a failed optimization on a corrupt database is flagged corrupt and NOT resumed (which
// would just fail again on every boot).
func TestCheckAndResumeOptimization_FailedCorruptMarksAndSkips(t *testing.T) {
	mockDB := helpers.NewMockMediaDBI()
	mockDB.On("GetOptimizationStatus").Return(mediadb.IndexingStatusFailed, nil)
	mockDB.On("QuickCheck").Return(false, nil)
	mockDB.On("IntegrityReport").Return([]string{"*** in database main ***", "Page 42: btree corrupt"})
	mockDB.On("MarkCorrupt", "quick_check failed before optimization resume").Return()
	mockDB.On("SetIndexingStatus", mediadb.IndexingStatusCorrupt).Return(nil)

	ns := make(chan models.Notification, 10)
	flaggedCorrupt := checkAndResumeOptimization(&database.Database{MediaDB: mockDB}, ns, syncutil.NewPauser())

	assert.True(t, flaggedCorrupt, "must report corruption so the caller can trigger recovery")
	mockDB.AssertCalled(t, "MarkCorrupt", "quick_check failed before optimization resume")
	mockDB.AssertCalled(t, "SetIndexingStatus", mediadb.IndexingStatusCorrupt)
	mockDB.AssertNotCalled(t, "RunBackgroundOptimization")
}

// TestCheckAndResumeOptimization_HealthyResumes verifies that a recoverable interrupted
// optimization resumes and is not reported as corrupt.
func TestCheckAndResumeOptimization_HealthyResumes(t *testing.T) {
	mockDB := helpers.NewMockMediaDBI()
	mockDB.On("GetOptimizationStatus").Return(mediadb.IndexingStatusPending, nil)
	mockDB.On("RunBackgroundOptimization", mock.Anything, mock.Anything).Return()

	ns := make(chan models.Notification, 10)
	flaggedCorrupt := checkAndResumeOptimization(&database.Database{MediaDB: mockDB}, ns, syncutil.NewPauser())

	assert.False(t, flaggedCorrupt)
	mockDB.AssertCalled(t, "RunBackgroundOptimization", mock.Anything, mock.Anything)
	mockDB.AssertNotCalled(t, "MarkCorrupt", mock.Anything)
}

// TestCheckAndResumeOptimization_NoResumeNeeded verifies a completed optimization is left
// alone and not reported as corrupt.
func TestCheckAndResumeOptimization_NoResumeNeeded(t *testing.T) {
	mockDB := helpers.NewMockMediaDBI()
	mockDB.On("GetOptimizationStatus").Return(mediadb.IndexingStatusCompleted, nil)

	ns := make(chan models.Notification, 10)
	flaggedCorrupt := checkAndResumeOptimization(&database.Database{MediaDB: mockDB}, ns, syncutil.NewPauser())

	assert.False(t, flaggedCorrupt)
	mockDB.AssertNotCalled(t, "RunBackgroundOptimization", mock.Anything, mock.Anything)
}

// TestCheckAndRecoverCorruptMediaDB_DefersWhenScrapingInFlight covers the scraping guard:
// a flagged-corrupt database must not be rebuilt while a scrape is running.
func TestCheckAndRecoverCorruptMediaDB_DefersWhenScrapingInFlight(t *testing.T) {
	mockDB := helpers.NewMockMediaDBI()
	mockDB.On("IsMarkedCorrupt").Return(true)
	mockDB.On("HasBackgroundOperations").Return(true)
	mockDB.On("GetIndexingStatus").Return(mediadb.IndexingStatusCompleted, nil)
	mockDB.On("GetScrapingStatus").Return(mediadb.IndexingStatusRunning, nil)

	checkAndRecoverCorruptMediaDB(nil, nil, &database.Database{MediaDB: mockDB}, nil, nil)

	mockDB.AssertNotCalled(t, "Recreate", mock.Anything)
}

// TestCheckAndRecoverCorruptMediaDB_StatusBackstopWithoutMarker covers the backstop that
// trusts a persisted corrupt status even when the sidecar marker is missing.
func TestCheckAndRecoverCorruptMediaDB_StatusBackstopWithoutMarker(t *testing.T) {
	mockDB := helpers.NewMockMediaDBI()
	mockDB.On("IsMarkedCorrupt").Return(false)
	mockDB.On("GetIndexingStatus").Return(mediadb.IndexingStatusCorrupt, nil)
	// Corruption is detected via the status backstop; a running scrape then defers recovery,
	// keeping the test off the heavy reindex path.
	mockDB.On("HasBackgroundOperations").Return(true)
	mockDB.On("GetScrapingStatus").Return(mediadb.IndexingStatusRunning, nil)

	checkAndRecoverCorruptMediaDB(nil, nil, &database.Database{MediaDB: mockDB}, nil, nil)

	mockDB.AssertCalled(t, "GetScrapingStatus")
	mockDB.AssertNotCalled(t, "Recreate", mock.Anything)
}

// TestWatchForCorruptMediaDBRecovery verifies the watcher runs a recovery check when a
// media-indexing notification arrives and shuts down cleanly on context cancellation.
func TestMediaDBCorruptionRecoveryBlocked_IgnoresStalePersistedStatus(t *testing.T) {
	mockDB := helpers.NewMockMediaDBI()
	mockDB.On("HasBackgroundOperations").Return(false)

	assert.False(t, mediaDBCorruptionRecoveryBlocked(mockDB))
	mockDB.AssertNotCalled(t, "GetIndexingStatus")
	mockDB.AssertNotCalled(t, "GetScrapingStatus")
}

func TestMediaDBCorruptionRecoveryBlocked_UsesStatusForActiveWork(t *testing.T) {
	mockDB := helpers.NewMockMediaDBI()
	mockDB.On("HasBackgroundOperations").Return(true)
	mockDB.On("GetIndexingStatus").Return(mediadb.IndexingStatusRunning, nil)

	assert.True(t, mediaDBCorruptionRecoveryBlocked(mockDB))
}

func TestWatchForCorruptMediaDBRecovery_PollsForegroundMarker(t *testing.T) {
	mockDB := helpers.NewMockMediaDBI()
	checked := make(chan struct{}, 1)
	mockDB.On("IsMarkedCorrupt").Return(true).Run(func(mock.Arguments) {
		select {
		case checked <- struct{}{}:
		default:
		}
	})
	mockDB.On("HasBackgroundOperations").Return(true)
	mockDB.On("GetIndexingStatus").Return(mediadb.IndexingStatusRunning, nil)

	ctx, cancel := context.WithCancel(context.Background())
	source := make(chan models.Notification, 1)
	b := broker.NewBroker(ctx, source)
	b.Start()

	done := make(chan struct{})
	go func() {
		watchForCorruptMediaDBRecoveryAtInterval(
			ctx, b, nil, nil, &database.Database{MediaDB: mockDB}, nil, nil, 10*time.Millisecond,
		)
		close(done)
	}()

	select {
	case <-checked:
	case <-time.After(2 * time.Second):
		t.Fatal("recovery watcher did not poll foreground corruption marker")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("watcher did not exit after context cancellation")
	}
	b.Stop()
}

func TestWatchForCorruptMediaDBRecovery(t *testing.T) {
	mockDB := helpers.NewMockMediaDBI()
	checked := make(chan struct{}, 1)
	mockDB.On("IsMarkedCorrupt").Return(false).Run(func(mock.Arguments) {
		select {
		case checked <- struct{}{}:
		default:
		}
	})
	mockDB.On("GetIndexingStatus").Return("", nil)

	ctx, cancel := context.WithCancel(context.Background())
	source := make(chan models.Notification, 10)
	b := broker.NewBroker(ctx, source)
	b.Start()

	done := make(chan struct{})
	go func() {
		watchForCorruptMediaDBRecovery(ctx, b, nil, nil, &database.Database{MediaDB: mockDB}, nil, nil)
		close(done)
	}()

	// Re-publish until the watcher has subscribed and run a recovery check. Re-publishing
	// is harmless: with no marker, each check is an idempotent no-op.
	require.Eventually(t, func() bool {
		select {
		case source <- models.Notification{Method: models.NotificationMediaIndexing}:
		default:
		}
		select {
		case <-checked:
			return true
		default:
			return false
		}
	}, 2*time.Second, 10*time.Millisecond)

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("watcher did not exit after context cancellation")
	}
	b.Stop()
}
