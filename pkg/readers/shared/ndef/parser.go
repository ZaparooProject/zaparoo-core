/*
Zaparoo Core
Copyright (c) 2025 The Zaparoo Project Contributors.
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

	"github.com/hsanjuan/go-ndef"
)

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

	// Parse using go-ndef
	msg := &ndef.Message{}
	_, err := msg.Unmarshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to parse NDEF message: %w", err)
	}

	// Find first text or URI record
	for _, rec := range msg.Records {
		if rec.TNF() == ndef.NFCForumWellKnownType {
			if result, err := handleWellKnownRecord(rec); err == nil {
				return result, nil
			}
		}
	}

	return "", ErrNoNDEF
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

// handleWellKnownRecord processes NFC Forum well-known types
func handleWellKnownRecord(rec *ndef.Record) (string, error) {
	typeStr := rec.Type()
	payloadBytes, err := extractPayloadBytes(rec)
	if err != nil {
		return "", err
	}

	switch typeStr {
	case "T": // Text
		return parseTextPayload(payloadBytes)
	case "U": // URI
		return parseURIPayload(payloadBytes)
	default:
		return "", fmt.Errorf("unsupported well-known type: %s", typeStr)
	}
}

// extractPayloadBytes extracts the payload bytes from an NDEF record
func extractPayloadBytes(rec *ndef.Record) ([]byte, error) {
	payload, err := rec.Payload()
	if err != nil {
		return nil, fmt.Errorf("failed to get NDEF record payload: %w", err)
	}
	return payload.Marshal(), nil
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
