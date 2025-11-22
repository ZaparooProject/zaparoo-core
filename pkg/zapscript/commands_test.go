// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
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
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript/models"
	"github.com/stretchr/testify/assert"
)

func TestIsMediaLaunchingCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cmdName string
		want    bool
	}{
		// Launch commands - should be blocked
		{
			name:    "launch command",
			cmdName: models.ZapScriptCmdLaunch,
			want:    true,
		},
		{
			name:    "launch.system command",
			cmdName: models.ZapScriptCmdLaunchSystem,
			want:    true,
		},
		{
			name:    "launch.random command",
			cmdName: models.ZapScriptCmdLaunchRandom,
			want:    true,
		},
		{
			name:    "launch.search command",
			cmdName: models.ZapScriptCmdLaunchSearch,
			want:    true,
		},
		{
			name:    "launch.title command",
			cmdName: models.ZapScriptCmdLaunchTitle,
			want:    true,
		},

		// Playlist commands that launch media - should be blocked
		{
			name:    "playlist.play command",
			cmdName: models.ZapScriptCmdPlaylistPlay,
			want:    true,
		},
		{
			name:    "playlist.next command",
			cmdName: models.ZapScriptCmdPlaylistNext,
			want:    true,
		},
		{
			name:    "playlist.previous command",
			cmdName: models.ZapScriptCmdPlaylistPrevious,
			want:    true,
		},
		{
			name:    "playlist.goto command",
			cmdName: models.ZapScriptCmdPlaylistGoto,
			want:    true,
		},
		{
			name:    "playlist.load command",
			cmdName: models.ZapScriptCmdPlaylistLoad,
			want:    true,
		},
		{
			name:    "playlist.open command",
			cmdName: models.ZapScriptCmdPlaylistOpen,
			want:    true,
		},

		// Playlist commands that don't launch media - should NOT be blocked
		{
			name:    "playlist.stop command",
			cmdName: models.ZapScriptCmdPlaylistStop,
			want:    false,
		},
		{
			name:    "playlist.pause command",
			cmdName: models.ZapScriptCmdPlaylistPause,
			want:    false,
		},

		// MiSTer commands
		{
			name:    "mister.mgl command - should be blocked",
			cmdName: models.ZapScriptCmdMisterMGL,
			want:    true,
		},
		{
			name:    "mister.core command - should NOT be blocked",
			cmdName: models.ZapScriptCmdMisterCore,
			want:    false,
		},
		{
			name:    "mister.ini command - should NOT be blocked",
			cmdName: models.ZapScriptCmdMisterINI,
			want:    false,
		},
		{
			name:    "mister.script command - should NOT be blocked",
			cmdName: models.ZapScriptCmdMisterScript,
			want:    false,
		},

		// Utility commands - should NOT be blocked
		{
			name:    "execute command",
			cmdName: models.ZapScriptCmdExecute,
			want:    false,
		},
		{
			name:    "delay command",
			cmdName: models.ZapScriptCmdDelay,
			want:    false,
		},
		{
			name:    "stop command",
			cmdName: models.ZapScriptCmdStop,
			want:    false,
		},
		{
			name:    "echo command",
			cmdName: models.ZapScriptCmdEcho,
			want:    false,
		},

		// HTTP commands - should NOT be blocked
		{
			name:    "http.get command",
			cmdName: models.ZapScriptCmdHTTPGet,
			want:    false,
		},
		{
			name:    "http.post command",
			cmdName: models.ZapScriptCmdHTTPPost,
			want:    false,
		},

		// Input commands - should NOT be blocked
		{
			name:    "input.keyboard command",
			cmdName: models.ZapScriptCmdInputKeyboard,
			want:    false,
		},
		{
			name:    "input.gamepad command",
			cmdName: models.ZapScriptCmdInputGamepad,
			want:    false,
		},
		{
			name:    "input.coinp1 command",
			cmdName: models.ZapScriptCmdInputCoinP1,
			want:    false,
		},
		{
			name:    "input.coinp2 command",
			cmdName: models.ZapScriptCmdInputCoinP2,
			want:    false,
		},

		// Deprecated aliases
		{
			name:    "random (deprecated) - should be blocked",
			cmdName: models.ZapScriptCmdRandom,
			want:    true,
		},
		{
			name:    "system (deprecated) - should be blocked",
			cmdName: models.ZapScriptCmdSystem,
			want:    true,
		},
		{
			name:    "shell (deprecated) - should NOT be blocked",
			cmdName: models.ZapScriptCmdShell,
			want:    false,
		},
		{
			name:    "command (deprecated) - should NOT be blocked",
			cmdName: models.ZapScriptCmdCommand,
			want:    false,
		},
		{
			name:    "ini (deprecated) - should NOT be blocked",
			cmdName: models.ZapScriptCmdINI,
			want:    false,
		},
		{
			name:    "get (deprecated) - should NOT be blocked",
			cmdName: models.ZapScriptCmdGet,
			want:    false,
		},
		{
			name:    "input.key (deprecated) - should NOT be blocked",
			cmdName: models.ZapScriptCmdInputKey,
			want:    false,
		},
		{
			name:    "key (deprecated) - should NOT be blocked",
			cmdName: models.ZapScriptCmdKey,
			want:    false,
		},
		{
			name:    "coinp1 (deprecated) - should NOT be blocked",
			cmdName: models.ZapScriptCmdCoinP1,
			want:    false,
		},
		{
			name:    "coinp2 (deprecated) - should NOT be blocked",
			cmdName: models.ZapScriptCmdCoinP2,
			want:    false,
		},

		// Unknown command - should NOT be blocked
		{
			name:    "unknown command",
			cmdName: "unknown.command",
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := IsMediaLaunchingCommand(tt.cmdName)
			assert.Equal(t, tt.want, got, "IsMediaLaunchingCommand(%q) = %v, want %v", tt.cmdName, got, tt.want)
		})
	}
}

// TestIsMediaLaunchingCommand_ComprehensiveCoverage ensures all known commands are tested
func TestIsMediaLaunchingCommand_ComprehensiveCoverage(t *testing.T) {
	t.Parallel()

	// Commands that SHOULD be blocked
	blockedCommands := []string{
		models.ZapScriptCmdLaunch,
		models.ZapScriptCmdLaunchSystem,
		models.ZapScriptCmdLaunchRandom,
		models.ZapScriptCmdLaunchSearch,
		models.ZapScriptCmdLaunchTitle,
		models.ZapScriptCmdPlaylistPlay,
		models.ZapScriptCmdPlaylistNext,
		models.ZapScriptCmdPlaylistPrevious,
		models.ZapScriptCmdPlaylistGoto,
		models.ZapScriptCmdPlaylistLoad,
		models.ZapScriptCmdPlaylistOpen,
		models.ZapScriptCmdMisterMGL,
		models.ZapScriptCmdRandom, // deprecated
		models.ZapScriptCmdSystem, // deprecated
	}

	// Commands that should NOT be blocked
	allowedCommands := []string{
		models.ZapScriptCmdExecute,
		models.ZapScriptCmdDelay,
		models.ZapScriptCmdStop,
		models.ZapScriptCmdEcho,
		models.ZapScriptCmdPlaylistStop,
		models.ZapScriptCmdPlaylistPause,
		models.ZapScriptCmdMisterINI,
		models.ZapScriptCmdMisterCore,
		models.ZapScriptCmdMisterScript,
		models.ZapScriptCmdHTTPGet,
		models.ZapScriptCmdHTTPPost,
		models.ZapScriptCmdInputKeyboard,
		models.ZapScriptCmdInputGamepad,
		models.ZapScriptCmdInputCoinP1,
		models.ZapScriptCmdInputCoinP2,
		models.ZapScriptCmdShell,    // deprecated
		models.ZapScriptCmdCommand,  // deprecated
		models.ZapScriptCmdINI,      // deprecated
		models.ZapScriptCmdGet,      // deprecated
		models.ZapScriptCmdInputKey, // deprecated
		models.ZapScriptCmdKey,      // deprecated
		models.ZapScriptCmdCoinP1,   // deprecated
		models.ZapScriptCmdCoinP2,   // deprecated
	}

	// Verify all blocked commands return true
	for _, cmd := range blockedCommands {
		assert.True(t, IsMediaLaunchingCommand(cmd),
			"Command %q should be blocked by playtime limits", cmd)
	}

	// Verify all allowed commands return false
	for _, cmd := range allowedCommands {
		assert.False(t, IsMediaLaunchingCommand(cmd),
			"Command %q should NOT be blocked by playtime limits", cmd)
	}
}
