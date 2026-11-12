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

package hoststatus

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsVirtualAdapter(t *testing.T) {
	t.Parallel()

	assert.True(t, isVirtualAdapter("Microsoft Wi-Fi Direct Virtual Adapter #2"))
	assert.True(t, isVirtualAdapter("Bluetooth Device (Personal Area Network)"))
	assert.True(t, isVirtualAdapter("Hyper-V Virtual Ethernet Adapter"))
	assert.False(t, isVirtualAdapter("Realtek PCIe GbE Family Controller"))
	assert.False(t, isVirtualAdapter("Realtek 8822CE Wireless LAN 802.11ac PCI-E NIC"))
	assert.False(t, isVirtualAdapter(""))
}

func TestXinputBatteryInfo(t *testing.T) {
	t.Parallel()

	connection, battery := xinputBatteryInfo(xinputBattery{batteryType: xinputBatteryWired})
	assert.Equal(t, ConnectionUSB, connection)
	assert.Nil(t, battery)

	connection, battery = xinputBatteryInfo(xinputBattery{batteryType: 0x03, batteryLevel: 2})
	assert.Equal(t, ConnectionUnknown, connection)
	if assert.NotNil(t, battery) {
		assert.Equal(t, BatteryLevelMedium, battery.Level)
		assert.Nil(t, battery.Percent)
	}

	_, battery = xinputBatteryInfo(xinputBattery{batteryType: xinputBatteryUnknown})
	assert.Nil(t, battery)
	_, battery = xinputBatteryInfo(xinputBattery{batteryType: xinputBatteryDisconnected})
	assert.Nil(t, battery)
	_, battery = xinputBatteryInfo(xinputBattery{batteryType: 0x02, batteryLevel: 9})
	assert.Nil(t, battery)
}
