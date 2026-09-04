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

// trackedLaunch is the process recorded for one run of a game.
type trackedLaunch struct {
	pid         int
	lifecycleID int
}

// WindowsPlatformIntegration provides game tracking integration for Windows.
type WindowsPlatformIntegration struct {
	tracker         *Tracker
	setTrackedProc  func(*os.Process)
	clearTrackedPID func(int) bool
	activeMedia     func() *models.ActiveMedia
	setActiveMedia  func(*models.ActiveMedia)
	steamRoot       func() string
	tracked         map[int]trackedLaunch
	done            chan struct{}
	// active is the run that owns ActiveMedia and the tracked process. mu is
	// held across every ownership check and the state change it authorises.
	// The tracker dispatches the start and stop callbacks concurrently, so a
	// check that released the lock before acting on it would let a stale
	// callback publish media, or install or discard a process, for a run that
	// had already been replaced.
	active   launchKey
	mu       syncutil.Mutex
	wg       sync.WaitGroup
	stopOnce sync.Once
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
		tracked:         make(map[int]trackedLaunch),
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

// Stop stops the game tracker, waits for any in-flight process search and
// releases every process the integration handed to the platform. A search
// left running past shutdown would go on to publish a process, and publishing
// kills whatever was tracked before it. The tracker reports no exits when it
// stops, so without the release the platform would keep a handle for a game
// nothing is watching any more.
func (pi *WindowsPlatformIntegration) Stop() {
	pi.stopOnce.Do(func() {
		close(pi.done)
	})
	if pi.tracker != nil {
		pi.tracker.Stop()
	}
	pi.wg.Wait()

	pi.mu.Lock()
	defer pi.mu.Unlock()
	for appID, launch := range pi.tracked {
		if pi.clearTrackedPID != nil {
			pi.clearTrackedPID(launch.pid)
		}
		delete(pi.tracked, appID)
	}
	pi.active = launchKey{}
}

// claimLaunch records this run as the one that owns ActiveMedia from now on.
func (pi *WindowsPlatformIntegration) claimLaunch(appID, lifecycleID int) {
	pi.mu.Lock()
	pi.active = launchKey{appID: appID, lifecycleID: lifecycleID}
	pi.mu.Unlock()
}

// ownsLaunch reports whether the run is still the active one.
func (pi *WindowsPlatformIntegration) ownsLaunch(appID, lifecycleID int) bool {
	pi.mu.Lock()
	defer pi.mu.Unlock()
	return pi.active == launchKey{appID: appID, lifecycleID: lifecycleID}
}

// onGameStart is called when a Steam game starts (detected via registry).
func (pi *WindowsPlatformIntegration) onGameStart(appID, lifecycleID int, _ string) {
	pi.claimLaunch(appID, lifecycleID)

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
	pi.startProcessSearch(appID, lifecycleID)

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
	if !pi.publishActiveMediaIfActive(appID, lifecycleID, activeMedia) {
		log.Debug().Int("appID", appID).Int("lifecycleID", lifecycleID).
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

// publishActiveMediaIfActive publishes only while this run is still the
// active one. The check and the publish happen under one hold of the lock so
// a slow name lookup cannot land its media after a game that started in the
// meantime has already published its own.
func (pi *WindowsPlatformIntegration) publishActiveMediaIfActive(
	appID, lifecycleID int,
	activeMedia *models.ActiveMedia,
) bool {
	pi.mu.Lock()
	defer pi.mu.Unlock()
	if pi.active != (launchKey{appID: appID, lifecycleID: lifecycleID}) {
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
		if !pi.ownsLaunch(appID, lifecycleID) {
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
// run is still active. Recording and publishing happen under one hold of the
// lock so a concurrent exit cannot slip between them and leave a handle
// behind for a game that has already gone.
func (pi *WindowsPlatformIntegration) trackProcessIfActive(
	appID, lifecycleID, pid int, proc *os.Process,
) bool {
	pi.mu.Lock()
	defer pi.mu.Unlock()

	if pi.active != (launchKey{appID: appID, lifecycleID: lifecycleID}) {
		return false
	}
	select {
	case <-pi.done:
		return false
	default:
	}

	pi.tracked[appID] = trackedLaunch{pid: pid, lifecycleID: lifecycleID}
	pi.setTrackedProc(proc)
	return true
}

// onGameStop is called when a Steam game exits (registry cleared or replaced).
func (pi *WindowsPlatformIntegration) onGameStop(appID, lifecycleID int) {
	pi.mu.Lock()
	defer pi.mu.Unlock()

	// Forget the exited process without signalling it, so a later stop does
	// not act on a handle whose game is already gone. Only the run that
	// recorded the process may discard it: a delayed stop for an earlier run
	// of the same game must not throw away the process the current run has
	// since found, or the current run could no longer be stopped.
	if launch, ok := pi.tracked[appID]; ok && launch.lifecycleID == lifecycleID {
		delete(pi.tracked, appID)
		if pi.clearTrackedPID != nil {
			pi.clearTrackedPID(launch.pid)
		}
	}

	if pi.active != (launchKey{appID: appID, lifecycleID: lifecycleID}) {
		log.Debug().Int("appID", appID).Int("lifecycleID", lifecycleID).
			Msg("ignoring stale Steam game exit")
		return
	}
	pi.active = launchKey{}

	log.Info().Int("appID", appID).Int("lifecycleID", lifecycleID).Msg("detected Steam game exit")

	// Still under the lock: a relaunch of the same game that claims ownership
	// after this point publishes after this clear, not before it.
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
