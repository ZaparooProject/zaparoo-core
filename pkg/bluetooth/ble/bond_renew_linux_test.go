//go:build linux

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
	"encoding/binary"
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeBondEvents is a management socket whose reads block until an event is
// sent or it is closed.
type fakeBondEvents struct {
	events chan []byte
	closed chan struct{}
	once   sync.Once
}

func newFakeBondEvents() *fakeBondEvents {
	return &fakeBondEvents{events: make(chan []byte, 8), closed: make(chan struct{})}
}

func (f *fakeBondEvents) Read(b []byte) (int, error) {
	select {
	case ev := <-f.events:
		return copy(b, ev), nil
	case <-f.closed:
		return 0, os.ErrClosed
	}
}

func (*fakeBondEvents) Write(b []byte) (int, error) { return len(b), nil }

func (*fakeBondEvents) SetReadDeadline(time.Time) error { return nil }

func (f *fakeBondEvents) Close() error {
	f.once.Do(func() { close(f.closed) })
	return nil
}

// newLTKEvent builds a New Long Term Key event for the controller at index.
func newLTKEvent(index uint16, storeHint uint8, address string, addrType uint8) []byte {
	wire, ok := wireAddress(address)
	if !ok {
		panic("bad address " + address)
	}
	ev := make([]byte, mgmtHeaderSize+1+mgmtLTKInfoSize)
	binary.LittleEndian.PutUint16(ev[0:], mgmtEvNewLongTermKey)
	binary.LittleEndian.PutUint16(ev[2:], index)
	binary.LittleEndian.PutUint16(ev[4:], uint16(1+mgmtLTKInfoSize))
	ev[mgmtHeaderSize] = storeHint
	copy(ev[mgmtHeaderSize+1:], wire[:])
	ev[mgmtHeaderSize+7] = addrType
	for i := mgmtHeaderSize + 8; i < len(ev); i++ {
		ev[i] = 0x5a
	}
	return ev
}

// listDevice makes bluetoothd's object tree hold a device.
func listDevice(fake *fakeBluez, path dbus.ObjectPath, address string) {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	fake.objects[path] = map[string]map[string]dbus.Variant{
		deviceIface: {"Address": dbus.MakeVariant(address)},
	}
}

// watchedPeripheral serves on the fake with the window's bond watch reading
// from the returned events. peripheral answers whether the device is
// connected to Core's advertisement.
func watchedPeripheral(t *testing.T, fake *fakeBluez, addr string, peripheral bool) (*peripheral, *fakeBondEvents) {
	t.Helper()
	p := servingPeripheral(t, fake, addr)
	listDevice(fake, fakeDevicePath, fakeDeviceAddr)
	p.bondStallTimeout = 100 * time.Millisecond
	events := newFakeBondEvents()
	p.openBondEvents = func() (mgmtConn, error) { return events, nil }
	p.linkRole = func(index uint16, wire [6]byte) (bool, error) {
		if index != 0 || addressString(wire) != fakeDeviceAddr {
			return false, nil
		}
		return peripheral, nil
	}
	return p, events
}

// A phone that forgot Core and pairs again is re-bonded by bluetoothd
// without asking the agent. Its link stalls the same way, so it is
// disconnected, and its bond goes with the window unless it is kept.
func TestIntegration_BondWindowDropsRenewedBondThatStalls(t *testing.T) {
	addr := startSessionBus(t)
	fake := newFakeBluez(t, addr)
	p, events := watchedPeripheral(t, fake, addr, true)

	window, err := p.AllowBonding(t.Context())
	require.NoError(t, err)
	events.events <- newLTKEvent(0, 1, fakeDeviceAddr, bdaddrLEPublic)
	assert.True(t, sawCall(fake, "Disconnect", integrationWait),
		"a re-bonded device that never used Core's service must be disconnected")

	require.NoError(t, window.Close(t.Context()))
	assert.True(t, removedDevices(fake)["RemoveDevice:"+string(fakeDevicePath)],
		"an unproven renewed bond must be removed")
	select {
	case <-events.closed:
	default:
		t.Fatal("closing the window must close the management socket")
	}
}

// A device the adapter connected to itself, a gamepad, is never touched.
func TestIntegration_BondWindowIgnoresRenewedBondOnCentralLink(t *testing.T) {
	addr := startSessionBus(t)
	fake := newFakeBluez(t, addr)
	p, events := watchedPeripheral(t, fake, addr, false)

	window, err := p.AllowBonding(t.Context())
	require.NoError(t, err)
	events.events <- newLTKEvent(0, 1, fakeDeviceAddr, bdaddrLEPublic)
	assert.False(t, sawCall(fake, "Disconnect", 500*time.Millisecond))

	require.NoError(t, window.Close(t.Context()))
	assert.False(t, removedDevices(fake)["RemoveDevice:"+string(fakeDevicePath)])
}

// A key for a device bluetoothd does not list, for another controller, or
// one the kernel does not keep, changes nothing.
func TestIntegration_BondWindowIgnoresUnrelatedKeys(t *testing.T) {
	addr := startSessionBus(t)
	fake := newFakeBluez(t, addr)
	p, events := watchedPeripheral(t, fake, addr, true)
	p.linkRole = func(uint16, [6]byte) (bool, error) { return true, nil }

	window, err := p.AllowBonding(t.Context())
	require.NoError(t, err)
	events.events <- newLTKEvent(0, 1, "AA:BB:CC:DD:EE:77", bdaddrLEPublic)
	events.events <- newLTKEvent(1, 1, fakeDeviceAddr, bdaddrLEPublic)
	events.events <- newLTKEvent(0, 0, fakeDeviceAddr, bdaddrLEPublic)
	assert.False(t, sawCall(fake, "Disconnect", 500*time.Millisecond))

	require.NoError(t, window.Close(t.Context()))
	for call := range removedDevices(fake) {
		assert.NotContains(t, call, "RemoveDevice")
	}
}

// A phone first seen at a private address keeps that object path after
// bluetoothd learns its identity, and the key names the identity.
func TestIntegration_BondWindowFindsRenewedBondByIdentity(t *testing.T) {
	addr := startSessionBus(t)
	fake := newFakeBluez(t, addr)
	p, events := watchedPeripheral(t, fake, addr, true)
	p.bondStallTimeout = time.Hour
	fake.mu.Lock()
	delete(fake.objects, fakeDevicePath)
	fake.mu.Unlock()
	private := dbus.ObjectPath(string(fakeAdapterPath) + "/dev_58_66_B4_01_02_03")
	listDevice(fake, private, "aa:bb:cc:dd:ee:01")

	window, err := p.AllowBonding(t.Context())
	require.NoError(t, err)
	events.events <- newLTKEvent(0, 1, fakeDeviceAddr, bdaddrLEPublic)
	require.Eventually(t, func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		return len(p.useWaiters[private]) == 1
	}, integrationWait, time.Millisecond)

	require.NoError(t, window.Close(t.Context()))
	assert.True(t, removedDevices(fake)["RemoveDevice:"+string(private)])
}

// A bond the agent accepted is not taken on twice when its key arrives.
func TestIntegration_BondWindowRenewedAfterAuthorizeIsOneBond(t *testing.T) {
	addr := startSessionBus(t)
	fake := newFakeBluez(t, addr)
	p, events := watchedPeripheral(t, fake, addr, true)
	p.bondStallTimeout = time.Hour
	var checks atomic.Int32
	p.linkRole = func(uint16, [6]byte) (bool, error) {
		checks.Add(1)
		return true, nil
	}

	window, err := p.AllowBonding(t.Context())
	require.NoError(t, err)
	var agent appRegistration
	select {
	case agent = <-fake.agents:
	case <-time.After(integrationWait):
		t.Fatal("the agent was never made the default")
	}
	require.NoError(t, authorize(fake, agent, fakeDevicePath))
	events.events <- newLTKEvent(0, 1, fakeDeviceAddr, bdaddrLEPublic)
	// Events are handled in order, so the second being checked means the
	// first is done with.
	events.events <- newLTKEvent(0, 1, "AA:BB:CC:DD:EE:77", bdaddrLEPublic)
	require.Eventually(t, func() bool { return checks.Load() == 2 }, integrationWait, time.Millisecond)
	require.NoError(t, window.Close(t.Context()))
	p.mu.Lock()
	defer p.mu.Unlock()
	assert.Len(t, p.useWaiters[fakeDevicePath], 1, "one stall check per bond")
}

func TestIntegration_BondWindowOpensWithoutManagementSocket(t *testing.T) {
	addr := startSessionBus(t)
	fake := newFakeBluez(t, addr)
	p := servingPeripheral(t, fake, addr)

	window, err := p.AllowBonding(t.Context())
	require.NoError(t, err)
	require.NoError(t, window.Close(t.Context()))
}

func TestBondWindow_RenewedSkipsClosedAndUncheckableLinks(t *testing.T) {
	a := &adapter{gone: make(chan struct{})}
	t.Cleanup(func() { close(a.gone) })
	p := newPeripheral(a)
	checked := 0
	p.linkRole = func(uint16, [6]byte) (bool, error) {
		checked++
		return false, errors.New("no socket")
	}
	w := &bondWindow{
		p:        p,
		closed:   make(chan struct{}),
		accepted: make(map[dbus.ObjectPath]bool),
		kept:     make(map[dbus.ObjectPath]bool),
	}
	wire, ok := wireAddress(fakeDeviceAddr)
	require.True(t, ok)

	w.renewed(0, wire)
	assert.Equal(t, 1, checked)
	assert.Empty(t, w.accepted, "a link that cannot be checked is left alone")

	close(w.closed)
	w.renewed(0, wire)
	assert.Equal(t, 1, checked, "nothing is checked once the window closed")
	assert.Empty(t, w.accepted)
}

func TestRenewedBondAddress(t *testing.T) {
	want, ok := wireAddress(fakeDeviceAddr)
	require.True(t, ok)

	got, ok := renewedBondAddress(newLTKEvent(2, 1, fakeDeviceAddr, bdaddrLEPublic), 2)
	assert.True(t, ok)
	assert.Equal(t, want, got)
	_, ok = renewedBondAddress(newLTKEvent(2, 1, fakeDeviceAddr, bdaddrLERandom), 2)
	assert.True(t, ok, "a static random identity is kept")

	full := newLTKEvent(0, 1, fakeDeviceAddr, bdaddrLEPublic)
	other := newLTKEvent(0, 1, fakeDeviceAddr, bdaddrLEPublic)
	binary.LittleEndian.PutUint16(other[0:], mgmtEvCmdComplete)
	for name, ev := range map[string][]byte{
		"other controller": newLTKEvent(1, 1, fakeDeviceAddr, bdaddrLEPublic),
		"not kept":         newLTKEvent(0, 0, fakeDeviceAddr, bdaddrLEPublic),
		"BR/EDR address":   newLTKEvent(0, 1, fakeDeviceAddr, 0),
		"other event":      other,
		"truncated":        full[:mgmtHeaderSize+7],
	} {
		_, ok := renewedBondAddress(ev, 0)
		assert.False(t, ok, name)
	}
}

func TestAddressString(t *testing.T) {
	wire, ok := wireAddress("90:5f:7a:10:90:a4")
	require.True(t, ok)
	assert.Equal(t, "90:5F:7A:10:90:A4", addressString(wire))
}

// connList builds a filled hci_conn_list_req.
func connList(infos ...[]byte) []byte {
	buf := make([]byte, 4, 4+len(infos)*hciConnInfoSize)
	binary.NativeEndian.PutUint16(buf[2:], uint16(len(infos))) //nolint:gosec // G115: a handful of test links
	for _, ci := range infos {
		buf = append(buf, ci...)
	}
	return buf
}

func connInfo(address string, linkType uint8, linkMode uint32) []byte {
	wire, ok := wireAddress(address)
	if !ok {
		panic("bad address " + address)
	}
	ci := make([]byte, hciConnInfoSize)
	copy(ci[2:8], wire[:])
	ci[8] = linkType
	binary.NativeEndian.PutUint32(ci[12:], linkMode)
	return ci
}

func TestPeripheralLinkIn(t *testing.T) {
	phone := "90:5F:7A:10:90:A4"
	pad := "44:16:22:D0:7B:4D"
	wire, ok := wireAddress(phone)
	require.True(t, ok)

	assert.True(t, peripheralLinkIn(connList(
		connInfo(pad, hciLELink, hciLMMaster),
		connInfo(phone, hciLELink, 0x0004),
	), wire), "the phone connected to Core's advertisement")
	assert.False(t, peripheralLinkIn(connList(connInfo(phone, hciLELink, hciLMMaster|0x0004)), wire),
		"a link the adapter made is a central link")
	assert.False(t, peripheralLinkIn(connList(connInfo(phone, 0x01, 0)), wire), "a classic link")
	assert.False(t, peripheralLinkIn(connList(connInfo(pad, hciLELink, 0)), wire), "not connected")
	assert.False(t, peripheralLinkIn(nil, wire))

	short := connList(connInfo(phone, hciLELink, 0))
	binary.NativeEndian.PutUint16(short[2:], 3)
	assert.False(t, peripheralLinkIn(short[:4+hciConnInfoSize-1], wire), "a count past the buffer")
}
