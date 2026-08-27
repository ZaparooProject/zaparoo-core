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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/virtualpath"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared"
	"github.com/rs/zerolog/log"
)

// heroicGameInfo represents a game entry in Heroic's library JSON files.
type heroicGameInfo struct {
	AppName     string `json:"app_name"` //nolint:tagliatelle // External JSON format from Heroic
	Title       string `json:"title"`
	Runner      string `json:"runner"`
	IsInstalled bool   `json:"is_installed"` //nolint:tagliatelle // External JSON format from Heroic
}

const (
	maxHeroicLibrarySize = 16 << 20
	maxHeroicGames       = 100_000
	maxHeroicFieldLength = 4096

	// Run Heroic's supported no-GUI entrypoint as the Flatpak command so its
	// lifetime represents this launch, never an unrelated resident Heroic GUI.
	heroicFlatpakMonitor = `exec /app/bin/heroic-run --no-gui "$1"`
)

// ScanHeroicGames scans Heroic Games Launcher library files for installed games.
func ScanHeroicGames(storeCacheDir string) ([]platforms.ScanResult, error) {
	info, err := os.Stat(storeCacheDir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("stat Heroic store cache: %w", err)
	}
	if !info.IsDir() {
		return nil, errors.New("heroic store cache path is not a directory")
	}

	libraries := []struct {
		path    string
		jsonKey string
		runner  string
		label   string
	}{
		{
			path:    filepath.Join(storeCacheDir, "legendary_library.json"),
			jsonKey: "library", runner: "legendary", label: "legendary_library.json",
		},
		{
			path:    filepath.Join(storeCacheDir, "gog_library.json"),
			jsonKey: "games", runner: "gog", label: "gog_library.json",
		},
		{
			path:    filepath.Join(storeCacheDir, "nile_library.json"),
			jsonKey: "library", runner: "nile", label: "nile_library.json",
		},
		{
			path:    filepath.Join(filepath.Dir(storeCacheDir), "sideload_apps", "library.json"),
			jsonKey: "games", runner: "sideload", label: "sideload_apps/library.json",
		},
	}
	results := make([]platforms.ScanResult, 0)
	for _, library := range libraries {
		items, scanErr := scanHeroicLibraryFile(library.path, library.jsonKey, library.runner)
		if scanErr != nil {
			log.Warn().Err(scanErr).Str("file", library.label).Msg("failed to scan Heroic library")
			continue
		}
		if len(results)+len(items) > maxHeroicGames {
			return nil, errors.New("heroic game library exceeds entry limit")
		}
		results = append(results, items...)
	}
	log.Debug().Int("count", len(results)).Msg("found Heroic games")
	return results, nil
}

func scanHeroicLibraryFile(filePath, jsonKey string, defaultRunners ...string) ([]platforms.ScanResult, error) {
	defaultRunner := ""
	if len(defaultRunners) > 0 {
		defaultRunner = defaultRunners[0]
	}
	//nolint:gosec // filePath is constructed from a known Heroic cache directory.
	file, err := os.Open(filePath)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open Heroic library file: %w", err)
	}
	defer file.Close() //nolint:errcheck // Read-only file.
	data, err := io.ReadAll(io.LimitReader(file, maxHeroicLibrarySize+1))
	if err != nil {
		return nil, fmt.Errorf("read Heroic library file: %w", err)
	}
	if len(data) > maxHeroicLibrarySize {
		return nil, errors.New("heroic library file exceeds size limit")
	}

	var libraryData map[string]json.RawMessage
	if err := json.Unmarshal(data, &libraryData); err != nil {
		return nil, fmt.Errorf("failed to parse Heroic library JSON: %w", err)
	}
	rawGames, ok := libraryData[jsonKey]
	if !ok {
		return nil, nil
	}
	var games []heroicGameInfo
	if err := json.Unmarshal(rawGames, &games); err != nil {
		return nil, fmt.Errorf("failed to parse Heroic game entries: %w", err)
	}
	if len(games) > maxHeroicGames {
		return nil, errors.New("heroic game library exceeds entry limit")
	}

	results := make([]platforms.ScanResult, 0, len(games))
	for _, game := range games {
		if !game.IsInstalled {
			continue
		}
		appName := strings.TrimSpace(game.AppName)
		title := strings.TrimSpace(game.Title)
		runner := strings.ToLower(strings.TrimSpace(game.Runner))
		if runner == "" {
			runner = defaultRunner
		}
		if !validHeroicRunner(runner) || appName == "" || len(appName) > maxHeroicFieldLength ||
			len(title) > maxHeroicFieldLength || virtualpath.ContainsControlChar(appName) ||
			virtualpath.ContainsControlChar(title) ||
			(runner == "gog" && strings.EqualFold(appName, "gog-redist")) {
			continue
		}
		results = append(results, platforms.ScanResult{
			Name: title, Path: virtualpath.CreateVirtualPath(
				shared.SchemeHeroic, runner+":"+appName, title,
			), NoExt: true,
		})
	}
	return results, nil
}

func validHeroicRunner(runner string) bool {
	switch runner {
	case "legendary", "gog", "nile", "sideload":
		return true
	default:
		return false
	}
}

// NewHeroicLauncher creates a configurable Heroic Games Launcher.
func NewHeroicLauncher(opts HeroicOptions) platforms.Launcher {
	resolve := newApplicationResolver("heroic", FlatpakHeroicID, applicationResolverOptions{
		lookPath: opts.lookPath, isFlatpakInstalled: opts.isFlatpakInstalled, checkFlatpak: opts.CheckFlatpak,
	})
	buildCommand := func(path string) (*platforms.LaunchCommand, error) {
		id, err := virtualpath.ExtractSchemeID(path, shared.SchemeHeroic)
		if err != nil {
			return nil, fmt.Errorf("extract Heroic game ID: %w", err)
		}
		runner := ""
		appName := id
		if candidate, value, found := strings.Cut(id, ":"); found && validHeroicRunner(candidate) {
			runner = candidate
			appName = value
		}
		if appName == "" || len(appName) > maxHeroicFieldLength || virtualpath.ContainsControlChar(appName) {
			return nil, errors.New("invalid Heroic game ID")
		}
		query := url.Values{"appName": []string{appName}, "gui": []string{"false"}}
		if runner != "" {
			query.Set("runner", runner)
		}
		launchURL := (&url.URL{Scheme: "heroic", Host: "launch", RawQuery: query.Encode()}).String()
		installation, err := resolve()
		if err != nil {
			return nil, err
		}
		if installation.flatpak {
			installation = withFlatpakDieWithParent(installation)
			installation.argsPrefix = []string{
				"run", "--die-with-parent", "--command=sh", FlatpakHeroicID,
			}
			return buildApplicationCommand(
				installation, []string{"-c", heroicFlatpakMonitor, "zaparoo-heroic", launchURL}, opts.launchEnv,
			), nil
		}
		return buildApplicationCommand(installation, []string{"--no-gui", launchURL}, opts.launchEnv), nil
	}

	return platforms.Launcher{
		ID: "Heroic", SystemID: systemdefs.SystemPC, Schemes: []string{shared.SchemeHeroic},
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
			storeCacheDir, found := FindHeroicStoreCache(opts.CheckFlatpak)
			if !found {
				return results, nil
			}
			heroicResults, err := ScanHeroicGames(storeCacheDir)
			if err != nil {
				return results, fmt.Errorf("scan Heroic games: %w", err)
			}
			return append(results, heroicResults...), nil
		},
		Launch: func(_ *config.Instance, path string, _ *platforms.LaunchOptions) (*os.Process, error) {
			command, err := buildCommand(path)
			if err != nil {
				return nil, err
			}
			process, err := startTrackedApplicationCommand(command)
			if err != nil {
				return nil, fmt.Errorf("start Heroic Games Launcher: %w", err)
			}
			return process, nil
		},
	}
}
