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
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/bluetooth/bluez"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/jonboulle/clockwork"
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
	last  atomic.Pointer[bluez.Peripheral]
	calls atomic.Int32
}

func (r *peripheralRecorder) fn(p bluez.Peripheral) {
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
	m := newManagerWith(newTestConfig(t, false), clock, func(context.Context) (bluez.Adapter, error) {
		opens.Add(1)
		return mocks.NewFakeAdapter(bluez.RolePeripheral), nil
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

	adapter := mocks.NewFakeAdapter(bluez.RoleCentral, bluez.RolePeripheral)
	clock := clockwork.NewFakeClock()
	m := newManagerWith(newTestConfig(t, true), clock, func(context.Context) (bluez.Adapter, error) {
		return adapter, nil
	})
	var rec peripheralRecorder
	m.OnPeripheral(rec.fn)
	m.Start()

	require.Eventually(t, func() bool { return rec.calls.Load() == 1 }, eventually, 10*time.Millisecond)
	assert.Same(t, adapter.Periph, *rec.last.Load())
	assert.Equal(t, []bluez.Role{bluez.RoleCentral, bluez.RolePeripheral}, m.Roles())

	m.Stop()
	assert.True(t, adapter.Closed(), "Stop must close the adapter")
	assert.Nil(t, m.Roles())
}

func TestManager_LateRegistrationGetsCurrentPeripheral(t *testing.T) {
	t.Parallel()

	var opens atomic.Int32
	clock := clockwork.NewFakeClock()
	m := newManagerWith(newTestConfig(t, true), clock, func(context.Context) (bluez.Adapter, error) {
		opens.Add(1)
		return mocks.NewFakeAdapter(bluez.RolePeripheral), nil
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

	adapter := mocks.NewFakeAdapter(bluez.RolePeripheral)
	var opens atomic.Int32
	clock := clockwork.NewFakeClock()
	m := newManagerWith(newTestConfig(t, true), clock, func(context.Context) (bluez.Adapter, error) {
		if opens.Add(1) < 3 {
			return nil, bluez.ErrNoAdapter
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
	reported := m.unavailableReported
	m.mu.Unlock()
	assert.False(t, reported, "a successful open resets the unavailable report")
}

func TestManager_ReopensAfterAdapterGone(t *testing.T) {
	t.Parallel()

	first := mocks.NewFakeAdapter(bluez.RolePeripheral)
	second := mocks.NewFakeAdapter(bluez.RolePeripheral)
	var opens atomic.Int32
	clock := clockwork.NewFakeClock()
	m := newManagerWith(newTestConfig(t, true), clock, func(context.Context) (bluez.Adapter, error) {
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

func TestManager_ClosesWhenDisabledAtRuntime(t *testing.T) {
	t.Parallel()

	adapter := mocks.NewFakeAdapter(bluez.RolePeripheral)
	cfg := newTestConfig(t, true)
	clock := clockwork.NewFakeClock()
	m := newManagerWith(cfg, clock, func(context.Context) (bluez.Adapter, error) {
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

	adapter := mocks.NewFakeAdapter(bluez.RoleCentral)
	var opens atomic.Int32
	clock := clockwork.NewFakeClock()
	m := newManagerWith(newTestConfig(t, true), clock, func(context.Context) (bluez.Adapter, error) {
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

	m := newManagerWith(newTestConfig(t, true), clockwork.NewFakeClock(), func(context.Context) (bluez.Adapter, error) {
		return nil, errors.New("must not be called")
	})
	m.Stop()
	m.Start()
	m.Stop()
	m.Stop()
}
