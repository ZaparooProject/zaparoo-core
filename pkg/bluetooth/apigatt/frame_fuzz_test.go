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
)

// splitAll collects every chunk Split emits.
func splitAll(tb testing.TB, c *Chunker, msg []byte, mtu int) [][]byte {
	tb.Helper()
	var chunks [][]byte
	if err := c.Split(msg, mtu, func(chunk []byte) error {
		chunks = append(chunks, chunk)
		return nil
	}); err != nil {
		tb.Fatalf("split: %v", err)
	}
	return chunks
}

// FuzzParseChunk checks that no input makes the header parser panic and that
// every accepted chunk survives an encode round trip.
func FuzzParseChunk(f *testing.F) {
	for _, c := range splitAll(f, &Chunker{Tag: 7}, []byte("seed message"), 100) {
		f.Add(c)
	}
	f.Add([]byte{})
	f.Add([]byte{0x10, 0, 0, 0, 0})
	f.Add([]byte{0x11, 0, 0, 0, 0, 0, 0, 0, 1, 'x'})
	f.Add(EncodeAck(7, 300))

	f.Fuzz(func(t *testing.T, chunk []byte) {
		h, payload, err := ParseChunk(chunk)
		if err != nil {
			return
		}
		encoded := EncodeChunk(h, payload)
		if h.Ack {
			encoded = EncodeAck(h.Tag, h.Seq)
		}
		again, payload2, err := ParseChunk(encoded)
		if err != nil {
			t.Fatalf("re-encoded chunk failed to parse: %v", err)
		}
		if again != h || !bytes.Equal(payload2, payload) {
			t.Fatalf("round trip changed the chunk: %+v vs %+v", again, h)
		}
	})
}

// FuzzReassembler feeds arbitrary chunk streams and checks the reassembler
// never panics and never returns or holds more than its caps.
func FuzzReassembler(f *testing.F) {
	chunks := splitAll(f, &Chunker{}, []byte("a longer seed message that needs several chunks to carry"), 23)
	var stream []byte
	for _, c := range chunks {
		stream = append(stream, byte(len(c))) //nolint:gosec // an MTU-23 chunk is at most 20 bytes
		stream = append(stream, c...)
	}
	f.Add(stream)
	f.Add([]byte{10, 0x13, 0, 0, 0, 0, 0, 0, 0, 1, 'x'})

	const limit = 4096
	f.Fuzz(func(t *testing.T, data []byte) {
		r := NewReassembler(limit)
		for len(data) > 0 {
			n := min(int(data[0]), len(data)-1)
			chunk := data[1 : 1+n]
			data = data[1+n:]
			h, payload, err := ParseChunk(chunk)
			if err != nil || h.Ack {
				continue
			}
			msgs, err := r.Push(h, payload)
			if err != nil {
				return
			}
			for _, msg := range msgs {
				if len(msg) > limit {
					t.Fatalf("reassembled %d bytes over the cap", len(msg))
				}
			}
			if len(r.buf) > limit || r.heldBytes > maxHeldBytes || len(r.held) >= ReorderWindow {
				t.Fatalf("holding %d message bytes, %d early bytes, %d early chunks",
					len(r.buf), r.heldBytes, len(r.held))
			}
		}
	})
}
