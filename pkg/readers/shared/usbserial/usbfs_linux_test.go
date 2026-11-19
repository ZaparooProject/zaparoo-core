//go:build linux && !android

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

package usbserial

import (
	"os"
	"path/filepath"
	"testing"
	"time"
	"unsafe"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

func TestUSBFSRequestNumbers(t *testing.T) {
	t.Parallel()
	// _IOWR('U', 0, struct usbdevfs_ctrltransfer) and
	// _IOWR('U', 2, struct usbdevfs_bulktransfer).
	if unsafe.Sizeof(uintptr(0)) == 8 {
		assert.Equal(t, uintptr(0xC0185500), usbfsControl)
		assert.Equal(t, uintptr(0xC0185502), usbfsBulk)
	} else {
		assert.Equal(t, uintptr(0xC0105500), usbfsControl)
		assert.Equal(t, uintptr(0xC0105502), usbfsBulk)
	}
}

func TestMapErrno(t *testing.T) {
	t.Parallel()
	require.ErrorIs(t, mapErrno(unix.EPIPE), errStall)
	require.ErrorIs(t, mapErrno(unix.ETIMEDOUT), errTimeout)
	for _, errno := range []unix.Errno{unix.ENODEV, unix.ESHUTDOWN, unix.ENOENT} {
		err := mapErrno(errno)
		require.ErrorIs(t, err, ErrDeviceGone)
		require.ErrorIs(t, err, unix.ENODEV)
	}
	err := mapErrno(unix.EPROTO)
	require.ErrorIs(t, err, unix.EPROTO)
	require.NotErrorIs(t, err, ErrDeviceGone)
}

func TestTimeoutMillis(t *testing.T) {
	t.Parallel()
	assert.Equal(t, uint32(1), timeoutMillis(0))
	assert.Equal(t, uint32(1), timeoutMillis(time.Microsecond))
	assert.Equal(t, uint32(250), timeoutMillis(250*time.Millisecond))
	assert.Equal(t, uint32(time.Hour/time.Millisecond), timeoutMillis(48*time.Hour))
}

func TestOpenUSBDeviceErrors(t *testing.T) {
	t.Parallel()
	_, err := openUSBDevice(filepath.Join(t.TempDir(), "missing"), 0)
	require.ErrorIs(t, err, ErrDeviceGone)

	// A regular file is no usbfs node: the claim fails without hanging.
	node := filepath.Join(t.TempDir(), "node")
	require.NoError(t, os.WriteFile(node, nil, 0o600))
	_, err = openUSBDevice(node, 0)
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrDriverBound)
}
