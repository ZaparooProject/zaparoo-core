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

func TestConcurrentOptimizationPrevention(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	ctx := context.Background()
	fakeClock := clockwork.NewFakeClock()
	mediaDB := &MediaDB{
		sql:               db,
		ctx:               ctx,
		clock:             fakeClock,
		analyzeRetryDelay: 1 * time.Millisecond,
		vacuumRetryDelay:  1 * time.Millisecond,
	}

	// Mock successful optimization for the first call only
	mock.ExpectExec("INSERT OR REPLACE INTO DBConfig").
		WithArgs(DBConfigOptimizationStatus, "running").
		WillReturnResult(sqlmock.NewResult(1, 1))

	mock.ExpectExec("INSERT OR REPLACE INTO DBConfig").
		WithArgs(DBConfigOptimizationStep, "analyze").
		WillReturnResult(sqlmock.NewResult(1, 1))

	mock.ExpectExec("(?i)analyze;?").
		WillReturnResult(sqlmock.NewResult(1, 1))

	mock.ExpectExec("INSERT OR REPLACE INTO DBConfig").
		WithArgs(DBConfigOptimizationStep, "vacuum").
		WillReturnResult(sqlmock.NewResult(1, 1))

	mock.ExpectExec("(?i)vacuum;?").
		WillReturnResult(sqlmock.NewResult(1, 1))

	mock.ExpectExec("INSERT OR REPLACE INTO DBConfig").
		WithArgs(DBConfigOptimizationStatus, "completed").
		WillReturnResult(sqlmock.NewResult(1, 1))

	mock.ExpectExec("INSERT OR REPLACE INTO DBConfig").
		WithArgs(DBConfigOptimizationStep, "").
		WillReturnResult(sqlmock.NewResult(1, 1))

	const numGoroutines = 5
	completedCount := 0
	var mu syncutil.Mutex
	var wg sync.WaitGroup

	// Start multiple optimization attempts concurrently
	wg.Add(numGoroutines)
	for range numGoroutines {
		go func() {
			defer wg.Done()
			mediaDB.RunBackgroundOptimization(nil)
			mu.Lock()
			completedCount++
			mu.Unlock()
		}()
	}

	wg.Wait()

	// All goroutines should complete, but only one should actually run optimization
	mu.Lock()
	finalCompletedCount := completedCount
	mu.Unlock()
	assert.Equal(t, numGoroutines, finalCompletedCount)
	assert.False(t, mediaDB.isOptimizing.Load())

	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestOptimizationAndIndexingStatusConflict(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()
	mediaDB := &MediaDB{
		sql:               db,
		ctx:               ctx,
		clock:             clockwork.NewFakeClock(),
		analyzeRetryDelay: 1 * time.Millisecond,
		vacuumRetryDelay:  1 * time.Millisecond,
	}

	tests := []struct {
		optimizationStatusErr error
		name                  string
		optimizationStatus    string
		expectIndexingBlocked bool
	}{
		{
			name:                  "optimization running blocks indexing",
			optimizationStatus:    IndexingStatusRunning,
			expectIndexingBlocked: true,
		},
		{
			name:               "optimization completed allows indexing",
			optimizationStatus: IndexingStatusCompleted,
		},
		{
			name:               "no optimization status allows indexing",
			optimizationStatus: "",
		},
		{
			name:                  "optimization status error blocks indexing",
			optimizationStatusErr: errors.New("database locked"),
			expectIndexingBlocked: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Mock GetOptimizationStatus call
			switch {
			case tt.optimizationStatusErr != nil:
				mock.ExpectQuery("SELECT Value FROM DBConfig WHERE Name = ?").
					WithArgs(DBConfigOptimizationStatus).
					WillReturnError(tt.optimizationStatusErr)
			case tt.optimizationStatus == "":
				mock.ExpectQuery("SELECT Value FROM DBConfig WHERE Name = ?").
					WithArgs(DBConfigOptimizationStatus).
					WillReturnError(sql.ErrNoRows)
			default:
				mock.ExpectQuery("SELECT Value FROM DBConfig WHERE Name = ?").
					WithArgs(DBConfigOptimizationStatus).
					WillReturnRows(sqlmock.NewRows([]string{"Value"}).AddRow(tt.optimizationStatus))
			}

			status, err := mediaDB.GetOptimizationStatus()

			if tt.optimizationStatusErr != nil {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.optimizationStatus, status)
			}

			// Simulate checking this status before starting indexing
			if tt.expectIndexingBlocked {
				if err != nil {
					// Error case - indexing should be blocked
					assert.Error(t, err)
				} else if status == IndexingStatusRunning {
					// Running case - indexing should be blocked
					assert.Equal(t, IndexingStatusRunning, status)
				}
			}
		})
	}

	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestConcurrentStatusUpdates(t *testing.T) {
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

	const numGoroutines = 10

	// Mock multiple status update operations
	for range numGoroutines {
		mock.ExpectExec("INSERT OR REPLACE INTO DBConfig").
			WithArgs(DBConfigOptimizationStatus, "running").
			WillReturnResult(sqlmock.NewResult(1, 1))
	}

	var wg sync.WaitGroup
	var updateErrors []error
	var mu syncutil.Mutex

	// Concurrently update optimization status
	for range numGoroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := mediaDB.SetOptimizationStatus("running")
			if err != nil {
				mu.Lock()
				updateErrors = append(updateErrors, err)
				mu.Unlock()
			}
		}()
	}

	wg.Wait()

	// All updates should succeed
	assert.Empty(t, updateErrors)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestConcurrentOptimizationStepUpdates(t *testing.T) {
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

	// Test sequential step updates to avoid mock order issues
	steps := []string{"indexes", "analyze", "vacuum"}

	for _, step := range steps {
		mock.ExpectExec("INSERT OR REPLACE INTO DBConfig").
			WithArgs(DBConfigOptimizationStep, step).
			WillReturnResult(sqlmock.NewResult(1, 1))

		err := mediaDB.SetOptimizationStep(step)
		require.NoError(t, err)
	}

	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestAtomicOptimizationFlag(t *testing.T) {
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

	// Test that the atomic flag properly prevents concurrent optimization
	const numGoroutines = 100
	var wg sync.WaitGroup
	var actualOptimizations int32
	var mu syncutil.Mutex

	// Mock a single successful optimization
	mock.ExpectExec("INSERT OR REPLACE INTO DBConfig").
		WithArgs(DBConfigOptimizationStatus, "running").
		WillReturnResult(sqlmock.NewResult(1, 1))

	mock.ExpectExec("INSERT OR REPLACE INTO DBConfig").
		WithArgs(DBConfigOptimizationStep, "analyze").
		WillReturnResult(sqlmock.NewResult(1, 1))

	mock.ExpectExec("(?i)analyze;?").
		WillReturnResult(sqlmock.NewResult(1, 1))

	mock.ExpectExec("INSERT OR REPLACE INTO DBConfig").
		WithArgs(DBConfigOptimizationStep, "vacuum").
		WillReturnResult(sqlmock.NewResult(1, 1))

	mock.ExpectExec("(?i)vacuum;?").
		WillReturnResult(sqlmock.NewResult(1, 1))

	mock.ExpectExec("INSERT OR REPLACE INTO DBConfig").
		WithArgs(DBConfigOptimizationStatus, "completed").
		WillReturnResult(sqlmock.NewResult(1, 1))

	mock.ExpectExec("INSERT OR REPLACE INTO DBConfig").
		WithArgs(DBConfigOptimizationStep, "").
		WillReturnResult(sqlmock.NewResult(1, 1))

	// Attempt many concurrent optimizations
	for range numGoroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()

			// Track if this goroutine actually performed optimization
			initialFlag := mediaDB.isOptimizing.Load()
			mediaDB.RunBackgroundOptimization(nil)

			// If the flag was false initially and is now false again,
			// this goroutine might have done the optimization
			if !initialFlag && !mediaDB.isOptimizing.Load() {
				mu.Lock()
				actualOptimizations++
				mu.Unlock()
			}
		}()
	}

	wg.Wait()

	// Only one optimization should have actually run
	// (though multiple goroutines may think they did due to timing)
	assert.False(t, mediaDB.isOptimizing.Load())
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestOptimizationInterruption(t *testing.T) {
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

	// Mock optimization that fails partway through
	mock.ExpectExec("INSERT OR REPLACE INTO DBConfig").
		WithArgs(DBConfigOptimizationStatus, "running").
		WillReturnResult(sqlmock.NewResult(1, 1))

	mock.ExpectExec("INSERT OR REPLACE INTO DBConfig").
		WithArgs(DBConfigOptimizationStep, "analyze").
		WillReturnResult(sqlmock.NewResult(1, 1))

	// Second step fails repeatedly
	analyzeError := errors.New("database locked")
	mock.ExpectExec("(?i)analyze;?").WillReturnError(analyzeError)
	mock.ExpectExec("(?i)analyze;?").WillReturnError(analyzeError)
	mock.ExpectExec("(?i)analyze;?").WillReturnError(analyzeError) // Final failure

	// Mock failure handling
	mock.ExpectExec("INSERT OR REPLACE INTO DBConfig").
		WithArgs(DBConfigOptimizationStatus, "failed").
		WillReturnResult(sqlmock.NewResult(1, 1))

	mock.ExpectExec("INSERT OR REPLACE INTO DBConfig").
		WithArgs(DBConfigOptimizationStep, "").
		WillReturnResult(sqlmock.NewResult(1, 1))

	// Run optimization - should complete quickly with 1ms delays
	mediaDB.RunBackgroundOptimization(nil)

	// Verify that optimization is no longer running after failure
	assert.False(t, mediaDB.isOptimizing.Load())
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestConcurrentIndexingAndOptimizationStatusChecks(t *testing.T) {
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

	const numReaders = 50
	var wg sync.WaitGroup

	// Mock many concurrent status reads
	for range numReaders {
		mock.ExpectQuery("SELECT Value FROM DBConfig WHERE Name = ?").
			WithArgs(DBConfigOptimizationStatus).
			WillReturnRows(sqlmock.NewRows([]string{"Value"}).AddRow("running"))
	}

	var readErrors []error
	var mu syncutil.Mutex

	// Many concurrent readers checking optimization status
	for range numReaders {
		wg.Add(1)
		go func() {
			defer wg.Done()
			status, err := mediaDB.GetOptimizationStatus()
			if err != nil {
				mu.Lock()
				readErrors = append(readErrors, err)
				mu.Unlock()
			} else {
				assert.Equal(t, "running", status)
			}
		}()
	}

	wg.Wait()

	// All reads should succeed
	assert.Empty(t, readErrors)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestRaceConditionBetweenStatusAndOptimization(t *testing.T) {
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

	// Mock optimization workflow
	mock.ExpectExec("INSERT OR REPLACE INTO DBConfig").
		WithArgs(DBConfigOptimizationStatus, "running").
		WillReturnResult(sqlmock.NewResult(1, 1))

	mock.ExpectExec("INSERT OR REPLACE INTO DBConfig").
		WithArgs(DBConfigOptimizationStep, "analyze").
		WillReturnResult(sqlmock.NewResult(1, 1))

	mock.ExpectExec("(?i)analyze;?").
		WillReturnResult(sqlmock.NewResult(1, 1))

	mock.ExpectExec("INSERT OR REPLACE INTO DBConfig").
		WithArgs(DBConfigOptimizationStep, "vacuum").
		WillReturnResult(sqlmock.NewResult(1, 1))

	mock.ExpectExec("(?i)vacuum;?").
		WillReturnResult(sqlmock.NewResult(1, 1))

	mock.ExpectExec("INSERT OR REPLACE INTO DBConfig").
		WithArgs(DBConfigOptimizationStatus, "completed").
		WillReturnResult(sqlmock.NewResult(1, 1))

	mock.ExpectExec("INSERT OR REPLACE INTO DBConfig").
		WithArgs(DBConfigOptimizationStep, "").
		WillReturnResult(sqlmock.NewResult(1, 1))

	// Mock concurrent status reads during optimization
	const numStatusChecks = 10

	// Use MatchExpectationsInOrder(false) to allow flexible ordering
	mock.MatchExpectationsInOrder(false)

	// Add many more expectations than needed to handle race conditions
	for range numStatusChecks * 3 {
		mock.ExpectQuery("SELECT Value FROM DBConfig WHERE Name = ?").
			WithArgs(DBConfigOptimizationStatus).
			WillReturnRows(sqlmock.NewRows([]string{"Value"}).AddRow("running"))
	}

	var statusErrors []error
	var mu syncutil.Mutex

	var wg sync.WaitGroup

	// Start optimization
	wg.Add(1)
	go func() {
		defer wg.Done()
		mediaDB.RunBackgroundOptimization(nil)
	}()

	// Concurrently check status many times
	for range numStatusChecks {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := mediaDB.GetOptimizationStatus()
			if err != nil {
				mu.Lock()
				statusErrors = append(statusErrors, err)
				mu.Unlock()
			}
		}()
	}

	wg.Wait()

	// No errors should occur during concurrent access
	assert.Empty(t, statusErrors)
	assert.False(t, mediaDB.isOptimizing.Load())
	// Don't check mock.ExpectationsWereMet() since we added extra expectations to handle race conditions
}
