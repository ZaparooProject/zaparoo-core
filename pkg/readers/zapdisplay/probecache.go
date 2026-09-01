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

package zapdisplay

import (
	"os"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
)

// probeRetryInterval is how long a failed port is left alone before it is
// tried again.
//
// A retry is needed because the display itself can fail a probe: the ESP32-S3
// enumerates from its ROM bootloader, so the device node exists before the
// firmware's protocol loop is answering. A probe that lands in that window
// times out, and with no retry the display would stay undetectable until it
// was physically replugged. Only ports that passed the vendor filter ever get
// here, so this is mostly re-asking the display rather than other hardware.
const probeRetryInterval = time.Minute

// Ports that answered something other than the zapdisplay handshake.
//
// Auto-detect runs once a second. Without this a port that is not a display
// would be opened and written to on every tick for as long as it stays plugged
// in, which for a 3D printer or a microcontroller is both pointless and rude.
//
// An entry is keyed by the device file's modification time, so unplugging and
// replugging - which recreates the node - clears it and the port is probed
// again. That mirrors how the PN532 driver tracks its own failed probes.
type failedProbeEntry struct {
	deviceModTime time.Time
	failedAt      time.Time
}

var (
	probeStateMu     syncutil.RWMutex
	failedProbePaths = make(map[string]failedProbeEntry)
)

// refreshFailedProbes drops entries whose device file has changed or gone away,
// so a replug or a different device at the same path is probed afresh.
func refreshFailedProbes() {
	probeStateMu.Lock()
	defer probeStateMu.Unlock()
	for path, entry := range failedProbePaths {
		info, err := os.Stat(path)
		if err != nil || !info.ModTime().Equal(entry.deviceModTime) {
			delete(failedProbePaths, path)
		}
	}
}

// probeFailedRecently reports whether a port failed a probe recently enough to
// skip. An older failure is retried, and recordFailedProbe re-arms the window
// if it fails again, so a port that is simply not a display is still only
// opened once a minute.
func probeFailedRecently(path string, now time.Time) bool {
	probeStateMu.RLock()
	defer probeStateMu.RUnlock()
	entry, failed := failedProbePaths[path]
	return failed && now.Sub(entry.failedAt) < probeRetryInterval
}

func recordFailedProbe(path string, now time.Time) {
	info, err := os.Stat(path)
	if err != nil {
		// The device vanished mid-probe. Nothing to remember it by, and it will
		// be reported fresh if it comes back.
		return
	}
	probeStateMu.Lock()
	defer probeStateMu.Unlock()
	failedProbePaths[path] = failedProbeEntry{deviceModTime: info.ModTime(), failedAt: now}
}

// clearFailedProbe forgets one path, so a port that has just been claimed is
// probed normally if the reader later disconnects.
func clearFailedProbe(path string) {
	probeStateMu.Lock()
	defer probeStateMu.Unlock()
	delete(failedProbePaths, path)
}
