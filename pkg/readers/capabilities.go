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

package readers

import "errors"

// CapabilityProvider is an interface for types that can report their
// capabilities. This allows capability checking without requiring the
// full Reader interface.
type CapabilityProvider interface {
	Capabilities() []Capability
}

// HasCapability checks if a reader has a specific capability.
func HasCapability(r CapabilityProvider, capability Capability) bool {
	for _, c := range r.Capabilities() {
		if c == capability {
			return true
		}
	}
	return false
}

// FilterByCapability returns only the Readers that have the specified capability.
func FilterByCapability(rs []Reader, capability Capability) []Reader {
	result := make([]Reader, 0, len(rs))
	for _, r := range rs {
		if r == nil {
			continue
		}
		if HasCapability(r, capability) {
			result = append(result, r)
		}
	}
	return result
}

// SelectWriterStrict finds a specific reader by ID and validates it is
// connected and write-capable. Returns an error if not found, disconnected,
// or lacking write capability.
func SelectWriterStrict(rs []Reader, readerID string) (Reader, error) {
	for _, r := range rs {
		if r == nil {
			continue
		}
		if r.ReaderID() == readerID {
			if !r.Connected() {
				return nil, errors.New("reader not connected: " + readerID)
			}
			if !HasCapability(r, CapabilityWrite) {
				return nil, errors.New("reader does not have write capability: " + readerID)
			}
			return r, nil
		}
	}
	return nil, errors.New("reader not found: " + readerID)
}

// SelectWriterPreferred finds a write-capable reader, trying each preferred
// ID in order. Falls back to the first available write-capable reader if no
// preferences match.
func SelectWriterPreferred(rs []Reader, preferredIDs []string) (Reader, error) {
	writeCapable := FilterByCapability(rs, CapabilityWrite)
	if len(writeCapable) == 0 {
		return nil, errors.New("no readers with write capability connected")
	}

	for _, id := range preferredIDs {
		if id == "" {
			continue
		}
		for _, r := range writeCapable {
			if r.ReaderID() == id {
				return r, nil
			}
		}
	}

	return writeCapable[0], nil
}
