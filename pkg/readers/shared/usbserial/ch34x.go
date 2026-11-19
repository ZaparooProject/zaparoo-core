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

// CH340/CH341 vendor protocol, as documented by the Linux ch341 driver.
const (
	ch34xReqReadVersion = 0x5F
	ch34xReqWriteReg    = 0x9A
	ch34xReqReadReg     = 0x95
	ch34xReqSerialInit  = 0xA1
	ch34xReqModemCtrl   = 0xA4

	ch34xRegBreak     = 0x05
	ch34xRegPrescaler = 0x12
	ch34xRegDivisor   = 0x13
	ch34xRegLCR       = 0x18
	ch34xRegLCR2      = 0x25
	ch34xRegFlowCtl   = 0x27

	ch34xLCREnableRX  = 0x80
	ch34xLCREnableTX  = 0x40
	ch34xLCRMarkSpace = 0x20
	ch34xLCRParEven   = 0x10
	ch34xLCREnablePar = 0x08
	ch34xLCRStop2     = 0x04

	ch34xBitDTR = 0x20
	ch34xBitRTS = 0x40

	ch34xBitCTS = 0x01
	ch34xBitDSR = 0x02
	ch34xBitRI  = 0x04
	ch34xBitDCD = 0x08

	ch34xNoBreakBit0 = 0x01
	ch34xNoBreakBit1 = 0x40

	// ch34xModemStatusReg reads the modem status register pair.
	ch34xModemStatusReg = 0x0706

	// Chip versions from 0x30 configure framing through the LCR register;
	// older ones are fixed at 8N1 here.
	ch34xVersionLCR = 0x30
	// Chips after version 0x27 buffer received data until a full packet
	// arrives unless the divisor's bit 7 is set.
	ch34xVersionNoBuffer = 0x27

	ch34xClockRate = 48000000
	ch34xMinBaud   = (ch34xClockRate + 4096*256 - 1) / (4096 * 256)
	ch34xMaxBaud   = ch34xClockRate / (2 * 8)

	// ch34xSimulatedBreakBaud is the lowest rate, at which one NUL byte
	// holds the line low for about 200ms.
	ch34xSimulatedBreakBaud = ch34xMinBaud
	ch34xSimulatedBreak     = 200 * time.Millisecond
)

type ch34x struct {
	dev     usbDevice
	line    lineSettings
	version uint8
	// limitedPrescaler and simulateBreak are set on chips that stall a read
	// of the break register.
	limitedPrescaler bool
	simulateBreak    bool
}

func newCH34x(dev usbDevice) (*ch34x, error) {
	c := &ch34x{dev: dev}
	version := make([]byte, 2)
	if err := controlIn(dev, reqTypeVendorIn, ch34xReqReadVersion, 0, 0, version); err != nil {
		return nil, fmt.Errorf("CH34x read version: %w", err)
	}
	c.version = version[0]
	if err := controlOut(dev, reqTypeVendorOut, ch34xReqSerialInit, 0, 0, nil); err != nil {
		return nil, fmt.Errorf("CH34x serial init: %w", err)
	}

	breakReg := make([]byte, 2)
	err := controlIn(dev, reqTypeVendorIn, ch34xReqReadReg, ch34xRegBreak, 0, breakReg)
	switch {
	case errors.Is(err, errStall):
		c.limitedPrescaler = true
		c.simulateBreak = true
	case err != nil:
		return nil, fmt.Errorf("CH34x read break register: %w", err)
	}
	return c, nil
}

// ch34xDivisor returns the prescaler and divisor register value for a baud
// rate. The rate is 48MHz / (2^(12 - 3*ps - fact) * div), with ps 0-3, fact
// 0-1 and div 2-256 (9-256 when fact is 1); the closest rate is chosen.
func ch34xDivisor(baud int, limitedPrescaler bool) (uint16, error) {
	speed := min(max(baud, ch34xMinBaud), ch34xMaxBaud)
	clkDiv := func(ps, fact int) int { return 1 << (12 - 3*ps - fact) }

	fact := 1
	ps := 3
	for ; ps >= 0; ps-- {
		if speed > ch34xClockRate/(clkDiv(ps, 1)*512) {
			break
		}
	}
	if ps < 0 {
		return 0, fmt.Errorf("CH34x cannot run at %d baud", baud)
	}

	div := clkDiv(ps, fact)
	divisor := ch34xClockRate / (div * speed)
	if divisor < 9 || divisor > 255 || (ps < 3 && limitedPrescaler) {
		divisor /= 2
		div *= 2
		fact = 0
	}
	if divisor < 2 {
		return 0, fmt.Errorf("CH34x cannot run at %d baud", baud)
	}

	// Take the next divisor when it lands closer to the requested rate,
	// scaled up to keep low rates from rounding away.
	if 16*ch34xClockRate/(div*divisor)-16*speed >= 16*speed-16*ch34xClockRate/(div*(divisor+1)) {
		divisor++
	}
	// Prefer the halved base clock when the divisor allows it.
	if fact == 1 && divisor%2 == 0 {
		divisor /= 2
		fact = 0
	}

	return uint16((0x100-divisor)<<8 | fact<<2 | ps), nil //nolint:gosec // divisor is 2-256
}

func ch34xLCR(line lineSettings) uint8 {
	lcr := uint8(ch34xLCREnableRX | ch34xLCREnableTX)
	lcr |= uint8(line.dataBits - 5) //nolint:gosec // data bits are validated as 5-8
	switch line.parity {
	case serial.OddParity:
		lcr |= ch34xLCREnablePar
	case serial.EvenParity:
		lcr |= ch34xLCREnablePar | ch34xLCRParEven
	case serial.MarkParity:
		lcr |= ch34xLCREnablePar | ch34xLCRMarkSpace
	case serial.SpaceParity:
		lcr |= ch34xLCREnablePar | ch34xLCRParEven | ch34xLCRMarkSpace
	case serial.NoParity:
	}
	if line.stopBits != serial.OneStopBit {
		lcr |= ch34xLCRStop2
	}
	return lcr
}

func (c *ch34x) setLine(line lineSettings) error {
	if c.version < ch34xVersionLCR &&
		(line.dataBits != 8 || line.parity != serial.NoParity || line.stopBits != serial.OneStopBit) {
		return fmt.Errorf("%w: CH34x version %#02x only runs 8N1", ErrUnsupportedChip, c.version)
	}
	value, err := ch34xDivisor(line.baud, c.limitedPrescaler)
	if err != nil {
		return err
	}
	if c.version > ch34xVersionNoBuffer {
		value |= 0x80
	}
	if err := controlOut(c.dev, reqTypeVendorOut, ch34xReqWriteReg,
		ch34xRegDivisor<<8|ch34xRegPrescaler, value, nil); err != nil {
		return fmt.Errorf("CH34x set baud rate: %w", err)
	}
	if c.version >= ch34xVersionLCR {
		if err := controlOut(c.dev, reqTypeVendorOut, ch34xReqWriteReg,
			ch34xRegLCR2<<8|ch34xRegLCR, uint16(ch34xLCR(line)), nil); err != nil {
			return fmt.Errorf("CH34x set line control: %w", err)
		}
	}
	if err := controlOut(c.dev, reqTypeVendorOut, ch34xReqWriteReg,
		ch34xRegFlowCtl<<8|ch34xRegFlowCtl, 0, nil); err != nil {
		return fmt.Errorf("CH34x set flow control: %w", err)
	}
	c.line = line
	return nil
}

func (c *ch34x) setControl(dtr, rts bool) error {
	var mcr uint8
	if dtr {
		mcr |= ch34xBitDTR
	}
	if rts {
		mcr |= ch34xBitRTS
	}
	// The chip takes the lines inverted.
	if err := controlOut(c.dev, reqTypeVendorOut, ch34xReqModemCtrl, ^uint16(mcr), 0, nil); err != nil {
		return fmt.Errorf("CH34x set modem control: %w", err)
	}
	return nil
}

func (c *ch34x) modemStatus() (*serial.ModemStatusBits, error) {
	buf := make([]byte, 2)
	if err := controlIn(c.dev, reqTypeVendorIn, ch34xReqReadReg, ch34xModemStatusReg, 0, buf); err != nil {
		return nil, fmt.Errorf("CH34x read modem status: %w", err)
	}
	// The status bits read inverted.
	msr := ^buf[0]
	return &serial.ModemStatusBits{
		CTS: msr&ch34xBitCTS != 0,
		DSR: msr&ch34xBitDSR != 0,
		RI:  msr&ch34xBitRI != 0,
		DCD: msr&ch34xBitDCD != 0,
	}, nil
}

func (c *ch34x) setBreak(on bool) error {
	reg := make([]byte, 2)
	value := uint16(ch34xRegLCR<<8 | ch34xRegBreak)
	if err := controlIn(c.dev, reqTypeVendorIn, ch34xReqReadReg, value, 0, reg); err != nil {
		return fmt.Errorf("CH34x read break register: %w", err)
	}
	if on {
		reg[0] &^= ch34xNoBreakBit0
		reg[1] &^= ch34xNoBreakBit1
	} else {
		reg[0] |= ch34xNoBreakBit0
		reg[1] |= ch34xNoBreakBit1
	}
	if err := controlOut(c.dev, reqTypeVendorOut, ch34xReqWriteReg, value,
		binary.LittleEndian.Uint16(reg), nil); err != nil {
		return fmt.Errorf("CH34x write break register: %w", err)
	}
	return nil
}

func (c *ch34x) sendBreak(d time.Duration, write func([]byte) error, sleep func(time.Duration)) error {
	if !c.simulateBreak {
		if err := c.setBreak(true); err != nil {
			return err
		}
		sleep(d)
		return c.setBreak(false)
	}

	// No break control: a NUL byte at the lowest rate holds the line low
	// for about 200ms, whatever d asks for.
	restore := c.line
	slow := restore
	slow.baud = ch34xSimulatedBreakBaud
	if err := c.setLine(slow); err != nil {
		return err
	}
	writeErr := write([]byte{0})
	if writeErr == nil {
		sleep(max(d, ch34xSimulatedBreak))
	}
	if err := c.setLine(restore); err != nil {
		return err
	}
	if writeErr != nil {
		return fmt.Errorf("CH34x simulated break: %w", writeErr)
	}
	return nil
}
