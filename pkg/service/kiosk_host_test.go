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
	"sync/atomic"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/kiosk"
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

func eventuallySeen(t *testing.T, pl *kioskHostPlatform, want []bool) {
	t.Helper()
	assert.Eventually(t, func() bool { return assert.ObjectsAreEqual(want, pl.seen()) },
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
	mediaStarted <- models.Notification{Method: models.NotificationStarted}
	eventuallySeen(t, pl, []bool{true, true})
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
