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
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/assets"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/steam"
	"github.com/rs/zerolog/log"
)

// Process search tuning. A game can take a while to appear behind a launcher
// stub, shader compilation or an anti-cheat bootstrap.
const (
	processSearchAttempts = 20
	processSearchDelay    = 500 * time.Millisecond
)

// WindowsPlatformIntegration provides game tracking integration for Windows.
type WindowsPlatformIntegration struct {
	tracker         *Tracker
	setTrackedProc  func(*os.Process)
	clearTrackedPID func(int) bool
	activeMedia     func() *models.ActiveMedia
	setActiveMedia  func(*models.ActiveMedia)
	steamRoot       func() string
	trackedPIDs     map[int]int
	done            chan struct{}
	activeLaunch    launchOwnership
	mu              syncutil.Mutex
	wg              sync.WaitGroup
	stopOnce        sync.Once
}

// NewWindowsPlatformIntegration creates a new platform integration for Windows.
// steamRoot resolves the Steam installation directory on each call, used to
// look up game names and install locations. It is a function rather than a
// value because the tracker can start long after Core did -- Steam may not have
// been installed yet -- and a root captured at startup would then be a stale
// fallback for the life of the process.
func NewWindowsPlatformIntegration(
	setTrackedProc func(*os.Process),
	clearTrackedPID func(int) bool,
	activeMedia func() *models.ActiveMedia,
	setActiveMedia func(*models.ActiveMedia),
	steamRoot func() string,
) *WindowsPlatformIntegration {
	pi := &WindowsPlatformIntegration{
		setTrackedProc:  setTrackedProc,
		clearTrackedPID: clearTrackedPID,
		activeMedia:     activeMedia,
		setActiveMedia:  setActiveMedia,
		steamRoot:       steamRoot,
		trackedPIDs:     make(map[int]int),
		done:            make(chan struct{}),
	}

	pi.tracker = New(pi.onGameStart, pi.onGameStop)
	return pi
}

// resolveSteamRoot returns the current Steam installation directory, or an
// empty string when none was supplied.
func (pi *WindowsPlatformIntegration) resolveSteamRoot() string {
	if pi.steamRoot == nil {
		return ""
	}
	return pi.steamRoot()
}

// Start begins monitoring for Steam games.
func (pi *WindowsPlatformIntegration) Start() error {
	return pi.tracker.Start()
}

// Stop stops the game tracker and waits for any in-flight process search.
// A search left running past shutdown would go on to publish a process, and
// publishing kills whatever was tracked before it.
func (pi *WindowsPlatformIntegration) Stop() {
	pi.stopOnce.Do(func() {
		close(pi.done)
	})
	if pi.tracker != nil {
		pi.tracker.Stop()
	}
	pi.wg.Wait()

	pi.mu.Lock()
	clear(pi.trackedPIDs)
	pi.mu.Unlock()
}

// onGameStart is called when a Steam game starts (detected via registry).
func (pi *WindowsPlatformIntegration) onGameStart(appID, pid int, _ string) {
	pi.activeLaunch.set(appID, pid)

	alreadyTracked := false
	current := pi.activeMedia()
	if current != nil {
		if existingAppID, ok := steam.ExtractAppIDFromPath(current.Path); ok {
			if existingAppID == appID {
				log.Debug().Int("appID", appID).Msg("game already tracked via Zaparoo launch")
				alreadyTracked = true
			}
		}
	}

	// Find and track the actual game process so it can be stopped later.
	pi.startProcessSearch(appID, pid)

	if alreadyTracked {
		return
	}

	gameName, found := steam.LookupAppNameInSteamDir(pi.resolveSteamRoot(), appID)
	if !found {
		gameName, found = steam.FindAppNameByAppID(appID)
	}
	if !found {
		gameName = steam.FormatGameName(appID, "")
	}

	log.Info().
		Int("appID", appID).
		Str("name", gameName).
		Msg("detected external Steam game start")

	systemMeta, err := assets.GetSystemMetadata(systemdefs.SystemPC)
	if err != nil {
		log.Warn().Err(err).Msg("failed to get PC system metadata")
	}

	activeMedia := models.NewActiveMedia(
		systemdefs.SystemPC,
		systemMeta.Name,
		fmt.Sprintf("steam://%d", appID),
		gameName,
		"Steam",
	)
	if !pi.publishActiveMediaIfActive(appID, pid, activeMedia) {
		log.Debug().Int("appID", appID).Int("pid", pid).
			Msg("discarding stale Steam game start")
	}
}

// startProcessSearch runs the process search under the integration's wait
// group so shutdown can wait for it.
func (pi *WindowsPlatformIntegration) startProcessSearch(appID, lifecycleID int) {
	select {
	case <-pi.done:
		return
	default:
	}

	pi.wg.Add(1)
	go func() {
		defer pi.wg.Done()
		pi.findAndTrackGameProcess(appID, lifecycleID)
	}()
}

// publishActiveMediaIfActive publishes only while this launch is still the
// active one, so a slow name lookup cannot overwrite media belonging to a
// game that started in the meantime.
func (pi *WindowsPlatformIntegration) publishActiveMediaIfActive(
	appID, pid int,
	activeMedia *models.ActiveMedia,
) bool {
	if !pi.activeLaunch.matches(appID, pid) {
		return false
	}
	pi.setActiveMedia(activeMedia)
	return true
}

// findAndTrackGameProcess looks for the game's process, retrying while it
// starts up. The ownership check before each attempt stops a late result from
// being applied to a game that has since been replaced -- which would
// otherwise track, and on the next stop kill, the wrong game.
func (pi *WindowsPlatformIntegration) findAndTrackGameProcess(appID, lifecycleID int) {
	// Resolved once: this reads appinfo.vdf and the library manifests, which
	// is far too expensive to repeat on every attempt, and neither value
	// changes while the game starts.
	paths := resolveGamePaths(pi.resolveSteamRoot(), appID)

	for i := range processSearchAttempts {
		if !pi.activeLaunch.matches(appID, lifecycleID) {
			log.Debug().Int("appID", appID).Msg("abandoning process search for replaced game")
			return
		}

		// The environment sweep reads every process's memory, so it is kept
		// for the final attempt once the cheap path matches have had their
		// chance.
		lastAttempt := i == processSearchAttempts-1
		proc, pid, err := scanForGameProcess(paths, appID, lastAttempt)
		if err != nil {
			log.Debug().Err(err).Int("appID", appID).Msg("error searching for game process")
		}
		if proc != nil {
			if pi.trackProcessIfActive(appID, lifecycleID, pid, proc) {
				log.Debug().Int("pid", pid).Int("attempt", i+1).Msg("found game process")
			} else {
				log.Debug().Int("appID", appID).Int("pid", pid).
					Msg("discarding stale game process match")
				_ = proc.Release()
			}
			return
		}

		if lastAttempt {
			break
		}
		select {
		case <-pi.done:
			return
		case <-time.After(processSearchDelay):
		}
	}
	log.Warn().Int("appID", appID).Msg("could not find game process after retries")
}

// trackProcessIfActive records and publishes the process, but only while the
// launch is still active. Recording and publishing happen under one lock so a
// concurrent exit cannot slip between them and leave a handle behind for a
// game that has already gone.
func (pi *WindowsPlatformIntegration) trackProcessIfActive(
	appID, lifecycleID, pid int, proc *os.Process,
) bool {
	pi.mu.Lock()
	defer pi.mu.Unlock()

	if !pi.activeLaunch.matches(appID, lifecycleID) {
		return false
	}
	select {
	case <-pi.done:
		return false
	default:
	}

	pi.trackedPIDs[appID] = pid
	pi.setTrackedProc(proc)
	return true
}

// onGameStop is called when a Steam game exits (registry cleared or replaced).
func (pi *WindowsPlatformIntegration) onGameStop(appID, pid int) {
	// Drop the recorded PID first. Start and stop callbacks are dispatched
	// concurrently, so a stop that loses the race to a replacement start would
	// otherwise leave this entry behind forever.
	pi.mu.Lock()
	trackedPID, hadPID := pi.trackedPIDs[appID]
	delete(pi.trackedPIDs, appID)
	// Forget the exited process without signalling it, so a later stop does
	// not act on a handle whose game is already gone.
	if hadPID && pi.clearTrackedPID != nil {
		pi.clearTrackedPID(trackedPID)
	}
	pi.mu.Unlock()

	if !pi.activeLaunch.clearIfMatches(appID, pid) {
		log.Debug().Int("appID", appID).Int("pid", pid).Msg("ignoring stale Steam game exit")
		return
	}

	log.Info().Int("appID", appID).Int("pid", pid).Msg("detected Steam game exit")

	current := pi.activeMedia()
	if current == nil {
		return
	}
	currentAppID, ok := steam.ExtractAppIDFromPath(current.Path)
	if !ok || currentAppID != appID {
		log.Debug().Int("appID", appID).Msg("preserving ActiveMedia owned by another launch")
		return
	}
	pi.setActiveMedia(nil)
}
