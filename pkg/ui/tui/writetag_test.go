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

package tui

import (
	"testing"
	"time"

	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateZapScript(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		input           string
		expectMsgSubstr string
		expectValid     bool
	}{
		{
			name:            "empty string",
			input:           "",
			expectValid:     false,
			expectMsgSubstr: "",
		},
		{
			name:            "whitespace only",
			input:           "   \t\n  ",
			expectValid:     false,
			expectMsgSubstr: "",
		},
		{
			name:            "valid launch command",
			input:           "**launch.system:nes",
			expectValid:     true,
			expectMsgSubstr: "Valid: launch.system",
		},
		{
			name:            "valid launch with path",
			input:           "/path/to/game.nes",
			expectValid:     true,
			expectMsgSubstr: "Valid: launch",
		},
		{
			name:            "valid http command",
			input:           "**http.get:http://example.com",
			expectValid:     true,
			expectMsgSubstr: "Valid: http.get",
		},
		{
			name:            "multiple valid commands",
			input:           "**launch.system:nes\n**input.key:enter",
			expectValid:     true,
			expectMsgSubstr: "Valid:",
		},
		{
			name:            "unknown command",
			input:           "**unknown.command:value",
			expectValid:     false,
			expectMsgSubstr: "Unknown command: unknown.command",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			valid, message := validateZapScript(tt.input)
			assert.Equal(t, tt.expectValid, valid)
			if tt.expectMsgSubstr != "" {
				assert.Contains(t, message, tt.expectMsgSubstr)
			}
		})
	}
}

func TestBuildTagsWriteMenu_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()

	pages := tview.NewPages()
	pages.AddPage(PageMain, tview.NewTextView().SetText("Main"), true, false)

	mockSvc := NewMockSettingsService()
	mockSvc.SetupWriteTagSuccess()
	mockSvc.SetupCancelWriteTagSuccess()

	runner.Start(pages)
	runner.Draw()

	session := NewSession()

	runner.QueueUpdateDraw(func() {
		BuildTagsWriteMenu(mockSvc, pages, runner.App(), session)
	})

	require.True(t, runner.WaitForText("Write Token", 500*time.Millisecond), "Write Token title should appear")

	// Verify UI elements are visible
	assert.True(t, runner.ContainsText("ZapScript"), "ZapScript label should be visible")
	assert.True(t, runner.ContainsText("Write"), "Write button should be visible")
	assert.True(t, runner.ContainsText("Clear"), "Clear button should be visible")
	assert.True(t, runner.ContainsText("Back"), "Back button should be visible")
}

func TestBuildTagsWriteMenu_EscapeGoesBack_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()

	pages := tview.NewPages()
	pages.AddPage(PageMain, tview.NewTextView().SetText("Main"), true, true)

	mockSvc := NewMockSettingsService()
	mockSvc.SetupWriteTagSuccess()

	runner.Start(pages)
	runner.Draw()

	session := NewSession()

	runner.QueueUpdateDraw(func() {
		BuildTagsWriteMenu(mockSvc, pages, runner.App(), session)
	})

	require.True(t, runner.WaitForText("Write Token", 500*time.Millisecond))

	// Press escape
	runner.Screen().InjectEscape()
	runner.Draw()
	time.Sleep(30 * time.Millisecond)

	// Verify we went back
	getFrontPage := func() string {
		var name string
		runner.QueueUpdateDraw(func() {
			name, _ = pages.GetFrontPage()
		})
		return name
	}

	assert.Equal(t, PageMain, getFrontPage(), "Should navigate back to main page")
}

func TestBuildTagsWriteMenu_ClearButton_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()

	pages := tview.NewPages()
	pages.AddPage(PageMain, tview.NewTextView().SetText("Main"), true, false)

	mockSvc := NewMockSettingsService()
	mockSvc.SetupWriteTagSuccess()

	runner.Start(pages)
	runner.Draw()

	session := NewSession()
	session.SetWriteTagZapScript("**launch.system:nes")

	runner.QueueUpdateDraw(func() {
		BuildTagsWriteMenu(mockSvc, pages, runner.App(), session)
	})

	require.True(t, runner.WaitForText("Write Token", 500*time.Millisecond))

	// Navigate to Clear button (Tab then right)
	runner.Screen().InjectTab()
	runner.Draw()
	runner.Screen().InjectArrowRight()
	runner.Draw()

	// Press Enter on Clear
	runner.Screen().InjectEnter()
	runner.Draw()
	time.Sleep(30 * time.Millisecond)

	// Verify session state was cleared
	assert.Empty(t, session.GetWriteTagZapScript(), "ZapScript should be cleared")
}

func TestBuildTagsWriteMenu_Validation_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()

	pages := tview.NewPages()
	pages.AddPage(PageMain, tview.NewTextView().SetText("Main"), true, false)

	mockSvc := NewMockSettingsService()
	mockSvc.SetupWriteTagSuccess()

	runner.Start(pages)
	runner.Draw()

	session := NewSession()
	session.SetWriteTagZapScript("**launch.system:nes")

	runner.QueueUpdateDraw(func() {
		BuildTagsWriteMenu(mockSvc, pages, runner.App(), session)
	})

	require.True(t, runner.WaitForText("Write Token", 500*time.Millisecond))

	// Wait a bit more for validation to render
	time.Sleep(50 * time.Millisecond)
	runner.Draw()

	// Verify page is showing and the ZapScript label is present
	assert.True(t, runner.ContainsText("ZapScript"), "Should show ZapScript label")
}

func TestBuildTagsWriteMenu_InvalidScript_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()

	pages := tview.NewPages()
	pages.AddPage(PageMain, tview.NewTextView().SetText("Main"), true, false)

	mockSvc := NewMockSettingsService()
	mockSvc.SetupWriteTagSuccess()

	runner.Start(pages)
	runner.Draw()

	session := NewSession()
	session.SetWriteTagZapScript("**unknown.command:value")

	runner.QueueUpdateDraw(func() {
		BuildTagsWriteMenu(mockSvc, pages, runner.App(), session)
	})

	require.True(t, runner.WaitForText("Write Token", 500*time.Millisecond))

	// Wait a bit for validation to render
	time.Sleep(50 * time.Millisecond)
	runner.Draw()

	// Verify page shows properly
	assert.True(t, runner.ContainsText("ZapScript"), "Should show ZapScript label")
}

func TestSession_WriteTagZapScript(t *testing.T) {
	t.Parallel()

	session := NewSession()

	// Test that session state can be set and retrieved
	session.SetWriteTagZapScript("test script")
	assert.Equal(t, "test script", session.GetWriteTagZapScript())

	// Test clearing
	session.SetWriteTagZapScript("")
	assert.Empty(t, session.GetWriteTagZapScript())
}
