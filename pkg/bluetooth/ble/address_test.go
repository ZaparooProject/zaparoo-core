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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAddressUint64RoundTrip(t *testing.T) {
	t.Parallel()

	v, err := addressToUint64("2c:cf:67:5e:fe:7d")
	require.NoError(t, err)
	assert.Equal(t, uint64(0x2CCF675EFE7D), v)
	assert.Equal(t, "2C:CF:67:5E:FE:7D", addressFromUint64(v))
	assert.Equal(t, "00:1A:7D:DA:71:14", addressFromUint64(0x001A7DDA7114))

	_, err = addressToUint64("not an address")
	require.Error(t, err)
}

func TestAddressFromDeviceID(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "2C:CF:67:5E:FE:7D",
		addressFromDeviceID("BluetoothLE#BluetoothLE00:1a:7d:da:71:14-2c:cf:67:5e:fe:7d"))
	assert.Empty(t, addressFromDeviceID("BluetoothLE#something-else"))
	assert.Empty(t, addressFromDeviceID("short"))
	assert.Empty(t, addressFromDeviceID(""))
}

func TestServiceUUIDsFromSection(t *testing.T) {
	t.Parallel()

	// The Zaparoo service, as it travels: least significant byte first.
	zaparoo := []byte{
		0x38, 0xa6, 0xb6, 0x34, 0x7d, 0x47, 0x6f, 0x83,
		0x3b, 0x44, 0x59, 0xb3, 0x01, 0x00, 0xa7, 0x0d,
	}
	assert.Equal(t, []string{"0da70001-b359-443b-836f-477d34b6a638"}, serviceUUIDsFromSection(0x07, zaparoo))
	assert.Equal(t, []string{"0da70001-b359-443b-836f-477d34b6a638"}, serviceUUIDsFromSection(0x06, zaparoo))

	// Two 16-bit UUIDs: battery and device information.
	assert.Equal(t,
		[]string{"0000180f-0000-1000-8000-00805f9b34fb", "0000180a-0000-1000-8000-00805f9b34fb"},
		serviceUUIDsFromSection(0x03, []byte{0x0f, 0x18, 0x0a, 0x18}))

	// Truncated and unrelated sections yield nothing rather than garbage.
	assert.Empty(t, serviceUUIDsFromSection(0x07, zaparoo[:15]))
	assert.Empty(t, serviceUUIDsFromSection(0x03, []byte{0x0f}))
	assert.Empty(t, serviceUUIDsFromSection(0x09, []byte("a name")))
}
