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
	cryptorand "crypto/rand"
	"errors"
	"math"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/crypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// randomKey returns a random byte slice of n bytes for tests.
func randomKey(t *testing.T, n int) []byte {
	t.Helper()
	b := make([]byte, n)
	_, err := cryptorand.Read(b)
	require.NoError(t, err)
	return b
}

func TestDeriveSessionKeys_Deterministic(t *testing.T) {
	t.Parallel()

	pairingKey := randomKey(t, crypto.PairingKeySize)
	salt := randomKey(t, crypto.SessionSaltSize)

	a, err := crypto.DeriveSessionKeys(pairingKey, salt)
	require.NoError(t, err)
	b, err := crypto.DeriveSessionKeys(pairingKey, salt)
	require.NoError(t, err)

	assert.Equal(t, a.C2SKey, b.C2SKey)
	assert.Equal(t, a.S2CKey, b.S2CKey)
	assert.Equal(t, a.C2SNonce, b.C2SNonce)
	assert.Equal(t, a.S2CNonce, b.S2CNonce)
}

func TestDeriveSessionKeys_DifferentSaltsDifferentKeys(t *testing.T) {
	t.Parallel()

	pairingKey := randomKey(t, crypto.PairingKeySize)
	salt1 := randomKey(t, crypto.SessionSaltSize)
	salt2 := randomKey(t, crypto.SessionSaltSize)

	a, err := crypto.DeriveSessionKeys(pairingKey, salt1)
	require.NoError(t, err)
	b, err := crypto.DeriveSessionKeys(pairingKey, salt2)
	require.NoError(t, err)

	assert.NotEqual(t, a.C2SKey, b.C2SKey)
	assert.NotEqual(t, a.S2CKey, b.S2CKey)
	assert.NotEqual(t, a.C2SNonce, b.C2SNonce)
	assert.NotEqual(t, a.S2CNonce, b.S2CNonce)
}

func TestDeriveSessionKeys_DirectionalKeysDifferent(t *testing.T) {
	t.Parallel()

	pairingKey := randomKey(t, crypto.PairingKeySize)
	salt := randomKey(t, crypto.SessionSaltSize)

	keys, err := crypto.DeriveSessionKeys(pairingKey, salt)
	require.NoError(t, err)

	// c2s and s2c must never collide — directional separation prevents
	// reflection attacks and cross-direction nonce collisions.
	assert.NotEqual(t, keys.C2SKey, keys.S2CKey)
	assert.NotEqual(t, keys.C2SNonce, keys.S2CNonce)
}

func TestDeriveSessionKeys_KeySizes(t *testing.T) {
	t.Parallel()

	keys, err := crypto.DeriveSessionKeys(
		randomKey(t, crypto.PairingKeySize),
		randomKey(t, crypto.SessionSaltSize),
	)
	require.NoError(t, err)

	assert.Len(t, keys.C2SKey, crypto.AESKeySize)
	assert.Len(t, keys.S2CKey, crypto.AESKeySize)
	assert.Len(t, keys.C2SNonce, crypto.NonceSize)
	assert.Len(t, keys.S2CNonce, crypto.NonceSize)
}

func TestDeriveSessionKeys_InvalidPairingKey(t *testing.T) {
	t.Parallel()

	salt := randomKey(t, crypto.SessionSaltSize)

	tests := []struct {
		name string
		key  []byte
	}{
		{name: "empty", key: []byte{}},
		{name: "too short", key: make([]byte, 16)},
		{name: "too long", key: make([]byte, 64)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := crypto.DeriveSessionKeys(tt.key, salt)
			assert.ErrorIs(t, err, crypto.ErrInvalidPairingKey)
		})
	}
}

func TestDeriveSessionKeys_InvalidSalt(t *testing.T) {
	t.Parallel()

	pairingKey := randomKey(t, crypto.PairingKeySize)

	tests := []struct {
		name string
		salt []byte
	}{
		{name: "empty", salt: []byte{}},
		{name: "too short", salt: make([]byte, 8)},
		{name: "too long", salt: make([]byte, 32)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := crypto.DeriveSessionKeys(pairingKey, tt.salt)
			assert.ErrorIs(t, err, crypto.ErrInvalidSessionSalt)
		})
	}
}

func TestEncryptDecrypt_RoundTrip(t *testing.T) {
	t.Parallel()

	keys, err := crypto.DeriveSessionKeys(
		randomKey(t, crypto.PairingKeySize),
		randomKey(t, crypto.SessionSaltSize),
	)
	require.NoError(t, err)

	gcm, err := crypto.NewAEAD(keys.C2SKey)
	require.NoError(t, err)

	plaintext := []byte(`{"jsonrpc":"2.0","method":"launch","params":{"uid":"abc"},"id":1}`)
	aad := []byte("token-1234:ws")

	ciphertext, err := crypto.Encrypt(gcm, keys.C2SNonce, 0, plaintext, aad)
	require.NoError(t, err)
	assert.NotEqual(t, plaintext, ciphertext, "ciphertext must differ from plaintext")
	assert.Greater(t, len(ciphertext), len(plaintext), "ciphertext must include GCM tag")

	decrypted, err := crypto.Decrypt(gcm, keys.C2SNonce, 0, ciphertext, aad)
	require.NoError(t, err)
	assert.Equal(t, plaintext, decrypted)
}

func TestEncryptDecrypt_DifferentCountersDifferentCiphertexts(t *testing.T) {
	t.Parallel()

	keys, err := crypto.DeriveSessionKeys(
		randomKey(t, crypto.PairingKeySize),
		randomKey(t, crypto.SessionSaltSize),
	)
	require.NoError(t, err)

	gcm, err := crypto.NewAEAD(keys.C2SKey)
	require.NoError(t, err)

	plaintext := []byte("identical plaintext")
	aad := []byte("token:ws")

	ct0, err := crypto.Encrypt(gcm, keys.C2SNonce, 0, plaintext, aad)
	require.NoError(t, err)
	ct1, err := crypto.Encrypt(gcm, keys.C2SNonce, 1, plaintext, aad)
	require.NoError(t, err)

	assert.NotEqual(t, ct0, ct1, "different counters must produce different ciphertexts")
}

func TestEncryptDecrypt_WrongCounterFails(t *testing.T) {
	t.Parallel()

	keys, err := crypto.DeriveSessionKeys(
		randomKey(t, crypto.PairingKeySize),
		randomKey(t, crypto.SessionSaltSize),
	)
	require.NoError(t, err)

	gcm, err := crypto.NewAEAD(keys.C2SKey)
	require.NoError(t, err)

	plaintext := []byte("hello")
	aad := []byte("token:ws")

	ciphertext, err := crypto.Encrypt(gcm, keys.C2SNonce, 5, plaintext, aad)
	require.NoError(t, err)

	_, err = crypto.Decrypt(gcm, keys.C2SNonce, 6, ciphertext, aad)
	assert.Error(t, err, "decrypting with wrong counter must fail")
}

func TestEncryptDecrypt_WrongKeyFails(t *testing.T) {
	t.Parallel()

	pairingKey := randomKey(t, crypto.PairingKeySize)
	salt := randomKey(t, crypto.SessionSaltSize)
	keys, err := crypto.DeriveSessionKeys(pairingKey, salt)
	require.NoError(t, err)

	// Use the s2c key to "decrypt" something encrypted with c2s — must fail.
	c2sGCM, err := crypto.NewAEAD(keys.C2SKey)
	require.NoError(t, err)
	s2cGCM, err := crypto.NewAEAD(keys.S2CKey)
	require.NoError(t, err)

	plaintext := []byte("hello")
	aad := []byte("token:ws")
	ciphertext, err := crypto.Encrypt(c2sGCM, keys.C2SNonce, 0, plaintext, aad)
	require.NoError(t, err)

	_, err = crypto.Decrypt(s2cGCM, keys.C2SNonce, 0, ciphertext, aad)
	assert.Error(t, err, "decrypting with wrong key must fail")
}

func TestEncryptDecrypt_WrongAADFails(t *testing.T) {
	t.Parallel()

	keys, err := crypto.DeriveSessionKeys(
		randomKey(t, crypto.PairingKeySize),
		randomKey(t, crypto.SessionSaltSize),
	)
	require.NoError(t, err)

	gcm, err := crypto.NewAEAD(keys.C2SKey)
	require.NoError(t, err)

	plaintext := []byte("hello")
	ciphertext, err := crypto.Encrypt(gcm, keys.C2SNonce, 0, plaintext, []byte("token-a:ws"))
	require.NoError(t, err)

	_, err = crypto.Decrypt(gcm, keys.C2SNonce, 0, ciphertext, []byte("token-b:ws"))
	assert.Error(t, err, "decrypting with wrong AAD must fail")
}

func TestEncryptDecrypt_TamperedCiphertextFails(t *testing.T) {
	t.Parallel()

	keys, err := crypto.DeriveSessionKeys(
		randomKey(t, crypto.PairingKeySize),
		randomKey(t, crypto.SessionSaltSize),
	)
	require.NoError(t, err)

	gcm, err := crypto.NewAEAD(keys.C2SKey)
	require.NoError(t, err)

	plaintext := []byte("hello world")
	aad := []byte("token:ws")
	ciphertext, err := crypto.Encrypt(gcm, keys.C2SNonce, 0, plaintext, aad)
	require.NoError(t, err)

	// Flip a bit in the middle of the ciphertext.
	tampered := bytes.Clone(ciphertext)
	tampered[len(tampered)/2] ^= 0x01

	_, err = crypto.Decrypt(gcm, keys.C2SNonce, 0, tampered, aad)
	assert.Error(t, err, "tampered ciphertext must fail authentication")
}

func TestEncrypt_CounterExhausted(t *testing.T) {
	t.Parallel()

	keys, err := crypto.DeriveSessionKeys(
		randomKey(t, crypto.PairingKeySize),
		randomKey(t, crypto.SessionSaltSize),
	)
	require.NoError(t, err)

	gcm, err := crypto.NewAEAD(keys.C2SKey)
	require.NoError(t, err)

	_, err = crypto.Encrypt(gcm, keys.C2SNonce, math.MaxUint64, []byte("x"), nil)
	assert.ErrorIs(t, err, crypto.ErrCounterExhausted)
}

func TestDecrypt_CounterExhausted(t *testing.T) {
	t.Parallel()

	keys, err := crypto.DeriveSessionKeys(
		randomKey(t, crypto.PairingKeySize),
		randomKey(t, crypto.SessionSaltSize),
	)
	require.NoError(t, err)

	gcm, err := crypto.NewAEAD(keys.C2SKey)
	require.NoError(t, err)

	_, err = crypto.Decrypt(gcm, keys.C2SNonce, math.MaxUint64, []byte("x"), nil)
	assert.ErrorIs(t, err, crypto.ErrCounterExhausted)
}

func TestNewAEAD_InvalidKeySize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		key  []byte
	}{
		{name: "empty", key: []byte{}},
		{name: "too short", key: make([]byte, 16)},
		{name: "too long", key: make([]byte, 64)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := crypto.NewAEAD(tt.key)
			assert.Error(t, err)
		})
	}
}

func TestEncrypt_InvalidNonceSize(t *testing.T) {
	t.Parallel()

	gcm, err := crypto.NewAEAD(randomKey(t, crypto.AESKeySize))
	require.NoError(t, err)

	_, err = crypto.Encrypt(gcm, make([]byte, 8), 0, []byte("x"), nil)
	assert.Error(t, err)
}

func TestDecrypt_InvalidNonceSize(t *testing.T) {
	t.Parallel()

	gcm, err := crypto.NewAEAD(randomKey(t, crypto.AESKeySize))
	require.NoError(t, err)

	_, err = crypto.Decrypt(gcm, make([]byte, 8), 0, []byte("x"), nil)
	assert.Error(t, err)
}

// TestNonceUniqueness verifies that different counter values produce
// distinct nonces. AES-GCM nonce reuse is catastrophic, so this is the
// single most important property of the counter-derived nonce design.
func TestNonceUniqueness(t *testing.T) {
	t.Parallel()

	keys, err := crypto.DeriveSessionKeys(
		randomKey(t, crypto.PairingKeySize),
		randomKey(t, crypto.SessionSaltSize),
	)
	require.NoError(t, err)

	gcm, err := crypto.NewAEAD(keys.C2SKey)
	require.NoError(t, err)

	// Encrypt the same plaintext at many counter values; ciphertexts must
	// all differ. (Same plaintext + same key + same nonce = same ciphertext;
	// uniqueness here proves nonces are unique.)
	plaintext := []byte("test")
	seen := make(map[string]struct{}, 1000)
	for counter := range uint64(1000) {
		ct, encErr := crypto.Encrypt(gcm, keys.C2SNonce, counter, plaintext, nil)
		require.NoError(t, encErr)
		key := string(ct)
		_, dup := seen[key]
		require.False(t, dup, "duplicate ciphertext at counter %d implies nonce collision", counter)
		seen[key] = struct{}{}
	}
}

// TestErrorsExported verifies the sentinel errors are usable with errors.Is.
func TestErrorsExported(t *testing.T) {
	t.Parallel()

	wrapped := errors.Join(errors.New("context"), crypto.ErrCounterExhausted)
	require.ErrorIs(t, wrapped, crypto.ErrCounterExhausted)

	wrapped2 := errors.Join(errors.New("context"), crypto.ErrInvalidPairingKey)
	require.ErrorIs(t, wrapped2, crypto.ErrInvalidPairingKey)

	wrapped3 := errors.Join(errors.New("context"), crypto.ErrInvalidSessionSalt)
	require.ErrorIs(t, wrapped3, crypto.ErrInvalidSessionSalt)
}
