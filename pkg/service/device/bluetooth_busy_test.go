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

package device

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/hoststatus"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// busyLog records what OnBusy was told.
type busyLog struct {
	calls []bool
	mu    syncutil.Mutex
}

func (l *busyLog) add(busy bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls = append(l.calls, busy)
}

func (l *busyLog) all() []bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]bool(nil), l.calls...)
}

func newBusyBluetooth(t *testing.T, stack *fakeBluetooth) (*Bluetooth, *clockwork.FakeClock, *busyLog) {
	t.Helper()
	clock := clockwork.NewFakeClock()
	busy := &busyLog{}
	manager := NewBluetooth(&BluetoothOptions{
		Control:     stack,
		Clock:       clock,
		Publish:     func(models.Notification) {},
		Controllers: func() []hoststatus.Controller { return nil },
		OnBusy:      busy.add,
	})
	t.Cleanup(manager.Close)
	return manager, clock, busy
}

func TestBluetooth_BusyFollowsTheScan(t *testing.T) {
	t.Parallel()

	stack := newFakeBluetooth()
	manager, clock, busy := newBusyBluetooth(t, stack)

	_, err := manager.StartScan(context.Background(), false, 0)
	require.NoError(t, err)
	assert.Equal(t, []bool{true}, busy.all())

	_, err = manager.StartScan(context.Background(), false, 0)
	require.NoError(t, err)
	assert.Equal(t, []bool{true}, busy.all(), "extending the scan is not a new busy period")

	clock.Advance(BluetoothScanDefault)
	require.Eventually(t, func() bool {
		return len(busy.all()) == 2
	}, 5*time.Second, time.Millisecond)
	assert.Equal(t, []bool{true, false}, busy.all())
}

func TestBluetooth_BusyEndsWhenTheScanCannotStart(t *testing.T) {
	t.Parallel()

	stack := newFakeBluetooth()
	stack.startErr = errors.New("busy")
	manager, _, busy := newBusyBluetooth(t, stack)

	_, err := manager.StartScan(context.Background(), false, 0)
	require.Error(t, err)
	assert.Equal(t, []bool{true, false}, busy.all())
}

func TestBluetooth_BusyThroughScanThenPair(t *testing.T) {
	t.Parallel()

	stack := newFakeBluetooth()
	release := make(chan struct{})
	stack.pair = func(ctx context.Context, _ string) error {
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	manager, _, busy := newBusyBluetooth(t, stack)

	_, err := manager.StartScan(context.Background(), false, 0)
	require.NoError(t, err)
	state, err := manager.Pair(context.Background(), padAddress)
	require.NoError(t, err)
	require.False(t, state.Scanning)
	assert.Equal(t, []bool{true}, busy.all(), "stopping the scan for the pairing does not release the radio")

	close(release)
	awaitPairing(t, manager, models.BluetoothPairingPaired)
	require.Eventually(t, func() bool {
		return len(busy.all()) == 2
	}, 5*time.Second, time.Millisecond)
	assert.Equal(t, []bool{true, false}, busy.all())
}

func TestBluetooth_BusyEndsOnClose(t *testing.T) {
	t.Parallel()

	stack := newFakeBluetooth()
	manager, _, busy := newBusyBluetooth(t, stack)

	_, err := manager.StartScan(context.Background(), false, 0)
	require.NoError(t, err)
	manager.Close()
	assert.Equal(t, []bool{true, false}, busy.all())
}
