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
	"errors"
	"fmt"
	"github.com/ZaparooProject/zaparoo-core/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/pkg/zapscript/parser"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/pkg/zapscript/models"

	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/pkg/service/playlists"
	"github.com/ZaparooProject/zaparoo-core/pkg/service/tokens"

	"github.com/rs/zerolog/log"

	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
)

var (
	ErrArgCount     = errors.New("invalid number of arguments")
	ErrRequiredArgs = errors.New("arguments are required")
	ErrRemoteSource = errors.New("cannot run from remote source")
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
	models.ZapScriptCmdEcho:    cmdEcho,

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

func getExprEnv(
	pl platforms.Platform,
	cfg *config.Instance,
	st *state.State,
) parser.ExprEnv {
	hostname, err := os.Hostname()
	if err != nil {
		log.Debug().Err(err).Msgf("error getting hostname, continuing")
	}

	lastScanned := st.GetLastScanned()
	activeMedia := st.ActiveMedia()

	env := parser.ExprEnv{
		Platform: pl.ID(),
		Version:  config.AppVersion,
		ScanMode: strings.ToLower(cfg.ReadersScan().Mode),
		Device: parser.ExprEnvDevice{
			Hostname: hostname,
			OS:       runtime.GOOS,
			Arch:     runtime.GOARCH,
		},
		LastScanned: parser.ExprEnvLastScanned{
			ID:    lastScanned.UID,
			Value: lastScanned.Text,
			Data:  lastScanned.Data,
		},
		MediaPlaying: activeMedia != nil,
		ActiveMedia:  parser.ExprEnvActiveMedia{},
	}

	if activeMedia != nil {
		env.ActiveMedia.LauncherID = activeMedia.LauncherID
		env.ActiveMedia.SystemID = activeMedia.SystemID
		env.ActiveMedia.SystemName = activeMedia.SystemName
		env.ActiveMedia.Path = activeMedia.Path
		env.ActiveMedia.Name = activeMedia.Name
	}

	return env
}

// RunCommand parses and runs a single ZapScript command.
func RunCommand(
	pl platforms.Platform,
	cfg *config.Instance,
	plsc playlists.PlaylistController,
	token tokens.Token,
	cmd parser.Command,
	totalCmds int,
	currentIndex int,
	db *database.Database,
	st *state.State,
) (platforms.CmdResult, error) {
	var unsafe bool
	linkValue, err := checkLink(cfg, pl, cmd)
	if err != nil {
		log.Error().Err(err).Msgf("error checking link, continuing")
	} else if linkValue != "" {
		log.Info().Msgf("valid zap link, replacing cmd: %s", linkValue)
		reader := parser.NewParser(linkValue)
		script, err := reader.ParseScript()
		if err != nil {
			return platforms.CmdResult{}, fmt.Errorf("error parsing zap link: %w", err)
		} else if len(script.Cmds) == 0 {
			return platforms.CmdResult{}, fmt.Errorf("zap link is empty")
		} else if len(script.Cmds) > 1 {
			log.Warn().Msgf("zap link has multiple commands, using first: %v", script.Cmds[0])
		}
		cmd = script.Cmds[0]
		unsafe = true
	}

	if token.Unsafe {
		unsafe = true
	}

	if token.Source == tokens.SourcePlaylist {
		// TODO: why not? why did i write this?
		log.Error().Msgf("playlists cannot run commands, skipping")
		return platforms.CmdResult{}, err
	}

	exprEnv := getExprEnv(pl, cfg, st)

	for i, arg := range cmd.Args {
		reader := parser.NewParser(arg)
		output, err := reader.PostProcess(exprEnv)
		if err != nil {
			return platforms.CmdResult{}, fmt.Errorf("error evaluating arg expression: %w", err)
		}
		cmd.Args[i] = output
	}

	for k, arg := range cmd.AdvArgs {
		reader := parser.NewParser(arg)
		output, err := reader.PostProcess(exprEnv)
		if err != nil {
			return platforms.CmdResult{}, fmt.Errorf("error evaluating advanced arg expression: %w", err)
		}
		cmd.AdvArgs[k] = output
	}

	if when, ok := cmd.AdvArgs["when"]; ok {
		if !strings.EqualFold(when, "true") && !strings.EqualFold(when, "yes") {
			log.Debug().Msgf("skipping command, does not meet when criteria: %s", cmd)
			return platforms.CmdResult{}, nil
		}
	}

	env := platforms.CmdEnv{
		Cmd:           cmd,
		Cfg:           cfg,
		Playlist:      plsc,
		TotalCommands: totalCmds,
		CurrentIndex:  currentIndex,
		Unsafe:        unsafe,
		Database:      db,
	}

	if f, ok := cmdMap[cmd.Name]; ok {
		log.Info().Msgf("running command: %s", cmd)
		res, err := f(pl, env)

		if err == nil && res.MediaChanged && token.Source != tokens.SourcePlaylist {
			log.Debug().Any("token", token).Msg("cmd launch: clearing current playlist")
			plsc.Queue <- nil
		}

		return res, err
	} else {
		return platforms.CmdResult{}, fmt.Errorf("unknown command: %s", cmd)
	}
}
