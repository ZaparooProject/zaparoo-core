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
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	testsqlmock "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSQLMarkMediaHistorySynced_ChunksFullUploadBatch(t *testing.T) {
	t.Parallel()

	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	updatedAt := time.Now().Truncate(time.Second)
	refs := make([]database.MediaHistorySyncRef, 500)
	for i := range refs {
		refs[i] = database.MediaHistorySyncRef{DBID: int64(i + 1), UpdatedAt: updatedAt}
	}

	mock.ExpectExec(`UPDATE MediaHistory SET SyncedAt.*WHERE \(DBID, UpdatedAt\) IN`).
		WillReturnResult(sqlmock.NewResult(0, mediaHistorySyncMarkChunkSize))
	mock.ExpectExec(`UPDATE MediaHistory SET SyncedAt.*WHERE \(DBID, UpdatedAt\) IN`).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err = sqlMarkMediaHistorySynced(t.Context(), db, refs, updatedAt.Add(time.Second))
	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}
