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

package updater

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/userdb"
	_ "github.com/mattn/go-sqlite3"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testPrevVersion   = "2.1.0"
	testTargetVersion = "2.2.0"
)

func TestDecideWatchdogAction(t *testing.T) {
	t.Parallel()

	tests := []struct {
		marker  *pendingMarker
		name    string
		running string
		want    watchdogAction
	}{
		{
			name:    "no marker",
			marker:  nil,
			running: testTargetVersion,
			want:    actionNone,
		},
		{
			name: "interrupted install still running previous version",
			marker: &pendingMarker{
				State: markerInstalling, PreviousVersion: testPrevVersion, TargetVersion: testTargetVersion,
			},
			running: testPrevVersion,
			want:    actionAbortInstall,
		},
		{
			name: "interrupted install running target version",
			marker: &pendingMarker{
				State: markerInstalling, PreviousVersion: testPrevVersion, TargetVersion: testTargetVersion,
			},
			running: testTargetVersion,
			want:    actionRollBack,
		},
		{
			name:    "installed and running the target",
			marker:  &pendingMarker{State: markerInstalled, TargetVersion: testTargetVersion},
			running: testTargetVersion,
			want:    actionConfirm,
		},
		{
			name: "installed but still running previous version",
			marker: &pendingMarker{
				State: markerInstalled, PreviousVersion: testPrevVersion, TargetVersion: testTargetVersion,
			},
			running: testPrevVersion,
			want:    actionAbortInstall,
		},
		{
			name:    "installed but running something else",
			marker:  &pendingMarker{State: markerInstalled, TargetVersion: testTargetVersion},
			running: testPrevVersion,
			want:    actionRollBack,
		},
		{
			name:    "confirming the running version means the last boot never finished",
			marker:  &pendingMarker{State: markerConfirming, TargetVersion: testTargetVersion},
			running: testTargetVersion,
			want:    actionRollBack,
		},
		{
			name:    "confirming a version that is not running is stale",
			marker:  &pendingMarker{State: markerConfirming, TargetVersion: testTargetVersion},
			running: "2.3.0",
			want:    actionClear,
		},
		{
			name:    "an interrupted rollback resumes",
			marker:  &pendingMarker{State: markerRollingBack, TargetVersion: testTargetVersion},
			running: testTargetVersion,
			want:    actionRollBack,
		},
		{
			name: "a blocked outcome retains recovery files without retrying",
			marker: &pendingMarker{
				State: markerRollingBack, TargetVersion: testTargetVersion,
				Outcome: outcomeRollbackBlocked,
			},
			running: testTargetVersion,
			want:    actionNone,
		},
		{
			name: "a succeeded outcome resumes terminal cleanup",
			marker: &pendingMarker{
				State: markerConfirming, TargetVersion: testTargetVersion,
				Outcome: outcomeSucceeded,
			},
			running: testTargetVersion,
			want:    actionFinalize,
		},
		{
			name: "a rolled back outcome resumes terminal cleanup",
			marker: &pendingMarker{
				State: markerRollingBack, TargetVersion: testTargetVersion,
				Outcome: outcomeRolledBack,
			},
			running: testPrevVersion,
			want:    actionFinalize,
		},
		{
			name:    "an unrecognised state cannot direct a rollback",
			marker:  &pendingMarker{State: "sideways", TargetVersion: testTargetVersion},
			running: testTargetVersion,
			want:    actionClear,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, decideWatchdogAction(tt.marker, tt.running))
		})
	}
}

// installFixture is a device mid-update: the new binary installed over the old
// one, the old one kept beside it, and a snapshot of the database as it was
// before any of it happened.
type installFixture struct {
	dataDir      string
	dbPath       string
	targetPath   string
	backupPath   string
	snapshotPath string
	stagingDir   string
}

func newInstallFixture(t *testing.T) *installFixture {
	t.Helper()

	dataDir := t.TempDir()
	binDir := t.TempDir()

	f := &installFixture{
		dataDir:    dataDir,
		dbPath:     filepath.Join(dataDir, config.UserDbFile),
		targetPath: filepath.Join(binDir, "zaparoo"),
		backupPath: filepath.Join(binDir, ".zaparoo.zap-old-"+testPrevVersion),
		snapshotPath: filepath.Join(dataDir, "backups",
			"backup-20260818-043000-000000001-update.db"),
		stagingDir: filepath.Join(stagingRootFor(dataDir), testTargetVersion),
	}

	//nolint:gosec // stand-ins for an executable, which is what the mode is for
	require.NoError(t, os.WriteFile(f.targetPath, []byte("new binary"), 0o755))
	//nolint:gosec // stand-ins for an executable, which is what the mode is for
	require.NoError(t, os.WriteFile(f.backupPath, []byte("old binary"), 0o755))
	require.NoError(t, os.MkdirAll(f.stagingDir, stateDirPerm))

	writeTestDB(t, f.dbPath, "after the update")
	writeTestDB(t, f.snapshotPath, "before the update")
	return f
}

// marker returns the record the install would have left, in the given state.
func (f *installFixture) marker(state markerState) *pendingMarker {
	return &pendingMarker{
		State:              state,
		Trigger:            triggerManual,
		TargetPath:         f.targetPath,
		BackupPath:         f.backupPath,
		StagingDir:         f.stagingDir,
		PreviousVersion:    testPrevVersion,
		TargetVersion:      testTargetVersion,
		PlatformID:         "mister",
		UserDBSnapshotPath: f.snapshotPath,
		// The fixture puts the new binary at the target, which is only true
		// once the swap has taken the name.
		BinaryReplaced: true,
	}
}

func writeTestDB(t *testing.T, path, note string) {
	t.Helper()

	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	db, err := sql.Open("sqlite3", path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	ctx := t.Context()
	_, err = db.ExecContext(ctx, `CREATE TABLE note (text TEXT)`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO note (text) VALUES (?)`, note)
	require.NoError(t, err)
	require.NoError(t, db.Close())
}

func readTestDBNote(t *testing.T, path string) string {
	t.Helper()

	db, err := sql.Open("sqlite3", path+"?mode=ro")
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	var note string
	require.NoError(t, db.QueryRowContext(t.Context(), `SELECT text FROM note`).Scan(&note))
	return note
}

func readFileString(t *testing.T, path string) string {
	t.Helper()

	data, err := os.ReadFile(path) //nolint:gosec // test-owned path
	require.NoError(t, err)
	return string(data)
}

func TestRunStartupWatchdog_NoMarker(t *testing.T) {
	t.Parallel()

	require.NoError(t, RunStartupWatchdog(context.Background(), t.TempDir(), testTargetVersion))
}

func TestRunStartupWatchdog_ConfirmsFirstBoot(t *testing.T) {
	t.Parallel()

	f := newInstallFixture(t)
	dir := stateDirFor(f.dataDir)
	require.NoError(t, saveMarker(dir, f.marker(markerInstalled)))

	require.NoError(t, RunStartupWatchdog(context.Background(), f.dataDir, testTargetVersion))

	m, err := loadMarker(dir)
	require.NoError(t, err)
	require.NotNil(t, m)
	assert.Equal(t, markerConfirming, m.State)
	assert.Equal(t, 1, m.Attempts)

	// Nothing is reclaimed while the update is still being judged.
	assert.FileExists(t, f.snapshotPath)
	assert.FileExists(t, f.backupPath)
	assert.Equal(t, "new binary", readFileString(t, f.targetPath))
}

func TestRunStartupWatchdog_RollsBackWhenConfirmationNeverHappened(t *testing.T) {
	t.Parallel()

	f := newInstallFixture(t)
	dir := stateDirFor(f.dataDir)
	require.NoError(t, saveMarker(dir, f.marker(markerConfirming)))

	err := RunStartupWatchdog(context.Background(), f.dataDir, testTargetVersion)
	require.ErrorIs(t, err, ErrRolledBack)
	rollbackTarget, ok := RollbackTargetPath(err)
	require.True(t, ok)
	assert.Equal(t, f.targetPath, rollbackTarget)

	assert.Equal(t, "old binary", readFileString(t, f.targetPath))
	assert.NoFileExists(t, f.backupPath)
	assert.Equal(t, "before the update", readTestDBNote(t, f.dbPath))
	assert.NoFileExists(t, markerPath(dir))
	assert.NoDirExists(t, f.stagingDir)
	assert.NoFileExists(t, f.snapshotPath)

	res := peekUpdateResult(dir)
	require.NotNil(t, res)
	assert.Equal(t, outcomeRolledBack, res.Outcome)
	assert.Equal(t, testPrevVersion, res.FromVersion)
	assert.Equal(t, testTargetVersion, res.ToVersion)
}

// A rollback interrupted after the binary was swapped but before the marker was
// cleared has to finish, not start again from a backup that is no longer there.
func TestRunStartupWatchdog_ResumesInterruptedRollback(t *testing.T) {
	t.Parallel()

	f := newInstallFixture(t)
	dir := stateDirFor(f.dataDir)
	m := f.marker(markerRollingBack)
	m.RestoringPath = f.targetPath
	require.NoError(t, saveMarker(dir, m))
	require.NoError(t, replaceFile(f.backupPath, f.targetPath))

	err := RunStartupWatchdog(context.Background(), f.dataDir, testTargetVersion)
	require.ErrorIs(t, err, ErrRolledBack)

	assert.Equal(t, "old binary", readFileString(t, f.targetPath))
	assert.Equal(t, "before the update", readTestDBNote(t, f.dbPath))
	assert.NoFileExists(t, markerPath(dir))
}

// The same missing backup on a rollback that has not started yet is a real
// failure: reporting success would strand the device on the broken version.
func TestRunStartupWatchdog_MissingBinaryBackupBlocksRollback(t *testing.T) {
	t.Parallel()

	f := newInstallFixture(t)
	dir := stateDirFor(f.dataDir)
	require.NoError(t, saveMarker(dir, f.marker(markerConfirming)))
	require.NoError(t, os.Remove(f.backupPath))

	require.NoError(t, RunStartupWatchdog(context.Background(), f.dataDir, testTargetVersion))

	assert.Equal(t, "new binary", readFileString(t, f.targetPath))
	m, err := loadMarker(dir)
	require.NoError(t, err)
	require.NotNil(t, m)
	assert.Equal(t, outcomeRollbackBlocked, m.Outcome)

	res := peekUpdateResult(dir)
	require.NotNil(t, res)
	assert.Equal(t, outcomeRollbackBlocked, res.Outcome)
	assert.NotEmpty(t, res.Detail)
}

func TestRunStartupWatchdog_KeepsTheSnapshotOfAnAbandonedRollback(t *testing.T) {
	t.Parallel()

	f := newInstallFixture(t)
	dir := stateDirFor(f.dataDir)
	m := f.marker(markerRollingBack)
	m.Outcome = outcomeRollbackBlocked
	require.NoError(t, saveMarker(dir, m))

	stale := filepath.Join(filepath.Dir(f.snapshotPath),
		"backup-20260817-043000-000000009-update.db")
	writeTestDB(t, stale, "some older update")

	require.NoError(t, RunStartupWatchdog(context.Background(), f.dataDir, testTargetVersion))

	// The rollback was already given up on, so nothing may be retried: the
	// marker, the new binary and the database all stay exactly as they were.
	assert.Equal(t, "new binary", readFileString(t, f.targetPath))
	assert.Equal(t, "after the update", readTestDBNote(t, f.dbPath))
	got, err := loadMarker(dir)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, outcomeRollbackBlocked, got.Outcome)

	// The snapshot is the only way back the user has left, and the inbox message
	// sends them to it, so the sweep must not take it. Snapshots nothing points
	// at still go.
	assert.FileExists(t, f.snapshotPath)
	assert.NoFileExists(t, stale)
}

func TestRunStartupWatchdog_MissingSnapshotBlocksRollback(t *testing.T) {
	t.Parallel()

	f := newInstallFixture(t)
	dir := stateDirFor(f.dataDir)
	require.NoError(t, saveMarker(dir, f.marker(markerConfirming)))
	require.NoError(t, os.Remove(f.snapshotPath))

	require.NoError(t, RunStartupWatchdog(context.Background(), f.dataDir, testTargetVersion))

	// Nothing moved: the database restore is the first step for exactly this
	// reason, so an old binary is never left in front of a migrated database.
	assert.Equal(t, "new binary", readFileString(t, f.targetPath))
	assert.FileExists(t, f.backupPath)
	assert.Equal(t, "after the update", readTestDBNote(t, f.dbPath))

	m, err := loadMarker(dir)
	require.NoError(t, err)
	require.NotNil(t, m)
	assert.Equal(t, outcomeRollbackBlocked, m.Outcome)

	// A blocked rollback is terminal, so the next boot leaves it alone.
	assert.Equal(t, actionNone, decideWatchdogAction(m, testTargetVersion))
	require.NoError(t, RunStartupWatchdog(context.Background(), f.dataDir, testTargetVersion))
	assert.Equal(t, "new binary", readFileString(t, f.targetPath))
}

func TestRunStartupWatchdog_ClearsMarkerForAnotherVersion(t *testing.T) {
	t.Parallel()

	f := newInstallFixture(t)
	dir := stateDirFor(f.dataDir)
	require.NoError(t, saveMarker(dir, f.marker(markerConfirming)))

	require.NoError(t, RunStartupWatchdog(context.Background(), f.dataDir, "2.4.0"))

	assert.NoFileExists(t, markerPath(dir))
	assert.Equal(t, "new binary", readFileString(t, f.targetPath))
	assert.NoFileExists(t, f.snapshotPath)
}

func TestConfirm(t *testing.T) {
	t.Parallel()

	f := newInstallFixture(t)
	dir := stateDirFor(f.dataDir)
	require.NoError(t, saveMarker(dir, f.marker(markerConfirming)))

	version, err := Confirm(context.Background(), f.dataDir, testTargetVersion)
	require.NoError(t, err)
	assert.Equal(t, testTargetVersion, version)

	assert.NoFileExists(t, markerPath(dir))
	assert.NoFileExists(t, f.backupPath)
	assert.NoFileExists(t, f.snapshotPath)
	assert.NoDirExists(t, f.stagingDir)
	assert.Equal(t, "new binary", readFileString(t, f.targetPath))
	assert.Equal(t, "after the update", readTestDBNote(t, f.dbPath))

	res := peekUpdateResult(dir)
	require.NotNil(t, res)
	assert.Equal(t, outcomeSucceeded, res.Outcome)
	assert.Equal(t, testPrevVersion, res.FromVersion)
	assert.Equal(t, testTargetVersion, res.ToVersion)
}

func TestConfirm_RetriesFailedBinaryBackupCleanup(t *testing.T) {
	t.Parallel()

	f := newInstallFixture(t)
	dir := stateDirFor(f.dataDir)
	require.NoError(t, saveMarker(dir, f.marker(markerConfirming)))
	require.NoError(t, os.Remove(f.backupPath))
	require.NoError(t, os.Mkdir(f.backupPath, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(f.backupPath, "locked"), []byte("x"), 0o600))

	version, err := Confirm(t.Context(), f.dataDir, testTargetVersion)
	require.Error(t, err)
	assert.Equal(t, testTargetVersion, version)

	m, loadErr := loadMarker(dir)
	require.NoError(t, loadErr)
	require.NotNil(t, m)
	assert.Equal(t, outcomeSucceeded, m.Outcome)
	assert.FileExists(t, f.snapshotPath)

	require.NoError(t, os.RemoveAll(f.backupPath))
	require.NoError(t, RunStartupWatchdog(t.Context(), f.dataDir, testTargetVersion))
	assert.NoFileExists(t, markerPath(dir))
	assert.NoFileExists(t, f.snapshotPath)
}

func TestConfirm_IgnoresAMarkerItDoesNotOwn(t *testing.T) {
	t.Parallel()

	f := newInstallFixture(t)
	dir := stateDirFor(f.dataDir)
	require.NoError(t, saveMarker(dir, f.marker(markerInstalled)))

	// Still installed rather than confirming: the watchdog has not run.
	version, err := Confirm(context.Background(), f.dataDir, testTargetVersion)
	require.NoError(t, err)
	assert.Empty(t, version)
	assert.FileExists(t, markerPath(dir))

	require.NoError(t, saveMarker(dir, f.marker(markerConfirming)))
	version, err = Confirm(context.Background(), f.dataDir, "9.9.9")
	require.NoError(t, err)
	assert.Empty(t, version)
	assert.FileExists(t, markerPath(dir))
}

func TestRollBackFailedStart(t *testing.T) {
	t.Parallel()

	f := newInstallFixture(t)
	dir := stateDirFor(f.dataDir)
	require.NoError(t, saveMarker(dir, f.marker(markerConfirming)))

	err := RollBackFailedStart(context.Background(), f.dataDir, testTargetVersion)
	require.ErrorIs(t, err, ErrRolledBack)
	assert.Equal(t, "old binary", readFileString(t, f.targetPath))
	assert.Equal(t, "before the update", readTestDBNote(t, f.dbPath))
}

func TestRollBackFailedStart_NothingPending(t *testing.T) {
	t.Parallel()

	f := newInstallFixture(t)
	require.NoError(t, RollBackFailedStart(context.Background(), f.dataDir, testTargetVersion))
	assert.Equal(t, "new binary", readFileString(t, f.targetPath))

	// A marker for a version this process is not running belongs to someone else.
	dir := stateDirFor(f.dataDir)
	require.NoError(t, saveMarker(dir, f.marker(markerConfirming)))
	require.NoError(t, RollBackFailedStart(context.Background(), f.dataDir, "2.4.0"))
	assert.Equal(t, "new binary", readFileString(t, f.targetPath))
}

func TestRecordCleanShutdown_RestartsConfirmationWithoutRollback(t *testing.T) {
	t.Parallel()

	f := newInstallFixture(t)
	dir := stateDirFor(f.dataDir)
	require.NoError(t, saveMarker(dir, f.marker(markerConfirming)))

	require.NoError(t, RecordCleanShutdown(f.dataDir, testTargetVersion))
	m, err := loadMarker(dir)
	require.NoError(t, err)
	require.NotNil(t, m)
	assert.Equal(t, markerInstalled, m.State)

	require.NoError(t, RunStartupWatchdog(t.Context(), f.dataDir, testTargetVersion))
	m, err = loadMarker(dir)
	require.NoError(t, err)
	require.NotNil(t, m)
	assert.Equal(t, markerConfirming, m.State)
	assert.Equal(t, "new binary", readFileString(t, f.targetPath))
	assert.Equal(t, "after the update", readTestDBNote(t, f.dbPath))
}

func TestRunStartupWatchdog_RetriesTransientRestoreFailure(t *testing.T) {
	f := newInstallFixture(t)
	dir := stateDirFor(f.dataDir)
	require.NoError(t, saveMarker(dir, f.marker(markerConfirming)))

	require.NoError(t, os.Remove(f.snapshotPath))
	require.NoError(t, os.Mkdir(f.snapshotPath, 0o750))

	err := RunStartupWatchdog(t.Context(), f.dataDir, testTargetVersion)
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrRolledBack)

	m, loadErr := loadMarker(dir)
	require.NoError(t, loadErr)
	require.NotNil(t, m)
	assert.Equal(t, markerRollingBack, m.State)
	assert.Empty(t, m.Outcome)
	assert.FileExists(t, f.backupPath)

	require.NoError(t, os.Remove(f.snapshotPath))
	writeTestDB(t, f.snapshotPath, "before the update")

	err = RunStartupWatchdog(t.Context(), f.dataDir, testTargetVersion)
	require.ErrorIs(t, err, ErrRolledBack)
	assert.Equal(t, "old binary", readFileString(t, f.targetPath))
	assert.Equal(t, "before the update", readTestDBNote(t, f.dbPath))
}

func TestRunStartupWatchdog_UnusableMarkerRetainsSnapshots(t *testing.T) {
	t.Parallel()

	f := newInstallFixture(t)
	dir := stateDirFor(f.dataDir)
	require.NoError(t, os.MkdirAll(dir, stateDirPerm))
	require.NoError(t, os.WriteFile(markerPath(dir), []byte("{not json"), stateFilePerm))

	require.NoError(t, RunStartupWatchdog(t.Context(), f.dataDir, testTargetVersion))
	assert.FileExists(t, f.snapshotPath)
	assert.FileExists(t, markerPath(dir)+markerBadSuffix)

	// A later boot still sees the quarantined evidence and must not treat the
	// marker as confirmed absent.
	require.NoError(t, RunStartupWatchdog(t.Context(), f.dataDir, testTargetVersion))
	assert.FileExists(t, f.snapshotPath)
}

func TestRunStartupWatchdog_FinalizesDurableSuccess(t *testing.T) {
	t.Parallel()

	f := newInstallFixture(t)
	dir := stateDirFor(f.dataDir)
	m := f.marker(markerConfirming)
	m.Outcome = outcomeSucceeded
	m.OutcomeAt = time.Date(2026, 8, 18, 5, 0, 0, 0, time.UTC)
	require.NoError(t, saveMarker(dir, m))
	// What a swap that had to vacate the target's name left behind, in the slot
	// an earlier unwind pushed it into rather than the first one.
	superseded := supersededPathFor(f.targetPath, 2)
	//nolint:gosec // executable stand-in owned by this test
	require.NoError(t, os.WriteFile(superseded, []byte("old binary"), 0o755))

	require.NoError(t, RunStartupWatchdog(t.Context(), f.dataDir, testTargetVersion))

	assert.NoFileExists(t, markerPath(dir))
	assert.NoFileExists(t, f.backupPath)
	assert.NoFileExists(t, f.snapshotPath)
	assert.NoDirExists(t, f.stagingDir)
	assert.NoFileExists(t, superseded,
		"the process running from the superseded binary has exited, so finalizing clears it")
	assert.Equal(t, "new binary", readFileString(t, f.targetPath))
	assert.Equal(t, "after the update", readTestDBNote(t, f.dbPath))

	res := peekUpdateResult(dir)
	require.NotNil(t, res)
	assert.Equal(t, outcomeSucceeded, res.Outcome)
	assert.Equal(t, testPrevVersion, res.FromVersion)
	assert.Equal(t, testTargetVersion, res.ToVersion)
}

func TestRunStartupWatchdog_AbortsInterruptedInstallWithoutRestoringDB(t *testing.T) {
	t.Parallel()

	f := newInstallFixture(t)
	dir := stateDirFor(f.dataDir)
	require.NoError(t, saveMarker(dir, f.marker(markerInstalling)))

	require.NoError(t, RunStartupWatchdog(t.Context(), f.dataDir, testPrevVersion))

	assert.Equal(t, "old binary", readFileString(t, f.targetPath))
	assert.Equal(t, "after the update", readTestDBNote(t, f.dbPath))
	assert.NoFileExists(t, markerPath(dir))
	assert.NoFileExists(t, f.snapshotPath)
	assert.NoDirExists(t, f.stagingDir)
}

func TestRestoreUserDB_UsesSuppliedFilesystem(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	dataDir := filepath.Join("data", "zaparoo")
	snapshotPath := filepath.Join(dataDir, "backups", "snapshot-update.db")
	require.NoError(t, fs.MkdirAll(filepath.Dir(snapshotPath), 0o750))
	require.NoError(t, afero.WriteFile(fs, snapshotPath, []byte("snapshot"), 0o600))

	restoreCalled := false
	fileOps := watchdogFileOps{
		fs: fs,
		restoreDatabase: func(_ context.Context, gotFS afero.Fs, backupPath, dbPath string) error {
			restoreCalled = true
			assert.Same(t, fs, gotFS)
			assert.Equal(t, snapshotPath, backupPath)
			assert.Equal(t, filepath.Join(dataDir, config.UserDbFile), dbPath)
			return nil
		},
	}
	err := restoreUserDB(t.Context(), dataDir, &pendingMarker{UserDBSnapshotPath: snapshotPath}, fileOps)
	require.NoError(t, err)
	assert.True(t, restoreCalled)
}

func TestSweepUpdateSnapshots_UsesSuppliedFilesystem(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	dataDir := filepath.Join("data", "zaparoo")
	backups := userdb.BackupsDir(dataDir)
	require.NoError(t, fs.MkdirAll(backups, 0o750))
	keep := filepath.Join(backups, "backup-20260818-043000-000000001-update.db")
	stale := filepath.Join(backups, "backup-20260817-043000-000000001-update.db")
	for _, path := range []string{keep, stale} {
		require.NoError(t, afero.WriteFile(fs, path, []byte("db"), 0o600))
	}

	sweepUpdateSnapshotsFS(fs, dataDir, keep)
	keepExists, err := afero.Exists(fs, keep)
	require.NoError(t, err)
	assert.True(t, keepExists)
	staleExists, err := afero.Exists(fs, stale)
	require.NoError(t, err)
	assert.False(t, staleExists)
}

func TestSweepUpdateSnapshots(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	backups := filepath.Join(dataDir, "backups")
	require.NoError(t, os.MkdirAll(backups, 0o750))

	keep := filepath.Join(backups, "backup-20260818-043000-000000001-update.db")
	stale := filepath.Join(backups, "backup-20260817-043000-000000001-update.db")
	auto := filepath.Join(backups, "backup-20260817-043000-000000002-auto.db")
	manual := filepath.Join(backups, "backup-20260817-043000-000000003-manual.db")
	for _, path := range []string{keep, stale, auto, manual} {
		require.NoError(t, os.WriteFile(path, []byte("db"), 0o600))
	}

	sweepUpdateSnapshots(dataDir, keep)

	assert.FileExists(t, keep)
	assert.NoFileExists(t, stale)
	assert.FileExists(t, auto)
	assert.FileExists(t, manual)

	sweepUpdateSnapshots(dataDir, "")
	assert.NoFileExists(t, keep)
	assert.FileExists(t, auto)
	assert.FileExists(t, manual)
}

func TestRolledBackError(t *testing.T) {
	t.Parallel()

	cause := errors.New("re-exec is unavailable")
	withCause := newRolledBackError("/usr/bin/zaparoo", cause)
	require.ErrorIs(t, withCause, ErrRolledBack)
	require.ErrorIs(t, withCause, cause)
	assert.Equal(t, ErrRolledBack.Error()+": "+cause.Error(), withCause.Error())

	// A rollback with no underlying cause still has to read as ErrRolledBack so
	// callers re-exec instead of exiting into the version that failed.
	bare := newRolledBackError("/usr/bin/zaparoo", nil)
	require.ErrorIs(t, bare, ErrRolledBack)
	assert.Equal(t, ErrRolledBack.Error(), bare.Error())
}

func TestRollbackTargetPath(t *testing.T) {
	t.Parallel()

	target, ok := RollbackTargetPath(newRolledBackError("/usr/bin/zaparoo", errors.New("boom")))
	assert.True(t, ok)
	assert.Equal(t, "/usr/bin/zaparoo", target)

	// Without a target there is nothing to re-exec, so callers must not be told
	// to try. Neither must an unrelated failure be mistaken for a rollback.
	for name, err := range map[string]error{
		"rollback without a target": newRolledBackError("", errors.New("boom")),
		"unrelated failure":         errors.New("boom"),
		"sentinel on its own":       ErrRolledBack,
		"no error":                  nil,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			target, ok := RollbackTargetPath(err)
			assert.False(t, ok)
			assert.Empty(t, target)
		})
	}
}

// A marker written by a newer build must survive this one untouched: only the
// build that understands it can decide what its update needs.
func TestWatchdog_LeavesANewerSchemaMarkerAlone(t *testing.T) {
	f := newInstallFixture(t)
	dir := stateDirFor(f.dataDir)
	m := f.marker(markerConfirming)
	m.MarkerVersion = currentMarkerVersion + 1
	raw, err := json.MarshalIndent(m, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(dir, stateDirPerm))
	require.NoError(t, os.WriteFile(markerPath(dir), raw, 0o600))
	before := readFileString(t, markerPath(dir))

	confirmed, err := Confirm(t.Context(), f.dataDir, testTargetVersion)
	require.NoError(t, err)
	assert.Empty(t, confirmed)
	assert.Equal(t, before, readFileString(t, markerPath(dir)))

	require.NoError(t, RecordCleanShutdown(f.dataDir, testTargetVersion))
	assert.Equal(t, before, readFileString(t, markerPath(dir)),
		"a marker this build cannot interpret must not be rewritten")
	assert.NoFileExists(t, markerPath(dir)+markerBadSuffix,
		"a newer schema is not corruption and must not be quarantined")
}

// RecordCleanShutdown runs on the way out of a boot that may have nothing to do
// with the pending update, so it must only touch a marker awaiting this version.
func TestRecordCleanShutdown_IgnoresAMarkerItDoesNotOwn(t *testing.T) {
	f := newInstallFixture(t)
	dir := stateDirFor(f.dataDir)

	for name, m := range map[string]*pendingMarker{
		"another version is confirming": func() *pendingMarker {
			m := f.marker(markerConfirming)
			m.TargetVersion = "9.9.9"
			return m
		}(),
		"already resolved": func() *pendingMarker {
			m := f.marker(markerConfirming)
			m.Outcome = outcomeSucceeded
			return m
		}(),
		"not confirming yet": f.marker(markerInstalled),
	} {
		t.Run(name, func(t *testing.T) {
			require.NoError(t, saveMarker(dir, m))
			before := readFileString(t, markerPath(dir))

			require.NoError(t, RecordCleanShutdown(f.dataDir, testTargetVersion))
			assert.Equal(t, before, readFileString(t, markerPath(dir)))
		})
	}
}

func TestRecordCleanShutdown_WithoutAMarkerDoesNothing(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	require.NoError(t, RecordCleanShutdown(dataDir, testTargetVersion))
	assert.NoFileExists(t, markerPath(stateDirFor(dataDir)))
}

// finalizeTerminalUpdate deletes the binary backup and snapshot, so it must
// refuse anything that has not actually reached an outcome.
func TestFinalizeTerminalUpdate_RequiresAnOutcome(t *testing.T) {
	t.Parallel()

	f := newInstallFixture(t)
	ops := defaultWatchdogFileOps()

	require.Error(t, finalizeTerminalUpdate(t.Context(), f.dataDir, nil, ops))
	require.Error(t, finalizeTerminalUpdate(t.Context(), f.dataDir, f.marker(markerConfirming), ops))

	assert.FileExists(t, f.backupPath)
	assert.FileExists(t, f.snapshotPath)
}

// A snapshot that fails its integrity check cannot be written over a working
// database: that would turn a failed update into lost data. The rollback is
// blocked instead, leaving the new binary and the snapshot for manual recovery.
func TestRunStartupWatchdog_CorruptSnapshotBlocksRollback(t *testing.T) {
	t.Parallel()

	f := newInstallFixture(t)
	dir := stateDirFor(f.dataDir)
	require.NoError(t, saveMarker(dir, f.marker(markerConfirming)))

	ops := defaultWatchdogFileOps()
	restoreCalls := 0
	ops.restoreDatabase = func(context.Context, afero.Fs, string, string) error {
		restoreCalls++
		return fmt.Errorf("%w: quick_check: page 3 is never used", userdb.ErrInvalidBackup)
	}

	require.NoError(t, runStartupWatchdogWithOps(t.Context(), f.dataDir, testTargetVersion, ops))
	assert.Equal(t, 1, restoreCalls)

	assert.Equal(t, "new binary", readFileString(t, f.targetPath))
	assert.FileExists(t, f.backupPath)
	assert.FileExists(t, f.snapshotPath, "the snapshot is the only remaining copy of the old data")
	assert.Equal(t, "after the update", readTestDBNote(t, f.dbPath))

	m, err := loadMarker(dir)
	require.NoError(t, err)
	require.NotNil(t, m)
	assert.Equal(t, outcomeRollbackBlocked, m.Outcome)
}

// An install that never recorded a snapshot leaves nothing to roll the database
// back to, so the binary must not be rolled back either: an old binary in front
// of a migrated database is worse than the failed update.
func TestRunStartupWatchdog_MarkerWithoutASnapshotBlocksRollback(t *testing.T) {
	t.Parallel()

	f := newInstallFixture(t)
	dir := stateDirFor(f.dataDir)
	m := f.marker(markerConfirming)
	m.UserDBSnapshotPath = ""
	require.NoError(t, saveMarker(dir, m))

	ops := defaultWatchdogFileOps()
	ops.restoreDatabase = func(context.Context, afero.Fs, string, string) error {
		t.Fatal("a marker with no snapshot must not reach the database restore")
		return nil
	}

	require.NoError(t, runStartupWatchdogWithOps(t.Context(), f.dataDir, testTargetVersion, ops))

	assert.Equal(t, "new binary", readFileString(t, f.targetPath))
	assert.FileExists(t, f.backupPath)
	assert.Equal(t, "after the update", readTestDBNote(t, f.dbPath))

	blocked, err := loadMarker(dir)
	require.NoError(t, err)
	require.NotNil(t, blocked)
	assert.Equal(t, outcomeRollbackBlocked, blocked.Outcome)
}

// Payload extras have no producer yet, but the rollback already restores them,
// and the binary has to go last: a failure part way through then leaves the new
// binary in place, which is the state a blocked rollback can live with.
func TestRunStartupWatchdog_RestoresPayloadFilesBeforeTheBinary(t *testing.T) {
	t.Parallel()

	f := newInstallFixture(t)
	dir := stateDirFor(f.dataDir)
	assetDir := t.TempDir()
	assetPath := filepath.Join(assetDir, "menu.png")
	assetBackup := filepath.Join(assetDir, ".menu.png.zap-old")
	require.NoError(t, os.WriteFile(assetPath, []byte("new asset"), 0o600))
	require.NoError(t, os.WriteFile(assetBackup, []byte("old asset"), 0o600))

	m := f.marker(markerConfirming)
	m.PayloadBackups = []payloadBackup{{TargetPath: assetPath, BackupPath: assetBackup}}
	require.NoError(t, saveMarker(dir, m))

	ops := defaultWatchdogFileOps()
	var restored []string
	// The binary and the payload extras are put back by different calls, so
	// both are recorded to see the order they actually run in.
	record := func(next func(string, string) error) func(string, string) error {
		return func(src, dst string) error {
			restored = append(restored, dst)
			return next(src, dst)
		}
	}
	ops.replace = record(ops.replace)
	ops.binary.replaceRunning = record(ops.binary.replaceRunning)

	err := runStartupWatchdogWithOps(t.Context(), f.dataDir, testTargetVersion, ops)
	require.ErrorIs(t, err, ErrRolledBack)

	assert.Equal(t, []string{assetPath, f.targetPath}, restored)
	assert.Equal(t, "old asset", readFileString(t, assetPath))
	assert.Equal(t, "old binary", readFileString(t, f.targetPath))
	assert.NoFileExists(t, assetBackup, "a restored backup is not left behind")
}

// A payload entry with no backup recorded cannot be restored, and guessing is
// not an option: the rollback is blocked before anything moves.
func TestRunStartupWatchdog_PayloadWithoutABackupBlocksRollback(t *testing.T) {
	t.Parallel()

	f := newInstallFixture(t)
	dir := stateDirFor(f.dataDir)
	m := f.marker(markerConfirming)
	m.PayloadBackups = []payloadBackup{{TargetPath: filepath.Join(t.TempDir(), "menu.png")}}
	require.NoError(t, saveMarker(dir, m))

	require.NoError(t, runStartupWatchdogWithOps(
		t.Context(), f.dataDir, testTargetVersion, defaultWatchdogFileOps()))

	assert.Equal(t, "new binary", readFileString(t, f.targetPath))
	assert.FileExists(t, f.backupPath)
	// The database goes back first by design, so a blocked rollback leaves the
	// new binary in front of the old data rather than the reverse.
	assert.Equal(t, "before the update", readTestDBNote(t, f.dbPath))

	blocked, err := loadMarker(dir)
	require.NoError(t, err)
	require.NotNil(t, blocked)
	assert.Equal(t, outcomeRollbackBlocked, blocked.Outcome)
}

// A rollback that fails after the snapshot went back resumes on the next boot,
// with the device having run and been written to in between. Writing the
// snapshot again then would throw those writes away, so it happens once per
// update rather than once per attempt.
func TestRunStartupWatchdog_ResumedRollbackKeepsTheRestoredDatabase(t *testing.T) {
	t.Parallel()

	f := newInstallFixture(t)
	dir := stateDirFor(f.dataDir)
	m := f.marker(markerRollingBack)
	m.UserDBRestored = true
	m.RollbackAttempts = 1
	require.NoError(t, saveMarker(dir, m))
	// What the device wrote while it ran between the failed attempt and this
	// boot, standing in for anything a resumed rollback would discard.
	require.NoError(t, os.Remove(f.dbPath))
	writeTestDB(t, f.dbPath, "written after the rollback started")

	ops := defaultWatchdogFileOps()
	ops.restoreDatabase = func(context.Context, afero.Fs, string, string) error {
		t.Error("a resumed rollback must not write the snapshot a second time")
		return nil
	}

	err := runStartupWatchdogWithOps(t.Context(), f.dataDir, testTargetVersion, ops)
	require.ErrorIs(t, err, ErrRolledBack)

	assert.Equal(t, "old binary", readFileString(t, f.targetPath))
	assert.Equal(t, "written after the rollback started", readTestDBNote(t, f.dbPath))
}

// A rollback that fails on something time might fix keeps its marker so the next
// boot resumes it. Without a bound that is a device rebooting into a version
// that already failed, forever, so it gives up and records why.
func TestRunStartupWatchdog_StopsRetryingARollbackThatKeepsFailing(t *testing.T) {
	t.Parallel()

	f := newInstallFixture(t)
	dir := stateDirFor(f.dataDir)
	require.NoError(t, saveMarker(dir, f.marker(markerConfirming)))

	restoreErr := errors.New("the old binary will not go back")
	ops := defaultWatchdogFileOps()
	ops.binary.replaceRunning = func(string, string) error { return restoreErr }

	for attempt := 1; attempt < rollbackAttemptLimit; attempt++ {
		err := runStartupWatchdogWithOps(t.Context(), f.dataDir, testTargetVersion, ops)
		require.ErrorIs(t, err, restoreErr)

		pending, loadErr := loadMarker(dir)
		require.NoError(t, loadErr)
		require.NotNil(t, pending)
		assert.Equal(t, markerRollingBack, pending.State)
		assert.Empty(t, pending.Outcome, "a rollback with attempts left stays pending")
		assert.Equal(t, attempt, pending.RollbackAttempts)
	}

	require.NoError(t, runStartupWatchdogWithOps(t.Context(), f.dataDir, testTargetVersion, ops))

	blocked, err := loadMarker(dir)
	require.NoError(t, err)
	require.NotNil(t, blocked)
	assert.Equal(t, outcomeRollbackBlocked, blocked.Outcome)
	assert.Equal(t, rollbackAttemptLimit, blocked.RollbackAttempts)
	assert.Contains(t, blocked.OutcomeDetail, restoreErr.Error())
	assert.Equal(t, "new binary", readFileString(t, f.targetPath),
		"a rollback that gave up leaves the failed version in place rather than nothing")
	assert.FileExists(t, f.snapshotPath, "the snapshot stays for a manual restore")
}

// A rollback puts the old binary back underneath the process that is running
// from it, which is the same problem the install swap has and the payload
// extras do not. Routing the binary through the plain replacement would leave
// Windows rollbacks failing where installs succeeded.
func TestRestoreReplacedFiles_RestoresTheBinaryThroughTheRunningImageSwap(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	fs := afero.NewOsFs()
	m := &pendingMarker{
		TargetPath: filepath.Join(dir, "zaparoo.exe"),
		BackupPath: filepath.Join(dir, "zaparoo.backup.exe"),
		PayloadBackups: []payloadBackup{{
			TargetPath: filepath.Join(dir, "extra.dll"),
			BackupPath: filepath.Join(dir, "extra.backup.dll"),
		}},
	}
	for _, path := range []string{m.BackupPath, m.PayloadBackups[0].BackupPath} {
		require.NoError(t, afero.WriteFile(fs, path, []byte("old"), 0o600))
	}

	var plain, running []string
	fileOps := watchdogFileOps{
		fs:      fs,
		replace: func(_, target string) error { plain = append(plain, target); return nil },
		binary: installBinaryOps{
			replaceRunning: func(_, target string) error { running = append(running, target); return nil },
		},
		syncDirectory: func(string) error { return nil },
	}

	require.NoError(t, restoreReplacedFiles(dir, m, fileOps))
	assert.Equal(t, []string{m.PayloadBackups[0].TargetPath}, plain)
	assert.Equal(t, []string{m.TargetPath}, running)
}

// A boot-time rollback puts the old binary back underneath the process running
// from the new one, which on Windows means the same two-rename swap the install
// used, on top of whatever that install had to leave behind. Driving it here
// checks the rollback against the state a Windows install actually leaves.
func TestRunStartupWatchdog_RollsBackThroughAVacatingSwap(t *testing.T) {
	t.Parallel()

	f := newInstallFixture(t)
	dir := stateDirFor(f.dataDir)
	require.NoError(t, saveMarker(dir, f.marker(markerConfirming)))
	// What the install swap left: a copy of the binary it replaced, under the
	// name the process it belonged to was running from until this boot.
	stale := supersededPathFor(f.targetPath, 0)
	//nolint:gosec // executable stand-in owned by this test
	require.NoError(t, os.WriteFile(stale, []byte("older binary"), 0o755))

	ops := defaultWatchdogFileOps()
	ops.binary = vacatingBinaryOps(mappedImageOps(f.targetPath))

	err := runStartupWatchdogWithOps(t.Context(), f.dataDir, testTargetVersion, ops)
	require.ErrorIs(t, err, ErrRolledBack)

	assert.Equal(t, "old binary", readFileString(t, f.targetPath))
	assert.NoFileExists(t, f.backupPath)
	// The rollback cleared the slot to move into, then filled it with the image
	// this process is running from, which is the one file it cannot delete.
	assert.Equal(t, "new binary", readFileString(t, stale),
		"the rolled-back binary stays put while the process running from it is alive")
	assert.NoFileExists(t, supersededPathFor(f.targetPath, 1),
		"a rollback reuses the slot it freed rather than taking another")
	assert.Equal(t, "before the update", readTestDBNote(t, f.dbPath))
	assert.NoFileExists(t, markerPath(dir))

	// Nothing holds it once that process exits, which is what the sweep at the
	// front of the next install is for.
	sweepSupersededBinaryWith(f.targetPath, realVacatingOps())
	assert.NoFileExists(t, stale, "the next install clears what the rollback had to leave")
}
