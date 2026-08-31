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

package power

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// systemPowerStatus mirrors the SYSTEM_POWER_STATUS structure filled in by
// GetSystemPowerStatus.
type systemPowerStatus struct {
	ACLineStatus        byte
	BatteryFlag         byte
	BatteryLifePercent  byte
	SystemStatusFlag    byte
	BatteryLifeTime     uint32
	BatteryFullLifeTime uint32
}

const (
	acLineOnline = 1

	// batteryFlagNoBattery is the bit Windows sets when the machine has no
	// system battery.
	batteryFlagNoBattery = 128
	// batteryFlagUnknown is the whole byte Windows returns when it cannot
	// determine the battery state.
	batteryFlagUnknown = 255
	// batteryPercentUnknown is what BatteryLifePercent holds when the charge
	// is not known.
	batteryPercentUnknown = 255
)

// Read reports the device's power state through the Windows power API.
func Read() (Status, error) {
	var raw systemPowerStatus
	proc := windows.NewLazySystemDLL("kernel32.dll").NewProc("GetSystemPowerStatus")
	//nolint:gosec // G103: the pointer is to a local struct the syscall fills in
	ret, _, err := proc.Call(uintptr(unsafe.Pointer(&raw)))
	if ret == 0 {
		return Status{Source: SourceUnknown}, fmt.Errorf("reading system power status: %w", err)
	}
	return statusFrom(&raw), nil
}

// statusFrom resolves the four states the updater distinguishes from one
// SYSTEM_POWER_STATUS reading.
func statusFrom(raw *systemPowerStatus) Status {
	if raw.BatteryFlag != batteryFlagUnknown && raw.BatteryFlag&batteryFlagNoBattery != 0 {
		return Status{Source: SourceNoBattery}
	}
	if raw.ACLineStatus == acLineOnline {
		return Status{Source: SourceExternal}
	}
	if raw.BatteryLifePercent <= 100 && raw.BatteryLifePercent != batteryPercentUnknown {
		return Status{Source: SourceBattery, Percent: int(raw.BatteryLifePercent)}
	}
	return Status{Source: SourceUnknown}
}
