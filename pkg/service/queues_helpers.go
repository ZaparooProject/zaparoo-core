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

package service

import (
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript"
	advargtypes "github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript/advargs/types"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript/parser"
)

// shouldRunBeforeMediaStartHook determines if the before_media_start hook should run.
// Returns true only when:
// - Not already in a hook context (prevents infinite recursion)
// - A hook script is configured (non-empty)
// - The command is a media-launching command
func shouldRunBeforeMediaStartHook(
	exprOpts *zapscript.ExprEnvOptions,
	hookScript string,
	cmdName string,
) bool {
	inHookContext := exprOpts != nil && exprOpts.InHookContext
	return !inHookContext && hookScript != "" && zapscript.IsMediaLaunchingCommand(cmdName)
}

// buildLaunchingExprOpts creates ExprEnvOptions for the before_media_start hook.
// Extracts path, system ID, and launcher ID from the command being launched.
func buildLaunchingExprOpts(cmd parser.Command) *zapscript.ExprEnvOptions {
	opts := &zapscript.ExprEnvOptions{
		Launching:     &parser.ExprEnvLaunching{},
		InHookContext: true,
	}

	if len(cmd.Args) > 0 {
		opts.Launching.Path = cmd.Args[0]
	}

	if sysID := cmd.AdvArgs.Get(advargtypes.KeySystem); sysID != "" {
		opts.Launching.SystemID = sysID
	}

	if launcherID := cmd.AdvArgs.Get(advargtypes.KeyLauncher); launcherID != "" {
		opts.Launching.LauncherID = launcherID
	}

	return opts
}

// scriptHasMediaLaunchingCommand checks if any command in the script launches media.
// Used to determine if playtime limits should be checked.
func scriptHasMediaLaunchingCommand(script *parser.Script) bool {
	if script == nil {
		return false
	}
	for _, cmd := range script.Cmds {
		if zapscript.IsMediaLaunchingCommand(cmd.Name) {
			return true
		}
	}
	return false
}

// injectCommands inserts new commands into the command slice after the given index.
// Returns the updated slice with new commands injected.
func injectCommands(cmds []parser.Command, afterIndex int, newCmds []parser.Command) []parser.Command {
	if len(newCmds) == 0 {
		return cmds
	}
	// Create a new slice to avoid aliasing issues when appending
	result := make([]parser.Command, 0, len(cmds)+len(newCmds))
	result = append(result, cmds[:afterIndex+1]...)
	result = append(result, newCmds...)
	result = append(result, cmds[afterIndex+1:]...)
	return result
}

// playlistNeedsUpdate determines if a playlist update requires action.
// Returns false if the current item and playing state are unchanged.
func playlistNeedsUpdate(incoming, active *playlists.Playlist) bool {
	if incoming == nil || active == nil {
		return true // nil cases handled separately by caller
	}
	// No update needed if current item and playing state are the same
	if incoming.Current() == active.Current() && incoming.Playing == active.Playing {
		return false
	}
	return true
}
