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
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// defaultTestSettings returns a SettingsResponse with sensible defaults for testing.
func defaultTestSettings() *models.SettingsResponse {
	return &models.SettingsResponse{
		AudioScanFeedback:       true,
		ReadersAutoDetect:       true,
		ReadersScanMode:         config.ScanModeTap,
		ReadersScanExitDelay:    0.5,
		ReadersScanIgnoreSystem: []string{},
		ReadersConnect:          nil,
		DebugLogging:            false,
	}
}

// defaultTestSystems returns a sample systems list for testing.
func defaultTestSystems() []models.System {
	return []models.System{
		{ID: "nes", Name: "Nintendo Entertainment System"},
		{ID: "snes", Name: "Super Nintendo"},
		{ID: "genesis", Name: "Sega Genesis"},
	}
}

func TestBuildSettingsMainMenu_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()

	pages := tview.NewPages()

	// Create mock service
	mockSvc := NewMockSettingsService()
	mockSvc.SetupGetSettings(defaultTestSettings())
	mockSvc.SetupGetSystems(defaultTestSystems())
	mockSvc.SetupUpdateSettingsSuccess()

	cfg := &config.Instance{}

	var rebuildCalled atomic.Bool
	rebuildMainPage := func() {
		rebuildCalled.Store(true)
	}

	runner.Start(pages)
	runner.Draw()

	// Build the settings menu
	runner.QueueUpdateDraw(func() {
		BuildSettingsMainMenuWithService(cfg, mockSvc, pages, runner.App(), nil, rebuildMainPage, "", "")
	})

	// Verify settings page is shown
	require.True(t, runner.WaitForText("Settings", 100*time.Millisecond), "Settings title should appear")

	// Verify menu items are visible
	assert.True(t, runner.ContainsText("Readers"), "Readers menu item should be visible")
	assert.True(t, runner.ContainsText("Audio"), "Audio menu item should be visible")
	assert.True(t, runner.ContainsText("TUI"), "TUI menu item should be visible")
	assert.True(t, runner.ContainsText("Advanced"), "Advanced menu item should be visible")
	assert.True(t, runner.ContainsText("Logs"), "Logs menu item should be visible")
	assert.True(t, runner.ContainsText("About"), "About menu item should be visible")
}

func TestBuildSettingsMainMenu_Navigation_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()

	pages := tview.NewPages()

	mockSvc := NewMockSettingsService()
	mockSvc.SetupGetSettings(defaultTestSettings())
	mockSvc.SetupGetSystems(defaultTestSystems())
	mockSvc.SetupUpdateSettingsSuccess()

	cfg := &config.Instance{}

	runner.Start(pages)
	runner.Draw()

	runner.QueueUpdateDraw(func() {
		BuildSettingsMainMenuWithService(cfg, mockSvc, pages, runner.App(), nil, nil, "", "")
	})

	require.True(t, runner.WaitForText("Settings", 100*time.Millisecond))

	// Navigate down through menu items
	runner.Screen().InjectArrowDown()
	runner.Draw()

	runner.Screen().InjectArrowDown()
	runner.Draw()

	// Should still be on the settings page
	assert.True(t, runner.ContainsText("Settings"), "Should still be on settings page")
}

func TestBuildSettingsMainMenu_EscapeGoesBack_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()

	pages := tview.NewPages()

	// Add main page first
	mainPage := tview.NewTextView().SetText("Main Page")
	pages.AddPage(PageMain, mainPage, true, true)

	mockSvc := NewMockSettingsService()
	mockSvc.SetupGetSettings(defaultTestSettings())
	mockSvc.SetupGetSystems(defaultTestSystems())

	cfg := &config.Instance{}

	var rebuildCalled atomic.Bool
	rebuildMainPage := func() {
		rebuildCalled.Store(true)
		pages.SwitchToPage(PageMain)
	}

	runner.Start(pages)
	runner.Draw()

	// Switch to settings
	runner.QueueUpdateDraw(func() {
		BuildSettingsMainMenuWithService(cfg, mockSvc, pages, runner.App(), nil, rebuildMainPage, "", "")
	})

	require.True(t, runner.WaitForText("Settings", 100*time.Millisecond))

	rebuildDone := make(chan struct{}, 1)
	go func() {
		for !rebuildCalled.Load() {
			time.Sleep(5 * time.Millisecond)
		}
		close(rebuildDone)
	}()

	// Press escape
	runner.Screen().InjectEscape()
	runner.Draw()

	// Should have called rebuild
	assert.True(t, runner.WaitForSignal(rebuildDone, 100*time.Millisecond), "Should have called rebuildMainPage")
}

func TestBuildAudioSettingsMenu_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()

	pages := tview.NewPages()

	mockSvc := NewMockSettingsService()
	settings := defaultTestSettings()
	settings.AudioScanFeedback = true
	mockSvc.SetupGetSettings(settings)
	mockSvc.SetupUpdateSettingsSuccess()

	runner.Start(pages)
	runner.Draw()

	// Build audio settings directly
	runner.QueueUpdateDraw(func() {
		buildAudioSettingsMenu(mockSvc, pages, runner.App())
	})

	// Verify audio page is shown
	require.True(t, runner.WaitForText("Audio", 100*time.Millisecond), "Audio title should appear")

	// Verify toggle is visible
	assert.True(t, runner.ContainsText("Audio feedback"), "Audio feedback toggle should be visible")
}

func TestBuildAudioSettingsMenu_Toggle_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()

	pages := tview.NewPages()

	mockSvc := NewMockSettingsService()
	settings := defaultTestSettings()
	settings.AudioScanFeedback = true
	mockSvc.SetupGetSettings(settings)
	mockSvc.SetupUpdateSettingsSuccess()

	runner.Start(pages)
	runner.Draw()

	runner.QueueUpdateDraw(func() {
		buildAudioSettingsMenu(mockSvc, pages, runner.App())
	})

	require.True(t, runner.WaitForText("Audio", 100*time.Millisecond))

	// Toggle by pressing Enter
	runner.Screen().InjectEnter()
	runner.Draw()

	// Wait for UpdateSettings to be called
	assert.True(t, runner.WaitForCondition(func() bool {
		return mockSvc.UpdateSettingsCallCount() > 0
	}, 100*time.Millisecond), "UpdateSettings should be called")
}

func TestBuildAudioSettingsMenu_Error_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()

	pages := tview.NewPages()

	// Add settings main page to go back to
	pages.AddPage(PageSettingsMain, tview.NewTextView().SetText("Settings Main"), true, false)

	mockSvc := NewMockSettingsService()
	mockSvc.SetupGetSettingsError(errors.New("failed to fetch settings"))

	runner.Start(pages)
	runner.Draw()

	runner.QueueUpdateDraw(func() {
		buildAudioSettingsMenu(mockSvc, pages, runner.App())
	})

	// Should show error modal
	require.True(t, runner.WaitForText("Failed", 100*time.Millisecond), "Error modal should appear")
}

func TestBuildReadersSettingsMenu_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()

	pages := tview.NewPages()

	mockSvc := NewMockSettingsService()
	settings := defaultTestSettings()
	settings.ReadersAutoDetect = true
	settings.ReadersScanMode = config.ScanModeTap
	mockSvc.SetupGetSettings(settings)
	mockSvc.SetupUpdateSettingsSuccess()

	cfg := &config.Instance{}

	runner.Start(pages)
	runner.Draw()

	runner.QueueUpdateDraw(func() {
		buildReadersSettingsMenu(cfg, mockSvc, pages, runner.App(), nil)
	})

	require.True(t, runner.WaitForText("Readers", 100*time.Millisecond), "Readers title should appear")

	// Verify menu items
	assert.True(t, runner.ContainsText("Auto-detect"), "Auto-detect toggle should be visible")
	assert.True(t, runner.ContainsText("Scan mode"), "Scan mode should be visible")
	assert.True(t, runner.ContainsText("Exit delay"), "Exit delay should be visible")
}

func TestBuildReadersSettingsMenu_ScanModeOptions(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()

	pages := tview.NewPages()

	mockSvc := NewMockSettingsService()
	settings := defaultTestSettings()
	settings.ReadersScanMode = config.ScanModeTap
	mockSvc.SetupGetSettings(settings)
	mockSvc.SetupUpdateSettingsSuccess()

	cfg := &config.Instance{}

	runner.Start(pages)
	runner.Draw()

	runner.QueueUpdateDraw(func() {
		buildReadersSettingsMenu(cfg, mockSvc, pages, runner.App(), nil)
	})

	require.True(t, runner.WaitForText("Readers", 100*time.Millisecond))

	// Verify scan mode displays Tap
	assert.True(t, runner.ContainsText("Tap"), "Tap mode should be visible")
}

func TestBuildAdvancedSettingsMenu_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()

	pages := tview.NewPages()

	mockSvc := NewMockSettingsService()
	settings := defaultTestSettings()
	settings.DebugLogging = false
	settings.ReadersScanIgnoreSystem = []string{"snes", "genesis"}
	mockSvc.SetupGetSettings(settings)
	mockSvc.SetupGetSystems(defaultTestSystems())
	mockSvc.SetupUpdateSettingsSuccess()

	runner.Start(pages)
	runner.Draw()

	runner.QueueUpdateDraw(func() {
		buildAdvancedSettingsMenu(mockSvc, pages, runner.App())
	})

	require.True(t, runner.WaitForText("Advanced", 100*time.Millisecond), "Advanced title should appear")

	// Verify menu items
	assert.True(t, runner.ContainsText("Ignore systems"), "Ignore systems should be visible")
	assert.True(t, runner.ContainsText("Debug logging"), "Debug logging should be visible")

	// Verify count indicator (2 systems selected)
	assert.True(t, runner.ContainsText("2 selected"), "Should show 2 systems selected")
}

func TestBuildAdvancedSettingsMenu_ToggleDebugLogging_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()

	pages := tview.NewPages()

	mockSvc := NewMockSettingsService()
	settings := defaultTestSettings()
	settings.DebugLogging = false
	mockSvc.SetupGetSettings(settings)
	mockSvc.SetupUpdateSettingsSuccess()

	runner.Start(pages)
	runner.Draw()

	runner.QueueUpdateDraw(func() {
		buildAdvancedSettingsMenu(mockSvc, pages, runner.App())
	})

	require.True(t, runner.WaitForText("Advanced", 100*time.Millisecond))

	// Navigate to debug logging (second item)
	runner.Screen().InjectArrowDown()
	runner.Draw()

	// Toggle
	runner.Screen().InjectEnter()
	runner.Draw()

	// Wait for UpdateSettings to be called
	assert.True(t, runner.WaitForCondition(func() bool {
		return mockSvc.UpdateSettingsCallCount() > 0
	}, 100*time.Millisecond), "UpdateSettings should be called")
}

func TestBuildAboutPage_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()

	pages := tview.NewPages()

	// Add settings main page to go back to
	pages.AddPage(PageSettingsMain, tview.NewTextView().SetText("Settings Main"), true, false)

	runner.Start(pages)
	runner.Draw()

	runner.QueueUpdateDraw(func() {
		buildAboutPage(pages, runner.App())
	})

	require.True(t, runner.WaitForText("About", 100*time.Millisecond), "About title should appear")

	// Verify content
	assert.True(t, runner.ContainsText("Zaparoo Core"), "Should show Zaparoo Core")
	assert.True(t, runner.ContainsText("Version"), "Should show Version")
	assert.True(t, runner.ContainsText("GPL"), "Should show GPL license")
}

func TestBuildAboutPage_BackNavigation_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()

	pages := tview.NewPages()

	// Add settings main page
	pages.AddPage(PageSettingsMain, tview.NewTextView().SetText("Settings Main"), true, false)

	runner.Start(pages)
	runner.Draw()

	runner.QueueUpdateDraw(func() {
		buildAboutPage(pages, runner.App())
	})

	require.True(t, runner.WaitForText("About", 100*time.Millisecond))

	// Helper to check current page
	getFrontPage := func() string {
		var name string
		runner.QueueUpdateDraw(func() {
			name, _ = pages.GetFrontPage()
		})
		return name
	}

	// Press escape
	runner.Screen().InjectEscape()
	runner.Draw()

	assert.True(t, runner.WaitForCondition(func() bool {
		return getFrontPage() == PageSettingsMain
	}, 100*time.Millisecond), "Should navigate back to settings main")
}

func TestExitDelayLabels(t *testing.T) {
	t.Parallel()

	labels := exitDelayLabels()

	assert.NotEmpty(t, labels, "Should return labels")
	assert.Len(t, labels, len(ExitDelayOptions), "Should have same length as ExitDelayOptions")

	// Verify first label
	assert.Equal(t, ExitDelayOptions[0].Label, labels[0])
}

func TestFindExitDelayIndex(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		delay    float32
		expected int
	}{
		{
			name:     "finds first option",
			delay:    ExitDelayOptions[0].Value,
			expected: 0,
		},
		{
			name:     "finds middle option",
			delay:    ExitDelayOptions[len(ExitDelayOptions)/2].Value,
			expected: len(ExitDelayOptions) / 2,
		},
		{
			name:     "unknown delay returns 0",
			delay:    999.0,
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := findExitDelayIndex(tt.delay)
			assert.Equal(t, tt.expected, result)
		})
	}
}
