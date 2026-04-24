/*
Zaparoo Core
Copyright (c) 2026 The Zaparoo Project Contributors.
SPDX-License-Identifier: GPL-3.0-or-later

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
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript/advargs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript/titles"
	"github.com/rs/zerolog/log"
)

var (
	ErrArgCount     = errors.New("invalid number of arguments")
	ErrRequiredArgs = errors.New("arguments are required")
	ErrRemoteSource = errors.New("cannot run from remote source")
	ErrFileNotFound = errors.New("file not found")
	ErrNoHistory    = errors.New("no play history available")
)

// getLauncherIDs extracts launcher IDs from the platform for validation context.
func getLauncherIDs(pl platforms.Platform, cfg *config.Instance) []string {
	launchers := pl.Launchers(cfg)
	ids := make([]string, len(launchers))
	for i := range launchers {
		ids[i] = launchers[i].ID
	}
	return ids
}

// ParseAdvArgs parses and validates advanced arguments for a command.
// Returns an error if parsing or validation fails.
func ParseAdvArgs[T any](pl platforms.Platform, env *platforms.CmdEnv, dest *T) error {
	ctx := advargs.NewParseContext(getLauncherIDs(pl, env.Cfg))
	if err := advargs.Parse(env.Cmd.AdvArgs.Raw(), dest, ctx); err != nil {
		return fmt.Errorf("failed to parse advanced args: %w", err)
	}
	return nil
}

type cmdFunc = func(platforms.Platform, platforms.CmdEnv) (platforms.CmdResult, error)

var (
	cmdMapOnce sync.Once
	cmdMapVal  map[string]cmdFunc
)

func lookupCmd(name string) (cmdFunc, bool) {
	cmdMapOnce.Do(func() {
		cmdMapVal = map[string]cmdFunc{
			zapscript.ZapScriptCmdLaunch:       cmdLaunch,
			zapscript.ZapScriptCmdLaunchSystem: cmdSystem,
			zapscript.ZapScriptCmdLaunchRandom: cmdRandom,
			zapscript.ZapScriptCmdLaunchSearch: cmdSearch,
			zapscript.ZapScriptCmdLaunchTitle:  cmdTitle,
			zapscript.ZapScriptCmdLaunchLast:   cmdLaunchLast,

			zapscript.ZapScriptCmdPlaylistPlay:     cmdPlaylistPlay,
			zapscript.ZapScriptCmdPlaylistStop:     cmdPlaylistStop,
			zapscript.ZapScriptCmdPlaylistNext:     cmdPlaylistNext,
			zapscript.ZapScriptCmdPlaylistPrevious: cmdPlaylistPrevious,
			zapscript.ZapScriptCmdPlaylistGoto:     cmdPlaylistGoto,
			zapscript.ZapScriptCmdPlaylistPause:    cmdPlaylistPause,
			zapscript.ZapScriptCmdPlaylistLoad:     cmdPlaylistLoad,
			zapscript.ZapScriptCmdPlaylistOpen:     cmdPlaylistOpen,

			zapscript.ZapScriptCmdExecute: cmdExecute,
			zapscript.ZapScriptCmdDelay:   cmdDelay,
			zapscript.ZapScriptCmdStop:    cmdStop,
			zapscript.ZapScriptCmdEcho:    cmdEcho,

			zapscript.ZapScriptCmdControl:    cmdControl,
			zapscript.ZapScriptCmdScreenshot: cmdScreenshot,

			zapscript.ZapScriptCmdMisterINI:       forwardCmd,
			zapscript.ZapScriptCmdMisterCore:      forwardCmd,
			zapscript.ZapScriptCmdMisterScript:    forwardCmd,
			zapscript.ZapScriptCmdMisterMGL:       forwardCmd,
			zapscript.ZapScriptCmdMisterWallpaper: forwardCmd,

			zapscript.ZapScriptCmdHTTPGet:  cmdHTTPGet,
			zapscript.ZapScriptCmdHTTPPost: cmdHTTPPost,

			zapscript.ZapScriptCmdInputKeyboard: cmdKeyboard,
			zapscript.ZapScriptCmdInputGamepad:  cmdGamepad,
			zapscript.ZapScriptCmdInputCoinP1:   cmdCoinP1,
			zapscript.ZapScriptCmdInputCoinP2:   cmdCoinP2,

			zapscript.ZapScriptCmdInputKey: cmdKey,     // DEPRECATED
			zapscript.ZapScriptCmdKey:      cmdKey,     // DEPRECATED
			zapscript.ZapScriptCmdCoinP1:   cmdCoinP1,  // DEPRECATED
			zapscript.ZapScriptCmdCoinP2:   cmdCoinP2,  // DEPRECATED
			zapscript.ZapScriptCmdRandom:   cmdRandom,  // DEPRECATED
			zapscript.ZapScriptCmdShell:    cmdExecute, // DEPRECATED
			zapscript.ZapScriptCmdCommand:  cmdExecute, // DEPRECATED
			zapscript.ZapScriptCmdINI:      forwardCmd, // DEPRECATED
			zapscript.ZapScriptCmdSystem:   cmdSystem,  // DEPRECATED
			zapscript.ZapScriptCmdGet:      cmdHTTPGet, // DEPRECATED
		}
	})
	f, ok := cmdMapVal[name]
	return f, ok
}

// isSensitiveCommand returns true if the command's arguments may contain
// sensitive data (URLs with API keys, keyboard input sequences, etc.) and
// should not be included in log output.
func isSensitiveCommand(cmdName string) bool {
	switch cmdName {
	case zapscript.ZapScriptCmdHTTPGet,
		zapscript.ZapScriptCmdHTTPPost,
		zapscript.ZapScriptCmdInputKeyboard,
		zapscript.ZapScriptCmdInputGamepad,
		zapscript.ZapScriptCmdExecute,
		zapscript.ZapScriptCmdInputKey, // DEPRECATED
		zapscript.ZapScriptCmdKey,      // DEPRECATED
		zapscript.ZapScriptCmdGet,      // DEPRECATED
		zapscript.ZapScriptCmdShell,    // DEPRECATED
		zapscript.ZapScriptCmdCommand:  // DEPRECATED
		return true
	default:
		return false
	}
}

// IsMediaDisruptingCommand returns true if the command would change or stop
// the currently playing media. Used by launch guard to decide whether a token
// should be staged for confirmation.
func IsMediaDisruptingCommand(cmdName string) bool {
	return IsMediaLaunchingCommand(cmdName) || IsPlaylistCommand(cmdName) || cmdName == zapscript.ZapScriptCmdStop
}

// IsMediaLaunchingCommand returns true if the command launches media and should be subject to playtime limits.
func IsMediaLaunchingCommand(cmdName string) bool {
	switch cmdName {
	// Launch commands
	case zapscript.ZapScriptCmdLaunch,
		zapscript.ZapScriptCmdLaunchSystem,
		zapscript.ZapScriptCmdLaunchRandom,
		zapscript.ZapScriptCmdLaunchSearch,
		zapscript.ZapScriptCmdLaunchTitle,
		zapscript.ZapScriptCmdLaunchLast:
		return true

	// MiSTer MGL launches games
	case zapscript.ZapScriptCmdMisterMGL:
		return true

	// Deprecated aliases
	case zapscript.ZapScriptCmdRandom, // alias for launch.random
		zapscript.ZapScriptCmdSystem: // alias for launch.system
		return true

	default:
		return false
	}
}

// IsValidCommand returns true if the command name is a valid ZapScript command.
func IsValidCommand(cmdName string) bool {
	_, ok := lookupCmd(cmdName)
	return ok
}

//nolint:gocritic // single-use parameter in command handler
func forwardCmd(pl platforms.Platform, env platforms.CmdEnv) (platforms.CmdResult, error) {
	result, err := pl.ForwardCmd(&env)
	if err != nil {
		return result, fmt.Errorf("failed to forward command: %w", err)
	}
	return result, nil
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

	return path, fmt.Errorf("%w: %s", ErrFileNotFound, path)
}

func GetExprEnv(
	pl platforms.Platform,
	cfg *config.Instance,
	st *state.State,
	scanned *zapscript.ExprEnvScanned,
	launching *zapscript.ExprEnvLaunching,
) zapscript.ArgExprEnv {
	hostname, err := os.Hostname()
	if err != nil {
		log.Debug().Err(err).Msgf("error getting hostname, continuing")
	}

	lastScanned := st.GetLastScanned()
	activeMedia := st.ActiveMedia()

	env := zapscript.ArgExprEnv{
		Platform: pl.ID(),
		Version:  config.AppVersion,
		ScanMode: strings.ToLower(cfg.ReadersScan().Mode),
		Device: zapscript.ExprEnvDevice{
			Hostname: hostname,
			OS:       runtime.GOOS,
			Arch:     runtime.GOARCH,
		},
		LastScanned: zapscript.ExprEnvLastScanned{
			ID:    lastScanned.UID,
			Value: lastScanned.Text,
			Data:  lastScanned.Data,
		},
		MediaPlaying: activeMedia != nil,
		ActiveMedia:  zapscript.ExprEnvActiveMedia{},
	}

	if activeMedia != nil {
		env.ActiveMedia.LauncherID = activeMedia.LauncherID
		env.ActiveMedia.SystemID = activeMedia.SystemID
		env.ActiveMedia.SystemName = activeMedia.SystemName
		env.ActiveMedia.Path = activeMedia.Path
		env.ActiveMedia.Name = activeMedia.Name
	}

	if scanned != nil {
		env.Scanned = *scanned
	}
	if launching != nil {
		env.Launching = *launching
	}

	return env
}

// RunCommand parses and runs a single ZapScript command.
// The lm parameter is only needed for media-launching commands (launch guard);
// pass nil for contexts where media launches are not allowed (e.g. control scripts).
func RunCommand(
	pl platforms.Platform,
	cfg *config.Instance,
	plsc playlists.PlaylistController,
	token tokens.Token, //nolint:gocritic // single-use parameter in command handler
	cmd zapscript.Command,
	totalCmds int,
	currentIndex int,
	db *database.Database,
	lm *state.LauncherManager,
	exprEnv *zapscript.ArgExprEnv,
) (platforms.CmdResult, error) {
	unsafe := token.Unsafe
	newCmds := make([]zapscript.Command, 0)

	linkValue, err := checkZapLink(cfg, pl, db, cmd)
	if err != nil {
		return platforms.CmdResult{}, fmt.Errorf("zap link error: %w", err)
	}
	if linkValue != "" {
		log.Info().Msgf("valid zap link, replacing cmd: %s", linkValue)
		reader := zapscript.NewParser(linkValue)
		script, parseErr := reader.ParseScript()
		switch {
		case parseErr != nil:
			return platforms.CmdResult{}, fmt.Errorf("error parsing zap link: %w", parseErr)
		case len(script.Cmds) == 0:
			return platforms.CmdResult{}, errors.New("zap link is empty")
		case len(script.Cmds) > 1:
			log.Warn().Msgf("zap link has multiple commands, queueing rest")
			// TODO: this could result in a recursive scan
			newCmds = append(newCmds, script.Cmds[1:]...)
		}

		cmd = script.Cmds[0]
		unsafe = true
	}

	for i, arg := range cmd.Args {
		reader := zapscript.NewParser(arg)
		output, evalErr := reader.EvalExpressions(*exprEnv)
		if evalErr != nil {
			return platforms.CmdResult{}, fmt.Errorf("error evaluating arg expression: %w", evalErr)
		}
		cmd.Args[i] = output
	}

	var advArgEvalErr error
	cmd.AdvArgs.Range(func(k zapscript.Key, arg string) bool {
		reader := zapscript.NewParser(arg)
		output, evalErr := reader.EvalExpressions(*exprEnv)
		if evalErr != nil {
			advArgEvalErr = fmt.Errorf("error evaluating advanced arg expression: %w", evalErr)
			return false
		}
		cmd.AdvArgs = cmd.AdvArgs.With(k, output)
		return true
	})
	if advArgEvalErr != nil {
		return platforms.CmdResult{}, advArgEvalErr
	}

	if when, ok := cmd.AdvArgs.GetWhen(); ok && !helpers.IsTruthy(when) {
		log.Debug().Msgf("skipping command, does not meet when criteria: %s", cmd)
		return platforms.CmdResult{
			Unsafe:      unsafe,
			NewCommands: newCmds,
		}, nil
	}

	env := platforms.CmdEnv{
		Cmd:           cmd,
		Cfg:           cfg,
		Playlist:      plsc,
		Source:        token.Source,
		TotalCommands: totalCmds,
		CurrentIndex:  currentIndex,
		Unsafe:        unsafe,
		Database:      db,
		ExprEnv:       exprEnv,
	}

	if lm != nil {
		env.LauncherCtx = lm.GetContext()
	}

	cmdFn, ok := lookupCmd(cmd.Name)
	if !ok {
		return platforms.CmdResult{}, fmt.Errorf("unknown command: %s", cmd.Name)
	}

	if cfg.IsCommandBlocked(cmd.Name) {
		return platforms.CmdResult{}, fmt.Errorf("command blocked: %s", cmd.Name)
	}

	// Acquire launch guard for media-launching commands to prevent concurrent launches
	if IsMediaLaunchingCommand(cmd.Name) {
		if lm == nil {
			return platforms.CmdResult{}, errors.New("launcher manager required for media-launching commands")
		}
		if guardErr := lm.TryStartLaunch(); guardErr != nil {
			return platforms.CmdResult{}, fmt.Errorf("launch guard: %w", guardErr)
		}
		defer lm.EndLaunch()
		env.LauncherCtx = lm.GetContext()
	}

	logCmd := cmd.String()
	if isSensitiveCommand(cmd.Name) {
		logCmd = cmd.Name
	}

	log.Info().Msgf("running command: %s", logCmd)
	res, err := cmdFn(pl, env)
	if err != nil {
		switch {
		case errors.Is(err, ErrFileNotFound),
			errors.Is(err, titles.ErrNoMatch),
			errors.Is(err, ErrNoControlCapabilities),
			errors.Is(err, ErrNoHistory):
			log.Warn().Err(err).Msgf("error running command: %s", logCmd)
		default:
			log.Error().Err(err).Msgf("error running command: %s", logCmd)
		}
		return platforms.CmdResult{}, err
	}

	res.Unsafe = unsafe
	res.NewCommands = newCmds
	return res, nil
}
