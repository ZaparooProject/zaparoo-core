// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
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
	"testing/synctest"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetGetOptimizationStatus(t *testing.T) {
	tests := []struct {
		name          string
		setStatus     string
		expectedError bool
	}{
		{
			name:      "set running status",
			setStatus: "running",
		},
		{
			name:      "set completed status",
			setStatus: "completed",
		},
		{
			name:      "set failed status",
			setStatus: "failed",
		},
		{
			name:      "set pending status",
			setStatus: "pending",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer func() { _ = db.Close() }()

			ctx := context.Background()
			mediaDB := &MediaDB{
				sql: db,
				ctx: ctx,
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
		sql: db,
		ctx: ctx,
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
				sql: db,
				ctx: ctx,
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
		sql: db,
		ctx: ctx,
	}

	// Set optimization as already running
	mediaDB.isOptimizing.Store(true)

	// This should return immediately without doing anything
	mediaDB.RunBackgroundOptimization()

	// Verify it's still marked as running
	assert.True(t, mediaDB.isOptimizing.Load())
}

func TestRunBackgroundOptimization_NilDatabase(t *testing.T) {
	ctx := context.Background()
	mediaDB := &MediaDB{
		sql: nil,
		ctx: ctx,
	}

	// This should return immediately without panicking
	mediaDB.RunBackgroundOptimization()

	// Verify optimization flag is not set
	assert.False(t, mediaDB.isOptimizing.Load())
}

func TestRunBackgroundOptimization_Success(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	ctx := context.Background()
	mediaDB := &MediaDB{
		sql: db,
		ctx: ctx,
	}

	// Mock setting status to running
	mock.ExpectExec("INSERT OR REPLACE INTO DBConfig").
		WithArgs(DBConfigOptimizationStatus, "running").
		WillReturnResult(sqlmock.NewResult(1, 1))

	// Mock setting step to analyze
	mock.ExpectExec("INSERT OR REPLACE INTO DBConfig").
		WithArgs(DBConfigOptimizationStep, "analyze").
		WillReturnResult(sqlmock.NewResult(1, 1))

	// Mock Analyze
	mock.ExpectExec("(?i)analyze;?").
		WillReturnResult(sqlmock.NewResult(1, 1))

	// Mock setting step to vacuum
	mock.ExpectExec("INSERT OR REPLACE INTO DBConfig").
		WithArgs(DBConfigOptimizationStep, "vacuum").
		WillReturnResult(sqlmock.NewResult(1, 1))

	// Mock Vacuum
	mock.ExpectExec("(?i)vacuum;?").
		WillReturnResult(sqlmock.NewResult(1, 1))

	// Mock setting status to completed
	mock.ExpectExec("INSERT OR REPLACE INTO DBConfig").
		WithArgs(DBConfigOptimizationStatus, "completed").
		WillReturnResult(sqlmock.NewResult(1, 1))

	// Mock clearing step
	mock.ExpectExec("INSERT OR REPLACE INTO DBConfig").
		WithArgs(DBConfigOptimizationStep, "").
		WillReturnResult(sqlmock.NewResult(1, 1))

	mediaDB.RunBackgroundOptimization()

	// Verify optimization is no longer running
	assert.False(t, mediaDB.isOptimizing.Load())

	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestRunBackgroundOptimization_FailureHandling(t *testing.T) {
	synctest.Run(func() {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		ctx := context.Background()
		mediaDB := &MediaDB{
			sql: db,
			ctx: ctx,
		}

		// Mock setting status to running
		mock.ExpectExec("INSERT OR REPLACE INTO DBConfig").
			WithArgs(DBConfigOptimizationStatus, "running").
			WillReturnResult(sqlmock.NewResult(1, 1))

		// Mock setting step to indexes
		mock.ExpectExec("INSERT OR REPLACE INTO DBConfig").
			WithArgs(DBConfigOptimizationStep, "analyze").
			WillReturnResult(sqlmock.NewResult(1, 1))

		// Mock Analyze failure with retries
		analyzeError := errors.New("analyze failed")
		mock.ExpectExec("(?i)analyze;?").
			WillReturnError(analyzeError)
		mock.ExpectExec("(?i)analyze;?").
			WillReturnError(analyzeError)
		mock.ExpectExec("(?i)analyze;?").
			WillReturnError(analyzeError) // Final failure after all retries

		// Mock setting status to failed
		mock.ExpectExec("INSERT OR REPLACE INTO DBConfig").
			WithArgs(DBConfigOptimizationStatus, "failed").
			WillReturnResult(sqlmock.NewResult(1, 1))

		// Mock clearing step on failure
		mock.ExpectExec("INSERT OR REPLACE INTO DBConfig").
			WithArgs(DBConfigOptimizationStep, "").
			WillReturnResult(sqlmock.NewResult(1, 1))

		mediaDB.RunBackgroundOptimization()

		// Verify optimization is no longer running
		assert.False(t, mediaDB.isOptimizing.Load())

		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestCheckAndResumeOptimization(t *testing.T) {
	tests := []struct {
		statusError   error
		name          string
		initialStatus string
		shouldResume  bool
	}{
		{
			name:          "resume pending optimization",
			initialStatus: "pending",
			shouldResume:  true,
		},
		{
			name:          "resume running optimization",
			initialStatus: "running",
			shouldResume:  true,
		},
		{
			name:          "retry failed optimization",
			initialStatus: "failed",
			shouldResume:  true,
		},
		{
			name:          "completed optimization - no resume",
			initialStatus: "completed",
			shouldResume:  false,
		},
		{
			name:          "no status - no resume",
			initialStatus: "",
			shouldResume:  false,
		},
		{
			name:         "status error - no resume",
			statusError:  errors.New("database error"),
			shouldResume: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer func() { _ = db.Close() }()

			ctx := context.Background()
			mediaDB := &MediaDB{
				sql: db,
				ctx: ctx,
			}

			switch {
			case tt.statusError != nil:
				mock.ExpectQuery("SELECT Value FROM DBConfig WHERE Name = ?").
					WithArgs(DBConfigOptimizationStatus).
					WillReturnError(tt.statusError)
			case tt.initialStatus == "":
				mock.ExpectQuery("SELECT Value FROM DBConfig WHERE Name = ?").
					WithArgs(DBConfigOptimizationStatus).
					WillReturnError(sql.ErrNoRows)
			default:
				mock.ExpectQuery("SELECT Value FROM DBConfig WHERE Name = ?").
					WithArgs(DBConfigOptimizationStatus).
					WillReturnRows(sqlmock.NewRows([]string{"Value"}).AddRow(tt.initialStatus))
			}

			if tt.shouldResume {
				// Mock the optimization workflow
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
			}

			synctest.Run(func() {
				mediaDB.checkAndResumeOptimization()

				// Give some time for goroutine to complete if it was started
				if tt.shouldResume {
					time.Sleep(100 * time.Millisecond)
				}
			})

			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestConcurrentOptimization(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	ctx := context.Background()
	mediaDB := &MediaDB{
		sql: db,
		ctx: ctx,
	}

	// Mock successful optimization for the first call
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

	var firstStarted, secondSkipped bool
	var mu sync.Mutex

	synctest.Run(func() {
		var wg sync.WaitGroup

		// Start first optimization
		wg.Add(1)
		go func() {
			defer wg.Done()
			mediaDB.RunBackgroundOptimization()
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
			mediaDB.RunBackgroundOptimization()
			mu.Lock()
			secondSkipped = true
			mu.Unlock()
		}()

		wg.Wait()
	})

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
		sql: db,
		ctx: ctx,
	}

	// Mock failure to set initial status
	statusError := errors.New("database connection lost")
	mock.ExpectExec("INSERT OR REPLACE INTO DBConfig").
		WithArgs(DBConfigOptimizationStatus, "running").
		WillReturnError(statusError)

	mediaDB.RunBackgroundOptimization()

	// Verify optimization is no longer running
	assert.False(t, mediaDB.isOptimizing.Load())

	assert.NoError(t, mock.ExpectationsWereMet())
}
