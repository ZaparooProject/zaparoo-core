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
	"slices"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	owedDevicePath  = dbus.ObjectPath("/org/bluez/hci0/dev_AA_BB_CC_DD_EE_11")
	owedDeviceAddr  = "AA:BB:CC:DD:EE:11"
	owedDevicePath2 = dbus.ObjectPath("/org/bluez/hci0/dev_AA_BB_CC_DD_EE_12")
	owedDeviceAddr2 = "AA:BB:CC:DD:EE:12"
)

func (f *fakeBluez) failNextRemoves(n int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failRemove = n
}

func (f *fakeBluez) failNextPairable(n int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failPairable = n
}

func (f *fakeBluez) removedPaths() []dbus.ObjectPath {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.removed)
}

func (f *fakeBluez) pairableHistory() []bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.pairableSets)
}

func (f *fakeBluez) pairable(t *testing.T) bool {
	t.Helper()
	v, dbusErr := f.adapterProp.Get(adapterIface, "Pairable")
	require.Nil(t, dbusErr)
	on, ok := v.Value().(bool)
	require.True(t, ok)
	return on
}

// bondWindowWith opens a window in which every given device bonds.
func bondWindowWith(t *testing.T, fake *fakeBluez, p *peripheral, devices ...dbus.ObjectPath) BondWindow {
	t.Helper()
	window, err := p.AllowBonding(t.Context())
	require.NoError(t, err)
	var agent appRegistration
	select {
	case agent = <-fake.agents:
	case <-time.After(integrationWait):
		t.Fatal("the agent was never made the default")
	}
	for _, device := range devices {
		require.NoError(t, authorize(fake, agent, device))
	}
	return window
}

// A bond the close could not remove is removed by closing again.
func TestIntegration_BondCleanupRetriesFailedRemove(t *testing.T) {
	addr := startSessionBus(t)
	fake := newFakeBluez(t, addr)
	p := servingPeripheral(t, fake, addr)
	listDevice(fake, owedDevicePath, owedDeviceAddr)
	window := bondWindowWith(t, fake, p, owedDevicePath)

	fake.failNextRemoves(1)
	err := window.Close(t.Context())
	require.Error(t, err)
	assert.Contains(t, err.Error(), owedDeviceAddr, "the error names the bond still owed")
	assert.Equal(t, []dbus.ObjectPath{owedDevicePath}, fake.removedPaths())

	require.NoError(t, window.Close(t.Context()))
	assert.Equal(t, []dbus.ObjectPath{owedDevicePath, owedDevicePath}, fake.removedPaths(),
		"the bond the first close could not remove must be removed by the next")

	require.NoError(t, window.Close(t.Context()))
	assert.Len(t, fake.removedPaths(), 2, "nothing is owed any more")
}

// The adapter is lost while the window is open: nothing can be undone
// through its closed connection, so the next connection to serve does it.
func TestIntegration_BondCleanupSettledByNextAdapter(t *testing.T) {
	addr := startSessionBus(t)
	fake := newFakeBluez(t, addr)
	debts := newBondDebts()
	p, _ := servingPeripheralOwing(t, fake, addr, debts)
	listDevice(fake, owedDevicePath, owedDeviceAddr)
	window := bondWindowWith(t, fake, p, owedDevicePath)

	require.NoError(t, p.a.Close())
	require.Error(t, window.Close(t.Context()))
	assert.Empty(t, fake.removedPaths())
	assert.True(t, fake.pairable(t))
	require.Error(t, window.Close(t.Context()), "the lost adapter's window cannot settle anything")

	servingPeripheralOwing(t, fake, addr, debts)
	assert.Equal(t, []dbus.ObjectPath{owedDevicePath}, fake.removedPaths(),
		"the unproven bond must be removed once the adapter works again")
	assert.False(t, fake.pairable(t), "the adapter must go back to not pairable")
	require.NoError(t, window.Close(t.Context()), "nothing is owed any more")
}

// A device that proves itself after the failed close keeps its bond, while
// one that did not still loses it.
func TestIntegration_BondCleanupSparesDeviceKeptSince(t *testing.T) {
	addr := startSessionBus(t)
	fake := newFakeBluez(t, addr)
	p := servingPeripheral(t, fake, addr)
	listDevice(fake, owedDevicePath, owedDeviceAddr)
	listDevice(fake, owedDevicePath2, owedDeviceAddr2)
	window := bondWindowWith(t, fake, p, owedDevicePath, owedDevicePath2)

	fake.failNextRemoves(2)
	require.Error(t, window.Close(t.Context()))
	require.Len(t, fake.removedPaths(), 2)
	window.Keep(Peer{Path: string(owedDevicePath)})

	require.NoError(t, window.Close(t.Context()))
	later := bondWindowWith(t, fake, p)
	require.NoError(t, later.Close(t.Context()))
	assert.Equal(t, []dbus.ObjectPath{owedDevicePath2}, fake.removedPaths()[2:],
		"only the device that never proved itself is removed")
}

// A bond bluetoothd no longer holds is not asked for again.
func TestIntegration_BondCleanupSkipsDeviceNoLongerBonded(t *testing.T) {
	addr := startSessionBus(t)
	fake := newFakeBluez(t, addr)
	p := servingPeripheral(t, fake, addr)
	unlisted := dbus.ObjectPath(string(fakeAdapterPath) + "/dev_AA_BB_CC_DD_EE_13")
	listDevice(fake, owedDevicePath, owedDeviceAddr)
	fake.mu.Lock()
	fake.objects[owedDevicePath2] = map[string]map[string]dbus.Variant{
		deviceIface: {"Address": dbus.MakeVariant(owedDeviceAddr2), "Paired": dbus.MakeVariant(false)},
	}
	fake.mu.Unlock()
	window := bondWindowWith(t, fake, p, owedDevicePath, owedDevicePath2, unlisted)

	fake.failNextRemoves(3)
	require.Error(t, window.Close(t.Context()))
	require.Len(t, fake.removedPaths(), 3)

	require.NoError(t, window.Close(t.Context()))
	assert.Equal(t, []dbus.ObjectPath{owedDevicePath}, fake.removedPaths()[3:],
		"a device that is gone or not paired has no bond to remove")
}

// Pairable the close could not switch back off is switched off by closing
// again.
func TestIntegration_BondCleanupRetriesFailedPairable(t *testing.T) {
	addr := startSessionBus(t)
	fake := newFakeBluez(t, addr)
	p := servingPeripheral(t, fake, addr)
	window := bondWindowWith(t, fake, p)

	fake.failNextPairable(1)
	err := window.Close(t.Context())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pairable")
	assert.True(t, fake.pairable(t))

	require.NoError(t, window.Close(t.Context()))
	assert.False(t, fake.pairable(t), "the adapter must go back to not pairable")
	assert.Equal(t, []bool{true, false, false}, fake.pairableHistory())
}

// A window opened while the restore is still owed takes it over: it leaves
// Pairable on while it is open and switches it off when it closes.
func TestIntegration_BondWindowTakesOverOwedPairable(t *testing.T) {
	addr := startSessionBus(t)
	fake := newFakeBluez(t, addr)
	p := servingPeripheral(t, fake, addr)
	window := bondWindowWith(t, fake, p)

	// The close fails, and so does the settle before the next window.
	fake.failNextPairable(2)
	require.Error(t, window.Close(t.Context()))
	next := bondWindowWith(t, fake, p)
	assert.True(t, fake.pairable(t))

	// Nothing is owed outside the open window, so the first one closing
	// again must not switch Pairable off under it.
	require.NoError(t, window.Close(t.Context()))
	assert.True(t, fake.pairable(t))

	require.NoError(t, next.Close(t.Context()))
	assert.False(t, fake.pairable(t), "Pairable was off before Core's first window and must end off")
}

// What the first close did is not done again when the rest is retried.
func TestIntegration_BondCleanupDoesNotRepeatWhatSucceeded(t *testing.T) {
	addr := startSessionBus(t)
	fake := newFakeBluez(t, addr)
	p := servingPeripheral(t, fake, addr)
	listDevice(fake, owedDevicePath, owedDeviceAddr)
	listDevice(fake, owedDevicePath2, owedDeviceAddr2)
	window := bondWindowWith(t, fake, p, owedDevicePath, owedDevicePath2)

	fake.failNextRemoves(1)
	require.Error(t, window.Close(t.Context()))
	first := fake.removedPaths()
	require.Len(t, first, 2)
	assert.False(t, fake.pairable(t))

	require.NoError(t, window.Close(t.Context()))
	assert.Equal(t, []dbus.ObjectPath{first[0], first[1], first[0]}, fake.removedPaths(),
		"only the removal that failed is tried again")
	assert.Equal(t, []bool{true, false}, fake.pairableHistory(), "Pairable was restored once and is left alone")
}

// What is owed to one adapter is never done to another.
func TestIntegration_BondCleanupIsPerAdapter(t *testing.T) {
	addr := startSessionBus(t)
	fake := newFakeBluez(t, addr)
	debts := newBondDebts()
	otherPath := bondDebtKey{path: "/org/bluez/hci1", address: "00:11:22:33:44:55"}
	otherDevice := dbus.ObjectPath("/org/bluez/hci1/dev_AA_BB_CC_DD_EE_11")
	debts.add(otherPath, true, []dbus.ObjectPath{otherDevice})
	// Another controller that took over this adapter's path.
	otherAddress := bondDebtKey{path: fakeAdapterPath, address: "66:77:88:99:AA:BB"}
	debts.add(otherAddress, true, []dbus.ObjectPath{owedDevicePath})
	listDevice(fake, owedDevicePath, owedDeviceAddr)
	listDevice(fake, otherDevice, owedDeviceAddr)
	require.Nil(t, fake.adapterProp.Set(adapterIface, "Pairable", dbus.MakeVariant(true)))

	p, _ := servingPeripheralOwing(t, fake, addr, debts)
	window := bondWindowWith(t, fake, p)
	require.NoError(t, window.Close(t.Context()))
	require.NoError(t, window.Close(t.Context()))

	assert.Empty(t, fake.removedPaths())
	assert.True(t, fake.pairable(t), "Pairable was on before the window and stays on")
	for _, key := range []bondDebtKey{otherPath, otherAddress} {
		pairableOff, devices := debts.snapshot(key)
		assert.True(t, pairableOff)
		assert.Len(t, devices, 1)
	}
}

func TestIsPairedDevice(t *testing.T) {
	t.Parallel()
	objects := managedObjects{
		"/org/bluez/hci0/dev_paired":   {deviceIface: {"Paired": dbus.MakeVariant(true)}},
		"/org/bluez/hci0/dev_unpaired": {deviceIface: {"Paired": dbus.MakeVariant(false)}},
		"/org/bluez/hci0/dev_unknown":  {deviceIface: {}},
		"/org/bluez/hci0/dev_other":    {gattServiceIface: {}},
	}
	assert.True(t, isPairedDevice(objects, "/org/bluez/hci0/dev_paired"))
	assert.False(t, isPairedDevice(objects, "/org/bluez/hci0/dev_unpaired"))
	assert.True(t, isPairedDevice(objects, "/org/bluez/hci0/dev_unknown"))
	assert.False(t, isPairedDevice(objects, "/org/bluez/hci0/dev_other"))
	assert.False(t, isPairedDevice(objects, "/org/bluez/hci0/dev_absent"))
}
