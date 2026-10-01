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

package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/userdb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/inbox"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedUserDB creates a migrated user database and returns the backup taken of
// it, when withBackup is set.
func seedUserDB(ctx context.Context, t *testing.T, pl platforms.Platform, withBackup bool) *database.BackupInfo {
	t.Helper()
	db, err := userdb.OpenUserDB(ctx, pl)
	require.NoError(t, err)
	require.NoError(t, db.MigrateUp())
	var backup *database.BackupInfo
	if withBackup {
		info, backupErr := db.Backup("test", true)
		require.NoError(t, backupErr)
		backup = &info
	}
	require.NoError(t, db.Close())
	return backup
}

// corruptUserDB overwrites the user database with bytes that are not a SQLite
// file, so opening it fails the way a ruined file does.
func corruptUserDB(t *testing.T, dataDir string) {
	t.Helper()
	junk := make([]byte, 8192)
	for i := range junk {
		junk[i] = byte(i%251) ^ 0xA5
	}
	for _, name := range []string{config.UserDbFile + "-wal", config.UserDbFile + "-shm"} {
		if err := os.Remove(filepath.Join(dataDir, name)); err != nil {
			require.ErrorIs(t, err, os.ErrNotExist)
		}
	}
	require.NoError(t, os.WriteFile(filepath.Join(dataDir, config.UserDbFile), junk, 0o600))
}

func TestMakeDatabase_HealthyUserDBReportsNoRecovery(t *testing.T) {
	ctx := context.Background()
	pl, _ := newMediaDBPlatform(t)
	seedUserDB(ctx, t, pl, true)

	db, _, err := makeDatabase(ctx, pl)
	t.Cleanup(func() { closeDatabase(db) })
	require.NoError(t, err)
	assert.Nil(t, db.UserDBRecovery, "a normal open must not tell the user anything was wrong")
}

func TestMakeDatabase_CorruptUserDBRestoredFromBackupIsReported(t *testing.T) {
	ctx := context.Background()
	pl, dataDir := newMediaDBPlatform(t)
	backup := seedUserDB(ctx, t, pl, true)
	corruptUserDB(t, dataDir)

	db, _, err := makeDatabase(ctx, pl)
	t.Cleanup(func() { closeDatabase(db) })
	require.NoError(t, err, "a corrupt user database with a backup must not stop startup")
	require.NotNil(t, db.UserDBRecovery, "the caller has to know saved data was replaced")
	require.NotNil(t, db.UserDBRecovery.RestoredFrom, "a valid backup existed, so one was restored")
	assert.Equal(t, backup.Name, db.UserDBRecovery.RestoredFrom.Name)
}

func TestMakeDatabase_CorruptUserDBWithoutBackupIsReportedAsFresh(t *testing.T) {
	ctx := context.Background()
	pl, dataDir := newMediaDBPlatform(t)
	seedUserDB(ctx, t, pl, false)
	corruptUserDB(t, dataDir)

	db, _, err := makeDatabase(ctx, pl)
	t.Cleanup(func() { closeDatabase(db) })
	require.NoError(t, err, "a corrupt user database must not stop startup")
	require.NotNil(t, db.UserDBRecovery, "starting empty is the loss most worth telling the user about")
	assert.Nil(t, db.UserDBRecovery.RestoredFrom, "no backup was valid, so nothing was restored")
}

func TestNotifyUserDBRecovery(t *testing.T) {
	backupTime := time.Date(2026, time.September, 28, 14, 30, 0, 0, time.UTC)
	tests := []struct {
		recovery  *database.UserDBRecovery
		name      string
		wantTitle string
		wantBody  string
	}{
		{
			name: "restored from a backup",
			recovery: &database.UserDBRecovery{
				RestoredFrom: &database.BackupInfo{Name: "backup-20260928-143000-auto.db", CreatedAt: backupTime},
			},
			wantTitle: "Saved data was restored from a backup after damage was found",
			wantBody:  "backup-20260928-143000-auto.db, made on 28 Sep 2026 14:30 UTC",
		},
		{
			name:      "no valid backup",
			recovery:  &database.UserDBRecovery{},
			wantTitle: "Saved data was reset after damage was found",
			wantBody:  "No valid backup was available",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			pl, _ := newMediaDBPlatform(t)

			db, _, err := makeDatabase(ctx, pl)
			t.Cleanup(func() { closeDatabase(db) })
			require.NoError(t, err)

			st, _ := state.NewState(pl, "test-boot-uuid")
			t.Cleanup(st.StopService)
			st.SetInbox(inbox.NewService(db.UserDB, st.Notifications))

			notifyUserDBRecovery(st, tt.recovery)

			messages, err := db.UserDB.GetInboxMessages()
			require.NoError(t, err)
			require.Len(t, messages, 1)
			assert.Equal(t, inbox.CategoryUserDBCorruptionRecovery, messages[0].Category)
			assert.Equal(t, inbox.SeverityWarning, messages[0].Severity)
			assert.Equal(t, tt.wantTitle, messages[0].Title)
			assert.Contains(t, messages[0].Body, tt.wantBody)
			assert.Contains(t, messages[0].Body, "check the device's storage")
		})
	}
}

func TestNotifyUserDBRecovery_NothingToReport(t *testing.T) {
	ctx := context.Background()
	pl, _ := newMediaDBPlatform(t)

	db, _, err := makeDatabase(ctx, pl)
	t.Cleanup(func() { closeDatabase(db) })
	require.NoError(t, err)

	st, _ := state.NewState(pl, "test-boot-uuid")
	t.Cleanup(st.StopService)
	st.SetInbox(inbox.NewService(db.UserDB, st.Notifications))

	notifyUserDBRecovery(st, nil)

	messages, err := db.UserDB.GetInboxMessages()
	require.NoError(t, err)
	assert.Empty(t, messages, "a normal start must leave the inbox alone")
}
