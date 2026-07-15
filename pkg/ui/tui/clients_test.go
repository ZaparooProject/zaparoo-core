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

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestPairingDisplayFormatting(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "123 456", formatPairingPIN("123456"))
	assert.Equal(t, "Admin", formatPairingRole("admin"))
	now := time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC)
	assert.Equal(t, "1:05", formatPairingCountdown(now.Add(65*time.Second), now))
	assert.Equal(t, "0:00", formatPairingCountdown(now, now))
}

func TestBuildClientsPage_FirstClientAdmin_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()
	pages := tview.NewPages()
	pages.AddPage(PageSettingsMain, tview.NewTextView().SetText("Settings"), true, false)

	mockSvc := NewMockSettingsService()
	mockSvc.On("GetClients", mock.Anything).Return(&models.ClientsResponse{}, nil)
	mockSvc.SetupGetSettings(&models.SettingsResponse{})
	mockSvc.SetupGetProfiles(&models.ProfilesResponse{})
	mockSvc.On("StartClientPairing", mock.Anything, "admin").
		Return(&models.ClientsPairStartResponse{PIN: "123456", ExpiresAt: time.Now().Add(2 * time.Second).Unix()}, nil)

	runner.Start(pages)
	runner.Draw()
	runner.QueueUpdateDraw(func() {
		BuildClientsPage(mockSvc, pages, runner.App())
	})

	require.True(t, runner.WaitForText("Clients", 100*time.Millisecond))
	assert.True(t, runner.ContainsText("no paired clients"))
	assert.True(t, runner.ContainsText("Pair"))

	// Move from list to button bar, select Pair, then confirm first-client
	// administrator assignment.
	runner.SimulateTab()
	runner.SimulateEnter()
	require.True(t, runner.WaitForText("first paired client", 100*time.Millisecond))
	runner.SimulateEnter()
	require.True(t, runner.WaitForText("Pairing PIN: 123 456", 100*time.Millisecond))
	assert.True(t, runner.ContainsText("Role: Admin"))
	assert.True(t, runner.ContainsText("Expires in:"))
	require.True(t, runner.WaitForText("Expires in: 0:00", 3*time.Second))
	runner.SimulateEnter()
	mockSvc.AssertExpectations(t)
}

func TestBuildClientsPage_RequireEncryptionConfirmation_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()
	pages := tview.NewPages()
	pages.AddPage(PageSettingsMain, tview.NewTextView().SetText("Settings"), true, false)

	mockSvc := NewMockSettingsService()
	mockSvc.On("GetClients", mock.Anything).Return(&models.ClientsResponse{}, nil)
	mockSvc.SetupGetSettings(&models.SettingsResponse{})
	mockSvc.SetupGetProfiles(&models.ProfilesResponse{})
	mockSvc.On("UpdateSettings", mock.Anything, mock.MatchedBy(func(params *models.UpdateSettingsParams) bool {
		return params.Encryption != nil && *params.Encryption
	})).Return(nil)

	runner.Start(pages)
	runner.Draw()
	runner.QueueUpdateDraw(func() {
		BuildClientsPage(mockSvc, pages, runner.App())
	})

	require.True(t, runner.WaitForText("Require encryption", 100*time.Millisecond))
	runner.SimulateArrowRight()
	require.True(t, runner.WaitForText("Require encrypted remote", 100*time.Millisecond))
	runner.SimulateEnter()
	mockSvc.AssertExpectations(t)
}

func TestBuildClientsPage_DisableEncryptionImmediately_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()
	pages := tview.NewPages()
	pages.AddPage(PageSettingsMain, tview.NewTextView().SetText("Settings"), true, false)

	mockSvc := NewMockSettingsService()
	mockSvc.On("GetClients", mock.Anything).Return(&models.ClientsResponse{}, nil)
	mockSvc.SetupGetSettings(&models.SettingsResponse{Encryption: true})
	mockSvc.SetupGetProfiles(&models.ProfilesResponse{})
	mockSvc.On("UpdateSettings", mock.Anything, mock.MatchedBy(func(params *models.UpdateSettingsParams) bool {
		return params.Encryption != nil && !*params.Encryption
	})).Return(nil)

	runner.Start(pages)
	runner.Draw()
	runner.QueueUpdateDraw(func() {
		BuildClientsPage(mockSvc, pages, runner.App())
	})

	require.True(t, runner.WaitForText("Require encryption", 100*time.Millisecond))
	runner.SimulateArrowLeft()
	mockSvc.AssertExpectations(t)
}

func TestShowClientRolePicker_AdminWithoutProfilesStartsPairing_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()
	pages := tview.NewPages()
	pages.AddPage(PageClients, tview.NewTextView().SetText("Clients"), true, false)

	mockSvc := NewMockSettingsService()
	mockSvc.On("StartClientPairing", mock.Anything, "admin").
		Return(&models.ClientsPairStartResponse{PIN: "654321", ExpiresAt: time.Now().Add(time.Minute).Unix()}, nil)

	runner.Start(pages)
	runner.Draw()
	runner.QueueUpdateDraw(func() {
		showClientRolePicker(mockSvc, pages, runner.App(), nil, func() {})
	})

	require.True(t, runner.WaitForText("Client Role", 100*time.Millisecond))
	runner.SimulateArrowDown()
	runner.SimulateEnter()
	require.True(t, runner.WaitForText("Pairing PIN: 654 321", 100*time.Millisecond))
	assert.False(t, runner.ContainsText("Profile PIN"))
	runner.SimulateEnter()
	mockSvc.AssertExpectations(t)
}

func TestShowClientRolePicker_AdminPromptsCredential_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()
	pages := tview.NewPages()
	pages.AddPage(PageClients, tview.NewTextView().SetText("Clients"), true, false)

	mockSvc := NewMockSettingsService()
	profiles := testProfilesResponse().Profiles
	mockSvc.On("VerifyProfileManagement", mock.Anything, "p1", "1234").Return(nil)
	mockSvc.On("StartClientPairing", mock.Anything, "admin").
		Return(&models.ClientsPairStartResponse{PIN: "654321", ExpiresAt: time.Now().Add(time.Minute).Unix()}, nil)

	runner.Start(pages)
	runner.Draw()
	runner.QueueUpdateDraw(func() {
		showClientRolePicker(mockSvc, pages, runner.App(), profiles, func() {})
	})

	require.True(t, runner.WaitForText("Client Role", 100*time.Millisecond))
	runner.SimulateArrowDown() // Admin
	runner.SimulateEnter()
	require.True(t, runner.WaitForText("Profile PIN", 100*time.Millisecond))
	runner.SimulateString("1234")
	runner.SimulateEnter()
	require.True(t, runner.WaitForText("Pairing PIN: 654 321", 100*time.Millisecond))
	runner.SimulateEnter()
	mockSvc.AssertExpectations(t)
}
