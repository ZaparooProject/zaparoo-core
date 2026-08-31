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

package profiles

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Profile PINs are short convenience barriers, not credentials. They are
// still stored only as PBKDF2-SHA256 hashes, with brute force limited by
// the in-memory rate limiter in Service. The iteration count is modest by
// design: a 4-8 digit keyspace falls instantly to offline cracking at any
// feasible count (the online rate limiter is the real defense), and each
// verify blocks an API handler on ARM-class device CPU. The count is
// encoded in the hash, so raising it later costs nothing.
const (
	pinMinLen     = 4
	pinMaxLen     = 8
	pinIterations = 50_000
	pinSaltLen    = 16
	pinKeyLen     = 32
	pinHashPrefix = "pbkdf2-sha256"
)

// ErrInvalidPINFormat is returned when a PIN is not 4-8 digits.
var ErrInvalidPINFormat = errors.New("PIN must be 4 to 8 digits")

// HashPIN validates and hashes a PIN for storage. The encoded form is
// "pbkdf2-sha256$<iterations>$<base64 salt>$<base64 key>".
func HashPIN(pin string) (string, error) {
	if err := validatePIN(pin); err != nil {
		return "", err
	}

	salt := make([]byte, pinSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("failed to generate PIN salt: %w", err)
	}

	key, err := pbkdf2.Key(sha256.New, pin, salt, pinIterations, pinKeyLen)
	if err != nil {
		return "", fmt.Errorf("failed to derive PIN hash: %w", err)
	}

	encoded := pinHashPrefix + "$" +
		strconv.Itoa(pinIterations) + "$" +
		base64.RawStdEncoding.EncodeToString(salt) + "$" +
		base64.RawStdEncoding.EncodeToString(key)
	return encoded, nil
}

// VerifyPIN reports whether pin matches the encoded hash. Malformed hashes
// verify as false.
func VerifyPIN(pin, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != pinHashPrefix {
		return false
	}

	iterations, err := strconv.Atoi(parts[1])
	if err != nil || iterations <= 0 {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[2])
	if err != nil {
		return false
	}
	expected, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil {
		return false
	}

	key, err := pbkdf2.Key(sha256.New, pin, salt, iterations, len(expected))
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(key, expected) == 1
}

func validatePIN(pin string) error {
	if len(pin) < pinMinLen || len(pin) > pinMaxLen {
		return ErrInvalidPINFormat
	}
	for _, r := range pin {
		if r < '0' || r > '9' {
			return ErrInvalidPINFormat
		}
	}
	return nil
}
