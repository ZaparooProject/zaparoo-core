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
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"go.bug.st/serial"
)

// PL2303 vendor and CDC-style protocol, as documented by the Linux pl2303
// driver.
const (
	pl2303ReqVendorWrite  = 0x01
	pl2303ReqVendorRead   = 0x01
	pl2303ReqVendorWriteN = 0x80
	pl2303ReqVendorReadN  = 0x81

	pl2303ReqSetLine    = 0x20
	pl2303ReqGetLine    = 0x21
	pl2303ReqSetControl = 0x22
	pl2303ReqBreak      = 0x23

	pl2303ControlDTR = 0x01
	pl2303ControlRTS = 0x02

	pl2303BreakOn  = 0xFFFF
	pl2303BreakOff = 0x0000

	pl2303ReadHXStatus = 0x8080

	pl2303FlowCtlMask    = 0xF0
	pl2303HXNResetReg    = 0x07
	pl2303HXNResetPipes  = 0x03
	pl2303HXNFlowCtlReg  = 0x0A
	pl2303HXNFlowCtlMask = 0x1C
	pl2303HXNFlowCtlNone = 0x1C

	// The interrupt endpoint reports the UART state in this byte.
	pl2303UARTStateIndex = 8
	pl2303UARTDCD        = 0x01
	pl2303UARTDSR        = 0x02
	pl2303UARTRing       = 0x08
	pl2303UARTCTS        = 0x80

	pl2303LineLen = 7

	// pl2303ProbeTimeout matches the short timeout the variant probes are
	// expected to answer within.
	pl2303ProbeTimeout = 100 * time.Millisecond
	// pl2303StatusPoll bounds each read of the interrupt endpoint.
	pl2303StatusPoll = 5 * time.Millisecond
	// pl2303StatusReads bounds how many queued status reports are drained.
	pl2303StatusReads = 8
)

type pl2303Type int

const (
	pl2303TypeH pl2303Type = iota + 1
	pl2303TypeHX
	pl2303TypeTA
	pl2303TypeTB
	pl2303TypeHXD
	pl2303TypeHXN
)

func (t pl2303Type) String() string {
	switch t {
	case pl2303TypeH:
		return "H"
	case pl2303TypeHX:
		return "HX"
	case pl2303TypeTA:
		return "TA"
	case pl2303TypeTB:
		return "TB"
	case pl2303TypeHXD:
		return "HXD"
	case pl2303TypeHXN:
		return "G"
	default:
		return "unknown"
	}
}

// maxBaud is the fastest rate the variant runs at.
func (t pl2303Type) maxBaud() int {
	switch t {
	case pl2303TypeH:
		return 1228800
	case pl2303TypeHX, pl2303TypeTA:
		return 6000000
	default:
		return 12000000
	}
}

// pl2303DirectRates are the rates the chips take as a plain number. Older
// variants fall back to 9600 for any other number, so those get a divisor.
var pl2303DirectRates = []int{
	75, 150, 300, 600, 1200, 1800, 2400, 3600, 4800, 7200, 9600,
	14400, 19200, 28800, 38400, 57600, 115200, 230400, 460800,
	614400, 921600, 1228800, 2457600, 3000000, 6000000,
}

type pl2303 struct {
	dev     usbDevice
	typ     pl2303Type
	iface   serialInterface
	line    [pl2303LineLen]byte
	lineSet bool
	noBreak bool
	status  uint8
}

// detectPL2303Type tells the variants apart from the device descriptor, and
// for two ambiguous releases by whether the chip answers an HX status read.
func detectPL2303Type(dev usbDevice, desc deviceDescriptor) (pl2303Type, error) {
	if desc.class == 0x02 || desc.maxPacket0 != 0x40 {
		return pl2303TypeH, nil
	}
	switch desc.bcdUSB {
	case 0x101, 0x110:
		if desc.bcdDevice == 0x400 {
			return pl2303TypeHXD, nil
		}
		return pl2303TypeHX, nil
	case 0x200:
		switch desc.bcdDevice {
		case 0x300:
			if pl2303SupportsHXStatus(dev) {
				return pl2303TypeTA, nil
			}
			return pl2303TypeHXN, nil
		case 0x500:
			if pl2303SupportsHXStatus(dev) {
				return pl2303TypeTB, nil
			}
			return pl2303TypeHXN, nil
		case 0x100, 0x105, 0x305, 0x400, 0x405, 0x505, 0x600, 0x605, 0x700, 0x705, 0x905, 0x1005:
			return pl2303TypeHXN, nil
		case 0x3302, 0x3304, 0x4302, 0x4304, 0x6502, 0x6504, 0x6506:
			return 0, fmt.Errorf("%w: multi-port PL256x", ErrUnsupportedChip)
		}
	}
	return 0, fmt.Errorf("%w: PL2303 with bcdUSB %#04x bcdDevice %#04x",
		ErrUnsupportedChip, desc.bcdUSB, desc.bcdDevice)
}

func pl2303SupportsHXStatus(dev usbDevice) bool {
	buf := make([]byte, 1)
	n, err := dev.control(reqTypeVendorIn, pl2303ReqVendorRead, pl2303ReadHXStatus, 0, buf, pl2303ProbeTimeout)
	return err == nil && n == 1
}

func newPL2303(dev usbDevice, desc deviceDescriptor, iface serialInterface) (*pl2303, error) {
	typ, err := detectPL2303Type(dev, desc)
	if err != nil {
		return nil, err
	}
	p := &pl2303{dev: dev, typ: typ, iface: iface}

	// Some HXD clones stall a line settings read and support no break.
	if typ == pl2303TypeHXD {
		buf := make([]byte, pl2303LineLen)
		_, err := dev.control(reqTypeClassIn, pl2303ReqGetLine, 0, 0, buf, pl2303ProbeTimeout)
		p.noBreak = errors.Is(err, errStall)
	}

	if typ != pl2303TypeHXN {
		if err := p.startup(); err != nil {
			return nil, err
		}
	}
	if err := p.resetPipes(); err != nil {
		return nil, err
	}
	return p, nil
}

func (p *pl2303) vendorRead(value uint16) (uint8, error) {
	request := uint8(pl2303ReqVendorRead)
	if p.typ == pl2303TypeHXN {
		request = pl2303ReqVendorReadN
	}
	buf := make([]byte, 1)
	if err := controlIn(p.dev, reqTypeVendorIn, request, value, 0, buf); err != nil {
		return 0, fmt.Errorf("PL2303 read %#04x: %w", value, err)
	}
	return buf[0], nil
}

func (p *pl2303) vendorWrite(value, index uint16) error {
	request := uint8(pl2303ReqVendorWrite)
	if p.typ == pl2303TypeHXN {
		request = pl2303ReqVendorWriteN
	}
	if err := controlOut(p.dev, reqTypeVendorOut, request, value, index, nil); err != nil {
		return fmt.Errorf("PL2303 write %#04x: %w", value, err)
	}
	return nil
}

// startup is the vendor initialisation sequence of the pre-G variants. The
// reads are part of the sequence; their values are not used.
func (p *pl2303) startup() error {
	type step struct {
		value, index uint16
		read         bool
	}
	steps := []step{
		{value: 0x8484, read: true},
		{value: 0x0404, index: 0},
		{value: 0x8484, read: true},
		{value: 0x8383, read: true},
		{value: 0x8484, read: true},
		{value: 0x0404, index: 1},
		{value: 0x8484, read: true},
		{value: 0x8383, read: true},
		{value: 0, index: 1},
		{value: 1, index: 0},
	}
	if p.typ == pl2303TypeH {
		steps = append(steps, step{value: 2, index: 0x24})
	} else {
		steps = append(steps, step{value: 2, index: 0x44})
	}
	for _, s := range steps {
		var err error
		if s.read {
			_, err = p.vendorRead(s.value)
		} else {
			err = p.vendorWrite(s.value, s.index)
		}
		if err != nil {
			return fmt.Errorf("PL2303 startup: %w", err)
		}
	}
	return nil
}

// resetPipes clears the data pipes when the port is opened.
func (p *pl2303) resetPipes() error {
	switch p.typ {
	case pl2303TypeH:
		// Best effort: the H variant has no reset request, and a halt
		// that cannot be cleared shows up on the first transfer anyway.
		_ = p.dev.clearHalt(p.iface.bulkOut)
		_ = p.dev.clearHalt(p.iface.bulkIn)
		return nil
	case pl2303TypeHXN:
		return p.vendorWrite(pl2303HXNResetReg, pl2303HXNResetPipes)
	default:
		if err := p.vendorWrite(8, 0); err != nil {
			return err
		}
		return p.vendorWrite(9, 0)
	}
}

// updateReg changes the masked bits of a vendor register.
func (p *pl2303) updateReg(reg, mask, value uint8) error {
	readAt := uint16(reg)
	if p.typ != pl2303TypeHXN {
		readAt |= 0x80
	}
	current, err := p.vendorRead(readAt)
	if err != nil {
		return err
	}
	current = current&^mask | value&mask
	return p.vendorWrite(uint16(reg), uint16(current))
}

// encodePL2303Baud fills buf[0:4] with the rate setting for a variant.
func encodePL2303Baud(typ pl2303Type, baud int, buf []byte) {
	baud = min(baud, typ.maxBaud())
	if typ == pl2303TypeHXN || nearestPL2303DirectRate(baud) == baud {
		binary.LittleEndian.PutUint32(buf, uint32(baud)) //nolint:gosec // capped at 12M
		return
	}

	// Divisor encoding: rate = 12MHz * 32 / (mantissa * base^exponent).
	const baseline = 12000000 * 32
	mantissa := max(baseline/baud, 1)
	exponent := 0
	buf[3] = 0x80
	if typ == pl2303TypeTA || typ == pl2303TypeTB {
		for mantissa >= 2048 {
			if exponent >= 15 {
				mantissa = 2047
				break
			}
			mantissa >>= 1
			exponent++
		}
		// mantissa < 2048 and exponent <= 15, so each byte is in range.
		buf[2] = byte(exponent & 0x01)
		buf[1] = byte((exponent&^0x01)<<4 | mantissa>>8) //nolint:gosec // see above
		buf[0] = byte(mantissa)                          //nolint:gosec // see above
		return
	}
	for mantissa >= 512 {
		if exponent >= 7 {
			mantissa = 511
			break
		}
		mantissa >>= 2
		exponent++
	}
	buf[2] = 0
	// mantissa < 512 and exponent <= 7, so each byte is in range.
	buf[1] = byte(exponent<<1 | mantissa>>8) //nolint:gosec // see above
	buf[0] = byte(mantissa)                  //nolint:gosec // see above
}

// nearestPL2303DirectRate returns the direct rate closest to baud.
func nearestPL2303DirectRate(baud int) int {
	i := 0
	for i < len(pl2303DirectRates) && pl2303DirectRates[i] <= baud {
		i++
	}
	switch {
	case i == len(pl2303DirectRates):
		return pl2303DirectRates[i-1]
	case i > 0 && pl2303DirectRates[i]-baud > baud-pl2303DirectRates[i-1]:
		return pl2303DirectRates[i-1]
	default:
		return pl2303DirectRates[i]
	}
}

func (p *pl2303) setLine(line lineSettings) error {
	var buf [pl2303LineLen]byte
	encodePL2303Baud(p.typ, line.baud, buf[:4])
	switch line.stopBits {
	case serial.OneStopBit:
		buf[4] = 0
	case serial.OnePointFiveStopBits:
		buf[4] = 1
	case serial.TwoStopBits:
		buf[4] = 2
	}
	switch line.parity {
	case serial.NoParity:
		buf[5] = 0
	case serial.OddParity:
		buf[5] = 1
	case serial.EvenParity:
		buf[5] = 2
	case serial.MarkParity:
		buf[5] = 3
	case serial.SpaceParity:
		buf[5] = 4
	}
	buf[6] = byte(line.dataBits) //nolint:gosec // data bits are validated as 5-8

	// Rewriting unchanged settings can make the chip drop bytes.
	if !p.lineSet || buf != p.line {
		if err := controlOut(p.dev, reqTypeClassOut, pl2303ReqSetLine, 0,
			uint16(p.iface.number), buf[:]); err != nil {
			return fmt.Errorf("PL2303 set line: %w", err)
		}
		p.line = buf
		p.lineSet = true
	}

	var err error
	if p.typ == pl2303TypeHXN {
		err = p.updateReg(pl2303HXNFlowCtlReg, pl2303HXNFlowCtlMask, pl2303HXNFlowCtlNone)
	} else {
		err = p.updateReg(0, pl2303FlowCtlMask, 0)
	}
	if err != nil {
		return fmt.Errorf("PL2303 set flow control: %w", err)
	}
	return nil
}

func (p *pl2303) setControl(dtr, rts bool) error {
	var control uint16
	if dtr {
		control |= pl2303ControlDTR
	}
	if rts {
		control |= pl2303ControlRTS
	}
	if err := controlOut(p.dev, reqTypeClassOut, pl2303ReqSetControl, control,
		uint16(p.iface.number), nil); err != nil {
		return fmt.Errorf("PL2303 set control lines: %w", err)
	}
	return nil
}

// modemStatus reports the latest UART state from the interrupt endpoint. The
// chip only sends a report when a line changes, so the last one is kept.
func (p *pl2303) modemStatus() (*serial.ModemStatusBits, error) {
	if p.iface.intrIn == 0 {
		return nil, fmt.Errorf("%w: PL2303 has no status endpoint", ErrUnsupportedChip)
	}
	buf := make([]byte, max(int(p.iface.intrPacket), pl2303UARTStateIndex+2))
	for range pl2303StatusReads {
		n, err := p.dev.transfer(p.iface.intrIn, buf, pl2303StatusPoll)
		if errors.Is(err, errTimeout) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("PL2303 read status: %w", err)
		}
		if n > pl2303UARTStateIndex {
			p.status = buf[pl2303UARTStateIndex]
		}
	}
	return &serial.ModemStatusBits{
		CTS: p.status&pl2303UARTCTS != 0,
		DSR: p.status&pl2303UARTDSR != 0,
		RI:  p.status&pl2303UARTRing != 0,
		DCD: p.status&pl2303UARTDCD != 0,
	}, nil
}

func (p *pl2303) setBreak(on bool) error {
	state := uint16(pl2303BreakOff)
	if on {
		state = pl2303BreakOn
	}
	if err := controlOut(p.dev, reqTypeClassOut, pl2303ReqBreak, state,
		uint16(p.iface.number), nil); err != nil {
		return fmt.Errorf("PL2303 break: %w", err)
	}
	return nil
}

func (p *pl2303) sendBreak(d time.Duration, _ func([]byte) error, sleep func(time.Duration)) error {
	if p.noBreak {
		return fmt.Errorf("%w: this PL2303 has no break control", ErrUnsupportedChip)
	}
	if err := p.setBreak(true); err != nil {
		return err
	}
	sleep(d)
	return p.setBreak(false)
}
