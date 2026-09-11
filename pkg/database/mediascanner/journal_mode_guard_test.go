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
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediadb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/require"
)

// TestNewNamesIndex_NonWALJournalModeFailsInsteadOfHanging exercises issue
// #1279's deadlock characterisation directly: indexing batches one write
// transaction per system while the next system's state load reads on a
// separate pool connection. Under WAL that read sees the last committed
// snapshot; under a rollback journal the writer's exclusive lock blocks it
// until commit, which never arrives because that read is on the critical
// path to reaching it. NewNamesIndex must reject a non-WAL database up front
// with a clear error instead of hanging mid-index — this test fails the slow
// way (timeout) if that guard regresses.
func TestNewNamesIndex_NonWALJournalModeFailsInsteadOfHanging(t *testing.T) {
	t.Parallel()

	// Launchers/RootDirs are deliberately left unstubbed: the WAL guard fires
	// before NewNamesIndex ever reaches launcher discovery, so a call to
	// either would itself indicate the guard regressed to running too late.
	tempDir := t.TempDir()
	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("test-platform")
	mockPlatform.On("Settings").Return(platforms.Settings{DataDir: tempDir})

	mediaDB, err := mediadb.OpenMediaDB(context.Background(), mockPlatform)
	require.NoError(t, err)
	defer func() { _ = mediaDB.Close() }()
	// Keep one pooled connection so the production DSN cannot open another and
	// restore WAL after this test deliberately switches the database to DELETE.
	sqlDB := mediaDB.UnsafeGetSQLDb()
	sqlDB.SetMaxOpenConns(1)
	var journalMode string
	require.NoError(t, sqlDB.QueryRowContext(context.Background(),
		"PRAGMA journal_mode=DELETE").Scan(&journalMode))
	require.Equal(t, "delete", journalMode)

	userDB, userCleanup := testhelpers.NewInMemoryUserDB(t)
	defer userCleanup()

	db := &database.Database{MediaDB: mediaDB, UserDB: userDB}

	fs := testhelpers.NewMemoryFS()
	cfg, err := testhelpers.NewTestConfig(fs, t.TempDir())
	require.NoError(t, err)

	done := make(chan error, 1)
	go func() {
		_, indexErr := NewNamesIndex(context.Background(), mockPlatform, cfg, nil, db, func(IndexStatus) {}, nil)
		done <- indexErr
	}()

	select {
	case indexErr := <-done:
		require.Error(t, indexErr)
		require.Contains(t, indexErr.Error(), "not in WAL journal mode")
	case <-time.After(10 * time.Second):
		t.Fatal("NewNamesIndex did not return promptly under a rollback journal; the WAL guard did not fire")
	}
}

// TestCheckWALJournalMode_NilHandleIsNotFatal pins the guard's tolerance for a
// MediaDBI with no open handle. Only a database that is definitely in the wrong
// journal mode may fail indexing; an inability to determine the mode at all
// must not, and must not panic either. This path used to be covered by a
// recover() in the caller, which meant a nil dereference anywhere inside the
// check would have been silently swallowed as the same "cannot verify" case.
func TestCheckWALJournalMode_NilHandleIsNotFatal(t *testing.T) {
	t.Parallel()

	mockDB := testhelpers.NewMockMediaDBI()

	var err error
	require.NotPanics(t, func() {
		err = checkWALJournalMode(context.Background(), mockDB)
	})
	require.NoError(t, err, "an unverifiable journal mode must let indexing proceed, not fail it")
}
