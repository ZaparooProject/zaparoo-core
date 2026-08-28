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
	"context"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	platformids "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/ids"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/kodi"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/launchers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/linuxbase"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/linuxbase/gamescope"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/linuxbase/procscanner"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/linuxemu"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/steam"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/steam/steamtracker"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/idle"
	"github.com/rs/zerolog/log"
)

// Platform implements the ChimeraOS platform.
// ChimeraOS is a pure console-like couch gaming experience with
// controller-first UI booting directly into Steam Gamepad UI.
type Platform struct {
	*linuxbase.Base
	procScanner              *procscanner.Scanner
	steamTracker             *steamtracker.PlatformIntegration
	gameMode                 *gamescope.Manager
	emulationOptionsOverride *linuxemu.Options
}

// NewPlatform creates a new ChimeraOS platform instance.
func NewPlatform() *Platform {
	return &Platform{
		Base:     linuxbase.NewBase(platformids.ChimeraOS),
		gameMode: gamescope.NewManager(gamescope.SessionOptions{Enabled: true}),
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
	ctx context.Context,
	cfg *config.Instance,
	launcherManager platforms.LauncherContextManager,
	activeMedia func() *models.ActiveMedia,
	setActiveMedia func(*models.ActiveMedia),
	db *database.Database,
	scheduler *idle.Scheduler,
) error {
	// Initialize base platform
	//nolint:wrapcheck // Pass-through to base implementation
	if err := p.Base.StartPost(ctx, cfg, launcherManager, activeMedia, setActiveMedia, db, scheduler); err != nil {
		return err
	}

	steamClient := steam.NewClient(steam.DefaultChimeraOSOptions())
	if !steamClient.IsSteamInstalled(cfg) {
		return nil
	}
	p.procScanner = procscanner.New()
	if err := p.procScanner.Start(); err != nil {
		log.Warn().Err(err).Msg("process scanner failed to start")
		return nil
	}
	p.steamTracker = steamtracker.NewPlatformIntegration(
		p.procScanner, p.Base, activeMedia, setActiveMedia, steamClient.FindSteamDir(cfg),
	)
	p.steamTracker.Start()

	return nil
}

// Stop stops the platform and cleans up resources.
func (p *Platform) Stop() error {
	p.gameMode.RevertFocus()
	if p.steamTracker != nil {
		p.steamTracker.Stop()
	}
	if p.procScanner != nil {
		p.procScanner.Stop()
	}
	//nolint:wrapcheck // Pass-through to base implementation
	return p.Base.Stop()
}

// ReturnToMenu stops active media on ChimeraOS. Its Steam shell remains
// responsible for presenting the menu.
func (p *Platform) ReturnToMenu() error {
	//nolint:wrapcheck // Pass-through to the shared Linux process manager.
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

// Launchers returns the available launchers for ChimeraOS.
// ChimeraOS uses direct steam command (console experience) and supports
// GOG games installed via the Chimera web app.
func (p *Platform) Launchers(cfg *config.Instance) []platforms.Launcher {
	steamLauncher := steam.NewSteamLauncher(steam.DefaultChimeraOSOptions())
	steamLauncher.Lifecycle = platforms.LifecycleExternal
	ls := make([]platforms.Launcher, 0, 64)
	ls = append(ls, []platforms.Launcher{
		// Kodi launchers (8 types)
		kodi.NewKodiLocalLauncher(),
		kodi.NewKodiMovieLauncher(),
		kodi.NewKodiTVLauncher(),
		kodi.NewKodiMusicLauncher(),
		kodi.NewKodiSongLauncher(),
		kodi.NewKodiAlbumLauncher(),
		kodi.NewKodiArtistLauncher(),
		kodi.NewKodiTVShowLauncher(),

		// Steam - primary launcher, direct command for console experience
		steamLauncher,

		// ChimeraOS-specific GOG launcher (scans Chimera content)
		NewChimeraGOGLauncher(),

		// Optional Linux game managers and remote streaming
		launchers.NewBottlesLauncher(),
		launchers.NewFaugusLauncher(),
		launchers.NewMoonlightLauncher(),

		// Generic scripts
		launchers.NewGenericLauncher(),
	}...)

	custom := helpers.ParseCustomLaunchers(p, cfg.CustomLaunchers())
	existing := append(append(make([]platforms.Launcher, 0, len(custom)+len(ls)), custom...), ls...)
	emuOpts := p.emulationOptions()
	ls = append(ls, linuxemu.Launchers(cfg, emuOpts, existing)...)
	linuxemu.AttachPlainESDEScanners(cfg, emuOpts, ls)
	allLaunchers := make([]platforms.Launcher, 0, len(custom)+len(ls))
	allLaunchers = append(allLaunchers, custom...)
	allLaunchers = append(allLaunchers, ls...)
	p.gameMode.WrapLaunchers(allLaunchers)
	return allLaunchers
}

func (*Platform) retroArchConfigPath() string {
	return linuxemu.DesktopRetroArchConfigPath()
}

func (p *Platform) emulationOptions() linuxemu.Options {
	if p.emulationOptionsOverride != nil {
		return *p.emulationOptionsOverride
	}
	return linuxemu.DesktopEmulationOptions(
		p.gameMode, linuxemu.DesktopRetroArchOptions(p.retroArchConfigPath()),
	)
}
