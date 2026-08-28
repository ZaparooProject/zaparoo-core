//go:build linux

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package launchers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

const (
	maxFaugusLibrarySize = 16 << 20
	maxFaugusGames       = 100_000
	maxFaugusFieldLength = 4096
)

type faugusGame struct {
	GameID string `json:"gameid"`
	Title  string `json:"title"`
}

type faugusOptions struct {
	resolver  applicationResolverOptions
	launchEnv func() []string
	homeDir   string
}

// NewFaugusLauncher creates a launcher for Faugus Launcher games.
func NewFaugusLauncher() platforms.Launcher {
	home, _ := os.UserHomeDir()
	return buildFaugusLauncher(faugusOptions{
		homeDir:  home,
		resolver: applicationResolverOptions{checkFlatpak: true},
	})
}

func buildFaugusLauncher(options faugusOptions) platforms.Launcher {
	resolve := newApplicationResolver("faugus-launcher", FlatpakFaugusID, options.resolver)
	buildCommand := func(path string) (*platforms.LaunchCommand, error) {
		gameID, err := virtualpath.ExtractSchemeID(path, shared.SchemeFaugus)
		if err != nil {
			return nil, fmt.Errorf("extract Faugus game ID: %w", err)
		}
		if !validApplicationField(gameID, maxFaugusFieldLength) {
			return nil, errors.New("invalid Faugus game ID")
		}
		installation, err := resolve()
		if err != nil {
			return nil, err
		}
		return buildApplicationCommand(
			withFlatpakDieWithParent(installation), []string{"--game", gameID}, options.launchEnv,
		), nil
	}

	return platforms.Launcher{
		ID: "Faugus", SystemID: systemdefs.SystemPC, Schemes: []string{shared.SchemeFaugus},
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
			installation, err := resolve()
			if err != nil {
				return results, nil //nolint:nilerr // Optional integration contributes no media when absent.
			}
			libraryPath, found := findFaugusLibrary(options.homeDir, installation.flatpak)
			if !found {
				return results, nil
			}
			games, err := scanFaugusGames(libraryPath)
			if err != nil {
				log.Warn().Err(err).Str("path", libraryPath).Msg("failed to scan Faugus games")
				return results, nil //nolint:nilerr // Malformed optional metadata must not abort indexing.
			}
			return append(results, games...), nil
		},
		Launch: func(_ *config.Instance, path string, _ *platforms.LaunchOptions) (*os.Process, error) {
			command, err := buildCommand(path)
			if err != nil {
				return nil, err
			}
			process, err := startTrackedApplicationCommand(command)
			if err != nil {
				return nil, fmt.Errorf("start Faugus game: %w", err)
			}
			return process, nil
		},
	}
}

func findFaugusLibrary(homeDir string, flatpak bool) (string, bool) {
	if homeDir == "" {
		return "", false
	}
	path := filepath.Join(homeDir, ".local", "share", "faugus-launcher", "games.json")
	if flatpak {
		path = filepath.Join(
			homeDir, ".var", "app", FlatpakFaugusID, "data", "faugus-launcher", "games.json",
		)
	}
	info, err := os.Stat(path)
	return path, err == nil && info.Mode().IsRegular()
}

func scanFaugusGames(path string) ([]platforms.ScanResult, error) {
	data, err := readLimitedApplicationFile(path, maxFaugusLibrarySize)
	if err != nil {
		return nil, err
	}
	var games []faugusGame
	if err := json.Unmarshal(data, &games); err != nil {
		return nil, fmt.Errorf("parse Faugus games: %w", err)
	}
	if len(games) > maxFaugusGames {
		return nil, errors.New("faugus game library exceeds entry limit")
	}
	results := make([]platforms.ScanResult, 0, len(games))
	for _, game := range games {
		gameID := strings.TrimSpace(game.GameID)
		title := strings.TrimSpace(game.Title)
		if !validApplicationField(gameID, maxFaugusFieldLength) ||
			!validApplicationField(title, maxFaugusFieldLength) {
			continue
		}
		results = append(results, platforms.ScanResult{
			Name: title, Path: virtualpath.CreateVirtualPath(shared.SchemeFaugus, gameID, title), NoExt: true,
		})
	}
	return results, nil
}

func validApplicationField(value string, maxLength int) bool {
	return value != "" && len(value) <= maxLength && !strings.HasPrefix(value, "-") &&
		!virtualpath.ContainsControlChar(value)
}
