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

package middleware_test

import (
	"encoding/base64"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/crypto"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/middleware"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// encryptForTransport encrypts a client-to-server frame with the AAD the
// given transport label produces.
func encryptForTransport(
	t *testing.T, c *database.Client, salt, plaintext []byte, counter uint64, transport string,
) []byte {
	t.Helper()
	keys, err := crypto.DeriveSessionKeys(c.PairingKey, salt)
	require.NoError(t, err)
	gcm, err := crypto.NewAEAD(keys.C2SKey)
	require.NoError(t, err)
	ct, err := crypto.Encrypt(gcm, keys.C2SNonce, counter, plaintext, []byte(c.AuthToken+":"+transport))
	require.NoError(t, err)
	return ct
}

func firstFrameFor(c *database.Client, salt, ct []byte) middleware.EncryptedFirstFrame {
	return middleware.EncryptedFirstFrame{
		Version:     middleware.EncryptionProtoVersion,
		Ciphertext:  base64.StdEncoding.EncodeToString(ct),
		AuthToken:   c.AuthToken,
		SessionSalt: base64.StdEncoding.EncodeToString(salt),
	}
}

func TestEstablishSessionForTransport_BLE(t *testing.T) {
	t.Parallel()

	c, _ := pairedClient(t)
	db := helpers.NewMockUserDBI()
	db.On("GetClientByToken", c.AuthToken).Return(c, nil)
	mgr := middleware.NewEncryptionGateway(db)

	salt := randomSalt(t)
	plaintext := []byte(`{"jsonrpc":"2.0","method":"version","id":1}`)
	ct := encryptForTransport(t, c, salt, plaintext, 0, middleware.TransportBLE)

	cs, decrypted, err := mgr.EstablishSessionForTransport(
		firstFrameFor(c, salt, ct), "ble:AA:BB:CC:DD:EE:FF", middleware.TransportBLE,
	)
	require.NoError(t, err)
	require.NotNil(t, cs)
	assert.Equal(t, plaintext, decrypted)

	// Subsequent frames on the session keep the BLE binding.
	second := []byte(`{"jsonrpc":"2.0","method":"media","id":2}`)
	pt, err := cs.DecryptIncoming(encryptForTransport(t, c, salt, second, 1, middleware.TransportBLE))
	require.NoError(t, err)
	assert.Equal(t, second, pt)
}

func TestEstablishSessionForTransport_RejectsOtherTransportsFrame(t *testing.T) {
	t.Parallel()

	c, _ := pairedClient(t)
	db := helpers.NewMockUserDBI()
	db.On("GetClientByToken", c.AuthToken).Return(c, nil)
	mgr := middleware.NewEncryptionGateway(db)

	plaintext := []byte(`{"jsonrpc":"2.0","method":"version","id":1}`)

	tests := []struct {
		name      string
		encrypted string
		presented string
	}{
		{name: "ws frame on ble", encrypted: middleware.TransportWebSocket, presented: middleware.TransportBLE},
		{name: "ble frame on ws", encrypted: middleware.TransportBLE, presented: middleware.TransportWebSocket},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			salt := randomSalt(t)
			ct := encryptForTransport(t, c, salt, plaintext, 0, tt.encrypted)
			cs, _, err := mgr.EstablishSessionForTransport(firstFrameFor(c, salt, ct), "192.168.1.50", tt.presented)
			require.Error(t, err)
			assert.Nil(t, cs)
		})
	}
}

func TestEstablishSession_DefaultsToWebSocketTransport(t *testing.T) {
	t.Parallel()

	c, _ := pairedClient(t)
	db := helpers.NewMockUserDBI()
	db.On("GetClientByToken", c.AuthToken).Return(c, nil)
	mgr := middleware.NewEncryptionGateway(db)

	salt := randomSalt(t)
	plaintext := []byte(`{"jsonrpc":"2.0","method":"version","id":1}`)
	ct := encryptForTransport(t, c, salt, plaintext, 0, middleware.TransportWebSocket)

	cs, decrypted, err := mgr.EstablishSession(firstFrameFor(c, salt, ct), "192.168.1.50")
	require.NoError(t, err)
	require.NotNil(t, cs)
	assert.Equal(t, plaintext, decrypted)
}
