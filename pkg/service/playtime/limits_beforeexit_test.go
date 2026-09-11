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

package playtime

import (
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// newExceededLimitsManager builds a manager whose session limit is already
// blown, so checkLimits takes the stop-the-game branch.
func newExceededLimitsManager(t *testing.T, events *[]string) (*LimitsManager, *mocks.MockPlatform) {
	t.Helper()

	mockDB := testhelpers.NewMockUserDBI()
	mockDB.On("SumMediaPlayTimeForDay", mock.Anything).Return(int64(0), nil).Maybe()

	mockPlatform := mocks.NewMockPlatform()
	// Only what the limit-reached path touches: the warning sound resolves the
	// data dir from the platform, then the launcher is stopped.
	mockPlatform.On("ID").Return("mock-platform").Maybe()
	mockPlatform.On("Settings").Return(platforms.Settings{}).Maybe()
	mockPlatform.On("StopActiveLauncher", platforms.StopForMenu).Run(func(_ mock.Arguments) {
		*events = append(*events, "stop")
	}).Return(nil).Once()

	limitsEnabled := true
	cfg := newTestConfig(t, &config.Values{
		Playtime: config.Playtime{Limits: config.PlaytimeLimits{
			Enabled: &limitsEnabled,
			Session: "1m",
		}},
	})

	start := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)
	clock := clockwork.NewFakeClockAt(start.Add(time.Hour))

	tm := NewLimitsManager(
		&database.Database{UserDB: mockDB}, mockPlatform, cfg, clock, newNoOpMockPlayer(),
	)
	tm.SetEnabled(true)

	tm.mu.Lock()
	tm.sessionStart = start
	tm.sessionStartMono = start
	tm.sessionStartReliable = true
	tm.mu.Unlock()

	return tm, mockPlatform
}

// A before_exit script must get its chance to save state before a limit yanks
// the game away.
func TestLimitsManager_RunsBeforeExitBeforeStoppingGame(t *testing.T) {
	t.Parallel()

	var events []string
	tm, mockPlatform := newExceededLimitsManager(t, &events)
	tm.SetBeforeExitHook(func() { events = append(events, "before_exit") })

	tm.checkLimits()

	assert.Equal(t, []string{"before_exit", "stop"}, events,
		"before_exit must run before the limit stops the launcher")
	mockPlatform.AssertExpectations(t)
}

func TestLimitsManager_NilBeforeExitHookIsSafe(t *testing.T) {
	t.Parallel()

	var events []string
	tm, mockPlatform := newExceededLimitsManager(t, &events)

	require.NotPanics(t, tm.checkLimits)

	assert.Equal(t, []string{"stop"}, events)
	mockPlatform.AssertExpectations(t)
}
