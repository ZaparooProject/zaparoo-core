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
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
)

// NDEFRecordType represents the type of an NDEF record
type NDEFRecordType string

const (
	// NDEFTypeText represents a text record type
	NDEFTypeText NDEFRecordType = "text"
	// NDEFTypeURI represents a URI record type
	NDEFTypeURI NDEFRecordType = "uri"
)

var (
	// NDEF markers
	NdefEnd = []byte{0xFE}

	// ErrNoNDEF is returned when no NDEF record is found.
	ErrNoNDEF = errors.New("no NDEF record found")
	// ErrInvalidNDEF is returned when the NDEF format is invalid.
	ErrInvalidNDEF = errors.New("invalid NDEF format")
)

// NDEFMessage represents an NDEF message
type NDEFMessage struct {
	Records []NDEFRecord
}

// NDEFRecord represents a single NDEF record
type NDEFRecord struct {
	Text    string
	URI     string
	Type    NDEFRecordType
	Payload []byte
}

// calculateNDEFHeader calculates the NDEF TLV header
func calculateNDEFHeader(payload []byte) ([]byte, error) {
	length := len(payload)

	// Short format (length < 255)
	if length < 255 {
		return []byte{0x03, byte(length)}, nil
	}

	// Long format (length >= 255)
	// NFCForum-TS-Type-2-Tag_1.1.pdf Page 9
	if length > 0xFFFF {
		return nil, errors.New("NDEF payload too large")
	}

	header := []byte{0x03, 0xFF}
	buf := new(bytes.Buffer)
	if err := binary.Write(buf, binary.BigEndian, uint16(length)); err != nil {
		return nil, fmt.Errorf("failed to write NDEF length header: %w", err)
	}

	return append(header, buf.Bytes()...), nil
}
