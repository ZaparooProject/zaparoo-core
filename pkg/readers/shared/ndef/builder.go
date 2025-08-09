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
	"fmt"

	"github.com/hsanjuan/go-ndef"
)

// BuildTextMessage creates an NDEF message with a single text record
func BuildTextMessage(text string) ([]byte, error) {
	// Create text message using go-ndef
	msg := ndef.NewTextMessage(text, "en")
	payload, err := msg.Marshal()
	if err != nil {
		return nil, fmt.Errorf("failed to marshal NDEF message: %w", err)
	}

	// Add TLV wrapper
	header, err := calculateNDEFHeader(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate NDEF header: %w", err)
	}

	// Build complete message
	result := make([]byte, 0, len(header)+len(payload)+1)
	result = append(result, header...)
	result = append(result, payload...)
	result = append(result, NdefEnd...)

	return result, nil
}

// BuildMessage is an alias for BuildTextMessage for backward compatibility
func BuildMessage(text string) ([]byte, error) {
	return BuildTextMessage(text)
}
