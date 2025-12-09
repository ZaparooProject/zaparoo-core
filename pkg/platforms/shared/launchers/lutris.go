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
	"database/sql"
	"fmt"
	"os"
	"os/exec"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/virtualpath"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared"
	_ "github.com/mattn/go-sqlite3"
	"github.com/rs/zerolog/log"
)

// ScanLutrisGames scans the Lutris pga.db SQLite database for installed games.
func ScanLutrisGames(dbPath string) ([]platforms.ScanResult, error) {
	results := make([]platforms.ScanResult, 0)

	// Check if database exists
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		log.Debug().Msg("Lutris database not found")
		return results, nil
	}

	// Open the SQLite database
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		log.Error().Err(err).Msg("failed to open Lutris database")
		return results, nil
	}
	defer func() {
		if closeErr := db.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close Lutris database")
		}
	}()

	// Query for installed games: name and slug
	query := "SELECT name, slug FROM games WHERE installed = 1"
	rows, err := db.QueryContext(context.Background(), query)
	if err != nil {
		log.Error().Err(err).Msg("failed to query Lutris games")
		return results, nil
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close Lutris query rows")
		}
	}()

	// Scan results
	for rows.Next() {
		var name, slug string
		if err := rows.Scan(&name, &slug); err != nil {
			log.Warn().Err(err).Msg("failed to scan Lutris game row")
			continue
		}

		// Skip games without a slug
		if slug == "" {
			continue
		}

		results = append(results, platforms.ScanResult{
			Name:  name,
			Path:  virtualpath.CreateVirtualPath(shared.SchemeLutris, slug, name),
			NoExt: true,
		})
	}

	// Check for errors during iteration
	if err := rows.Err(); err != nil {
		log.Error().Err(err).Msg("error iterating Lutris game rows")
		return results, nil
	}

	log.Debug().Msgf("found %d Lutris games", len(results))
	return results, nil
}

// NewLutrisLauncher creates a configurable Lutris launcher.
func NewLutrisLauncher(opts LutrisOptions) platforms.Launcher {
	return platforms.Launcher{
		ID:       "Lutris",
		SystemID: systemdefs.SystemPC,
		Schemes:  []string{shared.SchemeLutris},
		Scanner: func(
			_ context.Context,
			_ *config.Instance,
			_ string,
			results []platforms.ScanResult,
		) ([]platforms.ScanResult, error) {
			// Check if Lutris is installed
			_, err := exec.LookPath("lutris")
			if err != nil {
				log.Debug().Err(err).Msg("Lutris not found in PATH, skipping scanner")
				// Not an error condition - just means Lutris isn't installed
				return results, nil
			}

			// Find Lutris database (native or Flatpak)
			lutrisDB, found := FindLutrisDB(opts.CheckFlatpak)
			if !found {
				log.Debug().Msg("Lutris database not found")
				return results, nil
			}

			// Scan Lutris database for installed games
			lutrisResults, err := ScanLutrisGames(lutrisDB)
			if err != nil {
				log.Warn().Err(err).Msg("failed to scan Lutris games, continuing without them")
			} else {
				results = append(results, lutrisResults...)
			}

			return results, nil
		},
		Launch: func(_ *config.Instance, path string) (*os.Process, error) {
			// Extract game slug/id from lutris://game-slug format
			slug, err := virtualpath.ExtractSchemeID(path, shared.SchemeLutris)
			if err != nil {
				return nil, fmt.Errorf("failed to extract Lutris game slug from path: %w", err)
			}

			// Launch via lutris command with rungame action
			cmd := exec.CommandContext( //nolint:gosec // Game slug from internal database
				context.Background(),
				"lutris",
				"lutris:rungame/"+slug,
			)
			err = cmd.Start()
			if err != nil {
				return nil, fmt.Errorf("failed to start Lutris: %w", err)
			}
			return nil, nil
		},
	}
}
