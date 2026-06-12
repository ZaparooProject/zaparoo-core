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
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mediaslot"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/installer"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
)

func applySystemDefaultLauncher(pl platforms.Platform, env *platforms.CmdEnv, systemID string) string {
	current := env.Cmd.AdvArgs.Get(zapscript.KeyLauncher)
	if current != "" {
		return current
	}

	defaults, ok := env.Cfg.LookupSystemDefaults(systemID)
	if !ok || defaults.Launcher == "" {
		return ""
	}

	launcherID, found := resolveLauncherRefForSystem(pl, env, defaults.Launcher, systemID)
	if !found {
		log.Warn().
			Str("system", systemID).
			Str("launcher", defaults.Launcher).
			Msg("system default launcher not found")
		return ""
	}

	log.Info().
		Str("system", systemID).
		Str("launcher", launcherID).
		Str("ref", defaults.Launcher).
		Msg("using system default launcher")
	env.Cmd.AdvArgs = env.Cmd.AdvArgs.With(zapscript.KeyLauncher, launcherID)
	return launcherID
}

func resolveLauncherRefForSystem(
	pl platforms.Platform,
	env *platforms.CmdEnv,
	ref string,
	systemID string,
) (string, bool) {
	launchers := pl.Launchers(env.Cfg)
	for i := range launchers {
		if strings.EqualFold(launchers[i].SystemID, systemID) && strings.EqualFold(launchers[i].ID, ref) {
			return launchers[i].ID, true
		}
	}

	for i := range launchers {
		if !strings.EqualFold(launchers[i].SystemID, systemID) {
			continue
		}
		for _, group := range launchers[i].Groups {
			if strings.EqualFold(group, ref) {
				return launchers[i].ID, true
			}
		}
	}

	for i := range launchers {
		if !strings.EqualFold(launchers[i].ID, ref) {
			continue
		}
		if launchers[i].SystemID == "" || strings.EqualFold(launchers[i].SystemID, systemID) {
			return launchers[i].ID, true
		}
	}

	return "", false
}

func applySystemDefaultLauncherForPath(pl platforms.Platform, env *platforms.CmdEnv, path string) string {
	if current := env.Cmd.AdvArgs.Get(zapscript.KeyLauncher); current != "" {
		return current
	}

	launcher, found := inferLauncherForPath(pl, env, path)
	if !found || launcher.SystemID == "" {
		log.Debug().Str("path", path).Msg("could not infer system default launcher from path")
		return ""
	}
	return applySystemDefaultLauncher(pl, env, launcher.SystemID)
}

func inferLauncherForPath(pl platforms.Platform, env *platforms.CmdEnv, path string) (platforms.Launcher, bool) {
	launchers := pl.Launchers(env.Cfg)
	best := -1
	bestScore := -1
	for i := range launchers {
		if !helpers.PathIsLauncher(env.Cfg, pl, &launchers[i], path) {
			continue
		}
		score := launcherInferenceScore(&launchers[i])
		if score > bestScore {
			best = i
			bestScore = score
		}
	}
	if best == -1 {
		return platforms.Launcher{}, false
	}
	return launchers[best], true
}

func launcherInferenceScore(l *platforms.Launcher) int {
	score := 0
	if len(l.Schemes) > 0 {
		score += 1000
	}
	if len(l.Folders) > 0 {
		score += 100
	}
	if l.SystemID != "" {
		score += 10
	}
	return score
}

//nolint:gocritic // single-use parameter in command handler
func cmdSystem(pl platforms.Platform, env platforms.CmdEnv) (platforms.CmdResult, error) {
	if len(env.Cmd.Args) != 1 {
		return platforms.CmdResult{}, ErrArgCount
	}

	systemID := env.Cmd.Args[0]

	// For menu, use ReturnToMenu() instead of LaunchSystem
	// This ensures proper handling across all platforms (stops active launcher and returns to main menu)
	// TODO: move "menu" to a const somewhere else
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

	var args zapscript.LaunchRandomArgs
	if err := ParseAdvArgs(pl, &env, &args); err != nil {
		return platforms.CmdResult{}, fmt.Errorf("invalid advanced arguments: %w", err)
	}

	launch := getLaunchClosure(pl, &env)
	tagFilters := args.Tags

	gamesdb := env.Database.MediaDB
	ctx, cancel := mediaDBLookupContext(&env)
	defer cancel()

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
		game, gameErr := gamesdb.RandomGameWithQuery(ctx, &mediaQuery)
		if gameErr != nil {
			return platforms.CmdResult{}, fmt.Errorf("failed to get random game: %w", gameErr)
		}

		applySystemDefaultLauncher(pl, &env, game.SystemID)
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
		cleanedPath := filepath.Clean(query)
		mediaQuery := database.MediaQuery{
			PathPrefix: filepath.ToSlash(cleanedPath),
			Tags:       tagFilters,
		}
		searchResult, searchErr := gamesdb.RandomGameWithQuery(ctx, &mediaQuery)
		if errors.Is(searchErr, sql.ErrNoRows) {
			// Fallback: pick random file directly from disk for unindexed paths
			entries, readErr := os.ReadDir(cleanedPath)
			if readErr != nil {
				return platforms.CmdResult{}, fmt.Errorf("failed to read path '%s': %w", cleanedPath, readErr)
			}
			var files []string
			for _, e := range entries {
				if !e.IsDir() {
					files = append(files, filepath.Join(cleanedPath, e.Name()))
				}
			}
			if len(files) == 0 {
				return platforms.CmdResult{}, fmt.Errorf("no files found in: %s", cleanedPath)
			}
			file, randomErr := helpers.RandomElem(files)
			if randomErr != nil {
				return platforms.CmdResult{}, fmt.Errorf("failed to select random file: %w", randomErr)
			}
			applySystemDefaultLauncherForPath(pl, &env, file)
			if launchErr := launch(file); launchErr != nil {
				return platforms.CmdResult{
					MediaChanged: true,
				}, fmt.Errorf("failed to launch file '%s': %w", file, launchErr)
			}
			return platforms.CmdResult{
				MediaChanged: true,
			}, nil
		} else if searchErr != nil {
			return platforms.CmdResult{}, fmt.Errorf("failed to find random media for path '%s': %w", query, searchErr)
		}

		applySystemDefaultLauncher(pl, &env, searchResult.SystemID)
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
		systemID, searchQuery := ps[0], ps[1]

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
			systems = systemdefs.SystemsWithFallbacks([]systemdefs.System{*system})
		}

		// Handle the special case of /* pattern - use RandomGameWithQuery
		if searchQuery == "*" {
			systemIDs := make([]string, len(systems))
			for i, sys := range systems {
				systemIDs[i] = sys.ID
			}
			mediaQuery := database.MediaQuery{
				Systems: systemIDs,
				Tags:    tagFilters,
			}
			game, randomErr := gamesdb.RandomGameWithQuery(ctx, &mediaQuery)
			if randomErr != nil {
				return platforms.CmdResult{}, fmt.Errorf("failed to get random game: %w", randomErr)
			}

			applySystemDefaultLauncher(pl, &env, game.SystemID)
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
			PathGlob: searchQuery,
			Tags:     tagFilters,
		}
		game, randomErr := gamesdb.RandomGameWithQuery(ctx, &mediaQuery)
		if randomErr != nil {
			return platforms.CmdResult{},
				fmt.Errorf("failed to get random game matching '%s': %w", searchQuery, randomErr)
		}

		applySystemDefaultLauncher(pl, &env, game.SystemID)
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

	systems = systemdefs.SystemsWithFallbacks(systems)

	systemIDs := make([]string, len(systems))
	for i, sys := range systems {
		systemIDs[i] = sys.ID
	}
	mediaQuery := database.MediaQuery{
		Systems: systemIDs,
		Tags:    tagFilters,
	}
	game, err := gamesdb.RandomGameWithQuery(ctx, &mediaQuery)
	if err != nil {
		return platforms.CmdResult{}, fmt.Errorf("failed to get random game: %w", err)
	}

	applySystemDefaultLauncher(pl, &env, game.SystemID)
	if err := launch(game.Path); err != nil {
		return platforms.CmdResult{
			MediaChanged: true,
		}, fmt.Errorf("failed to launch random game '%s': %w", game.Path, err)
	}
	return platforms.CmdResult{
		MediaChanged: true,
	}, nil
}

func findLauncher(pl platforms.Platform, cfg *platforms.CmdEnv, launcherID string) *platforms.Launcher {
	if launcherID == "" {
		return nil
	}
	launchers := pl.Launchers(cfg.Cfg)
	for i := range launchers {
		if strings.EqualFold(launchers[i].ID, launcherID) {
			return &launchers[i]
		}
	}
	return helpers.GlobalLauncherCache.GetLauncherByID(launcherID)
}

func getLaunchClosure(
	pl platforms.Platform,
	env *platforms.CmdEnv,
) func(path string) error {
	return func(path string) error {
		launcherID := env.Cmd.AdvArgs.Get(zapscript.KeyLauncher)
		action := env.Cmd.AdvArgs.Get(zapscript.KeyAction)
		setName := env.Cmd.AdvArgs.Get(zapscript.KeySetName)
		setNameSameDir := env.Cmd.AdvArgs.Get(zapscript.KeySetNameSameDir)
		slot := env.Cmd.AdvArgs.Get(zapscript.KeySlot)
		if slot == "" && env.Playlist.Active != nil && env.Playlist.Active.Slot != "" {
			slot = env.Playlist.Active.Slot
		}
		normalizedSlot, err := mediaslot.Normalize(slot)
		if err != nil {
			return fmt.Errorf("normalize media slot: %w", err)
		}

		var opts *platforms.LaunchOptions
		if action != "" || setName != "" || setNameSameDir != "" || normalizedSlot != mediaslot.Primary {
			opts = &platforms.LaunchOptions{
				Action:         action,
				SetName:        setName,
				SetNameSameDir: setNameSameDir,
				Slot:           normalizedSlot,
			}
		}

		if launcherID != "" {
			launcher := findLauncher(pl, env, launcherID)
			if launcher == nil {
				return fmt.Errorf("launcher not found: %s", launcherID)
			}
			log.Info().Msgf("launching with launcher: %s", launcherID)
			return pl.LaunchMedia(env.Cfg, path, launcher, env.Database, opts)
		}

		return pl.LaunchMedia(env.Cfg, path, nil, env.Database, opts)
	}
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

	var args zapscript.LaunchArgs
	if err := ParseAdvArgs(pl, &env, &args); err != nil {
		return platforms.CmdResult{}, err
	}

	if dler, ok := isValidRemoteFileURL(path); ok && args.System != "" {
		installPath, err := installer.InstallRemoteFile(
			env.LauncherCtx,
			env.Cfg, pl,
			path,
			args.System,
			args.PreNotice,
			args.Name,
			dler,
		)
		if err != nil {
			return platforms.CmdResult{}, fmt.Errorf("failed to install remote file: %w", err)
		}
		path = installPath
	}

	if args.System != "" {
		system, lookupErr := systemdefs.LookupSystem(args.System)
		if lookupErr != nil {
			log.Warn().Err(lookupErr).Str("system", args.System).
				Msg("system arg provided but lookup failed - falling back to auto-detection")
		} else {
			applySystemDefaultLauncher(pl, &env, system.ID)
		}
	}

	launch := getLaunchClosure(pl, &env)

	// if it's an absolute path, just try launch it
	if filepath.IsAbs(path) {
		log.Debug().Msgf("launching absolute path: %s", path)
		applySystemDefaultLauncherForPath(pl, &env, path)
		return platforms.CmdResult{
			MediaChanged: true,
		}, launch(path)
	}

	// match for uri style launch syntax
	if helpers.ReURI.MatchString(path) {
		log.Debug().Msgf("launching uri: %s", path)
		applySystemDefaultLauncherForPath(pl, &env, path)
		return platforms.CmdResult{
			MediaChanged: true,
		}, launch(path)
	}

	// for relative paths, perform a basic check if the file exists in a games folder
	// this always takes precedence over the system/path format (but is not totally cross platform)
	var findErr error
	var p string
	if p, findErr = findFile(afero.NewOsFs(), pl, env.Cfg, path); findErr == nil {
		log.Debug().Msgf("launching found relative path: %s", p)
		applySystemDefaultLauncherForPath(pl, &env, p)
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

	// TODO: create central function for parsing the <system>/<path> format
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

	applySystemDefaultLauncher(pl, &env, system.ID)

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
		if fp, systemFindErr = findFile(afero.NewOsFs(), pl, env.Cfg, systemPath); systemFindErr == nil {
			log.Debug().Msgf("launching found system path: %s", fp)
			applySystemDefaultLauncherForPath(pl, &env, fp)
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
			return cmdSearch(pl, env)
		}
		log.Info().Msgf("searching in %s: %s", system.ID, lookupPath)
		// treat as a direct title launch
		ctx, cancel := mediaDBLookupContext(&env)
		defer cancel()
		res, err := gamesdb.SearchMediaPathExact(
			ctx,
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
		applySystemDefaultLauncher(pl, &env, game.SystemID)
		return platforms.CmdResult{
			MediaChanged: true,
		}, launch(game.Path)
	}

	return platforms.CmdResult{}, fmt.Errorf("%w: %s", ErrFileNotFound, path)
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

	var args zapscript.LaunchSearchArgs
	if err := ParseAdvArgs(pl, &env, &args); err != nil {
		return platforms.CmdResult{}, fmt.Errorf("invalid advanced arguments: %w", err)
	}

	launch := getLaunchClosure(pl, &env)
	tagFilters := args.Tags

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
		ctx, cancel := mediaDBLookupContext(&env)
		defer cancel()
		res, searchErr := gamesdb.SearchMediaWithFilters(ctx, &searchFilters)
		if searchErr != nil {
			return platforms.CmdResult{}, fmt.Errorf("failed to search all systems for '%s': %w", query, searchErr)
		}

		if len(res) == 0 {
			return platforms.CmdResult{}, fmt.Errorf("no results found for: %s", query)
		}

		applySystemDefaultLauncher(pl, &env, res[0].SystemID)
		return platforms.CmdResult{
			MediaChanged: true,
		}, launch(res[0].Path)
	}

	ps := strings.SplitN(query, "/", 2)
	if len(ps) < 2 {
		return platforms.CmdResult{}, fmt.Errorf("invalid search format: %s", query)
	}

	systemID, searchQuery := ps[0], ps[1]

	if searchQuery == "" {
		return platforms.CmdResult{}, errors.New("no query specified")
	}

	var systems []systemdefs.System

	if strings.EqualFold(systemID, "all") {
		systems = systemdefs.AllSystems()
	} else {
		system, lookupErr := systemdefs.LookupSystem(systemID)
		if lookupErr != nil {
			return platforms.CmdResult{}, fmt.Errorf("failed to lookup system '%s': %w", systemID, lookupErr)
		}

		systems = systemdefs.SystemsWithFallbacks([]systemdefs.System{*system})
	}

	searchFilters := database.SearchFilters{
		Systems: systems,
		Query:   searchQuery,
		Tags:    tagFilters,
		Limit:   1,
	}
	ctx, cancel := mediaDBLookupContext(&env)
	defer cancel()
	res, searchErr := gamesdb.SearchMediaWithFilters(ctx, &searchFilters)
	if searchErr != nil {
		return platforms.CmdResult{}, fmt.Errorf("failed to search systems for '%s': %w", searchQuery, searchErr)
	}

	if len(res) == 0 {
		return platforms.CmdResult{}, fmt.Errorf("no results found for: %s", searchQuery)
	}

	applySystemDefaultLauncher(pl, &env, res[0].SystemID)
	return platforms.CmdResult{
		MediaChanged: true,
	}, launch(res[0].Path)
}

// getUniqueRecentMedia returns the Nth most recently played unique game from
// media history, deduplicated by MediaPath (1-indexed: offset=1 is most recent).
func getUniqueRecentMedia(
	userDB database.UserDBI, offset int,
) (database.MediaHistoryEntry, error) {
	fetchLimit := min(offset*10, 100)
	entries, err := userDB.GetMediaHistory(nil, 0, fetchLimit)
	if err != nil {
		return database.MediaHistoryEntry{}, fmt.Errorf("failed to query media history: %w", err)
	}

	seen := make(map[string]bool)
	var unique []database.MediaHistoryEntry
	for i := range entries {
		if seen[entries[i].MediaPath] {
			continue
		}
		seen[entries[i].MediaPath] = true
		unique = append(unique, entries[i])
		if len(unique) >= offset {
			break
		}
	}

	if len(unique) < offset {
		return database.MediaHistoryEntry{}, fmt.Errorf(
			"%w: need %d unique games but only found %d",
			ErrNoHistory, offset, len(unique),
		)
	}

	return unique[offset-1], nil
}

//nolint:gocritic // single-use parameter in command handler
func cmdLaunchLast(pl platforms.Platform, env platforms.CmdEnv) (platforms.CmdResult, error) {
	offset := 1
	if len(env.Cmd.Args) > 0 && env.Cmd.Args[0] != "" {
		n, err := strconv.Atoi(env.Cmd.Args[0])
		if err != nil {
			return platforms.CmdResult{}, fmt.Errorf("invalid offset: %w", err)
		}
		if n <= 0 {
			return platforms.CmdResult{}, fmt.Errorf("offset must be positive, got %d", n)
		}
		offset = n
	}

	var args zapscript.LaunchLastArgs
	if err := ParseAdvArgs(pl, &env, &args); err != nil {
		return platforms.CmdResult{}, err
	}

	entry, err := getUniqueRecentMedia(env.Database.UserDB, offset)
	if err != nil {
		return platforms.CmdResult{}, err
	}

	path, err := findFile(afero.NewOsFs(), pl, env.Cfg, entry.MediaPath)
	if err != nil {
		return platforms.CmdResult{}, err
	}

	applySystemDefaultLauncher(pl, &env, entry.SystemID)
	launch := getLaunchClosure(pl, &env)

	log.Info().
		Str("media", entry.MediaName).
		Str("system", entry.SystemID).
		Int("offset", offset).
		Msgf("launching last played game")

	if err := launch(path); err != nil {
		return platforms.CmdResult{
			MediaChanged: true,
		}, fmt.Errorf("failed to launch last played game '%s': %w", entry.MediaPath, err)
	}

	return platforms.CmdResult{
		MediaChanged: true,
	}, nil
}
