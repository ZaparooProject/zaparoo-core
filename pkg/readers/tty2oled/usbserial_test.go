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

package tty2oled

import (
	"errors"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/shared/usbserial"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.bug.st/serial"
)

// probePort answers a hardware info probe with reply. detectDevice closes it
// on its own goroutine after reporting, so its state is guarded.
type probePort struct {
	serial.Port
	reply   string
	written []byte
	mu      syncutil.Mutex
	closed  bool
}

func (p *probePort) Write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.written = append(p.written, b...)
	return len(b), nil
}

func (p *probePort) state() (written string, closed bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return string(p.written), p.closed
}

func (p *probePort) isClosed() bool {
	_, closed := p.state()
	return closed
}

func (p *probePort) Read(b []byte) (int, error) {
	return copy(b, p.reply), nil
}

func (*probePort) SetReadTimeout(time.Duration) error { return nil }

func (p *probePort) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	return nil
}

func TestDetectIncludesUSBSerialAdapters(t *testing.T) {
	t.Parallel()

	reader := &Reader{}
	var mu syncutil.Mutex
	var opened []string
	display := &probePort{reply: "HWESP32DE;"}
	reader.listSerialPorts = func() ([]string, error) { return []string{"/dev/ttyUSB0"}, nil }
	reader.listUSBSerial = func() ([]usbserial.PortInfo, error) {
		return []usbserial.PortInfo{
			{Path: "usbserial:1-3", Chip: "CH34x"},
			{Path: "usbserial:1-4", Chip: "PL2303"},
		}, nil
	}
	reader.openPort = func(path string, _ *serial.Mode) (serial.Port, error) {
		mu.Lock()
		defer mu.Unlock()
		opened = append(opened, path)
		if path == "usbserial:1-4" {
			return display, nil
		}
		return nil, errors.New("not a display")
	}

	got := reader.Detect([]string{"pn532_uart:usbserial:1-3"})
	assert.Equal(t, "tty2oled:usbserial:1-4", got)

	mu.Lock()
	defer mu.Unlock()
	assert.NotContains(t, opened, "usbserial:1-3", "a port in use by another reader is not probed")
	assert.Equal(t, []string{"/dev/ttyUSB0", "usbserial:1-4"}, opened)
	assert.Eventually(t, display.isClosed, time.Second, time.Millisecond)
	written, _ := display.state()
	assert.Equal(t, CmdHardwareInfo+CommandTerminator, written)
}

func TestUSBSerialPortsListError(t *testing.T) {
	t.Parallel()

	reader := &Reader{}
	reader.listUSBSerial = func() ([]usbserial.PortInfo, error) { return nil, errors.New("sysfs gone") }
	assert.Empty(t, reader.usbSerialPorts())
}

func TestDetectDeviceRejectsOtherReplies(t *testing.T) {
	t.Parallel()

	reader := &Reader{}
	port := &probePort{reply: "garbage"}
	reader.openPort = func(string, *serial.Mode) (serial.Port, error) { return port, nil }
	assert.False(t, reader.detectDevice("usbserial:1-4"))
	assert.Eventually(t, port.isClosed, time.Second, time.Millisecond)
}

func TestReaderIDUsesUSBSerialTopology(t *testing.T) {
	t.Parallel()

	reader := &Reader{}
	reader.path = "usbserial:1-4"
	require.NotEmpty(t, reader.ReaderID())
}
