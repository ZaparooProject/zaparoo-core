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

//go:build !windows

package helpers

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// GetSystemUptime returns the duration since the system booted.
// On Unix systems, this reads /proc/uptime which provides the uptime in seconds.
func GetSystemUptime() (time.Duration, error) {
	// Read /proc/uptime which contains: "uptime idle_time"
	data, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0, fmt.Errorf("failed to read /proc/uptime: %w", err)
	}

	// Parse the first field (uptime in seconds)
	fields := strings.Fields(string(data))
	if len(fields) < 1 {
		return 0, errors.New("invalid /proc/uptime format")
	}

	uptimeSeconds, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse uptime: %w", err)
	}

	// Convert to time.Duration
	uptime := time.Duration(uptimeSeconds * float64(time.Second))

	return uptime, nil
}
