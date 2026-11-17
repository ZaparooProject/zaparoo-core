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
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/kiosk"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// startKioskMenu opens the Kiosk page over a mock service and returns the
// runner once the page has drawn.
func startKioskMenu(
	t *testing.T, mockSvc *MockSettingsService, rebuildMain func(), waitFor string,
) (*TestAppRunner, *tview.Pages) {
	t.Helper()
	runner := NewTestAppRunner(t, 80, 25)
	t.Cleanup(runner.Stop)
	pages := newKioskPages()
	runner.Start(pages)
	runner.Draw()
	runner.QueueUpdateDraw(func() {
		buildKioskSettingsMenu(mockSvc, pages, runner.App(), rebuildMain)
	})
	require.True(t, runner.WaitForText(waitFor, uiSettleTimeout))
	return runner, pages
}

func waitCalled(t *testing.T, called <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-called:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}

func down(runner *TestAppRunner, times int) {
	for range times {
		runner.SimulateArrowDown()
	}
}

func TestBuildKioskSettingsMenu_LoadFailureIsShown_Integration(t *testing.T) {
	t.Parallel()
	mockSvc := NewMockSettingsService()
	mockSvc.On("GetKiosk", mock.Anything).Return((*models.KioskResponse)(nil), errors.New("boom"))
	startKioskMenu(t, mockSvc, nil, "Failed to load kiosk mode")
}

func TestBuildKioskSettingsMenu_LockNow_Integration(t *testing.T) {
	t.Parallel()
	mockSvc := NewMockSettingsService()
	mockSvc.On("GetKiosk", mock.Anything).Return(kioskState("unlocked"), nil)
	locked := make(chan struct{}, 1)
	mockSvc.On("LockKiosk", mock.Anything).Run(func(mock.Arguments) { locked <- struct{}{} }).
		Return(kioskState("locked"), nil)
	rebuilt := make(chan struct{}, 1)

	runner, _ := startKioskMenu(t, mockSvc, func() { rebuilt <- struct{}{} }, "Lock now")
	down(runner, 2)
	runner.SimulateEnter()
	waitCalled(t, locked, "kiosk lock")
	waitCalled(t, rebuilt, "the main page rebuild")
}

func TestBuildKioskSettingsMenu_LockFailureIsShown_Integration(t *testing.T) {
	t.Parallel()
	mockSvc := NewMockSettingsService()
	mockSvc.On("GetKiosk", mock.Anything).Return(kioskState("unlocked"), nil)
	mockSvc.On("LockKiosk", mock.Anything).Return((*models.KioskResponse)(nil), errors.New("boom"))

	runner, _ := startKioskMenu(t, mockSvc, nil, "Lock now")
	down(runner, 2)
	runner.SimulateEnter()
	require.True(t, runner.WaitForText("Failed to lock", uiSettleTimeout))
}

func TestBuildKioskSettingsMenu_TurnOffConfirms_Integration(t *testing.T) {
	t.Parallel()
	mockSvc := NewMockSettingsService()
	mockSvc.On("GetKiosk", mock.Anything).Return(kioskState("unlocked"), nil).Once()
	mockSvc.On("GetKiosk", mock.Anything).Return(kioskState("off"), nil)
	disabled := make(chan struct{}, 1)
	mockSvc.On("DisableKiosk", mock.Anything).Run(func(mock.Arguments) { disabled <- struct{}{} }).
		Return(kioskState("off"), nil)

	runner, _ := startKioskMenu(t, mockSvc, nil, "Turn off")
	down(runner, 3)
	runner.SimulateEnter()
	require.True(t, runner.WaitForText("Turn kiosk mode off?", uiSettleTimeout))
	runner.SimulateEnter()
	waitCalled(t, disabled, "kiosk disable")
	// The page is rebuilt from the new state and offers the presets again.
	require.True(t, runner.WaitForText("Turn on: Family", uiSettleTimeout))
}

func TestBuildKioskSettingsMenu_TurnOffFailureIsShown_Integration(t *testing.T) {
	t.Parallel()
	mockSvc := NewMockSettingsService()
	mockSvc.On("GetKiosk", mock.Anything).Return(kioskState("unlocked"), nil)
	mockSvc.On("DisableKiosk", mock.Anything).Return((*models.KioskResponse)(nil), errors.New("boom"))

	runner, _ := startKioskMenu(t, mockSvc, nil, "Turn off")
	down(runner, 3)
	runner.SimulateEnter()
	require.True(t, runner.WaitForText("Turn kiosk mode off?", uiSettleTimeout))
	runner.SimulateEnter()
	require.True(t, runner.WaitForText("Failed to turn kiosk mode off", uiSettleTimeout))
}

func TestBuildKioskSettingsMenu_SealExplainsWhatIsOpen_Integration(t *testing.T) {
	t.Parallel()
	mockSvc := NewMockSettingsService()
	mockSvc.On("GetKiosk", mock.Anything).Return(kioskState("unlocked"), nil)

	runner, _ := startKioskMenu(t, mockSvc, nil, "Not sealed")
	down(runner, 1)
	runner.SimulateEnter()
	require.True(t, runner.WaitForText("Zaparoo is locked, but", uiSettleTimeout))
	assert.True(t, runner.ContainsText("own menus are not locked"))
}

func TestBuildKioskSettingsMenu_PublicNeedsAnAdminClient_Integration(t *testing.T) {
	t.Parallel()
	mockSvc := NewMockSettingsService()
	mockSvc.On("GetKiosk", mock.Anything).Return(kioskState("off"), nil)
	mockSvc.On("EnableKiosk", mock.Anything, kiosk.PresetPublic, false).
		Return((*models.KioskResponse)(nil), fmt.Errorf("failed to call kiosk.enable: %w", kiosk.ErrNoAdminClient))

	runner, _ := startKioskMenu(t, mockSvc, nil, "Turn on: Public")
	down(runner, 2)
	runner.SimulateEnter()
	require.True(t, runner.WaitForText("Lock this device for public use?", uiSettleTimeout))
	runner.SimulateEnter()
	require.True(t, runner.WaitForText("Pair an app as administrator first", uiSettleTimeout))
}

func TestBuildKioskSettingsMenu_NothingAllowedAsksAgain_Integration(t *testing.T) {
	t.Parallel()
	mockSvc := NewMockSettingsService()
	mockSvc.On("GetKiosk", mock.Anything).Return(kioskState("off"), nil)
	mockSvc.On("EnableKiosk", mock.Anything, kiosk.PresetFamily, false).
		Return((*models.KioskResponse)(nil), fmt.Errorf("failed to call kiosk.enable: %w", kiosk.ErrNothingAllowed))
	confirmed := make(chan struct{}, 1)
	mockSvc.On("EnableKiosk", mock.Anything, kiosk.PresetFamily, true).
		Run(func(mock.Arguments) { confirmed <- struct{}{} }).Return(kioskState("locked"), nil)
	rebuilt := make(chan struct{}, 1)

	runner, _ := startKioskMenu(t, mockSvc, func() { rebuilt <- struct{}{} }, "Turn on: Family")
	down(runner, 1)
	runner.SimulateEnter()
	require.True(t, runner.WaitForText("Lock this device for family use?", uiSettleTimeout))
	runner.SimulateEnter()
	// Locking into nothing is the owner's call, so it is asked, not assumed.
	require.True(t, runner.WaitForText("Nothing will launch on that profile", uiSettleTimeout))
	runner.SimulateEnter()
	waitCalled(t, confirmed, "the confirmed enable")
	waitCalled(t, rebuilt, "the main page rebuild")
}

func TestBuildKioskSettingsMenu_DecliningTheWarningLocksNothing_Integration(t *testing.T) {
	t.Parallel()
	mockSvc := NewMockSettingsService()
	mockSvc.On("GetKiosk", mock.Anything).Return(kioskState("off"), nil)

	runner, _ := startKioskMenu(t, mockSvc, nil, "Turn on: Family")
	down(runner, 1)
	runner.SimulateEnter()
	require.True(t, runner.WaitForText("Lock this device for family use?", uiSettleTimeout))
	runner.SimulateArrowRight()
	runner.SimulateEnter()
	require.True(t, runner.WaitForText("Turn on: Family", uiSettleTimeout))
	mockSvc.AssertNotCalled(t, "EnableKiosk", mock.Anything, mock.Anything, mock.Anything)
}

func contentResponse(mode string, entries ...models.ContentRuleEntry) *models.ProfilesContentResponse {
	if entries == nil {
		entries = []models.ContentRuleEntry{}
	}
	return &models.ProfilesContentResponse{Mode: mode, EffectiveMode: database.ContentModeAllowed, Entries: entries}
}

func TestBuildKioskContentMenu_ListsProfilesALockedDeviceCanBeLeftOn_Integration(t *testing.T) {
	t.Parallel()
	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()
	pages := newKioskPages()
	mockSvc := NewMockSettingsService()
	mockSvc.SetupGetProfiles(testProfilesResponse())

	runner.Start(pages)
	runner.Draw()
	runner.QueueUpdateDraw(func() {
		buildKioskContentMenu(mockSvc, pages, runner.App(), func() {})
	})
	require.True(t, runner.WaitForText("Shared profile", uiSettleTimeout))
	assert.True(t, runner.ContainsText("Kid B"))
	assert.False(t, runner.ContainsText("Kid A"), "a locked device is never left on an administrator profile")
}

func TestBuildKioskContentMenu_LoadFailureGoesBack_Integration(t *testing.T) {
	t.Parallel()
	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()
	pages := newKioskPages()
	mockSvc := NewMockSettingsService()
	mockSvc.On("GetProfiles", mock.Anything).Return((*models.ProfilesResponse)(nil), errors.New("boom"))
	back := make(chan struct{}, 1)

	runner.Start(pages)
	runner.Draw()
	runner.QueueUpdateDraw(func() {
		buildKioskContentMenu(mockSvc, pages, runner.App(), func() { back <- struct{}{} })
	})
	require.True(t, runner.WaitForText("Failed to load profiles", uiSettleTimeout))
	runner.SimulateEnter()
	waitCalled(t, back, "going back")
}

func TestBuildKioskProfileContent_ChangingModeSavesIt_Integration(t *testing.T) {
	t.Parallel()
	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()
	pages := newKioskPages()
	mockSvc := NewMockSettingsService()
	deny := models.ContentRuleEntry{Effect: "deny", Kind: "system", SystemID: "Genesis"}
	mockSvc.On("GetProfileContent", mock.Anything, "p2").Return(contentResponse("", deny), nil)
	saved := make(chan *models.ProfilesContentUpdateParams, 1)
	mockSvc.On("UpdateProfileContent", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
		params, ok := args.Get(1).(*models.ProfilesContentUpdateParams)
		if ok {
			saved <- params
		}
	}).Return(nil)

	runner.Start(pages)
	runner.Draw()
	runner.QueueUpdateDraw(func() {
		buildKioskProfileContent(mockSvc, pages, runner.App(), "p2", "Kid B", func() {})
	})
	require.True(t, runner.WaitForText("Device default", uiSettleTimeout))
	assert.True(t, runner.ContainsText("Allowed systems"))
	assert.True(t, runner.ContainsText("Other entries"), "entries this page cannot edit are still counted")

	runner.SimulateEnter()
	select {
	case params := <-saved:
		assert.Equal(t, "p2", params.ProfileID)
		assert.Equal(t, database.ContentModeAll, params.Mode)
		assert.Equal(t, []models.ContentRuleEntry{deny}, params.Entries, "changing the mode keeps every entry")
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the mode to be saved")
	}
	require.True(t, runner.WaitForText("Everything except denied", uiSettleTimeout))
}

func TestBuildKioskProfileContent_LoadFailureGoesBack_Integration(t *testing.T) {
	t.Parallel()
	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()
	pages := newKioskPages()
	mockSvc := NewMockSettingsService()
	mockSvc.On("GetProfileContent", mock.Anything, "").
		Return((*models.ProfilesContentResponse)(nil), errors.New("boom"))

	runner.Start(pages)
	runner.Draw()
	runner.QueueUpdateDraw(func() {
		buildKioskProfileContent(mockSvc, pages, runner.App(), "", "Shared profile", func() {})
	})
	require.True(t, runner.WaitForText("Failed to load allowed content", uiSettleTimeout))
}

func TestBuildKioskSystemsPage_SavesSelectionAndKeepsOtherEntries_Integration(t *testing.T) {
	t.Parallel()
	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()
	pages := newKioskPages()
	mockSvc := NewMockSettingsService()
	mockSvc.SetupGetSystems([]models.System{{ID: "NES", Name: "Nintendo"}, {ID: "SNES", Name: "Super Nintendo"}})
	deck := models.ContentRuleEntry{Effect: "allow", Kind: "deck", Value: "kids"}
	content := contentResponse(database.ContentModeAllowed,
		models.ContentRuleEntry{Effect: "allow", Kind: "system", SystemID: "NES"}, deck)
	saved := make(chan *models.ProfilesContentUpdateParams, 1)
	mockSvc.On("UpdateProfileContent", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
		params, ok := args.Get(1).(*models.ProfilesContentUpdateParams)
		if ok {
			saved <- params
		}
	}).Return(nil)
	back := make(chan struct{}, 1)

	runner.Start(pages)
	runner.Draw()
	runner.QueueUpdateDraw(func() {
		buildKioskSystemsPage(mockSvc, pages, runner.App(), "p2", "Kid B", content, func() { back <- struct{}{} })
	})
	require.True(t, runner.WaitForText("Super Nintendo", uiSettleTimeout))

	// Leave the selection as loaded and save it.
	runner.SimulateTab()
	runner.SimulateEnter()
	select {
	case params := <-saved:
		assert.Equal(t, "p2", params.ProfileID)
		assert.Equal(t, database.ContentModeAllowed, params.Mode)
		assert.Equal(t, []models.ContentRuleEntry{
			deck, {Effect: "allow", Kind: "system", SystemID: "NES"},
		}, params.Entries)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the systems to be saved")
	}
	waitCalled(t, back, "going back")
}

func TestBuildKioskSystemsPage_SaveFailureStaysOnThePage_Integration(t *testing.T) {
	t.Parallel()
	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()
	pages := newKioskPages()
	mockSvc := NewMockSettingsService()
	mockSvc.SetupGetSystems([]models.System{{ID: "NES", Name: "Nintendo"}})
	mockSvc.On("UpdateProfileContent", mock.Anything, mock.Anything).Return(errors.New("boom"))

	runner.Start(pages)
	runner.Draw()
	runner.QueueUpdateDraw(func() {
		buildKioskSystemsPage(mockSvc, pages, runner.App(), "", "Shared profile", contentResponse(""), func() {})
	})
	require.True(t, runner.WaitForText("Nintendo", uiSettleTimeout))
	runner.SimulateTab()
	runner.SimulateEnter()
	require.True(t, runner.WaitForText("Failed to save allowed systems", uiSettleTimeout))
}

func TestBuildKioskSystemsPage_LoadFailureGoesBack_Integration(t *testing.T) {
	t.Parallel()
	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()
	pages := newKioskPages()
	mockSvc := NewMockSettingsService()
	mockSvc.SetupGetSystemsError(errors.New("boom"))

	runner.Start(pages)
	runner.Draw()
	runner.QueueUpdateDraw(func() {
		buildKioskSystemsPage(mockSvc, pages, runner.App(), "", "Shared profile", contentResponse(""), func() {})
	})
	require.True(t, runner.WaitForText("Failed to load systems list", uiSettleTimeout))
}

func TestPromptKioskUnlock_SeveralAdminsAreOffered_Integration(t *testing.T) {
	t.Parallel()
	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()
	pages := newKioskPages()
	mockSvc := NewMockSettingsService()
	mockSvc.SetupGetProfiles(&models.ProfilesResponse{Profiles: []models.ProfileResponse{
		{ProfileID: "a1", Name: "Mum", Role: "admin", HasPIN: true},
		{ProfileID: "a2", Name: "Dad", Role: "admin", HasPIN: true},
		{ProfileID: "k1", Name: "Kid", Role: "member"},
	}})
	mockSvc.On("UnlockKiosk", mock.Anything, "a2", "5678").Return(kioskState("unlocked"), nil)
	unlocked := make(chan struct{}, 1)

	runner.Start(pages)
	runner.Draw()
	runner.QueueUpdateDraw(func() {
		promptKioskUnlock(mockSvc, pages, runner.App(), func() { unlocked <- struct{}{} }, func() {})
	})
	require.True(t, runner.WaitForText("Unlock as", uiSettleTimeout))
	assert.True(t, runner.ContainsText("Mum"))
	assert.False(t, runner.ContainsText("Kid"), "a member profile cannot unlock")

	runner.SimulateArrowDown()
	runner.SimulateEnter()
	require.True(t, runner.WaitForText("Profile PIN", uiSettleTimeout))
	runner.SimulateString("5678")
	runner.SimulateEnter()
	waitCalled(t, unlocked, "unlock")
	mockSvc.AssertExpectations(t)
}

func TestPromptKioskUnlock_NoAdminProfile_Integration(t *testing.T) {
	t.Parallel()
	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()
	pages := newKioskPages()
	mockSvc := NewMockSettingsService()
	mockSvc.SetupGetProfiles(&models.ProfilesResponse{Profiles: []models.ProfileResponse{
		{ProfileID: "k1", Name: "Kid", Role: "member"},
	}})
	cancelled := make(chan struct{}, 1)

	runner.Start(pages)
	runner.Draw()
	runner.QueueUpdateDraw(func() {
		promptKioskUnlock(mockSvc, pages, runner.App(), func() {}, func() { cancelled <- struct{}{} })
	})
	require.True(t, runner.WaitForText("No administrator profile exists", uiSettleTimeout))
	runner.SimulateEnter()
	waitCalled(t, cancelled, "the cancel callback")
}

func TestPromptKioskUnlock_ProfilesFailToLoad_Integration(t *testing.T) {
	t.Parallel()
	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()
	pages := newKioskPages()
	mockSvc := NewMockSettingsService()
	mockSvc.On("GetProfiles", mock.Anything).Return((*models.ProfilesResponse)(nil), errors.New("boom"))

	runner.Start(pages)
	runner.Draw()
	runner.QueueUpdateDraw(func() {
		promptKioskUnlock(mockSvc, pages, runner.App(), func() {}, func() {})
	})
	require.True(t, runner.WaitForText("Failed to load profiles", uiSettleTimeout))
}

// kioskAPI answers one method with a canned response and records the params
// it was sent.
func kioskAPI(t *testing.T, method, response string) (svc *DefaultSettingsService, sent *string) {
	t.Helper()
	apiClient := &mocks.MockAPIClient{}
	sent = new(string)
	apiClient.On("Call", mock.Anything, method, mock.Anything).Run(func(args mock.Arguments) {
		*sent = args.String(2)
	}).Return(response, nil)
	return NewSettingsService(apiClient), sent
}

const lockedKioskJSON = `{"state":"locked","seal":{"sealed":false,"unsealed":["hostMenus"]}}`

func TestSettingsServiceKioskCalls(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("status", func(t *testing.T) {
		t.Parallel()
		svc, sent := kioskAPI(t, models.MethodKiosk, lockedKioskJSON)
		state, err := svc.GetKiosk(ctx)
		require.NoError(t, err)
		assert.Equal(t, "locked", state.State)
		assert.Equal(t, []string{"hostMenus"}, state.Seal.Unsealed)
		assert.Empty(t, *sent)
	})

	t.Run("enable", func(t *testing.T) {
		t.Parallel()
		svc, sent := kioskAPI(t, models.MethodKioskEnable, lockedKioskJSON)
		_, err := svc.EnableKiosk(ctx, kiosk.PresetPublic, true)
		require.NoError(t, err)
		var params models.KioskEnableParams
		require.NoError(t, json.Unmarshal([]byte(*sent), &params))
		assert.Equal(t, kiosk.PresetPublic, params.Preset)
		assert.True(t, params.ConfirmEmpty)
	})

	t.Run("unlock sends the profile and PIN", func(t *testing.T) {
		t.Parallel()
		svc, sent := kioskAPI(t, models.MethodKioskUnlock, lockedKioskJSON)
		_, err := svc.UnlockKiosk(ctx, "admin-1", "4321")
		require.NoError(t, err)
		var params models.KioskUnlockParams
		require.NoError(t, json.Unmarshal([]byte(*sent), &params))
		require.NotNil(t, params.ProfileID)
		assert.Equal(t, "admin-1", *params.ProfileID)
		require.NotNil(t, params.PIN)
		assert.Equal(t, "4321", *params.PIN)
		assert.Nil(t, params.RecoveryToken)
	})

	t.Run("lock and disable", func(t *testing.T) {
		t.Parallel()
		svc, _ := kioskAPI(t, models.MethodKioskLock, lockedKioskJSON)
		state, err := svc.LockKiosk(ctx)
		require.NoError(t, err)
		assert.Equal(t, "locked", state.State)

		svc, _ = kioskAPI(t, models.MethodKioskDisable, `{"state":"off","seal":{"sealed":false,"unsealed":[]}}`)
		state, err = svc.DisableKiosk(ctx)
		require.NoError(t, err)
		assert.Equal(t, "off", state.State)
	})

	t.Run("content rule round trip", func(t *testing.T) {
		t.Parallel()
		svc, sent := kioskAPI(t, models.MethodProfilesContent,
			`{"profileId":"p2","mode":"allowed","effectiveMode":"allowed",`+
				`"entries":[{"effect":"allow","kind":"system","systemId":"NES"}]}`)
		content, err := svc.GetProfileContent(ctx, "p2")
		require.NoError(t, err)
		assert.Equal(t, database.ContentModeAllowed, content.Mode)
		require.Len(t, content.Entries, 1)
		assert.JSONEq(t, `{"profileId":"p2"}`, *sent)

		svc, sent = kioskAPI(t, models.MethodProfilesContentUpdate, `{}`)
		require.NoError(t, svc.UpdateProfileContent(ctx, &models.ProfilesContentUpdateParams{
			ProfileID: "p2", Mode: database.ContentModeAll,
		}))
		var params models.ProfilesContentUpdateParams
		require.NoError(t, json.Unmarshal([]byte(*sent), &params))
		assert.Equal(t, database.ContentModeAll, params.Mode)
	})
}

func TestSettingsServiceKioskCallErrors(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	failing := &mocks.MockAPIClient{}
	failing.On("Call", mock.Anything, mock.Anything, mock.Anything).Return("", errors.New("the device is locked"))
	svc := NewSettingsService(failing)
	_, err := svc.GetKiosk(ctx)
	require.ErrorContains(t, err, "the device is locked")
	_, err = svc.GetProfileContent(ctx, "")
	require.ErrorContains(t, err, "the device is locked")
	err = svc.UpdateProfileContent(ctx, &models.ProfilesContentUpdateParams{})
	require.ErrorContains(t, err, "the device is locked")

	garbled := &mocks.MockAPIClient{}
	garbled.On("Call", mock.Anything, mock.Anything, mock.Anything).Return("not json", nil)
	svc = NewSettingsService(garbled)
	_, err = svc.GetKiosk(ctx)
	require.ErrorContains(t, err, "failed to parse")
	_, err = svc.GetProfileContent(ctx, "")
	require.ErrorContains(t, err, "failed to parse")
}
