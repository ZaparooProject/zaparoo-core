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

package kiosk_test

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	gozapscript "github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/kiosk"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/jonboulle/clockwork"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	adminProfileID = "admin-1"
	kidProfileID   = "kid-1"
)

// newLock returns a lock over a real in-memory user database holding one
// admin profile and one member profile, the smallest household that can be
// locked.
func newLock(t *testing.T) (*kiosk.Service, *database.Database, *clockwork.FakeClock) {
	t.Helper()
	userDB, cleanup := helpers.NewInMemoryUserDB(t)
	t.Cleanup(cleanup)
	db := &database.Database{UserDB: userDB}
	require.NoError(t, userDB.CreateProfile(&database.Profile{
		ProfileID: adminProfileID, Name: "Parent", Role: "admin", SwitchID: "a-b-c", PINHash: "x",
	}))
	require.NoError(t, userDB.CreateProfile(&database.Profile{
		ProfileID: kidProfileID, Name: "Kid", Role: "member", SwitchID: "d-e-f",
	}))
	clock := clockwork.NewFakeClock()
	return kiosk.NewService(db, clock), db, clock
}

func preset(t *testing.T, name string) *kiosk.Settings {
	t.Helper()
	settings, ok := kiosk.PresetSettings(name)
	require.True(t, ok)
	return &settings
}

// enableOpen locks with a rule that allows everything, for tests that are
// not about the content rule.
func enableOpen(t *testing.T, lock *kiosk.Service) {
	t.Helper()
	settings := preset(t, kiosk.PresetFamily)
	settings.DefaultContentMode = database.ContentModeAll
	require.NoError(t, lock.Enable(settings, kiosk.EnableOptions{}))
}

func TestNilServiceReportsOff(t *testing.T) {
	t.Parallel()
	var lock *kiosk.Service
	assert.False(t, lock.Locked())
	assert.False(t, lock.Active())
	assert.Equal(t, kiosk.StateOff, lock.Status().State)
	assert.False(t, lock.CheckRecoveryToken("anything"))
	require.NoError(t, lock.CheckLaunch(context.Background(), "", "NES", "/games/a.nes", true))

	token := tokens.Token{}
	lock.GuardToken(&token)
	assert.False(t, token.AllowedCommands.Guarded(), "a device with no lock guards nothing")
	assert.True(t, lock.CommandPermitted(gozapscript.ZapScriptCmdExecute))
}

func TestEnableLocksAndPersists(t *testing.T) {
	t.Parallel()
	lock, db, clock := newLock(t)
	assert.False(t, lock.Active())

	enableOpen(t, lock)
	assert.True(t, lock.Locked())

	// A restart, a crash and a power cut all look the same from here: the
	// service is built again from what was stored.
	restarted := kiosk.NewService(db, clock)
	assert.True(t, restarted.Locked())
	assert.Equal(t, kiosk.PresetFamily, restarted.Status().Settings.Preset)
}

func TestRestartWhileUnlockedComesBackLocked(t *testing.T) {
	t.Parallel()
	lock, db, clock := newLock(t)
	enableOpen(t, lock)
	require.NoError(t, lock.Unlock(0))
	require.Equal(t, kiosk.StateUnlocked, lock.Status().State)

	restarted := kiosk.NewService(db, clock)
	assert.True(t, restarted.Locked(), "a restart must not carry a temporary unlock over")
}

func TestDisableStaysOffAfterRestart(t *testing.T) {
	t.Parallel()
	lock, db, clock := newLock(t)
	enableOpen(t, lock)
	require.NoError(t, lock.Disable())
	assert.False(t, lock.Active())
	assert.False(t, kiosk.NewService(db, clock).Active())
}

func TestUnlockExpiresOnItsOwn(t *testing.T) {
	t.Parallel()
	lock, _, clock := newLock(t)
	enableOpen(t, lock)

	require.NoError(t, lock.Unlock(time.Minute))
	status := lock.Status()
	require.Equal(t, kiosk.StateUnlocked, status.State)
	assert.Equal(t, clock.Now().Add(time.Minute), status.UnlockUntil)
	assert.False(t, lock.Locked())
	assert.True(t, lock.Active())

	clock.Advance(59 * time.Second)
	assert.False(t, lock.Locked())

	clock.Advance(2 * time.Second)
	assert.Eventually(t, lock.Locked, time.Second, time.Millisecond)
}

func TestRelockHoldsRestoreAccessForItsListeners(t *testing.T) {
	t.Parallel()
	lock, _, clock := newLock(t)
	var held, heldInListener atomic.Bool
	relocked := make(chan struct{}, 1)
	lock.SetRestoreAccess(func() (func(), error) {
		held.Store(true)
		return func() { held.Store(false) }, nil
	})
	lock.OnChange(func(status kiosk.Status) {
		if status.State == kiosk.StateLocked {
			heldInListener.Store(held.Load())
			select {
			case relocked <- struct{}{}:
			default:
			}
		}
	})
	enableOpen(t, lock)
	<-relocked
	require.NoError(t, lock.Unlock(time.Minute))

	clock.Advance(2 * time.Minute)
	select {
	case <-relocked:
	case <-time.After(5 * time.Second):
		t.Fatal("the lock did not relock")
	}
	assert.True(t, heldInListener.Load(), "listeners run with restore access held")
	assert.Eventually(t, func() bool { return !held.Load() }, time.Second, time.Millisecond,
		"and it is released after")
}

func TestRelockWithoutRestoreAccessStillLocks(t *testing.T) {
	t.Parallel()
	lock, _, clock := newLock(t)
	// Once a restore has finished and Core waits to restart, the gate
	// refuses; the device must lock all the same.
	lock.SetRestoreAccess(func() (func(), error) {
		return nil, errors.New("backup restore restart is pending")
	})
	enableOpen(t, lock)
	require.NoError(t, lock.Unlock(time.Minute))

	clock.Advance(2 * time.Minute)
	assert.Eventually(t, lock.Locked, time.Second, time.Millisecond)
}

func TestUnlockAgainRestartsTheClock(t *testing.T) {
	t.Parallel()
	lock, _, clock := newLock(t)
	enableOpen(t, lock)
	require.NoError(t, lock.Unlock(time.Minute))
	clock.Advance(50 * time.Second)
	require.NoError(t, lock.Unlock(time.Minute))

	// The first unlock's timer would have fired here.
	clock.Advance(20 * time.Second)
	assert.Never(t, lock.Locked, 50*time.Millisecond, 5*time.Millisecond)

	clock.Advance(41 * time.Second)
	assert.Eventually(t, lock.Locked, time.Second, time.Millisecond)
}

func TestLockEndsUnlockEarly(t *testing.T) {
	t.Parallel()
	lock, _, clock := newLock(t)
	enableOpen(t, lock)
	require.NoError(t, lock.Unlock(time.Minute))
	require.NoError(t, lock.Lock())
	assert.True(t, lock.Locked())

	// The abandoned timer must not disturb a later unlock.
	require.NoError(t, lock.Unlock(10*time.Minute))
	clock.Advance(2 * time.Minute)
	assert.Never(t, lock.Locked, 50*time.Millisecond, 5*time.Millisecond)
}

func TestTransitionsNeedKioskOn(t *testing.T) {
	t.Parallel()
	lock, _, _ := newLock(t)
	require.ErrorIs(t, lock.Unlock(0), kiosk.ErrNotEnabled)
	require.ErrorIs(t, lock.Lock(), kiosk.ErrNotEnabled)
	require.ErrorIs(t, lock.Disable(), kiosk.ErrNotEnabled)
	require.ErrorIs(t, lock.UpdateSettings(preset(t, kiosk.PresetFamily)), kiosk.ErrNotEnabled)

	enableOpen(t, lock)
	require.ErrorIs(t, lock.Enable(preset(t, kiosk.PresetPublic), kiosk.EnableOptions{}), kiosk.ErrAlreadyEnabled)
}

func TestEnableRefusesALockNobodyCouldOpen(t *testing.T) {
	t.Parallel()

	t.Run("no admin profile", func(t *testing.T) {
		t.Parallel()
		userDB, cleanup := helpers.NewInMemoryUserDB(t)
		t.Cleanup(cleanup)
		lock := kiosk.NewService(&database.Database{UserDB: userDB}, clockwork.NewFakeClock())
		err := lock.Enable(preset(t, kiosk.PresetFamily), kiosk.EnableOptions{ConfirmEmpty: true})
		require.ErrorIs(t, err, kiosk.ErrNoAdminProfile)
		assert.False(t, lock.Active())
	})

	t.Run("no on-device unlock and no admin client", func(t *testing.T) {
		t.Parallel()
		lock, db, _ := newLock(t)
		err := lock.Enable(preset(t, kiosk.PresetPublic), kiosk.EnableOptions{})
		require.ErrorIs(t, err, kiosk.ErrNoAdminClient)
		assert.False(t, lock.Active())

		require.NoError(t, db.UserDB.CreateClient(&database.Client{
			ClientID: "c1", ClientName: "kid phone", AuthToken: "t1", Role: "member", PairingKey: make([]byte, 32),
		}))
		require.ErrorIs(t, lock.Enable(preset(t, kiosk.PresetPublic), kiosk.EnableOptions{}), kiosk.ErrNoAdminClient)

		require.NoError(t, db.UserDB.CreateClient(&database.Client{
			ClientID: "c2", ClientName: "parent phone", AuthToken: "t2", Role: "admin", PairingKey: make([]byte, 32),
		}))
		require.NoError(t, lock.Enable(preset(t, kiosk.PresetPublic), kiosk.EnableOptions{}))
		assert.True(t, lock.Locked())
	})

	t.Run("settings cannot drop the last way in", func(t *testing.T) {
		t.Parallel()
		lock, _, _ := newLock(t)
		enableOpen(t, lock)
		settings := lock.Status().Settings
		settings.DeviceUnlock = false
		require.ErrorIs(t, lock.UpdateSettings(&settings), kiosk.ErrNoAdminClient)
		assert.True(t, lock.Status().Settings.DeviceUnlock)
	})
}

func TestEnableAsksBeforeLockingIntoNothing(t *testing.T) {
	t.Parallel()
	lock, db, _ := newLock(t)

	// Family limits every profile to allowed content, and nothing is
	// allowed yet.
	err := lock.Enable(preset(t, kiosk.PresetFamily), kiosk.EnableOptions{})
	require.ErrorIs(t, err, kiosk.ErrNothingAllowed)
	assert.False(t, lock.Active())

	// Allowing something for the shared profile is not enough while the
	// kid's profile still has nothing.
	allowNES := &database.ContentRule{Entries: []database.ContentRuleEntry{
		{Effect: database.ContentEffectAllow, Kind: database.ContentKindSystem, SystemID: "NES"},
	}}
	require.NoError(t, db.UserDB.ReplaceContentRule("", allowNES))
	require.ErrorIs(t, lock.Enable(preset(t, kiosk.PresetFamily), kiosk.EnableOptions{}), kiosk.ErrNothingAllowed)

	require.NoError(t, db.UserDB.ReplaceContentRule(kidProfileID, allowNES))
	require.NoError(t, lock.Enable(preset(t, kiosk.PresetFamily), kiosk.EnableOptions{}))
	assert.True(t, lock.Locked())
}

func TestEnableWithConfirmEmptyLocksAnyway(t *testing.T) {
	t.Parallel()
	lock, _, _ := newLock(t)
	require.NoError(t, lock.Enable(preset(t, kiosk.PresetFamily), kiosk.EnableOptions{ConfirmEmpty: true}))
	assert.True(t, lock.Locked())
}

func TestInvalidSettingsAreRefused(t *testing.T) {
	t.Parallel()
	lock, _, _ := newLock(t)
	tests := map[string]func(*kiosk.Settings){
		"unknown preset":       func(s *kiosk.Settings) { s.Preset = "arcade" },
		"unknown content mode": func(s *kiosk.Settings) { s.DefaultContentMode = "some" },
		"unknown downloads":    func(s *kiosk.Settings) { s.Downloads = "all" },
		"unlock too short":     func(s *kiosk.Settings) { s.UnlockDuration = 5 },
		"unlock too long":      func(s *kiosk.Settings) { s.UnlockDuration = 7200 },
		"idle timeout too short": func(s *kiosk.Settings) {
			s.IdleReset = true
			s.IdleTimeout = 5
		},
	}
	for name, mutate := range tests {
		settings := preset(t, kiosk.PresetFamily)
		mutate(settings)
		err := lock.Enable(settings, kiosk.EnableOptions{ConfirmEmpty: true})
		require.ErrorIs(t, err, kiosk.ErrInvalidSettings, name)
	}
	assert.False(t, lock.Active())
}

func TestListenersSeeEveryTransition(t *testing.T) {
	t.Parallel()
	lock, _, _ := newLock(t)
	var seen []kiosk.State
	lock.OnChange(func(status kiosk.Status) { seen = append(seen, status.State) })

	enableOpen(t, lock)
	require.NoError(t, lock.Unlock(time.Minute))
	require.NoError(t, lock.Lock())
	require.NoError(t, lock.Disable())

	assert.Equal(t, []kiosk.State{
		kiosk.StateLocked, kiosk.StateUnlocked, kiosk.StateLocked, kiosk.StateOff,
	}, seen)
}

func TestRecoveryToken(t *testing.T) {
	t.Parallel()
	lock, _, _ := newLock(t)
	fs := afero.NewMemMapFs()

	assert.False(t, lock.CheckRecoveryToken(""), "no token exists before one is written")

	require.NoError(t, lock.InitRecoveryToken(fs, "/data"))
	raw, err := afero.ReadFile(fs, kiosk.RecoveryTokenPath("/data"))
	require.NoError(t, err)
	token := strings.TrimSpace(string(raw))
	require.Len(t, token, 64)

	info, err := fs.Stat(kiosk.RecoveryTokenPath("/data"))
	require.NoError(t, err)
	assert.Equal(t, "-rw-------", info.Mode().String())

	assert.True(t, lock.CheckRecoveryToken(token))
	assert.False(t, lock.CheckRecoveryToken(""))
	assert.False(t, lock.CheckRecoveryToken(token+"0"))

	// A restart writes a new token and the old one stops working.
	require.NoError(t, lock.InitRecoveryToken(fs, "/data"))
	assert.False(t, lock.CheckRecoveryToken(token))
}

func TestCommandPolicyIsDefaultDeny(t *testing.T) {
	t.Parallel()
	lock, _, _ := newLock(t)
	enableOpen(t, lock)
	policy := lock.CommandPolicy()

	for _, name := range []string{
		gozapscript.ZapScriptCmdLaunch, gozapscript.ZapScriptCmdLaunchRandom,
		gozapscript.ZapScriptCmdStop, gozapscript.ZapScriptCmdPlaylistNext,
		gozapscript.ZapScriptCmdDelay, gozapscript.ZapScriptCmdProfile,
		gozapscript.ZapScriptCmdPlaytimeExtend,
	} {
		assert.True(t, policy.Allows(name), name)
	}
	for _, name := range []string{
		gozapscript.ZapScriptCmdExecute, gozapscript.ZapScriptCmdShell, gozapscript.ZapScriptCmdCommand,
		gozapscript.ZapScriptCmdInputKeyboard, gozapscript.ZapScriptCmdInputGamepad,
		gozapscript.ZapScriptCmdInputText, gozapscript.ZapScriptCmdKey,
		gozapscript.ZapScriptCmdHTTPGet, gozapscript.ZapScriptCmdHTTPPost, gozapscript.ZapScriptCmdGet,
		gozapscript.ZapScriptCmdMisterINI, gozapscript.ZapScriptCmdMisterCore,
		gozapscript.ZapScriptCmdMisterScript, gozapscript.ZapScriptCmdMisterMGL,
		gozapscript.ZapScriptCmdMisterWallpaper, gozapscript.ZapScriptCmdINI,
		gozapscript.ZapScriptCmdWrite, gozapscript.ZapScriptCmdScreenshot,
		gozapscript.ZapScriptCmdProfileClear, gozapscript.ZapScriptCmdAdd, gozapscript.ZapScriptCmdRemove,
		"a.command.added.later",
	} {
		assert.False(t, policy.Allows(name), name)
	}
}

func TestCommandPolicyFollowsSettings(t *testing.T) {
	t.Parallel()
	lock, _, _ := newLock(t)
	enableOpen(t, lock)
	settings := lock.Status().Settings
	settings.ProfileSwitching = false
	settings.ExtensionCard = false
	require.NoError(t, lock.UpdateSettings(&settings))

	policy := lock.CommandPolicy()
	assert.False(t, policy.Allows(gozapscript.ZapScriptCmdProfile))
	assert.False(t, policy.Allows(gozapscript.ZapScriptCmdPlaytimeExtend))
	assert.True(t, policy.Allows(gozapscript.ZapScriptCmdLaunch))
}

func TestGuardedTokenFollowsTheLock(t *testing.T) {
	t.Parallel()
	lock, _, _ := newLock(t)

	// The mark is made whatever the lock's state, and adds no bound.
	token := tokens.Token{}
	lock.GuardToken(&token)
	assert.True(t, token.AllowedCommands.Guarded())
	assert.True(t, token.AllowedCommands.Unrestricted())
	assert.True(t, lock.CommandPermitted(gozapscript.ZapScriptCmdExecute), "kiosk off permits everything")

	// What a guarded token may run is decided when the command runs.
	enableOpen(t, lock)
	assert.True(t, lock.CommandPermitted(gozapscript.ZapScriptCmdLaunch))
	assert.False(t, lock.CommandPermitted(gozapscript.ZapScriptCmdExecute))

	require.NoError(t, lock.Unlock(time.Minute))
	assert.True(t, lock.CommandPermitted(gozapscript.ZapScriptCmdExecute), "a temporary unlock lifts it")

	require.NoError(t, lock.Lock())
	assert.False(t, lock.CommandPermitted(gozapscript.ZapScriptCmdExecute), "and the relock brings it back")

	// A token that carries a bound of its own keeps it beside the guard.
	narrow := tokens.Token{AllowedCommands: tokens.NewCommandPolicy(gozapscript.ZapScriptCmdStop)}
	lock.GuardToken(&narrow)
	assert.True(t, narrow.AllowedCommands.Guarded())
	assert.True(t, narrow.AllowedCommands.Allows(gozapscript.ZapScriptCmdStop))
	assert.False(t, narrow.AllowedCommands.Allows(gozapscript.ZapScriptCmdLaunch))
}

func TestStoredGarbageDoesNotUnlock(t *testing.T) {
	t.Parallel()
	_, db, clock := newLock(t)
	require.NoError(t, db.UserDB.SetDeviceState(database.DeviceStateKeyKiosk,
		`{"version":1,"enabled":true,"settings":{"preset":"family","defaultContentMode":"nonsense"}}`))

	lock := kiosk.NewService(db, clock)
	assert.True(t, lock.Locked(), "an enabled lock with unreadable settings stays locked")
	assert.Equal(t, database.ContentModeAllowed, lock.Status().Settings.DefaultContentMode)
	require.NotErrorIs(t, lock.Lock(), kiosk.ErrNotEnabled)
}

// failingStateDB fails writes of the kiosk record on demand.
type failingStateDB struct {
	database.UserDBI
	fail bool
}

func (f *failingStateDB) SetDeviceState(key, value string) error {
	if f.fail && key == database.DeviceStateKeyKiosk {
		return errors.New("disk is full")
	}
	return f.UserDBI.SetDeviceState(key, value) //nolint:wrapcheck // test double passes the real result through
}

func TestFailedDisableDuringUnlockStillRelocks(t *testing.T) {
	t.Parallel()
	_, db, clock := newLock(t)
	failing := &failingStateDB{UserDBI: db.UserDB}
	lock := kiosk.NewService(&database.Database{UserDB: failing}, clock)
	enableOpen(t, lock)
	require.NoError(t, lock.Unlock(time.Minute))

	failing.fail = true
	require.Error(t, lock.Disable())
	require.Equal(t, kiosk.StateUnlocked, lock.Status().State, "a disable that was not stored changes nothing")

	// The unlock it rolled back to still has to end.
	clock.Advance(2 * time.Minute)
	assert.Eventually(t, lock.Locked, time.Second, time.Millisecond)
}

func TestSettingsPatchesBuildOnEachOther(t *testing.T) {
	t.Parallel()
	lock, _, _ := newLock(t)
	enableOpen(t, lock)

	done := make(chan error, 2)
	go func() {
		done <- lock.PatchSettings(func(s *kiosk.Settings) { s.ExtensionCard = false })
	}()
	go func() {
		done <- lock.PatchSettings(func(s *kiosk.Settings) { s.ProfileSwitching = false })
	}()
	require.NoError(t, <-done)
	require.NoError(t, <-done)

	settings := lock.Status().Settings
	assert.False(t, settings.ExtensionCard, "neither change undoes the other")
	assert.False(t, settings.ProfileSwitching)
}

func TestPatchSettingsRefusesAnInvalidResult(t *testing.T) {
	t.Parallel()
	lock, _, _ := newLock(t)
	enableOpen(t, lock)
	err := lock.PatchSettings(func(s *kiosk.Settings) { s.UnlockDuration = 1 })
	require.ErrorIs(t, err, kiosk.ErrInvalidSettings)
	assert.Equal(t, 300, lock.Status().Settings.UnlockDuration)
}
