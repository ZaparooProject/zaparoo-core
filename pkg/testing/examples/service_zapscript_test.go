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

package examples

import (
	"strings"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/pkg/testing/fixtures"
	"github.com/ZaparooProject/zaparoo-core/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestZapScriptCommandParsing demonstrates testing ZapScript command parsing with mocks
func TestZapScriptCommandParsing(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name          string
		scriptContent string
		description   string
		expectedCmds  []string
		expectedError bool
	}{
		{
			name:          "Simple LAUNCH command",
			scriptContent: "LAUNCH mario:smw",
			expectedCmds:  []string{"LAUNCH", "mario:smw"},
			expectedError: false,
			description:   "Basic launch command should parse correctly",
		},
		{
			name:          "SENDKEY command",
			scriptContent: "SENDKEY RETURN",
			expectedCmds:  []string{"SENDKEY", "RETURN"},
			expectedError: false,
			description:   "Keyboard input command should parse correctly",
		},
		{
			name:          "SENDPAD command",
			scriptContent: "SENDPAD A",
			expectedCmds:  []string{"SENDPAD", "A"},
			expectedError: false,
			description:   "Gamepad input command should parse correctly",
		},
		{
			name:          "SYSTEM command",
			scriptContent: "SYSTEM genesis",
			expectedCmds:  []string{"SYSTEM", "genesis"},
			expectedError: false,
			description:   "System launch command should parse correctly",
		},
		{
			name:          "Invalid command",
			scriptContent: "INVALID_CMD",
			expectedCmds:  nil,
			expectedError: true,
			description:   "Invalid commands should return an error",
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			t.Logf("Testing: %s", tt.description)

			// This demonstrates parsing ZapScript commands
			// In a real implementation, you would have a parser here
			commands := parseZapScriptCommands(tt.scriptContent)

			if tt.expectedError {
				assert.Nil(t, commands, "Expected nil commands for invalid input")
			} else {
				require.NotNil(t, commands, "Expected valid commands")
				assert.Equal(t, tt.expectedCmds, commands, "Commands should match expected")
			}
		})
	}
}

// TestZapScriptExecution demonstrates testing ZapScript execution with platform mocks
func TestZapScriptExecution(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		commands    []string
		expectError bool
	}{
		{
			name:        "Launch command execution",
			commands:    []string{"LAUNCH", "mario:smw"},
			expectError: false,
		},
		{
			name:        "Keyboard command execution",
			commands:    []string{"SENDKEY", "RETURN"},
			expectError: false,
		},
		{
			name:        "Gamepad command execution",
			commands:    []string{"SENDPAD", "A"},
			expectError: false,
		},
		{
			name:        "System command execution",
			commands:    []string{"SYSTEM", "genesis"},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// Setup mocks
			platform := mocks.NewMockPlatform()

            // Create a minimal config without touching the filesystem
            cfg := &config.Instance{}

			// Setup database mocks
			mockUserDB := helpers.NewMockUserDBI()
			mockMediaDB := helpers.NewMockMediaDBI()

			// Database struct available if needed for more complex scenarios
			_ = &database.Database{
				UserDB:  mockUserDB,
				MediaDB: mockMediaDB,
			}

			// Set up expectations based on command type
			if len(tt.commands) >= 2 {
				switch tt.commands[0] {
				case "LAUNCH":
					// Mock media database search
					mockMediaDB.On("SearchMediaPathExact", []systemdefs.System(nil), tt.commands[1]).
						Return([]database.SearchResult{fixtures.SearchResults.Collection[0]}, nil)
					platform.On("LaunchMedia", cfg, fixtures.SearchResults.Collection[0].Path).Return(nil)
				case "SENDKEY":
					platform.On("KeyboardPress", tt.commands[1]).Return(nil)
				case "SENDPAD":
					platform.On("GamepadPress", tt.commands[1]).Return(nil)
				case "SYSTEM":
					platform.On("LaunchSystem", cfg, tt.commands[1]).Return(nil)
				}
			}

			// Demonstrate calling the mocked methods directly
			// This shows how the TDD infrastructure works without actual ZapScript logic
			if !tt.expectError && len(tt.commands) >= 2 {
				switch tt.commands[0] {
				case "LAUNCH":
					// Simulate the workflow: search for media, then launch it
					results, err := mockMediaDB.SearchMediaPathExact([]systemdefs.System(nil), tt.commands[1])
					require.NoError(t, err)
					require.Len(t, results, 1, "Should find media")

					err = platform.LaunchMedia(cfg, results[0].Path)
					require.NoError(t, err)

					// Verify launch was tracked
					launched := platform.GetLaunchedMedia()
					assert.Len(t, launched, 1, "Should have launched media")

				case "SENDKEY":
					err := platform.KeyboardPress(tt.commands[1])
					require.NoError(t, err)

					// Verify key press was tracked
					keys := platform.GetKeyboardPresses()
					assert.Contains(t, keys, tt.commands[1], "Should have pressed key")

				case "SENDPAD":
					err := platform.GamepadPress(tt.commands[1])
					require.NoError(t, err)

					// Verify gamepad press was tracked
					buttons := platform.GetGamepadPresses()
					assert.Contains(t, buttons, tt.commands[1], "Should have pressed button")

				case "SYSTEM":
					err := platform.LaunchSystem(cfg, tt.commands[1])
					require.NoError(t, err)

					// Verify system launch was tracked
					systems := platform.GetLaunchedSystems()
					assert.Contains(t, systems, tt.commands[1], "Should have launched system")
				}
			}

			// Verify all mock expectations were met
			if !tt.expectError {
				platform.AssertExpectations(t)
				mockMediaDB.AssertExpectations(t)
			}
		})
	}
}

// TestZapScriptSafetyValidation demonstrates testing ZapScript safety checks
func TestZapScriptSafetyValidation(t *testing.T) {
	t.Parallel()
	safetyTests := []struct {
		name    string
		command string
		reason  string
		isSafe  bool
	}{
		{
			name:    "Safe LAUNCH command",
			command: "LAUNCH mario:smw",
			isSafe:  true,
			reason:  "Launch commands are generally safe",
		},
		{
			name:    "Safe SENDKEY command",
			command: "SENDKEY RETURN",
			isSafe:  true,
			reason:  "Basic key presses are safe",
		},
		{
			name:    "Potentially unsafe command with path",
			command: "LAUNCH /dangerous/path/../../etc/passwd",
			isSafe:  false,
			reason:  "Path traversal attempts should be blocked",
		},
		{
			name:    "Safe system command",
			command: "SYSTEM genesis",
			isSafe:  true,
			reason:  "System commands are generally safe",
		},
	}

	for _, tt := range safetyTests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			t.Logf("Testing safety: %s", tt.reason)

			// Validate command safety (this would be your actual validation logic)
			isSafe := validateZapScriptSafety(tt.command)

			assert.Equal(t, tt.isSafe, isSafe, "Safety validation should match expected result")
		})
	}
}

// TestZapScriptComplexWorkflow demonstrates a complete ZapScript workflow test
func TestZapScriptComplexWorkflow(t *testing.T) {
	t.Parallel()
	// Setup comprehensive test environment
	platform := mocks.NewMockPlatform()

    // Minimal config instance for testing (avoid disk I/O)
    cfg := &config.Instance{}

	mockUserDB := helpers.NewMockUserDBI()
	mockMediaDB := helpers.NewMockMediaDBI()

	// Set expectations for complex workflow
	mockUserDB.On("AddHistory", helpers.HistoryEntryMatcher()).Return(nil)
	mockMediaDB.On("SearchMediaPathExact", []systemdefs.System(nil), "Complex Game").
		Return([]database.SearchResult{fixtures.SearchResults.Collection[0]}, nil)
	platform.On("LaunchMedia", cfg, fixtures.SearchResults.Collection[0].Path).Return(nil)
	platform.On("KeyboardPress", "RETURN").Return(nil)

	// Database struct available if needed for more complex scenarios
	_ = &database.Database{
		UserDB:  mockUserDB,
		MediaDB: mockMediaDB,
	}

	// Execute complex workflow by directly calling mocked methods
	// This demonstrates the TDD infrastructure capabilities

	// Step 1: Search and launch media
	results, err := mockMediaDB.SearchMediaPathExact([]systemdefs.System(nil), "Complex Game")
	require.NoError(t, err)
	require.Len(t, results, 1, "Should find media")

	err = platform.LaunchMedia(cfg, results[0].Path)
	require.NoError(t, err)

	// Step 2: Send keyboard input
	err = platform.KeyboardPress("RETURN")
	require.NoError(t, err)

	// Record history entry
	historyEntry := fixtures.HistoryEntries.Successful
	historyEntry.Time = time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	err = mockUserDB.AddHistory(&historyEntry)
	require.NoError(t, err)

	// Verify all expectations were met
	platform.AssertExpectations(t)
	mockUserDB.AssertExpectations(t)
	mockMediaDB.AssertExpectations(t)

	// Verify launched media history
	launched := platform.GetLaunchedMedia()
	assert.Len(t, launched, 1, "Should have launched one media item")

	// Verify keyboard interactions
	keyPresses := platform.GetKeyboardPresses()
	assert.Contains(t, keyPresses, "RETURN", "Should have pressed RETURN key")
}

// Mock functions for demonstration purposes
// In a real implementation, these would be actual ZapScript parsing and execution logic

func parseZapScriptCommands(scriptContent string) []string {
	// This is a mock parser for demonstration
	// In reality, you would implement actual ZapScript parsing
	switch scriptContent {
	case "LAUNCH mario:smw":
		return []string{"LAUNCH", "mario:smw"}
	case "SENDKEY RETURN":
		return []string{"SENDKEY", "RETURN"}
	case "SENDPAD A":
		return []string{"SENDPAD", "A"}
	case "SYSTEM genesis":
		return []string{"SYSTEM", "genesis"}
	case "INVALID_CMD":
		return nil // Invalid command
	default:
		return []string{scriptContent}
	}
}

func validateZapScriptSafety(command string) bool {
	// This is a mock safety validator for demonstration
	// In reality, you would implement actual safety validation

	// Simple path traversal check
	if strings.Contains(command, "..") || strings.Contains(command, "/etc/") {
		return false
	}

	// Generally safe commands
	return true
}
