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
	"bytes"
	"errors"
	"io"
	"runtime"
	"syscall"
	"testing"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.bug.st/serial"
)

type portRig struct {
	dev   *fakeDevice
	clock *clockwork.FakeClock
	o     opener
	nodes []string
	slept []time.Duration
}

func newPortRig(t *testing.T, d *sysDevice, dev *fakeDevice) *portRig {
	t.Helper()
	fs := afero.NewMemMapFs()
	addSysDevice(t, fs, d)
	rig := &portRig{dev: dev, clock: clockwork.NewFakeClock()}
	rig.o = opener{
		sys: sysfs{fs: fs},
		openDevice: func(node string, iface uint8) (usbDevice, error) {
			rig.nodes = append(rig.nodes, node)
			assert.Equal(t, uint8(0), iface)
			return dev, nil
		},
		clock: rig.clock,
		sleep: func(d time.Duration) { rig.slept = append(rig.slept, d) },
	}
	return rig
}

func openCH340(t *testing.T, dev *fakeDevice) (*Port, *portRig) {
	t.Helper()
	rig := newPortRig(t, &sysDevice{name: "1-2", descriptors: ch340Descriptors(), bus: 1, dev: 5}, dev)
	p, err := rig.o.open("usbserial:1-2", &serial.Mode{BaudRate: 115200})
	require.NoError(t, err)
	return p, rig
}

func TestOpenCH340(t *testing.T) {
	t.Parallel()
	dev := ch34xDevice(0x31, false)
	p, rig := openCH340(t, dev)
	assert.Equal(t, []string{"/dev/bus/usb/001/005"}, rig.nodes)
	assert.Equal(t, "usbserial:1-2", p.Path())
	assert.Equal(t, []ctrlCall{
		in(reqTypeVendorIn, ch34xReqReadVersion, 0, 0, 2),
		out(reqTypeVendorOut, ch34xReqSerialInit, 0, 0),
		in(reqTypeVendorIn, ch34xReqReadReg, ch34xRegBreak, 0, 2),
		out(reqTypeVendorOut, ch34xReqWriteReg, 0x1312, 0xCC83),
		out(reqTypeVendorOut, ch34xReqWriteReg, 0x2518, 0xC3),
		out(reqTypeVendorOut, ch34xReqWriteReg, 0x2727, 0),
		// DTR and RTS raised, inverted.
		out(reqTypeVendorOut, ch34xReqModemCtrl, 0xFF9F, 0),
	}, dev.takeCalls())

	require.NoError(t, p.Close())
	assert.Equal(t, []ctrlCall{out(reqTypeVendorOut, ch34xReqModemCtrl, 0xFFFF, 0)}, dev.takeCalls())
	assert.Equal(t, 1, dev.closed)
	require.NoError(t, p.Close(), "second close is a no-op")
	assert.Equal(t, 1, dev.closed)
}

func TestOpenInitialStatusBits(t *testing.T) {
	t.Parallel()
	dev := ch34xDevice(0x31, false)
	rig := newPortRig(t, &sysDevice{name: "1-2", descriptors: ch340Descriptors(), bus: 1, dev: 5}, dev)
	p, err := rig.o.open("usbserial:1-2", &serial.Mode{
		InitialStatusBits: &serial.ModemOutputBits{DTR: false, RTS: true},
	})
	require.NoError(t, err)
	calls := dev.takeCalls()
	assert.Equal(t, out(reqTypeVendorOut, ch34xReqModemCtrl, 0xFFBF, 0), calls[len(calls)-1])
	assert.Equal(t, out(reqTypeVendorOut, ch34xReqWriteReg, 0x1312, 0xB282), calls[3], "default 9600 baud")
	require.NoError(t, p.Close())
}

func TestOpenPL2303(t *testing.T) {
	t.Parallel()
	dev := pl2303Device(false, 0)
	rig := newPortRig(t, &sysDevice{name: "3-1.2", descriptors: pl2303HXDescriptors(), bus: 3, dev: 12}, dev)
	p, err := rig.o.open("usbserial:3-1.2", &serial.Mode{BaudRate: 115200})
	require.NoError(t, err)
	assert.Equal(t, []string{"/dev/bus/usb/003/012"}, rig.nodes)
	calls := dev.takeCalls()
	assert.Equal(t, out(reqTypeClassOut, pl2303ReqSetControl, 0x03, 0), calls[len(calls)-1])
	require.NoError(t, p.Close())
}

func TestOpenRefusals(t *testing.T) {
	t.Parallel()
	dev := ch34xDevice(0x31, false)
	bound := &sysDevice{name: "1-2", descriptors: ch340Descriptors(), bus: 1, dev: 5, driver: "ch341"}
	rig := newPortRig(t, bound, dev)

	_, err := rig.o.open("usbserial:1-2", nil)
	require.ErrorIs(t, err, ErrDriverBound)
	_, err = rig.o.open("usbserial:1-3", nil)
	require.ErrorIs(t, err, ErrNotFound)
	_, err = rig.o.open("usbserial:../1-2", nil)
	require.ErrorIs(t, err, ErrBadPath)
	_, err = rig.o.open("usbserial:1-2", &serial.Mode{DataBits: 3})
	require.Error(t, err)
	assert.Empty(t, rig.nodes, "no device is opened for a refused port")
	assert.Empty(t, dev.takeCalls())
}

// setNodeTime gives the adapter a usbfs node last changed at when, which on a
// real system is when it was plugged in.
func setNodeTime(t *testing.T, rig *portRig, node string, when time.Time) {
	t.Helper()
	fs := rig.o.sys.fs
	require.NoError(t, afero.WriteFile(fs, node, nil, 0o600))
	require.NoError(t, fs.Chtimes(node, when, when))
}

// A kernel driver that is still loading has not bound the adapter yet, so one
// plugged in a moment ago is left alone until the driver has had its chance.
func TestNewAdapterIsLeftToSettle(t *testing.T) {
	t.Parallel()
	const node = "/dev/bus/usb/001/005"
	dev := ch34xDevice(0x31, false)
	rig := newPortRig(t, &sysDevice{name: "1-2", descriptors: ch340Descriptors(), bus: 1, dev: 5}, dev)
	setNodeTime(t, rig, node, rig.clock.Now())

	for _, wait := range []time.Duration{0, settleTime - time.Millisecond} {
		rig.clock.Advance(wait)
		infos, err := rig.o.list()
		require.NoError(t, err)
		assert.Empty(t, infos, "listed too soon after plug-in")
		_, err = rig.o.open("usbserial:1-2", nil)
		require.ErrorIs(t, err, ErrSettling)
	}
	assert.Empty(t, rig.nodes, "a settling adapter is not opened")
	assert.Empty(t, dev.takeCalls())

	rig.clock.Advance(time.Millisecond)
	infos, err := rig.o.list()
	require.NoError(t, err)
	require.Len(t, infos, 1)
	assert.Equal(t, "usbserial:1-2", infos[0].Path)
	p, err := rig.o.open("usbserial:1-2", nil)
	require.NoError(t, err)
	require.NoError(t, p.Close())
	assert.Equal(t, []string{node}, rig.nodes)
}

// The wait only applies when the plug-in time is known and in the past: a
// missing node or a clock that has moved backwards must not hide an adapter
// for good.
func TestSettleNeedsAKnownPlugInTime(t *testing.T) {
	t.Parallel()
	rig := newPortRig(t, &sysDevice{name: "1-2", descriptors: ch340Descriptors(), bus: 1, dev: 5},
		ch34xDevice(0x31, false))

	infos, err := rig.o.list()
	require.NoError(t, err)
	assert.Len(t, infos, 1, "no usbfs node to read a time from")

	setNodeTime(t, rig, "/dev/bus/usb/001/005", rig.clock.Now().Add(time.Hour))
	infos, err = rig.o.list()
	require.NoError(t, err)
	assert.Len(t, infos, 1, "node time is in the future")

	setNodeTime(t, rig, "/dev/bus/usb/001/005", rig.clock.Now().Add(-time.Hour))
	infos, err = rig.o.list()
	require.NoError(t, err)
	assert.Len(t, infos, 1, "plugged in long ago")
}

func TestOpenFailuresCloseDevice(t *testing.T) {
	t.Parallel()
	failing := []*fakeDevice{
		// Init fails.
		{onControl: func(ctrlCall, []byte) (int, error) { return 0, ErrDeviceGone }},
		// Old chip refuses 7-bit framing.
		ch34xDevice(0x20, false),
	}
	for i, dev := range failing {
		rig := newPortRig(t, &sysDevice{name: "1-2", descriptors: ch340Descriptors(), bus: 1, dev: 5}, dev)
		_, err := rig.o.open("usbserial:1-2", &serial.Mode{DataBits: 7})
		require.Error(t, err, i)
		assert.Equal(t, 1, dev.closed, i)
	}

	// The handshake fails.
	dev := ch34xDevice(0x31, false)
	onControl := dev.onControl
	dev.onControl = func(call ctrlCall, buf []byte) (int, error) {
		if call.request == ch34xReqModemCtrl {
			return 0, ErrDeviceGone
		}
		return onControl(call, buf)
	}
	rig := newPortRig(t, &sysDevice{name: "1-2", descriptors: ch340Descriptors(), bus: 1, dev: 5}, dev)
	_, err := rig.o.open("usbserial:1-2", nil)
	require.ErrorIs(t, err, ErrDeviceGone)
	assert.Equal(t, 1, dev.closed)

	// The device cannot be opened.
	rig = newPortRig(t, &sysDevice{name: "1-2", descriptors: ch340Descriptors(), bus: 1, dev: 5}, dev)
	rig.o.openDevice = func(string, uint8) (usbDevice, error) { return nil, ErrDriverBound }
	_, err = rig.o.open("usbserial:1-2", nil)
	require.ErrorIs(t, err, ErrDriverBound)
}

// packets returns a transfer function that hands out the given packets on
// the bulk IN endpoint, then times out, advancing clock by each wait.
func packets(
	t *testing.T, clock *clockwork.FakeClock, waits *[]time.Duration, data ...[]byte,
) func(uint8, []byte, time.Duration) (int, error) {
	t.Helper()
	return func(ep uint8, buf []byte, timeout time.Duration) (int, error) {
		assert.Equal(t, uint8(0x83), ep)
		*waits = append(*waits, timeout)
		if len(data) == 0 {
			clock.Advance(timeout)
			return 0, errTimeout
		}
		n := copy(buf, data[0])
		data = data[1:]
		return n, nil
	}
}

func TestReadFraming(t *testing.T) {
	t.Parallel()
	dev := ch34xDevice(0x31, false)
	p, rig := openCH340(t, dev)
	defer func() { _ = p.Close() }()
	var waits []time.Duration
	dev.onTransfer = packets(t, rig.clock, &waits, []byte("0123456789"), []byte("ab"))
	require.NoError(t, p.SetReadTimeout(time.Second))

	buf := make([]byte, 4)
	got := make([]string, 0, 4)
	for range 4 {
		n, err := p.Read(buf)
		require.NoError(t, err)
		got = append(got, string(buf[:n]))
	}
	assert.Equal(t, []string{"0123", "4567", "89", "ab"}, got)
	assert.Len(t, waits, 2, "leftover bytes are served without a transfer")

	n, err := p.Read(nil)
	require.NoError(t, err)
	assert.Zero(t, n)
}

func TestReadTimeout(t *testing.T) {
	t.Parallel()
	dev := ch34xDevice(0x31, false)
	p, rig := openCH340(t, dev)
	defer func() { _ = p.Close() }()
	var waits []time.Duration
	dev.onTransfer = packets(t, rig.clock, &waits)

	require.NoError(t, p.SetReadTimeout(250*time.Millisecond))
	start := rig.clock.Now()
	n, err := p.Read(make([]byte, 8))
	require.NoError(t, err)
	assert.Zero(t, n)
	assert.Equal(t, []time.Duration{100 * time.Millisecond, 100 * time.Millisecond, 50 * time.Millisecond}, waits)
	assert.Equal(t, 250*time.Millisecond, rig.clock.Since(start))

	waits = nil
	require.NoError(t, p.SetReadTimeout(0))
	n, err = p.Read(make([]byte, 8))
	require.NoError(t, err)
	assert.Zero(t, n)
	assert.Equal(t, []time.Duration{pollWait}, waits)

	require.Error(t, p.SetReadTimeout(-5*time.Second))
}

func TestReadNoTimeoutStopsOnClose(t *testing.T) {
	t.Parallel()
	dev := ch34xDevice(0x31, false)
	p, _ := openCH340(t, dev)
	closed := make(chan error, 1)
	calls := 0
	dev.onTransfer = func(_ uint8, _ []byte, timeout time.Duration) (int, error) {
		assert.Equal(t, readSlice, timeout)
		calls++
		if calls == 3 {
			go func() { closed <- p.Close() }()
			for !p.closed.Load() {
				runtime.Gosched()
			}
		}
		return 0, errTimeout
	}

	n, err := p.Read(make([]byte, 8))
	require.ErrorIs(t, err, ErrClosed)
	assert.Zero(t, n)
	require.NoError(t, <-closed)
	assert.Equal(t, 3, calls)

	_, err = p.Read(make([]byte, 8))
	require.ErrorIs(t, err, ErrClosed)
}

func TestReadDeviceGone(t *testing.T) {
	t.Parallel()
	dev := ch34xDevice(0x31, false)
	p, _ := openCH340(t, dev)
	defer func() { _ = p.Close() }()
	dev.onTransfer = func(uint8, []byte, time.Duration) (int, error) { return 0, ErrDeviceGone }

	_, err := p.Read(make([]byte, 8))
	require.ErrorIs(t, err, ErrDeviceGone)
	require.ErrorIs(t, err, syscall.ENODEV, "readers recognise ENODEV as an unplugged device")
}

func TestWriteChunks(t *testing.T) {
	t.Parallel()
	dev := ch34xDevice(0x31, false)
	p, _ := openCH340(t, dev)
	defer func() { _ = p.Close() }()
	var sent bytes.Buffer
	var sizes []int
	var timeouts []time.Duration
	dev.onTransfer = func(ep uint8, data []byte, timeout time.Duration) (int, error) {
		assert.Equal(t, uint8(0x02), ep)
		sent.Write(data)
		sizes = append(sizes, len(data))
		timeouts = append(timeouts, timeout)
		return len(data), nil
	}

	msg := bytes.Repeat([]byte("zaparoo!"), 1250)
	n, err := p.Write(msg)
	require.NoError(t, err)
	assert.Equal(t, len(msg), n)
	assert.Equal(t, msg, sent.Bytes())
	assert.Equal(t, []int{4096, 4096, 1808}, sizes)
	assert.Equal(t, time.Second+4096*20*time.Second/115200, timeouts[0])

	dev.onTransfer = func(uint8, []byte, time.Duration) (int, error) { return 0, nil }
	_, err = p.Write([]byte("x"))
	require.ErrorIs(t, err, io.ErrShortWrite)

	dev.onTransfer = func(_ uint8, data []byte, _ time.Duration) (int, error) { return len(data) / 2, ErrDeviceGone }
	n, err = p.Write([]byte("abcd"))
	require.ErrorIs(t, err, ErrDeviceGone)
	assert.Equal(t, 2, n)
}

func TestResetInputBuffer(t *testing.T) {
	t.Parallel()
	dev := ch34xDevice(0x31, false)
	p, rig := openCH340(t, dev)
	defer func() { _ = p.Close() }()
	var waits []time.Duration
	dev.onTransfer = packets(t, rig.clock, &waits, []byte("0123456789"), []byte("stale"), []byte("more"))
	require.NoError(t, p.SetReadTimeout(time.Second))

	n, err := p.Read(make([]byte, 2))
	require.NoError(t, err)
	assert.Equal(t, 2, n)

	waits = nil
	require.NoError(t, p.ResetInputBuffer())
	assert.Equal(t, []time.Duration{pollWait, pollWait, pollWait}, waits)

	n, err = p.Read(make([]byte, 8))
	require.NoError(t, err)
	assert.Zero(t, n, "pending and queued bytes are gone")

	dev.onTransfer = func(uint8, []byte, time.Duration) (int, error) { return 0, ErrDeviceGone }
	require.ErrorIs(t, p.ResetInputBuffer(), ErrDeviceGone)
}

func TestPortControl(t *testing.T) {
	t.Parallel()
	dev := ch34xDevice(0x31, false)
	p, _ := openCH340(t, dev)
	dev.takeCalls()

	require.NoError(t, p.SetDTR(false))
	require.NoError(t, p.SetRTS(false))
	require.NoError(t, p.SetDTR(true))
	assert.Equal(t, []ctrlCall{
		out(reqTypeVendorOut, ch34xReqModemCtrl, 0xFFBF, 0),
		out(reqTypeVendorOut, ch34xReqModemCtrl, 0xFFFF, 0),
		out(reqTypeVendorOut, ch34xReqModemCtrl, 0xFFDF, 0),
	}, dev.takeCalls())

	bits, err := p.GetModemStatusBits()
	require.NoError(t, err)
	assert.Equal(t, &serial.ModemStatusBits{CTS: true, DSR: true, RI: true, DCD: true}, bits)
	dev.takeCalls()

	// The same mode is not sent again; a new one is.
	require.NoError(t, p.SetMode(&serial.Mode{BaudRate: 115200}))
	assert.Empty(t, dev.takeCalls())
	require.NoError(t, p.SetMode(&serial.Mode{BaudRate: 9600}))
	assert.Equal(t, out(reqTypeVendorOut, ch34xReqWriteReg, 0x1312, 0xB282), dev.takeCalls()[0])
	require.Error(t, p.SetMode(&serial.Mode{DataBits: 2}))

	require.NoError(t, p.Drain())
	require.NoError(t, p.ResetOutputBuffer())

	require.NoError(t, p.Close())
	for name, err := range map[string]error{
		"SetDTR":            p.SetDTR(true),
		"SetRTS":            p.SetRTS(true),
		"SetMode":           p.SetMode(&serial.Mode{BaudRate: 19200}),
		"SetReadTimeout":    p.SetReadTimeout(time.Second),
		"Drain":             p.Drain(),
		"ResetInputBuffer":  p.ResetInputBuffer(),
		"ResetOutputBuffer": p.ResetOutputBuffer(),
		"Break":             p.Break(time.Millisecond),
	} {
		require.ErrorIs(t, err, ErrClosed, name)
	}
	_, err = p.GetModemStatusBits()
	require.ErrorIs(t, err, ErrClosed)
	_, err = p.Write([]byte("x"))
	require.ErrorIs(t, err, ErrClosed)
}

func TestPortBreak(t *testing.T) {
	t.Parallel()
	dev := ch34xDevice(0x31, true)
	p, rig := openCH340(t, dev)
	defer func() { _ = p.Close() }()
	var written []byte
	dev.onTransfer = func(ep uint8, data []byte, _ time.Duration) (int, error) {
		assert.Equal(t, uint8(0x02), ep)
		written = append(written, data...)
		return len(data), nil
	}
	require.NoError(t, p.Break(time.Millisecond))
	assert.Equal(t, []byte{0}, written)
	assert.Equal(t, []time.Duration{ch34xSimulatedBreak}, rig.slept)

	dev.onTransfer = func(uint8, []byte, time.Duration) (int, error) { return 0, errors.New("boom") }
	require.Error(t, p.Break(time.Millisecond))
}

func TestPublicAPIOnUnsupportedPaths(t *testing.T) {
	t.Parallel()
	if !supported {
		_, err := Open("usbserial:1-2", nil)
		require.ErrorIs(t, err, ErrUnsupported)
		require.ErrorIs(t, Exists("usbserial:1-2"), ErrUnsupported)
		infos, err := List()
		require.NoError(t, err)
		assert.Empty(t, infos)
		assert.Empty(t, DeviceNode("usbserial:1-2"))
		return
	}
	_, err := Open("usbserial:../x", nil)
	require.ErrorIs(t, err, ErrBadPath)
	require.ErrorIs(t, Exists("/dev/ttyUSB0"), ErrBadPath)
	assert.Empty(t, DeviceNode("usbserial:bad"))
}
