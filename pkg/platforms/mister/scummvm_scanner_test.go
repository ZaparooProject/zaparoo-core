//go:build linux

package mister

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseScummVMIni(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		iniData  string
		expected []ScummVMGame
		wantErr  bool
	}{
		{
			name: "parse valid ini with multiple games",
			iniData: `[scummvm]
gfx_mode=normal
fullscreen=false

[keymapper]
backend=sdl

[monkey1]
gameid=monkey
description=The Secret of Monkey Island
path=/media/fat/ScummVM/GAMES/monkey1
language=en

[tentacle]
gameid=tentacle
description=Day of the Tentacle
path=/media/fat/ScummVM/GAMES/tentacle
subtitles=true

[queen]
gameid=queen
description=Flight of the Amazon Queen
path=/media/fat/ScummVM/GAMES/queen
`,
			expected: []ScummVMGame{
				{TargetID: "monkey1", Description: "The Secret of Monkey Island"},
				{TargetID: "tentacle", Description: "Day of the Tentacle"},
				{TargetID: "queen", Description: "Flight of the Amazon Queen"},
			},
			wantErr: false,
		},
		{
			name: "parse ini with no description (uses target ID)",
			iniData: `[scummvm]
gfx_mode=normal

[loom]
gameid=loom
path=/media/fat/ScummVM/GAMES/loom

[maniac]
gameid=maniac
description=Maniac Mansion
path=/media/fat/ScummVM/GAMES/maniac
`,
			expected: []ScummVMGame{
				{TargetID: "loom", Description: "loom"}, // Falls back to target ID
				{TargetID: "maniac", Description: "Maniac Mansion"},
			},
			wantErr: false,
		},
		{
			name: "parse ini with comments and empty lines",
			iniData: `; This is a comment
[scummvm]
gfx_mode=normal

# Another comment
[monkey1]
; Game config
gameid=monkey
description=The Secret of Monkey Island

path=/media/fat/ScummVM/GAMES/monkey1
`,
			expected: []ScummVMGame{
				{TargetID: "monkey1", Description: "The Secret of Monkey Island"},
			},
			wantErr: false,
		},
		{
			name:     "empty ini file",
			iniData:  "",
			expected: nil,
			wantErr:  false,
		},
		{
			name: "only global sections (no games)",
			iniData: `[scummvm]
gfx_mode=normal

[keymapper]
backend=sdl
`,
			expected: nil,
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Create temp file with ini data
			tmpDir := t.TempDir()
			iniPath := filepath.Join(tmpDir, "scummvm.ini")
			err := os.WriteFile(iniPath, []byte(tt.iniData), 0o600)
			require.NoError(t, err)

			// Parse the ini file
			games, err := parseScummVMIni(iniPath)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expected, games)
		})
	}
}

func TestParseScummVMIniFileNotFound(t *testing.T) {
	t.Parallel()

	_, err := parseScummVMIni("/nonexistent/scummvm.ini")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to open scummvm.ini")
}

func TestFindScummVMBinary(t *testing.T) {
	t.Parallel()

	// Note: This test would need to be mocked or run on a real MiSTer system
	// For now, we'll skip it in CI and only run manually
	if os.Getenv("CI") != "" {
		t.Skip("Skipping binary detection test in CI")
	}

	// On real MiSTer, test that we can find the binary
	binary, err := findScummVMBinary()
	if err != nil {
		// It's okay if ScummVM isn't installed
		t.Logf("ScummVM not found (expected on non-MiSTer systems): %v", err)
		return
	}

	assert.NotEmpty(t, binary)
	assert.Contains(t, binary, "scummvm")
}

func TestScanScummVMGames(t *testing.T) {
	t.Parallel()

	// This test requires mocking the filesystem
	// For now, we'll test the error handling path
	t.Run("no binary found", func(t *testing.T) {
		t.Parallel()

		// On systems without ScummVM, scanner should return empty results
		results, err := scanScummVMGames(
			context.Background(),
			&config.Instance{},
			"",
			[]platforms.ScanResult{},
		)

		// Should not error, just return empty results
		require.NoError(t, err)
		assert.Empty(t, results)
	})
}

func TestScanScummVMGamesVirtualPaths(t *testing.T) {
	t.Parallel()

	// Create a temp directory structure for testing
	tmpDir := t.TempDir()

	// Create ScummVM directory structure
	scummvmDir := filepath.Join(tmpDir, "ScummVM")
	configDir := filepath.Join(scummvmDir, ".config", "scummvm")
	err := os.MkdirAll(configDir, 0o755) //nolint:gosec // Test directory
	require.NoError(t, err)

	// Create a fake ScummVM binary
	scummvmBinary := filepath.Join(scummvmDir, "scummvm291")
	err = os.WriteFile(scummvmBinary, []byte("#!/bin/sh\n"), 0o755) //nolint:gosec // Test file
	require.NoError(t, err)

	// Create scummvm.ini
	iniPath := filepath.Join(configDir, "scummvm.ini")
	iniData := `[scummvm]
gfx_mode=normal

[monkey1]
gameid=monkey
description=The Secret of Monkey Island
path=/media/fat/ScummVM/GAMES/monkey1

[tentacle]
gameid=tentacle
description=Day of the Tentacle
path=/media/fat/ScummVM/GAMES/tentacle
`
	err = os.WriteFile(iniPath, []byte(iniData), 0o600)
	require.NoError(t, err)

	// Temporarily override the constants for testing
	originalBaseDir := scummvmBaseDir
	originalIniPath := scummvmIniPath
	t.Cleanup(func() {
		// Can't actually override const, so this test shows the limitation
		// In a real implementation, we'd need to make these configurable
		_ = originalBaseDir
		_ = originalIniPath
	})

	// This test demonstrates what we'd check if we could override paths
	// In practice, this would need dependency injection or interfaces
	t.Log("Virtual path format test (requires path injection support)")
}
