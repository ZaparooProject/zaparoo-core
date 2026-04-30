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
	"sync"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func expectAnalyzeStep(mock sqlmock.Sqlmock) {
	mock.ExpectExec("INSERT OR REPLACE INTO DBConfig").
		WithArgs(DBConfigOptimizationStep, "analyze").
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("(?i)analyze;?").
		WillReturnResult(sqlmock.NewResult(1, 1))
}

func expectPagePrefetchStep(mock sqlmock.Sqlmock) {
	mock.ExpectExec("INSERT OR REPLACE INTO DBConfig").
		WithArgs(DBConfigOptimizationStep, "page_prefetch").
		WillReturnResult(sqlmock.NewResult(1, 1))
	for _, table := range prefetchTables {
		mock.ExpectQuery("^SELECT COUNT\\(\\*\\) FROM " + table + "$").
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	}
}

func expectBrowseCacheStep(mock sqlmock.Sqlmock) {
	mock.ExpectExec("INSERT OR REPLACE INTO DBConfig").
		WithArgs(DBConfigOptimizationStep, "browse_cache").
		WillReturnResult(sqlmock.NewResult(1, 1))
	// PopulateBrowseCache: BEGIN, SELECT (empty), index drops, DELETEs, root dir insert,
	// index creates, count prepare, COMMIT.
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT m.DBID, m.SystemDBID, m.Path, mt.Name").
		WillReturnRows(sqlmock.NewRows([]string{"DBID", "SystemDBID", "Path", "Name"}))
	mock.ExpectExec("DROP INDEX IF EXISTS idx_browseentries_parent_system_name").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("DROP INDEX IF EXISTS idx_browseentries_parent_system_file").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("DELETE FROM BrowseDirCounts").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("DELETE FROM BrowseEntries").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("DELETE FROM BrowseDirs").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectPrepare("INSERT INTO BrowseDirs").
		ExpectExec().
		WithArgs(int64(1), nil, "/", "/", false).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("CREATE INDEX IF NOT EXISTS idx_browseentries_parent_system_name").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("CREATE INDEX IF NOT EXISTS idx_browseentries_parent_system_file").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectPrepare("INSERT INTO BrowseDirCounts")
	mock.ExpectExec("INSERT OR REPLACE INTO DBConfig").
		WithArgs(DBConfigBrowseIndexVersion, browseIndexVersion).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()
}

func expectWALCheckpointStep(mock sqlmock.Sqlmock) {
	mock.ExpectExec("INSERT OR REPLACE INTO DBConfig").
		WithArgs(DBConfigOptimizationStep, "wal_checkpoint").
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("(?i)PRAGMA wal_checkpoint").
		WillReturnResult(sqlmock.NewResult(0, 0))
}

// expectPostAnalyzeSteps mocks all steps that run after analyze in the
// background optimization sequence: page_prefetch, browse_cache, wal_checkpoint.
func expectPostAnalyzeSteps(mock sqlmock.Sqlmock) {
	expectPagePrefetchStep(mock)
	expectBrowseCacheStep(mock)
	expectWALCheckpointStep(mock)
}

func TestSetGetOptimizationStatus(t *testing.T) {
	tests := []struct {
		name          string
		setStatus     string
		expectedError bool
	}{
		{
			name:      "set running status",
			setStatus: IndexingStatusRunning,
		},
		{
			name:      "set completed status",
			setStatus: IndexingStatusCompleted,
		},
		{
			name:      "set failed status",
			setStatus: IndexingStatusFailed,
		},
		{
			name:      "set pending status",
			setStatus: IndexingStatusPending,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer func() { _ = db.Close() }()

			ctx := context.Background()
			mediaDB := &MediaDB{
				sql:               db,
				ctx:               ctx,
				clock:             clockwork.NewFakeClock(),
				analyzeRetryDelay: 1 * time.Millisecond,
				vacuumRetryDelay:  1 * time.Millisecond,
			}

			// Mock set operation
			mock.ExpectExec("INSERT OR REPLACE INTO DBConfig").
				WithArgs(DBConfigOptimizationStatus, tt.setStatus).
				WillReturnResult(sqlmock.NewResult(1, 1))

			// Mock get operation
			mock.ExpectQuery("SELECT Value FROM DBConfig WHERE Name = ?").
				WithArgs(DBConfigOptimizationStatus).
				WillReturnRows(sqlmock.NewRows([]string{"Value"}).AddRow(tt.setStatus))

			err = mediaDB.SetOptimizationStatus(tt.setStatus)
			require.NoError(t, err)

			status, err := mediaDB.GetOptimizationStatus()
			require.NoError(t, err)
			assert.Equal(t, tt.setStatus, status)

			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestGetOptimizationStatus_NoStatus(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	ctx := context.Background()
	mediaDB := &MediaDB{
		sql:               db,
		ctx:               ctx,
		clock:             clockwork.NewFakeClock(),
		analyzeRetryDelay: 1 * time.Millisecond,
		vacuumRetryDelay:  1 * time.Millisecond,
	}

	// Mock no rows found
	mock.ExpectQuery("SELECT Value FROM DBConfig WHERE Name = ?").
		WithArgs(DBConfigOptimizationStatus).
		WillReturnError(sql.ErrNoRows)

	status, err := mediaDB.GetOptimizationStatus()
	require.NoError(t, err)
	assert.Empty(t, status)

	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSetGetOptimizationStep(t *testing.T) {
	tests := []struct {
		name string
		step string
	}{
		{
			name: "indexes step",
			step: "indexes",
		},
		{
			name: "analyze step",
			step: "analyze",
		},
		{
			name: "vacuum step",
			step: "vacuum",
		},
		{
			name: "empty step",
			step: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer func() { _ = db.Close() }()

			ctx := context.Background()
			mediaDB := &MediaDB{
				sql:               db,
				ctx:               ctx,
				clock:             clockwork.NewFakeClock(),
				analyzeRetryDelay: 1 * time.Millisecond,
				vacuumRetryDelay:  1 * time.Millisecond,
			}

			// Mock set operation
			mock.ExpectExec("INSERT OR REPLACE INTO DBConfig").
				WithArgs(DBConfigOptimizationStep, tt.step).
				WillReturnResult(sqlmock.NewResult(1, 1))

			// Mock get operation
			mock.ExpectQuery("SELECT Value FROM DBConfig WHERE Name = ?").
				WithArgs(DBConfigOptimizationStep).
				WillReturnRows(sqlmock.NewRows([]string{"Value"}).AddRow(tt.step))

			err = mediaDB.SetOptimizationStep(tt.step)
			require.NoError(t, err)

			step, err := mediaDB.GetOptimizationStep()
			require.NoError(t, err)
			assert.Equal(t, tt.step, step)

			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestRunBackgroundOptimization_AlreadyRunning(t *testing.T) {
	db, _, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	ctx := context.Background()
	mediaDB := &MediaDB{
		sql:               db,
		ctx:               ctx,
		clock:             clockwork.NewFakeClock(),
		analyzeRetryDelay: 1 * time.Millisecond,
		vacuumRetryDelay:  1 * time.Millisecond,
	}

	// Set optimization as already running
	mediaDB.isOptimizing.Store(true)

	// This should return immediately without doing anything
	mediaDB.RunBackgroundOptimization(nil, nil)

	// Verify it's still marked as running
	assert.True(t, mediaDB.isOptimizing.Load())
}

func TestRunBackgroundOptimization_NilDatabase(t *testing.T) {
	ctx := context.Background()
	mediaDB := &MediaDB{
		sql:   nil,
		ctx:   ctx,
		clock: clockwork.NewFakeClock(),
	}

	// This should return immediately without panicking
	mediaDB.RunBackgroundOptimization(nil, nil)

	// Verify optimization flag is not set
	assert.False(t, mediaDB.isOptimizing.Load())
}

func TestRunBackgroundOptimization_Success(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	ctx := context.Background()
	mediaDB := &MediaDB{
		sql:               db,
		ctx:               ctx,
		clock:             clockwork.NewFakeClock(),
		analyzeRetryDelay: 1 * time.Millisecond,
		vacuumRetryDelay:  1 * time.Millisecond,
	}

	// Steps run in order: analyze → page_prefetch → browse_cache → wal_checkpoint
	mock.ExpectExec("INSERT OR REPLACE INTO DBConfig").
		WithArgs(DBConfigOptimizationStatus, "running").
		WillReturnResult(sqlmock.NewResult(1, 1))
	expectAnalyzeStep(mock)
	expectPostAnalyzeSteps(mock)
	mock.ExpectExec("INSERT OR REPLACE INTO DBConfig").
		WithArgs(DBConfigOptimizationStatus, "completed").
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT OR REPLACE INTO DBConfig").
		WithArgs(DBConfigOptimizationStep, "").
		WillReturnResult(sqlmock.NewResult(1, 1))

	mediaDB.RunBackgroundOptimization(nil, nil)

	assert.False(t, mediaDB.isOptimizing.Load())
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestRunBackgroundOptimization_FailureHandling(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	ctx := context.Background()
	mediaDB := &MediaDB{
		sql:               db,
		ctx:               ctx,
		clock:             clockwork.NewRealClock(),
		analyzeRetryDelay: 1 * time.Millisecond,
		vacuumRetryDelay:  1 * time.Millisecond,
	}

	// analyze is now the first step; failure aborts before page_prefetch/browse_cache
	mock.ExpectExec("INSERT OR REPLACE INTO DBConfig").
		WithArgs(DBConfigOptimizationStatus, "running").
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT OR REPLACE INTO DBConfig").
		WithArgs(DBConfigOptimizationStep, "analyze").
		WillReturnResult(sqlmock.NewResult(1, 1))

	analyzeError := errors.New("analyze failed")
	mock.ExpectExec("(?i)analyze;?").WillReturnError(analyzeError)
	mock.ExpectExec("(?i)analyze;?").WillReturnError(analyzeError)
	mock.ExpectExec("(?i)analyze;?").WillReturnError(analyzeError) // all retries exhausted

	mock.ExpectExec("INSERT OR REPLACE INTO DBConfig").
		WithArgs(DBConfigOptimizationStatus, "failed").
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT OR REPLACE INTO DBConfig").
		WithArgs(DBConfigOptimizationStep, "").
		WillReturnResult(sqlmock.NewResult(1, 1))

	mediaDB.RunBackgroundOptimization(nil, nil)

	assert.False(t, mediaDB.isOptimizing.Load())
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestRunBackgroundOptimization_PagePrefetchCancellationAborts(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mediaDB := &MediaDB{
		sql:               db,
		ctx:               context.Background(),
		clock:             clockwork.NewRealClock(),
		analyzeRetryDelay: 1 * time.Millisecond,
		vacuumRetryDelay:  1 * time.Millisecond,
	}

	mock.ExpectExec("INSERT OR REPLACE INTO DBConfig").
		WithArgs(DBConfigOptimizationStatus, "running").
		WillReturnResult(sqlmock.NewResult(1, 1))
	expectAnalyzeStep(mock)
	mock.ExpectExec("INSERT OR REPLACE INTO DBConfig").
		WithArgs(DBConfigOptimizationStep, "page_prefetch").
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectQuery("^SELECT COUNT\\(\\*\\) FROM Tags$").
		WillReturnError(context.Canceled)
	mock.ExpectExec("INSERT OR REPLACE INTO DBConfig").
		WithArgs(DBConfigOptimizationStatus, "failed").
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT OR REPLACE INTO DBConfig").
		WithArgs(DBConfigOptimizationStep, "").
		WillReturnResult(sqlmock.NewResult(1, 1))

	mediaDB.RunBackgroundOptimization(nil, nil)

	assert.False(t, mediaDB.isOptimizing.Load())
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestConcurrentOptimization(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	ctx := context.Background()
	mediaDB := &MediaDB{
		sql:               db,
		ctx:               ctx,
		clock:             clockwork.NewFakeClock(),
		analyzeRetryDelay: 1 * time.Millisecond,
		vacuumRetryDelay:  1 * time.Millisecond,
	}

	// Mock successful optimization for the first call
	mock.ExpectExec("INSERT OR REPLACE INTO DBConfig").
		WithArgs(DBConfigOptimizationStatus, "running").
		WillReturnResult(sqlmock.NewResult(1, 1))
	expectAnalyzeStep(mock)
	expectPostAnalyzeSteps(mock)
	mock.ExpectExec("INSERT OR REPLACE INTO DBConfig").
		WithArgs(DBConfigOptimizationStatus, "completed").
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT OR REPLACE INTO DBConfig").
		WithArgs(DBConfigOptimizationStep, "").
		WillReturnResult(sqlmock.NewResult(1, 1))

	var firstStarted, secondSkipped bool
	var mu syncutil.Mutex

	var wg sync.WaitGroup

	// Start first optimization
	wg.Add(1)
	go func() {
		defer wg.Done()
		mediaDB.RunBackgroundOptimization(nil, nil)
		mu.Lock()
		firstStarted = true
		mu.Unlock()
	}()

	// Give first optimization time to start and set the atomic flag
	time.Sleep(10 * time.Millisecond)

	// Start second optimization (should be skipped)
	wg.Add(1)
	go func() {
		defer wg.Done()
		mediaDB.RunBackgroundOptimization(nil, nil)
		mu.Lock()
		secondSkipped = true
		mu.Unlock()
	}()

	wg.Wait()

	mu.Lock()
	finalFirstStarted := firstStarted
	finalSecondSkipped := secondSkipped
	mu.Unlock()

	assert.True(t, finalFirstStarted)
	assert.True(t, finalSecondSkipped)           // Second call completed immediately
	assert.False(t, mediaDB.isOptimizing.Load()) // Should be false after completion

	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestOptimizationDatabaseError(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	ctx := context.Background()
	mediaDB := &MediaDB{
		sql:               db,
		ctx:               ctx,
		clock:             clockwork.NewFakeClock(),
		analyzeRetryDelay: 1 * time.Millisecond,
		vacuumRetryDelay:  1 * time.Millisecond,
	}

	// Mock failure to set initial status
	statusError := errors.New("database connection lost")
	mock.ExpectExec("INSERT OR REPLACE INTO DBConfig").
		WithArgs(DBConfigOptimizationStatus, "running").
		WillReturnError(statusError)

	mediaDB.RunBackgroundOptimization(nil, nil)

	// Verify optimization is no longer running
	assert.False(t, mediaDB.isOptimizing.Load())

	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestOptimizationNotificationCallbacks(t *testing.T) {
	t.Run("successful optimization calls callback correctly", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		ctx := context.Background()
		mediaDB := &MediaDB{
			sql:               db,
			ctx:               ctx,
			clock:             clockwork.NewFakeClock(),
			analyzeRetryDelay: 1 * time.Millisecond,
			vacuumRetryDelay:  1 * time.Millisecond,
		}

		mock.ExpectExec("INSERT OR REPLACE INTO DBConfig").
			WithArgs(DBConfigOptimizationStatus, "running").
			WillReturnResult(sqlmock.NewResult(1, 1))
		expectAnalyzeStep(mock)
		expectPostAnalyzeSteps(mock)
		mock.ExpectExec("INSERT OR REPLACE INTO DBConfig").
			WithArgs(DBConfigOptimizationStatus, "completed").
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectExec("INSERT OR REPLACE INTO DBConfig").
			WithArgs(DBConfigOptimizationStep, "").
			WillReturnResult(sqlmock.NewResult(1, 1))

		// Track callback invocations
		var callbackCalls []bool
		var mu syncutil.Mutex

		callback := func(optimizing bool) {
			mu.Lock()
			callbackCalls = append(callbackCalls, optimizing)
			mu.Unlock()
		}

		mediaDB.RunBackgroundOptimization(callback, nil)

		mu.Lock()
		calls := make([]bool, len(callbackCalls))
		copy(calls, callbackCalls)
		mu.Unlock()

		// Should have exactly 2 calls: start (true) and completion (false)
		require.Len(t, calls, 2)
		assert.True(t, calls[0])  // Started
		assert.False(t, calls[1]) // Completed

		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("failed optimization calls callback with false", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		ctx := context.Background()
		mediaDB := &MediaDB{
			sql:               db,
			ctx:               ctx,
			clock:             clockwork.NewRealClock(),
			analyzeRetryDelay: 1 * time.Millisecond,
			vacuumRetryDelay:  1 * time.Millisecond,
		}

		// analyze is first; failure aborts before page_prefetch/browse_cache
		mock.ExpectExec("INSERT OR REPLACE INTO DBConfig").
			WithArgs(DBConfigOptimizationStatus, "running").
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectExec("INSERT OR REPLACE INTO DBConfig").
			WithArgs(DBConfigOptimizationStep, "analyze").
			WillReturnResult(sqlmock.NewResult(1, 1))

		analyzeError := errors.New("analyze failed")
		mock.ExpectExec("(?i)analyze;?").WillReturnError(analyzeError)
		mock.ExpectExec("(?i)analyze;?").WillReturnError(analyzeError)
		mock.ExpectExec("(?i)analyze;?").WillReturnError(analyzeError)

		mock.ExpectExec("INSERT OR REPLACE INTO DBConfig").
			WithArgs(DBConfigOptimizationStatus, "failed").
			WillReturnResult(sqlmock.NewResult(1, 1))

		// Track callback invocations
		var callbackCalls []bool
		var mu syncutil.Mutex

		callback := func(optimizing bool) {
			mu.Lock()
			callbackCalls = append(callbackCalls, optimizing)
			mu.Unlock()
		}

		mediaDB.RunBackgroundOptimization(callback, nil)

		mu.Lock()
		calls := make([]bool, len(callbackCalls))
		copy(calls, callbackCalls)
		mu.Unlock()

		// Should have exactly 2 calls: start (true) and failure (false)
		require.Len(t, calls, 2)
		assert.True(t, calls[0])  // Started
		assert.False(t, calls[1]) // Failed/Completed

		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("nil callback does not cause panic", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		ctx := context.Background()
		mediaDB := &MediaDB{
			sql:               db,
			ctx:               ctx,
			clock:             clockwork.NewFakeClock(),
			analyzeRetryDelay: 1 * time.Millisecond,
			vacuumRetryDelay:  1 * time.Millisecond,
		}

		mock.ExpectExec("INSERT OR REPLACE INTO DBConfig").
			WithArgs(DBConfigOptimizationStatus, "running").
			WillReturnResult(sqlmock.NewResult(1, 1))
		expectAnalyzeStep(mock)
		expectPostAnalyzeSteps(mock)
		mock.ExpectExec("INSERT OR REPLACE INTO DBConfig").
			WithArgs(DBConfigOptimizationStatus, "completed").
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectExec("INSERT OR REPLACE INTO DBConfig").
			WithArgs(DBConfigOptimizationStep, "").
			WillReturnResult(sqlmock.NewResult(1, 1))

		// Should not panic with nil callback
		assert.NotPanics(t, func() {
			mediaDB.RunBackgroundOptimization(nil, nil)
		})

		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("status update failure still calls callback with false", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		ctx := context.Background()
		mediaDB := &MediaDB{
			sql:               db,
			ctx:               ctx,
			clock:             clockwork.NewFakeClock(),
			analyzeRetryDelay: 1 * time.Millisecond,
			vacuumRetryDelay:  1 * time.Millisecond,
		}

		// Mock failure to set initial status
		statusError := errors.New("database connection lost")
		mock.ExpectExec("INSERT OR REPLACE INTO DBConfig").
			WithArgs(DBConfigOptimizationStatus, "running").
			WillReturnError(statusError)

		// Track callback invocations
		var callbackCalls []bool
		var mu syncutil.Mutex

		callback := func(optimizing bool) {
			mu.Lock()
			callbackCalls = append(callbackCalls, optimizing)
			mu.Unlock()
		}

		mediaDB.RunBackgroundOptimization(callback, nil)

		mu.Lock()
		calls := make([]bool, len(callbackCalls))
		copy(calls, callbackCalls)
		mu.Unlock()

		// Should have exactly 1 call with false (immediate failure)
		require.Len(t, calls, 1)
		assert.False(t, calls[0]) // Failed immediately

		assert.False(t, mediaDB.isOptimizing.Load())
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestRunBackgroundOptimization_PausesAndResumes(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	ctx := context.Background()
	mediaDB := &MediaDB{
		sql:               db,
		ctx:               ctx,
		clock:             clockwork.NewFakeClock(),
		analyzeRetryDelay: 1 * time.Millisecond,
		vacuumRetryDelay:  1 * time.Millisecond,
	}

	// Set up expectations for a full successful run
	mock.ExpectExec("INSERT OR REPLACE INTO DBConfig").
		WithArgs(DBConfigOptimizationStatus, "running").
		WillReturnResult(sqlmock.NewResult(1, 1))
	expectAnalyzeStep(mock)
	expectPostAnalyzeSteps(mock)
	mock.ExpectExec("INSERT OR REPLACE INTO DBConfig").
		WithArgs(DBConfigOptimizationStatus, "completed").
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT OR REPLACE INTO DBConfig").
		WithArgs(DBConfigOptimizationStep, "").
		WillReturnResult(sqlmock.NewResult(1, 1))

	pauser := syncutil.NewPauser()
	pauser.Pause()

	done := make(chan struct{})
	go func() {
		defer close(done)
		mediaDB.RunBackgroundOptimization(nil, pauser)
	}()

	// Optimization should be blocked while paused
	select {
	case <-done:
		t.Fatal("optimization completed while pauser was paused")
	case <-time.After(100 * time.Millisecond):
	}

	// Resume and let it complete
	pauser.Resume()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("optimization did not complete after resume")
	}

	assert.False(t, mediaDB.isOptimizing.Load())
	assert.NoError(t, mock.ExpectationsWereMet())
}
