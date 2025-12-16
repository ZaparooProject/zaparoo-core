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

package chimeraos

import (
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/launchers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/linuxbase"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/linuxbase/procscanner"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/steam"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/steam/steamtracker"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/rs/zerolog/log"
)

// Platform implements the ChimeraOS platform.
// ChimeraOS is a pure console-like couch gaming experience with
// controller-first UI booting directly into Steam Gamepad UI.
type Platform struct {
	*linuxbase.Base
	procScanner  *procscanner.Scanner
	steamTracker *steamtracker.PlatformIntegration
}

// NewPlatform creates a new ChimeraOS platform instance.
func NewPlatform() *Platform {
	return &Platform{
		Base: linuxbase.NewBase(platforms.PlatformIDChimeraOS),
	}
}

// SupportedReaders returns the list of enabled readers for ChimeraOS.
func (p *Platform) SupportedReaders(cfg *config.Instance) []readers.Reader {
	return linuxbase.SupportedReaders(cfg, p)
}

// Settings returns XDG-based settings for ChimeraOS.
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

	// Create process scanner for Steam game tracking
	p.procScanner = procscanner.New()
	if err := p.procScanner.Start(); err != nil {
		log.Warn().Err(err).Msg("process scanner failed to start")
		return nil
	}

	// Start Steam tracker for external Steam game detection
	p.steamTracker = steamtracker.NewPlatformIntegration(
		p.procScanner,
		p.Base,
		activeMedia,
		setActiveMedia,
	)
	p.steamTracker.Start()

	return nil
}

// Stop stops the platform and cleans up resources.
func (p *Platform) Stop() error {
	if p.steamTracker != nil {
		p.steamTracker.Stop()
	}
	if p.procScanner != nil {
		p.procScanner.Stop()
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

// Launchers returns the available launchers for ChimeraOS.
// ChimeraOS uses direct steam command (console experience) and supports
// GOG games installed via the Chimera web app.
func (p *Platform) Launchers(cfg *config.Instance) []platforms.Launcher {
	ls := []platforms.Launcher{
		// Steam - primary launcher, direct command for console experience
		steam.NewSteamLauncher(steam.DefaultChimeraOSOptions()),

		// ChimeraOS-specific GOG launcher (scans Chimera content)
		NewChimeraGOGLauncher(),

		// Generic scripts
		launchers.NewGenericLauncher(),
	}

	return append(helpers.ParseCustomLaunchers(p, cfg.CustomLaunchers()), ls...)
}
