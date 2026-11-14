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

package bluetooth

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/bluetooth/ble"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/jonboulle/clockwork"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const eventually = 2 * time.Second

func newTestConfig(t *testing.T, enabled bool) *config.Instance {
	t.Helper()
	cfg, err := config.NewConfig(t.TempDir(), config.BaseDefaults)
	require.NoError(t, err)
	cfg.SetBLEEnabled(enabled)
	return cfg
}

// peripheralRecorder counts the peripherals handed to OnPeripheral.
type peripheralRecorder struct {
	last  atomic.Pointer[ble.Peripheral]
	calls atomic.Int32
}

func (r *peripheralRecorder) fn(p ble.Peripheral) {
	r.last.Store(&p)
	r.calls.Add(1)
}

// advance moves the fake clock past one watch interval once the loop is
// waiting on its ticker.
func advance(t *testing.T, clock *clockwork.FakeClock) {
	t.Helper()
	require.NoError(t, clock.BlockUntilContext(t.Context(), 1))
	clock.Advance(watchInterval)
}

func TestManager_DisabledNeverOpens(t *testing.T) {
	t.Parallel()

	var opens atomic.Int32
	clock := clockwork.NewFakeClock()
	m := newManagerWith(newTestConfig(t, false), clock, func(context.Context) (ble.Adapter, error) {
		opens.Add(1)
		return mocks.NewFakeAdapter(ble.RolePeripheral), nil
	})
	m.Start()
	defer m.Stop()

	advance(t, clock)
	advance(t, clock)
	assert.Equal(t, int32(0), opens.Load())
	assert.Nil(t, m.Roles())
}

func TestManager_OpensAndHandsOutPeripheral(t *testing.T) {
	t.Parallel()

	adapter := mocks.NewFakeAdapter(ble.RoleCentral, ble.RolePeripheral)
	clock := clockwork.NewFakeClock()
	m := newManagerWith(newTestConfig(t, true), clock, func(context.Context) (ble.Adapter, error) {
		return adapter, nil
	})
	var rec peripheralRecorder
	m.OnPeripheral(rec.fn)
	m.Start()

	require.Eventually(t, func() bool { return rec.calls.Load() == 1 }, eventually, 10*time.Millisecond)
	assert.Same(t, adapter.Periph, *rec.last.Load())
	assert.Equal(t, []ble.Role{ble.RoleCentral, ble.RolePeripheral}, m.Roles())

	m.Stop()
	assert.True(t, adapter.Closed(), "Stop must close the adapter")
	assert.Nil(t, m.Roles())
}

func TestManager_LateRegistrationGetsCurrentPeripheral(t *testing.T) {
	t.Parallel()

	var opens atomic.Int32
	clock := clockwork.NewFakeClock()
	m := newManagerWith(newTestConfig(t, true), clock, func(context.Context) (ble.Adapter, error) {
		opens.Add(1)
		return mocks.NewFakeAdapter(ble.RolePeripheral), nil
	})
	m.Start()
	defer m.Stop()
	require.Eventually(t, func() bool { return m.Roles() != nil }, eventually, 10*time.Millisecond)

	var rec peripheralRecorder
	unregister := m.OnPeripheral(rec.fn)
	assert.Equal(t, int32(1), rec.calls.Load())

	// Once unregistered, a re-open no longer reaches the callback.
	unregister()
	m.mu.Lock()
	m.adapter.(*mocks.FakeAdapter).MarkGone()
	m.mu.Unlock()
	advance(t, clock)
	require.Eventually(t, func() bool { return opens.Load() == 2 }, eventually, 10*time.Millisecond)
	assert.Equal(t, int32(1), rec.calls.Load())
}

func TestManager_RetriesUntilAdapterAppears(t *testing.T) {
	t.Parallel()

	adapter := mocks.NewFakeAdapter(ble.RolePeripheral)
	var opens atomic.Int32
	clock := clockwork.NewFakeClock()
	m := newManagerWith(newTestConfig(t, true), clock, func(context.Context) (ble.Adapter, error) {
		if opens.Add(1) < 3 {
			return nil, ble.ErrNoAdapter
		}
		return adapter, nil
	})
	var rec peripheralRecorder
	m.OnPeripheral(rec.fn)
	m.Start()
	defer m.Stop()

	require.Eventually(t, func() bool { return opens.Load() == 1 }, eventually, 10*time.Millisecond)
	assert.Equal(t, int32(0), rec.calls.Load())
	advance(t, clock)
	require.Eventually(t, func() bool { return opens.Load() == 2 }, eventually, 10*time.Millisecond)
	assert.Equal(t, int32(0), rec.calls.Load())
	advance(t, clock)
	require.Eventually(t, func() bool { return rec.calls.Load() == 1 }, eventually, 10*time.Millisecond)
	assert.Equal(t, int32(3), opens.Load())

	m.mu.Lock()
	reported := m.absentReported
	m.mu.Unlock()
	assert.False(t, reported, "a successful open resets the absent report")
}

func TestManager_ReopensAfterAdapterGone(t *testing.T) {
	t.Parallel()

	first := mocks.NewFakeAdapter(ble.RolePeripheral)
	second := mocks.NewFakeAdapter(ble.RolePeripheral)
	var opens atomic.Int32
	clock := clockwork.NewFakeClock()
	m := newManagerWith(newTestConfig(t, true), clock, func(context.Context) (ble.Adapter, error) {
		if opens.Add(1) == 1 {
			return first, nil
		}
		return second, nil
	})
	var rec peripheralRecorder
	m.OnPeripheral(rec.fn)
	m.Start()
	defer m.Stop()
	require.Eventually(t, func() bool { return rec.calls.Load() == 1 }, eventually, 10*time.Millisecond)

	first.MarkGone()
	advance(t, clock)
	require.Eventually(t, func() bool { return rec.calls.Load() == 2 }, eventually, 10*time.Millisecond)
	assert.True(t, first.Closed())
	assert.Same(t, second.Periph, *rec.last.Load())
}

// An adapter that had been working and then vanished, as when bluetoothd
// restarts, is reopened without waiting for the next watch interval.
func TestManager_ReopensAtOnceAfterLosingAWorkingAdapter(t *testing.T) {
	t.Parallel()

	first := mocks.NewFakeAdapter(ble.RolePeripheral)
	second := mocks.NewFakeAdapter(ble.RolePeripheral)
	var opens atomic.Int32
	clock := clockwork.NewFakeClock()
	m := newManagerWith(newTestConfig(t, true), clock, func(context.Context) (ble.Adapter, error) {
		switch opens.Add(1) {
		case 1:
			return first, nil
		case 2, 3:
			// bluetoothd is still coming back.
			return nil, ble.ErrUnavailable
		default:
			return second, nil
		}
	})
	var rec peripheralRecorder
	m.OnPeripheral(rec.fn)
	m.Start()
	defer m.Stop()
	require.Eventually(t, func() bool { return rec.calls.Load() == 1 }, eventually, 10*time.Millisecond)

	// Long enough open to count as working.
	require.NoError(t, clock.BlockUntilContext(t.Context(), 1))
	clock.Advance(stableAfter)
	require.Equal(t, int32(1), opens.Load())

	first.MarkGone()
	require.Eventually(t, func() bool { return opens.Load() >= 2 }, eventually, 10*time.Millisecond,
		"reopened without waiting for anything")
	assert.True(t, first.Closed())

	// The retries that follow come every lossRetryInterval. Three of them
	// are well inside one watch interval, so the ticker cannot be what
	// brings the adapter back.
	for range 3 {
		if rec.calls.Load() == 2 {
			break
		}
		require.NoError(t, clock.BlockUntilContext(t.Context(), 2))
		clock.Advance(lossRetryInterval)
		time.Sleep(20 * time.Millisecond)
	}
	require.Eventually(t, func() bool { return rec.calls.Load() == 2 }, eventually, 10*time.Millisecond)
	assert.Same(t, second.Periph, *rec.last.Load())
}

// An adapter that fails as soon as it is opened is not reopened in a tight
// loop: it waits for the watch interval like any other unusable adapter.
func TestManager_AdapterThatFailsAtOnceWaitsForTheInterval(t *testing.T) {
	t.Parallel()

	var opens atomic.Int32
	clock := clockwork.NewFakeClock()
	m := newManagerWith(newTestConfig(t, true), clock, func(context.Context) (ble.Adapter, error) {
		opens.Add(1)
		a := mocks.NewFakeAdapter(ble.RolePeripheral)
		a.MarkGone()
		return a, nil
	})
	m.Start()
	defer m.Stop()

	require.Eventually(t, func() bool { return opens.Load() == 1 }, eventually, 10*time.Millisecond)
	// Only the ticker is waiting; no retry timer was armed.
	require.NoError(t, clock.BlockUntilContext(t.Context(), 1))
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, int32(1), opens.Load(), "no reopen before the interval")

	clock.Advance(watchInterval)
	require.Eventually(t, func() bool { return opens.Load() == 2 }, eventually, 10*time.Millisecond)
}

// On a platform with no BlueZ the manager asks once and never again: the
// transport is on by default, and nothing there will ever answer.
func TestManager_UnsupportedPlatformIsAskedOnce(t *testing.T) {
	t.Parallel()

	var opens atomic.Int32
	clock := clockwork.NewFakeClock()
	cfg, err := config.NewConfig(t.TempDir(), config.BaseDefaults)
	require.NoError(t, err)
	require.True(t, cfg.BLEEnabled(), "the transport is on by default")
	m := newManagerWith(cfg, clock, func(context.Context) (ble.Adapter, error) {
		opens.Add(1)
		return nil, ble.ErrUnsupported
	})
	m.Start()
	defer m.Stop()

	require.Eventually(t, func() bool { return opens.Load() == 1 }, eventually, 10*time.Millisecond)
	for range 3 {
		advance(t, clock)
	}
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, int32(1), opens.Load())
}

func TestManager_ClosesWhenDisabledAtRuntime(t *testing.T) {
	t.Parallel()

	adapter := mocks.NewFakeAdapter(ble.RolePeripheral)
	cfg := newTestConfig(t, true)
	clock := clockwork.NewFakeClock()
	m := newManagerWith(cfg, clock, func(context.Context) (ble.Adapter, error) {
		return adapter, nil
	})
	m.Start()
	defer m.Stop()
	require.Eventually(t, func() bool { return m.Roles() != nil }, eventually, 10*time.Millisecond)

	cfg.SetBLEEnabled(false)
	advance(t, clock)
	require.Eventually(t, adapter.Closed, eventually, 10*time.Millisecond)
	assert.Nil(t, m.Roles())
}

func TestManager_AdapterWithoutPeripheralRoleIsClosed(t *testing.T) {
	t.Parallel()

	adapter := mocks.NewFakeAdapter(ble.RoleCentral)
	var opens atomic.Int32
	clock := clockwork.NewFakeClock()
	m := newManagerWith(newTestConfig(t, true), clock, func(context.Context) (ble.Adapter, error) {
		opens.Add(1)
		return adapter, nil
	})
	var rec peripheralRecorder
	m.OnPeripheral(rec.fn)
	m.Start()
	defer m.Stop()

	require.Eventually(t, adapter.Closed, eventually, 10*time.Millisecond)
	assert.Equal(t, int32(0), rec.calls.Load())
	assert.Nil(t, m.Roles())

	m.mu.Lock()
	reported := m.roleReported
	m.mu.Unlock()
	assert.True(t, reported)
}

func TestManager_StopBeforeStartAndTwice(t *testing.T) {
	t.Parallel()

	m := newManagerWith(newTestConfig(t, true), clockwork.NewFakeClock(), func(context.Context) (ble.Adapter, error) {
		return nil, errors.New("must not be called")
	})
	m.Stop()
	m.Start()
	m.Stop()
	m.Stop()
}

// tickUntil advances the fake clock one watch interval at a time until cond
// holds.
func tickUntil(t *testing.T, clock *clockwork.FakeClock, cond func() bool) {
	t.Helper()
	for range 20 {
		if cond() {
			return
		}
		advance(t, clock)
		time.Sleep(5 * time.Millisecond)
	}
	require.Eventually(t, cond, eventually, 10*time.Millisecond)
}

// failUntilGivenUp ticks a freshly started manager whose every attempt fails
// until it gives up. Each failure is counted before the next tick: the fake
// ticker is always armed, so ticking while a slow attempt is still running
// would queue a tick that makes one attempt too many.
func failUntilGivenUp(t *testing.T, clock *clockwork.FakeClock, m *Manager) {
	t.Helper()
	for attempt := 1; ; attempt++ {
		require.Eventually(t, func() bool { return m.failuresForTest() == attempt },
			eventually, time.Millisecond)
		if attempt == maxFailures {
			break
		}
		advance(t, clock)
	}
	require.True(t, m.givenUp())
}

func managerGaveUp(m *Manager) func() bool {
	return m.givenUp
}

func (m *Manager) failuresForTest() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.failures
}

func (m *Manager) openedAtForTest() time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.openedAt
}

// A switched-off adapter is left alone and waited for, however long it
// stays off, and never counts towards giving up.
func TestManager_PoweredOffIsWaitedForNotCountedAsFailure(t *testing.T) {
	t.Parallel()

	adapter := mocks.NewFakeAdapter(ble.RolePeripheral)
	var opens atomic.Int32
	clock := clockwork.NewFakeClock()
	m := newManagerWith(newTestConfig(t, true), clock, func(context.Context) (ble.Adapter, error) {
		if opens.Add(1) <= maxFailures+2 {
			return nil, ble.ErrPoweredOff
		}
		return adapter, nil
	})
	var rec peripheralRecorder
	m.OnPeripheral(rec.fn)
	m.Start()
	defer m.Stop()

	tickUntil(t, clock, func() bool { return rec.calls.Load() == 1 })
	assert.Equal(t, int32(maxFailures+3), opens.Load())
	assert.False(t, managerGaveUp(m)())
	assert.True(t, m.Serving())
}

// An adapter that is there and keeps failing is given up after maxFailures
// attempts. While given up, an adapter that opens is not served; one that
// goes away earns a fresh start.
func TestManager_GivesUpAfterRepeatedFailures(t *testing.T) {
	t.Parallel()

	var mode atomic.Int32 // 0 failing, 1 working, 2 absent
	adapters := make(chan *mocks.FakeAdapter, 8)
	clock := clockwork.NewFakeClock()
	var opens atomic.Int32
	m := newManagerWith(newTestConfig(t, true), clock, func(context.Context) (ble.Adapter, error) {
		opens.Add(1)
		switch mode.Load() {
		case 1:
			a := mocks.NewFakeAdapter(ble.RolePeripheral)
			adapters <- a
			return a, nil
		case 2:
			return nil, ble.ErrNoAdapter
		default:
			return nil, errors.New("controller fault")
		}
	})
	var rec peripheralRecorder
	m.OnPeripheral(rec.fn)
	m.Start()
	defer m.Stop()

	failUntilGivenUp(t, clock, m)
	assert.Equal(t, int32(maxFailures), opens.Load())

	// Given up: a working adapter is opened to look at, closed, not served.
	mode.Store(1)
	tickUntil(t, clock, func() bool { return len(adapters) >= 1 })
	seen := <-adapters
	require.Eventually(t, seen.Closed, eventually, 10*time.Millisecond)
	assert.Equal(t, int32(0), rec.calls.Load())
	assert.False(t, m.Serving())

	// The adapter goes away and comes back: a fresh start.
	mode.Store(2)
	tickUntil(t, clock, func() bool { return !managerGaveUp(m)() })
	mode.Store(1)
	tickUntil(t, clock, func() bool { return rec.calls.Load() == 1 })
	assert.True(t, m.Serving())
}

// Turning the setting off and on again also earns a fresh start.
func TestManager_GiveUpClearedByDisabling(t *testing.T) {
	t.Parallel()

	cfg := newTestConfig(t, true)
	clock := clockwork.NewFakeClock()
	m := newManagerWith(cfg, clock, func(context.Context) (ble.Adapter, error) {
		return nil, errors.New("controller fault")
	})
	m.Start()
	defer m.Stop()

	tickUntil(t, clock, managerGaveUp(m))
	cfg.SetBLEEnabled(false)
	tickUntil(t, clock, func() bool { return !managerGaveUp(m)() })
}

// An adapter that opens and drops straight away, again and again, is a
// failure too, and is given up the same way.
func TestManager_FlappingAdapterIsGivenUp(t *testing.T) {
	t.Parallel()

	clock := clockwork.NewFakeClock()
	m := newManagerWith(newTestConfig(t, true), clock, func(context.Context) (ble.Adapter, error) {
		return mocks.NewFakeAdapter(ble.RolePeripheral), nil
	})
	m.OnPeripheral(func(ble.Peripheral) {
		m.mu.Lock()
		adapter := m.adapter
		m.mu.Unlock()
		adapter.(*mocks.FakeAdapter).MarkGone()
	})
	m.Start()
	defer m.Stop()

	tickUntil(t, clock, managerGaveUp(m))
}

// An adapter that cannot be a peripheral counts towards giving up.
func TestManager_PeripheralRoleMissingIsGivenUp(t *testing.T) {
	t.Parallel()

	clock := clockwork.NewFakeClock()
	m := newManagerWith(newTestConfig(t, true), clock, func(context.Context) (ble.Adapter, error) {
		return mocks.NewFakeAdapter(ble.RoleCentral), nil
	})
	m.Start()
	defer m.Stop()

	tickUntil(t, clock, managerGaveUp(m))
	assert.False(t, m.Serving())
}

// goneOnServe builds a manager over adapters that always open and find out
// what is wrong only when they are served, as the Windows one does. The
// first failing adapters go away with cause as soon as they are handed out;
// later ones work.
func goneOnServe(
	t *testing.T, clock *clockwork.FakeClock, failing int32, cause error,
) (m *Manager, opens *atomic.Int32, rec *peripheralRecorder) {
	t.Helper()
	opens = &atomic.Int32{}
	rec = &peripheralRecorder{}
	var current atomic.Pointer[mocks.FakeAdapter]
	m = newManagerWith(newTestConfig(t, true), clock, func(context.Context) (ble.Adapter, error) {
		opens.Add(1)
		a := mocks.NewFakeAdapter(ble.RolePeripheral)
		current.Store(a)
		return a, nil
	})
	m.OnPeripheral(func(p ble.Peripheral) {
		if rec.calls.Load() < failing {
			current.Load().MarkGoneBecause(cause)
		}
		rec.fn(p)
	})
	return m, opens, rec
}

// An adapter that opens and then says it has no usable radio is waited for
// like one that is absent when opened: however often it happens it never
// counts towards giving up, it is only tried again at the watch interval,
// and the transport is served once a radio is there.
func TestManager_LostForWantOfARadioIsWaitedFor(t *testing.T) {
	t.Parallel()

	for _, cause := range []error{
		ble.ErrNoAdapter, ble.ErrUnavailable, ble.ErrPoweredOff, ble.ErrRoleUnsupported,
	} {
		t.Run(cause.Error(), func(t *testing.T) {
			t.Parallel()

			const failing = maxFailures + 3
			clock := clockwork.NewFakeClock()
			m, opens, rec := goneOnServe(t, clock, failing, fmt.Errorf("%w: create gatt service", cause))
			m.Start()
			defer m.Stop()

			require.Eventually(t, func() bool { return rec.calls.Load() == 1 && !m.Serving() },
				eventually, 10*time.Millisecond)
			// Only the ticker is waiting; no retry timer was armed.
			require.NoError(t, clock.BlockUntilContext(t.Context(), 1))
			time.Sleep(50 * time.Millisecond)
			assert.Equal(t, int32(1), opens.Load(), "no reopen before the interval")

			tickUntil(t, clock, func() bool { return rec.calls.Load() == failing+1 })
			assert.Equal(t, int32(failing+1), opens.Load(), "one attempt per watch interval")
			assert.False(t, managerGaveUp(m)())
			assert.True(t, m.Serving())
		})
	}
}

// lookCountingAdapter counts how often the manager asks for its roles. With
// a reader configured the manager asks on every look at an open adapter,
// which lets a test wait for a tick to have been handled.
type lookCountingAdapter struct {
	*mocks.FakeAdapter
	looks atomic.Int32
}

func (a *lookCountingAdapter) Roles() []ble.Role {
	a.looks.Add(1)
	return a.FakeAdapter.Roles()
}

// A working adapter whose radio is then switched off is not chased with the
// quick retries: nothing will bring the radio back but the user.
func TestManager_WorkingAdapterLostForWantOfARadioWaitsForTheInterval(t *testing.T) {
	t.Parallel()

	cfg := newTestConfig(t, true)
	withBLEReader(t, cfg)
	roles := []ble.Role{ble.RoleCentral, ble.RolePeripheral, roleCentralPeripheral}
	first := &lookCountingAdapter{FakeAdapter: mocks.NewFakeAdapter(roles...)}
	var opens atomic.Int32
	clock := clockwork.NewFakeClock()
	m := newManagerWith(cfg, clock, func(context.Context) (ble.Adapter, error) {
		if opens.Add(1) == 1 {
			return first, nil
		}
		return mocks.NewFakeAdapter(roles...), nil
	})
	var rec peripheralRecorder
	m.OnPeripheral(rec.fn)
	m.Start()
	defer m.Stop()
	require.Eventually(t, func() bool { return rec.calls.Load() == 1 }, eventually, 10*time.Millisecond)

	// Long enough open to count as working. Each tick is waited for, so
	// none is left over to reopen the adapter after it is lost.
	for clock.Since(m.openedAtForTest()) < stableAfter {
		looks := first.looks.Load()
		advance(t, clock)
		require.Eventually(t, func() bool { return first.looks.Load() > looks }, eventually, time.Millisecond)
	}
	require.Equal(t, int32(1), opens.Load())

	first.MarkGoneBecause(fmt.Errorf("%w: advertising stopped", ble.ErrUnavailable))
	require.Eventually(t, first.Closed, eventually, 10*time.Millisecond)
	// Only the ticker is waiting; no retry timer was armed.
	require.NoError(t, clock.BlockUntilContext(t.Context(), 1))
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, int32(1), opens.Load(), "no reopen before the interval")

	tickUntil(t, clock, func() bool { return rec.calls.Load() == 2 })
	assert.True(t, m.Serving())
}

// An adapter that goes away for any other reason straight after opening is
// still a failure, and is given up after maxFailures of them.
func TestManager_LostForAnotherReasonIsGivenUp(t *testing.T) {
	t.Parallel()

	clock := clockwork.NewFakeClock()
	m, opens, rec := goneOnServe(t, clock, maxFailures+3, errors.New("create gatt service: controller fault"))
	m.Start()
	defer m.Stop()

	failUntilGivenUp(t, clock, m)
	assert.Equal(t, int32(maxFailures), opens.Load())
	assert.Equal(t, int32(maxFailures), rec.calls.Load())

	// Given up: later adapters are opened to look at and not served.
	tickUntil(t, clock, func() bool { return opens.Load() >= maxFailures+2 })
	assert.Equal(t, int32(maxFailures), rec.calls.Load())
	assert.True(t, managerGaveUp(m)())
	assert.False(t, m.Serving())
}

// lockedBuffer is a log sink the watch loop can write while the test reads.
type lockedBuffer struct {
	buf bytes.Buffer
	mu  syncutil.Mutex
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p) //nolint:wrapcheck // test sink
}

func (b *lockedBuffer) count(substr string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.Count(b.buf.String(), substr)
}

// A radio that stays missing is reported once, not on every interval, and
// neither is each look that found it still missing.
//
//nolint:paralleltest // replaces the global logger
func TestManager_LostForWantOfARadioIsReportedOnce(t *testing.T) {
	var out lockedBuffer
	original := log.Logger
	log.Logger = zerolog.New(&out).Level(zerolog.InfoLevel)
	t.Cleanup(func() { log.Logger = original })

	const failing = maxFailures + 3
	clock := clockwork.NewFakeClock()
	m, _, rec := goneOnServe(t, clock, failing, fmt.Errorf("%w: create gatt service", ble.ErrNoAdapter))
	m.Start()
	defer m.Stop()

	tickUntil(t, clock, func() bool { return rec.calls.Load() == failing && !m.Serving() })
	assert.Equal(t, 1, out.count("bluetooth unavailable, will keep checking"))
	assert.Equal(t, 1, out.count("bluetooth adapter ready"))
	assert.Equal(t, 0, out.count("closing bluetooth adapter"))

	// The radio shows up and stays: that is said, and a later loss is
	// reported afresh.
	tickUntil(t, clock, func() bool { return out.count("bluetooth is usable again") == 1 })
	assert.True(t, m.Serving())
	assert.Equal(t, 1, out.count("bluetooth unavailable, will keep checking"))
}

// Pausing advertising reaches the current peripheral and every later one.
func TestManager_AdvertisingPauseFollowsPeripherals(t *testing.T) {
	t.Parallel()

	first := mocks.NewFakeAdapter(ble.RolePeripheral)
	second := mocks.NewFakeAdapter(ble.RolePeripheral)
	var opens atomic.Int32
	clock := clockwork.NewFakeClock()
	m := newManagerWith(newTestConfig(t, true), clock, func(context.Context) (ble.Adapter, error) {
		if opens.Add(1) == 1 {
			return first, nil
		}
		return second, nil
	})

	// Set before there is a peripheral, it applies to the first.
	m.SetAdvertisingPaused(true)
	var rec peripheralRecorder
	m.OnPeripheral(rec.fn)
	m.Start()
	defer m.Stop()
	require.Eventually(t, func() bool { return rec.calls.Load() == 1 }, eventually, 10*time.Millisecond)
	assert.False(t, first.Periph.Advertising())

	m.SetAdvertisingPaused(false)
	assert.True(t, first.Periph.Advertising())
	m.SetAdvertisingPaused(true)
	m.SetAdvertisingPaused(true)
	assert.False(t, first.Periph.Advertising())

	first.MarkGone()
	advance(t, clock)
	require.Eventually(t, func() bool { return rec.calls.Load() == 2 }, eventually, 10*time.Millisecond)
	assert.False(t, second.Periph.Advertising())
}

func withBLEReader(t *testing.T, cfg *config.Instance) {
	t.Helper()
	cfg.SetReaderConnections([]config.ReadersConnect{
		{Driver: "simpleserial_ble", Path: "AA:BB:CC:DD:EE:01"},
	})
}

// A configured Bluetooth reader keeps the adapter when the controller
// cannot scan and advertise at once.
func TestManager_ReaderWinsWithoutCentralPeripheral(t *testing.T) {
	t.Parallel()

	cfg := newTestConfig(t, true)
	withBLEReader(t, cfg)
	adapter := mocks.NewFakeAdapter(ble.RoleCentral, ble.RolePeripheral)
	var opens atomic.Int32
	clock := clockwork.NewFakeClock()
	m := newManagerWith(cfg, clock, func(context.Context) (ble.Adapter, error) {
		opens.Add(1)
		return adapter, nil
	})
	var rec peripheralRecorder
	m.OnPeripheral(rec.fn)
	m.Start()
	defer m.Stop()

	require.Eventually(t, func() bool { return opens.Load() == 1 }, eventually, 10*time.Millisecond)
	require.Eventually(t, adapter.Closed, eventually, 10*time.Millisecond)
	assert.Equal(t, int32(0), rec.calls.Load())
	assert.False(t, managerGaveUp(m)(), "a reader taking the adapter is not a failure")
}

// A controller that can do both serves the reader and the app.
func TestManager_ReaderAndAppShareCentralPeripheral(t *testing.T) {
	t.Parallel()

	cfg := newTestConfig(t, true)
	withBLEReader(t, cfg)
	adapter := mocks.NewFakeAdapter(ble.RoleCentral, ble.RolePeripheral, roleCentralPeripheral)
	clock := clockwork.NewFakeClock()
	m := newManagerWith(cfg, clock, func(context.Context) (ble.Adapter, error) {
		return adapter, nil
	})
	var rec peripheralRecorder
	m.OnPeripheral(rec.fn)
	m.Start()
	defer m.Stop()

	require.Eventually(t, func() bool { return rec.calls.Load() == 1 }, eventually, 10*time.Millisecond)
	assert.False(t, adapter.Closed())
}

// A disabled reader entry, or a disabled driver, does not take the adapter.
func TestManager_DisabledReaderDoesNotWin(t *testing.T) {
	t.Parallel()

	off := false
	cfg := newTestConfig(t, true)
	cfg.SetReaderConnections([]config.ReadersConnect{
		{Driver: "simpleserial_ble", Path: "AA:BB:CC:DD:EE:01", Enabled: &off},
		{Driver: "pn532", Path: "/dev/ttyUSB0"},
	})
	m := newManagerWith(cfg, clockwork.NewFakeClock(), nil)
	assert.False(t, m.readerConfigured())

	withBLEReader(t, cfg)
	assert.True(t, m.readerConfigured())
}

// A reader added while the app transport is serving takes the adapter on
// the next pass.
func TestManager_ReaderAddedLaterTakesAdapter(t *testing.T) {
	t.Parallel()

	cfg := newTestConfig(t, true)
	adapter := mocks.NewFakeAdapter(ble.RoleCentral, ble.RolePeripheral)
	clock := clockwork.NewFakeClock()
	m := newManagerWith(cfg, clock, func(context.Context) (ble.Adapter, error) {
		return adapter, nil
	})
	var rec peripheralRecorder
	m.OnPeripheral(rec.fn)
	m.Start()
	defer m.Stop()
	require.Eventually(t, func() bool { return rec.calls.Load() == 1 }, eventually, 10*time.Millisecond)

	withBLEReader(t, cfg)
	advance(t, clock)
	require.Eventually(t, adapter.Closed, eventually, 10*time.Millisecond)
	assert.False(t, m.Serving())
}

// A failure count is cleared once an adapter has stayed up.
func TestManager_StableAdapterClearsFailures(t *testing.T) {
	t.Parallel()

	adapter := mocks.NewFakeAdapter(ble.RolePeripheral)
	var opens atomic.Int32
	clock := clockwork.NewFakeClock()
	m := newManagerWith(newTestConfig(t, true), clock, func(context.Context) (ble.Adapter, error) {
		if opens.Add(1) <= maxFailures-1 {
			return nil, errors.New("controller fault")
		}
		return adapter, nil
	})
	var rec peripheralRecorder
	m.OnPeripheral(rec.fn)
	m.Start()
	defer m.Stop()
	tickUntil(t, clock, func() bool { return rec.calls.Load() == 1 })

	failures := func() int {
		m.mu.Lock()
		defer m.mu.Unlock()
		return m.failures
	}
	assert.Equal(t, maxFailures-1, failures())
	tickUntil(t, clock, func() bool { return failures() == 0 })
	assert.False(t, managerGaveUp(m)())
}

// reloadingAdapter adds key reloading to the fake adapter.
type reloadingAdapter struct {
	*mocks.FakeAdapter
	err     error
	reloads atomic.Int32
	n       int
}

func (a *reloadingAdapter) ReloadLongTermKeys(context.Context) (int, error) {
	a.reloads.Add(1)
	return a.n, a.err
}

func TestManager_ReloadsKeysOnEveryOpen(t *testing.T) {
	t.Parallel()

	adapter := &reloadingAdapter{FakeAdapter: mocks.NewFakeAdapter(ble.RoleCentral, ble.RolePeripheral), n: 2}
	clock := clockwork.NewFakeClock()
	m := newManagerWith(newTestConfig(t, true), clock, func(context.Context) (ble.Adapter, error) {
		return adapter, nil
	})
	var rec peripheralRecorder
	m.OnPeripheral(rec.fn)
	m.Start()
	defer m.Stop()

	require.Eventually(t, func() bool { return rec.calls.Load() == 1 }, eventually, 10*time.Millisecond)
	assert.Equal(t, int32(1), adapter.reloads.Load(), "keys must be loaded before the peripheral is handed out")
}

func TestManager_KeyReloadFailureDoesNotStopTheTransport(t *testing.T) {
	t.Parallel()

	for _, err := range []error{errors.New("kernel said no"), os.ErrPermission} {
		adapter := &reloadingAdapter{FakeAdapter: mocks.NewFakeAdapter(ble.RoleCentral, ble.RolePeripheral), err: err}
		clock := clockwork.NewFakeClock()
		m := newManagerWith(newTestConfig(t, true), clock, func(context.Context) (ble.Adapter, error) {
			return adapter, nil
		})
		var rec peripheralRecorder
		m.OnPeripheral(rec.fn)
		m.Start()
		require.Eventually(t, func() bool { return rec.calls.Load() == 1 }, eventually, 10*time.Millisecond)
		assert.Equal(t, int32(1), adapter.reloads.Load())
		m.Stop()
	}
}
