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

package kodi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/virtualpath"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/gorilla/websocket"
	"github.com/jonboulle/clockwork"
	"github.com/rs/zerolog/log"
)

const (
	repeatOff = "off"

	// kodiWebSocketPort is Kodi's default JSON-RPC notification port. The
	// launcher defaults carry no override, so a Kodi on another port simply
	// never reports completion.
	kodiWebSocketPort = "9090"

	// dialTimeout caps the whole connect and handshake. The dial runs while
	// Core holds its launch gates, so a firewalled port must not stall launches.
	dialTimeout = 1500 * time.Millisecond
	// firstPlayTimeout bounds how long a launch may take to start playing
	// before the watcher gives up. Network sources can be slow to open.
	firstPlayTimeout = 60 * time.Second
	// queueGrace is how long a Kodi-side queue must stay silent after a
	// natural end before the whole queue counts as finished.
	queueGrace      = 3 * time.Second
	maxWatchMessage = 1 << 20
)

// watchSettings holds the tunables of a watcher so tests can substitute them.
type watchSettings struct {
	clock clockwork.Clock
	// settled, when set, runs each time the watcher has handled everything
	// delivered so far and is about to wait again. Tests use it to
	// synchronise with the watcher goroutine.
	settled func()
	// repeatOff, when set, runs after a repeat-off request succeeded.
	repeatOff        func()
	port             string
	firstPlayTimeout time.Duration
	grace            time.Duration
	dialTimeout      time.Duration
}

func defaultWatchSettings() *watchSettings {
	return &watchSettings{
		port:             kodiWebSocketPort,
		clock:            clockwork.NewRealClock(),
		firstPlayTimeout: firstPlayTimeout,
		grace:            queueGrace,
		dialTimeout:      dialTimeout,
	}
}

// expectation describes what a launch will start playing so the watcher can
// tell this launch's first Player.OnPlay from a stale one.
type expectation struct {
	// itemType and itemID identify a single library item. Zero values mean the
	// item cannot be identified in advance.
	itemType string
	itemID   int
	// queue marks launches that make Kodi play several items in turn (a TV
	// show, an album or an artist). The launch is finished only when the whole
	// queue is.
	queue bool
}

// libraryExpectation builds the expectation for a single library item path.
func libraryExpectation(path, scheme, itemType string) expectation {
	idStr, err := virtualpath.ExtractSchemeID(path, scheme)
	if err != nil {
		return expectation{}
	}
	id, err := strconv.Atoi(idStr)
	if err != nil {
		return expectation{}
	}
	return expectation{itemType: itemType, itemID: id}
}

// notification is the part of a Kodi JSON-RPC notification the watcher needs.
type notification struct {
	Method string       `json:"method"`
	Params notifyParams `json:"params"`
}

type notifyParams struct {
	Data notifyData `json:"data"`
}

type notifyData struct {
	Player   notifyPlayer   `json:"player"`
	Property notifyProperty `json:"property"`
	Item     notifyItem     `json:"item"`
	End      bool           `json:"end"`
}

// notifyPlayer carries the player ID; a pointer because audio is player 0.
type notifyPlayer struct {
	PlayerID *int `json:"playerid"`
}

type notifyProperty struct {
	Repeat string `json:"repeat"`
}

// loopGuard forces Kodi's repeat mode off for a playlist item. Kodi ignores
// the repeat option on Player.Open for files while its repeat mode is "one",
// so the mode is also set on the running player once it exists. off is true
// while repeat is known to be off.
type loopGuard struct {
	setRepeat func(ctx context.Context, playerID *int) error
	onOff     func()
	wg        sync.WaitGroup
	off       atomic.Bool
}

// forceOff sets repeat off without blocking the watcher. Failure is not
// fatal: the item may then loop, which the grace window still tolerates.
func (g *loopGuard) forceOff(ctx context.Context, playerID *int) {
	g.wg.Add(1)
	go func() {
		defer g.wg.Done()
		if err := g.setRepeat(ctx, playerID); err != nil {
			if ctx.Err() == nil {
				log.Warn().Err(err).Msg("kodi: could not turn repeat off for playlist item")
			}
			return
		}
		g.off.Store(true)
		if g.onOff != nil {
			g.onOff()
		}
	}()
}

// setRepeatOff builds the guard's request: it uses the notified player ID, or
// asks Kodi for the active player when the notification lacked one.
func setRepeatOff(c *Client) func(context.Context, *int) error {
	return func(ctx context.Context, playerID *int) error {
		id := 0
		if playerID != nil {
			id = *playerID
		} else {
			players, err := c.GetActivePlayers(ctx)
			if err != nil {
				return err
			}
			if len(players) == 0 {
				return errors.New("no active players")
			}
			id = players[0].ID
		}
		_, err := c.APIRequest(ctx, APIMethodPlayerSetRepeat, PlayerSetRepeatParams{
			PlayerID: id,
			Repeat:   repeatOff,
		})
		return err
	}
}

type notifyItem struct {
	Type  string `json:"type"`
	Title string `json:"title"`
	ID    int    `json:"id"`
}

// sameItem reports whether two notifications can refer to one item. Kodi
// fills in fewer fields on some events (an unlibrary file may gain an ID or
// lose its title), so missing data never counts as a difference.
func sameItem(a, b notifyItem) bool {
	if a.ID > 0 && b.ID > 0 {
		return a.ID == b.ID && a.Type == b.Type
	}
	if a.Title != "" && b.Title != "" {
		return a.Title == b.Title
	}
	return true
}

type activeWatch struct {
	cancel context.CancelFunc
}

var watchers = struct {
	byURL map[string]*activeWatch
	mu    syncutil.Mutex
}{byURL: make(map[string]*activeWatch)}

func cancelWatcher(serverURL string) {
	watchers.mu.Lock()
	w := watchers.byURL[serverURL]
	delete(watchers.byURL, serverURL)
	watchers.mu.Unlock()
	if w != nil {
		w.cancel()
	}
}

// Tracking reports whether a watcher is currently following a launch on the
// Kodi at serverURL (the URL a client's GetURL returns). A tracked launch
// reports its own end, so a poller that sees no players must leave the media
// alone until the watcher is gone.
func Tracking(serverURL string) bool {
	watchers.mu.Lock()
	defer watchers.mu.Unlock()
	return watchers.byURL[serverURL] != nil
}

func registerWatcher(serverURL string, w *activeWatch) {
	watchers.mu.Lock()
	watchers.byURL[serverURL] = w
	watchers.mu.Unlock()
}

func unregisterWatcher(serverURL string, w *activeWatch) {
	watchers.mu.Lock()
	if watchers.byURL[serverURL] == w {
		delete(watchers.byURL, serverURL)
	}
	watchers.mu.Unlock()
}

// outcome is how a watched launch ended.
type outcome int

const (
	// outcomeNone: the watcher gave up or was cancelled; nothing is reported.
	outcomeNone outcome = iota
	outcomeFinished
	outcomeExited
)

// launchTracked runs launch against client. When opts carries a MediaFinished
// or MediaExited callback it first connects to Kodi's notification socket and
// watches for the media ending naturally or being stopped from Kodi. Repeat is
// forced off only for playlist items, so an ordinary launch keeps the user's
// Kodi repeat mode and its request body. Without a callback the launch is
// unchanged. Any earlier watcher for the same
// Kodi is cancelled first so two never run for one server.
func launchTracked(
	client KodiClient,
	opts *platforms.LaunchOptions,
	exp expectation,
	launch func(KodiClient) error,
) error {
	c, ok := client.(*Client)
	if !ok {
		return launch(client)
	}
	cancelWatcher(c.url)
	if opts == nil || (opts.MediaFinished == nil && opts.MediaExited == nil) {
		return launch(client)
	}

	settings := c.watch
	if settings == nil {
		settings = defaultWatchSettings()
	}

	ctx, cancel := context.WithCancel(context.Background())
	conn, err := dialWatch(ctx, c.url, settings.port, settings.dialTimeout)
	if err != nil {
		cancel()
		log.Warn().Err(err).Str("url", c.url).
			Msg("kodi: could not connect to notifications, completion will not be reported")
		return launch(client)
	}

	var once sync.Once
	report := func(fn func()) {
		if fn != nil {
			once.Do(fn)
		}
	}
	finished := func() { report(opts.MediaFinished) }
	exited := func() { report(opts.MediaExited) }

	var guard *loopGuard
	if opts.PlaylistItem {
		guard = &loopGuard{setRepeat: setRepeatOff(c), onOff: settings.repeatOff}
	}

	w := &activeWatch{cancel: cancel}
	registerWatcher(c.url, w)
	go func() {
		defer unregisterWatcher(c.url, w)
		defer cancel()
		runWatch(ctx, conn, settings, exp, guard, finished, exited)
	}()

	tracked := *c
	tracked.noRepeat = opts.PlaylistItem
	if err := launch(&tracked); err != nil {
		cancelWatcher(c.url)
		return err
	}
	return nil
}

// watchURL derives Kodi's WebSocket endpoint from its JSON-RPC URL.
func watchURL(serverURL, port string) (string, error) {
	u, err := url.Parse(serverURL)
	if err != nil {
		return "", fmt.Errorf("parse kodi url: %w", err)
	}
	scheme := "ws"
	if u.Scheme == "https" {
		scheme = "wss"
	}
	return (&url.URL{
		Scheme: scheme,
		Host:   net.JoinHostPort(u.Hostname(), port),
		Path:   "/jsonrpc",
	}).String(), nil
}

func dialWatch(ctx context.Context, serverURL, port string, timeout time.Duration) (*websocket.Conn, error) {
	wsURL, err := watchURL(serverURL, port)
	if err != nil {
		return nil, err
	}
	header := http.Header{}
	if auth := authorizationHeader(serverURL); auth != "" {
		header.Set("Authorization", auth)
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	dialer := websocket.Dialer{HandshakeTimeout: timeout}
	//nolint:bodyclose // gorilla closes the handshake body on error and hands the conn on success
	conn, _, err := dialer.DialContext(ctx, wsURL, header)
	if err != nil {
		return nil, fmt.Errorf("dial kodi notifications: %w", err)
	}
	conn.SetReadLimit(maxWatchMessage)
	return conn, nil
}

// runWatch consumes notifications until the launch's media finishes, is
// stopped or replaced, Kodi goes away, or ctx is cancelled. It owns conn.
func runWatch(
	ctx context.Context,
	conn *websocket.Conn,
	s *watchSettings,
	exp expectation,
	guard *loopGuard,
	finished, exited func(),
) {
	ctx, cancel := context.WithCancel(ctx)
	msgs := make(chan notification)
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		defer close(msgs)
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var n notification
			if json.Unmarshal(data, &n) != nil || n.Method == "" {
				continue
			}
			select {
			case msgs <- n:
			case <-ctx.Done():
				return
			}
		}
	}()
	defer func() {
		cancel()
		_ = conn.Close()
		<-readerDone
		if guard != nil {
			guard.wg.Wait()
		}
	}()

	res := watchLoop(ctx, msgs, s, exp, guard)
	if ctx.Err() != nil {
		return
	}
	switch res {
	case outcomeFinished:
		finished()
	case outcomeExited:
		exited()
	case outcomeNone:
	}
}

// watchLoop reports how the launched media ended. Exited means it stopped
// without a natural end after playback started; giving up, a timeout or a
// different item taking over report nothing.
func watchLoop(
	ctx context.Context,
	msgs <-chan notification,
	s *watchSettings,
	exp expectation,
	guard *loopGuard,
) outcome {
	startTimer := s.clock.NewTimer(s.firstPlayTimeout)
	defer startTimer.Stop()

	var (
		armed     bool
		armedItem notifyItem
		// graceC is non-nil while a queue's natural end waits out its grace.
		graceC     <-chan time.Time
		graceTimer clockwork.Timer
	)
	defer func() {
		if graceTimer != nil {
			graceTimer.Stop()
		}
	}()

	for {
		if s.settled != nil {
			s.settled()
		}
		select {
		case <-ctx.Done():
			return outcomeNone
		case <-startTimer.Chan():
			if !armed {
				log.Debug().Msg("kodi: playback did not start in time, no longer watching")
				return outcomeNone
			}
		case <-graceC:
			return outcomeFinished
		case n, ok := <-msgs:
			if !ok {
				if armed {
					return outcomeExited
				}
				return outcomeNone
			}
			item := n.Params.Data.Item
			switch n.Method {
			case "Player.OnPlay", "Player.OnAVStart":
				switch {
				case !armed:
					if exp.itemID > 0 && (item.ID != exp.itemID || item.Type != exp.itemType) {
						continue
					}
					armed = true
					startTimer.Stop()
					armedItem = item
					if guard != nil {
						guard.forceOff(ctx, n.Params.Data.Player.PlayerID)
					}
				case !exp.queue && !sameItem(armedItem, item):
					return outcomeNone
				default:
					// A queue's next item, or the same item starting again
					// (Kodi loops through OnStop end=true then OnPlay).
					if graceTimer != nil {
						graceTimer.Stop()
						graceTimer, graceC = nil, nil
					}
				}
			case "Player.OnPropertyChanged":
				if guard != nil && n.Params.Data.Property.Repeat != "" && n.Params.Data.Property.Repeat != repeatOff {
					guard.off.Store(false)
				}
			case "Player.OnStop":
				if !armed {
					continue
				}
				if !n.Params.Data.End {
					return outcomeExited
				}
				if !exp.queue && guard != nil && guard.off.Load() {
					return outcomeFinished
				}
				if graceTimer == nil {
					graceTimer = s.clock.NewTimer(s.grace)
					graceC = graceTimer.Chan()
				}
			}
		}
	}
}
