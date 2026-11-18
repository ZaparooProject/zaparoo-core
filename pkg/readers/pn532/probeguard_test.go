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

//nolint:paralleltest // these tests swap the package's detection hooks
package pn532

import (
	"context"
	"testing"

	"github.com/ZaparooProject/go-pn532/detection"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/shared/serialport"
	"github.com/stretchr/testify/assert"
)

// stubGuard feeds Detect the given ports and a guard reporting busy as held
// by another program, and returns the port lists each tick asked to guard.
func stubGuard(t *testing.T, ports []serialport.USBPort, busy ...string) *[][]string {
	t.Helper()
	origList, origGuard := listProbePorts, guardProbes
	probeStateMu.Lock()
	origUnprobed := unprobedPaths
	unprobedPaths = make(map[string]string)
	probeStateMu.Unlock()
	t.Cleanup(func() {
		listProbePorts, guardProbes = origList, origGuard
		probeStateMu.Lock()
		unprobedPaths = origUnprobed
		probeStateMu.Unlock()
	})

	var guarded [][]string
	listProbePorts = func() []serialport.USBPort { return ports }
	guardProbes = func(paths []string) *serialport.ProbeGuard {
		guarded = append(guarded, paths)
		return serialport.StaticProbeGuard(busy...)
	}
	return &guarded
}

func TestDetect_LeavesPortsOpenInAnotherProgramAlone(t *testing.T) {
	// A MIDI adapter another program is driving must not be opened: the
	// probe would change its baud rate and write to it.
	resetFailedProbes(t)
	resetDetectSummary(t)
	stubProbeNodeID(t, map[string]string{"/dev/ttyUSB0": "a", "/dev/ttyUSB1": "b"})
	guarded := stubGuard(t, []serialport.USBPort{
		{Path: "/dev/ttyUSB0", VIDPID: "0403:6001"},
		{Path: "/dev/ttyUSB1", VIDPID: "1A86:7523"},
		{Path: "/dev/ttyACM0", VIDPID: "1209:4D69"}, // Greaseweazle, blocklisted
		{Path: "/dev/ttyACM1"},
	}, "/dev/ttyUSB0")

	orig := detectAll
	t.Cleanup(func() { detectAll = orig })
	var ignored []string
	detectAll = func(_ context.Context, opts *detection.Options) ([]detection.DeviceInfo, error) {
		ignored = opts.IgnorePaths
		opts.ReportProbe(detection.ProbeResult{Transport: detection.TransportUART, Path: "/dev/ttyUSB1"})
		return nil, detection.ErrNoDevicesFound
	}

	assert.Empty(t, (&Reader{}).Detect([]string{"simple_serial:/dev/ttyACM1"}))

	assert.Equal(t, [][]string{{"/dev/ttyUSB0", "/dev/ttyUSB1"}}, *guarded,
		"connected and blocklisted ports are not guarded, which would open them")
	assert.Contains(t, ignored, "/dev/ttyUSB0", "detection must be told to skip the busy port")
	assert.NotContains(t, ignored, "/dev/ttyUSB1")

	probeStateMu.RLock()
	defer probeStateMu.RUnlock()
	assert.NotContains(t, failedProbePaths, "/dev/ttyUSB0",
		"a busy port is checked again once the other program lets go of it")
	assert.Contains(t, failedProbePaths, "/dev/ttyUSB1")
}

func TestDetect_StopsGuardingPortsDetectionPassesOver(t *testing.T) {
	// Guarding opens the port. A port detection never probes would otherwise
	// be opened on every tick, until it is replugged.
	resetFailedProbes(t)
	resetDetectSummary(t)
	ids := map[string]string{"/dev/ttyUSB0": "a"}
	stubProbeNodeID(t, ids)
	guarded := stubGuard(t, []serialport.USBPort{{Path: "/dev/ttyUSB0"}})

	orig := detectAll
	t.Cleanup(func() { detectAll = orig })
	detectAll = func(context.Context, *detection.Options) ([]detection.DeviceInfo, error) {
		return nil, detection.ErrNoDevicesFound
	}

	reader := &Reader{}
	assert.Empty(t, reader.Detect(nil))
	assert.Empty(t, reader.Detect(nil))
	assert.Equal(t, [][]string{{"/dev/ttyUSB0"}, {}}, *guarded)

	ids["/dev/ttyUSB0"] = "b"
	assert.Empty(t, reader.Detect(nil))
	assert.Equal(t, []string{"/dev/ttyUSB0"}, (*guarded)[2], "a replugged port is guarded afresh")
}
