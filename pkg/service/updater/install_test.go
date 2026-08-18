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
	"os"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type installTestBackupper struct {
	version  string
	snapshot database.BackupInfo
	resumed  bool
}

func (b *installTestBackupper) BackupForUpdate(
	targetVersion string,
) (database.BackupInfo, func() error, error) {
	b.version = targetVersion
	return b.snapshot, func() error {
		b.resumed = true
		return nil
	}, nil
}

func TestInstallStaged_ArmsWatchdogBeforeRestart(t *testing.T) {
	require.NoError(t, errFakeBinary)

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
	backupper := &installTestBackupper{snapshot: database.BackupInfo{Path: snapshotPath}}

	err = installStaged(t.Context(), &installOptions{
		Staged: &StagedUpdate{
			Dir:        stagingDir,
			BinaryPath: stagedPath,
			Version:    testStageVersion,
		},
		UserDB:             backupper,
		TargetPath:         targetPath,
		DataDir:            dataDir,
		PreviousVersion:    testCurrentVersion,
		PlatformID:         testStagePlatform,
		ManifestGeneration: 412,
		Trigger:            triggerManual,
	})
	require.NoError(t, err)

	assert.Equal(t, testStageVersion, backupper.version)
	assert.False(t, backupper.resumed, "successful install keeps UserDB quiesced until restart")
	assert.Equal(t, "old binary", readFileString(t, installSidecarPath(targetPath, installBackupSuffix)))
	assert.FileExists(t, targetPath)
	assert.FileExists(t, snapshotPath)
	assert.DirExists(t, stagingDir)

	m, err := loadMarker(stateDirFor(dataDir))
	require.NoError(t, err)
	require.NotNil(t, m)
	assert.Equal(t, markerInstalled, m.State)
	assert.Equal(t, targetPath, m.TargetPath)
	assert.Equal(t, installSidecarPath(targetPath, installBackupSuffix), m.BackupPath)
	assert.Equal(t, snapshotPath, m.UserDBSnapshotPath)
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
