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

//nolint:paralleltest // these tests swap the package's usbserial and detection hooks
package pn532

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ZaparooProject/go-pn532/detection"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/shared/usbserial"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.bug.st/serial"
)

// usbPort is a serial.Port that records what the UART transport sets on it.
type usbPort struct {
	serial.Port
	mode    *serial.Mode
	timeout time.Duration
	closed  int
}

func (p *usbPort) SetMode(mode *serial.Mode) error {
	p.mode = mode
	return nil
}

func (p *usbPort) SetReadTimeout(t time.Duration) error {
	p.timeout = t
	return nil
}

func (p *usbPort) Close() error {
	p.closed++
	return nil
}

func swapUSBSerialOpen(t *testing.T, open func(string, *serial.Mode) (serial.Port, error)) {
	t.Helper()
	orig := openUSBSerialPort
	t.Cleanup(func() { openUSBSerialPort = orig })
	openUSBSerialPort = open
}

func TestDefaultTransportFactory_USBSerialPath(t *testing.T) {
	port := &usbPort{}
	var opened []string
	swapUSBSerialOpen(t, func(path string, mode *serial.Mode) (serial.Port, error) {
		opened = append(opened, path)
		assert.Equal(t, 115200, mode.BaudRate)
		return port, nil
	})

	transport, err := DefaultTransportFactory(detection.DeviceInfo{Transport: "uart", Path: "usbserial:1-2"})
	require.NoError(t, err)
	require.NotNil(t, transport)
	assert.Equal(t, []string{"usbserial:1-2"}, opened)
	require.NotNil(t, port.mode, "the transport applies the PN532 UART mode")
	assert.Equal(t, 115200, port.mode.BaudRate)
	assert.Positive(t, port.timeout)

	require.NoError(t, transport.Close())
	assert.Equal(t, 1, port.closed)
}

func TestDefaultTransportFactory_USBSerialOpenError(t *testing.T) {
	errOpen := errors.New("adapter bound to a driver")
	swapUSBSerialOpen(t, func(string, *serial.Mode) (serial.Port, error) { return nil, errOpen })

	_, err := DefaultTransportFactory(detection.DeviceInfo{Transport: "uart", Path: "usbserial:1-2"})
	require.ErrorIs(t, err, errOpen)

	// The default probe treats an adapter that will not open as no PN532.
	assert.False(t, probeUSBSerialPort(context.Background(), "usbserial:1-2"))
}

func TestDetect_ProbesUSBSerialAdapters(t *testing.T) {
	resetFailedProbes(t)
	resetDetectSummary(t)

	origDetect, origList, origProbe := detectAll, listUSBSerial, probeUSBSerial
	t.Cleanup(func() { detectAll, listUSBSerial, probeUSBSerial = origDetect, origList, origProbe })
	detectAll = func(context.Context, *detection.Options) ([]detection.DeviceInfo, error) {
		return nil, detection.ErrNoDevicesFound
	}
	listUSBSerial = func() ([]usbserial.PortInfo, error) {
		return []usbserial.PortInfo{
			// A Sinden Lightgun's ID is on the blocklist.
			{Path: "usbserial:1-1", VendorID: 0x16C0, ProductID: 0x0F38},
			{Path: "usbserial:1-2", VendorID: 0x1A86, ProductID: 0x7523},
			{Path: "usbserial:1-3", VendorID: 0x1A86, ProductID: 0x7523},
			{Path: "usbserial:1-4", VendorID: 0x067B, ProductID: 0x2303},
		}, nil
	}
	var probed []string
	probeUSBSerial = func(ctx context.Context, path string) bool {
		deadline, ok := ctx.Deadline()
		assert.True(t, ok)
		assert.LessOrEqual(t, time.Until(deadline), usbSerialProbeTimeout)
		probed = append(probed, path)
		return path == "usbserial:1-4"
	}

	got := (&Reader{}).Detect([]string{"simple_serial:usbserial:1-2"})
	assert.Equal(t, "pn532_uart:usbserial:1-4", got)
	assert.Equal(t, []string{"usbserial:1-3", "usbserial:1-4"}, probed,
		"blocklisted and connected adapters are not probed")
}

func TestDetect_USBSerialListErrorKeepsDetectionResult(t *testing.T) {
	resetFailedProbes(t)
	resetDetectSummary(t)

	origDetect, origList := detectAll, listUSBSerial
	t.Cleanup(func() { detectAll, listUSBSerial = origDetect, origList })
	detectAll = func(context.Context, *detection.Options) ([]detection.DeviceInfo, error) {
		return []detection.DeviceInfo{{Transport: "uart", Path: "/dev/ttyUSB0"}}, nil
	}
	listUSBSerial = func() ([]usbserial.PortInfo, error) { return nil, errors.New("no sysfs") }

	assert.Equal(t, "pn532_uart:/dev/ttyUSB0", (&Reader{}).Detect(nil))
}

func TestDetectUSBSerial_SkipsIgnoredAndStopsWhenCancelled(t *testing.T) {
	origList, origProbe := listUSBSerial, probeUSBSerial
	t.Cleanup(func() { listUSBSerial, probeUSBSerial = origList, origProbe })
	listUSBSerial = func() ([]usbserial.PortInfo, error) {
		return []usbserial.PortInfo{{Path: "usbserial:1-1"}, {Path: "usbserial:1-2"}}, nil
	}
	probeUSBSerial = func(context.Context, string) bool {
		t.Fatal("no adapter may be probed")
		return false
	}

	devices, probes := detectUSBSerial(context.Background(), []string{"usbserial:1-1", "usbserial:1-2"})
	assert.Empty(t, devices)
	assert.Empty(t, probes)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	devices, probes = detectUSBSerial(ctx, nil)
	assert.Empty(t, devices)
	assert.Empty(t, probes)
}

func TestProbeStatPath(t *testing.T) {
	assert.Equal(t, "/dev/ttyUSB0", probeStatPath("/dev/ttyUSB0"))
	// An adapter that is not plugged in has no usbfs node to stat.
	assert.Empty(t, probeStatPath("usbserial:250-9"))
}
