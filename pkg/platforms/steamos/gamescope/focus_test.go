//go:build linux

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

package gamescope

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsGamingMode_Caching(t *testing.T) {
	// Don't use t.Parallel() - this test modifies global cache state

	// Reset cache before test
	ResetCache()

	// First call should run detection (will return false in test environment)
	result1 := IsGamingMode()

	// Second call should return cached value
	result2 := IsGamingMode()

	// Both should return the same value
	assert.Equal(t, result1, result2)
}

func TestGamescopeDisplay_EmptyWhenNotGamingMode(t *testing.T) {
	// Don't use t.Parallel() - this test modifies global cache state

	// Reset cache before test
	ResetCache()

	// Force detection to run
	mode := IsGamingMode()

	// In test environment (no gamescope), display should be empty
	display := GamescopeDisplay()

	// If not in gaming mode, display must be empty
	if !mode {
		assert.Empty(t, display)
	}
}

func TestFocusManager_RevertIdempotent(t *testing.T) {
	t.Parallel()

	// Create a focus manager with no original layer
	fm := &FocusManager{
		display:       ":0",
		windowID:      "0x123",
		originalLayer: "",
	}

	// Revert should be safe to call multiple times
	fm.Revert()
	fm.Revert()
	fm.Revert()

	// Should be marked as reverted
	assert.True(t, fm.reverted)
}

func TestRevertFocus_SafeWhenNoActiveManager(t *testing.T) {
	// Don't use t.Parallel() - this test modifies global state

	// Clear any active manager
	activeFocusManagerMu.Lock()
	activeFocusManager = nil
	activeFocusManagerMu.Unlock()

	// Should not panic - if we get here, the test passed
	RevertFocus()

	// Verify no manager is active after revert
	activeFocusManagerMu.Lock()
	assert.Nil(t, activeFocusManager)
	activeFocusManagerMu.Unlock()
}

func TestSteamWindowPatterns(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		line    string
		isSteam bool
	}{
		{
			name:    "Steam main window",
			line:    `     0x1234 "Steam": ("steam" "Steam")  640x480+0+0  +0+0`,
			isSteam: true,
		},
		{
			name:    "Steam overlay",
			line:    `     0x1234 "SteamOverlay": ("steamoverlay")  1920x1080+0+0`,
			isSteam: true,
		},
		{
			name:    "Steam web helper",
			line:    `     0x1234 "steamwebhelper": ()  800x600+0+0`,
			isSteam: true,
		},
		{
			name:    "RetroArch game window",
			line:    `     0x5678 "RetroArch": ("retroarch" "RetroArch")  1920x1080+0+0`,
			isSteam: false,
		},
		{
			name:    "Dolphin emulator",
			line:    `     0x9abc "Dolphin": ("dolphin-emu" "Dolphin")  1280x720+0+0`,
			isSteam: false,
		},
		{
			name:    "mangoapp overlay",
			line:    `     0xdef0 "mangoapp overlay window": ()  200x50+10+10`,
			isSteam: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			isSteam := false
			for _, pattern := range steamWindowPatterns {
				if contains(tc.line, pattern) {
					isSteam = true
					break
				}
			}

			assert.Equal(t, tc.isSteam, isSteam)
		})
	}
}

// contains checks if s contains substr (helper for test).
func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestWindowLineRegex(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		line      string
		wantID    string
		wantMatch bool
	}{
		{
			name:      "valid window line",
			line:      `     0x1a00005 "RetroArch": ("retroarch" "RetroArch")  1920x1080+0+0  +0+0`,
			wantMatch: true,
			wantID:    "0x1a00005",
		},
		{
			name:      "window with special chars",
			line:      `     0x2c00003 "Game Title - v1.0": ("game")  1280x720+100+50`,
			wantMatch: true,
			wantID:    "0x2c00003",
		},
		{
			name:      "not a window line",
			line:      `  Parent window id: 0x1234 (the root window)`,
			wantMatch: false,
		},
		{
			name:      "empty line",
			line:      ``,
			wantMatch: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			matches := windowLineRegex.FindStringSubmatch(tc.line)

			if tc.wantMatch {
				assert.NotNil(t, matches, "expected match for: %s", tc.line)
				if matches != nil {
					assert.Equal(t, tc.wantID, matches[1])
				}
			} else {
				assert.Nil(t, matches, "expected no match for: %s", tc.line)
			}
		})
	}
}
