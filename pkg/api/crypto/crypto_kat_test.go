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

package crypto_test

import (
	"bytes"
	"crypto/hkdf"
	"crypto/sha256"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/crypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestKAT_DeriveSessionKeysMatchesSpecLabels independently re-derives the
// four session values using the literal info strings from
// docs/api/encryption.md. If the implementation's info-string constants
// drift from the spec, this test fails — and any JS/Swift/Kotlin client
// compatibility breaks at the same moment.
func TestKAT_DeriveSessionKeysMatchesSpecLabels(t *testing.T) {
	t.Parallel()

	pairingKey := bytes.Repeat([]byte{0x42}, crypto.PairingKeySize)
	salt := bytes.Repeat([]byte{0xAA}, crypto.SessionSaltSize)

	keys, err := crypto.DeriveSessionKeys(pairingKey, salt)
	require.NoError(t, err)

	prk, err := hkdf.Extract(sha256.New, pairingKey, salt)
	require.NoError(t, err)

	expC2SKey, err := hkdf.Expand(sha256.New, prk, "zaparoo-c2s-v1", 32)
	require.NoError(t, err)
	expS2CKey, err := hkdf.Expand(sha256.New, prk, "zaparoo-s2c-v1", 32)
	require.NoError(t, err)
	expC2SNonce, err := hkdf.Expand(sha256.New, prk, "zaparoo-c2s-nonce-v1", 12)
	require.NoError(t, err)
	expS2CNonce, err := hkdf.Expand(sha256.New, prk, "zaparoo-s2c-nonce-v1", 12)
	require.NoError(t, err)

	assert.Equal(t, expC2SKey, keys.C2SKey, "c2s key info string must be 'zaparoo-c2s-v1'")
	assert.Equal(t, expS2CKey, keys.S2CKey, "s2c key info string must be 'zaparoo-s2c-v1'")
	assert.Equal(t, expC2SNonce, keys.C2SNonce, "c2s nonce info string must be 'zaparoo-c2s-nonce-v1'")
	assert.Equal(t, expS2CNonce, keys.S2CNonce, "s2c nonce info string must be 'zaparoo-s2c-nonce-v1'")
}

// TestKAT_NonceXORConstruction pins the nonce derivation: the spec says
// nonce[0:4] = base[0:4] and nonce[4:12] = base[4:12] XOR (counter as
// 8 bytes big-endian). buildNonce is unexported, so we exercise it via
// Encrypt and compare against gcm.Seal called with a manually constructed
// nonce.
func TestKAT_NonceXORConstruction(t *testing.T) {
	t.Parallel()

	base := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c}
	plaintext := []byte("zaparoo")
	aad := []byte("auth-token:ws")
	key := bytes.Repeat([]byte{0x77}, crypto.AESKeySize)

	gcm, err := crypto.NewAEAD(key)
	require.NoError(t, err)

	cases := []struct {
		name        string
		expectNonce []byte
		counter     uint64
	}{
		{
			name:        "counter_0_leaves_base_unchanged",
			counter:     0,
			expectNonce: base,
		},
		{
			// 1 → BE = 00 00 00 00 00 00 00 01; XOR'd into base[4:12]:
			//   05^00 06^00 07^00 08^00 09^00 0a^00 0b^00 0c^01
			//   = 05 06 07 08 09 0a 0b 0d
			name:        "counter_1",
			counter:     1,
			expectNonce: []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0d},
		},
		{
			// MaxUint64-1 = 0xFF FF FF FF FF FF FF FE — XORs every byte.
			// MaxUint64 itself is rejected with ErrCounterExhausted.
			name:    "counter_near_max_xors_every_byte",
			counter: 0xFFFFFFFFFFFFFFFE,
			expectNonce: []byte{
				0x01, 0x02, 0x03, 0x04,
				0x05 ^ 0xFF, 0x06 ^ 0xFF, 0x07 ^ 0xFF, 0x08 ^ 0xFF,
				0x09 ^ 0xFF, 0x0a ^ 0xFF, 0x0b ^ 0xFF, 0x0c ^ 0xFE,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			gotCT, err := crypto.Encrypt(gcm, base, tc.counter, plaintext, aad)
			require.NoError(t, err)

			refCT := gcm.Seal(nil, tc.expectNonce, plaintext, aad)
			assert.Equal(t, refCT, gotCT,
				"Encrypt with counter=%d must use nonce = base XOR BE64(counter) at bytes [4:12]",
				tc.counter)
		})
	}
}
