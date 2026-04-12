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
	"encoding/base64"
	"errors"
	"fmt"

	selfupdate "github.com/creativeprojects/go-selfupdate"
)

// updateSigningPublicKey is the ed25519 public key used to verify the
// signature on checksums.txt. The corresponding private key is stored as
// a GitHub Actions secret and used to sign checksums at release time.
const updateSigningPublicKey = "/Lk2OtGRI9eAaQ+F+Nuwk4k0bSmWfhVsNUGm159QkJg="

var errInvalidSignature = errors.New("ed25519 signature verification failed")

// ed25519Validator verifies a detached ed25519 signature over the input bytes.
type ed25519Validator struct {
	publicKey ed25519.PublicKey
}

func (v *ed25519Validator) Validate(_ string, input, signature []byte) error {
	if !ed25519.Verify(v.publicKey, input, signature) {
		return errInvalidSignature
	}
	return nil
}

func (*ed25519Validator) GetValidationAssetName(releaseFilename string) string {
	return releaseFilename + ".sig"
}

// newSignedChecksumValidator creates a PatternValidator that verifies release
// archives against checksums.txt, and verifies checksums.txt itself against
// an ed25519 signature in checksums.txt.sig.
func newSignedChecksumValidator() (*selfupdate.PatternValidator, error) {
	pubKeyBytes, err := base64.StdEncoding.DecodeString(updateSigningPublicKey)
	if err != nil {
		return nil, fmt.Errorf("decoding update signing public key: %w", err)
	}

	if len(pubKeyBytes) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("invalid public key size: got %d, want %d", len(pubKeyBytes), ed25519.PublicKeySize)
	}

	v := new(selfupdate.PatternValidator).
		Add("checksums.txt", &ed25519Validator{publicKey: pubKeyBytes}).
		SkipValidation("*.sig").
		Add("*", &selfupdate.ChecksumValidator{UniqueFilename: "checksums.txt"})

	return v, nil
}
