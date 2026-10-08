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
	"errors"
	"time"
)

// ErrDetailUnsupported is returned by ReadDetail on a build that has no way to
// ask the hardware about its batteries.
var ErrDetailUnsupported = errors.New("battery detail is not supported on this platform")

// ChargeState is what a battery is doing with the power available to it. The
// empty value means the state could not be read.
type ChargeState string

const (
	ChargeCharging    ChargeState = "charging"
	ChargeDischarging ChargeState = "discharging"
	ChargeFull        ChargeState = "full"
	// ChargeNotCharging is a battery on external power that is neither filling
	// nor full, which is what a charge limit or a weak charger produces.
	ChargeNotCharging ChargeState = "notCharging"
)

// Battery is one battery the device itself runs on.
type Battery struct {
	// Percent is the remaining charge, 0-100, or nil when it cannot be read.
	Percent *int
	ID      string
	State   ChargeState
	// TimeRemaining is the estimated time until empty. Zero means unknown.
	TimeRemaining time.Duration
}

// Detail is the battery reading a status display needs. Unlike Status it keeps
// the charge level while the device is on external power, and it never folds
// an unreadable battery into a single fail-safe answer: each field that could
// not be read is left empty.
type Detail struct {
	// Percent is the lowest readable charge across Batteries, the one that
	// decides when the device dies.
	Percent *int
	// Source is SourceBattery or SourceExternal. It is empty when the supply
	// could not be determined.
	Source    Source
	State     ChargeState
	Batteries []Battery
	// TimeRemaining is the shortest estimate across discharging batteries.
	TimeRemaining time.Duration
	// Present reports whether the device has a battery of its own.
	Present bool
}

// summarize fills the device-level fields of a Detail from its batteries.
// external is whether a supply other than a battery announced itself.
func summarize(batteries []Battery, external bool) Detail {
	detail := Detail{Batteries: batteries, Present: len(batteries) > 0}
	if !detail.Present {
		detail.Source = SourceExternal
		return detail
	}

	var charging, discharging, full, idle int
	for i := range batteries {
		battery := &batteries[i]
		if battery.Percent != nil && (detail.Percent == nil || *battery.Percent < *detail.Percent) {
			percent := *battery.Percent
			detail.Percent = &percent
		}
		switch battery.State {
		case ChargeCharging:
			charging++
		case ChargeDischarging:
			discharging++
			remaining := battery.TimeRemaining
			if remaining > 0 && (detail.TimeRemaining == 0 || remaining < detail.TimeRemaining) {
				detail.TimeRemaining = remaining
			}
		case ChargeFull:
			full++
		case ChargeNotCharging:
			idle++
		}
	}

	switch {
	case charging > 0:
		detail.State = ChargeCharging
	case discharging > 0:
		detail.State = ChargeDischarging
	case idle > 0:
		detail.State = ChargeNotCharging
	case full > 0:
		detail.State = ChargeFull
	}

	switch {
	case external || charging > 0 || full > 0 || idle > 0:
		detail.Source = SourceExternal
	case discharging > 0:
		detail.Source = SourceBattery
	}
	if detail.Source != SourceBattery {
		detail.TimeRemaining = 0
	}
	return detail
}
