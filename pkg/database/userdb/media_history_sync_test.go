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

// The sweep reads an empty batch as "nothing left to backfill" and stamps its
// marker, so every read fault here must surface as an error instead of an
// empty page that would silently skip the rest of the history.
func TestSQLGetMediaHistoryIdentityBackfillBatch_ReadFaultsAreErrors(t *testing.T) {
	t.Parallel()

	const query = `SELECT DBID, SystemID, MediaPath\s+FROM MediaHistory`
	tests := []struct {
		arrange func(mock sqlmock.Sqlmock)
		name    string
		wantErr string
	}{
		{
			name: "query fails",
			arrange: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(query).WillReturnError(sqlmock.ErrCancelled)
			},
			wantErr: "failed to query media history identity backfill batch",
		},
		{
			name: "row does not scan",
			arrange: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(query).WillReturnRows(
					sqlmock.NewRows([]string{"DBID", "SystemID", "MediaPath"}).
						AddRow("not-a-dbid", "SNES", "/games/Game.sfc"),
				)
			},
			wantErr: "failed to scan media history identity backfill row",
		},
		{
			name: "iteration fails midway",
			arrange: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(query).WillReturnRows(
					sqlmock.NewRows([]string{"DBID", "SystemID", "MediaPath"}).
						AddRow(int64(1), "SNES", "/games/Game.sfc").
						RowError(0, sqlmock.ErrCancelled),
				)
			},
			wantErr: "error iterating media history identity backfill rows",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			db, mock, err := testsqlmock.NewSQLMock()
			require.NoError(t, err)
			defer func() { _ = db.Close() }()
			tt.arrange(mock)

			batch, err := sqlGetMediaHistoryIdentityBackfillBatch(
				t.Context(), db, 0, database.CurrentMediaIdentityPolicyVersion, 10,
			)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
			assert.Nil(t, batch)
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
