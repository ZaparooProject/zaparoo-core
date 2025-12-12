// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
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

package models

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRPCID_UnmarshalJSON_String(t *testing.T) {
	t.Parallel()

	var id RPCID
	err := json.Unmarshal([]byte(`"my-string-id"`), &id)

	require.NoError(t, err)
	assert.Equal(t, `"my-string-id"`, id.String())
	assert.False(t, id.IsNull())
}

func TestRPCID_UnmarshalJSON_Number(t *testing.T) {
	t.Parallel()

	var id RPCID
	err := json.Unmarshal([]byte(`12345`), &id)

	require.NoError(t, err)
	assert.Equal(t, `12345`, id.String())
	assert.False(t, id.IsNull())
}

func TestRPCID_UnmarshalJSON_Null(t *testing.T) {
	t.Parallel()

	var id RPCID
	err := json.Unmarshal([]byte(`null`), &id)

	require.NoError(t, err)
	assert.Equal(t, `null`, id.String())
	assert.True(t, id.IsNull())
}

func TestRPCID_UnmarshalJSON_UUID(t *testing.T) {
	t.Parallel()

	testUUID := uuid.New().String()
	var id RPCID
	err := json.Unmarshal([]byte(`"`+testUUID+`"`), &id)

	require.NoError(t, err)
	assert.Equal(t, `"`+testUUID+`"`, id.String())
	assert.False(t, id.IsNull())
}

func TestRPCID_UnmarshalJSON_RejectsObject(t *testing.T) {
	t.Parallel()

	var id RPCID
	err := json.Unmarshal([]byte(`{"nested":"object"}`), &id)

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidRPCID)
}

func TestRPCID_UnmarshalJSON_RejectsArray(t *testing.T) {
	t.Parallel()

	var id RPCID
	err := json.Unmarshal([]byte(`[1, 2, 3]`), &id)

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidRPCID)
}

func TestRPCID_MarshalJSON_String(t *testing.T) {
	t.Parallel()

	id := NewStringID("test-id")
	data, err := json.Marshal(id)

	require.NoError(t, err)
	assert.Equal(t, `"test-id"`, string(data))
}

func TestRPCID_MarshalJSON_Number(t *testing.T) {
	t.Parallel()

	id := NewNumberID(42)
	data, err := json.Marshal(id)

	require.NoError(t, err)
	assert.Equal(t, `42`, string(data))
}

func TestRPCID_MarshalJSON_Null(t *testing.T) {
	t.Parallel()

	data, err := json.Marshal(NullRPCID)

	require.NoError(t, err)
	assert.Equal(t, `null`, string(data))
}

func TestRPCID_MarshalJSON_Empty(t *testing.T) {
	t.Parallel()

	var id RPCID
	data, err := json.Marshal(id)

	require.NoError(t, err)
	assert.Equal(t, `null`, string(data))
}

func TestRPCID_Equal(t *testing.T) {
	t.Parallel()

	id1 := NewStringID("test")
	id2 := NewStringID("test")
	id3 := NewStringID("other")
	id4 := NewNumberID(123)

	assert.True(t, id1.Equal(id2))
	assert.False(t, id1.Equal(id3))
	assert.False(t, id1.Equal(id4))
}

func TestRPCID_Equal_NilReceiver(t *testing.T) {
	t.Parallel()

	var nilID *RPCID
	emptyID := RPCID{}
	validID := NewStringID("test")

	// nil receiver comparing to empty should return true (both are "absent")
	assert.True(t, nilID.Equal(emptyID))
	// nil receiver comparing to valid ID should return false
	assert.False(t, nilID.Equal(validID))
}

func TestRPCID_Key(t *testing.T) {
	t.Parallel()

	stringID := NewStringID("test")
	numberID := NewNumberID(123)

	assert.Equal(t, `"test"`, stringID.Key())
	assert.Equal(t, `123`, numberID.Key())

	// Test that Key() can be used as map key
	m := make(map[string]bool)
	m[stringID.Key()] = true
	m[numberID.Key()] = true

	assert.True(t, m[stringID.Key()])
	assert.True(t, m[numberID.Key()])
}

func TestRPCID_Key_NilReceiver(t *testing.T) {
	t.Parallel()

	var nilID *RPCID
	// nil receiver should return empty string
	assert.Empty(t, nilID.Key())
}

func TestRPCID_String_NilReceiver(t *testing.T) {
	t.Parallel()

	var nilID *RPCID
	// nil receiver should return "null" for logging purposes
	assert.Equal(t, "null", nilID.String())
}

func TestRPCID_IsNull(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		id       RPCID
		expected bool
	}{
		{"NullRPCID", NullRPCID, true},
		{"empty RPCID", RPCID{}, false}, // Empty is absent, not null
		{"string ID", NewStringID("test"), false},
		{"number ID", NewNumberID(1), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, tt.id.IsNull())
		})
	}
}

func TestRPCID_IsAbsent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		id       RPCID
		expected bool
	}{
		{"NullRPCID", NullRPCID, false}, // Null is present, not absent
		{"empty RPCID", RPCID{}, true},
		{"string ID", NewStringID("test"), false},
		{"number ID", NewNumberID(1), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, tt.id.IsAbsent())
		})
	}
}

func TestRPCID_IsAbsentOrNull(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		id       RPCID
		expected bool
	}{
		{"NullRPCID", NullRPCID, true},
		{"empty RPCID", RPCID{}, true},
		{"string ID", NewStringID("test"), false},
		{"number ID", NewNumberID(1), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, tt.id.IsAbsentOrNull())
		})
	}
}

func TestRPCID_RoundTrip(t *testing.T) {
	t.Parallel()

	// Test that marshaling and unmarshaling preserves the exact value
	tests := []struct {
		name  string
		input string
	}{
		{"string", `"my-id"`},
		{"number", `12345`},
		{"negative number", `-42`},
		{"float", `3.14`},
		{"null", `null`},
		{"uuid", `"` + uuid.New().String() + `"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var id RPCID
			err := json.Unmarshal([]byte(tt.input), &id)
			require.NoError(t, err)

			output, err := json.Marshal(id)
			require.NoError(t, err)

			assert.Equal(t, tt.input, string(output))
		})
	}
}

// TestRPCID_InStruct tests RPCID behavior when embedded in a struct,
// which is how it will be used in RequestObject/ResponseObject.
// Using non-pointer RPCID allows distinguishing missing vs null via json.RawMessage.
func TestRPCID_InStruct(t *testing.T) {
	t.Parallel()

	type testStruct struct {
		ID RPCID `json:"id,omitempty"`
	}

	t.Run("missing ID field", func(t *testing.T) {
		t.Parallel()
		var s testStruct
		err := json.Unmarshal([]byte(`{}`), &s)
		require.NoError(t, err)
		assert.True(t, s.ID.IsAbsent(), "missing ID should be absent")
		assert.False(t, s.ID.IsNull(), "missing ID should not be null")
	})

	t.Run("null ID field", func(t *testing.T) {
		t.Parallel()
		var s testStruct
		err := json.Unmarshal([]byte(`{"id":null}`), &s)
		require.NoError(t, err)
		assert.False(t, s.ID.IsAbsent(), "explicit null ID should not be absent")
		assert.True(t, s.ID.IsNull(), "explicit null ID should be null")
	})

	t.Run("string ID field", func(t *testing.T) {
		t.Parallel()
		var s testStruct
		err := json.Unmarshal([]byte(`{"id":"test"}`), &s)
		require.NoError(t, err)
		assert.False(t, s.ID.IsAbsent())
		assert.False(t, s.ID.IsNull())
		assert.Equal(t, `"test"`, s.ID.String())
	})

	t.Run("number ID field", func(t *testing.T) {
		t.Parallel()
		var s testStruct
		err := json.Unmarshal([]byte(`{"id":123}`), &s)
		require.NoError(t, err)
		assert.False(t, s.ID.IsAbsent())
		assert.False(t, s.ID.IsNull())
		assert.Equal(t, `123`, s.ID.String())
	})
}
