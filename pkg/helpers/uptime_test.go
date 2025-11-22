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
	"github.com/stretchr/testify/require"
)

func TestGetSystemUptime(t *testing.T) {
	t.Parallel()

	uptime, err := GetSystemUptime()
	require.NoError(t, err, "GetSystemUptime should not return error on running system")

	// Uptime should be a positive duration
	assert.Greater(t, uptime, time.Duration(0), "uptime should be positive")

	// Uptime should be less than a reasonable maximum (1 year)
	// This catches obvious errors like returning milliseconds as seconds
	maxUptime := 365 * 24 * time.Hour
	assert.Less(t, uptime, maxUptime, "uptime should be less than 1 year")

	// Uptime should be at least a few seconds (system must have been running to execute tests)
	minUptime := 1 * time.Second
	assert.GreaterOrEqual(t, uptime, minUptime, "uptime should be at least 1 second")
}

func TestGetSystemUptime_Consistency(t *testing.T) {
	t.Parallel()

	// Get uptime twice with a small delay
	uptime1, err1 := GetSystemUptime()
	require.NoError(t, err1)

	time.Sleep(100 * time.Millisecond)

	uptime2, err2 := GetSystemUptime()
	require.NoError(t, err2)

	// Second reading should be slightly larger (system has been running longer)
	assert.GreaterOrEqual(t, uptime2, uptime1, "uptime should increase over time")

	// Difference should be small (less than 1 second, accounting for test overhead)
	diff := uptime2 - uptime1
	assert.Less(t, diff, 1*time.Second, "uptime difference should be less than 1 second")
}
