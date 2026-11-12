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
	"errors"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// servingPeripheral opens the adapter on the fake and serves the test
// application.
func servingPeripheral(t *testing.T, fake *fakeBluez, addr string) *peripheral {
	t.Helper()
	p, _ := servingPeripheralApp(t, fake, addr)
	return p
}

// servingPeripheralApp is servingPeripheral that also returns where the
// application was registered.
func servingPeripheralApp(t *testing.T, fake *fakeBluez, addr string) (*peripheral, appRegistration) {
	t.Helper()
	return servingPeripheralOwing(t, fake, addr, newBondDebts())
}

// servingPeripheralOwing is servingPeripheralApp on a new adapter connection
// that shares debts, the way every connection of one process does.
func servingPeripheralOwing(
	t *testing.T, fake *fakeBluez, addr string, debts *bondDebts,
) (*peripheral, appRegistration) {
	t.Helper()
	adapter, err := Open(t.Context(), WithBusAddress(addr))
	require.NoError(t, err)
	t.Cleanup(func() { _ = adapter.Close() })
	p, err := adapter.Peripheral()
	require.NoError(t, err)
	lp, ok := p.(*peripheral)
	require.True(t, ok)
	// Keep what one test leaves owed away from the next.
	lp.debts = debts
	serveTestApplication(t, p)
	var reg appRegistration
	select {
	case reg = <-fake.appRoot:
	case <-time.After(integrationWait):
		t.Fatal("application was never registered")
	}
	fake.expectCall(t, "RegisterAdvertisement")
	// Keep the window's bond watch off the host's real adapter.
	lp.openBondEvents = func() (mgmtConn, error) { return nil, errors.New("no management socket in tests") }
	lp.linkRole = func(uint16, [6]byte) (bool, error) { return false, errors.New("no links in tests") }
	return lp, reg
}

// openBondWindow opens a bonding window and returns the agent it registered.
func openBondWindow(t *testing.T, fake *fakeBluez, p *peripheral) appRegistration {
	t.Helper()
	window, err := p.AllowBonding(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { _ = window.Close(t.Context()) })
	select {
	case agent := <-fake.agents:
		return agent
	case <-time.After(integrationWait):
		t.Fatal("the agent was never made the default")
		return appRegistration{}
	}
}

// sawCall reports whether the fake records want within wait.
func sawCall(fake *fakeBluez, want string, wait time.Duration) bool {
	deadline := time.After(wait)
	for {
		select {
		case got := <-fake.calls:
			if got == want {
				return true
			}
		case <-deadline:
			return false
		}
	}
}

// bluetoothd can leave the phone's own requests unanswered while it raises
// the link's security, so a device that bonds and never reaches Core's
// service is disconnected to reconnect bonded.
func TestIntegration_BondWindowDropsStalledPeer(t *testing.T) {
	addr := startSessionBus(t)
	fake := newFakeBluez(t, addr)
	p := servingPeripheral(t, fake, addr)
	p.bondStallTimeout = 100 * time.Millisecond
	agent := openBondWindow(t, fake, p)

	require.NoError(t, authorize(fake, agent, fakeDevicePath))
	assert.True(t, sawCall(fake, "Disconnect", integrationWait),
		"a bonded device that never used Core's service must be disconnected")
}

func TestIntegration_BondWindowKeepsPeerThatUsesService(t *testing.T) {
	addr := startSessionBus(t)
	fake := newFakeBluez(t, addr)
	p, reg := servingPeripheralApp(t, fake, addr)
	p.bondStallTimeout = 200 * time.Millisecond
	agent := openBondWindow(t, fake, p)

	require.NoError(t, authorize(fake, agent, fakeDevicePath))
	opts := map[string]dbus.Variant{"device": dbus.MakeVariant(fakeDevicePath)}
	require.NoError(t, fake.conn.Object(string(reg.sender), reg.path+"/service0/char0").
		Call(gattCharIface+".ReadValue", 0, opts).Err)
	assert.False(t, sawCall(fake, "Disconnect", time.Second),
		"a device that reached Core's service must stay connected")

	p.mu.Lock()
	defer p.mu.Unlock()
	assert.Empty(t, p.useWaiters, "a met wait is dropped")
}

// A use before the bond does not count: the stalled request comes after it.
func TestIntegration_BondWindowIgnoresUseBeforeBond(t *testing.T) {
	addr := startSessionBus(t)
	fake := newFakeBluez(t, addr)
	p, reg := servingPeripheralApp(t, fake, addr)
	p.bondStallTimeout = 100 * time.Millisecond
	agent := openBondWindow(t, fake, p)

	opts := map[string]dbus.Variant{"device": dbus.MakeVariant(fakeDevicePath)}
	require.NoError(t, fake.conn.Object(string(reg.sender), reg.path+"/service0/char0").
		Call(gattCharIface+".ReadValue", 0, opts).Err)
	require.NoError(t, authorize(fake, agent, fakeDevicePath))
	assert.True(t, sawCall(fake, "Disconnect", integrationWait))

	p.mu.Lock()
	defer p.mu.Unlock()
	assert.Empty(t, p.useWaiters, "an expired wait is dropped")
}

func authorize(fake *fakeBluez, agent appRegistration, device dbus.ObjectPath) error {
	return fake.conn.Object(string(agent.sender), agent.path).
		Call(agentIface+".RequestAuthorization", 0, device).Err
}

func isRejected(err error) bool {
	var dbusErr dbus.Error
	return errors.As(err, &dbusErr) && dbusErr.Name == bluezErrRejected
}

// removedDevices collects the RemoveDevice calls the fake saw.
func removedDevices(fake *fakeBluez) map[string]bool {
	removed := map[string]bool{}
	deadline := time.After(300 * time.Millisecond)
	for {
		select {
		case call := <-fake.calls:
			removed[call] = true
		case <-deadline:
			return removed
		}
	}
}

func TestIntegration_BondWindow(t *testing.T) {
	addr := startSessionBus(t)
	fake := newFakeBluez(t, addr)
	p := servingPeripheral(t, fake, addr)

	var bonder Bonder = p
	window, err := bonder.AllowBonding(t.Context())
	require.NoError(t, err)

	var agent appRegistration
	select {
	case agent = <-fake.agents:
	case <-time.After(integrationWait):
		t.Fatal("the agent was never made the default")
	}
	fake.expectCall(t, "RegisterAgent:"+string(agent.path)+":NoInputNoOutput")
	pairable, dbusErr := fake.adapterProp.Get(adapterIface, "Pairable")
	if dbusErr != nil {
		t.Fatal(dbusErr)
	}
	assert.Equal(t, true, pairable.Value())

	// Other prompts are refused outright; displaying a passkey is harmless.
	obj := fake.conn.Object(string(agent.sender), agent.path)
	assert.True(t, isRejected(obj.Call(agentIface+".RequestConfirmation", 0, fakeDevicePath, uint32(1)).Err))
	assert.True(t, isRejected(obj.Call(agentIface+".AuthorizeService", 0, fakeDevicePath, "uuid").Err))
	assert.True(t, isRejected(obj.Call(agentIface+".RequestPinCode", 0, fakeDevicePath).Err))
	assert.True(t, isRejected(obj.Call(agentIface+".RequestPasskey", 0, fakeDevicePath).Err))
	assert.True(t, isRejected(obj.Call(agentIface+".DisplayPinCode", 0, fakeDevicePath, "0000").Err))
	require.NoError(t, obj.Call(agentIface+".DisplayPasskey", 0, fakeDevicePath, uint32(1), uint16(0)).Err)
	require.NoError(t, obj.Call(agentIface+".Cancel", 0).Err)

	// Any device may bond while the window is open, a stranger included, and
	// one whose first attempt failed may pair again.
	stranger := dbus.ObjectPath(string(fakeAdapterPath) + "/dev_AA_BB_CC_DD_EE_66")
	require.NoError(t, authorize(fake, agent, stranger))
	require.NoError(t, authorize(fake, agent, fakeDevicePath))
	require.NoError(t, authorize(fake, agent, fakeDevicePath))

	kept := dbus.ObjectPath(string(fakeAdapterPath) + "/dev_AA_BB_CC_DD_EE_02")
	require.NoError(t, authorize(fake, agent, kept))
	window.Keep(Peer{Path: string(kept)})

	// A device bluetoothd already forgot is not an error.
	gone := fakeDevicePath + "_gone"
	require.NoError(t, authorize(fake, agent, gone))

	// Keeping a device that never bonded through the window removes nothing.
	never := dbus.ObjectPath(string(fakeAdapterPath) + "/dev_AA_BB_CC_DD_EE_03")
	window.Keep(Peer{Path: string(never)})

	require.NoError(t, window.Close(t.Context()))
	fake.expectCall(t, "UnregisterAgent:"+string(agent.path))
	pairable, dbusErr = fake.adapterProp.Get(adapterIface, "Pairable")
	if dbusErr != nil {
		t.Fatal(dbusErr)
	}
	assert.Equal(t, false, pairable.Value(), "the adapter goes back to not pairable")

	removed := removedDevices(fake)
	assert.True(t, removed["RemoveDevice:"+string(fakeDevicePath)], "an unproven bond must be removed")
	assert.True(t, removed["RemoveDevice:"+string(stranger)], "a stranger's bond must be removed")
	assert.True(t, removed["RemoveDevice:"+string(gone)], "removal is attempted for every unproven bond")
	assert.False(t, removed["RemoveDevice:"+string(kept)], "a kept bond must stay")
	assert.False(t, removed["RemoveDevice:"+string(never)], "a device that never bonded is left alone")

	// The agent is gone from the bus, and closing again is harmless.
	require.Error(t, authorize(fake, agent, fakeDevicePath))
	require.NoError(t, window.Close(t.Context()))
}

// A pairing that reaches the agent after the window started closing is
// refused, and one accepted before is still removed.
func TestBondWindow_AuthorizeAfterCloseIsRefused(t *testing.T) {
	a := &adapter{gone: make(chan struct{})}
	t.Cleanup(func() { close(a.gone) })
	w := &bondWindow{
		p:        newPeripheral(a),
		closed:   make(chan struct{}),
		accepted: make(map[dbus.ObjectPath]bool),
		kept:     make(map[dbus.ObjectPath]bool),
	}
	before := dbus.ObjectPath(string(fakeAdapterPath) + "/dev_AA_BB_CC_DD_EE_04")
	assert.True(t, w.authorize(before))
	close(w.closed)
	after := dbus.ObjectPath(string(fakeAdapterPath) + "/dev_AA_BB_CC_DD_EE_05")
	assert.False(t, w.authorize(after))
	assert.Equal(t, map[dbus.ObjectPath]bool{before: true}, w.accepted)
}

func TestIntegration_BondWindowLeavesPairableItFoundOn(t *testing.T) {
	addr := startSessionBus(t)
	fake := newFakeBluez(t, addr)
	p := servingPeripheral(t, fake, addr)
	require.Nil(t, fake.adapterProp.Set(adapterIface, "Pairable", dbus.MakeVariant(true)))

	window, err := p.AllowBonding(t.Context())
	require.NoError(t, err)
	require.NoError(t, window.Close(t.Context()))
	pairable, dbusErr := fake.adapterProp.Get(adapterIface, "Pairable")
	require.Nil(t, dbusErr)
	assert.Equal(t, true, pairable.Value(), "Pairable was on before the window and stays on")
}
