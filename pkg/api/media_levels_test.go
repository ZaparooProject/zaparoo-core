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

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/audio"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/gorilla/websocket"
	"github.com/olahol/melody"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// levelPlaybackStub is a playback manager that can report levels.
type levelPlaybackStub struct {
	apiPlaybackStub
	fn  func(string, *audio.Levels)
	mu  syncutil.Mutex
	set int
}

func (p *levelPlaybackStub) SetLevelListener(fn func(string, *audio.Levels)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.fn = fn
	p.set++
}

func (p *levelPlaybackStub) listener() func(string, *audio.Levels) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.fn
}

func startLevelsWSServer(t *testing.T, playback *levelPlaybackStub) string {
	t.Helper()
	cfg, err := config.NewConfig(t.TempDir(), config.BaseDefaults)
	require.NoError(t, err)
	st, _ := state.NewState(nil, "test-boot")
	hub := newLevelsHub(playback)
	require.NotNil(t, hub)

	m := newWebSocketSession()
	m.HandleConnect(hub.attach)
	m.HandleDisconnect(func(s *melody.Session) {
		closeWSDispatcher(s)
		hub.remove(s)
	})
	m.HandleMessage(handleWSMessage(
		NewMethodMap(), nil, cfg, st, nil, nil,
		nil, nil, nil, nil, playback, nil,
		nil, nil, nil, nil, nil, false,
	))
	mux := http.NewServeMux()
	mux.HandleFunc("/api", func(w http.ResponseWriter, r *http.Request) {
		_ = m.HandleRequest(w, r)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(func() {
		st.StopService()
		_ = m.Close()
		srv.Close()
	})
	u, err := url.Parse(srv.URL)
	require.NoError(t, err)
	return "ws://" + u.Host + "/api"
}

type wsFrame struct {
	ID     *int            `json:"id"`
	Error  json.RawMessage `json:"error"`
	Result json.RawMessage `json:"result"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

func readFrame(t *testing.T, c *websocket.Conn) wsFrame {
	t.Helper()
	require.NoError(t, c.SetReadDeadline(time.Now().Add(5*time.Second)))
	_, data, err := c.ReadMessage()
	require.NoError(t, err)
	var f wsFrame
	require.NoError(t, json.Unmarshal(data, &f))
	return f
}

func subscribeLevels(t *testing.T, c *websocket.Conn, id int, params string) wsFrame {
	t.Helper()
	req := `{"jsonrpc":"2.0","id":` + strconv.Itoa(id) + `,"method":"media.levels.subscribe","params":` + params + `}`
	require.NoError(t, c.WriteMessage(websocket.TextMessage, []byte(req)))
	f := readFrame(t, c)
	require.NotNil(t, f.ID)
	assert.Equal(t, id, *f.ID)
	return f
}

func TestMediaLevelsOnlyReachSubscribedConnections(t *testing.T) {
	t.Parallel()
	playback := &levelPlaybackStub{}
	wsURL := startLevelsWSServer(t, playback)

	a := dialWS(t, wsURL)
	defer func() { _ = a.Close() }()
	b := dialWS(t, wsURL)
	defer func() { _ = b.Close() }()

	// b has spoken, so it is settled and could receive notifications.
	f := subscribeLevels(t, b, 1, `{"enabled":false}`)
	require.Empty(t, f.Error)
	assert.Nil(t, playback.listener(), "nothing is measured without a subscriber")

	f = subscribeLevels(t, a, 1, `{"enabled":true}`)
	require.Empty(t, f.Error)
	assert.JSONEq(t, "null", string(f.Result))
	fn := playback.listener()
	require.NotNil(t, fn)

	var levels audio.Levels
	levels.Peak = 0.81234
	levels.Bands[3] = 0.5
	fn("primary", &levels)

	f = readFrame(t, a)
	assert.Equal(t, models.NotificationMediaLevels, f.Method)
	var got models.MediaLevelsNotification
	require.NoError(t, json.Unmarshal(f.Params, &got))
	assert.Equal(t, "primary", got.Slot)
	assert.InDelta(t, 0.812, got.Peak, 1e-9)
	require.Len(t, got.Bands, audio.LevelBands)
	assert.InDelta(t, 0.5, got.Bands[3], 1e-9)

	// b's next frame is the answer to its next request, not a notification.
	f = subscribeLevels(t, b, 2, `{"enabled":false}`)
	assert.Empty(t, f.Method)

	// A second subscriber does not restart measurement.
	subscribeLevels(t, b, 3, `{"enabled":true}`)
	subscribeLevels(t, b, 4, `{"enabled":false}`)
	playback.mu.Lock()
	assert.Equal(t, 1, playback.set)
	playback.mu.Unlock()

	// Unsubscribing the last connection stops measurement.
	subscribeLevels(t, a, 2, `{"enabled":false}`)
	assert.Nil(t, playback.listener())
}

func TestMediaLevelsEndOnDisconnect(t *testing.T) {
	t.Parallel()
	playback := &levelPlaybackStub{}
	wsURL := startLevelsWSServer(t, playback)

	a := dialWS(t, wsURL)
	subscribeLevels(t, a, 1, `{"enabled":true}`)
	require.NotNil(t, playback.listener())
	require.NoError(t, a.Close())
	assert.Eventually(t, func() bool { return playback.listener() == nil }, 5*time.Second, 10*time.Millisecond)
}

func TestMediaLevelsSubscribeRejectsBadParams(t *testing.T) {
	t.Parallel()
	playback := &levelPlaybackStub{}
	wsURL := startLevelsWSServer(t, playback)
	a := dialWS(t, wsURL)
	defer func() { _ = a.Close() }()
	for i, params := range []string{`{}`, `{"enabled":"yes"}`, `null`} {
		f := subscribeLevels(t, a, i+1, params)
		assert.NotEmpty(t, f.Error, params)
	}
	assert.Nil(t, playback.listener())
}

func TestNewLevelsHubNeedsLevelSource(t *testing.T) {
	t.Parallel()
	assert.Nil(t, newLevelsHub(&apiPlaybackStub{}))
	assert.Nil(t, newLevelsHub(nil))
	var hub *levelsHub
	hub.remove(nil)
}
