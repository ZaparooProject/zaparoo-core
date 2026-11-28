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
	"errors"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.bug.st/serial"
)

// timeoutFailPort is a port that opens but refuses a read timeout, and can
// also fail to close.
type timeoutFailPort struct {
	closeErr error
	mu       syncutil.Mutex
	closed   bool
}

func (*timeoutFailPort) Read([]byte) (int, error)    { return 0, nil }
func (*timeoutFailPort) Write(b []byte) (int, error) { return len(b), nil }

func (*timeoutFailPort) SetReadTimeout(time.Duration) error {
	return errors.New("ioctl failed")
}

func (p *timeoutFailPort) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	return p.closeErr
}

func (p *timeoutFailPort) isClosed() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.closed
}

// countingFactory records every port the reader tries to open.
type countingFactory struct {
	open  func(path string) (serialPort, error)
	paths []string
	modes []serial.Mode
	mu    syncutil.Mutex
}

func (c *countingFactory) factory(path string, mode *serial.Mode) (serialPort, error) {
	c.mu.Lock()
	c.paths = append(c.paths, path)
	c.modes = append(c.modes, *mode)
	c.mu.Unlock()
	return c.open(path)
}

func (c *countingFactory) opened() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.paths...)
}

func newProbeReader(factory *countingFactory, vid string) *Reader {
	r := NewReader(&config.Instance{})
	r.clock = clockwork.NewFakeClock()
	r.portFactory = factory.factory
	r.vendorIDs = func(string) (string, string, bool) { return vid, "1001", true }
	return r
}

func TestProbeIdentifiesDisplayAndReleasesPort(t *testing.T) {
	t.Parallel()

	dev := newFakeDevice()
	factory := &countingFactory{open: func(string) (serialPort, error) { return dev, nil }}
	r := newProbeReader(factory, espressifVID)

	require.True(t, r.probe(testPort))

	assert.Equal(t, []string{testPort}, factory.opened())
	require.Len(t, factory.modes, 1)
	assert.Equal(t, baudRate, factory.modes[0].BaudRate)
	assert.Equal(t, 8, factory.modes[0].DataBits)

	commands := dev.commandLog()
	require.GreaterOrEqual(t, len(commands), 3)
	assert.Equal(t, []string{"HELLO " + protocolVersion, "INFO", "QUIET"}, commands[:3])
	// Open re-opens the winner, so the probe must not keep the port.
	assert.True(t, dev.isClosed(), "a probe must release the port it opened")
	assert.False(t, r.Connected(), "probing does not connect the reader")
}

func TestProbeRejectsPortThatAnswersWrongly(t *testing.T) {
	t.Parallel()

	// Something on the port speaks a line protocol but refuses the handshake.
	dev := newFakeDevice()
	dev.rejectCommand("HELLO")
	factory := &countingFactory{open: func(string) (serialPort, error) { return dev, nil }}
	r := newProbeReader(factory, espressifVID)

	assert.False(t, r.probe(testPort))
	assert.Equal(t, []string{"HELLO " + protocolVersion}, dev.commandLog(),
		"nothing past HELLO may be written to a device that refused it")
	assert.True(t, dev.isClosed(), "a failed probe must still release the port")
}

func TestProbeFailsWhenPortCannotOpen(t *testing.T) {
	t.Parallel()

	factory := &countingFactory{open: func(string) (serialPort, error) {
		return nil, errors.New("permission denied")
	}}
	r := newProbeReader(factory, espressifVID)

	assert.False(t, r.probe(testPort))
	assert.Equal(t, []string{testPort}, factory.opened())
}

func TestDialClosesPortWhenReadTimeoutFails(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		closeErr error
		name     string
	}{
		{name: "close succeeds"},
		{name: "close also fails", closeErr: errors.New("close failed")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			port := &timeoutFailPort{closeErr: tc.closeErr}
			r := NewReader(&config.Instance{})
			r.portFactory = func(string, *serial.Mode) (serialPort, error) { return port, nil }

			sess, err := r.dial(testPort)
			require.Error(t, err)
			assert.Nil(t, sess)
			assert.Contains(t, err.Error(), "set read timeout")
			assert.Contains(t, err.Error(), testPort)
			// Without a read timeout every read would block forever, so the
			// port is unusable and must not be leaked.
			assert.True(t, port.isClosed())
		})
	}
}

func TestDialReportsOpenFailure(t *testing.T) {
	t.Parallel()

	openErr := errors.New("no such device")
	r := NewReader(&config.Instance{})
	r.portFactory = func(string, *serial.Mode) (serialPort, error) { return nil, openErr }

	sess, err := r.dial(testPort)
	require.ErrorIs(t, err, openErr)
	assert.Nil(t, sess)
	assert.Contains(t, err.Error(), testPort)
}

func TestSessionCloseReportsPortFailure(t *testing.T) {
	t.Parallel()

	closeErr := errors.New("device gone")
	sess := newSession(&timeoutFailPort{closeErr: closeErr})
	err := sess.close()
	require.ErrorIs(t, err, closeErr)
	assert.Contains(t, err.Error(), "close display port")
}

// The Detect tests read the machine's real serial port list, which cannot be
// injected. The port factory is always a fake, so no real device is ever
// opened, and each test asserts something that holds whatever ports exist.
// They share the package-level failed-probe cache, so they do not run in
// parallel.

func TestDetectNeverOpensPortsFromOtherVendors(t *testing.T) {
	resetProbeState(t)

	factory := &countingFactory{open: func(string) (serialPort, error) { return newFakeDevice(), nil }}
	r := newProbeReader(factory, "2341")

	// An Arduino-class board or 3D printer must never be written to.
	assert.Empty(t, r.Detect(nil))
	assert.Empty(t, factory.opened())
}

func TestDetectSkipsPortsClaimedByOtherReaders(t *testing.T) {
	resetProbeState(t)

	ports, err := helpers.GetUSBCDCDeviceList()
	require.NoError(t, err)
	connected := make([]string, 0, len(ports))
	for _, path := range ports {
		connected = append(connected, "pn532:"+path)
	}

	factory := &countingFactory{open: func(string) (serialPort, error) { return newFakeDevice(), nil }}
	r := newProbeReader(factory, espressifVID)

	// Writing a handshake to a port another driver owns would corrupt its
	// stream, so a claimed port is never opened.
	assert.Empty(t, r.Detect(connected))
	assert.Empty(t, factory.opened())
}

func TestDetectReturnsTheFirstPortThatAnswers(t *testing.T) {
	resetProbeState(t)

	ports, err := helpers.GetUSBCDCDeviceList()
	require.NoError(t, err)
	if len(ports) == 0 {
		t.Skip("no USB CDC ports on this machine to detect against")
	}

	factory := &countingFactory{open: func(string) (serialPort, error) { return newFakeDevice(), nil }}
	r := newProbeReader(factory, espressifVID)

	assert.Equal(t, driverID+":"+ports[0], r.Detect(nil))
	assert.Equal(t, []string{ports[0]}, factory.opened(), "detection stops at the first display")
}

func TestDetectDoesNotReprobeAPortThatFailed(t *testing.T) {
	resetProbeState(t)

	ports, err := helpers.GetUSBCDCDeviceList()
	require.NoError(t, err)
	if len(ports) == 0 {
		t.Skip("no USB CDC ports on this machine to detect against")
	}

	factory := &countingFactory{open: func(string) (serialPort, error) {
		dev := newFakeDevice()
		dev.rejectCommand("HELLO")
		return dev, nil
	}}
	r := newProbeReader(factory, espressifVID)

	assert.Empty(t, r.Detect(nil))
	first := len(factory.opened())
	require.Positive(t, first)

	// Auto-detect runs every second; a port that is not a display must not be
	// opened and written to again on the next tick.
	assert.Empty(t, r.Detect(nil))
	assert.Len(t, factory.opened(), first)
}
