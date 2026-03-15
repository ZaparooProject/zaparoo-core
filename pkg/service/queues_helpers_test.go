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
	"testing"

	gozapscript "github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	"github.com/stretchr/testify/assert"
)

func TestShouldRunBeforeMediaStartHook(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		hookScript    string
		cmdName       string
		inHookContext bool
		expected      bool
	}{
		{
			name:          "runs when all conditions met",
			inHookContext: false,
			hookScript:    "**echo:before launch",
			cmdName:       gozapscript.ZapScriptCmdLaunch,
			expected:      true,
		},
		{
			name:          "runs when not in hook context",
			inHookContext: false,
			hookScript:    "**echo:test",
			cmdName:       gozapscript.ZapScriptCmdLaunch,
			expected:      true,
		},
		{
			name:          "blocked when in hook context",
			inHookContext: true,
			hookScript:    "**echo:test",
			cmdName:       gozapscript.ZapScriptCmdLaunch,
			expected:      false,
		},
		{
			name:          "blocked when hook script empty",
			inHookContext: false,
			hookScript:    "",
			cmdName:       gozapscript.ZapScriptCmdLaunch,
			expected:      false,
		},
		{
			name:          "blocked when command is not media-launching",
			inHookContext: false,
			hookScript:    "**echo:test",
			cmdName:       gozapscript.ZapScriptCmdExecute,
			expected:      false,
		},
		{
			name:          "blocked when command is echo",
			inHookContext: false,
			hookScript:    "**echo:test",
			cmdName:       gozapscript.ZapScriptCmdEcho,
			expected:      false,
		},
		{
			name:          "blocked when command is delay",
			inHookContext: false,
			hookScript:    "**echo:test",
			cmdName:       gozapscript.ZapScriptCmdDelay,
			expected:      false,
		},
		{
			name:          "runs for launch.system command",
			inHookContext: false,
			hookScript:    "**echo:test",
			cmdName:       gozapscript.ZapScriptCmdLaunchSystem,
			expected:      true,
		},
		{
			name:          "runs for launch.random command",
			inHookContext: false,
			hookScript:    "**echo:test",
			cmdName:       gozapscript.ZapScriptCmdLaunchRandom,
			expected:      true,
		},
		{
			name:          "runs for launch.search command",
			inHookContext: false,
			hookScript:    "**echo:test",
			cmdName:       gozapscript.ZapScriptCmdLaunchSearch,
			expected:      true,
		},
		{
			name:          "blocked for playlist.play command (queues state change)",
			inHookContext: false,
			hookScript:    "**echo:test",
			cmdName:       gozapscript.ZapScriptCmdPlaylistPlay,
			expected:      false,
		},
		{
			name:          "blocked for playlist.stop command",
			inHookContext: false,
			hookScript:    "**echo:test",
			cmdName:       gozapscript.ZapScriptCmdPlaylistStop,
			expected:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := shouldRunBeforeMediaStartHook(tt.inHookContext, tt.hookScript, tt.cmdName)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestBuildLaunchingContext(t *testing.T) {
	t.Parallel()

	t.Run("empty command", func(t *testing.T) {
		t.Parallel()

		cmd := gozapscript.Command{
			Name: "launch",
			Args: []string{},
		}

		launching := buildLaunchingContext(cmd)

		assert.NotNil(t, launching)
		assert.Empty(t, launching.Path)
		assert.Empty(t, launching.SystemID)
		assert.Empty(t, launching.LauncherID)
	})

	t.Run("with path only", func(t *testing.T) {
		t.Parallel()

		cmd := gozapscript.Command{
			Name: "launch",
			Args: []string{"/games/snes/mario.sfc"},
		}

		launching := buildLaunchingContext(cmd)

		assert.Equal(t, "/games/snes/mario.sfc", launching.Path)
		assert.Empty(t, launching.SystemID)
		assert.Empty(t, launching.LauncherID)
	})

	t.Run("with system ID", func(t *testing.T) {
		t.Parallel()

		cmd := gozapscript.Command{
			Name:    "launch",
			Args:    []string{"/games/sonic.bin"},
			AdvArgs: gozapscript.NewAdvArgs(map[string]string{"system": "genesis"}),
		}

		launching := buildLaunchingContext(cmd)

		assert.Equal(t, "/games/sonic.bin", launching.Path)
		assert.Equal(t, "genesis", launching.SystemID)
		assert.Empty(t, launching.LauncherID)
	})

	t.Run("with launcher ID", func(t *testing.T) {
		t.Parallel()

		cmd := gozapscript.Command{
			Name:    "launch",
			Args:    []string{"/games/game.rom"},
			AdvArgs: gozapscript.NewAdvArgs(map[string]string{"launcher": "retroarch"}),
		}

		launching := buildLaunchingContext(cmd)

		assert.Equal(t, "/games/game.rom", launching.Path)
		assert.Empty(t, launching.SystemID)
		assert.Equal(t, "retroarch", launching.LauncherID)
	})

	t.Run("with all fields", func(t *testing.T) {
		t.Parallel()

		cmd := gozapscript.Command{
			Name:    "launch",
			Args:    []string{"/roms/snes/zelda.sfc"},
			AdvArgs: gozapscript.NewAdvArgs(map[string]string{"system": "snes", "launcher": "mister"}),
		}

		launching := buildLaunchingContext(cmd)

		assert.Equal(t, "/roms/snes/zelda.sfc", launching.Path)
		assert.Equal(t, "snes", launching.SystemID)
		assert.Equal(t, "mister", launching.LauncherID)
	})

	t.Run("multiple args uses first as path", func(t *testing.T) {
		t.Parallel()

		cmd := gozapscript.Command{
			Name: "launch",
			Args: []string{"/path/to/game.rom", "extra", "args"},
		}

		launching := buildLaunchingContext(cmd)

		assert.Equal(t, "/path/to/game.rom", launching.Path)
	})
}

func TestScriptHasMediaLaunchingCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		script   *gozapscript.Script
		name     string
		expected bool
	}{
		{
			name:     "nil script",
			script:   nil,
			expected: false,
		},
		{
			name:     "empty script",
			script:   &gozapscript.Script{Cmds: []gozapscript.Command{}},
			expected: false,
		},
		{
			name: "only non-launching commands",
			script: &gozapscript.Script{
				Cmds: []gozapscript.Command{
					{Name: gozapscript.ZapScriptCmdEcho},
					{Name: gozapscript.ZapScriptCmdDelay},
					{Name: gozapscript.ZapScriptCmdExecute},
				},
			},
			expected: false,
		},
		{
			name: "has launch command",
			script: &gozapscript.Script{
				Cmds: []gozapscript.Command{
					{Name: gozapscript.ZapScriptCmdLaunch},
				},
			},
			expected: true,
		},
		{
			name: "launch command after other commands",
			script: &gozapscript.Script{
				Cmds: []gozapscript.Command{
					{Name: gozapscript.ZapScriptCmdEcho},
					{Name: gozapscript.ZapScriptCmdDelay},
					{Name: gozapscript.ZapScriptCmdLaunchSystem},
				},
			},
			expected: true,
		},
		{
			name: "playlist.play is not media launching (queues state change)",
			script: &gozapscript.Script{
				Cmds: []gozapscript.Command{
					{Name: gozapscript.ZapScriptCmdPlaylistPlay},
				},
			},
			expected: false,
		},
		{
			name: "playlist.stop is not media launching",
			script: &gozapscript.Script{
				Cmds: []gozapscript.Command{
					{Name: gozapscript.ZapScriptCmdPlaylistStop},
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := scriptHasMediaLaunchingCommand(tt.script)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestInjectCommands(t *testing.T) {
	t.Parallel()

	t.Run("empty new commands returns original", func(t *testing.T) {
		t.Parallel()

		cmds := []gozapscript.Command{
			{Name: "cmd1"},
			{Name: "cmd2"},
		}

		result := injectCommands(cmds, 0, []gozapscript.Command{})

		assert.Equal(t, cmds, result)
	})

	t.Run("inject at beginning", func(t *testing.T) {
		t.Parallel()

		cmds := []gozapscript.Command{
			{Name: "cmd1"},
			{Name: "cmd2"},
		}
		newCmds := []gozapscript.Command{
			{Name: "new1"},
			{Name: "new2"},
		}

		result := injectCommands(cmds, 0, newCmds)

		assert.Len(t, result, 4)
		assert.Equal(t, "cmd1", result[0].Name)
		assert.Equal(t, "new1", result[1].Name)
		assert.Equal(t, "new2", result[2].Name)
		assert.Equal(t, "cmd2", result[3].Name)
	})

	t.Run("inject in middle", func(t *testing.T) {
		t.Parallel()

		cmds := []gozapscript.Command{
			{Name: "cmd1"},
			{Name: "cmd2"},
			{Name: "cmd3"},
		}
		newCmds := []gozapscript.Command{
			{Name: "injected"},
		}

		result := injectCommands(cmds, 1, newCmds)

		assert.Len(t, result, 4)
		assert.Equal(t, "cmd1", result[0].Name)
		assert.Equal(t, "cmd2", result[1].Name)
		assert.Equal(t, "injected", result[2].Name)
		assert.Equal(t, "cmd3", result[3].Name)
	})

	t.Run("inject at end", func(t *testing.T) {
		t.Parallel()

		cmds := []gozapscript.Command{
			{Name: "cmd1"},
			{Name: "cmd2"},
		}
		newCmds := []gozapscript.Command{
			{Name: "appended"},
		}

		result := injectCommands(cmds, 1, newCmds)

		assert.Len(t, result, 3)
		assert.Equal(t, "cmd1", result[0].Name)
		assert.Equal(t, "cmd2", result[1].Name)
		assert.Equal(t, "appended", result[2].Name)
	})
}

func TestPlaylistNeedsUpdate(t *testing.T) {
	t.Parallel()

	// Helper to create playlist with specific state
	makePlaylist := func(currentZapScript string, playing bool) *playlists.Playlist {
		return &playlists.Playlist{
			Items: []playlists.PlaylistItem{
				{ZapScript: currentZapScript},
			},
			Playing: playing,
		}
	}

	t.Run("nil incoming needs update", func(t *testing.T) {
		t.Parallel()

		active := makePlaylist("**launch:game.rom", true)
		result := playlistNeedsUpdate(nil, active)

		assert.True(t, result)
	})

	t.Run("nil active needs update", func(t *testing.T) {
		t.Parallel()

		incoming := makePlaylist("**launch:game.rom", true)
		result := playlistNeedsUpdate(incoming, nil)

		assert.True(t, result)
	})

	t.Run("both nil needs update", func(t *testing.T) {
		t.Parallel()

		result := playlistNeedsUpdate(nil, nil)

		assert.True(t, result)
	})

	t.Run("same state no update needed", func(t *testing.T) {
		t.Parallel()

		incoming := makePlaylist("**launch:game.rom", true)
		active := makePlaylist("**launch:game.rom", true)

		result := playlistNeedsUpdate(incoming, active)

		assert.False(t, result)
	})

	t.Run("different current item needs update", func(t *testing.T) {
		t.Parallel()

		incoming := makePlaylist("**launch:game2.rom", true)
		active := makePlaylist("**launch:game1.rom", true)

		result := playlistNeedsUpdate(incoming, active)

		assert.True(t, result)
	})

	t.Run("different playing state needs update", func(t *testing.T) {
		t.Parallel()

		incoming := makePlaylist("**launch:game.rom", false)
		active := makePlaylist("**launch:game.rom", true)

		result := playlistNeedsUpdate(incoming, active)

		assert.True(t, result)
	})
}
