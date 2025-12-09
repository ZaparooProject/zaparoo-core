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

// Package launchers provides reusable, configurable launcher factories for
// Linux-family platforms. These launchers support both native and Flatpak
// installations where applicable.
//
// Launchers are configured via option structs (e.g., SteamOptions) that allow
// platforms to customize behavior such as:
//   - Fallback paths for application detection
//   - Whether to use xdg-open or direct commands
//   - Whether to check Flatpak installations
//
// Example:
//
//	steam := launchers.NewSteamLauncher(launchers.SteamOptions{
//	    FallbackPath: "/home/deck/.steam/steam",
//	    UseXdgOpen:   false, // Direct steam command for Game Mode
//	    CheckFlatpak: false,
//	})
package launchers
