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
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUserDBBackupRestoreRoundTrip(t *testing.T) {
	userDB, cleanup := setupTempUserDB(t)
	defer cleanup()

	require.NoError(t, userDB.AddMapping(&database.Mapping{
		Label:    "Backup Test",
		Enabled:  true,
		Type:     MappingTypeID,
		Match:    MatchTypeExact,
		Pattern:  "backup-test-token",
		Override: "**launch.system:n64",
	}))

	backup, err := userDB.Backup("test", true)
	require.NoError(t, err)
	assert.True(t, backup.Valid)
	assert.Equal(t, "ok", backup.QuickCheck)
	assert.True(t, backup.Manual)
	assert.NotZero(t, backup.Size)

	mappings, err := userDB.GetAllMappings()
	require.NoError(t, err)
	require.Len(t, mappings, 1)
	require.NoError(t, userDB.DeleteMapping(mappings[0].DBID))

	restored, err := userDB.RestoreBackup(backup.Name)
	require.NoError(t, err)
	assert.Equal(t, backup.Name, restored.RestoredFrom.Name)
	require.NotNil(t, restored.PreRestoreBackup)
	assert.True(t, restored.PreRestoreBackup.Valid)

	mappings, err = userDB.GetAllMappings()
	require.NoError(t, err)
	require.Len(t, mappings, 1)
	assert.Equal(t, "backup-test-token", mappings[0].Pattern)
}

func TestUserDBEnsureRecentBackupReusesFreshBackup(t *testing.T) {
	userDB, cleanup := setupTempUserDB(t)
	defer cleanup()

	created, err := userDB.Backup("test", false)
	require.NoError(t, err)

	backup, didCreate, err := userDB.EnsureRecentBackup(24 * time.Hour)
	require.NoError(t, err)
	assert.False(t, didCreate)
	assert.Equal(t, created.Name, backup.Name)
}

// TestUserDBPruneAutoBackupsRetainsLimit verifies scheduled backups are pruned to the
// retention limit while a manual backup is never removed.
func TestUserDBPruneAutoBackupsRetainsLimit(t *testing.T) {
	userDB, cleanup := setupTempUserDB(t)
	defer cleanup()

	// Each scheduled (auto) backup triggers pruning at the end of Backup.
	for range autoBackupKeep + 2 {
		_, err := userDB.Backup("scheduled", false)
		require.NoError(t, err)
	}
	manual, err := userDB.Backup("test", true)
	require.NoError(t, err)

	backups, err := userDB.ListBackups()
	require.NoError(t, err)

	autoCount := 0
	manualPresent := false
	for _, b := range backups {
		if isAutoBackupName(b.Name) {
			autoCount++
		}
		if b.Name == manual.Name {
			manualPresent = true
		}
	}
	assert.Equal(t, autoBackupKeep, autoCount, "auto backups pruned to retention limit")
	assert.True(t, manualPresent, "manual backup must survive pruning")
}

// TestUserDBEnsureRecentBackupCreatesWhenAbsent covers the branch where no recent backup
// exists, so a scheduled one is created.
func TestUserDBEnsureRecentBackupCreatesWhenAbsent(t *testing.T) {
	userDB, cleanup := setupTempUserDB(t)
	defer cleanup()

	backup, didCreate, err := userDB.EnsureRecentBackup(24 * time.Hour)
	require.NoError(t, err)
	assert.True(t, didCreate, "a backup must be created when none exists")
	assert.True(t, backup.Valid)
	assert.False(t, backup.Manual, "a scheduled backup is an auto backup")
}

// TestUserDBRestoreBackupRejectsInvalidName verifies restore refuses names that escape the
// backup directory or aren't backup files, before the live connection is touched.
func TestUserDBRestoreBackupRejectsInvalidName(t *testing.T) {
	userDB, cleanup := setupTempUserDB(t)
	defer cleanup()

	for _, name := range []string{
		filepath.Join("..", "escape.db"),
		filepath.Join("sub", "backup-x.db"),
		"not-a-backup.txt",
	} {
		_, err := userDB.RestoreBackup(name)
		require.Error(t, err, "name %q must be rejected", name)
	}

	// Rejection happens before any Close, so the database is still usable.
	_, err := userDB.GetAllMappings()
	require.NoError(t, err)
}

// TestUserDBRestoreBackupRejectsInvalidBackup verifies a backup file that fails quick_check
// is reported invalid and refused, leaving the live database untouched.
func TestUserDBRestoreBackupRejectsInvalidBackup(t *testing.T) {
	userDB, cleanup := setupTempUserDB(t)
	defer cleanup()

	require.NoError(t, os.MkdirAll(userDB.backupDir(), 0o750))
	badPath := filepath.Join(userDB.backupDir(), "backup-00000000-000000-000000000-manual.db")
	require.NoError(t, os.WriteFile(badPath, []byte("not a sqlite database"), 0o600))

	backups, err := userDB.ListBackups()
	require.NoError(t, err)
	require.Len(t, backups, 1)
	assert.False(t, backups[0].Valid, "garbage file must fail quick_check")

	_, err = userDB.RestoreBackup(backups[0].Name)
	require.Error(t, err, "must refuse to restore an invalid backup")

	// The live database is untouched and usable.
	_, err = userDB.GetAllMappings()
	require.NoError(t, err)
}

// TestUserDBRecoverFromCorruptionRestoresBackup verifies the recovery flow preserves the
// damaged file and reinstates the most recent valid backup, leaving the connection usable.
func TestUserDBRecoverFromCorruptionRestoresBackup(t *testing.T) {
	userDB, cleanup := setupTempUserDB(t)
	defer cleanup()

	require.NoError(t, userDB.AddMapping(&database.Mapping{
		Label:    "Keep",
		Enabled:  true,
		Type:     MappingTypeID,
		Match:    MatchTypeExact,
		Pattern:  "keep-me",
		Override: "**launch.system:n64",
	}))

	backup, err := userDB.Backup("test", true)
	require.NoError(t, err)
	require.True(t, backup.Valid)

	// A mapping added after the backup must not survive recovery from that backup.
	require.NoError(t, userDB.AddMapping(&database.Mapping{
		Label:    "Discard",
		Enabled:  true,
		Type:     MappingTypeID,
		Match:    MatchTypeExact,
		Pattern:  "discard-me",
		Override: "**launch.system:n64",
	}))

	info, err := userDB.RecoverFromCorruption()
	require.NoError(t, err)
	assert.Equal(t, backup.Name, info.RestoredFrom.Name)

	// The pre-recovery file is preserved alongside the database for forensics.
	_, statErr := os.Stat(userDB.GetDBPath() + database.CorruptMarkerSuffix + ".bak")
	require.NoError(t, statErr, "corrupt file should be preserved")

	mappings, err := userDB.GetAllMappings()
	require.NoError(t, err)
	require.Len(t, mappings, 1)
	assert.Equal(t, "keep-me", mappings[0].Pattern)
}

// TestUserDBRecoverFromCorruptionWithoutBackupCreatesFresh verifies that with no valid
// backup available, recovery still leaves a usable (empty) database rather than a dead one.
func TestUserDBRecoverFromCorruptionWithoutBackupCreatesFresh(t *testing.T) {
	userDB, cleanup := setupTempUserDB(t)
	defer cleanup()

	require.NoError(t, userDB.AddMapping(&database.Mapping{
		Label:    "Gone",
		Enabled:  true,
		Type:     MappingTypeID,
		Match:    MatchTypeExact,
		Pattern:  "gone",
		Override: "**launch.system:n64",
	}))

	info, err := userDB.RecoverFromCorruption()
	require.NoError(t, err)
	assert.Empty(t, info.RestoredFrom.Name, "no backup means nothing was restored")

	_, statErr := os.Stat(userDB.GetDBPath() + database.CorruptMarkerSuffix + ".bak")
	require.NoError(t, statErr, "corrupt file should be preserved")

	// The fresh database is usable and empty.
	mappings, err := userDB.GetAllMappings()
	require.NoError(t, err)
	assert.Empty(t, mappings)
}

// TestUserDBRestoreConcurrentReaders exercises the live-restore hazard: RestoreBackup
// closes and reopens the connection (swapping the atomic db.sql handle) while other
// goroutines query the database. Run with -race, it proves the handle swap is race-free.
// Concurrent queries during the swap may transiently fail (closed connection), which is
// expected during a restore; the test only requires no data race and no panic.
func TestUserDBRestoreConcurrentReaders(t *testing.T) {
	userDB, cleanup := setupTempUserDB(t)
	defer cleanup()

	require.NoError(t, userDB.AddMapping(&database.Mapping{
		Label:    "Concurrent",
		Enabled:  true,
		Type:     MappingTypeID,
		Match:    MatchTypeExact,
		Pattern:  "concurrent-token",
		Override: "**launch.system:n64",
	}))

	backup, err := userDB.Backup("test", true)
	require.NoError(t, err)

	var wg sync.WaitGroup
	stop := make(chan struct{})
	// Background readers and writers hammer the connection while restores swap it.
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				// Errors are acceptable mid-swap; we only care about the race detector.
				_, _ = userDB.GetAllMappings()
				_, _ = userDB.AddMediaHistory(&database.MediaHistoryEntry{
					StartTime:  time.Now(),
					SystemID:   "n64",
					LauncherID: "test",
					MediaPath:  "concurrent",
				})
			}
		}()
	}

	for range 10 {
		_, restoreErr := userDB.RestoreBackup(backup.Name)
		require.NoError(t, restoreErr)
	}

	close(stop)
	wg.Wait()

	// The database is fully usable after the concurrent restores.
	mappings, err := userDB.GetAllMappings()
	require.NoError(t, err)
	require.Len(t, mappings, 1)
}
