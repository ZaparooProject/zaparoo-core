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
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	testsqlmock "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/sqlmock"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type failStagedInstallFs struct {
	afero.Fs
	dbPath       string
	failRollback bool
}

type failDirectorySyncFile struct {
	afero.File
	fail bool
}

func (f *failDirectorySyncFile) Sync() error {
	if f.fail {
		return errors.New("injected directory sync failure")
	}
	if err := f.File.Sync(); err != nil {
		return fmt.Errorf("sync test directory: %w", err)
	}
	return nil
}

type failDirectorySyncFs struct {
	afero.Fs
	dir    string
	calls  int
	failAt int
}

type transientRenameFs struct {
	afero.Fs
	err      error
	failures int
	attempts int
}

// failRemoveFs refuses to unlink one named file, standing in for a file that
// cannot be removed because of permissions or an open handle on Windows.
type failRemoveFs struct {
	afero.Fs
	failBase string
}

func (fs *transientRenameFs) Rename(oldPath, newPath string) error {
	fs.attempts++
	if fs.attempts <= fs.failures {
		return fs.err
	}
	if err := fs.Fs.Rename(oldPath, newPath); err != nil {
		return fmt.Errorf("rename transient test file: %w", err)
	}
	return nil
}

func (fs *failDirectorySyncFs) Open(name string) (afero.File, error) {
	file, err := fs.Fs.Open(name)
	if err != nil {
		return nil, fmt.Errorf("open test directory: %w", err)
	}
	if filepath.Clean(name) != filepath.Clean(fs.dir) {
		return file, nil
	}
	fs.calls++
	return &failDirectorySyncFile{File: file, fail: fs.calls == fs.failAt}, nil
}

func (fs failStagedInstallFs) Rename(oldPath, newPath string) error {
	if newPath == fs.dbPath {
		switch {
		case strings.HasPrefix(filepath.Base(oldPath), ".userdb-restore-"):
			return errors.New("injected staged install failure")
		case fs.failRollback && strings.HasSuffix(oldPath, ".restore-rollback"):
			return errors.New("injected rollback failure")
		}
	}
	if err := fs.Fs.Rename(oldPath, newPath); err != nil {
		return fmt.Errorf("rename test path: %w", err)
	}
	return nil
}

func (fs failRemoveFs) Remove(name string) error {
	if filepath.Base(name) == fs.failBase {
		return errors.New("injected remove failure")
	}
	if err := fs.Fs.Remove(name); err != nil {
		return fmt.Errorf("remove test path: %w", err)
	}
	return nil
}

func TestRenameDatabaseFileWithRetry(t *testing.T) {
	t.Parallel()

	transientErr := errors.New("transient rename failure")
	for _, tt := range []struct {
		name             string
		failures         int
		expectedAttempts int
		retryable        bool
		expectError      bool
	}{
		{name: "retries transient error", retryable: true, failures: 2, expectedAttempts: 3},
		{name: "stops on permanent error", failures: 2, expectedAttempts: 1, expectError: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fs := &transientRenameFs{Fs: afero.NewMemMapFs(), err: transientErr, failures: tt.failures}
			dir := "rename-test"
			oldPath := filepath.Join(dir, "old.db")
			newPath := filepath.Join(dir, "new.db")
			require.NoError(t, fs.MkdirAll(dir, 0o750))
			require.NoError(t, afero.WriteFile(fs, oldPath, []byte("database"), 0o600))

			waits := 0
			err := renameDatabaseFileWithRetry(
				fs,
				oldPath,
				newPath,
				func(err error) bool { return tt.retryable && errors.Is(err, transientErr) },
				func() { waits++ },
			)
			if tt.expectError {
				require.ErrorIs(t, err, transientErr)
			} else {
				require.NoError(t, err)
				contents, readErr := afero.ReadFile(fs, newPath)
				require.NoError(t, readErr)
				assert.Equal(t, []byte("database"), contents)
			}
			assert.Equal(t, tt.expectedAttempts, fs.attempts)
			assert.Equal(t, tt.expectedAttempts-1, waits)
		})
	}
}

func TestReplaceDatabaseFromBackup_RestoresOriginalAfterInstallFailure(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "user.db")
	backupPath := filepath.Join(dir, "backup.db")
	require.NoError(t, os.WriteFile(dbPath, []byte("original"), 0o600))
	require.NoError(t, os.WriteFile(backupPath, []byte("replacement"), 0o600))

	fs := failStagedInstallFs{Fs: afero.NewOsFs(), dbPath: dbPath}
	err := replaceDatabaseFromBackup(fs, backupPath, dbPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to install staged user database backup")
	contents, readErr := afero.ReadFile(fs, dbPath)
	require.NoError(t, readErr)
	assert.Equal(t, []byte("original"), contents)
	_, rollbackErr := os.Stat(dbPath + ".restore-rollback")
	assert.True(t, os.IsNotExist(rollbackErr))
}

func TestReplaceDatabaseFromBackup_RestoresOriginalAfterDirectorySyncFailure(t *testing.T) {
	t.Parallel()

	for _, failAt := range []int{1, 2} {
		t.Run(fmt.Sprintf("sync %d", failAt), func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			dbPath := filepath.Join(dir, "user.db")
			backupPath := filepath.Join(dir, "backup.db")
			require.NoError(t, os.WriteFile(dbPath, []byte("original"), 0o600))
			require.NoError(t, os.WriteFile(backupPath, []byte("replacement"), 0o600))

			fs := &failDirectorySyncFs{Fs: afero.NewOsFs(), dir: dir, failAt: failAt}
			err := replaceDatabaseFromBackup(fs, backupPath, dbPath)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "sync")
			contents, readErr := os.ReadFile(dbPath) // #nosec G304 -- test-owned path.
			require.NoError(t, readErr)
			assert.Equal(t, []byte("original"), contents)
		})
	}
}

func TestReplaceDatabaseFromBackup_RetainsRollbackAfterRestoreFailure(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "user.db")
	backupPath := filepath.Join(dir, "backup.db")
	require.NoError(t, os.WriteFile(dbPath, []byte("original"), 0o600))
	require.NoError(t, os.WriteFile(backupPath, []byte("replacement"), 0o600))

	fs := failStagedInstallFs{Fs: afero.NewOsFs(), dbPath: dbPath, failRollback: true}
	err := replaceDatabaseFromBackup(fs, backupPath, dbPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to install staged user database backup")
	assert.Contains(t, err.Error(), "failed to restore original user database")
	rollbackContents, readErr := afero.ReadFile(fs, dbPath+".restore-rollback")
	require.NoError(t, readErr)
	assert.Equal(t, []byte("original"), rollbackContents)
}

func TestUserDBBackupForTransferRemovesClientCredentials(t *testing.T) {
	userDB, closeDB := setupTempUserDB(t)
	defer closeDB()

	const authToken = "portable-backup-must-not-contain-this-token"
	pairingKey := []byte("0123456789abcdef0123456789abcdef")
	require.NoError(t, userDB.CreateClient(&database.Client{
		ClientID:   "client-1",
		ClientName: "Admin",
		AuthToken:  authToken,
		PairingKey: pairingKey,
		Role:       "admin",
		CreatedAt:  time.Now().Unix(),
	}))

	portable, cleanup, err := userDB.BackupForTransfer(context.Background(), "test-transfer")
	require.NoError(t, err)
	require.NotNil(t, cleanup)

	portableDB, err := sql.Open("sqlite3", portable.Path+"?mode=ro&_query_only=ON")
	require.NoError(t, err)
	var clientCount int
	require.NoError(t, portableDB.QueryRowContext(
		context.Background(), "SELECT COUNT(*) FROM Clients",
	).Scan(&clientCount))
	require.NoError(t, portableDB.Close())
	assert.Zero(t, clientCount)

	portableBytes, err := os.ReadFile(portable.Path)
	require.NoError(t, err)
	assert.NotContains(t, string(portableBytes), authToken)
	assert.NotContains(t, string(portableBytes), string(pairingKey))

	fullBackups, err := userDB.ListBackups()
	require.NoError(t, err)
	require.NotEmpty(t, fullBackups)
	fullDB, err := sql.Open("sqlite3", fullBackups[0].Path+"?mode=ro&_query_only=ON")
	require.NoError(t, err)
	require.NoError(t, fullDB.QueryRowContext(
		context.Background(), "SELECT COUNT(*) FROM Clients",
	).Scan(&clientCount))
	require.NoError(t, fullDB.Close())
	assert.Equal(t, 1, clientCount)

	portablePath := portable.Path
	require.NoError(t, cleanup())
	_, err = os.Stat(portablePath)
	require.ErrorIs(t, err, os.ErrNotExist)
	require.NoError(t, cleanup(), "cleanup must be idempotent")
}

func TestUserDBBackupForTransferHonorsCancellation(t *testing.T) {
	t.Parallel()
	userDB, closeDB := setupTempUserDB(t)
	defer closeDB()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, cleanup, err := userDB.BackupForTransfer(ctx, "canceled-transfer")
	require.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, cleanup)
}

func TestUserDBRestoreBackupFailsWhenCorruptMarkerCannotBeCleared(t *testing.T) {
	userDB, cleanup := setupTempUserDB(t)
	defer cleanup()

	backup, err := userDB.Backup("test", true)
	require.NoError(t, err)
	markerPath := database.CorruptMarkerPath(userDB.GetDBPath())
	require.NoError(t, os.Mkdir(markerPath, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(markerPath, "blocker"), []byte("x"), 0o600))

	_, err = userDB.RestoreBackup(backup.Name)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to clear user database corrupt marker")
	assert.True(t, userDB.IsMarkedCorrupt(), "failed marker removal must remain a pending recovery signal")
}

func TestUserDBRecoverFromCorruption_BackupCleanupFailureRetainsMarker(t *testing.T) {
	userDB, cleanup := setupTempUserDB(t)
	defer cleanup()

	_, err := userDB.Backup("test", true)
	require.NoError(t, err)
	markerPath := database.CorruptMarkerPath(userDB.GetDBPath())
	require.NoError(t, os.Mkdir(markerPath, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(markerPath, "blocker"), []byte("x"), 0o600))

	_, err = userDB.RecoverFromCorruption()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to clear user database corrupt marker after recovery")
	assert.True(t, userDB.IsMarkedCorrupt())
}

func TestUserDBRecoverFromCorruption_FreshCleanupFailureRetainsMarker(t *testing.T) {
	userDB, cleanup := setupTempUserDB(t)
	defer cleanup()

	markerPath := database.CorruptMarkerPath(userDB.GetDBPath())
	require.NoError(t, os.Mkdir(markerPath, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(markerPath, "blocker"), []byte("x"), 0o600))

	_, err := userDB.RecoverFromCorruption()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to clear user database corrupt marker after fresh recovery")
	assert.True(t, userDB.IsMarkedCorrupt())
}

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

// TestUserDBRestoreRefusesWhileQueriesInFlight covers the crash that motivated
// closeAndDrain: sql.DB.Close leaves a checked-out connection running, and
// replacing the database files under it faults the process rather than failing
// the query. The restore must refuse instead, leaving disk and connection intact.
func TestUserDBRestoreRefusesWhileQueriesInFlight(t *testing.T) {
	userDB, cleanup := setupTempUserDB(t)
	defer cleanup()
	userDB.drainTimeout = 50 * time.Millisecond

	require.NoError(t, userDB.AddMapping(&database.Mapping{
		Label:    "Original",
		Enabled:  true,
		Type:     MappingTypeID,
		Match:    MatchTypeExact,
		Pattern:  "original-token",
		Override: "**launch.system:n64",
	}))
	backup, err := userDB.Backup("test", true)
	require.NoError(t, err)

	// An open transaction holds a connection checked out, which is what an
	// in-flight query looks like to the pool.
	tx, err := userDB.sql.Load().BeginTx(context.Background(), nil)
	require.NoError(t, err)

	_, restoreErr := userDB.RestoreBackup(backup.Name)
	require.ErrorIs(t, restoreErr, ErrConnDrainTimeout)

	require.NoError(t, tx.Rollback())

	// No file was touched, so nothing needs rolling back.
	dbPath := userDB.GetDBPath()
	assert.NoFileExists(t, dbPath+restoreRollbackSuffix)
	assert.FileExists(t, dbPath)

	// The connection was put back, so the database is still usable.
	mappings, err := userDB.GetAllMappings()
	require.NoError(t, err)
	require.Len(t, mappings, 1)
	assert.Equal(t, "Original", mappings[0].Label)
}

func TestUserDBCloseAndDrainSucceedsWhenIdle(t *testing.T) {
	userDB, cleanup := setupTempUserDB(t)
	defer cleanup()

	_, err := userDB.GetAllMappings()
	require.NoError(t, err)
	require.NoError(t, userDB.closeAndDrain())
	require.NoError(t, userDB.Open())
}

// A UserDB whose Open failed before a connection was stored still reaches
// closeAndDrain through RecoverFromCorruption, so there is nothing to drain and
// nothing to dereference.
func TestUserDBCloseAndDrainWithoutConnection(t *testing.T) {
	t.Parallel()
	require.NoError(t, (&UserDB{}).closeAndDrain())
}

// A pool that will not close leaves the connection state unknown, so the caller
// has to see the failure rather than go on to swap the files underneath it.
func TestUserDBCloseAndDrainReportsCloseFailure(t *testing.T) {
	t.Parallel()

	sqlDB, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	closeErr := errors.New("driver refused to close")
	mock.ExpectClose().WillReturnError(closeErr)

	userDB := &UserDB{}
	userDB.sql.Store(sqlDB)

	require.ErrorIs(t, userDB.closeAndDrain(), closeErr)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRecoverInterruptedRestore(t *testing.T) {
	t.Parallel()

	const (
		originalData  = "original-database"
		installedData = "installed-database"
	)

	tests := []struct {
		name          string
		dbContent     string
		rollback      string
		wantDBContent string
		wantRollback  bool
	}{
		{
			name:          "rollback without database restores original",
			rollback:      originalData,
			wantDBContent: originalData,
		},
		{
			name:          "rollback beside database is discarded",
			dbContent:     installedData,
			rollback:      originalData,
			wantDBContent: installedData,
		},
		{
			name:          "database without rollback is left alone",
			dbContent:     installedData,
			wantDBContent: installedData,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fs := afero.NewMemMapFs()
			dbDir := filepath.Join("data", "zaparoo")
			require.NoError(t, fs.MkdirAll(dbDir, 0o750))
			dbPath := filepath.Join(dbDir, "user.db")
			if tt.dbContent != "" {
				require.NoError(t, afero.WriteFile(fs, dbPath, []byte(tt.dbContent), 0o600))
			}
			if tt.rollback != "" {
				require.NoError(t, afero.WriteFile(
					fs, dbPath+restoreRollbackSuffix, []byte(tt.rollback), 0o600,
				))
			}

			require.NoError(t, recoverInterruptedRestore(fs, dbPath))

			got, err := afero.ReadFile(fs, dbPath)
			require.NoError(t, err)
			assert.Equal(t, tt.wantDBContent, string(got))
			exists, err := afero.Exists(fs, dbPath+restoreRollbackSuffix)
			require.NoError(t, err)
			assert.False(t, exists, "rollback file should not survive recovery")
		})
	}
}

func TestRecoverInterruptedRestoreRemovesStagedFiles(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	dbDir := filepath.Join("data", "zaparoo")
	require.NoError(t, fs.MkdirAll(dbDir, 0o750))
	dbPath := filepath.Join(dbDir, "user.db")
	require.NoError(t, afero.WriteFile(fs, dbPath, []byte("db"), 0o600))
	staged := filepath.Join(dbDir, ".userdb-restore-123456")
	require.NoError(t, afero.WriteFile(fs, staged, []byte("staged"), 0o600))

	require.NoError(t, recoverInterruptedRestore(fs, dbPath))

	exists, err := afero.Exists(fs, staged)
	require.NoError(t, err)
	assert.False(t, exists, "staged restore file should be removed")
	dbExists, err := afero.Exists(fs, dbPath)
	require.NoError(t, err)
	assert.True(t, dbExists, "database must survive staged cleanup")
}

// TestRecoverInterruptedRestoreRetainsRollbackWhenRenameFails covers what
// happens when recovery itself cannot complete. The rollback is the only copy
// of the user's data at that point, so it has to be left where it is and the
// failure reported, rather than the caller carrying on as if the database were
// simply absent.
func TestRecoverInterruptedRestoreRetainsRollbackWhenRenameFails(t *testing.T) {
	t.Parallel()

	base := afero.NewMemMapFs()
	dbDir := filepath.Join("data", "zaparoo")
	require.NoError(t, base.MkdirAll(dbDir, 0o750))
	dbPath := filepath.Join(dbDir, "user.db")
	rollbackPath := dbPath + restoreRollbackSuffix
	require.NoError(t, afero.WriteFile(base, rollbackPath, []byte("original"), 0o600))

	fs := failStagedInstallFs{Fs: base, dbPath: dbPath, failRollback: true}
	err := recoverInterruptedRestore(fs, dbPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to recover user database from interrupted restore")

	contents, err := afero.ReadFile(fs, rollbackPath)
	require.NoError(t, err)
	assert.Equal(t, "original", string(contents), "rollback must survive so recovery can be retried")
	dbExists, err := afero.Exists(fs, dbPath)
	require.NoError(t, err)
	assert.False(t, dbExists, "no half-recovered database should be left behind")
}

// TestRecoverInterruptedRestoreKeepsDatabaseWhenStaleRollbackRemoveFails checks
// the other direction: the restore did finish, so the rollback is only clutter.
// Failing to delete it must not disturb the installed database.
func TestRecoverInterruptedRestoreKeepsDatabaseWhenStaleRollbackRemoveFails(t *testing.T) {
	t.Parallel()

	base := afero.NewMemMapFs()
	dbDir := filepath.Join("data", "zaparoo")
	require.NoError(t, base.MkdirAll(dbDir, 0o750))
	dbPath := filepath.Join(dbDir, "user.db")
	rollbackPath := dbPath + restoreRollbackSuffix
	require.NoError(t, afero.WriteFile(base, dbPath, []byte("installed"), 0o600))
	require.NoError(t, afero.WriteFile(base, rollbackPath, []byte("original"), 0o600))

	fs := failRemoveFs{Fs: base, failBase: filepath.Base(rollbackPath)}
	require.NoError(t, recoverInterruptedRestore(fs, dbPath))

	contents, err := afero.ReadFile(fs, dbPath)
	require.NoError(t, err)
	assert.Equal(t, "installed", string(contents))
}

// TestRecoverInterruptedRestoreKeepsDatabaseWhenSyncFails pins the ordering:
// the rename is what recovers the data and the directory sync only makes it
// durable, so a sync failure is reported but must not cost the recovery.
func TestRecoverInterruptedRestoreKeepsDatabaseWhenSyncFails(t *testing.T) {
	t.Parallel()

	base := afero.NewMemMapFs()
	dbDir := filepath.Join("data", "zaparoo")
	require.NoError(t, base.MkdirAll(dbDir, 0o750))
	dbPath := filepath.Join(dbDir, "user.db")
	require.NoError(t, afero.WriteFile(base, dbPath+restoreRollbackSuffix, []byte("original"), 0o600))

	fs := &failDirectorySyncFs{Fs: base, dir: dbDir, failAt: 1}
	require.NoError(t, recoverInterruptedRestore(fs, dbPath))

	contents, err := afero.ReadFile(fs, dbPath)
	require.NoError(t, err)
	assert.Equal(t, "original", string(contents))
}

// TestRemoveStagedRestoreFilesContinuesAfterFailure guards the loop: staged
// files are full copies of the database, so one that cannot be removed must not
// leave the rest sitting on disk consuming space.
func TestRemoveStagedRestoreFilesContinuesAfterFailure(t *testing.T) {
	t.Parallel()

	base := afero.NewMemMapFs()
	dbDir := filepath.Join("data", "zaparoo")
	require.NoError(t, base.MkdirAll(dbDir, 0o750))
	stuck := filepath.Join(dbDir, ".userdb-restore-stuck")
	other := filepath.Join(dbDir, ".userdb-restore-other")
	require.NoError(t, afero.WriteFile(base, stuck, []byte("staged"), 0o600))
	require.NoError(t, afero.WriteFile(base, other, []byte("staged"), 0o600))

	fs := failRemoveFs{Fs: base, failBase: filepath.Base(stuck)}
	removeStagedRestoreFiles(fs, dbDir)

	stuckExists, err := afero.Exists(fs, stuck)
	require.NoError(t, err)
	assert.True(t, stuckExists)
	otherExists, err := afero.Exists(fs, other)
	require.NoError(t, err)
	assert.False(t, otherExists, "a file that cannot be removed must not stop the rest")
}

// TestUserDBOpenRecoversInterruptedRestore is the failure this protects against:
// without recovery, Open sees no database file and allocates an empty one, so a
// crash mid-restore reads to the user as a wiped database.
func TestUserDBOpenRecoversInterruptedRestore(t *testing.T) {
	userDB, cleanup := setupTempUserDB(t)
	defer cleanup()

	require.NoError(t, userDB.AddMapping(&database.Mapping{
		Label:    "Survivor",
		Enabled:  true,
		Type:     MappingTypeID,
		Match:    MatchTypeExact,
		Pattern:  "survivor-token",
		Override: "**launch.system:n64",
	}))

	dbPath := userDB.GetDBPath()
	require.NoError(t, userDB.closeAndDrain())
	// The state a crash between preserving the original and installing the
	// replacement leaves behind.
	require.NoError(t, os.Rename(dbPath, dbPath+restoreRollbackSuffix))
	require.NoFileExists(t, dbPath)

	require.NoError(t, userDB.Open())

	mappings, err := userDB.GetAllMappings()
	require.NoError(t, err)
	require.Len(t, mappings, 1)
	assert.Equal(t, "Survivor", mappings[0].Label)
	assert.NoFileExists(t, dbPath+restoreRollbackSuffix)
}

// TestUserDBOpenRefusesWhenRecoveryFails closes the loop on the worst outcome.
// Allocating a fresh schema when recovery fails does not just lose this open:
// the empty database sitting beside the rollback makes the next open read the
// state as a finished restore, and the rollback — still the only copy of the
// data — gets deleted as leftovers. Failing the open keeps it recoverable.
func TestUserDBOpenRefusesWhenRecoveryFails(t *testing.T) {
	userDB, cleanup := setupTempUserDB(t)
	defer cleanup()

	require.NoError(t, userDB.AddMapping(&database.Mapping{
		Label:    "Survivor",
		Enabled:  true,
		Type:     MappingTypeID,
		Match:    MatchTypeExact,
		Pattern:  "survivor-token",
		Override: "**launch.system:n64",
	}))

	dbPath := userDB.GetDBPath()
	require.NoError(t, userDB.closeAndDrain())
	require.NoError(t, os.Rename(dbPath, dbPath+restoreRollbackSuffix))
	userDB.fs = failStagedInstallFs{Fs: afero.NewOsFs(), dbPath: dbPath, failRollback: true}

	err := userDB.Open()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to recover user database from interrupted restore")
	assert.NoFileExists(t, dbPath, "a fresh database would strand the rollback")
	assert.FileExists(t, dbPath+restoreRollbackSuffix)

	// With the fault removed the data comes back, which is what refusing bought.
	userDB.fs = nil
	require.NoError(t, userDB.Open())
	mappings, err := userDB.GetAllMappings()
	require.NoError(t, err)
	require.Len(t, mappings, 1)
	assert.Equal(t, "Survivor", mappings[0].Label)
}

func TestBackupName_KindDecidesRetention(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 18, 4, 30, 0, 1, time.UTC)

	autoName := backupName(backupKindAuto, now)
	manualName := backupName(backupKindManual, now)
	updateName := backupName(backupKindUpdate, now)

	assert.True(t, isBackupName(autoName))
	assert.True(t, isBackupName(manualName))
	assert.True(t, isBackupName(updateName), "an update snapshot is still restorable by name")

	assert.True(t, isAutoBackupName(autoName))
	assert.False(t, isAutoBackupName(manualName))
	assert.False(t, isAutoBackupName(updateName), "retention must not reclaim update snapshots")

	assert.False(t, IsUpdateSnapshotName(autoName))
	assert.False(t, IsUpdateSnapshotName(manualName))
	assert.True(t, IsUpdateSnapshotName(updateName))
}

// An update snapshot is not a manual backup, so it does not claim the exemption
// the app shows for one, and it is not an auto backup, so retention leaves it
// alone. The updater is what deletes it, once the update has been resolved.
func TestUserDBBackupForUpdate_SurvivesPruningAndIsNotManual(t *testing.T) {
	userDB, cleanup := setupTempUserDB(t)
	defer cleanup()

	snapshot, resume, err := userDB.BackupForUpdate("2.2.0")
	require.NoError(t, err)
	require.NoError(t, resume())
	assert.False(t, snapshot.Manual)
	assert.True(t, snapshot.Valid)
	assert.True(t, IsUpdateSnapshotName(snapshot.Name))

	for range autoBackupKeep + 2 {
		_, backupErr := userDB.Backup("scheduled", false)
		require.NoError(t, backupErr)
	}

	backups, err := userDB.ListBackups()
	require.NoError(t, err)

	autoCount := 0
	snapshotPresent := false
	for _, b := range backups {
		if isAutoBackupName(b.Name) {
			autoCount++
		}
		if b.Name == snapshot.Name {
			snapshotPresent = true
		}
	}
	assert.Equal(t, autoBackupKeep, autoCount, "auto backups pruned to retention limit")
	assert.True(t, snapshotPresent, "update snapshot must survive pruning")
	assert.FileExists(t, snapshot.Path)
}

func TestUserDBBackupForUpdate_QuiescesWritersUntilResume(t *testing.T) {
	userDB, cleanup := setupTempUserDB(t)
	defer cleanup()

	_, resume, err := userDB.BackupForUpdate("2.2.0")
	require.NoError(t, err)

	err = userDB.AddMapping(&database.Mapping{
		Label:    "too-late",
		Type:     MappingTypeID,
		Match:    MatchTypeExact,
		Pattern:  "after-update-snapshot",
		Override: "**launch.system:snes",
		Enabled:  true,
	})
	require.Error(t, err, "writes after the update snapshot must remain quiesced")

	require.NoError(t, resume())
	require.NoError(t, userDB.AddMapping(&database.Mapping{
		Label:    "after-resume",
		Type:     MappingTypeID,
		Match:    MatchTypeExact,
		Pattern:  "after-aborted-update",
		Override: "**launch.system:snes",
		Enabled:  true,
	}))
}

func TestRestoreFileTo_ReplacesTheLiveDatabase(t *testing.T) {
	userDB, cleanup := setupTempUserDB(t)
	defer cleanup()

	require.NoError(t, userDB.AddMapping(&database.Mapping{
		Label:    "restore-file-to",
		Type:     MappingTypeID,
		Match:    MatchTypeExact,
		Pattern:  "restore-file-to-token",
		Override: "**launch.system:snes",
		Enabled:  true,
	}))

	snapshot, _, err := userDB.BackupForUpdate("2.2.0")
	require.NoError(t, err)

	dbPath := userDB.GetDBPath()
	require.NoError(t, userDB.Close())

	require.NoError(t, RestoreFileTo(context.Background(), afero.NewOsFs(), snapshot.Path, dbPath))

	restored, err := sql.Open("sqlite3", dbPath+"?mode=ro&_query_only=ON")
	require.NoError(t, err)
	defer func() { _ = restored.Close() }()

	var count int
	require.NoError(t, restored.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM Mappings WHERE pattern = ?`, "restore-file-to-token",
	).Scan(&count))
	assert.Equal(t, 1, count)
}

// A backup that fails quick_check is not installed over a working database:
// replacing a good file with a broken one is worse than refusing the restore.
func TestRestoreFileTo_RejectsACorruptBackup(t *testing.T) {
	userDB, cleanup := setupTempUserDB(t)
	defer cleanup()

	snapshot, _, err := userDB.BackupForUpdate("2.2.0")
	require.NoError(t, err)

	dbPath := userDB.GetDBPath()
	require.NoError(t, userDB.Close())

	live, err := os.ReadFile(dbPath) // #nosec G304 -- test-owned path.
	require.NoError(t, err)

	corrupt, err := os.ReadFile(snapshot.Path) // #nosec G304 -- test-owned path.
	require.NoError(t, err)
	// Leave the header intact so it is opened as a database and then found bad,
	// rather than rejected as some other kind of file.
	for i := 100; i < len(corrupt) && i < 4096; i++ {
		corrupt[i] = 0xFF
	}
	require.NoError(t, os.WriteFile(snapshot.Path, corrupt, 0o600)) // #nosec G703 -- test-owned path.

	err = RestoreFileTo(context.Background(), afero.NewOsFs(), snapshot.Path, dbPath)
	require.ErrorIs(t, err, ErrInvalidBackup)
	assert.Contains(t, err.Error(), "quick_check", "the backup must be rejected by the integrity check")

	after, err := os.ReadFile(dbPath) // #nosec G304 -- test-owned path.
	require.NoError(t, err)
	assert.Equal(t, live, after, "the live database must be untouched")
}

func TestBackupsDir(t *testing.T) {
	t.Parallel()

	assert.Equal(t, filepath.Join("data", backupDirName), BackupsDir("data"))
}
