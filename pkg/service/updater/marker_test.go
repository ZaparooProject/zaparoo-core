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
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMarker_RoundTrip(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "updater")
	installed := time.Date(2026, 8, 18, 4, 30, 0, 0, time.UTC)

	require.NoError(t, saveMarker(dir, &pendingMarker{
		InstalledAt:        installed,
		State:              markerInstalled,
		Trigger:            triggerManual,
		TargetPath:         filepath.Join("opt", "zaparoo"),
		BackupPath:         filepath.Join("opt", ".zaparoo.zap-old-2.1.0"),
		StagingDir:         filepath.Join("data", "updater", "staging", "2.2.0"),
		PreviousVersion:    "2.1.0",
		TargetVersion:      "2.2.0",
		PlatformID:         "mister",
		UserDBSnapshotPath: filepath.Join("data", "backups", "backup-x-update.db"),
		PayloadBackups: []payloadBackup{
			{TargetPath: filepath.Join("opt", "extra"), BackupPath: filepath.Join("opt", ".extra.old")},
		},
		ManifestGeneration: 42,
		Attempts:           1,
	}))

	got, err := loadMarker(dir)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.True(t, installed.Equal(got.InstalledAt))
	assert.Equal(t, markerInstalled, got.State)
	assert.Equal(t, triggerManual, got.Trigger)
	assert.Equal(t, "2.1.0", got.PreviousVersion)
	assert.Equal(t, "2.2.0", got.TargetVersion)
	assert.Equal(t, "mister", got.PlatformID)
	assert.Equal(t, int64(42), got.ManifestGeneration)
	assert.Equal(t, 1, got.Attempts)
	assert.Equal(t, currentMarkerVersion, got.MarkerVersion)
	require.Len(t, got.PayloadBackups, 1)
	assert.Equal(t, filepath.Join("opt", "extra"), got.PayloadBackups[0].TargetPath)
}

func TestLoadMarker_AbsentIsNotAnError(t *testing.T) {
	t.Parallel()

	m, err := loadMarker(filepath.Join(t.TempDir(), "updater"))
	require.NoError(t, err)
	assert.Nil(t, m)

	m, err = loadMarker("")
	require.NoError(t, err)
	assert.Nil(t, m)
}

func TestLoadMarker_QuarantinesUnparseable(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "updater")
	require.NoError(t, os.MkdirAll(dir, stateDirPerm))
	require.NoError(t, os.WriteFile(markerPath(dir), []byte("{not json"), stateFilePerm))

	m, err := loadMarker(dir)
	require.ErrorIs(t, err, errMarkerUnusable)
	assert.Nil(t, m)

	_, statErr := os.Stat(markerPath(dir))
	assert.True(t, os.IsNotExist(statErr), "unparseable marker should have been moved aside")
	quarantined, readErr := os.ReadFile(markerPath(dir) + markerBadSuffix)
	require.NoError(t, readErr)
	assert.Equal(t, "{not json", string(quarantined))
}

func TestLoadMarker_QuarantinesStateless(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "updater")
	require.NoError(t, os.MkdirAll(dir, stateDirPerm))
	require.NoError(t, os.WriteFile(markerPath(dir),
		[]byte(`{"markerVersion":1,"targetVersion":"2.2.0"}`), stateFilePerm))

	m, err := loadMarker(dir)
	require.ErrorIs(t, err, errMarkerUnusable)
	assert.Nil(t, m)
	assert.FileExists(t, markerPath(dir)+markerBadSuffix)
}

// A marker from a newer build has to survive this one completely intact: that
// build may be rolled back to, run again and need to find its own marker.
func TestLoadMarker_LeavesNewerSchemaAlone(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "updater")
	require.NoError(t, os.MkdirAll(dir, stateDirPerm))
	raw := []byte(`{"markerVersion":99,"state":"installed","targetVersion":"9.0.0"}`)
	require.NoError(t, os.WriteFile(markerPath(dir), raw, stateFilePerm))

	m, err := loadMarker(dir)
	require.ErrorIs(t, err, errMarkerTooNew)
	assert.Nil(t, m)

	onDisk, readErr := os.ReadFile(markerPath(dir))
	require.NoError(t, readErr)
	assert.Equal(t, raw, onDisk)
	assert.NoFileExists(t, markerPath(dir)+markerBadSuffix)

	// Saving over it is refused for the same reason.
	require.NoError(t, saveMarker(dir, &pendingMarker{State: markerInstalled, MarkerVersion: 99}))
	onDisk, readErr = os.ReadFile(markerPath(dir))
	require.NoError(t, readErr)
	assert.Equal(t, raw, onDisk)
}

func TestClearMarker_ToleratesAbsence(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "updater")
	require.NoError(t, os.MkdirAll(dir, stateDirPerm))
	require.NoError(t, clearMarker(dir))

	require.NoError(t, saveMarker(dir, &pendingMarker{State: markerConfirming, TargetVersion: "2.2.0"}))
	require.NoError(t, clearMarker(dir))
	assert.NoFileExists(t, markerPath(dir))
}
