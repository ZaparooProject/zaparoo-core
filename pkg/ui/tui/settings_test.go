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
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	testingmocks "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
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
		ErrorReporting:          false,
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
	mockSvc.SetupGetBackupStatus(backupTestStatus(true))

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
	assert.True(t, runner.ContainsText("Online"),
		"the Online settings entry is always available")
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
	mockSvc.SetupGetBackupStatus(backupTestStatus(true))

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
	mockSvc.SetupGetBackupStatus(backupTestStatus(true))

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

	// Wait for UpdateSettings to be called using the mock's signal channel
	called := mockSvc.UpdateSettingsCalled()
	assert.True(t, runner.WaitForSignal(called, 100*time.Millisecond), "UpdateSettings should be called")
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

func TestBuildReaderEditPage_DownFromEnabledFocusesButtonBar(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()

	pages := tview.NewPages()
	cfg := &config.Instance{}
	mockSvc := NewMockSettingsService()
	mockReader := testingmocks.NewMockReader()
	mockReader.SetupBasicMock()
	mockPlatform := testingmocks.NewMockPlatform()
	mockPlatform.On("SupportedReaders", cfg).Return([]readers.Reader{mockReader})
	configuredReaders := []models.ReaderConnection{}

	runner.Start(pages)
	runner.Draw()

	runner.QueueUpdateDraw(func() {
		buildReaderEditPage(cfg, mockSvc, pages, runner.App(), mockPlatform, &configuredReaders, 0)
	})

	require.True(t, runner.WaitForText("Settings", 100*time.Millisecond), "Reader edit page should appear")

	runner.SimulateArrowDown()
	runner.SimulateArrowDown()
	runner.SimulateArrowDown()
	runner.SimulateArrowDown()

	focused := runner.App().GetFocus()
	_, focusedButtonBar := focused.(*ButtonBar)
	_, focusedInternalButton := focused.(*tview.Button)
	assert.True(t, focusedButtonBar, "Down from Enabled should focus the ButtonBar")
	assert.False(t, focusedInternalButton, "Down from Enabled should not focus an internal button")
	mockPlatform.AssertExpectations(t)
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

	// Wait for UpdateSettings to be called using the mock's signal channel
	called := mockSvc.UpdateSettingsCalled()
	assert.True(t, runner.WaitForSignal(called, 100*time.Millisecond), "UpdateSettings should be called")
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

func TestFormatBackupDetailsShowsPartialWarnings(t *testing.T) {
	t.Parallel()
	details := formatBackupDetails("Local backup", map[string]any{
		"status":    "partial",
		"integrity": "unchecked",
		"warnings": []any{
			map[string]any{"path": "saves/broken.sav", "reason": "broken symlink"},
		},
	})
	assert.NotContains(t, details, "Payload integrity")
	assert.Contains(t, details, "saves/broken.sav (broken symlink)")
}

func TestFormatHumanBytes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		want  string
		bytes int64
	}{
		{name: "bytes", bytes: 512, want: "512 B"},
		{name: "exact kb", bytes: 2048, want: "2 KB"},
		{name: "rounds up kb", bytes: 1537, want: "1.6 KB"},
		{name: "mb", bytes: 5 * 1024 * 1024, want: "5 MB"},
		{name: "gb", bytes: 3 * 1024 * 1024 * 1024, want: "3 GB"},
		{name: "tb", bytes: 1298312830128, want: "1.2 TB"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, formatHumanBytes(tt.bytes))
		})
	}
}

func backupTestStatus(linked bool) *models.BackupStatusResponse {
	return &models.BackupStatusResponse{
		Local: models.BackupStatusEntry{
			Enabled:    true,
			LastStatus: "never",
		},
		Remote: models.BackupStatusEntry{
			Enabled:    false,
			Linked:     linked,
			Schedule:   "daily",
			LastStatus: "never",
		},
	}
}

func TestBackupActionErrorTextMapsSafeGuidance(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		err      error
		expected string
	}{
		{
			name: "busy",
			err: errors.New(
				"create local backup: backup operation remote-upload has been running since now",
			),
			expected: "Another backup or restore is already running.",
		},
		{
			name: "active media", err: errors.New("cannot restore backup while media is active"),
			expected: "Stop active media before restoring this backup.",
		},
		{
			name: "media launch", err: errors.New("cannot restore backup while media is launching"),
			expected: "Wait for media launch to finish",
		},
		{
			name: "unsupported platform", err: errors.New("full-device backup is not supported on this platform"),
			expected: "Full-device backup is not available on this platform",
		},
		{
			name: "warp", err: errors.New("remote backup is not available for this account"),
			expected: "requires an active Zaparoo Warp subscription",
		},
		{
			name: "recovery", err: errors.New("backup restore rollback requires recovery: private path"),
			expected: "Restart Zaparoo Core to complete restore recovery",
		},
		{
			name: "restart", err: errors.New("backup restore restart is pending"),
			expected: "Restart Zaparoo Core before starting another backup",
		},
		{
			name: "unlinked", err: errors.New("remote backup is unlinked"),
			expected: "Relink this device to Zaparoo Online",
		},
		{
			name: "rate limited", err: errors.New("remote backup rate limited"),
			expected: "Wait a few minutes, then try again",
		},
		{
			name: "storage", err: errors.New("insufficient disk space for backup: /private/path"),
			expected: "Free storage space on this device",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			text := backupActionErrorText("Backup", tt.err)
			assert.Contains(t, text, tt.expected)
			assert.NotContains(t, text, "private path")
			assert.NotContains(t, text, "/private/path")
		})
	}
}

func TestBackupActionErrorTextRedactsUnknownError(t *testing.T) {
	t.Parallel()
	text := backupActionErrorText(
		"Backup", errors.New("upload failed for /media/fat/private with bearer secret-token"),
	)

	assert.Equal(t, "Backup failed.\n\nCheck Core logs for details, then try again.", text)
	assert.NotContains(t, text, "/media/fat")
	assert.NotContains(t, text, "secret-token")
}

func TestBuildBackupSettingsMenu_SlowFetchShowsLoader_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()
	pages := tview.NewPages()
	mockSvc := NewMockSettingsService()
	release := make(chan time.Time, 1)
	mockSvc.On("GetBackupStatus", mock.Anything).
		WaitUntil(release).
		Return(backupTestStatus(false), nil)

	runner.Start(pages)
	runner.QueueUpdateDraw(func() {
		buildBackupSettingsMenu(mockSvc, pages, runner.App(), func() {})
	})

	// A fetch that outlives the grace period shows the loading frame.
	require.True(t, runner.WaitForText("Loading backup status...", time.Second))
	release <- time.Now()
	require.True(t, runner.WaitForText("Back up now", time.Second))
	assert.False(t, runner.ContainsText("Loading backup status..."))
}

func TestBuildBackupSettingsMenu_FastFetchSkipsLoader_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()
	pages := tview.NewPages()
	previous := tview.NewTextView().SetText("Previous Page")
	pages.AddPage("previous", previous, true, true)
	mockSvc := NewMockSettingsService()
	mockSvc.SetupGetBackupStatus(backupTestStatus(false))

	runner.Start(pages)
	runner.QueueUpdateDraw(func() {
		buildBackupSettingsMenu(mockSvc, pages, runner.App(), func() {})
	})

	// An instant fetch renders the page directly, well inside the grace
	// period, so the loading frame never appears.
	require.True(t, runner.WaitForText("Back up now", 100*time.Millisecond))
	assert.False(t, runner.ContainsText("Loading backup status..."))
}

func TestBuildBackupSettingsMenu_UnlinkedShowsCloudLinkPointer_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()
	pages := tview.NewPages()
	mockSvc := NewMockSettingsService()
	mockSvc.SetupGetBackupStatus(backupTestStatus(false))

	runner.Start(pages)
	runner.QueueUpdateDraw(func() {
		buildBackupSettingsMenu(mockSvc, pages, runner.App(), func() {})
	})

	require.True(t, runner.WaitForText("Back up now", 100*time.Millisecond))
	assert.True(t, runner.ContainsText("Local"))
	assert.True(t, runner.ContainsText("Cloud"))
	assert.True(t, runner.ContainsText("View backups"))
	assert.True(t, runner.ContainsText("Link account"),
		"unlinked devices see a pointer to account linking in the Cloud section")
	assert.False(t, runner.ContainsText("Automatic backup"))
	assert.False(t, runner.ContainsText("Schedule"))
}

func TestBuildBackupSettingsMenu_CloudControlsVisibleWhenLinked_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()
	pages := tview.NewPages()
	mockSvc := NewMockSettingsService()
	mockSvc.SetupGetBackupStatus(backupTestStatus(true))
	mockSvc.SetupUpdateSettingsSuccess()

	runner.Start(pages)
	runner.QueueUpdateDraw(func() {
		buildBackupSettingsMenu(mockSvc, pages, runner.App(), func() {})
	})

	require.True(t, runner.WaitForText("Automatic backup", 100*time.Millisecond))
	assert.True(t, runner.ContainsText("Local"))
	assert.True(t, runner.ContainsText("Cloud"))
	assert.True(t, runner.ContainsText("Schedule"))
	assert.True(t, runner.ContainsText("Status"))
	assert.False(t, runner.ContainsText("Link account"),
		"linked devices manage the account from the Online page")
}

func TestBuildBackupSettingsMenu_ToggleWritesBackupRemoteEnabled_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()
	pages := tview.NewPages()
	mockSvc := NewMockSettingsService()
	mockSvc.SetupGetBackupStatus(backupTestStatus(true))
	mockSvc.SetupUpdateSettingsSuccess()

	runner.Start(pages)
	runner.QueueUpdateDraw(func() {
		buildBackupSettingsMenu(mockSvc, pages, runner.App(), func() {})
	})
	require.True(t, runner.WaitForText("Automatic backup", 100*time.Millisecond))

	// Navigate from local "Back up now" to the cloud toggle and flip it.
	runner.SimulateArrowDown()
	runner.SimulateArrowDown()
	runner.SimulateEnter()

	require.True(t, runner.WaitForCondition(func() bool {
		for _, call := range mockSvc.Calls {
			if call.Method != "UpdateSettings" {
				continue
			}
			params, ok := call.Arguments.Get(1).(*models.UpdateSettingsParams)
			if ok && params.BackupRemoteEnabled != nil {
				return true
			}
		}
		return false
	}, 100*time.Millisecond), "toggle should write BackupRemoteEnabled")
}

func TestBuildBackupSettingsMenu_UnavailableWarpStillAttemptsUpload_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 100, 30)
	defer runner.Stop()
	pages := tview.NewPages()
	mockSvc := NewMockSettingsService()
	status := backupTestStatus(true)
	status.Remote.Availability = "unavailable"
	mockSvc.SetupGetBackupStatus(status)
	mockSvc.SetupUpdateSettingsSuccess()
	mockSvc.On("RunRemoteBackup", mock.Anything).
		Return("", errors.New("remote backup is not available for this account"))

	runner.Start(pages)
	runner.QueueUpdateDraw(func() {
		buildBackupSettingsMenu(mockSvc, pages, runner.App(), func() {})
	})

	require.True(t, runner.WaitForText("Automatic backup", 100*time.Millisecond))
	assert.True(t, runner.ContainsText("View backups"), "restore must remain available")
	// Local: Back up now, View backups; Cloud: Automatic backup, Schedule,
	// Back up now. Four downs land on the cloud upload action.
	runner.SimulateArrowDown()
	runner.SimulateArrowDown()
	runner.SimulateArrowDown()
	runner.SimulateArrowDown()
	require.True(t, runner.WaitForText("Warp is required", 100*time.Millisecond))
	// The cached "unavailable" value is only a hint: pressing the action
	// must still attempt the upload, whose fresh server-side check is
	// authoritative (a just-activated subscription works immediately).
	runner.SimulateEnter()
	require.True(t, runner.WaitForText("requires an active Zaparoo Warp subscription", 500*time.Millisecond))
	mockSvc.AssertCalled(t, "RunRemoteBackup", mock.Anything)
}

func TestBuildBackupSettingsMenu_BackupNowCallsService_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()
	pages := tview.NewPages()
	mockSvc := NewMockSettingsService()
	mockSvc.SetupGetBackupStatus(backupTestStatus(false))
	release := make(chan time.Time, 1)
	mockSvc.On("CreateBackup", mock.Anything).
		WaitUntil(release).
		Return("backup-20260624-150405-000000000-manual.zip", nil)

	runner.Start(pages)
	runner.QueueUpdateDraw(func() {
		buildBackupSettingsMenu(mockSvc, pages, runner.App(), func() {})
	})

	require.True(t, runner.WaitForText("Back up now", 100*time.Millisecond))
	runner.SimulateEnter()
	require.True(t, runner.WaitForText("Creating backup", 500*time.Millisecond))
	require.True(t, runner.WaitForText("Elapsed:", 100*time.Millisecond))
	assert.False(t, runner.ContainsText("Hide"), "progress modal must not offer a Hide button")
	release <- time.Now()
	require.True(t, runner.WaitForText("Backup created", 500*time.Millisecond))
	mockSvc.AssertCalled(t, "CreateBackup", mock.Anything)
}

func TestBuildBackupSettingsMenu_BackupErrorShowsSafeGuidance_Integration(t *testing.T) {
	t.Parallel()
	runner := NewTestAppRunner(t, 100, 30)
	defer runner.Stop()
	pages := tview.NewPages()
	mockSvc := NewMockSettingsService()
	mockSvc.SetupGetBackupStatus(backupTestStatus(false))
	mockSvc.On("CreateBackup", mock.Anything).Return(
		"", errors.New("cannot restore backup while media is active: /private/path"),
	)

	runner.Start(pages)
	runner.QueueUpdateDraw(func() {
		buildBackupSettingsMenu(mockSvc, pages, runner.App(), func() {})
	})

	require.True(t, runner.WaitForText("Back up now", 100*time.Millisecond))
	runner.SimulateEnter()
	require.True(t, runner.WaitForText("Stop active media", 500*time.Millisecond))
	assert.False(t, runner.ContainsText("/private/path"))
}

func TestBuildBackupSettingsMenu_RemoteBackupNowCallsService_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()
	pages := tview.NewPages()
	mockSvc := NewMockSettingsService()
	mockSvc.SetupGetBackupStatus(backupTestStatus(true))
	mockSvc.SetupUpdateSettingsSuccess()
	mockSvc.On("RunRemoteBackup", mock.Anything).Return("backup-42", nil)

	runner.Start(pages)
	runner.QueueUpdateDraw(func() {
		buildBackupSettingsMenu(mockSvc, pages, runner.App(), func() {})
	})

	require.True(t, runner.WaitForText("Automatic backup", 100*time.Millisecond))
	// Local: Back up now, View backups; Cloud: Automatic backup, Schedule,
	// Back up now. Four downs land on the cloud upload action.
	runner.SimulateArrowDown()
	runner.SimulateArrowDown()
	runner.SimulateArrowDown()
	runner.SimulateArrowDown()
	runner.SimulateEnter()
	require.True(t, runner.WaitForText("Cloud backup created", 500*time.Millisecond))
	require.True(t, runner.WaitForText("Cloud backup backup-42", 100*time.Millisecond))
	mockSvc.AssertCalled(t, "RunRemoteBackup", mock.Anything)
}

func TestWaitForCoreRestart_DownThenUp_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()
	pages := tview.NewPages()
	pages.AddPage("main", tview.NewTextView().SetText("Main Page"), true, true)
	runner.Start(pages)
	runner.Draw()

	mockSvc := NewMockSettingsService()
	mockSvc.On("GetSettings", mock.Anything).Return(nil, errors.New("connection refused")).Twice()
	mockSvc.On("GetSettings", mock.Anything).Return(&models.SettingsResponse{}, nil)

	done := make(chan struct{})
	runner.QueueUpdateDraw(func() {
		waitForCoreRestartWith(mockSvc, pages, runner.App(),
			10*time.Millisecond, time.Minute, time.Minute, func() { close(done) })
	})

	require.True(t, runner.WaitForText("Core is restarting.", 100*time.Millisecond))
	require.True(t, runner.WaitForText("Core restarted", time.Second))
	assert.False(t, runner.ContainsText("Core is restarting."))
	select {
	case <-done:
		t.Fatal("onDone must wait for the user to confirm the restart modal")
	default:
	}

	runner.Screen().InjectEnter()
	runner.Draw()
	require.True(t, runner.WaitForSignal(done, time.Second),
		"onDone should run once the restart confirmation is dismissed")
}

func TestWaitForCoreRestart_NeverDownEndsAfterGrace_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()
	pages := tview.NewPages()
	pages.AddPage("main", tview.NewTextView().SetText("Main Page"), true, true)
	runner.Start(pages)
	runner.Draw()

	mockSvc := NewMockSettingsService()
	mockSvc.On("GetSettings", mock.Anything).Return(&models.SettingsResponse{}, nil)

	done := make(chan struct{})
	runner.QueueUpdateDraw(func() {
		waitForCoreRestartWith(mockSvc, pages, runner.App(),
			10*time.Millisecond, 50*time.Millisecond, time.Minute, func() { close(done) })
	})

	require.True(t, runner.WaitForText("Core restarted", time.Second))
	runner.Screen().InjectEnter()
	runner.Draw()
	require.True(t, runner.WaitForSignal(done, time.Second),
		"onDone should run after the no-drop grace period and confirmation")
}

func TestBuildBackupListPage_DisplaysBackups_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()
	pages := tview.NewPages()
	mockSvc := NewMockSettingsService()
	backupName := "backup-20260624-150405-000000000-manual.zip"
	mockSvc.SetupGetBackupStatus(backupTestStatus(false))
	mockSvc.SetupListBackups([]map[string]any{{"name": backupName, "createdAt": "2026-06-24T15:04:05Z"}})

	runner.Start(pages)
	runner.QueueUpdateDraw(func() {
		buildBackupListPage(mockSvc, pages, runner.App(), func() {})
	})

	require.True(t, runner.WaitForText("Local backup 2026-06-24 15:04:05 UTC", 100*time.Millisecond))
	pageName, primitive := pages.GetFrontPage()
	require.Equal(t, PageSettingsBackupList, pageName)
	frame, ok := primitive.(*PageFrame)
	require.True(t, ok)
	list, ok := frame.GetContent().(*tview.List)
	require.True(t, ok)
	require.Positive(t, list.GetItemCount())
	mainText, _ := list.GetItemText(0)
	assert.Equal(t, "Local backup 2026-06-24 15:04:05 UTC", mainText)
}

func TestBuildBackupListPage_SelectShowsDetailsModal_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 100, 40)
	defer runner.Stop()
	pages := tview.NewPages()
	mockSvc := NewMockSettingsService()
	backupName := "backup-20260624-150405-000000000-manual.zip"
	mockSvc.SetupGetBackupStatus(backupTestStatus(false))
	backupDetails := map[string]any{
		"name":      backupName,
		"createdAt": "2026-06-24T15:04:05Z",
		"size":      float64(2048),
		"status":    "success",
		"integrity": "unchecked",
		"categories": map[string]any{
			"zaparoo":  map[string]any{"files": float64(3), "bytes": float64(1000)},
			"settings": map[string]any{"files": float64(2), "bytes": float64(500)},
		},
	}
	mockSvc.SetupListBackups([]map[string]any{{
		"name":      backupName,
		"createdAt": "2026-06-24T15:04:05Z",
		"size":      float64(2048),
	}})
	mockSvc.SetupInspectBackup(backupDetails)

	runner.Start(pages)
	runner.QueueUpdateDraw(func() {
		buildBackupListPage(mockSvc, pages, runner.App(), func() {})
	})

	require.True(t, runner.WaitForText("Local backup 2026-06-24 15:04:05 UTC", 100*time.Millisecond))
	runner.SimulateEnter()
	require.True(t, runner.WaitForText("Backup details", 100*time.Millisecond))
	require.True(t, runner.WaitForText("Size: 2 KB", 100*time.Millisecond))
	assert.False(t, runner.ContainsText("Payload integrity"))
	require.True(t, runner.WaitForText("Manifest:", 100*time.Millisecond))
	require.True(t, runner.WaitForText("Zaparoo", 100*time.Millisecond), runner.GetScreenText())
	require.True(t, runner.WaitForText("Settings", 100*time.Millisecond), runner.GetScreenText())
}

func TestBuildBackupListPage_InspectFailureDisablesRestore_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 100, 40)
	defer runner.Stop()
	pages := tview.NewPages()
	mockSvc := NewMockSettingsService()
	backupName := "backup-20260624-150405-000000000-manual.zip"
	mockSvc.SetupGetBackupStatus(backupTestStatus(false))
	mockSvc.SetupListBackups([]map[string]any{{
		"name":      backupName,
		"createdAt": "2026-06-24T15:04:05Z",
		"size":      float64(2048),
	}})
	mockSvc.SetupInspectBackupError(errors.New("bad manifest"))

	runner.Start(pages)
	runner.QueueUpdateDraw(func() {
		buildBackupListPage(mockSvc, pages, runner.App(), func() {})
	})

	require.True(t, runner.WaitForText("Local backup 2026-06-24 15:04:05 UTC", 100*time.Millisecond))
	runner.SimulateEnter()
	require.True(t, runner.WaitForText("Unable", 500*time.Millisecond), runner.GetScreenText())
	require.True(t, runner.WaitForText("disabled", 100*time.Millisecond), runner.GetScreenText())
	mockSvc.AssertNotCalled(t, "RestoreBackup", mock.Anything, backupName)
}

func TestBackupDetailsModal_DeleteCallsService_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 100, 40)
	defer runner.Stop()
	pages := tview.NewPages()
	mockSvc := NewMockSettingsService()
	backupName := "backup-20260624-150405-000000000-manual.zip"
	backupDetails := map[string]any{
		"name":      backupName,
		"createdAt": "2026-06-24T15:04:05Z",
		"size":      float64(2048),
		"status":    "success",
		"integrity": "unchecked",
	}
	mockSvc.SetupGetBackupStatus(backupTestStatus(false))
	mockSvc.SetupListBackups([]map[string]any{{
		"name":      backupName,
		"createdAt": "2026-06-24T15:04:05Z",
		"size":      float64(2048),
	}})
	mockSvc.SetupInspectBackup(backupDetails)
	mockSvc.SetupDeleteBackupSuccess()

	runner.Start(pages)
	runner.QueueUpdateDraw(func() {
		buildBackupListPage(mockSvc, pages, runner.App(), func() {})
	})

	require.True(t, runner.WaitForText("Local backup 2026-06-24 15:04:05 UTC", 100*time.Millisecond))
	runner.SimulateEnter()
	require.True(t, runner.WaitForText("Backup details", 500*time.Millisecond))
	runner.SimulateTab()
	runner.SimulateEnter()
	require.True(t, runner.WaitForText("Delete Local backup", 100*time.Millisecond))
	runner.SimulateEnter()
	require.True(t, runner.WaitForText("Backup deleted", 500*time.Millisecond))
	mockSvc.AssertCalled(t, "DeleteBackup", mock.Anything, backupName)
}

func TestShowBackupRestoreConfirm_CallsService_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()
	pages := tview.NewPages()
	focus := tview.NewTextView().SetText("Backup list")
	pages.AddPage("main", focus, true, true)
	mockSvc := NewMockSettingsService()
	backupName := "backup-20260624-150405-000000000-manual.zip"
	mockSvc.SetupRestoreBackupSuccess()

	runner.Start(pages)
	runner.QueueUpdateDraw(func() {
		showBackupRestoreConfirm(mockSvc, pages, runner.App(), focus, backupName, nil)
	})

	require.True(t, runner.WaitForText("Restore Local backup", 100*time.Millisecond))
	runner.Screen().InjectEnter()
	runner.Draw()
	require.True(t, runner.WaitForText("Backup restored", 100*time.Millisecond))
	mockSvc.AssertCalled(t, "RestoreBackup", mock.Anything, backupName)
}

func TestBuildAdvancedSettingsMenu_ErrorReportingVisible_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()

	pages := tview.NewPages()

	mockSvc := NewMockSettingsService()
	settings := defaultTestSettings()
	settings.ErrorReporting = false
	mockSvc.SetupGetSettings(settings)
	mockSvc.SetupUpdateSettingsSuccess()

	runner.Start(pages)
	runner.Draw()

	runner.QueueUpdateDraw(func() {
		buildAdvancedSettingsMenu(mockSvc, pages, runner.App())
	})

	require.True(t, runner.WaitForText("Advanced", 100*time.Millisecond), "Advanced title should appear")

	// Verify error reporting toggle is visible
	assert.True(t, runner.ContainsText("Error reporting"), "Error reporting toggle should be visible")
}

func TestBuildAdvancedSettingsMenu_ErrorReportingShowsConfirmModal_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()

	pages := tview.NewPages()

	mockSvc := NewMockSettingsService()
	settings := defaultTestSettings()
	settings.ErrorReporting = false
	mockSvc.SetupGetSettings(settings)
	mockSvc.SetupUpdateSettingsSuccess()

	runner.Start(pages)
	runner.Draw()

	runner.QueueUpdateDraw(func() {
		buildAdvancedSettingsMenu(mockSvc, pages, runner.App())
	})

	require.True(t, runner.WaitForText("Advanced", 100*time.Millisecond))

	// Navigate to error reporting (third item: ignore systems, debug logging, error reporting)
	runner.SimulateArrowDown() // to debug logging
	runner.SimulateArrowDown() // to error reporting

	// Toggle error reporting
	runner.SimulateEnter()

	// Should show confirmation modal with Sentry mention
	assert.True(t, runner.WaitForText("Sentry", 100*time.Millisecond),
		"Confirmation modal should mention Sentry")
}

func TestBuildAdvancedSettingsMenu_ErrorReportingCancelDoesNotEnable_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()

	pages := tview.NewPages()

	mockSvc := NewMockSettingsService()
	settings := defaultTestSettings()
	settings.ErrorReporting = false
	mockSvc.SetupGetSettings(settings)
	mockSvc.SetupUpdateSettingsSuccess()

	runner.Start(pages)
	runner.Draw()

	runner.QueueUpdateDraw(func() {
		buildAdvancedSettingsMenu(mockSvc, pages, runner.App())
	})

	require.True(t, runner.WaitForText("Advanced", 100*time.Millisecond))

	// Navigate to error reporting
	runner.SimulateArrowDown() // to debug logging
	runner.SimulateArrowDown() // to error reporting

	// Toggle error reporting - shows modal
	runner.SimulateEnter()
	require.True(t, runner.WaitForText("Sentry", 100*time.Millisecond), "Modal should appear")

	// Press Escape to cancel
	runner.SimulateEscape()

	// Wait for modal to close
	time.Sleep(50 * time.Millisecond)
	runner.Draw()

	// UpdateSettings should NOT have been called (only cancel happened)
	assert.Equal(t, 0, mockSvc.UpdateSettingsCallCount(),
		"UpdateSettings should not be called when user cancels")
}

func TestBuildAdvancedSettingsMenu_ErrorReportingConfirmEnables_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()

	pages := tview.NewPages()

	mockSvc := NewMockSettingsService()
	settings := defaultTestSettings()
	settings.ErrorReporting = false
	mockSvc.SetupGetSettings(settings)
	mockSvc.SetupUpdateSettingsSuccess()

	runner.Start(pages)
	runner.Draw()

	runner.QueueUpdateDraw(func() {
		buildAdvancedSettingsMenu(mockSvc, pages, runner.App())
	})

	require.True(t, runner.WaitForText("Advanced", 100*time.Millisecond))

	// Navigate to error reporting
	runner.SimulateArrowDown() // to debug logging
	runner.SimulateArrowDown() // to error reporting

	// Toggle error reporting - shows modal
	runner.SimulateEnter()
	require.True(t, runner.WaitForText("Sentry", 100*time.Millisecond), "Modal should appear")

	// Press Enter to confirm (Yes is the default focused button)
	runner.SimulateEnter()

	// Wait for UpdateSettings to be called
	called := mockSvc.UpdateSettingsCalled()
	assert.True(t, runner.WaitForSignal(called, 100*time.Millisecond),
		"UpdateSettings should be called when user confirms")

	// Verify the toggle is now visually checked
	assert.True(t, runner.WaitForText("[*]", 100*time.Millisecond),
		"Toggle should show as checked after confirming")
}

func TestBuildAdvancedSettingsMenu_ErrorReportingDisableNoConfirm_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()

	pages := tview.NewPages()

	mockSvc := NewMockSettingsService()
	settings := defaultTestSettings()
	settings.ErrorReporting = true // Start with enabled
	mockSvc.SetupGetSettings(settings)
	mockSvc.SetupUpdateSettingsSuccess()

	runner.Start(pages)
	runner.Draw()

	runner.QueueUpdateDraw(func() {
		buildAdvancedSettingsMenu(mockSvc, pages, runner.App())
	})

	require.True(t, runner.WaitForText("Advanced", 100*time.Millisecond))

	// Navigate to error reporting
	runner.SimulateArrowDown() // to debug logging
	runner.SimulateArrowDown() // to error reporting

	// Toggle error reporting to disable - should NOT show modal
	runner.SimulateEnter()

	// UpdateSettings should be called immediately (no confirm modal for disable)
	called := mockSvc.UpdateSettingsCalled()
	assert.True(t, runner.WaitForSignal(called, 100*time.Millisecond),
		"UpdateSettings should be called immediately when disabling")

	// Verify no modal appeared (no "Sentry" text visible)
	assert.False(t, runner.ContainsText("Sentry"),
		"No confirmation modal should appear when disabling")
}

func remoteTestBackup(id string, createdAt time.Time, source *RemoteBackupSourceDevice) RemoteBackupItem {
	return RemoteBackupItem{
		ID:         id,
		CreatedAt:  createdAt,
		BackupType: "manual",
		SizeBytes:  4 << 20,
		Categories: map[string]RemoteBackupCategory{
			"zaparoo": {Files: 2, Bytes: 100},
			"saves":   {Files: 5, Bytes: 900},
		},
		SourceDevice: source,
	}
}

func TestGroupRemoteBackupsBySource_OrdersTiersAndSnapshots(t *testing.T) {
	t.Parallel()
	base := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	oldDevice := &RemoteBackupSourceDevice{ID: "dev-old", Name: "Old MiSTer", Linked: false}
	linkedDevice := &RemoteBackupSourceDevice{ID: "dev-b", Name: "Bedroom", Linked: true}
	currentDevice := &RemoteBackupSourceDevice{ID: "dev-cur", Name: "Living Room", Linked: true, Current: true}

	sources := groupRemoteBackupsBySource([]RemoteBackupItem{
		remoteTestBackup("old-1", base.Add(1*time.Hour), oldDevice),
		remoteTestBackup("legacy-1", base.Add(2*time.Hour), nil),
		remoteTestBackup("old-2", base.Add(3*time.Hour), oldDevice),
		remoteTestBackup("cur-1", base.Add(4*time.Hour), currentDevice),
		remoteTestBackup("linked-1", base.Add(5*time.Hour), linkedDevice),
	})

	require.Len(t, sources, 3)
	// Current device first, merged with the legacy item, newest snapshot first.
	assert.Equal(t, "Living Room", sources[0].Device.Name)
	assert.True(t, sources[0].Device.Current)
	require.Len(t, sources[0].Backups, 2)
	assert.Equal(t, "cur-1", sources[0].Backups[0].ID)
	assert.Equal(t, "legacy-1", sources[0].Backups[1].ID)
	// Other linked devices next.
	assert.Equal(t, "Bedroom", sources[1].Device.Name)
	// Unlinked devices last, snapshots newest first.
	assert.Equal(t, "Old MiSTer", sources[2].Device.Name)
	require.Len(t, sources[2].Backups, 2)
	assert.Equal(t, "old-2", sources[2].Backups[0].ID)
	assert.Equal(t, "old-1", sources[2].Backups[1].ID)
}

func TestGroupRemoteBackupsBySource_LegacyWithoutSourceDevice(t *testing.T) {
	t.Parallel()
	base := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	sources := groupRemoteBackupsBySource([]RemoteBackupItem{
		remoteTestBackup("a", base, nil),
		remoteTestBackup("b", base.Add(time.Hour), nil),
	})
	require.Len(t, sources, 1)
	assert.True(t, sources[0].Device.Current)
	assert.Equal(t, "This device", remoteBackupSourceLabel(&sources[0].Device))
	require.Len(t, sources[0].Backups, 2)
	assert.Equal(t, "b", sources[0].Backups[0].ID)
}

func TestRemoteBackupSourceLabel(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "This device",
		remoteBackupSourceLabel(&RemoteBackupSourceDevice{Current: true}))
	assert.Equal(t, "Living Room (this device)",
		remoteBackupSourceLabel(&RemoteBackupSourceDevice{Name: "Living Room", Current: true}))
	assert.Equal(t, "Bedroom",
		remoteBackupSourceLabel(&RemoteBackupSourceDevice{Name: "Bedroom", Linked: true}))
	assert.Equal(t, "Old MiSTer (unlinked)",
		remoteBackupSourceLabel(&RemoteBackupSourceDevice{Name: "Old MiSTer"}))
	assert.Equal(t, "Unnamed device",
		remoteBackupSourceLabel(&RemoteBackupSourceDevice{Linked: true}))
}

func TestRemoteBackupListPage_GroupsSources_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 100, 30)
	defer runner.Stop()
	pages := tview.NewPages()
	mockSvc := NewMockSettingsService()
	base := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	mockSvc.On("ListRemoteBackups", mock.Anything).Return([]RemoteBackupItem{
		remoteTestBackup("old-1", base, &RemoteBackupSourceDevice{ID: "dev-old", Name: "Old MiSTer"}),
		remoteTestBackup("cur-1", base.Add(time.Hour),
			&RemoteBackupSourceDevice{ID: "dev-cur", Name: "Living Room", Linked: true, Current: true}),
		remoteTestBackup("cur-2", base.Add(2*time.Hour), nil),
	}, nil)

	runner.Start(pages)
	runner.QueueUpdateDraw(func() {
		buildRemoteBackupListPage(mockSvc, pages, runner.App(), func() {})
	})

	require.True(t, runner.WaitForText("Living Room (this device)", 500*time.Millisecond))
	assert.True(t, runner.ContainsText("Old MiSTer (unlinked)"))
	assert.True(t, runner.ContainsText("2 backups"), "current device merges the legacy snapshot")
	assert.True(t, runner.ContainsText("1 backup"))
	assert.True(t, runner.ContainsText("Latest 2026-07-10 14:00:00 UTC"))
}

func TestRemoteBackupListPage_Empty_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()
	pages := tview.NewPages()
	mockSvc := NewMockSettingsService()
	mockSvc.On("ListRemoteBackups", mock.Anything).Return([]RemoteBackupItem{}, nil)

	runner.Start(pages)
	runner.QueueUpdateDraw(func() {
		buildRemoteBackupListPage(mockSvc, pages, runner.App(), func() {})
	})

	require.True(t, runner.WaitForText("(no cloud backups found)", 500*time.Millisecond))
}

func TestRemoteBackupSnapshotsPage_ShowsTypeAndSize_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 100, 30)
	defer runner.Stop()
	pages := tview.NewPages()
	mockSvc := NewMockSettingsService()
	base := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	scheduled := remoteTestBackup("snap-old", base, nil)
	scheduled.BackupType = "scheduled"
	mockSvc.On("ListRemoteBackups", mock.Anything).Return([]RemoteBackupItem{
		scheduled,
		remoteTestBackup("snap-new", base.Add(time.Hour), nil),
	}, nil)

	runner.Start(pages)
	runner.QueueUpdateDraw(func() {
		buildRemoteBackupListPage(mockSvc, pages, runner.App(), func() {})
	})
	require.True(t, runner.WaitForText("This device", 500*time.Millisecond))

	runner.SimulateEnter()
	require.True(t, runner.WaitForText("Cloud backup 2026-07-10 13:00:00 UTC", 500*time.Millisecond))
	assert.True(t, runner.ContainsText("Cloud backup 2026-07-10 12:00:00 UTC"))
	assert.True(t, runner.ContainsText("Manual"))
	assert.True(t, runner.ContainsText("Scheduled"))
	assert.True(t, runner.ContainsText("4 MB"))
}

func TestRemoteBackupSnapshotsPage_IncompatibleCannotRestore_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 100, 30)
	defer runner.Stop()
	pages := tview.NewPages()
	mockSvc := NewMockSettingsService()
	incompatible := remoteTestBackup("snap-1", time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC), nil)
	incompatible.Incompatible = true
	mockSvc.On("ListRemoteBackups", mock.Anything).Return([]RemoteBackupItem{incompatible}, nil)

	runner.Start(pages)
	runner.QueueUpdateDraw(func() {
		buildRemoteBackupListPage(mockSvc, pages, runner.App(), func() {})
	})
	require.True(t, runner.WaitForText("This device", 500*time.Millisecond))

	runner.SimulateEnter()
	require.True(t, runner.WaitForText("requires newer Core", 500*time.Millisecond))
	runner.SimulateEnter()
	require.True(t, runner.WaitForText("Incompatible backup", 500*time.Millisecond))
	runner.SimulateEnter()
	mockSvc.AssertNotCalled(t, "RestoreRemoteBackup", mock.Anything, mock.Anything)
}

func TestRemoteBackupRestoreConfirm_WordingAndRestore_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 100, 30)
	defer runner.Stop()
	pages := tview.NewPages()
	mockSvc := NewMockSettingsService()
	source := &RemoteBackupSourceDevice{ID: "dev-old", Name: "Old MiSTer"}
	mockSvc.On("ListRemoteBackups", mock.Anything).Return([]RemoteBackupItem{
		remoteTestBackup("backup-7", time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC), source),
	}, nil)
	mockSvc.On("RestoreRemoteBackup", mock.Anything, "backup-7").Return(nil)

	runner.Start(pages)
	runner.QueueUpdateDraw(func() {
		buildRemoteBackupListPage(mockSvc, pages, runner.App(), func() {})
	})
	require.True(t, runner.WaitForText("Old MiSTer (unlinked)", 500*time.Millisecond))

	runner.SimulateEnter()
	require.True(t, runner.WaitForText("Cloud backup 2026-07-10 12:00:00 UTC", 500*time.Millisecond))
	runner.SimulateEnter()

	require.True(t, runner.WaitForText("Restore backup from Old MiSTer (unlinked)?", 500*time.Millisecond))
	assert.True(t, runner.ContainsText("Snapshot: 2026-07-10 12:00:00 UTC"))
	assert.True(t, runner.ContainsText("Overwrites: Zaparoo, Saves"))
	assert.True(t, runner.ContainsText("The source backup is not changed."))
	assert.True(t, runner.ContainsText("keeps its own Zaparoo Online identity."))
	assert.True(t, runner.ContainsText("safety backup is created first."))
	assert.True(t, runner.ContainsText("Core restarts after restore."))

	// Confirm ("Yes" is focused first).
	runner.SimulateEnter()
	require.True(t, runner.WaitForText("Cloud backup restored", 2*time.Second))
	mockSvc.AssertCalled(t, "RestoreRemoteBackup", mock.Anything, "backup-7")
}

func TestStartAuthLinkFlow_SuccessMentionsCloudBackups_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 100, 30)
	defer runner.Stop()
	pages := tview.NewPages()
	mockSvc := NewMockSettingsService()
	mockSvc.On("StartAuthLink", mock.Anything).Return(&models.AuthLinkStatusResponse{
		Status:          models.AuthLinkStatusPending,
		UserCode:        "ABCD-1234",
		VerificationURL: "https://zaparoo.com/link",
	}, nil)
	mockSvc.On("GetAuthLinkStatus", mock.Anything).Return(&models.AuthLinkStatusResponse{
		Status: models.AuthLinkStatusApproved,
	}, nil)

	runner.Start(pages)
	runner.QueueUpdateDraw(func() {
		startAuthLinkFlow(mockSvc, pages, runner.App(), func() {})
	})
	require.True(t, runner.WaitForText("ABCD-1234", 500*time.Millisecond))

	// The poll loop ticks every 2 seconds before observing approval.
	require.True(t, runner.WaitForText("Device linked", 5*time.Second))
	assert.True(t, runner.ContainsText("Existing backups from your account are under"))
	assert.True(t, runner.ContainsText("Cloud backup > View backups."))
}
