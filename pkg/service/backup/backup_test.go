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
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	platformids "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/ids"
	inboxservice "github.com/ZaparooProject/zaparoo-core/v2/pkg/service/inbox"
	testinghelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	testifymock "github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

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

func (p backupPlatform) BackupDefinitions() []platforms.BackupDefinition {
	return p.definitions
}

func (p backupRestoreRootPlatform) BackupRestoreRoot() string {
	return p.restoreRoot
}

func newBackupTestEnv(t *testing.T, platformID string) backupTestEnv {
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
	mockPlatform.On("Settings").Return(platforms.Settings{
		DataDir: dataDir, ConfigDir: configDir, TempDir: filepath.Join(rootDir, "tmp"), LogDir: rootDir,
	})
	pl := backupPlatform{MockPlatform: mockPlatform, definitions: testPlatformDefinitions(rootDir)}

	userDB := testinghelpers.NewMockUserDBI()
	userDB.On("Backup", "local-zip", false).Return(database.BackupInfo{
		Name:  "snapshot.db",
		Path:  userSnapshot,
		Valid: true,
	}, nil)
	userDB.On("GetDBPath").Return(filepath.Join(dataDir, config.UserDbFile)).Maybe()
	userDB.On("RestoreBackup", testifymock.AnythingOfType("string")).Return(database.RestoreInfo{
		RestoredFrom: database.BackupInfo{Name: "staged.db", Valid: true},
	}, nil).Maybe()

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

func TestManagerCreateListRestore(t *testing.T) {
	t.Parallel()
	env := newBackupTestEnv(t, platformids.Mister)
	cfgPath := filepath.Join(env.ConfigDir, config.CfgFile)
	launcherPath := filepath.Join(env.DataDir, config.LaunchersDir, "custom.toml")
	mappingPath := filepath.Join(env.DataDir, config.MappingsDir, "tokens.toml")

	info, err := env.Manager.Create()
	require.NoError(t, err)
	assert.True(t, info.Valid)
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
	restore, err := env.Manager.Restore(info.Name)
	require.NoError(t, err)
	assert.Equal(t, info.Name, restore.RestoredFrom.Name)
	require.NotNil(t, restore.PreRestoreBackup)

	restoredSave, err := os.ReadFile(filepath.Join(env.RootDir, "saves", "game.sav"))
	require.NoError(t, err)
	assert.Equal(t, "save-data\n", string(restoredSave))
	// #nosec G304 -- test reads a path created under this test's temp directory.
	restoredCfg, err := os.ReadFile(cfgPath)
	require.NoError(t, err)
	assert.Equal(t, "debug_logging = false\n", string(restoredCfg))
	restoredLauncher, err := os.ReadFile(launcherPath) // #nosec G304 -- path belongs to test temp dir.
	require.NoError(t, err)
	assert.Equal(t, "[[launchers]]\n", string(restoredLauncher))
	restoredMapping, err := os.ReadFile(mappingPath) // #nosec G304 -- path belongs to test temp dir.
	require.NoError(t, err)
	assert.Equal(t, "[[mappings]]\n", string(restoredMapping))
	env.UserDB.AssertCalled(t, "RestoreBackup", testifymock.AnythingOfType("string"))
	stagingEntries, err := os.ReadDir(filepath.Join(env.DataDir, "backups"))
	if !errors.Is(err, os.ErrNotExist) {
		require.NoError(t, err)
		for _, entry := range stagingEntries {
			assert.NotRegexp(t, `^backup-.*-manual\.db$`, entry.Name())
		}
	}
}

func TestListUsesManifestOnlyButRestoreVerifiesPayloadHash(t *testing.T) {
	t.Parallel()
	env := newBackupTestEnv(t, platformids.Mister)

	info, err := env.Manager.Create()
	require.NoError(t, err)
	corruptZipPayload(t, info.Path, platformArchive(filepath.Join("saves", "game.sav")), "corrupt-save\n")

	backups, err := env.Manager.List()
	require.NoError(t, err)
	require.Len(t, backups, 1)
	assert.True(t, backups[0].Valid)

	_, err = env.Manager.Restore(info.Name)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "hash mismatch")
}

func TestReadAndVerifyZipRejectsUnsafeEntry(t *testing.T) {
	t.Parallel()
	zipPath := filepath.Join(t.TempDir(), "backup-20260624-150405-000000000-manual.zip")
	writeRawZip(t, zipPath, map[string]string{
		manifestName:                "{\"version\":1,\"files\":[]}",
		path.Join("..", "evil.txt"): "bad",
	})

	_, err := readAndVerifyZip(zipPath)
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

func TestCollectPlatformFilesIncludesDefinitionsAndSavestates(t *testing.T) {
	t.Parallel()
	env := newBackupTestEnv(t, platformids.Mister)

	files := collectPlatformFiles(nil, testPlatformDefinitions(env.RootDir))
	byArchive := make(map[string]FileRef, len(files))
	for _, file := range files {
		byArchive[file.ArchivePath] = file
	}

	assert.Equal(t, CategorySettings, byArchive[platformArchive("MiSTer.ini")].Category)
	assert.Equal(t, CategorySettings, byArchive[platformArchive(filepath.Join("config", "core.cfg"))].Category)
	assert.Equal(t, CategoryInputs, byArchive[platformArchive(filepath.Join("config", "inputs", "pad.map"))].Category)
	assert.Equal(t, CategorySaves, byArchive[platformArchive(filepath.Join("saves", "game.sav"))].Category)
	assert.Equal(t, CategorySavestates, byArchive[platformArchive(filepath.Join("savestates", "game.ss"))].Category)
	assert.NotContains(t, byArchive, platformArchive(filepath.Join("config", "core_recent.cfg")))
	assert.NotContains(t, byArchive, platformArchive(filepath.Join("config", "inputs", "ignored.txt")))
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

	info, err := env.Manager.Create()
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
	mockPlatform.On("Settings").Return(platforms.Settings{
		DataDir: dataDir, ConfigDir: configDir, TempDir: filepath.Join(rootDir, "tmp"), LogDir: rootDir,
	})
	pl := backupRestoreRootPlatform{
		backupPlatform: backupPlatform{MockPlatform: mockPlatform},
		restoreRoot:    platformRoot,
	}
	mgr := NewManager(cfg, pl, nil)
	manifest := &Manifest{Files: []FileRef{{
		ArchivePath: platformArchive(restorePath),
		RestorePath: filepath.ToSlash(restorePath),
		Category:    CategorySaves,
	}}}

	err = mgr.applyRestore(manifest, func(FileRef) ([]byte, error) { return []byte("save-data\n"), nil })
	require.NoError(t, err)
	assert.FileExists(t, filepath.Join(platformRoot, restorePath))
	assert.NoFileExists(t, filepath.Join(filepath.Dir(dataDir), restorePath))
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
			writeJSON(t, w, remoteDeviceMeResponse{ID: 1, Name: "test", BackupActive: true})
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
			manifestData, err := json.Marshal(remoteManifest{
				Format: "cas-v1", Categories: committed.Categories,
			})
			if !assert.NoError(t, err) {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			writeJSON(t, w, remoteBackupResponse{
				ID: 1, BackupType: committed.BackupType, SchemaVersion: committed.SchemaVersion,
				ManifestHash: sha256Hex(manifestData), SizeBytes: 123,
				Manifest: manifestData, CreatedAt: time.Now().UTC(),
			})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/device/backups":
			writeJSON(t, w, remoteListResponse{StorageUsedBytes: 99, StorageQuotaBytes: 1000})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()
	configureRemoteTestAuth(t, env.Manager, server.URL)

	info, err := env.Manager.RunRemote(context.Background(), RemoteBackupTypeManual)
	require.NoError(t, err)
	assert.Equal(t, int64(1), info.Backup.ID)
	assert.Positive(t, info.UploadedPacks)
	assert.Contains(t, committed.Categories, CategorySavestates)
	assert.NotEmpty(t, committed.Categories[CategorySavestates])
	assert.Equal(t, int64(99), info.StorageUsedBytes)
	status := env.Manager.Status()
	assert.Equal(t, StatusSuccess, status.Remote.LastStatus)
	assert.Positive(t, status.Remote.Categories[CategorySavestates].Files)
}

func TestRemoteUploadMissingSkipsOversizedFile(t *testing.T) {
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
	files := []FileRef{{
		SourcePath:  sourcePath,
		Category:    CategorySaves,
		RestorePath: "saves/large.sav",
		SHA256:      hash,
		Size:        remoteMaxPackBytes,
	}}
	missing := map[string]struct{}{hash: {}}

	result, err := client.uploadMissing(context.Background(), files, missing)
	require.NoError(t, err)
	assert.Zero(t, result.packs)
	assert.Zero(t, result.bytesUploaded)
	require.Len(t, result.skipped, 1)
	assert.Equal(t, "saves/large.sav", result.skipped[0].RestorePath)

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
		SourcePath:  sourcePath,
		Category:    CategorySaves,
		RestorePath: "saves/empty.sav",
		SHA256:      remoteEmptyContentSHA256,
		Size:        0,
	}}
	missing := map[string]struct{}{remoteEmptyContentSHA256: {}}

	result, err := client.uploadMissing(context.Background(), files, missing)
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
			writeJSON(t, w, remoteDeviceMeResponse{ID: 1, Name: "test", BackupActive: true})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/device/backup-objects/check":
			var req remoteCheckRequest
			decodeErr := json.NewDecoder(r.Body).Decode(&req)
			if !assert.NoError(t, decodeErr) {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			writeJSON(t, w, remoteCheckResponse{Missing: req.Hashes})
		case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/v1/device/backup-packs/"):
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{
				"error":{"code":"quota_exceeded","message":"Backup storage quota exceeded"}
			}`))
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

func TestManagerRestoreRemoteDownloadsObjects(t *testing.T) {
	env := newBackupTestEnv(t, platformids.Mister)
	newSave := []byte("remote-save\n")
	hash := sha256Hex(newSave)
	manifestData, err := json.Marshal(remoteManifest{
		Format: "cas-v1",
		Categories: map[string][]remoteManifestEntry{
			CategorySaves: {{Path: "saves/game.sav", SHA256: hash, Size: int64(len(newSave))}},
		},
	})
	require.NoError(t, err)
	restoreComplete := false

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/device/backups/7":
			writeJSON(t, w, remoteBackupResponse{
				ID: 7, BackupType: RemoteBackupTypeManual, SchemaVersion: remoteSchemaVersion,
				ManifestHash: sha256Hex(manifestData), SizeBytes: int64(len(newSave)),
				Manifest: manifestData, CreatedAt: time.Now().UTC(),
			})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/device/backup-objects/"+hash:
			_, _ = w.Write(newSave)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/device/backups/7/restore-complete":
			restoreComplete = true
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()
	configureRemoteTestAuth(t, env.Manager, server.URL)

	writeTestFile(t, filepath.Join(env.RootDir, "saves", "game.sav"), "old-save\n")
	restore, err := env.Manager.RestoreRemote(context.Background(), 7)
	require.NoError(t, err)
	assert.Equal(t, int64(7), restore.RestoredFrom.ID)
	assert.True(t, restoreComplete)
	restored, err := os.ReadFile(filepath.Join(env.RootDir, "saves", "game.sav"))
	require.NoError(t, err)
	assert.Equal(t, newSave, restored)
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

func TestManagerRestoreRemoteRefusesNewerSchema(t *testing.T) {
	env := newBackupTestEnv(t, platformids.Mister)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/device/backups/9":
			writeJSON(t, w, remoteBackupResponse{
				ID: 9, BackupType: RemoteBackupTypeManual, SchemaVersion: remoteSchemaVersion + 1,
				ManifestHash: "x", CreatedAt: time.Now().UTC(),
				Manifest: json.RawMessage(`{"format":"cas-v1","categories":{}}`),
			})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()
	configureRemoteTestAuth(t, env.Manager, server.URL)

	_, err := env.Manager.RestoreRemote(context.Background(), 9)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "newer Core")
}

func TestRemoteBackupToInfoMarksNewerSchemaIncompatible(t *testing.T) {
	t.Parallel()
	newer := remoteBackupResponse{ID: 1, SchemaVersion: remoteSchemaVersion + 1}
	assert.True(t, remoteBackupToInfo(&newer).Incompatible)
	current := remoteBackupResponse{ID: 2, SchemaVersion: remoteSchemaVersion}
	assert.False(t, remoteBackupToInfo(&current).Incompatible)
}

func TestManagerRestoreRemoteSynthesizesEmptyFiles(t *testing.T) {
	env := newBackupTestEnv(t, platformids.Mister)
	newSave := []byte("remote-save\n")
	hash := sha256Hex(newSave)
	manifestData, err := json.Marshal(remoteManifest{
		Format: "cas-v1",
		Categories: map[string][]remoteManifestEntry{
			CategorySaves: {
				{Path: "saves/game.sav", SHA256: hash, Size: int64(len(newSave))},
				{Path: "saves/empty.sav", SHA256: remoteEmptyContentSHA256, Size: 0},
			},
		},
	})
	require.NoError(t, err)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/device/backups/8":
			writeJSON(t, w, remoteBackupResponse{
				ID: 8, BackupType: RemoteBackupTypeManual, SchemaVersion: remoteSchemaVersion,
				ManifestHash: sha256Hex(manifestData), SizeBytes: int64(len(newSave)),
				Manifest: manifestData, CreatedAt: time.Now().UTC(),
			})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/device/backup-objects/"+hash:
			_, _ = w.Write(newSave)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/device/backup-objects/"+remoteEmptyContentSHA256:
			t.Error("the empty object must never be downloaded")
		case r.Method == http.MethodPost && r.URL.Path == "/v1/device/backups/8/restore-complete":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()
	configureRemoteTestAuth(t, env.Manager, server.URL)

	_, err = env.Manager.RestoreRemote(context.Background(), 8)
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
	manifestData, err := json.Marshal(remoteManifest{
		Format: "cas-v1",
		Categories: map[string][]remoteManifestEntry{
			CategorySaves: {
				{Path: "saves/a.sav", SHA256: sha256Hex(first), Size: int64(len(first))},
				{Path: "saves/b.sav", SHA256: sha256Hex(second), Size: int64(len(second))},
			},
		},
	})
	require.NoError(t, err)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/device/backups/6":
			writeJSON(t, w, remoteBackupResponse{
				ID: 6, BackupType: RemoteBackupTypeManual, SchemaVersion: remoteSchemaVersion,
				ManifestHash: sha256Hex(manifestData), Manifest: manifestData, CreatedAt: time.Now().UTC(),
			})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/device/backup-objects/"+sha256Hex(first):
			_, _ = w.Write(first)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/device/backup-objects/"+sha256Hex(second):
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":{"code":"not_found","message":"Object not found"}}`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()
	configureRemoteTestAuth(t, env.Manager, server.URL)

	writeTestFile(t, filepath.Join(env.RootDir, "saves", "a.sav"), "old-a\n")
	_, err = env.Manager.RestoreRemote(context.Background(), 6)
	require.Error(t, err)

	// Everything is downloaded and verified before anything is applied, so
	// a failed download must leave the device exactly as it was.
	current, err := os.ReadFile(filepath.Join(env.RootDir, "saves", "a.sav"))
	require.NoError(t, err)
	assert.Equal(t, []byte("old-a\n"), current)
	_, err = os.Stat(filepath.Join(env.RootDir, "saves", "b.sav"))
	require.ErrorIs(t, err, os.ErrNotExist)
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
			writeJSON(t, w, remoteDeviceMeResponse{ID: 1, Name: "test", BackupActive: true})
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
			manifestData, marshalErr := json.Marshal(remoteManifest{
				Format: "cas-v1", Categories: committed.Categories,
			})
			if !assert.NoError(t, marshalErr) {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			writeJSON(t, w, remoteBackupResponse{
				ID: 2, BackupType: committed.BackupType, SchemaVersion: committed.SchemaVersion,
				ManifestHash: sha256Hex(manifestData), Manifest: manifestData, CreatedAt: time.Now().UTC(),
			})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/device/backups":
			writeJSON(t, w, remoteListResponse{})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()
	configureRemoteTestAuth(t, env.Manager, server.URL)

	info, err := env.Manager.RunRemote(context.Background(), RemoteBackupTypeScheduled)
	require.NoError(t, err)
	assert.Equal(t, int64(2), info.Backup.ID)
	assert.Equal(t, 2, heartbeats, "integrity mismatch re-runs the whole snapshot once")
	assert.Equal(t, RemoteBackupTypeScheduled, committed.BackupType,
		"scheduler-initiated runs commit as scheduled")
}

func TestManagerRunRemoteRejectsInvalidType(t *testing.T) {
	env := newBackupTestEnv(t, platformids.Mister)
	_, err := env.Manager.RunRemote(context.Background(), "pre_restore")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid remote backup type")
}

func TestManagerRunRemoteRefusesConcurrentRuns(t *testing.T) {
	env := newBackupTestEnv(t, platformids.Mister)
	remoteRunActive.Store(true)
	defer remoteRunActive.Store(false)
	_, err := env.Manager.RunRemote(context.Background(), RemoteBackupTypeManual)
	require.ErrorIs(t, err, errRemoteBackupRunning)
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

	env.Manager.MarkRemoteLinked()
	status = env.Manager.Status()
	assert.True(t, status.Remote.Linked, "a fresh claim clears the unlinked marker")
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
				ID: 3, SchemaVersion: remoteSchemaVersion, Manifest: json.RawMessage(manifest),
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
	assert.Equal(t, int64(3), full.ID)
	assert.Len(t, full.Manifest, len(manifest))
}
