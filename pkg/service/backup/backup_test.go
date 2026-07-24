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

package backup

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	platformids "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/ids"
	inboxservice "github.com/ZaparooProject/zaparoo-core/v2/pkg/service/inbox"
	testinghelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	testifymock "github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type errorReader struct {
	err error
}

func (r *errorReader) Read([]byte) (int, error) {
	return 0, r.err
}

type blockingReadCloser struct {
	closed chan struct{}
	once   sync.Once
}

func (r *blockingReadCloser) Read([]byte) (int, error) {
	<-r.closed
	return 0, errors.New("reader closed")
}

func (r *blockingReadCloser) Close() error {
	r.once.Do(func() { close(r.closed) })
	return nil
}

type backupTestEnv struct {
	Manager      *Manager
	UserDB       *testinghelpers.MockUserDBI
	RootDir      string
	ConfigDir    string
	DataDir      string
	UserSnapshot string
}

type backupPlatform struct {
	*mocks.MockPlatform
	definitions []platforms.BackupDefinition
}

type backupRestoreRootPlatform struct {
	restoreRoot string
	backupPlatform
}

type backupRestorePreparingPlatform struct {
	finished *bool
	prepared *bool
	backupPlatform
}

type backupPlanningTestPlatform struct {
	backupPlatform
	plan platforms.BackupPlan
}

func (p backupPlatform) BackupDefinitions() []platforms.BackupDefinition {
	return p.definitions
}

func (p backupRestoreRootPlatform) BackupRestoreRoot() string {
	return p.restoreRoot
}

func (p *backupPlanningTestPlatform) BackupPlan() platforms.BackupPlan {
	return p.plan
}

func (p *backupRestorePreparingPlatform) PrepareBackupRestore() (func(bool) error, error) {
	*p.prepared = true
	return func(success bool) error {
		*p.finished = success
		return nil
	}, nil
}

func newBackupTestEnv(t *testing.T, platformID string) backupTestEnv {
	t.Helper()
	return newBackupTestEnvWithClients(t, platformID, nil)
}

func newBackupTestEnvWithClients(
	t *testing.T, platformID string, pairedClients []database.Client,
) backupTestEnv {
	t.Helper()
	rootDir := t.TempDir()
	dataDir := filepath.Join(rootDir, "zaparoo")
	configDir := filepath.Join(rootDir, "config-dir")
	require.NoError(t, os.MkdirAll(dataDir, 0o750))
	require.NoError(t, os.MkdirAll(configDir, 0o750))

	cfg, err := config.NewConfig(configDir, config.BaseDefaults)
	require.NoError(t, err)

	writeTestFile(t, filepath.Join(configDir, config.CfgFile), "debug_logging = false\n")
	writeTestFile(t, filepath.Join(dataDir, "frontend.toml"), "enabled = true\n")
	writeTestFile(t, filepath.Join(dataDir, config.TUIFile), "theme = \"default\"\n")
	writeTestFile(t, filepath.Join(dataDir, config.LaunchersDir, "custom.toml"), "[[launchers]]\n")
	writeTestFile(t, filepath.Join(dataDir, config.MappingsDir, "tokens.toml"), "[[mappings]]\n")

	writeTestFile(t, filepath.Join(rootDir, "MiSTer.ini"), "video_mode=0\n")
	writeTestFile(t, filepath.Join(rootDir, "config", "core.cfg"), "setting=1\n")
	writeTestFile(t, filepath.Join(rootDir, "config", "core_recent.cfg"), "recent=1\n")
	writeTestFile(t, filepath.Join(rootDir, "config", "inputs", "pad.map"), "map\n")
	writeTestFile(t, filepath.Join(rootDir, "config", "inputs", "ignored.txt"), "ignore\n")
	writeTestFile(t, filepath.Join(rootDir, "saves", "game.sav"), "save-data\n")
	writeTestFile(t, filepath.Join(rootDir, "savestates", "game.ss"), "state-data\n")

	userSnapshot := filepath.Join(rootDir, "user-snapshot.db")
	writeTestFile(t, userSnapshot, "user-db-snapshot\n")

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return(platformID)
	mockPlatform.On("RootDirs", testifymock.Anything).Return([]string{
		rootDir,
		filepath.Join(rootDir, "usb"),
	})
	mockPlatform.On("Settings").Return(platforms.Settings{
		DataDir: dataDir, ConfigDir: configDir, TempDir: filepath.Join(rootDir, "tmp"), LogDir: rootDir,
	})
	pl := backupPlatform{MockPlatform: mockPlatform, definitions: testPlatformDefinitions(rootDir)}

	userDB := testinghelpers.NewMockUserDBI()
	userDB.On(
		"BackupForTransfer", testifymock.Anything, testifymock.AnythingOfType("string"),
	).Return(database.BackupInfo{
		Name:  "snapshot.db",
		Path:  userSnapshot,
		Valid: true,
	}, func() error { return nil }, nil)
	userDB.On("Backup", "restore-rollback", false).Return(database.BackupInfo{
		Name: "rollback.db", Path: userSnapshot, Valid: true,
	}, nil).Maybe()
	userDB.On("GetDBPath").Return(filepath.Join(dataDir, config.UserDbFile)).Maybe()
	userDB.On("RestoreBackup", testifymock.AnythingOfType("string")).Return(database.RestoreInfo{
		RestoredFrom: database.BackupInfo{Name: "staged.db", Valid: true},
	}, nil).Maybe()
	userDB.On("ListClients").Return(pairedClients, nil).Maybe()
	userDB.On("ReplaceAllClients", testifymock.Anything).Return(nil).Maybe()

	mgr := NewManager(cfg, pl, &database.Database{UserDB: userDB})
	return backupTestEnv{
		Manager:      mgr,
		UserDB:       userDB,
		RootDir:      rootDir,
		ConfigDir:    configDir,
		DataDir:      dataDir,
		UserSnapshot: userSnapshot,
	}
}

func stageTestZip(t *testing.T, zipPath string) *zipReadResult {
	t.Helper()
	staged, err := stageLocalArchive(
		context.Background(), zipPath, localStagingOptions{parent: filepath.Dir(zipPath)},
	)
	require.NoError(t, err)
	t.Cleanup(staged.cleanup)
	return staged.result
}

// collectPlatformFiles runs the source collector over platform definitions
// without a Manager, for asserting collection results in isolation.
func collectPlatformFiles(files []FileRef, definitions []platforms.BackupDefinition) []FileRef {
	collector := newSourceCollector(context.Background(), nil)
	for i := range files {
		collector.appendFile(&files[i])
	}
	for i := range definitions {
		def := &definitions[i]
		spec := collectorDefinition{
			definition:   *def,
			trustedRoots: definitionCategoryRoots(def, nil),
			archive:      platformArchive,
		}
		collector.collect(&spec)
	}
	return collector.files
}

func testPlatformDefinitions(rootDir string) []platforms.BackupDefinition {
	return []platforms.BackupDefinition{
		{
			Category:     CategorySettings,
			SourceRoot:   rootDir,
			RestoreRoot:  "",
			NonRecursive: true,
			Include: []platforms.BackupPattern{
				{Glob: "MiSTer.ini"},
			},
		},
		{
			Category:    CategorySettings,
			SourceRoot:  filepath.Join(rootDir, "config"),
			RestoreRoot: "config",
			Include:     []platforms.BackupPattern{{Glob: "*.cfg"}},
			Exclude:     []platforms.BackupPattern{{Contains: "_recent"}},
		},
		{
			Category:    CategoryInputs,
			SourceRoot:  filepath.Join(rootDir, "config", "inputs"),
			RestoreRoot: filepath.Join("config", "inputs"),
			Include:     []platforms.BackupPattern{{Glob: "*.map"}},
			Exclude:     []platforms.BackupPattern{{Glob: filepath.Join("renamed", "*")}},
		},
		{
			Category:    CategorySaves,
			SourceRoot:  filepath.Join(rootDir, "zaparoo", "profiles"),
			RestoreRoot: filepath.Join("zaparoo", "profiles"),
			Include:     []platforms.BackupPattern{{Contains: "/saves/"}},
		},
		{
			Category:    CategorySavestates,
			SourceRoot:  filepath.Join(rootDir, "zaparoo", "profiles"),
			RestoreRoot: filepath.Join("zaparoo", "profiles"),
			Include:     []platforms.BackupPattern{{Contains: "/savestates/"}},
		},
		{
			Category:    CategorySaves,
			SourceRoot:  filepath.Join(rootDir, "saves"),
			RestoreRoot: "saves",
			Include:     []platforms.BackupPattern{{All: true}},
		},
		{
			Category:    CategorySavestates,
			SourceRoot:  filepath.Join(rootDir, "savestates"),
			RestoreRoot: "savestates",
			Include:     []platforms.BackupPattern{{All: true}},
		},
	}
}

func TestManagerCreateReportsPlatformPlanWarningsAsPartial(t *testing.T) {
	t.Parallel()
	env := newBackupTestEnv(t, platformids.Mister)
	basePlatform, ok := env.Manager.pl.(backupPlatform)
	require.True(t, ok)
	env.Manager.pl = &backupPlanningTestPlatform{
		backupPlatform: basePlatform,
		plan: platforms.BackupPlan{
			Definitions: basePlatform.BackupDefinitions(),
			Warnings: []platforms.BackupWarning{{
				Category: CategorySaves, Path: "saves",
				Reason: "shared profile data hidden by active profile mount",
			}},
		},
	}

	info, err := env.Manager.Create(context.Background())
	require.NoError(t, err)
	assert.Equal(t, StatusPartial, info.Status)
	assert.Contains(t, info.Warnings, models.BackupWarning{
		Category: CategorySaves, Path: "saves",
		Reason: "shared profile data hidden by active profile mount",
	})
	status := env.Manager.Status()
	assert.Equal(t, StatusPartial, status.Local.LastStatus)
	assert.Equal(t, 1, status.Local.SkippedFiles)
}

func TestManagerCreateBackupSkipsUnreadableSource(t *testing.T) {
	t.Parallel()
	env := newBackupTestEnv(t, platformids.Mister)
	defaultOpener := env.Manager.sourceOpener
	env.Manager.sourceOpener = func(ctx context.Context, file *FileRef) (io.ReadCloser, error) {
		if file.RestorePath == filepath.ToSlash(filepath.Join("saves", "game.sav")) {
			return nil, &os.PathError{Op: "open", Path: file.RestorePath, Err: os.ErrPermission}
		}
		return defaultOpener(ctx, file)
	}

	info, err := env.Manager.Create(context.Background())
	require.NoError(t, err)
	assert.Contains(t, info.Warnings, models.BackupWarning{
		Category: CategorySaves,
		Path:     filepath.ToSlash(filepath.Join("saves", "game.sav")),
		Reason:   "source unreadable during backup",
	})
	status := env.Manager.Status()
	assert.Equal(t, StatusPartial, status.Local.LastStatus)
	assert.Equal(t, 1, status.Local.SkippedFiles)
	result := stageTestZip(t, info.Path)
	for _, file := range result.Manifest.Files {
		assert.NotEqual(t, filepath.ToSlash(filepath.Join("saves", "game.sav")), file.RestorePath)
	}
}

func TestManagerCreateBackupSkipsNonportablePathAndSelfInspects(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows cannot create a filename containing a literal backslash")
	}
	env := newBackupTestEnv(t, platformids.Mister)
	backslash := string(rune(0x5c))
	nonportablePath := filepath.Join(env.RootDir, "saves", "literal"+backslash+"name.sav")
	writeTestFile(t, nonportablePath, "nonportable")

	info, err := env.Manager.createBackup(context.Background(), false)
	require.NoError(t, err)
	require.NotEmpty(t, info.Warnings)
	assert.Contains(t, info.Warnings, models.BackupWarning{
		Category: CategorySaves,
		Path:     "saves/literal%5Cname.sav",
		Reason:   "source path is not portable",
	})
	inspected, err := inspectZipManifest(info.Path)
	require.NoError(t, err)
	assert.Equal(t, info.Warnings, inspected.Warnings)
}

func TestWriteZipRejectsInvalidWarningBeforeCreatingArchive(t *testing.T) {
	t.Parallel()
	zipPath := filepath.Join(t.TempDir(), "invalid-warning.zip")
	manifest := Manifest{
		Version: 1, Platform: platformids.Mister, CreatedAt: time.Now().UTC(),
		Categories: summarize(nil),
		Warnings: []models.BackupWarning{{
			Category: CategorySaves, Path: "saves/invalid\\name.sav", Reason: "unreadable",
		}},
	}

	err := writeZip(context.Background(), zipPath, nil, &manifest)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid warning metadata")
	_, statErr := os.Stat(zipPath)
	require.ErrorIs(t, statErr, os.ErrNotExist)
}

func TestManagerRestorePreservesDestinationDeviceID(t *testing.T) {
	t.Parallel()
	env := newBackupTestEnv(t, platformids.Mister)
	configPath := filepath.Join(env.ConfigDir, config.CfgFile)
	writeTestFile(t, configPath, "[service]\ndevice_id = \"source-device\"\napi_port = 7497\n")
	info, err := env.Manager.Create(context.Background())
	require.NoError(t, err)
	destinationID := env.Manager.cfg.DeviceID()
	require.NotEmpty(t, destinationID)
	require.NotEqual(t, "source-device", destinationID)
	writeTestFile(t, configPath, "[service]\ndevice_id = \"destination-on-disk\"\n")

	_, err = env.Manager.Restore(context.Background(), info.Name)
	require.NoError(t, err)
	// #nosec G304 -- configPath is under a test-owned temporary root.
	restoredData, err := os.ReadFile(configPath)
	require.NoError(t, err)
	restoredConfig := &config.Instance{}
	require.NoError(t, restoredConfig.LoadTOML(string(restoredData)))
	assert.Equal(t, destinationID, restoredConfig.DeviceID())
	assert.NotContains(t, string(restoredData), "source-device")
}

func TestManagerPrunesPreRestoreZipsKeepsManual(t *testing.T) {
	t.Parallel()
	env := newBackupTestEnv(t, platformids.Mister)

	manualInfo, err := env.Manager.createBackup(context.Background(), false)
	require.NoError(t, err)
	for range preRestoreZipKeep + 2 {
		_, err = env.Manager.createBackup(context.Background(), true)
		require.NoError(t, err)
	}

	backups, err := env.Manager.List()
	require.NoError(t, err)
	preRestoreCount := 0
	manualCount := 0
	for _, backup := range backups {
		if strings.HasSuffix(backup.Name, "-pre-restore.zip") {
			preRestoreCount++
		} else {
			manualCount++
		}
	}
	assert.Equal(t, preRestoreZipKeep, preRestoreCount,
		"pre-restore ZIPs beyond the keep count must be pruned")
	assert.Equal(t, 1, manualCount, "manual backups must never be pruned")
	names := make([]string, 0, len(backups))
	for _, backup := range backups {
		names = append(names, backup.Name)
	}
	assert.Contains(t, names, manualInfo.Name, "the manual backup must survive pruning")
}

func TestManagerRestorePreservesPairedClients(t *testing.T) {
	t.Parallel()
	clients := []database.Client{{
		ClientID: "c1", ClientName: "Phone", AuthToken: "tok", Role: "admin",
		PairingKey: []byte{0x01}, CreatedAt: 10, LastSeenAt: 20,
	}}
	env := newBackupTestEnvWithClients(t, platformids.Mister, clients)
	info, err := env.Manager.Create(context.Background())
	require.NoError(t, err)

	_, err = env.Manager.Restore(context.Background(), info.Name)
	require.NoError(t, err)
	env.UserDB.AssertCalled(t, "ReplaceAllClients", clients)
}

func TestManagerRestorePreservesDestinationEncryption(t *testing.T) {
	t.Parallel()
	env := newBackupTestEnv(t, platformids.Mister)
	configPath := filepath.Join(env.ConfigDir, config.CfgFile)

	// Backup made while encryption was off; destination enables it later.
	info, err := env.Manager.Create(context.Background())
	require.NoError(t, err)
	env.Manager.cfg.SetEncryptionEnabled(true)

	_, err = env.Manager.Restore(context.Background(), info.Name)
	require.NoError(t, err)
	// #nosec G304 -- configPath is under a test-owned temporary root.
	restoredData, err := os.ReadFile(configPath)
	require.NoError(t, err)
	restoredConfig := &config.Instance{}
	require.NoError(t, restoredConfig.LoadTOML(string(restoredData)))
	assert.True(t, restoredConfig.EncryptionEnabled(),
		"restore must not silently disable required encryption")
}

func TestValidateManifestPolicyRequiresExactlyOneUserDB(t *testing.T) {
	t.Parallel()
	env := newBackupTestEnv(t, platformids.Mister)
	manifest := &Manifest{Platform: platformids.Mister}

	err := env.Manager.validateManifestPolicy(manifest)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exactly one files/zaparoo/user.db payload")

	userDB := FileRef{
		ArchivePath: zaparooArchive(config.UserDbFile), RestorePath: config.UserDbFile,
		Category: CategoryZaparoo,
	}
	manifest.Files = []FileRef{userDB, userDB}
	err = env.Manager.validateManifestPolicy(manifest)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exactly one files/zaparoo/user.db payload")
}

func TestManagerCreateListRestore(t *testing.T) {
	t.Parallel()
	env := newBackupTestEnv(t, platformids.Mister)
	cfgPath := filepath.Join(env.ConfigDir, config.CfgFile)
	launcherPath := filepath.Join(env.DataDir, config.LaunchersDir, "custom.toml")
	mappingPath := filepath.Join(env.DataDir, config.MappingsDir, "tokens.toml")
	profileSavePath := filepath.Join(
		env.DataDir, "profiles", "11111111-aaaa-bbbb-cccc-000000000001", "saves", "profile.sav",
	)
	writeTestFile(t, profileSavePath, "profile-save\n")

	info, err := env.Manager.Create(context.Background())
	require.NoError(t, err)
	assert.Equal(t, IntegrityValid, info.Integrity)
	assert.Equal(t, StatusSuccess, info.Status)
	assert.Contains(t, info.Categories, CategoryZaparoo)
	assert.Contains(t, info.Categories, CategorySavestates)
	assert.Positive(t, info.Categories[CategorySavestates].Files)

	backups, err := env.Manager.List()
	require.NoError(t, err)
	require.Len(t, backups, 1)
	assert.Equal(t, info.Name, backups[0].Name)

	writeTestFile(t, filepath.Join(env.RootDir, "saves", "game.sav"), "changed\n")
	writeTestFile(t, cfgPath, "debug_logging = true\n")
	writeTestFile(t, launcherPath, "[[changed_launchers]]\n")
	writeTestFile(t, mappingPath, "[[changed_mappings]]\n")
	require.NoError(t, os.RemoveAll(filepath.Dir(filepath.Dir(profileSavePath))))
	restore, err := env.Manager.Restore(context.Background(), info.Name)
	require.NoError(t, err)
	assert.Equal(t, info.Name, restore.RestoredFrom.Name)
	require.NotNil(t, restore.PreRestoreBackup)

	restoredSave, err := os.ReadFile(filepath.Join(env.RootDir, "saves", "game.sav"))
	require.NoError(t, err)
	assert.Equal(t, "save-data\n", string(restoredSave))
	// #nosec G304 -- test reads a path created under this test's temp directory.
	restoredCfg, err := os.ReadFile(cfgPath)
	require.NoError(t, err)
	assert.Contains(t, string(restoredCfg), "debug_logging = false")
	restoredConfig := &config.Instance{}
	require.NoError(t, restoredConfig.LoadTOML(string(restoredCfg)))
	assert.Equal(t, env.Manager.cfg.DeviceID(), restoredConfig.DeviceID())
	restoredLauncher, err := os.ReadFile(launcherPath) // #nosec G304 -- path belongs to test temp dir.
	require.NoError(t, err)
	assert.Equal(t, "[[launchers]]\n", string(restoredLauncher))
	restoredMapping, err := os.ReadFile(mappingPath) // #nosec G304 -- path belongs to test temp dir.
	require.NoError(t, err)
	assert.Equal(t, "[[mappings]]\n", string(restoredMapping))
	// #nosec G304 -- profileSavePath is under a test-owned temporary root.
	restoredProfileSave, err := os.ReadFile(profileSavePath)
	require.NoError(t, err)
	assert.Equal(t, "profile-save\n", string(restoredProfileSave))
	env.UserDB.AssertCalled(t, "RestoreBackup", testifymock.AnythingOfType("string"))
	stagingEntries, err := os.ReadDir(filepath.Join(env.DataDir, "backups"))
	if !errors.Is(err, os.ErrNotExist) {
		require.NoError(t, err)
		for _, entry := range stagingEntries {
			assert.NotRegexp(t, `^backup-.*-manual\.db$`, entry.Name())
		}
	}
}

func TestZaparooRestorePolicyMirrorsCollector(t *testing.T) {
	t.Parallel()
	// Restore policy validates against the same definitions collection
	// uses: globs match case-insensitively against basenames at any depth.
	base := t.TempDir()
	defs := zaparooBackupDefinitions(filepath.Join(base, "cfg"), filepath.Join(base, "data"))
	allowed := func(restorePath string) bool {
		return allowedRestorePath(&FileRef{Category: CategoryZaparoo, RestorePath: restorePath}, defs)
	}
	assert.True(t, allowed("Config.toml"))
	assert.True(t, allowed("FRONTEND.TOML"))
	assert.True(t, allowed("Tui.toml"))
	assert.True(t, allowed("mappings/group/CARDS.TOML"))
	assert.True(t, allowed("launchers/Custom.toml"))
	assert.True(t, allowed("mappings/deep/nested/dirs/file.toml"))
	// user.db is constructed with a fixed name, never collected by glob;
	// validateManifestPolicy allows it by exact name only.
	assert.False(t, allowed("USER.DB"))
	assert.False(t, allowed("user.db"))
	assert.False(t, allowed("mappings/notes.txt"))
	assert.False(t, allowed("other/file.toml"))
	assert.False(t, allowed("auth.toml"))
}

func TestManagerCreateInspectRestoreNestedConfigFiles(t *testing.T) {
	t.Parallel()
	env := newBackupTestEnv(t, platformids.Mister)
	nestedMapping := filepath.Join(env.DataDir, config.MappingsDir, "group", "cards.toml")
	nestedLauncher := filepath.Join(env.DataDir, config.LaunchersDir, "custom", "arcade.toml")
	writeTestFile(t, nestedMapping, "[[nested_mappings]]\n")
	writeTestFile(t, nestedLauncher, "[[nested_launchers]]\n")

	info, err := env.Manager.Create(context.Background())
	require.NoError(t, err)

	_, err = env.Manager.Inspect(context.Background(), info.Name)
	require.NoError(t, err, "backups containing nested mapping/launcher files must pass policy")

	writeTestFile(t, nestedMapping, "changed\n")
	require.NoError(t, os.Remove(nestedLauncher))
	_, err = env.Manager.Restore(context.Background(), info.Name)
	require.NoError(t, err)

	restoredMapping, err := os.ReadFile(nestedMapping) // #nosec G304 -- path belongs to test temp dir.
	require.NoError(t, err)
	assert.Equal(t, "[[nested_mappings]]\n", string(restoredMapping))
	restoredLauncher, err := os.ReadFile(nestedLauncher) // #nosec G304 -- path belongs to test temp dir.
	require.NoError(t, err)
	assert.Equal(t, "[[nested_launchers]]\n", string(restoredLauncher))
}

func TestManagerCreateZaparooScopeExcludesPlatformFiles(t *testing.T) {
	t.Parallel()
	env := newBackupTestEnv(t, platformids.Mister)
	env.Manager.cfg.SetBackupScope(config.BackupScopeZaparoo)

	info, err := env.Manager.Create(context.Background())
	require.NoError(t, err)
	assert.Equal(t, StatusSuccess, info.Status)
	assert.Positive(t, info.Categories[CategoryZaparoo].Files)
	assert.Zero(t, info.Categories[CategorySettings].Files)
	assert.Zero(t, info.Categories[CategoryInputs].Files)
	assert.Zero(t, info.Categories[CategorySaves].Files)
	assert.Zero(t, info.Categories[CategorySavestates].Files)

	zipResult := stageTestZip(t, info.Path)
	for _, file := range zipResult.Manifest.Files {
		assert.Equal(t, CategoryZaparoo, file.Category, file.RestorePath)
	}
}

func TestManagerPreRestoreBackupIgnoresZaparooScope(t *testing.T) {
	t.Parallel()
	env := newBackupTestEnv(t, platformids.Mister)
	env.Manager.cfg.SetBackupScope(config.BackupScopeZaparoo)

	pre, err := env.Manager.createBackup(context.Background(), true)
	require.NoError(t, err)
	assert.Positive(t, pre.Categories[CategoryZaparoo].Files)
	assert.Positive(t, pre.Categories[CategorySaves].Files)
	assert.Positive(t, pre.Categories[CategorySavestates].Files)
}

func TestManagerCreateUnsupportedPlatformScopes(t *testing.T) {
	t.Parallel()
	rootDir := t.TempDir()
	dataDir := filepath.Join(rootDir, "zaparoo")
	configDir := filepath.Join(rootDir, "config-dir")
	require.NoError(t, os.MkdirAll(dataDir, 0o750))
	require.NoError(t, os.MkdirAll(configDir, 0o750))
	cfg, err := config.NewConfig(configDir, config.BaseDefaults)
	require.NoError(t, err)
	writeTestFile(t, filepath.Join(configDir, config.CfgFile), "debug_logging = false\n")
	userSnapshot := filepath.Join(rootDir, "user-snapshot.db")
	writeTestFile(t, userSnapshot, "user-db-snapshot\n")

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("test-unsupported")
	mockPlatform.On("Settings").Return(platforms.Settings{
		DataDir: dataDir, ConfigDir: configDir, TempDir: filepath.Join(rootDir, "tmp"), LogDir: rootDir,
	})
	userDB := testinghelpers.NewMockUserDBI()
	userDB.On(
		"BackupForTransfer", testifymock.Anything, testifymock.AnythingOfType("string"),
	).Return(database.BackupInfo{
		Name: "snapshot.db", Path: userSnapshot, Valid: true,
	}, func() error { return nil }, nil)
	mgr := NewManager(cfg, mockPlatform, &database.Database{UserDB: userDB})

	// Platform scope still requires a backup provider.
	_, err = mgr.Create(context.Background())
	require.ErrorIs(t, err, ErrPlatformBackupUnsupported)

	// Zaparoo scope backs up Core data on any platform, and pre-restore
	// collection degrades to it when no provider exists.
	assert.Equal(t, config.BackupScopeZaparoo, mgr.preRestoreScope())
	cfg.SetBackupScope(config.BackupScopeZaparoo)
	info, err := mgr.Create(context.Background())
	require.NoError(t, err)
	assert.Positive(t, info.Categories[CategoryZaparoo].Files)
}

func TestManagerTrackScheduleStale(t *testing.T) {
	t.Parallel()
	env := newBackupTestEnv(t, platformids.Mister)
	staleAfter := 7 * 24 * time.Hour
	now := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)

	// An unreliable clock never reports stale.
	unreliable := time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)
	assert.False(t, env.Manager.TrackScheduleStale(unreliable, true, staleAfter))

	// The first active observation records the anchor; staleness starts
	// only once a full window passes with no success.
	assert.False(t, env.Manager.TrackScheduleStale(now, true, staleAfter))
	assert.False(t, env.Manager.TrackScheduleStale(now.Add(6*24*time.Hour), true, staleAfter))
	assert.True(t, env.Manager.TrackScheduleStale(now.Add(7*24*time.Hour), true, staleAfter))

	// A success resets the window and survives the status merge.
	successAt := now.Add(7 * 24 * time.Hour)
	require.NoError(t, env.Manager.writeRemoteStatus(&statusEntry{
		LastRunAt:     formatTime(successAt),
		LastSuccessAt: formatTime(successAt),
		LastStatus:    StatusSuccess,
	}))
	assert.False(t, env.Manager.TrackScheduleStale(now.Add(8*24*time.Hour), true, staleAfter))
	assert.True(t, env.Manager.TrackScheduleStale(now.Add(15*24*time.Hour), true, staleAfter))

	// Disabling clears the anchor; re-enabling restarts the window even
	// though an old success is on record.
	assert.False(t, env.Manager.TrackScheduleStale(now.Add(16*24*time.Hour), false, staleAfter))
	assert.False(t, env.Manager.TrackScheduleStale(now.Add(30*24*time.Hour), true, staleAfter))
}

func TestManagerNotifyScheduleStaleAddsInboxNotice(t *testing.T) {
	t.Parallel()
	env := newBackupTestEnv(t, platformids.Mister)
	ns := make(chan models.Notification, 1)
	env.UserDB.On("AddInboxMessage", testifymock.MatchedBy(func(msg *database.InboxMessage) bool {
		return msg.Title == "Remote backup is overdue" &&
			msg.Category == inboxservice.CategoryBackupRemoteStale &&
			msg.Severity == inboxservice.SeverityWarning
	})).Return(&database.InboxMessage{
		DBID: 1, Title: "Remote backup is overdue", Category: inboxservice.CategoryBackupRemoteStale,
	}, nil).Once()
	env.Manager.WithInbox(inboxservice.NewService(env.UserDB, ns))

	env.Manager.NotifyScheduleStale()

	env.UserDB.AssertNumberOfCalls(t, "AddInboxMessage", 1)
	select {
	case notification := <-ns:
		assert.Equal(t, models.NotificationInboxAdded, notification.Method)
	default:
		t.Fatal("expected stale backup inbox notification")
	}
}

func TestManagerSendHeartbeatRefreshesAvailability(t *testing.T) {
	env := newBackupTestEnv(t, platformids.Mister)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/device/heartbeat":
			var body map[string]any
			if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&body)) {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			capabilities, ok := body["capabilities"].(map[string]any)
			if !assert.True(t, ok) {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			assert.InDelta(t, 1, capabilities["backup"], 0)
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/device/me":
			writeJSON(t, w, remoteDeviceMeResponse{ID: "device-1", Name: "Living Room", BackupActive: true})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()
	configureRemoteTestAuth(t, env.Manager, server.URL)

	require.NoError(t, env.Manager.SendHeartbeat(context.Background()))
	status := env.Manager.Status()
	assert.Equal(t, RemoteAvailabilityAvailable, status.Remote.Availability)
	require.NotNil(t, status.Remote.DeviceName)
	assert.Equal(t, "Living Room", *status.Remote.DeviceName)
}

func TestManagerRestoreHoldsExclusiveGateThroughSuccess(t *testing.T) {
	t.Parallel()
	env := newBackupTestEnv(t, platformids.Mister)
	info, err := env.Manager.Create(context.Background())
	require.NoError(t, err)

	gateHeld := false
	finished := false
	env.Manager.WithRestoreGate(func() (func(bool), error) {
		gateHeld = true
		return func(success bool) {
			assert.True(t, success)
			finished = true
			gateHeld = false
		}, nil
	}).WithActiveMedia(func() *models.ActiveMedia {
		assert.True(t, gateHeld)
		return nil
	})

	_, err = env.Manager.Restore(context.Background(), info.Name)
	require.NoError(t, err)
	assert.True(t, finished)
	assert.False(t, gateHeld)
}

func TestManagerRestorePreparesAndFinishesPlatformProfileData(t *testing.T) {
	t.Parallel()
	env := newBackupTestEnv(t, platformids.Mister)
	info, err := env.Manager.Create(context.Background())
	require.NoError(t, err)
	basePlatform, ok := env.Manager.pl.(backupPlatform)
	require.True(t, ok)
	prepared := false
	finished := false
	env.Manager.pl = &backupRestorePreparingPlatform{
		backupPlatform: basePlatform, prepared: &prepared, finished: &finished,
	}

	_, err = env.Manager.Restore(context.Background(), info.Name)
	require.NoError(t, err)
	assert.True(t, prepared)
	assert.True(t, finished)
}

func TestManagerRestoreSucceedsWhenCommittedCleanupSyncFails(t *testing.T) {
	t.Parallel()
	env := newBackupTestEnv(t, platformids.Mister)
	info, err := env.Manager.Create(context.Background())
	require.NoError(t, err)
	writeTestFile(t, filepath.Join(env.RootDir, "saves", "game.sav"), "changed\n")
	transactionPath := env.Manager.restoreTransactionPath()
	transactionParent := filepath.Dir(transactionPath)
	cleanupSyncFailed := false
	env.Manager.directorySync = func(path string) error {
		if filepath.Clean(path) == filepath.Clean(transactionParent) && !cleanupSyncFailed {
			_, statErr := os.Lstat(transactionPath)
			if errors.Is(statErr, os.ErrNotExist) {
				cleanupSyncFailed = true
				return errors.New("injected committed cleanup sync failure")
			}
		}
		return syncDirectory(path)
	}
	gateSucceeded := false
	env.Manager.WithRestoreGate(func() (func(bool), error) {
		return func(success bool) { gateSucceeded = success }, nil
	})

	_, err = env.Manager.Restore(context.Background(), info.Name)
	require.NoError(t, err)
	assert.True(t, cleanupSyncFailed)
	assert.True(t, gateSucceeded, "durably committed restore must still request restart")
	// #nosec G304 -- restored path is under a test-owned temporary root.
	restored, err := os.ReadFile(filepath.Join(env.RootDir, "saves", "game.sav"))
	require.NoError(t, err)
	assert.Equal(t, "save-data\n", string(restored))
}

func TestManagerCreateFollowsTrustedSymlinksAndReportsUnsafeEntries(t *testing.T) {
	t.Parallel()
	env := newBackupTestEnv(t, platformids.Mister)

	usbSaves := filepath.Join(env.RootDir, "usb", "saves")
	writeTestFile(t, filepath.Join(usbSaves, "external.sav"), "external-save\n")
	require.NoError(t, os.Symlink(usbSaves, filepath.Join(env.RootDir, "saves", "external")))
	require.NoError(t, os.Symlink(
		filepath.Join(env.RootDir, "saves", "missing.sav"),
		filepath.Join(env.RootDir, "saves", "broken.sav"),
	))
	require.NoError(t, os.Symlink(
		filepath.Join(env.RootDir, "saves"),
		filepath.Join(env.RootDir, "saves", "loop"),
	))
	authPath := filepath.Join(env.DataDir, config.AuthFile)
	writeTestFile(t, authPath, "secret-bearer\n")
	require.NoError(t, os.Symlink(authPath, filepath.Join(env.RootDir, "saves", "leak.sav")))

	info, err := env.Manager.Create(context.Background())
	require.NoError(t, err)
	assert.Equal(t, StatusPartial, info.Status)
	assert.Contains(t, info.Warnings, models.BackupWarning{
		Category: CategorySaves, Path: "saves/broken.sav", Reason: "broken symlink",
	})
	assert.Contains(t, info.Warnings, models.BackupWarning{
		Category: CategorySaves, Path: "saves/loop", Reason: "symlink cycle",
	})
	assert.Contains(t, info.Warnings, models.BackupWarning{
		Category: CategorySaves, Path: "saves/leak.sav", Reason: "symlink target outside trusted roots",
	})

	zr, err := zip.OpenReader(info.Path)
	require.NoError(t, err)
	entries := zipEntriesByName(zr.File)
	external, ok := entries[platformArchive(filepath.Join("saves", "external", "external.sav"))]
	require.True(t, ok)
	payload, err := readZipEntryLimited(external, 1024)
	require.NoError(t, err)
	assert.Equal(t, "external-save\n", string(payload))
	_, leaked := entries[platformArchive(filepath.Join("saves", "leak.sav"))]
	assert.False(t, leaked)
	require.NoError(t, zr.Close())

	status := env.Manager.Status()
	assert.Equal(t, StatusPartial, status.Local.LastStatus)
	assert.Equal(t, len(info.Warnings), status.Local.SkippedFiles)
}

func TestManagerCreateAllowsApprovedCategoryRootSymlink(t *testing.T) {
	t.Parallel()
	env := newBackupTestEnv(t, platformids.Mister)

	require.NoError(t, os.RemoveAll(filepath.Join(env.RootDir, "saves")))
	usbSaves := filepath.Join(env.RootDir, "usb", "saves")
	writeTestFile(t, filepath.Join(usbSaves, "external.sav"), "external-save\n")
	require.NoError(t, os.Symlink(usbSaves, filepath.Join(env.RootDir, "saves")))

	info, err := env.Manager.Create(context.Background())
	require.NoError(t, err)
	zr, err := zip.OpenReader(info.Path)
	require.NoError(t, err)
	defer func() { require.NoError(t, zr.Close()) }()
	entries := zipEntriesByName(zr.File)
	assert.Contains(t, entries, platformArchive(filepath.Join("saves", "external.sav")))
}

func TestManagerCreateRejectsCategoryRootSymlinkToZaparooData(t *testing.T) {
	t.Parallel()
	env := newBackupTestEnv(t, platformids.Mister)

	require.NoError(t, os.RemoveAll(filepath.Join(env.RootDir, "saves")))
	authPath := filepath.Join(env.ConfigDir, config.AuthFile)
	writeTestFile(t, authPath, "secret-bearer\n")
	require.NoError(t, os.Symlink(env.ConfigDir, filepath.Join(env.RootDir, "saves")))

	info, err := env.Manager.Create(context.Background())
	require.NoError(t, err)
	assert.Contains(t, info.Warnings, models.BackupWarning{
		Category: CategorySaves, Path: "saves", Reason: "symlink target outside trusted roots",
	})

	zr, err := zip.OpenReader(info.Path)
	require.NoError(t, err)
	defer func() { require.NoError(t, zr.Close()) }()
	entries := zipEntriesByName(zr.File)
	assert.NotContains(t, entries, platformArchive(filepath.Join("saves", config.AuthFile)))
}

func TestManagerCreateExcludesHardLinkToAuthConfig(t *testing.T) {
	t.Parallel()
	env := newBackupTestEnv(t, platformids.Mister)
	authPath := filepath.Join(env.ConfigDir, config.AuthFile)
	writeTestFile(t, authPath, "secret-bearer\n")
	leakPath := filepath.Join(env.DataDir, config.MappingsDir, "leak.toml")
	if err := os.Link(authPath, leakPath); err != nil {
		t.Skipf("hard links unavailable on test filesystem: %v", err)
	}

	info, err := env.Manager.Create(context.Background())
	require.NoError(t, err)
	assert.Contains(t, info.Warnings, models.BackupWarning{
		Category: CategoryZaparoo,
		Path:     filepath.ToSlash(filepath.Join(config.MappingsDir, "leak.toml")),
		Reason:   "sensitive source excluded",
	})
	zr, err := zip.OpenReader(info.Path)
	require.NoError(t, err)
	defer func() { require.NoError(t, zr.Close()) }()
	assert.NotContains(t, zipEntriesByName(zr.File), zaparooArchive(filepath.Join(config.MappingsDir, "leak.toml")))
}

func TestOpenSourceRejectsSensitiveFileIdentity(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	authPath := filepath.Join(root, config.AuthFile)
	writeTestFile(t, authPath, "secret-bearer\n")
	info, err := os.Stat(authPath)
	require.NoError(t, err)
	file := FileRef{
		sourceIdentity: &sourceIdentity{info: info, excludedIdentities: []os.FileInfo{info}},
		SourceRoot:     root, SourceRel: config.AuthFile, RestorePath: "mappings/leak.toml",
	}

	opened, err := openSource(&file)
	require.ErrorIs(t, err, errSensitiveSource)
	assert.Nil(t, opened)
}

func TestOpenSourceRejectsCollectedFileReplacedBySensitiveSymlink(t *testing.T) {
	t.Parallel()
	env := newBackupTestEnv(t, platformids.Mister)

	collection, err := env.Manager.collectFiles(context.Background(), "identity-test", config.BackupScopePlatform)
	require.NoError(t, err)
	defer func() { require.NoError(t, collection.Cleanup()) }()

	var collected *FileRef
	for i := range collection.Files {
		if collection.Files[i].Category == CategoryZaparoo &&
			collection.Files[i].RestorePath == config.CfgFile {
			collected = &collection.Files[i]
			break
		}
	}
	require.NotNil(t, collected)

	authPath := filepath.Join(env.ConfigDir, config.AuthFile)
	writeTestFile(t, authPath, "secret-bearer\n")
	require.NoError(t, os.Remove(filepath.Join(env.ConfigDir, config.CfgFile)))
	require.NoError(t, os.Symlink(config.AuthFile, filepath.Join(env.ConfigDir, config.CfgFile)))

	opened, err := openSource(collected)
	require.ErrorIs(t, err, errSourceIdentityChanged)
	assert.Nil(t, opened)
	_, warnings, err := prepareSourceFiles(
		context.Background(), []FileRef{*collected}, openSourceContext, nil,
	)
	require.ErrorIs(t, err, errSourceIdentityChanged)
	assert.Empty(t, warnings, "identity changes must fail rather than become unreadable-file warnings")
}

func TestInspectUsesManifestOnlyButRestoreVerifiesPayloadHash(t *testing.T) {
	t.Parallel()
	env := newBackupTestEnv(t, platformids.Mister)

	info, err := env.Manager.Create(context.Background())
	require.NoError(t, err)
	corruptZipPayload(t, info.Path, platformArchive(filepath.Join("saves", "game.sav")), "bad!-data\n")

	backups, err := env.Manager.List()
	require.NoError(t, err)
	require.Len(t, backups, 1)
	assert.Equal(t, info.Name, backups[0].Name)
	assert.NotZero(t, backups[0].Size)

	inspected, err := env.Manager.Inspect(context.Background(), info.Name)
	require.NoError(t, err)
	assert.Equal(t, IntegrityUnchecked, inspected.Integrity)
	assert.Contains(t, inspected.Categories, CategorySavestates)

	_, err = env.Manager.Restore(context.Background(), info.Name)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "hash mismatch")
}

func TestManagerRestoreRejectsWrongPlatformBeforePayloadVerification(t *testing.T) {
	t.Parallel()
	env := newBackupTestEnv(t, platformids.Mister)
	info, err := env.Manager.Create(context.Background())
	require.NoError(t, err)

	zr, err := zip.OpenReader(info.Path)
	require.NoError(t, err)
	entries := zipEntriesByName(zr.File)
	manifestBody, err := readZipEntryLimited(entries[manifestName], maxManifestBytes)
	require.NoError(t, err)
	require.NoError(t, zr.Close())
	var manifest Manifest
	require.NoError(t, json.Unmarshal(manifestBody, &manifest))
	manifest.Platform = "other-platform"
	manifestBody, err = json.Marshal(&manifest)
	require.NoError(t, err)
	corruptZipPayload(t, info.Path, manifestName, string(manifestBody))
	inspected, err := env.Manager.Inspect(context.Background(), info.Name)
	require.Error(t, err)
	assert.Empty(t, inspected, "invalid manifest must not return partially trusted metadata")
	corruptZipPayload(
		t, info.Path, platformArchive(filepath.Join("saves", "game.sav")), "bad!-data\n",
	)

	_, err = env.Manager.Restore(context.Background(), info.Name)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not match")
	assert.NotContains(t, err.Error(), "hash mismatch")
}

func TestSourceCollectorAcceptsLargeLogicalTotal(t *testing.T) {
	t.Parallel()
	collector := newSourceCollector(context.Background(), nil)
	files := []FileRef{
		{Category: CategoryZaparoo, ArchivePath: "files/zaparoo/one", RestorePath: "one", Size: 4 << 30},
		{Category: CategoryZaparoo, ArchivePath: "files/zaparoo/two", RestorePath: "two", Size: 3 << 30},
	}
	for i := range files {
		collector.appendFile(&files[i])
	}

	require.NoError(t, collector.err)
	assert.Len(t, collector.files, 2)
	assert.Equal(t, int64(7<<30), collector.logicalSize)
}

func TestSourceCollectorRejectsLogicalSizeOverflow(t *testing.T) {
	t.Parallel()
	collector := newSourceCollector(context.Background(), nil)
	first := FileRef{
		Category: CategoryZaparoo, ArchivePath: "files/zaparoo/one", RestorePath: "one", Size: math.MaxInt64,
	}
	second := FileRef{Category: CategoryZaparoo, ArchivePath: "files/zaparoo/two", RestorePath: "two", Size: 1}

	collector.appendFile(&first)
	collector.appendFile(&second)

	require.Error(t, collector.err)
	assert.Contains(t, collector.err.Error(), "overflow")
	assert.Len(t, collector.files, 1)
}

func TestValidateZipHeadersRejectsArchiveLimits(t *testing.T) {
	t.Parallel()

	tooMany := make([]*zip.File, maxArchiveEntries+1)
	_, err := validateZipHeaders(tooMany)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "too many entries")

	longPath := &zip.File{FileHeader: zip.FileHeader{Name: strings.Repeat("a", maxArchivePathLen+1)}}
	_, err = validateZipHeaders([]*zip.File{longPath})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "path exceeds")

	first := &zip.File{FileHeader: zip.FileHeader{Name: "files/zaparoo/user.db"}}
	second := &zip.File{FileHeader: zip.FileHeader{Name: "files/zaparoo/user.db"}}
	_, err = validateZipHeaders([]*zip.File{first, second})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate ZIP entry")
}

func TestValidateZipHeadersAcceptsLargeLogicalTotal(t *testing.T) {
	t.Parallel()
	files := []*zip.File{
		{FileHeader: zip.FileHeader{Name: "files/zaparoo/one", UncompressedSize64: 4 << 30}},
		{FileHeader: zip.FileHeader{Name: "files/zaparoo/two", UncompressedSize64: 3 << 30}},
	}

	_, err := validateZipHeaders(files)
	require.NoError(t, err)

	overflow := []*zip.File{
		{FileHeader: zip.FileHeader{Name: "files/zaparoo/one", UncompressedSize64: math.MaxInt64}},
		{FileHeader: zip.FileHeader{Name: "files/zaparoo/two", UncompressedSize64: 1}},
	}
	_, err = validateZipHeaders(overflow)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "overflow")
}

func TestLogicalSizeAcceptsLargeTotalAndRejectsInvalidMetadata(t *testing.T) {
	t.Parallel()
	total, err := sumLogicalSize([]FileRef{{Size: 4 << 30}, {Size: 3 << 30}})
	require.NoError(t, err)
	assert.Equal(t, int64(7<<30), total)

	_, err = sumLogicalSize([]FileRef{{Size: -1}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "negative size")

	_, err = sumLogicalSize([]FileRef{{Size: math.MaxInt64}, {Size: 1}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "overflow")
}

func TestRemoteTransferTimeoutSaturates(t *testing.T) {
	t.Parallel()
	assert.Equal(t, remoteRequestTimeout, remoteTransferTimeout(0))
	assert.Equal(t, time.Duration(math.MaxInt64), remoteTransferTimeout(math.MaxInt64))
}

func TestStageLocalArchiveHonorsCancellationAndCleansUp(t *testing.T) {
	t.Parallel()
	env := newBackupTestEnv(t, platformids.Mister)
	info, err := env.Manager.Create(context.Background())
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = stageLocalArchive(ctx, info.Path, localStagingOptions{
		parent: env.Manager.backupDir(), validatePolicy: env.Manager.validateManifestPolicy,
	})
	require.ErrorIs(t, err, context.Canceled)
	stagingDirs, err := filepath.Glob(filepath.Join(env.Manager.backupDir(), "local-restore-*"))
	require.NoError(t, err)
	assert.Empty(t, stagingDirs)
}

func TestReadAndVerifyZipRejectsUnsafeEntry(t *testing.T) {
	t.Parallel()
	zipPath := filepath.Join(t.TempDir(), "backup-20260624-150405-000000000-manual.zip")
	writeRawZip(t, zipPath, map[string]string{
		manifestName:                "{\"version\":1,\"files\":[]}",
		path.Join("..", "evil.txt"): "bad",
	})

	_, err := stageLocalArchive(
		context.Background(), zipPath, localStagingOptions{parent: filepath.Dir(zipPath)},
	)
	require.Error(t, err)
}

func TestValidateFilesRejectsUnsafeAndDuplicatePaths(t *testing.T) {
	t.Parallel()

	require.Error(t, validateFiles([]FileRef{{ArchivePath: path.Join("..", "evil"), RestorePath: "safe"}}))
	require.Error(t, validateFiles([]FileRef{{ArchivePath: "safe", RestorePath: path.Join("..", "evil")}}))
	require.Error(t, validateFiles([]FileRef{
		{ArchivePath: path.Join(filesRoot, zaparooRoot, "one"), RestorePath: "one"},
		{ArchivePath: path.Join(filesRoot, zaparooRoot, "one"), RestorePath: "two"},
	}))
	require.Error(t, validateFiles([]FileRef{
		{ArchivePath: path.Join(filesRoot, zaparooRoot, "one"), RestorePath: "same"},
		{ArchivePath: path.Join(filesRoot, zaparooRoot, "two"), RestorePath: "same"},
	}))
}

func TestRemoteSourceProcessingHonorsCancellation(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	filePath := filepath.Join(root, "save.dat")
	writeTestFile(t, filePath, "save")
	info, err := os.Stat(filePath)
	require.NoError(t, err)
	file := FileRef{
		sourceIdentity: &sourceIdentity{info: info},
		SourceRoot:     root, SourceRel: "save.dat", Size: info.Size(), SHA256: strings.Repeat("0", 64),
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, warnings, err := prepareSourceFiles(ctx, []FileRef{file}, openSourceContext, nil)
	require.ErrorIs(t, err, context.Canceled)
	assert.Empty(t, warnings)
	_, err = hashRemoteFiles(ctx, []FileRef{file}, nil)
	require.ErrorIs(t, err, context.Canceled)
	_, _, err = buildRemotePack(ctx, []FileRef{file}, nil)
	require.ErrorIs(t, err, context.Canceled)
}

func TestSourceCollectorEnforcesFileBudgetDuringTraversal(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "one.sav"), "one")
	writeTestFile(t, filepath.Join(root, "two.sav"), "two")
	collector := newSourceCollector(context.Background(), nil)
	collector.maxFiles = 1
	spec := collectorDefinition{
		definition: platforms.BackupDefinition{
			SourceRoot: root, Category: CategorySaves,
			Include: []platforms.BackupPattern{{All: true}},
		},
		trustedRoots: canonicalCategoryRoots([]string{root}, ""),
		archive:      platformArchive,
	}

	collector.collect(&spec)
	require.Error(t, collector.err)
	assert.Contains(t, collector.err.Error(), "too many files")
	assert.Len(t, collector.files, 1)
}

func TestSourceCollectorStopsWhenContextCanceled(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	collector := newSourceCollector(ctx, nil)
	collector.collect(&collectorDefinition{})
	require.ErrorIs(t, collector.err, context.Canceled)
	assert.Empty(t, collector.files)
}

func TestCancelableSourceClosesBlockedReader(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	blocked := &blockingReadCloser{closed: make(chan struct{})}
	source := newCancelableSource(ctx, blocked)
	readDone := make(chan error, 1)
	go func() {
		_, err := source.Read(make([]byte, 1))
		readDone <- err
	}()

	cancel()
	select {
	case err := <-readDone:
		require.Error(t, err)
	case <-time.After(time.Second):
		t.Fatal("cancel did not close blocked backup source")
	}
	require.NoError(t, source.Close())
}

func TestCollectPlatformFilesIncludesDefinitionsAndSavestates(t *testing.T) {
	t.Parallel()
	env := newBackupTestEnv(t, platformids.Mister)
	writeTestFile(t, filepath.Join(env.RootDir, "config", "nested", "video.cfg"), "nested cfg\n")
	writeTestFile(t, filepath.Join(env.RootDir, "config", "inputs", "nested", "arcade.map"), "nested map\n")
	writeTestFile(t, filepath.Join(env.RootDir, "config", "inputs", "renamed", "old.map"), "excluded\n")

	files := collectPlatformFiles(nil, testPlatformDefinitions(env.RootDir))
	byArchive := make(map[string]FileRef, len(files))
	for _, file := range files {
		byArchive[file.ArchivePath] = file
	}

	assert.Equal(t, CategorySettings, byArchive[platformArchive("MiSTer.ini")].Category)
	assert.Equal(t, CategorySettings, byArchive[platformArchive(filepath.Join("config", "core.cfg"))].Category)
	assert.Equal(t, CategoryInputs, byArchive[platformArchive(filepath.Join("config", "inputs", "pad.map"))].Category)
	assert.Equal(t, CategorySettings,
		byArchive[platformArchive(filepath.Join("config", "nested", "video.cfg"))].Category)
	assert.Equal(t, CategoryInputs,
		byArchive[platformArchive(filepath.Join("config", "inputs", "nested", "arcade.map"))].Category)
	assert.Equal(t, CategorySaves, byArchive[platformArchive(filepath.Join("saves", "game.sav"))].Category)
	assert.Equal(t, CategorySavestates, byArchive[platformArchive(filepath.Join("savestates", "game.ss"))].Category)
	assert.NotContains(t, byArchive, platformArchive(filepath.Join("config", "core_recent.cfg")))
	assert.NotContains(t, byArchive, platformArchive(filepath.Join("config", "inputs", "ignored.txt")))
	assert.NotContains(t, byArchive, platformArchive(filepath.Join("config", "inputs", "renamed", "old.map")))
}

func TestSourceCollectorMatchesBasenameGlobThroughTrustedDirectorySymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation may require elevated Windows privileges")
	}
	t.Parallel()
	root := t.TempDir()
	target := filepath.Join(root, "approved-inputs")
	writeTestFile(t, filepath.Join(target, "nested", "pad.map"), "nested map\n")
	link := filepath.Join(root, "input-link")
	require.NoError(t, os.Symlink(target, link))
	collector := newSourceCollector(context.Background(), nil)
	collector.collect(&collectorDefinition{
		definition: platforms.BackupDefinition{
			Category: CategoryInputs, SourceRoot: link, RestoreRoot: filepath.Join("config", "inputs"),
			Include: []platforms.BackupPattern{{Glob: "*.map"}},
		},
		trustedRoots: canonicalCategoryRoots([]string{root}, "approved-inputs"),
		archive:      platformArchive,
	})

	require.NoError(t, collector.err)
	require.Len(t, collector.files, 1)
	assert.Equal(t, platformArchive(filepath.Join("config", "inputs", "nested", "pad.map")),
		collector.files[0].ArchivePath)
}

func TestSourceCollectorRejectsDirectorySymlinkIntoExcludedPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation may require elevated Windows privileges")
	}
	t.Parallel()
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "renamed", "pad.map"), "excluded map\n")
	require.NoError(t, os.Symlink(filepath.Join(root, "renamed"), filepath.Join(root, "alias")))
	collector := newSourceCollector(context.Background(), nil)
	collector.collect(&collectorDefinition{
		definition: platforms.BackupDefinition{
			Category: CategoryInputs, SourceRoot: root, RestoreRoot: filepath.Join("config", "inputs"),
			Include: []platforms.BackupPattern{{Glob: "*.map"}},
			Exclude: []platforms.BackupPattern{{Glob: filepath.Join("renamed", "*")}},
		},
		trustedRoots: canonicalCategoryRoots([]string{root}, ""),
		archive:      platformArchive,
	})

	require.NoError(t, collector.err)
	assert.Empty(t, collector.files)
	assert.Contains(t, collector.warnings, models.BackupWarning{
		Category: CategoryInputs,
		Path:     filepath.ToSlash(filepath.Join("config", "inputs", "alias", "pad.map")),
		Reason:   "source outside category policy",
	})
}

func TestSourceCollectorEnforcesNonRecursivePhysicalSymlinkPolicy(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation may require elevated Windows privileges")
	}
	t.Parallel()
	root := t.TempDir()
	target := filepath.Join(root, "nested", "MiSTer.ini")
	writeTestFile(t, target, "nested ini\n")
	require.NoError(t, os.Symlink(target, filepath.Join(root, "MiSTer.ini")))
	collector := newSourceCollector(context.Background(), nil)
	collector.collect(&collectorDefinition{
		definition: platforms.BackupDefinition{
			Category: CategorySettings, SourceRoot: root, NonRecursive: true,
			Include: []platforms.BackupPattern{{Glob: "MiSTer.ini"}},
		},
		trustedRoots: canonicalCategoryRoots([]string{root}, ""),
		archive:      platformArchive,
	})

	require.NoError(t, collector.err)
	assert.Empty(t, collector.files)
	assert.Contains(t, collector.warnings, models.BackupWarning{
		Category: CategorySettings, Path: "MiSTer.ini", Reason: "symlink target outside category policy",
	})
}

func TestNonRecursiveBackupDefinitionDoesNotDescend(t *testing.T) {
	t.Parallel()
	rootDir := t.TempDir()
	writeTestFile(t, filepath.Join(rootDir, "MiSTer.ini"), "root ini\n")
	writeTestFile(t, filepath.Join(rootDir, "nested", "MiSTer.ini"), "nested ini\n")
	writeTestFile(t, filepath.Join(rootDir, "games", "huge.rom"), "rom\n")

	files := collectPlatformFiles(nil, []platforms.BackupDefinition{{
		Category:     CategorySettings,
		SourceRoot:   rootDir,
		NonRecursive: true,
		Include:      []platforms.BackupPattern{{Glob: "MiSTer.ini"}},
	}})

	require.Len(t, files, 1)
	assert.Equal(t, platformArchive("MiSTer.ini"), files[0].ArchivePath)
	assert.Equal(t, "MiSTer.ini", files[0].RestorePath)
}

func TestManagerUsesConfiguredLocalDir(t *testing.T) {
	t.Parallel()
	env := newBackupTestEnv(t, platformids.Mister)
	localDir := filepath.Join(env.RootDir, "usb-backups")
	env.Manager.cfg.SetBackupLocalDir(localDir)

	info, err := env.Manager.Create(context.Background())
	require.NoError(t, err)
	assert.Equal(t, localDir, filepath.Dir(info.Path))
	assert.FileExists(t, filepath.Join(localDir, info.Name))
}

func TestApplyRestoreUsesPlatformRestoreRootProvider(t *testing.T) {
	t.Parallel()
	rootDir := t.TempDir()
	dataDir := filepath.Join(rootDir, "data", "zaparoo")
	configDir := filepath.Join(rootDir, "config")
	platformRoot := filepath.Join(rootDir, "platform-root")
	restorePath := filepath.Join("saves", "game.sav")
	require.NoError(t, os.MkdirAll(dataDir, 0o750))
	require.NoError(t, os.MkdirAll(configDir, 0o750))

	cfg, err := config.NewConfig(configDir, config.BaseDefaults)
	require.NoError(t, err)
	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return(platformids.Mister)
	mockPlatform.On("RootDirs", testifymock.Anything).Return([]string{platformRoot})
	mockPlatform.On("Settings").Return(platforms.Settings{
		DataDir: dataDir, ConfigDir: configDir, TempDir: filepath.Join(rootDir, "tmp"), LogDir: rootDir,
	})
	pl := backupRestoreRootPlatform{
		backupPlatform: backupPlatform{
			MockPlatform: mockPlatform,
			definitions: []platforms.BackupDefinition{{
				Category: CategorySaves, SourceRoot: filepath.Join(platformRoot, "saves"),
				RestoreRoot: "saves", Include: []platforms.BackupPattern{{All: true}},
			}},
		},
		restoreRoot: platformRoot,
	}
	mgr := NewManager(cfg, pl, nil)
	payload := []byte("save-data\n")
	manifest := &Manifest{Files: []FileRef{{
		ArchivePath: platformArchive(restorePath),
		RestorePath: filepath.ToSlash(restorePath),
		Category:    CategorySaves,
		SHA256:      sha256Hex(payload),
		Size:        int64(len(payload)),
	}}}

	err = mgr.applyRestore(context.Background(), manifest, func(FileRef) (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(payload)), nil
	})
	require.NoError(t, err)
	assert.FileExists(t, filepath.Join(platformRoot, restorePath))
	assert.NoFileExists(t, filepath.Join(filepath.Dir(dataDir), restorePath))
}

func TestConservativeRestoreSpaceRequirement(t *testing.T) {
	t.Parallel()

	required, err := conservativeRestoreSpaceRequirement(100, 80, 20)
	require.NoError(t, err)
	assert.Equal(t, int64(220), required)

	_, err = conservativeRestoreSpaceRequirement(math.MaxInt64, 1, 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "overflow")
}

func TestPreflightConservativeRestoreSpaceChecksEveryRoot(t *testing.T) {
	t.Parallel()

	first := filepath.Join(t.TempDir(), "first")
	second := filepath.Join(t.TempDir(), "second")
	paths := map[string]struct{}{first: {}, second: {}}
	const required = int64(1_000)
	checked := make(map[string]bool)
	err := preflightConservativeRestoreSpace(paths, required, func(path string) (uint64, error) {
		checked[path] = true
		if path == second {
			return uint64(required - 1), nil
		}
		return uint64(required), nil
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), second)
	assert.Contains(t, err.Error(), "conservative restore preflight")
	assert.True(t, checked[first])
	assert.True(t, checked[second])
}

func TestApplyRestoreStopsBeforeMutationWhenDurabilitySyncFails(t *testing.T) {
	t.Parallel()

	tests := []struct {
		failPath func(*backupTestEnv) string
		name     string
	}{
		{
			name: "transaction parent",
			failPath: func(env *backupTestEnv) string {
				return filepath.Dir(env.Manager.restoreTransactionPath())
			},
		},
		{
			name: "rollback directory",
			failPath: func(env *backupTestEnv) string {
				return filepath.Join(env.Manager.restoreTransactionPath(), restoreRollbackDir)
			},
		},
		{
			name: "journal directory",
			failPath: func(env *backupTestEnv) string {
				return env.Manager.restoreTransactionPath()
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			env := newBackupTestEnv(t, platformids.Mister)
			targetPath := filepath.Join(env.RootDir, "saves", "game.sav")
			payload := []byte("new-save\n")
			manifest := &Manifest{Files: []FileRef{{
				ArchivePath: platformArchive(filepath.Join("saves", "game.sav")),
				RestorePath: "saves/game.sav", Category: CategorySaves,
				SHA256: sha256Hex(payload), Size: int64(len(payload)),
			}}}
			failPath := filepath.Clean(tt.failPath(&env))
			env.Manager.directorySync = func(path string) error {
				if filepath.Clean(path) == failPath {
					return errors.New("injected directory sync failure")
				}
				return syncDirectory(path)
			}

			err := env.Manager.applyRestore(context.Background(), manifest, func(FileRef) (io.ReadCloser, error) {
				return io.NopCloser(bytes.NewReader(payload)), nil
			})
			require.Error(t, err)
			assert.Contains(t, err.Error(), "sync")
			// #nosec G304 -- targetPath is under a test-owned temporary root.
			stored, readErr := os.ReadFile(targetPath)
			require.NoError(t, readErr)
			assert.Equal(t, "save-data\n", string(stored))
			assert.NoDirExists(t, env.Manager.restoreTransactionPath())
		})
	}
}

func TestApplyRestoreRollsBackWhenTargetDirectorySyncFails(t *testing.T) {
	t.Parallel()
	env := newBackupTestEnv(t, platformids.Mister)
	targetPath := filepath.Join(env.RootDir, "saves", "game.sav")
	payload := []byte("new-save\n")
	manifest := &Manifest{Files: []FileRef{{
		ArchivePath: platformArchive(filepath.Join("saves", "game.sav")),
		RestorePath: "saves/game.sav", Category: CategorySaves,
		SHA256: sha256Hex(payload), Size: int64(len(payload)),
	}}}

	physicalTargetPath, err := resolvePhysicalRestorePath(targetPath)
	require.NoError(t, err)
	failPath := filepath.Dir(physicalTargetPath)
	failed := false
	env.Manager.directorySync = func(path string) error {
		if filepath.Clean(path) == filepath.Clean(failPath) && !failed {
			failed = true
			return errors.New("injected target directory sync failure")
		}
		return syncDirectory(path)
	}

	err = env.Manager.applyRestore(context.Background(), manifest, func(FileRef) (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(payload)), nil
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "syncing restored payload directory")
	// #nosec G304 -- targetPath is under a test-owned temporary root.
	stored, readErr := os.ReadFile(targetPath)
	require.NoError(t, readErr)
	assert.Equal(t, "save-data\n", string(stored))
	assert.NoDirExists(t, env.Manager.restoreTransactionPath())
}

func TestRestoreJournalPersistsImmutablePlanAndCompactStateLog(t *testing.T) {
	t.Parallel()
	env := newBackupTestEnv(t, platformids.Mister)
	manifest := &Manifest{}
	for i := range 3 {
		restorePath := filepath.ToSlash(filepath.Join("saves", fmt.Sprintf("journal-%d.sav", i)))
		writeTestFile(t, filepath.Join(env.RootDir, filepath.FromSlash(restorePath)), "old-save\n")
		payload := []byte(fmt.Sprintf("new-save-%d\n", i))
		manifest.Files = append(manifest.Files, FileRef{
			ArchivePath: platformArchive(filepath.FromSlash(restorePath)),
			RestorePath: restorePath,
			Category:    CategorySaves,
			SHA256:      sha256Hex(payload),
			Size:        int64(len(payload)),
		})
	}
	journal, err := env.Manager.prepareRestoreJournal(context.Background(), manifest)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, env.Manager.removeRestoreTransaction(env.Manager.restoreTransactionPath()))
	}()
	planPath := filepath.Join(env.Manager.restoreTransactionPath(), restoreJournalPlanName)
	// #nosec G304 -- planPath is fixed under a test-owned temporary root.
	originalPlan, err := os.ReadFile(planPath)
	require.NoError(t, err)

	require.NoError(t, env.Manager.persistRestorePhase(&journal, restorePhaseApplying))
	for i := range journal.Entries {
		require.NoError(t, env.Manager.persistRestoreEntryState(&journal, i, restoreEntryStarted))
		require.NoError(t, env.Manager.persistRestoreEntryState(&journal, i, restoreEntryApplied))
	}
	persisted, err := env.Manager.readRestoreJournal()
	require.NoError(t, err)
	assert.Equal(t, restorePhaseApplying, persisted.Phase)
	for i := range persisted.Entries {
		assert.Equal(t, restoreEntryApplied, persisted.Entries[i].State)
	}
	// #nosec G304 -- planPath is fixed under a test-owned temporary root.
	currentPlan, err := os.ReadFile(planPath)
	require.NoError(t, err)
	assert.Equal(t, originalPlan, currentPlan, "entry transitions must not rewrite immutable plan")
	stateInfo, err := os.Stat(filepath.Join(env.Manager.restoreTransactionPath(), restoreJournalStateName))
	require.NoError(t, err)
	assert.LessOrEqual(t, stateInfo.Size(), int64((2*len(journal.Entries)+1)*maxRestoreJournalEventBytes))
}

func TestRestoreJournalIgnoresIncompleteFinalEvent(t *testing.T) {
	t.Parallel()
	env := newBackupTestEnv(t, platformids.Mister)
	payload := []byte("new-save\n")
	manifest := &Manifest{Files: []FileRef{{
		ArchivePath: platformArchive(filepath.Join("saves", "game.sav")),
		RestorePath: "saves/game.sav", Category: CategorySaves,
		SHA256: sha256Hex(payload), Size: int64(len(payload)),
	}}}
	journal, err := env.Manager.prepareRestoreJournal(context.Background(), manifest)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, env.Manager.removeRestoreTransaction(env.Manager.restoreTransactionPath()))
	}()
	require.NoError(t, env.Manager.persistRestorePhase(&journal, restorePhaseApplying))
	require.NoError(t, env.Manager.persistRestoreEntryState(&journal, 0, restoreEntryStarted))
	statePath := filepath.Join(env.Manager.restoreTransactionPath(), restoreJournalStateName)
	// #nosec G304 -- statePath is fixed under a test-owned temporary root.
	state, err := os.OpenFile(statePath, os.O_APPEND|os.O_WRONLY, 0o600)
	require.NoError(t, err)
	_, err = state.WriteString(`{"kind":"entry","sequence":`)
	require.NoError(t, err)
	require.NoError(t, state.Close())

	persisted, err := env.Manager.readRestoreJournal()
	require.NoError(t, err)
	assert.Equal(t, restoreEntryStarted, persisted.Entries[0].State)
	assert.Equal(t, journal.Sequence, persisted.Sequence)
}

func TestRestoreJournalStorageRequirementScalesLinearly(t *testing.T) {
	t.Parallel()
	makeJournal := func(count int) restoreJournal {
		journal := restoreJournal{Version: restoreJournalVersion, Phase: restorePhasePrepared}
		journal.Entries = make([]restoreJournalEntry, count)
		for i := range journal.Entries {
			rel := filepath.ToSlash(filepath.Join("saves", fmt.Sprintf("entry-%06d.sav", i)))
			journal.Entries[i] = restoreJournalEntry{
				File: FileRef{
					ArchivePath: platformArchive(filepath.FromSlash(rel)), RestorePath: rel,
					Category: CategorySaves, SHA256: strings.Repeat("a", sha256.Size*2), Size: 1,
				},
				Root: filepath.Join(string(filepath.Separator), "restore-root"),
				Rel:  filepath.FromSlash(rel), Existed: true,
				RollbackPath: filepath.Join(restoreRollbackDir, fmt.Sprintf("%06d", i)), RollbackSize: 1,
			}
		}
		return journal
	}

	smallJournal := makeJournal(1_000)
	small, err := restoreJournalStorageRequirement(&smallJournal)
	require.NoError(t, err)
	largeJournal := makeJournal(2_000)
	large, err := restoreJournalStorageRequirement(&largeJournal)
	require.NoError(t, err)
	assert.Greater(t, large, small)
	assert.Less(t, large, small*3, "doubling entries must keep journal storage linear")
}

func TestValidateRestoreJournalRejectsUnsafeOperationID(t *testing.T) {
	t.Parallel()
	env := newBackupTestEnv(t, platformids.Mister)

	err := env.Manager.validateRestoreJournal(&restoreJournal{
		OperationID: "../../outside",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "operation ID")
}

func TestRollbackRestoreRemovesCrashLeftStagedTemps(t *testing.T) {
	t.Parallel()

	for _, existed := range []bool{true, false} {
		name := "new target"
		restorePath := "saves/new.sav"
		if existed {
			name = "existing target"
			restorePath = "saves/game.sav"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			env := newBackupTestEnv(t, platformids.Mister)
			payload := []byte("new-save\n")
			manifest := &Manifest{Files: []FileRef{{
				ArchivePath: platformArchive(filepath.FromSlash(restorePath)),
				RestorePath: restorePath, Category: CategorySaves,
				SHA256: sha256Hex(payload), Size: int64(len(payload)),
			}}}
			journal, err := env.Manager.prepareRestoreJournal(context.Background(), manifest)
			require.NoError(t, err)
			journal.Entries[0].State = restoreEntryStarted
			defer func() {
				require.NoError(t, env.Manager.removeRestoreTransaction(env.Manager.restoreTransactionPath()))
			}()

			target, err := env.Manager.resolveRestoreTarget(&manifest.Files[0])
			require.NoError(t, err)
			tmpPath := filepath.Join(target.root, restoreTempRel(target, journal.OperationID, 0))
			writeTestFile(t, tmpPath, "partial-rollback-payload")
			finalPath := filepath.Join(target.root, target.rel)
			if !existed {
				writeTestFile(t, finalPath, string(payload))
			}

			require.NoError(t, env.Manager.rollbackRestore(&journal))
			assert.NoFileExists(t, tmpPath)
			if existed {
				// #nosec G304 -- finalPath is under a test-owned temporary root.
				stored, readErr := os.ReadFile(finalPath)
				require.NoError(t, readErr)
				assert.Equal(t, "save-data\n", string(stored))
			} else {
				assert.NoFileExists(t, finalPath)
			}
		})
	}
}

func TestRecoverPreparedRestorePreservesExternalFile(t *testing.T) {
	t.Parallel()
	env := newBackupTestEnv(t, platformids.Mister)
	targetPath := filepath.Join(env.RootDir, "saves", "external.sav")
	payload := []byte("restore-payload\n")
	manifest := &Manifest{Files: []FileRef{{
		ArchivePath: platformArchive(filepath.Join("saves", "external.sav")),
		RestorePath: "saves/external.sav", Category: CategorySaves,
		SHA256: sha256Hex(payload), Size: int64(len(payload)),
	}}}
	_, err := env.Manager.prepareRestoreJournal(context.Background(), manifest)
	require.NoError(t, err)
	writeTestFile(t, targetPath, "created-after-preflight\n")

	require.NoError(t, env.Manager.recoverRestoreLocked(context.Background()))
	// #nosec G304 -- targetPath is under a test-owned temporary root.
	stored, err := os.ReadFile(targetPath)
	require.NoError(t, err)
	assert.Equal(t, "created-after-preflight\n", string(stored))
	assert.NoDirExists(t, env.Manager.restoreTransactionPath())
}

func TestRecoverRestoreRetainsUnexpectedThirdPartyContent(t *testing.T) {
	t.Parallel()
	env := newBackupTestEnv(t, platformids.Mister)
	targetPath := filepath.Join(env.RootDir, "saves", "external.sav")
	payload := []byte("restore-payload\n")
	manifest := &Manifest{Files: []FileRef{{
		ArchivePath: platformArchive(filepath.Join("saves", "external.sav")),
		RestorePath: "saves/external.sav", Category: CategorySaves,
		SHA256: sha256Hex(payload), Size: int64(len(payload)),
	}}}
	journal, err := env.Manager.prepareRestoreJournal(context.Background(), manifest)
	require.NoError(t, err)
	require.NoError(t, env.Manager.persistRestorePhase(&journal, restorePhaseApplying))
	require.NoError(t, env.Manager.persistRestoreEntryState(&journal, 0, restoreEntryStarted))
	writeTestFile(t, targetPath, "created-by-another-process\n")

	err = env.Manager.recoverRestoreLocked(context.Background())
	require.ErrorIs(t, err, ErrRestoreRecoveryNeeded)
	// #nosec G304 -- targetPath is under a test-owned temporary root.
	stored, readErr := os.ReadFile(targetPath)
	require.NoError(t, readErr)
	assert.Equal(t, "created-by-another-process\n", string(stored))
	assert.DirExists(t, env.Manager.restoreTransactionPath())
}

func TestApplyRestorePreservesUntouchedExternalFileOnLaterEntryFailure(t *testing.T) {
	t.Parallel()
	env := newBackupTestEnv(t, platformids.Mister)
	firstPath := filepath.Join(env.RootDir, "saves", "first.sav")
	secondPath := filepath.Join(env.RootDir, "saves", "external.sav")
	writeTestFile(t, firstPath, "first-old\n")
	firstNew := []byte("first-new\n")
	secondNew := []byte("second-new\n")
	manifest := &Manifest{Files: []FileRef{
		{
			ArchivePath: platformArchive(filepath.Join("saves", "first.sav")),
			RestorePath: "saves/first.sav", Category: CategorySaves,
			SHA256: sha256Hex(firstNew), Size: int64(len(firstNew)),
		},
		{
			ArchivePath: platformArchive(filepath.Join("saves", "external.sav")),
			RestorePath: "saves/external.sav", Category: CategorySaves,
			SHA256: sha256Hex(secondNew), Size: int64(len(secondNew)),
		},
	}}

	err := env.Manager.applyRestore(context.Background(), manifest, func(file FileRef) (io.ReadCloser, error) {
		if file.RestorePath == "saves/external.sav" {
			writeTestFile(t, secondPath, "created-by-another-process\n")
			return nil, errors.New("injected payload failure")
		}
		return io.NopCloser(bytes.NewReader(firstNew)), nil
	})
	require.Error(t, err)
	// #nosec G304 -- firstPath is under a test-owned temporary root.
	first, readErr := os.ReadFile(firstPath)
	require.NoError(t, readErr)
	assert.Equal(t, "first-old\n", string(first))
	// #nosec G304 -- secondPath is under a test-owned temporary root.
	second, readErr := os.ReadFile(secondPath)
	require.NoError(t, readErr)
	assert.Equal(t, "created-by-another-process\n", string(second))
	assert.NoDirExists(t, env.Manager.restoreTransactionPath())
}

func TestApplyRestoreRollsBackEarlierFilesOnFailure(t *testing.T) {
	t.Parallel()
	env := newBackupTestEnv(t, platformids.Mister)
	firstPath := filepath.Join(env.RootDir, "saves", "first.sav")
	secondPath := filepath.Join(env.RootDir, "saves", "second.sav")
	writeTestFile(t, firstPath, "first-old\n")
	writeTestFile(t, secondPath, "second-old\n")
	firstNew := []byte("first-new\n")
	secondNew := []byte("second-new\n")
	manifest := &Manifest{Files: []FileRef{
		{
			ArchivePath: platformArchive(filepath.Join("saves", "first.sav")),
			RestorePath: "saves/first.sav", Category: CategorySaves,
			SHA256: sha256Hex(firstNew), Size: int64(len(firstNew)),
		},
		{
			ArchivePath: platformArchive(filepath.Join("saves", "second.sav")),
			RestorePath: "saves/second.sav", Category: CategorySaves,
			SHA256: sha256Hex(secondNew), Size: int64(len(secondNew)),
		},
	}}

	err := env.Manager.applyRestore(context.Background(), manifest, func(file FileRef) (io.ReadCloser, error) {
		if file.RestorePath == "saves/second.sav" {
			return nil, errors.New("injected payload failure")
		}
		return io.NopCloser(bytes.NewReader(firstNew)), nil
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "injected payload failure")
	// #nosec G304 -- firstPath is created under test-owned temporary roots.
	first, readErr := os.ReadFile(firstPath)
	require.NoError(t, readErr)
	assert.Equal(t, "first-old\n", string(first))
	// #nosec G304 -- secondPath is created under test-owned temporary roots.
	second, readErr := os.ReadFile(secondPath)
	require.NoError(t, readErr)
	assert.Equal(t, "second-old\n", string(second))
	assert.NoDirExists(t, env.Manager.restoreTransactionPath())
}

func TestApplyRestoreRollsBackUserDBAfterRestoreFailure(t *testing.T) {
	t.Parallel()
	env := newBackupTestEnv(t, platformids.Mister)
	env.UserDB.ExpectedCalls = nil
	env.UserDB.On("ListClients").Return([]database.Client{}, nil)
	rollbackName := "backup-20260717-120000-000000001-auto.db"
	env.UserDB.On("Backup", "restore-rollback", false).Return(database.BackupInfo{
		Name: rollbackName, Path: env.UserSnapshot, Valid: true,
	}, nil)
	env.UserDB.On("GetDBPath").Return(filepath.Join(env.DataDir, config.UserDbFile))
	manualBackupName := testifymock.MatchedBy(func(name string) bool {
		return strings.HasSuffix(name, "-manual.db")
	})
	env.UserDB.On("RestoreBackup", manualBackupName).
		Return(database.RestoreInfo{}, errors.New("injected database restore failure")).Once()
	env.UserDB.On("RestoreBackup", manualBackupName).Return(database.RestoreInfo{
		RestoredFrom: database.BackupInfo{Valid: true},
	}, nil).Once()

	payload := []byte("portable-user-db")
	manifest := &Manifest{Files: []FileRef{{
		ArchivePath: zaparooArchive("user.db"), RestorePath: "user.db", Category: CategoryZaparoo,
		SHA256: sha256Hex(payload), Size: int64(len(payload)),
	}}}
	err := env.Manager.applyRestore(context.Background(), manifest, func(FileRef) (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(payload)), nil
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "injected database restore failure")
	env.UserDB.AssertNumberOfCalls(t, "RestoreBackup", 2)
	assert.NoDirExists(t, env.Manager.restoreTransactionPath())
}

func TestRecoverRestoreCleansStaleStagingDirectories(t *testing.T) {
	t.Parallel()
	env := newBackupTestEnv(t, platformids.Mister)
	backupDir := env.Manager.backupDir()
	localStaging := filepath.Join(backupDir, localRestoreStagingPrefix+"stale")
	remoteStaging := filepath.Join(backupDir, remoteRestoreStagingPrefix+"stale")
	unrelatedDir := filepath.Join(backupDir, "keep-me")
	writeTestFile(t, filepath.Join(localStaging, "payload"), "local")
	writeTestFile(t, filepath.Join(remoteStaging, "payload"), "remote")
	writeTestFile(t, filepath.Join(unrelatedDir, "payload"), "keep")
	reservedFile := filepath.Join(backupDir, localRestoreStagingPrefix+"not-a-directory")
	writeTestFile(t, reservedFile, "keep")
	stagingLink := filepath.Join(backupDir, remoteRestoreStagingPrefix+"symlink")
	if runtime.GOOS != "windows" {
		require.NoError(t, os.Symlink(unrelatedDir, stagingLink))
	}

	require.NoError(t, env.Manager.RecoverRestore(context.Background()))
	assert.NoDirExists(t, localStaging)
	assert.NoDirExists(t, remoteStaging)
	assert.DirExists(t, unrelatedDir)
	assert.FileExists(t, reservedFile)
	if runtime.GOOS != "windows" {
		_, err := os.Lstat(stagingLink)
		require.NoError(t, err)
	}
}

func TestRecoverRestoreRetainsTransactionOwnedUserDBRollbackAcrossRetries(t *testing.T) {
	t.Parallel()
	env := newBackupTestEnv(t, platformids.Mister)
	payload := []byte("portable-user-db")
	manifest := &Manifest{Files: []FileRef{{
		ArchivePath: zaparooArchive("user.db"), RestorePath: "user.db", Category: CategoryZaparoo,
		SHA256: sha256Hex(payload), Size: int64(len(payload)),
	}}}
	journal, err := env.Manager.prepareRestoreJournal(context.Background(), manifest)
	require.NoError(t, err)
	require.NoError(t, env.Manager.persistRestorePhase(&journal, restorePhaseApplying))
	require.NoError(t, env.Manager.persistRestoreUserDBStarted(&journal))
	require.NoError(t, os.Remove(env.UserSnapshot), "simulate retention pruning original auto backup")

	env.UserDB.ExpectedCalls = nil
	env.UserDB.On("GetDBPath").Return(filepath.Join(env.DataDir, config.UserDbFile))
	manualBackupName := testifymock.MatchedBy(func(name string) bool {
		return strings.HasSuffix(name, "-manual.db")
	})
	assertStagedRollback := func(args testifymock.Arguments) {
		name := args.String(0)
		staged := filepath.Join(env.DataDir, "backups", name)
		// #nosec G304 -- staged is generated under a test-owned temporary root.
		contents, readErr := os.ReadFile(staged)
		require.NoError(t, readErr)
		assert.Equal(t, []byte("user-db-snapshot\n"), contents)
	}
	env.UserDB.On("RestoreBackup", manualBackupName).Run(assertStagedRollback).
		Return(database.RestoreInfo{}, errors.New("injected rollback failure")).Once()
	env.UserDB.On("RestoreBackup", manualBackupName).Run(assertStagedRollback).
		Return(database.RestoreInfo{RestoredFrom: database.BackupInfo{Valid: true}}, nil).Once()

	err = env.Manager.recoverRestoreLocked(context.Background())
	require.ErrorIs(t, err, ErrRestoreRecoveryNeeded)
	require.NotNil(t, journal.UserDBRollback)
	artifactPath := filepath.Join(env.Manager.restoreTransactionPath(), journal.UserDBRollback.Path)
	assert.FileExists(t, artifactPath)

	require.NoError(t, env.Manager.recoverRestoreLocked(context.Background()))
	assert.NoDirExists(t, env.Manager.restoreTransactionPath())
	env.UserDB.AssertNumberOfCalls(t, "RestoreBackup", 2)
}

func TestApplyRestoreRetainsJournalWhenRollbackCannotReachTarget(t *testing.T) {
	t.Parallel()
	env := newBackupTestEnv(t, platformids.Mister)
	savesRoot := filepath.Join(env.RootDir, "saves")
	movedRoot := filepath.Join(env.RootDir, "saves-offline")
	firstNew := []byte("first-new\n")
	secondNew := []byte("second-new\n")
	manifest := &Manifest{Files: []FileRef{
		{
			ArchivePath: platformArchive(filepath.Join("saves", "first.sav")),
			RestorePath: "saves/first.sav", Category: CategorySaves,
			SHA256: sha256Hex(firstNew), Size: int64(len(firstNew)),
		},
		{
			ArchivePath: platformArchive(filepath.Join("saves", "second.sav")),
			RestorePath: "saves/second.sav", Category: CategorySaves,
			SHA256: sha256Hex(secondNew), Size: int64(len(secondNew)),
		},
	}}
	writeTestFile(t, filepath.Join(savesRoot, "first.sav"), "first-old\n")
	writeTestFile(t, filepath.Join(savesRoot, "second.sav"), "second-old\n")

	err := env.Manager.applyRestore(context.Background(), manifest, func(file FileRef) (io.ReadCloser, error) {
		if file.RestorePath == "saves/second.sav" {
			require.NoError(t, os.Rename(savesRoot, movedRoot))
			return nil, errors.New("injected payload failure")
		}
		return io.NopCloser(bytes.NewReader(firstNew)), nil
	})
	require.Error(t, err)
	require.ErrorIs(t, err, ErrRestoreRecoveryNeeded)
	assert.DirExists(t, env.Manager.restoreTransactionPath())

	require.NoError(t, os.Rename(movedRoot, savesRoot))
	require.NoError(t, env.Manager.RecoverRestore(context.Background()))
	// #nosec G304 -- savesRoot is created under test-owned temporary roots.
	first, readErr := os.ReadFile(filepath.Join(savesRoot, "first.sav"))
	require.NoError(t, readErr)
	assert.Equal(t, "first-old\n", string(first))
	assert.NoDirExists(t, env.Manager.restoreTransactionPath())
}

func TestApplyRestoreFollowsTrustedExistingSymlink(t *testing.T) {
	t.Parallel()
	env := newBackupTestEnv(t, platformids.Mister)
	usbTarget := filepath.Join(env.RootDir, "usb", "saves", "linked.sav")
	writeTestFile(t, usbTarget, "old-usb-save\n")
	logicalPath := filepath.Join(env.RootDir, "saves", "linked.sav")
	require.NoError(t, os.Symlink(usbTarget, logicalPath))
	newPayload := []byte("new-usb-save\n")
	manifest := &Manifest{Files: []FileRef{{
		ArchivePath: platformArchive(filepath.Join("saves", "linked.sav")),
		RestorePath: "saves/linked.sav", Category: CategorySaves,
		SHA256: sha256Hex(newPayload), Size: int64(len(newPayload)),
	}}}

	err := env.Manager.applyRestore(context.Background(), manifest, func(FileRef) (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(newPayload)), nil
	})
	require.NoError(t, err)
	linkTarget, err := os.Readlink(logicalPath)
	require.NoError(t, err)
	assert.Equal(t, usbTarget, linkTarget)
	// #nosec G304 -- usbTarget is created under test-owned temporary roots.
	payload, err := os.ReadFile(usbTarget)
	require.NoError(t, err)
	assert.Equal(t, string(newPayload), string(payload))
}

func TestApplyRestoreAllowsApprovedCategoryRootSymlink(t *testing.T) {
	t.Parallel()
	env := newBackupTestEnv(t, platformids.Mister)

	require.NoError(t, os.RemoveAll(filepath.Join(env.RootDir, "saves")))
	usbSaves := filepath.Join(env.RootDir, "usb", "saves")
	require.NoError(t, os.MkdirAll(usbSaves, 0o750))
	require.NoError(t, os.Symlink(usbSaves, filepath.Join(env.RootDir, "saves")))

	payload := []byte("restored-save\n")
	manifest := &Manifest{Files: []FileRef{{
		ArchivePath: platformArchive(filepath.Join("saves", "linked.sav")),
		RestorePath: "saves/linked.sav", Category: CategorySaves,
		SHA256: sha256Hex(payload), Size: int64(len(payload)),
	}}}
	err := env.Manager.applyRestore(context.Background(), manifest, func(FileRef) (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(payload)), nil
	})
	require.NoError(t, err)

	// #nosec G304 -- target is under a test-owned temporary root.
	restored, err := os.ReadFile(filepath.Join(usbSaves, "linked.sav"))
	require.NoError(t, err)
	assert.Equal(t, payload, restored)
}

func TestApplyRestoreRejectsCategoryRootSymlinkToAuthConfig(t *testing.T) {
	t.Parallel()
	env := newBackupTestEnv(t, platformids.Mister)

	require.NoError(t, os.RemoveAll(filepath.Join(env.RootDir, "saves")))
	authPath := filepath.Join(env.ConfigDir, config.AuthFile)
	writeTestFile(t, authPath, "secret-bearer\n")
	require.NoError(t, os.Symlink(env.ConfigDir, filepath.Join(env.RootDir, "saves")))

	payload := []byte("attacker-controlled\n")
	manifest := &Manifest{Files: []FileRef{{
		ArchivePath: platformArchive(filepath.Join("saves", config.AuthFile)),
		RestorePath: filepath.ToSlash(filepath.Join("saves", config.AuthFile)), Category: CategorySaves,
		SHA256: sha256Hex(payload), Size: int64(len(payload)),
	}}}
	err := env.Manager.applyRestore(context.Background(), manifest, func(FileRef) (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(payload)), nil
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "sensitive")

	// #nosec G304 -- authPath is under a test-owned temporary root.
	stored, err := os.ReadFile(authPath)
	require.NoError(t, err)
	assert.Equal(t, "secret-bearer\n", string(stored))
}

func TestApplyRestoreRejectsOutsideRootSymlink(t *testing.T) {
	t.Parallel()
	env := newBackupTestEnv(t, platformids.Mister)
	outside := filepath.Join(t.TempDir(), "secret.sav")
	writeTestFile(t, outside, "must-not-change\n")
	logicalPath := filepath.Join(env.RootDir, "saves", "leak.sav")
	require.NoError(t, os.Symlink(outside, logicalPath))
	newPayload := []byte("attacker-controlled\n")
	manifest := &Manifest{Files: []FileRef{{
		ArchivePath: platformArchive(filepath.Join("saves", "leak.sav")),
		RestorePath: "saves/leak.sav", Category: CategorySaves,
		SHA256: sha256Hex(newPayload), Size: int64(len(newPayload)),
	}}}

	err := env.Manager.applyRestore(context.Background(), manifest, func(FileRef) (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(newPayload)), nil
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "escapes trusted roots")
	// #nosec G304 -- outside is created under a test-owned temporary root.
	payload, readErr := os.ReadFile(outside)
	require.NoError(t, readErr)
	assert.Equal(t, "must-not-change\n", string(payload))
	assert.NoDirExists(t, env.Manager.restoreTransactionPath())
}

func TestRecoverRestoreRollsBackApplyingJournal(t *testing.T) {
	t.Parallel()
	env := newBackupTestEnv(t, platformids.Mister)
	targetPath := filepath.Join(env.RootDir, "saves", "game.sav")
	writeTestFile(t, targetPath, "before-crash\n")
	newPayload := []byte("after-crash\n")
	manifest := &Manifest{Files: []FileRef{{
		ArchivePath: platformArchive(filepath.Join("saves", "game.sav")),
		RestorePath: "saves/game.sav", Category: CategorySaves,
		SHA256: sha256Hex(newPayload), Size: int64(len(newPayload)),
	}}}
	journal, err := env.Manager.prepareRestoreJournal(context.Background(), manifest)
	require.NoError(t, err)
	require.NoError(t, env.Manager.persistRestorePhase(&journal, restorePhaseApplying))
	require.NoError(t, env.Manager.persistRestoreEntryState(&journal, 0, restoreEntryStarted))
	require.NoError(t, env.Manager.persistRestoreEntryState(&journal, 0, restoreEntryApplied))
	writeTestFile(t, targetPath, string(newPayload))

	require.NoError(t, env.Manager.recoverRestoreLocked(context.Background()))
	// #nosec G304 -- targetPath is created under test-owned temporary roots.
	payload, err := os.ReadFile(targetPath)
	require.NoError(t, err)
	assert.Equal(t, "before-crash\n", string(payload))
	assert.NoDirExists(t, env.Manager.restoreTransactionPath())
}

func TestRecoverRestoreUsesPersistedPolicyAfterConfiguredRootsChange(t *testing.T) {
	t.Parallel()
	env := newBackupTestEnv(t, platformids.Mister)

	usbTarget := filepath.Join(env.RootDir, "usb", "saves", "custom.sav")
	writeTestFile(t, usbTarget, "before-crash\n")
	logicalPath := filepath.Join(env.RootDir, "saves", "custom.sav")
	require.NoError(t, os.Symlink(usbTarget, logicalPath))
	newPayload := []byte("after-crash\n")
	manifest := &Manifest{Files: []FileRef{{
		ArchivePath: platformArchive(filepath.Join("saves", "custom.sav")),
		RestorePath: "saves/custom.sav", Category: CategorySaves,
		SHA256: sha256Hex(newPayload), Size: int64(len(newPayload)),
	}}}
	journal, err := env.Manager.prepareRestoreJournal(context.Background(), manifest)
	require.NoError(t, err)
	require.NoError(t, env.Manager.persistRestorePhase(&journal, restorePhaseApplying))
	require.NoError(t, env.Manager.persistRestoreEntryState(&journal, 0, restoreEntryStarted))
	require.NoError(t, env.Manager.persistRestoreEntryState(&journal, 0, restoreEntryApplied))
	writeTestFile(t, usbTarget, string(newPayload))

	changedPlatform := mocks.NewMockPlatform()
	changedPlatform.On("Settings").Return(platforms.Settings{
		DataDir: env.DataDir, ConfigDir: env.ConfigDir,
		TempDir: filepath.Join(env.RootDir, "tmp"), LogDir: env.RootDir,
	})
	changedPlatform.On("RootDirs", testifymock.Anything).Return([]string{env.RootDir}).Maybe()
	env.Manager.pl = backupPlatform{
		MockPlatform: changedPlatform,
		definitions:  testPlatformDefinitions(env.RootDir),
	}

	require.NoError(t, env.Manager.recoverRestoreLocked(context.Background()))
	// #nosec G304 -- usbTarget is under a test-owned temporary root.
	payload, err := os.ReadFile(usbTarget)
	require.NoError(t, err)
	assert.Equal(t, "before-crash\n", string(payload))
	assert.NoDirExists(t, env.Manager.restoreTransactionPath())
	changedPlatform.AssertNotCalled(t, "RootDirs", testifymock.Anything)
}

func TestRecoverRestoreKeepsCommittedFiles(t *testing.T) {
	t.Parallel()
	env := newBackupTestEnv(t, platformids.Mister)
	targetPath := filepath.Join(env.RootDir, "saves", "game.sav")
	writeTestFile(t, targetPath, "before-commit\n")
	newPayload := []byte("after-commit\n")
	manifest := &Manifest{Files: []FileRef{{
		ArchivePath: platformArchive(filepath.Join("saves", "game.sav")),
		RestorePath: "saves/game.sav", Category: CategorySaves,
		SHA256: sha256Hex(newPayload), Size: int64(len(newPayload)),
	}}}
	journal, err := env.Manager.prepareRestoreJournal(context.Background(), manifest)
	require.NoError(t, err)
	require.NoError(t, env.Manager.persistRestorePhase(&journal, restorePhaseApplying))
	require.NoError(t, env.Manager.persistRestoreEntryState(&journal, 0, restoreEntryStarted))
	require.NoError(t, env.Manager.persistRestoreEntryState(&journal, 0, restoreEntryApplied))
	require.NoError(t, env.Manager.persistRestorePhase(&journal, restorePhaseCommitted))
	writeTestFile(t, targetPath, string(newPayload))

	require.NoError(t, env.Manager.recoverRestoreLocked(context.Background()))
	// #nosec G304 -- targetPath is created under test-owned temporary roots.
	payload, err := os.ReadFile(targetPath)
	require.NoError(t, err)
	assert.Equal(t, string(newPayload), string(payload))
	assert.NoDirExists(t, env.Manager.restoreTransactionPath())
}

func TestManagerCorruptStatusFailsRemoteClosed(t *testing.T) {
	env := newBackupTestEnv(t, platformids.Mister)
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer server.Close()
	configureRemoteTestAuth(t, env.Manager, server.URL)

	require.NoError(t, os.MkdirAll(filepath.Dir(env.Manager.statusPath()), 0o750))
	require.NoError(t, os.WriteFile(env.Manager.statusPath(), []byte(`{"remote":`), 0o600))

	_, err := env.Manager.newRemoteClient()
	require.ErrorIs(t, err, errRemoteUnlinked)
	assert.True(t, env.Manager.coordinator.RemoteUnlinked())
	assert.False(t, env.Manager.Status().Remote.Linked)
}

func TestManagerWritesStatusAtomically(t *testing.T) {
	t.Parallel()
	env := newBackupTestEnv(t, platformids.Mister)

	require.NoError(t, env.Manager.writeRemoteStatus(&statusEntry{
		LastStatus: StatusFailed,
		LastError:  "device not linked",
		Unlinked:   true,
	}))
	// #nosec G304 -- status path is generated under a test-owned temporary root.
	data, err := os.ReadFile(env.Manager.statusPath())
	require.NoError(t, err)
	var stored statusFile
	require.NoError(t, json.Unmarshal(data, &stored))
	assert.True(t, stored.Remote.Unlinked)
	info, err := os.Stat(env.Manager.statusPath())
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	entries, err := os.ReadDir(filepath.Dir(env.Manager.statusPath()))
	require.NoError(t, err)
	for _, entry := range entries {
		assert.False(t, strings.HasPrefix(entry.Name(), ".status-"))
	}
}

func TestManagerStatusDoesNotRequireDatabase(t *testing.T) {
	t.Parallel()
	env := newBackupTestEnv(t, platformids.Mister)
	mgr := NewManager(env.Manager.cfg, env.Manager.pl, nil)

	status := mgr.Status()
	assert.True(t, status.Local.Enabled)
	assert.Equal(t, StatusNever, status.Local.LastStatus)
	assert.Equal(t, config.DefaultBackupRemoteSchedule, status.Remote.Schedule)
}

func TestStatusEntrySanitizesStoredError(t *testing.T) {
	t.Parallel()
	entry := toStatusEntry(&statusEntry{
		LastStatus: StatusFailed,
		LastError:  "creating backup directory: mkdir /media/fat/zaparoo/backups/files: permission denied",
	}, true, "")

	assert.Equal(t, "backup failed", entry.LastError)
	assert.NotContains(t, entry.LastError, "/media/fat")
}

func TestManagerRunRemoteBackupUploadsPackedSnapshot(t *testing.T) {
	env := newBackupTestEnv(t, platformids.Mister)
	uploaded := make(map[string][]byte)
	var committed remoteSnapshotRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/device/heartbeat":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/device/me":
			writeJSON(t, w, remoteDeviceMeResponse{ID: "device-1", Name: "test", BackupActive: true})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/device/backup-objects/check":
			var req remoteCheckRequest
			decodeErr := json.NewDecoder(r.Body).Decode(&req)
			if !assert.NoError(t, decodeErr) {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			writeJSON(t, w, remoteCheckResponse{Missing: req.Hashes})
		case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/v1/device/backup-packs/"):
			body, err := io.ReadAll(r.Body)
			if !assert.NoError(t, err) {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			assert.Equal(t, sha256Hex(body), strings.TrimPrefix(r.URL.Path, "/v1/device/backup-packs/"))
			for hash, payload := range parseTestPack(t, body) {
				uploaded[hash] = payload
			}
			writeJSON(t, w, remotePackResponse{
				PackHash: sha256Hex(body), ObjectCount: len(uploaded), CreatedAt: time.Now().UTC(),
			})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/device/backups":
			decodeErr := json.NewDecoder(r.Body).Decode(&committed)
			if !assert.NoError(t, decodeErr) {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			for _, entries := range committed.Categories {
				for _, entry := range entries {
					assert.Contains(t, uploaded, entry.SHA256)
				}
			}
			writeJSON(t, w, testCommittedRemoteResponse(
				t, "backup-1", platformids.Mister, &committed,
			))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/device/backups":
			writeJSON(t, w, remoteListResponse{StorageUsedBytes: 99, StorageQuotaBytes: 1 << 30})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()
	configureRemoteTestAuth(t, env.Manager, server.URL)
	env.Manager.cfg.SetBackupRemoteEnabled(false)
	defaultOpener := env.Manager.sourceOpener
	env.Manager.sourceOpener = func(ctx context.Context, file *FileRef) (io.ReadCloser, error) {
		if file.RestorePath == filepath.ToSlash(filepath.Join("saves", "game.sav")) {
			return io.NopCloser(&errorReader{err: &os.PathError{
				Op: "read", Path: file.RestorePath, Err: errors.New("network I/O failure"),
			}}), nil
		}
		return defaultOpener(ctx, file)
	}

	info, err := env.Manager.RunRemote(context.Background(), RemoteBackupTypeManual)
	require.NoError(t, err)
	assert.Equal(t, "backup-1", info.Backup.ID)
	assert.Positive(t, info.UploadedPacks)
	assert.Contains(t, committed.Categories, CategorySavestates)
	assert.NotEmpty(t, committed.Categories[CategorySavestates])
	assert.NotContains(t, committed.Categories, CategorySaves)
	assert.Contains(t, info.Warnings, models.BackupWarning{
		Category: CategorySaves,
		Path:     filepath.ToSlash(filepath.Join("saves", "game.sav")),
		Reason:   "source unreadable during backup",
	})
	assert.Equal(t, int64(99), info.StorageUsedBytes)
	status := env.Manager.Status()
	assert.Equal(t, StatusPartial, status.Remote.LastStatus)
	assert.Equal(t, 1, status.Remote.SkippedFiles)
	assert.Positive(t, status.Remote.Categories[CategorySavestates].Files)
}

func TestRemoteUploadMissingReportsEveryPathForDuplicateOversizedContent(t *testing.T) {
	t.Parallel()
	payload := []byte("oversized save payload")
	hash := sha256Hex(payload)
	sourcePath := filepath.Join(t.TempDir(), "large.sav")
	require.NoError(t, os.WriteFile(sourcePath, payload, 0o600))

	// The server has no way to store a file that cannot fit inside a single
	// pack (there is no per-object upload endpoint), so nothing may be sent.
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		t.Errorf("oversized file must not be uploaded, got %s %s", r.Method, r.URL.Path)
	}))
	defer server.Close()

	client := &remoteClient{httpClient: server.Client(), baseURL: server.URL, bearer: "test-token", platform: "test"}
	files := []FileRef{
		{
			Category:    CategorySaves,
			RestorePath: "saves/large.sav",
			SHA256:      hash,
			Size:        remoteMaxPackBytes,
		},
		{
			Category:    CategorySavestates,
			RestorePath: "savestates/duplicate-large.ss",
			SHA256:      hash,
			Size:        remoteMaxPackBytes,
		},
	}
	missing := map[string]struct{}{hash: {}}

	result, err := client.uploadMissing(context.Background(), files, missing, nil)
	require.NoError(t, err)
	assert.Zero(t, result.packs)
	assert.Zero(t, result.bytesUploaded)
	require.Len(t, result.skipped, 2)
	assert.Equal(t, []string{"saves/large.sav", "savestates/duplicate-large.ss"}, []string{
		result.skipped[0].RestorePath,
		result.skipped[1].RestorePath,
	})

	// And the manifest must drop the skipped file so the commit never
	// references bytes the server does not have.
	assert.Empty(t, withoutSkippedFiles(files, result.skipped))
}

func TestRemoteUploadMissingNeverPacksEmptyFiles(t *testing.T) {
	t.Parallel()
	sourcePath := filepath.Join(t.TempDir(), "empty.sav")
	require.NoError(t, os.WriteFile(sourcePath, nil, 0o600))

	// Even if a server (wrongly) reports the empty hash missing, it must
	// never be packed: a zero-length range cannot live inside a pack.
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		t.Errorf("empty file must not be uploaded, got %s %s", r.Method, r.URL.Path)
	}))
	defer server.Close()

	client := &remoteClient{httpClient: server.Client(), baseURL: server.URL, bearer: "test-token", platform: "test"}
	files := []FileRef{{
		Category:    CategorySaves,
		RestorePath: "saves/empty.sav",
		SHA256:      remoteEmptyContentSHA256,
		Size:        0,
	}}
	missing := map[string]struct{}{remoteEmptyContentSHA256: {}}

	result, err := client.uploadMissing(context.Background(), files, missing, nil)
	require.NoError(t, err)
	assert.Zero(t, result.packs)
	assert.Zero(t, result.bytesUploaded)
	assert.Empty(t, result.skipped, "empty files stay in the manifest; they are not skipped")
}

func TestManagerRunRemoteBackupReportsQuotaExceeded(t *testing.T) {
	env := newBackupTestEnv(t, platformids.Mister)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/device/heartbeat":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/device/me":
			writeJSON(t, w, remoteDeviceMeResponse{ID: "device-1", Name: "test", BackupActive: true})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/device/backup-objects/check":
			var req remoteCheckRequest
			decodeErr := json.NewDecoder(r.Body).Decode(&req)
			if !assert.NoError(t, decodeErr) {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			writeJSON(t, w, remoteCheckResponse{Missing: req.Hashes})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/device/backups":
			writeJSON(t, w, remoteListResponse{StorageUsedBytes: 0, StorageQuotaBytes: 1})
		case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/v1/device/backup-packs/"):
			t.Fatal("quota preflight must reject before uploading a pack")
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()
	configureRemoteTestAuth(t, env.Manager, server.URL)
	ns := make(chan models.Notification, 1)
	env.UserDB.On("AddInboxMessage", testifymock.MatchedBy(func(msg *database.InboxMessage) bool {
		return msg.Title == "Remote backup storage full" &&
			msg.Category == inboxservice.CategoryBackupRemoteQuotaExceeded &&
			msg.Severity == inboxservice.SeverityError
	})).Return(&database.InboxMessage{
		DBID: 1, Title: "Remote backup storage full", Category: inboxservice.CategoryBackupRemoteQuotaExceeded,
	}, nil).Once()
	env.Manager.WithInbox(inboxservice.NewService(env.UserDB, ns))

	_, err := env.Manager.RunRemote(context.Background(), RemoteBackupTypeManual)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "quota exceeded")
	status := env.Manager.Status()
	assert.Equal(t, StatusFailed, status.Remote.LastStatus)
	assert.Equal(t, "storage quota exceeded", status.Remote.LastError)
	env.UserDB.AssertExpectations(t)
	select {
	case notification := <-ns:
		assert.Equal(t, models.NotificationInboxAdded, notification.Method)
	default:
		t.Fatal("expected inbox notification")
	}
}

func TestRemotePackPlanReportsExactUploadBytes(t *testing.T) {
	t.Parallel()
	files := []FileRef{
		{Category: CategorySaves, RestorePath: "saves/one", SHA256: strings.Repeat("1", 64), Size: 10},
		{Category: CategorySaves, RestorePath: "saves/two", SHA256: strings.Repeat("2", 64), Size: 20},
	}
	missing := map[string]struct{}{files[0].SHA256: {}, files[1].SHA256: {}}

	plan, err := planRemotePacks(files, missing)
	require.NoError(t, err)
	require.Len(t, plan.packs, 1)
	expected, err := remotePackEncodedSize(plan.packs[0].files)
	require.NoError(t, err)
	assert.Equal(t, expected, plan.packs[0].size)
	assert.Equal(t, expected, plan.uploadBytes)
}

func TestLocateRemotePacksRejectsInvalidServerResponses(t *testing.T) {
	t.Parallel()

	requestedHash := strings.Repeat("a", 64)
	otherHash := strings.Repeat("b", 64)
	packHash := strings.Repeat("c", 64)
	validRef := remotePackObjectRef{Hash: requestedHash, Offset: 0, Length: 3}

	tests := []struct {
		name     string
		wantErr  string
		response remoteLocateResponse
	}{
		{
			name:     "reported missing",
			response: remoteLocateResponse{Missing: []string{requestedHash}},
			wantErr:  "no longer available",
		},
		{
			name: "conflicting pack sizes",
			response: remoteLocateResponse{Packs: []remotePackObjects{
				{PackHash: packHash, SizeBytes: 3, Objects: []remotePackObjectRef{validRef}},
				{PackHash: packHash, SizeBytes: 4},
			}},
			wantErr: "conflicting sizes",
		},
		{
			name: "invalid pack size",
			response: remoteLocateResponse{Packs: []remotePackObjects{{
				PackHash: packHash, SizeBytes: 0, Objects: []remotePackObjectRef{validRef},
			}}},
			wantErr: "invalid size",
		},
		{
			name: "unrequested object",
			response: remoteLocateResponse{Packs: []remotePackObjects{{
				PackHash: packHash, SizeBytes: 3,
				Objects: []remotePackObjectRef{{Hash: otherHash, Offset: 0, Length: 3}},
			}}},
			wantErr: "unrequested object",
		},
		{
			name: "duplicate object",
			response: remoteLocateResponse{Packs: []remotePackObjects{{
				PackHash: packHash, SizeBytes: 6, Objects: []remotePackObjectRef{
					validRef,
					{Hash: requestedHash, Offset: 3, Length: 3},
				},
			}}},
			wantErr: "located more than once",
		},
		{
			name: "range outside pack",
			response: remoteLocateResponse{Packs: []remotePackObjects{{
				PackHash: packHash, SizeBytes: 3,
				Objects: []remotePackObjectRef{{Hash: requestedHash, Offset: 1, Length: 3}},
			}}},
			wantErr: "inconsistent pack range",
		},
		{
			name: "requested object omitted",
			response: remoteLocateResponse{Packs: []remotePackObjects{{
				PackHash: packHash, SizeBytes: 3,
			}}},
			wantErr: "response is missing 1 objects",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Equal(t, "/v1/device/backup-objects/locate", r.URL.Path)
				writeJSON(t, w, tt.response)
			}))
			t.Cleanup(server.Close)
			client := &remoteClient{httpClient: server.Client(), baseURL: server.URL}

			_, err := locateRemotePacks(context.Background(), client, map[string]int64{requestedHash: 3})
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestDownloadPackAttemptRejectsIntegrityMismatch(t *testing.T) {
	t.Parallel()

	body := []byte("pack-data")
	tests := []struct {
		name     string
		packHash string
		wantErr  string
		size     int64
	}{
		{name: "short body", packHash: sha256Hex(body), size: int64(len(body) + 1), wantErr: "size mismatch"},
		{name: "extra body", packHash: sha256Hex(body), size: int64(len(body) - 1), wantErr: "size mismatch"},
		{name: "wrong hash", packHash: strings.Repeat("0", 64), size: int64(len(body)), wantErr: "hash mismatch"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write(body)
			}))
			t.Cleanup(server.Close)
			dir := t.TempDir()
			client := &remoteClient{httpClient: server.Client(), baseURL: server.URL}

			_, err := client.downloadPackAttempt(context.Background(), &remotePackObjects{
				PackHash: tt.packHash, SizeBytes: tt.size,
			}, dir)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
			entries, readErr := os.ReadDir(dir)
			require.NoError(t, readErr)
			assert.Empty(t, entries, "failed pack download must remove its staging file")
		})
	}
}

func TestExtractPackObjectRejectsHashMismatch(t *testing.T) {
	t.Parallel()

	packPath := filepath.Join(t.TempDir(), "pack")
	require.NoError(t, os.WriteFile(packPath, []byte("payload"), 0o600))
	// #nosec G304 -- packPath is created inside this test's temporary directory.
	pack, err := os.Open(packPath)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, pack.Close()) })

	err = extractPackObject(pack, &remotePackObjectRef{
		Hash: strings.Repeat("0", 64), Offset: 0, Length: int64(len("payload")),
	}, filepath.Join(t.TempDir(), "object"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "staged pack object hash mismatch")
}

func TestEnsureRemoteUploadCapacity(t *testing.T) {
	t.Parallel()
	require.NoError(t, ensureRemoteUploadCapacity(100, 200, 100))
	require.ErrorIs(t, ensureRemoteUploadCapacity(100, 200, 101), errRemoteQuotaExceeded)
	require.ErrorIs(t, ensureRemoteUploadCapacity(201, 200, 0), errRemoteQuotaExceeded)
	require.Error(t, ensureRemoteUploadCapacity(-1, 200, 1))
}

func TestRemoteUploadKeepsServerQuotaCheckAuthoritative(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPut, r.Method)
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"code":"quota_exceeded","message":"full"}}`))
	}))
	defer server.Close()
	client := &remoteClient{
		httpClient: server.Client(), baseURL: server.URL, bearer: "test-token", platform: "test",
	}

	err := client.doBytes(
		context.Background(), http.MethodPut, "/v1/device/backup-packs/hash", []byte("pack"), nil,
	)
	require.ErrorIs(t, err, errRemoteQuotaExceeded)
}

func TestRemotePackFilesSortsByCategoryThenPath(t *testing.T) {
	t.Parallel()
	files := []FileRef{
		{Category: CategorySavestates, RestorePath: "savestates/z.ss", SHA256: "5"},
		{Category: CategorySettings, RestorePath: "MiSTer.ini", SHA256: "2"},
		{Category: CategoryZaparoo, RestorePath: "user.db", SHA256: "1"},
		{Category: CategorySaves, RestorePath: "saves/b.sav", SHA256: "4"},
		{Category: CategoryInputs, RestorePath: "config/inputs/a.map", SHA256: "3"},
		{Category: CategorySaves, RestorePath: "saves/a.sav", SHA256: "6"},
	}

	sortRemotePackFiles(files)

	assert.Equal(t, []string{
		CategoryZaparoo + ":user.db",
		CategorySettings + ":MiSTer.ini",
		CategoryInputs + ":config/inputs/a.map",
		CategorySaves + ":saves/a.sav",
		CategorySaves + ":saves/b.sav",
		CategorySavestates + ":savestates/z.ss",
	}, remotePackOrder(files))
}

func TestManagerListRemoteMapsBackupSources(t *testing.T) {
	env := newBackupTestEnv(t, platformids.Mister)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/v1/device/backups", r.URL.Path)
		assert.Empty(t, r.URL.RawQuery)
		writeJSON(t, w, remoteListResponse{Items: []remoteBackupResponse{{
			ID: "backup-1", BackupType: RemoteBackupTypeManual, SchemaVersion: remoteSchemaVersion,
			Platform: testStringPointer(platformids.Mister), CreatedAt: time.Now().UTC(),
			SourceDevice: &remoteBackupSourceDevice{
				ID: "source-1", Name: "Old MiSTer", Platform: testStringPointer(platformids.Mister),
				Linked: false, Current: false,
			},
		}}})
	}))
	defer server.Close()
	configureRemoteTestAuth(t, env.Manager, server.URL)

	list, err := env.Manager.ListRemote(context.Background())
	require.NoError(t, err)
	require.Len(t, list.Items, 1)
	require.NotNil(t, list.Items[0].SourceDevice)
	assert.Equal(t, "source-1", list.Items[0].SourceDevice.ID)
	assert.Equal(t, "Old MiSTer", list.Items[0].SourceDevice.Name)
	assert.False(t, list.Items[0].SourceDevice.Linked)
}

func TestManagerRevokeRemoteLink(t *testing.T) {
	env := newBackupTestEnv(t, platformids.Mister)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		assert.Equal(t, "/v1/device/me", r.URL.Path)
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	configureRemoteTestAuth(t, env.Manager, server.URL)

	require.NoError(t, env.Manager.RevokeRemoteLink(context.Background()))
}

func TestManagerRestoreRemoteRejectsMissingUserDBBeforePreRestoreBackup(t *testing.T) {
	env := newBackupTestEnv(t, platformids.Mister)
	manifestData, manifestHash, err := canonicalRemoteManifest(map[string][]remoteManifestEntry{})
	require.NoError(t, err)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/v1/device/backups/missing-db" {
			writeJSON(t, w, remoteBackupResponse{
				ID: "missing-db", BackupType: RemoteBackupTypeManual, SchemaVersion: remoteSchemaVersion,
				Platform: testStringPointer(platformids.Mister), ManifestHash: manifestHash,
				Categories: map[string]remoteCategorySummary{}, Manifest: manifestData, CreatedAt: time.Now().UTC(),
			})
			return
		}
		t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
	}))
	defer server.Close()
	configureRemoteTestAuth(t, env.Manager, server.URL)

	_, err = env.Manager.RestoreRemote(context.Background(), "missing-db")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exactly one files/zaparoo/user.db payload")
	env.UserDB.AssertNotCalled(t, "BackupForTransfer", testifymock.Anything, testifymock.Anything)
	env.UserDB.AssertNotCalled(t, "RestoreBackup", testifymock.Anything)
}

func TestManagerRestoreRemoteStagesViaPacks(t *testing.T) {
	env := newBackupTestEnv(t, platformids.Mister)
	newSave := []byte("remote-save\n")
	hash := sha256Hex(newSave)
	remoteConfig := []byte("[service]\ndevice_id = \"remote-source-device\"\n")
	configHash := sha256Hex(remoteConfig)
	manifestData, manifestHash, err := canonicalRemoteManifest(map[string][]remoteManifestEntry{
		CategoryZaparoo: {
			{Path: config.UserDbFile, SHA256: remoteEmptyContentSHA256, Size: 0},
			{Path: config.CfgFile, SHA256: configHash, Size: int64(len(remoteConfig))},
		},
		CategorySaves: {{Path: "saves/game.sav", SHA256: hash, Size: int64(len(newSave))}},
	})
	require.NoError(t, err)
	restoreComplete := false
	var restoreCompleteID string
	pack := newTestPackFixture(t, newSave, remoteConfig)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/device/backups/7":
			writeJSON(t, w, remoteBackupResponse{
				ID: "7", BackupType: RemoteBackupTypeManual, SchemaVersion: remoteSchemaVersion,
				Platform:     testStringPointer(platformids.Mister),
				ManifestHash: manifestHash, SizeBytes: int64(len(newSave) + len(remoteConfig)),
				Categories: map[string]remoteCategorySummary{
					CategoryZaparoo: {Files: 2, Bytes: int64(len(remoteConfig))},
					CategorySaves:   {Files: 1, Bytes: int64(len(newSave))},
				},
				Manifest: manifestData, CreatedAt: time.Now().UTC(),
			})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/device/backup-objects/locate":
			pack.handleLocate(t, w, r)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/device/backup-packs/"+pack.packHash:
			_, _ = w.Write(pack.body)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/device/backups/7/restore-complete":
			var complete remoteRestoreCompleteRequest
			if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&complete)) {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			_, parseErr := uuid.Parse(complete.RestoreID)
			if !assert.NoError(t, parseErr) {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			restoreCompleteID = complete.RestoreID
			restoreComplete = true
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()
	configureRemoteTestAuth(t, env.Manager, server.URL)
	env.Manager.cfg.SetBackupRemoteEnabled(false)

	writeTestFile(t, filepath.Join(env.RootDir, "saves", "game.sav"), "old-save\n")
	restore, err := env.Manager.RestoreRemote(context.Background(), "7")
	require.NoError(t, err)
	assert.Equal(t, "7", restore.RestoredFrom.ID)
	assert.True(t, restoreComplete)
	assert.NotEmpty(t, restoreCompleteID)
	restored, err := os.ReadFile(filepath.Join(env.RootDir, "saves", "game.sav"))
	require.NoError(t, err)
	assert.Equal(t, newSave, restored)
	restoredConfigPath := filepath.Join(env.ConfigDir, config.CfgFile)
	// #nosec G304 -- restoredConfigPath is under a test-owned temporary root.
	restoredConfigData, err := os.ReadFile(restoredConfigPath)
	require.NoError(t, err)
	restoredConfig := &config.Instance{}
	require.NoError(t, restoredConfig.LoadTOML(string(restoredConfigData)))
	assert.Equal(t, env.Manager.cfg.DeviceID(), restoredConfig.DeviceID())
	assert.NotContains(t, string(restoredConfigData), "remote-source-device")
}

func remotePackOrder(files []FileRef) []string {
	out := make([]string, 0, len(files))
	for _, file := range files {
		out = append(out, file.Category+":"+file.RestorePath)
	}
	return out
}

func configureRemoteTestAuth(t *testing.T, mgr *Manager, baseURL string) {
	t.Helper()
	require.NoError(t, mgr.cfg.SetBackupRemoteBaseURL(baseURL))
	mgr.cfg.SetBackupRemoteEnabled(true)
	config.SetAuthCfgForTesting(map[string]config.CredentialEntry{
		config.BackupAuthLookupURL(baseURL): {Bearer: "test-token"},
	})
	t.Cleanup(func() { config.ClearAuthCfgForTesting() })
}

func parseTestPack(t *testing.T, body []byte) map[string][]byte {
	t.Helper()
	require.GreaterOrEqual(t, len(body), 4)
	footerLen := int(binary.BigEndian.Uint32(body[len(body)-4:]))
	footerStart := len(body) - 4 - footerLen
	require.GreaterOrEqual(t, footerStart, 0)
	var footer []packFooterEntry
	require.NoError(t, json.Unmarshal(body[footerStart:len(body)-4], &footer))
	out := make(map[string][]byte, len(footer))
	for _, entry := range footer {
		payload := body[entry.Offset : entry.Offset+entry.Length]
		assert.Equal(t, entry.Hash, sha256Hex(payload))
		out[entry.Hash] = payload
	}
	return out
}

func testStringPointer(value string) *string { return &value }

// testPackFixture is one server-side pack over a set of payloads, in the
// exact wire format the pack restore protocol serves: concatenated file
// bytes + JSON footer + 4-byte footer-length trailer.
type testPackFixture struct {
	body     []byte
	packHash string
	objects  []remotePackObjectRef
}

func newTestPackFixture(t *testing.T, payloads ...[]byte) *testPackFixture {
	t.Helper()
	var buf bytes.Buffer
	refs := make([]remotePackObjectRef, 0, len(payloads))
	entries := make([]packFooterEntry, 0, len(payloads))
	seen := make(map[string]struct{}, len(payloads))
	for _, payload := range payloads {
		hash := sha256Hex(payload)
		if _, dup := seen[hash]; dup {
			continue
		}
		seen[hash] = struct{}{}
		offset := int64(buf.Len())
		refs = append(refs, remotePackObjectRef{Hash: hash, Offset: offset, Length: int64(len(payload))})
		entries = append(entries, packFooterEntry{Hash: hash, Offset: offset, Length: int64(len(payload))})
		_, _ = buf.Write(payload)
	}
	footer, err := json.Marshal(entries)
	require.NoError(t, err)
	_, _ = buf.Write(footer)
	var trailer [4]byte
	//nolint:gosec // test footers are tiny, far below uint32 range.
	binary.BigEndian.PutUint32(trailer[:], uint32(len(footer)))
	_, _ = buf.Write(trailer[:])
	return &testPackFixture{
		body: buf.Bytes(), packHash: sha256Hex(buf.Bytes()), objects: refs,
	}
}

// locateResponse resolves the requested hashes against this pack, listing
// unknown hashes as missing — what a real locate endpoint would return for
// a single-pack account.
func (f *testPackFixture) locateResponse(hashes []string) remoteLocateResponse {
	have := make(map[string]remotePackObjectRef, len(f.objects))
	for _, ref := range f.objects {
		have[ref.Hash] = ref
	}
	resp := remoteLocateResponse{Missing: []string{}}
	pack := remotePackObjects{PackHash: f.packHash, SizeBytes: int64(len(f.body))}
	for _, hash := range hashes {
		if ref, ok := have[hash]; ok {
			pack.Objects = append(pack.Objects, ref)
		} else {
			resp.Missing = append(resp.Missing, hash)
		}
	}
	if len(pack.Objects) > 0 {
		resp.Packs = append(resp.Packs, pack)
	}
	return resp
}

// handleLocate decodes a locate request and answers it from this pack.
func (f *testPackFixture) handleLocate(t *testing.T, w http.ResponseWriter, r *http.Request) {
	t.Helper()
	var req remoteLocateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		t.Errorf("decoding locate request: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	assert.NotContains(t, req.Hashes, remoteEmptyContentSHA256,
		"the empty-content hash must never be located")
	writeJSON(t, w, f.locateResponse(req.Hashes))
}

func testCommittedRemoteResponse(
	t *testing.T, id, platform string, request *remoteSnapshotRequest,
) remoteBackupResponse {
	t.Helper()
	// Canonicalize exactly like the server's commit handler so the mock's
	// manifest_hash matches what a real server would record.
	manifestData, manifestHash, err := canonicalRemoteManifest(request.Categories)
	require.NoError(t, err)
	manifest := &remoteManifest{Format: remoteManifestFormat, Categories: request.Categories}
	files := remoteManifestFiles(manifest)
	var size int64
	for _, file := range files {
		size += file.Size
	}
	return remoteBackupResponse{
		ID: id, BackupType: request.BackupType, SchemaVersion: request.SchemaVersion,
		CoreVersion: request.CoreVersion, Platform: testStringPointer(platform),
		ManifestHash: manifestHash, SizeBytes: size,
		Categories: remoteCategorySummaries(files), Manifest: manifestData, CreatedAt: time.Now().UTC(),
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	encodeErr := json.NewEncoder(w).Encode(value)
	require.NoError(t, encodeErr)
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func writeTestFile(t *testing.T, filePath, contents string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(filePath), 0o750))
	require.NoError(t, os.WriteFile(filePath, []byte(contents), 0o600))
}

func corruptZipPayload(t *testing.T, zipPath, target, contents string) {
	t.Helper()
	zr, err := zip.OpenReader(zipPath)
	require.NoError(t, err)
	entries := make(map[string][]byte, len(zr.File))
	for _, file := range zr.File {
		r, openErr := file.Open()
		require.NoError(t, openErr)
		body, readErr := io.ReadAll(r)
		require.NoError(t, readErr)
		require.NoError(t, r.Close())
		if file.Name == target {
			body = []byte(contents)
		}
		entries[file.Name] = body
	}
	require.NoError(t, zr.Close())

	// #nosec G304 -- test helper writes only paths created by individual tests.
	out, err := os.Create(zipPath)
	require.NoError(t, err)
	zw := zip.NewWriter(out)
	for name, body := range entries {
		w, createErr := zw.Create(name)
		require.NoError(t, createErr)
		_, writeErr := w.Write(body)
		require.NoError(t, writeErr)
	}
	require.NoError(t, zw.Close())
	require.NoError(t, out.Close())
}

func writeRawZip(t *testing.T, zipPath string, entries map[string]string) {
	t.Helper()
	// #nosec G304 -- test helper writes only paths created by individual tests.
	out, err := os.Create(zipPath)
	require.NoError(t, err)
	zw := zip.NewWriter(out)
	for name, contents := range entries {
		w, createErr := zw.Create(name)
		require.NoError(t, createErr)
		_, writeErr := w.Write([]byte(contents))
		require.NoError(t, writeErr)
	}
	require.NoError(t, zw.Close())
	require.NoError(t, out.Close())
}

func TestManagerRestoreRemoteUsesOpaqueIDAsSinglePathSegment(t *testing.T) {
	env := newBackupTestEnv(t, platformids.Mister)
	const backupID = "save/a%b ?\u96ea"
	escapedBackupPath := "/v1/device/backups/" + url.PathEscape(backupID)
	decodedBackupPath := "/v1/device/backups/" + backupID
	manifestData, manifestHash, err := canonicalRemoteManifest(map[string][]remoteManifestEntry{
		CategoryZaparoo: {{Path: config.UserDbFile, SHA256: remoteEmptyContentSHA256, Size: 0}},
	})
	require.NoError(t, err)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			assert.Equal(t, decodedBackupPath, r.URL.Path)
			assert.Equal(t, escapedBackupPath, r.RequestURI)
			assert.Empty(t, r.URL.RawQuery)
			writeJSON(t, w, remoteBackupResponse{
				ID: backupID, BackupType: RemoteBackupTypeManual, SchemaVersion: remoteSchemaVersion,
				Platform: testStringPointer(platformids.Mister), ManifestHash: manifestHash,
				Categories: map[string]remoteCategorySummary{CategoryZaparoo: {Files: 1}},
				Manifest:   manifestData, CreatedAt: time.Now().UTC(),
			})
		case http.MethodPost:
			assert.Equal(t, decodedBackupPath+"/restore-complete", r.URL.Path)
			assert.Equal(t, escapedBackupPath+"/restore-complete", r.RequestURI)
			assert.Empty(t, r.URL.RawQuery)
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.RequestURI)
		}
	}))
	defer server.Close()
	configureRemoteTestAuth(t, env.Manager, server.URL)

	_, err = env.Manager.RestoreRemote(context.Background(), backupID)
	require.NoError(t, err)
}

func TestManagerRestoreRemoteRejectsMismatchedResponseIDBeforeChanges(t *testing.T) {
	env := newBackupTestEnv(t, platformids.Mister)
	localPath := filepath.Join(env.RootDir, "saves", "game.sav")
	// #nosec G304 -- localPath is under a test-owned temporary root.
	original, err := os.ReadFile(localPath)
	require.NoError(t, err)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/v1/device/backups/backup-a" {
			writeJSON(t, w, remoteBackupResponse{ID: "backup-b"})
			return
		}
		t.Fatalf("ID mismatch must precede further requests, got %s %s", r.Method, r.URL.Path)
	}))
	defer server.Close()
	configureRemoteTestAuth(t, env.Manager, server.URL)

	_, err = env.Manager.RestoreRemote(context.Background(), "backup-a")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not match requested ID")
	// #nosec G304 -- localPath is under a test-owned temporary root.
	after, readErr := os.ReadFile(localPath)
	require.NoError(t, readErr)
	assert.Equal(t, original, after)
	env.UserDB.AssertNotCalled(
		t, "BackupForTransfer", testifymock.Anything, testifymock.AnythingOfType("string"),
	)
}

func TestManagerRestoreRemoteRejectsManifestHashBeforeDownload(t *testing.T) {
	env := newBackupTestEnv(t, platformids.Mister)
	payload := []byte("remote-save\n")
	hash := sha256Hex(payload)
	manifestData, _, err := canonicalRemoteManifest(map[string][]remoteManifestEntry{
		CategorySaves: {{Path: "saves/game.sav", SHA256: hash, Size: int64(len(payload))}},
	})
	require.NoError(t, err)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/v1/device/backups/hash-test" {
			writeJSON(t, w, remoteBackupResponse{
				ID: "hash-test", BackupType: RemoteBackupTypeManual, SchemaVersion: remoteSchemaVersion,
				Platform: testStringPointer(platformids.Mister), ManifestHash: strings.Repeat("0", 64),
				SizeBytes: int64(len(payload)), Categories: map[string]remoteCategorySummary{
					CategorySaves: {Files: 1, Bytes: int64(len(payload))},
				},
				Manifest: manifestData, CreatedAt: time.Now().UTC(),
			})
			return
		}
		t.Fatalf("manifest rejection must precede object download, got %s %s", r.Method, r.URL.Path)
	}))
	defer server.Close()
	configureRemoteTestAuth(t, env.Manager, server.URL)

	_, err = env.Manager.RestoreRemote(context.Background(), "hash-test")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "manifest hash mismatch")
}

func TestManagerRestoreRemoteRefusesNewerSchema(t *testing.T) {
	env := newBackupTestEnv(t, platformids.Mister)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/device/backups/9":
			writeJSON(t, w, remoteBackupResponse{
				ID: "9", BackupType: RemoteBackupTypeManual, SchemaVersion: remoteSchemaVersion + 1,
				ManifestHash: "x", CreatedAt: time.Now().UTC(),
				Manifest: json.RawMessage(`{"format":"cas-v1","categories":{}}`),
			})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()
	configureRemoteTestAuth(t, env.Manager, server.URL)

	_, err := env.Manager.RestoreRemote(context.Background(), "9")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "newer Core")
}

func TestRemoteBackupToInfoMarksNewerSchemaIncompatible(t *testing.T) {
	t.Parallel()
	newer := remoteBackupResponse{ID: "1", SchemaVersion: remoteSchemaVersion + 1}
	assert.True(t, remoteBackupToInfo(&newer).Incompatible)
	current := remoteBackupResponse{ID: "2", SchemaVersion: remoteSchemaVersion}
	assert.False(t, remoteBackupToInfo(&current).Incompatible)
}

func TestManagerRestoreRemoteSynthesizesEmptyFiles(t *testing.T) {
	env := newBackupTestEnv(t, platformids.Mister)
	newSave := []byte("remote-save\n")
	hash := sha256Hex(newSave)
	manifestData, manifestHash, err := canonicalRemoteManifest(map[string][]remoteManifestEntry{
		CategoryZaparoo: {{Path: config.UserDbFile, SHA256: remoteEmptyContentSHA256, Size: 0}},
		CategorySaves: {
			{Path: "saves/game.sav", SHA256: hash, Size: int64(len(newSave))},
			{Path: "saves/empty.sav", SHA256: remoteEmptyContentSHA256, Size: 0},
		},
	})
	require.NoError(t, err)

	pack := newTestPackFixture(t, newSave)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/device/backups/8":
			writeJSON(t, w, remoteBackupResponse{
				ID: "8", BackupType: RemoteBackupTypeManual, SchemaVersion: remoteSchemaVersion,
				Platform:     testStringPointer(platformids.Mister),
				ManifestHash: manifestHash, SizeBytes: int64(len(newSave)),
				Categories: map[string]remoteCategorySummary{
					CategoryZaparoo: {Files: 1, Bytes: 0},
					CategorySaves:   {Files: 2, Bytes: int64(len(newSave))},
				},
				Manifest: manifestData, CreatedAt: time.Now().UTC(),
			})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/device/backup-objects/locate":
			// handleLocate asserts the empty-content hash is never requested.
			pack.handleLocate(t, w, r)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/device/backup-packs/"+pack.packHash:
			_, _ = w.Write(pack.body)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/device/backups/8/restore-complete":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()
	configureRemoteTestAuth(t, env.Manager, server.URL)

	_, err = env.Manager.RestoreRemote(context.Background(), "8")
	require.NoError(t, err)
	empty, err := os.ReadFile(filepath.Join(env.RootDir, "saves", "empty.sav"))
	require.NoError(t, err)
	assert.Empty(t, empty, "empty manifest entries restore as zero-byte files")
	restored, err := os.ReadFile(filepath.Join(env.RootDir, "saves", "game.sav"))
	require.NoError(t, err)
	assert.Equal(t, newSave, restored)
}

func TestManagerRestoreRemoteLeavesDeviceUntouchedOnDownloadFailure(t *testing.T) {
	env := newBackupTestEnv(t, platformids.Mister)
	first := []byte("first-save\n")
	second := []byte("second-save\n")
	manifestData, manifestHash, err := canonicalRemoteManifest(map[string][]remoteManifestEntry{
		CategoryZaparoo: {{Path: config.UserDbFile, SHA256: remoteEmptyContentSHA256, Size: 0}},
		CategorySaves: {
			{Path: "saves/a.sav", SHA256: sha256Hex(first), Size: int64(len(first))},
			{Path: "saves/b.sav", SHA256: sha256Hex(second), Size: int64(len(second))},
		},
	})
	require.NoError(t, err)
	pack := newTestPackFixture(t, first, second)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/device/backups/6":
			writeJSON(t, w, remoteBackupResponse{
				ID: "6", BackupType: RemoteBackupTypeManual, SchemaVersion: remoteSchemaVersion,
				Platform:     testStringPointer(platformids.Mister),
				ManifestHash: manifestHash, SizeBytes: int64(len(first) + len(second)),
				Categories: map[string]remoteCategorySummary{
					CategoryZaparoo: {Files: 1, Bytes: 0},
					CategorySaves:   {Files: 2, Bytes: int64(len(first) + len(second))},
				},
				Manifest: manifestData, CreatedAt: time.Now().UTC(),
			})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/device/backup-objects/locate":
			pack.handleLocate(t, w, r)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/device/backup-packs/"+pack.packHash:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":{"code":"not_found","message":"Pack not found"}}`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()
	configureRemoteTestAuth(t, env.Manager, server.URL)

	writeTestFile(t, filepath.Join(env.RootDir, "saves", "a.sav"), "old-a\n")
	_, err = env.Manager.RestoreRemote(context.Background(), "6")
	require.Error(t, err)

	// Everything is downloaded and verified before anything is applied, so
	// a failed download must leave the device exactly as it was.
	current, err := os.ReadFile(filepath.Join(env.RootDir, "saves", "a.sav"))
	require.NoError(t, err)
	assert.Equal(t, []byte("old-a\n"), current)
	_, err = os.Stat(filepath.Join(env.RootDir, "saves", "b.sav"))
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestManagerRestoreRemoteFailsWhenObjectsMissing(t *testing.T) {
	env := newBackupTestEnv(t, platformids.Mister)
	newSave := []byte("missing-object-save\n")
	manifestData, manifestHash, err := canonicalRemoteManifest(map[string][]remoteManifestEntry{
		CategoryZaparoo: {{Path: config.UserDbFile, SHA256: remoteEmptyContentSHA256, Size: 0}},
		CategorySaves: {
			{Path: "saves/game.sav", SHA256: sha256Hex(newSave), Size: int64(len(newSave))},
		},
	})
	require.NoError(t, err)
	// The fixture holds no payloads, so locate reports the hash missing.
	pack := newTestPackFixture(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/device/backups/9":
			writeJSON(t, w, remoteBackupResponse{
				ID: "9", BackupType: RemoteBackupTypeManual, SchemaVersion: remoteSchemaVersion,
				Platform:     testStringPointer(platformids.Mister),
				ManifestHash: manifestHash, SizeBytes: int64(len(newSave)),
				Categories: map[string]remoteCategorySummary{
					CategoryZaparoo: {Files: 1, Bytes: 0},
					CategorySaves:   {Files: 1, Bytes: int64(len(newSave))},
				},
				Manifest: manifestData, CreatedAt: time.Now().UTC(),
			})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/device/backup-objects/locate":
			pack.handleLocate(t, w, r)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()
	configureRemoteTestAuth(t, env.Manager, server.URL)

	_, err = env.Manager.RestoreRemote(context.Background(), "9")
	require.ErrorContains(t, err, "no longer available")
}

func TestManagerRunRemoteRechecksDeduplicatedSourcesBeforeCommit(t *testing.T) {
	env := newBackupTestEnv(t, platformids.Mister)
	checkCalls := 0
	commitCalls := 0
	var committed remoteSnapshotRequest
	savePath := filepath.Join(env.RootDir, "saves", "game.sav")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/device/heartbeat":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/device/me":
			writeJSON(t, w, remoteDeviceMeResponse{ID: "device-1", BackupActive: true})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/device/backup-objects/check":
			checkCalls++
			if checkCalls == 1 && !assert.NoError(
				t, os.WriteFile(savePath, []byte("changed-save\n"), 0o600),
			) {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			writeJSON(t, w, remoteCheckResponse{})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/device/backups":
			commitCalls++
			if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&committed)) {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			writeJSON(t, w, testCommittedRemoteResponse(
				t, "backup-dedup", platformids.Mister, &committed,
			))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/device/backups":
			writeJSON(t, w, remoteListResponse{})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()
	configureRemoteTestAuth(t, env.Manager, server.URL)

	info, err := env.Manager.RunRemote(context.Background(), RemoteBackupTypeManual)
	require.NoError(t, err)
	assert.Equal(t, "backup-dedup", info.Backup.ID)
	assert.Equal(t, 2, checkCalls, "source change retries the entire snapshot once")
	assert.Equal(t, 1, commitCalls, "known-stale snapshot must never be committed")
}

func TestManagerRunRemoteRetriesOnceOnIntegrityMismatch(t *testing.T) {
	env := newBackupTestEnv(t, platformids.Mister)
	heartbeats := 0
	packAttempts := 0
	var committed remoteSnapshotRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/device/heartbeat":
			heartbeats++
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/device/me":
			writeJSON(t, w, remoteDeviceMeResponse{ID: "device-1", Name: "test", BackupActive: true})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/device/backup-objects/check":
			var req remoteCheckRequest
			if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&req)) {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			writeJSON(t, w, remoteCheckResponse{Missing: req.Hashes})
		case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/v1/device/backup-packs/"):
			packAttempts++
			if packAttempts == 1 {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":{"code":"integrity_mismatch","message":"hash mismatch"}}`))
				return
			}
			body, readErr := io.ReadAll(r.Body)
			if !assert.NoError(t, readErr) {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusCreated)
			writeJSON(t, w, remotePackResponse{PackHash: sha256Hex(body), CreatedAt: time.Now().UTC()})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/device/backups":
			if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&committed)) {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			writeJSON(t, w, testCommittedRemoteResponse(
				t, "backup-2", platformids.Mister, &committed,
			))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/device/backups":
			writeJSON(t, w, remoteListResponse{StorageQuotaBytes: 1 << 30})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()
	configureRemoteTestAuth(t, env.Manager, server.URL)

	info, err := env.Manager.RunRemote(context.Background(), RemoteBackupTypeScheduled)
	require.NoError(t, err)
	assert.Equal(t, "backup-2", info.Backup.ID)
	assert.Equal(t, 2, heartbeats, "integrity mismatch re-runs the whole snapshot once")
	assert.Equal(t, RemoteBackupTypeScheduled, committed.BackupType,
		"scheduler-initiated runs commit as scheduled")
}

func TestManagerRunRemoteRepairsMissingObjectsOnce(t *testing.T) {
	env := newBackupTestEnv(t, platformids.Mister)
	checkCalls := 0
	commitCalls := 0
	packCalls := 0
	var committed remoteSnapshotRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/device/heartbeat":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/device/me":
			writeJSON(t, w, remoteDeviceMeResponse{ID: "device-1", BackupActive: true})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/device/backup-objects/check":
			checkCalls++
			var request remoteCheckRequest
			if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&request)) {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if checkCalls == 1 {
				writeJSON(t, w, remoteCheckResponse{})
			} else {
				writeJSON(t, w, remoteCheckResponse{Missing: request.Hashes})
			}
		case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/v1/device/backup-packs/"):
			packCalls++
			body, readErr := io.ReadAll(r.Body)
			if !assert.NoError(t, readErr) {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			writeJSON(t, w, remotePackResponse{PackHash: sha256Hex(body), CreatedAt: time.Now().UTC()})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/device/backups":
			commitCalls++
			if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&committed)) {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if commitCalls == 1 {
				w.WriteHeader(http.StatusConflict)
				_, _ = w.Write([]byte(`{"error":{"code":"missing_objects","message":"missing"}}`))
				return
			}
			writeJSON(t, w, testCommittedRemoteResponse(
				t, "backup-repaired", platformids.Mister, &committed,
			))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/device/backups":
			writeJSON(t, w, remoteListResponse{StorageQuotaBytes: 1 << 30})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()
	configureRemoteTestAuth(t, env.Manager, server.URL)

	info, err := env.Manager.RunRemote(context.Background(), RemoteBackupTypeManual)
	require.NoError(t, err)
	assert.Equal(t, "backup-repaired", info.Backup.ID)
	assert.Equal(t, 2, checkCalls)
	assert.Equal(t, 2, commitCalls)
	assert.Positive(t, packCalls)
}

func TestManagerRunRemoteStopsAfterSecondMissingObjectsResponse(t *testing.T) {
	env := newBackupTestEnv(t, platformids.Mister)
	commitCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/device/heartbeat":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/device/me":
			writeJSON(t, w, remoteDeviceMeResponse{ID: "device-1", BackupActive: true})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/device/backup-objects/check":
			writeJSON(t, w, remoteCheckResponse{})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/device/backups":
			commitCalls++
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"error":{"code":"missing_objects","message":"missing"}}`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()
	configureRemoteTestAuth(t, env.Manager, server.URL)

	_, err := env.Manager.RunRemote(context.Background(), RemoteBackupTypeManual)
	require.ErrorIs(t, err, errRemoteMissingObjects)
	assert.Equal(t, 2, commitCalls, "missing-object repair has one bounded recommit")
}

func TestManagerRunRemoteRejectsInvalidType(t *testing.T) {
	env := newBackupTestEnv(t, platformids.Mister)
	_, err := env.Manager.RunRemote(context.Background(), "pre_restore")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid remote backup type")
}

func TestManagerRunRemoteRefusesConcurrentRuns(t *testing.T) {
	env := newBackupTestEnv(t, platformids.Mister)
	lease, err := env.Manager.coordinator.Begin(
		context.Background(), OperationLocalCreate, OperationWrite,
	)
	require.NoError(t, err)
	defer lease.Release()

	_, err = env.Manager.RunRemote(context.Background(), RemoteBackupTypeManual)
	var busy *BusyError
	require.ErrorAs(t, err, &busy)
	assert.Equal(t, OperationLocalCreate, busy.Kind)
}

func TestManagerRunRemoteMarksUnlinkedOn401(t *testing.T) {
	env := newBackupTestEnv(t, platformids.Mister)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Device-token middleware failures return bare status codes.
		if r.Method == http.MethodPost && r.URL.Path == "/v1/device/heartbeat" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
	}))
	defer server.Close()
	configureRemoteTestAuth(t, env.Manager, server.URL)

	_, err := env.Manager.RunRemote(context.Background(), RemoteBackupTypeManual)
	require.ErrorIs(t, err, errRemoteUnlinked)

	status := env.Manager.Status()
	assert.False(t, status.Remote.Linked,
		"a 401 records revocation: linked reports false even though the credential file remains")
	assert.Equal(t, "device not linked", status.Remote.LastError)
	assert.Equal(t, RemoteAvailabilityUnknown, status.Remote.Availability)
	entry := config.LookupAuth(
		config.GetAuthCfg(), config.BackupAuthLookupURL(env.Manager.cfg.BackupRemoteBaseURL()),
	)
	require.NotNil(t, entry)
	assert.Equal(t, "test-token", entry.Bearer, "revocation retains the credential for explicit relinking")
	_, err = env.Manager.ListRemote(context.Background())
	require.ErrorIs(t, err, errRemoteUnlinked, "retained bearer must not be reused after a 401")

	env.Manager.MarkRemoteLinked()
	status = env.Manager.Status()
	assert.True(t, status.Remote.Linked, "a fresh claim clears the unlinked marker")
}

func TestUnauthorizedOldBearerDoesNotRevokeFreshCredential(t *testing.T) {
	env := newBackupTestEnv(t, platformids.Mister)
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer server.Close()
	configureRemoteTestAuth(t, env.Manager, server.URL)

	oldClient, err := env.Manager.newRemoteClient()
	require.NoError(t, err)
	env.Manager.coordinator.SetRemoteUnlinked(true)
	config.SetAuthCfgForTesting(map[string]config.CredentialEntry{
		config.BackupAuthLookupURL(server.URL): {Bearer: "fresh-token"},
	})
	env.Manager.MarkRemoteLinked()

	oldClient.onUnauthorized()
	assert.False(t, env.Manager.coordinator.RemoteUnlinked())
	status := env.Manager.Status()
	assert.True(t, status.Remote.Linked)
	entry := config.LookupAuth(config.GetAuthCfg(), config.BackupAuthLookupURL(server.URL))
	require.NotNil(t, entry)
	assert.Equal(t, "fresh-token", entry.Bearer)
}

func TestManagerRefreshRemoteAvailabilityCachesTriState(t *testing.T) {
	env := newBackupTestEnv(t, platformids.Mister)
	var backupActive atomic.Bool
	backupActive.Store(true)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/device/me" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		requests.Add(1)
		writeJSON(t, w, remoteDeviceMeResponse{
			ID: "device-1", Name: "test", BackupActive: backupActive.Load(),
		})
	}))
	defer server.Close()
	configureRemoteTestAuth(t, env.Manager, server.URL)

	availability, err := env.Manager.RefreshRemoteAvailability(context.Background())
	require.NoError(t, err)
	assert.Equal(t, RemoteAvailabilityAvailable, availability)
	status := env.Manager.Status()
	assert.Equal(t, RemoteAvailabilityAvailable, status.Remote.Availability)
	require.NotNil(t, status.Remote.AvailabilityCheckedAt)

	availability, err = env.Manager.RefreshRemoteAvailabilityIfStale(context.Background())
	require.NoError(t, err)
	assert.Equal(t, RemoteAvailabilityAvailable, availability)
	assert.Equal(t, int32(1), requests.Load(), "fresh eligibility must use the cache")

	backupActive.Store(false)
	availability, err = env.Manager.RefreshRemoteAvailability(context.Background())
	require.NoError(t, err)
	assert.Equal(t, RemoteAvailabilityUnavailable, availability)
	assert.Equal(t, RemoteAvailabilityUnavailable, env.Manager.Status().Remote.Availability)
}

func TestManagerRefreshRemoteAvailabilityPersistsDeviceIdentity(t *testing.T) {
	env := newBackupTestEnv(t, platformids.Mister)
	linkedAt := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/device/me" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		writeJSON(t, w, remoteDeviceMeResponse{
			ID: "device-1", Name: "Living Room", LinkedAt: linkedAt, BackupActive: true,
		})
	}))
	defer server.Close()
	configureRemoteTestAuth(t, env.Manager, server.URL)

	_, err := env.Manager.RefreshRemoteAvailability(context.Background())
	require.NoError(t, err)

	status := env.Manager.Status()
	require.NotNil(t, status.Remote.DeviceName)
	assert.Equal(t, "Living Room", *status.Remote.DeviceName)
	require.NotNil(t, status.Remote.LinkedAt)
	parsed, err := time.Parse(time.RFC3339Nano, *status.Remote.LinkedAt)
	require.NoError(t, err)
	assert.True(t, parsed.Equal(linkedAt))

	env.Manager.MarkRemoteUnlinked()
	status = env.Manager.Status()
	assert.Nil(t, status.Remote.DeviceName, "unlinking clears the stored device identity")
	assert.Nil(t, status.Remote.LinkedAt, "unlinking clears the stored link time")
}

func TestManagerRefreshRemoteAvailabilityIfStaleAsync(t *testing.T) {
	env := newBackupTestEnv(t, platformids.Mister)
	var requests atomic.Int32
	refreshed := make(chan struct{}, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/device/me" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		requests.Add(1)
		writeJSON(t, w, remoteDeviceMeResponse{ID: "device-1", Name: "test", BackupActive: true})
		refreshed <- struct{}{}
	}))
	defer server.Close()

	// Not linked: no background refresh is spawned.
	env.Manager.RefreshRemoteAvailabilityIfStaleAsync()

	configureRemoteTestAuth(t, env.Manager, server.URL)
	env.Manager.RefreshRemoteAvailabilityIfStaleAsync()
	select {
	case <-refreshed:
	case <-time.After(5 * time.Second):
		t.Fatal("expected a background availability refresh")
	}
	require.Eventually(t, func() bool {
		return env.Manager.Status().Remote.Availability == RemoteAvailabilityAvailable
	}, 2*time.Second, 10*time.Millisecond, "background refresh persists availability")
	assert.Equal(t, int32(1), requests.Load(), "the unlinked call must not reach the server")

	// A fresh cache short-circuits without spawning another refresh.
	env.Manager.RefreshRemoteAvailabilityIfStaleAsync()
	assert.Never(t, func() bool { return requests.Load() > 1 },
		300*time.Millisecond, 50*time.Millisecond, "fresh availability must use the cache")
}

func TestManagerRefreshRemoteAvailabilityMarksTransientFailureUnknown(t *testing.T) {
	env := newBackupTestEnv(t, platformids.Mister)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	configureRemoteTestAuth(t, env.Manager, server.URL)

	availability, err := env.Manager.RefreshRemoteAvailability(context.Background())
	require.Error(t, err)
	assert.Equal(t, RemoteAvailabilityUnknown, availability)
	status := env.Manager.Status()
	assert.Equal(t, RemoteAvailabilityUnknown, status.Remote.Availability)
	require.NotNil(t, status.Remote.AvailabilityCheckedAt)
	assert.True(t, status.Remote.Linked)
}

func TestRemoteAvailabilityNeedsRefreshUsesStateSpecificTTL(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	checkedRecently := formatTime(now.Add(-4 * time.Minute))
	checkedEarlier := formatTime(now.Add(-30 * time.Minute))
	checkedStale := formatTime(now.Add(-2 * time.Hour))

	assert.False(t, RemoteAvailabilityNeedsRefresh(now, &models.BackupStatusEntry{
		Availability: RemoteAvailabilityUnknown, AvailabilityCheckedAt: &checkedRecently,
	}))
	assert.True(t, RemoteAvailabilityNeedsRefresh(now, &models.BackupStatusEntry{
		Availability: RemoteAvailabilityUnknown, AvailabilityCheckedAt: &checkedEarlier,
	}))
	// Unavailable uses the short retry TTL too, so a subscription activated
	// after the check is picked up quickly.
	assert.False(t, RemoteAvailabilityNeedsRefresh(now, &models.BackupStatusEntry{
		Availability: RemoteAvailabilityUnavailable, AvailabilityCheckedAt: &checkedRecently,
	}))
	assert.True(t, RemoteAvailabilityNeedsRefresh(now, &models.BackupStatusEntry{
		Availability: RemoteAvailabilityUnavailable, AvailabilityCheckedAt: &checkedEarlier,
	}))
	// Only a confirmed-available result is cached for the long TTL.
	assert.False(t, RemoteAvailabilityNeedsRefresh(now, &models.BackupStatusEntry{
		Availability: RemoteAvailabilityAvailable, AvailabilityCheckedAt: &checkedEarlier,
	}))
	assert.True(t, RemoteAvailabilityNeedsRefresh(now, &models.BackupStatusEntry{
		Availability: RemoteAvailabilityAvailable, AvailabilityCheckedAt: &checkedStale,
	}))
}

func TestRemoteClientManifestResponsesExceedDefaultLimit(t *testing.T) {
	t.Parallel()
	// A manifest-bearing response larger than the general response cap must
	// still decode: commit and snapshot GET use the dedicated larger limit.
	bigManifest := bytes.Repeat([]byte(`{"pad":"xxxxxxxxxxxxxxxx"},`), 80_000) // ~1.8 MiB
	manifest := append([]byte(`[`), bigManifest...)
	manifest = append(manifest, []byte(`{"pad":"end"}]`)...)
	require.Greater(t, len(manifest), int(helpers.MaxResponseBodySize))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/v1/device/backups/3" {
			writeJSON(t, w, remoteBackupResponse{
				ID: "backup-3", SchemaVersion: remoteSchemaVersion, Manifest: json.RawMessage(manifest),
				CreatedAt: time.Now().UTC(),
			})
			return
		}
		t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
	}))
	defer server.Close()

	client := &remoteClient{httpClient: server.Client(), baseURL: server.URL, bearer: "t", platform: "test"}

	var small remoteBackupResponse
	err := client.doJSON(context.Background(), http.MethodGet, "/v1/device/backups/3", nil, &small)
	require.Error(t, err, "the default limit truncates manifest-bearing responses")

	var full remoteBackupResponse
	err = client.doJSONLimit(
		context.Background(), http.MethodGet, "/v1/device/backups/3", nil, &full, remoteManifestResponseLimit,
	)
	require.NoError(t, err)
	assert.Equal(t, "backup-3", full.ID)
	assert.Len(t, full.Manifest, len(manifest))
}

func TestValidateRemoteManifestResponse_AcceptsJSONBReserializedManifest(t *testing.T) {
	t.Parallel()
	saveHash := strings.Repeat("a", 64)
	dbHash := strings.Repeat("b", 64)
	_, manifestHash, err := canonicalRemoteManifest(map[string][]remoteManifestEntry{
		CategoryZaparoo: {
			{Path: "config.toml", SHA256: saveHash, Size: 3},
			{Path: "user.db", SHA256: dbHash, Size: 5},
		},
	})
	require.NoError(t, err)

	// Postgres JSONB does not preserve the committed bytes: keys are
	// reordered (shortest first) and whitespace is inserted. The client
	// must still accept this by recomputing the canonical hash.
	jsonbManifest := `{"categories": {"zaparoo": [` +
		`{"path": "config.toml", "size": 3, "sha256": "` + saveHash + `"}, ` +
		`{"path": "user.db", "size": 5, "sha256": "` + dbHash + `"}]}, "format": "cas-v1"}`
	resp := remoteBackupResponse{
		Platform:     testStringPointer(platformids.Mister),
		Manifest:     json.RawMessage(jsonbManifest),
		ManifestHash: manifestHash,
		SizeBytes:    8,
		Categories: map[string]remoteCategorySummary{
			CategoryZaparoo: {Files: 2, Bytes: 8},
		},
	}

	manifest, err := validateRemoteManifestResponse(&resp, platformids.Mister)
	require.NoError(t, err)
	assert.Len(t, manifest.Categories[CategoryZaparoo], 2)
}

func TestValidateRemoteManifestResponse_RejectsTamperedManifest(t *testing.T) {
	t.Parallel()
	_, manifestHash, err := canonicalRemoteManifest(map[string][]remoteManifestEntry{
		CategoryZaparoo: {{Path: "user.db", SHA256: strings.Repeat("a", 64), Size: 5}},
	})
	require.NoError(t, err)

	tampered, tamperErr := json.Marshal(remoteManifest{
		Format: remoteManifestFormat,
		Categories: map[string][]remoteManifestEntry{
			CategoryZaparoo: {{Path: "user.db", SHA256: strings.Repeat("c", 64), Size: 5}},
		},
	})
	require.NoError(t, tamperErr)
	resp := remoteBackupResponse{
		Platform:     testStringPointer(platformids.Mister),
		Manifest:     tampered,
		ManifestHash: manifestHash,
	}

	_, err = validateRemoteManifestResponse(&resp, platformids.Mister)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "manifest hash mismatch")
}

func TestManagerRecoverInterruptedRuns(t *testing.T) {
	env := newBackupTestEnv(t, platformids.Mister)
	started := formatTime(time.Now().UTC())
	require.NoError(t, env.Manager.writeLocalStatus(&statusEntry{LastRunAt: started, LastStatus: StatusRunning}))
	require.NoError(t, env.Manager.writeRemoteStatus(&statusEntry{LastRunAt: started, LastStatus: StatusRunning}))

	env.Manager.RecoverInterruptedRuns()

	status := env.Manager.Status()
	assert.Equal(t, StatusFailed, status.Local.LastStatus)
	assert.Equal(t, "backup interrupted before completion", status.Local.LastError)
	assert.Equal(t, StatusFailed, status.Remote.LastStatus)
	assert.Equal(t, "backup interrupted before completion", status.Remote.LastError)

	// Completed statuses are left untouched.
	require.NoError(t, env.Manager.writeRemoteStatus(&statusEntry{
		LastRunAt: started, LastSuccessAt: started, LastStatus: StatusSuccess,
	}))
	env.Manager.RecoverInterruptedRuns()
	status = env.Manager.Status()
	assert.Equal(t, StatusSuccess, status.Remote.LastStatus)
	assert.Empty(t, status.Remote.LastError)
}

// newRemoteBackupSuccessServer returns a server accepting a full remote
// backup run, counting every request it receives.
func newRemoteBackupSuccessServer(t *testing.T, requests *atomic.Int32) *httptest.Server {
	t.Helper()
	uploadedMu := syncutil.Mutex{}
	uploaded := make(map[string][]byte)
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/device/heartbeat":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/device/me":
			writeJSON(t, w, remoteDeviceMeResponse{ID: "device-1", Name: "test", BackupActive: true})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/device/backup-objects/check":
			var req remoteCheckRequest
			if decodeErr := json.NewDecoder(r.Body).Decode(&req); decodeErr != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			writeJSON(t, w, remoteCheckResponse{Missing: req.Hashes})
		case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/v1/device/backup-packs/"):
			body, readErr := io.ReadAll(r.Body)
			if readErr != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			uploadedMu.Lock()
			for hash, payload := range parseTestPack(t, body) {
				uploaded[hash] = payload
			}
			uploadedMu.Unlock()
			writeJSON(t, w, remotePackResponse{
				PackHash: sha256Hex(body), ObjectCount: len(uploaded), CreatedAt: time.Now().UTC(),
			})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/device/backups":
			var committed remoteSnapshotRequest
			if decodeErr := json.NewDecoder(r.Body).Decode(&committed); decodeErr != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			writeJSON(t, w, testCommittedRemoteResponse(t, "backup-1", platformids.Mister, &committed))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/device/backups":
			writeJSON(t, w, remoteListResponse{StorageUsedBytes: 1, StorageQuotaBytes: 1 << 30})
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func TestManagerRunRemotePausedBlocksUntilResumed(t *testing.T) {
	env := newBackupTestEnv(t, platformids.Mister)
	var requests atomic.Int32
	server := newRemoteBackupSuccessServer(t, &requests)
	defer server.Close()
	configureRemoteTestAuth(t, env.Manager, server.URL)

	pauser := syncutil.NewPauser()
	pauser.Pause()
	env.Manager.WithPauser(pauser)

	done := make(chan error, 1)
	go func() {
		_, err := env.Manager.RunRemote(context.Background(), RemoteBackupTypeManual)
		done <- err
	}()

	// While paused, the run must not reach the server at all.
	assert.Never(t, func() bool { return requests.Load() > 0 },
		300*time.Millisecond, 25*time.Millisecond, "paused backup must not contact the server")

	pauser.Resume()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("resumed backup did not complete")
	}
	assert.Positive(t, requests.Load())
	assert.Equal(t, StatusSuccess, env.Manager.Status().Remote.LastStatus)
}

func TestManagerRunRemoteThrottledStillCompletes(t *testing.T) {
	env := newBackupTestEnv(t, platformids.Mister)
	var requests atomic.Int32
	server := newRemoteBackupSuccessServer(t, &requests)
	defer server.Close()
	configureRemoteTestAuth(t, env.Manager, server.URL)

	pauser := syncutil.NewPauser()
	pauser.Throttle(syncutil.ThrottleLight)
	env.Manager.WithPauser(pauser)

	info, err := env.Manager.RunRemote(context.Background(), RemoteBackupTypeManual)
	require.NoError(t, err)
	assert.Equal(t, "backup-1", info.Backup.ID)
	assert.True(t, pauser.IsThrottled())
}

func TestManagerRecoverInterruptedRunsSkipsActiveOperation(t *testing.T) {
	env := newBackupTestEnv(t, platformids.Mister)
	started := formatTime(time.Now().UTC())
	require.NoError(t, env.Manager.writeRemoteStatus(&statusEntry{LastRunAt: started, LastStatus: StatusRunning}))

	// A run that began before the recovery pass owns the "running" status.
	lease, err := env.Manager.coordinator.Begin(context.Background(), OperationRemoteUpload, OperationWrite)
	require.NoError(t, err)
	env.Manager.RecoverInterruptedRuns()
	assert.Equal(t, StatusRunning, env.Manager.Status().Remote.LastStatus)

	lease.Release()
	env.Manager.RecoverInterruptedRuns()
	status := env.Manager.Status()
	assert.Equal(t, StatusFailed, status.Remote.LastStatus)
	assert.Equal(t, statusErrorInterrupted, status.Remote.LastError)
}

func testRateLimitWaits() *rateLimitWaits {
	return &rateLimitWaits{
		minWait:     time.Millisecond,
		defaultWait: time.Millisecond,
		maxWait:     5 * time.Millisecond,
	}
}

func TestParseRetryAfter(t *testing.T) {
	t.Parallel()
	assert.Equal(t, 30*time.Second, parseRetryAfter("30"))
	assert.Equal(t, 5*time.Second, parseRetryAfter(" 5 "))
	assert.Equal(t, time.Duration(0), parseRetryAfter(""))
	assert.Equal(t, time.Duration(0), parseRetryAfter("-3"))
	assert.Equal(t, time.Duration(0), parseRetryAfter("soon"))
	// HTTP-date form is valid per spec but unused by the backup server;
	// it degrades to the default wait rather than being parsed.
	assert.Equal(t, time.Duration(0), parseRetryAfter("Wed, 21 Oct 2026 07:28:00 GMT"))
}

func TestRemoteStatusErrorRateLimited(t *testing.T) {
	t.Parallel()

	t.Run("route-level plain-text 429 with Retry-After", func(t *testing.T) {
		t.Parallel()
		resp := &http.Response{
			StatusCode: http.StatusTooManyRequests,
			Header:     http.Header{"Retry-After": []string{"30"}},
			Body:       io.NopCloser(strings.NewReader("Too Many Requests\n")),
		}
		err := remoteStatusError(resp)
		require.ErrorIs(t, err, errRemoteRateLimited)
		var rateLimited *remoteRateLimitedError
		require.ErrorAs(t, err, &rateLimited)
		assert.Equal(t, 30*time.Second, rateLimited.retryAfter)
	})

	t.Run("handler-level JSON rate_limited without header", func(t *testing.T) {
		t.Parallel()
		resp := &http.Response{
			StatusCode: http.StatusTooManyRequests,
			Header:     http.Header{},
			Body: io.NopCloser(strings.NewReader(
				`{"error":{"code":"rate_limited","message":"Snapshot commit rate limit reached"}}`,
			)),
		}
		err := remoteStatusError(resp)
		require.ErrorIs(t, err, errRemoteRateLimited)
		var rateLimited *remoteRateLimitedError
		require.ErrorAs(t, err, &rateLimited)
		assert.Equal(t, time.Duration(0), rateLimited.retryAfter)
	})
}

func TestRemoteClientRetryRateLimited(t *testing.T) {
	t.Parallel()
	newClient := func() *remoteClient {
		return &remoteClient{retryWaits: *testRateLimitWaits()}
	}

	t.Run("waits out rate limits and succeeds", func(t *testing.T) {
		t.Parallel()
		attempts := 0
		err := newClient().retryRateLimited(context.Background(), func() error {
			attempts++
			if attempts < 3 {
				return &remoteRateLimitedError{retryAfter: time.Millisecond}
			}
			return nil
		})
		require.NoError(t, err)
		assert.Equal(t, 3, attempts)
	})

	t.Run("gives up after the attempt cap", func(t *testing.T) {
		t.Parallel()
		attempts := 0
		err := newClient().retryRateLimited(context.Background(), func() error {
			attempts++
			return &remoteRateLimitedError{retryAfter: time.Millisecond}
		})
		require.ErrorIs(t, err, errRemoteRateLimited)
		assert.Equal(t, remoteRateLimitMaxAttempts, attempts)
	})

	t.Run("other errors return immediately", func(t *testing.T) {
		t.Parallel()
		attempts := 0
		err := newClient().retryRateLimited(context.Background(), func() error {
			attempts++
			return errRemoteQuotaExceeded
		})
		require.ErrorIs(t, err, errRemoteQuotaExceeded)
		assert.Equal(t, 1, attempts)
	})

	t.Run("context cancellation interrupts the wait", func(t *testing.T) {
		t.Parallel()
		client := &remoteClient{retryWaits: rateLimitWaits{
			minWait: time.Hour, defaultWait: time.Hour, maxWait: time.Hour,
		}}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()
		attempts := 0
		err := client.retryRateLimited(ctx, func() error {
			attempts++
			return &remoteRateLimitedError{}
		})
		require.ErrorIs(t, err, context.DeadlineExceeded)
		assert.Equal(t, 1, attempts)
	})
}

func TestManagerRestoreRemoteRetriesRateLimitedPackDownload(t *testing.T) {
	env := newBackupTestEnv(t, platformids.Mister)
	env.Manager.rateLimitWaits = testRateLimitWaits()
	newSave := []byte("rate-limited-save\n")
	manifestData, manifestHash, err := canonicalRemoteManifest(map[string][]remoteManifestEntry{
		CategoryZaparoo: {{Path: config.UserDbFile, SHA256: remoteEmptyContentSHA256, Size: 0}},
		CategorySaves: {
			{Path: "saves/game.sav", SHA256: sha256Hex(newSave), Size: int64(len(newSave))},
		},
	})
	require.NoError(t, err)
	pack := newTestPackFixture(t, newSave)
	packGets := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/device/backups/11":
			writeJSON(t, w, remoteBackupResponse{
				ID: "11", BackupType: RemoteBackupTypeManual, SchemaVersion: remoteSchemaVersion,
				Platform:     testStringPointer(platformids.Mister),
				ManifestHash: manifestHash, SizeBytes: int64(len(newSave)),
				Categories: map[string]remoteCategorySummary{
					CategoryZaparoo: {Files: 1, Bytes: 0},
					CategorySaves:   {Files: 1, Bytes: int64(len(newSave))},
				},
				Manifest: manifestData, CreatedAt: time.Now().UTC(),
			})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/device/backup-objects/locate":
			pack.handleLocate(t, w, r)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/device/backup-packs/"+pack.packHash:
			packGets++
			if packGets <= 2 {
				// Route-level rate limiter shape: plain text + Retry-After.
				w.Header().Set("Retry-After", "1")
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = w.Write([]byte("Too Many Requests\n"))
				return
			}
			_, _ = w.Write(pack.body)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/device/backups/11/restore-complete":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()
	configureRemoteTestAuth(t, env.Manager, server.URL)

	_, err = env.Manager.RestoreRemote(context.Background(), "11")
	require.NoError(t, err)
	assert.Equal(t, 3, packGets, "two rate-limited responses are waited out and retried")
	restored, err := os.ReadFile(filepath.Join(env.RootDir, "saves", "game.sav"))
	require.NoError(t, err)
	assert.Equal(t, newSave, restored)
}

func TestManagerRunRemoteRetriesRateLimitedPackUpload(t *testing.T) {
	env := newBackupTestEnv(t, platformids.Mister)
	env.Manager.rateLimitWaits = testRateLimitWaits()
	putCalls := 0
	var committed remoteSnapshotRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/device/heartbeat":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/device/me":
			writeJSON(t, w, remoteDeviceMeResponse{ID: "device-1", BackupActive: true})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/device/backup-objects/check":
			var request remoteCheckRequest
			if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&request)) {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			writeJSON(t, w, remoteCheckResponse{Missing: request.Hashes})
		case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/v1/device/backup-packs/"):
			putCalls++
			if putCalls == 1 {
				w.Header().Set("Retry-After", "1")
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = w.Write([]byte("Too Many Requests\n"))
				return
			}
			body, readErr := io.ReadAll(r.Body)
			if !assert.NoError(t, readErr) {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusCreated)
			writeJSON(t, w, remotePackResponse{PackHash: sha256Hex(body), CreatedAt: time.Now().UTC()})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/device/backups":
			if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&committed)) {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			writeJSON(t, w, testCommittedRemoteResponse(
				t, "backup-rate-limited", platformids.Mister, &committed,
			))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/device/backups":
			writeJSON(t, w, remoteListResponse{StorageQuotaBytes: 1 << 30})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()
	configureRemoteTestAuth(t, env.Manager, server.URL)

	info, err := env.Manager.RunRemote(context.Background(), RemoteBackupTypeManual)
	require.NoError(t, err)
	assert.Equal(t, "backup-rate-limited", info.Backup.ID)
	assert.GreaterOrEqual(t, putCalls, 2, "the rate-limited pack upload is retried")
}

func TestManagerRunRemoteRecordsNoChangesOnDedupe(t *testing.T) {
	env := newBackupTestEnv(t, platformids.Mister)
	snapshotCreatedAt := time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC)
	verifiedAt := time.Now().UTC()
	started := time.Now().UTC().Add(-time.Second)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/device/heartbeat":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/device/me":
			writeJSON(t, w, remoteDeviceMeResponse{ID: "device-1", BackupActive: true})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/device/backup-objects/check":
			writeJSON(t, w, remoteCheckResponse{})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/device/backups":
			var committed remoteSnapshotRequest
			if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&committed)) {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			// The server dedupe returns the EXISTING snapshot: an older
			// created_at and, here, a different backup type than the
			// manual run that committed — only deduplicated responses may
			// mismatch the requested type.
			response := testCommittedRemoteResponse(t, "backup-existing", platformids.Mister, &committed)
			response.BackupType = RemoteBackupTypeScheduled
			response.CreatedAt = snapshotCreatedAt
			response.VerifiedAt = &verifiedAt
			response.Deduplicated = true
			writeJSON(t, w, response)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/device/backups":
			writeJSON(t, w, remoteListResponse{StorageQuotaBytes: 1 << 30})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()
	configureRemoteTestAuth(t, env.Manager, server.URL)

	info, err := env.Manager.RunRemote(context.Background(), RemoteBackupTypeManual)
	require.NoError(t, err,
		"a manual run deduplicating onto a scheduled snapshot must pass response validation")
	assert.True(t, info.NoChanges)
	assert.Equal(t, "backup-existing", info.Backup.ID)
	assert.True(t, info.Backup.CreatedAt.Equal(snapshotCreatedAt))
	require.NotNil(t, info.Backup.VerifiedAt)

	remote := env.Manager.Status().Remote
	assert.True(t, remote.LastRunNoChanges, "status records the run as verified-unchanged")
	require.NotNil(t, remote.LastSuccessAt)
	lastSuccess, err := time.Parse(time.RFC3339Nano, *remote.LastSuccessAt)
	require.NoError(t, err)
	assert.False(t, lastSuccess.Before(started),
		"lastSuccessAt is the run's own time, not the deduped snapshot's created_at")
	require.NotNil(t, remote.LastSnapshotCreatedAt)
	assert.Equal(t, formatTime(snapshotCreatedAt), *remote.LastSnapshotCreatedAt,
		"lastSnapshotCreatedAt preserves when the stored content last changed")
}
