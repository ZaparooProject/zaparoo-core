//go:build linux && !android

/*
Zaparoo Core
Copyright (c) 2026 The Zaparoo Project Contributors.
SPDX-License-Identifier: GPL-3.0-or-later

This file is part of Zaparoo Core.

Zaparoo Core is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

Zaparoo Core is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with Zaparoo Core.  If not, see <http://www.gnu.org/licenses/>.
*/

package ccid

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/ccid/proto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The transfers themselves need a real reader. These tests cover what can be
// reached without one: a file that is not a USB device refuses every usbfs
// request, which exercises each request's failure path.

func notAUSBDevice(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "node")
	require.NoError(t, os.WriteFile(path, nil, 0o600))
	return path
}

func testTarget() *usbTarget {
	return &usbTarget{bus: 999, dev: 999, iface: proto.Interface{Number: 1, BulkIn: 0x81, BulkOut: 0x01}}
}

func TestOpenUSBFS_DeviceGone(t *testing.T) {
	t.Parallel()

	_, err := openUSBFS(testTarget())

	require.ErrorIs(t, err, errReaderGone, "a device node that no longer exists is an unplugged reader")
}

func TestOpenUSBFS_ClaimRefused(t *testing.T) {
	t.Parallel()

	_, err := openUSBFSNode(notAUSBDevice(t), testTarget())

	require.ErrorContains(t, err, "claim USB interface 1")
	require.NotErrorIs(t, err, errReaderGone)
}

func TestOpenUSBFS_Unreadable(t *testing.T) {
	t.Parallel()

	_, err := openUSBFSNode(t.TempDir(), testTarget())

	require.ErrorContains(t, err, "open USB device")
}

func openTestLink(t *testing.T) *usbfsLink {
	t.Helper()
	file, err := os.OpenFile(notAUSBDevice(t), os.O_RDWR, 0)
	require.NoError(t, err)
	return &usbfsLink{file: file, in: 0x81, out: 0x01, packet: 64}
}

func TestUSBFSLink_TransferFailures(t *testing.T) {
	t.Parallel()
	link := openTestLink(t)
	ctx := context.Background()

	require.ErrorContains(t, link.Send(ctx, []byte{0x65, 0, 0, 0, 0, 0, 1, 0, 0, 0}), "usb transfer")
	_, err := link.Receive(ctx, 0)
	require.ErrorContains(t, err, "usb transfer")
	require.NoError(t, link.Send(ctx, nil), "nothing to send is not a transfer")

	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	require.ErrorIs(t, link.Send(cancelled, []byte{1}), context.Canceled)
	_, err = link.Receive(cancelled, 10)
	require.ErrorIs(t, err, context.Canceled)

	require.NoError(t, link.Close())
	require.Error(t, link.Close(), "the device node is already closed")
}

func TestReceiveSize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		want   int
		packet int
		size   int
	}{
		{name: "length not known yet", want: 0, packet: 64, size: 64},
		{name: "less than a packet", want: 6, packet: 64, size: 64},
		{name: "exactly one packet", want: 64, packet: 64, size: 64},
		{name: "rounds up to whole packets", want: 70, packet: 64, size: 128},
		{name: "exact multiple", want: 128, packet: 64, size: 128},
		{name: "high-speed packets", want: 600, packet: 512, size: 1024},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.size, receiveSize(tt.want, tt.packet))
		})
	}
}

func TestTimeoutFor(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	limit := uint32(usbfsBulkTimeout / time.Millisecond)
	assert.Equal(t, limit, timeoutFor(ctx))

	soon, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()
	got := timeoutFor(soon)
	assert.Positive(t, got)
	assert.LessOrEqual(t, got, uint32(200))

	later, cancelLater := context.WithTimeout(ctx, time.Hour)
	defer cancelLater()
	assert.Equal(t, limit, timeoutFor(later), "a transfer never waits past the cap")

	past, cancelPast := context.WithDeadline(ctx, time.Now().Add(-time.Second))
	defer cancelPast()
	assert.Equal(t, uint32(1), timeoutFor(past), "zero would mean wait forever")
}

func TestPlatformBackends(t *testing.T) {
	t.Parallel()

	backends := platformBackends()

	require.Len(t, backends, 2)
	assert.Equal(t, backendPCSC, backends[0].Name(), "the PC/SC service is preferred where it has the reader")
	assert.Equal(t, backendUSB, backends[1].Name())
}
