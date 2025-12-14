//go:build linux

/*
Zaparoo Core
Copyright (C) 2024, 2025 Callan Barrett

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

package steamtracker

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseAppIDFromCmdline(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cmdline string
		wantID  int
		wantOK  bool
	}{
		{
			name: "standard_steam_reaper",
			cmdline: "/home/deck/.local/share/Steam/ubuntu12_32/reaper\x00SteamLaunch\x00" +
				"AppId=348550\x00--\x00/home/deck/.steam/steam/steamapps/common/Batman Arkham Knight/BatmanAK.exe",
			wantID: 348550,
			wantOK: true,
		},
		{
			name: "proton_game",
			cmdline: "/home/user/.steam/ubuntu12_32/reaper\x00SteamLaunch\x00" +
				"AppId=1091500\x00--\x00/proton/run\x00game.exe",
			wantID: 1091500,
			wantOK: true,
		},
		{
			name: "native_game",
			cmdline: "/home/user/.local/share/Steam/ubuntu12_32/reaper\x00SteamLaunch\x00" +
				"AppId=250900\x00--\x00/home/user/.steam/steam/steamapps/common/Binding of Isaac Rebirth/isaac.x64",
			wantID: 250900,
			wantOK: true,
		},
		{
			name:    "no_appid",
			cmdline: "/home/user/.steam/ubuntu12_32/reaper\x00SteamLaunch\x00--\x00game.exe",
			wantID:  0,
			wantOK:  false,
		},
		{
			name:    "invalid_appid",
			cmdline: "/home/user/.steam/ubuntu12_32/reaper\x00SteamLaunch\x00AppId=abc\x00--\x00game.exe",
			wantID:  0,
			wantOK:  false,
		},
		{
			name:    "empty_cmdline",
			cmdline: "",
			wantID:  0,
			wantOK:  false,
		},
		{
			name:    "appid_with_spaces",
			cmdline: "reaper SteamLaunch AppId=12345 -- game",
			wantID:  12345,
			wantOK:  true,
		},
		{
			name:    "appid_zero",
			cmdline: "reaper\x00SteamLaunch\x00AppId=0\x00--\x00game",
			wantID:  0,
			wantOK:  true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			appID, ok := parseAppIDFromCmdline(tc.cmdline)

			assert.Equal(t, tc.wantOK, ok)
			if tc.wantOK {
				assert.Equal(t, tc.wantID, appID)
			}
		})
	}
}

func TestScanReaperProcessesWithProcPath(t *testing.T) {
	t.Parallel()

	t.Run("finds_reaper_processes", func(t *testing.T) {
		t.Parallel()

		// Create mock /proc filesystem
		procDir := t.TempDir()

		// Create a mock reaper process
		createMockProcess(t, procDir, 12345, "reaper",
			"/home/user/.steam/ubuntu12_32/reaper\x00SteamLaunch\x00AppId=348550\x00--\x00game.exe")

		// Create a non-reaper process
		createMockProcess(t, procDir, 12346, "bash", "/bin/bash")

		// Create another reaper with different AppID
		createMockProcess(t, procDir, 12347, "reaper",
			"/home/user/.steam/ubuntu12_32/reaper\x00SteamLaunch\x00AppId=250900\x00--\x00isaac.x64")

		reapers, err := ScanReaperProcessesWithProcPath(procDir)

		require.NoError(t, err)
		require.Len(t, reapers, 2)

		// Check that we found both reapers
		appIDs := make(map[int]bool)
		for _, r := range reapers {
			appIDs[r.AppID] = true
		}
		assert.True(t, appIDs[348550])
		assert.True(t, appIDs[250900])
	})

	t.Run("ignores_non_steam_reaper", func(t *testing.T) {
		t.Parallel()

		procDir := t.TempDir()

		// Create a "reaper" process without SteamLaunch
		createMockProcess(t, procDir, 12345, "reaper", "some\x00other\x00reaper\x00AppId=12345")

		reapers, err := ScanReaperProcessesWithProcPath(procDir)

		require.NoError(t, err)
		assert.Empty(t, reapers)
	})

	t.Run("handles_empty_proc", func(t *testing.T) {
		t.Parallel()

		procDir := t.TempDir()

		reapers, err := ScanReaperProcessesWithProcPath(procDir)

		require.NoError(t, err)
		assert.Empty(t, reapers)
	})

	t.Run("handles_nonexistent_path", func(t *testing.T) {
		t.Parallel()

		_, err := ScanReaperProcessesWithProcPath("/nonexistent/path")

		require.Error(t, err)
	})

	t.Run("handles_non_numeric_dirs", func(t *testing.T) {
		t.Parallel()

		procDir := t.TempDir()

		// Create some non-numeric directories that should be ignored
		//nolint:gosec // G301: test directory permissions are fine
		require.NoError(t, os.Mkdir(filepath.Join(procDir, "self"), 0o755))
		//nolint:gosec // G301: test directory permissions are fine
		require.NoError(t, os.Mkdir(filepath.Join(procDir, "sys"), 0o755))
		//nolint:gosec // G301: test directory permissions are fine
		require.NoError(t, os.Mkdir(filepath.Join(procDir, "bus"), 0o755))

		// Create a valid reaper
		createMockProcess(t, procDir, 12345, "reaper",
			"/home/user/.steam/ubuntu12_32/reaper\x00SteamLaunch\x00AppId=100\x00--\x00game")

		reapers, err := ScanReaperProcessesWithProcPath(procDir)

		require.NoError(t, err)
		require.Len(t, reapers, 1)
		assert.Equal(t, 100, reapers[0].AppID)
	})

	t.Run("handles_missing_cmdline", func(t *testing.T) {
		t.Parallel()

		procDir := t.TempDir()

		// Create a process with comm but no cmdline
		pidDir := filepath.Join(procDir, "12345")
		//nolint:gosec // G301: test directory permissions are fine
		require.NoError(t, os.Mkdir(pidDir, 0o755))
		//nolint:gosec // G306: test file permissions are fine
		require.NoError(t, os.WriteFile(filepath.Join(pidDir, "comm"), []byte("reaper\n"), 0o644))
		// No cmdline file

		reapers, err := ScanReaperProcessesWithProcPath(procDir)

		require.NoError(t, err)
		assert.Empty(t, reapers)
	})

	t.Run("handles_reaper_without_appid", func(t *testing.T) {
		t.Parallel()

		procDir := t.TempDir()

		// Create a reaper process without AppId in cmdline
		createMockProcess(t, procDir, 12345, "reaper",
			"/home/user/.steam/ubuntu12_32/reaper\x00SteamLaunch\x00--\x00game.exe")

		reapers, err := ScanReaperProcessesWithProcPath(procDir)

		require.NoError(t, err)
		assert.Empty(t, reapers)
	})
}

// createMockProcess creates a mock /proc/{pid} directory for testing.
func createMockProcess(t *testing.T, procDir string, pid int, comm, cmdline string) {
	t.Helper()

	pidStr := filepath.Join(procDir, formatPID(pid))
	//nolint:gosec // G301: test directory permissions are fine
	require.NoError(t, os.Mkdir(pidStr, 0o755))

	// Write comm file
	//nolint:gosec // G306: test file permissions are fine
	require.NoError(t, os.WriteFile(filepath.Join(pidStr, "comm"), []byte(comm+"\n"), 0o644))

	// Write cmdline file
	//nolint:gosec // G306: test file permissions are fine
	require.NoError(t, os.WriteFile(filepath.Join(pidStr, "cmdline"), []byte(cmdline), 0o644))
}

func formatPID(pid int) string {
	return string([]byte{
		byte('0' + pid/10000%10),
		byte('0' + pid/1000%10),
		byte('0' + pid/100%10),
		byte('0' + pid/10%10),
		byte('0' + pid%10),
	})
}

func TestAppIDRegex(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  string
	}{
		{"AppId=12345", "12345"},
		{"AppId=0", "0"},
		{"AppId=999999999", "999999999"},
		{"something AppId=123 else", "123"},
		{"noAppId", ""},
		{"AppId=", ""},
		{"AppId=abc", ""},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()

			matches := appIDRegex.FindStringSubmatch(tc.input)
			if tc.want == "" {
				assert.True(t, len(matches) < 2 || matches[1] == "")
			} else {
				require.Len(t, matches, 2)
				assert.Equal(t, tc.want, matches[1])
			}
		})
	}
}

func TestParseGamePathFromCmdline(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cmdline string
		want    string
	}{
		{
			name: "standard_steam_cmdline",
			cmdline: "/home/user/.steam/ubuntu12_32/reaper\x00SteamLaunch\x00AppId=348550\x00--\x00" +
				"/home/user/.steam/steam/steamapps/common/BatmanAK/BatmanAK.exe",
			want: "/home/user/.steam/steam/steamapps/common/BatmanAK/BatmanAK.exe",
		},
		{
			name: "proton_with_multiple_dashes",
			cmdline: "/home/user/.steam/ubuntu12_32/reaper\x00SteamLaunch\x00AppId=12345\x00--\x00" +
				"/proton/waitforexitandrun\x00--\x00/games/game.exe",
			want: "/games/game.exe",
		},
		{
			name:    "no_double_dash",
			cmdline: "/home/user/.steam/ubuntu12_32/reaper\x00SteamLaunch\x00AppId=12345\x00game.exe",
			want:    "",
		},
		{
			name:    "double_dash_at_end",
			cmdline: "/home/user/.steam/ubuntu12_32/reaper\x00SteamLaunch\x00--",
			want:    "",
		},
		{
			name:    "empty_cmdline",
			cmdline: "",
			want:    "",
		},
		{
			name: "game_path_with_spaces",
			cmdline: "/home/user/.steam/ubuntu12_32/reaper\x00SteamLaunch\x00AppId=12345\x00--\x00" +
				"/home/user/games/My Game/game.exe",
			want: "/home/user/games/My Game/game.exe",
		},
		{
			name: "trailing_null",
			cmdline: "/home/user/.steam/ubuntu12_32/reaper\x00SteamLaunch\x00AppId=12345\x00--\x00" +
				"/path/to/game.exe\x00",
			want: "/path/to/game.exe",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result := parseGamePathFromCmdline(tc.cmdline)
			assert.Equal(t, tc.want, result)
		})
	}
}

func TestFindGamePIDWithProcPath(t *testing.T) {
	t.Parallel()

	t.Run("finds_exact_match", func(t *testing.T) {
		t.Parallel()

		procDir := t.TempDir()
		gamePath := "/home/user/games/game.exe"

		// Create a process with exact path match
		createMockProcess(t, procDir, 12345, "game", gamePath)

		pid, found := FindGamePIDWithProcPath(procDir, gamePath)

		assert.True(t, found)
		assert.Equal(t, 12345, pid)
	})

	t.Run("falls_back_to_directory_match", func(t *testing.T) {
		t.Parallel()

		procDir := t.TempDir()
		gamePath := "/home/user/games/game.exe"

		// Create a process with different executable in same directory
		createMockProcess(t, procDir, 12345, "launcher", "/home/user/games/launcher.bin")

		pid, found := FindGamePIDWithProcPath(procDir, gamePath)

		assert.True(t, found)
		assert.Equal(t, 12345, pid)
	})

	t.Run("prefers_exact_match_over_fallback", func(t *testing.T) {
		t.Parallel()

		procDir := t.TempDir()
		gamePath := "/home/user/games/game.exe"

		// Create fallback process first
		createMockProcess(t, procDir, 11111, "launcher", "/home/user/games/launcher.bin")
		// Create exact match
		createMockProcess(t, procDir, 22222, "game", gamePath)

		pid, found := FindGamePIDWithProcPath(procDir, gamePath)

		assert.True(t, found)
		assert.Equal(t, 22222, pid)
	})

	t.Run("ignores_reaper_processes", func(t *testing.T) {
		t.Parallel()

		procDir := t.TempDir()
		gamePath := "/home/user/games/game.exe"

		// Create a reaper process
		createMockProcess(t, procDir, 12345, "reaper", gamePath)
		// Create actual game process
		createMockProcess(t, procDir, 12346, "game", gamePath)

		pid, found := FindGamePIDWithProcPath(procDir, gamePath)

		assert.True(t, found)
		assert.Equal(t, 12346, pid) // Should find the non-reaper process
	})

	t.Run("returns_false_for_empty_path", func(t *testing.T) {
		t.Parallel()

		procDir := t.TempDir()

		pid, found := FindGamePIDWithProcPath(procDir, "")

		assert.False(t, found)
		assert.Equal(t, 0, pid)
	})

	t.Run("returns_false_when_not_found", func(t *testing.T) {
		t.Parallel()

		procDir := t.TempDir()
		gamePath := "/home/user/games/game.exe"

		// Create unrelated process
		createMockProcess(t, procDir, 12345, "bash", "/bin/bash")

		pid, found := FindGamePIDWithProcPath(procDir, gamePath)

		assert.False(t, found)
		assert.Equal(t, 0, pid)
	})

	t.Run("handles_nonexistent_proc", func(t *testing.T) {
		t.Parallel()

		pid, found := FindGamePIDWithProcPath("/nonexistent/path", "/some/game.exe")

		assert.False(t, found)
		assert.Equal(t, 0, pid)
	})

	t.Run("handles_missing_cmdline_file", func(t *testing.T) {
		t.Parallel()

		procDir := t.TempDir()
		gamePath := "/home/user/games/game.exe"

		// Create a process directory with comm but no cmdline
		pidDir := filepath.Join(procDir, "12345")
		//nolint:gosec // G301: test directory permissions are fine
		require.NoError(t, os.Mkdir(pidDir, 0o755))
		//nolint:gosec // G306: test file permissions are fine
		require.NoError(t, os.WriteFile(filepath.Join(pidDir, "comm"), []byte("game\n"), 0o644))
		// No cmdline file

		pid, found := FindGamePIDWithProcPath(procDir, gamePath)

		assert.False(t, found)
		assert.Equal(t, 0, pid)
	})
}
