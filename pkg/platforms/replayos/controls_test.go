//go:build linux && !android

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

package replayos

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recordedRequest struct {
	token    string
	endpoint string
	query    string
}

// fakeReplay serves the net control API. view is the get_status view; every
// other request is recorded.
type fakeReplay struct {
	view     string
	token    string
	gameFile string
	// views are returned by successive get_status calls ahead of view.
	views     []string
	requests  []recordedRequest
	failWith  int
	rateLimit int
}

func (f *fakeReplay) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.requests = append(f.requests, recordedRequest{
		token:    r.Header.Get("X-RePlay-Token"),
		endpoint: filepath.Base(r.URL.Path),
		query:    r.URL.RawQuery,
	})
	if f.rateLimit > 0 {
		f.rateLimit--
		w.WriteHeader(http.StatusTooManyRequests)
		return
	}
	if f.failWith != 0 {
		w.WriteHeader(f.failWith)
		_, _ = w.Write([]byte(`{"error":"Invalid Slot"}`))
		return
	}
	if r.Header.Get("X-RePlay-Token") != f.token {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"Unauthorized"}`))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if filepath.Base(r.URL.Path) == "get_status" {
		view := f.view
		if len(f.views) > 0 {
			view, f.views = f.views[0], f.views[1:]
		}
		_, _ = w.Write([]byte(
			`{"system":"snes","game_file":"` + f.gameFile + `","view":"` + view + `","paused":false}`,
		))
		return
	}
	_, _ = w.Write([]byte(`{"command":"ok"}`))
}

func newTestNetControl(t *testing.T, f *fakeReplay, cfg string) *netControl {
	t.Helper()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	cfgPath := filepath.Join(t.TempDir(), "replay.cfg")
	require.NoError(t, os.WriteFile(cfgPath, []byte(cfg), 0o600))
	n := newNetControl()
	n.baseURL = srv.URL + "/api/v1"
	n.cfgPath = cfgPath
	return n
}

const testCfg = "wifi_name = \"x\"\nreplay_http_token = \"604318\"\nsystem_net_control = \"true\"\n"

func TestNetControl_StateControls(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		endpoint  string
		args      map[string]string
		wantQuery string
	}{
		{"save defaults to slot 1", "save_state", nil, "slot=1"},
		{"save slot 18", "save_state", map[string]string{"slot": "18"}, "slot=18"},
		{"load defaults to slot 1", "load_state", nil, "slot=1"},
		{"load slot 7", "load_state", map[string]string{"slot": "7"}, "slot=7"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := &fakeReplay{view: "game_play", token: "604318"}
			n := newTestNetControl(t, f, testCfg)

			params := platforms.ControlParams{Args: tt.args}
			require.NoError(t, n.stateControl(tt.endpoint)(context.Background(), nil, params))

			require.Len(t, f.requests, 2)
			assert.Equal(t, "get_status", f.requests[0].endpoint)
			assert.Equal(t, tt.endpoint, f.requests[1].endpoint)
			assert.Equal(t, tt.wantQuery, f.requests[1].query)
			assert.Equal(t, "604318", f.requests[1].token, "the code comes from replay.cfg")
		})
	}
}

func TestNetControl_SlotValidation(t *testing.T) {
	t.Parallel()

	for _, slot := range []string{"0", "19", "-3", "abc"} {
		t.Run(slot, func(t *testing.T) {
			t.Parallel()
			f := &fakeReplay{view: "game_play", token: "604318"}
			n := newTestNetControl(t, f, testCfg)
			err := n.stateControl("save_state")(context.Background(), nil,
				platforms.ControlParams{Args: map[string]string{"slot": slot}})
			require.Error(t, err)
			assert.Empty(t, f.requests, "an invalid slot must not reach RePlayOS")
		})
	}
}

func TestNetControl_Reset(t *testing.T) {
	t.Parallel()

	f := &fakeReplay{view: "game_play", token: "604318"}
	n := newTestNetControl(t, f, testCfg)
	require.NoError(t, n.reset(context.Background(), nil, platforms.ControlParams{}))
	require.Len(t, f.requests, 2)
	assert.Equal(t, "set_cmd", f.requests[1].endpoint)
	assert.Equal(t, "cmd=game_reset", f.requests[1].query)
}

func TestNetControl_RefusedOutsideAGame(t *testing.T) {
	t.Parallel()

	for _, view := range []string{"system_list", "game_options"} {
		t.Run(view, func(t *testing.T) {
			t.Parallel()
			f := &fakeReplay{view: view, token: "604318"}
			n := newTestNetControl(t, f, testCfg)

			err := n.reset(context.Background(), nil, platforms.ControlParams{})
			require.ErrorIs(t, err, errNoGameRunning)
			require.ErrorIs(t, err, platforms.ErrControlUnavailable)
			err = n.stateControl("save_state")(context.Background(), nil, platforms.ControlParams{})
			require.ErrorIs(t, err, errNoGameRunning)

			for _, r := range f.requests {
				assert.Equal(t, "get_status", r.endpoint, "no command may be queued without a game")
			}
		})
	}
}

func TestNetControl_Unauthorized(t *testing.T) {
	t.Parallel()

	f := &fakeReplay{view: "game_play", token: "other"}
	n := newTestNetControl(t, f, testCfg)
	require.ErrorIs(t, n.reset(context.Background(), nil, platforms.ControlParams{}), errNetControlToken)
}

func TestNetControl_NoTokenInConfig(t *testing.T) {
	t.Parallel()

	//nolint:gosec // config key name in a fixture, not a credential
	for name, cfg := range map[string]string{
		"missing key": "system_net_control = \"true\"\n",
		"empty token": "replay_http_token = \"\"\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := &fakeReplay{view: "game_play", token: "604318"}
			n := newTestNetControl(t, f, cfg)
			require.ErrorIs(t, n.reset(context.Background(), nil, platforms.ControlParams{}), errNetControlUnavailable)
			assert.Empty(t, f.requests)
		})
	}
}

func TestNetControl_ServerDown(t *testing.T) {
	t.Parallel()

	f := &fakeReplay{view: "game_play", token: "604318"}
	n := newTestNetControl(t, f, testCfg)
	n.baseURL = "http://127.0.0.1:1/api/v1"
	err := n.reset(context.Background(), nil, platforms.ControlParams{})
	require.ErrorIs(t, err, errNetControlUnavailable)
	assert.Contains(t, err.Error(), "NET_CONTROL")
}

func TestNetControl_APIErrorSurfaced(t *testing.T) {
	t.Parallel()

	f := &fakeReplay{view: "game_play", token: "604318", failWith: http.StatusBadRequest}
	n := newTestNetControl(t, f, testCfg)
	err := n.reset(context.Background(), nil, platforms.ControlParams{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Invalid Slot")
}

func TestReadCfgValue_LastValueWins(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "replay.cfg")
	require.NoError(t, os.WriteFile(path,
		[]byte("# comment\nreplay_http_token = \"111111\"\n\nreplay_http_token = \"222222\"\n"), 0o600))
	v, ok := readCfgValue(path, "replay_http_token")
	assert.True(t, ok)
	assert.Equal(t, "222222", v)

	_, ok = readCfgValue(path, "absent")
	assert.False(t, ok)
	_, ok = readCfgValue(filepath.Join(t.TempDir(), "none.cfg"), "replay_http_token")
	assert.False(t, ok)
}

func TestPlatformControls(t *testing.T) {
	t.Parallel()

	cfg, err := helpers.NewTestConfig(helpers.NewMemoryFS(), t.TempDir())
	require.NoError(t, err)

	p := &Platform{}
	assert.Equal(t,
		[]string{
			platforms.ControlLoadState, platforms.ControlReset,
			platforms.ControlSaveState, platforms.ControlStop,
		},
		platforms.ControlNames(&platforms.Launcher{Controls: p.controls()}),
	)

	for _, l := range p.Launchers(cfg) {
		if l.Launch != nil && l.ID != "Generic" {
			assert.Contains(t, l.Controls, platforms.ControlSaveState, l.ID)
		}
	}
}

func TestSystemInfoFromROMPath(t *testing.T) {
	t.Parallel()

	info, ok := systemInfoFromROMPath("/media/sd/roms/nintendo_snes/Game.sfc", "/media/sd")
	require.True(t, ok)
	assert.Equal(t, SystemMap["nintendo_snes"].SystemID, info.SystemID)

	_, ok = systemInfoFromROMPath("/media/sd/roms/unknown_folder/Game.bin", "/media/sd")
	assert.False(t, ok)
	_, ok = systemInfoFromROMPath("", "/media/sd")
	assert.False(t, ok)
}
