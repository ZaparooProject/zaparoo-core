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

package online_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/methods"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/backup"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/librarysync"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/remote"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/fakes/online"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/scantest"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	testifymock "github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// These tests drive Core's own clients against the fake, so a change on
// either side that breaks the other fails here. None run in parallel: Core
// keeps the device credentials and the link session in package state.

const (
	testDeviceID = "dev-sim"
	testPlatform = "test-platform"
	testBearer   = "zpd1_contract" //nolint:gosec // Test fixture, not a credential.
)

// startFake serves a fresh fake and returns it with its base URL.
func startFake(t *testing.T, opts online.Options) (srv *online.Server, baseURL string) {
	t.Helper()
	srv = online.New(opts)
	httpServer := httptest.NewServer(srv.Handler())
	t.Cleanup(func() {
		// Closing the fake first releases any request a fault is holding.
		require.NoError(t, srv.Close())
		httpServer.Close()
	})
	return srv, httpServer.URL
}

// linkSeeded links a device on the fake and gives Core its bearer, the way a
// sim pre-seeds auth.toml.
func linkSeeded(t *testing.T, srv *online.Server, baseURL string) {
	t.Helper()
	_, err := srv.SeedDevice(testDeviceID, testPlatform, testBearer)
	require.NoError(t, err)
	config.SetAuthCfgForTesting(map[string]config.CredentialEntry{
		config.RemoteAuthLookupURL(baseURL): {Bearer: testBearer},
	})
	t.Cleanup(config.ClearAuthCfgForTesting)
}

func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))
}

// newBackupManager builds a backup manager over a small Zaparoo data
// directory, linked to the fake with cloud backup on.
func newBackupManager(t *testing.T, srv *online.Server, baseURL string) *backup.Manager {
	t.Helper()
	root := t.TempDir()
	dataDir := filepath.Join(root, "zaparoo")
	configDir := filepath.Join(root, "config")
	cfg, err := config.NewConfig(configDir, config.BaseDefaults)
	require.NoError(t, err)
	writeFile(t, filepath.Join(configDir, config.CfgFile), "debug_logging = false\n")
	writeFile(t, filepath.Join(dataDir, config.MappingsDir, "tokens.toml"), "[[mappings]]\n")
	snapshot := filepath.Join(root, "user-snapshot.db")
	writeFile(t, snapshot, "user-db-snapshot\n")

	platform := mocks.NewMockPlatform()
	platform.On("ID").Return(testPlatform)
	platform.On("RootDirs", testifymock.Anything).Return([]string{root}).Maybe()
	platform.On("Settings").Return(platforms.Settings{
		DataDir: dataDir, ConfigDir: configDir, TempDir: filepath.Join(root, "tmp"), LogDir: root,
	})

	userDB := testhelpers.NewMockUserDBI()
	userDB.On("BackupForTransfer", testifymock.Anything, testifymock.AnythingOfType("string")).
		Return(database.BackupInfo{Name: "snapshot.db", Path: snapshot, Valid: true}, func() error { return nil }, nil).
		Maybe()
	userDB.On("Backup", "restore-rollback", false).
		Return(database.BackupInfo{Name: "rollback.db", Path: snapshot, Valid: true}, nil).Maybe()
	userDB.On("GetDBPath").Return(filepath.Join(dataDir, config.UserDbFile)).Maybe()
	userDB.On("RestoreBackup", testifymock.AnythingOfType("string")).
		Return(database.RestoreInfo{RestoredFrom: database.BackupInfo{Name: "staged.db", Valid: true}}, nil).Maybe()
	userDB.On("ListClients").Return([]database.Client(nil), nil).Maybe()
	userDB.On("ReplaceAllClients", testifymock.Anything).Return(nil).Maybe()

	require.NoError(t, cfg.SetOnlineBaseURL(baseURL))
	cfg.SetBackupRemoteEnabled(true)
	linkSeeded(t, srv, baseURL)
	return backup.NewManager(cfg, platform, &database.Database{UserDB: userDB})
}

func waitForLinkStatus(t *testing.T, want string) models.AuthLinkStatusResponse {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		result, err := methods.HandleSettingsAuthLinkStatus(requests.RequestEnv{})
		require.NoError(t, err)
		status, ok := result.(models.AuthLinkStatusResponse)
		require.True(t, ok)
		if status.Status == want {
			return status
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("link status never reached %s", want)
	return models.AuthLinkStatusResponse{}
}

func TestContractLinkFlow(t *testing.T) {
	tests := []struct {
		decide     func(*online.Server) (online.LinkRequest, error)
		name       string
		wantStatus string
		wantLinked bool
	}{
		{
			name:       "approved",
			decide:     func(srv *online.Server) (online.LinkRequest, error) { return srv.ApproveLink("") },
			wantStatus: models.AuthLinkStatusApproved,
			wantLinked: true,
		},
		{
			name:       "denied",
			decide:     func(srv *online.Server) (online.LinkRequest, error) { return srv.DenyLink("") },
			wantStatus: models.AuthLinkStatusFailed,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, baseURL := startFake(t, online.Options{})
			config.SetAuthCfgForTesting(map[string]config.CredentialEntry{})
			t.Cleanup(config.ClearAuthCfgForTesting)

			cfg, err := config.NewConfigWithFs(t.TempDir(), config.BaseDefaults, afero.NewMemMapFs())
			require.NoError(t, err)
			require.NoError(t, cfg.SetOnlineBaseURL(baseURL))
			platform := mocks.NewMockPlatform()
			platform.On("ID").Return(testPlatform).Maybe()
			platform.On("Settings").Return(platforms.Settings{DataDir: t.TempDir(), ConfigDir: t.TempDir()})
			st, _ := state.NewState(platform, "contract-boot")
			t.Cleanup(st.StopService)

			params, err := json.Marshal(models.SettingsAuthLinkParams{URL: baseURL})
			require.NoError(t, err)
			env := requests.RequestEnv{
				Context: context.Background(), Config: cfg, Platform: platform, State: st,
				Params: params, IsLocal: true,
			}
			fetchWK := func(string) (*zapscript.WellKnown, error) { return zapscript.FetchWellKnown(baseURL) }
			result, err := methods.HandleSettingsAuthLink(env, fetchWK)
			require.NoError(t, err)
			started, ok := result.(models.AuthLinkStatusResponse)
			require.True(t, ok)
			assert.Equal(t, models.AuthLinkStatusPending, started.Status)

			pending := srv.LinkRequests()
			require.Len(t, pending, 1)
			assert.Equal(t, started.UserCode, pending[0].UserCode)
			assert.Equal(t, testPlatform, pending[0].Platform, "Core sends its platform header")

			_, err = tt.decide(srv)
			require.NoError(t, err)
			waitForLinkStatus(t, tt.wantStatus)

			entry := config.LookupAuth(config.GetAuthCfg(), config.RemoteAuthLookupURL(baseURL))
			if !tt.wantLinked {
				assert.Nil(t, entry, "a denied link stores no credential")
				assert.Empty(t, srv.Devices())
				return
			}
			require.NotNil(t, entry)
			devices := srv.Devices()
			require.Len(t, devices, 1)
			assert.True(t, devices[0].Linked)
			assert.Equal(t, online.LinkClaimed, srv.LinkRequests()[0].Status)

			// The stored bearer is the one the fake issued: a device call
			// with it is accepted.
			mgr := newLinkedManager(t, cfg)
			require.NoError(t, mgr.SendHeartbeat(context.Background()))
		})
	}
}

// newLinkedManager builds a bare manager for a config whose
// credential is already stored.
func newLinkedManager(t *testing.T, cfg *config.Instance) *backup.Manager {
	t.Helper()
	platform := mocks.NewMockPlatform()
	platform.On("ID").Return(testPlatform)
	platform.On("Settings").Return(platforms.Settings{DataDir: t.TempDir(), ConfigDir: t.TempDir()}).Maybe()
	return backup.NewManager(cfg, platform, &database.Database{})
}

func TestContractHeartbeat(t *testing.T) {
	srv, baseURL := startFake(t, online.Options{})
	mgr := newBackupManager(t, srv, baseURL)

	require.NoError(t, mgr.SendHeartbeat(context.Background()))
	devices := srv.Devices()
	require.Len(t, devices, 1)
	require.NotNil(t, devices[0].LastHeartbeatAt)
	assert.NotEmpty(t, devices[0].Capabilities)
	assert.NotEmpty(t, devices[0].CoreVersion)

	availability, err := mgr.RefreshRemoteAvailability(context.Background())
	require.NoError(t, err)
	assert.Equal(t, backup.RemoteAvailabilityAvailable, availability)

	require.NoError(t, srv.SetAccount(online.Account{}))
	availability, err = mgr.RefreshRemoteAvailability(context.Background())
	require.NoError(t, err)
	assert.NotEqual(t, backup.RemoteAvailabilityAvailable, availability, "a non-premium account has no backup")
}

func TestContractBackupUploadAndRestore(t *testing.T) {
	srv, baseURL := startFake(t, online.Options{})
	mgr := newBackupManager(t, srv, baseURL)
	ctx := context.Background()

	run, err := mgr.RunRemote(ctx, backup.RemoteBackupTypeManual)
	require.NoError(t, err)
	assert.NotEmpty(t, run.Backup.ID)
	assert.Positive(t, run.UploadedPacks)
	backups := srv.Backups()
	require.Len(t, backups, 1)
	assert.Equal(t, run.Backup.ID, backups[0].ID)
	assert.Equal(t, testDeviceID, backups[0].DeviceID)

	again, err := mgr.RunRemote(ctx, backup.RemoteBackupTypeManual)
	require.NoError(t, err)
	assert.Zero(t, again.UploadedPacks, "an unchanged run uploads nothing")
	assert.Len(t, srv.Backups(), 1, "an unchanged run makes no new snapshot")

	list, err := mgr.ListRemote(ctx)
	require.NoError(t, err)
	require.Len(t, list.Items, 1)
	assert.Equal(t, online.StandardQuotaBytes, list.StorageQuotaBytes)
	assert.Positive(t, list.StorageUsedBytes)

	restored, err := mgr.RestoreRemote(ctx, run.Backup.ID)
	require.NoError(t, err)
	assert.Equal(t, run.Backup.ID, restored.RestoredFrom.ID)
	require.NotNil(t, srv.Backups()[0].RestoredAt, "Core reports the restore complete")
}

func TestContractBackupQuota(t *testing.T) {
	srv, baseURL := startFake(t, online.Options{})
	mgr := newBackupManager(t, srv, baseURL)
	require.NoError(t, srv.SetAccount(online.Account{Premium: true, Limited: true}))

	list, err := mgr.ListRemote(context.Background())
	require.NoError(t, err)
	assert.Equal(t, online.LimitedQuotaBytes, list.StorageQuotaBytes)
}

func TestContractLibrarySync(t *testing.T) {
	srv, baseURL := startFake(t, online.Options{})
	db, cleanup := testhelpers.NewTestDatabase(t)
	t.Cleanup(cleanup)
	scantest.IndexMediaPaths(t, db.MediaDB, "NES",
		filepath.ToSlash(filepath.Join("roms", "NES", "Metroid (USA).nes")),
		filepath.ToSlash(filepath.Join("roms", "NES", "Zelda (USA).nes")),
	)
	_, err := db.MediaDB.BumpIndexGeneration()
	require.NoError(t, err)

	cfg, err := testhelpers.NewTestConfig(nil, t.TempDir())
	require.NoError(t, err)
	require.NoError(t, cfg.SetOnlineBaseURL(baseURL))
	cfg.SetLibrarySync(true)
	linkSeeded(t, srv, baseURL)
	platform := mocks.NewMockPlatform()
	platform.On("ID").Return(testPlatform)
	mgr := backup.NewManager(cfg, platform, db)
	svc := librarysync.New(&librarysync.Options{
		Config: cfg, DB: db, NewClient: mgr.NewOnlineClient, ResolvePace: time.Millisecond,
	})

	first, err := svc.SyncInventory(context.Background(), false)
	require.NoError(t, err)
	assert.Equal(t, librarysync.InventoryUploaded, first.Outcome)
	assert.Equal(t, 2, first.ItemCount)
	assert.Equal(t, 2, first.Resolved)

	again, err := svc.SyncInventory(context.Background(), true)
	require.NoError(t, err)
	assert.Equal(t, librarysync.InventoryConfirmed, again.Outcome, "the account holds what was uploaded")
}

func TestContractRemoteOperationRoundTrip(t *testing.T) {
	srv, baseURL := startFake(t, online.Options{MaxWait: 200 * time.Millisecond})
	db, cleanup := testhelpers.NewTestDatabase(t)
	t.Cleanup(cleanup)
	cfg, err := testhelpers.NewTestConfig(nil, t.TempDir())
	require.NoError(t, err)
	require.NoError(t, cfg.SetOnlineBaseURL(baseURL))
	cfg.SetRemoteControl(true)
	linkSeeded(t, srv, baseURL)
	platform := mocks.NewMockPlatform()
	platform.On("ID").Return(testPlatform)

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	t.Cleanup(func() {
		cancel()
		wg.Wait()
	})
	remote.Start(ctx, &remote.Deps{Platform: platform, Config: cfg, DB: db}, &wg)

	queued, err := srv.EnqueueOperation("echo", json.RawMessage(`{"message":"hi"}`))
	require.NoError(t, err)

	var done online.Operation
	require.Eventually(t, func() bool {
		for _, op := range srv.Operations() {
			if op.CommandID == queued.CommandID && op.CompletedAt != nil {
				done = op
				return true
			}
		}
		return false
	}, 10*time.Second, 20*time.Millisecond)
	assert.Equal(t, online.OpSucceeded, done.Status)
	assert.NotNil(t, done.DeliveredAt)
	assert.NotNil(t, done.AcceptedAt)
	assert.JSONEq(t, `{"message":"hi"}`, string(done.Result))
}

func TestContractFaults(t *testing.T) {
	tests := []struct {
		kind       string
		wantStatus int
	}{
		{kind: online.FaultUnauthorized, wantStatus: http.StatusUnauthorized},
		{kind: online.FaultForbidden, wantStatus: http.StatusForbidden},
		{kind: online.FaultRateLimited, wantStatus: http.StatusTooManyRequests},
		{kind: online.FaultServerError, wantStatus: http.StatusInternalServerError},
		{kind: online.FaultUnavailable, wantStatus: http.StatusServiceUnavailable},
		{kind: online.FaultTimeout},
		{kind: online.FaultSlow},
	}
	for _, tt := range tests {
		t.Run(tt.kind, func(t *testing.T) {
			srv, baseURL := startFake(t, online.Options{SlowDelay: 50 * time.Millisecond})
			mgr := newBackupManager(t, srv, baseURL)
			require.NoError(t, srv.SetFault(online.Fault{PathPrefix: "/v1/device/heartbeat", Kind: tt.kind}))

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if tt.kind == online.FaultTimeout {
				short, shortCancel := context.WithTimeout(ctx, 200*time.Millisecond)
				defer shortCancel()
				ctx = short
			}
			start := time.Now()
			err := mgr.SendHeartbeat(ctx)
			switch tt.kind {
			case online.FaultSlow:
				require.NoError(t, err)
				assert.GreaterOrEqual(t, time.Since(start), 50*time.Millisecond)
			case online.FaultTimeout:
				require.ErrorIs(t, err, context.DeadlineExceeded)
			case online.FaultRateLimited:
				// Core waits out Retry-After and reports the rate limit
				// once its retries run out.
				assert.True(t, backup.IsRateLimitedError(err), "got %v", err)
			default:
				apiErr, ok := backup.AsAPIError(err)
				require.True(t, ok, "Core decodes the fault as an API error: %v", err)
				assert.Equal(t, tt.wantStatus, apiErr.Status)
			}

			srv.ClearFaults()
			if tt.kind == online.FaultUnauthorized {
				// Core treats a 401 as an unlink and stops sending; the
				// fault itself left the fake's link in place.
				assert.True(t, srv.Devices()[0].Linked)
				return
			}
			require.NoError(t, mgr.SendHeartbeat(context.Background()), "clearing the fault restores service")
		})
	}
}
