package zapscript

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/pkg/service/playlists"

	"github.com/rs/zerolog/log"

	"github.com/ZaparooProject/zaparoo-core/pkg/database/gamesdb"
	"github.com/ZaparooProject/zaparoo-core/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/pkg/utils"
)

func cmdSystem(pl platforms.Platform, env platforms.CmdEnv) error {
	// TODO: launcher named arg support

	if strings.EqualFold(env.Args, "menu") {
		return pl.KillLauncher()
	}

	return pl.LaunchSystem(env.Cfg, env.Args)
}

func cmdRandom(pl platforms.Platform, env platforms.CmdEnv) error {
	if env.Args == "" {
		return fmt.Errorf("no system specified")
	}

	launch, err := getAltLauncher(pl, env)
	if err != nil {
		return err
	}

	if env.Args == "all" {
		game, err := gamesdb.RandomGame(pl, systemdefs.AllSystems())
		if err != nil {
			return err
		}

		return launch(game.Path)
	}

	// absolute path, try read dir and pick random file
	// TODO: won't work for zips, switch to using gamesdb when it indexes paths
	// TODO: doesn't filter on extensions
	if filepath.IsAbs(env.Args) {
		if _, err := os.Stat(env.Args); err != nil {
			return err
		}

		files, err := filepath.Glob(filepath.Join(env.Args, "*"))
		if err != nil {
			return err
		}

		if len(files) == 0 {
			return fmt.Errorf("no files found in: %s", env.Args)
		}

		file, err := utils.RandomElem(files)
		if err != nil {
			return err
		}

		return launch(file)
	}

	// perform a search similar to launch.search and pick randomly
	// looking for <system>/<query> format
	ps := strings.SplitN(env.Args, "/", 2)
	if len(ps) == 2 {
		systemId, query := ps[0], ps[1]

		system, err := systemdefs.LookupSystem(systemId)
		if err != nil {
			return err
		} else if system == nil {
			return fmt.Errorf("system not found: %s", systemId)
		}

		query = strings.ToLower(query)

		res, err := gamesdb.SearchNamesGlob(pl, []systemdefs.System{*system}, query)
		if err != nil {
			return err
		}

		if len(res) == 0 {
			return fmt.Errorf("no results found for: %s", query)
		}

		game, err := utils.RandomElem(res)
		if err != nil {
			return err
		}

		return launch(game.Path)
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
		return err
	}

	return launch(game.Path)
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
			return pl.LaunchFile(env.Cfg, args)
		}, nil
	}
}

var reUri = regexp.MustCompile(`^.+://`)

func cmdLaunch(pl platforms.Platform, env platforms.CmdEnv) error {
	launch, err := getAltLauncher(pl, env)
	if err != nil {
		return err
	}

	// if it's an absolute path, just try launch it
	if filepath.IsAbs(env.Args) {
		log.Debug().Msgf("launching absolute path: %s", env.Args)
		return launch(env.Args)
	}

	// match for uri style launch syntax
	if reUri.MatchString(env.Args) {
		log.Debug().Msgf("launching uri: %s", env.Args)
		return launch(env.Args)
	}

	// for relative paths, perform a basic check if the file exists in a games folder
	// this always takes precedence over the system/path format (but is not totally cross platform)
	if p, err := findFile(pl, env.Cfg, env.Args); err == nil {
		log.Debug().Msgf("launching found relative path: %s", p)
		return launch(p)
	} else {
		log.Debug().Err(err).Msgf("error finding file: %s", env.Args)
	}

	// attempt to parse the <system>/<path> format
	ps := strings.SplitN(env.Text, "/", 2)
	if len(ps) < 2 {
		return fmt.Errorf("invalid launch format: %s", env.Text)
	}

	systemId, path := ps[0], ps[1]

	system, err := systemdefs.LookupSystem(systemId)
	if err != nil {
		return err
	}

	log.Info().Msgf("launching system: %s, path: %s", systemId, path)

	var launchers []platforms.Launcher
	for _, l := range pl.Launchers() {
		if l.SystemId == system.Id {
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
		if fp, err := findFile(pl, env.Cfg, systemPath); err == nil {
			log.Debug().Msgf("launching found system path: %s", fp)
			return launch(fp)
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
			log.Info().Msgf("searching in %s: %s", system.Id, path)
			// treat as a direct title launch
			res, err := gamesdb.SearchNamesExact(
				pl,
				[]systemdefs.System{*system},
				path,
			)

			if err != nil {
				return err
			} else if len(res) == 0 {
				return fmt.Errorf("no results found for: %s", path)
			}

			log.Info().Msgf("found result: %s", res[0].Path)

			game := res[0]
			return launch(game.Path)
		}
	}

	return fmt.Errorf("file not found: %s", env.Args)
}

func cmdSearch(pl platforms.Platform, env platforms.CmdEnv) error {
	if env.Args == "" {
		return fmt.Errorf("no query specified")
	}

	launch, err := getAltLauncher(pl, env)
	if err != nil {
		return err
	}

	query := strings.ToLower(env.Args)
	query = strings.TrimSpace(query)

	if !strings.Contains(env.Args, "/") {
		// search all systems
		res, err := gamesdb.SearchNamesGlob(pl, systemdefs.AllSystems(), query)
		if err != nil {
			return err
		}

		if len(res) == 0 {
			return fmt.Errorf("no results found for: %s", query)
		}

		return launch(res[0].Path)
	}

	ps := strings.SplitN(query, "/", 2)
	if len(ps) < 2 {
		return fmt.Errorf("invalid search format: %s", query)
	}

	systemId, query := ps[0], ps[1]

	if query == "" {
		return fmt.Errorf("no query specified")
	}

	systems := make([]systemdefs.System, 0)

	if systemId == "all" {
		systems = systemdefs.AllSystems()
	} else {
		system, err := systemdefs.LookupSystem(systemId)
		if err != nil {
			return err
		}

		systems = append(systems, *system)
	}

	res, err := gamesdb.SearchNamesGlob(pl, systems, query)
	if err != nil {
		return err
	}

	if len(res) == 0 {
		return fmt.Errorf("no results found for: %s", query)
	}

	return launch(res[0].Path)
}

func cmdPlaylistPlay(_ platforms.Platform, env platforms.CmdEnv) error {
	if env.Args == "" {
		return fmt.Errorf("no playlist path specified")
	}

	if _, err := os.Stat(env.Args); err != nil {
		return err
	}

	files, err := os.ReadDir(env.Args)
	if err != nil {
		return err
	}

	media := make([]string, 0)
	for _, file := range files {
		if file.IsDir() || filepath.Ext(file.Name()) == "" {
			continue
		}

		media = append(media, filepath.Join(env.Args, file.Name()))
	}

	if len(media) == 0 {
		return fmt.Errorf("no media found in: %s", env.Args)
	}

	log.Info().Any("media", media).Msgf("new playlist: %s", env.Args)
	pls := playlists.NewPlaylist(media)
	env.Playlist.Queue <- pls

	return nil
}

func cmdPlaylistNext(_ platforms.Platform, env platforms.CmdEnv) error {
	if env.Playlist.Active == nil {
		return fmt.Errorf("no playlist active")
	}

	env.Playlist.Queue <- playlists.Next(*env.Playlist.Active)

	return nil
}

func cmdPlaylistPrevious(_ platforms.Platform, env platforms.CmdEnv) error {
	if env.Playlist.Active == nil {
		return fmt.Errorf("no playlist active")
	}

	env.Playlist.Queue <- playlists.Previous(*env.Playlist.Active)

	return nil
}
