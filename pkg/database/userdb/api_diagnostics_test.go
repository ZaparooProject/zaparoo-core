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

package userdb

import (
	"testing"

	testsqlmock "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAPIDiagnosticsOnlyReadsPoolCounters(t *testing.T) {
	t.Parallel()
	var nilDB *UserDB
	assert.False(t, nilDB.APIDiagnostics().Pool.Available)
	db := &UserDB{}
	assert.False(t, db.APIDiagnostics().Pool.Available)
	sqlDB, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	t.Cleanup(func() {
		mock.ExpectClose()
		require.NoError(t, sqlDB.Close())
		require.NoError(t, mock.ExpectationsWereMet())
	})
	db.sql.Store(sqlDB)
	sqlDB.SetMaxOpenConns(3)
	assert.Equal(t, 3, db.APIDiagnostics().Pool.Max)
	assert.True(t, db.APIDiagnostics().Pool.Available)
	require.NoError(t, mock.ExpectationsWereMet())
}
