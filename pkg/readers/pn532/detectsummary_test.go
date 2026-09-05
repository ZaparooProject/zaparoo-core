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

// summary guard is package-level state
//
//nolint:paralleltest // captureDebugLog swaps the global logger, and the
package pn532

import (
	"errors"
	"testing"

	"github.com/ZaparooProject/go-pn532/detection"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// resetDetectSummary clears the package-level guard so each test starts from a
// known state and does not inherit another test's last summary.
func resetDetectSummary(t *testing.T) {
	t.Helper()
	detectSummaryMu.Lock()
	lastDetectSummary = ""
	detectSummaryMu.Unlock()
	t.Cleanup(func() {
		detectSummaryMu.Lock()
		lastDetectSummary = ""
		detectSummaryMu.Unlock()
	})
}

func TestLogDetectionSummary_LogsOncePerChange(t *testing.T) {
	// The reader manager calls Detect once a second. Logging every call would
	// bury the log, so the summary is the thing that makes an info-level
	// report affordable at all: an unchanged picture must stay silent.
	resetDetectSummary(t)
	buf := captureDebugLog(t)

	ports := []string{"/dev/ttyUSB0"}
	devices := []detection.DeviceInfo{{Transport: "uart", Path: "/dev/ttyUSB0"}}

	for range 5 {
		logDetectionSummary(ports, nil, devices, nil, nil)
	}

	entries := decodeLogEntries(t, buf)
	require.Len(t, entries, 1, "five identical ticks should produce one line")
	assert.Equal(t, "PN532 auto-detect", entries[0].Message)
}

func TestLogDetectionSummary_LogsAgainWhenPortsChange(t *testing.T) {
	// Plugging or unplugging a port is exactly what a user needs to see.
	resetDetectSummary(t)
	buf := captureDebugLog(t)

	logDetectionSummary([]string{"/dev/ttyUSB0"}, nil, nil, nil, nil)
	logDetectionSummary([]string{"/dev/ttyUSB0", "/dev/ttyUSB1"}, nil, nil, nil, nil)
	logDetectionSummary([]string{"/dev/ttyUSB0"}, nil, nil, nil, nil)

	assert.Len(t, decodeLogEntries(t, buf), 3, "each change should be reported")
}

func TestLogDetectionSummary_IgnoresOrderingOfPortsAndIgnores(t *testing.T) {
	// The ignore list is built from map iteration and the enumerated ports
	// arrive in directory order. Without sorting, a stable bus would appear to
	// change on every tick and the log would fill up again.
	resetDetectSummary(t)
	buf := captureDebugLog(t)

	logDetectionSummary(
		[]string{"/dev/ttyUSB0", "/dev/ttyUSB1"},
		[]string{"/dev/ttyACM0", "/dev/ttyUSB9"}, nil, nil, nil)
	logDetectionSummary(
		[]string{"/dev/ttyUSB1", "/dev/ttyUSB0"},
		[]string{"/dev/ttyUSB9", "/dev/ttyACM0"}, nil, nil, nil)

	assert.Len(t, decodeLogEntries(t, buf), 1, "reordering alone is not a change")
}

func TestLogDetectionSummary_ReportsEnumerationFailure(t *testing.T) {
	// An enumeration failure leaves the port list empty, which reads exactly
	// like a bus with no serial ports on it. Without the error surfaced, the
	// summary cannot tell those apart, which is the ambiguity it exists to
	// remove.
	resetDetectSummary(t)
	buf := captureDebugLog(t)

	logDetectionSummary(nil, nil, nil, errors.New("permission denied"), nil)

	require.Len(t, decodeLogEntries(t, buf), 1)
	assert.Contains(t, buf.String(), "enumeration_error")
	assert.Contains(t, buf.String(), "permission denied")
}

func TestLogDetectionSummary_EnumerationFailureIsPartOfTheSummary(t *testing.T) {
	// An enumeration that starts failing is a change worth reporting even
	// though every visible list stays empty.
	resetDetectSummary(t)
	buf := captureDebugLog(t)

	logDetectionSummary(nil, nil, nil, nil, nil)
	logDetectionSummary(nil, nil, nil, errors.New("permission denied"), nil)

	assert.Len(t, decodeLogEntries(t, buf), 2,
		"an enumeration failure appearing is a state change")
}

func TestLogDetectionSummary_OmitsExpectedDetectionMiss(t *testing.T) {
	// "no devices found" is the steady state on a machine with no reader, not
	// an error worth showing a user.
	resetDetectSummary(t)
	buf := captureDebugLog(t)

	logDetectionSummary([]string{"/dev/ttyUSB0"}, nil, nil, nil, detection.ErrNoDevicesFound)

	require.Len(t, decodeLogEntries(t, buf), 1)
	assert.NotContains(t, buf.String(), "error",
		"an expected miss should not be dressed up as an error")
}

func TestLogDetectionSummary_ReportsUnexpectedDetectionError(t *testing.T) {
	resetDetectSummary(t)
	buf := captureDebugLog(t)

	logDetectionSummary([]string{"/dev/ttyUSB0"}, nil, nil, nil, errors.New("bus exploded"))

	require.Len(t, decodeLogEntries(t, buf), 1)
	assert.Contains(t, buf.String(), "bus exploded")
}
