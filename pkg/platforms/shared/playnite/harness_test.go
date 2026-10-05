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

package playnite

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/jonboulle/clockwork"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

const (
	idPC      = "67feee56-a90d-4022-9be8-7bec24e4fac0"
	idNES     = "513cf1fd-4866-4fa0-911d-6217b8712ded"
	idHidden  = "0a2f7f0c-6a51-4a8e-8f65-0f2f5a3a8d01"
	idMissing = "0a2f7f0c-6a51-4a8e-8f65-0f2f5a3a8d02"
	idOdd     = "0a2f7f0c-6a51-4a8e-8f65-0f2f5a3a8d03"
	idSteam   = "0a2f7f0c-6a51-4a8e-8f65-0f2f5a3a8d04"
	idBare    = "0a2f7f0c-6a51-4a8e-8f65-0f2f5a3a8d05"

	waitFor = 5 * time.Second
	tick    = 5 * time.Millisecond
)

// fixtureGames is a small library covering every indexing rule.
func fixtureGames() []Game {
	pc := []Platform{{SpecificationID: "pc_windows", Name: "PC (Windows)"}}
	return []Game{
		{ID: idPC, Name: "PC Game", IsInstalled: true, Platforms: pc},
		{
			ID: idNES, Name: "NES Game", IsInstalled: true,
			Platforms: []Platform{{SpecificationID: "nintendo_nes", Name: "My NES"}},
		},
		{ID: idHidden, Name: "Hidden Game", IsInstalled: true, Hidden: true, Platforms: pc},
		{ID: idMissing, Name: "Uninstalled Game", Platforms: pc},
		{ID: idOdd, Name: "Odd Game", IsInstalled: true, Platforms: []Platform{{Name: "Homebrew Box"}}},
		{ID: idSteam, Name: "Steam Game", IsInstalled: true, Platforms: pc, LibraryPluginID: SteamLibraryPluginID},
		{ID: idBare, Name: "Bare Program", IsInstalled: true},
	}
}

// pipeListener hands out in-memory connections, standing in for the named
// pipe so the server runs unchanged on any platform.
type pipeListener struct {
	conns  chan net.Conn
	closed chan struct{}
	once   sync.Once
}

func newPipeListener() *pipeListener {
	return &pipeListener{conns: make(chan net.Conn), closed: make(chan struct{})}
}

func (l *pipeListener) Accept() (net.Conn, error) {
	select {
	case conn := <-l.conns:
		return conn, nil
	case <-l.closed:
		return nil, net.ErrClosed
	}
}

func (l *pipeListener) Close() error {
	l.once.Do(func() { close(l.closed) })
	return nil
}

func (*pipeListener) Addr() net.Addr {
	return &net.UnixAddr{Name: "playnite-test", Net: "pipe"}
}

// dial returns the extension's end of a new connection.
func (l *pipeListener) dial(t *testing.T) net.Conn {
	t.Helper()
	server, client := net.Pipe()
	select {
	case l.conns <- server:
	case <-l.closed:
		require.Fail(t, "listener closed before dial")
	case <-time.After(waitFor):
		require.Fail(t, "server did not accept the connection")
	}
	return client
}

// fakeExtension plays the Playnite extension: it answers commands the way
// the real one does unless a test overrides a reply.
type fakeExtension struct {
	conn     net.Conn
	onLaunch func(cmd Command) *Event
	onStop   func(cmd Command) *Event
	onGames  func(cmd Command) []Event
	done     chan struct{}
	games    []Game
	commands []Command
	mu       syncutil.Mutex
	writeMu  syncutil.Mutex
}

func newFakeExtension(conn net.Conn, games []Game) *fakeExtension {
	f := &fakeExtension{conn: conn, games: games, done: make(chan struct{})}
	go f.run()
	return f
}

func (f *fakeExtension) run() {
	defer close(f.done)
	scanner := bufio.NewScanner(f.conn)
	scanner.Buffer(make([]byte, 64*1024), MaxLineSize)
	for scanner.Scan() {
		var cmd Command
		if err := json.Unmarshal(scanner.Bytes(), &cmd); err != nil {
			continue
		}
		f.mu.Lock()
		f.commands = append(f.commands, cmd)
		onLaunch, onStop, onGames := f.onLaunch, f.onStop, f.onGames
		games := f.games
		f.mu.Unlock()

		switch cmd.Command {
		case CommandGetGames:
			if onGames != nil {
				events := onGames(cmd)
				for i := range events {
					f.write(&events[i])
				}
				continue
			}
			// Two chunks, as a real library larger than one chunk arrives.
			half := len(games) / 2
			f.write(&Event{Event: EventGames, RequestID: cmd.RequestID, Games: games[:half]})
			f.write(&Event{Event: EventGames, RequestID: cmd.RequestID, Games: games[half:], Final: true})
		case CommandLaunch:
			reply := &Event{Event: EventLaunchResult, ID: cmd.ID, Status: StatusCompleted}
			if onLaunch != nil {
				reply = onLaunch(cmd)
			}
			if reply != nil {
				f.write(reply)
			}
		case CommandStop:
			reply := &Event{Event: EventMediaStopResult, ID: cmd.ID, Status: StatusCompleted}
			if onStop != nil {
				reply = onStop(cmd)
			}
			if reply != nil {
				f.write(reply)
			}
		}
	}
}

// write sends one event; a closed connection is not an error for the fake.
func (f *fakeExtension) write(event *Event) {
	data, err := json.Marshal(event)
	if err != nil {
		return
	}
	f.writeRaw(append(data, '\n'))
}

func (f *fakeExtension) writeRaw(data []byte) {
	f.writeMu.Lock()
	defer f.writeMu.Unlock()
	_, _ = f.conn.Write(data)
}

func (f *fakeExtension) close() {
	_ = f.conn.Close()
	<-f.done
}

func (f *fakeExtension) count(command string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, cmd := range f.commands {
		if cmd.Command == command {
			n++
		}
	}
	return n
}

func (f *fakeExtension) set(configure func(f *fakeExtension)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	configure(f)
}

// started builds the event the extension sends when a game starts.
func started(game *Game, session, pid int) *Event {
	event := &Event{Event: EventMediaStarted, ID: game.ID, Game: game, Session: session, Pid: pid}
	if pid > 0 {
		event.Exe = `C:\Games\game.exe`
	}
	return event
}

func stopped(id string, session int) *Event {
	return &Event{Event: EventMediaStopped, ID: id, Session: session}
}

type trackCall struct {
	exe string
	pid int
}

// fakeFrontend records Playnite being started with a game.
type fakeFrontend struct {
	err     error
	started []string
	mu      syncutil.Mutex
}

func (f *fakeFrontend) StartGame(_ context.Context, _ *Install, gameID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.started = append(f.started, gameID)
	return nil
}

func (f *fakeFrontend) launches() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.started...)
}

// harness wires an integration to in-memory boundaries and records what it
// tells the platform.
type harness struct {
	t           *testing.T
	integration *Integration
	listener    *pipeListener
	frontend    *fakeFrontend
	fs          afero.Fs
	media       *models.ActiveMedia
	installDir  string
	tracked     []trackCall
	untracked   []int
	writes      []string
	mu          syncutil.Mutex
	steam       bool
}

type harnessOptions struct {
	clock    clockwork.Clock
	timeouts Timeouts
	steam    bool
}

func newHarness(t *testing.T, opts harnessOptions) *harness {
	t.Helper()
	h := &harness{
		t:          t,
		listener:   newPipeListener(),
		frontend:   &fakeFrontend{},
		fs:         afero.NewMemMapFs(),
		installDir: filepath.Join(t.TempDir(), "Playnite"),
		steam:      opts.steam,
	}
	h.integration = NewIntegration(&Deps{
		ActiveMedia: h.activeMedia,
		SetActiveMedia: func(media *models.ActiveMedia) {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.media = media
		},
		TrackProcess: func(pid int, exe string) {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.tracked = append(h.tracked, trackCall{pid: pid, exe: exe})
		},
		UntrackProcess: func(pid int) {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.untracked = append(h.untracked, pid)
		},
		WriteTag: func(_ *config.Instance, path string) {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.writes = append(h.writes, path)
		},
		SteamIndexed: func(*config.Instance) bool { return h.steam },
		Frontend:     h.frontend,
		Clock:        opts.clock,
		Locator:      Locator{FS: h.fs, Candidates: []string{h.installDir}},
		Timeouts:     opts.timeouts,
	})
	h.integration.Serve(nil, h.listener)
	t.Cleanup(h.integration.Stop)
	return h
}

func (h *harness) activeMedia() *models.ActiveMedia {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.media
}

func (h *harness) setMedia(media *models.ActiveMedia) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.media = media
}

func (h *harness) trackedCalls() []trackCall {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]trackCall(nil), h.tracked...)
}

func (h *harness) untrackedPIDs() []int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]int(nil), h.untracked...)
}

func (h *harness) tagWrites() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.writes...)
}

// install puts a Playnite install where the locator looks.
func (h *harness) install() {
	h.t.Helper()
	require.NoError(h.t, h.fs.MkdirAll(h.installDir, 0o755))
	require.NoError(h.t, afero.WriteFile(h.fs, filepath.Join(h.installDir, desktopExeName), nil, 0o600))
}

// connect attaches a fake extension and waits for the server to see it.
func (h *harness) connect(games []Game) *fakeExtension {
	h.t.Helper()
	ext := newFakeExtension(h.listener.dial(h.t), games)
	h.t.Cleanup(ext.close)
	ext.write(&Event{Event: EventHello, PluginVersion: "1.0.0", ProtocolVersion: 1, Mode: "Desktop"})
	require.Eventually(h.t, func() bool {
		return h.integration.Connected() && h.integration.server.Hello().ProtocolVersion == 1
	}, waitFor, tick)
	return ext
}

// requireMediaPath waits for ActiveMedia to name path; an empty path waits for it
// to be cleared.
func (h *harness) requireMediaPath(path string) {
	h.t.Helper()
	require.Eventually(h.t, func() bool {
		media := h.activeMedia()
		if path == "" {
			return media == nil
		}
		return media != nil && media.Path == path
	}, waitFor, tick)
}
