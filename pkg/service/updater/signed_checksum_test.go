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

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/updater/otameta"
	selfupdate "github.com/creativeprojects/go-selfupdate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testValidator builds a validator whose only known key is pub, under the id
// the manifest is pretending to have named.
func testValidator(keyID string, pub ed25519.PublicKey) *ed25519Validator {
	ref := &keyRef{}
	ref.set(keyID)
	return &ed25519Validator{
		key: ref,
		lookup: func(id string) (ed25519.PublicKey, error) {
			if id != keyID {
				return nil, otameta.ErrUnknownKeyID
			}
			return pub, nil
		},
	}
}

func TestEd25519Validator_ValidSignature(t *testing.T) {
	t.Parallel()

	pub, priv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)

	data := []byte("checksums content")
	sig := ed25519.Sign(priv, data)

	require.NoError(t, testValidator("k1", pub).Validate("checksums.txt", data, sig))
}

func TestEd25519Validator_InvalidSignature(t *testing.T) {
	t.Parallel()

	pub, _, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)

	err = testValidator("k1", pub).
		Validate("checksums.txt", []byte("data"), []byte("bad signature that is not valid"))
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

	assert.ErrorIs(t, testValidator("k1", otherPub).Validate("checksums.txt", data, sig), errInvalidSignature)
}

func TestEd25519Validator_TamperedData(t *testing.T) {
	t.Parallel()

	pub, priv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)

	data := []byte("original checksums")
	sig := ed25519.Sign(priv, data)

	err = testValidator("k1", pub).Validate("checksums.txt", []byte("tampered checksums"), sig)
	assert.ErrorIs(t, err, errInvalidSignature)
}

// The checksums chain runs after the manifest, so it should only ever see a key
// id that a verified manifest named. If it somehow runs first, refusing is the
// only safe answer — falling back to a build-time default would validate
// against a key the publisher may have rotated away from.
func TestEd25519Validator_NoKeyEstablished(t *testing.T) {
	t.Parallel()

	pub, priv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)

	data := []byte("checksums content")
	sig := ed25519.Sign(priv, data)

	v := &ed25519Validator{
		key:    &keyRef{},
		lookup: func(string) (ed25519.PublicKey, error) { return pub, nil },
	}
	assert.ErrorIs(t, v.Validate("checksums.txt", data, sig), errNoVerifiedKey)
}

// An id with no embedded key is a hard reject rather than a prompt to try the
// other keys, which is what makes revocation mean anything.
func TestEd25519Validator_UnknownKeyID(t *testing.T) {
	t.Parallel()

	pub, priv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)

	data := []byte("checksums content")
	sig := ed25519.Sign(priv, data)

	v := testValidator("k1", pub)
	v.key.set("k99")

	assert.ErrorIs(t, v.Validate("checksums.txt", data, sig), otameta.ErrUnknownKeyID)
}

func TestEd25519Validator_GetValidationAssetName(t *testing.T) {
	t.Parallel()

	v := &ed25519Validator{}
	assert.Equal(t, "checksums.txt.sig", v.GetValidationAssetName("checksums.txt"))
}

func TestNewSignedChecksumValidator(t *testing.T) {
	t.Parallel()

	assert.NotNil(t, newSignedChecksumValidator(&keyRef{}))
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

	// Step 1: verify checksums.txt signature.
	require.NoError(t, testValidator("k1", pub).Validate("checksums.txt", checksums, sig))

	// Step 2: verify release against checksums.txt.
	cv := &selfupdate.ChecksumValidator{UniqueFilename: "checksums.txt"}
	require.NoError(t, cv.Validate("test.tar.gz", release, checksums))
}
