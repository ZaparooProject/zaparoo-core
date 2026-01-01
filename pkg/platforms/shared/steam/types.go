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

package steam

// Options configures Steam client behavior across all platforms.
type Options struct {
	// FallbackPath is used if Steam directory detection fails.
	// Linux examples: "/home/deck/.steam/steam", "/usr/games/steam"
	// Windows example: "C:\\Program Files (x86)\\Steam"
	FallbackPath string

	// ExtraPaths are additional paths to check for Steam installation.
	// Only used on Linux; ignored on Windows.
	ExtraPaths []string

	// UseXdgOpen uses xdg-open for launching (desktop-friendly).
	// When false, uses direct `steam` command (console/Game Mode friendly).
	// Only used on Linux; ignored on Windows.
	UseXdgOpen bool

	// CheckFlatpak enables checking for Flatpak Steam installation.
	// Only used on Linux; ignored on Windows.
	CheckFlatpak bool
}

// DefaultLinuxOptions returns sensible defaults for desktop Linux.
func DefaultLinuxOptions() Options {
	return Options{
		FallbackPath: "/usr/games/steam",
		UseXdgOpen:   true,
		CheckFlatpak: true,
	}
}

// DefaultSteamOSOptions returns optimized settings for SteamOS/Steam Deck.
func DefaultSteamOSOptions() Options {
	return Options{
		FallbackPath: "/home/deck/.steam/steam",
		ExtraPaths:   []string{"/home/deck/.local/share/Steam"},
		UseXdgOpen:   false, // Direct steam command for Game Mode
		CheckFlatpak: false,
	}
}

// DefaultBazziteOptions returns settings for Bazzite (Fedora Atomic gaming distro).
func DefaultBazziteOptions() Options {
	return Options{
		FallbackPath: "/usr/games/steam",
		UseXdgOpen:   true, // Works with native + Flatpak
		CheckFlatpak: true,
	}
}

// DefaultChimeraOSOptions returns optimized settings for ChimeraOS.
func DefaultChimeraOSOptions() Options {
	return Options{
		FallbackPath: "/home/gamer/.steam/steam",
		UseXdgOpen:   false, // Direct steam command for console mode
		CheckFlatpak: false,
	}
}

// DefaultWindowsOptions returns sensible defaults for Windows.
func DefaultWindowsOptions() Options {
	return Options{
		FallbackPath: `C:\Program Files (x86)\Steam`,
	}
}

// DefaultDarwinOptions returns sensible defaults for macOS.
func DefaultDarwinOptions() Options {
	return Options{
		FallbackPath: "~/Library/Application Support/Steam",
	}
}
