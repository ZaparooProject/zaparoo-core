//go:build linux

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

import "path/filepath"

// platformSteamAppsDirs returns default steamapps locations for Linux,
// covering native, Steam Deck, Flatpak and Snap installs. Every candidate is
// relative to the home directory, so an empty home yields none rather than
// bare relative paths that would resolve against the working directory.
func platformSteamAppsDirs(home string) []string {
	if home == "" {
		return nil
	}
	return []string{
		filepath.Join(home, ".steam", "steam", "steamapps"),
		filepath.Join(home, ".local", "share", "Steam", "steamapps"),
		// Steam Deck
		filepath.Join(home, ".steam", "steamapps"),
		// Flatpak
		filepath.Join(home, ".var", "app", FlatpakSteamID, ".steam", "steam", "steamapps"),
		// Snap
		filepath.Join(home, "snap", "steam", "common", ".steam", "steam", "steamapps"),
	}
}
