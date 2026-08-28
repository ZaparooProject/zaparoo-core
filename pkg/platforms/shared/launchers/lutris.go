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
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/virtualpath"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared"
	_ "github.com/mattn/go-sqlite3"
	"github.com/rs/zerolog/log"
)

const (
	maxLutrisGames       = 100_000
	maxLutrisFieldLength = 4096
	lutrisQueryTimeout   = 5 * time.Second
)

// ScanLutrisGames scans the Lutris pga.db SQLite database for installed games.
func ScanLutrisGames(dbPath string) ([]platforms.ScanResult, error) {
	results := make([]platforms.ScanResult, 0)
	info, err := os.Stat(dbPath)
	if os.IsNotExist(err) {
		log.Debug().Msg("Lutris database not found")
		return results, nil
	}
	if err != nil {
		return nil, fmt.Errorf("stat Lutris database: %w", err)
	}
	if info.IsDir() {
		return nil, errors.New("lutris database path is a directory")
	}

	dsn := (&url.URL{
		Scheme: "file", Path: dbPath, RawQuery: "mode=ro&_busy_timeout=1000",
	}).String()
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("open Lutris database: %w", err)
	}
	defer func() {
		if closeErr := db.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close Lutris database")
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), lutrisQueryTimeout)
	defer cancel()
	rows, err := db.QueryContext(ctx,
		"SELECT name, slug FROM games WHERE installed = 1 LIMIT ?", maxLutrisGames+1)
	if err != nil {
		return nil, fmt.Errorf("query Lutris games: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close Lutris query rows")
		}
	}()

	for rows.Next() {
		var name, slug string
		if err := rows.Scan(&name, &slug); err != nil {
			return nil, fmt.Errorf("scan Lutris game row: %w", err)
		}
		if len(results) >= maxLutrisGames {
			return nil, errors.New("lutris game library exceeds entry limit")
		}
		name = strings.TrimSpace(name)
		slug = strings.TrimSpace(slug)
		if name == "" || slug == "" || len(name) > maxLutrisFieldLength || len(slug) > maxLutrisFieldLength ||
			virtualpath.ContainsControlChar(name) || virtualpath.ContainsControlChar(slug) {
			continue
		}
		results = append(results, platforms.ScanResult{
			Name: name, Path: virtualpath.CreateVirtualPath(shared.SchemeLutris, slug, name), NoExt: true,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate Lutris game rows: %w", err)
	}

	log.Debug().Int("count", len(results)).Msg("found Lutris games")
	return results, nil
}

// NewLutrisLauncher creates a configurable Lutris launcher.
func NewLutrisLauncher(opts LutrisOptions) platforms.Launcher {
	resolve := newApplicationResolver("lutris", FlatpakLutrisID, applicationResolverOptions{
		lookPath: opts.lookPath, isFlatpakInstalled: opts.isFlatpakInstalled, checkFlatpak: opts.CheckFlatpak,
	})
	buildCommand := func(path string) (*platforms.LaunchCommand, error) {
		slug, err := virtualpath.ExtractSchemeID(path, shared.SchemeLutris)
		if err != nil {
			return nil, fmt.Errorf("extract Lutris game slug: %w", err)
		}
		if slug == "" || len(slug) > maxLutrisFieldLength || virtualpath.ContainsControlChar(slug) {
			return nil, errors.New("invalid Lutris game slug")
		}
		installation, err := resolve()
		if err != nil {
			return nil, err
		}
		return buildApplicationCommand(
			withFlatpakDieWithParent(installation), []string{"lutris:rungame/" + slug}, opts.launchEnv,
		), nil
	}

	return platforms.Launcher{
		ID: "Lutris", SystemID: systemdefs.SystemPC, Schemes: []string{shared.SchemeLutris},
		Lifecycle: platforms.LifecycleBlocking,
		Availability: func(*config.Instance) error {
			_, err := resolve()
			return err
		},
		BuildLaunchCommand: func(
			_ *config.Instance, path string, _ *platforms.LaunchOptions,
		) (*platforms.LaunchCommand, error) {
			return buildCommand(path)
		},
		Scanner: func(
			_ context.Context,
			_ *config.Instance,
			_ string,
			results []platforms.ScanResult,
		) ([]platforms.ScanResult, error) {
			if _, err := resolve(); err != nil {
				return results, nil //nolint:nilerr // An uninstalled optional launcher contributes no media.
			}
			lutrisDB, found := FindLutrisDB(opts.CheckFlatpak)
			if !found {
				return results, nil
			}
			lutrisResults, err := ScanLutrisGames(lutrisDB)
			if err != nil {
				return results, fmt.Errorf("scan Lutris games: %w", err)
			}
			return append(results, lutrisResults...), nil
		},
		Launch: func(_ *config.Instance, path string, _ *platforms.LaunchOptions) (*os.Process, error) {
			command, err := buildCommand(path)
			if err != nil {
				return nil, err
			}
			process, err := startTrackedApplicationCommand(command)
			if err != nil {
				return nil, fmt.Errorf("start Lutris: %w", err)
			}
			return process, nil
		},
	}
}
