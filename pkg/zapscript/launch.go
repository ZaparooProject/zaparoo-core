package zapscript

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/rs/zerolog/log"

	"github.com/ZaparooProject/zaparoo-core/pkg/database/gamesdb"
	"github.com/ZaparooProject/zaparoo-core/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/pkg/utils"
)

func cmdSystem(pl platforms.Platform, env platforms.CmdEnv) (platforms.CmdResult, error) {
	// TODO: launcher named arg support

	if strings.EqualFold(env.Args, "menu") {
		return platforms.CmdResult{
			MediaChanged: true,
		}, pl.StopActiveLauncher()
	}

	return platforms.CmdResult{
		MediaChanged: true,
	}, pl.LaunchSystem(env.Cfg, env.Args)
}

func cmdRandom(pl platforms.Platform, env platforms.CmdEnv) (platforms.CmdResult, error) {
	if env.Args == "" {
		return platforms.CmdResult{}, fmt.Errorf("no system specified")
	}

	launch, err := getAltLauncher(pl, env)
	if err != nil {
		return platforms.CmdResult{}, err
	}

	if env.Args == "all" {
		game, err := gamesdb.RandomGame(pl, systemdefs.AllSystems())
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
	if filepath.IsAbs(env.Args) {
		if _, err := os.Stat(env.Args); err != nil {
			return platforms.CmdResult{}, err
		}

		files, err := filepath.Glob(filepath.Join(env.Args, "*"))
		if err != nil {
			return platforms.CmdResult{}, err
		}

		if len(files) == 0 {
			return platforms.CmdResult{}, fmt.Errorf("no files found in: %s", env.Args)
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
	ps := strings.SplitN(env.Args, "/", 2)
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

		res, err := gamesdb.SearchNamesGlob(pl, systems, query)
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

	systemIds := strings.Split(env.Args, ",")
	systems := make([]systemdefs.System, 0, len(systemIds))

	for _, id := range systemIds {
		system, err := systemdefs.LookupSystem(id)
		if err != nil {
			log.Error().Err(err).Msgf("error looking up system: %s", id)
			continue
		}

		systems = append(systems, *system)
	}

	game, err := gamesdb.RandomGame(pl, systems)
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
	if env.NamedArgs["launcher"] != "" {
		var launcher platforms.Launcher

		for _, l := range pl.Launchers() {
			if l.Id == env.NamedArgs["launcher"] {
				launcher = l
				break
			}
		}

		if launcher.Launch == nil {
			return nil, fmt.Errorf("alt launcher not found: %s", env.NamedArgs["launcher"])
		}

		log.Info().Msgf("launching with alt launcher: %s", env.NamedArgs["launcher"])

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
	launch, err := getAltLauncher(pl, env)
	if err != nil {
		return platforms.CmdResult{}, err
	}

	// if it's an absolute path, just try launch it
	if filepath.IsAbs(env.Args) {
		log.Debug().Msgf("launching absolute path: %s", env.Args)
		return platforms.CmdResult{
			MediaChanged: true,
		}, launch(env.Args)
	}

	// match for uri style launch syntax
	if reUri.MatchString(env.Args) {
		log.Debug().Msgf("launching uri: %s", env.Args)
		return platforms.CmdResult{
			MediaChanged: true,
		}, launch(env.Args)
	}

	// for relative paths, perform a basic check if the file exists in a games folder
	// this always takes precedence over the system/path format (but is not totally cross platform)
	if p, err := findFile(pl, env.Cfg, env.Args); err == nil {
		log.Debug().Msgf("launching found relative path: %s", p)
		return platforms.CmdResult{
			MediaChanged: true,
		}, launch(p)
	} else {
		log.Debug().Err(err).Msgf("error finding file: %s", env.Args)
	}

	// attempt to parse the <system>/<path> format
	ps := strings.SplitN(env.Text, "/", 2)
	if len(ps) < 2 {
		return platforms.CmdResult{}, fmt.Errorf("invalid launch format: %s", env.Text)
	}

	systemId, path := ps[0], ps[1]

	system, err := systemdefs.LookupSystem(systemId)
	if err != nil {
		return platforms.CmdResult{}, err
	}

	log.Info().Msgf("launching system: %s, path: %s", systemId, path)

	var launchers []platforms.Launcher
	for _, l := range pl.Launchers() {
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
		systemPath := filepath.Join(f, path)
		log.Debug().Msgf("checking system path: %s", systemPath)
		if fp, err := findFile(pl, env.Cfg, systemPath); err == nil {
			log.Debug().Msgf("launching found system path: %s", fp)
			return platforms.CmdResult{
				MediaChanged: true,
			}, launch(fp)
		} else {
			log.Debug().Err(err).Msgf("error finding system file: %s", path)
		}
	}

	// search if the path contains no / or file extensions
	if !strings.Contains(path, "/") && filepath.Ext(path) == "" {
		if strings.Contains(path, "*") {
			// treat as a search
			// TODO: passthrough advanced args
			return cmdSearch(pl, env)
		} else {
			log.Info().Msgf("searching in %s: %s", system.ID, path)
			// treat as a direct title launch
			res, err := gamesdb.SearchNamesExact(
				pl,
				[]systemdefs.System{*system},
				path,
			)

			if err != nil {
				return platforms.CmdResult{}, err
			} else if len(res) == 0 {
				return platforms.CmdResult{}, fmt.Errorf("no results found for: %s", path)
			}

			log.Info().Msgf("found result: %s", res[0].Path)

			game := res[0]
			return platforms.CmdResult{
				MediaChanged: true,
			}, launch(game.Path)
		}
	}

	return platforms.CmdResult{}, fmt.Errorf("file not found: %s", env.Args)
}

func cmdSearch(pl platforms.Platform, env platforms.CmdEnv) (platforms.CmdResult, error) {
	if env.Args == "" {
		return platforms.CmdResult{}, fmt.Errorf("no query specified")
	}

	launch, err := getAltLauncher(pl, env)
	if err != nil {
		return platforms.CmdResult{}, err
	}

	query := strings.ToLower(env.Args)
	query = strings.TrimSpace(query)

	if !strings.Contains(env.Args, "/") {
		// search all systems
		res, err := gamesdb.SearchNamesGlob(pl, systemdefs.AllSystems(), query)
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

	systemId, query := ps[0], ps[1]

	if query == "" {
		return platforms.CmdResult{}, fmt.Errorf("no query specified")
	}

	systems := make([]systemdefs.System, 0)

	if strings.EqualFold(systemId, "all") {
		systems = systemdefs.AllSystems()
	} else {
		system, err := systemdefs.LookupSystem(systemId)
		if err != nil {
			return platforms.CmdResult{}, err
		}

		systems = append(systems, *system)
	}

	res, err := gamesdb.SearchNamesGlob(pl, systems, query)
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
