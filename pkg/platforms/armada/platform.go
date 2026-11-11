//go:build linux && !android

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

// Package armada implements the Armada OS platform, a Fedora bootc distro
// with a SteamOS-like Gaming Mode for ARM64 handhelds.
package armada

import (
	"context"
	"os"
	"path/filepath"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	platformids "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/ids"
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

const defaultSteamRoot = "/var/home/armada/.local/share/Steam"

// Platform implements the Armada OS platform.
type Platform struct {
	*linuxbase.Base
	procScanner              *procscanner.Scanner
	steamTracker             *steamtracker.PlatformIntegration
	gameMode                 *gamescope.Manager
	emulationOptionsOverride *linuxemu.Options
}

// newProcScanner watches the real /proc; tests point it at a fake one.
var newProcScanner = func() *procscanner.Scanner { return procscanner.New() }

// NewPlatform creates a new Armada platform instance.
func NewPlatform() *Platform {
	return &Platform{
		Base:     linuxbase.NewBase(platformids.Armada),
		gameMode: gamescope.NewManager(gamescope.SessionOptions{Enabled: true}),
	}
}

// steamOptions targets the native linuxarm64 Steam client. Armada's
// /usr/bin/steam wrapper hands steam:// URLs to the running client, so the
// direct command is used rather than xdg-open. Flathub Steam is x86_64 only.
func steamOptions() steam.Options {
	return steam.Options{
		FallbackPath: defaultSteamRoot,
		UseXdgOpen:   false,
		CheckFlatpak: false,
	}
}

// SupportedReaders returns the list of enabled readers for Armada.
func (p *Platform) SupportedReaders(cfg *config.Instance) []readers.Reader {
	return linuxbase.SupportedReaders(cfg, p)
}

// Settings returns XDG-based settings for Armada.
func (*Platform) Settings() platforms.Settings {
	return linuxbase.Settings()
}

// RootDirs returns configured roots or ES-DE's default ROM root, because
// Armada itself does not define a ROM directory.
func (*Platform) RootDirs(cfg *config.Instance) []string {
	if roots := cfg.IndexRoots(); len(roots) > 0 {
		return roots
	}
	home, err := os.UserHomeDir()
	if err != nil {
		// No home, so no default root; a relative one would resolve
		// against wherever Core happens to run.
		return nil
	}
	return []string{filepath.Join(home, "ROMs")}
}

// StartPost initializes the platform after service startup and starts the
// Steam game tracker when Steam is installed.
func (p *Platform) StartPost(
	ctx context.Context,
	cfg *config.Instance,
	launcherManager platforms.LauncherContextManager,
	activeMedia func() *models.ActiveMedia,
	setActiveMedia func(*models.ActiveMedia),
	db *database.Database,
	scheduler *idle.Scheduler,
) error {
	//nolint:wrapcheck // Pass-through to base implementation
	if err := p.Base.StartPost(ctx, cfg, launcherManager, activeMedia, setActiveMedia, db, scheduler); err != nil {
		return err
	}

	steamClient := steam.NewClient(steamOptions())
	if !steamClient.IsSteamInstalled(cfg) {
		return nil
	}
	p.procScanner = newProcScanner()
	if err := p.procScanner.Start(); err != nil {
		log.Warn().Err(err).Msg("process scanner failed to start")
		return nil
	}
	p.SetProtectedProcess(ownsCore)
	p.steamTracker = steamtracker.NewPlatformIntegration(
		p.procScanner, p.Base, activeMedia, setActiveMedia, steamClient.FindSteamDir(cfg),
	)
	p.steamTracker.IgnoreProcessTree(ownsCore)
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

// ReturnToMenu stops active media. The Steam Gaming Mode shell remains
// responsible for presenting its menu.
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

// steamLaunchEnv gives Steam launches the user session's environment,
// including the gamescope DISPLAY in Gaming Mode, which the service unit lacks.
func (p *Platform) steamLaunchEnv() []string {
	return linuxbase.DesktopSessionEnvOverrides(p.gameMode)
}

// Launchers returns the available launchers for Armada. Armada ships an
// aarch64 Heroic; Lutris, Bottles and Faugus are omitted because they only
// ship for x86_64. Kodi is not offered on Armada.
func (p *Platform) Launchers(cfg *config.Instance) []platforms.Launcher {
	steamLauncher := steam.NewSteamLauncherWithEnv(steamOptions(), p.steamLaunchEnv)
	steamLauncher.Lifecycle = platforms.LifecycleExternal
	ls := make([]platforms.Launcher, 0, 64)
	ls = append(ls, []platforms.Launcher{
		steamLauncher,
		launchers.NewHeroicLauncher(launchers.HeroicOptions{
			CheckFlatpak: true,
		}),
		launchers.NewMoonlightLauncher(),
		launchers.NewWebBrowserLauncher(),
		launchers.NewGenericLauncher(),
	}...)

	custom := helpers.ParseCustomLaunchers(p, cfg.CustomLaunchers())
	existing := append(append(make([]platforms.Launcher, 0, len(custom)+len(ls)), custom...), ls...)
	emuOpts := p.emulationOptions()
	ls = append(ls, linuxemu.Launchers(cfg, emuOpts, existing)...)
	linuxemu.AttachPlainESDEScannersForRoots(cfg, emuOpts, ls, p.RootDirs(cfg))
	allLaunchers := helpers.CombineParsedLaunchers(custom, ls)
	p.gameMode.WrapLaunchers(allLaunchers)
	return allLaunchers
}

func (p *Platform) emulationOptions() linuxemu.Options {
	if p.emulationOptionsOverride != nil {
		return *p.emulationOptionsOverride
	}
	return linuxemu.DesktopEmulationOptions(
		p.gameMode, linuxemu.DesktopRetroArchOptions(linuxemu.DesktopRetroArchConfigPath()),
	)
}
