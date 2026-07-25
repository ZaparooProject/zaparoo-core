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

package methods

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/permissions"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// resetAuthLinkState clears the package-level link session between tests.
func resetAuthLinkState(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		authLinkMu.Lock()
		if activeAuthLink != nil && activeAuthLink.cancel != nil {
			activeAuthLink.cancel()
		}
		activeAuthLink = nil
		authLinkStarting = false
		authLinkMu.Unlock()
	})
	authLinkMu.Lock()
	activeAuthLink = nil
	authLinkStarting = false
	authLinkMu.Unlock()
}

func readAuthLinkNotification(
	t *testing.T, notifications <-chan models.Notification,
) models.AuthLinkStatusResponse {
	t.Helper()
	select {
	case notification := <-notifications:
		require.Equal(t, models.NotificationAuthLinkStatus, notification.Method)
		var status models.AuthLinkStatusResponse
		require.NoError(t, json.Unmarshal(notification.Params, &status))
		return status
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for auth link notification")
		return models.AuthLinkStatusResponse{}
	}
}

func assertAuthLinkNotificationRedacted(t *testing.T, status *models.AuthLinkStatusResponse) {
	t.Helper()
	assert.Empty(t, status.UserCode)
	assert.Empty(t, status.VerificationURL)
	assert.Empty(t, status.VerificationURLComplete)
}

func waitForAuthLinkStatus(t *testing.T, want string, timeout time.Duration) models.AuthLinkStatusResponse {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		result, err := HandleSettingsAuthLinkStatus(requests.RequestEnv{})
		require.NoError(t, err)
		status, ok := result.(models.AuthLinkStatusResponse)
		require.True(t, ok)
		if status.Status == want {
			return status
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("link flow never reached status %q", want)
	return models.AuthLinkStatusResponse{}
}

func TestSettingsAuthLink_RequiresLocalOrAdminClient(t *testing.T) {
	// Not parallel: uses package-level link session state.
	resetAuthLinkState(t)

	_, err := HandleSettingsAuthLink(requests.RequestEnv{IsLocal: false}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "local or admin client")

	memberEnv := requests.RequestEnv{IsLocal: false, ClientRole: string(permissions.RoleMember)}
	_, err = HandleSettingsAuthLink(memberEnv, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "local or admin client")

	_, err = HandleSettingsAuthLinkCancel(requests.RequestEnv{IsLocal: false})
	require.Error(t, err)

	// A paired admin client passes the access gate; with no active flow
	// cancel reports "no active link request" instead of forbidden.
	adminEnv := requests.RequestEnv{IsLocal: false, ClientRole: string(permissions.RoleAdmin)}
	_, err = HandleSettingsAuthLinkCancel(adminEnv)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no active link request")
}

func TestSettingsAuthLinkStatus_RedactsUnpairedRemoteAndRequiresAdminForDetails(t *testing.T) {
	// Not parallel: uses package-level link session state.
	resetAuthLinkState(t)

	authLinkMu.Lock()
	activeAuthLink = &authLinkSession{status: models.AuthLinkStatusResponse{
		Status:                  models.AuthLinkStatusPending,
		UserCode:                "ABCD-1234",
		VerificationURL:         "https://online.example/link",
		VerificationURLComplete: "https://online.example/link?code=ABCD-1234",
	}}
	authLinkMu.Unlock()

	result, err := HandleSettingsAuthLinkStatus(requests.RequestEnv{})
	require.NoError(t, err)
	status, ok := result.(models.AuthLinkStatusResponse)
	require.True(t, ok)
	assert.Equal(t, models.AuthLinkStatusPending, status.Status)
	assert.Empty(t, status.UserCode)
	assert.Empty(t, status.VerificationURL)
	assert.Empty(t, status.VerificationURLComplete)

	_, err = HandleSettingsAuthLinkStatus(requests.RequestEnv{
		ClientRole: string(permissions.RoleMember),
	})
	require.ErrorIs(t, err, ErrForbidden)

	result, err = HandleSettingsAuthLinkStatus(requests.RequestEnv{
		ClientRole: string(permissions.RoleAdmin),
	})
	require.NoError(t, err)
	status, ok = result.(models.AuthLinkStatusResponse)
	require.True(t, ok)
	assert.Equal(t, "ABCD-1234", status.UserCode)
	assert.Equal(t, "https://online.example/link", status.VerificationURL)
	assert.Equal(t, "https://online.example/link?code=ABCD-1234", status.VerificationURLComplete)
}

func TestSettingsAuthLink_RejectsSecondPendingStart(t *testing.T) {
	// Not parallel: uses package-level link session state.
	resetAuthLinkState(t)

	require.NoError(t, beginAuthLinkStart())
	err := beginAuthLinkStart()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already pending")
	finishAuthLinkStart(nil)

	authLinkMu.Lock()
	activeAuthLink = &authLinkSession{status: models.AuthLinkStatusResponse{
		Status: models.AuthLinkStatusPending,
	}}
	authLinkMu.Unlock()
	err = beginAuthLinkStart()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already pending")
}

func TestSettingsAuthLink_HappyPath(t *testing.T) {
	// Not parallel: swaps package-level claimClient and link session state.
	resetAuthLinkState(t)

	var polls atomic.Int32
	var mux http.ServeMux
	server := httptest.NewServer(&mux)
	defer server.Close()

	mux.HandleFunc("POST /v1/device-link-requests", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "test-platform", r.Header.Get(zapscript.HeaderZaparooPlatform))
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(deviceLinkCreateResponse{
			DeviceCode:              "zpl1_secret",
			UserCode:                "ABCD-1234",
			VerificationURL:         "https://online.example/link",
			VerificationURLComplete: "https://online.example/link?code=ABCD1234",
			ExpiresAt:               time.Now().Add(10 * time.Minute),
			Interval:                1,
		})
	})
	mux.HandleFunc("POST /v1/device-link-requests/poll", func(w http.ResponseWriter, r *http.Request) {
		var req deviceLinkPollRequest
		if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&req)) {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		assert.Equal(t, "zpl1_secret", req.DeviceCode)
		if polls.Add(1) == 1 {
			_ = json.NewEncoder(w).Encode(deviceLinkPollResponse{Status: "pending", Interval: 1})
			return
		}
		_ = json.NewEncoder(w).Encode(deviceLinkPollResponse{
			Status:   "approved",
			Interval: 1,
			Token:    "zpc1_claim", //nolint:gosec // test fixture claim token
			ClaimURL: server.URL + "/v1/device-claims/redeem",
		})
	})
	mux.HandleFunc("POST /v1/device-claims/redeem", func(w http.ResponseWriter, r *http.Request) {
		var req claimRequest
		if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&req)) {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		assert.Equal(t, "zpc1_claim", req.Token)
		_ = json.NewEncoder(w).Encode(claimResponse{Bearer: "zpd1_device_token"}) //nolint:gosec // test fixture
	})

	origClient := claimClient
	claimClient = server.Client()
	t.Cleanup(func() { claimClient = origClient })

	config.SetAuthCfgForTesting(map[string]config.CredentialEntry{})
	t.Cleanup(config.ClearAuthCfgForTesting)

	cfg, err := config.NewConfigWithFs(t.TempDir(), config.BaseDefaults, afero.NewMemMapFs())
	require.NoError(t, err)
	require.NoError(t, cfg.SetBackupRemoteBaseURL(server.URL))

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("test-platform").Maybe()
	mockPlatform.On("Settings").Return(platforms.Settings{
		DataDir: t.TempDir(), ConfigDir: t.TempDir(),
	})
	st, notificationCh := state.NewState(mockPlatform, "test-boot")
	t.Cleanup(st.StopService)
	st.BackupCoordinator().SetRemoteUnlinked(true)

	mockFetchWK := func(_ string) (*zapscript.WellKnown, error) {
		return &zapscript.WellKnown{Auth: 1}, nil
	}

	params, err := json.Marshal(models.SettingsAuthLinkParams{URL: server.URL})
	require.NoError(t, err)
	env := requests.RequestEnv{
		Context:  context.Background(),
		Config:   cfg,
		Platform: mockPlatform,
		State:    st,
		Params:   params,
		IsLocal:  true,
	}

	result, err := HandleSettingsAuthLink(env, mockFetchWK)
	require.NoError(t, err)
	started, ok := result.(models.AuthLinkStatusResponse)
	require.True(t, ok)
	assert.Equal(t, models.AuthLinkStatusPending, started.Status)
	assert.Equal(t, "ABCD-1234", started.UserCode)
	assert.Equal(t, "https://online.example/link", started.VerificationURL)
	require.NotNil(t, started.ExpiresAt)
	pendingNotification := readAuthLinkNotification(t, notificationCh)
	assert.Equal(t, models.AuthLinkStatusPending, pendingNotification.Status)
	assertAuthLinkNotificationRedacted(t, &pendingNotification)

	approved := waitForAuthLinkStatus(t, models.AuthLinkStatusApproved, 15*time.Second)
	assert.Empty(t, approved.UserCode)
	assert.Empty(t, approved.VerificationURL)
	assert.Empty(t, approved.VerificationURLComplete)
	approvedNotification := readAuthLinkNotification(t, notificationCh)
	assert.Equal(t, models.AuthLinkStatusApproved, approvedNotification.Status)
	assertAuthLinkNotificationRedacted(t, &approvedNotification)

	entry := config.LookupAuth(config.GetAuthCfg(), config.BackupAuthLookupURL(server.URL))
	require.NotNil(t, entry, "the approved claim stores the credential")
	assert.Equal(t, "zpd1_device_token", entry.Bearer)
	assert.False(t, st.BackupCoordinator().RemoteUnlinked())
}

func TestSettingsAuthLink_ExpiredPollFails(t *testing.T) {
	// Not parallel: swaps package-level claimClient and link session state.
	resetAuthLinkState(t)

	var mux http.ServeMux
	server := httptest.NewServer(&mux)
	defer server.Close()

	mux.HandleFunc("POST /v1/device-link-requests", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(deviceLinkCreateResponse{
			DeviceCode: "zpl1_secret",
			UserCode:   "ABCD-1234",
			ExpiresAt:  time.Now().Add(10 * time.Minute),
			Interval:   1,
		})
	})
	mux.HandleFunc("POST /v1/device-link-requests/poll", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"code":"invalid_link_code"}}`))
	})

	origClient := claimClient
	claimClient = server.Client()
	t.Cleanup(func() { claimClient = origClient })

	cfg, err := config.NewConfigWithFs(t.TempDir(), config.BaseDefaults, afero.NewMemMapFs())
	require.NoError(t, err)
	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("test-platform").Maybe()

	params, err := json.Marshal(models.SettingsAuthLinkParams{URL: server.URL})
	require.NoError(t, err)
	env := requests.RequestEnv{
		Context:  context.Background(),
		Config:   cfg,
		Platform: mockPlatform,
		Params:   params,
		IsLocal:  true,
	}

	_, err = HandleSettingsAuthLink(env, nil)
	require.NoError(t, err)

	failed := waitForAuthLinkStatus(t, models.AuthLinkStatusFailed, 15*time.Second)
	assert.Contains(t, failed.Error, "start over")
}

func TestPollDeviceLink_ParentCancellationTerminalizesPendingSession(t *testing.T) {
	// Not parallel: uses package-level link session state.
	resetAuthLinkState(t)
	ctx, cancel := context.WithCancel(context.Background())
	notificationCh := make(chan models.Notification, 1)
	session := &authLinkSession{
		cancel: cancel,
		status: models.AuthLinkStatusResponse{
			Status: models.AuthLinkStatusPending, UserCode: "secret-code",
			VerificationURL: "https://online.example/secret",
		},
	}
	authLinkMu.Lock()
	activeAuthLink = session
	authLinkMu.Unlock()

	go pollDeviceLink(ctx, session, &authLinkDeps{ns: notificationCh}, "", "", time.Hour)
	cancel()
	failedNotification := readAuthLinkNotification(t, notificationCh)
	assert.Equal(t, models.AuthLinkStatusFailed, failedNotification.Status)
	assert.Contains(t, failedNotification.Error, "linking stopped")
	assertAuthLinkNotificationRedacted(t, &failedNotification)
	status := waitForAuthLinkStatus(t, models.AuthLinkStatusFailed, time.Second)
	assertAuthLinkNotificationRedacted(t, &status)
	require.NoError(t, beginAuthLinkStart(), "terminalized session must not block a new link flow")
}

func TestSettingsAuthLink_Cancel(t *testing.T) {
	// Not parallel: swaps package-level claimClient and link session state.
	resetAuthLinkState(t)

	var mux http.ServeMux
	server := httptest.NewServer(&mux)
	defer server.Close()

	mux.HandleFunc("POST /v1/device-link-requests", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(deviceLinkCreateResponse{
			DeviceCode: "zpl1_secret",
			UserCode:   "ABCD-1234",
			ExpiresAt:  time.Now().Add(10 * time.Minute),
			Interval:   5,
		})
	})
	mux.HandleFunc("POST /v1/device-link-requests/poll", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(deviceLinkPollResponse{Status: "pending", Interval: 5})
	})

	origClient := claimClient
	claimClient = server.Client()
	t.Cleanup(func() { claimClient = origClient })

	cfg, err := config.NewConfigWithFs(t.TempDir(), config.BaseDefaults, afero.NewMemMapFs())
	require.NoError(t, err)
	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("test-platform").Maybe()
	st, notificationCh := state.NewState(mockPlatform, "cancel-test-boot")
	t.Cleanup(st.StopService)

	params, err := json.Marshal(models.SettingsAuthLinkParams{URL: server.URL})
	require.NoError(t, err)
	env := requests.RequestEnv{
		Context:  context.Background(),
		Config:   cfg,
		Platform: mockPlatform,
		State:    st,
		Params:   params,
		IsLocal:  true,
	}

	_, err = HandleSettingsAuthLink(env, nil)
	require.NoError(t, err)
	pendingNotification := readAuthLinkNotification(t, notificationCh)
	assert.Equal(t, models.AuthLinkStatusPending, pendingNotification.Status)
	assertAuthLinkNotificationRedacted(t, &pendingNotification)

	result, err := HandleSettingsAuthLinkCancel(env)
	require.NoError(t, err)
	cancelled, ok := result.(models.AuthLinkStatusResponse)
	require.True(t, ok)
	assert.Equal(t, models.AuthLinkStatusCancelled, cancelled.Status)
	cancelledNotification := readAuthLinkNotification(t, notificationCh)
	assert.Equal(t, models.AuthLinkStatusCancelled, cancelledNotification.Status)
	assertAuthLinkNotificationRedacted(t, &cancelledNotification)
	select {
	case notification := <-notificationCh:
		t.Fatalf("explicit cancellation must remain terminal, got extra notification %s", notification.Method)
	case <-time.After(100 * time.Millisecond):
	}
	status := waitForAuthLinkStatus(t, models.AuthLinkStatusCancelled, time.Second)
	assert.Equal(t, models.AuthLinkStatusCancelled, status.Status)

	// Cancelling again reports no active request.
	_, err = HandleSettingsAuthLinkCancel(env)
	require.Error(t, err)
}
