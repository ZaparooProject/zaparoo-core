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
	"sync"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/linuxbase/proctracker"
	"github.com/rs/zerolog/log"
)

// DefaultPollInterval is the default interval for scanning reaper processes.
const DefaultPollInterval = 2 * time.Second

// GameStartCallback is called when a Steam game starts.
// appID is the Steam App ID, pid is the reaper process ID, gamePath is the game executable.
type GameStartCallback func(appID int, pid int, gamePath string)

// GameStopCallback is called when a Steam game exits.
// appID is the Steam App ID that was running.
type GameStopCallback func(appID int)

// TrackedGame represents a currently tracked Steam game.
type TrackedGame struct {
	StartTime time.Time
	GamePath  string
	AppID     int
	PID       int
}

// Tracker monitors Steam game lifecycle events.
type Tracker struct {
	onGameStart  GameStartCallback
	onGameStop   GameStopCallback
	procTracker  *proctracker.Tracker
	tracked      map[int]*TrackedGame
	appIDToPID   map[int]int
	done         chan struct{}
	procPath     string
	wg           sync.WaitGroup
	pollInterval time.Duration
	mu           syncutil.Mutex
}

// Option configures a Tracker.
type Option func(*Tracker)

// WithPollInterval sets the polling interval for reaper process scanning.
func WithPollInterval(d time.Duration) Option {
	return func(t *Tracker) {
		t.pollInterval = d
	}
}

// WithProcPath sets a custom /proc path (for testing).
func WithProcPath(path string) Option {
	return func(t *Tracker) {
		t.procPath = path
	}
}

// New creates a new game tracker.
// onStart is called when a game starts, onStop is called when a game exits.
func New(onStart GameStartCallback, onStop GameStopCallback, opts ...Option) *Tracker {
	t := &Tracker{
		onGameStart:  onStart,
		onGameStop:   onStop,
		procTracker:  proctracker.New(),
		tracked:      make(map[int]*TrackedGame),
		appIDToPID:   make(map[int]int),
		pollInterval: DefaultPollInterval,
		procPath:     "/proc",
		done:         make(chan struct{}),
	}

	for _, opt := range opts {
		opt(t)
	}

	return t
}

// Start begins monitoring for Steam games.
func (t *Tracker) Start() error {
	t.wg.Add(1)
	go t.pollLoop()
	log.Info().Msg("steam game tracker started")
	return nil
}

// Stop stops the game tracker and waits for goroutines to finish.
func (t *Tracker) Stop() {
	close(t.done)
	t.procTracker.Stop()
	t.wg.Wait()
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

// pollLoop periodically scans for new reaper processes.
func (t *Tracker) pollLoop() {
	defer t.wg.Done()

	// Initial scan
	t.scan()

	ticker := time.NewTicker(t.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-t.done:
			return
		case <-ticker.C:
			t.scan()
		}
	}
}

// scan looks for new and exited reaper processes.
func (t *Tracker) scan() {
	reapers, err := ScanReaperProcessesWithProcPath(t.procPath)
	if err != nil {
		log.Warn().Err(err).Msg("failed to scan reaper processes")
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	// Build set of current reaper PIDs
	currentPIDs := make(map[int]bool)
	for _, r := range reapers {
		currentPIDs[r.PID] = true
	}

	// Check for new games
	for _, r := range reapers {
		// Skip if already tracking this PID
		if _, exists := t.tracked[r.PID]; exists {
			continue
		}

		// Skip if already tracking this AppID (dedup)
		if _, exists := t.appIDToPID[r.AppID]; exists {
			continue
		}

		// New game detected
		t.trackGame(r)
	}
}

// trackGame starts tracking a new game.
func (t *Tracker) trackGame(r ReaperProcess) {
	game := &TrackedGame{
		AppID:     r.AppID,
		PID:       r.PID,
		GamePath:  r.GamePath,
		StartTime: time.Now(),
	}

	t.tracked[r.PID] = game
	t.appIDToPID[r.AppID] = r.PID

	log.Info().
		Int("appID", r.AppID).
		Int("pid", r.PID).
		Str("gamePath", r.GamePath).
		Msg("detected Steam game start")

	appID := r.AppID
	pid := r.PID
	err := t.procTracker.Track(pid, func(_ int) {
		t.handleGameExit(pid, appID)
	})
	if err != nil {
		log.Warn().Err(err).Int("pid", pid).Msg("failed to track process exit")
	}

	if t.onGameStart != nil {
		go t.onGameStart(r.AppID, r.PID, r.GamePath)
	}
}

// handleGameExit is called when a tracked game's reaper process exits.
func (t *Tracker) handleGameExit(pid, appID int) {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Clean up tracking state
	delete(t.tracked, pid)
	delete(t.appIDToPID, appID)

	log.Info().
		Int("appID", appID).
		Int("pid", pid).
		Msg("detected Steam game exit")

	// Call stop callback
	if t.onGameStop != nil {
		go t.onGameStop(appID)
	}
}
