/*
Zaparoo Core
Copyright (c) 2026 The Zaparoo Project Contributors.
SPDX-License-Identifier: GPL-3.0-or-later

This file is part of Zaparoo Core.

Zaparoo Core is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

Zaparoo Core is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with Zaparoo Core.  If not, see <http://www.gnu.org/licenses/>.
*/

package proto

import "encoding/binary"

const (
	// ClassSmartCard is the USB interface class of a CCID reader.
	ClassSmartCard = 0x0B

	descInterface = 0x04
	descEndpoint  = 0x05
	descCCID      = 0x21

	interfaceDescLen = 9
	endpointDescLen  = 7
	ccidDescLen      = 54

	endpointDirIn    = 0x80
	endpointTypeMask = 0x03
	endpointBulk     = 0x02
	endpointIntr     = 0x03

	featureLevelMask     = 0x00070000
	featureShortAPDU     = 0x00020000
	featureExtendedAPDU  = 0x00040000
	featureLevelTPDU     = 0x00010000
	featureLevelNone     = 0x00000000
	exchangeLevelUnknown = "unknown"
)

// Interface is one smart card interface found in a USB device's descriptors.
type Interface struct {
	// Number is the USB interface number.
	Number uint8
	// BulkIn and BulkOut are the endpoint addresses messages travel over.
	BulkIn  uint8
	BulkOut uint8
	// Interrupt is the slot-change endpoint address, or 0 when there is none.
	Interrupt uint8
	// BulkInPacket is the largest packet the reader sends. A read has to
	// offer at least this much room.
	BulkInPacket uint16
	// MaxSlotIndex is the highest slot number the interface has.
	MaxSlotIndex uint8
	// Protocols and Features are the raw class descriptor fields.
	Protocols uint32
	Features  uint32
	// MaxMessageLength is the largest CCID message the reader accepts.
	MaxMessageLength uint32
}

// APDULevel reports whether the reader exchanges whole APDUs. Readers that
// work at character or TPDU level need the host to run the card protocol,
// which this package does not do.
func (i Interface) APDULevel() bool {
	level := i.Features & featureLevelMask
	return level == featureShortAPDU || level == featureExtendedAPDU
}

// ExchangeLevel names the level for diagnostics.
func (i Interface) ExchangeLevel() string {
	switch i.Features & featureLevelMask {
	case featureShortAPDU:
		return "short APDU"
	case featureExtendedAPDU:
		return "extended APDU"
	case featureLevelTPDU:
		return "TPDU"
	case featureLevelNone:
		return "character"
	default:
		return exchangeLevelUnknown
	}
}

// ParseInterfaces finds the usable smart card interfaces in a device's raw
// descriptors, as the kernel exposes them. Interfaces without both bulk
// endpoints or without a class descriptor are left out.
func ParseInterfaces(raw []byte) []Interface {
	var out []Interface
	var cur *Interface
	hasClassDesc := false

	flush := func() {
		if cur != nil && hasClassDesc && cur.BulkIn != 0 && cur.BulkOut != 0 {
			out = append(out, *cur)
		}
		cur = nil
		hasClassDesc = false
	}

	for offset := 0; offset+2 <= len(raw); {
		length := int(raw[offset])
		if length < 2 || offset+length > len(raw) {
			break
		}
		desc := raw[offset : offset+length]
		offset += length

		switch {
		case desc[1] == descInterface && length >= interfaceDescLen:
			flush()
			if desc[5] == ClassSmartCard {
				cur = &Interface{Number: desc[2]}
			}
		case cur == nil:
		case desc[1] == descCCID && length >= ccidDescLen:
			cur.MaxSlotIndex = desc[4]
			cur.Protocols = binary.LittleEndian.Uint32(desc[6:10])
			cur.Features = binary.LittleEndian.Uint32(desc[40:44])
			cur.MaxMessageLength = binary.LittleEndian.Uint32(desc[44:48])
			hasClassDesc = true
		case desc[1] == descEndpoint && length >= endpointDescLen:
			setEndpoint(cur, desc)
		}
	}
	flush()
	return out
}

func setEndpoint(iface *Interface, desc []byte) {
	addr := desc[2]
	kind := desc[3] & endpointTypeMask
	in := addr&endpointDirIn != 0
	switch {
	case kind == endpointBulk && in:
		iface.BulkIn = addr
		iface.BulkInPacket = binary.LittleEndian.Uint16(desc[4:6]) & 0x07FF
	case kind == endpointBulk:
		iface.BulkOut = addr
	case kind == endpointIntr && in:
		iface.Interrupt = addr
	}
}
