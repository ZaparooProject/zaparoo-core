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
)

const (
	descTypeDevice    = 0x01
	descTypeConfig    = 0x02
	descTypeInterface = 0x04
	descTypeEndpoint  = 0x05

	deviceDescLen    = 18
	interfaceDescLen = 9
	endpointDescLen  = 7

	endpointDirIn    = 0x80
	endpointTypeMask = 0x03
	endpointBulk     = 0x02
	endpointIntr     = 0x03
	maxPacketMask    = 0x07FF
)

var errBadDescriptor = errors.New("malformed USB device descriptor")

// deviceDescriptor holds the fields of a USB device descriptor the chip
// drivers need.
type deviceDescriptor struct {
	bcdUSB     uint16
	vendorID   uint16
	productID  uint16
	bcdDevice  uint16
	class      uint8
	maxPacket0 uint8
}

// serialInterface is an interface with the endpoints a USB serial adapter
// uses: a bulk pair for data and, usually, an interrupt endpoint for status.
type serialInterface struct {
	bulkInPacket uint16
	intrPacket   uint16
	number       uint8
	bulkIn       uint8
	bulkOut      uint8
	intrIn       uint8
}

// parseDescriptors reads the raw descriptors the kernel exposes in sysfs: the
// device descriptor followed by the configuration descriptors. It returns the
// device descriptor and the interfaces of the first configuration that have a
// bulk endpoint in each direction, in descriptor order. Alternate settings
// other than the default are ignored.
func parseDescriptors(raw []byte) (deviceDescriptor, []serialInterface, error) {
	if len(raw) < deviceDescLen || raw[0] < deviceDescLen || raw[1] != descTypeDevice {
		return deviceDescriptor{}, nil, errBadDescriptor
	}
	dev := deviceDescriptor{
		bcdUSB:     binary.LittleEndian.Uint16(raw[2:4]),
		class:      raw[4],
		maxPacket0: raw[7],
		vendorID:   binary.LittleEndian.Uint16(raw[8:10]),
		productID:  binary.LittleEndian.Uint16(raw[10:12]),
		bcdDevice:  binary.LittleEndian.Uint16(raw[12:14]),
	}

	var ifaces []serialInterface
	var cur *serialInterface
	flush := func() {
		if cur != nil && cur.bulkIn != 0 && cur.bulkOut != 0 {
			ifaces = append(ifaces, *cur)
		}
		cur = nil
	}

	configs := 0
	for offset := int(raw[0]); offset+2 <= len(raw); {
		length := int(raw[offset])
		if length < 2 || offset+length > len(raw) {
			break
		}
		desc := raw[offset : offset+length]
		offset += length

		switch {
		case desc[1] == descTypeConfig:
			configs++
			if configs > 1 {
				flush()
				return dev, ifaces, nil
			}
		case desc[1] == descTypeInterface && length >= interfaceDescLen:
			flush()
			if desc[3] == 0 {
				cur = &serialInterface{number: desc[2]}
			}
		case cur == nil:
		case desc[1] == descTypeEndpoint && length >= endpointDescLen:
			addEndpoint(cur, desc)
		}
	}
	flush()
	return dev, ifaces, nil
}

func addEndpoint(iface *serialInterface, desc []byte) {
	addr := desc[2]
	kind := desc[3] & endpointTypeMask
	packet := binary.LittleEndian.Uint16(desc[4:6]) & maxPacketMask
	in := addr&endpointDirIn != 0
	switch {
	case kind == endpointBulk && in && iface.bulkIn == 0:
		iface.bulkIn = addr
		iface.bulkInPacket = packet
	case kind == endpointBulk && !in && iface.bulkOut == 0:
		iface.bulkOut = addr
	case kind == endpointIntr && in && iface.intrIn == 0:
		iface.intrIn = addr
		iface.intrPacket = packet
	}
}
