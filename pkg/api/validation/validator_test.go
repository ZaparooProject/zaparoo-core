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

//nolint:revive // custom validation tags (letter, duration, etc.) are unknown to revive
package validation

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateOneof(t *testing.T) {
	t.Parallel()

	type testStruct struct {
		Type string `validate:"oneof=id value data uid text"`
		Mode string `validate:"oneof=tap hold"`
	}

	tests := []struct {
		name      string
		typeVal   string
		modeVal   string
		wantError bool
	}{
		{name: "valid type id", typeVal: "id", modeVal: "tap", wantError: false},
		{name: "valid type value", typeVal: "value", modeVal: "hold", wantError: false},
		{name: "valid legacy uid", typeVal: "uid", modeVal: "tap", wantError: false},
		{name: "invalid type", typeVal: "invalid", modeVal: "tap", wantError: true},
		{name: "invalid mode", typeVal: "id", modeVal: "swipe", wantError: true},
		{name: "wrong case type", typeVal: "ID", modeVal: "tap", wantError: true},
	}

	v := NewValidator()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := testStruct{Type: tt.typeVal, Mode: tt.modeVal}
			err := v.Validate(&s)
			if tt.wantError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "must be one of")
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateLetter(t *testing.T) {
	t.Parallel()

	type testStruct struct {
		Letter string `validate:"letter"`
	}

	tests := []struct {
		name      string
		value     string
		wantError bool
	}{
		{name: "empty is valid", value: "", wantError: false},
		{name: "uppercase letter", value: "A", wantError: false},
		{name: "lowercase letter", value: "z", wantError: false},
		{name: "middle letter", value: "M", wantError: false},
		{name: "hash symbol", value: "#", wantError: false},
		{name: "numeric range", value: "0-9", wantError: false},
		{name: "lowercase numeric range", value: "0-9", wantError: false},
		{name: "multiple letters invalid", value: "AB", wantError: true},
		{name: "number invalid", value: "5", wantError: true},
		{name: "special char invalid", value: "@", wantError: true},
	}

	v := NewValidator()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := testStruct{Letter: tt.value}
			err := v.Validate(&s)
			if tt.wantError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "letter must be A-Z, 0-9, or #")
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateDuration(t *testing.T) {
	t.Parallel()

	type testStruct struct {
		Duration string `validate:"duration"`
	}

	tests := []struct {
		name      string
		value     string
		wantError bool
	}{
		{name: "empty is valid", value: "", wantError: false},
		{name: "hours", value: "1h", wantError: false},
		{name: "minutes", value: "30m", wantError: false},
		{name: "seconds", value: "45s", wantError: false},
		{name: "combined", value: "1h30m45s", wantError: false},
		{name: "milliseconds", value: "100ms", wantError: false},
		{name: "negative duration", value: "-5m", wantError: false},
		{name: "invalid format", value: "1hour", wantError: true},
		{name: "just number", value: "100", wantError: true},
		{name: "invalid string", value: "hello", wantError: true},
	}

	v := NewValidator()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := testStruct{Duration: tt.value}
			err := v.Validate(&s)
			if tt.wantError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "duration must be a valid duration")
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateRegex(t *testing.T) {
	t.Parallel()

	type testStruct struct {
		Pattern string `validate:"regex"`
	}

	tests := []struct {
		name      string
		value     string
		wantError bool
	}{
		{name: "empty is valid", value: "", wantError: false},
		{name: "simple pattern", value: "abc", wantError: false},
		{name: "wildcard pattern", value: ".*", wantError: false},
		{name: "complex pattern", value: "^[a-zA-Z0-9]+$", wantError: false},
		{name: "groups pattern", value: "(abc|def)", wantError: false},
		{name: "unclosed bracket", value: "[abc", wantError: true},
		{name: "unclosed paren", value: "(abc", wantError: true},
		{name: "invalid repetition", value: "*invalid", wantError: true},
	}

	v := NewValidator()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := testStruct{Pattern: tt.value}
			err := v.Validate(&s)
			if tt.wantError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "pattern must be a valid regex pattern")
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateSystem(t *testing.T) {
	t.Parallel()

	type testStruct struct {
		System string `validate:"system"`
	}

	tests := []struct {
		name      string
		value     string
		wantError bool
	}{
		{name: "empty is valid", value: "", wantError: false},
		{name: "valid system NES", value: "NES", wantError: false},
		{name: "valid system SNES", value: "SNES", wantError: false},
		{name: "valid system Genesis", value: "Genesis", wantError: false},
		{name: "invalid system", value: "NotASystem", wantError: true},
	}

	v := NewValidator()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := testStruct{System: tt.value}
			err := v.Validate(&s)
			if tt.wantError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "not found")
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateHexadecimal(t *testing.T) {
	t.Parallel()

	type testStruct struct {
		Hex string `validate:"omitempty,hexadecimal"`
	}

	tests := []struct {
		name      string
		value     string
		wantError bool
	}{
		{name: "empty is valid", value: "", wantError: false},
		{name: "valid lowercase hex", value: "deadbeef", wantError: false},
		{name: "valid uppercase hex", value: "DEADBEEF", wantError: false},
		{name: "valid mixed case", value: "DeAdBeEf", wantError: false},
		{name: "valid short", value: "ff", wantError: false},
		{name: "odd length valid for hexadecimal", value: "abc", wantError: false},
		{name: "invalid chars", value: "gggg", wantError: true},
		{name: "mixed invalid", value: "abcg", wantError: true},
	}

	v := NewValidator()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := testStruct{Hex: tt.value}
			err := v.Validate(&s)
			if tt.wantError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "hex must be a valid hexadecimal string")
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateHexData(t *testing.T) {
	t.Parallel()

	type testStruct struct {
		Data string `validate:"omitempty,hexdata"`
	}

	tests := []struct {
		name      string
		value     string
		wantError bool
	}{
		{name: "empty is valid", value: "", wantError: false},
		{name: "valid lowercase hex", value: "deadbeef", wantError: false},
		{name: "valid uppercase hex", value: "DEADBEEF", wantError: false},
		{name: "valid mixed case", value: "DeAdBeEf", wantError: false},
		{name: "valid short", value: "ff", wantError: false},
		{name: "valid with spaces", value: "AA BB CC", wantError: false},
		{name: "valid with spaces lowercase", value: "aa bb cc", wantError: false},
		{name: "valid single byte with spaces", value: "AA", wantError: false},
		{name: "valid many spaces", value: "AA  BB  CC", wantError: false},
		{name: "valid no spaces", value: "AABBCC", wantError: false},
		{name: "odd length invalid (incomplete byte)", value: "abc", wantError: true},
		{name: "odd length with spaces invalid", value: "AA BB C", wantError: true},
		{name: "invalid chars", value: "gggg", wantError: true},
		{name: "mixed invalid", value: "abcg", wantError: true},
		{name: "only spaces invalid", value: "   ", wantError: true},
	}

	v := NewValidator()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := testStruct{Data: tt.value}
			err := v.Validate(&s)
			if tt.wantError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "data must be valid hex data")
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateLauncher(t *testing.T) {
	t.Parallel()

	type testStruct struct {
		Launcher string `validate:"launcher"`
	}

	tests := []struct {
		name        string
		value       string
		launcherIDs []string
		wantError   bool
	}{
		{name: "empty is valid", value: "", launcherIDs: nil, wantError: false},
		{name: "no context skips validation", value: "anything", launcherIDs: nil, wantError: false},
		{name: "valid launcher in list", value: "steam", launcherIDs: []string{"steam", "retroarch"}, wantError: false},
		{name: "case insensitive match", value: "STEAM", launcherIDs: []string{"steam"}, wantError: false},
		{name: "invalid launcher not in list", value: "unknown", launcherIDs: []string{"steam"}, wantError: true},
	}

	v := NewValidator()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := testStruct{Launcher: tt.value}

			var vctx *Context
			if tt.launcherIDs != nil {
				vctx = NewContext(tt.launcherIDs)
			}

			err := v.ValidateCtx(context.Background(), &s, vctx)
			if tt.wantError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "not found")
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateAndUnmarshal(t *testing.T) {
	t.Parallel()

	type testParams struct {
		Name  string `json:"name" validate:"required"`
		Type  string `json:"type" validate:"oneof=id value data uid text"`
		Count int    `json:"count" validate:"gt=0"`
	}

	tests := []struct {
		wantError error
		name      string
		errorMsg  string
		input     json.RawMessage
	}{
		{
			name:      "empty params returns ErrMissingParams",
			input:     nil,
			wantError: ErrMissingParams,
		},
		{
			name:      "empty array returns ErrMissingParams",
			input:     json.RawMessage{},
			wantError: ErrMissingParams,
		},
		{
			name:      "invalid JSON returns ErrInvalidParams",
			input:     json.RawMessage(`{invalid}`),
			wantError: ErrInvalidParams,
		},
		{
			name:  "valid params pass validation",
			input: json.RawMessage(`{"name": "test", "type": "id", "count": 5}`),
		},
		{
			name:     "missing required field",
			input:    json.RawMessage(`{"type": "id", "count": 5}`),
			errorMsg: "name is required",
		},
		{
			name:     "invalid enum value",
			input:    json.RawMessage(`{"name": "test", "type": "bad", "count": 5}`),
			errorMsg: "type must be one of",
		},
		{
			name:     "invalid number value",
			input:    json.RawMessage(`{"name": "test", "type": "id", "count": 0}`),
			errorMsg: "count must be greater than",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var params testParams
			err := ValidateAndUnmarshal(tt.input, &params)

			switch {
			case tt.wantError != nil:
				require.ErrorIs(t, err, tt.wantError)
			case tt.errorMsg != "":
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
			default:
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateRegexPattern(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		pattern   string
		wantError bool
	}{
		{name: "empty pattern is valid", pattern: "", wantError: false},
		{name: "simple pattern", pattern: "abc", wantError: false},
		{name: "complex pattern", pattern: "^[a-zA-Z0-9]+\\.(jpg|png)$", wantError: false},
		{name: "invalid bracket", pattern: "[abc", wantError: true},
		{name: "invalid paren", pattern: "(abc", wantError: true},
		{name: "invalid repetition", pattern: "*invalid", wantError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateRegexPattern(tt.pattern)
			if tt.wantError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "invalid regex pattern")
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestErrorFormatting(t *testing.T) {
	t.Parallel()

	type testStruct struct {
		Type  string `validate:"required,oneof=id value data uid text"`
		Match string `validate:"required,oneof=exact partial regex"`
	}

	v := NewValidator()
	s := testStruct{Type: "", Match: ""}
	err := v.Validate(&s)

	require.Error(t, err)

	// Error should contain both field errors
	errStr := err.Error()
	assert.Contains(t, errStr, "type is required")
	assert.Contains(t, errStr, "match is required")

	// Should be a validation.Error type
	var valErr *Error
	require.ErrorAs(t, err, &valErr)
	assert.Len(t, valErr.Fields, 2)
}

func TestErrorFormattingAllCases(t *testing.T) {
	t.Parallel()

	v := NewValidator()

	tests := []struct {
		name       string
		structDef  any
		wantSubstr string
	}{
		{
			name: "lt validation",
			structDef: &struct {
				Value int `validate:"lt=10"`
			}{Value: 15},
			wantSubstr: "must be less than 10",
		},
		{
			name: "lte validation",
			structDef: &struct {
				Value int `validate:"lte=10"`
			}{Value: 15},
			wantSubstr: "must be less than or equal to 10",
		},
		{
			name: "gte validation",
			structDef: &struct {
				Value int `validate:"gte=10"`
			}{Value: 5},
			wantSubstr: "must be greater than or equal to 10",
		},
		{
			name: "max validation",
			structDef: &struct {
				Value string `validate:"max=5"`
			}{Value: "toolong"},
			wantSubstr: "must be at most 5",
		},
		{
			name: "unknown tag falls back to default",
			structDef: &struct {
				Value string `validate:"alphanum"`
			}{Value: "test!@#"},
			wantSubstr: "failed alphanum validation",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := v.Validate(tt.structDef)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantSubstr)
		})
	}
}

func TestErrorEmptyFields(t *testing.T) {
	t.Parallel()

	// Test Error.Error() with empty fields
	err := &Error{Fields: []FieldError{}}
	assert.Equal(t, "validation failed", err.Error())
}
