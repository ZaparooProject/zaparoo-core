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
	"context"
	"errors"
	"fmt"

	gozapscript "github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
)

var ErrControlCommandNotAllowed = errors.New("command not allowed in control context")

// IsPlaylistCommand returns true if the command is a playlist command.
// Playlist commands require a PlaylistController and are not safe in control context.
func IsPlaylistCommand(cmdName string) bool {
	switch cmdName {
	case gozapscript.ZapScriptCmdPlaylistPlay,
		gozapscript.ZapScriptCmdPlaylistStop,
		gozapscript.ZapScriptCmdPlaylistNext,
		gozapscript.ZapScriptCmdPlaylistPrevious,
		gozapscript.ZapScriptCmdPlaylistGoto,
		gozapscript.ZapScriptCmdPlaylistPause,
		gozapscript.ZapScriptCmdPlaylistLoad,
		gozapscript.ZapScriptCmdPlaylistOpen:
		return true
	default:
		return false
	}
}

// IsControlCommand returns true if the command is the control command.
// The control command is blocked in control context to prevent recursion
// where a control's Script invokes another control command.
func IsControlCommand(cmdName string) bool {
	return cmdName == gozapscript.ZapScriptCmdControl
}

// isControlAllowed returns true if the command is safe to run in control context.
func isControlAllowed(cmdName string) bool {
	return !IsMediaLaunchingCommand(cmdName) && !IsPlaylistCommand(cmdName) && !IsControlCommand(cmdName)
}

// RunControlScript parses and executes a zapscript string in control context.
// All commands are validated before any are executed to prevent partial execution.
// The exprEnv is passed directly to each command instead of building from state.
func RunControlScript(
	ctx context.Context,
	pl platforms.Platform,
	cfg *config.Instance,
	db *database.Database,
	script string,
	exprEnv *gozapscript.ArgExprEnv,
) error {
	if ctx == nil {
		ctx = context.Background()
	}

	parser := gozapscript.NewParser(script)
	parsed, err := parser.ParseScript()
	if err != nil {
		return fmt.Errorf("failed to parse control script: %w", err)
	}

	if len(parsed.Cmds) == 0 {
		return errors.New("control script is empty")
	}

	// Validate all commands before executing any
	for _, cmd := range parsed.Cmds {
		if !isControlAllowed(cmd.Name) {
			return fmt.Errorf("%w: %s", ErrControlCommandNotAllowed, cmd.Name)
		}
	}

	token := tokens.Token{
		Text:   script,
		Source: tokens.SourceControl,
	}

	var env gozapscript.ArgExprEnv
	if exprEnv != nil {
		env = *exprEnv
	}

	for i, cmd := range parsed.Cmds {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("control script canceled: %w", err)
		}

		_, err := RunCommand(
			ctx,
			pl, cfg,
			playlists.PlaylistController{},
			token,
			cmd,
			len(parsed.Cmds),
			i,
			db,
			nil, // lm not needed — control commands cannot launch media
			&env,
		)
		if err != nil {
			return fmt.Errorf("control command %q failed: %w", cmd.Name, err)
		}
	}

	return nil
}
