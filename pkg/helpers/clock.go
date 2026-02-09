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

package helpers

import (
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
)

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

// SleepWakeMonitor detects system sleep/wake events by monitoring wall clock jumps.
// When the system sleeps, the monotonic clock pauses but wall clock continues.
// On wake, the wall clock will have jumped forward significantly more than expected.
//
// Usage:
//
//	monitor := NewSleepWakeMonitor(5 * time.Second)
//	for {
//	    if monitor.Check() {
//	        // Handle wake from sleep
//	    }
//	    time.Sleep(1 * time.Second)
//	}
type SleepWakeMonitor struct {
	lastCheck time.Time
	threshold time.Duration
	mu        syncutil.Mutex
}

// NewSleepWakeMonitor creates a monitor that detects time jumps exceeding threshold.
// Recommended threshold: 5 seconds for fast polling loops (allows for normal
// scheduling delays), or match the polling interval for slower loops.
func NewSleepWakeMonitor(threshold time.Duration) *SleepWakeMonitor {
	return &SleepWakeMonitor{
		// Round(0) strips monotonic clock reading, forcing wall clock comparison
		lastCheck: time.Now().Round(0),
		threshold: threshold,
	}
}

// Check returns true if a wake-from-sleep was likely detected since last check.
// Should be called regularly (e.g., every 100ms-1s) from a polling loop.
// Thread-safe for concurrent access.
func (m *SleepWakeMonitor) Check() bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Round(0) strips the monotonic clock reading, forcing wall clock comparison.
	// IMPORTANT: Go's time.Sub() uses monotonic time when available,
	// but monotonic clock pauses during system sleep. We need wall clock
	// to detect the time jump that occurs during sleep.
	now := time.Now().Round(0)
	wasReliable := IsClockReliable(m.lastCheck)
	elapsed := now.Sub(m.lastCheck)
	m.lastCheck = now

	// If the previous check was from an unreliable clock (e.g. epoch on MiSTer
	// before NTP sync), the elapsed time is meaningless — an NTP clock jump
	// from 1970 to 2025 is not a wake-from-sleep event.
	if !wasReliable {
		return false
	}

	// If wall clock jumped more than threshold, we likely woke from sleep
	return elapsed > m.threshold
}

// Reset resets the monitor's last check time to now.
// Useful after handling a wake event to prevent false positives,
// or when resuming monitoring after a pause.
func (m *SleepWakeMonitor) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastCheck = time.Now().Round(0)
}

// SetLastCheckForTesting sets the last check time for testing purposes.
// This allows tests to simulate time jumps without accessing private fields.
func (m *SleepWakeMonitor) SetLastCheckForTesting(t time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastCheck = t.Round(0)
}
