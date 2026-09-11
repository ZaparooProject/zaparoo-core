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
	"os"
	"path/filepath"
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

func uartProbes(paths ...string) []detection.ProbeResult {
	probes := make([]detection.ProbeResult, 0, len(paths))
	for _, path := range paths {
		probes = append(probes, detection.ProbeResult{Transport: detection.TransportUART, Path: path})
	}
	return probes
}

func TestLogDetectionSummary_LogsOncePerChange(t *testing.T) {
	// The reader manager calls Detect once a second. Logging every call would
	// bury the log, so the summary is the thing that makes an info-level
	// report affordable at all: an unchanged picture must stay silent.
	resetDetectSummary(t)
	buf := captureDebugLog(t)

	probes := []detection.ProbeResult{{Transport: detection.TransportUART, Path: "/dev/ttyUSB0", Found: true}}
	devices := []detection.DeviceInfo{{Transport: "uart", Path: "/dev/ttyUSB0"}}

	for range 5 {
		logDetectionSummary(probes, nil, devices, nil)
	}

	entries := decodeLogEntries(t, buf)
	require.Len(t, entries, 1, "five identical ticks should produce one line")
	assert.Equal(t, "PN532 auto-detect", entries[0].Message)
}

func TestLogDetectionSummary_LogsAgainWhenProbesChange(t *testing.T) {
	// Plugging or unplugging a port is exactly what a user needs to see.
	resetDetectSummary(t)
	buf := captureDebugLog(t)

	logDetectionSummary(uartProbes("/dev/ttyUSB0"), nil, nil, nil)
	logDetectionSummary(uartProbes("/dev/ttyUSB0", "/dev/ttyUSB1"), nil, nil, nil)
	logDetectionSummary(uartProbes("/dev/ttyUSB0"), nil, nil, nil)

	assert.Len(t, decodeLogEntries(t, buf), 3, "each change should be reported")
}

func TestLogDetectionSummary_IgnoresOrderingOfProbesAndIgnores(t *testing.T) {
	// The ignore list is built from map iteration and probes arrive in the
	// order detectors finished. Without sorting, a stable bus would appear to
	// change on every tick and the log would fill up again.
	resetDetectSummary(t)
	buf := captureDebugLog(t)

	logDetectionSummary(
		uartProbes("/dev/ttyUSB0", "/dev/ttyUSB1"),
		[]string{"/dev/ttyACM0", "/dev/ttyUSB9"}, nil, nil)
	logDetectionSummary(
		uartProbes("/dev/ttyUSB1", "/dev/ttyUSB0"),
		[]string{"/dev/ttyUSB9", "/dev/ttyACM0"}, nil, nil)

	assert.Len(t, decodeLogEntries(t, buf), 1, "reordering alone is not a change")
}

func TestLogDetectionSummary_OmitsExpectedDetectionMiss(t *testing.T) {
	// "no devices found" is the steady state on a machine with no reader, not
	// an error worth showing a user.
	resetDetectSummary(t)
	buf := captureDebugLog(t)

	logDetectionSummary(uartProbes("/dev/ttyUSB0"), nil, nil, detection.ErrNoDevicesFound)

	require.Len(t, decodeLogEntries(t, buf), 1)
	assert.NotContains(t, buf.String(), "error",
		"an expected miss should not be dressed up as an error")
}

func TestLogDetectionSummary_ReportsADetectionTimeout(t *testing.T) {
	// A pass that runs out of time used to be filed with "nothing there" and
	// vanished at trace level. That hid a controller adapter parking every
	// probe for months; it now travels in the summary.
	resetDetectSummary(t)
	buf := captureDebugLog(t)

	logDetectionSummary(uartProbes("/dev/ttyACM0", "/dev/ttyUSB0"), nil, nil, detection.ErrDetectionTimeout)

	require.Len(t, decodeLogEntries(t, buf), 1)
	assert.Contains(t, buf.String(), detection.ErrDetectionTimeout.Error())
}

func TestLogDetectionSummary_ReportsUnexpectedDetectionError(t *testing.T) {
	resetDetectSummary(t)
	buf := captureDebugLog(t)

	logDetectionSummary(uartProbes("/dev/ttyUSB0"), nil, nil, errors.New("bus exploded"))

	require.Len(t, decodeLogEntries(t, buf), 1)
	assert.Contains(t, buf.String(), "bus exploded")
}

// resetFailedProbes gives a test an empty failed-probe set and restores the
// package-level state afterwards.
func resetFailedProbes(t *testing.T) {
	t.Helper()
	probeStateMu.Lock()
	saved := failedProbePaths
	failedProbePaths = make(map[string]failedProbeEntry)
	probeStateMu.Unlock()
	t.Cleanup(func() {
		probeStateMu.Lock()
		failedProbePaths = saved
		probeStateMu.Unlock()
	})
}

func TestRecordFailedProbes(t *testing.T) {
	// Only a probe that ran and got no answer marks a port. One that answered,
	// one already connected, one detection reported anyway, and one whose
	// device file has gone must all stay probe-able.
	resetFailedProbes(t)

	dir := t.TempDir()
	paths := make(map[string]string)
	for _, name := range []string{"failed", "answered", "connected", "detected"} {
		path := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(path, nil, 0o600))
		paths[name] = path
	}
	probes := []detection.ProbeResult{
		{Transport: detection.TransportUART, Path: paths["failed"]},
		{Transport: detection.TransportUART, Path: paths["answered"], Found: true},
		{Transport: detection.TransportUART, Path: paths["connected"]},
		{Transport: detection.TransportUART, Path: paths["detected"]},
		{Transport: detection.TransportUART, Path: filepath.Join(dir, "gone")},
	}
	devices := []detection.DeviceInfo{{Transport: detection.TransportUART, Path: paths["detected"]}}

	recordFailedProbes(probes, devices, map[string]bool{paths["connected"]: true})

	probeStateMu.RLock()
	defer probeStateMu.RUnlock()
	assert.Len(t, failedProbePaths, 1)
	assert.Contains(t, failedProbePaths, paths["failed"])
}
