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

package zapscript

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/filters"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/installer"
	"github.com/rs/zerolog/log"
)

// parseTagsAdvArg parses comma-delimited tags from advanced args
// Format: "type:value,-type2:value2,~type3:value3"
// Operators: "-" = NOT, "~" = OR, none = AND (default)
// Uses shared parser from pkg/database/filters
func parseTagsAdvArg(tagsStr string) []database.TagFilter {
	if tagsStr == "" {
		return nil
	}

	parts := strings.Split(tagsStr, ",")
	tagFilters, err := filters.ParseTagFilters(parts)
	if err != nil {
		log.Warn().Err(err).Msg("failed to parse tag filters")
		return nil
	}

	return tagFilters
}

//nolint:gocritic // single-use parameter in command handler
func cmdSystem(pl platforms.Platform, env platforms.CmdEnv) (platforms.CmdResult, error) {
	if len(env.Cmd.Args) != 1 {
		return platforms.CmdResult{}, ErrArgCount
	}

	systemID := env.Cmd.Args[0]

	// For menu, use ReturnToMenu() instead of LaunchSystem
	// This ensures proper handling across all platforms (stops active launcher and returns to main menu)
	if strings.EqualFold(systemID, "menu") {
		if err := pl.ReturnToMenu(); err != nil {
			return platforms.CmdResult{
				MediaChanged: true,
			}, fmt.Errorf("failed to return to menu: %w", err)
		}
		return platforms.CmdResult{
			MediaChanged: true,
		}, nil
	}

	if err := pl.LaunchSystem(env.Cfg, systemID); err != nil {
		return platforms.CmdResult{
			MediaChanged: true,
		}, fmt.Errorf("failed to launch system '%s': %w", systemID, err)
	}
	return platforms.CmdResult{
		MediaChanged: true,
	}, nil
}

//nolint:gocritic // single-use parameter in command handler
func cmdRandom(pl platforms.Platform, env platforms.CmdEnv) (platforms.CmdResult, error) {
	if len(env.Cmd.Args) == 0 {
		return platforms.CmdResult{}, ErrArgCount
	}

	query := env.Cmd.Args[0]

	if query == "" {
		return platforms.CmdResult{}, ErrRequiredArgs
	}

	launch, err := getAltLauncher(pl, env)
	if err != nil {
		return platforms.CmdResult{}, err
	}

	// Parse tags from advanced args
	tagFilters := parseTagsAdvArg(env.Cmd.AdvArgs["tags"])

	gamesdb := env.Database.MediaDB

	if strings.EqualFold(query, "all") {
		allSystems := systemdefs.AllSystems()
		systemIDs := make([]string, len(allSystems))
		for i, sys := range allSystems {
			systemIDs[i] = sys.ID
		}
		mediaQuery := database.MediaQuery{
			Systems: systemIDs,
			Tags:    tagFilters,
		}
		game, gameErr := gamesdb.RandomGameWithQuery(&mediaQuery)
		if gameErr != nil {
			return platforms.CmdResult{}, fmt.Errorf("failed to get random game: %w", gameErr)
		}

		if launchErr := launch(game.Path); launchErr != nil {
			return platforms.CmdResult{
				MediaChanged: true,
			}, fmt.Errorf("failed to launch random game: %w", launchErr)
		}
		return platforms.CmdResult{
			MediaChanged: true,
		}, nil
	}

	// absolute path, use database query to find random media with this path prefix
	// this includes virtual paths and zips as options
	if filepath.IsAbs(query) {
		mediaQuery := database.MediaQuery{
			PathPrefix: query,
			Tags:       tagFilters,
		}
		searchResult, searchErr := gamesdb.RandomGameWithQuery(&mediaQuery)
		if searchErr != nil {
			return platforms.CmdResult{}, fmt.Errorf("failed to find random media for path '%s': %w", query, searchErr)
		}

		if launchErr := launch(searchResult.Path); launchErr != nil {
			return platforms.CmdResult{
				MediaChanged: true,
			}, fmt.Errorf("failed to launch file '%s': %w", searchResult.Path, launchErr)
		}
		return platforms.CmdResult{
			MediaChanged: true,
		}, nil
	}

	// perform a search similar to launch.search and pick randomly
	// looking for <system>/<query> format
	// TODO: use parser for launch command
	ps := strings.SplitN(query, "/", 2)
	if len(ps) == 2 {
		systemID, query := ps[0], ps[1]

		var systems []systemdefs.System
		if strings.EqualFold(systemID, "all") {
			systems = systemdefs.AllSystems()
		} else {
			system, lookupErr := systemdefs.LookupSystem(systemID)
			if lookupErr != nil {
				return platforms.CmdResult{}, fmt.Errorf("failed to lookup system '%s': %w", systemID, lookupErr)
			} else if system == nil {
				return platforms.CmdResult{}, fmt.Errorf("system not found: %s", systemID)
			}
			systems = []systemdefs.System{*system}
		}

		// Handle the special case of /* pattern - use RandomGameWithQuery
		if query == "*" {
			systemIDs := make([]string, len(systems))
			for i, sys := range systems {
				systemIDs[i] = sys.ID
			}
			mediaQuery := database.MediaQuery{
				Systems: systemIDs,
				Tags:    tagFilters,
			}
			game, randomErr := gamesdb.RandomGameWithQuery(&mediaQuery)
			if randomErr != nil {
				return platforms.CmdResult{}, fmt.Errorf("failed to get random game: %w", randomErr)
			}

			if launchErr := launch(game.Path); launchErr != nil {
				return platforms.CmdResult{
					MediaChanged: true,
				}, fmt.Errorf("failed to launch random game '%s': %w", game.Path, launchErr)
			}
			return platforms.CmdResult{
				MediaChanged: true,
			}, nil
		}

		systemIDs := make([]string, len(systems))
		for i, sys := range systems {
			systemIDs[i] = sys.ID
		}
		mediaQuery := database.MediaQuery{
			Systems:  systemIDs,
			PathGlob: query,
			Tags:     tagFilters,
		}
		game, randomErr := gamesdb.RandomGameWithQuery(&mediaQuery)
		if randomErr != nil {
			return platforms.CmdResult{}, fmt.Errorf("failed to get random game matching '%s': %w", query, randomErr)
		}

		if launchErr := launch(game.Path); launchErr != nil {
			return platforms.CmdResult{
				MediaChanged: true,
			}, fmt.Errorf("failed to launch game '%s': %w", game.Path, launchErr)
		}
		return platforms.CmdResult{
			MediaChanged: true,
		}, nil
	}

	// assume given a list of system ids
	systems := make([]systemdefs.System, 0, len(env.Cmd.Args))

	for _, id := range env.Cmd.Args {
		system, lookupErr := systemdefs.LookupSystem(id)
		if lookupErr != nil {
			log.Error().Err(lookupErr).Msgf("error looking up system: %s", id)
			continue
		}

		systems = append(systems, *system)
	}

	systemIDs := make([]string, len(systems))
	for i, sys := range systems {
		systemIDs[i] = sys.ID
	}
	mediaQuery := database.MediaQuery{
		Systems: systemIDs,
		Tags:    tagFilters,
	}
	game, err := gamesdb.RandomGameWithQuery(&mediaQuery)
	if err != nil {
		return platforms.CmdResult{}, fmt.Errorf("failed to get random game: %w", err)
	}

	if err := launch(game.Path); err != nil {
		return platforms.CmdResult{
			MediaChanged: true,
		}, fmt.Errorf("failed to launch random game '%s': %w", game.Path, err)
	}
	return platforms.CmdResult{
		MediaChanged: true,
	}, nil
}

func getAltLauncher(
	pl platforms.Platform,
	env platforms.CmdEnv, //nolint:gocritic // single-use parameter in command handler
) (func(args string) error, error) {
	if env.Cmd.AdvArgs["launcher"] != "" {
		var launcher platforms.Launcher

		launchers := pl.Launchers(env.Cfg)
		for i := range launchers {
			if launchers[i].ID == env.Cmd.AdvArgs["launcher"] {
				launcher = launchers[i]
				break
			}
		}

		if launcher.Launch == nil {
			return nil, fmt.Errorf("alt launcher not found: %s", env.Cmd.AdvArgs["launcher"])
		}

		log.Info().Msgf("launching with alt launcher: %s", env.Cmd.AdvArgs["launcher"])

		return func(args string) error {
			// Pass the specific launcher - DoLaunch handles lifecycle
			return pl.LaunchMedia(env.Cfg, args, &launcher, env.Database)
		}, nil
	}
	// Normal path - pass nil for auto-detection
	return func(args string) error {
		return pl.LaunchMedia(env.Cfg, args, nil, env.Database)
	}, nil
}

func isValidRemoteFileURL(s string) (func(installer.DownloaderArgs) error, bool) {
	u, err := url.Parse(s)
	if err != nil {
		return nil, false
	}

	if u.Scheme == "" || u.Host == "" {
		return nil, false
	}

	if strings.EqualFold(u.Scheme, "http") || strings.EqualFold(u.Scheme, "https") {
		return installer.DownloadHTTPFile, true
	} else if strings.EqualFold(u.Scheme, "smb") {
		return installer.DownloadSMBFile, true
	}

	return nil, false
}

//nolint:gocritic // single-use parameter in command handler
func cmdLaunch(pl platforms.Platform, env platforms.CmdEnv) (platforms.CmdResult, error) {
	if len(env.Cmd.Args) == 0 {
		return platforms.CmdResult{}, ErrArgCount
	}

	path := env.Cmd.Args[0]
	if path == "" {
		return platforms.CmdResult{}, ErrRequiredArgs
	}

	systemArg := env.Cmd.AdvArgs["system"]
	if dler, ok := isValidRemoteFileURL(path); ok && systemArg != "" {
		name := env.Cmd.AdvArgs["name"]
		preNotice := env.Cmd.AdvArgs["pre_notice"]
		installPath, err := installer.InstallRemoteFile(
			env.Cfg, pl,
			path,
			systemArg,
			preNotice,
			name,
			dler,
		)
		if err != nil {
			return platforms.CmdResult{}, fmt.Errorf("failed to install remote file: %w", err)
		}
		path = installPath
	}

	// Apply system defaults from system advanced arg if no explicit launcher specified
	if env.Cmd.AdvArgs["launcher"] == "" && env.Cmd.AdvArgs["system"] != "" {
		systemID := env.Cmd.AdvArgs["system"]
		system, lookupErr := systemdefs.LookupSystem(systemID)
		if lookupErr != nil {
			log.Warn().Err(lookupErr).Str("system", systemID).
				Msg("system arg provided but lookup failed - falling back to auto-detection")
		} else {
			if systemDefaults, ok := env.Cfg.LookupSystemDefaults(system.ID); ok && systemDefaults.Launcher != "" {
				log.Debug().Str("system", system.ID).Str("launcher", systemDefaults.Launcher).
					Msg("applying system default launcher from system arg")
				env.Cmd.AdvArgs["launcher"] = systemDefaults.Launcher
			}
		}
	}

	launch, err := getAltLauncher(pl, env)
	if err != nil {
		return platforms.CmdResult{}, err
	}

	// if it's an absolute path, just try launch it
	if filepath.IsAbs(path) {
		log.Debug().Msgf("launching absolute path: %s", path)
		return platforms.CmdResult{
			MediaChanged: true,
		}, launch(path)
	}

	// match for uri style launch syntax
	if helpers.ReURI.MatchString(path) {
		log.Debug().Msgf("launching uri: %s", path)
		return platforms.CmdResult{
			MediaChanged: true,
		}, launch(path)
	}

	// for relative paths, perform a basic check if the file exists in a games folder
	// this always takes precedence over the system/path format (but is not totally cross platform)
	var findErr error
	var p string
	if p, findErr = findFile(pl, env.Cfg, path); findErr == nil {
		log.Debug().Msgf("launching found relative path: %s", p)
		return platforms.CmdResult{
			MediaChanged: true,
		}, launch(p)
	}
	log.Debug().Err(findErr).Msgf("error finding file: %s", path)

	// check for title launch format: SystemID/Game Name
	if mightBeTitle(path) {
		log.Debug().Msgf("detected possible title format, forwarding to cmdTitle: %s", path)
		return cmdTitle(pl, env)
	}

	// attempt to parse the <system>/<path> format
	ps := strings.SplitN(path, "/", 2)
	if len(ps) < 2 {
		return platforms.CmdResult{}, fmt.Errorf("invalid launch format: %s", path)
	}

	systemID, lookupPath := ps[0], ps[1]

	system, err := systemdefs.LookupSystem(systemID)
	if err != nil {
		return platforms.CmdResult{}, fmt.Errorf("failed to lookup system '%s': %w", systemID, err)
	}

	// Check system defaults for launcher if not already specified
	if env.Cmd.AdvArgs["launcher"] == "" {
		if systemDefaults, ok := env.Cfg.LookupSystemDefaults(system.ID); ok && systemDefaults.Launcher != "" {
			log.Info().Msgf("using system default launcher for %s: %s", system.ID, systemDefaults.Launcher)
			if env.Cmd.AdvArgs == nil {
				env.Cmd.AdvArgs = make(map[string]string)
			}
			env.Cmd.AdvArgs["launcher"] = systemDefaults.Launcher
		}
	}

	log.Info().Msgf("launching system: %s, path: %s", systemID, lookupPath)

	var launchers []platforms.Launcher
	allLaunchers := pl.Launchers(env.Cfg)
	for i := range allLaunchers {
		if allLaunchers[i].SystemID == system.ID {
			launchers = append(launchers, allLaunchers[i])
		}
	}

	// Also collect launchers from fallback systems
	for _, fallbackID := range system.Fallbacks {
		for i := range allLaunchers {
			if allLaunchers[i].SystemID == fallbackID {
				launchers = append(launchers, allLaunchers[i])
			}
		}
	}

	var folders []string
	for i := range launchers {
		for _, folder := range launchers[i].Folders {
			if !helpers.Contains(folders, folder) {
				folders = append(folders, folder)
			}
		}
	}

	for _, f := range folders {
		systemPath := filepath.Join(f, lookupPath)
		log.Debug().Msgf("checking system path: %s", systemPath)
		var systemFindErr error
		var fp string
		if fp, systemFindErr = findFile(pl, env.Cfg, systemPath); systemFindErr == nil {
			log.Debug().Msgf("launching found system path: %s", fp)
			return platforms.CmdResult{
				MediaChanged: true,
			}, launch(fp)
		}
		log.Debug().Err(systemFindErr).Msgf("error finding system file: %s", lookupPath)
	}

	gamesdb := env.Database.MediaDB

	// search if the path contains no / or file extensions
	if !strings.Contains(lookupPath, "/") && filepath.Ext(lookupPath) == "" {
		if strings.Contains(lookupPath, "*") {
			// treat as a search
			// TODO: passthrough advanced args
			return cmdSearch(pl, env)
		}
		log.Info().Msgf("searching in %s: %s", system.ID, lookupPath)
		// treat as a direct title launch
		res, err := gamesdb.SearchMediaPathExact(
			[]systemdefs.System{*system},
			lookupPath,
		)

		if err != nil {
			return platforms.CmdResult{}, fmt.Errorf("failed to search for exact path '%s': %w", lookupPath, err)
		} else if len(res) == 0 {
			return platforms.CmdResult{}, fmt.Errorf("no results found for: %s", lookupPath)
		}

		log.Info().Msgf("found result: %s", res[0].Path)

		game := res[0]
		return platforms.CmdResult{
			MediaChanged: true,
		}, launch(game.Path)
	}

	return platforms.CmdResult{}, fmt.Errorf("file not found: %s", path)
}

//nolint:gocritic // single-use parameter in command handler
func cmdSearch(pl platforms.Platform, env platforms.CmdEnv) (platforms.CmdResult, error) {
	if len(env.Cmd.Args) == 0 {
		return platforms.CmdResult{}, ErrArgCount
	}

	query := env.Cmd.Args[0]

	if query == "" {
		return platforms.CmdResult{}, ErrRequiredArgs
	}

	launch, err := getAltLauncher(pl, env)
	if err != nil {
		return platforms.CmdResult{}, err
	}

	// Parse tags from advanced args
	tagFilters := parseTagsAdvArg(env.Cmd.AdvArgs["tags"])

	query = strings.ToLower(query)
	query = strings.TrimSpace(query)

	gamesdb := env.Database.MediaDB

	if !strings.Contains(query, "/") {
		// search all systems
		searchFilters := database.SearchFilters{
			Systems: systemdefs.AllSystems(),
			Query:   query,
			Tags:    tagFilters,
			Limit:   1,
		}
		// TODO: context should come from service state
		res, searchErr := gamesdb.SearchMediaWithFilters(context.Background(), &searchFilters)
		if searchErr != nil {
			return platforms.CmdResult{}, fmt.Errorf("failed to search all systems for '%s': %w", query, searchErr)
		}

		if len(res) == 0 {
			return platforms.CmdResult{}, fmt.Errorf("no results found for: %s", query)
		}

		return platforms.CmdResult{
			MediaChanged: true,
		}, launch(res[0].Path)
	}

	ps := strings.SplitN(query, "/", 2)
	if len(ps) < 2 {
		return platforms.CmdResult{}, fmt.Errorf("invalid search format: %s", query)
	}

	systemID, query := ps[0], ps[1]

	if query == "" {
		return platforms.CmdResult{}, errors.New("no query specified")
	}

	systems := make([]systemdefs.System, 0)

	if strings.EqualFold(systemID, "all") {
		systems = systemdefs.AllSystems()
	} else {
		system, lookupErr := systemdefs.LookupSystem(systemID)
		if lookupErr != nil {
			return platforms.CmdResult{}, fmt.Errorf("failed to lookup system '%s': %w", systemID, lookupErr)
		}

		systems = append(systems, *system)
	}

	searchFilters := database.SearchFilters{
		Systems: systems,
		Query:   query,
		Tags:    tagFilters,
		Limit:   1,
	}
	// TODO: context should come from service state
	res, searchErr := gamesdb.SearchMediaWithFilters(context.Background(), &searchFilters)
	if searchErr != nil {
		return platforms.CmdResult{}, fmt.Errorf("failed to search systems for '%s': %w", query, searchErr)
	}

	if len(res) == 0 {
		return platforms.CmdResult{}, fmt.Errorf("no results found for: %s", query)
	}

	return platforms.CmdResult{
		MediaChanged: true,
	}, launch(res[0].Path)
}
