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

package linux

import (
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	platformids "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/ids"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/kodi"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/launchers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/linuxbase"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/linuxbase/procscanner"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/steam"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/steam/steamtracker"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/rs/zerolog/log"
)

// Platform implements the generic Linux platform.
// This is the reference implementation for Linux-family platforms with
// full launcher support including Kodi, Steam, Lutris, and Heroic.
type Platform struct {
	*linuxbase.Base
	procScanner  *procscanner.Scanner
	steamTracker *steamtracker.PlatformIntegration
}

// NewPlatform creates a new Linux platform instance.
func NewPlatform() *Platform {
	return &Platform{
		Base: linuxbase.NewBase(platformids.Linux),
	}
}

// SupportedReaders returns the list of enabled readers for Linux.
func (p *Platform) SupportedReaders(cfg *config.Instance) []readers.Reader {
	return linuxbase.SupportedReaders(cfg, p)
}

// Settings returns XDG-based settings for Linux.
func (*Platform) Settings() platforms.Settings {
	return linuxbase.Settings()
}

// StartPost initializes the platform after service startup.
// Starts the Steam tracker if Steam is installed.
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

	// Only start Steam tracker if Steam is installed
	steamClient := steam.NewClient(steam.DefaultLinuxOptions())
	if steamClient.IsSteamInstalled(cfg) {
		p.procScanner = procscanner.New()
		if err := p.procScanner.Start(); err != nil {
			log.Warn().Err(err).Msg("process scanner failed to start")
			return nil
		}

		p.steamTracker = steamtracker.NewPlatformIntegration(
			p.procScanner,
			p.Base,
			activeMedia,
			setActiveMedia,
		)
		p.steamTracker.Start()
	} else {
		log.Debug().Msg("steam not installed, skipping steam tracker")
	}

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

// ReturnToMenu stops the active media on desktop Linux.
// Desktop Linux has no menu concept, so this just stops any running game/media.
func (p *Platform) ReturnToMenu() error {
	//nolint:wrapcheck // Pass-through to base implementation
	return p.StopActiveLauncher(platforms.StopForMenu)
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

// Launchers returns the available launchers for Linux.
// Linux supports the full set of launchers: Kodi (8 types), Steam,
// Lutris, Heroic, WebBrowser, and Generic scripts.
func (p *Platform) Launchers(cfg *config.Instance) []platforms.Launcher {
	ls := []platforms.Launcher{
		// Kodi launchers (8 types)
		kodi.NewKodiLocalLauncher(),
		kodi.NewKodiMovieLauncher(),
		kodi.NewKodiTVLauncher(),
		kodi.NewKodiMusicLauncher(),
		kodi.NewKodiSongLauncher(),
		kodi.NewKodiAlbumLauncher(),
		kodi.NewKodiArtistLauncher(),
		kodi.NewKodiTVShowLauncher(),

		// Steam - support native, Flatpak, and Snap
		steam.NewSteamLauncher(steam.DefaultLinuxOptions()),

		// Lutris - check both native and Flatpak
		launchers.NewLutrisLauncher(launchers.LutrisOptions{
			CheckFlatpak: true,
		}),

		// Heroic - check both native and Flatpak
		launchers.NewHeroicLauncher(launchers.HeroicOptions{
			CheckFlatpak: true,
		}),

		// Web browser for URLs
		launchers.NewWebBrowserLauncher(),

		// Generic scripts
		launchers.NewGenericLauncher(),
	}

	return append(helpers.ParseCustomLaunchers(p, cfg.CustomLaunchers()), ls...)
}
