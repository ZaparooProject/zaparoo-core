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
	"errors"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

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
