// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
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

import (
	"os"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
)

// SteamClient defines the interface for Steam operations.
// This interface enables proper mocking and TDD for Steam integration.
type SteamClient interface {
	// FindSteamDir locates the Steam installation directory.
	// Returns the path to the Steam root directory or the fallback path.
	FindSteamDir(cfg *config.Instance) string

	// Launch launches a Steam game by its virtual path.
	// Path format: "steam://[id]/[name]" or "steam://rungameid/[id]"
	// Returns nil for fire-and-forget launches (Steam handles the process).
	Launch(cfg *config.Instance, path string) (*os.Process, error)

	// ScanApps scans Steam library for installed official apps.
	// steamAppsDir should point to the steamapps directory (e.g., ~/.steam/steam/steamapps).
	ScanApps(steamAppsDir string) ([]platforms.ScanResult, error)

	// ScanShortcuts scans Steam for non-Steam games (user-added shortcuts).
	// steamDir should point to the Steam root directory.
	ScanShortcuts(steamDir string) ([]platforms.ScanResult, error)
}
