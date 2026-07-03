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
		WithArgs(DBConfigOptimizationStep, "pragma_optimize").
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("(?i)PRAGMA optimize").
		WillReturnResult(sqlmock.NewResult(1, 1))
}

func expectTemporaryParentDirRepairStepNoop(mock sqlmock.Sqlmock) {
	mock.ExpectExec("INSERT OR REPLACE INTO DBConfig").
		WithArgs(DBConfigOptimizationStep, "temporary_repair_parent_dirs").
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectQuery("SELECT Value FROM DBConfig WHERE Name = ").
		WithArgs(DBConfigTemporaryRepairParentDirVersion).
		WillReturnRows(sqlmock.NewRows([]string{"Value"}).AddRow(temporaryRepairParentDirVersion))
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
	// PopulateBrowseCache: BEGIN, SELECT (empty), DELETEs, root dir insert,
	// count prepare, COMMIT.
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT m.SystemDBID, m.Path").
		WillReturnRows(sqlmock.NewRows([]string{"SystemDBID", "Path"}))
	mock.ExpectExec("DELETE FROM BrowseDirCounts").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("DELETE FROM BrowseDirs").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectPrepare("INSERT INTO BrowseDirs").
		ExpectExec().
		WithArgs(int64(1), nil, "/", "/", false).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectPrepare("INSERT INTO BrowseDirCounts")
	mock.ExpectExec("INSERT OR REPLACE INTO DBConfig").
		WithArgs(DBConfigBrowseIndexVersion, browseCacheSchemaVersion).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()
}

// expectDisambiguationBackfillStepNoop mocks the disambiguation_backfill step
// finding the stamp already at the current algorithm version and skipping.
func expectDisambiguationBackfillStepNoop(mock sqlmock.Sqlmock) {
	mock.ExpectExec("INSERT OR REPLACE INTO DBConfig").
		WithArgs(DBConfigOptimizationStep, "disambiguation_backfill").
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectQuery("SELECT Value FROM DBConfig WHERE Name = ").
		WithArgs(DBConfigDisambiguationVersion).
		WillReturnRows(sqlmock.NewRows([]string{"Value"}).AddRow(disambiguationAlgoVersion))
}

func expectWALCheckpointStep(mock sqlmock.Sqlmock) {
	mock.ExpectExec("INSERT OR REPLACE INTO DBConfig").
		WithArgs(DBConfigOptimizationStep, "wal_checkpoint").
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("(?i)PRAGMA wal_checkpoint").
		WillReturnResult(sqlmock.NewResult(0, 0))
}

// expectOptimizationResumeRead mocks the read of the persisted optimization step
// that RunBackgroundOptimization performs to decide where to resume. An empty
// value means "start from the first step".
func expectOptimizationResumeRead(mock sqlmock.Sqlmock) {
	mock.ExpectQuery("SELECT Value FROM DBConfig WHERE Name = ").
		WithArgs(DBConfigOptimizationStep).
		WillReturnRows(sqlmock.NewRows([]string{"Value"}).AddRow(""))
}

// expectPostAnalyzeSteps mocks the steps that run after PRAGMA optimize in the
// background optimization sequence: page_prefetch, wal_checkpoint. browse_cache
// runs before PRAGMA optimize (see expectBrowseCacheStep), not here.
func expectPostAnalyzeSteps(mock sqlmock.Sqlmock) {
	expectPagePrefetchStep(mock)
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
				ctx:               ctx,
				clock:             clockwork.NewFakeClock(),
				analyzeRetryDelay: 1 * time.Millisecond,
				vacuumRetryDelay:  1 * time.Millisecond,
			}
			mediaDB.sql.Store(db)

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
		ctx:               ctx,
		clock:             clockwork.NewFakeClock(),
		analyzeRetryDelay: 1 * time.Millisecond,
		vacuumRetryDelay:  1 * time.Millisecond,
	}
	mediaDB.sql.Store(db)

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
			name: "pragma optimize step",
			step: "pragma_optimize",
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
				ctx:               ctx,
				clock:             clockwork.NewFakeClock(),
				analyzeRetryDelay: 1 * time.Millisecond,
				vacuumRetryDelay:  1 * time.Millisecond,
			}
			mediaDB.sql.Store(db)

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
		ctx:               ctx,
		clock:             clockwork.NewFakeClock(),
		analyzeRetryDelay: 1 * time.Millisecond,
		vacuumRetryDelay:  1 * time.Millisecond,
	}
	mediaDB.sql.Store(db)

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
		ctx:   ctx,
		clock: clockwork.NewFakeClock(),
	}
	mediaDB.sql.Store(nil)

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
		ctx:               ctx,
		clock:             clockwork.NewFakeClock(),
		analyzeRetryDelay: 1 * time.Millisecond,
		vacuumRetryDelay:  1 * time.Millisecond,
	}
	mediaDB.sql.Store(db)

	// Steps run in order: temporary_repair_parent_dirs → browse_cache →
	// pragma_optimize → page_prefetch → wal_checkpoint.
	mock.ExpectExec("INSERT OR REPLACE INTO DBConfig").
		WithArgs(DBConfigOptimizationStatus, "running").
		WillReturnResult(sqlmock.NewResult(1, 1))
	expectOptimizationResumeRead(mock)
	expectTemporaryParentDirRepairStepNoop(mock)
	expectBrowseCacheStep(mock)
	expectDisambiguationBackfillStepNoop(mock)
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
		ctx:               ctx,
		clock:             clockwork.NewRealClock(),
		analyzeRetryDelay: 1 * time.Millisecond,
		vacuumRetryDelay:  1 * time.Millisecond,
	}
	mediaDB.sql.Store(db)

	// temporary repair and browse_cache run first; pragma_optimize failure aborts
	// before page_prefetch/wal_checkpoint.
	mock.ExpectExec("INSERT OR REPLACE INTO DBConfig").
		WithArgs(DBConfigOptimizationStatus, "running").
		WillReturnResult(sqlmock.NewResult(1, 1))
	expectOptimizationResumeRead(mock)
	expectTemporaryParentDirRepairStepNoop(mock)
	expectBrowseCacheStep(mock)
	expectDisambiguationBackfillStepNoop(mock)
	mock.ExpectExec("INSERT OR REPLACE INTO DBConfig").
		WithArgs(DBConfigOptimizationStep, "pragma_optimize").
		WillReturnResult(sqlmock.NewResult(1, 1))

	analyzeError := errors.New("pragma optimize failed")
	mock.ExpectExec("(?i)PRAGMA optimize").WillReturnError(analyzeError)
	mock.ExpectExec("(?i)PRAGMA optimize").WillReturnError(analyzeError)
	mock.ExpectExec("(?i)PRAGMA optimize").WillReturnError(analyzeError) // all retries exhausted

	mock.ExpectExec("INSERT OR REPLACE INTO DBConfig").
		WithArgs(DBConfigOptimizationStep, "").
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT OR REPLACE INTO DBConfig").
		WithArgs(DBConfigOptimizationStatus, "failed").
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
		ctx:               context.Background(),
		clock:             clockwork.NewRealClock(),
		analyzeRetryDelay: 1 * time.Millisecond,
		vacuumRetryDelay:  1 * time.Millisecond,
	}
	mediaDB.sql.Store(db)

	mock.ExpectExec("INSERT OR REPLACE INTO DBConfig").
		WithArgs(DBConfigOptimizationStatus, "running").
		WillReturnResult(sqlmock.NewResult(1, 1))
	expectOptimizationResumeRead(mock)
	expectTemporaryParentDirRepairStepNoop(mock)
	expectBrowseCacheStep(mock)
	expectDisambiguationBackfillStepNoop(mock)
	expectAnalyzeStep(mock)
	mock.ExpectExec("INSERT OR REPLACE INTO DBConfig").
		WithArgs(DBConfigOptimizationStep, "page_prefetch").
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectQuery("^SELECT COUNT\\(\\*\\) FROM Tags$").
		WillReturnError(context.Canceled)
	mock.ExpectExec("INSERT OR REPLACE INTO DBConfig").
		WithArgs(DBConfigOptimizationStep, "").
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT OR REPLACE INTO DBConfig").
		WithArgs(DBConfigOptimizationStatus, "failed").
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
		ctx:               ctx,
		clock:             clockwork.NewFakeClock(),
		analyzeRetryDelay: 1 * time.Millisecond,
		vacuumRetryDelay:  1 * time.Millisecond,
	}
	mediaDB.sql.Store(db)

	// Mock successful optimization for the first call
	mock.ExpectExec("INSERT OR REPLACE INTO DBConfig").
		WithArgs(DBConfigOptimizationStatus, "running").
		WillReturnResult(sqlmock.NewResult(1, 1))
	expectOptimizationResumeRead(mock)
	expectTemporaryParentDirRepairStepNoop(mock)
	expectBrowseCacheStep(mock)
	expectDisambiguationBackfillStepNoop(mock)
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
		ctx:               ctx,
		clock:             clockwork.NewFakeClock(),
		analyzeRetryDelay: 1 * time.Millisecond,
		vacuumRetryDelay:  1 * time.Millisecond,
	}
	mediaDB.sql.Store(db)

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
			ctx:               ctx,
			clock:             clockwork.NewFakeClock(),
			analyzeRetryDelay: 1 * time.Millisecond,
			vacuumRetryDelay:  1 * time.Millisecond,
		}
		mediaDB.sql.Store(db)

		mock.ExpectExec("INSERT OR REPLACE INTO DBConfig").
			WithArgs(DBConfigOptimizationStatus, "running").
			WillReturnResult(sqlmock.NewResult(1, 1))
		expectOptimizationResumeRead(mock)
		expectTemporaryParentDirRepairStepNoop(mock)
		expectBrowseCacheStep(mock)
		expectDisambiguationBackfillStepNoop(mock)
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
			ctx:               ctx,
			clock:             clockwork.NewRealClock(),
			analyzeRetryDelay: 1 * time.Millisecond,
			vacuumRetryDelay:  1 * time.Millisecond,
		}
		mediaDB.sql.Store(db)

		// temporary repair and browse_cache run first; pragma_optimize failure
		// aborts before page_prefetch/wal_checkpoint.
		mock.ExpectExec("INSERT OR REPLACE INTO DBConfig").
			WithArgs(DBConfigOptimizationStatus, "running").
			WillReturnResult(sqlmock.NewResult(1, 1))
		expectOptimizationResumeRead(mock)
		expectTemporaryParentDirRepairStepNoop(mock)
		expectBrowseCacheStep(mock)
		expectDisambiguationBackfillStepNoop(mock)
		mock.ExpectExec("INSERT OR REPLACE INTO DBConfig").
			WithArgs(DBConfigOptimizationStep, "pragma_optimize").
			WillReturnResult(sqlmock.NewResult(1, 1))

		analyzeError := errors.New("pragma optimize failed")
		mock.ExpectExec("(?i)PRAGMA optimize").WillReturnError(analyzeError)
		mock.ExpectExec("(?i)PRAGMA optimize").WillReturnError(analyzeError)
		mock.ExpectExec("(?i)PRAGMA optimize").WillReturnError(analyzeError)

		mock.ExpectExec("INSERT OR REPLACE INTO DBConfig").
			WithArgs(DBConfigOptimizationStep, "").
			WillReturnResult(sqlmock.NewResult(1, 1))
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
			ctx:               ctx,
			clock:             clockwork.NewFakeClock(),
			analyzeRetryDelay: 1 * time.Millisecond,
			vacuumRetryDelay:  1 * time.Millisecond,
		}
		mediaDB.sql.Store(db)

		mock.ExpectExec("INSERT OR REPLACE INTO DBConfig").
			WithArgs(DBConfigOptimizationStatus, "running").
			WillReturnResult(sqlmock.NewResult(1, 1))
		expectOptimizationResumeRead(mock)
		expectTemporaryParentDirRepairStepNoop(mock)
		expectBrowseCacheStep(mock)
		expectDisambiguationBackfillStepNoop(mock)
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
			ctx:               ctx,
			clock:             clockwork.NewFakeClock(),
			analyzeRetryDelay: 1 * time.Millisecond,
			vacuumRetryDelay:  1 * time.Millisecond,
		}
		mediaDB.sql.Store(db)

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
		ctx:               ctx,
		clock:             clockwork.NewFakeClock(),
		analyzeRetryDelay: 1 * time.Millisecond,
		vacuumRetryDelay:  1 * time.Millisecond,
	}
	mediaDB.sql.Store(db)

	// Set up expectations for a full successful run
	mock.ExpectExec("INSERT OR REPLACE INTO DBConfig").
		WithArgs(DBConfigOptimizationStatus, "running").
		WillReturnResult(sqlmock.NewResult(1, 1))
	expectOptimizationResumeRead(mock)
	expectTemporaryParentDirRepairStepNoop(mock)
	expectBrowseCacheStep(mock)
	expectDisambiguationBackfillStepNoop(mock)
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
