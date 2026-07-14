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

package profiles

import (
	"strings"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/userdb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func newTestService(t *testing.T) (svc *Service, mockDB *helpers.MockUserDBI, st *state.State) {
	t.Helper()
	mockDB = helpers.NewMockUserDBI()
	st, ns := state.NewState(nil, "boot")
	t.Cleanup(func() {
		for {
			select {
			case <-ns:
			default:
				return
			}
		}
	})
	svc = NewService(&database.Database{UserDB: mockDB, MediaDB: nil}, st)
	return svc, mockDB, st
}

func pinProfile(t *testing.T, pin string) *database.Profile {
	t.Helper()
	p := &database.Profile{
		ProfileID: "profile-1",
		Name:      "Kid A",
		SwitchID:  "corn-arm-truck",
	}
	if pin != "" {
		hash, err := HashPIN(pin)
		require.NoError(t, err)
		p.PINHash = hash
	}
	return p
}

func TestActivateByID_NoPIN(t *testing.T) {
	t.Parallel()
	svc, mockDB, st := newTestService(t)

	mockDB.On("GetProfile", "profile-1").Return(pinProfile(t, ""), nil)
	mockDB.On("SetDeviceState", database.DeviceStateKeyActiveProfile, "profile-1").Return(nil)

	snap, err := svc.ActivateByID("profile-1", "")
	require.NoError(t, err)
	assert.Equal(t, "Kid A", snap.Name)
	assert.False(t, snap.HasPIN)

	active := st.ActiveProfile()
	require.NotNil(t, active)
	assert.Equal(t, "profile-1", active.ProfileID)
	mockDB.AssertExpectations(t)
}

func TestActivateByID_PINEnforced(t *testing.T) {
	t.Parallel()
	svc, mockDB, st := newTestService(t)

	mockDB.On("GetProfile", "profile-1").Return(pinProfile(t, "1234"), nil)

	_, err := svc.ActivateByID("profile-1", "")
	require.ErrorIs(t, err, ErrPINRequired)

	_, err = svc.ActivateByID("profile-1", "9999")
	require.ErrorIs(t, err, ErrPINIncorrect)

	assert.Nil(t, st.ActiveProfile())

	mockDB.On("SetDeviceState", database.DeviceStateKeyActiveProfile, "profile-1").Return(nil)
	snap, err := svc.ActivateByID("profile-1", "1234")
	require.NoError(t, err)
	assert.True(t, snap.HasPIN)
	require.NotNil(t, st.ActiveProfile())
}

func TestActivateByID_RateLimited(t *testing.T) {
	t.Parallel()
	svc, mockDB, _ := newTestService(t)

	now := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }

	mockDB.On("GetProfile", "profile-1").Return(pinProfile(t, "1234"), nil)

	for range pinAttemptLimit {
		_, err := svc.ActivateByID("profile-1", "9999")
		require.ErrorIs(t, err, ErrPINIncorrect)
	}

	// Even the correct PIN is rejected while rate limited.
	_, err := svc.ActivateByID("profile-1", "1234")
	require.ErrorIs(t, err, ErrPINRateLimited)

	// After the window passes, attempts work again.
	now = now.Add(pinAttemptWindow + time.Second)
	mockDB.On("SetDeviceState", database.DeviceStateKeyActiveProfile, "profile-1").Return(nil)
	_, err = svc.ActivateByID("profile-1", "1234")
	require.NoError(t, err)
}

func TestActivateBySwitchID_BypassesPIN(t *testing.T) {
	t.Parallel()
	svc, mockDB, st := newTestService(t)

	// The switch ID is a bearer credential: presenting it activates a
	// PIN-protected profile with no PIN, on every path.
	mockDB.On("GetProfileBySwitchID", "corn-arm-truck").Return(pinProfile(t, "1234"), nil)
	mockDB.On("SetDeviceState", database.DeviceStateKeyActiveProfile, "profile-1").Return(nil)

	snap, err := svc.ActivateBySwitchID("corn-arm-truck")
	require.NoError(t, err)
	assert.Equal(t, "profile-1", snap.ProfileID)
	require.NotNil(t, st.ActiveProfile())
}

func TestDeactivate(t *testing.T) {
	t.Parallel()
	svc, mockDB, st := newTestService(t)

	st.SetActiveProfile(&models.ActiveProfile{ProfileID: "profile-1", Name: "Kid A"})
	mockDB.On("DeleteDeviceState", database.DeviceStateKeyActiveProfile).Return(nil)

	require.NoError(t, svc.Deactivate())
	assert.Nil(t, st.ActiveProfile())
}

func TestRestoreOnBoot_Restores(t *testing.T) {
	t.Parallel()
	svc, mockDB, st := newTestService(t)

	mockDB.On("GetDeviceState", database.DeviceStateKeyActiveProfile).Return("profile-1", true, nil)
	mockDB.On("GetProfile", "profile-1").Return(pinProfile(t, ""), nil)

	require.NoError(t, svc.RestoreOnBoot())
	active := st.ActiveProfile()
	require.NotNil(t, active)
	assert.Equal(t, "profile-1", active.ProfileID)
}

func TestRestoreOnBoot_NothingPersisted(t *testing.T) {
	t.Parallel()
	svc, mockDB, st := newTestService(t)

	mockDB.On("GetDeviceState", database.DeviceStateKeyActiveProfile).Return("", false, nil)

	require.NoError(t, svc.RestoreOnBoot())
	assert.Nil(t, st.ActiveProfile())
}

func TestRestoreOnBoot_DanglingCleared(t *testing.T) {
	t.Parallel()
	svc, mockDB, st := newTestService(t)

	mockDB.On("GetDeviceState", database.DeviceStateKeyActiveProfile).Return("deleted-profile", true, nil)
	mockDB.On("GetProfile", "deleted-profile").Return(nil, userdb.ErrProfileNotFound)
	mockDB.On("DeleteDeviceState", database.DeviceStateKeyActiveProfile).Return(nil)

	require.NoError(t, svc.RestoreOnBoot())
	assert.Nil(t, st.ActiveProfile())
	mockDB.AssertExpectations(t)
}

func TestCreate_GeneratesSwitchIDAndHashesPIN(t *testing.T) {
	t.Parallel()
	svc, mockDB, _ := newTestService(t)

	pin := "1234"
	mockDB.On("CreateProfile", mock.MatchedBy(func(p *database.Profile) bool {
		return p.ProfileID != "" &&
			len(strings.Split(p.SwitchID, "-")) == switchIDWords &&
			strings.HasPrefix(p.PINHash, "pbkdf2-sha256$")
	})).Return(nil)

	p, err := svc.Create(&models.NewProfileParams{Name: "Kid A", PIN: &pin})
	require.NoError(t, err)
	assert.Equal(t, "Kid A", p.Name)
	assert.True(t, VerifyPIN("1234", p.PINHash))
	mockDB.AssertExpectations(t)
}

func TestCreate_RejectsBadDuration(t *testing.T) {
	t.Parallel()
	svc, _, _ := newTestService(t)

	bad := "2 hours"
	_, err := svc.Create(&models.NewProfileParams{Name: "Kid A", DailyLimit: &bad})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid limit duration")

	// Negative durations would silently behave as "no limit" downstream —
	// reject them at validation instead.
	negative := "-5m"
	_, err = svc.Create(&models.NewProfileParams{Name: "Kid A", SessionLimit: &negative})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must not be negative")
}

func TestUpdate_RefreshesActiveSnapshot(t *testing.T) {
	t.Parallel()
	svc, mockDB, st := newTestService(t)

	existing := pinProfile(t, "")
	st.SetActiveProfile(&models.ActiveProfile{ProfileID: existing.ProfileID, Name: existing.Name})

	mockDB.On("GetProfile", "profile-1").Return(existing, nil)
	mockDB.On("UpdateProfile", mock.Anything).Return(nil)

	daily := "2h"
	enabled := true
	_, err := svc.Update(&models.UpdateProfileParams{
		ProfileID:     "profile-1",
		DailyLimit:    &daily,
		LimitsEnabled: &enabled,
	})
	require.NoError(t, err)

	active := st.ActiveProfile()
	require.NotNil(t, active)
	require.NotNil(t, active.DailyLimit)
	assert.Equal(t, "2h", *active.DailyLimit)
	require.NotNil(t, active.LimitsEnabled)
	assert.True(t, *active.LimitsEnabled)
}

func TestUpdate_ClearLimits(t *testing.T) {
	t.Parallel()
	svc, mockDB, _ := newTestService(t)

	existing := pinProfile(t, "")
	enabled := true
	daily := "1h"
	existing.LimitsEnabled = &enabled
	existing.DailyLimit = &daily

	mockDB.On("GetProfile", "profile-1").Return(existing, nil)
	mockDB.On("UpdateProfile", mock.MatchedBy(func(p *database.Profile) bool {
		return p.LimitsEnabled == nil && p.DailyLimit == nil && p.SessionLimit == nil
	})).Return(nil)

	_, err := svc.Update(&models.UpdateProfileParams{ProfileID: "profile-1", ClearLimits: true})
	require.NoError(t, err)
	mockDB.AssertExpectations(t)
}

func TestDelete_ActiveProfileDeactivates(t *testing.T) {
	t.Parallel()
	svc, mockDB, st := newTestService(t)

	st.SetActiveProfile(&models.ActiveProfile{ProfileID: "profile-1", Name: "Kid A"})
	mockDB.On("DeleteProfile", "profile-1").Return(nil)

	require.NoError(t, svc.Delete("profile-1"))
	assert.Nil(t, st.ActiveProfile())
}
