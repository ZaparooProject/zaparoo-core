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

package models

import (
	"bytes"
	"encoding/json"
	"errors"
)

// RPCID represents a JSON-RPC 2.0 request/response ID.
// Per spec, ID can be a String, Number, or Null value.
// We use json.RawMessage to preserve the exact JSON representation,
// ensuring IDs are echoed back exactly as received.
type RPCID struct {
	json.RawMessage
}

// ErrInvalidRPCID is returned when an ID is an object or array.
var ErrInvalidRPCID = errors.New("JSON-RPC ID cannot be an object or array")

// UnmarshalJSON enforces JSON-RPC 2.0 spec compliance by rejecting
// objects and arrays as ID values at parse time.
func (id *RPCID) UnmarshalJSON(data []byte) error {
	// Trim whitespace before checking type to handle edge cases where
	// json.RawMessage may contain leading whitespace
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) > 0 && (trimmed[0] == '{' || trimmed[0] == '[') {
		return ErrInvalidRPCID
	}
	id.RawMessage = make([]byte, len(data))
	copy(id.RawMessage, data)
	return nil
}

// MarshalJSON returns the raw JSON bytes of the ID.
func (id RPCID) MarshalJSON() ([]byte, error) {
	if len(id.RawMessage) == 0 {
		return []byte("null"), nil
	}
	return id.RawMessage, nil
}

// IsAbsent returns true if the ID field was not present in the JSON.
// This indicates a notification in JSON-RPC 2.0.
func (id *RPCID) IsAbsent() bool {
	return id == nil || len(id.RawMessage) == 0
}

// IsNull returns true if the ID is explicitly JSON null.
// Note: This returns false for absent IDs - use IsAbsent() for that.
func (id *RPCID) IsNull() bool {
	return id != nil && bytes.Equal(id.RawMessage, []byte("null"))
}

// IsAbsentOrNull returns true if the ID is either absent or explicitly null.
// For JSON-RPC 2.0, absent means notification (no response), while null means
// request with null ID (must respond). Use IsAbsent() to distinguish.
func (id *RPCID) IsAbsentOrNull() bool {
	return id.IsAbsent() || id.IsNull()
}

// Equal compares two RPCIDs for byte-level equality.
func (id *RPCID) Equal(other RPCID) bool {
	if id == nil {
		return len(other.RawMessage) == 0
	}
	return bytes.Equal(id.RawMessage, other.RawMessage)
}

// String returns the string representation for logging/debugging.
// Note: If the ID is a JSON string "foo", this returns "foo" (with quotes).
func (id *RPCID) String() string {
	if id == nil || len(id.RawMessage) == 0 {
		return "null"
	}
	return string(id.RawMessage)
}

// Key returns a string suitable for use as a map key.
// This is the raw JSON representation of the ID.
func (id *RPCID) Key() string {
	if id == nil {
		return ""
	}
	return string(id.RawMessage)
}

// NullRPCID represents a null JSON-RPC ID.
var NullRPCID = RPCID{RawMessage: []byte("null")}

// NewStringID creates an RPCID from a string value.
func NewStringID(s string) RPCID {
	b, _ := json.Marshal(s)
	return RPCID{RawMessage: b}
}

// NewNumberID creates an RPCID from an integer value.
func NewNumberID(n int64) RPCID {
	b, _ := json.Marshal(n)
	return RPCID{RawMessage: b}
}
