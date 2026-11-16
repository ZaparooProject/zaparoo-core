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

package jellyfin

import (
	"context"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuthorizationHeaderFormat(t *testing.T) {
	t.Parallel()
	f := newFakeJellyfin(t)
	c := testClient(t, f, "")

	_, err := c.CurrentUserID(context.Background())
	require.NoError(t, err)

	reqs := f.recorded()
	require.Len(t, reqs, 1)
	assert.Equal(t, "/Users/Me", reqs[0].path)
	want := `MediaBrowser Client="Zaparoo", Device="` + url.PathEscape(deviceName()) +
		`", DeviceId="zaparoo-test-device", Version="` + url.PathEscape(config.AppVersion) +
		`", Token="` + testToken + `"`
	assert.Equal(t, want, reqs[0].auth)
	assert.Empty(t, reqs[0].query.Get("api_key"), "the token never travels in the query")
}

func TestAuthorizationHeaderWithoutToken(t *testing.T) {
	t.Parallel()
	h := authorizationHeader(`dev "1", x`, "")
	assert.NotContains(t, h, "Token=")
	assert.Contains(t, h, `DeviceId="dev%20%221%22%2C%20x"`)
}

func TestCurrentUserIDIsCached(t *testing.T) {
	t.Parallel()
	f := newFakeJellyfin(t)
	c := testClient(t, f, "")
	for range 3 {
		got, err := c.CurrentUserID(context.Background())
		require.NoError(t, err)
		assert.Equal(t, testUserID, got)
	}
	assert.Len(t, f.recorded(), 1)
}

func TestNewClientWithLauncherID(t *testing.T) {
	t.Parallel()
	f := newFakeJellyfin(t)
	cfg := testConfig(t, f.URL()+"/", "Living Room")

	c, err := NewClientWithLauncherID(cfg, shared.LauncherJellyfinMovie, []string{shared.LauncherGroupJellyfin})
	require.NoError(t, err)
	assert.Equal(t, "Living Room", c.PlayerName())
	assert.Equal(t, f.URL(), c.base.String())
	assert.Equal(t, DeviceID(cfg), c.DeviceID())
	_, err = c.CurrentUserID(context.Background())
	require.NoError(t, err, "the token stored for the server is sent")
}

func TestNewClientWithLauncherIDNotConfigured(t *testing.T) {
	t.Parallel()
	cfg, err := config.NewConfigWithFs(t.TempDir(), config.BaseDefaults, afero.NewMemMapFs())
	require.NoError(t, err)
	_, err = NewClientWithLauncherID(cfg, shared.LauncherJellyfinMovie, []string{shared.LauncherGroupJellyfin})
	require.ErrorIs(t, err, ErrNotConfigured)
}

func TestNewClientWithLauncherIDNotLinked(t *testing.T) {
	t.Parallel()
	defaults := config.BaseDefaults
	defaults.Launchers.Default = []config.LaunchersDefault{{
		Launcher: shared.LauncherGroupJellyfin, ServerURL: "http://jellyfin-unlinked.test:8096",
	}}
	cfg, err := config.NewConfigWithFs(t.TempDir(), defaults, afero.NewMemMapFs())
	require.NoError(t, err)
	_, err = NewClientWithLauncherID(cfg, shared.LauncherJellyfinSong, []string{shared.LauncherGroupJellyfin})
	require.ErrorIs(t, err, ErrNotLinked)
}

func TestNormalizeServerURL(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"jellyfin.local:8096":           "http://jellyfin.local:8096",
		"HTTPS://media.example/jf/":     "https://media.example/jf",
		"\thttp://10.0.0.5:8096\n":      "http://10.0.0.5:8096",
		"https://jellyfin.example.com/": "https://jellyfin.example.com",
	} {
		got, err := NormalizeServerURL(in)
		require.NoError(t, err, in)
		assert.Equal(t, want, got, in)
	}
	for _, bad := range []string{"", "ftp://x", "http://u:p@x", "http://x/?a=1", "http://x/#f"} {
		_, err := NormalizeServerURL(bad)
		require.Error(t, err, bad)
	}
}

func TestUnreachableServerWrapsScannerError(t *testing.T) {
	t.Parallel()
	f := newFakeJellyfin(t)
	c := testClient(t, f, "")
	f.srv.Close()
	_, err := c.CurrentUserID(context.Background())
	require.ErrorIs(t, err, platforms.ErrScannerSourceUnreachable)
}

func TestServerErrorsWrapScannerError(t *testing.T) {
	t.Parallel()
	f := newFakeJellyfin(t)
	f.getItemErr = http.StatusInternalServerError
	c := testClient(t, f, "")
	_, err := c.GetItem(context.Background(), id(1))
	require.ErrorIs(t, err, platforms.ErrScannerSourceUnreachable)
}

func TestRejectedTokenIsUnauthorized(t *testing.T) {
	t.Parallel()
	f := newFakeJellyfin(t)
	c, err := NewClient(f.URL(), "wrong", "", "dev")
	require.NoError(t, err)
	_, err = c.CurrentUserID(context.Background())
	require.ErrorIs(t, err, ErrUnauthorized)
	assert.NotErrorIs(t, err, platforms.ErrScannerSourceUnreachable)
}

func TestGetItemRejectsInvalidID(t *testing.T) {
	t.Parallel()
	f := newFakeJellyfin(t)
	c := testClient(t, f, "")
	for _, bad := range []string{"", "../Users", "abc/def", "zz"} {
		_, err := c.GetItem(context.Background(), bad)
		require.Error(t, err, bad)
	}
	assert.Empty(t, f.recorded(), "an invalid ID never reaches the server")
}

func TestSelectSession(t *testing.T) {
	t.Parallel()
	now := time.Now()
	sessions := []Session{
		{
			ID: "s-tv", DeviceName: "Living Room TV", Client: "Jellyfin Android TV", DeviceID: "dev-tv",
			LastActivityDate: now,
		},
		{
			ID: "s-web", DeviceName: "Firefox", Client: "Jellyfin Web", DeviceID: "dev-web",
			LastActivityDate: now.Add(-time.Minute),
		},
		{
			ID: "s-self", DeviceName: "core", Client: "Zaparoo", DeviceID: "zaparoo-test-device",
			LastActivityDate: now.Add(time.Minute),
		},
	}
	tests := []struct {
		name    string
		player  string
		want    string
		wantErr []string
	}{
		{name: "device name", player: "living room tv", want: "s-tv"},
		{name: "client name", player: "Jellyfin Web", want: "s-web"},
		{name: "device id", player: "DEV-TV", want: "s-tv"},
		{name: "not found lists players", player: "Bedroom", wantErr: []string{
			`"Bedroom"`, "Living Room TV (Jellyfin Android TV)", "Firefox (Jellyfin Web)",
		}},
		{name: "unset with several players", player: "", wantErr: []string{"set client"}},
		{name: "own session is never a player", player: "Zaparoo", wantErr: []string{`"Zaparoo"`}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newFakeJellyfin(t)
			f.setSessions(sessions...)
			got, err := selectSession(context.Background(), testClient(t, f, tt.player))
			if tt.wantErr != nil {
				require.ErrorIs(t, err, ErrNoPlayer)
				for _, s := range tt.wantErr {
					assert.Contains(t, err.Error(), s)
				}
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got.ID)
		})
	}
}

func TestSelectSessionSingleDefault(t *testing.T) {
	t.Parallel()
	f := newFakeJellyfin(t)
	f.setSessions(Session{ID: "only", DeviceName: "Shield", Client: "Jellyfin Android TV"})
	got, err := selectSession(context.Background(), testClient(t, f, ""))
	require.NoError(t, err)
	assert.Equal(t, "only", got.ID)
}

func TestSelectSessionNone(t *testing.T) {
	t.Parallel()
	f := newFakeJellyfin(t)
	_, err := selectSession(context.Background(), testClient(t, f, ""))
	require.ErrorIs(t, err, ErrNoPlayer)
	_, err = selectSession(context.Background(), testClient(t, f, "Shield"))
	require.ErrorIs(t, err, ErrNoPlayer)
	assert.Contains(t, err.Error(), `"Shield"`)
}

func TestSelectSessionPrefersMostRecent(t *testing.T) {
	t.Parallel()
	now := time.Now()
	f := newFakeJellyfin(t)
	f.setSessions(
		Session{ID: "old", DeviceName: "Shield", LastActivityDate: now.Add(-time.Hour)},
		Session{ID: "new", DeviceName: "Shield", LastActivityDate: now},
	)
	got, err := selectSession(context.Background(), testClient(t, f, "shield"))
	require.NoError(t, err)
	assert.Equal(t, "new", got.ID)
}
