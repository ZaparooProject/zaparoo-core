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
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/jonboulle/clockwork"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOptimizationCancellationPreservesCheckpoint(t *testing.T) {
	for _, stepName := range []string{"page_prefetch", "pragma_optimize"} {
		t.Run(stepName, func(t *testing.T) {
			ctx := context.Background()
			sqlDB, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer func() { _ = sqlDB.Close() }()
			db := &MediaDB{ctx: ctx, clock: clockwork.NewFakeClock()}
			db.sql.Store(sqlDB)
			mock.ExpectExec("INSERT OR REPLACE INTO DBConfig").WithArgs(DBConfigOptimizationStatus, "running").
				WillReturnResult(sqlmock.NewResult(1, 1))
			mock.ExpectQuery("SELECT Value FROM DBConfig WHERE Name = ").WithArgs(DBConfigOptimizationStep).
				WillReturnRows(sqlmock.NewRows([]string{"Value"}).AddRow(stepName))
			mock.ExpectExec("INSERT OR REPLACE INTO DBConfig").WithArgs(DBConfigOptimizationStep, stepName).
				WillReturnResult(sqlmock.NewResult(1, 1))
			if stepName == "page_prefetch" {
				mock.ExpectQuery("^SELECT COUNT\\(\\*\\) FROM Tags$").WillReturnError(context.Canceled)
			} else {
				mock.ExpectExec("(?i)PRAGMA optimize").WillReturnError(context.Canceled)
			}
			var output bytes.Buffer
			old, level := log.Logger, zerolog.GlobalLevel()
			log.Logger = zerolog.New(&output)
			zerolog.SetGlobalLevel(zerolog.DebugLevel)
			t.Cleanup(func() { log.Logger = old; zerolog.SetGlobalLevel(level) })
			lease, err := db.AcquireMediaWrite(database.MediaWriteOperationOptimization)
			require.NoError(t, err)
			var callbacks []bool
			err = db.RunBackgroundOptimizationWithLease(func(active bool) {
				callbacks = append(callbacks, active)
			}, nil, lease)
			require.ErrorIs(t, err, context.Canceled)
			assert.NotContains(t, output.String(), `"level":"error"`)
			assert.NotContains(t, output.String(), "failed to clear optimization step")
			assert.Equal(t, []bool{true, false}, callbacks)
			assert.False(t, db.isOptimizing.Load())
			assert.Equal(t, database.MediaWriteOperationNone, db.ActiveMediaWriteOperation())
			db.WaitForBackgroundOperations()
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestOptimizationFailureLogging(t *testing.T) {
	for _, tc := range []struct {
		err   error
		name  string
		level string
	}{
		{err: context.Canceled, name: "canceled", level: "debug"},
		{err: context.DeadlineExceeded, name: "deadline", level: "error"},
		{err: errors.New("context canceled"), name: "text", level: "error"},
		{err: errors.Join(context.Canceled, errors.New("disk failure")), name: "mixed", level: "error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var output bytes.Buffer
			old, level := log.Logger, zerolog.GlobalLevel()
			log.Logger = zerolog.New(&output)
			zerolog.SetGlobalLevel(zerolog.DebugLevel)
			t.Cleanup(func() { log.Logger = old; zerolog.SetGlobalLevel(level) })
			logOptimizationFailure(tc.err, "optimization stopped")
			assert.Contains(t, output.String(), `"level":"`+tc.level+`"`)
		})
	}
}

func TestOptimizationRetryCancellationPreservesFailure(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sqlDB, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = sqlDB.Close() }()
	clock := clockwork.NewFakeClock()
	db := &MediaDB{ctx: ctx, clock: clock, analyzeRetryDelay: time.Hour}
	db.indexingCacheBoost.Store(true)
	db.sql.Store(sqlDB)
	mock.ExpectExec("INSERT OR REPLACE INTO DBConfig").WithArgs(DBConfigOptimizationStatus, "running").
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectQuery("SELECT Value FROM DBConfig WHERE Name = ").WithArgs(DBConfigOptimizationStep).
		WillReturnRows(sqlmock.NewRows([]string{"Value"}).AddRow("pragma_optimize"))
	mock.ExpectExec("INSERT OR REPLACE INTO DBConfig").WithArgs(DBConfigOptimizationStep, "pragma_optimize").
		WillReturnResult(sqlmock.NewResult(1, 1))
	failure := errors.New("storage operation failed")
	mock.ExpectExec("(?i)PRAGMA optimize").WillReturnError(failure)
	lease, err := db.AcquireMediaWrite(database.MediaWriteOperationOptimization)
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() { done <- db.RunBackgroundOptimizationWithLease(nil, nil, lease) }()
	guard, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	require.NoError(t, clock.BlockUntilContext(guard, 1))
	cancel()
	select {
	case err = <-done:
		require.ErrorIs(t, err, context.Canceled)
		require.ErrorIs(t, err, failure, "concurrent cancellation must not hide original error")
	case <-guard.Done():
		t.Fatal("retry wait ignored cancellation")
	}
	assert.False(t, db.isOptimizing.Load())
	assert.Equal(t, database.MediaWriteOperationNone, db.ActiveMediaWriteOperation())
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestOptimizationCancellationStatusBoundaries(t *testing.T) {
	t.Parallel()
	for _, phase := range []string{"start", "resume", "step", "complete", "clear"} {
		t.Run(phase, func(t *testing.T) {
			t.Parallel()
			sqlDB, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer func() { _ = sqlDB.Close() }()
			db := &MediaDB{ctx: context.Background(), clock: clockwork.NewFakeClock()}
			db.indexingCacheBoost.Store(true)
			db.sql.Store(sqlDB)
			start := mock.ExpectExec("INSERT OR REPLACE INTO DBConfig").WithArgs(DBConfigOptimizationStatus, "running")
			if phase == "start" {
				start.WillReturnError(context.Canceled)
			} else {
				start.WillReturnResult(sqlmock.NewResult(1, 1))
				resume := mock.ExpectQuery("SELECT Value FROM DBConfig WHERE Name = ").
					WithArgs(DBConfigOptimizationStep)
				if phase == "resume" {
					resume.WillReturnError(context.Canceled)
				} else {
					resume.WillReturnRows(sqlmock.NewRows([]string{"Value"}).AddRow("wal_checkpoint"))
					step := mock.ExpectExec("INSERT OR REPLACE INTO DBConfig").
						WithArgs(DBConfigOptimizationStep, "wal_checkpoint")
					if phase == "step" {
						step.WillReturnError(context.Canceled)
					} else {
						step.WillReturnResult(sqlmock.NewResult(1, 1))
						mock.ExpectQuery("(?i)^PRAGMA wal_checkpoint\\(TRUNCATE\\);?$").
							WillReturnRows(sqlmock.NewRows([]string{"busy", "log", "checkpointed"}).AddRow(0, 0, 0))
						complete := mock.ExpectExec("INSERT OR REPLACE INTO DBConfig").
							WithArgs(DBConfigOptimizationStatus, "completed")
						if phase == "complete" {
							complete.WillReturnError(context.Canceled)
						} else {
							complete.WillReturnResult(sqlmock.NewResult(1, 1))
							mock.ExpectExec("INSERT OR REPLACE INTO DBConfig").WithArgs(DBConfigOptimizationStep, "").
								WillReturnError(context.Canceled)
						}
					}
				}
			}
			lease, err := db.AcquireMediaWrite(database.MediaWriteOperationOptimization)
			require.NoError(t, err)
			var callbacks []bool
			err = db.RunBackgroundOptimizationWithLease(func(active bool) {
				callbacks = append(callbacks, active)
			}, nil, lease)
			require.ErrorIs(t, err, context.Canceled)
			if phase == "start" {
				assert.Equal(t, []bool{false}, callbacks)
			} else {
				assert.Equal(t, []bool{true, false}, callbacks)
			}
			assert.False(t, db.isOptimizing.Load())
			assert.Equal(t, database.MediaWriteOperationNone, db.ActiveMediaWriteOperation())
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestPrefetchCancellationPreservesDatabaseFailure(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	sqlDB, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = sqlDB.Close() }()
	db := &MediaDB{ctx: ctx}
	db.sql.Store(sqlDB)
	failure := errors.New("storage failure during prefetch")
	mock.ExpectQuery("^SELECT COUNT\\(\\*\\) FROM Tags$").WillReturnError(errors.Join(failure, context.Canceled))
	err = db.prefetchSearchPages(ctx)
	require.ErrorIs(t, err, failure)
	require.ErrorIs(t, err, context.Canceled)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestOptimizationCancellationKeepsDurableProgress(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempMediaDB(t)
	defer cleanup()
	require.NoError(t, db.SetOptimizationStatus(IndexingStatusPending))
	require.NoError(t, db.SetOptimizationStep("page_prefetch"))
	originalCtx := db.ctx
	ctx, cancel := context.WithCancel(originalCtx)
	defer cancel()
	db.ctx = ctx
	lease, err := db.AcquireMediaWrite(database.MediaWriteOperationOptimization)
	require.NoError(t, err)
	var callbacks []bool
	err = db.RunBackgroundOptimizationWithLease(func(active bool) {
		callbacks = append(callbacks, active)
		if active {
			cancel()
		}
	}, nil, lease)
	require.ErrorIs(t, err, context.Canceled)
	db.ctx = originalCtx
	step, err := db.GetOptimizationStep()
	require.NoError(t, err)
	assert.Equal(t, "page_prefetch", step)
	status, err := db.GetOptimizationStatus()
	require.NoError(t, err)
	assert.Equal(t, IndexingStatusRunning, status, "running checkpoint remains resumable after cancellation")
	assert.Equal(t, []bool{true, false}, callbacks)
	assert.Equal(t, database.MediaWriteOperationNone, db.ActiveMediaWriteOperation())
}
