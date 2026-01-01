//go:build linux

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

package gamescope

import (
	"errors"
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
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
				if strings.Contains(tc.line, pattern) {
					isSteam = true
					break
				}
			}

			assert.Equal(t, tc.isSteam, isSteam)
		})
	}
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

func TestParseBaselayerOutput(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		output string
		want   string
	}{
		{
			name:   "typical gamescope output",
			output: "GAMESCOPECTRL_BASELAYER_APPID(CARDINAL) = 769, 0",
			want:   "769, 0",
		},
		{
			name:   "single value",
			output: "GAMESCOPECTRL_BASELAYER_APPID(CARDINAL) = 1",
			want:   "1",
		},
		{
			name:   "property not set",
			output: "GAMESCOPECTRL_BASELAYER_APPID:  not found.",
			want:   "",
		},
		{
			name:   "empty output",
			output: "",
			want:   "",
		},
		{
			name:   "value with spaces",
			output: "GAMESCOPECTRL_BASELAYER_APPID(CARDINAL) =   123, 456  ",
			want:   "123, 456",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result := ParseBaselayerOutput(tc.output)
			assert.Equal(t, tc.want, result)
		})
	}
}

func TestBuildBaselayerValue(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		appID    string
		original string
		want     string
	}{
		{
			name:     "no original value",
			appID:    "1",
			original: "",
			want:     "1",
		},
		{
			name:     "with original value",
			appID:    "1",
			original: "769, 0",
			want:     "1, 769, 0",
		},
		{
			name:     "prepends to single value",
			appID:    "1",
			original: "123",
			want:     "1, 123",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result := BuildBaselayerValue(tc.appID, tc.original)
			assert.Equal(t, tc.want, result)
		})
	}
}

func TestParseWindowOutput(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		output   string
		wantID   string
		wantErr  bool
		wantNone bool
	}{
		{
			name: "finds RetroArch window",
			output: `xwininfo: Window id: 0x200001 (has no name)

  Root window id: 0x200001 (the root window) (has no name)
  Parent window id: 0x0 (none)
     0x1a00005 "RetroArch": ("retroarch" "RetroArch")  1920x1080+0+0  +0+0
     0x1234 "Steam": ("steam" "Steam")  640x480+0+0  +0+0`,
			wantID: "0x1a00005",
		},
		{
			name: "skips Steam windows finds game",
			output: `     0x1234 "Steam": ("steam" "Steam")  640x480+0+0
     0x5678 "SteamOverlay": ("steamoverlay")  1920x1080+0+0
     0x9abc "Dolphin": ("dolphin-emu" "Dolphin")  1280x720+0+0`,
			wantID: "0x9abc",
		},
		{
			name: "only Steam windows",
			output: `     0x1234 "Steam": ("steam" "Steam")  640x480+0+0
     0x5678 "SteamOverlay": ("steamoverlay")  1920x1080+0+0
     0x7890 "steamwebhelper": ()  800x600+0+0`,
			wantNone: true,
		},
		{
			name:     "empty output",
			output:   "",
			wantNone: true,
		},
		{
			name: "no valid window lines",
			output: `xwininfo: Window id: 0x200001 (has no name)

  Root window id: 0x200001 (the root window) (has no name)
  Parent window id: 0x0 (none)`,
			wantNone: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			windowID, err := parseWindowOutput(tc.output)

			if tc.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)

			if tc.wantNone {
				assert.Empty(t, windowID)
			} else {
				assert.Equal(t, tc.wantID, windowID)
			}
		})
	}
}

func TestRevertFocus_WithActiveManager(t *testing.T) {
	// Don't use t.Parallel() - modifies global state

	// Set up mock executor
	mockExec := &mocks.MockCommandExecutor{}
	mockExec.On("Run", mock.Anything, mock.AnythingOfType("string"), mock.Anything).Return(nil).Maybe()
	SetExecutor(mockExec)
	defer ResetExecutor()

	// Set up an active focus manager
	fm := &FocusManager{
		display:       ":0",
		windowID:      "0x123",
		originalLayer: "", // Empty so Revert() won't try to call xprop
	}

	activeFocusManagerMu.Lock()
	activeFocusManager = fm
	activeFocusManagerMu.Unlock()

	// Call RevertFocus
	RevertFocus()

	// Verify manager was cleared
	activeFocusManagerMu.Lock()
	assert.Nil(t, activeFocusManager)
	activeFocusManagerMu.Unlock()

	// Verify the manager was reverted
	assert.True(t, fm.reverted)
}

func TestFocusManager_RevertWithOriginalLayer(t *testing.T) {
	// Don't use t.Parallel() - modifies global executor

	// Set up mock executor that succeeds
	mockExec := &mocks.MockCommandExecutor{}
	mockExec.On("Run", mock.Anything, "xprop", mock.Anything).Return(nil)
	SetExecutor(mockExec)
	defer ResetExecutor()

	fm := &FocusManager{
		display:       ":0",
		windowID:      "0x123",
		originalLayer: "769, 0",
	}

	fm.Revert()

	assert.True(t, fm.reverted)
	mockExec.AssertExpectations(t)
}

func TestFocusManager_RevertWithOriginalLayer_CommandFails(t *testing.T) {
	// Don't use t.Parallel() - modifies global executor

	// Set up mock executor that fails
	mockExec := &mocks.MockCommandExecutor{}
	mockExec.On("Run", mock.Anything, "xprop", mock.Anything).Return(errors.New("command failed"))
	SetExecutor(mockExec)
	defer ResetExecutor()

	fm := &FocusManager{
		display:       ":0",
		windowID:      "0x123",
		originalLayer: "769, 0",
	}

	// Should not panic even when command fails
	fm.Revert()

	// Should still be marked as reverted
	assert.True(t, fm.reverted)
	mockExec.AssertExpectations(t)
}

func TestResetCache(t *testing.T) {
	// Don't use t.Parallel() - modifies global state

	// First, ensure cache is in a known state
	ResetCache()

	// Run detection to populate cache
	_ = IsGamingMode()

	// Reset should clear the cache
	ResetCache()

	// Verify state is cleared
	assert.False(t, cachedGamingMode)
	assert.Empty(t, gamescopeDisplay)
}

func TestHasGamescopeAtom_WithMock(t *testing.T) {
	// Don't use t.Parallel() - modifies global executor

	testCases := []struct {
		err        error
		name       string
		output     []byte
		wantResult bool
	}{
		{
			name:       "gamescope detected",
			output:     []byte("GAMESCOPE_XWAYLAND_SERVER_ID(CARDINAL) = 1"),
			err:        nil,
			wantResult: true,
		},
		{
			name:       "no gamescope atom",
			output:     []byte("GAMESCOPE_XWAYLAND_SERVER_ID:  not found."),
			err:        nil,
			wantResult: false,
		},
		{
			name:       "command fails",
			output:     nil,
			err:        errors.New("no display"),
			wantResult: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			mockExec := &mocks.MockCommandExecutor{}
			mockExec.On("Output", mock.Anything, "xprop", mock.Anything).Return(tc.output, tc.err)
			SetExecutor(mockExec)
			defer ResetExecutor()

			result := hasGamescopeAtom(":0")
			assert.Equal(t, tc.wantResult, result)
			mockExec.AssertExpectations(t)
		})
	}
}

func TestSetSteamGameProperty_WithMock(t *testing.T) {
	// Don't use t.Parallel() - modifies global executor

	testCases := []struct {
		err     error
		name    string
		wantErr bool
	}{
		{
			name:    "success",
			err:     nil,
			wantErr: false,
		},
		{
			name:    "command fails",
			err:     errors.New("xprop failed"),
			wantErr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			mockExec := &mocks.MockCommandExecutor{}
			mockExec.On("Run", mock.Anything, "xprop", mock.Anything).Return(tc.err)
			SetExecutor(mockExec)
			defer ResetExecutor()

			err := setSteamGameProperty(":0", "0x123")

			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			mockExec.AssertExpectations(t)
		})
	}
}

func TestSetBaselayerValue_WithMock(t *testing.T) {
	// Don't use t.Parallel() - modifies global executor

	testCases := []struct {
		err      error
		name     string
		appID    string
		original string
		wantErr  bool
	}{
		{
			name:     "success with no original",
			appID:    "1",
			original: "",
			err:      nil,
			wantErr:  false,
		},
		{
			name:     "success with original",
			appID:    "1",
			original: "769, 0",
			err:      nil,
			wantErr:  false,
		},
		{
			name:     "command fails",
			appID:    "1",
			original: "",
			err:      errors.New("xprop failed"),
			wantErr:  true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			mockExec := &mocks.MockCommandExecutor{}
			mockExec.On("Run", mock.Anything, "xprop", mock.Anything).Return(tc.err)
			SetExecutor(mockExec)
			defer ResetExecutor()

			err := setBaselayerValue(":0", tc.appID, tc.original)

			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			mockExec.AssertExpectations(t)
		})
	}
}

func TestGetBaselayerValue_WithMock(t *testing.T) {
	// Don't use t.Parallel() - modifies global executor

	testCases := []struct {
		err       error
		name      string
		wantValue string
		output    []byte
		wantErr   bool
	}{
		{
			name:      "typical gamescope output",
			output:    []byte("GAMESCOPECTRL_BASELAYER_APPID(CARDINAL) = 769, 0"),
			err:       nil,
			wantValue: "769, 0",
			wantErr:   false,
		},
		{
			name:      "property not set",
			output:    []byte("GAMESCOPECTRL_BASELAYER_APPID:  not found."),
			err:       nil,
			wantValue: "",
			wantErr:   false,
		},
		{
			name:      "command fails",
			output:    nil,
			err:       errors.New("xprop failed"),
			wantValue: "",
			wantErr:   true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			mockExec := &mocks.MockCommandExecutor{}
			mockExec.On("Output", mock.Anything, "xprop", mock.Anything).Return(tc.output, tc.err)
			SetExecutor(mockExec)
			defer ResetExecutor()

			value, err := getBaselayerValue(":0")

			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tc.wantValue, value)
			}
			mockExec.AssertExpectations(t)
		})
	}
}

func TestFindNonSteamWindow_WithMock(t *testing.T) {
	// Don't use t.Parallel() - modifies global executor

	testCases := []struct {
		err      error
		name     string
		wantID   string
		output   []byte
		wantErr  bool
		wantNone bool
	}{
		{
			name: "finds game window",
			output: []byte(`     0x1234 "Steam": ("steam" "Steam")  640x480+0+0
     0x5678 "RetroArch": ("retroarch" "RetroArch")  1920x1080+0+0`),
			err:    nil,
			wantID: "0x5678",
		},
		{
			name:    "command fails",
			output:  nil,
			err:     errors.New("xwininfo failed"),
			wantErr: true,
		},
		{
			name:     "no game windows",
			output:   []byte(`     0x1234 "Steam": ("steam" "Steam")  640x480+0+0`),
			err:      nil,
			wantNone: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			mockExec := &mocks.MockCommandExecutor{}
			mockExec.On("Output", mock.Anything, "xwininfo", mock.Anything).Return(tc.output, tc.err)
			SetExecutor(mockExec)
			defer ResetExecutor()

			windowID, err := findNonSteamWindow(":0")

			if tc.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)

			if tc.wantNone {
				assert.Empty(t, windowID)
			} else {
				assert.Equal(t, tc.wantID, windowID)
			}
			mockExec.AssertExpectations(t)
		})
	}
}
