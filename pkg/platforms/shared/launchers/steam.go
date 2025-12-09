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
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/virtualpath"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared"
	"github.com/rs/zerolog/log"
)

// NewSteamLauncher creates a configurable Steam launcher.
func NewSteamLauncher(opts SteamOptions) platforms.Launcher {
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
			steamRoot := findSteamDir(cfg, opts)
			steamAppsRoot := filepath.Join(steamRoot, "steamapps")

			// Scan official Steam apps
			appResults, err := helpers.ScanSteamApps(steamAppsRoot)
			if err != nil {
				return nil, fmt.Errorf("failed to scan Steam apps: %w", err)
			}
			results = append(results, appResults...)

			// Scan non-Steam games (shortcuts)
			shortcutResults, err := helpers.ScanSteamShortcuts(steamRoot)
			if err != nil {
				log.Warn().Err(err).Msg("failed to scan Steam shortcuts, continuing without them")
			} else {
				results = append(results, shortcutResults...)
			}

			return results, nil
		},
		Launch: func(_ *config.Instance, path string) (*os.Process, error) {
			// Handle native Steam URL format: steam://rungameid/123
			// Normalize to standard virtual path format: steam://123
			if strings.HasPrefix(path, "steam://rungameid/") {
				path = strings.Replace(path, "steam://rungameid/", "steam://", 1)
			}

			id, err := virtualpath.ExtractSchemeID(path, shared.SchemeSteam)
			if err != nil {
				return nil, fmt.Errorf("failed to extract Steam game ID from path: %w", err)
			}

			if _, parseErr := strconv.ParseUint(id, 10, 64); parseErr != nil {
				return nil, fmt.Errorf("invalid Steam game ID: %s", id)
			}

			steamURL := "steam://rungameid/" + id

			var cmd *exec.Cmd
			if opts.UseXdgOpen {
				// Desktop-friendly: works with native + Flatpak Steam
				cmd = exec.CommandContext( //nolint:gosec // Steam ID validated as numeric-only above
					context.Background(), "xdg-open", steamURL)
			} else {
				// Console-friendly: direct steam command for Game Mode
				cmd = exec.CommandContext( //nolint:gosec // Steam ID validated as numeric-only above
					context.Background(), "steam", steamURL)
			}

			if err := cmd.Start(); err != nil {
				return nil, fmt.Errorf("failed to launch Steam: %w", err)
			}
			return nil, nil
		},
	}
}

func findSteamDir(cfg *config.Instance, opts SteamOptions) string {
	// Check for user-configured Steam install directory first
	if def, ok := cfg.LookupLauncherDefaults("Steam"); ok && def.InstallDir != "" {
		if _, err := os.Stat(def.InstallDir); err == nil {
			log.Debug().Msgf("using user-configured Steam directory: %s", def.InstallDir)
			return def.InstallDir
		}
		log.Warn().Msgf("user-configured Steam directory not found: %s", def.InstallDir)
	}

	// Try common Steam installation paths
	home, err := os.UserHomeDir()
	if err != nil {
		log.Warn().Err(err).Msg("failed to get user home directory")
		return opts.FallbackPath
	}

	// Build paths list
	paths := []string{
		filepath.Join(home, ".steam", "steam"),
		filepath.Join(home, ".local", "share", "Steam"),
	}

	// Add extra paths from options
	paths = append(paths, opts.ExtraPaths...)

	// Add Flatpak path if enabled
	if opts.CheckFlatpak {
		paths = append(paths, filepath.Join(
			FlatpakAppPath(FlatpakSteamID),
			".steam", "steam",
		))
	}

	// Add Snap and common system paths
	paths = append(paths,
		filepath.Join(home, "snap", "steam", "common", ".steam", "steam"),
		"/usr/games/steam",
		"/opt/steam",
	)

	for _, path := range paths {
		if _, err := os.Stat(path); err == nil {
			log.Debug().Msgf("found Steam installation: %s", path)
			return path
		}
	}

	log.Debug().Msgf("Steam detection failed, using fallback: %s", opts.FallbackPath)
	return opts.FallbackPath
}
