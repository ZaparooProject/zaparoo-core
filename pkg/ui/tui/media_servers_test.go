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

func openMediaServersMenu(t *testing.T) (*TestAppRunner, *MockSettingsService) {
	t.Helper()
	runner := NewTestAppRunner(t, 100, 30)
	t.Cleanup(runner.Stop)
	mockSvc := NewMockSettingsService()
	pages := tview.NewPages()
	runner.Start(pages)
	runner.QueueUpdateDraw(func() {
		buildMediaServersMenu(mockSvc, pages, runner.App(), func() {})
	})
	require.True(t, runner.WaitForText("Link Jellyfin", uiSettleTimeout))
	return runner, mockSvc
}

func TestMediaServersMenu_JellyfinNeedsAddress_Integration(t *testing.T) {
	t.Parallel()

	runner, mockSvc := openMediaServersMenu(t)
	// Plex address, Link Plex, Jellyfin address, Link Jellyfin.
	for range 3 {
		runner.SimulateArrowDown()
	}
	runner.SimulateEnter()
	require.True(t, runner.WaitForText("Enter the Jellyfin server address first.", uiSettleTimeout))
	mockSvc.AssertNotCalled(t, "StartAuthLink", mock.Anything, mock.Anything, mock.Anything)
}

func TestMediaServersMenu_LinkPlexWithoutAddress_Integration(t *testing.T) {
	t.Parallel()

	runner, mockSvc := openMediaServersMenu(t)
	mockSvc.On("StartAuthLink", mock.Anything, models.AuthLinkProviderPlex, "").
		Return(&models.AuthLinkStatusResponse{
			Status:          models.AuthLinkStatusPending,
			UserCode:        "PX12",
			VerificationURL: "https://plex.tv/link",
			Provider:        models.AuthLinkProviderPlex,
		}, nil)
	mockSvc.On("GetAuthLinkStatus", mock.Anything).Return(&models.AuthLinkStatusResponse{
		Status: models.AuthLinkStatusApproved,
	}, nil)

	runner.SimulateArrowDown()
	runner.SimulateEnter()
	require.True(t, runner.WaitForText("PX12", uiSettleTimeout))
	assert.True(t, runner.ContainsText("plex.tv/link"))
	// The poll loop ticks every 2 seconds before observing approval.
	require.True(t, runner.WaitForText("Plex linked", 5*time.Second))
	assert.False(t, runner.ContainsText("Turn on every online feature"))
}

func TestMediaServersMenu_LinkJellyfinShowsQuickConnect_Integration(t *testing.T) {
	t.Parallel()

	runner, mockSvc := openMediaServersMenu(t)
	mockSvc.On("StartAuthLink", mock.Anything, models.AuthLinkProviderJellyfin, "http://jf.lan:8096").
		Return(&models.AuthLinkStatusResponse{
			Status:   models.AuthLinkStatusPending,
			UserCode: "123456",
			Provider: models.AuthLinkProviderJellyfin,
		}, nil)
	mockSvc.On("GetAuthLinkStatus", mock.Anything).Return(&models.AuthLinkStatusResponse{
		Status: models.AuthLinkStatusPending,
	}, nil)
	cancelled := make(chan struct{})
	mockSvc.On("CancelAuthLink", mock.Anything).Return(nil).Run(func(mock.Arguments) { close(cancelled) })

	runner.SimulateArrowDown()
	runner.SimulateArrowDown()
	runner.SimulateEnter()
	require.True(t, runner.WaitForText("Jellyfin server address", uiSettleTimeout))
	runner.SimulateString("jf.lan:8096")
	runner.SimulateEnter()
	require.True(t, runner.WaitForText("jf.lan:8096", uiSettleTimeout))

	runner.SimulateArrowDown()
	runner.SimulateEnter()
	require.True(t, runner.WaitForText("123456", uiSettleTimeout))
	assert.True(t, runner.ContainsText("Quick Connect"))
	assert.True(t, runner.ContainsText("http://jf.lan:8096"), "the sign-in address keeps its scheme")

	runner.SimulateEnter()
	require.True(t, runner.WaitForSignal(cancelled, uiSettleTimeout))
}

func TestValidateServerURL(t *testing.T) {
	t.Parallel()

	for text, want := range map[string]string{
		"":                           "",
		"  ":                         "",
		"http://jf.lan:8096":         "http://jf.lan:8096",
		"https://plex.example.com/":  "https://plex.example.com/",
		" jf.lan:8096 ":              "http://jf.lan:8096",
		"https://media.lan/jellyfin": "https://media.lan/jellyfin",
	} {
		got, err := normalizeServerURL(text)
		require.NoError(t, err, text)
		assert.Equal(t, want, got, text)
		require.NoError(t, validateServerURL(text), text)
	}
	for _, bad := range []string{"ftp://jf.lan", "http://", "not a url"} {
		require.ErrorIs(t, validateServerURL(bad), errServerURL, bad)
	}
	for _, bad := range []string{
		"http://jf.lan:8096/web/#/home", "http://jf.lan:8096/?api_key=x", "http://jf.lan:8096/?", "http://u:p@jf.lan",
	} {
		require.ErrorIs(t, validateServerURL(bad), errServerURLParts, bad)
	}
}
