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

package methods

import (
	"testing"

	"github.com/ZaparooProject/zaparoo-core/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/pkg/database/userdb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateAddMappingParams(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		errorMsg  string
		params    models.AddMappingParams
		wantError bool
	}{
		{
			name: "valid complete params - exact match",
			params: models.AddMappingParams{
				Label:    "test mapping",
				Type:     userdb.MappingTypeID,
				Match:    userdb.MatchTypeExact,
				Pattern:  "test_pattern",
				Override: "test_override",
				Enabled:  true,
			},
			wantError: false,
		},
		{
			name: "valid complete params - partial match",
			params: models.AddMappingParams{
				Label:    "test mapping",
				Type:     userdb.MappingTypeValue,
				Match:    userdb.MatchTypePartial,
				Pattern:  "test_pattern",
				Override: "test_override",
				Enabled:  true,
			},
			wantError: false,
		},
		{
			name: "valid complete params - regex match",
			params: models.AddMappingParams{
				Label:    "test mapping",
				Type:     userdb.MappingTypeData,
				Match:    userdb.MatchTypeRegex,
				Pattern:  "^test.*pattern$",
				Override: "test_override",
				Enabled:  false,
			},
			wantError: false,
		},
		{
			name: "valid regex pattern - complex",
			params: models.AddMappingParams{
				Label:    "complex regex",
				Type:     userdb.MappingTypeID,
				Match:    userdb.MatchTypeRegex,
				Pattern:  "^[a-zA-Z0-9]+(\\.[a-zA-Z0-9]+)*$",
				Override: "test",
				Enabled:  true,
			},
			wantError: false,
		},
		{
			name: "invalid mapping type",
			params: models.AddMappingParams{
				Label:    "test mapping",
				Type:     "invalid_type",
				Match:    userdb.MatchTypeExact,
				Pattern:  "test_pattern",
				Override: "test_override",
				Enabled:  true,
			},
			wantError: true,
			errorMsg:  "invalid type",
		},
		{
			name: "invalid match type",
			params: models.AddMappingParams{
				Label:    "test mapping",
				Type:     userdb.MappingTypeID,
				Match:    "invalid_match",
				Pattern:  "test_pattern",
				Override: "test_override",
				Enabled:  true,
			},
			wantError: true,
			errorMsg:  "invalid match",
		},
		{
			name: "empty pattern",
			params: models.AddMappingParams{
				Label:    "test mapping",
				Type:     userdb.MappingTypeID,
				Match:    userdb.MatchTypeExact,
				Pattern:  "",
				Override: "test_override",
				Enabled:  true,
			},
			wantError: true,
			errorMsg:  "missing pattern",
		},
		{
			name: "invalid regex pattern - unclosed bracket",
			params: models.AddMappingParams{
				Label:    "test mapping",
				Type:     userdb.MappingTypeID,
				Match:    userdb.MatchTypeRegex,
				Pattern:  "[abc",
				Override: "test_override",
				Enabled:  true,
			},
			wantError: true,
			errorMsg:  "failed to compile regex pattern",
		},
		{
			name: "invalid regex pattern - unclosed parenthesis",
			params: models.AddMappingParams{
				Label:    "test mapping",
				Type:     userdb.MappingTypeValue,
				Match:    userdb.MatchTypeRegex,
				Pattern:  "(test",
				Override: "test_override",
				Enabled:  true,
			},
			wantError: true,
			errorMsg:  "failed to compile regex pattern",
		},
		{
			name: "invalid regex pattern - invalid repetition",
			params: models.AddMappingParams{
				Label:    "test mapping",
				Type:     userdb.MappingTypeData,
				Match:    userdb.MatchTypeRegex,
				Pattern:  "*invalid",
				Override: "test_override",
				Enabled:  true,
			},
			wantError: true,
			errorMsg:  "failed to compile regex pattern",
		},
		{
			name: "valid empty override",
			params: models.AddMappingParams{
				Label:    "test mapping",
				Type:     userdb.MappingTypeID,
				Match:    userdb.MatchTypeExact,
				Pattern:  "test_pattern",
				Override: "",
				Enabled:  true,
			},
			wantError: false,
		},
		{
			name: "valid special characters in pattern - non-regex",
			params: models.AddMappingParams{
				Label:    "special chars",
				Type:     userdb.MappingTypeValue,
				Match:    userdb.MatchTypeExact,
				Pattern:  "test[bracket](paren){brace}.dot*star+plus?question",
				Override: "test",
				Enabled:  true,
			},
			wantError: false,
		},
		{
			name: "valid unicode pattern - non-regex",
			params: models.AddMappingParams{
				Label:    "unicode test",
				Type:     userdb.MappingTypeID,
				Match:    userdb.MatchTypePartial,
				Pattern:  "测试日本語🎮", //nolint:gosmopolitan // Intentional unicode test
				Override: "test",
				Enabled:  true,
			},
			wantError: false,
		},
		{
			name: "valid unicode regex pattern",
			params: models.AddMappingParams{
				Label:    "unicode regex",
				Type:     userdb.MappingTypeData,
				Match:    userdb.MatchTypeRegex,
				Pattern:  "^[测试日本語🎮]+$", //nolint:gosmopolitan // Intentional unicode test
				Override: "test",
				Enabled:  true,
			},
			wantError: false,
		},
		{
			name: "boundary test - very long pattern",
			params: models.AddMappingParams{
				Label:    "long pattern",
				Type:     userdb.MappingTypeValue,
				Match:    userdb.MatchTypeExact,
				Pattern:  "a" + string(make([]byte, 1000)),
				Override: "test",
				Enabled:  true,
			},
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := validateAddMappingParams(&tt.params)

			if tt.wantError {
				require.Error(t, err, "Expected error for test case: %s", tt.name)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg, "Error message should contain expected text")
				}
			} else {
				assert.NoError(t, err, "Expected no error for test case: %s", tt.name)
			}
		})
	}
}

func TestValidateUpdateMappingParams(t *testing.T) {
	t.Parallel()

	// Helper function to create string pointers
	strPtr := func(s string) *string { return &s }
	boolPtr := func(b bool) *bool { return &b }

	tests := []struct {
		params    models.UpdateMappingParams
		name      string
		errorMsg  string
		wantError bool
	}{
		{
			name: "valid partial update - label only",
			params: models.UpdateMappingParams{
				ID:    1,
				Label: strPtr("updated label"),
			},
			wantError: false,
		},
		{
			name: "valid partial update - enabled only",
			params: models.UpdateMappingParams{
				ID:      1,
				Enabled: boolPtr(false),
			},
			wantError: false,
		},
		{
			name: "valid partial update - type only",
			params: models.UpdateMappingParams{
				ID:   1,
				Type: strPtr(userdb.MappingTypeValue),
			},
			wantError: false,
		},
		{
			name: "valid partial update - match only",
			params: models.UpdateMappingParams{
				ID:    1,
				Match: strPtr(userdb.MatchTypePartial),
			},
			wantError: false,
		},
		{
			name: "valid partial update - pattern only",
			params: models.UpdateMappingParams{
				ID:      1,
				Pattern: strPtr("new_pattern"),
			},
			wantError: false,
		},
		{
			name: "valid partial update - override only",
			params: models.UpdateMappingParams{
				ID:       1,
				Override: strPtr("new_override"),
			},
			wantError: false,
		},
		{
			name: "valid complete update",
			params: models.UpdateMappingParams{
				ID:       1,
				Label:    strPtr("updated mapping"),
				Enabled:  boolPtr(true),
				Type:     strPtr(userdb.MappingTypeData),
				Match:    strPtr(userdb.MatchTypeRegex),
				Pattern:  strPtr("^test.*$"),
				Override: strPtr("updated_override"),
			},
			wantError: false,
		},
		{
			name: "valid regex pattern update",
			params: models.UpdateMappingParams{
				ID:      1,
				Match:   strPtr(userdb.MatchTypeRegex),
				Pattern: strPtr("^[a-zA-Z0-9]+\\.(jpg|png|gif)$"),
			},
			wantError: false,
		},
		{
			name: "all fields nil",
			params: models.UpdateMappingParams{
				ID: 1,
			},
			wantError: true,
			errorMsg:  "missing fields",
		},
		{
			name: "invalid type",
			params: models.UpdateMappingParams{
				ID:   1,
				Type: strPtr("invalid_type"),
			},
			wantError: true,
			errorMsg:  "invalid type",
		},
		{
			name: "invalid match type",
			params: models.UpdateMappingParams{
				ID:    1,
				Match: strPtr("invalid_match"),
			},
			wantError: true,
			errorMsg:  "invalid match",
		},
		{
			name: "empty pattern",
			params: models.UpdateMappingParams{
				ID:      1,
				Pattern: strPtr(""),
			},
			wantError: true,
			errorMsg:  "missing pattern",
		},
		{
			name: "invalid regex pattern with regex match",
			params: models.UpdateMappingParams{
				ID:      1,
				Match:   strPtr(userdb.MatchTypeRegex),
				Pattern: strPtr("[abc"),
			},
			wantError: true,
			errorMsg:  "failed to compile regex pattern",
		},
		{
			name: "invalid regex pattern - unclosed parenthesis",
			params: models.UpdateMappingParams{
				ID:      1,
				Match:   strPtr(userdb.MatchTypeRegex),
				Pattern: strPtr("(test"),
			},
			wantError: true,
			errorMsg:  "failed to compile regex pattern",
		},
		{
			name: "invalid regex pattern - invalid repetition",
			params: models.UpdateMappingParams{
				ID:      1,
				Match:   strPtr(userdb.MatchTypeRegex),
				Pattern: strPtr("*invalid"),
			},
			wantError: true,
			errorMsg:  "failed to compile regex pattern",
		},
		{
			name: "valid empty override update",
			params: models.UpdateMappingParams{
				ID:       1,
				Override: strPtr(""),
			},
			wantError: false,
		},
		{
			name: "valid special characters in pattern - non-regex match",
			params: models.UpdateMappingParams{
				ID:      1,
				Match:   strPtr(userdb.MatchTypeExact),
				Pattern: strPtr("test[bracket](paren){brace}.dot*star+plus?question"),
			},
			wantError: false,
		},
		{
			name: "mixed valid and invalid - should fail on invalid",
			params: models.UpdateMappingParams{
				ID:      1,
				Label:   strPtr("valid label"),
				Type:    strPtr("invalid_type"),
				Pattern: strPtr("valid_pattern"),
			},
			wantError: true,
			errorMsg:  "invalid type",
		},
		{
			name: "valid unicode pattern update",
			params: models.UpdateMappingParams{
				ID:      1,
				Pattern: strPtr("测试日本語🎮"), //nolint:gosmopolitan // Intentional unicode test
			},
			wantError: false,
		},
		{
			name: "valid unicode regex pattern update",
			params: models.UpdateMappingParams{
				ID:      1,
				Match:   strPtr(userdb.MatchTypeRegex),
				Pattern: strPtr("^[测试日本語🎮]+$"), //nolint:gosmopolitan // Intentional unicode test
			},
			wantError: false,
		},
		{
			name: "regex match with nil pattern - validates security fix",
			params: models.UpdateMappingParams{
				ID:    1,
				Match: strPtr(userdb.MatchTypeRegex),
				// Pattern is intentionally nil
			},
			wantError: true,
			errorMsg:  "pattern is required for regex match",
		},
		// Additional regression tests for the security fix
		{
			name: "regex match with nil pattern and other fields - validates security fix",
			params: models.UpdateMappingParams{
				ID:      1,
				Label:   strPtr("test"),
				Match:   strPtr(userdb.MatchTypeRegex),
				Enabled: boolPtr(true),
				// Pattern is intentionally nil
			},
			wantError: true,
			errorMsg:  "pattern is required for regex match",
		},
		{
			name: "non-regex match with nil pattern - should not error",
			params: models.UpdateMappingParams{
				ID:    1,
				Match: strPtr(userdb.MatchTypeExact),
				// Pattern is intentionally nil - this should be allowed for non-regex
			},
			wantError: false,
		},
		{
			name: "partial match with nil pattern - should not error",
			params: models.UpdateMappingParams{
				ID:    1,
				Match: strPtr(userdb.MatchTypePartial),
				// Pattern is intentionally nil - this should be allowed for non-regex
			},
			wantError: false,
		},
		{
			name: "regex match with valid pattern - should work",
			params: models.UpdateMappingParams{
				ID:      1,
				Match:   strPtr(userdb.MatchTypeRegex),
				Pattern: strPtr("^valid.*regex$"),
			},
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := validateUpdateMappingParams(&tt.params)

			if tt.wantError {
				require.Error(t, err, "Expected error for test case: %s", tt.name)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg, "Error message should contain expected text")
				}
			} else {
				assert.NoError(t, err, "Expected no error for test case: %s", tt.name)
			}
		})
	}
}
