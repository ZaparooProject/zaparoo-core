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
	"strings"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/permissions"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/jonboulle/clockwork"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeQuickConnect is a Jellyfin server that only speaks Quick Connect.
type fakeQuickConnect struct {
	srv            *httptest.Server
	authHeaders    []string
	initiateStatus int
	connectStatus  int
	approveAfter   int
	connects       int
	mu             syncutil.Mutex
	disabled       bool
}

func newFakeQuickConnect(t *testing.T) *fakeQuickConnect {
	t.Helper()
	f := &fakeQuickConnect{approveAfter: 2}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /QuickConnect/Enabled", func(w http.ResponseWriter, r *http.Request) {
		f.record(r)
		f.mu.Lock()
		defer f.mu.Unlock()
		writeJSON(w, !f.disabled)
	})
	mux.HandleFunc("POST /QuickConnect/Initiate", func(w http.ResponseWriter, r *http.Request) {
		f.record(r)
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.initiateStatus != 0 {
			w.WriteHeader(f.initiateStatus)
			return
		}
		writeJSON(w, map[string]any{"Secret": "qc-secret", "Code": "123456", "Authenticated": false})
	})
	mux.HandleFunc("GET /QuickConnect/Connect", func(w http.ResponseWriter, r *http.Request) {
		f.record(r)
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.connectStatus != 0 {
			w.WriteHeader(f.connectStatus)
			return
		}
		if r.URL.Query().Get("secret") != "qc-secret" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		f.connects++
		writeJSON(w, map[string]any{
			"Secret": "qc-secret", "Code": "123456", "Authenticated": f.connects >= f.approveAfter,
		})
	})
	mux.HandleFunc("POST /Users/AuthenticateWithQuickConnect", func(w http.ResponseWriter, r *http.Request) {
		f.record(r)
		var body struct{ Secret string }
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Secret != "qc-secret" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		writeJSON(w, map[string]any{"AccessToken": "jf-access-token"})
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeQuickConnect) record(r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.authHeaders = append(f.authHeaders, r.Header.Get("Authorization"))
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// useJellyfinLinkClock swaps the link clock for a fake one.
func useJellyfinLinkClock(t *testing.T) *clockwork.FakeClock {
	t.Helper()
	clock := clockwork.NewFakeClock()
	orig := jellyfinLinkClock
	jellyfinLinkClock = clock
	t.Cleanup(func() { jellyfinLinkClock = orig })
	return clock
}

// advanceJellyfinPoll lets one poll of the link run.
func advanceJellyfinPoll(t *testing.T, clock *clockwork.FakeClock, by time.Duration) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, clock.BlockUntilContext(ctx, 1), "link is not waiting to poll")
	clock.Advance(by)
}

func jellyfinLinkEnv(
	t *testing.T, serverURL string,
) (requests.RequestEnv, *config.Instance, <-chan models.Notification) {
	t.Helper()
	cfg, err := config.NewConfigWithFs(t.TempDir(), config.BaseDefaults, afero.NewMemMapFs())
	require.NoError(t, err)
	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("test-platform").Maybe()
	mockPlatform.On("Settings").Return(platforms.Settings{
		DataDir: t.TempDir(), ConfigDir: t.TempDir(),
	}).Maybe()
	st, notificationCh := state.NewState(mockPlatform, "test-boot")
	t.Cleanup(st.StopService)
	params, err := json.Marshal(models.SettingsAuthLinkParams{
		URL: serverURL, Provider: models.AuthLinkProviderJellyfin,
	})
	require.NoError(t, err)
	return requests.RequestEnv{
		Context:  context.Background(),
		Config:   cfg,
		Platform: mockPlatform,
		State:    st,
		Params:   params,
		IsLocal:  true,
	}, cfg, notificationCh
}

func useEmptyAuthCfg(t *testing.T) {
	t.Helper()
	config.SetAuthCfgForTesting(map[string]config.CredentialEntry{})
	t.Cleanup(config.ClearAuthCfgForTesting)
}

func TestSettingsAuthLinkJellyfin_Approved(t *testing.T) {
	// Not parallel: swaps the link clock and uses package-level link state.
	resetAuthLinkState(t)
	useEmptyAuthCfg(t)
	clock := useJellyfinLinkClock(t)
	f := newFakeQuickConnect(t)

	env, cfg, notificationCh := jellyfinLinkEnv(t, f.srv.URL+"/")
	result, err := HandleSettingsAuthLink(env, nil)
	require.NoError(t, err)
	started, ok := result.(models.AuthLinkStatusResponse)
	require.True(t, ok)
	assert.Equal(t, models.AuthLinkStatusPending, started.Status)
	assert.Equal(t, models.AuthLinkProviderJellyfin, started.Provider)
	assert.Equal(t, "123456", started.UserCode)
	assert.Equal(t, f.srv.URL+"/web/#/quickconnect", started.VerificationURL)
	require.NotNil(t, started.ExpiresAt)
	assert.Equal(t, clock.Now().Add(jellyfinLinkTTL), *started.ExpiresAt)

	pending := readAuthLinkNotification(t, notificationCh)
	assert.Equal(t, models.AuthLinkStatusPending, pending.Status)
	assertAuthLinkNotificationRedacted(t, &pending)

	advanceJellyfinPoll(t, clock, jellyfinLinkInterval)
	advanceJellyfinPoll(t, clock, jellyfinLinkInterval)
	approved := waitForAuthLinkStatus(t, models.AuthLinkStatusApproved, 10*time.Second)
	assert.Equal(t, models.AuthLinkProviderJellyfin, approved.Provider)

	entry := config.LookupAuth(config.GetAuthCfg(), f.srv.URL+"/Items")
	require.NotNil(t, entry, "the approved link stores the token for the server")
	assert.Equal(t, "jf-access-token", entry.Bearer)
	for _, id := range []string{shared.LauncherJellyfinMovie, shared.LauncherJellyfinArtist} {
		def := cfg.LookupLauncherDefaults(id, []string{shared.LauncherGroupJellyfin})
		assert.Equal(t, f.srv.URL, def.ServerURL, id)
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	require.NotEmpty(t, f.authHeaders)
	for _, h := range f.authHeaders {
		assert.True(t, strings.HasPrefix(h, "MediaBrowser "), h)
		assert.Contains(t, h, `DeviceId="`)
		assert.NotContains(t, h, "Token=")
	}
}

func TestSettingsAuthLinkJellyfin_Disabled(t *testing.T) {
	// Not parallel: uses package-level link session state.
	resetAuthLinkState(t)
	useJellyfinLinkClock(t)

	for _, setup := range []func(*fakeQuickConnect){
		func(f *fakeQuickConnect) { f.disabled = true },
		func(f *fakeQuickConnect) { f.initiateStatus = http.StatusUnauthorized },
	} {
		f := newFakeQuickConnect(t)
		setup(f)
		env, _, _ := jellyfinLinkEnv(t, f.srv.URL)
		_, err := HandleSettingsAuthLink(env, nil)
		var clientErr *models.ClientError
		require.ErrorAs(t, err, &clientErr)
		assert.Contains(t, err.Error(), "Quick Connect is disabled")
	}
	authLinkMu.Lock()
	defer authLinkMu.Unlock()
	assert.Nil(t, activeAuthLink)
	assert.False(t, authLinkStarting)
}

func TestSettingsAuthLinkJellyfin_StartFailureReleasesSession(t *testing.T) {
	// Not parallel: uses package-level link session state.
	resetAuthLinkState(t)
	useJellyfinLinkClock(t)
	f := newFakeQuickConnect(t)
	f.initiateStatus = http.StatusInternalServerError

	env, _, _ := jellyfinLinkEnv(t, f.srv.URL)
	_, err := HandleSettingsAuthLink(env, nil)
	require.Error(t, err)

	authLinkMu.Lock()
	defer authLinkMu.Unlock()
	assert.Nil(t, activeAuthLink)
	assert.False(t, authLinkStarting, "a failed start can be retried")
}

func TestSettingsAuthLinkJellyfin_Expired(t *testing.T) {
	// Not parallel: swaps the link clock and uses package-level link state.
	resetAuthLinkState(t)
	useEmptyAuthCfg(t)
	clock := useJellyfinLinkClock(t)
	f := newFakeQuickConnect(t)
	f.approveAfter = 1 << 30

	env, cfg, _ := jellyfinLinkEnv(t, f.srv.URL)
	_, err := HandleSettingsAuthLink(env, nil)
	require.NoError(t, err)
	advanceJellyfinPoll(t, clock, jellyfinLinkInterval)
	advanceJellyfinPoll(t, clock, jellyfinLinkTTL)

	failed := waitForAuthLinkStatus(t, models.AuthLinkStatusFailed, 10*time.Second)
	assert.Contains(t, failed.Error, "expired")
	assert.Nil(t, config.LookupAuth(config.GetAuthCfg(), f.srv.URL+"/Items"))
	assert.Empty(t, cfg.LookupLauncherDefaults(shared.LauncherJellyfinMovie,
		[]string{shared.LauncherGroupJellyfin}).ServerURL)
}

func TestSettingsAuthLinkJellyfin_ForgottenByServer(t *testing.T) {
	// Not parallel: swaps the link clock and uses package-level link state.
	resetAuthLinkState(t)
	useEmptyAuthCfg(t)
	clock := useJellyfinLinkClock(t)
	f := newFakeQuickConnect(t)

	env, _, _ := jellyfinLinkEnv(t, f.srv.URL)
	_, err := HandleSettingsAuthLink(env, nil)
	require.NoError(t, err)
	f.mu.Lock()
	f.connectStatus = http.StatusNotFound
	f.mu.Unlock()
	advanceJellyfinPoll(t, clock, jellyfinLinkInterval)

	failed := waitForAuthLinkStatus(t, models.AuthLinkStatusFailed, 10*time.Second)
	assert.Contains(t, failed.Error, "expired")
}

func TestSettingsAuthLinkJellyfin_RetriesServerErrors(t *testing.T) {
	// Not parallel: swaps the link clock and uses package-level link state.
	resetAuthLinkState(t)
	useEmptyAuthCfg(t)
	clock := useJellyfinLinkClock(t)
	f := newFakeQuickConnect(t)
	f.approveAfter = 1

	env, _, _ := jellyfinLinkEnv(t, f.srv.URL)
	_, err := HandleSettingsAuthLink(env, nil)
	require.NoError(t, err)
	f.mu.Lock()
	f.connectStatus = http.StatusBadGateway
	f.mu.Unlock()
	advanceJellyfinPoll(t, clock, jellyfinLinkInterval)
	advanceJellyfinPoll(t, clock, jellyfinLinkInterval)
	// Both failed polls are done once the loop waits again.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, clock.BlockUntilContext(ctx, 1))
	waitForAuthLinkStatus(t, models.AuthLinkStatusPending, time.Second)
	f.mu.Lock()
	f.connectStatus = 0
	f.mu.Unlock()
	clock.Advance(jellyfinLinkInterval)

	waitForAuthLinkStatus(t, models.AuthLinkStatusApproved, 10*time.Second)
}

func TestSettingsAuthLinkJellyfin_RequiresLocalOrAdmin(t *testing.T) {
	// Not parallel: swaps the link clock and uses package-level link state.
	resetAuthLinkState(t)
	clock := useJellyfinLinkClock(t)
	f := newFakeQuickConnect(t)

	env, _, _ := jellyfinLinkEnv(t, f.srv.URL)
	env.IsLocal = false
	env.ClientRole = string(permissions.RoleMember)
	_, err := HandleSettingsAuthLink(env, nil)
	require.Error(t, err)
	assert.Nil(t, activeAuthLink, "a refused start leaves no session")
	f.mu.Lock()
	assert.Empty(t, f.authHeaders, "a refused start never contacts the server")
	f.mu.Unlock()

	env.ClientRole = string(permissions.RoleAdmin)
	result, err := HandleSettingsAuthLink(env, nil)
	require.NoError(t, err)
	status, ok := result.(models.AuthLinkStatusResponse)
	require.True(t, ok)
	assert.Equal(t, models.AuthLinkProviderJellyfin, status.Provider)
	// Wait for the poll loop to read the clock before the test swaps it back.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, clock.BlockUntilContext(ctx, 1))
}

func TestSettingsAuthLinkJellyfin_RequiresURL(t *testing.T) {
	// Not parallel: uses package-level link session state.
	resetAuthLinkState(t)

	for _, u := range []string{"", "  ", "ftp://jellyfin.test", "http://user:pw@jellyfin.test"} {
		env, _, _ := jellyfinLinkEnv(t, u)
		_, err := HandleSettingsAuthLink(env, nil)
		var clientErr *models.ClientError
		require.ErrorAs(t, err, &clientErr, u)
	}
	assert.Nil(t, activeAuthLink)
}
