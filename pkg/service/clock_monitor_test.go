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
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestHealTimestampsIfClockReliable_RetriesUntilRowsHealed(t *testing.T) {
	t.Parallel()

	mockUserDB := &testhelpers.MockUserDBI{}
	db := &database.Database{UserDB: mockUserDB}
	now := time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC)

	mockUserDB.On("HealTimestamps", "boot-uuid", mock.AnythingOfType("time.Time")).Return(int64(0), nil).Once()
	mockUserDB.On("HealTimestamps", "boot-uuid", mock.AnythingOfType("time.Time")).Return(int64(3), nil).Once()

	healed := healTimestampsIfClockReliable(db, "boot-uuid", now, false, false)
	assert.False(t, healed)

	healed = healTimestampsIfClockReliable(db, "boot-uuid", now.Add(time.Minute), true, healed)
	assert.True(t, healed)

	mockUserDB.AssertExpectations(t)
}

func TestHealTimestampsIfClockReliable_SkipsUnreliableOrAlreadyHealed(t *testing.T) {
	t.Parallel()

	mockUserDB := &testhelpers.MockUserDBI{}
	db := &database.Database{UserDB: mockUserDB}
	unreliableNow := time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)
	reliableNow := time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC)

	assert.False(t, healTimestampsIfClockReliable(db, "boot-uuid", unreliableNow, false, false))
	assert.True(t, healTimestampsIfClockReliable(db, "boot-uuid", reliableNow, true, true))
	mockUserDB.AssertNotCalled(t, "HealTimestamps", mock.Anything, mock.Anything)
}
