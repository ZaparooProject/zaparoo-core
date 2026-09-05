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

package apigatt

import (
	"bytes"
	"testing"

	"github.com/jonboulle/clockwork"
)

// FuzzParseChunk checks that no input makes the header parser panic and that
// every accepted chunk survives an encode round trip.
func FuzzParseChunk(f *testing.F) {
	good, _ := Chunker{MTU: 100, Tag: 7}.Split([]byte("seed message"))
	for _, c := range good {
		f.Add(c)
	}
	f.Add([]byte{})
	f.Add([]byte{0x10, 0, 0, 0})
	f.Add([]byte{0x11, 0, 0, 0, 0, 0, 0, 1, 'x'})

	f.Fuzz(func(t *testing.T, chunk []byte) {
		h, payload, err := ParseChunk(chunk)
		if err != nil {
			return
		}
		again, payload2, err := ParseChunk(EncodeChunk(h, payload))
		if err != nil {
			t.Fatalf("re-encoded chunk failed to parse: %v", err)
		}
		if again != h || !bytes.Equal(payload2, payload) {
			t.Fatalf("round trip changed the chunk: %+v vs %+v", again, h)
		}
	})
}

// FuzzReassembler feeds arbitrary chunk streams and checks the reassembler
// never panics, never holds more than its cap, and either fails or keeps
// going without contradiction.
func FuzzReassembler(f *testing.F) {
	chunks, _ := Chunker{MTU: 23}.Split([]byte("a longer seed message that needs several chunks to carry"))
	var stream []byte
	for _, c := range chunks {
		stream = append(stream, byte(len(c))) //nolint:gosec // an MTU-23 chunk is at most 20 bytes
		stream = append(stream, c...)
	}
	f.Add(stream)
	f.Add([]byte{9, 0x11, 0, 0, 0, 0, 0, 0, 1, 'x'})

	f.Fuzz(func(t *testing.T, data []byte) {
		r := NewReassembler(clockwork.NewFakeClock(), 4096)
		for len(data) > 0 {
			n := int(data[0])
			data = data[1:]
			if n > len(data) {
				n = len(data)
			}
			chunk := data[:n]
			data = data[n:]
			msg, err := r.Push(chunk)
			if err != nil {
				return
			}
			if len(msg) > 4096 {
				t.Fatalf("reassembled %d bytes over the cap", len(msg))
			}
			if len(r.buf)+r.heldBytes > 4096 {
				t.Fatalf("holding %d bytes over the cap", len(r.buf)+r.heldBytes)
			}
		}
	})
}
