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

//nolint:paralleltest // these tests swap the package's probe hooks
package tty2oled

import (
	"errors"
	"os"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/shared/serialport"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/shared/usbserial"
	"github.com/stretchr/testify/assert"
	"go.bug.st/serial"
)

// stubProbeHooks gives every listed port a device file identity and reports
// busy as held by another program.
func stubProbeHooks(t *testing.T, ids map[string]string, busy ...string) {
	t.Helper()
	origID, origGuard := probeNodeID, guardProbes
	failedProbesMu.Lock()
	origFailed := failedProbes
	failedProbes = make(map[string]failedProbe)
	failedProbesMu.Unlock()
	t.Cleanup(func() {
		probeNodeID, guardProbes = origID, origGuard
		failedProbesMu.Lock()
		failedProbes = origFailed
		failedProbesMu.Unlock()
	})
	probeNodeID = func(path string) (string, error) {
		id, ok := ids[path]
		if !ok {
			return "", os.ErrNotExist
		}
		return id, nil
	}
	guardProbes = func([]string) *serialport.ProbeGuard { return serialport.StaticProbeGuard(busy...) }
}

// countingReader is a Reader whose ports never answer, with a count of the
// opens each one got.
func countingReader(ports ...string) (reader *Reader, openCount func(string) int) {
	var mu syncutil.Mutex
	opens := make(map[string]int)
	reader = &Reader{}
	reader.listSerialPorts = func() ([]string, error) { return ports, nil }
	reader.listUSBSerial = func() ([]usbserial.PortInfo, error) { return nil, nil }
	reader.openPort = func(path string, _ *serial.Mode) (serial.Port, error) {
		mu.Lock()
		defer mu.Unlock()
		opens[path]++
		return nil, errors.New("not a display")
	}
	return reader, func(path string) int {
		mu.Lock()
		defer mu.Unlock()
		return opens[path]
	}
}

func TestDetectLeavesPortsOpenInAnotherProgramAlone(t *testing.T) {
	stubProbeHooks(t, map[string]string{"/dev/ttyUSB0": "a", "/dev/ttyUSB1": "b"}, "/dev/ttyUSB0")
	reader, opens := countingReader("/dev/ttyUSB0", "/dev/ttyUSB1")

	assert.Empty(t, reader.Detect(nil))
	assert.Zero(t, opens("/dev/ttyUSB0"), "a port another program has open must not be probed")
	assert.Equal(t, 1, opens("/dev/ttyUSB1"))
}

func TestDetectGivesUpOnAPortUntilItIsReplugged(t *testing.T) {
	ids := map[string]string{"/dev/ttyUSB0": "a"}
	stubProbeHooks(t, ids)
	reader, opens := countingReader("/dev/ttyUSB0")

	for range maxProbeAttempts + 5 {
		assert.Empty(t, reader.Detect(nil))
	}
	assert.Equal(t, maxProbeAttempts, opens("/dev/ttyUSB0"),
		"a port that never answers must not be written to on every tick")

	ids["/dev/ttyUSB0"] = "b"
	assert.Empty(t, reader.Detect(nil))
	assert.Equal(t, maxProbeAttempts+1, opens("/dev/ttyUSB0"), "a replugged port gets a fresh set of attempts")
}

func TestDetectForgetsFailuresOnceADisplayAnswers(t *testing.T) {
	stubProbeHooks(t, map[string]string{"/dev/ttyUSB0": "a"})
	recordFailedProbe("/dev/ttyUSB0")

	reader := &Reader{}
	reader.listSerialPorts = func() ([]string, error) { return []string{"/dev/ttyUSB0"}, nil }
	reader.listUSBSerial = func() ([]usbserial.PortInfo, error) { return nil, nil }
	reader.openPort = func(string, *serial.Mode) (serial.Port, error) {
		return &probePort{reply: "HWESP32DE;"}, nil
	}

	assert.Equal(t, "tty2oled:/dev/ttyUSB0", reader.Detect(nil))
	failedProbesMu.Lock()
	defer failedProbesMu.Unlock()
	assert.Empty(t, failedProbes)
}
