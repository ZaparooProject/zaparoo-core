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

package steamos

import (
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/launchers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/linuxbase"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/steam"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/steam/steamtracker"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/rs/zerolog/log"
)

// Platform implements the SteamOS platform (Steam Deck and compatible handhelds).
// Uses console-first approach with direct steam command for Game Mode integration.
type Platform struct {
	*linuxbase.Base
	steamTracker *steamtracker.PlatformIntegration
}

// NewPlatform creates a new SteamOS platform instance.
func NewPlatform() *Platform {
	return &Platform{
		Base: linuxbase.NewBase(platforms.PlatformIDSteamOS),
	}
}

// SupportedReaders returns the list of enabled readers for SteamOS.
func (p *Platform) SupportedReaders(cfg *config.Instance) []readers.Reader {
	return linuxbase.SupportedReaders(cfg, p)
}

// Settings returns XDG-based settings for SteamOS.
func (*Platform) Settings() platforms.Settings {
	return linuxbase.Settings()
}

// StartPost initializes the platform after service startup.
// Starts the game tracker to monitor Steam game lifecycle.
func (p *Platform) StartPost(
	cfg *config.Instance,
	launcherManager platforms.LauncherContextManager,
	activeMedia func() *models.ActiveMedia,
	setActiveMedia func(*models.ActiveMedia),
	db *database.Database,
) error {
	// Initialize base platform
	//nolint:wrapcheck // Pass-through to base implementation
	if err := p.Base.StartPost(cfg, launcherManager, activeMedia, setActiveMedia, db); err != nil {
		return err
	}

	// Start Steam tracker for external Steam game detection
	p.steamTracker = steamtracker.NewPlatformIntegration(p.Base, activeMedia, setActiveMedia)
	if err := p.steamTracker.Start(); err != nil {
		log.Warn().Err(err).Msg("steam game tracker failed to start")
	}

	return nil
}

// Stop stops the platform and cleans up resources.
func (p *Platform) Stop() error {
	if p.steamTracker != nil {
		p.steamTracker.Stop()
	}
	//nolint:wrapcheck // Pass-through to base implementation
	return p.Base.Stop()
}

// LaunchMedia launches media using the appropriate launcher.
func (p *Platform) LaunchMedia(
	cfg *config.Instance,
	path string,
	launcher *platforms.Launcher,
	db *database.Database,
	opts *platforms.LaunchOptions,
) error {
	//nolint:wrapcheck // Pass-through to base implementation
	return p.Base.LaunchMedia(cfg, path, launcher, db, opts, p)
}

// Launchers returns the available launchers for SteamOS.
// SteamOS uses direct steam command (not xdg-open) for better Game Mode integration.
func (p *Platform) Launchers(cfg *config.Instance) []platforms.Launcher {
	ls := []platforms.Launcher{
		// Steam with Steam Deck optimizations
		steam.NewSteamLauncher(steam.DefaultSteamOSOptions()),

		// Generic for EmuDeck and custom scripts
		launchers.NewGenericLauncher(),
	}

	return append(helpers.ParseCustomLaunchers(p, cfg.CustomLaunchers()), ls...)
}
