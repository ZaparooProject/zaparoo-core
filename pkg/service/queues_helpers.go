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
	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mediaslot"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	zscript "github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript"
)

// shouldRunBeforeExitHook reports whether before_exit should run before cmd
// executes, i.e. cmd is about to stop the active primary media.
//
// Launch commands are not listed here. They run the hook from inside the launch
// path instead, once the target and launcher have been resolved, so a launch
// that is rejected cannot fire it. **mister.mgl is the exception: it forwards
// to the platform and never reaches that point.
//
// Playlist navigation commands are deliberately excluded: they queue a playlist
// update whose resulting launch command comes back through the launch path, so
// including them here would fire the hook twice. playlist.pause is excluded for
// a different reason: it only stops the launcher when the playback manager has
// no path, and a pause is not an exit either way.
func shouldRunBeforeExitHook(inHookContext bool, cmd zapscript.Command) bool {
	if inHookContext || commandTargetsBackgroundSlot(cmd) {
		return false
	}
	return cmd.Name == zapscript.ZapScriptCmdMisterMGL ||
		commandStopsPrimaryMedia(cmd)
}

// beforeExitCallback returns the hook the launch path invokes once it knows a
// launch is going ahead, or nil inside a hook script so a before_exit script's
// own launch cannot re-enter the hook.
func beforeExitCallback(svc *ServiceContext, inHookContext bool) func() {
	if inHookContext {
		return nil
	}
	return svc.State.RunBeforeExitHook
}

// commandStopsPrimaryMedia reports whether cmd exits the active primary media
// outright rather than replacing it. These are the commands whose stop must be
// skipped when before_exit launched media of its own, the same way HandleStop
// skips it.
func commandStopsPrimaryMedia(cmd zapscript.Command) bool {
	return cmd.Name == zapscript.ZapScriptCmdStop ||
		cmd.Name == zapscript.ZapScriptCmdPlaylistStop
}

// shouldRunBeforeMediaStartHook determines if the before_media_start hook should run.
// Returns true only when:
// - Not already in a hook context (prevents infinite recursion)
// - A hook script is configured (non-empty)
// - The command is a media-launching command
func shouldRunBeforeMediaStartHook(
	inHookContext bool,
	hookScript string,
	cmdName string,
) bool {
	return !inHookContext && hookScript != "" && zscript.IsMediaLaunchingCommand(cmdName)
}

// buildLaunchingContext extracts launching context from a command being launched.
func buildLaunchingContext(cmd zapscript.Command) *zapscript.ExprEnvLaunching {
	launching := &zapscript.ExprEnvLaunching{}

	if len(cmd.Args) > 0 {
		launching.Path = cmd.Args[0]
	}

	if sysID := cmd.AdvArgs.Get(zapscript.KeySystem); sysID != "" {
		launching.SystemID = sysID
	}

	if launcherID := cmd.AdvArgs.Get(zapscript.KeyLauncher); launcherID != "" {
		launching.LauncherID = launcherID
	}

	return launching
}

// scriptHasMediaLaunchingCommand checks if any command in the script launches media.
// Used to determine if playtime limits should be checked.
func scriptHasMediaLaunchingCommand(script *zapscript.Script) bool {
	if script == nil {
		return false
	}
	for _, cmd := range script.Cmds {
		if zscript.IsMediaLaunchingCommand(cmd.Name) {
			return true
		}
	}
	return false
}

// scriptActivatesProfileBeforeLaunch reports whether the script contains a
// profile command before its first media-launching command. Such a
// combo card (profile switch + favorite game in one scan) satisfies the
// require-profile launch gate: by the time the launch command runs, the
// switch will have activated a profile, or failed and aborted the script.
func scriptActivatesProfileBeforeLaunch(script *zapscript.Script) bool {
	if script == nil {
		return false
	}
	for _, cmd := range script.Cmds {
		if cmd.Name == zapscript.ZapScriptCmdProfile {
			return true
		}
		if zscript.IsMediaLaunchingCommand(cmd.Name) {
			return false
		}
	}
	return false
}

// scriptHasMediaDisruptingCommand checks if any command in the script would
// change or stop the currently playing media. Used by launch guard.
func scriptHasMediaDisruptingCommand(script *zapscript.Script) bool {
	if script == nil {
		return false
	}
	for _, cmd := range script.Cmds {
		if !zscript.IsMediaDisruptingCommand(cmd.Name) {
			continue
		}
		if commandTargetsBackgroundSlot(cmd) {
			continue
		}
		return true
	}
	return false
}

func commandTargetsBackgroundSlot(cmd zapscript.Command) bool {
	if !zscript.IsMediaLaunchingCommand(cmd.Name) && !zscript.IsPlaylistCommand(cmd.Name) &&
		cmd.Name != zapscript.ZapScriptCmdStop {
		return false
	}
	slot, err := mediaslot.Normalize(cmd.AdvArgs.Get(zapscript.KeySlot))
	return err == nil && slot == mediaslot.Background
}

// shouldPlayScanSuccessSound reports whether scan feedback should play for the
// script. Suppressed only when the script's media commands all target the
// background slot, so the sound doesn't clash with background music starting; a
// mixed script that also launches primary media keeps normal feedback.
func shouldPlayScanSuccessSound(script *zapscript.Script) bool {
	if script == nil {
		return true
	}
	hasBackground := false
	for _, cmd := range script.Cmds {
		if commandTargetsBackgroundSlot(cmd) {
			hasBackground = true
			continue
		}
		if zscript.IsMediaDisruptingCommand(cmd.Name) {
			return true
		}
	}
	return !hasBackground
}

// injectCommands inserts new commands into the command slice after the given index.
// Returns the updated slice with new commands injected.
func injectCommands(cmds []zapscript.Command, afterIndex int, newCmds []zapscript.Command) []zapscript.Command {
	if len(newCmds) == 0 {
		return cmds
	}
	// Create a new slice to avoid aliasing issues when appending
	result := make([]zapscript.Command, 0, len(cmds)+len(newCmds))
	result = append(result, cmds[:afterIndex+1]...)
	result = append(result, newCmds...)
	result = append(result, cmds[afterIndex+1:]...)
	return result
}

// playlistNeedsUpdate determines if a playlist update requires action.
// Returns false if the current item and playing state are unchanged.
// ForceRelaunch bypasses dedup so the same track can be re-launched (e.g. repeat=one).
func playlistNeedsUpdate(incoming, active *playlists.Playlist) bool {
	if incoming == nil || active == nil {
		return true // nil cases handled separately by caller
	}
	if incoming.ForceRelaunch {
		return true
	}
	// No update needed if current item, playing state, and repeat mode are the same.
	if incoming.Current() == active.Current() && incoming.Playing == active.Playing &&
		incoming.Loop == active.Loop && incoming.LoopOne == active.LoopOne {
		return false
	}
	return true
}
