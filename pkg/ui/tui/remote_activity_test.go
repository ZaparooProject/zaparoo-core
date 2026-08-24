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
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatRemoteActivityTime(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "20 Aug 12:00", formatRemoteActivityTime("2026-08-20T12:00:00Z"))
	assert.Equal(t, "not-a-time", formatRemoteActivityTime("not-a-time"))
}

func TestFormatRemoteActivityOrigin(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "first_party", formatRemoteActivityOrigin(&models.RemoteActivityEntry{OriginKind: "first_party"}))
	assert.Equal(t, "api_key (companion-app)", formatRemoteActivityOrigin(&models.RemoteActivityEntry{
		OriginKind: "api_key", OriginKeyName: "companion-app",
	}))
}

func TestFormatRemoteActivitySecondary(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "first_party, succeeded", formatRemoteActivitySecondary(&models.RemoteActivityEntry{
		OriginKind: "first_party", Status: "succeeded",
	}))
	assert.Equal(t, "api_key (companion-app), failed: bad_params", formatRemoteActivitySecondary(
		&models.RemoteActivityEntry{
			OriginKind: "api_key", OriginKeyName: "companion-app", Status: "failed", ErrorCode: "bad_params",
		}))
	// No terminal result yet: falls back to the ledger state.
	assert.Equal(t, "first_party, executing", formatRemoteActivitySecondary(&models.RemoteActivityEntry{
		OriginKind: "first_party", State: "executing",
	}))
}

func TestFormatRemoteActivityDetail(t *testing.T) {
	t.Parallel()

	assert.Equal(t,
		"Operation: launch\nFrom: first_party\nWhen: 20 Aug 12:00\nOutcome: succeeded",
		formatRemoteActivityDetail(&models.RemoteActivityEntry{
			OperationType: "launch", OriginKind: "first_party",
			CreatedAt: "2026-08-20T12:00:00Z", Status: "succeeded",
		}))
	assert.Equal(t,
		"Operation: launch\nFrom: first_party\nWhen: 20 Aug 12:00\nOutcome: failed\nError: media_not_found",
		formatRemoteActivityDetail(&models.RemoteActivityEntry{
			OperationType: "launch", OriginKind: "first_party",
			CreatedAt: "2026-08-20T12:00:00Z", Status: "failed", ErrorCode: "media_not_found",
		}))
}

func TestBuildRemoteActivityPage_ShowsEntries_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()
	pages := tview.NewPages()
	mockSvc := NewMockSettingsService()
	mockSvc.SetupGetRemoteActivity(&models.RemoteActivityResponse{
		Entries: []models.RemoteActivityEntry{
			{
				CreatedAt: "2026-08-20T12:00:00Z", OperationType: "launch",
				OriginKind: "first_party", Status: "succeeded",
			},
		},
	})

	runner.Start(pages)
	runner.QueueUpdateDraw(func() {
		buildRemoteActivityPage(mockSvc, pages, runner.App(), func() {})
	})

	require.True(t, runner.WaitForText("launch", 100*time.Millisecond))
	assert.True(t, runner.ContainsText("20 Aug 12:00"))
}

func TestBuildRemoteActivityPage_EmptyShowsPlaceholder_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()
	pages := tview.NewPages()
	mockSvc := NewMockSettingsService()
	mockSvc.SetupGetRemoteActivity(&models.RemoteActivityResponse{Entries: []models.RemoteActivityEntry{}})

	runner.Start(pages)
	runner.QueueUpdateDraw(func() {
		buildRemoteActivityPage(mockSvc, pages, runner.App(), func() {})
	})

	require.True(t, runner.WaitForText("no remote activity yet", 100*time.Millisecond))
}

func TestBuildOnlineSettingsMenu_RemoteControlActivityNavigatesToActivityPage_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()
	pages := tview.NewPages()
	mockSvc := NewMockSettingsService()
	mockSvc.SetupGetBackupStatus(backupTestStatus(true))
	mockSvc.SetupGetSettings(onlineTestSettings(config.DefaultBackupRemoteBaseURL))
	mockSvc.SetupGetRemoteActivity(&models.RemoteActivityResponse{Entries: []models.RemoteActivityEntry{}})

	runner.Start(pages)
	runner.QueueUpdateDraw(func() {
		buildOnlineSettingsMenu(mockSvc, pages, runner.App(), func() {})
	})
	require.True(t, runner.WaitForText("Remote control activity", 100*time.Millisecond))

	// Account, Warp, Unlink account, Remote control, then Remote control activity.
	runner.SimulateArrowDown()
	runner.SimulateArrowDown()
	runner.SimulateArrowDown()
	runner.SimulateArrowDown()
	runner.SimulateEnter()

	require.True(t, runner.WaitForText("no remote activity yet", 500*time.Millisecond),
		"selecting Remote control activity should open the activity page")
}
