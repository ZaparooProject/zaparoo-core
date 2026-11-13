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

// ch34xDevice answers the CH34x init requests with the given version, and
// stalls the break register read when quirky.
func ch34xDevice(version uint8, quirky bool) *fakeDevice {
	return &fakeDevice{onControl: func(call ctrlCall, buf []byte) (int, error) {
		switch {
		case call.request == ch34xReqReadVersion:
			buf[0], buf[1] = version, 0
			return 2, nil
		case call.request == ch34xReqReadReg && call.value == ch34xRegBreak && quirky:
			return 0, errStall
		case call.reqType&0x80 != 0:
			clear(buf)
			return len(buf), nil
		default:
			return len(buf), nil
		}
	}}
}

func TestNewCH34xInitSequence(t *testing.T) {
	t.Parallel()
	dev := ch34xDevice(0x31, false)
	c, err := newCH34x(dev)
	require.NoError(t, err)
	assert.Equal(t, []ctrlCall{
		in(reqTypeVendorIn, ch34xReqReadVersion, 0, 0, 2),
		out(reqTypeVendorOut, ch34xReqSerialInit, 0, 0),
		in(reqTypeVendorIn, ch34xReqReadReg, ch34xRegBreak, 0, 2),
	}, dev.takeCalls())
	assert.Equal(t, uint8(0x31), c.version)
	assert.False(t, c.limitedPrescaler)
	assert.False(t, c.simulateBreak)
}

func TestNewCH34xQuirk(t *testing.T) {
	t.Parallel()
	c, err := newCH34x(ch34xDevice(0x31, true))
	require.NoError(t, err)
	assert.True(t, c.limitedPrescaler)
	assert.True(t, c.simulateBreak)
}

func TestNewCH34xErrors(t *testing.T) {
	t.Parallel()
	short := &fakeDevice{onControl: func(_ ctrlCall, _ []byte) (int, error) { return 1, nil }}
	_, err := newCH34x(short)
	require.ErrorContains(t, err, "short reply")

	gone := &fakeDevice{onControl: func(call ctrlCall, buf []byte) (int, error) {
		if call.request == ch34xReqReadReg {
			return 0, ErrDeviceGone
		}
		return len(buf), nil
	}}
	_, err = newCH34x(gone)
	require.ErrorIs(t, err, ErrDeviceGone)
}

// ch34xRate decodes a divisor register value back to its baud rate.
func ch34xRate(value uint16) int {
	ps := int(value & 0x03)
	fact := int(value>>2) & 0x01
	div := 0x100 - int(value>>8)
	return ch34xClockRate / ((1 << (12 - 3*ps - fact)) * div)
}

func TestCH34xDivisor(t *testing.T) {
	t.Parallel()
	// Values from the Linux ch341 driver's divisor calculation.
	cases := []struct {
		baud    int
		limited bool
		want    uint16
	}{
		{baud: 9600, want: 0xB202},
		{baud: 115200, want: 0xCC03},
		{baud: 9600, limited: true, want: 0xB202},
		{baud: 50, want: 0x1600},
		{baud: 2000000, want: 0xFD03},
	}
	for _, tc := range cases {
		got, err := ch34xDivisor(tc.baud, tc.limited)
		require.NoError(t, err, tc.baud)
		assert.Equal(t, tc.want, got, "baud %d limited %v: %#04x", tc.baud, tc.limited, got)
	}

	for _, baud := range []int{300, 1200, 2400, 4800, 19200, 38400, 57600, 230400, 460800, 921600, 1500000} {
		for _, limited := range []bool{false, true} {
			value, err := ch34xDivisor(baud, limited)
			require.NoError(t, err, baud)
			got := ch34xRate(value)
			assert.InEpsilon(t, baud, got, 0.03, "baud %d limited %v gave %d", baud, limited, got)
			if limited && value&0x03 != 3 {
				assert.Zero(t, value&0x04, "baud %d: limited prescaler must not use fact", baud)
			}
		}
	}

	// Out-of-range rates are clamped to the chip's range.
	low, err := ch34xDivisor(1, false)
	require.NoError(t, err)
	lowest, err := ch34xDivisor(ch34xMinBaud, false)
	require.NoError(t, err)
	assert.Equal(t, lowest, low)
	high, err := ch34xDivisor(10000000, false)
	require.NoError(t, err)
	assert.Equal(t, ch34xMaxBaud, ch34xRate(high))
}

func TestCH34xSetLine(t *testing.T) {
	t.Parallel()
	dev := ch34xDevice(0x31, false)
	c, err := newCH34x(dev)
	require.NoError(t, err)
	dev.takeCalls()

	require.NoError(t, c.setLine(lineSettings{
		baud: 9600, dataBits: 7, parity: serial.EvenParity, stopBits: serial.TwoStopBits,
	}))
	assert.Equal(t, []ctrlCall{
		out(reqTypeVendorOut, ch34xReqWriteReg, 0x1312, 0xB282),
		out(reqTypeVendorOut, ch34xReqWriteReg, 0x2518,
			ch34xLCREnableRX|ch34xLCREnableTX|0x02|ch34xLCREnablePar|ch34xLCRParEven|ch34xLCRStop2),
		out(reqTypeVendorOut, ch34xReqWriteReg, 0x2727, 0),
	}, dev.takeCalls())

	require.NoError(t, c.setLine(lineSettings{baud: 115200, dataBits: 8}))
	assert.Equal(t, []ctrlCall{
		out(reqTypeVendorOut, ch34xReqWriteReg, 0x1312, 0xCC83),
		out(reqTypeVendorOut, ch34xReqWriteReg, 0x2518, 0xC3),
		out(reqTypeVendorOut, ch34xReqWriteReg, 0x2727, 0),
	}, dev.takeCalls())
}

func TestCH34xLCR(t *testing.T) {
	t.Parallel()
	cases := []struct {
		parity serial.Parity
		want   uint8
	}{
		{serial.NoParity, 0xC3},
		{serial.OddParity, 0xCB},
		{serial.EvenParity, 0xDB},
		{serial.MarkParity, 0xEB},
		{serial.SpaceParity, 0xFB},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, ch34xLCR(lineSettings{dataBits: 8, parity: tc.parity}), tc.parity)
	}
	assert.Equal(t, uint8(0xC4), ch34xLCR(lineSettings{dataBits: 5, stopBits: serial.TwoStopBits}))
}

func TestCH34xOldVersionOnly8N1(t *testing.T) {
	t.Parallel()
	dev := ch34xDevice(0x27, false)
	c, err := newCH34x(dev)
	require.NoError(t, err)
	dev.takeCalls()

	err = c.setLine(lineSettings{baud: 9600, dataBits: 7})
	require.ErrorIs(t, err, ErrUnsupportedChip)
	assert.Empty(t, dev.takeCalls())

	// Version 0x27 skips the LCR write and the no-buffer bit.
	require.NoError(t, c.setLine(lineSettings{baud: 9600, dataBits: 8}))
	assert.Equal(t, []ctrlCall{
		out(reqTypeVendorOut, ch34xReqWriteReg, 0x1312, 0xB202),
		out(reqTypeVendorOut, ch34xReqWriteReg, 0x2727, 0),
	}, dev.takeCalls())
}

func TestCH34xSetControlInverted(t *testing.T) {
	t.Parallel()
	dev := ch34xDevice(0x31, false)
	c, err := newCH34x(dev)
	require.NoError(t, err)
	dev.takeCalls()

	require.NoError(t, c.setControl(true, true))
	require.NoError(t, c.setControl(true, false))
	require.NoError(t, c.setControl(false, false))
	assert.Equal(t, []ctrlCall{
		out(reqTypeVendorOut, ch34xReqModemCtrl, 0xFF9F, 0),
		out(reqTypeVendorOut, ch34xReqModemCtrl, 0xFFDF, 0),
		out(reqTypeVendorOut, ch34xReqModemCtrl, 0xFFFF, 0),
	}, dev.takeCalls())
}

func TestCH34xModemStatus(t *testing.T) {
	t.Parallel()
	dev := &fakeDevice{onControl: func(call ctrlCall, buf []byte) (int, error) {
		if call.value == ch34xModemStatusReg {
			// Inverted: CTS and DCD asserted.
			buf[0], buf[1] = ^uint8(ch34xBitCTS|ch34xBitDCD), 0
		}
		return len(buf), nil
	}}
	c := &ch34x{dev: dev, version: 0x31}
	bits, err := c.modemStatus()
	require.NoError(t, err)
	assert.Equal(t, &serial.ModemStatusBits{CTS: true, DCD: true}, bits)
	assert.Equal(t, []ctrlCall{in(reqTypeVendorIn, ch34xReqReadReg, 0x0706, 0, 2)}, dev.takeCalls())
}

func TestCH34xBreak(t *testing.T) {
	t.Parallel()
	dev := &fakeDevice{onControl: func(call ctrlCall, buf []byte) (int, error) {
		if call.request == ch34xReqReadReg {
			buf[0], buf[1] = 0x01|0x10, 0x40|0x02
		}
		return len(buf), nil
	}}
	c := &ch34x{dev: dev, version: 0x31}
	var slept []time.Duration
	write := func([]byte) error {
		t.Fatal("a real break must not write data")
		return nil
	}
	require.NoError(t, c.sendBreak(50*time.Millisecond, write, func(d time.Duration) {
		slept = append(slept, d)
	}))
	assert.Equal(t, []time.Duration{50 * time.Millisecond}, slept)
	assert.Equal(t, []ctrlCall{
		in(reqTypeVendorIn, ch34xReqReadReg, 0x1805, 0, 2),
		out(reqTypeVendorOut, ch34xReqWriteReg, 0x1805, 0x0210),
		in(reqTypeVendorIn, ch34xReqReadReg, 0x1805, 0, 2),
		out(reqTypeVendorOut, ch34xReqWriteReg, 0x1805, 0x4211),
	}, dev.takeCalls())
}

func TestCH34xSimulatedBreak(t *testing.T) {
	t.Parallel()
	dev := ch34xDevice(0x31, true)
	c, err := newCH34x(dev)
	require.NoError(t, err)
	require.NoError(t, c.setLine(lineSettings{baud: 115200, dataBits: 8}))
	dev.takeCalls()

	var written [][]byte
	var slept []time.Duration
	err = c.sendBreak(10*time.Millisecond, func(b []byte) error {
		written = append(written, append([]byte(nil), b...))
		assert.Equal(t, ch34xSimulatedBreakBaud, c.line.baud, "NUL must go out at the slow rate")
		return nil
	}, func(d time.Duration) { slept = append(slept, d) })
	require.NoError(t, err)
	assert.Equal(t, [][]byte{{0}}, written)
	assert.Equal(t, []time.Duration{ch34xSimulatedBreak}, slept)
	assert.Equal(t, 115200, c.line.baud, "line rate restored")

	slow, err := ch34xDivisor(ch34xSimulatedBreakBaud, true)
	require.NoError(t, err)
	calls := dev.takeCalls()
	require.Len(t, calls, 6)
	assert.Equal(t, out(reqTypeVendorOut, ch34xReqWriteReg, 0x1312, slow|0x80), calls[0])
	assert.Equal(t, uint16(0x1312), calls[3].value)
	assert.NotEqual(t, slow|0x80, calls[3].index)

	// A failed write still restores the rate and reports the write error.
	err = c.sendBreak(0, func([]byte) error { return ErrDeviceGone }, func(time.Duration) {})
	require.ErrorIs(t, err, ErrDeviceGone)
	assert.Equal(t, 115200, c.line.baud)
}
