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
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	_ "github.com/mattn/go-sqlite3"
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

	require.NoError(t, RunStartupWatchdog(t.Context(), f.dataDir, testTargetVersion))

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
