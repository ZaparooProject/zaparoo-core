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

package api

import (
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/mock"
)

// FuzzStartSession tests the PAKE session start with arbitrary name and
// message bytes to verify the handshake rejects malformed inputs safely.
func FuzzStartSession(f *testing.F) {
	// Valid-looking inputs
	f.Add("MyDevice", []byte(`{"role":0,"x":"AAAA","y":"BBBB"}`))
	f.Add("Test Client", []byte(`{"role":0,"x":"dGVzdA==","y":"dGVzdA=="}`))

	// Edge case names
	f.Add("", []byte(`{"role":0,"x":"dGVzdA==","y":"dGVzdA=="}`))
	f.Add("a", []byte(`data`))
	longName := strings.Repeat("a", 160)
	f.Add(longName, []byte(`data`))

	// Malformed PAKE messages
	f.Add("Test", []byte(``))
	f.Add("Test", []byte(`{}`))
	f.Add("Test", []byte(`not json`))
	f.Add("Test", []byte(`{"role":99}`))
	f.Add("Test", []byte(`null`))

	// Binary/random data as PAKE message
	f.Add("Test", []byte{0x00, 0x01, 0x02, 0x03})
	f.Add("Test", []byte{0xff, 0xfe, 0xfd})

	// Oversized message (>2048 bytes)
	oversized := make([]byte, 3000)
	for i := range oversized {
		oversized[i] = 'A'
	}
	f.Add("Test", oversized)

	// Unicode names
	f.Add("\u65e5\u672c\u8a9e\u30c7\u30d0\u30a4\u30b9", []byte(`data`))
	f.Add("\U0001F3AE Gaming", []byte(`data`))

	f.Fuzz(func(t *testing.T, name string, msgA []byte) {
		db := helpers.NewMockUserDBI()
		db.On("CountClients").Return(0, nil).Maybe()
		db.On("CreateClient", mock.AnythingOfType("*database.Client")).Return(nil).Maybe()
		notifChan := make(chan models.Notification, 16)

		mgr := NewPairingManager(db, notifChan)
		_, _, err := mgr.StartPairing()
		if err != nil {
			t.Fatalf("StartPairing failed: %v", err)
		}

		sessionID, msgB, err := mgr.startSession(name, msgA)

		// Empty name must always fail
		if name == "" && err == nil {
			t.Error("expected error for empty name")
		}

		// Oversized message must always fail
		if len(msgA) > 2048 && err == nil {
			t.Error("expected error for oversized PAKE message")
		}

		// Success requires non-empty session ID and message B
		if err == nil {
			if sessionID == "" {
				t.Error("empty session ID on success")
			}
			if len(msgB) == 0 {
				t.Error("empty message B on success")
			}
		}

		// Error requires empty session ID
		if err != nil && sessionID != "" {
			t.Error("non-empty session ID on error")
		}
	})
}
