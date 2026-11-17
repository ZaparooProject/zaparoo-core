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
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/kiosk"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/profiles"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// kioskHostPlatform is a platform with a lock of its own that records what
// Core asks of it, and can be told to refuse.
type kioskHostPlatform struct {
	*mocks.MockPlatform
	calls []bool
	fail  bool
	mu    syncutil.Mutex
}

func (*kioskHostPlatform) KioskUnsealed() []string { return []string{} }

func (p *kioskHostPlatform) SetKioskLocked(locked bool) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.fail {
		return errors.New("host is between processes")
	}
	p.calls = append(p.calls, locked)
	return nil
}

func (p *kioskHostPlatform) seen() []bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]bool{}, p.calls...)
}

func (p *kioskHostPlatform) setFail(fail bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.fail = fail
}

// inputPlatform is a platform that reports input when the test says so.
type inputPlatform struct {
	*mocks.MockPlatform
	activity chan struct{}
	coverage platforms.InputClass
}

func (p *inputPlatform) InputCoverage() platforms.InputClass { return p.coverage }

func (p *inputPlatform) WatchInput(ctx context.Context, _ bool, onActivity func()) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-p.activity:
			onActivity()
		}
	}
}

func newHostLock(t *testing.T) (*kiosk.Service, *clockwork.FakeClock) {
	t.Helper()
	userDB, cleanup := helpers.NewInMemoryUserDB(t)
	t.Cleanup(cleanup)
	require.NoError(t, userDB.CreateProfile(&database.Profile{
		ProfileID: "admin-1", Name: "Parent", Role: "admin", SwitchID: "a-b-c", PINHash: "x",
	}))
	clock := clockwork.NewFakeClock()
	return kiosk.NewService(&database.Database{UserDB: userDB}, clock), clock
}

func enableHostLock(t *testing.T, lock *kiosk.Service, mutate func(*kiosk.Settings)) {
	t.Helper()
	settings, ok := kiosk.PresetSettings(kiosk.PresetFamily)
	require.True(t, ok)
	settings.DefaultContentMode = database.ContentModeAll
	if mutate != nil {
		mutate(&settings)
	}
	require.NoError(t, lock.Enable(&settings, kiosk.EnableOptions{}))
}

func startHostSync(
	t *testing.T, lock *kiosk.Service, pl platforms.Platform, clock clockwork.Clock,
) chan models.Notification {
	t.Helper()
	host := newKioskHostSync(lock, pl, clock)
	mediaStarted := make(chan models.Notification)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		host.run(ctx, mediaStarted)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	return mediaStarted
}

// collapse drops consecutive repeats. The host's lock is offered again
// whenever it may have been lost, and a change that lands while the worker
// is starting is applied once from the state it reads and once from the
// change itself, so a repeat is the contract working and not a second
// transition.
func collapse(calls []bool) []bool {
	out := make([]bool, 0, len(calls))
	for i, call := range calls {
		if i == 0 || call != calls[i-1] {
			out = append(out, call)
		}
	}
	return out
}

func eventuallySeen(t *testing.T, pl *kioskHostPlatform, want []bool) {
	t.Helper()
	assert.Eventually(t, func() bool { return assert.ObjectsAreEqual(want, collapse(pl.seen())) },
		2*time.Second, time.Millisecond, "host lock calls")
}

func TestKioskHostSyncLeavesTheHostAloneWhileKioskIsOff(t *testing.T) {
	t.Parallel()
	lock, clock := newHostLock(t)
	pl := &kioskHostPlatform{MockPlatform: mocks.NewMockPlatform()}
	mediaStarted := startHostSync(t, lock, pl, clock)

	// Media starting on a device that never turned kiosk mode on must not
	// touch a lock its owner may have set on the host themselves.
	mediaStarted <- models.Notification{Method: models.NotificationStarted}
	mediaStarted <- models.Notification{Method: models.NotificationStarted}
	assert.Empty(t, pl.seen())
}

func TestKioskHostSyncFollowsEveryTransition(t *testing.T) {
	t.Parallel()
	lock, clock := newHostLock(t)
	pl := &kioskHostPlatform{MockPlatform: mocks.NewMockPlatform()}
	startHostSync(t, lock, pl, clock)

	enableHostLock(t, lock, nil)
	eventuallySeen(t, pl, []bool{true})

	require.NoError(t, lock.Unlock(time.Minute))
	eventuallySeen(t, pl, []bool{true, false})

	require.NoError(t, lock.Lock())
	eventuallySeen(t, pl, []bool{true, false, true})

	require.NoError(t, lock.Disable())
	eventuallySeen(t, pl, []bool{true, false, true, false})
}

func TestKioskHostSyncAppliesAStoredLockAtStartup(t *testing.T) {
	t.Parallel()
	lock, clock := newHostLock(t)
	enableHostLock(t, lock, nil)

	pl := &kioskHostPlatform{MockPlatform: mocks.NewMockPlatform()}
	startHostSync(t, lock, pl, clock)
	eventuallySeen(t, pl, []bool{true})
}

func TestKioskHostSyncReassertsWhenMediaStarts(t *testing.T) {
	t.Parallel()
	lock, clock := newHostLock(t)
	enableHostLock(t, lock, nil)
	pl := &kioskHostPlatform{MockPlatform: mocks.NewMockPlatform()}
	mediaStarted := startHostSync(t, lock, pl, clock)
	eventuallySeen(t, pl, []bool{true})

	// A core load replaces the host's process, and with it the lock.
	before := len(pl.seen())
	mediaStarted <- models.Notification{Method: models.NotificationStarted}
	assert.Eventually(t, func() bool { return len(pl.seen()) > before },
		2*time.Second, time.Millisecond, "the lock was not offered again")
	eventuallySeen(t, pl, []bool{true})
}

func TestKioskHostSyncRetriesAHostThatRefused(t *testing.T) {
	t.Parallel()
	lock, clock := newHostLock(t)
	pl := &kioskHostPlatform{MockPlatform: mocks.NewMockPlatform()}
	pl.setFail(true)
	startHostSync(t, lock, pl, clock)

	enableHostLock(t, lock, nil)
	require.NoError(t, clock.BlockUntilContext(t.Context(), 1))
	assert.Empty(t, pl.seen())

	pl.setFail(false)
	clock.Advance(kioskHostRetry)
	eventuallySeen(t, pl, []bool{true})
}

func TestKioskHostSyncIgnoresPlatformsWithoutALock(t *testing.T) {
	t.Parallel()
	lock, clock := newHostLock(t)
	host := newKioskHostSync(lock, mocks.NewMockPlatform(), clock)
	done := make(chan struct{})
	go func() {
		defer close(done)
		host.run(context.Background(), nil)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("host sync should return at once for a platform with no lock of its own")
	}
}

type idleHarness struct {
	lock  *kiosk.Service
	clock *clockwork.FakeClock
	pl    *inputPlatform
	stops chan struct{}
	// looked counts the times the watcher has looked at the clock, which is
	// how a test knows a tick has been dealt with before sending the next.
	looked atomic.Int64
	media  atomic.Bool
}

func startIdleReset(t *testing.T, coverage platforms.InputClass) *idleHarness {
	t.Helper()
	lock, clock := newHostLock(t)
	pl := &inputPlatform{
		MockPlatform: mocks.NewMockPlatform(), activity: make(chan struct{}), coverage: coverage,
	}
	h := &idleHarness{lock: lock, clock: clock, pl: pl, stops: make(chan struct{}, 8)}
	h.media.Store(true)
	idle := newKioskIdleReset(lock, pl, clock,
		func() bool {
			h.looked.Add(1)
			return h.media.Load()
		},
		func() { h.stops <- struct{}{} })
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		idle.run(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	return h
}

// tick advances the clock one idle tick at a time and waits for the watcher
// to deal with each before sending the next, so no tick is judged against a
// clock that has already moved on.
func (h *idleHarness) tick(t *testing.T, d time.Duration) {
	t.Helper()
	for elapsed := time.Duration(0); elapsed < d; elapsed += kioskIdleTick {
		require.NoError(t, h.clock.BlockUntilContext(t.Context(), 1))
		before := h.looked.Load()
		h.clock.Advance(kioskIdleTick)
		require.Eventually(t, func() bool { return h.looked.Load() > before },
			2*time.Second, time.Millisecond, "watcher did not deal with a tick")
	}
}

// touch reports input and waits until the watcher has taken it.
func (h *idleHarness) touch(t *testing.T) {
	t.Helper()
	select {
	case h.pl.activity <- struct{}{}:
	case <-time.After(2 * time.Second):
		t.Fatal("input watch is not running")
	}
	// A second report can only be taken once the first has been handled.
	select {
	case h.pl.activity <- struct{}{}:
	case <-time.After(2 * time.Second):
		t.Fatal("input watch stopped taking input")
	}
}

func (h *idleHarness) expectStop(t *testing.T) {
	t.Helper()
	select {
	case <-h.stops:
	case <-time.After(2 * time.Second):
		t.Fatal("idle reset did not stop the media")
	}
}

func (h *idleHarness) expectNoStop(t *testing.T) {
	t.Helper()
	select {
	case <-h.stops:
		t.Fatal("idle reset stopped the media")
	case <-time.After(50 * time.Millisecond):
	}
}

func idleOn(s *kiosk.Settings) {
	s.IdleReset = true
	s.IdleTimeout = 60
}

func TestKioskIdleResetStopsAnUntouchedDevice(t *testing.T) {
	t.Parallel()
	h := startIdleReset(t, platforms.InputAll)
	enableHostLock(t, h.lock, idleOn)

	h.tick(t, 55*time.Second)
	h.expectNoStop(t)
	h.tick(t, 10*time.Second)
	h.expectStop(t)
}

func TestKioskIdleResetWaitsWhileTheDeviceIsUsed(t *testing.T) {
	t.Parallel()
	h := startIdleReset(t, platforms.InputAll)
	enableHostLock(t, h.lock, idleOn)

	h.tick(t, 50*time.Second)
	h.touch(t)
	h.tick(t, 50*time.Second)
	h.expectNoStop(t)

	h.tick(t, 15*time.Second)
	h.expectStop(t)
}

func TestKioskIdleResetOnlyCountsWhileMediaRuns(t *testing.T) {
	t.Parallel()
	h := startIdleReset(t, platforms.InputAll)
	h.media.Store(false)
	enableHostLock(t, h.lock, idleOn)

	h.tick(t, 5*time.Minute)
	h.expectNoStop(t)

	// The wait starts when media does, not when the device was last used.
	h.media.Store(true)
	h.tick(t, 55*time.Second)
	h.expectNoStop(t)
	h.tick(t, 10*time.Second)
	h.expectStop(t)
}

func TestKioskIdleResetIsOffUnlessLockedAndTurnedOn(t *testing.T) {
	t.Parallel()

	t.Run("setting off", func(t *testing.T) {
		t.Parallel()
		h := startIdleReset(t, platforms.InputAll)
		enableHostLock(t, h.lock, nil)
		h.clock.Advance(time.Hour)
		h.expectNoStop(t)
	})

	t.Run("temporarily unlocked", func(t *testing.T) {
		t.Parallel()
		h := startIdleReset(t, platforms.InputAll)
		enableHostLock(t, h.lock, idleOn)
		h.tick(t, 10*time.Second)
		require.NoError(t, h.lock.Unlock(30*time.Minute))
		h.clock.Advance(10 * time.Minute)
		h.expectNoStop(t)
	})

	t.Run("input it cannot fully see", func(t *testing.T) {
		t.Parallel()
		// Stopping a game under someone using the one kind of input the
		// platform cannot see is worse than never resetting.
		h := startIdleReset(t, platforms.InputKeyboard)
		enableHostLock(t, h.lock, idleOn)
		h.clock.Advance(time.Hour)
		h.expectNoStop(t)
		assert.False(t, kiosk.IdleResetSupported(h.pl))
	})
}

// setupFixture is what setupKiosk needs: a platform with a data directory,
// state, a real user database with an admin and a member profile, and the
// profiles service over them.
type setupFixture struct {
	st            *state.State
	db            *database.Database
	pl            *mocks.MockPlatform
	profiles      *profiles.Service
	notifications <-chan models.Notification
	dataDir       string
}

func newSetupFixture(t *testing.T) *setupFixture {
	t.Helper()
	dataDir := t.TempDir()
	pl := mocks.NewMockPlatform()
	pl.On("Settings").Return(platforms.Settings{DataDir: dataDir, HostManagedPaths: true})
	pl.On("ID").Return("mock-platform").Maybe()

	userDB, cleanup := helpers.NewInMemoryUserDB(t)
	t.Cleanup(cleanup)
	db := &database.Database{UserDB: userDB}
	hash, err := profiles.HashPIN("4321")
	require.NoError(t, err)
	require.NoError(t, userDB.CreateProfile(&database.Profile{
		ProfileID: "admin-1", Name: "Parent", Role: profiles.ProfileRoleAdmin, SwitchID: "a-b-c", PINHash: hash,
	}))
	require.NoError(t, userDB.CreateProfile(&database.Profile{
		ProfileID: "kid-1", Name: "Kid", Role: profiles.ProfileRoleMember, SwitchID: "d-e-f",
	}))

	st, notifications := state.NewState(pl, "boot")
	return &setupFixture{
		st: st, db: db, pl: pl, profiles: profiles.NewService(db, st),
		notifications: notifications, dataDir: dataDir,
	}
}

func (f *setupFixture) setup() *kiosk.Service {
	return setupKiosk(f.st, f.db, f.pl, f.profiles)
}

func TestSetupKioskAttachesTheLockAndWritesTheRecoveryToken(t *testing.T) {
	t.Parallel()
	f := newSetupFixture(t)
	st, dataDir := f.st, f.dataDir

	lock := f.setup()
	require.NotNil(t, lock)
	assert.Same(t, lock, st.Kiosk())
	assert.False(t, lock.Active())

	raw, err := os.ReadFile(kiosk.RecoveryTokenPath(dataDir))
	require.NoError(t, err)
	assert.True(t, lock.CheckRecoveryToken(strings.TrimSpace(string(raw))))
}

func TestSetupKioskAnnouncesEveryChange(t *testing.T) {
	t.Parallel()
	f := newSetupFixture(t)
	lock := f.setup()

	enableHostLock(t, lock, nil)
	select {
	case notif := <-f.notifications:
		assert.Equal(t, models.NotificationKioskState, notif.Method)
		assert.Contains(t, string(notif.Params), `"state":"locked"`)
		assert.Contains(t, string(notif.Params), platforms.KioskUnsealedHostMenus)
	case <-time.After(2 * time.Second):
		t.Fatal("no kiosk.state notification after locking")
	}
}

func TestSetupKioskDropsAnAdminProfileWhenTheLockEngages(t *testing.T) {
	t.Parallel()
	f := newSetupFixture(t)
	st, profilesSvc := f.st, f.profiles
	lock := f.setup()
	_, err := profilesSvc.ActivateByID("admin-1", "4321")
	require.NoError(t, err)

	enableHostLock(t, lock, nil)
	assert.Nil(t, st.ActiveProfile(), "a locked device is not left on an admin profile")

	// The same on the relock that ends a temporary unlock.
	require.NoError(t, lock.Unlock(time.Minute))
	_, err = profilesSvc.ActivateByID("admin-1", "4321")
	require.NoError(t, err)
	require.NoError(t, lock.Lock())
	assert.Nil(t, st.ActiveProfile())
}

func TestSetupKioskStartsLockedAndDropsAStoredAdminProfile(t *testing.T) {
	t.Parallel()
	f := newSetupFixture(t)
	st, db, profilesSvc := f.st, f.db, f.profiles

	// A device that was locked when it stopped, with an admin profile still
	// recorded as active.
	previous := kiosk.NewService(db, clockwork.NewFakeClock())
	enableHostLock(t, previous, nil)
	_, err := profilesSvc.ActivateByID("admin-1", "4321")
	require.NoError(t, err)

	lock := f.setup()
	assert.True(t, lock.Locked())
	assert.Nil(t, st.ActiveProfile())
}

func TestSetupKioskLeavesAMemberProfileActive(t *testing.T) {
	t.Parallel()
	f := newSetupFixture(t)
	st, profilesSvc := f.st, f.profiles
	lock := f.setup()
	_, err := profilesSvc.ActivateBySwitchID("d-e-f")
	require.NoError(t, err)

	enableHostLock(t, lock, nil)
	require.NotNil(t, st.ActiveProfile())
	assert.Equal(t, "kid-1", st.ActiveProfile().ProfileID)
}

func TestKioskIdleStopStopsTheRunningMedia(t *testing.T) {
	t.Parallel()
	pl := mocks.NewMockPlatform()
	pl.On("StopActiveLauncher", platforms.StopForMenu).Return(nil).Once()
	st, notifications := state.NewState(pl, "boot")
	t.Cleanup(func() {
		for {
			select {
			case <-notifications:
			default:
				return
			}
		}
	})

	kioskIdleStop(&ServiceContext{Platform: pl, State: st})
	pl.AssertExpectations(t)
}

func TestKioskIdleStopSurvivesAFailedStop(t *testing.T) {
	t.Parallel()
	pl := mocks.NewMockPlatform()
	pl.On("StopActiveLauncher", platforms.StopForMenu).Return(errors.New("launcher is gone")).Once()
	st, notifications := state.NewState(pl, "boot")
	t.Cleanup(func() {
		for {
			select {
			case <-notifications:
			default:
				return
			}
		}
	})

	// The idle watcher keeps running whatever the stop returned.
	kioskIdleStop(&ServiceContext{Platform: pl, State: st})
	pl.AssertExpectations(t)
}
