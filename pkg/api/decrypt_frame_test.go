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

package api

import (
	"encoding/json"
	"testing"

	apimiddleware "github.com/ZaparooProject/zaparoo-core/v2/pkg/api/middleware"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func marshalFirstFrame(t *testing.T, frame apimiddleware.EncryptedFirstFrame) []byte {
	t.Helper()
	data, err := json.Marshal(frame) //nolint:gosec // test fixture; the token is opaque test data
	require.NoError(t, err)
	return data
}

func TestDecryptFrame_Plaintext(t *testing.T) {
	t.Parallel()

	first := newTestEncryptionFirstFrame(t)
	msg := []byte(`{"jsonrpc":"2.0","method":"version","id":1}`)

	frame, err := decryptFrame(nil, msg, first.gateway, testEncryptionSourceIP, apimiddleware.TransportWebSocket)
	require.NoError(t, err)
	assert.Equal(t, framePlaintext, frame.outcome)
	assert.Nil(t, frame.session)
	assert.Equal(t, msg, frame.plaintext)
}

func TestDecryptFrame_EstablishesThenDecrypts(t *testing.T) {
	t.Parallel()

	first := newTestEncryptionFirstFrame(t)

	established, err := decryptFrame(
		nil, marshalFirstFrame(t, first.frame), first.gateway,
		testEncryptionSourceIP, apimiddleware.TransportWebSocket,
	)
	require.NoError(t, err)
	assert.Equal(t, frameEstablished, established.outcome)
	require.NotNil(t, established.session)
	assert.JSONEq(t, `{"jsonrpc":"2.0","method":"version","id":1}`, string(established.plaintext))

	second := []byte(`{"jsonrpc":"2.0","method":"media","id":2}`)
	decrypted, err := decryptFrame(
		established.session, first.secrets.encryptSubsequent(t, second, 1), first.gateway,
		testEncryptionSourceIP, apimiddleware.TransportWebSocket,
	)
	require.NoError(t, err)
	assert.Equal(t, frameDecrypted, decrypted.outcome)
	assert.Nil(t, decrypted.session, "an established session must not be replaced")
	assert.Equal(t, second, decrypted.plaintext)
}

func TestDecryptFrame_MalformedOnEstablishedSession(t *testing.T) {
	t.Parallel()

	first := newTestEncryptionFirstFrame(t)
	established, err := decryptFrame(
		nil, marshalFirstFrame(t, first.frame), first.gateway,
		testEncryptionSourceIP, apimiddleware.TransportWebSocket,
	)
	require.NoError(t, err)
	require.NotNil(t, established.session)

	for _, msg := range []string{`{"e":""}`, `not json`, `{"jsonrpc":"2.0","method":"version","id":1}`} {
		frame, err := decryptFrame(
			established.session, []byte(msg), first.gateway,
			testEncryptionSourceIP, apimiddleware.TransportWebSocket,
		)
		require.ErrorIs(t, err, apimiddleware.ErrInvalidFrame, msg)
		assert.Equal(t, frameDecrypted, frame.outcome, msg)
		assert.Nil(t, frame.plaintext, msg)
		assert.Nil(t, frame.session, msg)
	}
}

func TestDecryptFrame_UnsupportedVersion(t *testing.T) {
	t.Parallel()

	first := newTestEncryptionFirstFrame(t)
	firstFrame := first.frame
	firstFrame.Version = apimiddleware.EncryptionProtoVersion + 1

	frame, err := decryptFrame(
		nil, marshalFirstFrame(t, firstFrame), first.gateway,
		testEncryptionSourceIP, apimiddleware.TransportWebSocket,
	)
	require.ErrorIs(t, err, apimiddleware.ErrUnsupportedVersion)
	assert.Equal(t, frameUnsupportedVersion, frame.outcome)
	assert.Nil(t, frame.plaintext)
	assert.Nil(t, frame.session)
}

func TestDecryptFrame_TransportMismatchFailsToEstablish(t *testing.T) {
	t.Parallel()

	// The fixture encrypts its first frame for the WebSocket transport, so
	// presenting it as a BLE frame must fail the AEAD check.
	first := newTestEncryptionFirstFrame(t)

	frame, err := decryptFrame(
		nil, marshalFirstFrame(t, first.frame), first.gateway,
		testEncryptionSourceIP, apimiddleware.TransportBLE,
	)
	require.Error(t, err)
	assert.Equal(t, frameEstablished, frame.outcome)
	assert.Nil(t, frame.plaintext)
	assert.Nil(t, frame.session)
}
