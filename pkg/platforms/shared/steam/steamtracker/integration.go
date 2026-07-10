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
	"fmt"
	"os"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/assets"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/linuxbase"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/linuxbase/procscanner"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/steam"
	"github.com/rs/zerolog/log"
)

// PlatformIntegration provides common game tracking integration for Steam-based platforms.
// It handles game start/stop callbacks and ActiveMedia management.
type PlatformIntegration struct {
	tracker        *Tracker
	base           *linuxbase.Base
	activeMedia    func() *models.ActiveMedia
	setActiveMedia func(*models.ActiveMedia)
	activeGames    map[int]int
	steamRoot      string
	mu             syncutil.Mutex
}

// NewPlatformIntegration creates a new platform integration for game tracking.
func NewPlatformIntegration(
	scanner *procscanner.Scanner,
	base *linuxbase.Base,
	activeMedia func() *models.ActiveMedia,
	setActiveMedia func(*models.ActiveMedia),
	steamRoots ...string,
) *PlatformIntegration {
	steamRoot := ""
	if len(steamRoots) > 0 {
		steamRoot = steamRoots[0]
	}

	pi := &PlatformIntegration{
		base:           base,
		activeMedia:    activeMedia,
		setActiveMedia: setActiveMedia,
		steamRoot:      steamRoot,
		activeGames:    make(map[int]int),
	}
	pi.tracker = New(scanner, pi.onGameStart, pi.onGameStop)
	return pi
}

// Start begins monitoring for Steam games.
func (pi *PlatformIntegration) Start() {
	pi.tracker.Start()
}

// Stop stops the game tracker.
func (pi *PlatformIntegration) Stop() {
	if pi.tracker != nil {
		pi.tracker.Stop()
	}
	pi.mu.Lock()
	clear(pi.activeGames)
	pi.mu.Unlock()
}

// onGameStart is called when a Steam game starts (detected via reaper process).
func (pi *PlatformIntegration) onGameStart(appID, reaperPID int, gamePath string) {
	pi.mu.Lock()
	pi.activeGames[appID] = reaperPID
	pi.mu.Unlock()

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

	if alreadyTracked {
		// Find and track the actual game process for killing.
		// Run in goroutine with retries since game may not have started yet.
		go pi.findAndTrackGameProcess(appID, reaperPID, gamePath)
		return
	}

	gameName, found := steam.LookupAppNameInSteamDir(pi.steamRoot, appID)
	if !found {
		gameName, found = steam.FindAppNameByAppID(appID)
	}
	if !found {
		gameName = steam.FormatGameName(appID, "")
	}

	log.Info().
		Int("appID", appID).
		Int("reaperPID", reaperPID).
		Str("gamePath", gamePath).
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
	if !pi.publishActiveMediaIfActive(appID, reaperPID, activeMedia) {
		log.Debug().Int("appID", appID).Int("reaperPID", reaperPID).
			Msg("discarding stale Steam game start")
		return
	}

	// Find and track the actual game process for killing after publishing
	// ActiveMedia, so ownership checks see this external Steam launch.
	go pi.findAndTrackGameProcess(appID, reaperPID, gamePath)
}

func (pi *PlatformIntegration) publishActiveMediaIfActive(
	appID, reaperPID int,
	activeMedia *models.ActiveMedia,
) bool {
	pi.mu.Lock()
	defer pi.mu.Unlock()
	if pi.activeGames[appID] != reaperPID {
		return false
	}
	pi.setActiveMedia(activeMedia)
	return true
}

// findAndTrackGameProcess attempts to find the game process with retries.
func (pi *PlatformIntegration) findAndTrackGameProcess(appID, reaperPID int, gamePath string) {
	const maxRetries = 10
	const retryDelay = 500 * time.Millisecond

	targets, installDir := resolveGameProcessTargets(pi.steamRoot, appID, gamePath)
	if len(targets) == 0 && installDir == "" {
		log.Debug().Int("appID", appID).Msg("no Steam process targets available")
		return
	}

	for i := range maxRetries {
		if !pi.gameIsActive(appID, reaperPID) {
			log.Debug().Int("appID", appID).Int("reaperPID", reaperPID).
				Msg("skipping stale Steam process lookup")
			return
		}

		if gamePID, ok := findGamePIDWithPaths("/proc", targets, installDir); ok {
			if !pi.gameIsActive(appID, reaperPID) {
				log.Debug().Int("appID", appID).Int("reaperPID", reaperPID).
					Msg("discarding stale Steam process match")
				return
			}
			if !pi.canOwnActiveProcess(appID) {
				log.Debug().Int("appID", appID).
					Msg("discarding Steam process owned by another active launch")
				return
			}

			log.Debug().Int("appID", appID).Int("gamePID", gamePID).
				Int("attempt", i+1).Msg("found Steam game process")
			if proc, err := os.FindProcess(gamePID); err == nil && pi.base != nil {
				pi.base.SetTrackedProcess(proc)
			}
			return
		}
		time.Sleep(retryDelay)
	}
	log.Warn().Int("appID", appID).Str("gamePath", gamePath).
		Msg("could not find Steam game process after retries")
}

func (pi *PlatformIntegration) gameIsActive(appID, reaperPID int) bool {
	pi.mu.Lock()
	defer pi.mu.Unlock()
	return pi.activeGames[appID] == reaperPID
}

func (pi *PlatformIntegration) canOwnActiveProcess(appID int) bool {
	current := pi.activeMedia()
	if current == nil {
		return true
	}

	currentAppID, ok := steam.ExtractAppIDFromPath(current.Path)
	return ok && currentAppID == appID
}

// onGameStop is called when a Steam game exits (reaper process terminated).
func (pi *PlatformIntegration) onGameStop(appID int) {
	pi.mu.Lock()
	defer pi.mu.Unlock()
	delete(pi.activeGames, appID)

	log.Info().Int("appID", appID).Msg("detected Steam game exit")
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
