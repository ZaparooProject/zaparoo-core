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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.bug.st/serial"
)

var testIface = serialInterface{
	number: 0, bulkIn: 0x83, bulkOut: 0x02, intrIn: 0x81, bulkInPacket: 64, intrPacket: 10,
}

func pl2303Desc(bcdUSB, bcdDevice uint16) deviceDescriptor {
	return deviceDescriptor{
		bcdUSB: bcdUSB, bcdDevice: bcdDevice, maxPacket0: 0x40,
		vendorID: vendorProlific, productID: 0x2303,
	}
}

// pl2303Device answers the HX status probe when hxStatus is set and stalls it
// otherwise, and answers vendor reads with reg.
func pl2303Device(hxStatus bool, reg uint8) *fakeDevice {
	return &fakeDevice{onControl: func(call ctrlCall, buf []byte) (int, error) {
		if call.value == pl2303ReadHXStatus && call.request == pl2303ReqVendorRead && call.reqType == reqTypeVendorIn {
			if !hxStatus {
				return 0, errStall
			}
		}
		if call.reqType&0x80 != 0 {
			for i := range buf {
				buf[i] = reg
			}
		}
		return len(buf), nil
	}}
}

func TestDetectPL2303Type(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		desc     deviceDescriptor
		hxStatus bool
		want     pl2303Type
	}{
		{
			name: "CDC class is H",
			desc: deviceDescriptor{class: 0x02, maxPacket0: 0x40, bcdUSB: 0x110},
			want: pl2303TypeH,
		},
		{name: "small EP0 is H", desc: deviceDescriptor{maxPacket0: 0x08, bcdUSB: 0x110}, want: pl2303TypeH},
		{name: "HX", desc: pl2303Desc(0x110, 0x300), want: pl2303TypeHX},
		{name: "HX USB 1.01", desc: pl2303Desc(0x101, 0x300), want: pl2303TypeHX},
		{name: "HXD", desc: pl2303Desc(0x110, 0x400), want: pl2303TypeHXD},
		{name: "TA", desc: pl2303Desc(0x200, 0x300), hxStatus: true, want: pl2303TypeTA},
		{name: "G at 0x300", desc: pl2303Desc(0x200, 0x300), want: pl2303TypeHXN},
		{name: "TB", desc: pl2303Desc(0x200, 0x500), hxStatus: true, want: pl2303TypeTB},
		{name: "G at 0x500", desc: pl2303Desc(0x200, 0x500), want: pl2303TypeHXN},
		{name: "G", desc: pl2303Desc(0x200, 0x100), want: pl2303TypeHXN},
		{name: "GS", desc: pl2303Desc(0x200, 0x905), want: pl2303TypeHXN},
	}
	for _, tc := range cases {
		got, err := detectPL2303Type(pl2303Device(tc.hxStatus, 0), tc.desc)
		require.NoError(t, err, tc.name)
		assert.Equal(t, tc.want, got, tc.name)
	}

	descs := []deviceDescriptor{pl2303Desc(0x200, 0x3302), pl2303Desc(0x200, 0x6506), pl2303Desc(0x300, 0x300)}
	for _, desc := range descs {
		_, err := detectPL2303Type(pl2303Device(true, 0), desc)
		require.ErrorIs(t, err, ErrUnsupportedChip)
	}
}

func TestNewPL2303StartupSequence(t *testing.T) {
	t.Parallel()
	dev := pl2303Device(false, 0)
	p, err := newPL2303(dev, pl2303Desc(0x110, 0x300), testIface)
	require.NoError(t, err)
	assert.Equal(t, pl2303TypeHX, p.typ)
	rd := func(v uint16) ctrlCall { return in(reqTypeVendorIn, pl2303ReqVendorRead, v, 0, 1) }
	wr := func(v, i uint16) ctrlCall { return out(reqTypeVendorOut, pl2303ReqVendorWrite, v, i) }
	assert.Equal(t, []ctrlCall{
		rd(0x8484), wr(0x0404, 0), rd(0x8484), rd(0x8383),
		rd(0x8484), wr(0x0404, 1), rd(0x8484), rd(0x8383),
		wr(0, 1), wr(1, 0), wr(2, 0x44),
		wr(8, 0), wr(9, 0),
	}, dev.takeCalls())
	assert.Empty(t, dev.halts)
}

func TestNewPL2303TypeH(t *testing.T) {
	t.Parallel()
	dev := pl2303Device(false, 0)
	p, err := newPL2303(dev, deviceDescriptor{class: 0x02, maxPacket0: 0x40}, testIface)
	require.NoError(t, err)
	assert.Equal(t, pl2303TypeH, p.typ)
	calls := dev.takeCalls()
	require.Len(t, calls, 11)
	assert.Equal(t, out(reqTypeVendorOut, pl2303ReqVendorWrite, 2, 0x24), calls[10])
	assert.Equal(t, []uint8{0x02, 0x83}, dev.halts)
}

func TestNewPL2303TypeG(t *testing.T) {
	t.Parallel()
	dev := pl2303Device(false, 0)
	p, err := newPL2303(dev, pl2303Desc(0x200, 0x100), testIface)
	require.NoError(t, err)
	assert.Equal(t, pl2303TypeHXN, p.typ)
	assert.Equal(t, []ctrlCall{
		out(reqTypeVendorOut, pl2303ReqVendorWriteN, pl2303HXNResetReg, pl2303HXNResetPipes),
	}, dev.takeCalls())
}

func TestNewPL2303HXDClone(t *testing.T) {
	t.Parallel()
	dev := &fakeDevice{onControl: func(call ctrlCall, buf []byte) (int, error) {
		if call.request == pl2303ReqGetLine && call.reqType == reqTypeClassIn {
			return 0, errStall
		}
		return len(buf), nil
	}}
	p, err := newPL2303(dev, pl2303Desc(0x110, 0x400), testIface)
	require.NoError(t, err)
	assert.True(t, p.noBreak)
	err = p.sendBreak(time.Millisecond, nil, func(time.Duration) {})
	require.ErrorIs(t, err, ErrUnsupportedChip)

	genuine, err := newPL2303(pl2303Device(false, 0), pl2303Desc(0x110, 0x400), testIface)
	require.NoError(t, err)
	assert.False(t, genuine.noBreak)
}

func TestNewPL2303StartupError(t *testing.T) {
	t.Parallel()
	dev := &fakeDevice{onControl: func(ctrlCall, []byte) (int, error) { return 0, ErrDeviceGone }}
	_, err := newPL2303(dev, pl2303Desc(0x110, 0x300), testIface)
	require.ErrorIs(t, err, ErrDeviceGone)
}

func TestEncodePL2303Baud(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		typ  pl2303Type
		baud int
		want [4]byte
	}{
		{name: "direct", typ: pl2303TypeHX, baud: 115200, want: [4]byte{0x00, 0xC2, 0x01, 0x00}},
		{name: "direct 9600", typ: pl2303TypeTA, baud: 9600, want: [4]byte{0x80, 0x25, 0x00, 0x00}},
		{name: "divisor", typ: pl2303TypeHX, baud: 500000, want: [4]byte{192, 0x02, 0x00, 0x80}},
		{name: "alt divisor", typ: pl2303TypeTA, baud: 500000, want: [4]byte{0x00, 0x03, 0x00, 0x80}},
		{name: "alt divisor exponent", typ: pl2303TypeTB, baud: 110, want: [4]byte{0xA8, 0xA6, 0x01, 0x80}},
		{name: "divisor clamp", typ: pl2303TypeHX, baud: 1, want: [4]byte{0xFF, 0x0F, 0x00, 0x80}},
		{name: "G is always direct", typ: pl2303TypeHXN, baud: 500000, want: [4]byte{0x20, 0xA1, 0x07, 0x00}},
		{name: "H is capped", typ: pl2303TypeH, baud: 3000000, want: [4]byte{0x00, 0xC0, 0x12, 0x00}},
	}
	for _, tc := range cases {
		var buf [4]byte
		encodePL2303Baud(tc.typ, tc.baud, buf[:])
		assert.Equal(t, tc.want, buf, tc.name)
	}
}

func TestNearestPL2303DirectRate(t *testing.T) {
	t.Parallel()
	assert.Equal(t, 75, nearestPL2303DirectRate(1))
	assert.Equal(t, 9600, nearestPL2303DirectRate(9600))
	assert.Equal(t, 460800, nearestPL2303DirectRate(500000))
	assert.Equal(t, 614400, nearestPL2303DirectRate(600000))
	assert.Equal(t, 6000000, nearestPL2303DirectRate(20000000))
}

func TestPL2303SetLine(t *testing.T) {
	t.Parallel()
	dev := pl2303Device(false, 0xF5)
	p, err := newPL2303(dev, pl2303Desc(0x110, 0x300), testIface)
	require.NoError(t, err)
	p.iface.number = 2
	dev.takeCalls()

	line := lineSettings{baud: 115200, dataBits: 7, parity: serial.OddParity, stopBits: serial.TwoStopBits}
	require.NoError(t, p.setLine(line))
	assert.Equal(t, []ctrlCall{
		{
			reqType: reqTypeClassOut, request: pl2303ReqSetLine, index: 2, length: 7,
			data: []byte{0x00, 0xC2, 0x01, 0x00, 2, 1, 7},
		},
		in(reqTypeVendorIn, pl2303ReqVendorRead, 0x80, 0, 1),
		out(reqTypeVendorOut, pl2303ReqVendorWrite, 0, 0x05),
	}, dev.takeCalls())

	// Unchanged settings are not sent again; flow control still is.
	require.NoError(t, p.setLine(line))
	calls := dev.takeCalls()
	require.Len(t, calls, 2)
	assert.Equal(t, uint8(pl2303ReqVendorRead), calls[0].request)

	line.parity, line.stopBits = serial.SpaceParity, serial.OnePointFiveStopBits
	require.NoError(t, p.setLine(line))
	calls = dev.takeCalls()
	require.Len(t, calls, 3)
	assert.Equal(t, []byte{0x00, 0xC2, 0x01, 0x00, 1, 4, 7}, calls[0].data)
}

func TestPL2303SetLineTypeG(t *testing.T) {
	t.Parallel()
	dev := pl2303Device(false, 0x03)
	p, err := newPL2303(dev, pl2303Desc(0x200, 0x100), testIface)
	require.NoError(t, err)
	dev.takeCalls()

	require.NoError(t, p.setLine(lineSettings{baud: 9600, dataBits: 8, parity: serial.MarkParity}))
	assert.Equal(t, []ctrlCall{
		{
			reqType: reqTypeClassOut, request: pl2303ReqSetLine, length: 7,
			data: []byte{0x80, 0x25, 0x00, 0x00, 0, 3, 8},
		},
		in(reqTypeVendorIn, pl2303ReqVendorReadN, pl2303HXNFlowCtlReg, 0, 1),
		out(reqTypeVendorOut, pl2303ReqVendorWriteN, pl2303HXNFlowCtlReg, 0x1F),
	}, dev.takeCalls())
}

func TestPL2303ControlAndBreak(t *testing.T) {
	t.Parallel()
	dev := pl2303Device(false, 0)
	p := &pl2303{dev: dev, typ: pl2303TypeHX, iface: testIface}
	p.iface.number = 1

	require.NoError(t, p.setControl(true, false))
	require.NoError(t, p.setControl(false, true))
	var slept []time.Duration
	require.NoError(t, p.sendBreak(30*time.Millisecond, nil, func(d time.Duration) { slept = append(slept, d) }))
	assert.Equal(t, []time.Duration{30 * time.Millisecond}, slept)
	assert.Equal(t, []ctrlCall{
		out(reqTypeClassOut, pl2303ReqSetControl, 0x01, 1),
		out(reqTypeClassOut, pl2303ReqSetControl, 0x02, 1),
		out(reqTypeClassOut, pl2303ReqBreak, 0xFFFF, 1),
		out(reqTypeClassOut, pl2303ReqBreak, 0x0000, 1),
	}, dev.takeCalls())
}

func TestPL2303ModemStatus(t *testing.T) {
	t.Parallel()
	reports := [][]byte{
		{0xA1, 0x20, 0, 0, 0, 0, 0x02, 0, 0x08, 0},
		{0xA1, 0x20, 0, 0, 0, 0, 0x02, 0, 0x83, 0},
	}
	var eps []uint8
	dev := &fakeDevice{onTransfer: func(ep uint8, data []byte, _ time.Duration) (int, error) {
		eps = append(eps, ep)
		if len(reports) == 0 {
			return 0, errTimeout
		}
		n := copy(data, reports[0])
		reports = reports[1:]
		return n, nil
	}}
	p := &pl2303{dev: dev, typ: pl2303TypeHX, iface: testIface}

	bits, err := p.modemStatus()
	require.NoError(t, err)
	assert.Equal(t, &serial.ModemStatusBits{CTS: true, DSR: true, DCD: true}, bits)
	assert.Equal(t, []uint8{0x81, 0x81, 0x81}, eps)

	// No new report keeps the last state.
	bits, err = p.modemStatus()
	require.NoError(t, err)
	assert.Equal(t, &serial.ModemStatusBits{CTS: true, DSR: true, DCD: true}, bits)

	dev.onTransfer = func(uint8, []byte, time.Duration) (int, error) { return 0, ErrDeviceGone }
	_, err = p.modemStatus()
	require.ErrorIs(t, err, ErrDeviceGone)

	p.iface.intrIn = 0
	_, err = p.modemStatus()
	require.ErrorIs(t, err, ErrUnsupportedChip)
}

func TestPL2303TypeString(t *testing.T) {
	t.Parallel()
	names := map[pl2303Type]string{
		pl2303TypeH: "H", pl2303TypeHX: "HX", pl2303TypeTA: "TA", pl2303TypeTB: "TB",
		pl2303TypeHXD: "HXD", pl2303TypeHXN: "G", 0: "unknown",
	}
	for typ, name := range names {
		assert.Equal(t, name, typ.String())
	}
}
