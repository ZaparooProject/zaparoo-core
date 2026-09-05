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

package bluez

import (
	"testing"

	"github.com/godbus/dbus/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPropertiesChangedDecoder(t *testing.T) {
	t.Parallel()

	sig := &dbus.Signal{
		Name: signalPropertiesChanged,
		Path: "/org/bluez/hci0/dev_AA_BB_CC_DD_EE_FF",
		Body: []any{
			deviceIface,
			map[string]dbus.Variant{"Connected": dbus.MakeVariant(false)},
			[]string{},
		},
	}
	iface, changed, ok := propertiesChanged(sig)
	require.True(t, ok)
	assert.Equal(t, deviceIface, iface)
	connected, present := changedBool(changed, "Connected")
	assert.True(t, present)
	assert.False(t, connected)
	_, present = changedBool(changed, "ServicesResolved")
	assert.False(t, present)

	_, _, ok = propertiesChanged(&dbus.Signal{Name: signalInterfacesAdded, Body: sig.Body})
	assert.False(t, ok, "wrong signal name")
	_, _, ok = propertiesChanged(&dbus.Signal{Name: signalPropertiesChanged, Body: []any{deviceIface}})
	assert.False(t, ok, "short body")
	_, _, ok = propertiesChanged(&dbus.Signal{Name: signalPropertiesChanged, Body: []any{1, 2, 3}})
	assert.False(t, ok, "wrong types")
	_, _, ok = propertiesChanged(nil)
	assert.False(t, ok)
}

func TestChangedBytes(t *testing.T) {
	t.Parallel()

	changed := map[string]dbus.Variant{
		"Value": dbus.MakeVariant([]byte("SCAN\tuid=1\n")),
		"MTU":   dbus.MakeVariant(uint16(185)),
	}
	value, ok := changedBytes(changed, "Value")
	require.True(t, ok)
	assert.Equal(t, []byte("SCAN\tuid=1\n"), value)
	_, ok = changedBytes(changed, "MTU")
	assert.False(t, ok, "not a byte array")
	_, ok = changedBytes(changed, "Missing")
	assert.False(t, ok)
}

func TestInterfacesAddedDecoder(t *testing.T) {
	t.Parallel()

	path := dbus.ObjectPath("/org/bluez/hci0/dev_AA_BB_CC_DD_EE_FF")
	sig := &dbus.Signal{
		Name: signalInterfacesAdded,
		Path: "/",
		Body: []any{
			path,
			map[string]map[string]dbus.Variant{
				deviceIface: {"Address": dbus.MakeVariant("AA:BB:CC:DD:EE:FF")},
			},
		},
	}
	gotPath, ifaces, ok := interfacesAdded(sig)
	require.True(t, ok)
	assert.Equal(t, path, gotPath)
	assert.Equal(t, "AA:BB:CC:DD:EE:FF", stringProp(ifaces[deviceIface], "Address"))
	assert.Empty(t, stringProp(ifaces[deviceIface], "Name"))
	assert.Empty(t, stringProp(nil, "Address"))

	_, _, ok = interfacesAdded(&dbus.Signal{Name: signalInterfacesAdded, Body: []any{"not a path", 1}})
	assert.False(t, ok)
}

func TestInterfacesRemovedDecoder(t *testing.T) {
	t.Parallel()

	path := dbus.ObjectPath("/org/bluez/hci0")
	sig := &dbus.Signal{
		Name: signalInterfacesRemoved,
		Path: "/",
		Body: []any{path, []string{adapterIface, gattManagerIface}},
	}
	gotPath, ifaces, ok := interfacesRemoved(sig)
	require.True(t, ok)
	assert.Equal(t, path, gotPath)
	assert.Contains(t, ifaces, adapterIface)

	_, _, ok = interfacesRemoved(&dbus.Signal{Name: signalInterfacesRemoved, Body: []any{path}})
	assert.False(t, ok, "short body")
}

func TestPeerFromPath(t *testing.T) {
	t.Parallel()

	peer := peerFromPath("/org/bluez/hci0/dev_AA_BB_CC_DD_EE_FF")
	assert.Equal(t, "/org/bluez/hci0/dev_AA_BB_CC_DD_EE_FF", peer.Path)
	assert.Equal(t, "AA:BB:CC:DD:EE:FF", peer.Address)

	assert.Empty(t, addressFromPath("/org/bluez/hci0"))
	assert.Equal(t, Peer{}, peerFromOptions(nil))
	assert.Equal(t, Peer{}, peerFromOptions(map[string]dbus.Variant{"device": dbus.MakeVariant("string not path")}))
	assert.Equal(t, peer, peerFromOptions(map[string]dbus.Variant{
		"device": dbus.MakeVariant(dbus.ObjectPath(peer.Path)),
	}))
}

func TestMapBusError(t *testing.T) {
	t.Parallel()

	unknown := dbus.Error{Name: "org.freedesktop.DBus.Error.ServiceUnknown"}
	require.ErrorIs(t, mapBusError("op", unknown), ErrUnavailable)
	noObject := dbus.Error{Name: "org.freedesktop.DBus.Error.UnknownObject"}
	require.ErrorIs(t, mapBusError("op", noObject), ErrNotFound)
	other := dbus.Error{Name: "org.bluez.Error.Failed"}
	err := mapBusError("op", other)
	require.NotErrorIs(t, err, ErrUnavailable)
	var dbusErr dbus.Error
	require.ErrorAs(t, err, &dbusErr)
	assert.Equal(t, other.Name, dbusErr.Name)
}

func TestSignalRouter(t *testing.T) {
	t.Parallel()

	r := newSignalRouter()
	in := make(chan *dbus.Signal, 4)
	go r.run(in)

	all, cancelAll := r.subscribe(func(*dbus.Signal) bool { return true })
	added, cancelAdded := r.subscribe(func(sig *dbus.Signal) bool { return sig.Name == signalInterfacesAdded })

	in <- &dbus.Signal{Name: signalPropertiesChanged}
	in <- &dbus.Signal{Name: signalInterfacesAdded}
	assert.Equal(t, signalPropertiesChanged, (<-all).Name)
	assert.Equal(t, signalInterfacesAdded, (<-all).Name)
	assert.Equal(t, signalInterfacesAdded, (<-added).Name)

	cancelAdded()
	_, open := <-added
	assert.False(t, open, "cancel closes the channel")
	cancelAdded()

	close(in)
	_, open = <-all
	assert.False(t, open, "closing the input closes every subscriber")
	cancelAll()

	late, cancelLate := r.subscribe(func(*dbus.Signal) bool { return true })
	_, open = <-late
	assert.False(t, open, "subscribing after close yields a closed channel")
	cancelLate()
}
