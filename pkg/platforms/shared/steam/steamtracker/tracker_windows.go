//go:build windows

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

package steamtracker

import (
	"sync"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/rs/zerolog/log"
)

// steamInstallPollInterval is how often the tracker re-checks for Steam when
// it was not installed at startup. Shortened by tests.
var steamInstallPollInterval = 30 * time.Second

// Tracker monitors Steam game lifecycle events on Windows via registry
// notifications.
//
// Steam publishes a single RunningAppID, so at most one game is tracked at a
// time: seeing a different AppID means the previous one is no longer the
// running game and must be reported as stopped.
type Tracker struct {
	onGameStart     GameStartCallback
	onGameStop      GameStopCallback
	watcher         *RegistryWatcher
	tracked         map[int]*TrackedGame
	done            chan struct{}
	mu              syncutil.Mutex
	wg              sync.WaitGroup
	stopOnce        sync.Once
	nextLifecycleID int
}

// Option configures a Tracker.
type Option func(*Tracker)

// New creates a new game tracker for Windows.
func New(onStart GameStartCallback, onStop GameStopCallback, opts ...Option) *Tracker {
	t := &Tracker{
		onGameStart: onStart,
		onGameStop:  onStop,
		tracked:     make(map[int]*TrackedGame),
		done:        make(chan struct{}),
	}
	for _, opt := range opts {
		opt(t)
	}
	return t
}

// Start begins monitoring for Steam games. When Steam is not installed yet the
// tracker waits for it rather than disabling itself for the lifetime of the
// process, so installing Steam after Core starts still produces game events.
func (t *Tracker) Start() error {
	if IsSteamInstalled() {
		return t.startWatcher()
	}

	log.Info().Msg("steam not installed, waiting for it before tracking games")
	t.wg.Add(1)
	go t.waitForSteam()
	return nil
}

// waitForSteam polls for the Steam registry key and starts the watcher once it
// appears.
func (t *Tracker) waitForSteam() {
	defer t.wg.Done()

	ticker := time.NewTicker(steamInstallPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-t.done:
			return
		case <-ticker.C:
			if !IsSteamInstalled() {
				continue
			}
			if err := t.startWatcher(); err != nil {
				log.Warn().Err(err).Msg("failed to start steam game tracker after install")
				return
			}
			return
		}
	}
}

func (t *Tracker) startWatcher() error {
	watcher := NewRegistryWatcher(t.onAppIDChange)
	if err := watcher.Start(); err != nil {
		log.Warn().Err(err).Msg("failed to start registry watcher")
		return err
	}

	t.mu.Lock()
	t.watcher = watcher
	t.mu.Unlock()

	log.Info().Msg("windows steam game tracker started (event-driven)")
	return nil
}

// Stop stops the game tracker.
func (t *Tracker) Stop() {
	t.stopOnce.Do(func() {
		close(t.done)
	})
	t.wg.Wait()

	t.mu.Lock()
	watcher := t.watcher
	t.watcher = nil
	t.mu.Unlock()

	if watcher != nil {
		watcher.Stop()
	}
	log.Info().Msg("windows steam game tracker stopped")
}

// TrackedGames returns a copy of currently tracked games.
func (t *Tracker) TrackedGames() []TrackedGame {
	t.mu.Lock()
	defer t.mu.Unlock()

	games := make([]TrackedGame, 0, len(t.tracked))
	for _, game := range t.tracked {
		games = append(games, *game)
	}
	return games
}

// onAppIDChange is called when the registry RunningAppID changes. Any game
// that is no longer the running AppID is reported as stopped, including when
// Steam moves straight from one game to another without passing through zero.
func (t *Tracker) onAppIDChange(appID int) {
	t.mu.Lock()
	defer t.mu.Unlock()

	for id, game := range t.tracked {
		if id == appID {
			continue
		}
		log.Info().Int("appID", id).Msg("detected Steam game exit")
		if t.onGameStop != nil {
			go t.onGameStop(id, game.PID)
		}
		delete(t.tracked, id)
	}

	if appID == 0 {
		return
	}

	if _, exists := t.tracked[appID]; exists {
		return
	}

	// RunningAppID does not expose a process ID. Use a monotonic lifecycle
	// identifier so delayed stop callbacks cannot clear a same-AppID relaunch.
	t.nextLifecycleID++
	game := &TrackedGame{
		AppID:     appID,
		PID:       t.nextLifecycleID,
		StartTime: time.Now(),
	}
	t.tracked[appID] = game

	log.Info().Int("appID", appID).Msg("detected Steam game start")

	if t.onGameStart != nil {
		go t.onGameStart(appID, game.PID, "")
	}
}
