//go:build linux

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
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/linuxbase/procscanner"
	"github.com/rs/zerolog/log"
)

// Tracker monitors Steam game lifecycle events on Linux.
type Tracker struct {
	onGameStart GameStartCallback
	onGameStop  GameStopCallback
	scanner     *procscanner.Scanner
	tracked     map[int]*TrackedGame
	appIDToPID  map[int]int
	watchID     procscanner.WatchID
	mu          syncutil.Mutex
}

// New creates a new game tracker.
// scanner must be a running process scanner.
// onStart is called when a game starts, onStop is called when a game exits.
func New(scanner *procscanner.Scanner, onStart GameStartCallback, onStop GameStopCallback) *Tracker {
	return &Tracker{
		scanner:     scanner,
		onGameStart: onStart,
		onGameStop:  onStop,
		tracked:     make(map[int]*TrackedGame),
		appIDToPID:  make(map[int]int),
	}
}

// steamReaperMatcher matches Steam reaper processes.
type steamReaperMatcher struct{}

func (*steamReaperMatcher) Match(proc procscanner.ProcessInfo) bool {
	// Must be named "reaper"
	if proc.Comm != "reaper" {
		return false
	}

	// Must contain SteamLaunch in cmdline
	cmdline := strings.ReplaceAll(proc.Cmdline, "\x00", " ")
	if !strings.Contains(cmdline, "SteamLaunch") {
		return false
	}

	// Must have AppId
	_, ok := parseAppIDFromCmdline(proc.Cmdline)
	return ok
}

// Start begins monitoring for Steam games.
func (t *Tracker) Start() {
	t.watchID = t.scanner.Watch(
		&steamReaperMatcher{},
		procscanner.Callbacks{
			OnStart: t.handleProcessStart,
			OnStop:  t.handleProcessStop,
		},
	)
	log.Info().Msg("steam game tracker started")
}

// Stop stops the game tracker.
func (t *Tracker) Stop() {
	t.scanner.Unwatch(t.watchID)
	log.Info().Msg("steam game tracker stopped")
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

// handleProcessStart is called when a reaper process is detected.
func (t *Tracker) handleProcessStart(proc procscanner.ProcessInfo) {
	// Extract AppID and game path
	appID, ok := parseAppIDFromCmdline(proc.Cmdline)
	if !ok {
		return
	}

	gamePath := parseGamePathFromCmdline(proc.Cmdline)

	t.mu.Lock()
	defer t.mu.Unlock()

	// Skip if already tracking this PID
	if _, exists := t.tracked[proc.PID]; exists {
		return
	}

	// Skip if already tracking this AppID (dedup)
	if _, exists := t.appIDToPID[appID]; exists {
		return
	}

	// Track the game
	game := &TrackedGame{
		AppID:     appID,
		PID:       proc.PID,
		GamePath:  gamePath,
		StartTime: time.Now(),
	}

	t.tracked[proc.PID] = game
	t.appIDToPID[appID] = proc.PID

	log.Info().
		Int("appID", appID).
		Int("pid", proc.PID).
		Str("gamePath", gamePath).
		Msg("detected Steam game start")

	// Call callback
	if t.onGameStart != nil {
		go t.onGameStart(appID, proc.PID, gamePath)
	}
}

// handleProcessStop is called when a tracked reaper process exits.
func (t *Tracker) handleProcessStop(pid int) {
	t.mu.Lock()
	defer t.mu.Unlock()

	game, exists := t.tracked[pid]
	if !exists {
		return
	}

	appID := game.AppID

	// Clean up tracking state
	delete(t.tracked, pid)
	delete(t.appIDToPID, appID)

	log.Info().
		Int("appID", appID).
		Int("pid", pid).
		Msg("detected Steam game exit")

	// Call callback
	if t.onGameStop != nil {
		go t.onGameStop(appID)
	}
}
