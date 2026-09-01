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
	"path/filepath"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// resetProbeState clears the package-level cache so tests do not leak into
// each other. These cannot run in parallel for the same reason.
func resetProbeState(t *testing.T) {
	t.Helper()
	probeStateMu.Lock()
	failedProbePaths = make(map[string]failedProbeEntry)
	probeStateMu.Unlock()
	t.Cleanup(func() {
		probeStateMu.Lock()
		failedProbePaths = make(map[string]failedProbeEntry)
		probeStateMu.Unlock()
	})
}

func fakeDevNode(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ttyACM0")
	require.NoError(t, os.WriteFile(path, []byte("x"), 0o600))
	return path
}

func TestFailedProbeIsRememberedSoPortsAreNotHammered(t *testing.T) {
	resetProbeState(t)
	now := time.Now()
	path := fakeDevNode(t)

	assert.False(t, probeFailedRecently(path, now), "nothing known about a fresh port")
	recordFailedProbe(path, now)

	// Auto-detect runs once a second; a port that answered wrongly must not be
	// reopened and written to on every tick.
	assert.True(t, probeFailedRecently(path, now))
	refreshFailedProbes()
	assert.True(t, probeFailedRecently(path, now), "an unchanged device stays remembered")
}

func TestFailedProbeClearedWhenDeviceReplugged(t *testing.T) {
	resetProbeState(t)
	now := time.Now()
	path := fakeDevNode(t)
	recordFailedProbe(path, now)
	require.True(t, probeFailedRecently(path, now))

	// Replugging recreates the device node, which changes its mtime. The port
	// may now be a different device entirely, so it has to be probed again.
	future := time.Now().Add(2 * time.Second)
	require.NoError(t, os.Chtimes(path, future, future))
	refreshFailedProbes()

	assert.False(t, probeFailedRecently(path, now))
}

func TestFailedProbeClearedWhenDeviceDisappears(t *testing.T) {
	resetProbeState(t)
	now := time.Now()
	path := fakeDevNode(t)
	recordFailedProbe(path, now)
	require.NoError(t, os.Remove(path))

	refreshFailedProbes()
	assert.False(t, probeFailedRecently(path, now), "an unplugged device leaves no entry behind")
}

func TestClearFailedProbeForgetsOnePath(t *testing.T) {
	resetProbeState(t)
	now := time.Now()
	a, b := fakeDevNode(t), fakeDevNode(t)
	recordFailedProbe(a, now)
	recordFailedProbe(b, now)

	clearFailedProbe(a)
	assert.False(t, probeFailedRecently(a, now))
	assert.True(t, probeFailedRecently(b, now), "clearing one path leaves the others alone")
}

func TestRecordFailedProbeIgnoresMissingDevice(t *testing.T) {
	resetProbeState(t)
	now := time.Now()
	missing := filepath.Join(t.TempDir(), "gone")

	// The device vanished mid-probe: there is nothing to key an entry on, and
	// it should be treated as unknown rather than remembered as failed.
	recordFailedProbe(missing, now)
	assert.False(t, probeFailedRecently(missing, now))
}

func TestFailedProbeRetriedAfterTheRetryInterval(t *testing.T) {
	resetProbeState(t)
	now := time.Now()
	path := fakeDevNode(t)

	recordFailedProbe(path, now)
	require.True(t, probeFailedRecently(path, now.Add(probeRetryInterval-time.Second)))

	// The display itself can fail a probe: its USB peripheral enumerates from
	// the ROM bootloader, so the node exists before the firmware answers. A
	// failure that is never retried would leave it undetectable until replug.
	assert.False(t, probeFailedRecently(path, now.Add(probeRetryInterval)))
}

func TestFailedProbeRearmsAfterEachRetry(t *testing.T) {
	resetProbeState(t)
	now := time.Now()
	path := fakeDevNode(t)

	recordFailedProbe(path, now)
	retryAt := now.Add(probeRetryInterval)
	require.False(t, probeFailedRecently(path, retryAt))

	// A port that is simply not a display must go back to being skipped, so
	// retrying costs one open a minute rather than one a second.
	recordFailedProbe(path, retryAt)
	assert.True(t, probeFailedRecently(path, retryAt.Add(time.Second)))
}

func TestCouldBeDisplayAllowsUnknownVendor(t *testing.T) {
	t.Parallel()

	// Where the vendor cannot be read - no udevadm, or a non-USB node - the
	// port must still be probed, or the display would be undetectable there.
	r := NewReader(&config.Instance{})
	r.vendorIDs = func(string) (string, string, bool) { return "", "", false }
	assert.True(t, r.couldBeDisplay("/dev/ttyACM0"))
}

func TestCouldBeDisplayAcceptsTheDisplayVendor(t *testing.T) {
	t.Parallel()

	r := NewReader(&config.Instance{})
	r.vendorIDs = func(string) (string, string, bool) { return espressifVID, "1001", true }
	assert.True(t, r.couldBeDisplay("/dev/ttyACM0"))
}

func TestCouldBeDisplayRejectsOtherVendors(t *testing.T) {
	t.Parallel()

	// Probing writes to whatever is on the other end, and /dev/ttyACM* is also
	// where 3D printers and Arduino-class boards appear. A known other vendor
	// must never be opened.
	r := NewReader(&config.Instance{})
	r.vendorIDs = func(string) (string, string, bool) { return "2341", "0043", true }
	assert.False(t, r.couldBeDisplay("/dev/ttyACM0"))
}

func TestDefaultEnabledIsPerPlatform(t *testing.T) {
	t.Parallel()

	// Detection writes to a serial port, so it is only on by default where the
	// display is an expected accessory.
	assert.False(t, NewReader(&config.Instance{}).Metadata().DefaultEnabled)
	assert.False(t, NewReaderWithDefaults(&config.Instance{}, false).Metadata().DefaultEnabled)
	assert.True(t, NewReaderWithDefaults(&config.Instance{}, true).Metadata().DefaultEnabled)

	// Auto-detect stays on regardless: it only runs once the driver is enabled.
	assert.True(t, NewReader(&config.Instance{}).Metadata().DefaultAutoDetect)
}
