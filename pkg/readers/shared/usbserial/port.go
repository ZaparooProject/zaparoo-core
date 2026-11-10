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
	"io"
	"sync/atomic"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/jonboulle/clockwork"
	"github.com/spf13/afero"
	"go.bug.st/serial"
)

const (
	// readSlice bounds each bulk read, so a blocked Read notices Close and
	// its own deadline promptly.
	readSlice = 100 * time.Millisecond
	// pollWait is the wait used for a non-blocking read.
	pollWait = time.Millisecond
	// writeChunk bounds the bytes sent per bulk transfer.
	writeChunk = 4096
	// writeTimeoutBase is added to the time the bytes take on the wire.
	writeTimeoutBase = time.Second
	// defaultPacket is used when an endpoint reports no packet size.
	defaultPacket = 64
	// resetDrainReads bounds the packets ResetInputBuffer discards.
	resetDrainReads = 64
)

// opener finds and opens adapters. The default one uses the host's sysfs and
// usbfs; tests swap both.
type opener struct {
	sys        sysfs
	openDevice func(node string, iface uint8) (usbDevice, error)
	clock      clockwork.Clock
	sleep      func(time.Duration)
}

func defaultOpener() opener {
	return opener{
		sys:        sysfs{fs: afero.NewOsFs()},
		openDevice: openUSBDevice,
		clock:      clockwork.NewRealClock(),
		sleep:      time.Sleep,
	}
}

// Open opens the adapter at a usbserial path and applies mode. It fails with
// ErrDriverBound when a kernel driver or another program has the adapter.
func Open(path string, mode *serial.Mode) (*Port, error) {
	if !supported {
		return nil, ErrUnsupported
	}
	return defaultOpener().open(path, mode)
}

// List returns the supported adapters no driver is bound to, sorted by path.
// It reads sysfs only and opens no device.
func List() ([]PortInfo, error) {
	if !supported {
		return nil, nil
	}
	return defaultOpener().sys.list()
}

// Exists reports whether a supported adapter is plugged in at path, whether
// or not it is free to open.
func Exists(path string) error {
	if !supported {
		return ErrUnsupported
	}
	sysName, err := parsePath(path)
	if err != nil {
		return err
	}
	_, err = defaultOpener().sys.lookup(sysName)
	return err
}

// DeviceNode returns the usbfs device node of the adapter at path, such as
// "/dev/bus/usb/001/004", or an empty string when there is none. The node is
// recreated with a new number whenever the adapter is plugged in again.
func DeviceNode(path string) string {
	if !supported {
		return ""
	}
	sysName, err := parsePath(path)
	if err != nil {
		return ""
	}
	t, err := defaultOpener().sys.lookup(sysName)
	if err != nil {
		return ""
	}
	return t.node()
}

func (o opener) open(path string, mode *serial.Mode) (*Port, error) {
	sysName, err := parsePath(path)
	if err != nil {
		return nil, err
	}
	line, err := lineFromMode(mode)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	t, err := o.sys.lookup(sysName)
	if err != nil {
		return nil, err
	}
	if t.driver != "" {
		return nil, fmt.Errorf("%w: %s is bound to %q", ErrDriverBound, path, t.driver)
	}

	dev, err := o.openDevice(t.node(), t.iface.number)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	chip, err := newChipDriver(dev, t)
	if err != nil {
		_ = dev.close()
		return nil, fmt.Errorf("%s: %w", path, err)
	}

	p := &Port{
		dev:   dev,
		chip:  chip,
		clock: o.clock,
		sleep: o.sleep,
		path:  path,
		in:    t.iface.bulkIn,
		out:   t.iface.bulkOut,
	}
	packet := int(t.iface.bulkInPacket)
	if packet == 0 {
		packet = defaultPacket
	}
	p.buf = make([]byte, packet)
	p.readTimeout.Store(int64(serial.NoTimeout))

	dtr, rts := true, true
	if mode != nil && mode.InitialStatusBits != nil {
		dtr, rts = mode.InitialStatusBits.DTR, mode.InitialStatusBits.RTS
	}
	if err := p.applyLine(line); err != nil {
		_ = dev.close()
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if err := chip.setControl(dtr, rts); err != nil {
		_ = dev.close()
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	p.dtr, p.rts = dtr, rts
	return p, nil
}

// Port is an open adapter. It implements go.bug.st/serial's Port.
//
// Locks are taken in the order ctrlMu, writeMu, readMu.
type Port struct {
	dev   usbDevice
	chip  chipDriver
	clock clockwork.Clock
	sleep func(time.Duration)
	path  string
	// pending holds bytes of a received packet that did not fit the
	// caller's buffer. Guarded by readMu, as is buf.
	pending     []byte
	buf         []byte
	readTimeout atomic.Int64
	baud        atomic.Int64
	line        lineSettings
	closed      atomic.Bool
	// ctrlMu guards line, dtr, rts and every chip request.
	ctrlMu  syncutil.Mutex
	writeMu syncutil.Mutex
	readMu  syncutil.Mutex
	dtr     bool
	rts     bool
	in      uint8
	out     uint8
}

var _ serial.Port = (*Port)(nil)

// Path returns the port path.
func (p *Port) Path() string {
	return p.path
}

// applyLine sets the line and records it. Callers hold ctrlMu, or own the
// port before it is returned.
func (p *Port) applyLine(line lineSettings) error {
	if err := p.chip.setLine(line); err != nil {
		return err
	}
	p.line = line
	p.baud.Store(int64(line.baud))
	return nil
}

func (p *Port) wrap(err error) error {
	return fmt.Errorf("%s: %w", p.path, err)
}

// SetMode changes the baud rate and framing. A mode equal to the current one
// is not sent again, as rewriting it can make a chip drop bytes.
func (p *Port) SetMode(mode *serial.Mode) error {
	line, err := lineFromMode(mode)
	if err != nil {
		return p.wrap(err)
	}
	p.ctrlMu.Lock()
	defer p.ctrlMu.Unlock()
	if p.closed.Load() {
		return p.wrap(ErrClosed)
	}
	if line == p.line {
		return nil
	}
	if err := p.applyLine(line); err != nil {
		return p.wrap(err)
	}
	return nil
}

// Read reads received bytes. It waits up to the read timeout for the first
// byte and returns 0 bytes and no error when none arrive. Each USB packet is
// read whole; bytes that do not fit p are returned by the next Read.
func (p *Port) Read(b []byte) (int, error) {
	if p.closed.Load() {
		return 0, p.wrap(ErrClosed)
	}
	if len(b) == 0 {
		return 0, nil
	}
	p.readMu.Lock()
	defer p.readMu.Unlock()

	if len(p.pending) > 0 {
		n := copy(b, p.pending)
		p.pending = p.pending[n:]
		return n, nil
	}

	timeout := time.Duration(p.readTimeout.Load())
	deadline := p.clock.Now().Add(timeout)
	for {
		if p.closed.Load() {
			return 0, p.wrap(ErrClosed)
		}
		wait := readSlice
		switch {
		case timeout == 0:
			wait = pollWait
		case timeout > 0:
			remaining := deadline.Sub(p.clock.Now())
			if remaining <= 0 {
				return 0, nil
			}
			wait = max(min(remaining, readSlice), pollWait)
		}

		n, err := p.dev.transfer(p.in, p.buf, wait)
		switch {
		case errors.Is(err, errTimeout), err == nil && n == 0:
			if timeout == 0 {
				return 0, nil
			}
			continue
		case err != nil:
			return 0, p.wrap(err)
		}
		copied := copy(b, p.buf[:n])
		p.pending = append(p.pending[:0], p.buf[copied:n]...)
		return copied, nil
	}
}

// Write sends b. It returns once the adapter has taken every byte.
func (p *Port) Write(b []byte) (int, error) {
	if p.closed.Load() {
		return 0, p.wrap(ErrClosed)
	}
	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	return p.writeLocked(b)
}

func (p *Port) writeLocked(b []byte) (int, error) {
	written := 0
	for written < len(b) {
		if p.closed.Load() {
			return written, p.wrap(ErrClosed)
		}
		chunk := b[written:min(len(b), written+writeChunk)]
		n, err := p.dev.transfer(p.out, chunk, p.writeTimeout(len(chunk)))
		written += n
		if err != nil {
			return written, p.wrap(err)
		}
		if n == 0 {
			return written, p.wrap(io.ErrShortWrite)
		}
	}
	return written, nil
}

// writeTimeout allows twice the time the bytes take on the wire, plus a
// second for the USB side.
func (p *Port) writeTimeout(n int) time.Duration {
	baud := max(p.baud.Load(), 1)
	wire := time.Duration(int64(n) * 10 * 2 * int64(time.Second) / baud)
	return writeTimeoutBase + wire
}

// Drain returns at once: a write already waits for the adapter to take the
// bytes, and the adapters cannot report when their own buffer has emptied.
func (p *Port) Drain() error {
	if p.closed.Load() {
		return p.wrap(ErrClosed)
	}
	return nil
}

// ResetInputBuffer discards received bytes, both those held by the port and
// those waiting on the adapter. It waits for a Read in progress to return.
func (p *Port) ResetInputBuffer() error {
	p.readMu.Lock()
	defer p.readMu.Unlock()
	if p.closed.Load() {
		return p.wrap(ErrClosed)
	}
	p.pending = p.pending[:0]
	for range resetDrainReads {
		n, err := p.dev.transfer(p.in, p.buf, pollWait)
		if errors.Is(err, errTimeout) || (err == nil && n == 0) {
			return nil
		}
		if err != nil {
			return p.wrap(err)
		}
	}
	return nil
}

// ResetOutputBuffer does nothing: a write returns only once the adapter has
// taken the bytes, so the port holds none back.
func (p *Port) ResetOutputBuffer() error {
	if p.closed.Load() {
		return p.wrap(ErrClosed)
	}
	return nil
}

// SetDTR sets the DTR output.
func (p *Port) SetDTR(dtr bool) error {
	p.ctrlMu.Lock()
	defer p.ctrlMu.Unlock()
	if p.closed.Load() {
		return p.wrap(ErrClosed)
	}
	if err := p.chip.setControl(dtr, p.rts); err != nil {
		return p.wrap(err)
	}
	p.dtr = dtr
	return nil
}

// SetRTS sets the RTS output.
func (p *Port) SetRTS(rts bool) error {
	p.ctrlMu.Lock()
	defer p.ctrlMu.Unlock()
	if p.closed.Load() {
		return p.wrap(ErrClosed)
	}
	if err := p.chip.setControl(p.dtr, rts); err != nil {
		return p.wrap(err)
	}
	p.rts = rts
	return nil
}

// GetModemStatusBits reads the modem input lines.
func (p *Port) GetModemStatusBits() (*serial.ModemStatusBits, error) {
	p.ctrlMu.Lock()
	defer p.ctrlMu.Unlock()
	if p.closed.Load() {
		return nil, p.wrap(ErrClosed)
	}
	bits, err := p.chip.modemStatus()
	if err != nil {
		return nil, p.wrap(err)
	}
	return bits, nil
}

// SetReadTimeout sets how long Read waits for data. serial.NoTimeout waits
// until data arrives or the port is closed; zero does not wait.
func (p *Port) SetReadTimeout(t time.Duration) error {
	if t < 0 && t != serial.NoTimeout {
		return p.wrap(fmt.Errorf("invalid read timeout %v", t))
	}
	if p.closed.Load() {
		return p.wrap(ErrClosed)
	}
	p.readTimeout.Store(int64(t))
	return nil
}

// Break holds a break condition on the line for d. CH34x chips without break
// control simulate one of about 200ms.
func (p *Port) Break(d time.Duration) error {
	p.ctrlMu.Lock()
	defer p.ctrlMu.Unlock()
	if p.closed.Load() {
		return p.wrap(ErrClosed)
	}
	write := func(b []byte) error {
		p.writeMu.Lock()
		defer p.writeMu.Unlock()
		_, err := p.writeLocked(b)
		return err
	}
	if err := p.chip.sendBreak(d, write, p.sleep); err != nil {
		return p.wrap(err)
	}
	return nil
}

// Close lowers DTR and RTS, releases the adapter and stops any Read in
// progress within one read slice. Closing again does nothing.
func (p *Port) Close() error {
	if !p.closed.CompareAndSwap(false, true) {
		return nil
	}
	p.ctrlMu.Lock()
	defer p.ctrlMu.Unlock()
	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	p.readMu.Lock()
	defer p.readMu.Unlock()

	// Best effort: the adapter may already be unplugged.
	_ = p.chip.setControl(false, false)
	if err := p.dev.close(); err != nil {
		return p.wrap(err)
	}
	return nil
}
