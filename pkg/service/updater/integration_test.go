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
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/userdb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/updater/otameta"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The unit tests in this package each hold one stage still and drive it with
// stand-ins. These drive the whole chain — signed manifest, real archive over
// HTTP, real binary swap, real SQLite user database — because the failures worth
// catching here live in the seams: a snapshot taken of the wrong file, a
// rollback that restores a binary but not the database it has to open, a
// database migrated past what the restored binary understands. None of those are
// visible from inside a single stage.

const (
	// otaManifestGeneration is any value above zero; the watermark itself is
	// covered by the source tests.
	otaManifestGeneration = 7

	// otaOutgoingBinary stands in for the running executable. It is deliberately
	// not the fake release binary: after a rollback the target has to be shown to
	// be the old file and not the new one, and two identical files cannot show
	// that. Nothing in the chain executes the outgoing binary — only the staged
	// candidate is probed — so its contents are free.
	otaOutgoingBinary = "outgoing zaparoo build 2.10.1"

	otaSeedKey   = "integration-seed"
	otaSeedValue = "written before the update"
)

// otaHarness is one device: a data directory holding the user database, the
// updater's state and the backups, an install directory holding the binary, and
// the two servers a release arrives from.
type otaHarness struct {
	userDB     *userdb.UserDB
	ms         *manifestServer
	archive    *otameta.Asset
	dataDir    string
	targetPath string
}

func newOTAHarness(t *testing.T) *otaHarness {
	t.Helper()

	// The data directory is the platform's, so the user database, the updater
	// state directory and the backup directory sit where production puts them
	// relative to each other.
	dataDir := t.TempDir()
	pl := mocks.NewMockPlatform()
	pl.On("Settings").Return(platforms.Settings{DataDir: dataDir})

	db, err := userdb.OpenUserDB(t.Context(), pl)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.Equal(t, filepath.Join(dataDir, config.UserDbFile), db.GetDBPath(),
		"the harness only proves anything if the updater and the database agree on the data directory")
	require.NoError(t, db.SetDeviceState(otaSeedKey, otaSeedValue))

	targetPath := filepath.Join(t.TempDir(), testBinaryName("zaparoo"))
	//nolint:gosec // an executable stand-in owned by this test
	require.NoError(t, os.WriteFile(targetPath, []byte(otaOutgoingBinary), 0o755))

	archive := servedAsset(t, testArchiveName(testStageVersion, otameta.ArchiveExtTarGz),
		releaseArchive(t, otameta.ArchiveExtTarGz, testBinaryName("zaparoo"), fakeBinaryBytes(t)))

	return &otaHarness{
		dataDir:    dataDir,
		targetPath: targetPath,
		userDB:     db,
		archive:    archive,
		ms:         newManifestServer(t, otaReleaseManifest(otaManifestGeneration, archive)),
	}
}

// otaReleaseManifest is the document the CDN serves: one stable release whose
// archive lives on another host entirely, which is how the published manifest
// refers to release assets.
func otaReleaseManifest(generation int64, archive *otameta.Asset) string {
	return fmt.Sprintf(`manifest_version: 1
generation: %d
issued_at: 2026-08-17T02:00:00Z
key_id: test1
last_release_id: 1
last_asset_id: 3
releases:
  - id: 1
    name: v%[2]s
    tag_name: v%[2]s
    channel: stable
    published_at: 2026-08-10T00:00:00Z
    release_notes: integration release
    assets:
      - id: 1
        name: %[3]s
        size: %[4]d
        sha256: %[5]s
        url: %[6]s
      - id: 2
        name: checksums.txt
        url: checksums.txt
      - id: 3
        name: checksums.txt.sig
        url: checksums.txt.sig
`, generation, testStageVersion, archive.Name, archive.Size, archive.SHA256, archive.URL)
}

// detect runs the same detection Apply runs: fetch and verify the manifest,
// then take the release this device is offered out of it.
func (h *otaHarness) detect(t *testing.T) (*verifiedSource, *otameta.Release) {
	t.Helper()

	src := h.ms.source(stateDirFor(h.dataDir), testStagePlatform, testStageArch)
	require.NoError(t, src.load(t.Context(), updateOwner, updateRepo))

	release, err := src.selectRelease(otameta.ChannelStable)
	require.NoError(t, err)
	require.NotNil(t, release, "the manifest offers a release for this platform")
	require.Equal(t, testStageVersion, otameta.VersionFromTag(release.TagName))
	return src, release
}

// install runs detection, staging and the install, leaving the watchdog armed
// exactly as a real apply does just before the service restarts.
func (h *otaHarness) install(t *testing.T) *StagedUpdate {
	t.Helper()

	src, manifestRelease := h.detect(t)
	staged, err := stageRelease(t.Context(), &StageOptions{
		Release:        manifestRelease,
		PlatformID:     testStagePlatform,
		Arch:           testStageArch,
		OS:             "linux",
		TargetPath:     h.targetPath,
		StagingRoot:    stagingRootFor(h.dataDir),
		CurrentVersion: testCurrentVersion,
	}, testAssetFetcher(t))
	require.NoError(t, err)
	require.Equal(t, testStageVersion, staged.Version)

	require.NoError(t, installStaged(t.Context(), &installOptions{
		Staged:             staged,
		UserDB:             h.userDB,
		TargetPath:         h.targetPath,
		DataDir:            h.dataDir,
		PreviousVersion:    testCurrentVersion,
		PlatformID:         testStagePlatform,
		ManifestGeneration: src.manifestGeneration(),
		Trigger:            triggerManual,
	}))
	return staged
}

// marker reads what is on disk rather than what a call returned, because the
// marker is the only thing the next boot gets to see.
func (h *otaHarness) marker(t *testing.T) *pendingMarker {
	t.Helper()

	m, err := loadMarker(stateDirFor(h.dataDir))
	require.NoError(t, err)
	return m
}

func (h *otaHarness) result(t *testing.T) *updateResult {
	t.Helper()

	res := peekUpdateResult(stateDirFor(h.dataDir))
	require.NotNil(t, res, "a finished update has to leave something for the next boot to report")
	return res
}

// reopenUserDB opens the database the way a booting binary does, migrations and
// all, so a schema the restored build cannot handle fails here rather than
// silently passing.
func (h *otaHarness) reopenUserDB(t *testing.T) *userdb.UserDB {
	t.Helper()

	pl := mocks.NewMockPlatform()
	pl.On("Settings").Return(platforms.Settings{DataDir: h.dataDir})
	db, err := userdb.OpenUserDB(t.Context(), pl)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func assertSeedIntact(t *testing.T, db *userdb.UserDB) {
	t.Helper()

	value, found, err := db.GetDeviceState(otaSeedKey)
	require.NoError(t, err)
	require.True(t, found, "the row written before the update has to survive")
	assert.Equal(t, otaSeedValue, value)
}

// openRawUserDB opens the database file directly. The updater closes the live
// pool for the snapshot and does not reopen it, so anything acting as the newly
// installed version has to bring its own connection.
func openRawUserDB(t *testing.T, dataDir string) *sql.DB {
	t.Helper()

	raw, err := sql.Open("sqlite3", filepath.Join(dataDir, config.UserDbFile))
	require.NoError(t, err)
	t.Cleanup(func() { _ = raw.Close() })
	return raw
}

// TestOTA_InstallsAVerifiedReleaseAndConfirmsIt walks a release from a signed
// manifest onto the target path and through the confirmation the next boot
// performs, then checks that nothing the update needed is still on disk.
func TestOTA_InstallsAVerifiedReleaseAndConfirmsIt(t *testing.T) {
	t.Parallel()

	h := newOTAHarness(t)
	staged := h.install(t)

	installed, err := os.ReadFile(h.targetPath) //nolint:gosec // path owned by this test
	require.NoError(t, err)
	assert.Equal(t, fakeBinaryBytes(t), installed, "the target has to be the staged release binary")

	armed := h.marker(t)
	require.NotNil(t, armed, "the install has to arm the watchdog before the service restarts")
	assert.Equal(t, markerInstalled, armed.State)
	assert.Equal(t, testCurrentVersion, armed.PreviousVersion)
	assert.Equal(t, testStageVersion, armed.TargetVersion)
	assert.Equal(t, int64(otaManifestGeneration), armed.ManifestGeneration)
	assert.FileExists(t, armed.BackupPath, "rollback needs the outgoing binary kept")
	assert.FileExists(t, armed.UserDBSnapshotPath, "rollback needs the pre-update database kept")

	// First boot of the new version.
	require.NoError(t, RunStartupWatchdog(t.Context(), h.dataDir, testStageVersion))
	assert.Equal(t, markerConfirming, h.marker(t).State)

	confirmed, err := Confirm(t.Context(), h.dataDir, testStageVersion)
	require.NoError(t, err)
	assert.Equal(t, testStageVersion, confirmed)

	assert.Nil(t, h.marker(t), "a confirmed update leaves no marker for the next boot")
	assert.NoFileExists(t, armed.BackupPath)
	assert.NoFileExists(t, armed.UserDBSnapshotPath)
	assert.NoFileExists(t, installSidecarPath(h.targetPath, installCandidateSuffix))
	assert.NoDirExists(t, staged.Dir, "the staged payload is dead weight once the update is confirmed")

	res := h.result(t)
	assert.Equal(t, outcomeSucceeded, res.Outcome)
	assert.Equal(t, testCurrentVersion, res.FromVersion)
	assert.Equal(t, testStageVersion, res.ToVersion)

	assertSeedIntact(t, h.reopenUserDB(t))
}

// TestOTA_RollsBackAVersionThatNeverConfirms is the failure the whole mechanism
// exists for: the new binary starts, writes to the database, and then never
// survives startup. Both the binary and the database have to go back.
func TestOTA_RollsBackAVersionThatNeverConfirms(t *testing.T) {
	t.Parallel()

	h := newOTAHarness(t)
	staged := h.install(t)
	armed := h.marker(t)
	require.NotNil(t, armed)

	// First boot of the new version: it gets as far as confirming.
	require.NoError(t, RunStartupWatchdog(t.Context(), h.dataDir, testStageVersion))

	// ...and then writes something before dying, which the rollback must undo
	// along with the binary.
	newVersionDB := h.reopenUserDB(t)
	require.NoError(t, newVersionDB.SetDeviceState("written-by-the-new-version", "1"))
	require.NoError(t, newVersionDB.Close())

	// Second boot: still the new version, still unconfirmed.
	err := RunStartupWatchdog(t.Context(), h.dataDir, testStageVersion)
	require.ErrorIs(t, err, ErrRolledBack)
	restartInto, ok := RollbackTargetPath(err)
	require.True(t, ok, "the caller has to be told which binary to re-exec")
	assert.Equal(t, h.targetPath, restartInto)

	assert.Equal(t, otaOutgoingBinary, readFileString(t, h.targetPath),
		"the target has to be the outgoing binary again, byte for byte")
	assert.Nil(t, h.marker(t))
	assert.NoFileExists(t, armed.BackupPath)
	assert.NoFileExists(t, armed.UserDBSnapshotPath)
	assert.NoDirExists(t, staged.Dir)

	res := h.result(t)
	assert.Equal(t, outcomeRolledBack, res.Outcome)
	assert.Equal(t, testStageVersion, res.ToVersion)
	assert.Equal(t, testCurrentVersion, res.FromVersion)

	restored := h.reopenUserDB(t)
	assertSeedIntact(t, restored)
	_, found, err := restored.GetDeviceState("written-by-the-new-version")
	require.NoError(t, err)
	assert.False(t, found, "writes made by the version being rolled back must not survive it")
}

// TestOTA_RollsBackAcrossASchemaMigration is the case that decides whether a
// rollback is worth having at all. A new version that migrates the user database
// and then fails leaves a schema the restored binary refuses to open, and a
// device with no supervisor cannot recover from that by itself. The snapshot is
// the only thing standing between the two.
func TestOTA_RollsBackAcrossASchemaMigration(t *testing.T) {
	t.Parallel()

	h := newOTAHarness(t)
	h.install(t)
	require.NoError(t, RunStartupWatchdog(t.Context(), h.dataDir, testStageVersion))

	// Stand in for a migration the incoming release carries and this build does
	// not: goose records nothing but the version, so a row above the highest
	// embedded migration is exactly what a newer binary leaves behind.
	raw := openRawUserDB(t, h.dataDir)
	var applied int64
	require.NoError(t, raw.QueryRowContext(t.Context(),
		"SELECT MAX(version_id) FROM goose_db_version").Scan(&applied))
	_, err := raw.ExecContext(t.Context(),
		"INSERT INTO goose_db_version (version_id, is_applied) VALUES (?, 1)", applied+1)
	require.NoError(t, err)
	require.NoError(t, raw.Close())

	// The premise, asserted rather than assumed: without the rollback this
	// database is one the outgoing build will not open.
	crossed := h.reopenUserDB(t)
	require.ErrorIs(t, crossed.MigrateUp(), database.ErrSchemaAhead,
		"the test only means something if the crossed schema really does lock this build out")
	require.NoError(t, crossed.Close())

	require.ErrorIs(t, RunStartupWatchdog(t.Context(), h.dataDir, testStageVersion), ErrRolledBack)

	restored := h.reopenUserDB(t)
	require.NoError(t, restored.MigrateUp(),
		"the restored snapshot has to be a database the outgoing build can migrate and open")
	assertSeedIntact(t, restored)
}

// TestOTA_ReopensTheUserDatabaseWhenTheInstallFails covers the one thing a
// stand-in backupper cannot: BackupForUpdate closes the live connection pool,
// and an install that fails after that point owes the running service a working
// database back.
func TestOTA_ReopensTheUserDatabaseWhenTheInstallFails(t *testing.T) {
	skipUnlessDirPermsEnforced(t)
	t.Parallel()

	h := newOTAHarness(t)
	src, manifestRelease := h.detect(t)
	staged, err := stageRelease(t.Context(), &StageOptions{
		Release:        manifestRelease,
		PlatformID:     testStagePlatform,
		Arch:           testStageArch,
		OS:             "linux",
		TargetPath:     h.targetPath,
		StagingRoot:    stagingRootFor(h.dataDir),
		CurrentVersion: testCurrentVersion,
	}, testAssetFetcher(t))
	require.NoError(t, err)

	// Readable so the install still gets as far as taking the snapshot, and
	// unwritable so arming the marker is what fails.
	stateDir := stateDirFor(h.dataDir)
	require.NoError(t, os.MkdirAll(stateDir, stateDirPerm))
	makeDirUnwritable(t, stateDir)

	installErr := installStaged(t.Context(), &installOptions{
		Staged:             staged,
		UserDB:             h.userDB,
		TargetPath:         h.targetPath,
		DataDir:            h.dataDir,
		PreviousVersion:    testCurrentVersion,
		PlatformID:         testStagePlatform,
		ManifestGeneration: src.manifestGeneration(),
		Trigger:            triggerManual,
	})
	require.Error(t, installErr)
	assert.NotContains(t, installErr.Error(), "reopening user database",
		"the install failed, but reopening the database it closed must not have")

	assert.Equal(t, otaOutgoingBinary, readFileString(t, h.targetPath),
		"a failed install leaves the running binary exactly where it was")
	assert.NoFileExists(t, installSidecarPath(h.targetPath, installBackupSuffix))
	assert.NoFileExists(t, installSidecarPath(h.targetPath, installCandidateSuffix))

	require.NoError(t, h.userDB.SetDeviceState("after-the-failed-install", "1"),
		"the service carries on running, so its database has to be usable again")
	assertSeedIntact(t, h.userDB)
}
