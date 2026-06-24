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

package methods

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleBackup_Success(t *testing.T) {
	t.Parallel()

	mockUserDB := helpers.NewMockUserDBI()
	mockUserDB.On("Backup", "manual", true).Return(database.BackupInfo{
		Name:  "backup-20260624-150405-000000001-manual.db",
		Valid: true,
	}, nil)

	env := requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{UserDB: mockUserDB},
		IsLocal:  true,
	}

	result, err := HandleBackup(env)
	require.NoError(t, err)
	info, ok := result.(database.BackupInfo)
	require.True(t, ok)
	assert.Equal(t, "backup-20260624-150405-000000001-manual.db", info.Name)
	mockUserDB.AssertExpectations(t)
}

func TestHandleBackupList_Success(t *testing.T) {
	t.Parallel()

	mockUserDB := helpers.NewMockUserDBI()
	mockUserDB.On("ListBackups").Return([]database.BackupInfo{
		{Name: "backup-a-manual.db", Valid: true},
		{Name: "backup-b-auto.db", Valid: true},
	}, nil)

	env := requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{UserDB: mockUserDB},
		IsLocal:  true,
	}

	result, err := HandleBackupList(env)
	require.NoError(t, err)
	backups, ok := result.([]database.BackupInfo)
	require.True(t, ok)
	require.Len(t, backups, 2)
	mockUserDB.AssertExpectations(t)
}

func TestHandleBackupRestore_Success(t *testing.T) {
	t.Parallel()

	mockUserDB := helpers.NewMockUserDBI()
	mockUserDB.On("RestoreBackup", "backup-a-manual.db").Return(database.RestoreInfo{
		RestoredFrom: database.BackupInfo{Name: "backup-a-manual.db", Valid: true},
	}, nil)

	params, err := json.Marshal(map[string]string{"name": "backup-a-manual.db"})
	require.NoError(t, err)

	env := requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{UserDB: mockUserDB},
		IsLocal:  true,
		Params:   params,
	}

	result, err := HandleBackupRestore(env)
	require.NoError(t, err)
	info, ok := result.(database.RestoreInfo)
	require.True(t, ok)
	assert.Equal(t, "backup-a-manual.db", info.RestoredFrom.Name)
	mockUserDB.AssertExpectations(t)
}

func TestHandleBackupRestore_MissingName(t *testing.T) {
	t.Parallel()

	mockUserDB := helpers.NewMockUserDBI()
	params, err := json.Marshal(map[string]string{})
	require.NoError(t, err)

	env := requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{UserDB: mockUserDB},
		IsLocal:  true,
		Params:   params,
	}

	_, err = HandleBackupRestore(env)
	require.Error(t, err)
	mockUserDB.AssertNotCalled(t, "RestoreBackup")
}

func TestHandleBackup_RejectsNonLocal(t *testing.T) {
	t.Parallel()

	mockUserDB := helpers.NewMockUserDBI()
	env := requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{UserDB: mockUserDB},
		IsLocal:  false,
	}

	_, err := HandleBackup(env)
	require.Error(t, err)
	mockUserDB.AssertNotCalled(t, "Backup")
}
