//go:build darwin

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
	"github.com/rs/zerolog/log"
	"github.com/shirou/gopsutil/v4/process"
)

// pollInterval is the interval between game state scans on macOS.
const pollInterval = DefaultPollInterval

// trackedGameDarwin represents a game being tracked for exit detection.
type trackedGameDarwin struct {
	GamePath string
	GameName string
	AppID    int
	PID      int
}

// Tracker monitors for Steam game starts and stops on macOS.
// It uses polling-based process enumeration since macOS doesn't have
// registry events (like Windows) or reaper processes (like Linux).
type Tracker struct {
	onGameStart GameStartCallback
	onGameStop  GameStopCallback
	tracked     map[int]*trackedGameDarwin // appID -> game info
	done        chan struct{}
	trackedMu   syncutil.Mutex
	wg          sync.WaitGroup
}

// New creates a new macOS Steam game tracker.
func New(onStart GameStartCallback, onStop GameStopCallback, _ ...Option) *Tracker {
	return &Tracker{
		onGameStart: onStart,
		onGameStop:  onStop,
		done:        make(chan struct{}),
		tracked:     make(map[int]*trackedGameDarwin),
	}
}

// Option configures a Tracker.
type Option func(*Tracker)

// Start begins tracking Steam games.
func (t *Tracker) Start() error {
	t.wg.Add(1)
	go t.pollLoop()
	log.Info().Msg("macOS Steam game tracker started")
	return nil
}

// Stop stops tracking Steam games.
func (t *Tracker) Stop() {
	close(t.done)
	t.wg.Wait()
	log.Info().Msg("macOS Steam game tracker stopped")
}

// pollLoop periodically scans for Steam game processes.
func (t *Tracker) pollLoop() {
	defer t.wg.Done()

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	// Do an initial scan
	t.scanForGames()

	for {
		select {
		case <-t.done:
			return
		case <-ticker.C:
			t.scanForGames()
		}
	}
}

// scanForGames scans for running Steam games and detects starts/stops.
func (t *Tracker) scanForGames() {
	// Find all running Steam game processes
	gameProcs, err := FindSteamGameProcesses()
	if err != nil {
		log.Debug().Err(err).Msg("failed to scan for Steam games")
		return
	}

	t.trackedMu.Lock()
	defer t.trackedMu.Unlock()

	// Build set of currently running appIDs
	runningAppIDs := make(map[int]SteamProcess)
	for _, proc := range gameProcs {
		// If multiple processes for same appID, keep the first one found
		if _, exists := runningAppIDs[proc.AppID]; !exists {
			runningAppIDs[proc.AppID] = proc
		}
	}

	// Check for stopped games (was tracked but no longer running)
	for appID, game := range t.tracked {
		if _, stillRunning := runningAppIDs[appID]; !stillRunning {
			// Also verify the specific PID is gone
			if !isProcessRunning(game.PID) {
				log.Info().
					Int("appID", appID).
					Int("pid", game.PID).
					Str("gameName", game.GameName).
					Msg("detected Steam game stop")

				if t.onGameStop != nil {
					go t.onGameStop(appID)
				}
				delete(t.tracked, appID)
			}
		}
	}

	// Check for newly started games
	for appID, proc := range runningAppIDs {
		if _, alreadyTracked := t.tracked[appID]; !alreadyTracked {
			log.Info().
				Int("appID", appID).
				Int("pid", proc.PID).
				Str("exe", proc.Exe).
				Str("gameName", proc.GameName).
				Msg("detected Steam game start")

			t.tracked[appID] = &trackedGameDarwin{
				AppID:    appID,
				PID:      proc.PID,
				GamePath: proc.Exe,
				GameName: proc.GameName,
			}

			if t.onGameStart != nil {
				go t.onGameStart(appID, proc.PID, proc.Exe)
			}
		}
	}
}

// isProcessRunning checks if a process with the given PID is still running.
func isProcessRunning(pid int) bool {
	exists, err := process.PidExists(int32(pid)) //nolint:gosec // PID fits in int32
	if err != nil {
		return false
	}
	return exists
}

// TrackedGames returns a copy of currently tracked games.
func (t *Tracker) TrackedGames() []TrackedGame {
	t.trackedMu.Lock()
	defer t.trackedMu.Unlock()

	result := make([]TrackedGame, 0, len(t.tracked))
	for _, game := range t.tracked {
		result = append(result, TrackedGame{
			AppID:     game.AppID,
			PID:       game.PID,
			GamePath:  game.GamePath,
			StartTime: time.Now(), // Approximate, we don't track exact start time
		})
	}
	return result
}
