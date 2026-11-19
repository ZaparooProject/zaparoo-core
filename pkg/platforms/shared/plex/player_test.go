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

package plex

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeCompanion is a player that speaks the Plex Companion HTTP protocol.
type fakeCompanion struct {
	*httptest.Server
	timeline string
	commands []url.URL
	headers  []http.Header
	mu       syncutil.Mutex
	// refuseCreate makes the player answer createPlayQueue with 404, as
	// players that do not know it do.
	refuseCreate bool
	// hang makes the player leave every command open, as a wedged player
	// behind the server's Companion proxy does.
	hang atomic.Bool
}

func newFakeCompanion(t *testing.T) *fakeCompanion {
	t.Helper()
	f := &fakeCompanion{timeline: `<MediaContainer></MediaContainer>`}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.commands = append(f.commands, *r.URL)
		f.headers = append(f.headers, r.Header.Clone())
		f.mu.Unlock()
		if f.hang.Load() {
			<-r.Context().Done()
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		switch {
		case r.URL.Path == "/player/timeline/poll" && r.Header.Get("X-Plex-Device-Name") == "":
			// The server's Companion proxy refuses a poll with no device name.
			http.Error(w, "Bad Request", http.StatusBadRequest)
		case r.URL.Path == "/player/timeline/poll":
			w.Header().Set("Content-Type", "text/xml")
			_, _ = w.Write([]byte(f.timeline))
		case r.URL.Path == "/player/playback/createPlayQueue" && f.refuseCreate:
			http.NotFound(w, r)
		default:
			_, _ = w.Write([]byte(`<Response code="200" status="OK"/>`))
		}
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeCompanion) setTimeline(xml string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.timeline = xml
}

func (f *fakeCompanion) calls(path string) []url.URL {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []url.URL
	for i := range f.commands {
		if f.commands[i].Path == path {
			out = append(out, f.commands[i])
		}
	}
	return out
}

func (f *fakeCompanion) serverClient(name, machineID string) ServerClient {
	u, _ := url.Parse(f.URL)
	port, _ := strconv.Atoi(u.Port())
	return ServerClient{
		Name: name, Address: u.Hostname(), Port: FlexInt(port), MachineIdentifier: machineID,
		ProtocolCapabilities: "timeline,playback,navigation",
	}
}

// fakeTV is plex.tv: PINs on its base URL and the resource list.
type fakeTV struct {
	*httptest.Server
	pins      map[int64]*Pin
	resources []Resource
	tokens    []string
	mu        syncutil.Mutex
}

func newFakeTV(t *testing.T) *fakeTV {
	t.Helper()
	f := &fakeTV{pins: map[int64]*Pin{}}
	f.Server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeTV) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if r.Header.Get("X-Plex-Client-Identifier") == "" || r.Header.Get("X-Plex-Product") == "" {
		http.Error(w, "missing client identity", http.StatusBadRequest)
		return
	}
	switch {
	case r.URL.Path == "/api/v2/pins" && r.Method == http.MethodPost:
		pin := &Pin{ID: FlexInt(len(f.pins) + 7), Code: "AB12", ExpiresIn: 900}
		f.pins[int64(pin.ID)] = pin
		_ = json.NewEncoder(w).Encode(pin) //nolint:gosec // G117: a fake plex.tv returns PIN tokens by design
	case len(r.URL.Path) > len("/api/v2/pins/") && r.URL.Path[:len("/api/v2/pins/")] == "/api/v2/pins/":
		id, _ := strconv.ParseInt(r.URL.Path[len("/api/v2/pins/"):], 10, 64)
		pin, ok := f.pins[id]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(pin) //nolint:gosec // G117: a fake plex.tv returns PIN tokens by design
	case r.URL.Path == "/api/v2/resources":
		f.tokens = append(f.tokens, r.Header.Get("X-Plex-Token"))
		out := f.resources
		if r.URL.Query().Get("includeRelay") == "0" {
			out = nil
			for _, res := range f.resources {
				var conns []Connection
				for _, c := range res.Connections {
					if !c.Relay {
						conns = append(conns, c)
					}
				}
				res.Connections = conns
				out = append(out, res)
			}
		}
		_ = json.NewEncoder(w).Encode(out) //nolint:gosec // G117: a fake plex.tv returns resource tokens by design
	default:
		http.NotFound(w, r)
	}
}

func (f *fakeTV) approve(id int64, token string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pins[id].AuthToken = token
}

func (f *fakeTV) expire(id int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.pins, id)
}

func testFinder(srv *fakeServer, devices []castDevice, dial castDialer) *playerFinder {
	return &playerFinder{
		http: srv.Client(),
		browseCast: func(context.Context) ([]castDevice, error) {
			return devices, nil
		},
		dialCast: dial,
	}
}

func TestCompanionPlay(t *testing.T) {
	// Not parallel: uses the global auth config.
	srv := newFakeServer(t)
	useTokens(t, srv.URL, "", "")
	player := newFakeCompanion(t)
	player.refuseCreate = true
	srv.clients = []ServerClient{player.serverClient("Living Room", "player-1")}

	client := srv.client("living room")
	p, err := testFinder(srv, nil, nil).find(context.Background(), client)
	require.NoError(t, err)
	assert.Equal(t, "Living Room", p.Name())
	assert.Equal(t, playerKindCompanion, p.Kind())

	server, err := newServerInfo(client, &Identity{MachineIdentifier: testMachineID, Version: "1.40"})
	require.NoError(t, err)
	err = p.Play(context.Background(), &playRequest{
		Queue:  &PlayQueue{ID: 42},
		First:  &Metadata{RatingKey: "200"},
		Server: server,
		Kind:   kindMovie,
	})
	require.NoError(t, err, "a player without createPlayQueue still plays")

	plays := player.calls("/player/playback/playMedia")
	require.Len(t, plays, 1)
	q := plays[0].Query()
	assert.Equal(t, "/library/metadata/200", q.Get("key"))
	assert.Equal(t, "/playQueues/42?own=1&window=200", q.Get("containerKey"))
	assert.Equal(t, testMachineID, q.Get("machineIdentifier"))
	assert.Equal(t, "127.0.0.1", q.Get("address"))
	assert.Equal(t, server.Port, mustAtoi(t, q.Get("port")))
	assert.Equal(t, "http", q.Get("protocol"))
	assert.Equal(t, testServerToken, q.Get("token"))
	assert.Equal(t, "video", q.Get("type"))
	assert.NotEmpty(t, q.Get("commandID"))

	creates := player.calls("/player/playback/createPlayQueue")
	require.Len(t, creates, 1)
	assert.Equal(t, libraryURI(testMachineID, "200"), creates[0].Query().Get("uri"))
	assert.Less(t, mustAtoi(t, creates[0].Query().Get("commandID")), mustAtoi(t, q.Get("commandID")))

	player.mu.Lock()
	h := player.headers[len(player.headers)-1]
	player.mu.Unlock()
	assert.Equal(t, "player-1", h.Get("X-Plex-Target-Client-Identifier"))
	assert.Equal(t, testClientID, h.Get("X-Plex-Client-Identifier"))
}

func mustAtoi(t *testing.T, s string) int {
	t.Helper()
	n, err := strconv.Atoi(s)
	require.NoError(t, err)
	return n
}

func TestCompanionCommandAndState(t *testing.T) {
	// Not parallel: uses the global auth config.
	srv := newFakeServer(t)
	useTokens(t, srv.URL, "", "")
	player := newFakeCompanion(t)
	srv.clients = []ServerClient{player.serverClient("Shield", "shield-1")}
	p, err := testFinder(srv, nil, nil).find(context.Background(), srv.client(""))
	require.NoError(t, err, "the only player is used when none is named")

	require.NoError(t, p.Command(context.Background(), cmdPause, kindTrack))
	calls := player.calls("/player/playback/pause")
	require.Len(t, calls, 1)
	assert.Equal(t, "music", calls[0].Query().Get("type"))

	player.setTimeline(`<MediaContainer>
		<Timeline type="music" state="stopped"/>
		<Timeline type="video" state="playing" key="/library/metadata/200"/>
	</MediaContainer>`)
	state, err := p.State(context.Background())
	require.NoError(t, err)
	assert.True(t, state.Playing)
	assert.Equal(t, "200", state.RatingKey)

	player.setTimeline(`<MediaContainer><Timeline type="video" state="paused" ratingKey="201"/></MediaContainer>`)
	state, err = p.State(context.Background())
	require.NoError(t, err)
	assert.False(t, state.Playing)
	assert.True(t, state.Paused)
	assert.Equal(t, "201", state.RatingKey)

	player.setTimeline(`not xml <`)
	_, err = p.State(context.Background())
	require.Error(t, err)
}

// wedgedTransport records each request's path, then leaves it open until the
// request's context ends, as a wedged player does. Recording happens before
// the wait, so it does not depend on how fast a request reaches a server.
type wedgedTransport struct {
	paths []string
	mu    syncutil.Mutex
}

func (w *wedgedTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	w.mu.Lock()
	w.paths = append(w.paths, r.URL.Path)
	w.mu.Unlock()
	<-r.Context().Done()
	return nil, r.Context().Err()
}

func (w *wedgedTransport) calls(path string) int {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := 0
	for _, p := range w.paths {
		if p == path {
			n++
		}
	}
	return n
}

func TestCompanionPlayFailsFastOnWedgedPlayer(t *testing.T) {
	t.Parallel()
	wedged := &wedgedTransport{}
	p := &companionPlayer{
		http:      &http.Client{Transport: wedged},
		name:      "Desktop",
		machineID: "player-1",
		baseURL:   "http://player.invalid:32500",
		timeout:   50 * time.Millisecond,
	}

	err := p.Play(context.Background(), &playRequest{
		Queue:  &PlayQueue{ID: 42},
		First:  &Metadata{RatingKey: "200"},
		Server: serverInfo{MachineID: testMachineID, Address: "127.0.0.1", Port: 32400, Protocol: "http"},
		Kind:   kindMovie,
	})
	require.ErrorIs(t, err, errPlayerNoAnswer)
	assert.Contains(t, err.Error(), `"Desktop"`)
	assert.Equal(t, 1, wedged.calls("/player/playback/createPlayQueue"))
	assert.Zero(t, wedged.calls("/player/playback/playMedia"), "no playMedia after an unanswered command")

	err = p.Command(context.Background(), cmdPause, kindMovie)
	require.ErrorIs(t, err, errPlayerNoAnswer)
}

func TestCompanionCallerCancelIsNotNoAnswer(t *testing.T) {
	t.Parallel()
	fake := newFakeCompanion(t)
	fake.hang.Store(true)
	p := &companionPlayer{http: fake.Client(), name: "Desktop", baseURL: fake.URL, timeout: time.Minute}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err := p.Command(ctx, cmdPause, kindMovie)
	require.Error(t, err)
	assert.NotErrorIs(t, err, errPlayerNoAnswer)
}

func TestFindPlayer(t *testing.T) {
	// Not parallel: uses the global auth config.
	srv := newFakeServer(t)
	tv := newFakeTV(t)
	useTokens(t, srv.URL, tv.URL, "account-token")
	lan := newFakeCompanion(t)
	remote := newFakeCompanion(t)
	srv.clients = []ServerClient{
		lan.serverClient("Shield", "shield-1"),
		{Name: "Web", Address: "10.0.0.9", Port: 32500, MachineIdentifier: "web-1", ProtocolCapabilities: "timeline"},
	}
	tv.resources = []Resource{
		{
			Name: "Shield", ClientIdentifier: "shield-1", Provides: "player,pubsub-player",
			Connections: []Connection{{URI: lan.URL, Local: true}},
		},
		{
			Name: "Bedroom TV", ClientIdentifier: "tv-2", Provides: "client,player", AccessToken: "device-token",
			Connections: []Connection{
				{URI: "https://relay.invalid", Relay: true},
				{URI: remote.URL, Local: false},
			},
		},
		{
			Name: "Server", ClientIdentifier: "server-3", Provides: "server",
			Connections: []Connection{{URI: "http://10.0.0.1:32400", Local: true}},
		},
	}
	devices := []castDevice{{Name: "Kitchen speaker", ID: "cast-1", Host: "10.0.0.20", Port: 8009}}
	finder := testFinder(srv, devices, nil)
	newClient := func(name string) *Client {
		return NewClient(srv.URL, tv.URL, testClientID, name, srv.Client())
	}

	p, err := finder.find(context.Background(), newClient("bedroom tv"))
	require.NoError(t, err)
	cp, ok := p.(*companionPlayer)
	require.True(t, ok)
	assert.Equal(t, remote.URL, cp.baseURL, "the relay connection is skipped")
	assert.Equal(t, "device-token", cp.token)
	require.NotEmpty(t, tv.tokens)
	assert.Equal(t, "account-token", tv.tokens[0])

	p, err = finder.find(context.Background(), newClient("Kitchen Speaker"))
	require.NoError(t, err)
	assert.Equal(t, playerKindCast, p.Kind())

	_, err = finder.find(context.Background(), newClient("Garage"))
	require.Error(t, err)
	assert.Equal(t,
		`plex player "Garage" not found; players found: "Shield" (Companion), "Bedroom TV" (Companion), `+
			`"Kitchen speaker" (Cast)`,
		err.Error())

	_, err = finder.find(context.Background(), newClient(""))
	require.ErrorContains(t, err, "several plex players found")

	srv.clients = nil
	tv.resources = nil
	finder.browseCast = func(context.Context) ([]castDevice, error) { return nil, errors.New("no network") }
	_, err = finder.find(context.Background(), newClient(""))
	require.ErrorContains(t, err, "no plex players found")
}

// TestFindPlayerPrefersServerPlayers guards that a player the server knows
// is found without the optional plex.tv lookup, which can be slow enough to
// use up a control's whole deadline.
func TestFindPlayerPrefersServerPlayers(t *testing.T) {
	// Not parallel: uses the global auth config.
	srv := newFakeServer(t)
	tv := newFakeTV(t)
	useTokens(t, srv.URL, tv.URL, "account-token")
	lan := newFakeCompanion(t)
	srv.clients = []ServerClient{lan.serverClient("Shield", "shield-1")}
	devices := []castDevice{{Name: "Shield", ID: "cast-1", Host: "10.0.0.20", Port: 8009}}
	finder := testFinder(srv, devices, nil)

	p, err := finder.find(context.Background(), NewClient(srv.URL, tv.URL, testClientID, "shield", srv.Client()))
	require.NoError(t, err)
	assert.Equal(t, playerKindCompanion, p.Kind(), "a player reachable both ways is driven over Companion")
	tv.mu.Lock()
	defer tv.mu.Unlock()
	assert.Empty(t, tv.tokens, "plex.tv is not asked when the server knows the player")
}

func TestFindPlayerRejectsAmbiguousName(t *testing.T) {
	// Not parallel: uses the global auth config.
	srv := newFakeServer(t)
	useTokens(t, srv.URL, "", "")
	one := newFakeCompanion(t)
	two := newFakeCompanion(t)
	srv.clients = []ServerClient{one.serverClient("Living Room", "lr-1"), two.serverClient("living room", "lr-2")}
	devices := []castDevice{
		{Name: "Kitchen", ID: "cast-1", Host: "10.0.0.20", Port: 8009},
		{Name: "Kitchen", ID: "cast-2", Host: "10.0.0.21", Port: 8009},
	}
	finder := testFinder(srv, devices, nil)

	_, err := finder.find(context.Background(), srv.client("Living Room"))
	require.ErrorContains(t, err, `several plex players are named "Living Room"`)

	_, err = finder.find(context.Background(), srv.client("Kitchen"))
	require.ErrorContains(t, err, `several plex players are named "Kitchen"`)
}

func TestCompanionFromServerSkipsUnusable(t *testing.T) {
	t.Parallel()
	f := &playerFinder{http: http.DefaultClient}
	client := NewClient("http://127.0.0.1:32400", "", testClientID, "", nil)
	for _, c := range []ServerClient{
		{Name: "No host", Port: 1, MachineIdentifier: "a"},
		{Name: "No port", Address: "10.0.0.1", MachineIdentifier: "b"},
		{Name: "No id", Address: "10.0.0.1", Port: 1},
		{Name: "No playback", Address: "10.0.0.1", Port: 1, MachineIdentifier: "c", ProtocolCapabilities: "timeline"},
	} {
		assert.Nil(t, newCompanionFromServer(f, client, &c), c.Name)
	}
	p := newCompanionFromServer(f, client, &ServerClient{
		Name: "IPv6", Host: "fe80::1", Port: 32500, MachineIdentifier: "d",
	})
	require.NotNil(t, p)
	assert.Equal(t, "http://"+net.JoinHostPort("fe80::1", "32500"), p.baseURL)
}

func TestNewServerInfo(t *testing.T) {
	t.Parallel()
	id := &Identity{MachineIdentifier: "m", Version: "v"}
	info, err := newServerInfo(NewClient("https://plex.example.com", "", "c", "", nil), id)
	require.NoError(t, err)
	assert.Equal(t, 443, info.Port)
	assert.Equal(t, "https", info.Protocol)
	assert.Equal(t, "plex.example.com", info.Address)

	info, err = newServerInfo(NewClient("192.168.1.5:32400", "", "c", "", nil), id)
	require.NoError(t, err)
	assert.Equal(t, 32400, info.Port)
	assert.Equal(t, "http", info.Protocol)

	_, err = newServerInfo(NewClient("http://", "", "c", "", nil), id)
	require.Error(t, err)
}

func TestKeyFromPath(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "200", keyFromPath("/library/metadata/200"))
	assert.Equal(t, "200", keyFromPath("/library/metadata/200/"))
	assert.Empty(t, keyFromPath("/library/metadata/abc"))
	assert.Empty(t, keyFromPath(""))
}
