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

package ndef

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// ndefRecord holds the parsed fields of a single NDEF record.
type ndefRecord struct {
	recType string
	payload []byte
	tnf     byte
}

// ParseToText parses raw NDEF data and returns the first text or URI record as a string
func ParseToText(data []byte) (string, error) {
	// Validate NDEF message structure first
	if err := ValidateNDEFMessage(data); err != nil {
		return "", fmt.Errorf("invalid NDEF message: %w", err)
	}

	// Strip TLV wrapper if present
	payload := extractNDEFPayload(data)
	if payload == nil {
		return "", ErrNoNDEF
	}

	// Parse the NDEF record header and extract type + payload directly,
	// without relying on go-ndef (which has algorithmic complexity bugs
	// that can cause hangs with malformed input).
	rec, err := parseNDEFRecord(payload)
	if err != nil {
		return "", fmt.Errorf("invalid NDEF record: %w", err)
	}

	// TNF 0x01 = NFC Forum Well-Known Type
	if rec.tnf != 1 {
		return "", ErrNoNDEF
	}

	switch rec.recType {
	case "T":
		return parseTextPayload(rec.payload)
	case "U":
		return parseURIPayload(rec.payload)
	default:
		return "", ErrNoNDEF
	}
}

// ValidateNDEFMessage validates basic NDEF message structure
func ValidateNDEFMessage(data []byte) error {
	if len(data) < 4 {
		return fmt.Errorf("%w: message too short", ErrInvalidNDEF)
	}

	// Look for NDEF TLV (0x03)
	found := false
	for i := range len(data) - 1 {
		if data[i] == 0x03 {
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("%w: no NDEF TLV found", ErrInvalidNDEF)
	}

	return nil
}

// parseNDEFRecord parses a single NDEF record from the payload, supporting
// both short records (SR=1, 1-byte payload length) and long records (SR=0,
// 4-byte payload length). Only single-record messages (MB+ME set) without
// chunking or ID fields are accepted.
func parseNDEFRecord(payload []byte) (ndefRecord, error) {
	if len(payload) < 3 {
		return ndefRecord{}, fmt.Errorf("%w: record too short", ErrInvalidNDEF)
	}

	// First byte is flags: MB|ME|CF|SR|IL|TNF(3 bits)
	flags := payload[0]
	tnf := flags & 0x07 // Type Name Format (bits 0-2)

	// TNF must be 0-6 (0x00-0x06), value 7 is reserved
	if tnf > 6 {
		return ndefRecord{}, fmt.Errorf("%w: invalid TNF value %d", ErrInvalidNDEF, tnf)
	}

	// MB (Message Begin) must be set for first record
	if (flags & 0x80) == 0 {
		return ndefRecord{}, fmt.Errorf("%w: MB flag not set on first record", ErrInvalidNDEF)
	}

	// ME (Message End) must be set — we only support single-record messages
	if (flags & 0x40) == 0 {
		return ndefRecord{}, fmt.Errorf("%w: ME flag not set (multi-record messages not supported)", ErrInvalidNDEF)
	}

	// CF (Chunk Flag) must NOT be set — chunked records not supported
	if (flags & 0x20) != 0 {
		return ndefRecord{}, fmt.Errorf("%w: chunked records not supported", ErrInvalidNDEF)
	}

	// IL (ID Length) must NOT be set — records with IDs not supported
	if (flags & 0x08) != 0 {
		return ndefRecord{}, fmt.Errorf("%w: records with ID not supported", ErrInvalidNDEF)
	}

	shortRecord := (flags & 0x10) != 0
	typeLen := int(payload[1])

	// Parse payload length depending on SR flag
	var payloadLen int
	var headerLen int

	if shortRecord {
		// Short record: flags(1) + typeLen(1) + payloadLen(1) + type
		headerLen = 3 + typeLen
		payloadLen = int(payload[2])
	} else {
		// Long record: flags(1) + typeLen(1) + payloadLen(4) + type
		headerLen = 6 + typeLen
		if len(payload) < 6 {
			return ndefRecord{}, fmt.Errorf("%w: truncated record header", ErrInvalidNDEF)
		}
		rawLen := binary.BigEndian.Uint32(payload[2:6])
		if rawLen > 0xFFFF {
			return ndefRecord{}, fmt.Errorf(
				"%w: payload length %d exceeds maximum",
				ErrInvalidNDEF, rawLen,
			)
		}
		payloadLen = int(rawLen)
	}

	if len(payload) < headerLen {
		return ndefRecord{}, fmt.Errorf("%w: truncated record header", ErrInvalidNDEF)
	}

	totalLen := headerLen + payloadLen
	if len(payload) < totalLen {
		return ndefRecord{}, fmt.Errorf(
			"%w: truncated payload (need %d, have %d)",
			ErrInvalidNDEF, totalLen, len(payload),
		)
	}

	// For TNF 0x00 (Empty), type and payload length must be 0
	if tnf == 0 && (typeLen != 0 || payloadLen != 0) {
		return ndefRecord{}, fmt.Errorf("%w: empty record must have zero lengths", ErrInvalidNDEF)
	}

	// For TNF 0x01 (Well-Known), type must be present
	if tnf == 1 && typeLen == 0 {
		return ndefRecord{}, fmt.Errorf("%w: well-known record must have type", ErrInvalidNDEF)
	}

	// Extract type and payload
	var recType string
	if shortRecord {
		recType = string(payload[3 : 3+typeLen])
	} else {
		recType = string(payload[6 : 6+typeLen])
	}

	recPayload := payload[headerLen : headerLen+payloadLen]

	return ndefRecord{
		tnf:     tnf,
		recType: recType,
		payload: recPayload,
	}, nil
}

// extractNDEFPayload extracts the NDEF message from TLV format
func extractNDEFPayload(data []byte) []byte {
	// Look for NDEF TLV (0x03)
	for i := range len(data) - 2 {
		if data[i] != 0x03 {
			continue
		}

		payload := extractTLVPayload(data, i)
		if payload != nil {
			return payload
		}
	}
	return nil
}

// extractTLVPayload extracts the payload from a TLV structure at the given offset
func extractTLVPayload(data []byte, offset int) []byte {
	if offset+1 >= len(data) {
		return nil
	}

	// Short format
	if data[offset+1] != 0xFF {
		return extractShortFormatPayload(data, offset)
	}

	// Long format
	return extractLongFormatPayload(data, offset)
}

// extractShortFormatPayload extracts payload from short format TLV
func extractShortFormatPayload(data []byte, offset int) []byte {
	length := int(data[offset+1])
	if offset+2+length <= len(data) {
		return data[offset+2 : offset+2+length]
	}
	return nil
}

// extractLongFormatPayload extracts payload from long format TLV
func extractLongFormatPayload(data []byte, offset int) []byte {
	if offset+4 > len(data) {
		return nil
	}

	length := int(binary.BigEndian.Uint16(data[offset+2 : offset+4]))
	if offset+4+length <= len(data) {
		return data[offset+4 : offset+4+length]
	}
	return nil
}

// parseTextPayload parses a text record payload
func parseTextPayload(payload []byte) (string, error) {
	if len(payload) < 1 {
		return "", errors.New("text payload too short")
	}

	// First byte contains status
	status := payload[0]
	langLen := int(status & 0x3F)

	if len(payload) < 1+langLen {
		return "", errors.New("invalid text payload length")
	}

	// Skip language code and return text
	return string(payload[1+langLen:]), nil
}

// parseURIPayload parses a URI record payload
func parseURIPayload(payload []byte) (string, error) {
	if len(payload) < 1 {
		return "", errors.New("URI payload too short")
	}

	// URI prefixes as defined in NFC Forum URI RTD
	prefixes := []string{
		"",
		"http://www.",
		"https://www.",
		"http://",
		"https://",
		"tel:",
		"mailto:",
		"ftp://anonymous:anonymous@",
		"ftp://ftp.",
		"ftps://",
		"sftp://",
		"smb://",
		"nfs://",
		"ftp://",
		"dav://",
		"news:",
		"telnet://",
		"imap:",
		"rtsp://",
		"urn:",
		"pop:",
		"sip:",
		"sips:",
		"tftp:",
		"btspp://",
		"btl2cap://",
		"btgoep://",
		"tcpobex://",
		"irdaobex://",
		"file://",
		"urn:epc:id:",
		"urn:epc:tag:",
		"urn:epc:pat:",
		"urn:epc:raw:",
		"urn:epc:",
		"urn:nfc:",
	}

	prefixCode := int(payload[0])
	if prefixCode >= len(prefixes) {
		return "", fmt.Errorf("invalid URI prefix code: %d", prefixCode)
	}

	return prefixes[prefixCode] + string(payload[1:]), nil
}
