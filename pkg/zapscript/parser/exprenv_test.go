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

package parser_test

import (
	"encoding/json"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript/parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestArgExprEnv_JSONSerialization verifies that expression env serializes correctly to JSON
// with snake_case field names for external script consumption.
func TestArgExprEnv_JSONSerialization(t *testing.T) {
	t.Parallel()

	env := parser.ArgExprEnv{
		Platform: "mister",
		Version:  "2.0.0",
		ScanMode: "hold",
		Device: parser.ExprEnvDevice{
			Hostname: "mister",
			OS:       "linux",
			Arch:     "arm",
		},
		LastScanned: parser.ExprEnvLastScanned{
			ID:    "abc123",
			Value: "**launch:snes/mario",
			Data:  "extra-data",
		},
		MediaPlaying: true,
		ActiveMedia: parser.ExprEnvActiveMedia{
			LauncherID: "retroarch",
			SystemID:   "snes",
			SystemName: "Super Nintendo",
			Path:       "/games/snes/mario.sfc",
			Name:       "Super Mario World",
		},
	}

	jsonBytes, err := json.Marshal(env)
	require.NoError(t, err, "should marshal to JSON")

	jsonStr := string(jsonBytes)

	// Verify snake_case keys are used (not camelCase)
	assert.Contains(t, jsonStr, `"platform"`, "should contain platform field")
	assert.Contains(t, jsonStr, `"version"`, "should contain version field")
	assert.Contains(t, jsonStr, `"scan_mode"`, "should contain scan_mode field")
	assert.Contains(t, jsonStr, `"media_playing"`, "should contain media_playing field")
	assert.Contains(t, jsonStr, `"active_media"`, "should contain active_media field")
	assert.Contains(t, jsonStr, `"last_scanned"`, "should contain last_scanned field")
	assert.Contains(t, jsonStr, `"launcher_id"`, "should contain launcher_id field")
	assert.Contains(t, jsonStr, `"system_id"`, "should contain system_id field")
	assert.Contains(t, jsonStr, `"system_name"`, "should contain system_name field")

	// Verify values are correct
	assert.Contains(t, jsonStr, `"mister"`, "should contain platform value")
	assert.Contains(t, jsonStr, `"2.0.0"`, "should contain version value")
	assert.Contains(t, jsonStr, `"hold"`, "should contain scan_mode value")
	assert.Contains(t, jsonStr, `true`, "should contain media_playing value")
}

// TestArgExprEnv_JSONRoundTrip verifies JSON can be unmarshalled back.
func TestArgExprEnv_JSONRoundTrip(t *testing.T) {
	t.Parallel()

	original := parser.ArgExprEnv{
		Platform:     "test",
		Version:      "1.0.0",
		ScanMode:     "tap",
		MediaPlaying: true,
		Device: parser.ExprEnvDevice{
			Hostname: "testhost",
			OS:       "linux",
			Arch:     "amd64",
		},
		LastScanned: parser.ExprEnvLastScanned{
			ID:    "id123",
			Value: "value",
			Data:  "data",
		},
		ActiveMedia: parser.ExprEnvActiveMedia{
			LauncherID: "launcher",
			SystemID:   "system",
			SystemName: "System Name",
			Path:       "/path/to/game",
			Name:       "Game Name",
		},
	}

	jsonBytes, err := json.Marshal(original)
	require.NoError(t, err, "should marshal to JSON")

	var decoded parser.ArgExprEnv
	err = json.Unmarshal(jsonBytes, &decoded)
	require.NoError(t, err, "should unmarshal from JSON")

	assert.Equal(t, original.Platform, decoded.Platform)
	assert.Equal(t, original.Version, decoded.Version)
	assert.Equal(t, original.ScanMode, decoded.ScanMode)
	assert.Equal(t, original.MediaPlaying, decoded.MediaPlaying)
	assert.Equal(t, original.Device.Hostname, decoded.Device.Hostname)
	assert.Equal(t, original.LastScanned.ID, decoded.LastScanned.ID)
	assert.Equal(t, original.ActiveMedia.Path, decoded.ActiveMedia.Path)
}

// TestExprEnvScanned_JSONSerialization verifies scanned context serializes correctly.
func TestExprEnvScanned_JSONSerialization(t *testing.T) {
	t.Parallel()

	scanned := parser.ExprEnvScanned{
		ID:    "test-id",
		Value: "test-value",
		Data:  "test-data",
	}

	jsonBytes, err := json.Marshal(scanned)
	require.NoError(t, err)

	jsonStr := string(jsonBytes)
	assert.Contains(t, jsonStr, `"id"`)
	assert.Contains(t, jsonStr, `"value"`)
	assert.Contains(t, jsonStr, `"data"`)
}

// TestExprEnvLaunching_JSONSerialization verifies launching context serializes correctly.
func TestExprEnvLaunching_JSONSerialization(t *testing.T) {
	t.Parallel()

	launching := parser.ExprEnvLaunching{
		Path:       "/path/to/game.rom",
		SystemID:   "snes",
		LauncherID: "retroarch",
	}

	jsonBytes, err := json.Marshal(launching)
	require.NoError(t, err)

	jsonStr := string(jsonBytes)
	assert.Contains(t, jsonStr, `"path"`)
	assert.Contains(t, jsonStr, `"system_id"`)
	assert.Contains(t, jsonStr, `"launcher_id"`)
}

// TestArgExprEnv_EmptyFieldsSerialization verifies that empty struct fields are serialized
// (no omitempty behavior) for consistent JSON structure in external scripts.
func TestArgExprEnv_EmptyFieldsSerialization(t *testing.T) {
	t.Parallel()

	// Create env with empty Scanned and Launching
	env := parser.ArgExprEnv{
		Platform: "test",
		Version:  "1.0.0",
		// Scanned and Launching are zero values
	}

	jsonBytes, err := json.Marshal(env)
	require.NoError(t, err)

	jsonStr := string(jsonBytes)

	// Empty struct fields should still be present for consistent JSON schema
	// Scripts can rely on field existence rather than checking for presence
	assert.Contains(t, jsonStr, `"scanned"`, "scanned field should be present even when empty")
	assert.Contains(t, jsonStr, `"launching"`, "launching field should be present even when empty")
	assert.Contains(t, jsonStr, `"platform":"test"`, "platform should have correct value")
	assert.Contains(t, jsonStr, `"version":"1.0.0"`, "version should have correct value")
}
