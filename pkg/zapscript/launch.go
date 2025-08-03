package zapscript

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms/shared/installer"
	"github.com/rs/zerolog/log"
)

func cmdSystem(pl platforms.Platform, env platforms.CmdEnv) (platforms.CmdResult, error) { //nolint:gocritic // single-use parameter in command handler
	if len(env.Cmd.Args) != 1 {
		return platforms.CmdResult{}, ErrArgCount
	}

	systemID := env.Cmd.Args[0]

	if strings.EqualFold(systemID, "menu") {
		return platforms.CmdResult{
			MediaChanged: true,
		}, pl.StopActiveLauncher()
	}

	return platforms.CmdResult{
		MediaChanged: true,
	}, pl.LaunchSystem(env.Cfg, systemID)
}

func cmdRandom(pl platforms.Platform, env platforms.CmdEnv) (platforms.CmdResult, error) { //nolint:gocritic // single-use parameter in command handler
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

	gamesdb := env.Database.MediaDB

	if strings.EqualFold(query, "all") {
		game, gameErr := gamesdb.RandomGame(systemdefs.AllSystems())
		if gameErr != nil {
			return platforms.CmdResult{}, gameErr
		}

		return platforms.CmdResult{
			MediaChanged: true,
		}, launch(game.Path)
	}

	// absolute path, try read dir and pick random file
	// TODO: won't work for zips, switch to using gamesdb when it indexes paths
	// TODO: doesn't filter on extensions
	if filepath.IsAbs(query) {
		if _, statErr := os.Stat(query); statErr != nil {
			return platforms.CmdResult{}, statErr
		}

		files, globErr := filepath.Glob(filepath.Join(query, "*"))
		if globErr != nil {
			return platforms.CmdResult{}, globErr
		}

		if len(files) == 0 {
			return platforms.CmdResult{}, fmt.Errorf("no files found in: %s", query)
		}

		file, randomErr := helpers.RandomElem(files)
		if randomErr != nil {
			return platforms.CmdResult{}, randomErr
		}

		return platforms.CmdResult{
			MediaChanged: true,
		}, launch(file)
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
				return platforms.CmdResult{}, lookupErr
			} else if system == nil {
				return platforms.CmdResult{}, fmt.Errorf("system not found: %s", systemID)
			}
			systems = []systemdefs.System{*system}
		}

		query = strings.ToLower(query)

		res, searchErr := gamesdb.SearchMediaPathGlob(systems, query)
		if searchErr != nil {
			return platforms.CmdResult{}, searchErr
		}

		if len(res) == 0 {
			return platforms.CmdResult{}, fmt.Errorf("no results found for: %s", query)
		}

		game, randomErr := helpers.RandomElem(res)
		if randomErr != nil {
			return platforms.CmdResult{}, randomErr
		}

		return platforms.CmdResult{
			MediaChanged: true,
		}, launch(game.Path)
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

	game, err := gamesdb.RandomGame(systems)
	if err != nil {
		return platforms.CmdResult{}, err
	}

	return platforms.CmdResult{
		MediaChanged: true,
	}, launch(game.Path)
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
			return launcher.Launch(env.Cfg, args)
		}, nil
	}
	return func(args string) error {
		return pl.LaunchMedia(env.Cfg, args)
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

func cmdLaunch(pl platforms.Platform, env platforms.CmdEnv) (platforms.CmdResult, error) { //nolint:gocritic // single-use parameter in command handler
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
			return platforms.CmdResult{}, err
		}
		path = installPath
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

	// attempt to parse the <system>/<path> format
	ps := strings.SplitN(path, "/", 2)
	if len(ps) < 2 {
		return platforms.CmdResult{}, fmt.Errorf("invalid launch format: %s", path)
	}

	systemID, lookupPath := ps[0], ps[1]

	system, err := systemdefs.LookupSystem(systemID)
	if err != nil {
		return platforms.CmdResult{}, err
	}

	log.Info().Msgf("launching system: %s, path: %s", systemID, lookupPath)

	var launchers []platforms.Launcher
	allLaunchers := pl.Launchers(env.Cfg)
	for i := range allLaunchers {
		if allLaunchers[i].SystemID == system.ID {
			launchers = append(launchers, allLaunchers[i])
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
			return platforms.CmdResult{}, err
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

func cmdSearch(pl platforms.Platform, env platforms.CmdEnv) (platforms.CmdResult, error) { //nolint:gocritic // single-use parameter in command handler
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

	query = strings.ToLower(query)
	query = strings.TrimSpace(query)

	gamesdb := env.Database.MediaDB

	if !strings.Contains(query, "/") {
		// search all systems
		res, searchErr := gamesdb.SearchMediaPathGlob(systemdefs.AllSystems(), query)
		if searchErr != nil {
			return platforms.CmdResult{}, searchErr
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
			return platforms.CmdResult{}, lookupErr
		}

		systems = append(systems, *system)
	}

	res, searchErr := gamesdb.SearchMediaPathGlob(systems, query)
	if searchErr != nil {
		return platforms.CmdResult{}, searchErr
	}

	if len(res) == 0 {
		return platforms.CmdResult{}, fmt.Errorf("no results found for: %s", query)
	}

	return platforms.CmdResult{
		MediaChanged: true,
	}, launch(res[0].Path)
}
