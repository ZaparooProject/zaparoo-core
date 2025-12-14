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

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/assets"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/linuxbase"
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
}

// NewPlatformIntegration creates a new platform integration for game tracking.
func NewPlatformIntegration(
	base *linuxbase.Base,
	activeMedia func() *models.ActiveMedia,
	setActiveMedia func(*models.ActiveMedia),
) *PlatformIntegration {
	pi := &PlatformIntegration{
		base:           base,
		activeMedia:    activeMedia,
		setActiveMedia: setActiveMedia,
	}

	pi.tracker = New(pi.onGameStart, pi.onGameStop)
	return pi
}

// Start begins monitoring for Steam games.
func (pi *PlatformIntegration) Start() error {
	return pi.tracker.Start()
}

// Stop stops the game tracker.
func (pi *PlatformIntegration) Stop() {
	if pi.tracker != nil {
		pi.tracker.Stop()
	}
}

// onGameStart is called when a Steam game starts (detected via reaper process).
func (pi *PlatformIntegration) onGameStart(appID, pid int) {
	// Check if we're already tracking this game (launched via Zaparoo)
	current := pi.activeMedia()
	if current != nil {
		// Check if current media is this Steam game
		if existingAppID, ok := steam.ExtractAppIDFromPath(current.Path); ok {
			if existingAppID == appID {
				log.Debug().
					Int("appID", appID).
					Msg("game already tracked via Zaparoo launch")
				return
			}
		}
	}

	// Look up game name from appmanifest
	gameName, found := steam.FindAppNameByAppID(appID)
	if !found {
		gameName = steam.FormatGameName(appID, "")
	}

	log.Info().
		Int("appID", appID).
		Int("pid", pid).
		Str("name", gameName).
		Msg("detected external Steam game start")

	// Get PC system metadata for display name
	systemMeta, err := assets.GetSystemMetadata(systemdefs.SystemPC)
	if err != nil {
		log.Warn().Err(err).Msg("failed to get PC system metadata")
	}

	// Set active media
	activeMedia := models.NewActiveMedia(
		systemdefs.SystemPC,
		systemMeta.Name,
		fmt.Sprintf("steam://%d", appID),
		gameName,
		"Steam",
	)
	pi.setActiveMedia(activeMedia)

	// Store process for potential killing via StopActiveLauncher
	if proc, err := os.FindProcess(pid); err == nil {
		pi.base.SetTrackedProcess(proc)
	}
}

// onGameStop is called when a Steam game exits (reaper process terminated).
func (pi *PlatformIntegration) onGameStop(appID int) {
	log.Info().Int("appID", appID).Msg("detected Steam game exit")
	pi.setActiveMedia(nil)
}
