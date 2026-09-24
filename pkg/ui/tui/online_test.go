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
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func onlineTestSettings(baseURL string) *models.SettingsResponse {
	playtimeSyncEnabled := true
	remoteControlEnabled := true
	return &models.SettingsResponse{
		OnlineBaseURL:        &baseURL,
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
	assert.Empty(t, onlineServerHost(onlineTestSettings(config.DefaultOnlineBaseURL)))
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

func TestRemoteStatusValue(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "Unknown", remoteStatusValue(nil))
	assert.Equal(t, "Unknown", remoteStatusValue(onlineTestActivity("")))
	assert.Equal(t, "Off", remoteStatusValue(onlineTestActivity(state.RemoteStateDisabled)))
	assert.Equal(t, "Not linked", remoteStatusValue(onlineTestActivity(state.RemoteStateUnlinked)))
	assert.Equal(t, "Waiting for commands", remoteStatusValue(onlineTestActivity(state.RemoteStateWaiting)))
	assert.Equal(t, "Not the remote device",
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

func TestBuildOnlineSettingsMenu_CustomServerShowsWarning_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()
	pages := tview.NewPages()
	mockSvc := NewMockSettingsService()
	mockSvc.SetupGetBackupStatus(backupTestStatus(true))
	mockSvc.SetupGetSettings(onlineTestSettings("https://custom.example.com"))
	mockSvc.SetupGetRemoteActivity(onlineTestActivity(state.RemoteStateWaiting))
	mockSvc.SetupUpdateSettingsSuccess()

	runner.Start(pages)
	runner.QueueUpdateDraw(func() {
		buildOnlineSettingsMenu(mockSvc, pages, runner.App(), func() {})
	})

	require.True(t, runner.WaitForText("Custom server: custom.example.com.", uiSettleTimeout))
}

func TestBuildOnlineSettingsMenu_DefaultServerShowsNoWarning_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()
	pages := tview.NewPages()
	mockSvc := NewMockSettingsService()
	mockSvc.SetupGetBackupStatus(backupTestStatus(true))
	mockSvc.SetupGetSettings(onlineTestSettings(config.DefaultOnlineBaseURL))
	mockSvc.SetupGetRemoteActivity(onlineTestActivity(state.RemoteStateWaiting))
	mockSvc.SetupUpdateSettingsSuccess()

	runner.Start(pages)
	runner.QueueUpdateDraw(func() {
		buildOnlineSettingsMenu(mockSvc, pages, runner.App(), func() {})
	})

	require.True(t, runner.WaitForText("Remote control", uiSettleTimeout))
	assert.False(t, runner.ContainsText("Custom server:"))
}

// updateRecorder keeps every UpdateSettings request so tests can wait for
// one without reading the mock's call list while the UI goroutine writes it.
type updateRecorder struct {
	params []*models.UpdateSettingsParams
	mu     syncutil.Mutex
}

// recordUpdateSettings makes UpdateSettings return err and records its
// params.
func recordUpdateSettings(mockSvc *MockSettingsService, err error) *updateRecorder {
	recorder := &updateRecorder{}
	mockSvc.On("UpdateSettings", mock.Anything, mock.Anything).Return(err).Run(func(args mock.Arguments) {
		params, ok := args.Get(1).(*models.UpdateSettingsParams)
		if !ok {
			return
		}
		recorder.mu.Lock()
		defer recorder.mu.Unlock()
		recorder.params = append(recorder.params, params)
	})
	return recorder
}

// seen reports whether a recorded update matches.
func (r *updateRecorder) seen(match func(*models.UpdateSettingsParams) bool) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, params := range r.params {
		if match(params) {
			return true
		}
	}
	return false
}

// startOnlinePage builds the Online page against mockSvc and waits for it.
func startOnlinePage(t *testing.T, runner *TestAppRunner, mockSvc *MockSettingsService) *tview.Pages {
	t.Helper()
	pages := tview.NewPages()
	runner.Start(pages)
	runner.QueueUpdateDraw(func() {
		buildOnlineSettingsMenu(mockSvc, pages, runner.App(), func() {})
	})
	require.True(t, runner.WaitForText("All online features", uiSettleTimeout))
	return pages
}

// linkedOnlineMock returns a service mock for a linked device with the given
// Warp availability and settings.
func linkedOnlineMock(availability string, settings *models.SettingsResponse) *MockSettingsService {
	mockSvc := NewMockSettingsService()
	status := backupTestStatus(true)
	status.Remote.Availability = availability
	mockSvc.SetupGetBackupStatus(status)
	mockSvc.SetupGetSettings(settings)
	mockSvc.SetupGetRemoteActivity(onlineTestActivity(state.RemoteStateWaiting))
	return mockSvc
}

func allFeatureSettings(on bool) *models.SettingsResponse {
	settings := onlineTestSettings(config.DefaultOnlineBaseURL)
	settings.RemoteControlEnabled = &on
	settings.PlaytimeSyncEnabled = &on
	settings.LibrarySyncEnabled = &on
	settings.BackupRemoteEnabled = &on
	return settings
}

func boolIs(value *bool, want bool) bool {
	return value != nil && *value == want
}

func TestBuildOnlineSettingsMenu_NotLinkedDisablesFeatures_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()
	mockSvc := NewMockSettingsService()
	mockSvc.SetupGetBackupStatus(backupTestStatus(false))
	mockSvc.SetupGetSettings(onlineTestSettings(config.DefaultOnlineBaseURL))
	mockSvc.SetupGetRemoteActivity(onlineTestActivity(state.RemoteStateUnlinked))
	startOnlinePage(t, runner, mockSvc)

	assert.True(t, runner.ContainsText("Link account"))
	assert.True(t, runner.ContainsText("Status: Not linked"), "link status shows on the menu line")
	assert.True(t, runner.ContainsText("Play history sync"), "features stay visible while unlinked")
	assert.True(t, runner.ContainsText("Library sync"))
	assert.True(t, runner.ContainsText("Cloud backup"))
	assert.False(t, runner.ContainsText("Unlink account"))
	assert.False(t, runner.ContainsText("Warp:"), "Warp status is hidden until an account is linked")

	// Status, Link account, then "All online features". Linking resets
	// consent, so the feature rows explain that and ignore activation.
	runner.SimulateArrowDown()
	runner.SimulateArrowDown()
	require.True(t, runner.WaitForText(onlineLinkFirstDesc, uiSettleTimeout))
	runner.SimulateEnter()
	runner.SimulateArrowRight()
	require.True(t, runner.WaitForText("- [ ] Library sync", uiSettleTimeout))
	runner.SimulateEnter()
	runner.SimulateKey(tcell.KeyRune, ' ', tcell.ModNone)
	mockSvc.AssertNotCalled(t, "UpdateSettings", mock.Anything, mock.Anything)
}

func TestBuildOnlineSettingsMenu_LinkedShowsAccountControls_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()
	mockSvc := linkedOnlineMock("", onlineTestSettings(config.DefaultOnlineBaseURL))
	startOnlinePage(t, runner, mockSvc)

	assert.True(t, runner.ContainsText("Status: Linked"), "link status shows on the menu line")
	assert.True(t, runner.ContainsText("Warp: Checking..."), "Warp subscription status shows on the menu line")
	assert.True(t, runner.ContainsText("Remote control"))
	assert.True(t, runner.ContainsText("Status: Waiting for commands"), "remote status shows on the menu line")
	assert.True(t, runner.ContainsText("Play history sync"))
	assert.True(t, runner.ContainsText("Library sync"))
	assert.True(t, runner.ContainsText("Cloud backup"))
	assert.True(t, runner.ContainsText("Schedule: < daily >"))
	assert.True(t, runner.ContainsText("Manage backups"))
	assert.True(t, runner.ContainsText("Unlink account"))
	assert.False(t, runner.ContainsText("Link account"))
	assert.True(t, runner.ContainsText("- [-] All online features"),
		"remote control and play history on, library sync off reads as mixed")
}

// TestBuildOnlineSettingsMenu_FitsCRTWithoutScrolling pins that every row of
// the linked page is on screen in the fixed 75x15 CRT window.
func TestBuildOnlineSettingsMenu_FitsCRTWithoutScrolling_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 75, 15)
	defer runner.Stop()
	mockSvc := linkedOnlineMock(warpAvailable, onlineTestSettings("https://custom.example.com"))
	startOnlinePage(t, runner, mockSvc)

	for _, text := range []string{
		"Status: Linked", "Warp: Active", "Unlink account", "Status: Waiting for commands", "Activity",
		"All online features", "Remote control", "Play history sync", "Library sync", "Cloud backup",
		"Schedule: < daily >", "Manage backups", "Custom server: custom.example.com.",
	} {
		assert.True(t, runner.ContainsText(text), "missing %q", text)
	}
}

// TestBuildOnlineSettingsMenu_RemoteStatusExplainsSlot_Integration pins the
// case the status line exists for: remote control is switched on but the
// server refuses this device because it isn't the account's remote slot.
// The menu line says so, and selecting it tells the owner what to do.
func TestBuildOnlineSettingsMenu_RemoteStatusExplainsSlot_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()
	mockSvc := NewMockSettingsService()
	mockSvc.SetupGetBackupStatus(backupTestStatus(true))
	mockSvc.SetupGetSettings(onlineTestSettings(config.DefaultOnlineBaseURL))
	activity := onlineTestActivity(state.RemoteStateNotRemoteDevice)
	activity.Status.LastErrorCode = "remote_slot_required"
	mockSvc.SetupGetRemoteActivity(activity)
	startOnlinePage(t, runner, mockSvc)
	require.True(t, runner.WaitForText("Not the remote device", uiSettleTimeout))

	// Right column: Remote control, three more feature toggles, then the
	// remote Status.
	runner.SimulateArrowRight()
	for range 4 {
		runner.SimulateArrowDown()
	}
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
	mockSvc := NewMockSettingsService()
	mockSvc.SetupGetBackupStatus(backupTestStatus(true))
	mockSvc.SetupGetSettings(onlineTestSettings(config.DefaultOnlineBaseURL))
	mockSvc.On("GetRemoteActivity", mock.Anything).Return(nil, errors.New("api unavailable"))
	startOnlinePage(t, runner, mockSvc)

	assert.True(t, runner.ContainsText("Status: Unknown"))
	assert.False(t, runner.ContainsText("Failed to load online status"))
}

func TestBuildOnlineSettingsMenu_FeatureTogglesUpdateConsent_Integration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		match func(*models.UpdateSettingsParams) bool
		name  string
		downs int
	}{
		{
			name: "remote control", downs: 0,
			match: func(p *models.UpdateSettingsParams) bool { return boolIs(p.RemoteControlEnabled, false) },
		},
		{
			name: "play history sync", downs: 1,
			match: func(p *models.UpdateSettingsParams) bool { return boolIs(p.PlaytimeSyncEnabled, false) },
		},
		{
			name: "library sync", downs: 2,
			match: func(p *models.UpdateSettingsParams) bool { return boolIs(p.LibrarySyncEnabled, true) },
		},
		{
			name: "cloud backup", downs: 3,
			match: func(p *models.UpdateSettingsParams) bool { return boolIs(p.BackupRemoteEnabled, true) },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			runner := NewTestAppRunner(t, 80, 25)
			defer runner.Stop()
			mockSvc := linkedOnlineMock(warpAvailable, onlineTestSettings(config.DefaultOnlineBaseURL))
			updates := recordUpdateSettings(mockSvc, nil)
			startOnlinePage(t, runner, mockSvc)

			runner.SimulateArrowRight()
			for range tt.downs {
				runner.SimulateArrowDown()
			}
			runner.SimulateEnter()
			require.True(t, runner.WaitForCondition(func() bool {
				return updates.seen(tt.match)
			}, uiSettleTimeout))
		})
	}
}

func TestBuildOnlineSettingsMenu_FeatureToggleRevertsOnFailure_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()
	mockSvc := linkedOnlineMock(warpAvailable, allFeatureSettings(false))
	recordUpdateSettings(mockSvc, errors.New("save failed"))
	startOnlinePage(t, runner, mockSvc)

	// Right lands on Remote control, the first feature toggle.
	runner.SimulateArrowRight()
	runner.SimulateEnter()
	require.True(t, runner.WaitForText("Failed to save remote control setting", uiSettleTimeout))
	assert.True(t, runner.ContainsText("- [ ] Remote control"))
}

func TestBuildOnlineSettingsMenu_EnableAll_Integration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		availability string
		wantBackup   bool
	}{
		{name: "Warp active turns on cloud backup", availability: warpAvailable, wantBackup: true},
		{name: "no Warp leaves cloud backup alone", availability: warpUnavailable, wantBackup: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			runner := NewTestAppRunner(t, 80, 25)
			defer runner.Stop()
			mockSvc := linkedOnlineMock(tt.availability, allFeatureSettings(false))
			updates := recordUpdateSettings(mockSvc, nil)
			startOnlinePage(t, runner, mockSvc)
			require.True(t, runner.ContainsText("- [ ] All online features"))

			// Status, Warp, then "All online features".
			runner.SimulateArrowDown()
			runner.SimulateArrowDown()
			runner.SimulateEnter()
			require.True(t, runner.WaitForCondition(func() bool {
				return updates.seen(func(p *models.UpdateSettingsParams) bool {
					backupOK := p.BackupRemoteEnabled == nil
					if tt.wantBackup {
						backupOK = boolIs(p.BackupRemoteEnabled, true)
					}
					return boolIs(p.RemoteControlEnabled, true) && boolIs(p.PlaytimeSyncEnabled, true) &&
						boolIs(p.LibrarySyncEnabled, true) && backupOK
				})
			}, uiSettleTimeout))
			require.True(t, runner.WaitForText("- [*] All online features", uiSettleTimeout))
			assert.True(t, runner.ContainsText("- [*] Library sync"))
			if tt.wantBackup {
				assert.True(t, runner.ContainsText("- [*] Cloud backup"))
			} else {
				assert.True(t, runner.ContainsText("- [ ] Cloud backup"))
			}
			assert.False(t, runner.ContainsText("Warp"+" is required"), "no upsell message is shown")
		})
	}
}

// TestBuildOnlineSettingsMenu_EnableAllWaitsForWarpCheck_Integration pins
// that an unknown Warp status is re-read before deciding on cloud backup.
func TestBuildOnlineSettingsMenu_EnableAllWaitsForWarpCheck_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()
	mockSvc := NewMockSettingsService()
	unknown := backupTestStatus(true)
	unknown.Remote.Availability = "unknown"
	available := backupTestStatus(true)
	available.Remote.Availability = warpAvailable
	mockSvc.On("GetBackupStatus", mock.Anything).Return(unknown, nil).Once()
	mockSvc.On("GetBackupStatus", mock.Anything).Return(available, nil)
	mockSvc.SetupGetSettings(allFeatureSettings(false))
	mockSvc.SetupGetRemoteActivity(onlineTestActivity(state.RemoteStateWaiting))
	updates := recordUpdateSettings(mockSvc, nil)
	startOnlinePage(t, runner, mockSvc)

	// Status, Warp, then "All online features".
	runner.SimulateArrowDown()
	runner.SimulateArrowDown()
	runner.SimulateEnter()
	require.True(t, runner.WaitForText("Checking Zaparoo Warp...", uiSettleTimeout))
	require.True(t, runner.WaitForCondition(func() bool {
		return updates.seen(func(p *models.UpdateSettingsParams) bool {
			return boolIs(p.BackupRemoteEnabled, true) && boolIs(p.LibrarySyncEnabled, true)
		})
	}, uiSettleTimeout))
}

func TestBuildOnlineSettingsMenu_DisableAll_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()
	mockSvc := linkedOnlineMock(warpUnavailable, allFeatureSettings(true))
	updates := recordUpdateSettings(mockSvc, nil)
	startOnlinePage(t, runner, mockSvc)
	require.True(t, runner.ContainsText("- [*] All online features"))

	// Status, Warp, then "All online features".
	runner.SimulateArrowDown()
	runner.SimulateArrowDown()
	runner.SimulateEnter()
	require.True(t, runner.WaitForCondition(func() bool {
		return updates.seen(func(p *models.UpdateSettingsParams) bool {
			return boolIs(p.RemoteControlEnabled, false) && boolIs(p.PlaytimeSyncEnabled, false) &&
				boolIs(p.LibrarySyncEnabled, false) && boolIs(p.BackupRemoteEnabled, false)
		})
	}, uiSettleTimeout))
	require.True(t, runner.WaitForText("- [ ] All online features", uiSettleTimeout))
	assert.True(t, runner.ContainsText("- [ ] Cloud backup"))
}

func TestBuildOnlineSettingsMenu_EnableAllRevertsOnFailure_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()
	mockSvc := linkedOnlineMock(warpAvailable, allFeatureSettings(false))
	recordUpdateSettings(mockSvc, errors.New("save failed"))
	startOnlinePage(t, runner, mockSvc)

	// Status, Warp, then "All online features".
	runner.SimulateArrowDown()
	runner.SimulateArrowDown()
	runner.SimulateEnter()
	require.True(t, runner.WaitForText("Failed to save online features", uiSettleTimeout))
	assert.True(t, runner.ContainsText("- [ ] All online features"))
	assert.True(t, runner.ContainsText("- [ ] Library sync"))
}

func TestBuildOnlineSettingsMenu_LinkedShowsDeviceName_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()
	mockSvc := NewMockSettingsService()
	status := backupTestStatus(true)
	deviceName := "Living Room MiSTer"
	status.Remote.DeviceName = &deviceName
	mockSvc.SetupGetBackupStatus(status)
	mockSvc.SetupGetSettings(onlineTestSettings(config.DefaultOnlineBaseURL))
	mockSvc.SetupGetRemoteActivity(onlineTestActivity(state.RemoteStateWaiting))
	startOnlinePage(t, runner, mockSvc)

	assert.True(t, runner.ContainsText("Linked as: Living Room MiSTer"))
}

func TestBuildOnlineSettingsMenu_CustomServerShownInStatus_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()
	mockSvc := linkedOnlineMock("", onlineTestSettings("https://backup.example.com"))
	startOnlinePage(t, runner, mockSvc)

	// The custom server host shows in the help text of the selected
	// account status row.
	require.True(t, runner.WaitForText("This device is linked to backup.example.com", uiSettleTimeout))
}

func TestBuildOnlineSettingsMenu_UnlinkConfirmFlow_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()
	mockSvc := NewMockSettingsService()
	// First build: linked. After unlinking the page rebuilds: not linked.
	mockSvc.On("GetBackupStatus", mock.Anything).Return(backupTestStatus(true), nil).Once()
	mockSvc.On("GetBackupStatus", mock.Anything).Return(backupTestStatus(false), nil)
	mockSvc.SetupGetSettings(onlineTestSettings(config.DefaultOnlineBaseURL))
	mockSvc.SetupGetRemoteActivity(onlineTestActivity(state.RemoteStateWaiting))
	mockSvc.On("Unlink", mock.Anything).Return(nil).Once()
	startOnlinePage(t, runner, mockSvc)

	// Account section: Status, Warp, All online features, then Unlink account.
	for range 3 {
		runner.SimulateArrowDown()
	}
	runner.SimulateEnter()
	require.True(t, runner.WaitForText("Unlink from Zaparoo Online?", uiSettleTimeout))
	assert.True(t, runner.ContainsText("This turns off remote control, play history"),
		"the warning names every feature unlinking switches off")

	// Confirm ("Yes" is focused first).
	runner.SimulateEnter()
	require.True(t, runner.WaitForText("credentials were removed", uiSettleTimeout))

	// Dismiss the confirmation: the page rebuilds in the unlinked state.
	runner.SimulateEnter()
	require.True(t, runner.WaitForText("Link account", uiSettleTimeout))
	mockSvc.AssertCalled(t, "Unlink", mock.Anything)
}

func TestBuildOnlineSettingsMenu_ManageBackupsNavigatesToBackupPage_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()
	mockSvc := linkedOnlineMock("", onlineTestSettings(config.DefaultOnlineBaseURL))
	startOnlinePage(t, runner, mockSvc)

	// Left column: Status, Warp, All online features, Unlink account,
	// Schedule, then Manage backups.
	for range 5 {
		runner.SimulateArrowDown()
	}
	runner.SimulateEnter()

	require.True(t, runner.WaitForText("Automatic backup", uiSettleTimeout),
		"selecting Manage backups should open the backup settings page")
}

func TestOnlineFeaturesTriState(t *testing.T) {
	t.Parallel()

	all := onlineFeatures{remoteControl: true, playHistory: true, library: true, cloudBackup: true}
	assert.Equal(t, TriStateOn, all.triState(warpAvailable))
	assert.Equal(t, TriStateOff, onlineFeatures{}.triState(warpAvailable))

	noBackup := onlineFeatures{remoteControl: true, playHistory: true, library: true}
	assert.Equal(t, TriStateMixed, noBackup.triState(warpAvailable))
	assert.Equal(t, TriStateOn, noBackup.triState(warpUnavailable),
		"without Warp, cloud backup does not count against all on")
	assert.Equal(t, TriStateOn, noBackup.triState("unknown"))
	assert.Equal(t, TriStateOff, onlineFeatures{cloudBackup: true}.triState(warpUnavailable))
}

func TestAllOnlineFeaturesUpdate(t *testing.T) {
	t.Parallel()

	current := onlineFeatures{cloudBackup: true}
	next, params := allOnlineFeaturesUpdate(current, true, warpUnavailable)
	assert.Nil(t, params.BackupRemoteEnabled, "turning on never touches cloud backup without Warp")
	assert.True(t, next.cloudBackup, "an existing cloud backup setting is left as it is")
	assert.True(t, boolIs(params.LibrarySyncEnabled, true))

	next, params = allOnlineFeaturesUpdate(onlineFeatures{}, true, warpAvailable)
	assert.True(t, boolIs(params.BackupRemoteEnabled, true))
	assert.True(t, next.cloudBackup)

	next, params = allOnlineFeaturesUpdate(onlineFeatures{cloudBackup: true, library: true}, false, "")
	assert.True(t, boolIs(params.BackupRemoteEnabled, false), "turning off always includes cloud backup")
	assert.Equal(t, onlineFeatures{}, next)
}

func TestResolveWarpAvailability(t *testing.T) {
	t.Parallel()

	t.Run("known availability returns immediately", func(t *testing.T) {
		t.Parallel()
		mockSvc := NewMockSettingsService()
		assert.Equal(t, warpUnavailable,
			resolveWarpAvailability(context.Background(), mockSvc, warpUnavailable, 3, time.Millisecond))
		mockSvc.AssertNotCalled(t, "GetBackupStatus", mock.Anything)
	})

	t.Run("re-reads until the check completes", func(t *testing.T) {
		t.Parallel()
		mockSvc := NewMockSettingsService()
		unknown := backupTestStatus(true)
		unknown.Remote.Availability = "unknown"
		available := backupTestStatus(true)
		available.Remote.Availability = warpAvailable
		mockSvc.On("GetBackupStatus", mock.Anything).Return(unknown, nil).Once()
		mockSvc.On("GetBackupStatus", mock.Anything).Return(available, nil).Once()
		assert.Equal(t, warpAvailable,
			resolveWarpAvailability(context.Background(), mockSvc, "unknown", 5, time.Millisecond))
		mockSvc.AssertNumberOfCalls(t, "GetBackupStatus", 2)
	})

	t.Run("gives up while still unknown", func(t *testing.T) {
		t.Parallel()
		mockSvc := NewMockSettingsService()
		mockSvc.On("GetBackupStatus", mock.Anything).Return(nil, errors.New("offline")).Times(3)
		assert.Equal(t, "unknown",
			resolveWarpAvailability(context.Background(), mockSvc, "unknown", 3, time.Millisecond))
		mockSvc.AssertNumberOfCalls(t, "GetBackupStatus", 3)
	})

	t.Run("stops when cancelled", func(t *testing.T) {
		t.Parallel()
		mockSvc := NewMockSettingsService()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		assert.Empty(t, resolveWarpAvailability(ctx, mockSvc, "", 5, time.Hour))
		mockSvc.AssertNotCalled(t, "GetBackupStatus", mock.Anything)
	})
}
