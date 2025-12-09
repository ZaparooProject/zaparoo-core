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

package launchers

import (
	"os"
	"path/filepath"

	"github.com/rs/zerolog/log"
)

// Common Flatpak app IDs.
const (
	FlatpakSteamID  = "com.valvesoftware.Steam"
	FlatpakLutrisID = "net.lutris.Lutris"
	FlatpakHeroicID = "com.heroicgameslauncher.hgl"
)

// FlatpakBasePath returns the base path for Flatpak app data.
func FlatpakBasePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".var", "app")
}

// FlatpakAppPath returns the data path for a specific Flatpak app.
func FlatpakAppPath(appID string) string {
	return filepath.Join(FlatpakBasePath(), appID)
}

// IsFlatpakInstalled checks if a Flatpak app is installed by checking for its data directory.
// Note: This directory is created when the app is first run, not when installed.
func IsFlatpakInstalled(appID string) bool {
	path := FlatpakAppPath(appID)
	if _, err := os.Stat(path); err != nil {
		log.Debug().
			Str("appID", appID).
			Msg("Flatpak data dir not found; if installed, run the app once to generate configs")
		return false
	}
	return true
}

// FindLutrisDB finds the Lutris pga.db database file.
// Checks native path first, then Flatpak path if enabled.
func FindLutrisDB(checkFlatpak bool) (string, bool) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", false
	}

	// Native path (default on Bazzite and most distros)
	nativePath := filepath.Join(home, ".local", "share", "lutris", "pga.db")
	if _, err := os.Stat(nativePath); err == nil {
		return nativePath, true
	}

	if checkFlatpak {
		flatpakPath := filepath.Join(
			FlatpakAppPath(FlatpakLutrisID),
			"data", "lutris", "pga.db",
		)
		if _, err := os.Stat(flatpakPath); err == nil {
			return flatpakPath, true
		}
	}

	return "", false
}

// FindHeroicStoreCache finds the Heroic store_cache directory.
// Checks native path first, then Flatpak path if enabled.
func FindHeroicStoreCache(checkFlatpak bool) (string, bool) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", false
	}

	// Native path
	nativePath := filepath.Join(home, ".config", "heroic", "store_cache")
	if _, err := os.Stat(nativePath); err == nil {
		return nativePath, true
	}

	if checkFlatpak {
		flatpakPath := filepath.Join(
			FlatpakAppPath(FlatpakHeroicID),
			"config", "heroic", "store_cache",
		)
		if _, err := os.Stat(flatpakPath); err == nil {
			return flatpakPath, true
		}
	}

	return "", false
}
