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
	"embed"
	"encoding/base64"
	"errors"
	"fmt"
	"path"
	"strings"
	"sync"
)

// DefaultKeyID is the key the publisher signs with unless told otherwise. It
// exists so tooling has a default to pass around; verification always uses the
// key named by the manifest, never this one.
const DefaultKeyID = "k1"

// keyFS holds the ed25519 public keys update metadata may be signed with, one
// bare base64 key per file, named after its key id. The matching private keys
// live in the ota-publish GitHub Environment. Adding or removing a key changes
// what the fleet will trust, so the directory is owned by
// @ZaparooProject/admins via CODEOWNERS.
//
// Rotation is additive: ship a release embedding both the old and the new key,
// wait for it to reach the fleet, then start signing with the new one. Devices
// that never took the intermediate release stop updating rather than accept a
// key they do not know, so the middle step cannot be rushed.
//
//go:embed keys/*.pub
var keyFS embed.FS

// ErrUnknownKeyID is returned for a key id with no embedded public key. It is
// deliberately fatal rather than a prompt to try the other keys: trying every
// key would mean a revoked key still verifies, which defeats revocation.
var ErrUnknownKeyID = errors.New("unknown update signing key id")

// keyset parses the embedded keys once. A malformed key is a build-time
// mistake, so the error is carried rather than swallowed.
var keyset = sync.OnceValues(loadKeys)

func loadKeys() (map[string]ed25519.PublicKey, error) {
	entries, err := keyFS.ReadDir("keys")
	if err != nil {
		return nil, fmt.Errorf("reading embedded key directory: %w", err)
	}

	keys := make(map[string]ed25519.PublicKey, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".pub") {
			continue
		}

		raw, readErr := keyFS.ReadFile(path.Join("keys", name))
		if readErr != nil {
			return nil, fmt.Errorf("reading embedded key %q: %w", name, readErr)
		}

		decoded, decodeErr := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw)))
		if decodeErr != nil {
			return nil, fmt.Errorf("decoding embedded key %q: %w", name, decodeErr)
		}
		if len(decoded) != ed25519.PublicKeySize {
			return nil, fmt.Errorf("embedded key %q is %d bytes, want %d",
				name, len(decoded), ed25519.PublicKeySize)
		}

		keys[strings.TrimSuffix(name, ".pub")] = decoded
	}

	if len(keys) == 0 {
		return nil, errors.New("no update signing keys are embedded")
	}
	return keys, nil
}

// PublicKey returns the embedded verification key for a key id.
func PublicKey(keyID string) (ed25519.PublicKey, error) {
	keys, err := keyset()
	if err != nil {
		return nil, err
	}

	key, ok := keys[keyID]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownKeyID, keyID)
	}
	return key, nil
}

// KeyIDs returns every embedded key id, for diagnostics and tests.
func KeyIDs() ([]string, error) {
	keys, err := keyset()
	if err != nil {
		return nil, err
	}

	ids := make([]string, 0, len(keys))
	for id := range keys {
		ids = append(ids, id)
	}
	return ids, nil
}
