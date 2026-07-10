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
	"os"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	backupsvc "github.com/ZaparooProject/zaparoo-core/v2/pkg/service/backup"
	testinghelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	testifymock "github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func newBackupTestEnv(t *testing.T) requests.RequestEnv {
	t.Helper()
	rootDir := t.TempDir()
	configDir := filepath.Join(rootDir, "config")
	dataDir := filepath.Join(rootDir, "data")
	tempDir := filepath.Join(rootDir, "tmp")
	logDir := filepath.Join(rootDir, "logs")
	require.NoError(t, os.MkdirAll(configDir, 0o750))
	require.NoError(t, os.MkdirAll(dataDir, 0o750))
	require.NoError(t, os.MkdirAll(tempDir, 0o750))
	require.NoError(t, os.MkdirAll(logDir, 0o750))
	cfg, err := config.NewConfig(configDir, config.BaseDefaults)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dataDir, "frontend.toml"), []byte("enabled=true\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dataDir, config.TUIFile), []byte("theme=\"default\"\n"), 0o600))
	userDBPath := filepath.Join(dataDir, config.UserDbFile)
	require.NoError(t, os.WriteFile(userDBPath, []byte("test user db snapshot"), 0o600))
	mockUserDB := testinghelpers.NewMockUserDBI()
	mockUserDB.On("Backup", "local-zip", false).Return(database.BackupInfo{
		Name:  "backup-20260624-150405-000000001-auto.db",
		Path:  userDBPath,
		Valid: true,
	}, nil)
	mockUserDB.On("GetDBPath").Return(userDBPath)
	mockUserDB.On("RestoreBackup", testifymock.AnythingOfType("string")).Return(database.RestoreInfo{
		RestoredFrom: database.BackupInfo{Name: "staged.db", Valid: true},
	}, nil)
	pl := mocks.NewMockPlatform()
	pl.On("ID").Return("mock-platform")
	pl.On("Settings").Return(platforms.Settings{
		DataDir: dataDir, ConfigDir: configDir, TempDir: tempDir, LogDir: logDir,
	})
	return requests.RequestEnv{
		Context:  context.Background(),
		Config:   cfg,
		Platform: pl,
		Database: &database.Database{UserDB: mockUserDB},
		IsLocal:  true,
	}
}

func TestHandleBackup_Success(t *testing.T) {
	env := newBackupTestEnv(t)

	result, err := HandleBackup(env)
	require.NoError(t, err)
	info, ok := result.(backupsvc.Info)
	require.True(t, ok)
	assert.True(t, info.Valid)
	assert.Contains(t, info.Name, "backup-")
	assert.Contains(t, info.Name, ".zip")
	assert.NotZero(t, info.Categories[backupsvc.CategoryZaparoo].Files)
}

func TestHandleBackupList_Success(t *testing.T) {
	env := newBackupTestEnv(t)
	_, err := HandleBackup(env)
	require.NoError(t, err)

	result, err := HandleBackupList(env)
	require.NoError(t, err)
	backups, ok := result.([]backupsvc.ListInfo)
	require.True(t, ok)
	require.Len(t, backups, 1)
	assert.NotEmpty(t, backups[0].Name)
	assert.NotZero(t, backups[0].Size)
}

func TestHandleBackupInspect_Success(t *testing.T) {
	env := newBackupTestEnv(t)
	created, err := HandleBackup(env)
	require.NoError(t, err)
	backupInfo, ok := created.(backupsvc.Info)
	require.True(t, ok)

	params, err := json.Marshal(map[string]string{"name": backupInfo.Name})
	require.NoError(t, err)
	env.Params = params

	result, err := HandleBackupInspect(env)
	require.NoError(t, err)
	inspected, ok := result.(backupsvc.Info)
	require.True(t, ok)
	assert.True(t, inspected.Valid)
	assert.NotEmpty(t, inspected.Categories)
}

func TestHandleBackupDelete_Success(t *testing.T) {
	env := newBackupTestEnv(t)
	created, err := HandleBackup(env)
	require.NoError(t, err)
	backupInfo, ok := created.(backupsvc.Info)
	require.True(t, ok)

	params, err := json.Marshal(map[string]string{"name": backupInfo.Name})
	require.NoError(t, err)
	env.Params = params

	result, err := HandleBackupDelete(env)
	require.NoError(t, err)
	assert.Equal(t, NoContent{}, result)

	_, err = backupsvc.NewManager(env.Config, env.Platform, env.Database).Inspect(backupInfo.Name)
	require.Error(t, err)
}

func TestHandleBackupRestore_Success(t *testing.T) {
	env := newBackupTestEnv(t)
	created, err := HandleBackup(env)
	require.NoError(t, err)
	backupInfo, ok := created.(backupsvc.Info)
	require.True(t, ok)

	params, err := json.Marshal(map[string]string{"name": backupInfo.Name})
	require.NoError(t, err)
	env.Params = params

	result, err := HandleBackupRestore(env)
	require.NoError(t, err)
	info, ok := result.(backupsvc.RestoreInfo)
	require.True(t, ok)
	assert.Equal(t, backupInfo.Name, info.RestoredFrom.Name)
	require.NotNil(t, info.PreRestoreBackup)
	assert.True(t, info.PreRestoreBackup.Valid)
}

func TestHandleBackupRestore_MissingName(t *testing.T) {
	env := newBackupTestEnv(t)
	params, err := json.Marshal(map[string]string{})
	require.NoError(t, err)
	env.Params = params

	_, err = HandleBackupRestore(env)
	require.Error(t, err)
}

func TestHandleBackupStatus_AllowsNonLocal(t *testing.T) {
	env := newBackupTestEnv(t)
	env.IsLocal = false
	env.Database = nil

	result, err := HandleBackupStatus(env)
	require.NoError(t, err)
	status, ok := result.(models.BackupStatusResponse)
	require.True(t, ok)
	assert.True(t, status.Local.Enabled)
	assert.Equal(t, config.DefaultBackupRemoteSchedule, status.Remote.Schedule)
}

func TestHandleBackup_RejectsNonLocal(t *testing.T) {
	env := newBackupTestEnv(t)
	env.IsLocal = false

	_, err := HandleBackup(env)
	require.Error(t, err)
}

func TestHandleBackupList_RejectsNonLocal(t *testing.T) {
	env := newBackupTestEnv(t)
	env.IsLocal = false

	_, err := HandleBackupList(env)
	require.Error(t, err)
}

func TestHandleBackupRestore_RejectsNonLocal(t *testing.T) {
	env := newBackupTestEnv(t)
	env.IsLocal = false
	params, err := json.Marshal(map[string]string{"name": "backup-a-manual.zip"})
	require.NoError(t, err)
	env.Params = params

	_, err = HandleBackupRestore(env)
	require.Error(t, err)
}

func TestHandleBackup_RejectsUnavailableDatabase(t *testing.T) {
	env := newBackupTestEnv(t)
	env.Database = &database.Database{UserDB: nil}

	_, err := HandleBackup(env)
	require.Error(t, err)
}
