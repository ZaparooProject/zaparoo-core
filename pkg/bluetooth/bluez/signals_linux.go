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

import "github.com/godbus/dbus/v5"

// propertiesChanged decodes a PropertiesChanged signal into the interface it
// concerns and the changed values.
func propertiesChanged(sig *dbus.Signal) (iface string, changed map[string]dbus.Variant, ok bool) {
	if sig == nil || sig.Name != signalPropertiesChanged || len(sig.Body) < 2 {
		return "", nil, false
	}
	iface, ok = sig.Body[0].(string)
	if !ok {
		return "", nil, false
	}
	changed, ok = sig.Body[1].(map[string]dbus.Variant)
	if !ok {
		return "", nil, false
	}
	return iface, changed, true
}

// interfacesAdded decodes an InterfacesAdded signal into the object path and
// the interfaces (with their properties) that appeared on it.
func interfacesAdded(sig *dbus.Signal) (path dbus.ObjectPath, ifaces map[string]map[string]dbus.Variant, ok bool) {
	if sig == nil || sig.Name != signalInterfacesAdded || len(sig.Body) < 2 {
		return "", nil, false
	}
	path, ok = sig.Body[0].(dbus.ObjectPath)
	if !ok {
		return "", nil, false
	}
	ifaces, ok = sig.Body[1].(map[string]map[string]dbus.Variant)
	if !ok {
		return "", nil, false
	}
	return path, ifaces, true
}

// interfacesRemoved decodes an InterfacesRemoved signal into the object path
// and the interface names that vanished from it.
func interfacesRemoved(sig *dbus.Signal) (path dbus.ObjectPath, ifaces []string, ok bool) {
	if sig == nil || sig.Name != signalInterfacesRemoved || len(sig.Body) < 2 {
		return "", nil, false
	}
	path, ok = sig.Body[0].(dbus.ObjectPath)
	if !ok {
		return "", nil, false
	}
	ifaces, ok = sig.Body[1].([]string)
	if !ok {
		return "", nil, false
	}
	return path, ifaces, true
}

// changedBool reports a boolean property from a PropertiesChanged payload.
func changedBool(changed map[string]dbus.Variant, name string) (value, present bool) {
	v, ok := changed[name]
	if !ok {
		return false, false
	}
	value, ok = v.Value().(bool)
	return value, ok
}

// changedBytes reports a byte-array property from a PropertiesChanged payload.
func changedBytes(changed map[string]dbus.Variant, name string) ([]byte, bool) {
	v, ok := changed[name]
	if !ok {
		return nil, false
	}
	b, ok := v.Value().([]byte)
	return b, ok
}
