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

package zapscript

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript/titles"
	"github.com/rs/zerolog/log"
)

// cmdTitle implements the launch.title command for media title-based game launching
//
//nolint:gocritic // single-use parameter in command handler
func cmdTitle(pl platforms.Platform, env platforms.CmdEnv) (platforms.CmdResult, error) {
	if len(env.Cmd.Args) != 1 {
		return platforms.CmdResult{}, ErrArgCount
	}

	query := env.Cmd.Args[0]
	if query == "" {
		return platforms.CmdResult{}, ErrRequiredArgs
	}

	// Validate title format
	valid, systemID, gameName := isValidTitleFormat(query)
	if !valid {
		return platforms.CmdResult{}, fmt.Errorf(
			"invalid title format: %s (expected SystemID/GameName)", query)
	}

	// Validate system ID
	system, err := systemdefs.LookupSystem(systemID)
	if err != nil {
		return platforms.CmdResult{}, fmt.Errorf("failed to lookup system '%s': %w", systemID, err)
	}
	if system == nil {
		return platforms.CmdResult{}, fmt.Errorf("system not found: %s", systemID)
	}

	var args zapscript.LaunchTitleArgs
	if parseErr := ParseAdvArgs(pl, &env, &args); parseErr != nil {
		return platforms.CmdResult{}, fmt.Errorf("invalid advanced arguments: %w", parseErr)
	}

	args.Launcher = applySystemDefaultLauncher(&env, system.ID)
	launch := getLaunchClosure(pl, &env)

	// Collect all launchers for this system to enable file type prioritization
	// during result selection. If user specified an alt launcher explicitly,
	// use only that one. Otherwise, get all launchers for the system.
	var launchersForSystem []platforms.Launcher
	if args.Launcher != "" {
		allLaunchers := pl.Launchers(env.Cfg)
		for i := range allLaunchers {
			if allLaunchers[i].ID == args.Launcher {
				launchersForSystem = []platforms.Launcher{allLaunchers[i]}
				log.Debug().Msgf("using explicitly specified launcher: %s", allLaunchers[i].ID)
				break
			}
		}
		if len(launchersForSystem) == 0 {
			log.Warn().Msgf("explicitly specified launcher not found: %s, using all system launchers",
				args.Launcher)
			launchersForSystem = helpers.GlobalLauncherCache.GetLaunchersBySystem(system.ID)
		}
	} else {
		launchersForSystem = helpers.GlobalLauncherCache.GetLaunchersBySystem(system.ID)
	}

	ctx := context.Background() // TODO: use proper context from env when available

	result, err := titles.ResolveTitle(ctx, &titles.ResolveParams{
		SystemID:       system.ID,
		GameName:       gameName,
		AdditionalTags: args.Tags,
		MediaDB:        env.Database.MediaDB,
		Cfg:            env.Cfg,
		Launchers:      launchersForSystem,
		MediaType:      system.GetMediaType(),
	})
	if err != nil {
		return platforms.CmdResult{}, fmt.Errorf("error resolving title %s/%s: %w", system.ID, gameName, err)
	}

	if result.Confidence < titles.ConfidenceAcceptable {
		log.Warn().Msgf("launching with low confidence (%.2f): %s", result.Confidence, result.Result.Name)
	} else {
		log.Info().Msgf("launching with confidence %.2f: %s", result.Confidence, result.Result.Name)
	}

	return platforms.CmdResult{
		MediaChanged: true,
		Strategy:     result.Strategy,
		Confidence:   result.Confidence,
	}, launch(result.Result.Path)
}

// mightBeTitle checks if input might be a title format for routing purposes in cmdLaunch to cmdTitle.
// Returns false for paths with file extensions, wildcards, or Windows-style backslashes.
func mightBeTitle(input string) bool {
	valid, _, game := isValidTitleFormat(input)
	if !valid {
		return false
	}

	// Reject wildcard patterns which should go to search instead
	if strings.Contains(game, "*") {
		return false
	}

	// Game part should not contain backslashes (Windows file path indicator)
	if strings.Contains(game, "\\") {
		return false
	}

	// Game part should not contain file extensions (path indicator)
	ext := filepath.Ext(game)
	return !helpers.IsValidExtension(ext)
}

// isValidTitleFormat checks if the input string is valid title format for cmdTitle.
// This is a lenient validation - just ensures basic SystemID/GameName format.
// The command itself handles all parsing complexity.
func isValidTitleFormat(input string) (valid bool, systemID, gameName string) {
	// Must contain at least one slash
	if !strings.Contains(input, "/") {
		return false, "", ""
	}

	// Split into system and game parts (only on first slash)
	parts := strings.SplitN(input, "/", 2)
	if len(parts) != 2 {
		return false, "", ""
	}

	system, game := parts[0], parts[1]

	// Both parts must be non-empty
	if system == "" || game == "" {
		return false, "", ""
	}

	return true, system, game
}
