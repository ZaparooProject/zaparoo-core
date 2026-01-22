//go:build windows

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

package windows

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPluginEventJSONSerialization(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		jsonStr  string
		expected pluginEvent
	}{
		{
			name: "MediaStarted event",
			jsonStr: `{"Event":"MediaStarted","Id":"abc123","Title":"Test Game",` +
				`"Platform":"Nintendo Entertainment System","ApplicationPath":"C:\\Games\\game.nes"}`,
			expected: pluginEvent{
				Event:           "MediaStarted",
				ID:              "abc123",
				Title:           "Test Game",
				Platform:        "Nintendo Entertainment System",
				ApplicationPath: `C:\Games\game.nes`,
			},
		},
		{
			name:    "MediaStopped event",
			jsonStr: `{"Event":"MediaStopped","Id":"abc123","Title":"Test Game"}`,
			expected: pluginEvent{
				Event: "MediaStopped",
				ID:    "abc123",
				Title: "Test Game",
			},
		},
		{
			name:    "Write event",
			jsonStr: `{"Event":"Write","Id":"abc123","Title":"Test Game","Platform":"Nintendo Entertainment System"}`,
			expected: pluginEvent{
				Event:    "Write",
				ID:       "abc123",
				Title:    "Test Game",
				Platform: "Nintendo Entertainment System",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var event pluginEvent
			err := json.Unmarshal([]byte(tt.jsonStr), &event)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, event)
		})
	}
}

func TestPluginCommandJSONSerialization(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		command  pluginCommand
		expected string
	}{
		{
			name: "Launch command",
			command: pluginCommand{
				Command: "Launch",
				ID:      "abc123",
			},
			expected: `{"Command":"Launch","Id":"abc123"}`,
		},
		{
			name: "Ping command",
			command: pluginCommand{
				Command: "Ping",
			},
			expected: `{"Command":"Ping"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			data, err := json.Marshal(tt.command)
			require.NoError(t, err)
			assert.JSONEq(t, tt.expected, string(data))
		})
	}
}

func TestLaunchBoxPlatformMapping(t *testing.T) {
	t.Parallel()

	tests := []struct {
		systemID         string
		expectedPlatform string
		exists           bool
	}{
		{systemdefs.SystemNES, "Nintendo Entertainment System", true},
		{systemdefs.SystemSNES, "Super Nintendo Entertainment System", true},
		{systemdefs.SystemGenesis, "Sega Genesis", true},
		{systemdefs.SystemPSX, "Sony Playstation", true},
		{systemdefs.SystemGameboy, "Nintendo Game Boy", true},
		{systemdefs.SystemGBA, "Nintendo Game Boy Advance", true},
		{systemdefs.SystemNintendo64, "Nintendo 64", true},
		{systemdefs.SystemPC, "Windows", true},
		{"nonexistent-system", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.systemID, func(t *testing.T) {
			t.Parallel()

			platform, exists := lbSysMap[tt.systemID]
			assert.Equal(t, tt.exists, exists)
			if exists {
				assert.Equal(t, tt.expectedPlatform, platform)
			}
		})
	}
}

func TestLaunchBoxPlatformMappingReverse(t *testing.T) {
	t.Parallel()

	// Build reverse map
	lbSysMapReverse := make(map[string]string, len(lbSysMap))
	for sysID, lbName := range lbSysMap {
		lbSysMapReverse[lbName] = sysID
	}

	tests := []struct {
		platform      string
		expectedSysID string
		exists        bool
	}{
		{"Nintendo Entertainment System", systemdefs.SystemNES, true},
		{"Super Nintendo Entertainment System", systemdefs.SystemSNES, true},
		{"Sega Genesis", systemdefs.SystemGenesis, true},
		{"Sony Playstation", systemdefs.SystemPSX, true},
		{"Windows", systemdefs.SystemPC, true},
		{"Nonexistent Platform", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.platform, func(t *testing.T) {
			t.Parallel()

			sysID, exists := lbSysMapReverse[tt.platform]
			assert.Equal(t, tt.exists, exists)
			if exists {
				assert.Equal(t, tt.expectedSysID, sysID)
			}
		})
	}
}

func TestLaunchBoxPipeServerIsConnected(t *testing.T) {
	t.Parallel()

	server := NewLaunchBoxPipeServer()
	assert.NotNil(t, server)
	assert.False(t, server.IsConnected())
}

func TestLaunchBoxPipeServerLaunchGameNotConnected(t *testing.T) {
	t.Parallel()

	server := NewLaunchBoxPipeServer()
	err := server.LaunchGame("test-id")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not connected")
}

func TestLaunchBoxPlatformsEventJSONDeserialization(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		jsonStr  string
		expected launchBoxPlatformsEvent
	}{
		{
			name: "single platform with ScrapeAs",
			jsonStr: `{"Event":"Platforms","Platforms":[` +
				`{"Name":"Mame Arcade","ScrapeAs":"Arcade"}]}`,
			expected: launchBoxPlatformsEvent{
				Event: "Platforms",
				Platforms: []launchBoxPlatformInfo{
					{Name: "Mame Arcade", ScrapeAs: "Arcade"},
				},
			},
		},
		{
			name: "multiple platforms",
			jsonStr: `{"Event":"Platforms","Platforms":[` +
				`{"Name":"Nintendo Entertainment System","ScrapeAs":"Nintendo Entertainment System"},` +
				`{"Name":"My SNES Games","ScrapeAs":"Super Nintendo Entertainment System"},` +
				`{"Name":"Arcade","ScrapeAs":"Arcade"}]}`,
			expected: launchBoxPlatformsEvent{
				Event: "Platforms",
				Platforms: []launchBoxPlatformInfo{
					{Name: "Nintendo Entertainment System", ScrapeAs: "Nintendo Entertainment System"},
					{Name: "My SNES Games", ScrapeAs: "Super Nintendo Entertainment System"},
					{Name: "Arcade", ScrapeAs: "Arcade"},
				},
			},
		},
		{
			name:    "empty platforms list",
			jsonStr: `{"Event":"Platforms","Platforms":[]}`,
			expected: launchBoxPlatformsEvent{
				Event:     "Platforms",
				Platforms: []launchBoxPlatformInfo{},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var event launchBoxPlatformsEvent
			err := json.Unmarshal([]byte(tt.jsonStr), &event)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, event)
		})
	}
}

func TestGetPlatformsCommandJSONSerialization(t *testing.T) {
	t.Parallel()

	cmd := pluginCommand{
		Command: "GetPlatforms",
	}

	data, err := json.Marshal(cmd)
	require.NoError(t, err)
	assert.JSONEq(t, `{"Command":"GetPlatforms"}`, string(data))
}

func TestLaunchBoxPipeServerRequestPlatformsNotConnected(t *testing.T) {
	t.Parallel()

	server := NewLaunchBoxPipeServer()
	err := server.RequestPlatforms()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not connected")
}

func TestBuildPlatformMappingsFromPluginData(t *testing.T) {
	t.Parallel()

	tests := []struct {
		platforms                  []launchBoxPlatformInfo
		name                       string
		expectedCustomToSystem     map[string]string
		expectedSystemToCustoms    map[string][]string
		expectedCustomToSystemLen  int
		expectedSystemToCustomsLen int
	}{
		{
			name: "custom platform name with ScrapeAs",
			platforms: []launchBoxPlatformInfo{
				{Name: "Mame Arcade", ScrapeAs: "Arcade"},
			},
			expectedCustomToSystem: map[string]string{
				"Mame Arcade": systemdefs.SystemArcade,
			},
			expectedSystemToCustoms: map[string][]string{
				systemdefs.SystemArcade: {"Mame Arcade"},
			},
			expectedCustomToSystemLen:  1,
			expectedSystemToCustomsLen: 1,
		},
		{
			name: "standard platform name matches canonical",
			platforms: []launchBoxPlatformInfo{
				{Name: "Arcade", ScrapeAs: "Arcade"},
			},
			expectedCustomToSystem: map[string]string{
				"Arcade": systemdefs.SystemArcade,
			},
			// No reverse mapping when name matches canonical
			expectedSystemToCustoms:    map[string][]string{},
			expectedCustomToSystemLen:  1,
			expectedSystemToCustomsLen: 0,
		},
		{
			name: "multiple custom platforms for different systems",
			platforms: []launchBoxPlatformInfo{
				{Name: "Mame Arcade", ScrapeAs: "Arcade"},
				{Name: "My NES Collection", ScrapeAs: "Nintendo Entertainment System"},
				{Name: "Super Nintendo Entertainment System", ScrapeAs: "Super Nintendo Entertainment System"},
			},
			expectedCustomToSystem: map[string]string{
				"Mame Arcade":                         systemdefs.SystemArcade,
				"My NES Collection":                   systemdefs.SystemNES,
				"Super Nintendo Entertainment System": systemdefs.SystemSNES,
			},
			expectedSystemToCustoms: map[string][]string{
				systemdefs.SystemArcade: {"Mame Arcade"},
				systemdefs.SystemNES:    {"My NES Collection"},
				// SNES not in reverse map because name matches canonical
			},
			expectedCustomToSystemLen:  3,
			expectedSystemToCustomsLen: 2,
		},
		{
			name: "multiple custom platforms mapping to same system",
			platforms: []launchBoxPlatformInfo{
				{Name: "SNES Hacks", ScrapeAs: "Super Nintendo Entertainment System"},
				{Name: "SNES Romhacks", ScrapeAs: "Super Nintendo Entertainment System"},
				{Name: "SNES Translations", ScrapeAs: "Super Nintendo Entertainment System"},
			},
			expectedCustomToSystem: map[string]string{
				"SNES Hacks":        systemdefs.SystemSNES,
				"SNES Romhacks":     systemdefs.SystemSNES,
				"SNES Translations": systemdefs.SystemSNES,
			},
			expectedSystemToCustoms: map[string][]string{
				systemdefs.SystemSNES: {"SNES Hacks", "SNES Romhacks", "SNES Translations"},
			},
			expectedCustomToSystemLen:  3,
			expectedSystemToCustomsLen: 1,
		},
		{
			name: "unknown ScrapeAs value",
			platforms: []launchBoxPlatformInfo{
				{Name: "My Custom Platform", ScrapeAs: "Unknown Platform That Does Not Exist"},
			},
			expectedCustomToSystem:     map[string]string{},
			expectedSystemToCustoms:    map[string][]string{},
			expectedCustomToSystemLen:  0,
			expectedSystemToCustomsLen: 0,
		},
		{
			name: "empty ScrapeAs falls back to Name",
			platforms: []launchBoxPlatformInfo{
				{Name: "Arcade", ScrapeAs: ""},
			},
			expectedCustomToSystem: map[string]string{
				"Arcade": systemdefs.SystemArcade,
			},
			expectedSystemToCustoms:    map[string][]string{},
			expectedCustomToSystemLen:  1,
			expectedSystemToCustomsLen: 0,
		},
		{
			name: "case insensitive matching",
			platforms: []launchBoxPlatformInfo{
				{Name: "My Arcade Games", ScrapeAs: "arcade"}, // lowercase
			},
			expectedCustomToSystem: map[string]string{
				"My Arcade Games": systemdefs.SystemArcade,
			},
			expectedSystemToCustoms: map[string][]string{
				systemdefs.SystemArcade: {"My Arcade Games"},
			},
			expectedCustomToSystemLen:  1,
			expectedSystemToCustomsLen: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Simulate the mapping building logic from initLaunchBoxPipe
			customPlatformToSystem := make(map[string]string)
			systemToCustomPlatforms := make(map[string][]string)

			for _, plat := range tt.platforms {
				canonicalName := plat.ScrapeAs
				if canonicalName == "" {
					canonicalName = plat.Name
				}

				for sysID, lbName := range lbSysMap {
					if strings.EqualFold(lbName, canonicalName) {
						customPlatformToSystem[plat.Name] = sysID
						if !strings.EqualFold(plat.Name, lbName) {
							systemToCustomPlatforms[sysID] = append(systemToCustomPlatforms[sysID], plat.Name)
						}
						break
					}
				}
			}

			assert.Len(t, customPlatformToSystem, tt.expectedCustomToSystemLen)
			assert.Len(t, systemToCustomPlatforms, tt.expectedSystemToCustomsLen)

			for name, expectedSysID := range tt.expectedCustomToSystem {
				assert.Equal(t, expectedSysID, customPlatformToSystem[name],
					"customPlatformToSystem[%q] mismatch", name)
			}

			for sysID, expectedNames := range tt.expectedSystemToCustoms {
				assert.ElementsMatch(t, expectedNames, systemToCustomPlatforms[sysID],
					"systemToCustomPlatforms[%q] mismatch", sysID)
			}
		})
	}
}
