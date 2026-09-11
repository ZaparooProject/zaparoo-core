//go:build darwin

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
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/assets"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/steam"
	"github.com/rs/zerolog/log"
)

// DarwinPlatformIntegration provides game tracking integration for macOS.
type DarwinPlatformIntegration struct {
	tracker        *Tracker
	setTrackedProc func(*os.Process)
	activeMedia    func() *models.ActiveMedia
	setActiveMedia func(*models.ActiveMedia)
	// active is the run that owns ActiveMedia and the tracked process. mu is
	// held across every ownership check and the state change it authorises,
	// so a callback for a run that has since been replaced can neither
	// publish its media nor install its process.
	active launchKey
	mu     syncutil.Mutex
}

// NewDarwinPlatformIntegration creates a new platform integration for macOS.
func NewDarwinPlatformIntegration(
	setTrackedProc func(*os.Process),
	activeMedia func() *models.ActiveMedia,
	setActiveMedia func(*models.ActiveMedia),
) *DarwinPlatformIntegration {
	pi := &DarwinPlatformIntegration{
		setTrackedProc: setTrackedProc,
		activeMedia:    activeMedia,
		setActiveMedia: setActiveMedia,
	}

	pi.tracker = New(pi.onGameStart, pi.onGameStop)
	return pi
}

// Start begins monitoring for Steam games.
func (pi *DarwinPlatformIntegration) Start() error {
	return pi.tracker.Start()
}

// Stop stops the game tracker and forgets the run it was following.
func (pi *DarwinPlatformIntegration) Stop() {
	if pi.tracker != nil {
		pi.tracker.Stop()
	}
	pi.mu.Lock()
	pi.active = launchKey{}
	pi.mu.Unlock()
}

// claimLaunch records this run as the one that owns ActiveMedia from now on.
func (pi *DarwinPlatformIntegration) claimLaunch(appID, lifecycleID int) {
	pi.mu.Lock()
	pi.active = launchKey{appID: appID, lifecycleID: lifecycleID}
	pi.mu.Unlock()
}

// ownsLaunch reports whether the run is still the active one.
func (pi *DarwinPlatformIntegration) ownsLaunch(appID, lifecycleID int) bool {
	pi.mu.Lock()
	defer pi.mu.Unlock()
	return pi.active == launchKey{appID: appID, lifecycleID: lifecycleID}
}

// onGameStart is called when a Steam game starts (detected via process scanning).
func (pi *DarwinPlatformIntegration) onGameStart(appID, pid int, _ string) {
	pi.claimLaunch(appID, pid)
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

	// Find and track the actual game process for killing.
	go pi.findAndTrackGameProcess(appID, pid)

	if alreadyTracked {
		return
	}

	gameName, found := steam.FindAppNameByAppID(appID)
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
		log.Debug().Int("appID", appID).Int("pid", pid).Msg("discarding stale Steam game start")
	}
}

// publishActiveMediaIfActive publishes only while this run is still the
// active one. The check and the publish happen under one hold of the lock so
// a slow name lookup cannot land its media after a game that started in the
// meantime has already published its own.
func (pi *DarwinPlatformIntegration) publishActiveMediaIfActive(
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

// findAndTrackGameProcess attempts to find the game process with retries. The
// ownership check before each attempt stops a late result being applied to a
// game that has since been replaced, which would otherwise track -- and on the
// next stop kill -- the wrong game.
func (pi *DarwinPlatformIntegration) findAndTrackGameProcess(appID, lifecycleID int) {
	const maxRetries = 10
	const retryDelay = 500 * time.Millisecond

	for i := range maxRetries {
		if !pi.ownsLaunch(appID, lifecycleID) {
			log.Debug().Int("appID", appID).Msg("abandoning process search for replaced game")
			return
		}
		proc, pid, err := FindGameProcess("", appID)
		if err == nil && proc != nil {
			if pi.trackProcessIfActive(appID, lifecycleID, proc) {
				log.Debug().Int("pid", pid).Int("attempt", i+1).Msg("found game process")
			} else {
				log.Debug().Int("appID", appID).Int("pid", pid).
					Msg("discarding stale game process match")
				_ = proc.Release()
			}
			return
		}
		time.Sleep(retryDelay)
	}
	log.Warn().Int("appID", appID).Msg("could not find game process after retries")
}

// trackProcessIfActive publishes the process, but only while the run is still
// active, under the same hold of the lock as the check.
func (pi *DarwinPlatformIntegration) trackProcessIfActive(
	appID, lifecycleID int, proc *os.Process,
) bool {
	pi.mu.Lock()
	defer pi.mu.Unlock()
	if pi.active != (launchKey{appID: appID, lifecycleID: lifecycleID}) {
		return false
	}
	pi.setTrackedProc(proc)
	return true
}

// onGameStop is called when a Steam game exits (process no longer found).
func (pi *DarwinPlatformIntegration) onGameStop(appID, pid int) {
	pi.mu.Lock()
	defer pi.mu.Unlock()

	if pi.active != (launchKey{appID: appID, lifecycleID: pid}) {
		log.Debug().Int("appID", appID).Int("pid", pid).Msg("ignoring stale Steam game exit")
		return
	}
	pi.active = launchKey{}

	// Still under the lock: a relaunch of the same game that claims ownership
	// after this point publishes after this clear, not before it.
	current := pi.activeMedia()
	if current == nil {
		return
	}
	currentAppID, ok := steam.ExtractAppIDFromPath(current.Path)
	if !ok || currentAppID != appID {
		return
	}
	log.Info().Int("appID", appID).Int("pid", pid).Msg("detected Steam game exit")
	pi.setActiveMedia(nil)
}
