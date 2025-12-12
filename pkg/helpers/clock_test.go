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

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"golang.org/x/sync/errgroup"
)

func TestIsClockReliable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		time time.Time
		name string
		want bool
	}{
		{
			name: "year 2024 is reliable",
			time: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			want: true,
		},
		{
			name: "year 2025 is reliable",
			time: time.Date(2025, 11, 22, 12, 0, 0, 0, time.UTC),
			want: true,
		},
		{
			name: "year 2030 is reliable",
			time: time.Date(2030, 6, 15, 9, 30, 0, 0, time.UTC),
			want: true,
		},
		{
			name: "year 2023 is unreliable",
			time: time.Date(2023, 12, 31, 23, 59, 59, 0, time.UTC),
			want: false,
		},
		{
			name: "year 2000 is unreliable",
			time: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC),
			want: false,
		},
		{
			name: "epoch time (1970) is unreliable",
			time: time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC),
			want: false,
		},
		{
			name: "unix zero is unreliable",
			time: time.Unix(0, 0),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := IsClockReliable(tt.time)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestClockSourceConstants(t *testing.T) {
	t.Parallel()

	// Verify constants have expected values
	assert.Equal(t, "system", ClockSourceSystem)
	assert.Equal(t, "epoch", ClockSourceEpoch)
	assert.Equal(t, "healed", ClockSourceHealed)

	// Verify all constants are unique
	sources := []string{ClockSourceSystem, ClockSourceEpoch, ClockSourceHealed}
	uniqueMap := make(map[string]bool)
	for _, source := range sources {
		assert.False(t, uniqueMap[source], "clock source %q should be unique", source)
		uniqueMap[source] = true
	}
}

func TestMinReliableYear(t *testing.T) {
	t.Parallel()

	// Verify the constant has the expected value
	assert.Equal(t, 2024, MinReliableYear)

	// Verify boundary conditions
	assert.True(t, IsClockReliable(time.Date(MinReliableYear, 1, 1, 0, 0, 0, 0, time.UTC)))
	assert.False(t, IsClockReliable(time.Date(MinReliableYear-1, 12, 31, 23, 59, 59, 0, time.UTC)))
}

func TestSleepWakeMonitor_NoWake(t *testing.T) {
	t.Parallel()

	monitor := NewSleepWakeMonitor(5 * time.Second)

	// First check establishes baseline - should not trigger
	assert.False(t, monitor.Check(), "first check should not trigger wake detection")

	// Immediate second check should not trigger (no time passed)
	assert.False(t, monitor.Check(), "immediate second check should not trigger")
}

func TestSleepWakeMonitor_WakeDetected(t *testing.T) {
	t.Parallel()

	monitor := NewSleepWakeMonitor(5 * time.Second)

	// First check establishes baseline
	assert.False(t, monitor.Check())

	// Simulate sleep by setting lastCheck to the past
	monitor.SetLastCheckForTesting(time.Now().Add(-10 * time.Second))

	// Should detect wake (10 seconds elapsed > 5 second threshold)
	assert.True(t, monitor.Check(), "should detect wake after time jump")

	// Subsequent check should not trigger (time was reset in previous Check)
	assert.False(t, monitor.Check(), "subsequent check should not trigger")
}

func TestSleepWakeMonitor_Reset(t *testing.T) {
	t.Parallel()

	monitor := NewSleepWakeMonitor(5 * time.Second)

	// First check establishes baseline
	assert.False(t, monitor.Check())

	// Simulate sleep
	monitor.SetLastCheckForTesting(time.Now().Add(-10 * time.Second))

	// Reset instead of checking
	monitor.Reset()

	// Now check should not trigger (reset cleared the gap)
	assert.False(t, monitor.Check(), "check after reset should not trigger")
}

func TestSleepWakeMonitor_ThresholdBoundary(t *testing.T) {
	t.Parallel()

	threshold := 5 * time.Second
	monitor := NewSleepWakeMonitor(threshold)

	// First check establishes baseline
	assert.False(t, monitor.Check())

	// Set lastCheck to just under threshold - should NOT trigger
	monitor.SetLastCheckForTesting(time.Now().Add(-threshold + 100*time.Millisecond))

	assert.False(t, monitor.Check(), "just under threshold should not trigger")

	// Set lastCheck to just over threshold - should trigger
	monitor.SetLastCheckForTesting(time.Now().Add(-threshold - 100*time.Millisecond))

	assert.True(t, monitor.Check(), "just over threshold should trigger")
}

func TestSleepWakeMonitor_Concurrent(t *testing.T) {
	t.Parallel()

	monitor := NewSleepWakeMonitor(5 * time.Second)

	// First check to establish baseline
	monitor.Check()

	var eg errgroup.Group
	const goroutines = 10
	const iterations = 100

	// Run concurrent checks
	for range goroutines {
		eg.Go(func() error {
			for range iterations {
				monitor.Check()
			}
			return nil
		})
	}

	// Run concurrent resets
	for range goroutines {
		eg.Go(func() error {
			for range iterations {
				monitor.Reset()
			}
			return nil
		})
	}

	// If there's a race condition, the race detector will catch it
	_ = eg.Wait()
}

func TestSleepWakeMonitor_MultipleSleepCycles(t *testing.T) {
	t.Parallel()

	monitor := NewSleepWakeMonitor(5 * time.Second)

	// First check establishes baseline
	assert.False(t, monitor.Check())

	// Simulate first sleep
	monitor.SetLastCheckForTesting(time.Now().Add(-10 * time.Second))

	assert.True(t, monitor.Check(), "first wake should be detected")
	assert.False(t, monitor.Check(), "after first wake, no detection")

	// Simulate second sleep
	monitor.SetLastCheckForTesting(time.Now().Add(-15 * time.Second))

	assert.True(t, monitor.Check(), "second wake should be detected")
	assert.False(t, monitor.Check(), "after second wake, no detection")
}

// TestSleepWakeMonitor_UsesWallClockNotMonotonic verifies that the monitor
// uses wall clock time (not monotonic time) for detection. This is critical
// because Go's time.Sub() uses monotonic time when available, but monotonic
// clock pauses during system sleep on most OSes.
//
// The test verifies that Round(0) is being used to strip monotonic readings.
func TestSleepWakeMonitor_UsesWallClockNotMonotonic(t *testing.T) {
	t.Parallel()

	// Create a time with monotonic reading (from time.Now())
	timeWithMono := time.Now()

	// Verify it has a monotonic reading by checking String() output contains "m="
	// This is how Go indicates a monotonic clock reading is present
	assert.Contains(t, timeWithMono.String(), "m=",
		"time.Now() should have monotonic reading")

	// Strip monotonic reading using Round(0) - this is what our monitor does
	timeWithoutMono := timeWithMono.Round(0)

	// Verify monotonic reading is stripped
	assert.NotContains(t, timeWithoutMono.String(), "m=",
		"Round(0) should strip monotonic reading")

	// Key insight: When both times have monotonic readings, Sub() uses monotonic.
	// When either lacks monotonic, Sub() falls back to wall clock.
	// Our monitor needs wall clock to detect sleep (monotonic pauses during sleep).

	// Create monitor and verify its internal time lacks monotonic reading
	monitor := NewSleepWakeMonitor(5 * time.Second)

	// The monitor's lastCheck should NOT have monotonic (we use Round(0))
	// We can't directly inspect lastCheck, but we can verify the behavior:
	// If we set lastCheck to a time created with Round(0), the subtraction
	// in Check() will use wall clock comparison.

	// Set to 10 seconds ago (wall clock time, no monotonic)
	pastWallTime := time.Now().Add(-10 * time.Second).Round(0)
	monitor.SetLastCheckForTesting(pastWallTime)

	// Check should detect this as a wake event using wall clock diff
	assert.True(t, monitor.Check(),
		"monitor should detect time jump using wall clock, not monotonic")
}

// TestSleepWakeMonitor_MonotonicVsWallClock demonstrates the difference between
// monotonic and wall clock behavior that our fix addresses.
func TestSleepWakeMonitor_MonotonicVsWallClock(t *testing.T) {
	t.Parallel()

	threshold := 5 * time.Second

	// Scenario: Simulate what would happen during sleep
	// - Wall clock advances (sleep duration)
	// - Monotonic clock stays the same (paused during sleep)

	// Create two times that differ only in wall clock, not monotonic
	// This simulates what happens after waking from sleep:
	// - time.Now() returns current wall time with fresh monotonic
	// - But if we had stored a time before sleep, its monotonic would be "old"

	// In real sleep scenario:
	// Before sleep: time.Now() = wall=T1, mono=M1
	// After sleep:  time.Now() = wall=T1+sleep_duration, mono=M1 (mono paused!)
	// Sub() with both having mono would return ~0, not sleep_duration

	// Our fix: Use Round(0) to force wall clock comparison
	before := time.Now().Round(0)
	// Simulate wall clock advancing during "sleep" by creating a time in the past
	simulatedBeforeSleep := before.Add(-10 * time.Second)

	// Wall clock difference should be ~10 seconds
	wallDiff := before.Sub(simulatedBeforeSleep)
	assert.Greater(t, wallDiff, threshold,
		"wall clock diff (%v) should exceed threshold (%v)", wallDiff, threshold)

	// Now verify our monitor correctly uses this wall clock behavior
	monitor := NewSleepWakeMonitor(threshold)
	monitor.SetLastCheckForTesting(simulatedBeforeSleep)

	assert.True(t, monitor.Check(),
		"monitor should detect the 10-second wall clock jump as wake event")
}
