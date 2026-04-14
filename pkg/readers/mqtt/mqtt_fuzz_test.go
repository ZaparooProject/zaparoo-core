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

package mqtt

import (
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
)

// FuzzMQTTMessageHandler tests the MQTT message handler with arbitrary payloads
// to verify it safely converts network messages into tokens.
func FuzzMQTTMessageHandler(f *testing.F) {
	// Valid ZapScript payloads
	f.Add("SNES/Super Metroid.sfc")
	f.Add("**launch.system:nes")
	f.Add("**launch.random:snes")

	// Multi-line ZapScript
	f.Add("**launch.system:nes\n**media.change")

	// Edge cases
	f.Add("")
	f.Add(" ")
	f.Add("\t\n\r")
	f.Add("\x00")
	f.Add("\x00\x01\x02\x03")

	// Long payloads
	f.Add("SNES/Super Metroid.sfc?launcher=retroarch&system=snes&action=run&tags=genre:rpg,region:usa")

	// Unicode
	f.Add("\u65e5\u672c\u8a9e/\u30b2\u30fc\u30e0.rom")
	f.Add("\U0001F3AE")

	// Special characters
	f.Add("path/with spaces/file.rom")
	f.Add("path\\with\\backslashes\\file.rom")
	f.Add("<script>alert('xss')</script>")

	f.Fuzz(func(t *testing.T, payload string) {
		scanCh := make(chan readers.Scan, 1)
		reader := &Reader{
			cfg:    &config.Instance{},
			scanCh: scanCh,
			broker: "test-broker",
			topic:  "test/topic",
		}

		handler := reader.createMessageHandler()
		msg := &mockMessage{payload: []byte(payload)}

		handler(nil, msg)

		if payload == "" {
			// Empty payload should not produce a scan
			select {
			case scan := <-scanCh:
				t.Errorf("empty payload produced scan: %+v", scan)
			default:
				// Expected: no scan
			}
		} else {
			// Non-empty payload should produce exactly one scan.
			// The handler sends synchronously to a buffered channel,
			// so the token is already available.
			select {
			case scan := <-scanCh:
				if scan.Token == nil {
					t.Fatal("scan has nil token")
				}
				if scan.Token.Text != payload {
					t.Errorf("token text mismatch: got %q want %q", scan.Token.Text, payload)
				}
				if scan.Token.Type != TokenType {
					t.Errorf("token type mismatch: got %q want %q", scan.Token.Type, TokenType)
				}
				if scan.Token.UID == "" {
					t.Error("token UID is empty")
				}
				if scan.Token.ReaderID == "" {
					t.Error("token ReaderID is empty")
				}
			default:
				t.Fatal("expected scan from non-empty payload")
			}
		}
	})
}
