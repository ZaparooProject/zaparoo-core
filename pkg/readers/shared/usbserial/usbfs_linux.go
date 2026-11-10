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
	"errors"
	"fmt"
	"os"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

// supported reports whether this system has usbfs.
const supported = true

// usbfs requests, from linux/usbdevice_fs.h. The control and bulk request
// numbers depend on the pointer size, so they are built from the structs.
const (
	usbfsClaimInterface   = 0x8004550F
	usbfsReleaseInterface = 0x80045510
	usbfsClearHalt        = 0x80045515

	// usbfsMaxTimeout keeps a transfer timeout well inside uint32
	// milliseconds; usbfs treats zero as no timeout.
	usbfsMaxTimeout = time.Hour
)

// usbfsCtrlArg is struct usbdevfs_ctrltransfer.
//
//nolint:govet // the field order is the kernel's
type usbfsCtrlArg struct {
	reqType uint8
	request uint8
	value   uint16
	index   uint16
	length  uint16
	timeout uint32
	data    unsafe.Pointer
}

// usbfsBulkArg is struct usbdevfs_bulktransfer.
//
//nolint:govet // the field order is the kernel's
type usbfsBulkArg struct {
	ep      uint32
	length  uint32
	timeout uint32
	data    unsafe.Pointer
}

var (
	// _IOWR('U', 0, struct usbdevfs_ctrltransfer)
	usbfsControl = uintptr(3)<<30 | unsafe.Sizeof(usbfsCtrlArg{})<<16 | uintptr('U')<<8
	usbfsBulk    = uintptr(3)<<30 | unsafe.Sizeof(usbfsBulkArg{})<<16 | uintptr('U')<<8 | 2
)

// usbfsDevice is a USB device opened through usbfs with one interface
// claimed.
type usbfsDevice struct {
	file  *os.File
	iface uint32
}

// openUSBDevice opens a usbfs node and claims an interface. The claim is a
// plain one: it fails with EBUSY when any driver has the interface, so a
// kernel driver is never detached.
func openUSBDevice(node string, iface uint8) (usbDevice, error) {
	file, err := os.OpenFile(node, os.O_RDWR, 0) //nolint:gosec // node is built from sysfs bus and device numbers
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("open %s: %w", node, ErrDeviceGone)
		}
		return nil, fmt.Errorf("open %s: %w", node, err)
	}
	dev := &usbfsDevice{file: file, iface: uint32(iface)}
	number := dev.iface
	if _, err := dev.ioctl(usbfsClaimInterface, unsafe.Pointer(&number)); err != nil { //nolint:gosec // ioctl argument
		_ = file.Close()
		if errors.Is(err, unix.EBUSY) {
			return nil, fmt.Errorf("claim interface %d: %w", iface, ErrDriverBound)
		}
		return nil, fmt.Errorf("claim interface %d: %w", iface, mapErrno(err))
	}
	return dev, nil
}

// ioctl runs one request and returns the kernel's errno unchanged. It goes
// through the file's raw connection so the descriptor stays open until the
// request returns, even if the device is closed meanwhile.
func (d *usbfsDevice) ioctl(request uintptr, arg unsafe.Pointer) (int, error) {
	conn, err := d.file.SyscallConn()
	if err != nil {
		return 0, fmt.Errorf("usbfs: %w", err)
	}
	var n uintptr
	var errno unix.Errno
	ctlErr := conn.Control(func(fd uintptr) {
		n, _, errno = unix.Syscall(unix.SYS_IOCTL, fd, request, uintptr(arg))
	})
	if ctlErr != nil {
		return 0, fmt.Errorf("usbfs: %w", ctlErr)
	}
	if errno != 0 {
		return 0, errno
	}
	return int(n), nil
}

// mapErrno turns usbfs errors into the package's errors.
func mapErrno(err error) error {
	switch {
	case errors.Is(err, unix.EPIPE):
		return errStall
	case errors.Is(err, unix.ETIMEDOUT):
		return errTimeout
	case errors.Is(err, unix.ENODEV), errors.Is(err, unix.ESHUTDOWN),
		errors.Is(err, unix.ENOENT):
		return fmt.Errorf("%w (%w)", ErrDeviceGone, err)
	default:
		return fmt.Errorf("usbfs: %w", err)
	}
}

func timeoutMillis(timeout time.Duration) uint32 {
	timeout = min(max(timeout, time.Millisecond), usbfsMaxTimeout)
	return uint32(timeout.Milliseconds()) //nolint:gosec // bounded by usbfsMaxTimeout
}

func (d *usbfsDevice) control(
	reqType, request uint8, value, index uint16, data []byte, timeout time.Duration,
) (int, error) {
	if len(data) > 0xFFFF {
		return 0, fmt.Errorf("control transfer of %d bytes is too long", len(data))
	}
	arg := usbfsCtrlArg{
		reqType: reqType,
		request: request,
		value:   value,
		index:   index,
		length:  uint16(len(data)), //nolint:gosec // checked above
		timeout: timeoutMillis(timeout),
	}
	if len(data) > 0 {
		arg.data = unsafe.Pointer(&data[0]) //nolint:gosec // ioctl buffer
	}
	n, err := d.ioctl(usbfsControl, unsafe.Pointer(&arg)) //nolint:gosec // ioctl argument
	if err != nil {
		return 0, mapErrno(err)
	}
	return n, nil
}

func (d *usbfsDevice) transfer(endpoint uint8, data []byte, timeout time.Duration) (int, error) {
	arg := usbfsBulkArg{
		ep:      uint32(endpoint),
		length:  uint32(len(data)), //nolint:gosec // reads are one packet, writes one chunk
		timeout: timeoutMillis(timeout),
	}
	if len(data) > 0 {
		arg.data = unsafe.Pointer(&data[0]) //nolint:gosec // ioctl buffer
	}
	n, err := d.ioctl(usbfsBulk, unsafe.Pointer(&arg)) //nolint:gosec // ioctl argument
	if err != nil {
		return 0, mapErrno(err)
	}
	return n, nil
}

func (d *usbfsDevice) clearHalt(endpoint uint8) error {
	ep := uint32(endpoint)
	if _, err := d.ioctl(usbfsClearHalt, unsafe.Pointer(&ep)); err != nil { //nolint:gosec // ioctl argument
		return mapErrno(err)
	}
	return nil
}

func (d *usbfsDevice) close() error {
	number := d.iface
	_, _ = d.ioctl(usbfsReleaseInterface, unsafe.Pointer(&number)) //nolint:gosec // ioctl argument
	if err := d.file.Close(); err != nil {
		return fmt.Errorf("close USB device: %w", err)
	}
	return nil
}
