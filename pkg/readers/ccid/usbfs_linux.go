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
	"errors"
	"fmt"
	"os"
	"runtime"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

// usbfs requests, from linux/usbdevice_fs.h. The bulk request number depends
// on the pointer size, so it is built from the struct.
const (
	usbfsClaimInterface   = 0x8004550F
	usbfsReleaseInterface = 0x80045510
	usbfsDisconnectClaim  = 0x8108551B

	// usbfsExceptDriver makes a disconnect-claim leave the named driver in
	// place and fail instead: an interface another program has claimed is
	// never taken from it.
	usbfsExceptDriver = 0x02

	usbfsBulkTimeout = 3 * time.Second
	usbDeviceNode    = "/dev/bus/usb/%03d/%03d"
)

// usbfsBulkArg is struct usbdevfs_bulktransfer, whose data pointer comes last.
//
//nolint:govet // the field order is the kernel's
type usbfsBulkArg struct {
	ep      uint32
	length  uint32
	timeout uint32
	data    unsafe.Pointer
}

var usbfsBulk = uintptr(3)<<30 | unsafe.Sizeof(usbfsBulkArg{})<<16 | uintptr('U')<<8 | 2

type usbfsDisconnectClaimArg struct {
	iface  uint32
	flags  uint32
	driver [256]byte
}

// usbfsLink is a claimed smart card interface, used through the kernel's
// usbfs with no USB library in between.
type usbfsLink struct {
	file   *os.File
	iface  uint32
	in     uint8
	out    uint8
	packet int
}

func openUSBFS(target *usbTarget) (usbLink, error) {
	node := fmt.Sprintf(usbDeviceNode, target.bus, target.dev)
	file, err := os.OpenFile(node, os.O_RDWR, 0) //nolint:gosec // path is built from sysfs bus and device numbers
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w: %w", errReaderGone, err)
		}
		return nil, fmt.Errorf("open USB device: %w", err)
	}

	link := &usbfsLink{
		file:   file,
		iface:  uint32(target.iface.Number),
		in:     target.iface.BulkIn,
		out:    target.iface.BulkOut,
		packet: int(target.iface.BulkInPacket),
	}
	if link.packet == 0 {
		link.packet = 64
	}
	if err := link.claim(); err != nil {
		_ = file.Close()
		return nil, err
	}
	return link, nil
}

func (l *usbfsLink) ioctl(request uintptr, arg unsafe.Pointer) (int, error) {
	n, _, errno := unix.Syscall(unix.SYS_IOCTL, l.file.Fd(), request, uintptr(arg))
	runtime.KeepAlive(l.file)
	if errno != 0 {
		return 0, errno
	}
	return int(n), nil
}

// claim takes the interface, first detaching a kernel driver such as the NFC
// driver that binds an ACR122U.
func (l *usbfsLink) claim() error {
	arg := usbfsDisconnectClaimArg{iface: l.iface, flags: usbfsExceptDriver}
	copy(arg.driver[:], driverUSBFS)
	_, err := l.ioctl(usbfsDisconnectClaim, unsafe.Pointer(&arg)) //nolint:gosec // ioctl argument
	if errors.Is(err, unix.ENOTTY) {
		// A kernel too old for disconnect-claim.
		iface := l.iface
		_, err = l.ioctl(usbfsClaimInterface, unsafe.Pointer(&iface)) //nolint:gosec // ioctl argument
	}
	if err != nil {
		return fmt.Errorf("claim USB interface %d: %w", l.iface, err)
	}
	return nil
}

func timeoutFor(ctx context.Context) uint32 {
	timeout := usbfsBulkTimeout
	if deadline, ok := ctx.Deadline(); ok {
		if remaining := time.Until(deadline); remaining < timeout {
			timeout = remaining
		}
	}
	if timeout < time.Millisecond {
		timeout = time.Millisecond
	}
	return uint32(timeout.Milliseconds()) //nolint:gosec // bounded by usbfsBulkTimeout
}

func (l *usbfsLink) bulk(ctx context.Context, ep uint8, buf []byte) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, fmt.Errorf("usb transfer: %w", err)
	}
	arg := usbfsBulkArg{
		ep:      uint32(ep),
		length:  uint32(len(buf)), //nolint:gosec // CCID messages are far below 4 GiB
		timeout: timeoutFor(ctx),
	}
	if len(buf) > 0 {
		arg.data = unsafe.Pointer(&buf[0]) //nolint:gosec // ioctl buffer
	}
	n, err := l.ioctl(usbfsBulk, unsafe.Pointer(&arg)) //nolint:gosec // ioctl argument
	runtime.KeepAlive(buf)
	if err != nil {
		if errors.Is(err, unix.ENODEV) || errors.Is(err, unix.ESHUTDOWN) || errors.Is(err, unix.ENOENT) {
			return 0, fmt.Errorf("%w: %w", errReaderGone, err)
		}
		return 0, fmt.Errorf("usb transfer: %w", err)
	}
	return n, nil
}

func (l *usbfsLink) Send(ctx context.Context, msg []byte) error {
	for len(msg) > 0 {
		n, err := l.bulk(ctx, l.out, msg)
		if err != nil {
			return err
		}
		if n == 0 {
			return errors.New("usb transfer: nothing written")
		}
		msg = msg[n:]
	}
	return nil
}

// Receive reads the next part of a response. The buffer is always a whole
// number of packets: a smaller one makes the kernel reject a full packet, and
// a read ends at a short packet or when the buffer fills, so it never waits
// past the end of a message.
func (l *usbfsLink) Receive(ctx context.Context, want int) ([]byte, error) {
	size := l.packet
	if want > size {
		size = (want + l.packet - 1) / l.packet * l.packet
	}
	buf := make([]byte, size)
	n, err := l.bulk(ctx, l.in, buf)
	if err != nil {
		return nil, err
	}
	return buf[:n], nil
}

func (l *usbfsLink) Close() error {
	iface := l.iface
	_, _ = l.ioctl(usbfsReleaseInterface, unsafe.Pointer(&iface)) //nolint:gosec // ioctl argument
	if err := l.file.Close(); err != nil {
		return fmt.Errorf("close USB device: %w", err)
	}
	return nil
}
