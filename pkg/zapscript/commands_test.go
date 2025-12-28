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
	"testing"

	"github.com/ZaparooProject/go-zapscript"
	apimodels "github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
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
			cmdName: zapscript.ZapScriptCmdLaunch,
			want:    true,
		},
		{
			name:    "launch.system command",
			cmdName: zapscript.ZapScriptCmdLaunchSystem,
			want:    true,
		},
		{
			name:    "launch.random command",
			cmdName: zapscript.ZapScriptCmdLaunchRandom,
			want:    true,
		},
		{
			name:    "launch.search command",
			cmdName: zapscript.ZapScriptCmdLaunchSearch,
			want:    true,
		},
		{
			name:    "launch.title command",
			cmdName: zapscript.ZapScriptCmdLaunchTitle,
			want:    true,
		},

		// Playlist commands that launch media - should be blocked
		{
			name:    "playlist.play command",
			cmdName: zapscript.ZapScriptCmdPlaylistPlay,
			want:    true,
		},
		{
			name:    "playlist.next command",
			cmdName: zapscript.ZapScriptCmdPlaylistNext,
			want:    true,
		},
		{
			name:    "playlist.previous command",
			cmdName: zapscript.ZapScriptCmdPlaylistPrevious,
			want:    true,
		},
		{
			name:    "playlist.goto command",
			cmdName: zapscript.ZapScriptCmdPlaylistGoto,
			want:    true,
		},
		{
			name:    "playlist.load command",
			cmdName: zapscript.ZapScriptCmdPlaylistLoad,
			want:    true,
		},
		{
			name:    "playlist.open command",
			cmdName: zapscript.ZapScriptCmdPlaylistOpen,
			want:    true,
		},

		// Playlist commands that don't launch media - should NOT be blocked
		{
			name:    "playlist.stop command",
			cmdName: zapscript.ZapScriptCmdPlaylistStop,
			want:    false,
		},
		{
			name:    "playlist.pause command",
			cmdName: zapscript.ZapScriptCmdPlaylistPause,
			want:    false,
		},

		// MiSTer commands
		{
			name:    "mister.mgl command - should be blocked",
			cmdName: zapscript.ZapScriptCmdMisterMGL,
			want:    true,
		},
		{
			name:    "mister.core command - should NOT be blocked",
			cmdName: zapscript.ZapScriptCmdMisterCore,
			want:    false,
		},
		{
			name:    "mister.ini command - should NOT be blocked",
			cmdName: zapscript.ZapScriptCmdMisterINI,
			want:    false,
		},
		{
			name:    "mister.script command - should NOT be blocked",
			cmdName: zapscript.ZapScriptCmdMisterScript,
			want:    false,
		},

		// Utility commands - should NOT be blocked
		{
			name:    "execute command",
			cmdName: zapscript.ZapScriptCmdExecute,
			want:    false,
		},
		{
			name:    "delay command",
			cmdName: zapscript.ZapScriptCmdDelay,
			want:    false,
		},
		{
			name:    "stop command",
			cmdName: zapscript.ZapScriptCmdStop,
			want:    false,
		},
		{
			name:    "echo command",
			cmdName: zapscript.ZapScriptCmdEcho,
			want:    false,
		},

		// HTTP commands - should NOT be blocked
		{
			name:    "http.get command",
			cmdName: zapscript.ZapScriptCmdHTTPGet,
			want:    false,
		},
		{
			name:    "http.post command",
			cmdName: zapscript.ZapScriptCmdHTTPPost,
			want:    false,
		},

		// Input commands - should NOT be blocked
		{
			name:    "input.keyboard command",
			cmdName: zapscript.ZapScriptCmdInputKeyboard,
			want:    false,
		},
		{
			name:    "input.gamepad command",
			cmdName: zapscript.ZapScriptCmdInputGamepad,
			want:    false,
		},
		{
			name:    "input.coinp1 command",
			cmdName: zapscript.ZapScriptCmdInputCoinP1,
			want:    false,
		},
		{
			name:    "input.coinp2 command",
			cmdName: zapscript.ZapScriptCmdInputCoinP2,
			want:    false,
		},

		// Deprecated aliases
		{
			name:    "random (deprecated) - should be blocked",
			cmdName: zapscript.ZapScriptCmdRandom,
			want:    true,
		},
		{
			name:    "system (deprecated) - should be blocked",
			cmdName: zapscript.ZapScriptCmdSystem,
			want:    true,
		},
		{
			name:    "shell (deprecated) - should NOT be blocked",
			cmdName: zapscript.ZapScriptCmdShell,
			want:    false,
		},
		{
			name:    "command (deprecated) - should NOT be blocked",
			cmdName: zapscript.ZapScriptCmdCommand,
			want:    false,
		},
		{
			name:    "ini (deprecated) - should NOT be blocked",
			cmdName: zapscript.ZapScriptCmdINI,
			want:    false,
		},
		{
			name:    "get (deprecated) - should NOT be blocked",
			cmdName: zapscript.ZapScriptCmdGet,
			want:    false,
		},
		{
			name:    "input.key (deprecated) - should NOT be blocked",
			cmdName: zapscript.ZapScriptCmdInputKey,
			want:    false,
		},
		{
			name:    "key (deprecated) - should NOT be blocked",
			cmdName: zapscript.ZapScriptCmdKey,
			want:    false,
		},
		{
			name:    "coinp1 (deprecated) - should NOT be blocked",
			cmdName: zapscript.ZapScriptCmdCoinP1,
			want:    false,
		},
		{
			name:    "coinp2 (deprecated) - should NOT be blocked",
			cmdName: zapscript.ZapScriptCmdCoinP2,
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
		zapscript.ZapScriptCmdLaunch,
		zapscript.ZapScriptCmdLaunchSystem,
		zapscript.ZapScriptCmdLaunchRandom,
		zapscript.ZapScriptCmdLaunchSearch,
		zapscript.ZapScriptCmdLaunchTitle,
		zapscript.ZapScriptCmdPlaylistPlay,
		zapscript.ZapScriptCmdPlaylistNext,
		zapscript.ZapScriptCmdPlaylistPrevious,
		zapscript.ZapScriptCmdPlaylistGoto,
		zapscript.ZapScriptCmdPlaylistLoad,
		zapscript.ZapScriptCmdPlaylistOpen,
		zapscript.ZapScriptCmdMisterMGL,
		zapscript.ZapScriptCmdRandom, // deprecated
		zapscript.ZapScriptCmdSystem, // deprecated
	}

	// Commands that should NOT be blocked
	allowedCommands := []string{
		zapscript.ZapScriptCmdExecute,
		zapscript.ZapScriptCmdDelay,
		zapscript.ZapScriptCmdStop,
		zapscript.ZapScriptCmdEcho,
		zapscript.ZapScriptCmdPlaylistStop,
		zapscript.ZapScriptCmdPlaylistPause,
		zapscript.ZapScriptCmdMisterINI,
		zapscript.ZapScriptCmdMisterCore,
		zapscript.ZapScriptCmdMisterScript,
		zapscript.ZapScriptCmdHTTPGet,
		zapscript.ZapScriptCmdHTTPPost,
		zapscript.ZapScriptCmdInputKeyboard,
		zapscript.ZapScriptCmdInputGamepad,
		zapscript.ZapScriptCmdInputCoinP1,
		zapscript.ZapScriptCmdInputCoinP2,
		zapscript.ZapScriptCmdShell,    // deprecated
		zapscript.ZapScriptCmdCommand,  // deprecated
		zapscript.ZapScriptCmdINI,      // deprecated
		zapscript.ZapScriptCmdGet,      // deprecated
		zapscript.ZapScriptCmdInputKey, // deprecated
		zapscript.ZapScriptCmdKey,      // deprecated
		zapscript.ZapScriptCmdCoinP1,   // deprecated
		zapscript.ZapScriptCmdCoinP2,   // deprecated
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

// TestGetExprEnv_ScannedContext verifies that Scanned fields are populated from ExprEnvOptions.
func TestGetExprEnv_ScannedContext(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("test")

	cfg := &config.Instance{}
	st, _ := state.NewState(mockPlatform, "test-boot-uuid")

	opts := &ExprEnvOptions{
		Scanned: &zapscript.ExprEnvScanned{
			ID:    "scanned-token-id",
			Value: "**launch:/games/sonic.bin",
			Data:  "NDEF-record-data",
		},
	}

	env := getExprEnv(mockPlatform, cfg, st, opts)

	assert.Equal(t, "scanned-token-id", env.Scanned.ID, "Scanned.ID should be populated")
	assert.Equal(t, "**launch:/games/sonic.bin", env.Scanned.Value, "Scanned.Value should be populated")
	assert.Equal(t, "NDEF-record-data", env.Scanned.Data, "Scanned.Data should be populated")
}

// TestGetExprEnv_LaunchingContext verifies that Launching fields are populated from ExprEnvOptions.
func TestGetExprEnv_LaunchingContext(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("test")

	cfg := &config.Instance{}
	st, _ := state.NewState(mockPlatform, "test-boot-uuid")

	opts := &ExprEnvOptions{
		Launching: &zapscript.ExprEnvLaunching{
			Path:       "/games/genesis/sonic.bin",
			SystemID:   "genesis",
			LauncherID: "retroarch",
		},
	}

	env := getExprEnv(mockPlatform, cfg, st, opts)

	assert.Equal(t, "/games/genesis/sonic.bin", env.Launching.Path, "Launching.Path should be populated")
	assert.Equal(t, "genesis", env.Launching.SystemID, "Launching.SystemID should be populated")
	assert.Equal(t, "retroarch", env.Launching.LauncherID, "Launching.LauncherID should be populated")
}

// TestGetExprEnv_NilOpts verifies that nil ExprEnvOptions leaves Scanned/Launching empty.
func TestGetExprEnv_NilOpts(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("test")

	cfg := &config.Instance{}
	st, _ := state.NewState(mockPlatform, "test-boot-uuid")

	env := getExprEnv(mockPlatform, cfg, st, nil)

	assert.Empty(t, env.Scanned.ID, "Scanned.ID should be empty with nil opts")
	assert.Empty(t, env.Scanned.Value, "Scanned.Value should be empty with nil opts")
	assert.Empty(t, env.Scanned.Data, "Scanned.Data should be empty with nil opts")
	assert.Empty(t, env.Launching.Path, "Launching.Path should be empty with nil opts")
	assert.Empty(t, env.Launching.SystemID, "Launching.SystemID should be empty with nil opts")
	assert.Empty(t, env.Launching.LauncherID, "Launching.LauncherID should be empty with nil opts")
}

// TestGetExprEnv_BothContexts verifies both Scanned and Launching can be set simultaneously.
func TestGetExprEnv_BothContexts(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("test")

	cfg := &config.Instance{}
	st, _ := state.NewState(mockPlatform, "test-boot-uuid")

	opts := &ExprEnvOptions{
		Scanned: &zapscript.ExprEnvScanned{
			ID:    "token-123",
			Value: "test-value",
			Data:  "test-data",
		},
		Launching: &zapscript.ExprEnvLaunching{
			Path:       "/path/to/game",
			SystemID:   "snes",
			LauncherID: "mister",
		},
	}

	env := getExprEnv(mockPlatform, cfg, st, opts)

	// Verify Scanned
	assert.Equal(t, "token-123", env.Scanned.ID)
	assert.Equal(t, "test-value", env.Scanned.Value)
	assert.Equal(t, "test-data", env.Scanned.Data)

	// Verify Launching
	assert.Equal(t, "/path/to/game", env.Launching.Path)
	assert.Equal(t, "snes", env.Launching.SystemID)
	assert.Equal(t, "mister", env.Launching.LauncherID)
}

// TestGetExprEnv_ActiveMedia verifies ActiveMedia fields are populated when media is playing.
func TestGetExprEnv_ActiveMedia(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("test")

	cfg := &config.Instance{}
	st, _ := state.NewState(mockPlatform, "test-boot-uuid")

	// Set active media on state
	st.SetActiveMedia(&apimodels.ActiveMedia{
		LauncherID: "retroarch",
		SystemID:   "snes",
		SystemName: "Super Nintendo",
		Path:       "/games/snes/mario.sfc",
		Name:       "Super Mario World",
	})

	env := getExprEnv(mockPlatform, cfg, st, nil)

	assert.True(t, env.MediaPlaying, "MediaPlaying should be true when media is active")
	assert.Equal(t, "retroarch", env.ActiveMedia.LauncherID)
	assert.Equal(t, "snes", env.ActiveMedia.SystemID)
	assert.Equal(t, "Super Nintendo", env.ActiveMedia.SystemName)
	assert.Equal(t, "/games/snes/mario.sfc", env.ActiveMedia.Path)
	assert.Equal(t, "Super Mario World", env.ActiveMedia.Name)
}

// TestGetExprEnv_NoActiveMedia verifies MediaPlaying is false when no media is active.
func TestGetExprEnv_NoActiveMedia(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("test")

	cfg := &config.Instance{}
	st, _ := state.NewState(mockPlatform, "test-boot-uuid")

	env := getExprEnv(mockPlatform, cfg, st, nil)

	assert.False(t, env.MediaPlaying, "MediaPlaying should be false when no media is active")
	assert.Empty(t, env.ActiveMedia.LauncherID)
	assert.Empty(t, env.ActiveMedia.SystemID)
	assert.Empty(t, env.ActiveMedia.Path)
}

func TestIsValidCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cmdName string
		want    bool
	}{
		// Valid commands
		{name: "launch", cmdName: models.ZapScriptCmdLaunch, want: true},
		{name: "launch.system", cmdName: models.ZapScriptCmdLaunchSystem, want: true},
		{name: "launch.random", cmdName: models.ZapScriptCmdLaunchRandom, want: true},
		{name: "launch.search", cmdName: models.ZapScriptCmdLaunchSearch, want: true},
		{name: "launch.title", cmdName: models.ZapScriptCmdLaunchTitle, want: true},
		{name: "playlist.play", cmdName: models.ZapScriptCmdPlaylistPlay, want: true},
		{name: "execute", cmdName: models.ZapScriptCmdExecute, want: true},
		{name: "delay", cmdName: models.ZapScriptCmdDelay, want: true},
		{name: "stop", cmdName: models.ZapScriptCmdStop, want: true},
		{name: "http.get", cmdName: models.ZapScriptCmdHTTPGet, want: true},
		{name: "input.keyboard", cmdName: models.ZapScriptCmdInputKeyboard, want: true},
		// Invalid commands
		{name: "unknown command", cmdName: "unknown.cmd", want: false},
		{name: "empty string", cmdName: "", want: false},
		{name: "typo", cmdName: "launh.system", want: false},
		{name: "random string", cmdName: "foobar", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := IsValidCommand(tt.cmdName)
			assert.Equal(t, tt.want, got, "IsValidCommand(%q) = %v, want %v", tt.cmdName, got, tt.want)
		})
	}
}
