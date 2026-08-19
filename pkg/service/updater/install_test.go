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
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type installTestBackupper struct {
	err       error
	resumeErr error
	version   string
	snapshot  database.BackupInfo
	called    bool
	resumed   bool
}

func (b *installTestBackupper) BackupForUpdate(
	targetVersion string,
) (database.BackupInfo, func() error, error) {
	b.called = true
	b.version = targetVersion
	if b.err != nil {
		return database.BackupInfo{}, nil, b.err
	}
	return b.snapshot, func() error {
		b.resumed = true
		return b.resumeErr
	}, nil
}

// installStagedFixture is a staged release sitting next to a live binary, laid
// out the way stage.go leaves it just before an install starts.
type installStagedFixture struct {
	backupper    *installTestBackupper
	dataDir      string
	targetPath   string
	stagedPath   string
	stagingDir   string
	snapshotPath string
}

func newInstallStagedFixture(t *testing.T) *installStagedFixture {
	t.Helper()
	require.NoError(t, errFakeBinary, "the fake release binary did not build")

	dataDir := t.TempDir()
	binDir := t.TempDir()
	targetPath := filepath.Join(binDir, testBinaryName("zaparoo"))
	//nolint:gosec // executable stand-in owned by this test
	require.NoError(t, os.WriteFile(targetPath, []byte("old binary"), 0o755))

	stagingDir := filepath.Join(stagingRootFor(dataDir), testStageVersion)
	require.NoError(t, os.MkdirAll(stagingDir, stateDirPerm))
	stagedPath := filepath.Join(stagingDir, testBinaryName("zaparoo"))
	fakeBinary, err := os.ReadFile(fakeBinaryPath) //nolint:gosec // package test fixture
	require.NoError(t, err)
	//nolint:gosec // executable stand-in owned by this test
	require.NoError(t, os.WriteFile(stagedPath, fakeBinary, 0o755))

	snapshotPath := filepath.Join(dataDir, "backups", "backup-20260818-043000-000000001-update.db")
	require.NoError(t, os.MkdirAll(filepath.Dir(snapshotPath), 0o750))
	require.NoError(t, os.WriteFile(snapshotPath, []byte("snapshot"), 0o600))

	return &installStagedFixture{
		backupper:    &installTestBackupper{snapshot: database.BackupInfo{Path: snapshotPath}},
		dataDir:      dataDir,
		targetPath:   targetPath,
		stagedPath:   stagedPath,
		stagingDir:   stagingDir,
		snapshotPath: snapshotPath,
	}
}

// blockStateDir makes the updater state directory unwritable so the marker
// cannot be armed, without disturbing anything the install reads first.
func (f *installStagedFixture) blockStateDir(t *testing.T) {
	t.Helper()
	dir := stateDirFor(f.dataDir)
	require.NoError(t, os.MkdirAll(dir, stateDirPerm))
	makeDirUnwritable(t, dir)
}

// assertInstallUndone checks that a failed install left the machine exactly as
// it found it: the old binary in place and no update artifacts behind.
func (f *installStagedFixture) assertInstallUndone(t *testing.T) {
	t.Helper()
	assert.Equal(t, "old binary", readFileString(t, f.targetPath))
	assert.NoFileExists(t, installSidecarPath(f.targetPath, installBackupSuffix))
	assert.NoFileExists(t, installSidecarPath(f.targetPath, installCandidateSuffix))
	assert.NoFileExists(t, f.snapshotPath)
	assert.NoDirExists(t, f.stagingDir)
}

func (f *installStagedFixture) options() *installOptions {
	return &installOptions{
		Staged: &StagedUpdate{
			Dir:        f.stagingDir,
			BinaryPath: f.stagedPath,
			Version:    testStageVersion,
		},
		UserDB:          f.backupper,
		TargetPath:      f.targetPath,
		DataDir:         f.dataDir,
		PreviousVersion: testCurrentVersion,
		PlatformID:      testStagePlatform,
		Trigger:         triggerManual,
	}
}

func TestInstallStaged_ArmsWatchdogBeforeRestart(t *testing.T) {
	f := newInstallStagedFixture(t)
	opts := f.options()
	opts.ManifestGeneration = 412

	require.NoError(t, installStaged(t.Context(), opts))

	assert.Equal(t, testStageVersion, f.backupper.version)
	assert.False(t, f.backupper.resumed, "successful install keeps UserDB quiesced until restart")
	assert.Equal(t, "old binary", readFileString(t, installSidecarPath(f.targetPath, installBackupSuffix)))
	assert.FileExists(t, f.targetPath)
	assert.FileExists(t, f.snapshotPath)
	assert.DirExists(t, f.stagingDir)

	m, err := loadMarker(stateDirFor(f.dataDir))
	require.NoError(t, err)
	require.NotNil(t, m)
	assert.Equal(t, markerInstalled, m.State)
	assert.Equal(t, f.targetPath, m.TargetPath)
	assert.Equal(t, installSidecarPath(f.targetPath, installBackupSuffix), m.BackupPath)
	assert.Equal(t, f.snapshotPath, m.UserDBSnapshotPath)
	assert.Equal(t, testCurrentVersion, m.PreviousVersion)
	assert.Equal(t, testStageVersion, m.TargetVersion)
	assert.Equal(t, int64(412), m.ManifestGeneration)
}

func TestPreserveCurrentBinary_LeavesBootTargetPresent(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	targetPath := filepath.Join(dir, "zaparoo")
	backupPath := installSidecarPath(targetPath, installBackupSuffix)
	//nolint:gosec // executable stand-in owned by this test
	require.NoError(t, os.WriteFile(targetPath, []byte("old binary"), 0o755))

	require.NoError(t, preserveCurrentBinary(targetPath, backupPath))

	assert.Equal(t, "old binary", readFileString(t, targetPath))
	assert.Equal(t, "old binary", readFileString(t, backupPath))
}

func TestInstallStaged_ReopensUserDBAfterInstallFailure(t *testing.T) {
	require.NoError(t, errFakeBinary)

	dataDir := t.TempDir()
	binDir := t.TempDir()
	targetPath := filepath.Join(binDir, testBinaryName("missing-target"))
	stagingDir := filepath.Join(stagingRootFor(dataDir), testStageVersion)
	require.NoError(t, os.MkdirAll(stagingDir, stateDirPerm))
	stagedPath := filepath.Join(stagingDir, testBinaryName("zaparoo"))
	fakeBinary, err := os.ReadFile(fakeBinaryPath) //nolint:gosec // package test fixture
	require.NoError(t, err)
	//nolint:gosec // executable stand-in owned by this test
	require.NoError(t, os.WriteFile(stagedPath, fakeBinary, 0o755))

	snapshotPath := filepath.Join(dataDir, "backups", "backup-20260818-043000-000000001-update.db")
	require.NoError(t, os.MkdirAll(filepath.Dir(snapshotPath), 0o750))
	require.NoError(t, os.WriteFile(snapshotPath, []byte("snapshot"), 0o600))
	backupper := &installTestBackupper{snapshot: database.BackupInfo{Path: snapshotPath}}

	err = installStaged(t.Context(), &installOptions{
		Staged:          &StagedUpdate{Dir: stagingDir, BinaryPath: stagedPath, Version: testStageVersion},
		UserDB:          backupper,
		TargetPath:      targetPath,
		DataDir:         dataDir,
		PreviousVersion: testCurrentVersion,
		PlatformID:      testStagePlatform,
		Trigger:         triggerManual,
	})
	require.Error(t, err)
	assert.True(t, backupper.resumed)
	assert.NoFileExists(t, markerPath(stateDirFor(dataDir)))
}

func TestInstallSidecarPath_PreservesExecutableExtension(t *testing.T) {
	t.Parallel()

	assert.Equal(t,
		filepath.Join("bin", "zaparoo"+installBackupSuffix+".exe"),
		installSidecarPath(filepath.Join("bin", "zaparoo.exe"), installBackupSuffix))
	assert.Equal(t,
		filepath.Join("bin", "zaparoo"+installBackupSuffix),
		installSidecarPath(filepath.Join("bin", "zaparoo"), installBackupSuffix))
}

func TestInstallStaged_RejectsIncompleteOptions(t *testing.T) {
	t.Parallel()

	staged := &StagedUpdate{Dir: "staging", BinaryPath: "staged", Version: testStageVersion}
	backupper := &installTestBackupper{}

	for name, opts := range map[string]*installOptions{
		"nil options": nil,
		"no staged release": {
			UserDB: backupper, TargetPath: "target", DataDir: "data",
		},
		"no user database": {
			Staged: staged, TargetPath: "target", DataDir: "data",
		},
		"no target path": {
			Staged: staged, UserDB: backupper, DataDir: "data",
		},
		"no data directory": {
			Staged: staged, UserDB: backupper, TargetPath: "target",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			require.Error(t, installStaged(t.Context(), opts))
			assert.Empty(t, backupper.version, "an incomplete install must not touch the user database")
		})
	}
}

// TestInstallStaged_RefusesWhileAnUpdateIsUnresolved keeps a second install from
// overwriting the binary backup and marker the watchdog still needs to undo the
// first one.
func TestInstallStaged_RefusesWhileAnUpdateIsUnresolved(t *testing.T) {
	f := newInstallStagedFixture(t)
	dir := stateDirFor(f.dataDir)
	require.NoError(t, saveMarker(dir, &pendingMarker{
		State:         markerInstalled,
		TargetPath:    f.targetPath,
		TargetVersion: "9.9.9",
	}))

	err := installStaged(t.Context(), f.options())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "9.9.9")
	assert.False(t, f.backupper.called, "a blocked install must not quiesce the user database")

	m, err := loadMarker(dir)
	require.NoError(t, err)
	require.NotNil(t, m)
	assert.Equal(t, "9.9.9", m.TargetVersion, "the unresolved marker must survive untouched")
}

// TestInstallStaged_ClearsAnOrphanedBinaryBackup covers the retry after an
// install that finished its cleanup incompletely: the previous backup is stale
// once the target is present, and keeping it would make the next rollback
// restore the wrong binary.
func TestInstallStaged_ClearsAnOrphanedBinaryBackup(t *testing.T) {
	f := newInstallStagedFixture(t)
	backupPath := installSidecarPath(f.targetPath, installBackupSuffix)
	//nolint:gosec // executable stand-in owned by this test
	require.NoError(t, os.WriteFile(backupPath, []byte("orphaned backup"), 0o755))

	require.NoError(t, installStaged(t.Context(), f.options()))

	assert.Equal(t, "old binary", readFileString(t, backupPath),
		"the backup must describe the binary this install replaced, not the orphan")
}

func TestInstallStaged_RefusesWhenAnOrphanedBackupHasNoTarget(t *testing.T) {
	f := newInstallStagedFixture(t)
	backupPath := installSidecarPath(f.targetPath, installBackupSuffix)
	//nolint:gosec // executable stand-in owned by this test
	require.NoError(t, os.WriteFile(backupPath, []byte("orphaned backup"), 0o755))
	require.NoError(t, os.Remove(f.targetPath))

	err := installStaged(t.Context(), f.options())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "orphaned binary backup")
	assert.Equal(t, "orphaned backup", readFileString(t, backupPath),
		"the only remaining copy of a binary must not be removed")
	assert.False(t, f.backupper.called)
}

// TestInstallStaged_LeavesTheUserDBAloneWhenTheCandidateWillNotRun proves the
// probe on the target filesystem runs before anything the old version depends
// on is disturbed.
func TestInstallStaged_LeavesTheUserDBAloneWhenTheCandidateWillNotRun(t *testing.T) {
	f := newInstallStagedFixture(t)
	//nolint:gosec // deliberately not an executable
	require.NoError(t, os.WriteFile(f.stagedPath, []byte("not a real binary"), 0o755))

	err := installStaged(t.Context(), f.options())
	require.Error(t, err)

	assert.False(t, f.backupper.called, "the user database must stay open when the candidate fails")
	assert.Equal(t, "old binary", readFileString(t, f.targetPath))
	assert.NoFileExists(t, installSidecarPath(f.targetPath, installCandidateSuffix))
	assert.NoFileExists(t, markerPath(stateDirFor(f.dataDir)))
	assert.NoDirExists(t, f.stagingDir, "a condemned release must not keep occupying the device")
}

// TestInstallStaged_CleansUpWhenTheSnapshotFails covers the window between a
// verified candidate and an armed marker: without the snapshot there is nothing
// to roll the database back to, so the install must not start.
func TestInstallStaged_CleansUpWhenTheSnapshotFails(t *testing.T) {
	f := newInstallStagedFixture(t)
	f.backupper.err = errors.New("database is busy")

	err := installStaged(t.Context(), f.options())
	require.Error(t, err)
	require.ErrorIs(t, err, f.backupper.err)

	assert.Equal(t, "old binary", readFileString(t, f.targetPath))
	assert.NoFileExists(t, installSidecarPath(f.targetPath, installCandidateSuffix))
	assert.NoFileExists(t, markerPath(stateDirFor(f.dataDir)))
	assert.NoDirExists(t, f.stagingDir)
}

func TestAbortInstall_RestoresTheCurrentBinary(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	binDir := t.TempDir()
	targetPath := filepath.Join(binDir, testBinaryName("zaparoo"))
	backupPath := installSidecarPath(targetPath, installBackupSuffix)
	candidatePath := installSidecarPath(targetPath, installCandidateSuffix)
	//nolint:gosec // executable stand-ins owned by this test
	require.NoError(t, os.WriteFile(targetPath, []byte("half-installed binary"), 0o755))
	//nolint:gosec // executable stand-ins owned by this test
	require.NoError(t, os.WriteFile(backupPath, []byte("old binary"), 0o755))
	//nolint:gosec // executable stand-ins owned by this test
	require.NoError(t, os.WriteFile(candidatePath, []byte("candidate"), 0o755))

	snapshotPath := filepath.Join(dataDir, "backups", "backup-20260818-043000-000000001-update.db")
	require.NoError(t, os.MkdirAll(filepath.Dir(snapshotPath), 0o750))
	require.NoError(t, os.WriteFile(snapshotPath, []byte("snapshot"), 0o600))
	stagingDir := filepath.Join(stagingRootFor(dataDir), testStageVersion)
	require.NoError(t, os.MkdirAll(stagingDir, stateDirPerm))

	m := &pendingMarker{
		State:              markerInstalling,
		TargetPath:         targetPath,
		BackupPath:         backupPath,
		StagingDir:         stagingDir,
		UserDBSnapshotPath: snapshotPath,
		PreviousVersion:    testCurrentVersion,
		TargetVersion:      testStageVersion,
	}
	require.NoError(t, saveMarker(stateDirFor(dataDir), m))

	require.NoError(t, abortInstall(t.Context(), dataDir, m, candidatePath))

	assert.Equal(t, "old binary", readFileString(t, targetPath))
	assert.NoFileExists(t, backupPath)
	assert.NoFileExists(t, candidatePath)
	assert.NoFileExists(t, snapshotPath)
	assert.NoDirExists(t, stagingDir)
	assert.NoFileExists(t, markerPath(stateDirFor(dataDir)))
}

func TestAbortInstall_FailsWhenNeitherBinaryNorBackupExists(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	binDir := t.TempDir()
	targetPath := filepath.Join(binDir, testBinaryName("zaparoo"))

	err := abortInstall(t.Context(), dataDir, &pendingMarker{
		State:      markerInstalling,
		TargetPath: targetPath,
		BackupPath: installSidecarPath(targetPath, installBackupSuffix),
	}, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "neither current binary nor backup")
}

func TestAbortInstall_WithoutAMarkerDoesNothing(t *testing.T) {
	t.Parallel()

	require.NoError(t, abortInstall(t.Context(), t.TempDir(), nil, ""))
}

// A marker left unreadable by an earlier crash means an update may still be
// unresolved. Starting a second install on top of it would lose the record of
// the first, so the install has to refuse rather than assume nothing is pending.
func TestInstallStaged_RefusesWhenTheMarkerIsUnreadable(t *testing.T) {
	f := newInstallStagedFixture(t)
	dir := stateDirFor(f.dataDir)
	require.NoError(t, os.MkdirAll(dir, stateDirPerm))
	require.NoError(t, os.WriteFile(markerPath(dir), []byte("{not json"), 0o600))

	err := installStaged(t.Context(), f.options())

	require.ErrorContains(t, err, "checking for an unresolved update")
	assert.False(t, f.backupper.called, "the database must not be quiesced for an install that cannot start")
	assert.Equal(t, "old binary", readFileString(t, f.targetPath))
}

// The marker is what makes an install recoverable, so it is written before the
// binary is swapped. If it cannot be written the install must undo itself
// completely instead of running a new binary nothing is watching.
func TestInstallStaged_UndoesEverythingWhenTheMarkerCannotBeArmed(t *testing.T) {
	skipUnlessDirPermsEnforced(t)

	f := newInstallStagedFixture(t)
	f.blockStateDir(t)

	err := installStaged(t.Context(), f.options())

	require.ErrorContains(t, err, "recording the start of the update install")
	assert.True(t, f.backupper.resumed, "the user database must be reopened after a failed install")
	f.assertInstallUndone(t)

	m, loadErr := loadMarker(stateDirFor(f.dataDir))
	require.NoError(t, loadErr)
	assert.Nil(t, m)
}

// A database that will not reopen leaves the service running without storage,
// so that failure has to reach the caller alongside the install failure rather
// than being lost behind it.
func TestInstallStaged_ReportsAFailedDatabaseReopen(t *testing.T) {
	skipUnlessDirPermsEnforced(t)

	f := newInstallStagedFixture(t)
	f.blockStateDir(t)
	reopenErr := errors.New("database is locked")
	f.backupper.resumeErr = reopenErr

	err := installStaged(t.Context(), f.options())

	require.ErrorIs(t, err, reopenErr)
	require.ErrorContains(t, err, "recording the start of the update install")
	assert.ErrorContains(t, err, "reopening user database after failed update install")
}

// When an install fails and the undo fails too the machine is in the state the
// operator most needs described, so neither failure may be dropped.
func TestAbortInstallAfterError_ReportsBothFailures(t *testing.T) {
	f := newInstallStagedFixture(t)
	backupPath := installSidecarPath(f.targetPath, installBackupSuffix)
	//nolint:gosec // executable stand-in owned by this test
	require.NoError(t, os.WriteFile(backupPath, []byte("old binary"), 0o755))

	m := &pendingMarker{
		State:      markerInstalling,
		TargetPath: filepath.Join(f.dataDir, "gone", "zaparoo"),
		BackupPath: backupPath,
	}
	installErr := errors.New("installing the staged binary")

	err := abortInstallAfterError(t.Context(), f.dataDir, m, "", installErr)

	require.ErrorIs(t, err, installErr)
	require.ErrorContains(t, err, "aborting the partial install also failed")
	require.ErrorContains(t, err, "restoring the current binary after a failed install")
	assert.FileExists(t, backupPath, "a backup that could not be restored must be kept")
}
