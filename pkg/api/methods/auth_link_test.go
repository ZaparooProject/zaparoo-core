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
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
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
		authLinkMu.Unlock()
	})
	authLinkMu.Lock()
	activeAuthLink = nil
	authLinkMu.Unlock()
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

func TestSettingsAuthLink_RequiresLocalClient(t *testing.T) {
	// Not parallel: uses package-level link session state.
	resetAuthLinkState(t)

	_, err := HandleSettingsAuthLink(requests.RequestEnv{IsLocal: false}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "local client")

	_, err = HandleSettingsAuthLinkCancel(requests.RequestEnv{IsLocal: false})
	require.Error(t, err)
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

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("test-platform").Maybe()

	mockFetchWK := func(_ string) (*zapscript.WellKnown, error) {
		return &zapscript.WellKnown{Auth: 1}, nil
	}

	params, err := json.Marshal(models.SettingsAuthLinkParams{URL: server.URL})
	require.NoError(t, err)
	env := requests.RequestEnv{
		Context:  context.Background(),
		Config:   cfg,
		Platform: mockPlatform,
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

	waitForAuthLinkStatus(t, models.AuthLinkStatusApproved, 15*time.Second)

	entry := config.LookupAuth(config.GetAuthCfg(), config.BackupAuthLookupURL(server.URL))
	require.NotNil(t, entry, "the approved claim stores the credential")
	assert.Equal(t, "zpd1_device_token", entry.Bearer)
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

	result, err := HandleSettingsAuthLinkCancel(requests.RequestEnv{IsLocal: true})
	require.NoError(t, err)
	cancelled, ok := result.(models.AuthLinkStatusResponse)
	require.True(t, ok)
	assert.Equal(t, models.AuthLinkStatusCancelled, cancelled.Status)

	// Cancelling again reports no active request.
	_, err = HandleSettingsAuthLinkCancel(requests.RequestEnv{IsLocal: true})
	require.Error(t, err)
}
