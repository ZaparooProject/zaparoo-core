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

func TestShouldPromptEncryption(t *testing.T) {
	t.Parallel()

	assert.False(t, shouldPromptEncryption(nil))
	assert.True(t, shouldPromptEncryption(&models.ClientsResponse{}))
	assert.False(t, shouldPromptEncryption(&models.ClientsResponse{
		Clients: []models.PairedClient{{ClientID: "a", ClientName: "Phone", Role: "admin"}},
	}))
}

func TestShowEncryptionPrompt_Buttons_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()

	pages := tview.NewPages()
	pages.AddPage("main", tview.NewTextView().SetText("Main Page"), true, true)
	runner.Start(pages)
	runner.Draw()

	secureCalled := make(chan struct{}, 1)
	dontAskCalled := make(chan struct{}, 1)

	runner.QueueUpdateDraw(func() {
		ShowEncryptionPrompt(pages, runner.App(),
			func() { close(secureCalled) },
			nil,
			func() { close(dontAskCalled) },
		)
	})

	require.True(t, runner.WaitForText("Secure Zaparoo?", 100*time.Millisecond))
	assert.True(t, runner.ContainsText("Secure Now"))
	assert.True(t, runner.ContainsText("Not Now"))
	assert.True(t, runner.ContainsText("Don't Ask Again"))

	runner.Screen().InjectEnter()
	runner.Draw()

	assert.True(t, runner.WaitForSignal(secureCalled, 100*time.Millisecond),
		"Secure Now callback should be called")
	select {
	case <-dontAskCalled:
		t.Fatal("Don't Ask Again callback should not be called")
	default:
	}
}

func TestMaybeShowEncryptionPrompt_SkipsWithPairedClients(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()

	pages := tview.NewPages()
	pages.AddPage("main", tview.NewTextView().SetText("Main Page"), true, true)
	runner.Start(pages)
	runner.Draw()

	svc := NewMockSettingsService()
	svc.On("GetClients", mock.Anything).Return(&models.ClientsResponse{
		Clients: []models.PairedClient{{ClientID: "a", ClientName: "Phone", Role: "admin"}},
	}, nil)

	runner.QueueUpdateDraw(func() {
		maybeShowEncryptionPrompt(svc, pages, runner.App(), func() {
			t.Error("markPrompted should not be called")
		})
	})
	runner.Draw()

	assert.False(t, runner.ContainsText("Secure Zaparoo?"))
	svc.AssertExpectations(t)
}

func TestMaybeShowEncryptionPrompt_ShowsWithNoClients(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()

	pages := tview.NewPages()
	pages.AddPage("main", tview.NewTextView().SetText("Main Page"), true, true)
	runner.Start(pages)
	runner.Draw()

	svc := NewMockSettingsService()
	svc.On("GetClients", mock.Anything).Return(&models.ClientsResponse{}, nil)

	runner.QueueUpdateDraw(func() {
		maybeShowEncryptionPrompt(svc, pages, runner.App(), nil)
	})

	assert.True(t, runner.WaitForText("Secure Zaparoo?", 100*time.Millisecond))
	svc.AssertExpectations(t)
}

func TestEncryptionPairingModal_SuccessKeepsEncryption(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()

	pages := tview.NewPages()
	pages.AddPage("main", tview.NewTextView().SetText("Main Page"), true, true)
	runner.Start(pages)
	runner.Draw()

	svc := NewMockSettingsService()
	svc.On("GetClients", mock.Anything).Return(&models.ClientsResponse{
		Clients: []models.PairedClient{{ClientID: "a", ClientName: "Phone", Role: "admin"}},
	}, nil)
	svc.On("CancelClientPairing", mock.Anything).Return(nil)

	pairing := &models.ClientsPairStartResponse{
		PIN:       "123456",
		ExpiresAt: time.Now().Add(time.Minute).Unix(),
	}
	markPrompted := make(chan struct{}, 1)
	runner.QueueUpdateDraw(func() {
		showEncryptionPairingModal(svc, pages, runner.App(), pairing,
			10*time.Millisecond, func() { close(markPrompted) })
	})

	require.True(t, runner.WaitForText("Zaparoo secured.", time.Second))
	assert.True(t, runner.WaitForSignal(markPrompted, 100*time.Millisecond),
		"markPrompted should be called on success")
	// Encryption must not be reverted on success.
	assert.Equal(t, 0, svc.UpdateSettingsCallCount())
	svc.AssertExpectations(t)
}

func TestEncryptionPairingModal_ExpiryFailsOpen(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()

	pages := tview.NewPages()
	pages.AddPage("main", tview.NewTextView().SetText("Main Page"), true, true)
	runner.Start(pages)
	runner.Draw()

	svc := NewMockSettingsService()
	disabled := false
	svc.On("UpdateSettings", mock.Anything, &models.UpdateSettingsParams{
		Encryption: &disabled,
	}).Return(nil)

	pairing := &models.ClientsPairStartResponse{
		PIN:       "123456",
		ExpiresAt: time.Now().Add(-time.Second).Unix(),
	}
	runner.QueueUpdateDraw(func() {
		showEncryptionPairingModal(svc, pages, runner.App(), pairing,
			10*time.Millisecond, func() { t.Error("markPrompted should not be called on expiry") })
	})

	require.True(t, runner.WaitForText("No changes were made.", time.Second))
	assert.True(t, runner.ContainsText("PIN expired"))
	svc.AssertExpectations(t)
}

func TestEncryptionPairingModal_CancelFailsOpen(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()

	pages := tview.NewPages()
	pages.AddPage("main", tview.NewTextView().SetText("Main Page"), true, true)
	runner.Start(pages)
	runner.Draw()

	svc := NewMockSettingsService()
	svc.On("GetClients", mock.Anything).Return(&models.ClientsResponse{}, nil).Maybe()
	svc.On("CancelClientPairing", mock.Anything).Return(nil)
	disabled := false
	svc.On("UpdateSettings", mock.Anything, &models.UpdateSettingsParams{
		Encryption: &disabled,
	}).Return(nil)

	pairing := &models.ClientsPairStartResponse{
		PIN:       "123456",
		ExpiresAt: time.Now().Add(time.Minute).Unix(),
	}
	runner.QueueUpdateDraw(func() {
		showEncryptionPairingModal(svc, pages, runner.App(), pairing,
			time.Minute, func() { t.Error("markPrompted should not be called on cancel") })
	})
	require.True(t, runner.WaitForText("Pairing PIN: 123 456", time.Second))

	runner.Screen().InjectEnter()
	runner.Draw()

	require.True(t, runner.WaitForText("No changes were made.", time.Second))
	assert.True(t, runner.ContainsText("Pairing cancelled."))
	svc.AssertExpectations(t)
}

func TestStartEncryptionSetup_PairStartFailureFailsOpen(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()

	pages := tview.NewPages()
	pages.AddPage("main", tview.NewTextView().SetText("Main Page"), true, true)
	runner.Start(pages)
	runner.Draw()

	svc := NewMockSettingsService()
	enabled := true
	disabled := false
	svc.On("UpdateSettings", mock.Anything, &models.UpdateSettingsParams{
		Encryption: &enabled,
	}).Return(nil)
	svc.On("StartClientPairing", mock.Anything, "admin").
		Return(nil, assert.AnError)
	svc.On("UpdateSettings", mock.Anything, &models.UpdateSettingsParams{
		Encryption: &disabled,
	}).Return(nil)

	runner.QueueUpdateDraw(func() {
		startEncryptionSetup(svc, pages, runner.App(), func() {
			t.Error("markPrompted should not be called on failure")
		})
	})

	require.True(t, runner.WaitForText("No changes were made.", time.Second))
	svc.AssertExpectations(t)
}
