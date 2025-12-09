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
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/virtualpath"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared"
	"github.com/rs/zerolog/log"
)

const chimeraContentPath = ".local/share/chimera/content"

// Common executable names to look for in Chimera game directories.
var chimeraExecutableNames = []string{
	"start.sh",
	"start",
	"run.sh",
	"launch.sh",
}

// NewChimeraGOGLauncher creates a launcher for GOG games installed via Chimera.
// Uses the universal gog:// scheme with GOG product IDs.
func NewChimeraGOGLauncher() platforms.Launcher {
	return platforms.Launcher{
		ID:       "ChimeraGOG",
		SystemID: systemdefs.SystemPC,
		Schemes:  []string{shared.SchemeGOG},
		Scanner: func(
			_ context.Context,
			_ *config.Instance,
			_ string,
			results []platforms.ScanResult,
		) ([]platforms.ScanResult, error) {
			home, homeErr := os.UserHomeDir()
			if homeErr != nil {
				log.Debug().Err(homeErr).Msg("failed to get user home directory")
				return results, nil
			}

			gogPath := filepath.Join(home, chimeraContentPath, "gog")
			if _, statErr := os.Stat(gogPath); os.IsNotExist(statErr) {
				log.Debug().Msg("Chimera GOG content directory not found")
				return results, nil
			}

			// Scan GOG game directories
			entries, err := os.ReadDir(gogPath)
			if err != nil {
				log.Warn().Err(err).Msg("failed to read Chimera GOG directory")
				return results, nil
			}

			for _, entry := range entries {
				if !entry.IsDir() {
					continue
				}

				gameID := entry.Name()
				gamePath := filepath.Join(gogPath, gameID)

				// Look for game executable
				startScript := findChimeraExecutable(gamePath)
				if startScript != "" {
					results = append(results, platforms.ScanResult{
						Name:  gameID, // TODO: Parse game name from info file if available
						Path:  virtualpath.CreateVirtualPath(shared.SchemeGOG, gameID, gameID),
						NoExt: true,
					})
				}
			}

			log.Debug().Msgf("found %d Chimera GOG games", len(results))
			return results, nil
		},
		Launch: func(_ *config.Instance, path string) (*os.Process, error) {
			// Extract game ID from gog://game_id
			gameID, err := virtualpath.ExtractSchemeID(path, shared.SchemeGOG)
			if err != nil {
				return nil, fmt.Errorf("failed to extract GOG game ID: %w", err)
			}

			// Sanitize gameID to prevent path traversal
			originalGameID := gameID
			gameID = filepath.Base(gameID)
			if gameID == "." || gameID == ".." || gameID == string(filepath.Separator) || gameID != originalGameID {
				return nil, fmt.Errorf("invalid GOG game ID: %s", originalGameID)
			}

			home, err := os.UserHomeDir()
			if err != nil {
				return nil, fmt.Errorf("failed to get home directory: %w", err)
			}

			gamePath := filepath.Join(home, chimeraContentPath, "gog", gameID)
			startScript := findChimeraExecutable(gamePath)

			if startScript == "" {
				return nil, fmt.Errorf("no executable found for GOG game: %s", gameID)
			}

			//nolint:gosec // Path constructed from known base directory
			cmd := exec.CommandContext(context.Background(), startScript)
			if err := cmd.Start(); err != nil {
				return nil, fmt.Errorf("failed to launch GOG game: %w", err)
			}
			return nil, nil
		},
	}
}

// findChimeraExecutable looks for common executable names in a game directory.
func findChimeraExecutable(gamePath string) string {
	for _, name := range chimeraExecutableNames {
		execPath := filepath.Join(gamePath, name)
		if info, err := os.Stat(execPath); err == nil && !info.IsDir() {
			return execPath
		}
	}
	return ""
}
