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
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	// batteryFlagCharging is the bit Windows sets while the battery is
	// filling.
	batteryFlagCharging = 8
	// batteryLifeTimeUnknown is what BatteryLifeTime holds when Windows has
	// no estimate, which includes every reading taken on AC power.
	batteryLifeTimeUnknown = 0xFFFFFFFF
	acLineOffline          = 0
)

// ReadDetail reports the system battery through the Windows power API, which
// folds every battery in the machine into one reading.
func ReadDetail() (Detail, error) {
	var raw systemPowerStatus
	proc := windows.NewLazySystemDLL("kernel32.dll").NewProc("GetSystemPowerStatus")
	//nolint:gosec // G103: the pointer is to a local struct the syscall fills in
	ret, _, err := proc.Call(uintptr(unsafe.Pointer(&raw)))
	if ret == 0 {
		return Detail{}, fmt.Errorf("reading system power status: %w", err)
	}
	return detailFromRaw(&raw), nil
}

func detailFromRaw(raw *systemPowerStatus) Detail {
	flagKnown := raw.BatteryFlag != batteryFlagUnknown
	if flagKnown && raw.BatteryFlag&batteryFlagNoBattery != 0 {
		return summarize(nil, true)
	}

	battery := Battery{ID: "system"}
	if raw.BatteryLifePercent <= 100 {
		percent := int(raw.BatteryLifePercent)
		battery.Percent = &percent
	}
	switch {
	case flagKnown && raw.BatteryFlag&batteryFlagCharging != 0:
		battery.State = ChargeCharging
	case raw.ACLineStatus == acLineOnline && battery.Percent != nil && *battery.Percent == 100:
		battery.State = ChargeFull
	case raw.ACLineStatus == acLineOnline:
		battery.State = ChargeNotCharging
	case raw.ACLineStatus == acLineOffline:
		battery.State = ChargeDischarging
		if raw.BatteryLifeTime != batteryLifeTimeUnknown {
			battery.TimeRemaining = time.Duration(raw.BatteryLifeTime) * time.Second
		}
	}
	return summarize([]Battery{battery}, raw.ACLineStatus == acLineOnline)
}
