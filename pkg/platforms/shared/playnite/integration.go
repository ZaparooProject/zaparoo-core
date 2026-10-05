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
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/assets"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/jonboulle/clockwork"
	"github.com/rs/zerolog/log"
)

// Timeouts bound every wait the integration performs.
type Timeouts struct {
	// Library is how long the extension may take to send the whole library.
	Library time.Duration
	// LaunchAccept is how long the extension may take to say whether
	// Playnite accepted a launch. The game starting is reported separately.
	LaunchAccept time.Duration
	// Stop is how long the extension may take to end a game. It has to fit
	// inside the platform's stop budget with room for the fallback.
	Stop time.Duration
	// LibraryCache is how long a library snapshot serves later scans. An
	// index asks once per system, and the answer is the same every time.
	LibraryCache time.Duration
}

// DefaultTimeouts returns the production waits.
func DefaultTimeouts() Timeouts {
	return Timeouts{
		Library:      30 * time.Second,
		LaunchAccept: 10 * time.Second,
		Stop:         12 * time.Second,
		LibraryCache: 2 * time.Minute,
	}
}

// Deps are the platform hooks and external boundaries the integration uses.
// Every boundary is an interface or function so tests can script Playnite and
// the clock.
type Deps struct {
	ActiveMedia    func() *models.ActiveMedia
	SetActiveMedia func(*models.ActiveMedia)
	// TrackProcess hands the platform the process Playnite started for the
	// running game, so the platform can end its tree if the extension cannot.
	// exe is the process image the extension saw, for the platform to check
	// the PID still names that program.
	TrackProcess func(pid int, exe string)
	// UntrackProcess tells the platform the game's process has gone.
	UntrackProcess func(pid int)
	// WriteTag asks Core to write a game's path to a token.
	WriteTag func(cfg *config.Instance, path string)
	// SteamIndexed reports whether Core indexes Steam games itself.
	SteamIndexed func(cfg *config.Instance) bool
	Frontend     Frontend
	Clock        clockwork.Clock
	Locator      Locator
	Timeouts     Timeouts
}

// ErrNoActiveGame is returned by StopGame when Core is not tracking a
// running Playnite game.
var ErrNoActiveGame = errors.New("no active Playnite game")

// librarySnapshot is a cached answer to a library request without details.
type librarySnapshot struct {
	fetched time.Time
	games   []Game
	valid   bool
}

// Integration indexes and launches games through Playnite and follows the
// games Playnite reports running. It owns ActiveMedia for Playnite launches,
// as the launcher has an external lifecycle.
type Integration struct {
	cfg       *config.Instance
	server    *Server
	done      chan struct{}
	snapshot  librarySnapshot
	active    activeRun
	deps      Deps
	nextID    int
	stopOnce  sync.Once
	mu        syncutil.Mutex
	requestMu syncutil.Mutex
	libraryMu syncutil.Mutex
}

// NewIntegration wires the integration; a nil clock and zero timeouts get
// real ones.
func NewIntegration(deps *Deps) *Integration {
	if deps.Clock == nil {
		deps.Clock = clockwork.NewRealClock()
	}
	if deps.Timeouts == (Timeouts{}) {
		deps.Timeouts = DefaultTimeouts()
	}
	i := &Integration{deps: *deps, done: make(chan struct{})}
	i.server = NewServer(deps.Clock, Handlers{
		Started:      i.onStarted,
		Stopped:      i.onStopped,
		Write:        i.onWrite,
		Disconnected: i.onDisconnected,
	})
	return i
}

// Serve starts accepting the extension on listener, which the integration
// then owns. cfg is used for work the extension starts, such as tag writes.
func (i *Integration) Serve(cfg *config.Instance, listener net.Listener) {
	i.mu.Lock()
	i.cfg = cfg
	i.mu.Unlock()
	i.server.Serve(listener)
}

// Stop closes the connection and forgets the game it followed.
func (i *Integration) Stop() {
	i.stopOnce.Do(func() {
		close(i.done)
	})
	// Launch and StopGame hold requestMu for their whole exchange. Do not
	// hold it across Close: the reader may be delivering their answer.
	i.requestMu.Lock()
	i.requestMu.Unlock() //nolint:gocritic,staticcheck // Admission barrier; later requests see done.
	i.server.Close()

	i.mu.Lock()
	defer i.mu.Unlock()
	i.active = activeRun{}
	i.snapshot = librarySnapshot{}
}

func (i *Integration) stopping() bool {
	select {
	case <-i.done:
		return true
	default:
		return false
	}
}

// Connected reports whether the Playnite extension is connected.
func (i *Integration) Connected() bool {
	return i.server.Connected()
}

// Locate resolves the install from config, registry and default paths.
func (i *Integration) Locate(cfg *config.Instance) (Install, error) {
	configDir := ""
	if cfg != nil {
		configDir = strings.TrimSpace(cfg.LookupLauncherDefaults(LauncherID, nil).InstallDir)
	}
	inst, err := i.deps.Locator.Locate(configDir)
	if err != nil {
		return Install{}, err
	}
	return inst, nil
}

// Available reports whether Playnite can be reached: either its extension is
// connected, which also covers a portable copy Core cannot find on disk, or
// an install exists for Core to start.
func (i *Integration) Available(cfg *config.Instance) error {
	if i.server.Connected() {
		return nil
	}
	if _, err := i.Locate(cfg); err != nil {
		return err
	}
	return nil
}

func (i *Integration) skipSteam(cfg *config.Instance) bool {
	return i.deps.SteamIndexed != nil && i.deps.SteamIndexed(cfg)
}

// Scan returns the indexable games that belong to systemID. An absent
// install contributes nothing. An install whose extension is not connected
// cannot be read, which is reported as an unavailable source so the games
// already indexed are kept.
func (i *Integration) Scan(ctx context.Context, cfg *config.Instance, systemID string) ([]platforms.ScanResult, error) {
	if !i.server.Connected() {
		_, err := i.Locate(cfg)
		if errors.Is(err, ErrNotInstalled) {
			return nil, nil
		}
		if err != nil {
			return nil, fmt.Errorf("scan Playnite games: %w", err)
		}
		return nil, platforms.ErrScannerUnavailable
	}
	games, err := i.cachedLibrary(ctx)
	if errors.Is(err, ErrNotConnected) {
		return nil, platforms.ErrScannerUnavailable
	}
	if err != nil {
		return nil, fmt.Errorf("scan Playnite games: %w", err)
	}
	return ScanResults(games, systemID, i.skipSteam(cfg)), nil
}

// cachedLibrary returns the library without details, from the snapshot when it is
// recent enough. Callers must not modify the result.
func (i *Integration) cachedLibrary(ctx context.Context) ([]Game, error) {
	i.libraryMu.Lock()
	defer i.libraryMu.Unlock()

	now := i.deps.Clock.Now()
	i.mu.Lock()
	snapshot := i.snapshot
	i.mu.Unlock()
	if snapshot.valid && now.Sub(snapshot.fetched) < i.deps.Timeouts.LibraryCache {
		return snapshot.games, nil
	}

	games, err := i.server.Games(ctx, false, i.deps.Timeouts.Library)
	if err != nil {
		return nil, err
	}
	log.Debug().Int("games", len(games)).Msg("read Playnite library")
	i.mu.Lock()
	// A disconnect while the request was out has already dropped the
	// snapshot; what came back may be from the previous Playnite session.
	if i.server.Connected() {
		i.snapshot = librarySnapshot{games: games, fetched: now, valid: true}
	}
	i.mu.Unlock()
	return games, nil
}

// LibraryDetails reads the whole library with metadata and image paths.
func (i *Integration) LibraryDetails(ctx context.Context) ([]Game, error) {
	games, err := i.server.Games(ctx, true, i.deps.Timeouts.Library)
	if err != nil {
		return nil, fmt.Errorf("read Playnite library: %w", err)
	}
	return games, nil
}

// Launch asks Playnite to start a game. With the extension connected the
// request goes over the pipe; otherwise Playnite itself is run with the game
// to start. Either way ActiveMedia is published only when the extension
// reports the game running.
func (i *Integration) Launch(cfg *config.Instance, path string) error {
	i.requestMu.Lock()
	defer i.requestMu.Unlock()
	if i.stopping() {
		return context.Canceled
	}
	gameID, err := ParseGamePath(path)
	if err != nil {
		return err
	}

	ctx := context.Background()
	if i.server.Connected() {
		key := i.claim(gameID, path)
		if launchErr := i.server.Launch(ctx, gameID, i.deps.Timeouts.LaunchAccept); launchErr != nil {
			i.release(key)
			return fmt.Errorf("launch Playnite game: %w", launchErr)
		}
		log.Info().Str("gameID", gameID).Msg("asked Playnite to launch game")
		return nil
	}

	inst, err := i.Locate(cfg)
	if err != nil {
		return fmt.Errorf("launch Playnite game: %w", err)
	}
	if i.deps.Frontend == nil {
		return fmt.Errorf("launch Playnite game: %w", ErrNotConnected)
	}
	key := i.claim(gameID, path)
	if startErr := i.deps.Frontend.StartGame(ctx, &inst, gameID); startErr != nil {
		i.release(key)
		return fmt.Errorf("launch Playnite game: %w", startErr)
	}
	log.Warn().Str("gameID", gameID).
		Msg("Playnite extension not connected; started Playnite with the game, tracked once the extension connects")
	return nil
}

// StopGame asks the extension to end the running game and waits for it to
// confirm. It never clears ActiveMedia itself: the platform does that once
// the stop is confirmed.
func (i *Integration) StopGame() error {
	i.requestMu.Lock()
	defer i.requestMu.Unlock()

	i.mu.Lock()
	run := i.active
	i.mu.Unlock()
	if !run.adopted {
		return ErrNoActiveGame
	}

	if err := i.server.StopGame(context.Background(), run.key.gameID, i.deps.Timeouts.Stop); err != nil {
		return fmt.Errorf("stop Playnite game: %w", err)
	}
	log.Info().Str("gameID", run.key.gameID).Msg("Playnite game stopped")
	// Retire the run so the extension's own stop event for it is stale.
	i.release(run.key)
	return nil
}

// GameRunning reports whether the extension has reported a game running and
// not yet stopped. It lets the platform refuse to report a stop it cannot
// confirm.
func (i *Integration) GameRunning() bool {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.active.adopted
}

// claim records a new run as the one that owns ActiveMedia from now on.
func (i *Integration) claim(gameID, path string) launchKey {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.nextID++
	i.active = activeRun{key: launchKey{gameID: gameID, lifecycleID: i.nextID}, path: path}
	return i.active.key
}

// release drops the run if it is still the active one.
func (i *Integration) release(key launchKey) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.active.key == key {
		i.active = activeRun{}
	}
}

// onStarted adopts a game the extension reports running, whether Core asked
// for it or the user started it in Playnite. Recording and publishing happen
// under one hold of the lock so a concurrent stop or relaunch cannot slip
// between them.
func (i *Integration) onStarted(event *Event) {
	if event.Game == nil {
		log.Debug().Msg("ignoring Playnite start without a game")
		return
	}
	game := event.Game
	gameID, err := NormalizeGameID(game.ID)
	if err != nil {
		log.Debug().Err(err).Msg("ignoring Playnite start with an invalid game ID")
		return
	}

	i.mu.Lock()
	cfg := i.cfg
	i.mu.Unlock()
	systemID, ok := trackedSystem(game, i.skipSteam(cfg))
	if !ok {
		log.Debug().Str("gameID", gameID).Str("game", game.Name).
			Msg("Playnite started a game Core does not index, not tracking it")
		return
	}
	name := strings.TrimSpace(game.Name)
	path := GamePath(gameID, name)

	i.mu.Lock()
	if i.stopping() {
		i.mu.Unlock()
		return
	}
	if i.active.key.gameID != gameID {
		i.nextID++
		i.active = activeRun{key: launchKey{gameID: gameID, lifecycleID: i.nextID}}
	}
	previousPID := 0
	if i.active.adopted && i.active.pid != event.Pid {
		previousPID = i.active.pid
	}
	i.active.adopted = true
	i.active.session = event.Session
	i.active.pid = event.Pid
	i.active.path = path

	systemName := systemID
	if meta, metaErr := assets.GetSystemMetadata(systemID); metaErr == nil {
		systemName = meta.Name
	}
	log.Info().Str("gameID", gameID).Int("pid", event.Pid).Str("game", name).Msg("Playnite game started")
	if i.deps.SetActiveMedia != nil {
		i.deps.SetActiveMedia(models.NewActiveMedia(systemID, systemName, path, name, LauncherID))
	}
	i.mu.Unlock()

	if previousPID > 0 && i.deps.UntrackProcess != nil {
		i.deps.UntrackProcess(previousPID)
	}
	if event.Pid > 0 && i.deps.TrackProcess != nil {
		i.deps.TrackProcess(event.Pid, event.Exe)
	}
}

// onStopped clears ActiveMedia for the run the extension says ended, but
// only when that run is still the active one: a late stop for an earlier run
// must not discard the media of the run that replaced it.
func (i *Integration) onStopped(event *Event) {
	gameID, err := NormalizeGameID(event.ID)
	if err != nil {
		return
	}

	i.mu.Lock()
	run := i.active
	if !run.adopted || run.key.gameID != gameID || run.session != event.Session {
		i.mu.Unlock()
		log.Debug().Str("gameID", gameID).Msg("ignoring stale Playnite game exit")
		return
	}
	i.active = activeRun{}
	log.Info().Str("gameID", gameID).Msg("Playnite game exited")
	i.clearMediaLocked(run.path)
	i.mu.Unlock()

	if run.pid > 0 && i.deps.UntrackProcess != nil {
		i.deps.UntrackProcess(run.pid)
	}
}

// clearMediaLocked clears ActiveMedia if it still names path. The caller
// holds mu.
func (i *Integration) clearMediaLocked(path string) {
	if i.deps.ActiveMedia == nil || i.deps.SetActiveMedia == nil {
		return
	}
	current := i.deps.ActiveMedia()
	if current == nil || current.Path != path {
		return
	}
	i.deps.SetActiveMedia(nil)
}

// onDisconnected handles Playnite closing or the pipe breaking. Nothing can
// report the game's exit any more, so Core stops claiming it is running; the
// extension announces running games again when it reconnects.
func (i *Integration) onDisconnected() {
	i.mu.Lock()
	run := i.active
	i.snapshot = librarySnapshot{}
	if !run.adopted || i.stopping() {
		i.mu.Unlock()
		return
	}
	i.active = activeRun{}
	log.Info().Str("gameID", run.key.gameID).Msg("Playnite disconnected with a game running, no longer tracking it")
	i.clearMediaLocked(run.path)
	i.mu.Unlock()

	if run.pid > 0 && i.deps.UntrackProcess != nil {
		i.deps.UntrackProcess(run.pid)
	}
}

// onWrite handles the extension's request to write a game to a token.
func (i *Integration) onWrite(id, name string) {
	gameID, err := NormalizeGameID(id)
	if err != nil {
		log.Warn().Err(err).Msg("ignoring Playnite write request")
		return
	}
	name = strings.TrimSpace(name)
	if name == "" || i.deps.WriteTag == nil {
		return
	}
	i.mu.Lock()
	cfg := i.cfg
	i.mu.Unlock()
	i.deps.WriteTag(cfg, GamePath(gameID, name))
}
