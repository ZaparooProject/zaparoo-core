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

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type monitorHealCall struct {
	now         time.Time
	wasReliable bool
	healed      bool
}

func TestMonitorClockAndHealTimestamps_TransitionState(t *testing.T) {
	t.Parallel()

	baseTime := time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC)
	fixedUptime := func() (time.Duration, error) { return 2 * time.Hour, nil }

	tests := []struct {
		name                string
		ticks               []time.Time
		reliabilitySequence []bool
		healReturns         []bool
		expectedCalls       []monitorHealCall
	}{
		{
			name:                "reliable startup keeps reliable state",
			ticks:               []time.Time{baseTime.Add(time.Minute)},
			reliabilitySequence: []bool{true, true},
			healReturns:         []bool{false},
			expectedCalls: []monitorHealCall{
				{now: baseTime.Add(time.Minute), wasReliable: true, healed: false},
			},
		},
		{
			name:                "unreliable to reliable transition marks healed on success",
			ticks:               []time.Time{baseTime.Add(time.Minute)},
			reliabilitySequence: []bool{false, true},
			healReturns:         []bool{true},
			expectedCalls: []monitorHealCall{
				{now: baseTime.Add(time.Minute), wasReliable: false, healed: false},
			},
		},
		{
			name: "unreliable to reliable transition retries after failed heal",
			ticks: []time.Time{
				baseTime.Add(time.Minute),
				baseTime.Add(2 * time.Minute),
			},
			reliabilitySequence: []bool{false, true, true},
			healReturns:         []bool{false, true},
			expectedCalls: []monitorHealCall{
				{now: baseTime.Add(time.Minute), wasReliable: false, healed: false},
				{now: baseTime.Add(2 * time.Minute), wasReliable: false, healed: false},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			reliabilitySequence := append([]bool(nil), tt.reliabilitySequence...)
			healReturns := append([]bool(nil), tt.healReturns...)
			calls := make([]monitorHealCall, 0, len(tt.expectedCalls))
			ticks := make(chan time.Time, len(tt.ticks))

			isClockReliable := func(time.Time) bool {
				require.NotEmpty(t, reliabilitySequence)
				next := reliabilitySequence[0]
				reliabilitySequence = reliabilitySequence[1:]
				return next
			}
			healFunc := func(
				_ *database.Database,
				_ string,
				now time.Time,
				wasReliable bool,
				healed bool,
				getUptime uptimeProvider,
				isReliable clockReliabilityFunc,
			) bool {
				require.NotNil(t, getUptime)
				require.NotNil(t, isReliable)
				require.NotEmpty(t, healReturns)
				calls = append(calls, monitorHealCall{
					now:         now,
					wasReliable: wasReliable,
					healed:      healed,
				})
				next := healReturns[0]
				healReturns = healReturns[1:]
				return next
			}

			done := make(chan struct{})
			go func() {
				defer close(done)
				monitorClockAndHealTimestampsWithDeps(
					context.Background(),
					&database.Database{},
					"boot-uuid",
					ticks,
					func() time.Time { return baseTime },
					isClockReliable,
					fixedUptime,
					healFunc,
				)
			}()

			for _, tick := range tt.ticks {
				ticks <- tick
			}
			close(ticks)

			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("monitor did not stop after ticker channel closed")
			}

			assert.Equal(t, tt.expectedCalls, calls)
			assert.Empty(t, reliabilitySequence)
			assert.Empty(t, healReturns)
		})
	}
}

func TestHealTimestampsIfClockReliable_SkipsReliableStartup(t *testing.T) {
	t.Parallel()

	mockUserDB := &testhelpers.MockUserDBI{}
	db := &database.Database{UserDB: mockUserDB}
	reliableNow := time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC)
	fixedUptime := func() (time.Duration, error) { return 2 * time.Hour, nil }

	assert.False(t, healTimestampsIfClockReliable(db, "boot-uuid", reliableNow, true, false, fixedUptime))
	mockUserDB.AssertNotCalled(t, "HealTimestamps", mock.Anything, mock.Anything)
}

func TestHealTimestampsIfClockReliable_HealsOnTransition(t *testing.T) {
	t.Parallel()

	mockUserDB := &testhelpers.MockUserDBI{}
	db := &database.Database{UserDB: mockUserDB}
	reliableNow := time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC)
	fixedUptime := func() (time.Duration, error) { return 2 * time.Hour, nil }

	mockUserDB.On("HealTimestamps", "boot-uuid", mock.AnythingOfType("time.Time")).Return(int64(3), nil).Once()

	healed := healTimestampsIfClockReliable(db, "boot-uuid", reliableNow, false, false, fixedUptime)
	assert.True(t, healed)

	mockUserDB.AssertExpectations(t)
}

func TestHealTimestampsIfClockReliable_ZeroRowsMarksHealed(t *testing.T) {
	t.Parallel()

	mockUserDB := &testhelpers.MockUserDBI{}
	db := &database.Database{UserDB: mockUserDB}
	reliableNow := time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC)
	fixedUptime := func() (time.Duration, error) { return 2 * time.Hour, nil }

	mockUserDB.On("HealTimestamps", "boot-uuid", mock.AnythingOfType("time.Time")).Return(int64(0), nil).Once()

	healed := healTimestampsIfClockReliable(db, "boot-uuid", reliableNow, false, false, fixedUptime)
	assert.True(t, healed)

	healed = healTimestampsIfClockReliable(db, "boot-uuid", reliableNow.Add(time.Minute), false, healed, fixedUptime)
	assert.True(t, healed)

	mockUserDB.AssertExpectations(t)
}

func TestHealTimestampsIfClockReliable_RetriesAfterDBError(t *testing.T) {
	t.Parallel()

	mockUserDB := &testhelpers.MockUserDBI{}
	db := &database.Database{UserDB: mockUserDB}
	reliableNow := time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC)
	boom := errors.New("boom")
	fixedUptime := func() (time.Duration, error) { return 2 * time.Hour, nil }

	mockUserDB.On("HealTimestamps", "boot-uuid", mock.AnythingOfType("time.Time")).Return(int64(0), boom).Once()
	mockUserDB.On("HealTimestamps", "boot-uuid", mock.AnythingOfType("time.Time")).Return(int64(1), nil).Once()

	healed := healTimestampsIfClockReliable(db, "boot-uuid", reliableNow, false, false, fixedUptime)
	assert.False(t, healed)

	healed = healTimestampsIfClockReliable(db, "boot-uuid", reliableNow.Add(time.Minute), false, healed, fixedUptime)
	assert.True(t, healed)

	mockUserDB.AssertExpectations(t)
}

func TestHealTimestampsIfClockReliable_RetriesAfterUptimeError(t *testing.T) {
	t.Parallel()

	mockUserDB := &testhelpers.MockUserDBI{}
	db := &database.Database{UserDB: mockUserDB}
	reliableNow := time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC)
	boom := errors.New("boom")
	failedUptime := func() (time.Duration, error) { return 0, boom }
	fixedUptime := func() (time.Duration, error) { return 2 * time.Hour, nil }

	mockUserDB.On("HealTimestamps", "boot-uuid", mock.AnythingOfType("time.Time")).Return(int64(1), nil).Once()

	healed := healTimestampsIfClockReliable(db, "boot-uuid", reliableNow, false, false, failedUptime)
	assert.False(t, healed)

	healed = healTimestampsIfClockReliable(db, "boot-uuid", reliableNow.Add(time.Minute), false, healed, fixedUptime)
	assert.True(t, healed)

	mockUserDB.AssertExpectations(t)
}

func TestHealTimestampsIfClockReliable_SkipsUnreliableOrAlreadyHealed(t *testing.T) {
	t.Parallel()

	mockUserDB := &testhelpers.MockUserDBI{}
	db := &database.Database{UserDB: mockUserDB}
	unreliableNow := time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)
	reliableNow := time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC)
	fixedUptime := func() (time.Duration, error) { return 2 * time.Hour, nil }

	assert.False(t, healTimestampsIfClockReliable(db, "boot-uuid", unreliableNow, false, false, fixedUptime))
	assert.True(t, healTimestampsIfClockReliable(db, "boot-uuid", reliableNow, false, true, fixedUptime))
	mockUserDB.AssertNotCalled(t, "HealTimestamps", mock.Anything, mock.Anything)
}
