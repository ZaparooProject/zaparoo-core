package zapscript

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/rs/zerolog/log"

	"github.com/ZaparooProject/zaparoo-core/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/pkg/utils"
)

func cmdSystem(pl platforms.Platform, env platforms.CmdEnv) (platforms.CmdResult, error) {
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

	gamesdb := env.Database.MediaDB

	if strings.EqualFold(query, "all") {
		game, err := gamesdb.RandomGame(systemdefs.AllSystems())
		if err != nil {
			return platforms.CmdResult{}, err
		}

		return platforms.CmdResult{
			MediaChanged: true,
		}, launch(game.Path)
	}

	// absolute path, try read dir and pick random file
	// TODO: won't work for zips, switch to using gamesdb when it indexes paths
	// TODO: doesn't filter on extensions
	if filepath.IsAbs(query) {
		if _, err := os.Stat(query); err != nil {
			return platforms.CmdResult{}, err
		}

		files, err := filepath.Glob(filepath.Join(query, "*"))
		if err != nil {
			return platforms.CmdResult{}, err
		}

		if len(files) == 0 {
			return platforms.CmdResult{}, fmt.Errorf("no files found in: %s", query)
		}

		file, err := utils.RandomElem(files)
		if err != nil {
			return platforms.CmdResult{}, err
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
		systemId, query := ps[0], ps[1]

		var systems []systemdefs.System
		if strings.EqualFold(systemId, "all") {
			systems = systemdefs.AllSystems()
		} else {
			system, err := systemdefs.LookupSystem(systemId)
			if err != nil {
				return platforms.CmdResult{}, err
			} else if system == nil {
				return platforms.CmdResult{}, fmt.Errorf("system not found: %s", systemId)
			}
			systems = []systemdefs.System{*system}
		}

		query = strings.ToLower(query)

		res, err := gamesdb.SearchMediaPathGlob(systems, query)
		if err != nil {
			return platforms.CmdResult{}, err
		}

		if len(res) == 0 {
			return platforms.CmdResult{}, fmt.Errorf("no results found for: %s", query)
		}

		game, err := utils.RandomElem(res)
		if err != nil {
			return platforms.CmdResult{}, err
		}

		return platforms.CmdResult{
			MediaChanged: true,
		}, launch(game.Path)
	}

	// assume given a list of system ids
	systems := make([]systemdefs.System, 0, len(env.Cmd.Args))

	for _, id := range env.Cmd.Args {
		system, err := systemdefs.LookupSystem(id)
		if err != nil {
			log.Error().Err(err).Msgf("error looking up system: %s", id)
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
	env platforms.CmdEnv,
) (func(args string) error, error) {
	if env.Cmd.AdvArgs["launcher"] != "" {
		var launcher platforms.Launcher

		for _, l := range pl.Launchers(env.Cfg) {
			if l.ID == env.Cmd.AdvArgs["launcher"] {
				launcher = l
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
	} else {
		return func(args string) error {
			return pl.LaunchMedia(env.Cfg, args)
		}, nil
	}
}

var reUri = regexp.MustCompile(`^.+://`)

func cmdLaunch(pl platforms.Platform, env platforms.CmdEnv) (platforms.CmdResult, error) {
	if len(env.Cmd.Args) == 0 {
		return platforms.CmdResult{}, ErrArgCount
	}

	path := env.Cmd.Args[0]

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
	if reUri.MatchString(path) {
		log.Debug().Msgf("launching uri: %s", path)
		return platforms.CmdResult{
			MediaChanged: true,
		}, launch(path)
	}

	// for relative paths, perform a basic check if the file exists in a games folder
	// this always takes precedence over the system/path format (but is not totally cross platform)
	if p, err := findFile(pl, env.Cfg, path); err == nil {
		log.Debug().Msgf("launching found relative path: %s", p)
		return platforms.CmdResult{
			MediaChanged: true,
		}, launch(p)
	} else {
		log.Debug().Err(err).Msgf("error finding file: %s", path)
	}

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
	for _, l := range pl.Launchers(env.Cfg) {
		if l.SystemID == system.ID {
			launchers = append(launchers, l)
		}
	}

	var folders []string
	for _, l := range launchers {
		for _, folder := range l.Folders {
			if !utils.Contains(folders, folder) {
				folders = append(folders, folder)
			}
		}
	}

	for _, f := range folders {
		systemPath := filepath.Join(f, lookupPath)
		log.Debug().Msgf("checking system path: %s", systemPath)
		if fp, err := findFile(pl, env.Cfg, systemPath); err == nil {
			log.Debug().Msgf("launching found system path: %s", fp)
			return platforms.CmdResult{
				MediaChanged: true,
			}, launch(fp)
		} else {
			log.Debug().Err(err).Msgf("error finding system file: %s", lookupPath)
		}
	}

	gamesdb := env.Database.MediaDB

	// search if the path contains no / or file extensions
	if !strings.Contains(lookupPath, "/") && filepath.Ext(lookupPath) == "" {
		if strings.Contains(lookupPath, "*") {
			// treat as a search
			// TODO: passthrough advanced args
			return cmdSearch(pl, env)
		} else {
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
	}

	return platforms.CmdResult{}, fmt.Errorf("file not found: %s", path)
}

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

	query = strings.ToLower(query)
	query = strings.TrimSpace(query)

	gamesdb := env.Database.MediaDB

	if !strings.Contains(query, "/") {
		// search all systems
		res, err := gamesdb.SearchMediaPathGlob(systemdefs.AllSystems(), query)
		if err != nil {
			return platforms.CmdResult{}, err
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
		return platforms.CmdResult{}, fmt.Errorf("no query specified")
	}

	systems := make([]systemdefs.System, 0)

	if strings.EqualFold(systemID, "all") {
		systems = systemdefs.AllSystems()
	} else {
		system, err := systemdefs.LookupSystem(systemID)
		if err != nil {
			return platforms.CmdResult{}, err
		}

		systems = append(systems, *system)
	}

	res, err := gamesdb.SearchMediaPathGlob(systems, query)
	if err != nil {
		return platforms.CmdResult{}, err
	}

	if len(res) == 0 {
		return platforms.CmdResult{}, fmt.Errorf("no results found for: %s", query)
	}

	return platforms.CmdResult{
		MediaChanged: true,
	}, launch(res[0].Path)
}
