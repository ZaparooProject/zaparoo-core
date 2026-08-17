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

package otameta

import (
	"crypto/ed25519"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPublicKey_DefaultKeyIsEmbedded(t *testing.T) {
	t.Parallel()

	key, err := PublicKey(DefaultKeyID)
	require.NoError(t, err)
	assert.Len(t, []byte(key), ed25519.PublicKeySize)
}

func TestPublicKey_UnknownKeyID(t *testing.T) {
	t.Parallel()

	_, err := PublicKey("nosuchkey")
	require.ErrorIs(t, err, ErrUnknownKeyID)
}

// An empty key id must not resolve to anything. It is what a manifest with no
// key_id would produce if the pre-parse check were ever removed.
func TestPublicKey_EmptyKeyID(t *testing.T) {
	t.Parallel()

	_, err := PublicKey("")
	require.ErrorIs(t, err, ErrUnknownKeyID)
}

func TestKeyIDs_IncludesDefault(t *testing.T) {
	t.Parallel()

	ids, err := KeyIDs()
	require.NoError(t, err)
	require.NotEmpty(t, ids)
	assert.Contains(t, ids, DefaultKeyID)
}

// Every embedded key must be a usable ed25519 public key. A malformed one is a
// build mistake that would otherwise only surface on a device mid-rotation.
func TestKeyIDs_AllEmbeddedKeysAreValid(t *testing.T) {
	t.Parallel()

	ids, err := KeyIDs()
	require.NoError(t, err)

	for _, id := range ids {
		key, keyErr := PublicKey(id)
		require.NoErrorf(t, keyErr, "key %q", id)
		assert.Lenf(t, []byte(key), ed25519.PublicKeySize, "key %q", id)
	}
}
