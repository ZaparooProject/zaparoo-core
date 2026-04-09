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

// Package crypto provides AES-256-GCM encryption with HKDF-derived per-session
// keys for the Zaparoo API WebSocket transport.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

// PairingKeySize is the size in bytes of the long-term pairing key derived
// from the PAKE exchange and stored in the clients table.
const PairingKeySize = 32

// SessionSaltSize is the required size in bytes of the per-connection session
// salt sent by the client on the first WebSocket frame.
const SessionSaltSize = 16

// AESKeySize is the size in bytes of an AES-256 key.
const AESKeySize = 32

// NonceSize is the size in bytes of an AES-GCM nonce.
const NonceSize = 12

// HKDF info strings for domain separation (versioned for future changes).
const (
	infoC2SKey   = "zaparoo-c2s-v1"
	infoS2CKey   = "zaparoo-s2c-v1"
	infoC2SNonce = "zaparoo-c2s-nonce-v1"
	infoS2CNonce = "zaparoo-s2c-nonce-v1"
)

// ErrCounterExhausted prevents silent nonce reuse on counter overflow (unreachable in practice).
var ErrCounterExhausted = errors.New("counter exhausted: rotate session keys")

// ErrInvalidPairingKey is returned when the pairing key is not exactly PairingKeySize bytes.
var ErrInvalidPairingKey = errors.New("pairing key must be 32 bytes")

// ErrInvalidSessionSalt is returned when the session salt is not exactly SessionSaltSize bytes.
var ErrInvalidSessionSalt = errors.New("session salt must be 16 bytes")

// SessionKeys holds the four derived values for a single WebSocket session:
// directional AES-256 keys and directional 12-byte nonce bases.
type SessionKeys struct {
	C2SKey   []byte
	S2CKey   []byte
	C2SNonce []byte
	S2CNonce []byte
}

// DeriveSessionKeys derives directional session keys from a pairing key and
// per-connection salt. Separate directional keys prevent reflection attacks.
func DeriveSessionKeys(pairingKey, sessionSalt []byte) (*SessionKeys, error) {
	if len(pairingKey) != PairingKeySize {
		return nil, ErrInvalidPairingKey
	}
	if len(sessionSalt) != SessionSaltSize {
		return nil, ErrInvalidSessionSalt
	}

	prk, err := hkdf.Extract(sha256.New, pairingKey, sessionSalt)
	if err != nil {
		return nil, fmt.Errorf("hkdf extract: %w", err)
	}

	c2sKey, err := hkdf.Expand(sha256.New, prk, infoC2SKey, AESKeySize)
	if err != nil {
		return nil, fmt.Errorf("hkdf expand c2s key: %w", err)
	}
	s2cKey, err := hkdf.Expand(sha256.New, prk, infoS2CKey, AESKeySize)
	if err != nil {
		return nil, fmt.Errorf("hkdf expand s2c key: %w", err)
	}
	c2sNonce, err := hkdf.Expand(sha256.New, prk, infoC2SNonce, NonceSize)
	if err != nil {
		return nil, fmt.Errorf("hkdf expand c2s nonce: %w", err)
	}
	s2cNonce, err := hkdf.Expand(sha256.New, prk, infoS2CNonce, NonceSize)
	if err != nil {
		return nil, fmt.Errorf("hkdf expand s2c nonce: %w", err)
	}

	return &SessionKeys{
		C2SKey:   c2sKey,
		S2CKey:   s2cKey,
		C2SNonce: c2sNonce,
		S2CNonce: s2cNonce,
	}, nil
}

// NewAEAD creates a cipher.AEAD from a 32-byte AES-256 key.
func NewAEAD(key []byte) (cipher.AEAD, error) {
	if len(key) != AESKeySize {
		return nil, fmt.Errorf("aes key must be %d bytes, got %d", AESKeySize, len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("aes new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("cipher new gcm: %w", err)
	}
	return gcm, nil
}

// Encrypt encrypts plaintext using AES-256-GCM with a counter-derived nonce.
// The caller must ensure the counter never repeats with the same key.
func Encrypt(gcm cipher.AEAD, nonceBase []byte, counter uint64, plaintext, aad []byte) ([]byte, error) {
	if counter == math.MaxUint64 {
		return nil, ErrCounterExhausted
	}
	if len(nonceBase) != NonceSize {
		return nil, fmt.Errorf("nonce base must be %d bytes, got %d", NonceSize, len(nonceBase))
	}
	nonce := buildNonce(nonceBase, counter)
	return gcm.Seal(nil, nonce, plaintext, aad), nil
}

// Decrypt decrypts ciphertext using AES-256-GCM with a counter-derived nonce.
func Decrypt(gcm cipher.AEAD, nonceBase []byte, counter uint64, ciphertext, aad []byte) ([]byte, error) {
	if counter == math.MaxUint64 {
		return nil, ErrCounterExhausted
	}
	if len(nonceBase) != NonceSize {
		return nil, fmt.Errorf("nonce base must be %d bytes, got %d", NonceSize, len(nonceBase))
	}
	nonce := buildNonce(nonceBase, counter)
	plaintext, err := gcm.Open(nil, nonce, ciphertext, aad)
	if err != nil {
		return nil, fmt.Errorf("gcm open: %w", err)
	}
	return plaintext, nil
}

// buildNonce XORs the counter into the last 8 bytes of the nonce base.
func buildNonce(base []byte, counter uint64) []byte {
	nonce := make([]byte, NonceSize)
	copy(nonce, base)
	var counterBytes [8]byte
	binary.BigEndian.PutUint64(counterBytes[:], counter)
	for i := range 8 {
		nonce[4+i] ^= counterBytes[i]
	}
	return nonce
}
