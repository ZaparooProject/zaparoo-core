//go:build windows

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

package windows

import (
	"encoding/json"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/stretchr/testify/assert"
)

func TestPluginEventJSONSerialization(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		jsonStr  string
		expected pluginEvent
	}{
		{
			name:    "MediaStarted event",
			jsonStr: `{"Event":"MediaStarted","Id":"abc123","Title":"Test Game","Platform":"Nintendo Entertainment System","ApplicationPath":"C:\\Games\\game.nes"}`,
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
			assert.NoError(t, err)
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
			assert.NoError(t, err)
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
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not connected")
}
