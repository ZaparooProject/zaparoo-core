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
	"strings"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestProfileCardZapScript(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "**profile:corn-arm-truck", profileCardZapScript("corn-arm-truck"))
}

func TestValidDuration(t *testing.T) {
	t.Parallel()
	assert.True(t, validDuration(""))
	assert.True(t, validDuration("2h30m"))
	assert.True(t, validDuration("0"))
	assert.False(t, validDuration("2 hours"))
	assert.False(t, validDuration("abc"))
}

func TestPINValidation(t *testing.T) {
	t.Parallel()

	assert.True(t, validPIN("", false))
	assert.False(t, validPIN("", true))
	assert.False(t, validPIN("123", false))
	assert.True(t, validPIN("1234", true))
	assert.True(t, validPIN("12345678", true))
	assert.False(t, validPIN("123456789", true))
	assert.False(t, numericPINAcceptance("12a4", '4'))
}

func TestDefaultSettingsService_GetProfiles(t *testing.T) {
	t.Parallel()

	mockClient := mocks.NewMockAPIClient()
	mockClient.On("Call", mock.Anything, models.MethodProfiles, "").
		Return(`{"profiles":[{"profileId":"p1","name":"Kid A","switchId":"corn-arm-truck","hasPin":true}]}`, nil)

	svc := NewSettingsService(mockClient)
	profiles, err := svc.GetProfiles(context.Background())

	require.NoError(t, err)
	require.Len(t, profiles.Profiles, 1)
	assert.Equal(t, "Kid A", profiles.Profiles[0].Name)
	assert.Equal(t, "corn-arm-truck", profiles.Profiles[0].SwitchID)
	assert.True(t, profiles.Profiles[0].HasPIN)
	mockClient.AssertExpectations(t)
}

func TestDefaultSettingsService_GetActiveProfile(t *testing.T) {
	t.Parallel()

	t.Run("active profile", func(t *testing.T) {
		t.Parallel()
		mockClient := mocks.NewMockAPIClient()
		mockClient.On("Call", mock.Anything, models.MethodProfilesActive, "").
			Return(`{"profileId":"p1","name":"Kid A","hasPin":false}`, nil)

		svc := NewSettingsService(mockClient)
		active, err := svc.GetActiveProfile(context.Background())
		require.NoError(t, err)
		require.NotNil(t, active)
		assert.Equal(t, "p1", active.ProfileID)
	})

	t.Run("shared profile is null", func(t *testing.T) {
		t.Parallel()
		mockClient := mocks.NewMockAPIClient()
		mockClient.On("Call", mock.Anything, models.MethodProfilesActive, "").
			Return("null", nil)

		svc := NewSettingsService(mockClient)
		active, err := svc.GetActiveProfile(context.Background())
		require.NoError(t, err)
		assert.Nil(t, active)
	})
}

func TestDefaultSettingsService_NewProfile(t *testing.T) {
	t.Parallel()

	mockClient := mocks.NewMockAPIClient()
	mockClient.On("Call", mock.Anything, models.MethodProfilesNew,
		mock.MatchedBy(func(params string) bool {
			return assert.ObjectsAreEqual(true, params != "") &&
				containsAll(params, `"name":"Kid A"`, `"pin":"1234"`)
		})).
		Return(`{"profileId":"p1","name":"Kid A","switchId":"corn-arm-truck","hasPin":true}`, nil)

	svc := NewSettingsService(mockClient)
	pin := "1234"
	created, err := svc.NewProfile(context.Background(), &models.NewProfileParams{
		Name: "Kid A",
		PIN:  &pin,
	})

	require.NoError(t, err)
	assert.Equal(t, "corn-arm-truck", created.SwitchID)
	mockClient.AssertExpectations(t)
}

func TestDefaultSettingsService_ProfileManagementCredential(t *testing.T) {
	t.Parallel()

	mockClient := mocks.NewMockAPIClient()
	mockClient.On("Call", mock.Anything, models.MethodProfilesVerify,
		mock.MatchedBy(func(params string) bool {
			return containsAll(params, `"profileId":"p1"`, `"pin":"1234"`)
		})).Return(`{"profileId":"p1","name":"Parent","role":"admin","hasPin":true}`, nil)
	svc := NewSettingsService(mockClient)
	require.NoError(t, svc.VerifyProfileManagement(context.Background(), "p1", "1234"))
	mockClient.AssertExpectations(t)
}

func TestDefaultSettingsService_DeleteAndSwitchProfile(t *testing.T) {
	t.Parallel()

	mockClient := mocks.NewMockAPIClient()
	mockClient.On("Call", mock.Anything, models.MethodProfilesDelete, `{"profileId":"p1"}`).
		Return("null", nil)
	mockClient.On("Call", mock.Anything, models.MethodProfilesSwitch, "").
		Return("null", nil)
	mockClient.On("Call", mock.Anything, models.MethodProfilesSwitch,
		mock.MatchedBy(func(params string) bool {
			return containsAll(params, `"switchId":"corn-arm-truck"`)
		})).
		Return(`{"profileId":"p1","name":"Kid A","hasPin":false}`, nil)

	svc := NewSettingsService(mockClient)
	require.NoError(t, svc.DeleteProfile(context.Background(), "p1"))

	switchID := "corn-arm-truck"
	require.NoError(t, svc.SwitchProfile(context.Background(), &models.SwitchProfileParams{
		SwitchID: &switchID,
	}))

	// Nil params deactivates: the request carries no params at all.
	require.NoError(t, svc.SwitchProfile(context.Background(), nil))
	mockClient.AssertExpectations(t)
}

func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			return false
		}
	}
	return true
}

func testProfilesResponse() *models.ProfilesResponse {
	limitsOn := true
	daily := "2h"
	return &models.ProfilesResponse{
		Profiles: []models.ProfileResponse{
			{
				ProfileID:     "p1",
				Name:          "Kid A",
				Role:          "admin",
				SwitchID:      "corn-arm-truck",
				HasPIN:        true,
				LimitsEnabled: &limitsOn,
				DailyLimit:    &daily,
			},
			{
				ProfileID: "p2",
				Name:      "Kid B",
				Role:      "member",
				SwitchID:  "blue-fox-lamp",
			},
		},
	}
}

func TestFormatProfileLastUsed(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	unix := func(at time.Time) *int64 {
		value := at.Unix()
		return &value
	}
	tests := []struct {
		lastUsedAt *int64
		name       string
		expected   string
	}{
		{name: "never", expected: "Never used"},
		{name: "just now", lastUsedAt: unix(now.Add(-30 * time.Second)), expected: "Last used just now"},
		{name: "minutes", lastUsedAt: unix(now.Add(-12 * time.Minute)), expected: "Last used 12m ago"},
		{name: "hours", lastUsedAt: unix(now.Add(-3 * time.Hour)), expected: "Last used 3h ago"},
		{name: "days", lastUsedAt: unix(now.Add(-4 * 24 * time.Hour)), expected: "Last used 4d ago"},
		{name: "date", lastUsedAt: unix(now.Add(-8 * 24 * time.Hour)), expected: "Last used Jul 7, 2026"},
		{name: "future clock", lastUsedAt: unix(now.Add(time.Hour)), expected: "Last used just now"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, formatProfileLastUsed(tt.lastUsedAt, now))
		})
	}
}

func TestBuildProfilesSettingsMenu_ToggleRequiresAdminPIN_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()
	pages := tview.NewPages()
	pages.AddPage(PageSettingsMain, tview.NewTextView().SetText("Settings"), true, false)

	mockSvc := NewMockSettingsService()
	mockSvc.On("GetSettings", mock.Anything).Return(&models.SettingsResponse{
		ProfilesRequireForLaunch: false,
	}, nil)
	mockSvc.SetupGetProfiles(testProfilesResponse())
	mockSvc.On("VerifyProfileManagement", mock.Anything, "p1", "1234").Return(nil)
	updated := make(chan struct{}, 1)
	mockSvc.On("UpdateSettings", mock.Anything, mock.MatchedBy(func(params *models.UpdateSettingsParams) bool {
		return params.ProfilesRequireForLaunch != nil && *params.ProfilesRequireForLaunch
	})).Run(func(mock.Arguments) {
		updated <- struct{}{}
	}).Return(nil)

	runner.Start(pages)
	runner.Draw()
	runner.QueueUpdateDraw(func() {
		buildProfilesSettingsMenu(mockSvc, pages, runner.App())
	})

	require.True(t, runner.WaitForText("Require profile for launch", 100*time.Millisecond))
	assert.True(t, runner.ContainsText("Block media launches until a profile is active"))
	runner.SimulateEnter()
	require.True(t, runner.WaitForText("Profile PIN", 100*time.Millisecond))
	runner.SimulateString("1234")
	runner.SimulateEnter()
	select {
	case <-updated:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for profile setting update")
	}
	mockSvc.AssertExpectations(t)
}

func TestBuildProfilesPage_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()

	pages := tview.NewPages()
	pages.AddPage(PageMain, tview.NewTextView().SetText("Main"), true, false)

	mockSvc := NewMockSettingsService()
	profiles := testProfilesResponse()
	lastUsedAt := time.Now().Add(-3 * time.Hour).Unix()
	profiles.Profiles[0].LastUsedAt = &lastUsedAt
	mockSvc.SetupGetProfiles(profiles)
	mockSvc.SetupGetActiveProfile(&models.ActiveProfile{ProfileID: "p1", Name: "Kid A"})

	runner.Start(pages)
	runner.Draw()

	runner.QueueUpdateDraw(func() {
		BuildProfilesPage(mockSvc, pages, runner.App())
	})

	require.True(t, runner.WaitForText("Profiles", 100*time.Millisecond), "Profiles title should appear")
	assert.True(t, runner.ContainsText("Kid A"), "profile names should be listed")
	assert.True(t, runner.ContainsText("Kid B"), "profile names should be listed")
	assert.True(t, runner.ContainsText("active"), "active profile should be marked")
	assert.True(t, runner.ContainsText("Admin"), "administrator role should be title-cased in the row")
	assert.True(t, runner.ContainsText("Member"), "member role should be title-cased in the row")
	assert.True(t, runner.ContainsText("Last used 3h ago"), "recent usage should be shown in the row")
	assert.True(t, runner.ContainsText("Never used"), "profiles without usage should be identified")
	assert.True(t, runner.ContainsText("Select a profile to edit"), "profile rows should use static page help")
	assert.True(t, runner.ContainsText("New"), "stable New action should be visible")
	// Bearer credentials and PIN status must never render in the list: a
	// bystander watching someone use this menu must not learn them.
	assert.False(t, runner.ContainsText("corn-arm-truck"), "switch IDs must not be displayed")
	assert.False(t, runner.ContainsText("PIN"), "PIN status must not be displayed")
}

func TestBuildProfilesPage_SelectPromptsThenOpensEdit_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()
	pages := tview.NewPages()
	pages.AddPage(PageMain, tview.NewTextView().SetText("Main"), true, false)

	mockSvc := NewMockSettingsService()
	profiles := testProfilesResponse()
	mockSvc.SetupGetProfiles(profiles)
	mockSvc.SetupGetActiveProfile(nil)
	mockSvc.On("VerifyProfileManagement", mock.Anything, "p1", "1234").Return(nil)

	runner.Start(pages)
	runner.Draw()
	runner.QueueUpdateDraw(func() {
		BuildProfilesPage(mockSvc, pages, runner.App())
	})

	require.True(t, runner.WaitForText("Kid A", 100*time.Millisecond))
	assert.True(t, runner.ContainsText("New"))
	assert.True(t, runner.ContainsText("Switch"))
	assert.True(t, runner.ContainsText("Back"))
	runner.SimulateEnter()
	require.True(t, runner.WaitForText("Profile PIN", 100*time.Millisecond))
	runner.SimulateString("1234")
	runner.SimulateEnter()
	require.True(t, runner.WaitForText("Edit", 100*time.Millisecond))
	assert.True(t, runner.ContainsText("Write Card"))
	assert.True(t, runner.ContainsText("Switch ID: ******"))
	assert.False(t, runner.ContainsText("New ID"))
	assert.True(t, runner.ContainsText("Delete"))
	mockSvc.AssertExpectations(t)
}

func TestProfileSwitchModal_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()

	pages := tview.NewPages()
	pages.AddPage(PageMain, tview.NewTextView().SetText("Main"), true, false)

	mockSvc := NewMockSettingsService()
	// Selecting the first entry (shared profile) deactivates: nil params.
	mockSvc.On("SwitchProfile", mock.Anything, (*models.SwitchProfileParams)(nil)).Return(nil)

	runner.Start(pages)
	runner.Draw()

	switched := make(chan bool, 1)
	runner.QueueUpdateDraw(func() {
		showProfileSwitchModal(mockSvc, pages, runner.App(),
			testProfilesResponse().Profiles, "p1", func(ok bool) {
				switched <- ok
			})
	})

	require.True(t, runner.WaitForText("Switch Profile", 100*time.Millisecond), "modal title should appear")
	assert.True(t, runner.ContainsText("Shared profile"), "shared profile entry should be listed")
	assert.True(t, runner.ContainsText("Kid A (active)"), "active profile should be marked")
	assert.True(t, runner.ContainsText("Kid B"), "profiles should be listed")
	assert.False(t, runner.ContainsText("corn-arm-truck"), "switch IDs must not be displayed")

	// Enter on the first entry (shared) switches and closes the modal.
	runner.SimulateEnter()
	select {
	case ok := <-switched:
		assert.True(t, ok, "switch should be reported successful")
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for switch callback")
	}
	mockSvc.AssertExpectations(t)
}

func TestProfileSwitchModal_ProtectedProfileRequiresPIN_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()

	pages := tview.NewPages()
	pages.AddPage(PageMain, tview.NewTextView().SetText("Main"), true, false)

	mockSvc := NewMockSettingsService()
	mockSvc.On("SwitchProfile", mock.Anything, mock.MatchedBy(func(params *models.SwitchProfileParams) bool {
		return params != nil && params.ProfileID != nil && *params.ProfileID == "p1" &&
			params.SwitchID == nil && params.PIN != nil && *params.PIN == "1234"
	})).Return(nil)

	runner.Start(pages)
	runner.Draw()

	switched := make(chan bool, 1)
	runner.QueueUpdateDraw(func() {
		showProfileSwitchModal(mockSvc, pages, runner.App(),
			testProfilesResponse().Profiles, "", func(ok bool) {
				switched <- ok
			})
	})

	// Shared is first; move to protected Kid A. Selecting it opens a PIN
	// prompt and does not send its bearer switch ID behind the user's back.
	runner.SimulateArrowDown()
	runner.SimulateEnter()
	require.True(t, runner.WaitForText("Profile PIN", 100*time.Millisecond))
	mockSvc.AssertNotCalled(t, "SwitchProfile", mock.Anything, mock.Anything)

	// Non-digits are rejected by the field. A short PIN remains local and
	// displays a validation error instead of making an API request.
	runner.SimulateString("a123")
	assert.False(t, runner.ContainsText("123"), "entered PIN must be masked")
	runner.SimulateEnter()
	require.True(t, runner.WaitForText("PIN must be 4 to 8 digits", 100*time.Millisecond))
	mockSvc.AssertNotCalled(t, "SwitchProfile", mock.Anything, mock.Anything)
	runner.SimulateEnter() // dismiss validation error

	runner.SimulateRune('4')
	runner.SimulateEnter()
	select {
	case ok := <-switched:
		assert.True(t, ok)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for protected profile switch")
	}
	mockSvc.AssertExpectations(t)
}

func TestPromptProfileManagement_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()
	pages := tview.NewPages()
	pages.AddPage(PageMain, tview.NewTextView().SetText("Main"), true, false)

	mockSvc := NewMockSettingsService()
	profiles := testProfilesResponse().Profiles
	mockSvc.On("VerifyProfileManagement", mock.Anything, "p1", "1234").Return(nil)

	runner.Start(pages)
	runner.Draw()
	completed := make(chan struct{}, 1)
	runner.QueueUpdateDraw(func() {
		promptProfileManagement(mockSvc, pages, runner.App(), profiles, func() {
			completed <- struct{}{}
		}, nil)
	})

	require.True(t, runner.WaitForText("Profile PIN", 100*time.Millisecond))
	runner.SimulateString("1234")
	runner.SimulateEnter()
	select {
	case <-completed:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for administrator credential")
	}
	mockSvc.AssertExpectations(t)
}

func TestPromptProfileManagement_MultipleAdminsShowsChooser_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()
	pages := tview.NewPages()
	pages.AddPage(PageMain, tview.NewTextView().SetText("Main"), true, false)

	mockSvc := NewMockSettingsService()
	profiles := testProfilesResponse().Profiles
	profiles[1].Role = "admin"

	runner.Start(pages)
	runner.Draw()
	runner.QueueUpdateDraw(func() {
		promptProfileManagement(mockSvc, pages, runner.App(), profiles, func() {}, nil)
	})

	require.True(t, runner.WaitForText("Administrator", 100*time.Millisecond))
	assert.True(t, runner.ContainsText("Kid A"))
	assert.True(t, runner.ContainsText("Kid B"))
}

func TestProfilePINEditModal_ShowsContextualClear_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()
	pages := tview.NewPages()
	pages.AddPage(PageMain, tview.NewTextView().SetText("Main"), true, false)
	runner.Start(pages)
	runner.Draw()

	runner.QueueUpdateDraw(func() {
		showProfilePINEditModal(pages, runner.App(), true, func(string) {}, func() {}, nil)
	})

	require.True(t, runner.WaitForText("Profile PIN", 100*time.Millisecond))
	assert.True(t, runner.ContainsText("Set PIN"))
	assert.True(t, runner.ContainsText("Clear PIN"))
	assert.True(t, runner.ContainsText("Cancel"))
}

func TestProfileSwitchIDModal_RevealsOnlyOnSelection_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()
	pages := tview.NewPages()
	pages.AddPage(PageMain, tview.NewTextView().SetText("Main"), true, false)
	runner.Start(pages)
	runner.Draw()

	runner.QueueUpdateDraw(func() {
		showProfileSwitchIDModal(pages, runner.App(), "corn-arm-truck", func() {}, nil)
	})

	require.True(t, runner.WaitForText("corn-arm-truck", 100*time.Millisecond))
	assert.True(t, runner.ContainsText("Reset"))
}

func TestBuildProfilesPage_EmptyState_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()

	pages := tview.NewPages()
	pages.AddPage(PageMain, tview.NewTextView().SetText("Main"), true, false)

	mockSvc := NewMockSettingsService()
	mockSvc.SetupGetProfiles(&models.ProfilesResponse{})
	mockSvc.SetupGetActiveProfile(nil)

	runner.Start(pages)
	runner.Draw()

	runner.QueueUpdateDraw(func() {
		BuildProfilesPage(mockSvc, pages, runner.App())
	})

	require.True(t, runner.WaitForText("no profiles", 100*time.Millisecond),
		"empty state should explain profile setup")
	assert.True(t, runner.ContainsText("New"), "stable New action should be visible")
}

func TestBuildProfileEditPage_ProfileLookupFailureFailsClosed_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()
	pages := tview.NewPages()
	pages.AddPage(PageMain, tview.NewTextView().SetText("Main"), true, false)
	mockSvc := NewMockSettingsService()
	mockSvc.On("GetProfiles", mock.Anything).Return(nil, errors.New("database unavailable"))

	runner.Start(pages)
	runner.Draw()
	runner.QueueUpdateDraw(func() {
		buildProfileEditPage(mockSvc, pages, runner.App(), nil)
	})

	require.True(t, runner.WaitForText("Failed to load profiles", 100*time.Millisecond))
	assert.False(t, runner.ContainsText("initial administrator"))
	mockSvc.AssertNotCalled(t, "NewProfile", mock.Anything, mock.Anything)
}

func TestBuildProfileEditPage_New_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()

	pages := tview.NewPages()
	pages.AddPage(PageMain, tview.NewTextView().SetText("Main"), true, false)

	mockSvc := NewMockSettingsService()
	mockSvc.SetupGetProfiles(&models.ProfilesResponse{})

	runner.Start(pages)
	runner.Draw()

	runner.QueueUpdateDraw(func() {
		buildProfileEditPage(mockSvc, pages, runner.App(), nil)
	})

	require.True(t, runner.WaitForText("New", 100*time.Millisecond), "New title should appear")
	assert.True(t, runner.ContainsText("Name"), "Name field should be visible")
	assert.True(t, runner.ContainsText("PIN"), "PIN field should be visible")
	assert.True(t, runner.ContainsText("Limits"), "Limits toggle should be visible")
	assert.True(t, runner.ContainsText("Daily limit"), "Daily limit field should be visible")
	assert.True(t, runner.ContainsText("Session limit"), "Session limit field should be visible")
	assert.True(t, runner.ContainsText("Save"), "Save button should be visible")
}

func TestBuildProfileEditPage_Edit_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()

	pages := tview.NewPages()
	pages.AddPage(PageMain, tview.NewTextView().SetText("Main"), true, false)

	mockSvc := NewMockSettingsService()
	profiles := testProfilesResponse()
	mockSvc.SetupGetProfiles(profiles)

	runner.Start(pages)
	runner.Draw()

	runner.QueueUpdateDraw(func() {
		buildProfileEditPage(mockSvc, pages, runner.App(), &profiles.Profiles[0])
	})

	require.True(t, runner.WaitForText("Edit", 100*time.Millisecond), "Edit title should appear")
	assert.True(t, runner.ContainsText("Kid A"), "existing name should be pre-filled")
	assert.True(t, runner.ContainsText("PIN: ******"), "existing PIN should use a fixed mask")
	assert.True(t, runner.ContainsText("Switch ID: ******"), "switch ID should be masked until selected")
	assert.False(t, runner.ContainsText("corn-arm-truck"), "switch ID should not appear directly in editor")
	assert.False(t, runner.ContainsText("Current PIN"), "implementation state should not be exposed")
	assert.True(t, runner.ContainsText("2h"), "existing daily limit should be pre-filled")
}
