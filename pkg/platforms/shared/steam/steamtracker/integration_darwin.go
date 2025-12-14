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
	"fmt"
	"os"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/assets"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/steam"
	"github.com/rs/zerolog/log"
)

// DarwinPlatformIntegration provides game tracking integration for macOS.
type DarwinPlatformIntegration struct {
	tracker        *Tracker
	setTrackedProc func(*os.Process)
	activeMedia    func() *models.ActiveMedia
	setActiveMedia func(*models.ActiveMedia)
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

// Stop stops the game tracker.
func (pi *DarwinPlatformIntegration) Stop() {
	if pi.tracker != nil {
		pi.tracker.Stop()
	}
}

// onGameStart is called when a Steam game starts (detected via process scanning).
func (pi *DarwinPlatformIntegration) onGameStart(appID, _ int, _ string) {
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
	go pi.findAndTrackGameProcess(appID)

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
	pi.setActiveMedia(activeMedia)
}

// findAndTrackGameProcess attempts to find the game process with retries.
func (pi *DarwinPlatformIntegration) findAndTrackGameProcess(appID int) {
	const maxRetries = 10
	const retryDelay = 500 * time.Millisecond

	for i := range maxRetries {
		proc, pid, err := FindGameProcess(appID)
		if err == nil && proc != nil {
			log.Debug().Int("pid", pid).Int("attempt", i+1).Msg("found game process")
			pi.setTrackedProc(proc)
			return
		}
		time.Sleep(retryDelay)
	}
	log.Warn().Int("appID", appID).Msg("could not find game process after retries")
}

// onGameStop is called when a Steam game exits (process no longer found).
func (pi *DarwinPlatformIntegration) onGameStop(appID int) {
	log.Info().Int("appID", appID).Msg("detected Steam game exit")
	pi.setActiveMedia(nil)
}
