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
	"slices"
	"time"

	"go.bug.st/serial"
)

const (
	vendorWCH      = 0x1a86
	vendorProlific = 0x067b

	// Standard USB request type bits.
	reqTypeVendorOut = 0x40
	reqTypeVendorIn  = 0xC0
	reqTypeClassOut  = 0x21
	reqTypeClassIn   = 0xA1

	// controlTimeout bounds a control transfer.
	controlTimeout = time.Second
)

// ErrUnsupportedChip is returned for an adapter variant the driver cannot
// run, such as a multi-port PL256x.
var ErrUnsupportedChip = errors.New("unsupported USB serial chip variant")

type chipKind int

const (
	chipCH34x chipKind = iota + 1
	chipPL2303
)

func (k chipKind) String() string {
	switch k {
	case chipCH34x:
		return "CH34x"
	case chipPL2303:
		return "PL2303"
	default:
		return "unknown"
	}
}

// ch34xProducts are the WCH product IDs of single-port CH340/CH341 chips.
// CH343, CH9102 and other WCH chips speak a different protocol.
var ch34xProducts = []uint16{0x5523, 0x7522, 0x7523}

// pl2303Products are Prolific's own PL2303 product IDs. Prolific makes other
// USB devices too, so the vendor ID alone is not enough.
var pl2303Products = []uint16{0x2303, 0x2304, 0x23a3, 0x23b3, 0x23c3, 0x23d3, 0x23e3, 0x23f3}

// supportedChip returns the chip family of a supported adapter.
func supportedChip(vendor, product uint16) (chipKind, bool) {
	switch {
	case vendor == vendorWCH && slices.Contains(ch34xProducts, product):
		return chipCH34x, true
	case vendor == vendorProlific && slices.Contains(pl2303Products, product):
		return chipPL2303, true
	default:
		return 0, false
	}
}

// lineSettings is a validated serial.Mode.
type lineSettings struct {
	baud     int
	dataBits int
	parity   serial.Parity
	stopBits serial.StopBits
}

// lineFromMode validates a mode, applying go.bug.st/serial's defaults of 9600
// baud and 8 data bits for zero values.
func lineFromMode(mode *serial.Mode) (lineSettings, error) {
	line := lineSettings{baud: 9600, dataBits: 8}
	if mode == nil {
		return line, nil
	}
	if mode.BaudRate < 0 {
		return line, fmt.Errorf("invalid baud rate %d", mode.BaudRate)
	}
	if mode.BaudRate > 0 {
		line.baud = mode.BaudRate
	}
	if mode.DataBits != 0 {
		if mode.DataBits < 5 || mode.DataBits > 8 {
			return line, fmt.Errorf("invalid data bits %d", mode.DataBits)
		}
		line.dataBits = mode.DataBits
	}
	switch mode.Parity {
	case serial.NoParity, serial.OddParity, serial.EvenParity, serial.MarkParity, serial.SpaceParity:
		line.parity = mode.Parity
	default:
		return line, fmt.Errorf("invalid parity %d", mode.Parity)
	}
	switch mode.StopBits {
	case serial.OneStopBit, serial.OnePointFiveStopBits, serial.TwoStopBits:
		line.stopBits = mode.StopBits
	default:
		return line, fmt.Errorf("invalid stop bits %d", mode.StopBits)
	}
	return line, nil
}

// chipDriver is the vendor protocol of one adapter family.
type chipDriver interface {
	// setLine applies the baud rate and framing.
	setLine(line lineSettings) error
	// setControl sets the DTR and RTS outputs.
	setControl(dtr, rts bool) error
	// modemStatus reads the modem input lines.
	modemStatus() (*serial.ModemStatusBits, error)
	// sendBreak holds a break condition for at least d, waiting with sleep.
	// write sends bytes on the data endpoint, for chips that can only
	// simulate a break.
	sendBreak(d time.Duration, write func([]byte) error, sleep func(time.Duration)) error
}

// newChipDriver initialises the adapter behind dev.
func newChipDriver(dev usbDevice, t *target) (chipDriver, error) {
	switch t.chip {
	case chipCH34x:
		return newCH34x(dev)
	case chipPL2303:
		return newPL2303(dev, t.desc, t.iface)
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedChip, t.chip)
	}
}

// controlIn runs an IN control transfer that must fill buf.
func controlIn(dev usbDevice, reqType, request uint8, value, index uint16, buf []byte) error {
	n, err := dev.control(reqType, request, value, index, buf, controlTimeout)
	if err != nil {
		return fmt.Errorf("control request %#02x: %w", request, err)
	}
	if n != len(buf) {
		return fmt.Errorf("control request %#02x: short reply of %d bytes", request, n)
	}
	return nil
}

// controlOut runs an OUT control transfer.
func controlOut(dev usbDevice, reqType, request uint8, value, index uint16, data []byte) error {
	if _, err := dev.control(reqType, request, value, index, data, controlTimeout); err != nil {
		return fmt.Errorf("control request %#02x: %w", request, err)
	}
	return nil
}
