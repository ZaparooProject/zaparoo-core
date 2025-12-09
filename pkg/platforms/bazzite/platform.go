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

package bazzite

import (
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/launchers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/linuxbase"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
)

// Platform implements the Bazzite gaming platform.
// Bazzite is a Fedora Atomic distro with extensive gaming optimizations,
// pre-installed Steam and Lutris, and Heroic available via Flatpak.
type Platform struct {
	*linuxbase.Base
}

// NewPlatform creates a new Bazzite platform instance.
func NewPlatform() *Platform {
	return &Platform{
		Base: linuxbase.NewBase(platforms.PlatformIDBazzite),
	}
}

// SupportedReaders returns the list of enabled readers for Bazzite.
func (p *Platform) SupportedReaders(cfg *config.Instance) []readers.Reader {
	return linuxbase.SupportedReaders(cfg, p)
}

// Settings returns XDG-based settings for Bazzite.
func (*Platform) Settings() platforms.Settings {
	return linuxbase.Settings()
}

// LaunchMedia launches media using the appropriate launcher.
func (p *Platform) LaunchMedia(
	cfg *config.Instance,
	path string,
	launcher *platforms.Launcher,
	db *database.Database,
) error {
	//nolint:wrapcheck // Pass-through to base implementation
	return p.Base.LaunchMedia(cfg, path, launcher, db, p)
}

// Launchers returns the available launchers for Bazzite.
// Bazzite supports Steam (native/Flatpak), Lutris (pre-installed),
// Heroic (Flatpak via Bazaar), WebBrowser, and Generic scripts.
func (p *Platform) Launchers(cfg *config.Instance) []platforms.Launcher {
	ls := []platforms.Launcher{
		// Steam - support both native (default) and Flatpak
		launchers.NewSteamLauncher(launchers.SteamOptions{
			FallbackPath: "/usr/games/steam",
			UseXdgOpen:   true, // Works with native + Flatpak
			CheckFlatpak: true,
		}),

		// Lutris - pre-installed native on Bazzite
		launchers.NewLutrisLauncher(launchers.LutrisOptions{
			CheckFlatpak: true, // Also check Flatpak if user installs it
		}),

		// Heroic - typically Flatpak from Bazaar
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
