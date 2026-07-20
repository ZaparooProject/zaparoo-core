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
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	backupsvc "github.com/ZaparooProject/zaparoo-core/v2/pkg/service/backup"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSettingsAuthStatus_RequiresURL(t *testing.T) {
	cfg, err := config.NewConfig(t.TempDir(), config.BaseDefaults)
	require.NoError(t, err)

	_, err = HandleSettingsAuthStatus(requests.RequestEnv{Config: cfg})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "url is required")
}

func TestSettingsAuthStatus_LinkedOfficialURL(t *testing.T) {
	config.SetAuthCfgForTesting(map[string]config.CredentialEntry{
		"https://api.zaparoo.com": {Bearer: "token"},
	})
	t.Cleanup(config.ClearAuthCfgForTesting)
	cfg, err := config.NewConfig(t.TempDir(), config.BaseDefaults)
	require.NoError(t, err)
	params, err := json.Marshal(models.SettingsAuthStatusParams{URL: "https://api.zaparoo.com"})
	require.NoError(t, err)

	result, err := HandleSettingsAuthStatus(requests.RequestEnv{Config: cfg, Params: params})
	require.NoError(t, err)
	resp, ok := result.(models.SettingsAuthStatusResponse)
	require.True(t, ok)
	assert.True(t, resp.Linked)
}

func TestSettingsAuthStatus_RejectsUnsupportedURL(t *testing.T) {
	config.SetAuthCfgForTesting(map[string]config.CredentialEntry{
		"https://other.example.com": {Bearer: "token"},
	})
	t.Cleanup(config.ClearAuthCfgForTesting)
	cfg, err := config.NewConfig(t.TempDir(), config.BaseDefaults)
	require.NoError(t, err)
	params, err := json.Marshal(models.SettingsAuthStatusParams{URL: "https://other.example.com"})
	require.NoError(t, err)

	result, err := HandleSettingsAuthStatus(requests.RequestEnv{Config: cfg, Params: params})
	require.NoError(t, err)
	resp, ok := result.(models.SettingsAuthStatusResponse)
	require.True(t, ok)
	assert.False(t, resp.Linked)
}

func TestSettingsAuthStatus_AllowsConfiguredBackupURL(t *testing.T) {
	config.SetAuthCfgForTesting(map[string]config.CredentialEntry{
		"http://127.0.0.1:8787": {Bearer: "token"},
	})
	t.Cleanup(config.ClearAuthCfgForTesting)
	cfg, err := config.NewConfig(t.TempDir(), config.BaseDefaults)
	require.NoError(t, err)
	require.NoError(t, cfg.SetBackupRemoteBaseURL("http://127.0.0.1:8787"))
	params, err := json.Marshal(models.SettingsAuthStatusParams{URL: "http://127.0.0.1:8787"})
	require.NoError(t, err)

	result, err := HandleSettingsAuthStatus(requests.RequestEnv{Config: cfg, Params: params})
	require.NoError(t, err)
	resp, ok := result.(models.SettingsAuthStatusResponse)
	require.True(t, ok)
	assert.True(t, resp.Linked)
}

func TestSettingsAuthClaim_MissingParams(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("test-platform").Maybe()

	env := requests.RequestEnv{
		Context:  context.Background(),
		Platform: mockPlatform,
		Params:   nil,
	}

	_, err := HandleSettingsAuthClaim(env, nil)
	require.Error(t, err)
}

func TestSettingsAuthClaim_RequiresHTTPS(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("test-platform").Maybe()

	params := models.SettingsAuthClaimParams{
		ClaimURL: "http://not-secure.com/claim",
		Token:    "test-token",
	}
	paramsJSON, err := json.Marshal(params)
	require.NoError(t, err)

	env := requests.RequestEnv{
		Context:  context.Background(),
		Platform: mockPlatform,
		Params:   paramsJSON,
	}

	_, err = HandleSettingsAuthClaim(env, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "HTTPS")
}

func TestSettingsAuthClaim_ClaimTokenFailure(t *testing.T) {
	// Not parallel: swaps package-level claimClient

	// Claim server returns 401, verify Zaparoo headers are sent
	claimServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, runtime.GOOS, r.Header.Get(zapscript.HeaderZaparooOS))
		assert.Equal(t, runtime.GOARCH, r.Header.Get(zapscript.HeaderZaparooArch))
		assert.Equal(t, "test-platform", r.Header.Get(zapscript.HeaderZaparooPlatform))
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("invalid token"))
	}))
	defer claimServer.Close()

	// Swap claimClient to trust the test server's self-signed cert
	origClient := claimClient
	claimClient = claimServer.Client()
	t.Cleanup(func() { claimClient = origClient })

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("test-platform").Maybe()

	cfg, err := config.NewConfigWithFs(t.TempDir(), config.BaseDefaults, afero.NewMemMapFs())
	require.NoError(t, err)

	params := models.SettingsAuthClaimParams{
		ClaimURL: claimServer.URL + "/claim",
		Token:    "bad-token",
	}
	paramsJSON, err := json.Marshal(params)
	require.NoError(t, err)

	// Root well-known confirms auth support so the flow reaches claim redemption
	mockFetchWK := func(_ string) (*zapscript.WellKnown, error) {
		return &zapscript.WellKnown{ZapScript: 1, Auth: 1}, nil
	}

	env := requests.RequestEnv{
		Context:  context.Background(),
		Platform: mockPlatform,
		Config:   cfg,
		Params:   paramsJSON,
	}

	_, err = HandleSettingsAuthClaim(env, mockFetchWK)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "401")
}

func TestSettingsAuthClaim_RootMissingAuth(t *testing.T) {
	// Not parallel: swaps package-level claimClient

	// Claim server returns a valid bearer token
	claimServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"bearer": "secret-api-key"}`))
	}))
	defer claimServer.Close()

	origClient := claimClient
	claimClient = claimServer.Client()
	t.Cleanup(func() { claimClient = origClient })

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("test-platform")

	cfg, err := config.NewConfigWithFs(t.TempDir(), config.BaseDefaults, afero.NewMemMapFs())
	require.NoError(t, err)

	// Root well-known has zapscript but NOT auth support
	mockFetchWK := func(_ string) (*zapscript.WellKnown, error) {
		return &zapscript.WellKnown{ZapScript: 1}, nil
	}

	params := models.SettingsAuthClaimParams{
		ClaimURL: claimServer.URL + "/claim",
		Token:    "claim-token-123",
	}
	paramsJSON, err := json.Marshal(params)
	require.NoError(t, err)

	env := requests.RequestEnv{
		Context:  context.Background(),
		Platform: mockPlatform,
		Config:   cfg,
		Params:   paramsJSON,
	}

	_, err = HandleSettingsAuthClaim(env, mockFetchWK)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not support auth")
}

func TestSettingsAuthClaim_HappyPath(t *testing.T) {
	// Not parallel: swaps package-level claimClient

	// Claim server returns a bearer token, verify JSON body and Zaparoo headers
	claimServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		body, readErr := io.ReadAll(r.Body)
		if assert.NoError(t, readErr) {
			var req claimRequest
			if assert.NoError(t, json.Unmarshal(body, &req)) {
				assert.Equal(t, "claim-token-123", req.Token)
			}
		}

		assert.Equal(t, runtime.GOOS, r.Header.Get(zapscript.HeaderZaparooOS))
		assert.Equal(t, runtime.GOARCH, r.Header.Get(zapscript.HeaderZaparooArch))
		assert.Equal(t, "test-platform", r.Header.Get(zapscript.HeaderZaparooPlatform))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"bearer": "secret-api-key"}`))
	}))
	defer claimServer.Close()

	origClient := claimClient
	claimClient = claimServer.Client()
	t.Cleanup(func() { claimClient = origClient })

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("test-platform")

	cfg, err := config.NewConfigWithFs(t.TempDir(), config.BaseDefaults, afero.NewMemMapFs())
	require.NoError(t, err)
	require.NoError(t, cfg.SetBackupRemoteBaseURL(claimServer.URL))
	mockPlatform.On("Settings").Return(platforms.Settings{
		DataDir: t.TempDir(), ConfigDir: t.TempDir(),
	})
	st, _ := state.NewState(mockPlatform, "test-boot")
	t.Cleanup(st.StopService)
	st.BackupCoordinator().SetRemoteUnlinked(true)

	// Mock well-known fetcher: root has auth and trusts spoke.example.com,
	// related domain confirms trust back to root
	mockFetchWK := func(baseURL string) (*zapscript.WellKnown, error) {
		switch baseURL {
		case claimServer.URL:
			return &zapscript.WellKnown{
				ZapScript: 1,
				Auth:      1,
				Trusted:   []string{"spoke.example.com"},
			}, nil
		case "https://spoke.example.com":
			host := claimServer.URL[len("https://"):]
			return &zapscript.WellKnown{
				ZapScript: 1,
				Auth:      1,
				Trusted:   []string{host},
			}, nil
		default:
			return nil, errors.New("unknown domain")
		}
	}

	params := models.SettingsAuthClaimParams{
		ClaimURL: claimServer.URL + "/claim",
		Token:    "claim-token-123",
	}
	paramsJSON, err := json.Marshal(params)
	require.NoError(t, err)

	env := requests.RequestEnv{
		Context:  context.Background(),
		Platform: mockPlatform,
		Config:   cfg,
		State:    st,
		Params:   paramsJSON,
	}

	result, err := HandleSettingsAuthClaim(env, mockFetchWK)
	require.NoError(t, err)

	resp, ok := result.(models.SettingsAuthClaimResponse)
	require.True(t, ok)

	// Should have stored creds for both root and spoke
	assert.Contains(t, resp.Domains, claimServer.URL)
	assert.Contains(t, resp.Domains, "https://spoke.example.com")
	assert.Len(t, resp.Domains, 2)
	assert.False(t, st.BackupCoordinator().RemoteUnlinked())
}

func TestSettingsAuthClaim_NoRelatedTrust(t *testing.T) {
	// Not parallel: swaps package-level claimClient

	// Claim server returns a bearer token
	claimServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"bearer": "secret-api-key"}`))
	}))
	defer claimServer.Close()

	origClient := claimClient
	claimClient = claimServer.Client()
	t.Cleanup(func() { claimClient = origClient })

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("test-platform")

	cfg, err := config.NewConfigWithFs(t.TempDir(), config.BaseDefaults, afero.NewMemMapFs())
	require.NoError(t, err)

	// Root supports auth but has no trusted related domains
	mockFetchWK := func(_ string) (*zapscript.WellKnown, error) {
		return &zapscript.WellKnown{ZapScript: 1, Auth: 1}, nil
	}

	params := models.SettingsAuthClaimParams{
		ClaimURL: claimServer.URL + "/claim",
		Token:    "claim-token-123",
	}
	paramsJSON, err := json.Marshal(params)
	require.NoError(t, err)

	env := requests.RequestEnv{
		Context:  context.Background(),
		Platform: mockPlatform,
		Config:   cfg,
		Params:   paramsJSON,
	}

	result, err := HandleSettingsAuthClaim(env, mockFetchWK)
	require.NoError(t, err)

	resp, ok := result.(models.SettingsAuthClaimResponse)
	require.True(t, ok)

	// Only root domain stored
	assert.Equal(t, []string{claimServer.URL}, resp.Domains)

	// The stored entry carries its provenance so unlink can find it.
	t.Cleanup(config.ClearAuthCfgForTesting)
	entry := config.LookupAuth(config.GetAuthCfg(), config.BackupAuthLookupURL(claimServer.URL))
	require.NotNil(t, entry)
	assert.Equal(t, claimServer.URL, entry.LinkedVia)
}

func TestRedeemClaimToken_Success(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		// Verify the token is sent as JSON body
		body, readErr := io.ReadAll(r.Body)
		if assert.NoError(t, readErr) {
			var req claimRequest
			if assert.NoError(t, json.Unmarshal(body, &req)) {
				assert.Equal(t, "test-token-123", req.Token)
			}
		}

		// Verify Zaparoo identification headers are present
		assert.Equal(t, runtime.GOOS, r.Header.Get(zapscript.HeaderZaparooOS))
		assert.Equal(t, runtime.GOARCH, r.Header.Get(zapscript.HeaderZaparooArch))
		assert.Equal(t, "test-platform", r.Header.Get(zapscript.HeaderZaparooPlatform))

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"bearer": "real-api-key"}`))
	}))
	defer server.Close()

	bearer, err := redeemClaimToken(
		context.Background(), server.URL+"/claim", "test-token-123", "test-platform", "hint-uuid",
	)
	require.NoError(t, err)
	assert.Equal(t, "real-api-key", bearer)
}

func TestRedeemClaimToken_EmptyBearer(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"bearer": ""}`))
	}))
	defer server.Close()

	_, err := redeemClaimToken(context.Background(), server.URL+"/claim", "token", "test-platform", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing bearer token")
}

func TestRedeemClaimToken_ServerError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal error"))
	}))
	defer server.Close()

	_, err := redeemClaimToken(context.Background(), server.URL+"/claim", "token", "test-platform", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "500")
}

func TestConfirmRelatedTrust_Valid(t *testing.T) {
	t.Parallel()

	// Related server serves .well-known with auth support and trusts the root
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/zaparoo" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"zapscript": 1, "auth": 1, "trusted": ["root.example.com"]}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	result := confirmRelatedTrust(server.URL, "https://root.example.com", nil, zapscript.FetchWellKnown)
	assert.True(t, result)
}

func TestConfirmRelatedTrust_NoAuthSupport(t *testing.T) {
	t.Parallel()

	// Related server serves .well-known without auth
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/zaparoo" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"zapscript": 1}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	result := confirmRelatedTrust(server.URL, "https://root.example.com", nil, zapscript.FetchWellKnown)
	assert.False(t, result)
}

func TestConfirmRelatedTrust_DoesNotTrustRoot(t *testing.T) {
	t.Parallel()

	// Related domain has auth but doesn't list the root in trusted
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/zaparoo" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"zapscript": 1, "auth": 1, "trusted": ["other.example.com"]}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	result := confirmRelatedTrust(server.URL, "https://root.example.com", nil, zapscript.FetchWellKnown)
	assert.False(t, result)
}

func TestConfirmRelatedTrust_ServerDown(t *testing.T) {
	t.Parallel()

	result := confirmRelatedTrust("http://127.0.0.1:1", "https://root.example.com", nil, zapscript.FetchWellKnown)
	assert.False(t, result)
}

func newAuthUnlinkTestEnv(t *testing.T) requests.RequestEnv {
	t.Helper()
	dataDir := t.TempDir()
	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("test-platform").Maybe()
	mockPlatform.On("Settings").Return(platforms.Settings{DataDir: dataDir}).Maybe()
	cfg, err := config.NewConfigWithFs(t.TempDir(), config.BaseDefaults, afero.NewMemMapFs())
	require.NoError(t, err)
	t.Cleanup(config.ClearAuthCfgForTesting)
	originalRevoke := revokeRemoteDevice
	revokeRemoteDevice = func(context.Context, *backupsvc.Manager) error { return nil }
	t.Cleanup(func() { revokeRemoteDevice = originalRevoke })
	return requests.RequestEnv{
		Context:  context.Background(),
		Config:   cfg,
		Platform: mockPlatform,
		IsLocal:  true,
	}
}

func TestSettingsAuthUnlink_RemovesTaggedFamily(t *testing.T) {
	// Not parallel: SaveAuthEntry updates the global auth config.
	env := newAuthUnlinkTestEnv(t)

	// The claim flow tags the root and each trusted-domain copy with the
	// root that created them — including domains on no static list
	// (zaparoo.run). Bearers deliberately differ to prove the provenance
	// tag decides, not credential contents.
	root := "https://api.zaparoo.com"
	require.NoError(t, env.Config.SaveAuthEntry(root,
		config.CredentialEntry{Bearer: "b1", LinkedVia: root}))
	require.NoError(t, env.Config.SaveAuthEntry("https://edge.zaparoo.com",
		config.CredentialEntry{Bearer: "b2", LinkedVia: root}))
	require.NoError(t, env.Config.SaveAuthEntry("https://zaparoo.run",
		config.CredentialEntry{Bearer: "b3", LinkedVia: root}))
	// Tagged with a different root: belongs to another link, must survive.
	require.NoError(t, env.Config.SaveAuthEntry("https://service.example.com",
		config.CredentialEntry{Bearer: "b4", LinkedVia: "https://other-root.example.com"}))
	// Hand-written entry: must survive.
	require.NoError(t, env.Config.SaveAuthEntry("https://other.example.com",
		config.CredentialEntry{Bearer: "b5"}))

	result, err := HandleSettingsAuthUnlink(env)
	require.NoError(t, err)
	resp, ok := result.(models.SettingsAuthUnlinkResponse)
	require.True(t, ok)
	assert.Equal(t, []string{
		"https://api.zaparoo.com", "https://edge.zaparoo.com", "https://zaparoo.run",
	}, resp.Domains)

	creds := config.GetAuthCfg()
	require.Len(t, creds, 2)
	assert.Equal(t, "b4", creds["https://service.example.com"].Bearer)
	assert.Equal(t, "b5", creds["https://other.example.com"].Bearer)
}

func TestSettingsAuthUnlink_CustomServerScopedToItsOwnFamily(t *testing.T) {
	// Not parallel: SaveAuthEntry updates the global auth config.
	env := newAuthUnlinkTestEnv(t)

	// Unlinking the configured custom server removes only its entry;
	// credentials for other domains are untouched.
	require.NoError(t, env.Config.SetBackupRemoteBaseURL("http://127.0.0.1:8787"))
	require.NoError(t, env.Config.SaveAuthEntry("http://127.0.0.1:8787",
		config.CredentialEntry{Bearer: "t1", LinkedVia: "http://127.0.0.1:8787"}))
	require.NoError(t, env.Config.SaveAuthEntry("https://api.zaparoo.com",
		config.CredentialEntry{Bearer: "t2", LinkedVia: "https://api.zaparoo.com"}))

	result, err := HandleSettingsAuthUnlink(env)
	require.NoError(t, err)
	resp, ok := result.(models.SettingsAuthUnlinkResponse)
	require.True(t, ok)
	assert.Equal(t, []string{"http://127.0.0.1:8787"}, resp.Domains)

	creds := config.GetAuthCfg()
	require.Len(t, creds, 1)
	assert.Equal(t, "t2", creds["https://api.zaparoo.com"].Bearer)
}

func TestSettingsAuthUnlink_NotLinkedIsNoOp(t *testing.T) {
	// Not parallel: SaveAuthEntry updates the global auth config.
	env := newAuthUnlinkTestEnv(t)

	require.NoError(t, env.Config.SaveAuthEntry("https://other.example.com", config.CredentialEntry{Bearer: "t3"}))

	result, err := HandleSettingsAuthUnlink(env)
	require.NoError(t, err)
	resp, ok := result.(models.SettingsAuthUnlinkResponse)
	require.True(t, ok)
	assert.Empty(t, resp.Domains)

	creds := config.GetAuthCfg()
	require.Len(t, creds, 1)
	assert.Equal(t, "t3", creds["https://other.example.com"].Bearer)
}

func TestSettingsAuthUnlink_RemovesConfiguredBackupServerCredential(t *testing.T) {
	// Not parallel: SaveAuthEntry updates the global auth config.
	env := newAuthUnlinkTestEnv(t)

	require.NoError(t, env.Config.SetBackupRemoteBaseURL("http://127.0.0.1:8787"))
	require.NoError(t, env.Config.SaveAuthEntry("http://127.0.0.1:8787", config.CredentialEntry{Bearer: "t1"}))

	result, err := HandleSettingsAuthUnlink(env)
	require.NoError(t, err)
	resp, ok := result.(models.SettingsAuthUnlinkResponse)
	require.True(t, ok)
	assert.Equal(t, []string{"http://127.0.0.1:8787"}, resp.Domains)
	assert.Empty(t, config.GetAuthCfg())
}

func TestSettingsAuthUnlink_RevokeFailureKeepsLocalCredential(t *testing.T) {
	// Not parallel: SaveAuthEntry and revokeRemoteDevice are process globals.
	env := newAuthUnlinkTestEnv(t)
	revokeRemoteDevice = func(context.Context, *backupsvc.Manager) error {
		return errors.New("server unavailable")
	}

	require.NoError(t, env.Config.SaveAuthEntry(
		"https://api.zaparoo.com", config.CredentialEntry{Bearer: "t1"},
	))

	_, err := HandleSettingsAuthUnlink(env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to revoke remote device link")
	entry := config.LookupAuth(config.GetAuthCfg(), "https://api.zaparoo.com")
	require.NotNil(t, entry)
	assert.Equal(t, "t1", entry.Bearer)
}

func TestSettingsAuthUnlink_MarksRemoteUnlinked(t *testing.T) {
	// Not parallel: SaveAuthEntry updates the global auth config.
	env := newAuthUnlinkTestEnv(t)

	require.NoError(t, env.Config.SaveAuthEntry("https://api.zaparoo.com", config.CredentialEntry{Bearer: "t1"}))

	_, err := HandleSettingsAuthUnlink(env)
	require.NoError(t, err)

	statusPath := filepath.Join(env.Platform.Settings().DataDir, "backups", "status.json")
	data, err := os.ReadFile(statusPath) //nolint:gosec // test-owned temp path
	require.NoError(t, err)
	assert.Contains(t, string(data), `"unlinked": true`)
}

func TestSettingsAuthUnlink_RejectsRemoteClients(t *testing.T) {
	t.Parallel()

	env := requests.RequestEnv{
		Context: context.Background(),
		IsLocal: false,
	}
	_, err := HandleSettingsAuthUnlink(env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "local or admin")

	env.ClientRole = "member"
	_, err = HandleSettingsAuthUnlink(env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "local or admin")
}
