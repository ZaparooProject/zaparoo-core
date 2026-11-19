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
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/kiosk"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func kioskState(state string) *models.KioskResponse {
	resp := &models.KioskResponse{State: state, Seal: models.KioskSeal{Unsealed: []string{}}}
	if state != "off" {
		resp.Settings = &models.KioskSettings{Preset: kiosk.PresetFamily, DeviceUnlock: true}
		resp.Seal.Unsealed = []string{platforms.KioskUnsealedHostMenus}
	}
	return resp
}

func TestKioskStateHelpers(t *testing.T) {
	t.Parallel()
	assert.False(t, kioskLocked(nil))
	assert.False(t, kioskActive(nil))
	assert.False(t, kioskLocked(kioskState("off")))
	assert.False(t, kioskActive(kioskState("off")))
	assert.True(t, kioskLocked(kioskState("locked")))
	assert.True(t, kioskActive(kioskState("locked")))
	assert.False(t, kioskLocked(kioskState("unlocked")), "a temporary unlock is not a lock")
	assert.True(t, kioskActive(kioskState("unlocked")))
}

func TestKioskStateText(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "Off", kioskStateText(kioskState("off")))
	assert.Equal(t, "Locked", kioskStateText(kioskState("locked")))

	unlocked := kioskState("unlocked")
	assert.Equal(t, "Unlocked", kioskStateText(unlocked))
	until := time.Date(2026, 11, 17, 9, 30, 0, 0, time.Now().Location()).Format(time.RFC3339)
	unlocked.UnlockUntil = &until
	assert.Equal(t, "Unlocked until 09:30", kioskStateText(unlocked))
}

func TestKioskSealTextSaysWhatIsNotLocked(t *testing.T) {
	t.Parallel()
	assert.Contains(t, kioskSealText(&models.KioskSeal{Sealed: true}), "Sealed")

	text := kioskSealText(&models.KioskSeal{Unsealed: []string{
		platforms.KioskUnsealedHostMenus, platforms.KioskUnsealedIdleReset, "somethingNew",
	}})
	assert.Contains(t, text, "Zaparoo is locked, but")
	assert.Contains(t, text, "this device's own menus are not locked")
	assert.Contains(t, text, "idle reset is not supported here")
	assert.Contains(t, text, "somethingNew", "a reason this build does not know is still shown")
}

func TestKioskModeCycleRoundTrips(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"", database.ContentModeAll, database.ContentModeAllowed} {
		assert.Equal(t, mode, kioskModeValue(kioskModeIndex(mode)))
	}
	assert.Len(t, kioskModeOptions, 3)
}

func TestWithAllowedSystemsKeepsEntriesItDoesNotEdit(t *testing.T) {
	t.Parallel()
	entries := []models.ContentRuleEntry{
		{Effect: "allow", Kind: "system", SystemID: "NES"},
		{Effect: "deny", Kind: "system", SystemID: "Genesis"},
		{Effect: "allow", Kind: "deck", Value: "kids"},
		{Effect: "deny", Kind: "media", SystemID: "NES", Path: "/a.nes"},
		{Effect: "allow", Kind: "system", SystemID: "SNES"},
	}
	assert.Equal(t, []string{"NES", "SNES"}, allowedSystemIDs(entries))

	updated := withAllowedSystems(entries, []string{"GBA"})
	assert.Equal(t, []models.ContentRuleEntry{
		{Effect: "deny", Kind: "system", SystemID: "Genesis"},
		{Effect: "allow", Kind: "deck", Value: "kids"},
		{Effect: "deny", Kind: "media", SystemID: "NES", Path: "/a.nes"},
		{Effect: "allow", Kind: "system", SystemID: "GBA"},
	}, updated)
	assert.Empty(t, allowedSystemIDs(withAllowedSystems(entries, nil)))
}

func TestKioskEnableErrorNamesTheMissingPiece(t *testing.T) {
	t.Parallel()
	assert.Contains(t, kioskEnableError(kiosk.ErrNoAdminProfile), "administrator profile")
	assert.Contains(t, kioskEnableError(kiosk.ErrNoAdminClient), "Pair an app")
	assert.Equal(t, "Failed to turn kiosk mode on", kioskEnableError(errors.New("boom")))
}

func newKioskPages() *tview.Pages {
	pages := tview.NewPages()
	pages.AddPage(PageMain, tview.NewTextView().SetText("Main"), true, false)
	pages.AddPage(PageSettingsMain, tview.NewTextView().SetText("Settings"), true, false)
	return pages
}

func TestBuildKioskSettingsMenu_OffOffersThePresets_Integration(t *testing.T) {
	t.Parallel()
	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()
	pages := newKioskPages()

	mockSvc := NewMockSettingsService()
	mockSvc.On("GetKiosk", mock.Anything).Return(kioskState("off"), nil)

	runner.Start(pages)
	runner.Draw()
	runner.QueueUpdateDraw(func() {
		buildKioskSettingsMenu(mockSvc, pages, runner.App(), nil)
	})

	require.True(t, runner.WaitForText("Turn on: Family", uiSettleTimeout))
	assert.True(t, runner.ContainsText("Turn on: Public"))
	assert.True(t, runner.ContainsText("Allowed content"))
	assert.False(t, runner.ContainsText("Turn off"))
	assert.False(t, runner.ContainsText("Lock now"))
}

func TestBuildKioskSettingsMenu_UnlockedOffersLockAndOff_Integration(t *testing.T) {
	t.Parallel()
	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()
	pages := newKioskPages()

	mockSvc := NewMockSettingsService()
	mockSvc.On("GetKiosk", mock.Anything).Return(kioskState("unlocked"), nil)

	runner.Start(pages)
	runner.Draw()
	runner.QueueUpdateDraw(func() {
		buildKioskSettingsMenu(mockSvc, pages, runner.App(), nil)
	})

	require.True(t, runner.WaitForText("Lock now", uiSettleTimeout))
	assert.True(t, runner.ContainsText("Turn off"))
	assert.True(t, runner.ContainsText("Not sealed"), "a lock that leaves the host open says so")
	assert.False(t, runner.ContainsText("Turn on: Family"))
}

func TestBuildKioskSettingsMenu_EnableFamilyConfirmsThenLocks_Integration(t *testing.T) {
	t.Parallel()
	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()
	pages := newKioskPages()

	mockSvc := NewMockSettingsService()
	mockSvc.On("GetKiosk", mock.Anything).Return(kioskState("off"), nil)
	enabled := make(chan struct{}, 1)
	mockSvc.On("EnableKiosk", mock.Anything, kiosk.PresetFamily, false).Run(func(mock.Arguments) {
		enabled <- struct{}{}
	}).Return(kioskState("locked"), nil)

	rebuilt := make(chan struct{}, 1)
	runner.Start(pages)
	runner.Draw()
	runner.QueueUpdateDraw(func() {
		buildKioskSettingsMenu(mockSvc, pages, runner.App(), func() { rebuilt <- struct{}{} })
	})

	require.True(t, runner.WaitForText("Turn on: Family", uiSettleTimeout))
	runner.SimulateArrowDown()
	runner.SimulateEnter()
	require.True(t, runner.WaitForText("Lock this device for family use?", uiSettleTimeout))
	assert.True(t, runner.ContainsText("not paired will be disconnected"),
		"the warning says what locking does to other clients")
	runner.SimulateEnter()

	select {
	case <-enabled:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for kiosk enable")
	}
	select {
	case <-rebuilt:
	case <-time.After(time.Second):
		t.Fatal("the main page was not rebuilt after locking")
	}
	mockSvc.AssertExpectations(t)
}

func TestPromptKioskUnlock_SendsThePINToCore_Integration(t *testing.T) {
	t.Parallel()
	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()
	pages := newKioskPages()

	mockSvc := NewMockSettingsService()
	mockSvc.SetupGetProfiles(testProfilesResponse())
	mockSvc.On("UnlockKiosk", mock.Anything, "p1", "1234").Return(kioskState("unlocked"), nil)

	unlocked := make(chan struct{}, 1)
	runner.Start(pages)
	runner.Draw()
	runner.QueueUpdateDraw(func() {
		promptKioskUnlock(mockSvc, pages, runner.App(), func() { unlocked <- struct{}{} }, func() {})
	})

	require.True(t, runner.WaitForText("Profile PIN", uiSettleTimeout))
	runner.SimulateString("1234")
	runner.SimulateEnter()
	select {
	case <-unlocked:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for unlock")
	}
	mockSvc.AssertExpectations(t)
}

func TestPromptKioskUnlock_RefusedPINDoesNotUnlock_Integration(t *testing.T) {
	t.Parallel()
	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()
	pages := newKioskPages()

	mockSvc := NewMockSettingsService()
	mockSvc.SetupGetProfiles(testProfilesResponse())
	mockSvc.On("UnlockKiosk", mock.Anything, "p1", "9999").
		Return((*models.KioskResponse)(nil), errors.New("incorrect PIN"))

	unlocked := false
	runner.Start(pages)
	runner.Draw()
	runner.QueueUpdateDraw(func() {
		promptKioskUnlock(mockSvc, pages, runner.App(), func() { unlocked = true }, func() {})
	})

	require.True(t, runner.WaitForText("Profile PIN", uiSettleTimeout))
	runner.SimulateString("9999")
	runner.SimulateEnter()
	require.True(t, runner.WaitForText("Administrator PIN was not accepted", uiSettleTimeout))
	assert.False(t, unlocked)
}
