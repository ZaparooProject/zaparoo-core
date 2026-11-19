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
	"encoding/json"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/crypto"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/middleware"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// binaryFirstFrameFor lays a first frame out in the binary envelope.
func binaryFirstFrameFor(c *database.Client, salt, ct []byte) []byte {
	out := []byte{middleware.EncryptionProtoVersion}
	out = append(out, salt...)
	out = append(out, byte(len(c.AuthToken))) //nolint:gosec // test tokens are short
	out = append(out, c.AuthToken...)
	return append(out, ct...)
}

func TestParseBinaryFirstFrame(t *testing.T) {
	t.Parallel()

	c, _ := pairedClient(t)
	salt := randomSalt(t)
	ct := []byte("ciphertext-bytes")
	wire := binaryFirstFrameFor(c, salt, ct)

	frame, err := middleware.ParseBinaryFirstFrame(wire)
	require.NoError(t, err)
	assert.Equal(t, middleware.EncryptionProtoVersion, frame.Version)
	assert.Equal(t, salt, frame.SessionSalt)
	assert.Equal(t, c.AuthToken, frame.AuthToken)
	assert.Equal(t, ct, frame.Ciphertext)

	// Cut anywhere short of one ciphertext byte, it is refused.
	headerLen := len(wire) - len(ct)
	for n := range headerLen + 1 {
		_, err = middleware.ParseBinaryFirstFrame(wire[:n])
		require.ErrorIs(t, err, middleware.ErrInvalidFrame, "%d bytes", n)
	}

	// A zero-length token is not a token.
	empty := append([]byte{middleware.EncryptionProtoVersion}, salt...)
	empty = append(empty, 0, 'x')
	_, err = middleware.ParseBinaryFirstFrame(empty)
	require.ErrorIs(t, err, middleware.ErrInvalidFrame)
}

func TestEstablishBinarySession(t *testing.T) {
	t.Parallel()

	c, _ := pairedClient(t)
	db := helpers.NewMockUserDBI()
	db.On("GetClientByToken", c.AuthToken).Return(c, nil)
	mgr := middleware.NewEncryptionGateway(db)

	salt := randomSalt(t)
	plaintext := []byte(`{"jsonrpc":"2.0","method":"version","id":1}`)
	ct := encryptForTransport(t, c, salt, plaintext, 0, middleware.TransportWebSocket)
	frame, err := middleware.ParseBinaryFirstFrame(binaryFirstFrameFor(c, salt, ct))
	require.NoError(t, err)

	cs, decrypted, err := mgr.EstablishBinarySession(frame, "192.168.1.50", middleware.TransportWebSocket)
	require.NoError(t, err)
	assert.Equal(t, plaintext, decrypted)
	assert.Equal(t, middleware.FrameBinary, cs.Format())

	// Later frames are the ciphertext alone, in both directions.
	second := []byte(`{"jsonrpc":"2.0","method":"media","id":2}`)
	pt, err := cs.DecryptFrame(encryptForTransport(t, c, salt, second, 1, middleware.TransportWebSocket))
	require.NoError(t, err)
	assert.Equal(t, second, pt)

	reply := []byte(`{"jsonrpc":"2.0","result":true,"id":2}`)
	var wire []byte
	require.NoError(t, cs.SendEncryptedFrame(reply, func(b []byte) error {
		wire = append([]byte(nil), b...)
		return nil
	}))
	keys, err := crypto.DeriveSessionKeys(c.PairingKey, salt)
	require.NoError(t, err)
	s2c, err := crypto.NewAEAD(keys.S2CKey)
	require.NoError(t, err)
	opened, err := crypto.Decrypt(s2c, keys.S2CNonce, 0, wire, []byte(c.AuthToken+":"+middleware.TransportWebSocket))
	require.NoError(t, err, "the frame on the wire is the bare ciphertext")
	assert.Equal(t, reply, opened)
	assert.Less(t, len(wire), len(reply)+32, "no base64 and no JSON wrapper")

	// The JSON envelope is not accepted on a binary session.
	wrapped, err := json.Marshal(middleware.EncryptedFrame{
		Ciphertext: base64.StdEncoding.EncodeToString(encryptForTransport(
			t, c, salt, second, 2, middleware.TransportWebSocket,
		)),
	})
	require.NoError(t, err)
	_, err = cs.DecryptFrame(wrapped)
	require.Error(t, err)
}

// The same salt cannot open a second session in the other format: the replay
// defence is shared.
func TestEstablishBinarySession_SharesReplayDefenceWithJSON(t *testing.T) {
	t.Parallel()

	c, _ := pairedClient(t)
	db := helpers.NewMockUserDBI()
	db.On("GetClientByToken", c.AuthToken).Return(c, nil)
	mgr := middleware.NewEncryptionGateway(db)

	salt := randomSalt(t)
	ct := encryptForTransport(t, c, salt, []byte(`{"jsonrpc":"2.0","method":"version","id":1}`), 0,
		middleware.TransportWebSocket)
	frame, err := middleware.ParseBinaryFirstFrame(binaryFirstFrameFor(c, salt, ct))
	require.NoError(t, err)
	_, _, err = mgr.EstablishBinarySession(frame, "192.168.1.50", middleware.TransportWebSocket)
	require.NoError(t, err)

	cs, _, err := mgr.EstablishSession(firstFrameFor(c, salt, ct), "192.168.1.50")
	require.Error(t, err)
	assert.Nil(t, cs)
}

func TestEstablishBinarySession_RejectsWrongVersionAndUnknownToken(t *testing.T) {
	t.Parallel()

	c, _ := pairedClient(t)
	db := helpers.NewMockUserDBI()
	db.On("GetClientByToken", c.AuthToken).Return(c, nil)
	db.On("GetClientByToken", "nobody").Return(nil, nil)
	mgr := middleware.NewEncryptionGateway(db)

	salt := randomSalt(t)
	ct := encryptForTransport(t, c, salt, []byte("{}"), 0, middleware.TransportBLE)
	frame, err := middleware.ParseBinaryFirstFrame(binaryFirstFrameFor(c, salt, ct))
	require.NoError(t, err)

	wrongVersion := frame
	wrongVersion.Version++
	_, _, err = mgr.EstablishBinarySession(wrongVersion, "ble:peer", middleware.TransportBLE)
	require.ErrorIs(t, err, middleware.ErrUnsupportedVersion)

	unknown := frame
	unknown.AuthToken = "nobody"
	_, _, err = mgr.EstablishBinarySession(unknown, "ble:peer", middleware.TransportBLE)
	require.ErrorIs(t, err, middleware.ErrUnknownAuthToken)

	// A JSON session keeps the JSON envelope.
	js, _, err := mgr.EstablishSessionForTransport(firstFrameFor(c, salt, ct), "ble:peer", middleware.TransportBLE)
	require.NoError(t, err)
	assert.Equal(t, middleware.FrameJSON, js.Format())
}

// FuzzParseBinaryFirstFrame checks that no input makes the parser panic or
// hand back slices outside the message.
func FuzzParseBinaryFirstFrame(f *testing.F) {
	salt := make([]byte, crypto.SessionSaltSize)
	seed := append([]byte{middleware.EncryptionProtoVersion}, salt...)
	seed = append(seed, 5)
	seed = append(seed, "tokenciphertext"...)
	f.Add(seed)
	f.Add([]byte{})
	f.Add([]byte{1})
	f.Add(append(append([]byte{1}, salt...), 255))

	f.Fuzz(func(t *testing.T, data []byte) {
		frame, err := middleware.ParseBinaryFirstFrame(data)
		if err != nil {
			return
		}
		if len(frame.SessionSalt) != crypto.SessionSaltSize || frame.AuthToken == "" || len(frame.Ciphertext) == 0 {
			t.Fatalf("accepted a frame with an empty part: %+v", frame)
		}
		total := 1 + len(frame.SessionSalt) + 1 + len(frame.AuthToken) + len(frame.Ciphertext)
		if total != len(data) {
			t.Fatalf("parts add up to %d bytes of a %d-byte message", total, len(data))
		}
	})
}
