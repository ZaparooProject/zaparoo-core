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
	"errors"
	"fmt"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/updater/otameta"
	selfupdate "github.com/creativeprojects/go-selfupdate"
)

var (
	errInvalidSignature = errors.New("ed25519 signature verification failed")

	// errNoVerifiedKey means the checksums validator ran without a verified
	// manifest having named a key first. It cannot happen in the normal flow —
	// listing releases is what triggers a download — so it is treated as a bug
	// and fails the update rather than falling back to a default key.
	errNoVerifiedKey = errors.New("no signing key established from a verified manifest")
)

// keyRef carries the key id from the verified manifest to the checksums
// validator, which go-selfupdate runs later and gives no manifest access.
// Resolving the key here rather than at build time is what lets a rotation take
// effect for the checksums chain at the same moment it does for the manifest.
type keyRef struct {
	keyID string
	mu    syncutil.Mutex
}

func (k *keyRef) set(keyID string) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.keyID = keyID
}

func (k *keyRef) get() string {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.keyID
}

// keyLookup resolves a key id to a verification key.
type keyLookup func(keyID string) (ed25519.PublicKey, error)

// ed25519Validator verifies a detached ed25519 signature over the input bytes.
type ed25519Validator struct {
	key    *keyRef
	lookup keyLookup
}

func (v *ed25519Validator) Validate(_ string, input, signature []byte) error {
	keyID := v.key.get()
	if keyID == "" {
		return errNoVerifiedKey
	}

	pub, err := v.lookup(keyID)
	if err != nil {
		return fmt.Errorf("resolving signing key: %w", err)
	}
	if !ed25519.Verify(pub, input, signature) {
		return fmt.Errorf("%w with key %q", errInvalidSignature, keyID)
	}
	return nil
}

func (*ed25519Validator) GetValidationAssetName(releaseFilename string) string {
	return releaseFilename + ".sig"
}

// newSignedChecksumValidator creates a PatternValidator that verifies release
// archives against checksums.txt, and verifies checksums.txt itself against an
// ed25519 signature in checksums.txt.sig, using whichever key the verified
// manifest named.
func newSignedChecksumValidator(key *keyRef) *selfupdate.PatternValidator {
	return new(selfupdate.PatternValidator).
		Add("checksums.txt", &ed25519Validator{key: key, lookup: otameta.PublicKey}).
		SkipValidation("*.sig").
		Add("*", &selfupdate.ChecksumValidator{UniqueFilename: "checksums.txt"})
}
