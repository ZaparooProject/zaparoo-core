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
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared"
	"github.com/rs/zerolog/log"
)

// NewSteamLauncher creates a configurable Steam launcher.
func NewSteamLauncher(opts Options) platforms.Launcher {
	client := NewClient(opts)

	return platforms.Launcher{
		ID:       "Steam",
		SystemID: systemdefs.SystemPC,
		Schemes:  []string{shared.SchemeSteam},
		Scanner: func(
			_ context.Context,
			cfg *config.Instance,
			_ string,
			results []platforms.ScanResult,
		) ([]platforms.ScanResult, error) {
			steamRoot := client.FindSteamDir(cfg)
			steamAppsRoot := filepath.Join(steamRoot, "steamapps")

			// Scan official Steam apps
			appResults, err := client.ScanApps(steamAppsRoot)
			if err != nil {
				return nil, fmt.Errorf("failed to scan Steam apps: %w", err)
			}
			results = append(results, appResults...)

			// Scan non-Steam games (shortcuts)
			shortcutResults, err := client.ScanShortcuts(steamRoot)
			if err != nil {
				log.Warn().Err(err).Msg("failed to scan Steam shortcuts, continuing without them")
			} else {
				results = append(results, shortcutResults...)
			}

			return results, nil
		},
		Launch: func(cfg *config.Instance, path string) (*os.Process, error) {
			return client.Launch(cfg, path)
		},
	}
}
