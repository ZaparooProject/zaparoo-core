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
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/kiosk"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newLockedService returns a profiles service over a real user database
// with an admin and a member profile, and the kiosk lock attached to its
// state.
type lockedFixture struct {
	svc  *Service
	lock *kiosk.Service
	db   *database.Database
	st   *state.State
}

func newLockedService(t *testing.T) *lockedFixture {
	t.Helper()
	userDB, cleanup := helpers.NewInMemoryUserDB(t)
	t.Cleanup(cleanup)
	db := &database.Database{UserDB: userDB}
	adminHash, err := HashPIN("4321")
	require.NoError(t, err)
	require.NoError(t, userDB.CreateProfile(&database.Profile{
		ProfileID: "admin-1", Name: "Parent", Role: ProfileRoleAdmin, SwitchID: "parent-card", PINHash: adminHash,
	}))
	require.NoError(t, userDB.CreateProfile(&database.Profile{
		ProfileID: "kid-1", Name: "Kid", Role: ProfileRoleMember, SwitchID: "kid-card",
	}))
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
	lock := kiosk.NewService(db, clockwork.NewFakeClock())
	st.SetKiosk(lock)
	return &lockedFixture{svc: NewService(db, st), lock: lock, db: db, st: st}
}

func lockOpen(t *testing.T, lock *kiosk.Service, mutate func(*kiosk.Settings)) {
	t.Helper()
	settings, ok := kiosk.PresetSettings(kiosk.PresetFamily)
	require.True(t, ok)
	settings.DefaultContentMode = database.ContentModeAll
	if mutate != nil {
		mutate(&settings)
	}
	require.NoError(t, lock.Enable(&settings, kiosk.EnableOptions{}))
}

func TestKioskGuard_NothingSwitchesToAnAdminProfile(t *testing.T) {
	t.Parallel()
	f := newLockedService(t)
	svc, lock, st := f.svc, f.lock, f.st

	// Unlocked, both the PIN and the card work as they always have.
	_, err := svc.ActivateByID("admin-1", "4321")
	require.NoError(t, err)
	require.NoError(t, svc.Deactivate())

	lockOpen(t, lock, nil)
	_, err = svc.ActivateByID("admin-1", "4321")
	require.ErrorIs(t, err, ErrKioskLocked, "the right PIN")
	_, err = svc.ActivateBySwitchID("parent-card")
	require.ErrorIs(t, err, ErrKioskLocked, "the admin's own card")
	assert.Nil(t, st.ActiveProfile())

	require.NoError(t, lock.Unlock(time.Minute))
	_, err = svc.ActivateBySwitchID("parent-card")
	require.NoError(t, err, "temporarily unlocked")
}

func TestKioskGuard_RefusalDoesNotSpendAPINAttempt(t *testing.T) {
	t.Parallel()
	f := newLockedService(t)
	svc, lock := f.svc, f.lock
	lockOpen(t, lock, nil)

	// The lock answers before the PIN is looked at, so a locked device
	// tells a guesser nothing about whether a PIN was right.
	for range 10 {
		_, err := svc.ActivateByID("admin-1", "0000")
		require.ErrorIs(t, err, ErrKioskLocked)
	}
	require.NoError(t, lock.Unlock(time.Minute))
	_, err := svc.ActivateByID("admin-1", "4321")
	require.NoError(t, err)
}

func TestKioskGuard_MemberProfilesStillSwitch(t *testing.T) {
	t.Parallel()
	f := newLockedService(t)
	svc, lock, st := f.svc, f.lock, f.st
	lockOpen(t, lock, nil)

	_, err := svc.ActivateBySwitchID("kid-card")
	require.NoError(t, err)
	require.NotNil(t, st.ActiveProfile())
	assert.Equal(t, "kid-1", st.ActiveProfile().ProfileID)
}

func TestKioskGuard_NoLeavingToTheSharedProfile(t *testing.T) {
	t.Parallel()
	f := newLockedService(t)
	svc, lock, st := f.svc, f.lock, f.st
	_, err := svc.ActivateBySwitchID("kid-card")
	require.NoError(t, err)
	lockOpen(t, lock, nil)

	// The shared profile cannot carry a PIN, so it would be a way out of
	// whatever the kid's profile is held to.
	require.ErrorIs(t, svc.Deactivate(), ErrKioskLocked)
	require.NotNil(t, st.ActiveProfile())
	assert.Equal(t, "kid-1", st.ActiveProfile().ProfileID)
}

func TestKioskGuard_FixedProfile(t *testing.T) {
	t.Parallel()
	f := newLockedService(t)
	svc, lock, st := f.svc, f.lock, f.st
	lockOpen(t, lock, func(s *kiosk.Settings) { s.ProfileSwitching = false })

	_, err := svc.ActivateBySwitchID("kid-card")
	require.ErrorIs(t, err, ErrKioskLocked)
	_, err = svc.ActivateByID("kid-1", "")
	require.ErrorIs(t, err, ErrKioskLocked)
	assert.Nil(t, st.ActiveProfile())
}

func TestDeactivateForKioskLock(t *testing.T) {
	t.Parallel()

	t.Run("an active admin profile is dropped", func(t *testing.T) {
		t.Parallel()
		f := newLockedService(t)
		svc, lock, db, st := f.svc, f.lock, f.db, f.st
		_, err := svc.ActivateByID("admin-1", "4321")
		require.NoError(t, err)
		lockOpen(t, lock, nil)

		require.NoError(t, svc.DeactivateForKioskLock())
		assert.Nil(t, st.ActiveProfile())
		_, found, err := db.UserDB.GetDeviceState(database.DeviceStateKeyActiveProfile)
		require.NoError(t, err)
		assert.False(t, found, "and stays dropped across a restart")
	})

	t.Run("a member profile stays", func(t *testing.T) {
		t.Parallel()
		f := newLockedService(t)
		svc, lock, st := f.svc, f.lock, f.st
		_, err := svc.ActivateBySwitchID("kid-card")
		require.NoError(t, err)
		lockOpen(t, lock, nil)

		require.NoError(t, svc.DeactivateForKioskLock())
		require.NotNil(t, st.ActiveProfile())
		assert.Equal(t, "kid-1", st.ActiveProfile().ProfileID)
	})

	t.Run("no profile active", func(t *testing.T) {
		t.Parallel()
		f := newLockedService(t)
		svc, st := f.svc, f.st
		st.SetActiveProfile((*models.ActiveProfile)(nil))
		require.NoError(t, svc.DeactivateForKioskLock())
	})
}

func TestPINAttemptsSurviveARestart(t *testing.T) {
	t.Parallel()
	f := newLockedService(t)
	svc, db, st := f.svc, f.db, f.st
	// A restarted service reads the stored attempts against the real clock,
	// so the test's clock starts there too.
	now := time.Now()
	svc.now = func() time.Time { return now }

	for range pinAttemptLimit {
		_, err := svc.VerifyByID("admin-1", "0000")
		require.ErrorIs(t, err, ErrPINIncorrect)
	}
	_, err := svc.VerifyByID("admin-1", "4321")
	require.ErrorIs(t, err, ErrPINRateLimited)

	// Restarting the service must not hand out a fresh set of guesses.
	restarted := NewService(db, st)
	restarted.now = func() time.Time { return now.Add(10 * time.Second) }
	_, err = restarted.VerifyByID("admin-1", "4321")
	require.ErrorIs(t, err, ErrPINRateLimited)

	// The window still ends when it should.
	later := NewService(db, st)
	later.now = func() time.Time { return now.Add(pinAttemptWindow + time.Second) }
	_, err = later.VerifyByID("admin-1", "4321")
	require.NoError(t, err)
}

func TestPINAttemptsAreClearedBySuccess(t *testing.T) {
	t.Parallel()
	f := newLockedService(t)
	svc, db, st := f.svc, f.db, f.st
	_, err := svc.VerifyByID("admin-1", "0000")
	require.ErrorIs(t, err, ErrPINIncorrect)
	_, found, err := db.UserDB.GetDeviceState(database.DeviceStateKeyPINAttempts)
	require.NoError(t, err)
	require.True(t, found)

	_, err = svc.VerifyByID("admin-1", "4321")
	require.NoError(t, err)
	_, found, err = db.UserDB.GetDeviceState(database.DeviceStateKeyPINAttempts)
	require.NoError(t, err)
	assert.False(t, found, "nothing is left stored once the right PIN is given")

	restarted := NewService(db, st)
	for range pinAttemptLimit - 1 {
		_, err = restarted.VerifyByID("admin-1", "0000")
		require.ErrorIs(t, err, ErrPINIncorrect)
	}
	_, err = restarted.VerifyByID("admin-1", "4321")
	require.NoError(t, err)
}

func TestPINAttemptsFromTheFutureAgeOut(t *testing.T) {
	t.Parallel()
	f := newLockedService(t)
	db, st := f.db, f.st

	// A device without a real-time clock can boot with its clock years
	// behind the attempts it stored. Those must not lock the PIN until the
	// clock catches up: they count from now and age out as usual.
	boot := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	stored := `{"admin-1":[1794916800000,1794916800000,1794916800000,1794916800000,1794916800000]}`
	require.NoError(t, db.UserDB.SetDeviceState(database.DeviceStateKeyPINAttempts, stored))

	svc := NewService(db, st)
	now := boot
	svc.now = func() time.Time { return now }
	// NewService read the attempts against the real clock; reload against
	// the boot clock the way a device with a reset clock would.
	svc.pinAttempts = make(map[string][]time.Time)
	svc.loadPINAttempts()

	_, err := svc.VerifyByID("admin-1", "4321")
	require.ErrorIs(t, err, ErrPINRateLimited, "the stored attempts still count")

	now = boot.Add(pinAttemptWindow + time.Second)
	_, err = svc.VerifyByID("admin-1", "4321")
	require.NoError(t, err, "and age out a window later")
}
