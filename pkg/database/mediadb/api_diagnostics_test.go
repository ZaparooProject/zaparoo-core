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
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/internal/apidiag"
	testsqlmock "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAPIDiagnosticsDoesNotQueryOrWaitForTransaction(t *testing.T) {
	t.Parallel()
	sqlDB, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	t.Cleanup(func() {
		mock.ExpectClose()
		require.NoError(t, sqlDB.Close())
		require.NoError(t, mock.ExpectationsWereMet())
	})
	sqlDB.SetMaxOpenConns(2)
	conn, err := sqlDB.Conn(t.Context())
	require.NoError(t, err)
	defer func() { require.NoError(t, conn.Close()) }()
	db := &MediaDB{inTransaction: true}
	db.sql.Store(sqlDB)
	db.indexingCacheBoost.Store(true)
	db.isOptimizing.Store(true)
	db.recreating.Store(true)

	locked, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	go func() {
		db.sqlMu.Lock()
		close(locked)
		<-release
		db.sqlMu.Unlock()
		close(done)
	}()
	<-locked
	snapshot := db.APIDiagnostics()
	close(release)
	<-done
	assert.True(t, snapshot.Pool.Available)
	assert.Equal(t, 2, snapshot.Pool.Max)
	assert.Equal(t, 1, snapshot.Pool.InUse)
	assert.Equal(t, apidiag.Unknown, snapshot.Transaction)
	assert.Equal(t, apidiag.Active, snapshot.Indexing)
	assert.Equal(t, apidiag.Active, snapshot.Optimizing)
	assert.Equal(t, apidiag.Active, snapshot.Recovery)
	assert.Equal(t, apidiag.Active, db.APIDiagnostics().Transaction)
	require.NoError(t, mock.ExpectationsWereMet(), "diagnostics must not execute SQL")
}

func TestAPIDiagnosticsWithoutDatabase(t *testing.T) {
	t.Parallel()
	var db *MediaDB
	assert.False(t, db.APIDiagnostics().Pool.Available)
	assert.False(t, (&MediaDB{}).APIDiagnostics().Pool.Available)
}
