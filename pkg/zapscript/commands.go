/*
Zaparoo Core
Copyright (C) 2023, 2024 Callan Barrett

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

package zapscript

import (
	"errors"
	"fmt"
	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/service/playlists"
	"github.com/ZaparooProject/zaparoo-core/pkg/service/tokens"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/exp/slices"

	"github.com/rs/zerolog/log"

	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
)

// TODO: adding some logging for each command
// TODO: game file by hash

var commandMappings = map[string]func(platforms.Platform, platforms.CmdEnv) error{
	"launch":        cmdLaunch,
	"launch.system": cmdSystem,
	"launch.random": cmdRandom,
	"launch.search": cmdSearch,

	"playlist.play":     cmdPlaylistPlay,
	"playlist.next":     cmdPlaylistNext,
	"playlist.previous": cmdPlaylistPrevious,

	"execute": cmdExecute,
	"delay":   cmdDelay,

	"mister.ini":    forwardCmd,
	"mister.core":   forwardCmd,
	"mister.script": forwardCmd,
	"mister.mgl":    forwardCmd,

	"http.get":  cmdHttpGet,
	"http.post": cmdHttpPost,

	"input.keyboard": cmdKeyboard,
	"input.gamepad":  cmdGamepad,
	"input.coinp1":   cmdCoinP1,
	"input.coinp2":   cmdCoinP2,

	"input.key": cmdKey,     // DEPRECATED
	"key":       cmdKey,     // DEPRECATED
	"coinp1":    cmdCoinP1,  // DEPRECATED
	"coinp2":    cmdCoinP2,  // DEPRECATED
	"random":    cmdRandom,  // DEPRECATED
	"shell":     cmdExecute, // DEPRECATED
	"command":   cmdExecute, // DEPRECATED
	"ini":       forwardCmd, // DEPRECATED
	"system":    cmdSystem,  // DEPRECATED
	"get":       cmdHttpGet, // DEPRECATED
}

var softwareChangeCommands = []string{
	"random", // DEPRECATED
	"launch",
	"launch.system",
	"launch.random",
	"launch.search",
	"mister.core",
	"mister.mgl",
}

func forwardCmd(pl platforms.Platform, env platforms.CmdEnv) error {
	return pl.ForwardCmd(env)
}

// Check all games folders for a relative path to a file
func findFile(pl platforms.Platform, cfg *config.Instance, path string) (string, error) {
	// TODO: can do basic file exists check here too
	if filepath.IsAbs(path) {
		return path, nil
	}

	ps := strings.Split(path, string(filepath.Separator))
	statPath := path

	// if the file is inside a zip or virtual list, we just check that file exists
	// TODO: both of these things are very specific to mister, it would be good to
	//       have a more generic way of handling this for other platforms, or
	//       implement them from zaparoo(?)
	for i, p := range ps {
		ext := filepath.Ext(strings.ToLower(p))
		if ext == ".zip" || ext == ".txt" {
			statPath = filepath.Join(ps[:i+1]...)
			log.Debug().Msgf("found zip/txt, setting stat path: %s", statPath)
			break
		}
	}

	for _, gf := range pl.RootDirs(cfg) {
		fullPath := filepath.Join(gf, statPath)
		if _, err := os.Stat(fullPath); err == nil {
			log.Debug().Msgf("found file: %s", fullPath)
			return filepath.Join(gf, path), nil
		}
	}

	return path, fmt.Errorf("file not found: %s", path)
}

/**
 * Will launch a command related to the token, and if it is a software that will
 * change the currently loaded software will also return a boolean set to true
 */
func LaunchToken(
	pl platforms.Platform,
	cfg *config.Instance,
	plsc playlists.PlaylistController,
	t tokens.Token,
	manual bool,
	text string,
	totalCommands int,
	currentIndex int,
) (error, bool) {
	namedArgs := make(map[string]string)
	if i := strings.LastIndex(text, "?"); i != -1 {
		u, err := url.Parse(text[i:])
		if err != nil {
			return err, false
		}

		qs, err := url.ParseQuery(u.RawQuery)
		if err != nil {
			return err, false
		}

		text = text[:i]

		for k, v := range qs {
			namedArgs[k] = v[0]
		}
	}
	log.Debug().Msgf("named args: %v", namedArgs)

	// explicit commands must begin with **
	if strings.HasPrefix(text, "**") {
		if t.Source == tokens.SourcePlaylist {
			log.Debug().Str("text", text).Msgf("playlists cannot run commands, skipping")
			return nil, false
		}

		text = strings.TrimPrefix(text, "**")
		ps := strings.SplitN(text, ":", 2)
		if len(ps) < 2 {
			return fmt.Errorf("invalid command: %s", text), false
		}

		cmd, args := strings.ToLower(strings.TrimSpace(ps[0])), strings.TrimSpace(ps[1])

		env := platforms.CmdEnv{
			Cmd:           cmd,
			Args:          args,
			NamedArgs:     namedArgs,
			Cfg:           cfg,
			Playlist:      plsc,
			Manual:        manual,
			Text:          text,
			TotalCommands: totalCommands,
			CurrentIndex:  currentIndex,
		}

		if f, ok := commandMappings[cmd]; ok {
			log.Info().Msgf("launching command: %s", cmd)

			if cmd == "execute" && !cfg.IsExecuteAllowed(args) {
				return errors.New("execute not allowed"), false
			}

			softwareChange := slices.Contains(softwareChangeCommands, cmd)
			if softwareChange {
				// a launch triggered outside a playlist itself
				log.Debug().Msg("clearing current playlist")
				plsc.Queue <- nil
			}

			return f(pl, env), softwareChange
		} else {
			return fmt.Errorf("unknown command: %s", cmd), false
		}
	}

	if t.Source != tokens.SourcePlaylist {
		// a launch triggered outside a playlist itself
		log.Debug().Msg("clearing current playlist")
		plsc.Queue <- nil
	}

	// if it's not a command, treat it as a generic launch command
	return cmdLaunch(pl, platforms.CmdEnv{
		Cmd:           "launch",
		Args:          text,
		NamedArgs:     namedArgs,
		Cfg:           cfg,
		Manual:        manual,
		Text:          text,
		TotalCommands: totalCommands,
		CurrentIndex:  currentIndex,
	}), true
}
