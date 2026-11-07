//go:build linux && !android

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

package hoststatus

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/godbus/dbus/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testAdapter = dbus.ObjectPath("/org/bluez/hci0")
	testPad     = dbus.ObjectPath("/org/bluez/hci0/dev_AA_BB_CC_DD_EE_FF")
	testPadAddr = "AA:BB:CC:DD:EE:FF"
)

// fakeBluez is bluetoothd on a bus: it answers from a table of objects and
// records every call as "path method".
type fakeBluez struct {
	objects bluezObjects
	errs    map[string]error
	onPair  func(ctx context.Context, agent *pairingAgent) error
	agent   *pairingAgent
	signals chan *dbus.Signal
	args    map[string][]any
	limits  map[string]time.Duration
	calls   []string
	subs    int
	mu      syncutil.Mutex
}

func newFakeBluez(powered bool) *fakeBluez {
	return &fakeBluez{
		objects: bluezObjects{
			testAdapter: {bluezAdapter: {
				"Powered":     dbus.MakeVariant(powered),
				"Discovering": dbus.MakeVariant(false),
			}},
			testPad: {bluezDevice: {
				"Address":    dbus.MakeVariant("aa:bb:cc:dd:ee:ff"),
				"Alias":      dbus.MakeVariant("Wireless Controller"),
				"Adapter":    dbus.MakeVariant(testAdapter),
				"Class":      dbus.MakeVariant(uint32(0x002508)),
				"Paired":     dbus.MakeVariant(false),
				"Connected":  dbus.MakeVariant(false),
				"Trusted":    dbus.MakeVariant(false),
				"RSSI":       dbus.MakeVariant(int16(-50)),
				"UUIDs":      dbus.MakeVariant([]string{"00001124-0000-1000-8000-00805f9b34fb"}),
				"Appearance": dbus.MakeVariant(uint16(0)),
				"Modalias":   dbus.MakeVariant("usb:v054Cp09CCd0100"),
			}},
		},
		errs:    map[string]error{},
		args:    map[string][]any{},
		limits:  map[string]time.Duration{},
		signals: make(chan *dbus.Signal, 8),
	}
}

func (f *fakeBluez) call(ctx context.Context, path dbus.ObjectPath, method string, args ...any) ([]any, error) {
	f.mu.Lock()
	f.calls = append(f.calls, string(path)+" "+method)
	f.args[method] = args
	if deadline, ok := ctx.Deadline(); ok {
		f.limits[method] = time.Until(deadline)
	}
	err := f.errs[method]
	agent := f.agent
	onPair := f.onPair
	f.mu.Unlock()
	if err != nil {
		return nil, err
	}
	switch method {
	case bluezGetManagedObjects:
		return []any{f.objects}, nil
	case bluezPair:
		if onPair != nil {
			return nil, onPair(ctx, agent)
		}
	}
	return nil, nil
}

func (f *fakeBluez) export(v any, _ dbus.ObjectPath, _ string) error {
	agent, ok := v.(*pairingAgent)
	if !ok {
		return errors.New("not an agent")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.agent = agent
	f.calls = append(f.calls, "export")
	return nil
}

func (f *fakeBluez) unexport(dbus.ObjectPath, string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.agent = nil
	f.calls = append(f.calls, "unexport")
}

func (f *fakeBluez) subscribe() (signals <-chan *dbus.Signal, stop func(), err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.subs++
	return f.signals, func() {}, nil
}

func (f *fakeBluez) recorded() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func dbusErr(name string) error { return dbus.Error{Name: name} }

func TestBlueZ_Read(t *testing.T) {
	t.Parallel()

	bus := newFakeBluez(true)
	bus.objects["/org/bluez/hci0/dev_11_22_33_44_55_66"] = map[string]map[string]dbus.Variant{bluezDevice: {
		"Address": dbus.MakeVariant("11:22:33:44:55:66"),
		"Alias":   dbus.MakeVariant("11-22-33-44-55-66"),
		"Adapter": dbus.MakeVariant(testAdapter),
		"Paired":  dbus.MakeVariant(true),
	}}
	bus.objects["/org/bluez/hci1/dev_99_99_99_99_99_99"] = map[string]map[string]dbus.Variant{bluezDevice: {
		"Address": dbus.MakeVariant("99:99:99:99:99:99"),
		"Adapter": dbus.MakeVariant(dbus.ObjectPath("/org/bluez/hci1")),
	}}
	controller := &BlueZ{bus: bus}

	reading, err := controller.Read(context.Background())
	require.NoError(t, err)
	assert.True(t, reading.Powered)
	require.Len(t, reading.Devices, 2, "a device on another adapter is left out")
	assert.Equal(t, BluetoothDevice{
		Address: "11:22:33:44:55:66", Paired: true,
	}, reading.Devices[0], "an alias made up from the address is not a name")
	pad := reading.Devices[1]
	assert.Equal(t, testPadAddr, pad.Address)
	assert.Equal(t, "Wireless Controller", pad.Name)
	assert.Equal(t, "054c", pad.VendorID)
	assert.Equal(t, "09cc", pad.ProductID)
	assert.True(t, pad.InRange)
	assert.True(t, IsBluetoothGamepad(&pad))
}

func TestBlueZ_ReadErrors(t *testing.T) {
	t.Parallel()

	t.Run("bluetoothd not running", func(t *testing.T) {
		t.Parallel()
		bus := newFakeBluez(true)
		bus.errs[bluezGetManagedObjects] = dbusErr(dbusErrServiceUnknown)
		_, err := (&BlueZ{bus: bus}).Read(context.Background())
		require.ErrorIs(t, err, ErrUnsupported)
	})
	t.Run("no adapter", func(t *testing.T) {
		t.Parallel()
		bus := newFakeBluez(true)
		delete(bus.objects, testAdapter)
		_, err := (&BlueZ{bus: bus}).Read(context.Background())
		require.ErrorIs(t, err, ErrBluetoothNoAdapter)
	})
}

func TestBlueZ_Discovery(t *testing.T) {
	t.Parallel()

	t.Run("filter then start, then stop", func(t *testing.T) {
		t.Parallel()
		bus := newFakeBluez(true)
		controller := &BlueZ{bus: bus}
		require.NoError(t, controller.StartDiscovery(context.Background()))
		require.NoError(t, controller.StopDiscovery(context.Background()))
		assert.Equal(t, []string{
			"/ " + bluezGetManagedObjects,
			string(testAdapter) + " " + bluezSetDiscoveryFilter,
			string(testAdapter) + " " + bluezStartDiscovery,
			"/ " + bluezGetManagedObjects,
			string(testAdapter) + " " + bluezStopDiscovery,
		}, bus.recorded())
	})
	t.Run("powered off is refused, not switched on", func(t *testing.T) {
		t.Parallel()
		bus := newFakeBluez(false)
		err := (&BlueZ{bus: bus}).StartDiscovery(context.Background())
		require.ErrorIs(t, err, ErrBluetoothNotPowered)
		assert.NotContains(t, bus.recorded(), string(testAdapter)+" "+bluezStartDiscovery)
	})
	t.Run("a session already running is fine", func(t *testing.T) {
		t.Parallel()
		bus := newFakeBluez(true)
		bus.errs[bluezStartDiscovery] = dbusErr(bluezErrInProgress)
		require.NoError(t, (&BlueZ{bus: bus}).StartDiscovery(context.Background()))
	})
	t.Run("stopping with no session is fine", func(t *testing.T) {
		t.Parallel()
		bus := newFakeBluez(true)
		bus.errs[bluezStopDiscovery] = dbusErr("org.bluez.Error.Failed")
		require.NoError(t, (&BlueZ{bus: bus}).StopDiscovery(context.Background()))
	})
}

func TestBlueZ_PairRegistersAgentForTheAttemptOnly(t *testing.T) {
	t.Parallel()

	bus := newFakeBluez(true)
	var agentDuringPair *pairingAgent
	bus.onPair = func(_ context.Context, agent *pairingAgent) error {
		agentDuringPair = agent
		return nil
	}
	controller := &BlueZ{bus: bus}

	require.NoError(t, controller.Pair(context.Background(), testPadAddr))
	require.NotNil(t, agentDuringPair, "the agent is exported while pairing")
	assert.Equal(t, []string{
		"/ " + bluezGetManagedObjects,
		"export",
		string(bluezRoot) + " " + bluezRegisterAgent,
		string(bluezRoot) + " " + bluezRequestDefaultAgent,
		string(testPad) + " " + bluezPair,
		string(bluezRoot) + " " + bluezUnregisterAgent,
		"unexport",
	}, bus.recorded())
	assert.Equal(t, []any{bluezAgentPath, "NoInputNoOutput"}, bus.args[bluezRegisterAgent])

	// Just Works is accepted for the device being paired and nothing else.
	assert.Nil(t, agentDuringPair.RequestConfirmation(testPad, 123456))
	assert.Nil(t, agentDuringPair.RequestAuthorization(testPad))
	assert.Nil(t, agentDuringPair.AuthorizeService(testPad, "00001124-0000-1000-8000-00805f9b34fb"))
	assert.NotNil(t, agentDuringPair.RequestAuthorization("/org/bluez/hci0/dev_11_22_33_44_55_66"))
}

func TestBlueZ_PairPinRequired(t *testing.T) {
	t.Parallel()

	for _, ask := range []struct {
		ask  func(agent *pairingAgent) *dbus.Error
		name string
	}{
		{name: "PIN code", ask: func(a *pairingAgent) *dbus.Error { _, err := a.RequestPinCode(testPad); return err }},
		{name: "passkey", ask: func(a *pairingAgent) *dbus.Error { _, err := a.RequestPasskey(testPad); return err }},
		{name: "displayed passkey", ask: func(a *pairingAgent) *dbus.Error { return a.DisplayPasskey(testPad, 1, 0) }},
		{name: "displayed PIN", ask: func(a *pairingAgent) *dbus.Error { return a.DisplayPinCode(testPad, "0000") }},
	} {
		t.Run(ask.name, func(t *testing.T) {
			t.Parallel()
			bus := newFakeBluez(true)
			bus.onPair = func(ctx context.Context, agent *pairingAgent) error {
				assert.NotNil(t, ask.ask(agent), "the request is refused")
				// The attempt ends at once instead of waiting on the device.
				<-ctx.Done()
				return ctx.Err()
			}
			err := (&BlueZ{bus: bus}).Pair(context.Background(), testPadAddr)
			require.ErrorIs(t, err, ErrBluetoothPinRequired)
			calls := bus.recorded()
			assert.Contains(t, calls, string(testPad)+" "+bluezCancelPairing)
			assert.Contains(t, calls, string(bluezRoot)+" "+bluezUnregisterAgent)
			assert.Equal(t, "unexport", calls[len(calls)-1])
		})
	}
}

func TestBlueZ_PairFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		err  error
		want error
		name string
	}{
		{name: "rejected", err: dbusErr(bluezErrAuthRejected), want: ErrBluetoothRejected},
		{name: "canceled by the device", err: dbusErr(bluezErrAuthCanceled), want: ErrBluetoothRejected},
		{name: "no answer", err: dbusErr(bluezErrConnectionFailed), want: ErrBluetoothTimeout},
		{name: "gone", err: dbusErr(bluezErrDoesNotExist), want: ErrBluetoothNotFound},
		{name: "adapter not ready", err: dbusErr(bluezErrNotReady), want: ErrBluetoothNotPowered},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			bus := newFakeBluez(true)
			bus.onPair = func(context.Context, *pairingAgent) error { return tt.err }
			err := (&BlueZ{bus: bus}).Pair(context.Background(), testPadAddr)
			require.ErrorIs(t, err, tt.want)
			assert.Equal(t, "unexport", bus.recorded()[len(bus.recorded())-1])
		})
	}

	t.Run("already paired counts as paired", func(t *testing.T) {
		t.Parallel()
		bus := newFakeBluez(true)
		bus.onPair = func(context.Context, *pairingAgent) error { return dbusErr(bluezErrAlreadyExists) }
		require.NoError(t, (&BlueZ{bus: bus}).Pair(context.Background(), testPadAddr))
	})
	t.Run("unknown device", func(t *testing.T) {
		t.Parallel()
		bus := newFakeBluez(true)
		err := (&BlueZ{bus: bus}).Pair(context.Background(), "01:02:03:04:05:06")
		require.ErrorIs(t, err, ErrBluetoothNotFound)
		assert.NotContains(t, bus.recorded(), "export")
	})
	t.Run("powered off", func(t *testing.T) {
		t.Parallel()
		bus := newFakeBluez(false)
		err := (&BlueZ{bus: bus}).Pair(context.Background(), testPadAddr)
		require.ErrorIs(t, err, ErrBluetoothNotPowered)
	})
	t.Run("cancelled attempt is cancelled in the stack too", func(t *testing.T) {
		t.Parallel()
		bus := newFakeBluez(true)
		bus.onPair = func(ctx context.Context, _ *pairingAgent) error {
			<-ctx.Done()
			return ctx.Err()
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		defer cancel()
		err := (&BlueZ{bus: bus}).Pair(ctx, testPadAddr)
		require.ErrorIs(t, err, context.DeadlineExceeded)
		assert.Contains(t, bus.recorded(), string(testPad)+" "+bluezCancelPairing)
		assert.Contains(t, bus.recorded(), string(bluezRoot)+" "+bluezUnregisterAgent)
	})
}

func TestBlueZ_TrustConnectRemove(t *testing.T) {
	t.Parallel()

	bus := newFakeBluez(true)
	controller := &BlueZ{bus: bus}
	require.NoError(t, controller.Trust(context.Background(), testPadAddr))
	assert.Equal(t, []any{bluezDevice, "Trusted", dbus.MakeVariant(true)}, bus.args[bluezPropertiesSet])

	bus.errs[bluezConnect] = dbusErr(bluezErrAlreadyConnected)
	require.NoError(t, controller.Connect(context.Background(), testPadAddr))

	require.NoError(t, controller.Remove(context.Background(), testPadAddr))
	assert.Equal(t, []any{testPad}, bus.args[bluezRemoveDevice])
	assert.Contains(t, bus.recorded(), string(testAdapter)+" "+bluezRemoveDevice)

	require.ErrorIs(t, controller.Remove(context.Background(), "01:02:03:04:05:06"), ErrBluetoothNotFound)
}

// A slow adapter answers these after the quick limit, and the change still
// lands, so they get the longer one.
func TestBlueZ_RadioCallsOutwaitTheQuickLimit(t *testing.T) {
	t.Parallel()

	bus := newFakeBluez(true)
	controller := &BlueZ{bus: bus}
	require.NoError(t, controller.StartDiscovery(context.Background()))
	require.NoError(t, controller.StopDiscovery(context.Background()))
	require.NoError(t, controller.Remove(context.Background(), testPadAddr))

	bus.mu.Lock()
	defer bus.mu.Unlock()
	for _, method := range []string{bluezStartDiscovery, bluezStopDiscovery, bluezRemoveDevice} {
		assert.Greater(t, bus.limits[method], bluezCallLimit, method)
	}
	assert.LessOrEqual(t, bus.limits[bluezGetManagedObjects], bluezCallLimit)
}

func TestBlueZ_Watch(t *testing.T) {
	t.Parallel()

	bus := newFakeBluez(true)
	controller := &BlueZ{bus: bus, watchRetry: time.Millisecond}
	changes := make(chan struct{}, 16)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		controller.Watch(ctx, func() { changes <- struct{}{} })
	}()

	// Subscribing reports once, for whatever changed before it took hold.
	select {
	case <-changes:
	case <-time.After(5 * time.Second):
		t.Fatal("subscribing did not report a change")
	}
	bus.signals <- &dbus.Signal{
		Name: signalPropertiesChanged, Path: testPad,
		Body: []any{"org.bluez.Battery1", map[string]dbus.Variant{}, []string{}},
	}
	bus.signals <- &dbus.Signal{
		Name: signalPropertiesChanged, Path: testPad,
		Body: []any{bluezDevice, map[string]dbus.Variant{"Connected": dbus.MakeVariant(true)}, []string{}},
	}
	select {
	case <-changes:
	case <-time.After(5 * time.Second):
		t.Fatal("a device property change was not reported")
	}
	bus.signals <- &dbus.Signal{
		Name: signalInterfacesAdded, Path: "/",
		Body: []any{testPad, map[string]map[string]dbus.Variant{bluezDevice: {}}},
	}
	select {
	case <-changes:
	case <-time.After(5 * time.Second):
		t.Fatal("a new device was not reported")
	}
	assert.Empty(t, changes, "a battery change is not a bluetooth change")

	cancel()
	<-done
}

func TestBlueZ_WatchResubscribes(t *testing.T) {
	t.Parallel()

	bus := newFakeBluez(true)
	controller := &BlueZ{bus: bus, watchRetry: time.Millisecond}
	changes := make(chan struct{}, 16)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		controller.Watch(ctx, func() { changes <- struct{}{} })
	}()
	waitChange := func(why string) {
		t.Helper()
		select {
		case <-changes:
		case <-time.After(5 * time.Second):
			t.Fatal(why)
		}
	}

	waitChange("the first subscription did not report a change")
	bus.mu.Lock()
	lost := bus.signals
	bus.signals = make(chan *dbus.Signal, 8)
	bus.mu.Unlock()
	close(lost)
	waitChange("the new subscription did not report a change")
	bus.mu.Lock()
	subs := bus.subs
	bus.mu.Unlock()
	assert.Equal(t, 2, subs)

	cancel()
	<-done
}

func TestBlueZ_PairIgnoresOtherDevicesPins(t *testing.T) {
	t.Parallel()

	other := dbus.ObjectPath("/org/bluez/hci0/dev_11_22_33_44_55_66")
	bus := newFakeBluez(true)
	bus.onPair = func(_ context.Context, agent *pairingAgent) error {
		_, err := agent.RequestPinCode(other)
		assert.NotNil(t, err, "another device's PIN request is refused")
		_, err = agent.RequestPasskey(other)
		assert.NotNil(t, err)
		assert.NotNil(t, agent.DisplayPasskey(other, 1, 0))
		assert.NotNil(t, agent.DisplayPinCode(other, "0000"))
		return nil
	}
	require.NoError(t, (&BlueZ{bus: bus}).Pair(context.Background(), testPadAddr))
	assert.NotContains(t, bus.recorded(), string(testPad)+" "+bluezCancelPairing)
}

func TestRelevantBluezSignal(t *testing.T) {
	t.Parallel()

	assert.True(t, relevantBluezSignal(&dbus.Signal{
		Name: signalInterfacesRemoved, Path: "/", Body: []any{testPad, []string{bluezDevice}},
	}))
	assert.False(t, relevantBluezSignal(&dbus.Signal{
		Name: signalInterfacesRemoved, Path: "/", Body: []any{testPad, []string{"org.bluez.MediaControl1"}},
	}))
	assert.True(t, relevantBluezSignal(&dbus.Signal{
		Name: signalPropertiesChanged, Path: testAdapter, Body: []any{bluezAdapter, map[string]dbus.Variant{}},
	}))
	assert.False(t, relevantBluezSignal(&dbus.Signal{
		Name: signalPropertiesChanged, Path: "/org/freedesktop/NetworkManager", Body: []any{bluezAdapter},
	}))
	assert.True(t, relevantBluezSignal(&dbus.Signal{
		Name: signalNameOwnerChanged, Path: "/org/freedesktop/DBus", Body: []any{bluezService, ":1.4", ""},
	}))
	assert.False(t, relevantBluezSignal(&dbus.Signal{
		Name: signalNameOwnerChanged, Path: "/org/freedesktop/DBus",
		Body: []any{"org.freedesktop.NetworkManager", "", ":1.9"},
	}))
	assert.False(t, relevantBluezSignal(&dbus.Signal{Name: "org.example.Other"}))
}

func TestBlueZ_Batteries(t *testing.T) {
	t.Parallel()

	bus := newFakeBluez(true)
	bus.objects[testPad][bluezBattery] = map[string]dbus.Variant{"Percentage": dbus.MakeVariant(byte(83))}
	other := dbus.ObjectPath("/org/bluez/hci0/dev_11_22_33_44_55_66")
	bus.objects[other] = map[string]map[string]dbus.Variant{
		bluezDevice: {"Address": dbus.MakeVariant("11:22:33:44:55:66")},
	}
	bluez := &BlueZ{bus: bus}

	batteries, err := bluez.Batteries(context.Background())
	require.NoError(t, err)
	assert.Equal(t, map[string]int{"aa:bb:cc:dd:ee:ff": 83}, batteries)
}
