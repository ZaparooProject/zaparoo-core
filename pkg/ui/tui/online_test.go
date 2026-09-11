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
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func onlineTestSettings(baseURL string) *models.SettingsResponse {
	playtimeSyncEnabled := true
	remoteControlEnabled := true
	return &models.SettingsResponse{
		BackupRemoteBaseURL:  &baseURL,
		PlaytimeSyncEnabled:  &playtimeSyncEnabled,
		RemoteControlEnabled: &remoteControlEnabled,
	}
}

// onlineTestActivity builds a remote.activity response reporting the given
// poller state, for the Remote status line.
func onlineTestActivity(remoteState string) *models.RemoteActivityResponse {
	return &models.RemoteActivityResponse{
		Status:  models.RemoteStatusInfo{State: remoteState},
		Entries: []models.RemoteActivityEntry{},
	}
}

func TestOnlineServerHost(t *testing.T) {
	t.Parallel()

	assert.Empty(t, onlineServerHost(nil))
	assert.Empty(t, onlineServerHost(&models.SettingsResponse{}))
	assert.Empty(t, onlineServerHost(onlineTestSettings(config.DefaultBackupRemoteBaseURL)))
	assert.Equal(t, "backup.example.com:8787",
		onlineServerHost(onlineTestSettings("https://backup.example.com:8787")))
}

func TestCustomBaseURLHost(t *testing.T) {
	t.Parallel()

	assert.Empty(t, customBaseURLHost(""))
	assert.Empty(t, customBaseURLHost(config.DefaultOnlineBaseURL))
	assert.Equal(t, "self-hosted.example.com", customBaseURLHost("https://self-hosted.example.com"))
	assert.Equal(t, "not-a-url", customBaseURLHost("not-a-url"))
}

func TestCustomEndpointWarning(t *testing.T) {
	t.Parallel()

	assert.Empty(t, customEndpointWarning(""))
	assert.Contains(t, customEndpointWarning("self-hosted.example.com"), "Custom server: self-hosted.example.com.")
}

func TestRemoteStatusValue(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "Unknown", remoteStatusValue(nil))
	assert.Equal(t, "Unknown", remoteStatusValue(onlineTestActivity("")))
	assert.Equal(t, "Off", remoteStatusValue(onlineTestActivity(state.RemoteStateDisabled)))
	assert.Equal(t, "Not linked", remoteStatusValue(onlineTestActivity(state.RemoteStateUnlinked)))
	assert.Equal(t, "Waiting for commands", remoteStatusValue(onlineTestActivity(state.RemoteStateWaiting)))
	assert.Equal(t, "Not this account's remote device",
		remoteStatusValue(onlineTestActivity(state.RemoteStateNotRemoteDevice)))
	assert.Equal(t, "Link rejected", remoteStatusValue(onlineTestActivity(state.RemoteStateCredentialRejected)))
	assert.Equal(t, "Server unreachable", remoteStatusValue(onlineTestActivity(state.RemoteStateError)))
}

func TestRemoteStatusDetail(t *testing.T) {
	t.Parallel()

	assert.Contains(t, remoteStatusDetail(nil), "could not be loaded")
	assert.Contains(t, remoteStatusDetail(onlineTestActivity(state.RemoteStateNotRemoteDevice)),
		"Choose this device for remote access on Zaparoo Online.")

	activity := onlineTestActivity(state.RemoteStateError)
	activity.Status.LastErrorCode = "unreachable"
	activity.Status.LastContactAt = "2026-08-30T01:02:03Z"
	detail := remoteStatusDetail(activity)
	assert.Contains(t, detail, "Last error: unreachable")
	assert.Contains(t, detail, "Last contact: 30 Aug 01:02")
}

// onlineTestSettingsWithEndpoints builds a settings response with all three
// configurable Online endpoints set explicitly, for pinning per-feature
// custom-server warnings independently.
func onlineTestSettingsWithEndpoints(backupURL, playtimeURL, remoteControlURL string) *models.SettingsResponse {
	playtimeSyncEnabled := true
	remoteControlEnabled := true
	return &models.SettingsResponse{
		BackupRemoteBaseURL:  &backupURL,
		PlaytimeBaseURL:      &playtimeURL,
		RemoteControlBaseURL: &remoteControlURL,
		PlaytimeSyncEnabled:  &playtimeSyncEnabled,
		RemoteControlEnabled: &remoteControlEnabled,
	}
}

func TestBuildOnlineSettingsMenu_CustomEndpointsShowWarnings_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()
	pages := tview.NewPages()
	mockSvc := NewMockSettingsService()
	mockSvc.SetupGetBackupStatus(backupTestStatus(true))
	mockSvc.SetupGetSettings(onlineTestSettingsWithEndpoints(
		config.DefaultBackupRemoteBaseURL,
		"https://custom-playtime.example.com",
		"https://custom-remote.example.com",
	))
	mockSvc.SetupGetRemoteActivity(onlineTestActivity(state.RemoteStateWaiting))
	mockSvc.SetupUpdateSettingsSuccess()

	runner.Start(pages)
	runner.QueueUpdateDraw(func() {
		buildOnlineSettingsMenu(mockSvc, pages, runner.App(), func() {})
	})

	require.True(t, runner.WaitForText(
		"One or more Zaparoo Online endpoints are set to a custom server.", uiSettleTimeout))

	// Row descriptions only show in the help line for the currently
	// selected row (dynamic help mode). Account, Warp, Unlink account,
	// then Remote control.
	require.True(t, runner.WaitForText("Remote control", uiSettleTimeout))
	runner.SimulateArrowDown()
	runner.SimulateArrowDown()
	runner.SimulateArrowDown()
	assert.True(t, runner.WaitForText("Custom server: custom-remote.example.com.", uiSettleTimeout))

	// Three more down: past Remote status and Remote control activity, to
	// Play history sync.
	runner.SimulateArrowDown()
	runner.SimulateArrowDown()
	runner.SimulateArrowDown()
	assert.True(t, runner.WaitForText("Custom server: custom-playtime.example.com.", uiSettleTimeout))
}

func TestBuildOnlineSettingsMenu_DefaultEndpointsShowNoWarning_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()
	pages := tview.NewPages()
	mockSvc := NewMockSettingsService()
	mockSvc.SetupGetBackupStatus(backupTestStatus(true))
	mockSvc.SetupGetSettings(onlineTestSettingsWithEndpoints(
		config.DefaultBackupRemoteBaseURL, config.DefaultPlaytimeBaseURL, config.DefaultRemoteControlBaseURL,
	))
	mockSvc.SetupGetRemoteActivity(onlineTestActivity(state.RemoteStateWaiting))
	mockSvc.SetupUpdateSettingsSuccess()

	runner.Start(pages)
	runner.QueueUpdateDraw(func() {
		buildOnlineSettingsMenu(mockSvc, pages, runner.App(), func() {})
	})

	require.True(t, runner.WaitForText("Remote control", uiSettleTimeout))
	assert.False(t, runner.ContainsText("Custom server:"))
}

func TestBuildOnlineSettingsMenu_NotLinkedShowsLinkAction_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()
	pages := tview.NewPages()
	mockSvc := NewMockSettingsService()
	mockSvc.SetupGetBackupStatus(backupTestStatus(false))
	mockSvc.SetupGetSettings(onlineTestSettings(config.DefaultBackupRemoteBaseURL))
	mockSvc.SetupGetRemoteActivity(onlineTestActivity(state.RemoteStateUnlinked))

	runner.Start(pages)
	runner.QueueUpdateDraw(func() {
		buildOnlineSettingsMenu(mockSvc, pages, runner.App(), func() {})
	})

	require.True(t, runner.WaitForText("Link account", uiSettleTimeout))
	assert.True(t, runner.ContainsText("Not linked"), "link status shows on the menu line")
	assert.True(t, runner.ContainsText("Play history sync"), "sync consent is configurable before linking")
	assert.True(t, runner.ContainsText("Cloud backup"), "features are discoverable while unlinked")
	assert.False(t, runner.ContainsText("Unlink account"))
	assert.False(t, runner.ContainsText("Warp:"), "Warp status is hidden until an account is linked")
}

func TestBuildOnlineSettingsMenu_LinkedShowsAccountControls_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()
	pages := tview.NewPages()
	mockSvc := NewMockSettingsService()
	mockSvc.SetupGetBackupStatus(backupTestStatus(true))
	mockSvc.SetupGetSettings(onlineTestSettings(config.DefaultBackupRemoteBaseURL))
	mockSvc.SetupGetRemoteActivity(onlineTestActivity(state.RemoteStateWaiting))
	mockSvc.SetupUpdateSettingsSuccess()

	runner.Start(pages)
	runner.QueueUpdateDraw(func() {
		buildOnlineSettingsMenu(mockSvc, pages, runner.App(), func() {})
	})

	require.True(t, runner.WaitForText("Account", uiSettleTimeout))
	assert.True(t, runner.ContainsText("Linked"), "link status shows on the menu line")
	assert.True(t, runner.ContainsText("Warp"), "Warp subscription status shows on the menu line")
	assert.True(t, runner.ContainsText("Remote control"))
	assert.True(t, runner.ContainsText("Waiting for commands"), "remote status shows on the menu line")
	assert.True(t, runner.ContainsText("Play history sync"))
	assert.True(t, runner.ContainsText("Cloud backup"))
	assert.True(t, runner.ContainsText("Unlink account"))
	assert.False(t, runner.ContainsText("Link account"))
}

// TestBuildOnlineSettingsMenu_RemoteStatusExplainsSlot_Integration pins the
// case the status line exists for: remote control is switched on but the
// server refuses this device because it isn't the account's remote slot.
// The menu line says so, and selecting it tells the owner what to do.
func TestBuildOnlineSettingsMenu_RemoteStatusExplainsSlot_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()
	pages := tview.NewPages()
	mockSvc := NewMockSettingsService()
	mockSvc.SetupGetBackupStatus(backupTestStatus(true))
	mockSvc.SetupGetSettings(onlineTestSettings(config.DefaultBackupRemoteBaseURL))
	activity := onlineTestActivity(state.RemoteStateNotRemoteDevice)
	activity.Status.LastErrorCode = "remote_slot_required"
	mockSvc.SetupGetRemoteActivity(activity)
	mockSvc.SetupUpdateSettingsSuccess()

	runner.Start(pages)
	runner.QueueUpdateDraw(func() {
		buildOnlineSettingsMenu(mockSvc, pages, runner.App(), func() {})
	})
	require.True(t, runner.WaitForText("Not this account's remote device", uiSettleTimeout))

	// Account, Warp, Unlink account, Remote control, then Remote status.
	runner.SimulateArrowDown()
	runner.SimulateArrowDown()
	runner.SimulateArrowDown()
	runner.SimulateArrowDown()
	runner.SimulateEnter()
	require.True(t, runner.WaitForText("Choose this device for remote access on Zaparoo Online.", uiSettleTimeout))
	assert.True(t, runner.ContainsText("Last error: remote_slot_required"))
}

// TestBuildOnlineSettingsMenu_RemoteStatusLoadFailureIsNotFatal_Integration
// pins that the page still renders when remote.activity fails: the status
// line reads Unknown instead of the whole Online page failing to load.
func TestBuildOnlineSettingsMenu_RemoteStatusLoadFailureIsNotFatal_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()
	pages := tview.NewPages()
	mockSvc := NewMockSettingsService()
	mockSvc.SetupGetBackupStatus(backupTestStatus(true))
	mockSvc.SetupGetSettings(onlineTestSettings(config.DefaultBackupRemoteBaseURL))
	mockSvc.On("GetRemoteActivity", mock.Anything).Return(nil, errors.New("api unavailable"))
	mockSvc.SetupUpdateSettingsSuccess()

	runner.Start(pages)
	runner.QueueUpdateDraw(func() {
		buildOnlineSettingsMenu(mockSvc, pages, runner.App(), func() {})
	})
	require.True(t, runner.WaitForText("Remote status", uiSettleTimeout))
	assert.True(t, runner.ContainsText("Unknown"))
	assert.False(t, runner.ContainsText("Failed to load online status"))
}

func TestBuildOnlineSettingsMenu_RemoteControlToggleUpdatesConsent_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()
	pages := tview.NewPages()
	mockSvc := NewMockSettingsService()
	mockSvc.SetupGetBackupStatus(backupTestStatus(true))
	mockSvc.SetupGetSettings(onlineTestSettings(config.DefaultBackupRemoteBaseURL))
	mockSvc.SetupGetRemoteActivity(onlineTestActivity(state.RemoteStateWaiting))
	mockSvc.SetupUpdateSettingsSuccess()

	runner.Start(pages)
	runner.QueueUpdateDraw(func() {
		buildOnlineSettingsMenu(mockSvc, pages, runner.App(), func() {})
	})
	require.True(t, runner.WaitForText("Remote control", uiSettleTimeout))

	// Account, Warp, Unlink account, then Remote control.
	runner.SimulateArrowDown()
	runner.SimulateArrowDown()
	runner.SimulateArrowDown()
	runner.SimulateEnter()

	require.True(t, runner.WaitForCondition(func() bool {
		for _, call := range mockSvc.Calls {
			if call.Method != "UpdateSettings" {
				continue
			}
			params, ok := call.Arguments.Get(1).(*models.UpdateSettingsParams)
			if ok && params.RemoteControlEnabled != nil && !*params.RemoteControlEnabled {
				return true
			}
		}
		return false
	}, uiSettleTimeout), "toggle should disable remote control consent")
}

func TestBuildOnlineSettingsMenu_PlayHistoryToggleUpdatesConsent_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()
	pages := tview.NewPages()
	mockSvc := NewMockSettingsService()
	mockSvc.SetupGetBackupStatus(backupTestStatus(true))
	mockSvc.SetupGetSettings(onlineTestSettings(config.DefaultBackupRemoteBaseURL))
	mockSvc.SetupGetRemoteActivity(onlineTestActivity(state.RemoteStateWaiting))
	mockSvc.SetupUpdateSettingsSuccess()

	runner.Start(pages)
	runner.QueueUpdateDraw(func() {
		buildOnlineSettingsMenu(mockSvc, pages, runner.App(), func() {})
	})
	require.True(t, runner.WaitForText("Play history sync", uiSettleTimeout))

	// Account, Warp, Unlink account, Remote control, Remote status, Remote
	// control activity, then Play history sync.
	runner.SimulateArrowDown()
	runner.SimulateArrowDown()
	runner.SimulateArrowDown()
	runner.SimulateArrowDown()
	runner.SimulateArrowDown()
	runner.SimulateArrowDown()
	runner.SimulateEnter()

	require.True(t, runner.WaitForCondition(func() bool {
		for _, call := range mockSvc.Calls {
			if call.Method != "UpdateSettings" {
				continue
			}
			params, ok := call.Arguments.Get(1).(*models.UpdateSettingsParams)
			if ok && params.PlaytimeSyncEnabled != nil && !*params.PlaytimeSyncEnabled {
				return true
			}
		}
		return false
	}, uiSettleTimeout), "toggle should disable playtime sync consent")
}

func TestBuildOnlineSettingsMenu_LinkedShowsDeviceName_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()
	pages := tview.NewPages()
	mockSvc := NewMockSettingsService()
	status := backupTestStatus(true)
	deviceName := "Living Room MiSTer"
	status.Remote.DeviceName = &deviceName
	mockSvc.SetupGetBackupStatus(status)
	mockSvc.SetupGetSettings(onlineTestSettings(config.DefaultBackupRemoteBaseURL))
	mockSvc.SetupGetRemoteActivity(onlineTestActivity(state.RemoteStateWaiting))

	runner.Start(pages)
	runner.QueueUpdateDraw(func() {
		buildOnlineSettingsMenu(mockSvc, pages, runner.App(), func() {})
	})

	require.True(t, runner.WaitForText("Linked as Living Room MiSTer", uiSettleTimeout))
}

func TestBuildOnlineSettingsMenu_CustomServerShownInStatus_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()
	pages := tview.NewPages()
	mockSvc := NewMockSettingsService()
	mockSvc.SetupGetBackupStatus(backupTestStatus(true))
	mockSvc.SetupGetSettings(onlineTestSettings("https://backup.example.com"))
	mockSvc.SetupGetRemoteActivity(onlineTestActivity(state.RemoteStateWaiting))
	mockSvc.SetupUpdateSettingsSuccess()

	runner.Start(pages)
	runner.QueueUpdateDraw(func() {
		buildOnlineSettingsMenu(mockSvc, pages, runner.App(), func() {})
	})

	// The custom server host shows in the help text of the selected
	// Account row.
	require.True(t, runner.WaitForText("This device is linked to backup.example.com", uiSettleTimeout))
}

func TestBuildOnlineSettingsMenu_UnlinkConfirmFlow_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()
	pages := tview.NewPages()
	mockSvc := NewMockSettingsService()
	// First build: linked. After unlinking the page rebuilds: not linked.
	mockSvc.On("GetBackupStatus", mock.Anything).Return(backupTestStatus(true), nil).Once()
	mockSvc.On("GetBackupStatus", mock.Anything).Return(backupTestStatus(false), nil)
	mockSvc.SetupGetSettings(onlineTestSettings(config.DefaultBackupRemoteBaseURL))
	mockSvc.SetupGetRemoteActivity(onlineTestActivity(state.RemoteStateWaiting))
	mockSvc.On("Unlink", mock.Anything).Return(nil).Once()

	runner.Start(pages)
	runner.QueueUpdateDraw(func() {
		buildOnlineSettingsMenu(mockSvc, pages, runner.App(), func() {})
	})
	require.True(t, runner.WaitForText("Unlink account", uiSettleTimeout))

	// Account section: Account row, Warp row, Unlink account.
	runner.SimulateArrowDown()
	runner.SimulateArrowDown()
	runner.SimulateEnter()
	require.True(t, runner.WaitForText("Unlink from Zaparoo Online?", uiSettleTimeout))

	// Confirm ("Yes" is focused first).
	runner.SimulateEnter()
	require.True(t, runner.WaitForText("credentials were removed", uiSettleTimeout))

	// Dismiss the confirmation: the page rebuilds in the unlinked state.
	runner.SimulateEnter()
	require.True(t, runner.WaitForText("Link account", uiSettleTimeout))
	mockSvc.AssertCalled(t, "Unlink", mock.Anything)
}

func TestBuildOnlineSettingsMenu_CloudBackupNavigatesToBackupPage_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()
	pages := tview.NewPages()
	mockSvc := NewMockSettingsService()
	mockSvc.SetupGetBackupStatus(backupTestStatus(true))
	mockSvc.SetupGetSettings(onlineTestSettings(config.DefaultBackupRemoteBaseURL))
	mockSvc.SetupGetRemoteActivity(onlineTestActivity(state.RemoteStateWaiting))
	mockSvc.SetupUpdateSettingsSuccess()

	runner.Start(pages)
	runner.QueueUpdateDraw(func() {
		buildOnlineSettingsMenu(mockSvc, pages, runner.App(), func() {})
	})
	require.True(t, runner.WaitForText("Cloud backup", uiSettleTimeout))

	// Account, Warp, Unlink account, Remote control, Remote status, Remote
	// control activity, Play history sync, then Cloud backup.
	runner.SimulateArrowDown()
	runner.SimulateArrowDown()
	runner.SimulateArrowDown()
	runner.SimulateArrowDown()
	runner.SimulateArrowDown()
	runner.SimulateArrowDown()
	runner.SimulateArrowDown()
	runner.SimulateEnter()

	require.True(t, runner.WaitForText("Automatic backup", uiSettleTimeout),
		"selecting Cloud backup should open the backup settings page")
}
