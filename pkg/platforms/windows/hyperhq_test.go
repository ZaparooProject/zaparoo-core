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
	"bufio"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/virtualpath"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHqEventJSONSerialization(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		jsonStr  string
		expected hqEvent
	}{
		{
			name: "MediaStarted event",
			jsonStr: `{"Event":"MediaStarted","Id":"abc123","Title":"Test Game",` +
				`"Platform":"Nintendo Entertainment System","SystemReferenceId":"sys-nes"}`,
			expected: hqEvent{
				Event:             "MediaStarted",
				ID:                "abc123",
				Title:             "Test Game",
				Platform:          "Nintendo Entertainment System",
				SystemReferenceID: "sys-nes",
			},
		},
		{
			name:    "MediaStopped event",
			jsonStr: `{"Event":"MediaStopped","Id":"abc123","Title":"Test Game"}`,
			expected: hqEvent{
				Event: "MediaStopped",
				ID:    "abc123",
				Title: "Test Game",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var event hqEvent
			err := json.Unmarshal([]byte(tt.jsonStr), &event)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, event)
		})
	}
}

func TestHqCommandJSONSerialization(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		expected string
		command  hqCommand
	}{
		{
			name:     "Launch command",
			command:  hqCommand{Command: "Launch", ID: "abc123"},
			expected: `{"Command":"Launch","Id":"abc123"}`,
		},
		{
			name:     "Ping command",
			command:  hqCommand{Command: "Ping"},
			expected: `{"Command":"Ping"}`,
		},
		{
			name:     "GetSystems command",
			command:  hqCommand{Command: "GetSystems"},
			expected: `{"Command":"GetSystems"}`,
		},
		{
			name: "GetGamesForSystem command",
			command: hqCommand{
				Command:           "GetGamesForSystem",
				SystemReferenceID: "sys-nes",
			},
			expected: `{"Command":"GetGamesForSystem","SystemReferenceId":"sys-nes"}`,
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

func TestHyperHqPlatformMapping(t *testing.T) {
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

			lookup := buildHqSystemLookup()
			if !tt.exists {
				_, exists := lookup[hqSystemLookupKey(tt.systemID)]
				assert.False(t, exists)
				return
			}

			assert.Equal(t, tt.systemID, lookup[hqSystemLookupKey(tt.systemID)])
			assert.Equal(t, tt.systemID, lookup[hqSystemLookupKey(tt.expectedPlatform)])
		})
	}
}

func TestHqSystemsEventJSONDeserialization(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		jsonStr  string
		expected hqSystemsEvent
	}{
		{
			name: "single system",
			jsonStr: `{"Event":"Systems","Systems":[` +
				`{"Name":"Nintendo Entertainment System","ReferenceId":"nes-1",` +
				`"Platform":"Nintendo Entertainment System"}]}`,
			expected: hqSystemsEvent{
				Event: "Systems",
				Systems: []HqSystemInfo{
					{
						Name:        "Nintendo Entertainment System",
						ReferenceID: "nes-1",
						Platform:    "Nintendo Entertainment System",
					},
				},
			},
		},
		{
			name: "multiple systems with custom names",
			jsonStr: `{"Event":"Systems","Systems":[` +
				`{"Name":"Mame Arcade","ReferenceId":"arc-1","Platform":"Arcade"},` +
				`{"Name":"My SNES Games","ReferenceId":"snes-1","Platform":"Super Nintendo Entertainment System"}]}`,
			expected: hqSystemsEvent{
				Event: "Systems",
				Systems: []HqSystemInfo{
					{Name: "Mame Arcade", ReferenceID: "arc-1", Platform: "Arcade"},
					{Name: "My SNES Games", ReferenceID: "snes-1", Platform: "Super Nintendo Entertainment System"},
				},
			},
		},
		{
			name:     "empty systems list",
			jsonStr:  `{"Event":"Systems","Systems":[]}`,
			expected: hqSystemsEvent{Event: "Systems", Systems: []HqSystemInfo{}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var event hqSystemsEvent
			err := json.Unmarshal([]byte(tt.jsonStr), &event)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, event)
		})
	}
}

func TestHqGamesEventJSONDeserialization(t *testing.T) {
	t.Parallel()

	jsonStr := `{"Event":"Games","SystemReferenceId":"nes-1","Games":[` +
		`{"Id":"g1","Title":"Super Mario Bros","Platform":"Nintendo Entertainment System"},` +
		`{"Id":"g2","Title":"Zelda","Platform":"Nintendo Entertainment System"}]}`

	var event hqGamesEvent
	err := json.Unmarshal([]byte(jsonStr), &event)
	require.NoError(t, err)
	assert.Equal(t, "Games", event.Event)
	assert.Equal(t, "nes-1", event.SystemReferenceID)
	assert.Empty(t, event.Error)
	assert.Len(t, event.Games, 2)
	assert.Equal(t, "Super Mario Bros", event.Games[0].Title)
}

func TestHqGamesEventErrorPath(t *testing.T) {
	t.Parallel()

	jsonStr := `{"Event":"Games","SystemReferenceId":"nes-1","Error":"system not found","Games":[]}`

	var event hqGamesEvent
	err := json.Unmarshal([]byte(jsonStr), &event)
	require.NoError(t, err)
	assert.Equal(t, "system not found", event.Error)
	assert.Empty(t, event.Games)
}

func TestHyperHqPipeServerIsConnected(t *testing.T) {
	t.Parallel()

	server := NewHyperHqPipeServer()
	assert.NotNil(t, server)
	assert.False(t, server.IsConnected())
}

func TestHyperHqPipeServerLaunchGameNotConnected(t *testing.T) {
	t.Parallel()

	server := NewHyperHqPipeServer()
	err := server.LaunchGame("test-id")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not connected")
}

func TestHyperHqPipeServerRequestSystemsNotConnected(t *testing.T) {
	t.Parallel()

	server := NewHyperHqPipeServer()
	err := server.RequestSystems()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not connected")
}

func TestShouldIgnoreEmptyHqSystemsRefresh(t *testing.T) {
	t.Parallel()

	assert.False(t, shouldIgnoreEmptyHqSystemsRefresh(nil, nil, nil))
	assert.False(t, shouldIgnoreEmptyHqSystemsRefresh([]HqSystemInfo{{Name: "Arcade"}}, map[string]string{"arcade": systemdefs.SystemArcade}, nil))
	assert.True(t, shouldIgnoreEmptyHqSystemsRefresh(nil, map[string]string{"arcade": systemdefs.SystemArcade}, nil))
	assert.True(t, shouldIgnoreEmptyHqSystemsRefresh(nil, nil, map[string][]hqSystemQueryTarget{
		systemdefs.SystemArcade: {{ReferenceID: "arcade"}},
	}))
}

func TestBuildHqMappings(t *testing.T) {
	t.Parallel()

	tests := []struct {
		expectedKeyToSys    map[string]string
		expectedSystemToHqs map[string][]hqSystemQueryTarget
		name                string
		systems             []HqSystemInfo
	}{
		{
			name: "canonical platform name",
			systems: []HqSystemInfo{
				{Name: "Arcade", ReferenceID: "arc-1", Platform: "Arcade"},
			},
			expectedKeyToSys: map[string]string{"arc-1": systemdefs.SystemArcade},
			expectedSystemToHqs: map[string][]hqSystemQueryTarget{
				systemdefs.SystemArcade: {{ReferenceID: "arc-1"}},
			},
		},
		{
			name: "system id is used for game query and both ids map to system",
			systems: []HqSystemInfo{
				{ID: "sys-nes", Name: "My NES Collection", ReferenceID: "nes-99", Platform: "Nintendo Entertainment System"},
			},
			expectedKeyToSys: map[string]string{
				"sys-nes": systemdefs.SystemNES,
				"nes-99":  systemdefs.SystemNES,
			},
			expectedSystemToHqs: map[string][]hqSystemQueryTarget{
				systemdefs.SystemNES: {{ID: "sys-nes", ReferenceID: "nes-99"}},
			},
		},
		{
			name: "custom system name with canonical Platform",
			systems: []HqSystemInfo{
				{Name: "My NES Collection", ReferenceID: "nes-99", Platform: "Nintendo Entertainment System"},
			},
			expectedKeyToSys: map[string]string{"nes-99": systemdefs.SystemNES},
			expectedSystemToHqs: map[string][]hqSystemQueryTarget{
				systemdefs.SystemNES: {{ReferenceID: "nes-99"}},
			},
		},
		{
			name: "multiple systems mapping to same Zaparoo system",
			systems: []HqSystemInfo{
				{Name: "SNES Hacks", ReferenceID: "snes-h", Platform: "Super Nintendo Entertainment System"},
				{Name: "SNES Romhacks", ReferenceID: "snes-r", Platform: "Super Nintendo Entertainment System"},
			},
			expectedKeyToSys: map[string]string{
				"snes-h": systemdefs.SystemSNES,
				"snes-r": systemdefs.SystemSNES,
			},
			expectedSystemToHqs: map[string][]hqSystemQueryTarget{
				systemdefs.SystemSNES: {{ReferenceID: "snes-h"}, {ReferenceID: "snes-r"}},
			},
		},
		{
			name: "empty Platform falls back to Name",
			systems: []HqSystemInfo{
				{Name: "Arcade", ReferenceID: "arc-2", Platform: ""},
			},
			expectedKeyToSys: map[string]string{"arc-2": systemdefs.SystemArcade},
			expectedSystemToHqs: map[string][]hqSystemQueryTarget{
				systemdefs.SystemArcade: {{ReferenceID: "arc-2"}},
			},
		},
		{
			name: "case insensitive platform matching",
			systems: []HqSystemInfo{
				{Name: "My Arcade", ReferenceID: "arc-3", Platform: "arcade"},
			},
			expectedKeyToSys: map[string]string{"arc-3": systemdefs.SystemArcade},
			expectedSystemToHqs: map[string][]hqSystemQueryTarget{
				systemdefs.SystemArcade: {{ReferenceID: "arc-3"}},
			},
		},
		{
			name: "zaparoo system id platform matches",
			systems: []HqSystemInfo{
				{Name: "NES", ReferenceID: "nes-short", Platform: systemdefs.SystemNES},
			},
			expectedKeyToSys: map[string]string{"nes-short": systemdefs.SystemNES},
			expectedSystemToHqs: map[string][]hqSystemQueryTarget{
				systemdefs.SystemNES: {{ReferenceID: "nes-short"}},
			},
		},
		{
			name: "reference id system id matches",
			systems: []HqSystemInfo{
				{Name: "Nintendo", ReferenceID: systemdefs.SystemNES, Platform: ""},
			},
			expectedKeyToSys: map[string]string{systemdefs.SystemNES: systemdefs.SystemNES},
			expectedSystemToHqs: map[string][]hqSystemQueryTarget{
				systemdefs.SystemNES: {{ReferenceID: systemdefs.SystemNES}},
			},
		},
		{
			name: "HyperSpin alias platform matching",
			systems: []HqSystemInfo{
				{Name: "PC Engine CD", ReferenceID: "pcecd", Platform: "NEC TurboGrafx-CD"},
			},
			expectedKeyToSys: map[string]string{"pcecd": systemdefs.SystemTurboGrafx16CD},
			expectedSystemToHqs: map[string][]hqSystemQueryTarget{
				systemdefs.SystemTurboGrafx16CD: {{ReferenceID: "pcecd"}},
			},
		},
		{
			name: "unknown platform maps to Custom",
			systems: []HqSystemInfo{
				{Name: "Made Up", ReferenceID: "x-1", Platform: "Definitely Not A Real Platform"},
			},
			expectedKeyToSys: map[string]string{"x-1": systemdefs.SystemCustom},
			expectedSystemToHqs: map[string][]hqSystemQueryTarget{
				systemdefs.SystemCustom: {{ReferenceID: "x-1"}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			keyToSys, systemToHqs := buildHqMappings(tt.systems)

			assert.Equal(t, tt.expectedKeyToSys, keyToSys)

			require.Len(t, systemToHqs, len(tt.expectedSystemToHqs))
			for sysID, expectedTargets := range tt.expectedSystemToHqs {
				assert.ElementsMatch(t, expectedTargets, systemToHqs[sysID],
					"systemToHqs[%q] mismatch", sysID)
			}
		})
	}
}

func TestHyperHqVirtualPathRoundTrip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		id    string
		title string
	}{
		{"abc-123", "Super Mario Bros"},
		{"uuid-style-1234-5678", "The Legend of Zelda"},
		{"id with spaces", "Game with / Special : Characters"},
	}

	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			t.Parallel()

			path := virtualpath.CreateVirtualPath(shared.SchemeHyperHq, tt.id, tt.title)
			extracted, err := virtualpath.ExtractSchemeID(path, shared.SchemeHyperHq)
			require.NoError(t, err)
			assert.Equal(t, tt.id, extracted)
		})
	}
}

func TestHyperHqLauncherFields(t *testing.T) {
	t.Parallel()

	p := &Platform{}
	launcher := p.NewHyperHqLauncher()

	assert.Equal(t, "HyperHQ", launcher.ID)
	assert.Equal(t, []string{shared.SchemeHyperHq}, launcher.Schemes)
	assert.True(t, launcher.SkipFilesystemScan)
	assert.NotNil(t, launcher.Scanner)
	assert.NotNil(t, launcher.Launch)
}

func TestHyperHqScannerBufferHandlesLargeResponses(t *testing.T) {
	t.Parallel()

	const numGames = 8888
	games := make([]HqGameInfo, numGames)
	for i := range games {
		games[i] = HqGameInfo{
			ID:    fmt.Sprintf("game-%d-with-long-uuid-style-id-12345", i),
			Title: fmt.Sprintf("Test Game %d with a reasonably long title for testing", i),
		}
	}

	event := hqGamesEvent{
		Event:             "Games",
		SystemReferenceID: "nes-1",
		Games:             games,
	}

	jsonData, err := json.Marshal(event)
	require.NoError(t, err)

	require.Greater(t, len(jsonData), 1024*1024,
		"test JSON should exceed 1MB to be a valid regression test")

	reader := strings.NewReader(string(jsonData) + "\n")
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), hyperHqScannerMaxBuffer)

	require.True(t, scanner.Scan(), "scanner should read large JSON response")
	require.NoError(t, scanner.Err(), "scanner should not return 'token too long' error")

	var parsed hqGamesEvent
	err = json.Unmarshal(scanner.Bytes(), &parsed)
	require.NoError(t, err)
	assert.Len(t, parsed.Games, numGames)
}
