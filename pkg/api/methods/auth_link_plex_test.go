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
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/permissions"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/plex"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/jonboulle/clockwork"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	fakePlexClientsURL = "https://clients.plex.test"
	fakePlexPinID      = 77
)

// fakePlexLink is plex.tv for the link flow: one PIN and the account's
// resources.
type fakePlexLink struct {
	unreachable map[string]bool
	token       string
	resources   []plex.Resource
	checks      int
	mu          syncutil.Mutex
	expired     bool
}

// identity answers as the server that lists serverURL, unless the test
// marked that address unreachable.
func (f *fakePlexLink) identity(_ context.Context, serverURL, _ string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.unreachable[serverURL] {
		return "", errors.New("connection refused")
	}
	for i := range f.resources {
		for _, c := range f.resources[i].Connections {
			if plex.NormalizeURL(c.URI) == serverURL {
				return f.resources[i].ClientIdentifier, nil
			}
		}
	}
	return "", errors.New("no server answers")
}

func (*fakePlexLink) ClientsURL() string { return fakePlexClientsURL }

func (*fakePlexLink) CreatePin(context.Context) (*plex.Pin, error) {
	return &plex.Pin{ID: fakePlexPinID, Code: "AB12", ExpiresIn: 900}, nil
}

func (f *fakePlexLink) CheckPin(_ context.Context, id int64) (*plex.Pin, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.checks++
	if id != fakePlexPinID || f.expired {
		return nil, plex.ErrPinExpired
	}
	return &plex.Pin{ID: fakePlexPinID, Code: "AB12", AuthToken: f.token}, nil
}

func (f *fakePlexLink) Resources(_ context.Context, token string) ([]plex.Resource, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if token != f.token {
		return nil, plex.ErrNotFound
	}
	return f.resources, nil
}

func (f *fakePlexLink) approve(token string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.token = token
}

func (f *fakePlexLink) checkCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.checks
}

func plexServer(name, token string, owned bool, conns ...plex.Connection) plex.Resource {
	return plex.Resource{
		Name: name, Provides: "server", ClientIdentifier: name + "-id",
		AccessToken: token, Owned: owned, Connections: conns,
	}
}

func useFakePlexLink(t *testing.T, f *fakePlexLink) *clockwork.FakeClock {
	t.Helper()
	origClient, origIdentity, origClock := newPlexLinkClient, plexServerIdentity, plexLinkClock
	clock := clockwork.NewFakeClock()
	newPlexLinkClient = func(string) plexLinkClient { return f }
	plexServerIdentity = f.identity
	plexLinkClock = clock
	t.Cleanup(func() {
		newPlexLinkClient = origClient
		plexServerIdentity = origIdentity
		plexLinkClock = origClock
	})
	config.SetAuthCfgForTesting(map[string]config.CredentialEntry{})
	t.Cleanup(config.ClearAuthCfgForTesting)
	return clock
}

func plexLinkEnv(t *testing.T, serverURL string) (requests.RequestEnv, *config.Instance, <-chan models.Notification) {
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
		URL: serverURL, Provider: models.AuthLinkProviderPlex,
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

// startPlexLink starts the flow and returns the notifications after the
// pending one.
func startPlexLink(t *testing.T, serverURL string) (cfg *config.Instance, ch <-chan models.Notification) {
	t.Helper()
	env, cfg, ch := plexLinkEnv(t, serverURL)
	result, err := HandleSettingsAuthLink(env, nil)
	require.NoError(t, err)
	started, ok := result.(models.AuthLinkStatusResponse)
	require.True(t, ok)
	assert.Equal(t, models.AuthLinkStatusPending, started.Status)
	assert.Equal(t, models.AuthLinkProviderPlex, started.Provider)
	assert.Equal(t, "AB12", started.UserCode)
	assert.Equal(t, plexLinkURL, started.VerificationURL)
	require.NotNil(t, started.ExpiresAt)

	pending := readAuthLinkNotification(t, ch)
	assert.Equal(t, models.AuthLinkStatusPending, pending.Status)
	assertAuthLinkNotificationRedacted(t, &pending)
	return cfg, ch
}

// waitForPlexPoll returns once the poll loop is idle, waiting for its next
// check.
func waitForPlexPoll(t *testing.T, clock *clockwork.FakeClock) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, clock.BlockUntilContext(ctx, 1))
}

// pollPlexOnce lets the poll loop run one check.
func pollPlexOnce(t *testing.T, clock *clockwork.FakeClock, step time.Duration) {
	t.Helper()
	waitForPlexPoll(t, clock)
	clock.Advance(step)
}

func plexLauncherServerURL(cfg *config.Instance) string {
	return cfg.LookupLauncherDefaults(shared.LauncherPlexMovie, []string{shared.LauncherGroupPlex}).ServerURL
}

func TestSettingsAuthLinkPlex_Approved(t *testing.T) {
	// Not parallel: swaps the plex.tv client and uses package-level link state.
	resetAuthLinkState(t)
	fake := &fakePlexLink{resources: []plex.Resource{
		{Name: "Phone", Provides: "player", Owned: true},
		plexServer("Home", "server-token", true,
			plex.Connection{URI: "https://relay.plex.direct:8443", Protocol: "https", Relay: true},
			plex.Connection{URI: "https://1-2-3-4.abc.plex.direct:32400", Protocol: "https"},
			plex.Connection{URI: "http://192.168.1.5:32400/", Protocol: "http", Local: true},
		),
	}}
	clock := useFakePlexLink(t, fake)
	cfg, notificationCh := startPlexLink(t, "")

	pollPlexOnce(t, clock, plexLinkPollInterval)
	pollPlexOnce(t, clock, plexLinkPollInterval)
	waitForPlexPoll(t, clock)
	fake.approve("account-token")
	pollPlexOnce(t, clock, plexLinkPollInterval)

	done := readAuthLinkNotification(t, notificationCh)
	assert.Equal(t, models.AuthLinkStatusApproved, done.Status, done.Error)
	assert.Equal(t, models.AuthLinkProviderPlex, done.Provider)
	assertAuthLinkNotificationRedacted(t, &done)
	assert.Equal(t, 3, fake.checkCount())

	server := config.LookupAuth(config.GetAuthCfg(), "http://192.168.1.5:32400/library/sections")
	require.NotNil(t, server, "the local connection is stored, not the relay")
	assert.Equal(t, "server-token", server.Bearer)
	account := config.LookupAuth(config.GetAuthCfg(), fakePlexClientsURL+"/api/v2/resources")
	require.NotNil(t, account)
	assert.Equal(t, "account-token", account.Bearer)
	assert.Equal(t, "http://192.168.1.5:32400", plexLauncherServerURL(cfg))
}

func TestSettingsAuthLinkPlex_URLSelectsServer(t *testing.T) {
	// Not parallel: swaps the plex.tv client and uses package-level link state.
	resetAuthLinkState(t)
	fake := &fakePlexLink{resources: []plex.Resource{
		plexServer("Home", "home-token", true,
			plex.Connection{
				URI: "https://192-168-1-5.abc.plex.direct:32400", Address: "192.168.1.5",
				Port: 32400, Protocol: "https", Local: true,
			}),
		plexServer("Office", "office-token", true,
			plex.Connection{URI: "http://10.0.0.9:32400", Address: "10.0.0.9", Port: 32400, Local: true}),
	}}
	clock := useFakePlexLink(t, fake)
	fake.approve("account-token")
	cfg, notificationCh := startPlexLink(t, "http://192.168.1.5:32400")
	pollPlexOnce(t, clock, plexLinkPollInterval)

	done := readAuthLinkNotification(t, notificationCh)
	require.Equal(t, models.AuthLinkStatusApproved, done.Status, done.Error)
	entry := config.LookupAuth(config.GetAuthCfg(), "http://192.168.1.5:32400/identity")
	require.NotNil(t, entry, "the server matched by address keeps the URL the user gave")
	assert.Equal(t, "home-token", entry.Bearer)
	assert.Equal(t, "http://192.168.1.5:32400", plexLauncherServerURL(cfg))
}

func TestSettingsAuthLinkPlex_SeveralServersNeedURL(t *testing.T) {
	// Not parallel: swaps the plex.tv client and uses package-level link state.
	resetAuthLinkState(t)
	fake := &fakePlexLink{resources: []plex.Resource{
		plexServer("Home", "home-token", true, plex.Connection{URI: "http://192.168.1.5:32400", Local: true}),
		plexServer("Office", "office-token", true, plex.Connection{URI: "http://10.0.0.9:32400", Local: true}),
		plexServer("Friend", "friend-token", false, plex.Connection{URI: "http://10.9.9.9:32400", Local: true}),
	}}
	clock := useFakePlexLink(t, fake)
	fake.approve("account-token")
	cfg, notificationCh := startPlexLink(t, "")
	pollPlexOnce(t, clock, plexLinkPollInterval)

	done := readAuthLinkNotification(t, notificationCh)
	assert.Equal(t, models.AuthLinkStatusFailed, done.Status)
	assert.Contains(t, done.Error, "several Plex servers found, start over with url set to one")
	assert.Contains(t, done.Error, `"Home" (http://192.168.1.5:32400)`)
	assert.Contains(t, done.Error, `"Office" (http://10.0.0.9:32400)`)
	assert.NotContains(t, done.Error, "Friend", "servers the account does not own are not offered")
	assert.Empty(t, config.GetAuthCfg(), "nothing is stored")
	assert.Empty(t, plexLauncherServerURL(cfg))
}

func TestSettingsAuthLinkPlex_NonAdminRejected(t *testing.T) {
	// Not parallel: swaps the plex.tv client and uses package-level link state.
	resetAuthLinkState(t)
	fake := &fakePlexLink{resources: []plex.Resource{
		plexServer("Friend", "friend-token", false,
			plex.Connection{URI: "http://10.9.9.9:32400", Address: "10.9.9.9", Port: 32400, Local: true}),
	}}
	clock := useFakePlexLink(t, fake)
	fake.approve("account-token")

	_, notificationCh := startPlexLink(t, "")
	pollPlexOnce(t, clock, plexLinkPollInterval)
	done := readAuthLinkNotification(t, notificationCh)
	assert.Equal(t, models.AuthLinkStatusFailed, done.Status)
	assert.Contains(t, done.Error, "not the admin of any server")

	resetAuthLinkState(t)
	_, notificationCh = startPlexLink(t, "http://10.9.9.9:32400")
	pollPlexOnce(t, clock, plexLinkPollInterval)
	done = readAuthLinkNotification(t, notificationCh)
	assert.Equal(t, models.AuthLinkStatusFailed, done.Status)
	assert.Contains(t, done.Error, `not the admin of "Friend"`)
	assert.Empty(t, config.GetAuthCfg())
}

func TestSettingsAuthLinkPlex_Expired(t *testing.T) {
	// Not parallel: swaps the plex.tv client and uses package-level link state.
	resetAuthLinkState(t)
	fake := &fakePlexLink{}
	clock := useFakePlexLink(t, fake)

	_, notificationCh := startPlexLink(t, "")
	pollPlexOnce(t, clock, plexLinkPollInterval)
	pollPlexOnce(t, clock, 900*time.Second)
	done := readAuthLinkNotification(t, notificationCh)
	assert.Equal(t, models.AuthLinkStatusFailed, done.Status)
	assert.Equal(t, "link code expired, start over to link this device", done.Error)
	assert.Equal(t, 1, fake.checkCount(), "an expired code is not checked again")

	// plex.tv dropping the PIN early ends the link the same way.
	resetAuthLinkState(t)
	fake.mu.Lock()
	fake.expired = true
	fake.mu.Unlock()
	_, notificationCh = startPlexLink(t, "")
	pollPlexOnce(t, clock, plexLinkPollInterval)
	done = readAuthLinkNotification(t, notificationCh)
	assert.Equal(t, models.AuthLinkStatusFailed, done.Status)
	assert.Equal(t, "link code expired, start over to link this device", done.Error)
	assert.Empty(t, config.GetAuthCfg())
}

func TestSettingsAuthLinkPlex_RequiresLocalOrAdmin(t *testing.T) {
	// Not parallel: swaps the plex.tv client and uses package-level link state.
	resetAuthLinkState(t)
	useFakePlexLink(t, &fakePlexLink{})

	env, _, _ := plexLinkEnv(t, "")
	env.IsLocal = false
	env.ClientRole = string(permissions.RoleMember)
	_, err := HandleSettingsAuthLink(env, nil)
	var clientErr *models.ClientError
	require.ErrorAs(t, err, &clientErr)
	assert.Nil(t, activeAuthLink, "a refused start leaves no session")

	env.ClientRole = string(permissions.RoleAdmin)
	_, err = HandleSettingsAuthLink(env, nil)
	require.NoError(t, err)
}

func TestSettingsAuthLinkPlex_InvalidURL(t *testing.T) {
	// Not parallel: swaps the plex.tv client and uses package-level link state.
	resetAuthLinkState(t)
	useFakePlexLink(t, &fakePlexLink{})

	for _, u := range []string{
		"ftp://plex.test", "http://user:pw@plex.test:32400", "http://plex.test:32400/?token=x", "http://",
	} {
		env, _, _ := plexLinkEnv(t, u)
		_, err := HandleSettingsAuthLink(env, nil)
		var clientErr *models.ClientError
		require.ErrorAs(t, err, &clientErr, u)
	}
	assert.Nil(t, activeAuthLink)
}

func TestSelectPlexServer(t *testing.T) {
	t.Parallel()
	remoteOnly := plexServer("Remote", "t", true,
		plex.Connection{URI: "https://relay.plex.direct", Protocol: "https", Relay: true},
		plex.Connection{URI: "http://8.8.8.8:32400", Protocol: "http"},
		plex.Connection{URI: "https://8-8-8-8.abc.plex.direct:32400", Protocol: "https"},
	)
	server, conns, msg := selectPlexServer([]plex.Resource{remoteOnly}, "")
	require.Empty(t, msg)
	assert.Equal(t, "Remote", server.Name)
	assert.Equal(t, []string{"https://8-8-8-8.abc.plex.direct:32400"}, conns, "plain remote http is not stored")

	relayOnly := plexServer("Relay", "t", true,
		plex.Connection{URI: "https://relay.plex.direct", Protocol: "https", Relay: true})
	_, _, msg = selectPlexServer([]plex.Resource{relayOnly}, "")
	assert.Contains(t, msg, "has no direct connection")

	_, _, msg = selectPlexServer([]plex.Resource{{Name: "Phone", Provides: "player"}}, "")
	assert.Contains(t, msg, "has no servers")

	_, _, msg = selectPlexServer([]plex.Resource{remoteOnly}, "http://10.0.0.1:32400")
	assert.Contains(t, msg, "no Plex server on the account answers at http://10.0.0.1:32400")
	assert.Contains(t, msg, `"Remote"`)
}

func TestSelectPlexServerPrefersExactURI(t *testing.T) {
	t.Parallel()
	// A stale server left on the account after a reinstall still lists the
	// LAN address the new one answers at.
	stale := plexServer("Old", "old-token", true,
		plex.Connection{URI: "http://192.168.1.5:32400", Address: "192.168.1.5", Port: 32400, Local: true})
	current := plexServer("New", "new-token", true,
		plex.Connection{
			URI: "https://192-168-1-5.def.plex.direct:32400", Address: "192.168.1.5",
			Port: 32400, Protocol: "https", Local: true,
		})
	resources := []plex.Resource{stale, current}

	server, conns, msg := selectPlexServer(resources, "https://192-168-1-5.def.plex.direct:32400")
	require.Empty(t, msg)
	assert.Equal(t, "New", server.Name, "the URI match wins over an address match")
	assert.Equal(t, []string{"https://192-168-1-5.def.plex.direct:32400"}, conns)

	_, _, msg = selectPlexServer(resources, "http://192.168.1.5:32400")
	assert.Empty(t, msg, "the stale server's own URI is an exact match")

	_, _, msg = selectPlexServer(resources, "https://192.168.1.5:32400")
	assert.Contains(t, msg, "several Plex servers on the account list https://192.168.1.5:32400")
}

func TestSettingsAuthLinkPlex_SkipsUnreachableConnection(t *testing.T) {
	// Not parallel: swaps the plex.tv client and uses package-level link state.
	resetAuthLinkState(t)
	fake := &fakePlexLink{
		resources: []plex.Resource{plexServer("Home", "server-token", true,
			plex.Connection{URI: "http://172.17.0.2:32400", Protocol: "http", Local: true},
			plex.Connection{URI: "http://192.168.1.5:32400", Protocol: "http", Local: true},
		)},
		unreachable: map[string]bool{"http://172.17.0.2:32400": true},
	}
	clock := useFakePlexLink(t, fake)
	fake.approve("account-token")
	cfg, notificationCh := startPlexLink(t, "")
	pollPlexOnce(t, clock, plexLinkPollInterval)

	done := readAuthLinkNotification(t, notificationCh)
	require.Equal(t, models.AuthLinkStatusApproved, done.Status, done.Error)
	assert.Equal(t, "http://192.168.1.5:32400", plexLauncherServerURL(cfg),
		"the container address the server reported is skipped")
}

func TestSettingsAuthLinkPlex_NoConnectionAnswers(t *testing.T) {
	// Not parallel: swaps the plex.tv client and uses package-level link state.
	resetAuthLinkState(t)
	fake := &fakePlexLink{
		resources: []plex.Resource{plexServer("Home", "server-token", true,
			plex.Connection{URI: "http://172.17.0.2:32400", Protocol: "http", Local: true},
		)},
		unreachable: map[string]bool{"http://172.17.0.2:32400": true},
	}
	clock := useFakePlexLink(t, fake)
	fake.approve("account-token")
	cfg, notificationCh := startPlexLink(t, "")
	pollPlexOnce(t, clock, plexLinkPollInterval)

	done := readAuthLinkNotification(t, notificationCh)
	require.Equal(t, models.AuthLinkStatusFailed, done.Status)
	assert.Contains(t, done.Error, `"Home" did not answer at any address plex.tv lists`)
	assert.Empty(t, plexLauncherServerURL(cfg))
	assert.Nil(t, config.LookupAuth(config.GetAuthCfg(), "http://172.17.0.2:32400/identity"))
}
