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
	"fmt"
	"slices"
	"strings"
)

// bluetoothBaseUUID is what a 16-bit service UUID is shorthand for.
const bluetoothBaseUUID = "0000%04x-0000-1000-8000-00805f9b34fb"

// addressToUint64 packs a normalised Bluetooth address into the 48-bit
// integer some stacks use, most significant byte first.
func addressToUint64(address string) (uint64, error) {
	addr, err := NormalizeAddress(address)
	if err != nil {
		return 0, err
	}
	var out uint64
	if _, err := fmt.Sscanf(strings.ReplaceAll(addr, ":", ""), "%012X", &out); err != nil {
		return 0, fmt.Errorf("invalid bluetooth address %q", address)
	}
	return out, nil
}

// addressFromUint64 is the inverse of addressToUint64.
func addressFromUint64(v uint64) string {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], v)
	return fmt.Sprintf("%02X:%02X:%02X:%02X:%02X:%02X", b[2], b[3], b[4], b[5], b[6], b[7])
}

// addressFromDeviceID recovers a remote device's address from a WinRT
// device identifier such as
// "BluetoothLE#BluetoothLE00:1a:7d:da:71:14-2c:cf:67:5e:fe:7d", where the
// local adapter comes first and the remote device last. It returns "" when
// the identifier does not end in an address.
func addressFromDeviceID(id string) string {
	const addrLen = len("00:00:00:00:00:00")
	if len(id) < addrLen {
		return ""
	}
	addr, err := NormalizeAddress(id[len(id)-addrLen:])
	if err != nil {
		return ""
	}
	return addr
}

// serviceUUIDsFromSection reads the service UUIDs out of one advertising
// data section, given its type and contents. Sections of other types yield
// nothing.
func serviceUUIDsFromSection(dataType uint8, data []byte) []string {
	const (
		incomplete16  = 0x02
		complete16    = 0x03
		incomplete128 = 0x06
		complete128   = 0x07
	)
	var uuids []string
	switch dataType {
	case incomplete16, complete16:
		for pair := range slices.Chunk(data, 2) {
			if len(pair) == 2 {
				uuids = append(uuids, fmt.Sprintf(bluetoothBaseUUID, binary.LittleEndian.Uint16(pair)))
			}
		}
	case incomplete128, complete128:
		for raw := range slices.Chunk(data, 16) {
			if len(raw) != 16 {
				continue
			}
			// Sent least significant byte first.
			b := slices.Clone(raw)
			slices.Reverse(b)
			uuids = append(uuids, fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]))
		}
	}
	return uuids
}
