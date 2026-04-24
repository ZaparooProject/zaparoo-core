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
	"testing"
)

// FuzzRPCIDUnmarshalJSON tests custom JSON unmarshaling with arbitrary byte inputs
// to verify JSON-RPC 2.0 ID type enforcement (rejects objects and arrays).
func FuzzRPCIDUnmarshalJSON(f *testing.F) {
	// Valid ID types
	f.Add([]byte(`"my-string-id"`))
	f.Add([]byte(`12345`))
	f.Add([]byte(`-42`))
	f.Add([]byte(`3.14`))
	f.Add([]byte(`0`))
	f.Add([]byte(`null`))
	f.Add([]byte(`""`))
	f.Add([]byte(`"12345"`))
	f.Add([]byte(`9223372036854775807`))
	f.Add([]byte(`1.7976931348623157e+308`))

	// Invalid ID types (must be rejected)
	f.Add([]byte(`{"nested":"object"}`))
	f.Add([]byte(`[1, 2, 3]`))
	f.Add([]byte(`{}`))
	f.Add([]byte(`[]`))

	// Edge cases
	f.Add([]byte(`  null  `))
	f.Add([]byte("\t\"id\"\t"))
	f.Add([]byte("\n123\n"))
	f.Add([]byte(``))
	f.Add([]byte(`true`))
	f.Add([]byte(`false`))

	f.Fuzz(func(t *testing.T, data []byte) {
		var id RPCID
		err := id.UnmarshalJSON(data)

		// Objects and arrays must always be rejected
		trimmed := bytes.TrimSpace(data)
		if len(trimmed) > 0 {
			if trimmed[0] == '{' || trimmed[0] == '[' {
				if err == nil {
					t.Errorf("expected ErrInvalidRPCID for input starting with %c: %q", trimmed[0], data)
				}
				return
			}
		}

		// If unmarshal succeeded, verify round-trip stability
		if err == nil {
			marshaled, marshalErr := id.MarshalJSON()
			if marshalErr != nil {
				t.Errorf("MarshalJSON failed after successful unmarshal: %v", marshalErr)
			}

			// Re-unmarshal and verify determinism
			var id2 RPCID
			err2 := id2.UnmarshalJSON(data)
			if err2 != nil {
				t.Errorf("non-deterministic error: first call succeeded, second failed: %v", err2)
			}
			if !bytes.Equal(id.RawMessage, id2.RawMessage) {
				t.Errorf("non-deterministic result: %q vs %q", id.RawMessage, id2.RawMessage)
			}

			// Marshal output must round-trip cleanly
			var id3 RPCID
			if err3 := id3.UnmarshalJSON(marshaled); err3 != nil {
				t.Errorf("round-trip unmarshal failed: %v (marshaled=%q)", err3, marshaled)
			} else if !bytes.Equal(id.RawMessage, id3.RawMessage) {
				t.Errorf("round-trip mismatch: %q -> marshal -> %q -> unmarshal -> %q",
					data, marshaled, id3.RawMessage)
			}
		}
	})
}
