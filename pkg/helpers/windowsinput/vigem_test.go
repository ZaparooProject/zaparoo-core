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

package windowsinput

import (
	"testing"
	"unsafe"

	"github.com/stretchr/testify/assert"
)

// The driver checks each request's Size against its own sizeof and rejects a
// mismatch without saying why, so the layouts are pinned here rather than left
// to surface as an unexplained failure on a user's machine. The expected
// values come from the driver's BusShared.h.
func TestRequestStructSizes(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		got  uintptr
		want uintptr
	}{
		{"VIGEM_CHECK_VERSION", unsafe.Sizeof(vigemCheckVersion{}), 8},
		{"VIGEM_PLUGIN_TARGET", unsafe.Sizeof(vigemPluginTarget{}), 16},
		{"VIGEM_WAIT_DEVICE_READY", unsafe.Sizeof(vigemWaitDeviceReady{}), 8},
		{"VIGEM_UNPLUG_TARGET", unsafe.Sizeof(vigemUnplugTarget{}), 8},
		{"XUSB_REPORT", unsafe.Sizeof(xusbReport{}), 12},
		{"XUSB_SUBMIT_REPORT", unsafe.Sizeof(xusbSubmitReport{}), 20},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, tc.got)
		})
	}
}

// The report has to sit immediately after the two header fields, with no
// padding, or the driver reads the wrong bytes as button state.
func TestSubmitReportLayout(t *testing.T) {
	t.Parallel()

	assert.Equal(t, uintptr(8), unsafe.Offsetof(xusbSubmitReport{}.Report))
}

// CTL_CODE arithmetic is easy to get subtly wrong, and a wrong code reaches
// the driver as an unrecognised request rather than an obvious mistake.
func TestIoctlCodes(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		got  uint32
		want uint32
	}{
		{"IOCTL_VIGEM_PLUGIN_TARGET", ioctlPluginTarget, 0x2AA004},
		{"IOCTL_VIGEM_UNPLUG_TARGET", ioctlUnplugTarget, 0x2AA008},
		{"IOCTL_VIGEM_CHECK_VERSION", ioctlCheckVersion, 0x2AA00C},
		{"IOCTL_VIGEM_WAIT_DEVICE_READY", ioctlWaitDeviceReady, 0x2AA010},
		{"IOCTL_XUSB_SUBMIT_REPORT", ioctlXusbSubmitReport, 0x2AA808},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, tc.got)
		})
	}
}
