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
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHashPIN_RoundTrip(t *testing.T) {
	t.Parallel()

	hash, err := HashPIN("1234")
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(hash, "pbkdf2-sha256$"))
	assert.True(t, VerifyPIN("1234", hash))
	assert.False(t, VerifyPIN("4321", hash))
	assert.False(t, VerifyPIN("", hash))
}

func TestHashPIN_SaltsDiffer(t *testing.T) {
	t.Parallel()

	hash1, err := HashPIN("12345678")
	require.NoError(t, err)
	hash2, err := HashPIN("12345678")
	require.NoError(t, err)
	assert.NotEqual(t, hash1, hash2)
	assert.True(t, VerifyPIN("12345678", hash1))
	assert.True(t, VerifyPIN("12345678", hash2))
}

func TestHashPIN_RejectsInvalidFormats(t *testing.T) {
	t.Parallel()

	for _, pin := range []string{"", "123", "123456789", "12a4", "12.4", "abcd"} {
		_, err := HashPIN(pin)
		require.ErrorIs(t, err, ErrInvalidPINFormat, "pin %q", pin)
	}
}

func TestVerifyPIN_MalformedHash(t *testing.T) {
	t.Parallel()

	assert.False(t, VerifyPIN("1234", ""))
	assert.False(t, VerifyPIN("1234", "not-a-hash"))
	assert.False(t, VerifyPIN("1234", "pbkdf2-sha256$abc$def$ghi"))
	assert.False(t, VerifyPIN("1234", "pbkdf2-sha256$0$AAAA$AAAA"))
	assert.False(t, VerifyPIN("1234", "other-scheme$600000$AAAA$AAAA"))
}
