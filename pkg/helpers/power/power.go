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

// Package power reports whether the device is running on mains power or a
// battery, and how much of that battery is left. The updater uses it to
// refuse an install that could lose power part-way through.
package power

// Source is where the device is drawing power from.
type Source string

const (
	// SourceNoBattery means the hardware has no battery at all, so it is
	// running on whatever mains supply it always runs on.
	SourceNoBattery Source = "noBattery"
	// SourceExternal means a charger or dock is supplying power. The battery
	// percentage does not matter while this is true.
	SourceExternal Source = "external"
	// SourceBattery means the device is discharging and Percent is its
	// remaining charge.
	SourceBattery Source = "battery"
	// SourceUnknown means the hardware may have a battery but its state could
	// not be read. Callers must treat this as "could lose power at any
	// moment", not as "probably fine".
	SourceUnknown Source = "unknown"
)

// Status is a snapshot of where the device's power is coming from.
type Status struct {
	Source Source
	// Percent is the remaining charge, 0-100. It is only meaningful when
	// Source is SourceBattery.
	Percent int
}
