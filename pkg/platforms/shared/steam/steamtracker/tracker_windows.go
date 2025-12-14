//go:build windows

/*
Zaparoo Core
Copyright (C) 2024, 2025 Callan Barrett

This file is part of Zaparoo Core.

Zaparoo Core is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

Zaparoo Core is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with Zaparoo Core.  If not, see <http://www.gnu.org/licenses/>.
*/

package steamtracker

import (
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/rs/zerolog/log"
)

// Tracker monitors Steam game lifecycle events on Windows via registry notifications.
type Tracker struct {
	onGameStart GameStartCallback
	onGameStop  GameStopCallback
	watcher     *RegistryWatcher
	tracked     map[int]*TrackedGame
	mu          syncutil.Mutex
}

// Option configures a Tracker.
type Option func(*Tracker)

// New creates a new game tracker for Windows.
func New(onStart GameStartCallback, onStop GameStopCallback, _ ...Option) *Tracker {
	return &Tracker{
		onGameStart: onStart,
		onGameStop:  onStop,
		tracked:     make(map[int]*TrackedGame),
	}
}

// Start begins monitoring for Steam games via registry notifications.
func (t *Tracker) Start() error {
	if !IsSteamInstalled() {
		log.Info().Msg("steam not installed, game tracker disabled")
		return nil
	}

	t.watcher = NewRegistryWatcher(t.onAppIDChange)
	if err := t.watcher.Start(); err != nil {
		log.Warn().Err(err).Msg("failed to start registry watcher")
		return err
	}

	log.Info().Msg("windows steam game tracker started (event-driven)")
	return nil
}

// Stop stops the game tracker.
func (t *Tracker) Stop() {
	if t.watcher != nil {
		t.watcher.Stop()
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

// onAppIDChange is called when the registry RunningAppID changes.
func (t *Tracker) onAppIDChange(appID int) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if appID == 0 {
		// No game running - notify for all tracked games
		for id := range t.tracked {
			log.Info().Int("appID", id).Msg("detected Steam game exit")
			if t.onGameStop != nil {
				go t.onGameStop(id)
			}
			delete(t.tracked, id)
		}
		return
	}

	// Game is running - check if it's new
	if _, exists := t.tracked[appID]; exists {
		return
	}

	// New game detected
	game := &TrackedGame{
		AppID:     appID,
		StartTime: time.Now(),
	}
	t.tracked[appID] = game

	log.Info().Int("appID", appID).Msg("detected Steam game start")

	if t.onGameStart != nil {
		go t.onGameStart(appID, 0, "")
	}
}
