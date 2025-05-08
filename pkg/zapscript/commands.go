/*
Zaparoo Core
Copyright (C) 2023 - 2025 Callan Barrett

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
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/pkg/zapscript/models"

	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/pkg/service/playlists"
	"github.com/ZaparooProject/zaparoo-core/pkg/service/tokens"

	"github.com/rs/zerolog/log"

	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
)

var cmdMap = map[string]func(
	platforms.Platform,
	platforms.CmdEnv,
) (platforms.CmdResult, error){
	models.ZapScriptCmdLaunch:       cmdLaunch,
	models.ZapScriptCmdLaunchSystem: cmdSystem,
	models.ZapScriptCmdLaunchRandom: cmdRandom,
	models.ZapScriptCmdLaunchSearch: cmdSearch,

	models.ZapScriptCmdPlaylistPlay:     cmdPlaylistPlay,
	models.ZapScriptCmdPlaylistStop:     cmdPlaylistStop,
	models.ZapScriptCmdPlaylistNext:     cmdPlaylistNext,
	models.ZapScriptCmdPlaylistPrevious: cmdPlaylistPrevious,
	models.ZapScriptCmdPlaylistGoto:     cmdPlaylistGoto,
	models.ZapScriptCmdPlaylistPause:    cmdPlaylistPause,
	models.ZapScriptCmdPlaylistLoad:     cmdPlaylistLoad,
	models.ZapScriptCmdPlaylistOpen:     cmdPlaylistOpen,

	models.ZapScriptCmdExecute: cmdExecute,
	models.ZapScriptCmdDelay:   cmdDelay,
	models.ZapScriptCmdStop:    cmdStop,

	models.ZapScriptCmdMisterINI:    forwardCmd,
	models.ZapScriptCmdMisterCore:   forwardCmd,
	models.ZapScriptCmdMisterScript: forwardCmd,
	models.ZapScriptCmdMisterMGL:    forwardCmd,

	models.ZapScriptCmdHTTPGet:  cmdHttpGet,
	models.ZapScriptCmdHTTPPost: cmdHttpPost,

	models.ZapScriptCmdInputKeyboard: cmdKeyboard,
	models.ZapScriptCmdInputGamepad:  cmdGamepad,
	models.ZapScriptCmdInputCoinP1:   cmdCoinP1,
	models.ZapScriptCmdInputCoinP2:   cmdCoinP2,

	models.ZapScriptCmdInputKey: cmdKey,     // DEPRECATED
	models.ZapScriptCmdKey:      cmdKey,     // DEPRECATED
	models.ZapScriptCmdCoinP1:   cmdCoinP1,  // DEPRECATED
	models.ZapScriptCmdCoinP2:   cmdCoinP2,  // DEPRECATED
	models.ZapScriptCmdRandom:   cmdRandom,  // DEPRECATED
	models.ZapScriptCmdShell:    cmdExecute, // DEPRECATED
	models.ZapScriptCmdCommand:  cmdExecute, // DEPRECATED
	models.ZapScriptCmdINI:      forwardCmd, // DEPRECATED
	models.ZapScriptCmdSystem:   cmdSystem,  // DEPRECATED
	models.ZapScriptCmdGet:      cmdHttpGet, // DEPRECATED
}

func forwardCmd(pl platforms.Platform, env platforms.CmdEnv) (platforms.CmdResult, error) {
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

// LaunchToken parses and runs a single ZapScript command.
func LaunchToken(
	pl platforms.Platform,
	cfg *config.Instance,
	plsc playlists.PlaylistController,
	t tokens.Token,
	text string,
	totalCommands int,
	currentIndex int,
	db *database.Database,
) (platforms.CmdResult, error) {
	var unsafe bool
	newText, err := checkLink(cfg, pl, text)
	if err != nil {
		log.Error().Err(err).Msgf("error checking link, continuing")
	} else if newText != "" {
		log.Info().Msgf("valid zap link, replacing text: %s", newText)
		text = newText
		unsafe = true
	}

	if t.Unsafe {
		unsafe = true
	}

	// parse advanced args
	namedArgs := make(map[string]string)
	if i := strings.LastIndex(text, "?"); i != -1 {
		u, err := url.Parse(text[i:])
		if err != nil {
			return platforms.CmdResult{}, err
		}

		qs, err := url.ParseQuery(u.RawQuery)
		if err != nil {
			return platforms.CmdResult{}, err
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
			// TODO: why not? why did i write this?
			log.Error().Str("text", text).Msgf("playlists cannot run commands, skipping")
			return platforms.CmdResult{}, err
		}

		text = strings.TrimPrefix(text, "**")
		ps := strings.SplitN(text, ":", 2)

		var cmd string
		var args string

		if len(ps) < 2 {
			cmd = strings.ToLower(strings.TrimSpace(ps[0]))
			args = ""
		} else {
			cmd = strings.ToLower(strings.TrimSpace(ps[0]))
			args = strings.TrimSpace(ps[1])
		}

		env := platforms.CmdEnv{
			Cmd:           cmd,
			Args:          args,
			NamedArgs:     namedArgs,
			Cfg:           cfg,
			Playlist:      plsc,
			Text:          text,
			TotalCommands: totalCommands,
			CurrentIndex:  currentIndex,
			Unsafe:        unsafe,
			Database:      db,
		}

		if f, ok := cmdMap[cmd]; ok {
			log.Info().Msgf("launching command: %s", cmd)
			res, err := f(pl, env)

			if err == nil && res.MediaChanged && t.Source != tokens.SourcePlaylist {
				log.Debug().Any("token", t).Msg("cmd launch: clearing current playlist")
				plsc.Queue <- nil
			}

			return res, err
		} else {
			return platforms.CmdResult{}, fmt.Errorf("unknown command: %s", cmd)
		}
	}

	// if it's not a command, treat it as a generic launch command
	res, err := cmdLaunch(pl, platforms.CmdEnv{
		Cmd:           "launch",
		Args:          text,
		NamedArgs:     namedArgs,
		Cfg:           cfg,
		Text:          text,
		TotalCommands: totalCommands,
		CurrentIndex:  currentIndex,
		Unsafe:        unsafe,
		Database:      db,
	})

	if err == nil && res.MediaChanged && t.Source != tokens.SourcePlaylist {
		log.Debug().Msg("generic launch: clearing current playlist")
		plsc.Queue <- nil
	}

	return res, err
}
