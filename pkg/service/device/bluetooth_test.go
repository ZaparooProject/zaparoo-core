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
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/hoststatus"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	padAddress      = "AA:BB:CC:DD:EE:FF"
	keyboardAddress = "11:22:33:44:55:66"
	pairedAddress   = "01:02:03:04:05:06"
)

// fakeBluetooth is a Bluetooth stack with one adapter.
type fakeBluetooth struct {
	readErr   error
	startErr  error
	removeErr error
	pair      func(ctx context.Context, address string) error
	onChange  func()
	trusted   []string
	connected []string
	removed   []string
	reading   hoststatus.BluetoothReading
	starts    int
	stops     int
	watching  int
	mu        syncutil.Mutex
}

func newFakeBluetooth() *fakeBluetooth {
	return &fakeBluetooth{reading: hoststatus.BluetoothReading{
		Powered: true,
		Devices: []hoststatus.BluetoothDevice{
			{Address: pairedAddress, Name: "Old Keyboard", Class: 0x002540, Paired: true, Trusted: true},
			{
				Address: padAddress, Name: "Wireless Controller", Class: 0x002508, InRange: true,
				VendorID: "054c", ProductID: "0ce6",
			},
			{Address: keyboardAddress, Name: "Keyboard", Class: 0x002540, InRange: true},
		},
	}}
}

func (f *fakeBluetooth) Read(context.Context) (hoststatus.BluetoothReading, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	reading := f.reading
	reading.Devices = append([]hoststatus.BluetoothDevice(nil), f.reading.Devices...)
	return reading, f.readErr
}

func (f *fakeBluetooth) StartDiscovery(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.startErr != nil {
		return f.startErr
	}
	f.starts++
	f.reading.Discovering = true
	return nil
}

func (f *fakeBluetooth) StopDiscovery(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stops++
	f.reading.Discovering = false
	return nil
}

func (f *fakeBluetooth) Pair(ctx context.Context, address string) error {
	f.mu.Lock()
	pair := f.pair
	f.mu.Unlock()
	if pair == nil {
		return nil
	}
	return pair(ctx, address)
}

func (f *fakeBluetooth) Trust(_ context.Context, address string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.trusted = append(f.trusted, address)
	return nil
}

func (f *fakeBluetooth) Connect(_ context.Context, address string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.connected = append(f.connected, address)
	return nil
}

func (f *fakeBluetooth) Remove(_ context.Context, address string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.removeErr != nil {
		return f.removeErr
	}
	f.removed = append(f.removed, address)
	return nil
}

func (f *fakeBluetooth) Watch(ctx context.Context, onChange func()) {
	f.mu.Lock()
	f.watching++
	f.onChange = onChange
	f.mu.Unlock()
	<-ctx.Done()
	f.mu.Lock()
	f.watching--
	f.mu.Unlock()
}

func (f *fakeBluetooth) counts() (starts, stops, watching int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.starts, f.stops, f.watching
}

func (f *fakeBluetooth) signal() {
	f.mu.Lock()
	onChange := f.onChange
	f.mu.Unlock()
	onChange()
}

func (f *fakeBluetooth) update(change func(reading *hoststatus.BluetoothReading)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	change(&f.reading)
}

// published collects notifications.
type published struct {
	items []models.Notification
	mu    syncutil.Mutex
}

func (p *published) add(n models.Notification) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.items = append(p.items, n)
}

func (p *published) all() []models.Notification {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]models.Notification(nil), p.items...)
}

func newTestBluetooth(
	t *testing.T,
	stack *fakeBluetooth,
	controllers ...hoststatus.Controller,
) (*Bluetooth, *clockwork.FakeClock, *published) {
	t.Helper()
	clock := clockwork.NewFakeClock()
	sent := &published{}
	manager := NewBluetooth(&BluetoothOptions{
		Control:     stack,
		Clock:       clock,
		Publish:     sent.add,
		Controllers: func() []hoststatus.Controller { return controllers },
	})
	t.Cleanup(manager.Close)
	return manager, clock, sent
}

func ids(state models.DeviceBluetoothState) []string {
	list := make([]string, 0, len(state.Devices))
	for _, device := range state.Devices {
		list = append(list, device.ID)
	}
	return list
}

func pairingState(t *testing.T, manager *Bluetooth) *models.DeviceBluetoothPairing {
	t.Helper()
	state, err := manager.State(context.Background())
	require.NoError(t, err)
	return state.Pairing
}

func awaitPairing(t *testing.T, manager *Bluetooth, want string) *models.DeviceBluetoothPairing {
	t.Helper()
	require.Eventually(t, func() bool {
		pairing := pairingState(t, manager)
		return pairing != nil && pairing.State == want
	}, 5*time.Second, time.Millisecond)
	return pairingState(t, manager)
}

func TestBluetooth_ListsPairedAndScannedGamepads(t *testing.T) {
	t.Parallel()

	stack := newFakeBluetooth()
	manager, _, _ := newTestBluetooth(t, stack)
	ctx := context.Background()

	state, err := manager.State(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{pairedAddress}, ids(state), "nothing is discovered before a scan")

	state, err = manager.StartScan(ctx, false, 0)
	require.NoError(t, err)
	assert.True(t, state.Scanning)
	assert.True(t, state.Powered)
	assert.Equal(t, []string{pairedAddress, padAddress}, ids(state), "paired first, and no keyboard")
	assert.True(t, state.Devices[1].Gamepad)
	assert.False(t, state.Devices[0].Gamepad)
	require.NotNil(t, state.Devices[1].DisplayName)
	assert.Equal(t, "DualSense", *state.Devices[1].DisplayName)
	require.NotNil(t, state.Devices[1].VendorID)
	assert.Equal(t, "054c", *state.Devices[1].VendorID)
	assert.Nil(t, state.Devices[0].DisplayName, "no modalias, no model name")
	assert.Nil(t, state.Devices[0].VendorID)

	state, err = manager.StopScan(ctx)
	require.NoError(t, err)
	assert.False(t, state.Scanning)
	stack.update(func(reading *hoststatus.BluetoothReading) {
		for i := range reading.Devices {
			reading.Devices[i].InRange = false
		}
	})
	state, err = manager.State(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{pairedAddress, padAddress}, ids(state), "the last scan's finds stay listed")

	state, err = manager.StartScan(ctx, true, 0)
	require.NoError(t, err)
	assert.Equal(t, []string{pairedAddress}, ids(state), "a new scan starts from nothing")
	stack.update(func(reading *hoststatus.BluetoothReading) {
		for i := range reading.Devices {
			reading.Devices[i].InRange = true
		}
	})
	state, err = manager.State(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{pairedAddress, keyboardAddress, padAddress}, ids(state), "all lists every device")

	starts, stops, _ := stack.counts()
	assert.Equal(t, 2, starts)
	assert.Equal(t, 1, stops)
}

func TestBluetooth_ScanStopsByItself(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		timeout time.Duration
		wait    time.Duration
	}{
		{name: "default", wait: BluetoothScanDefault},
		{name: "requested", timeout: 10 * time.Second, wait: 10 * time.Second},
		{name: "capped", timeout: time.Hour, wait: BluetoothScanMax},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stack := newFakeBluetooth()
			manager, clock, _ := newTestBluetooth(t, stack)

			_, err := manager.StartScan(context.Background(), false, tt.timeout)
			require.NoError(t, err)
			clock.Advance(tt.wait - time.Millisecond)
			_, stops, _ := stack.counts()
			assert.Zero(t, stops)

			clock.Advance(time.Millisecond)
			require.Eventually(t, func() bool {
				_, stops, _ := stack.counts()
				return stops == 1
			}, 5*time.Second, time.Millisecond)
			state, err := manager.State(context.Background())
			require.NoError(t, err)
			assert.False(t, state.Scanning)
		})
	}
}

func TestBluetooth_ScanEndedByTheStack(t *testing.T) {
	t.Parallel()

	stack := newFakeBluetooth()
	manager, clock, sent := newTestBluetooth(t, stack)
	manager.ClientConnected()
	require.Eventually(t, func() bool {
		_, _, watching := stack.counts()
		return watching == 1
	}, 5*time.Second, time.Millisecond)

	_, err := manager.StartScan(context.Background(), false, 0)
	require.NoError(t, err)

	// The stack stops discovering with no word from Core.
	stack.update(func(reading *hoststatus.BluetoothReading) { reading.Discovering = false })
	stack.signal()
	clock.Advance(bluetoothDebounce)
	require.Eventually(t, func() bool {
		items := sent.all()
		if len(items) == 0 {
			return false
		}
		var state models.DeviceBluetoothState
		require.NoError(t, json.Unmarshal(items[len(items)-1].Params, &state))
		return !state.Scanning
	}, 5*time.Second, time.Millisecond, "clients hear the scan is over")

	// The scan's own end no longer fires, and asking again starts afresh.
	clock.Advance(BluetoothScanDefault)
	_, stops, _ := stack.counts()
	assert.Zero(t, stops)
	state, err := manager.StartScan(context.Background(), false, 0)
	require.NoError(t, err)
	assert.True(t, state.Scanning)
	starts, _, _ := stack.counts()
	assert.Equal(t, 2, starts)
}

func TestBluetooth_StartingAgainExtendsTheScan(t *testing.T) {
	t.Parallel()

	stack := newFakeBluetooth()
	manager, clock, _ := newTestBluetooth(t, stack)
	ctx := context.Background()

	_, err := manager.StartScan(ctx, false, 10*time.Second)
	require.NoError(t, err)
	// The pad goes quiet, so only the scan's own memory keeps it listed.
	stack.update(func(reading *hoststatus.BluetoothReading) {
		for i := range reading.Devices {
			reading.Devices[i].InRange = false
		}
	})
	clock.Advance(8 * time.Second)

	state, err := manager.StartScan(ctx, false, 10*time.Second)
	require.NoError(t, err)
	assert.True(t, state.Scanning)
	assert.Equal(t, []string{pairedAddress, padAddress}, ids(state), "extending keeps what was found")

	// The first timeout would have fired here; the new one runs from the
	// second call.
	clock.Advance(9 * time.Second)
	_, stops, _ := stack.counts()
	assert.Zero(t, stops)
	state, err = manager.State(ctx)
	require.NoError(t, err)
	assert.True(t, state.Scanning)

	clock.Advance(time.Second)
	require.Eventually(t, func() bool {
		_, stops, _ := stack.counts()
		return stops == 1
	}, 5*time.Second, time.Millisecond)
	state, err = manager.State(ctx)
	require.NoError(t, err)
	assert.False(t, state.Scanning)
	assert.Equal(t, []string{pairedAddress, padAddress}, ids(state), "the finds outlive the scan")
}

func TestBluetooth_ScanRefusedWhenPoweredOff(t *testing.T) {
	t.Parallel()

	stack := newFakeBluetooth()
	stack.startErr = hoststatus.ErrBluetoothNotPowered
	manager, _, _ := newTestBluetooth(t, stack)

	_, err := manager.StartScan(context.Background(), false, 0)
	require.ErrorIs(t, err, hoststatus.ErrBluetoothNotPowered)
	state, err := manager.State(context.Background())
	require.NoError(t, err)
	assert.False(t, state.Scanning)
}

func TestBluetooth_LastClientLeavingStopsTheScan(t *testing.T) {
	t.Parallel()

	stack := newFakeBluetooth()
	manager, _, _ := newTestBluetooth(t, stack)

	manager.ClientConnected()
	manager.ClientConnected()
	require.Eventually(t, func() bool {
		_, _, watching := stack.counts()
		return watching == 1
	}, 5*time.Second, time.Millisecond, "one watch for every client")

	_, err := manager.StartScan(context.Background(), false, 0)
	require.NoError(t, err)

	manager.ClientDisconnected()
	_, stops, _ := stack.counts()
	assert.Zero(t, stops, "a client is still there")

	manager.ClientDisconnected()
	require.Eventually(t, func() bool {
		_, stops, watching := stack.counts()
		return stops == 1 && watching == 0
	}, 5*time.Second, time.Millisecond)
	state, err := manager.State(context.Background())
	require.NoError(t, err)
	assert.False(t, state.Scanning)
}

func TestBluetooth_CloseStopsTheScan(t *testing.T) {
	t.Parallel()

	stack := newFakeBluetooth()
	manager, _, _ := newTestBluetooth(t, stack)
	_, err := manager.StartScan(context.Background(), false, 0)
	require.NoError(t, err)

	manager.Close()
	_, stops, _ := stack.counts()
	assert.Equal(t, 1, stops)
}

func TestBluetooth_PairSuccess(t *testing.T) {
	t.Parallel()

	stack := newFakeBluetooth()
	release := make(chan struct{})
	stack.pair = func(ctx context.Context, address string) error {
		assert.Equal(t, padAddress, address)
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	manager, _, _ := newTestBluetooth(t, stack)
	_, err := manager.StartScan(context.Background(), false, 0)
	require.NoError(t, err)

	state, err := manager.Pair(context.Background(), padAddress)
	require.NoError(t, err)
	require.NotNil(t, state.Pairing)
	want := models.DeviceBluetoothPairing{ID: padAddress, State: models.BluetoothPairingPairing, Attempt: 1}
	assert.Equal(t, want, *state.Pairing)
	assert.False(t, state.Scanning, "the scan stops so the pairing has the radio")

	close(release)
	pairing := awaitPairing(t, manager, models.BluetoothPairingPaired)
	assert.Nil(t, pairing.Error)
	stack.mu.Lock()
	assert.Equal(t, []string{padAddress}, stack.trusted)
	assert.Equal(t, []string{padAddress}, stack.connected)
	stack.mu.Unlock()
}

func TestBluetooth_PairFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		err  error
		want string
	}{
		{err: hoststatus.ErrBluetoothPinRequired, want: models.BluetoothErrorPinRequired},
		{err: hoststatus.ErrBluetoothRejected, want: models.BluetoothErrorRejected},
		{err: hoststatus.ErrBluetoothNotFound, want: models.BluetoothErrorNotFound},
		{err: hoststatus.ErrBluetoothTimeout, want: models.BluetoothErrorTimeout},
		{err: hoststatus.ErrBluetoothNotPowered, want: models.BluetoothErrorNotPowered},
		{err: errors.New("org.bluez.Error.Failed"), want: models.BluetoothErrorFailed},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			t.Parallel()
			stack := newFakeBluetooth()
			stack.pair = func(context.Context, string) error { return tt.err }
			manager, _, _ := newTestBluetooth(t, stack)

			_, err := manager.Pair(context.Background(), padAddress)
			require.NoError(t, err)
			pairing := awaitPairing(t, manager, models.BluetoothPairingFailed)
			require.NotNil(t, pairing.Error)
			assert.Equal(t, tt.want, *pairing.Error)
			stack.mu.Lock()
			assert.Empty(t, stack.trusted, "a failed pairing is not trusted")
			stack.mu.Unlock()
		})
	}
}

func TestBluetooth_PairTimesOut(t *testing.T) {
	t.Parallel()

	stack := newFakeBluetooth()
	stack.pair = func(ctx context.Context, _ string) error {
		<-ctx.Done()
		return ctx.Err()
	}
	manager, clock, _ := newTestBluetooth(t, stack)

	_, err := manager.Pair(context.Background(), padAddress)
	require.NoError(t, err)
	clock.Advance(bluetoothPairTimeout)
	pairing := awaitPairing(t, manager, models.BluetoothPairingFailed)
	assert.Equal(t, models.BluetoothErrorTimeout, *pairing.Error)
}

func TestBluetooth_PairRefusals(t *testing.T) {
	t.Parallel()

	t.Run("powered off", func(t *testing.T) {
		t.Parallel()
		stack := newFakeBluetooth()
		stack.reading.Powered = false
		called := false
		stack.pair = func(context.Context, string) error { called = true; return nil }
		manager, _, _ := newTestBluetooth(t, stack)
		_, err := manager.Pair(context.Background(), padAddress)
		require.ErrorIs(t, err, hoststatus.ErrBluetoothNotPowered)
		assert.False(t, called)
		assert.Nil(t, pairingState(t, manager))
	})
	t.Run("one attempt at a time", func(t *testing.T) {
		t.Parallel()
		stack := newFakeBluetooth()
		stack.pair = func(ctx context.Context, _ string) error {
			<-ctx.Done()
			return ctx.Err()
		}
		manager, _, _ := newTestBluetooth(t, stack)
		_, err := manager.Pair(context.Background(), padAddress)
		require.NoError(t, err)
		_, err = manager.Pair(context.Background(), keyboardAddress)
		require.ErrorIs(t, err, ErrBluetoothBusy)
	})
	t.Run("no adapter", func(t *testing.T) {
		t.Parallel()
		stack := newFakeBluetooth()
		stack.readErr = hoststatus.ErrBluetoothNoAdapter
		manager, _, _ := newTestBluetooth(t, stack)
		_, err := manager.Pair(context.Background(), padAddress)
		require.ErrorIs(t, err, hoststatus.ErrBluetoothNoAdapter)
	})
}

func TestBluetooth_Forget(t *testing.T) {
	t.Parallel()

	stack := newFakeBluetooth()
	manager, _, _ := newTestBluetooth(t, stack)
	_, err := manager.Forget(context.Background(), pairedAddress)
	require.NoError(t, err)
	stack.mu.Lock()
	assert.Equal(t, []string{pairedAddress}, stack.removed)
	stack.mu.Unlock()

	stack.removeErr = hoststatus.ErrBluetoothNotFound
	_, err = manager.Forget(context.Background(), keyboardAddress)
	require.ErrorIs(t, err, hoststatus.ErrBluetoothNotFound)
}

func TestBluetooth_ControllerID(t *testing.T) {
	t.Parallel()

	stack := newFakeBluetooth()
	stack.reading.Devices[0].Connected = true
	manager, _, _ := newTestBluetooth(t, stack,
		hoststatus.Controller{ID: "input17", Address: "01:02:03:04:05:06"},
		hoststatus.Controller{ID: "input3", Address: "aa:bb:cc:dd:ee:ff"},
	)
	_, err := manager.StartScan(context.Background(), false, 0)
	require.NoError(t, err)
	state, err := manager.State(context.Background())
	require.NoError(t, err)
	require.Len(t, state.Devices, 2)
	require.NotNil(t, state.Devices[0].ControllerID)
	assert.Equal(t, "input17", *state.Devices[0].ControllerID)
	assert.Nil(t, state.Devices[1].ControllerID, "a device that is not connected has no controller")
}

func TestBluetooth_NotificationsAreDebounced(t *testing.T) {
	t.Parallel()

	stack := newFakeBluetooth()
	manager, clock, sent := newTestBluetooth(t, stack)

	// Nobody listening: nothing is sent.
	_, err := manager.StartScan(context.Background(), false, 0)
	require.NoError(t, err)
	clock.Advance(time.Second)
	assert.Empty(t, sent.all())

	manager.ClientConnected()
	require.Eventually(t, func() bool {
		_, _, watching := stack.counts()
		return watching == 1
	}, 5*time.Second, time.Millisecond)

	for range 5 {
		stack.signal()
	}
	clock.Advance(bluetoothDebounce - time.Millisecond)
	assert.Empty(t, sent.all())
	clock.Advance(time.Millisecond)
	require.Eventually(t, func() bool { return len(sent.all()) == 1 }, 5*time.Second, time.Millisecond)

	notification := sent.all()[0]
	assert.Equal(t, models.NotificationDeviceBluetooth, notification.Method)
	var state models.DeviceBluetoothState
	require.NoError(t, json.Unmarshal(notification.Params, &state))
	assert.True(t, state.Scanning)
	assert.Equal(t, []string{pairedAddress, padAddress}, ids(state))

	// A signal that changes nothing a client sees is not sent again.
	stack.signal()
	clock.Advance(bluetoothDebounce)
	assert.Never(t, func() bool { return len(sent.all()) > 1 }, 50*time.Millisecond, 5*time.Millisecond)

	stack.update(func(reading *hoststatus.BluetoothReading) { reading.Devices[1].Name = "DualSense" })
	stack.signal()
	clock.Advance(bluetoothDebounce)
	require.Eventually(t, func() bool { return len(sent.all()) == 2 }, 5*time.Second, time.Millisecond)
}

func TestBluetooth_PairingOutcomeIsAnnounced(t *testing.T) {
	t.Parallel()

	stack := newFakeBluetooth()
	stack.pair = func(context.Context, string) error { return hoststatus.ErrBluetoothPinRequired }
	manager, clock, sent := newTestBluetooth(t, stack)
	manager.ClientConnected()

	_, err := manager.Pair(context.Background(), keyboardAddress)
	require.NoError(t, err)
	awaitPairing(t, manager, models.BluetoothPairingFailed)
	clock.Advance(bluetoothDebounce)

	require.Eventually(t, func() bool {
		for _, notification := range sent.all() {
			var state models.DeviceBluetoothState
			require.NoError(t, json.Unmarshal(notification.Params, &state))
			if state.Pairing != nil && state.Pairing.State == models.BluetoothPairingFailed {
				assert.Equal(t, models.BluetoothErrorPinRequired, *state.Pairing.Error)
				assert.Contains(t, ids(state), keyboardAddress, "the device being paired stays listed")
				return true
			}
		}
		clock.Advance(bluetoothDebounce)
		return false
	}, 5*time.Second, time.Millisecond)
}

func TestBluetooth_RepeatFailureIsAnnouncedAgain(t *testing.T) {
	t.Parallel()

	stack := newFakeBluetooth()
	stack.pair = func(context.Context, string) error { return hoststatus.ErrBluetoothNotFound }
	manager, clock, sent := newTestBluetooth(t, stack)
	manager.ClientConnected()

	announced := func(attempt int) bool {
		for _, notification := range sent.all() {
			var state models.DeviceBluetoothState
			require.NoError(t, json.Unmarshal(notification.Params, &state))
			if state.Pairing != nil && state.Pairing.Attempt == attempt &&
				state.Pairing.State == models.BluetoothPairingFailed {
				return true
			}
		}
		clock.Advance(bluetoothDebounce)
		return false
	}
	for attempt := 1; attempt <= 2; attempt++ {
		reply, err := manager.Pair(context.Background(), padAddress)
		require.NoError(t, err)
		require.NotNil(t, reply.Pairing)
		assert.Equal(t, attempt, reply.Pairing.Attempt, "the reply names its attempt")
		awaitPairing(t, manager, models.BluetoothPairingFailed)
		require.Eventually(t, func() bool { return announced(attempt) }, 5*time.Second, time.Millisecond,
			"an outcome identical to the last one but for its attempt is still sent")
	}
}

func TestMonitor_DrivesBluetooth(t *testing.T) {
	t.Parallel()

	stack := newFakeBluetooth()
	stack.reading.Devices[0].Connected = true
	monitor := NewMonitor(&Options{
		Platform: testPlatform(platforms.Settings{}),
		Clock:    clockwork.NewFakeClock(),
		Readers: hoststatus.Readers{
			Controllers: func() ([]hoststatus.Controller, error) {
				return []hoststatus.Controller{{ID: "input17", Address: "01:02:03:04:05:06"}}, nil
			},
			BluetoothControl: stack,
		},
	})
	require.NotNil(t, monitor.Bluetooth())
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		monitor.Run(ctx)
	}()

	monitor.ClientConnected()
	require.Eventually(t, func() bool {
		_, _, watching := stack.counts()
		return watching == 1
	}, testWait, time.Millisecond)

	monitor.Snapshot(context.Background())
	state, err := monitor.Bluetooth().StartScan(context.Background(), false, 0)
	require.NoError(t, err)
	require.NotNil(t, state.Devices[0].ControllerID, "matched against the monitor's controllers")
	assert.Equal(t, "input17", *state.Devices[0].ControllerID)

	monitor.ClientDisconnected()
	require.Eventually(t, func() bool {
		_, stops, watching := stack.counts()
		return stops == 1 && watching == 0
	}, testWait, time.Millisecond)

	_, err = monitor.Bluetooth().StartScan(context.Background(), false, 0)
	require.NoError(t, err)
	cancel()
	<-done
	_, stops, _ := stack.counts()
	assert.Equal(t, 2, stops, "stopping Core stops the scan")
}

func TestMonitor_NoBluetoothControl(t *testing.T) {
	t.Parallel()

	monitor := NewMonitor(&Options{Platform: testPlatform(platforms.Settings{})})
	assert.Nil(t, monitor.Bluetooth())
}
