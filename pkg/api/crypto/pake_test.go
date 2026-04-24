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

package crypto

import (
	"bytes"
	"encoding/json"
	"testing"
	"unicode"

	"github.com/schollz/pake/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEncodePakeMessage_ASCIIFieldNames(t *testing.T) {
	t.Parallel()
	client, err := pake.InitCurve([]byte("123456"), 0, "p256")
	require.NoError(t, err)

	wire, err := EncodePakeMessage(client.Bytes())
	require.NoError(t, err)

	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(wire, &raw))

	expectedKeys := []string{"role", "ux", "uy", "vx", "vy", "xx", "xy", "yx", "yy"}
	for _, key := range expectedKeys {
		_, ok := raw[key]
		assert.True(t, ok, "expected key %q in wire format", key)
	}

	for key := range raw {
		for _, r := range key {
			assert.Less(t, r, unicode.MaxASCII, "key %q contains non-ASCII character", key)
		}
	}
}

func TestEncodePakeMessage_CoordinatesAreStrings(t *testing.T) {
	t.Parallel()
	client, err := pake.InitCurve([]byte("123456"), 0, "p256")
	require.NoError(t, err)

	wire, err := EncodePakeMessage(client.Bytes())
	require.NoError(t, err)

	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(wire, &raw))

	coordFields := []string{"ux", "uy", "vx", "vy", "xx", "xy", "yx", "yy"}
	for _, field := range coordFields {
		val := raw[field]
		assert.NotEmpty(t, val, "field %q should be present", field)
		assert.Equal(t, byte('"'), val[0],
			"field %q should be a quoted string, got: %s", field, string(val))
	}

	// role should be a number, not a string
	roleVal := raw["role"]
	assert.NotEqual(t, byte('"'), roleVal[0], "role should be a number")
}

func TestRoundTrip_EncodeDecodePreservesData(t *testing.T) {
	t.Parallel()
	client, err := pake.InitCurve([]byte("654321"), 0, "p256")
	require.NoError(t, err)

	internal := client.Bytes()

	wire, err := EncodePakeMessage(internal)
	require.NoError(t, err)

	roundTripped, err := DecodePakeMessage(wire)
	require.NoError(t, err)

	// Unmarshal both into generic maps to compare values
	var orig map[string]any
	var rt map[string]any
	require.NoError(t, json.Unmarshal(internal, &orig))
	require.NoError(t, json.Unmarshal(roundTripped, &rt))

	assert.InDelta(t, orig["Role"], rt["Role"], 0,
		"Role must survive round-trip")

	// The Unicode-keyed fields in orig should match their round-tripped values.
	// orig has bare numbers, rt has bare numbers (both are pake internal format).
	pairs := [][2]string{
		{"Uᵤ", "Uᵤ"},
		{"Uᵥ", "Uᵥ"},
		{"Vᵤ", "Vᵤ"},
		{"Vᵥ", "Vᵥ"},
		{"Xᵤ", "Xᵤ"},
		{"Xᵥ", "Xᵥ"},
		{"Yᵤ", "Yᵤ"},
		{"Yᵥ", "Yᵥ"},
	}
	for _, p := range pairs {
		// json.Unmarshal into any produces float64 for numbers, which loses
		// precision. Use json.Number via decoder instead.
		origN := jsonNumberFromBytes(t, internal, p[0])
		rtN := jsonNumberFromBytes(t, roundTripped, p[1])
		assert.Equal(t, origN, rtN, "field %q must survive round-trip", p[0])
	}
}

// jsonNumberFromBytes extracts a json.Number for the given key using
// json.Decoder with UseNumber to preserve arbitrary-precision integers.
func jsonNumberFromBytes(t *testing.T, data []byte, key string) string {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var m map[string]any
	require.NoError(t, dec.Decode(&m))
	v, ok := m[key]
	if !ok || v == nil {
		return "0"
	}
	n, ok := v.(json.Number)
	require.True(t, ok, "expected json.Number for key %q, got %T", key, v)
	return n.String()
}

func TestFullHandshake_ThroughWireFormat(t *testing.T) {
	t.Parallel()
	pin := []byte("999999")

	// Client (role 0) generates message A
	client, err := pake.InitCurve(pin, 0, "p256")
	require.NoError(t, err)

	msgAWire, err := EncodePakeMessage(client.Bytes())
	require.NoError(t, err)

	// Server (role 1) processes message A
	server, err := pake.InitCurve(pin, 1, "p256")
	require.NoError(t, err)

	msgAInternal, err := DecodePakeMessage(msgAWire)
	require.NoError(t, err)
	require.NoError(t, server.Update(msgAInternal))

	msgBWire, err := EncodePakeMessage(server.Bytes())
	require.NoError(t, err)

	// Client processes message B
	msgBInternal, err := DecodePakeMessage(msgBWire)
	require.NoError(t, err)
	require.NoError(t, client.Update(msgBInternal))

	// Both sides derive the same session key
	clientKey, err := client.SessionKey()
	require.NoError(t, err)
	serverKey, err := server.SessionKey()
	require.NoError(t, err)
	assert.Equal(t, clientKey, serverKey, "session keys must match through wire format")
}

func TestEncodePakeMessage_NilFieldsEncodeAsZero(t *testing.T) {
	t.Parallel()
	// Role 1 before Update() has nil X and Y fields
	server, err := pake.InitCurve([]byte("000000"), 1, "p256")
	require.NoError(t, err)

	wire, err := EncodePakeMessage(server.Bytes())
	require.NoError(t, err)

	var msg PakeMessage
	require.NoError(t, json.Unmarshal(wire, &msg))

	// X and Y should be "0" since role-1 hasn't computed them yet
	assert.Equal(t, "0", msg.XX)
	assert.Equal(t, "0", msg.XY)
	assert.Equal(t, "0", msg.YX)
	assert.Equal(t, "0", msg.YY)
	// U and V should be non-zero (curve base points)
	assert.NotEqual(t, "0", msg.UX)
	assert.NotEqual(t, "0", msg.UY)
}

func TestDecodePakeMessage_InvalidJSON(t *testing.T) {
	t.Parallel()
	_, err := DecodePakeMessage([]byte("not json"))
	assert.ErrorIs(t, err, ErrInvalidPakeMessage)
}

func TestDecodePakeMessage_InvalidCoordinate(t *testing.T) {
	t.Parallel()
	bad := `{"role":0,"ux":"not_a_number","uy":"0","vx":"0","vy":"0","xx":"0","xy":"0","yx":"0","yy":"0"}`
	_, err := DecodePakeMessage([]byte(bad))
	assert.ErrorIs(t, err, ErrInvalidPakeMessage)
}

func TestDecodePakeMessage_NegativeCoordinate(t *testing.T) {
	t.Parallel()
	neg := `{"role":0,"ux":"-42","uy":"0","vx":"0","vy":"0","xx":"0","xy":"0","yx":"0","yy":"0"}`
	_, err := DecodePakeMessage([]byte(neg))
	assert.ErrorIs(t, err, ErrInvalidPakeMessage)
}

func TestDecodePakeMessage_EmptyCoordinate(t *testing.T) {
	t.Parallel()
	empty := `{"role":0,"ux":"","uy":"0","vx":"0","vy":"0","xx":"0","xy":"0","yx":"0","yy":"0"}`
	_, err := DecodePakeMessage([]byte(empty))
	assert.ErrorIs(t, err, ErrInvalidPakeMessage)
}

func TestEncodePakeMessage_InvalidJSON(t *testing.T) {
	t.Parallel()
	_, err := EncodePakeMessage([]byte("not json"))
	assert.ErrorIs(t, err, ErrInvalidPakeMessage)
}
