// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
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

package helpers

import "time"

const (
	// MinReliableYear is the earliest year considered valid for the system clock.
	// Zaparoo Core v2 was released in 2024 - any earlier date indicates an unset clock.
	MinReliableYear = 2024
)

// ClockSource values indicate how a timestamp was determined
const (
	// ClockSourceSystem indicates the timestamp came from a system clock that appeared reliable.
	// This could be NTP, RTC, or manually set - we don't distinguish at record creation time.
	ClockSourceSystem = "system"

	// ClockSourceEpoch indicates the timestamp came from an unreliable clock (year < 2024).
	// Common on MiSTer devices that boot without RTC and haven't synced NTP yet.
	ClockSourceEpoch = "epoch"

	// ClockSourceHealed indicates the timestamp was mathematically reconstructed.
	// Original timestamp was unreliable, but was later corrected using:
	// TrueTimestamp = TrueBootTime + MonotonicOffset
	ClockSourceHealed = "healed"
)

// IsClockReliable checks if the system clock appears to be set correctly.
// Returns false if the clock is clearly wrong (e.g., year < 2024).
// This handles MiSTer's lack of RTC chip - clock often resets to epoch on boot.
func IsClockReliable(t time.Time) bool {
	return t.Year() >= MinReliableYear
}
