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
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	testsqlmock "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	clientSelectByTokenRe = `SELECT DBID, ClientID, ClientName, AuthToken, ` +
		`PairingKey, CreatedAt, LastSeenAt FROM Clients WHERE AuthToken = \?`
	clientSelectListRe = `SELECT DBID, ClientID, ClientName, AuthToken, ` +
		`PairingKey, CreatedAt, LastSeenAt FROM Clients ORDER BY CreatedAt DESC`
)

var clientRowColumns = []string{
	"DBID", "ClientID", "ClientName", "AuthToken", "PairingKey", "CreatedAt", "LastSeenAt",
}

func newTestClient() *database.Client {
	//nolint:gosec // test fixture; AuthToken is opaque test data, not a credential
	return &database.Client{
		ClientID:   "client-uuid-1",
		ClientName: "Test App",
		AuthToken:  "auth-token-uuid",
		PairingKey: []byte("0123456789abcdef0123456789abcdef"),
		CreatedAt:  1700000000,
		LastSeenAt: 1700000100,
	}
}

func TestSqlCreateClient_Success(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	c := newTestClient()
	mock.ExpectQuery(`INSERT INTO Clients`).
		WithArgs(c.ClientID, c.ClientName, c.AuthToken, c.PairingKey, c.CreatedAt, c.LastSeenAt).
		WillReturnRows(sqlmock.NewRows([]string{"DBID"}).AddRow(int64(42)))

	err = sqlCreateClient(context.Background(), db, c)
	require.NoError(t, err)
	assert.Equal(t, int64(42), c.DBID)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestSqlCreateClient_RejectsAuthTokenWithColon pins the constraint that
// auth tokens cannot contain ':' — the encryption AAD scheme uses
// `<authToken>:ws` and would become ambiguous if the token itself
// contained a colon. The check is enforced both in Go (clean error) and
// at the schema level via a CHECK constraint.
func TestSqlCreateClient_RejectsAuthTokenWithColon(t *testing.T) {
	t.Parallel()
	db, _, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	c := newTestClient()
	c.AuthToken = "evil:token"
	err = sqlCreateClient(context.Background(), db, c)
	require.ErrorIs(t, err, ErrInvalidAuthToken)
}

func TestSqlCreateClient_DatabaseError(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	c := newTestClient()
	mock.ExpectQuery(`INSERT INTO Clients`).
		WithArgs(c.ClientID, c.ClientName, c.AuthToken, c.PairingKey, c.CreatedAt, c.LastSeenAt).
		WillReturnError(sqlmock.ErrCancelled)

	err = sqlCreateClient(context.Background(), db, c)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to insert client")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlGetClientByToken_Success(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	c := newTestClient()
	mock.ExpectQuery(clientSelectByTokenRe).
		WithArgs(c.AuthToken).
		WillReturnRows(sqlmock.NewRows(clientRowColumns).
			AddRow(int64(7), c.ClientID, c.ClientName, c.AuthToken, c.PairingKey, c.CreatedAt, c.LastSeenAt))

	got, err := sqlGetClientByToken(context.Background(), db, c.AuthToken)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, int64(7), got.DBID)
	assert.Equal(t, c.ClientID, got.ClientID)
	assert.Equal(t, c.ClientName, got.ClientName)
	assert.Equal(t, c.AuthToken, got.AuthToken)
	assert.Equal(t, c.PairingKey, got.PairingKey)
	assert.Equal(t, c.CreatedAt, got.CreatedAt)
	assert.Equal(t, c.LastSeenAt, got.LastSeenAt)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlGetClientByToken_NotFound(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectQuery(clientSelectByTokenRe).
		WithArgs("missing").
		WillReturnRows(sqlmock.NewRows(clientRowColumns))

	got, err := sqlGetClientByToken(context.Background(), db, "missing")
	require.Error(t, err)
	assert.Nil(t, got)
	assert.Contains(t, err.Error(), "client not found")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlListClients_Success(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	key1 := []byte("key-1-key-1-key-1-key-1-key-1-12")
	key2 := []byte("key-2-key-2-key-2-key-2-key-2-12")
	rows := sqlmock.NewRows(clientRowColumns).
		AddRow(int64(1), "id-1", "App One", "tok-1", key1, int64(1000), int64(2000)).
		AddRow(int64(2), "id-2", "App Two", "tok-2", key2, int64(1100), int64(2100))

	mock.ExpectQuery(clientSelectListRe).WillReturnRows(rows)

	got, err := sqlListClients(context.Background(), db)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "id-1", got[0].ClientID)
	assert.Equal(t, "id-2", got[1].ClientID)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlListClients_Empty(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectQuery(clientSelectListRe).
		WillReturnRows(sqlmock.NewRows(clientRowColumns))

	got, err := sqlListClients(context.Background(), db)
	require.NoError(t, err)
	assert.Empty(t, got)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlDeleteClient_Success(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectExec(`DELETE FROM Clients WHERE ClientID = \?`).
		WithArgs("client-uuid-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	err = sqlDeleteClient(context.Background(), db, "client-uuid-1")
	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlDeleteClient_NotFound(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectExec(`DELETE FROM Clients WHERE ClientID = \?`).
		WithArgs("missing").
		WillReturnResult(sqlmock.NewResult(0, 0))

	err = sqlDeleteClient(context.Background(), db, "missing")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "client not found")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlUpdateClientLastSeen_Success(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectExec(`UPDATE Clients SET LastSeenAt = \? WHERE AuthToken = \?`).
		WithArgs(int64(1700001000), "tok").
		WillReturnResult(sqlmock.NewResult(0, 1))

	err = sqlUpdateClientLastSeen(context.Background(), db, "tok", 1700001000)
	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlCountClients_Success(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM Clients`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(5))

	got, err := sqlCountClients(context.Background(), db)
	require.NoError(t, err)
	assert.Equal(t, 5, got)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlCountClients_Empty(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM Clients`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

	got, err := sqlCountClients(context.Background(), db)
	require.NoError(t, err)
	assert.Equal(t, 0, got)
	assert.NoError(t, mock.ExpectationsWereMet())
}
