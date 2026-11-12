//go:build windows

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

package ble

import (
	"context"
	"testing"
	"time"

	"github.com/saltosystems/winrt-go/windows/devices/bluetooth"
	gatt "github.com/saltosystems/winrt-go/windows/devices/bluetooth/genericattributeprofile"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestCentral(t *testing.T, host *fakeHost) *central {
	t.Helper()
	a, _ := newTestAdapter(host)
	c, err := a.Central()
	require.NoError(t, err)
	impl, ok := c.(*central)
	require.True(t, ok)
	return impl
}

func receive(t *testing.T, ch <-chan ScanResult) ScanResult {
	t.Helper()
	select {
	case r, ok := <-ch:
		require.True(t, ok, "scan ended")
		return r
	case <-time.After(testWait):
		require.FailNow(t, "no scan result")
		return ScanResult{}
	}
}

func requireNoResult(t *testing.T, ch <-chan ScanResult) {
	t.Helper()
	select {
	case r := <-ch:
		require.Failf(t, "unexpected scan result", "%+v", r)
	default:
	}
}

func requireClosed(t *testing.T, ch <-chan ScanResult) {
	t.Helper()
	select {
	case r, ok := <-ch:
		require.False(t, ok, "unexpected scan result %+v", r)
	case <-time.After(testWait):
		require.FailNow(t, "scan did not end")
	}
}

func TestAdvertisesAny(t *testing.T) {
	t.Parallel()
	assert.True(t, advertisesAny(nil, nil))
	assert.True(t, advertisesAny([]string{"a"}, nil))
	assert.True(t, advertisesAny([]string{"x", testServiceUUID}, []string{"6e400001-b5a3-f393-e0a9-e50e24dcca9e"}))
	assert.False(t, advertisesAny(nil, []string{testServiceUUID}))
	assert.False(t, advertisesAny([]string{"x"}, []string{testServiceUUID}))
}

func TestMergeScanResult(t *testing.T) {
	t.Parallel()
	known := &ScanResult{Address: testDeviceAddr}
	assert.True(t, mergeScanResult(known, &ScanResult{RSSI: -60}), "the first sighting is news")
	assert.Equal(t, int16(-60), known.RSSI)
	assert.True(t, known.HasRSSI)

	assert.False(t, mergeScanResult(known, &ScanResult{RSSI: -50}), "a signal change alone is not")
	assert.Equal(t, int16(-50), known.RSSI)

	assert.True(t, mergeScanResult(known, &ScanResult{Name: "Pad"}))
	assert.False(t, mergeScanResult(known, &ScanResult{Name: "Pad"}))
	assert.False(t, mergeScanResult(known, &ScanResult{}), "a missing name keeps the old one")
	assert.Equal(t, "Pad", known.Name)

	assert.True(t, mergeScanResult(known, &ScanResult{ServiceUUIDs: []string{"a", "b"}}))
	assert.False(t, mergeScanResult(known, &ScanResult{ServiceUUIDs: []string{"b"}}))
	assert.True(t, mergeScanResult(known, &ScanResult{ServiceUUIDs: []string{"c"}}))
	assert.Equal(t, []string{"a", "b", "c"}, known.ServiceUUIDs)

	assert.True(t, mergeScanResult(known, &ScanResult{ManufacturerData: map[uint16][]byte{1: {1}}}))
	assert.False(t, mergeScanResult(known, &ScanResult{ManufacturerData: map[uint16][]byte{1: {1}}}))
	assert.True(t, mergeScanResult(known, &ScanResult{ManufacturerData: map[uint16][]byte{1: {2}, 2: {3}}}))
	assert.Equal(t, map[uint16][]byte{1: {2}, 2: {3}}, known.ManufacturerData)
}

func TestEmitScanResultCopiesAndDropsOldest(t *testing.T) {
	t.Parallel()
	out := make(chan ScanResult, 2)
	r := &ScanResult{
		Address:          "1",
		ServiceUUIDs:     []string{"a"},
		ManufacturerData: map[uint16][]byte{1: {1}},
	}
	emitScanResult(out, r)
	r.ServiceUUIDs[0] = "changed"
	r.ManufacturerData[1][0] = 9
	emitScanResult(out, &ScanResult{Address: "2"})
	emitScanResult(out, &ScanResult{Address: "3"})

	first := <-out
	assert.Equal(t, "2", first.Address, "the oldest result was dropped")
	assert.Equal(t, "3", (<-out).Address)

	emitScanResult(out, &ScanResult{Address: "4", ServiceUUIDs: []string{"a"}, ManufacturerData: map[uint16][]byte{}})
	copied := <-out
	assert.NotNil(t, copied.ManufacturerData)

	out = make(chan ScanResult, 1)
	r = &ScanResult{ServiceUUIDs: []string{"a"}, ManufacturerData: map[uint16][]byte{1: {1}}}
	emitScanResult(out, r)
	r.ServiceUUIDs[0] = "changed"
	r.ManufacturerData[1][0] = 9
	got := <-out
	assert.Equal(t, []string{"a"}, got.ServiceUUIDs)
	assert.Equal(t, []byte{1}, got.ManufacturerData[1])
}

func TestScanFiltersAndReportsChanges(t *testing.T) {
	t.Parallel()
	host := newFakeHost()
	c := newTestCentral(t, host)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	results, err := c.Scan(ctx, ScanFilter{ServiceUUIDs: []string{testServiceUUID}})
	require.NoError(t, err)
	w := host.nextWatcher(t)
	assert.Equal(t, int32(1), w.started.Load())

	w.onReceived(ScanResult{Address: testDeviceAddr, RSSI: -70, HasRSSI: true})
	requireNoResult(t, results)

	w.onReceived(ScanResult{Address: testDeviceAddr, ServiceUUIDs: []string{testServiceUUID}})
	got := receive(t, results)
	assert.Equal(t, testDeviceAddr, got.Address)
	assert.Equal(t, []string{testServiceUUID}, got.ServiceUUIDs)

	w.onReceived(ScanResult{Address: testDeviceAddr, RSSI: -40})
	requireNoResult(t, results)

	w.onReceived(ScanResult{Address: testDeviceAddr, Name: "Pad", RSSI: -45})
	got = receive(t, results)
	assert.Equal(t, "Pad", got.Name)
	assert.Equal(t, int16(-45), got.RSSI)

	w.onReceived(ScanResult{Address: "AA:BB:CC:DD:EE:02", Name: "Other"})
	requireNoResult(t, results)

	cancel()
	requireClosed(t, results)
	w.waitReleased(t)
	assert.Equal(t, int32(1), w.stopped.Load())
	assert.Equal(t, int32(1), w.unregistered.Load())

	// A sighting after the scan ended is ignored.
	w.onReceived(ScanResult{Address: "AA:BB:CC:DD:EE:03"})
}

func TestScanEndsWhenAdapterGoes(t *testing.T) {
	t.Parallel()
	host := newFakeHost()
	c := newTestCentral(t, host)
	results, err := c.Scan(context.Background(), ScanFilter{})
	require.NoError(t, err)
	w := host.nextWatcher(t)
	w.stopErr = errFake
	w.onReceived(ScanResult{Address: testDeviceAddr})
	receive(t, results)

	require.NoError(t, c.a.Close())
	requireClosed(t, results)
	w.waitReleased(t)
	assert.Equal(t, int32(1), w.stopped.Load())
}

func TestScanStartFailures(t *testing.T) {
	t.Parallel()
	host := newFakeHost()
	host.watcherErr = errFake
	c := newTestCentral(t, host)
	_, err := c.Scan(context.Background(), ScanFilter{})
	require.ErrorIs(t, err, errFake)

	host = newFakeHost()
	host.startErr = errFake
	c = newTestCentral(t, host)
	_, err = c.Scan(context.Background(), ScanFilter{})
	require.ErrorIs(t, err, ErrUnavailable)
	require.ErrorIs(t, err, errFake)
	w := host.nextWatcher(t)
	assert.Equal(t, int32(1), w.unregistered.Load())
	assert.Equal(t, int32(1), w.releases.Load())
	assert.Equal(t, int32(0), w.stopped.Load())
}

func TestFindKnownDevice(t *testing.T) {
	t.Parallel()
	host := newFakeHost()
	le := &fakeLEDevice{}
	host.lookups = []lookupResult{{dev: le}}
	c := newTestCentral(t, host)

	dev, err := c.Find(context.Background(), "aa:bb:cc:dd:ee:01")
	require.NoError(t, err)
	assert.Equal(t, testDeviceAddr, dev.Address())
	assert.Equal(t, []string{testDeviceAddr}, host.lookupAddrs)
	select {
	case <-host.watchers:
		assert.Fail(t, "a known device is not scanned for")
	default:
	}
}

func TestFindInvalidAddress(t *testing.T) {
	t.Parallel()
	c := newTestCentral(t, newFakeHost())
	_, err := c.Find(context.Background(), "not an address")
	require.ErrorContains(t, err, "invalid bluetooth address")
}

func TestFindLookupFails(t *testing.T) {
	t.Parallel()
	host := newFakeHost()
	host.lookups = []lookupResult{{err: errFake}}
	c := newTestCentral(t, host)
	_, err := c.Find(context.Background(), testDeviceAddr)
	require.ErrorIs(t, err, errFake)
	require.ErrorContains(t, err, "look up "+testDeviceAddr)
}

type findResult struct {
	dev Device
	err error
}

func findAsync(ctx context.Context, c *central, addr string) <-chan findResult {
	done := make(chan findResult, 1)
	go func() {
		dev, err := c.Find(ctx, addr)
		done <- findResult{dev: dev, err: err}
	}()
	return done
}

func awaitFind(t *testing.T, done <-chan findResult) findResult {
	t.Helper()
	select {
	case r := <-done:
		return r
	case <-time.After(testWait):
		require.FailNow(t, "Find did not return")
		return findResult{}
	}
}

func TestFindScansForUnknownDevice(t *testing.T) {
	t.Parallel()
	host := newFakeHost()
	le := &fakeLEDevice{}
	host.lookups = []lookupResult{{}, {dev: le}}
	c := newTestCentral(t, host)

	done := findAsync(context.Background(), c, testDeviceAddr)
	w := host.nextWatcher(t)
	w.onReceived(ScanResult{Address: "AA:BB:CC:DD:EE:02"})
	w.onReceived(ScanResult{Address: testDeviceAddr})

	r := awaitFind(t, done)
	require.NoError(t, r.err)
	assert.Equal(t, testDeviceAddr, r.dev.Address())
	w.waitReleased(t)
	assert.Equal(t, []string{testDeviceAddr, testDeviceAddr}, host.lookupAddrs)
}

func TestFindHeardButUnknown(t *testing.T) {
	t.Parallel()
	host := newFakeHost()
	c := newTestCentral(t, host)
	done := findAsync(context.Background(), c, testDeviceAddr)
	host.nextWatcher(t).onReceived(ScanResult{Address: testDeviceAddr})
	r := awaitFind(t, done)
	require.ErrorIs(t, r.err, ErrNotFound)
}

func TestFindSecondLookupFails(t *testing.T) {
	t.Parallel()
	host := newFakeHost()
	host.lookups = []lookupResult{{}, {err: errFake}}
	c := newTestCentral(t, host)
	done := findAsync(context.Background(), c, testDeviceAddr)
	host.nextWatcher(t).onReceived(ScanResult{Address: testDeviceAddr})
	r := awaitFind(t, done)
	require.ErrorIs(t, r.err, errFake)
}

func TestFindCanceled(t *testing.T) {
	t.Parallel()
	host := newFakeHost()
	c := newTestCentral(t, host)
	ctx, cancel := context.WithCancel(context.Background())
	done := findAsync(ctx, c, testDeviceAddr)
	w := host.nextWatcher(t)
	cancel()
	r := awaitFind(t, done)
	require.ErrorIs(t, r.err, context.Canceled)
	w.waitReleased(t)
}

func TestFindAdapterGone(t *testing.T) {
	t.Parallel()
	host := newFakeHost()
	c := newTestCentral(t, host)
	done := findAsync(context.Background(), c, testDeviceAddr)
	host.nextWatcher(t)
	require.NoError(t, c.a.Close())
	r := awaitFind(t, done)
	require.ErrorIs(t, r.err, ErrUnavailable)
}

func TestFindScanFails(t *testing.T) {
	t.Parallel()
	host := newFakeHost()
	host.watcherErr = errFake
	c := newTestCentral(t, host)
	_, err := c.Find(context.Background(), testDeviceAddr)
	require.ErrorIs(t, err, errFake)
}

func connected(t *testing.T, le *fakeLEDevice) *device {
	t.Helper()
	d := newDevice(le, testDeviceAddr)
	require.NoError(t, d.Connect(context.Background()))
	return d
}

func TestConnectAndDisconnect(t *testing.T) {
	t.Parallel()
	svc := &fakeService{id: testServiceUUID}
	le := &fakeLEDevice{services: []remoteService{svc}}
	d := connected(t, le)
	assert.Equal(t, testDeviceAddr, d.Address())

	select {
	case <-d.Disconnected():
		require.FailNow(t, "disconnected before Disconnect")
	default:
	}
	require.NoError(t, d.Disconnect(context.Background()))
	require.NoError(t, d.Disconnect(context.Background()))
	<-d.Disconnected()
	assert.Equal(t, int32(1), svc.closed.Load())
	assert.Equal(t, int32(1), le.statusEvent.removed.Load())
	assert.Equal(t, 1, le.statusEvent.released.count())
	assert.Equal(t, int32(1), le.session.closed.Load())
	assert.Equal(t, int32(1), le.closed.Load())
}

func TestDisconnectWithoutConnect(t *testing.T) {
	t.Parallel()
	le := &fakeLEDevice{}
	d := newDevice(le, testDeviceAddr)
	require.NoError(t, d.Disconnect(context.Background()))
	<-d.Disconnected()
	assert.Equal(t, int32(1), le.closed.Load())
}

func TestConnectionStatusReportsDisconnect(t *testing.T) {
	t.Parallel()
	le := &fakeLEDevice{}
	d := connected(t, le)
	le.setStatus(bluetooth.BluetoothConnectionStatusConnected)
	select {
	case <-d.Disconnected():
		require.FailNow(t, "a connected status is not a disconnect")
	default:
	}
	le.setStatus(bluetooth.BluetoothConnectionStatusDisconnected)
	le.setStatus(bluetooth.BluetoothConnectionStatusDisconnected)
	<-d.Disconnected()
	require.NoError(t, d.Disconnect(context.Background()))
}

func TestConnectFailures(t *testing.T) {
	t.Parallel()
	tests := []struct {
		target        error
		le            *fakeLEDevice
		name          string
		sessionClosed bool
	}{
		{name: "session", le: &fakeLEDevice{sessionErr: errFake}, target: errFake},
		{name: "watch", le: &fakeLEDevice{watchErr: errFake}, target: errFake, sessionClosed: true},
		{name: "discover", le: &fakeLEDevice{discoverErr: errFake}, target: errFake},
		{
			name:   "unreachable",
			le:     &fakeLEDevice{discoverStatus: gatt.GattCommunicationStatusUnreachable},
			target: errDisconnected,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			d := newDevice(tt.le, testDeviceAddr)
			err := d.Connect(context.Background())
			require.ErrorIs(t, err, tt.target)
			require.ErrorContains(t, err, "connect "+testDeviceAddr)
			if tt.sessionClosed {
				assert.Equal(t, int32(1), tt.le.session.closed.Load())
			}
		})
	}
}

func TestConnectProtocolError(t *testing.T) {
	t.Parallel()
	le := &fakeLEDevice{discoverStatus: gatt.GattCommunicationStatusProtocolError}
	err := newDevice(le, testDeviceAddr).Connect(context.Background())
	require.ErrorContains(t, err, "gatt status")
	require.NotErrorIs(t, err, errDisconnected)
}

func TestCharacteristicLookup(t *testing.T) {
	t.Parallel()
	rx := &fakeGattChar{id: rxLower}
	other := &fakeGattChar{id: txLower}
	broken := &fakeGattChar{uuidErr: errFake}
	duplicate := &fakeGattChar{id: rxLower}
	le := &fakeLEDevice{services: []remoteService{
		&fakeService{uuidErr: errFake},
		&fakeService{id: "0000180f-0000-1000-8000-00805f9b34fb"},
		&fakeService{id: testServiceUUID, chars: []*fakeGattChar{broken, other, rx, duplicate}},
	}}
	d := connected(t, le)

	rc, err := d.Characteristic(testServiceUUID, testRXUUID)
	require.NoError(t, err)
	impl, ok := rc.(*remoteChar)
	require.True(t, ok)
	assert.Same(t, rx, impl.c)
	assert.Equal(t, 0, rx.released.count())
	assert.Equal(t, 1, broken.released.count())
	assert.Equal(t, 1, other.released.count())
	assert.Equal(t, 1, duplicate.released.count())

	_, err = d.Characteristic("0000180a-0000-1000-8000-00805f9b34fb", testRXUUID)
	require.ErrorIs(t, err, ErrNotFound)
	require.ErrorContains(t, err, "service 0000180a")

	_, err = d.Characteristic(testServiceUUID, "6e400009-b5a3-f393-e0a9-e50e24dcca9e")
	require.ErrorIs(t, err, ErrNotFound)
}

func TestCharacteristicListFailures(t *testing.T) {
	t.Parallel()
	d := connected(t, &fakeLEDevice{services: []remoteService{&fakeService{id: testServiceUUID, charsErr: errFake}}})
	_, err := d.Characteristic(testServiceUUID, testRXUUID)
	require.ErrorIs(t, err, errFake)

	d = connected(t, &fakeLEDevice{services: []remoteService{&fakeService{
		id: testServiceUUID, status: gatt.GattCommunicationStatusUnreachable,
	}}})
	_, err = d.Characteristic(testServiceUUID, testRXUUID)
	require.ErrorIs(t, err, errDisconnected)
}

func remote(t *testing.T, ch *fakeGattChar) (*remoteChar, *device, *fakeLEDevice) {
	t.Helper()
	le := &fakeLEDevice{
		services: []remoteService{&fakeService{id: testServiceUUID, chars: []*fakeGattChar{ch}}},
		session:  &fakeClientSession{mtu: 185},
	}
	d := connected(t, le)
	rc, err := d.Characteristic(testServiceUUID, ch.id)
	require.NoError(t, err)
	impl, ok := rc.(*remoteChar)
	require.True(t, ok)
	return impl, d, le
}

func nextValue(t *testing.T, ch <-chan []byte) []byte {
	t.Helper()
	select {
	case v, ok := <-ch:
		require.True(t, ok, "subscription ended")
		return v
	case <-time.After(testWait):
		require.FailNow(t, "no notification")
		return nil
	}
}

func requireValuesEnd(t *testing.T, ch <-chan []byte) {
	t.Helper()
	deadline := time.After(testWait)
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return
			}
		case <-deadline:
			require.FailNow(t, "subscription did not end")
		}
	}
}

func TestSubscribeDeliversUntilCanceled(t *testing.T) {
	t.Parallel()
	ch := &fakeGattChar{id: txLower}
	rc, _, _ := remote(t, ch)
	ctx, cancel := context.WithCancel(context.Background())
	values, err := rc.Subscribe(ctx)
	require.NoError(t, err)

	ch.notify([]byte("a"))
	assert.Equal(t, []byte("a"), nextValue(t, values))

	cancel()
	requireValuesEnd(t, values)
	assert.Equal(t, []gatt.GattClientCharacteristicConfigurationDescriptorValue{
		gatt.GattClientCharacteristicConfigurationDescriptorValueNotify,
		gatt.GattClientCharacteristicConfigurationDescriptorValueNone,
	}, ch.configurations())
	assert.Eventually(t, func() bool { return ch.valueEvent().released.count() == 1 }, testWait, time.Millisecond)
	assert.Equal(t, int32(1), ch.valueEvent().removed.Load())

	ch.notify([]byte("late"))
}

func TestSubscribeEndsOnDisconnect(t *testing.T) {
	t.Parallel()
	ch := &fakeGattChar{id: txLower}
	rc, d, _ := remote(t, ch)
	values, err := rc.Subscribe(context.Background())
	require.NoError(t, err)
	require.NoError(t, d.Disconnect(context.Background()))
	requireValuesEnd(t, values)
	assert.Len(t, ch.configurations(), 1, "a gone device is not told to stop")
}

func TestSubscribeStopFailureIsIgnored(t *testing.T) {
	t.Parallel()
	ch := &fakeGattChar{id: txLower}
	rc, _, _ := remote(t, ch)
	ctx, cancel := context.WithCancel(context.Background())
	values, err := rc.Subscribe(ctx)
	require.NoError(t, err)
	ch.mu.Lock()
	ch.configureErr = errFake
	ch.mu.Unlock()
	cancel()
	requireValuesEnd(t, values)
}

func TestSubscribeDropsWhenBehind(t *testing.T) {
	t.Parallel()
	ch := &fakeGattChar{id: txLower}
	rc, _, _ := remote(t, ch)
	values, err := rc.Subscribe(context.Background())
	require.NoError(t, err)
	for i := range notifyBuffer + 5 {
		ch.notify([]byte{byte(i)})
	}
	assert.Len(t, values, notifyBuffer)
	assert.Equal(t, []byte{0}, nextValue(t, values))
}

func TestSubscribeFailures(t *testing.T) {
	t.Parallel()
	ch := &fakeGattChar{id: txLower, watchErr: errFake}
	rc, _, _ := remote(t, ch)
	_, err := rc.Subscribe(context.Background())
	require.ErrorIs(t, err, errFake)
	assert.Empty(t, ch.configurations())

	ch = &fakeGattChar{id: txLower, configureErr: errFake}
	rc, _, _ = remote(t, ch)
	_, err = rc.Subscribe(context.Background())
	require.ErrorIs(t, err, errFake)
	assert.Equal(t, int32(1), ch.valueEvent().removed.Load())
	assert.Equal(t, 1, ch.valueEvent().released.count())

	ch = &fakeGattChar{id: txLower, configStatus: gatt.GattCommunicationStatusUnreachable}
	rc, _, _ = remote(t, ch)
	_, err = rc.Subscribe(context.Background())
	require.ErrorIs(t, err, errDisconnected)
	assert.Equal(t, 1, ch.valueEvent().released.count())
}

func TestRemoteWrite(t *testing.T) {
	t.Parallel()
	ch := &fakeGattChar{id: rxLower}
	rc, _, _ := remote(t, ch)
	require.NoError(t, rc.Write(context.Background(), []byte("cmd"), false))
	require.NoError(t, rc.Write(context.Background(), []byte("req"), true))
	ch.mu.Lock()
	assert.Equal(t, []clientWrite{
		{value: []byte("cmd"), option: gatt.GattWriteOptionWriteWithoutResponse},
		{value: []byte("req"), option: gatt.GattWriteOptionWriteWithResponse},
	}, ch.writes)
	ch.writeErr = errFake
	ch.mu.Unlock()
	require.ErrorIs(t, rc.Write(context.Background(), nil, true), errFake)

	ch.mu.Lock()
	ch.writeErr, ch.writeStatus = nil, gatt.GattCommunicationStatusUnreachable
	ch.mu.Unlock()
	require.ErrorIs(t, rc.Write(context.Background(), nil, true), errDisconnected)
}

func TestRemoteRead(t *testing.T) {
	t.Parallel()
	ch := &fakeGattChar{id: txLower, readValue: []byte("v")}
	rc, _, _ := remote(t, ch)
	got, err := rc.Read(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []byte("v"), got)

	ch.readStatus = gatt.GattCommunicationStatusAccessDenied
	_, err = rc.Read(context.Background())
	require.ErrorContains(t, err, "read characteristic: gatt status")

	ch.readErr = errFake
	_, err = rc.Read(context.Background())
	require.ErrorIs(t, err, errFake)
}

func TestRemoteMTU(t *testing.T) {
	t.Parallel()
	ch := &fakeGattChar{id: txLower}
	rc, d, le := remote(t, ch)
	mtu, err := rc.MTU(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 185, mtu)

	le.session.mtuErr = errFake
	_, err = rc.MTU(context.Background())
	require.ErrorIs(t, err, errFake)

	require.NoError(t, d.Disconnect(context.Background()))
	mtu, err = rc.MTU(context.Background())
	require.NoError(t, err)
	assert.Zero(t, mtu)
}
