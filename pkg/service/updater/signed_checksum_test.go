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

package updater

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	selfupdate "github.com/creativeprojects/go-selfupdate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEd25519Validator_ValidSignature(t *testing.T) {
	t.Parallel()

	pub, priv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)

	data := []byte("checksums content")
	sig := ed25519.Sign(priv, data)

	v := &ed25519Validator{publicKey: pub}
	require.NoError(t, v.Validate("checksums.txt", data, sig))
}

func TestEd25519Validator_InvalidSignature(t *testing.T) {
	t.Parallel()

	pub, _, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)

	v := &ed25519Validator{publicKey: pub}
	err = v.Validate("checksums.txt", []byte("data"), []byte("bad signature that is not valid"))
	assert.ErrorIs(t, err, errInvalidSignature)
}

func TestEd25519Validator_WrongKey(t *testing.T) {
	t.Parallel()

	_, priv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)

	otherPub, _, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)

	data := []byte("checksums content")
	sig := ed25519.Sign(priv, data)

	v := &ed25519Validator{publicKey: otherPub}
	assert.ErrorIs(t, v.Validate("checksums.txt", data, sig), errInvalidSignature)
}

func TestEd25519Validator_TamperedData(t *testing.T) {
	t.Parallel()

	pub, priv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)

	data := []byte("original checksums")
	sig := ed25519.Sign(priv, data)

	v := &ed25519Validator{publicKey: pub}
	assert.ErrorIs(t, v.Validate("checksums.txt", []byte("tampered checksums"), sig), errInvalidSignature)
}

func TestEd25519Validator_GetValidationAssetName(t *testing.T) {
	t.Parallel()

	v := &ed25519Validator{}
	assert.Equal(t, "checksums.txt.sig", v.GetValidationAssetName("checksums.txt"))
}

func TestNewSignedChecksumValidator(t *testing.T) {
	t.Parallel()

	v, err := newSignedChecksumValidator()
	require.NoError(t, err)
	assert.NotNil(t, v)
}

func TestSignedChecksumValidator_EndToEnd(t *testing.T) {
	t.Parallel()

	pub, priv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)

	// Build a checksums.txt and sign it.
	release := []byte("fake binary content")
	digest := sha256.Sum256(release)
	checksums := []byte(hex.EncodeToString(digest[:]) + "  test.tar.gz\n")
	sig := ed25519.Sign(priv, checksums)

	// Wire up the same PatternValidator chain that newSignedChecksumValidator
	// creates, but with our test key.
	v := &ed25519Validator{publicKey: pub}

	// Step 1: verify checksums.txt signature.
	require.NoError(t, v.Validate("checksums.txt", checksums, sig))

	// Step 2: verify release against checksums.txt.
	cv := &selfupdate.ChecksumValidator{UniqueFilename: "checksums.txt"}
	require.NoError(t, cv.Validate("test.tar.gz", release, checksums))
}
