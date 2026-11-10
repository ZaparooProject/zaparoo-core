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

// Package usbserial drives CH34x and PL2303 USB serial adapters from
// userspace, through the kernel's usbfs, for systems whose kernel has no
// driver for them. It only ever takes an interface no kernel driver is bound
// to and never detaches one, so where the kernel's ch341 or pl2303 driver is
// present the adapter is left to it and appears as a normal /dev/ttyUSB port.
//
// An adapter is addressed as "usbserial:<port>", where <port> is the USB
// topology name the kernel gives the device in /sys/bus/usb/devices, such as
// "usbserial:1-1.2". The name follows the physical port, so it survives
// unplugging and matches the topology Core uses to identify readers on
// /dev/ttyUSB ports.
package usbserial

import (
	"errors"
	"fmt"
	"strings"
	"syscall"
	"time"
)

// PathPrefix starts every port path this package serves.
const PathPrefix = "usbserial:"

var (
	// ErrUnsupported is returned on systems without usbfs.
	ErrUnsupported = errors.New("userspace USB serial is not supported on this system")
	// ErrBadPath is returned for a path that is not a valid usbserial path.
	ErrBadPath = errors.New("not a usbserial port path")
	// ErrNotFound is returned when no supported adapter is at the path.
	ErrNotFound = errors.New("no supported USB serial adapter at this port")
	// ErrDriverBound is returned when a kernel driver or another program
	// already has the adapter. The driver is never detached.
	ErrDriverBound = errors.New("USB serial adapter is bound to a driver")
	// ErrClosed is returned by operations on a closed port.
	ErrClosed = errors.New("usbserial port is closed")
	// ErrDeviceGone is returned once the adapter has been unplugged. It
	// wraps ENODEV so code that classifies errno values sees an unplugged
	// device.
	ErrDeviceGone = fmt.Errorf("USB serial adapter is gone: %w", syscall.ENODEV)

	// errStall is a request the device refused with a stall.
	errStall = errors.New("USB request stalled")
	// errTimeout is a transfer that did not complete in time.
	errTimeout = errors.New("USB transfer timed out")
)

// IsPath reports whether path is addressed to this package.
func IsPath(path string) bool {
	return strings.HasPrefix(path, PathPrefix)
}

// MakePath returns the port path for the USB device with the given sysfs name.
func MakePath(sysName string) string {
	return PathPrefix + sysName
}

// Topology returns the USB topology of a port path, such as "1-1.2", or an
// empty string when the path is not valid.
func Topology(path string) string {
	sysName, err := parsePath(path)
	if err != nil {
		return ""
	}
	return sysName
}

// parsePath returns the sysfs device name of a port path. The name must be a
// plain USB topology ("<bus>-<port>[.<port>...]"), so it can never reach
// outside the sysfs USB device directory.
func parsePath(path string) (string, error) {
	sysName, ok := strings.CutPrefix(path, PathPrefix)
	if !ok || !validSysName(sysName) {
		return "", fmt.Errorf("%w: %q", ErrBadPath, path)
	}
	return sysName, nil
}

func validSysName(name string) bool {
	bus, ports, ok := strings.Cut(name, "-")
	if !ok || !allDigits(bus) {
		return false
	}
	for _, port := range strings.Split(ports, ".") {
		if !allDigits(port) {
			return false
		}
	}
	return true
}

func allDigits(s string) bool {
	if s == "" || len(s) > 3 {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// PortInfo describes a supported adapter with no driver bound to it.
type PortInfo struct {
	// Path is the port path to open, such as "usbserial:1-1.2".
	Path string
	// Name is the manufacturer and product strings, when the device has them.
	Name string
	// Chip is the adapter family, "CH34x" or "PL2303".
	Chip string
	// Serial is the device's serial number string, when it has one.
	Serial    string
	VendorID  uint16
	ProductID uint16
}

// VIDPID returns the vendor and product IDs as "VVVV:PPPP" in upper case hex.
func (i *PortInfo) VIDPID() string {
	return fmt.Sprintf("%04X:%04X", i.VendorID, i.ProductID)
}

// usbDevice is a USB device with its serial interface claimed. It is the
// boundary to the kernel: usbfs on Linux, a fake in tests.
type usbDevice interface {
	// control runs a control transfer on endpoint 0. For a request with the
	// direction bit set, data receives the reply; otherwise it is sent.
	control(reqType, request uint8, value, index uint16, data []byte, timeout time.Duration) (int, error)
	// transfer runs a bulk or interrupt transfer. The direction follows the
	// endpoint address.
	transfer(endpoint uint8, data []byte, timeout time.Duration) (int, error)
	// clearHalt clears a stall on an endpoint.
	clearHalt(endpoint uint8) error
	// close releases the interface and the device.
	close() error
}
