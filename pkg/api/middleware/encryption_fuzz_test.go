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

package middleware_test

import (
	cryptorand "crypto/rand"
	"encoding/base64"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/crypto"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/middleware"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/mock"
)

// FuzzEstablishSession tests encrypted session establishment with fuzzed
// frame fields to discover edge cases in base64 decoding, salt validation,
// and AEAD key derivation.
func FuzzEstablishSession(f *testing.F) {
	// Correct version with various field values
	f.Add(1, "auth-token-uuid", base64.StdEncoding.EncodeToString(make([]byte, 16)), "Y2lwaGVydGV4dA==", "127.0.0.1")
	f.Add(1, "auth-token-uuid", "not-valid-base64!!!", "Y2lwaGVydGV4dA==", "127.0.0.1")

	// Wrong versions
	f.Add(0, "auth-token-uuid", "c2FsdA==", "Y2lwaGVydGV4dA==", "127.0.0.1")
	f.Add(2, "auth-token-uuid", "c2FsdA==", "Y2lwaGVydGV4dA==", "127.0.0.1")
	f.Add(99, "auth-token-uuid", "c2FsdA==", "Y2lwaGVydGV4dA==", "127.0.0.1")
	f.Add(-1, "auth-token-uuid", "c2FsdA==", "Y2lwaGVydGV4dA==", "127.0.0.1")

	// Empty/missing fields
	f.Add(1, "", "c2FsdA==", "Y2lwaGVydGV4dA==", "127.0.0.1")
	f.Add(1, "auth-token-uuid", "", "Y2lwaGVydGV4dA==", "127.0.0.1")
	f.Add(1, "auth-token-uuid", "c2FsdA==", "", "127.0.0.1")
	f.Add(1, "auth-token-uuid", "c2FsdA==", "Y2lwaGVydGV4dA==", "")

	// Invalid base64
	f.Add(1, "auth-token-uuid", "not-base64", "not-base64", "127.0.0.1")
	f.Add(1, "auth-token-uuid", "====", "====", "127.0.0.1")

	// Wrong salt lengths (base64 of 8 bytes vs required 16)
	f.Add(1, "auth-token-uuid", base64.StdEncoding.EncodeToString(make([]byte, 8)), "Y2lwaGVydGV4dA==", "127.0.0.1")
	f.Add(1, "auth-token-uuid", base64.StdEncoding.EncodeToString(make([]byte, 32)), "Y2lwaGVydGV4dA==", "127.0.0.1")

	// Unknown auth token
	f.Add(1, "unknown-token", "c2FsdA==", "Y2lwaGVydGV4dA==", "127.0.0.1")

	// Build a paired client inline (pairedClient() takes *testing.T, not *testing.F)
	pairingKey := make([]byte, crypto.PairingKeySize)
	if _, err := cryptorand.Read(pairingKey); err != nil {
		f.Fatalf("generate pairing key: %v", err)
	}
	//nolint:gosec // test fixture
	c := &database.Client{
		ClientID:   "client-uuid",
		ClientName: "Test",
		AuthToken:  "auth-token-uuid",
		PairingKey: pairingKey,
		CreatedAt:  1700000000,
		LastSeenAt: 1700000000,
	}

	db := helpers.NewMockUserDBI()
	db.On("GetClientByToken", c.AuthToken).Return(c, nil)
	// Return nil for any other token
	db.On("GetClientByToken", mock.Anything).Return(nil, nil).Maybe()
	mgr := middleware.NewEncryptionGateway(db)

	f.Fuzz(func(t *testing.T, version int, authToken, salt, ciphertext, sourceIP string) {
		frame := middleware.EncryptedFirstFrame{
			Version:     version,
			AuthToken:   authToken,
			SessionSalt: salt,
			Ciphertext:  ciphertext,
		}

		cs, plaintext, err := mgr.EstablishSession(frame, sourceIP)

		// Wrong version must always fail
		if version != middleware.EncryptionProtoVersion && err == nil {
			t.Errorf("expected error for version %d", version)
		}

		// Empty auth token must always fail
		if authToken == "" && err == nil {
			t.Error("expected error for empty auth token")
		}

		// Success requires non-nil session and plaintext
		if err == nil {
			if cs == nil {
				t.Error("nil ClientSession on success")
			}
			if plaintext == nil {
				t.Error("nil plaintext on success")
			}
		}

		// Error must have nil session
		if err != nil && cs != nil {
			t.Error("non-nil ClientSession on error")
		}
	})
}
