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

package database

import (
	"database/sql"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestSchema_NullableFieldsUseCorrectTypes is a proactive regression test that ensures
// nullable database columns are represented with sql.Null* types in Go structs.
//
// This prevents bugs like the SecondarySlug issue where a nullable database column
// was mapped to a non-nullable Go string type, causing scan failures.
//
// Add any new nullable fields to the nullableFields map below.
func TestSchema_NullableFieldsUseCorrectTypes(t *testing.T) {
	t.Parallel()

	// Map of struct type -> field name -> expected Go type
	type fieldSpec struct {
		structType reflect.Type
		fieldName  string
		expectType string
	}

	nullableFields := []fieldSpec{
		{
			structType: reflect.TypeOf(MediaTitle{}),
			fieldName:  "SecondarySlug",
			expectType: "sql.NullString",
		},
		// Add future nullable fields here as they're added to the schema
	}

	for _, spec := range nullableFields {
		t.Run(spec.structType.Name()+"."+spec.fieldName, func(t *testing.T) {
			field, found := spec.structType.FieldByName(spec.fieldName)
			assert.True(t, found, "Field %s.%s not found in struct",
				spec.structType.Name(), spec.fieldName)

			actualType := field.Type.String()

			assert.Equal(t, spec.expectType, actualType,
				"Field %s.%s should be %s to match nullable database column, but is %s. "+
					"Scanning NULL values will fail!",
				spec.structType.Name(), spec.fieldName, spec.expectType, actualType)
		})
	}
}

// TestSchema_SqlNullStringIsComparable verifies that sql.NullString works correctly
// in our use cases (can be assigned, compared, etc.)
func TestSchema_SqlNullStringIsComparable(t *testing.T) {
	t.Parallel()

	// Test NULL value
	nullValue := sql.NullString{Valid: false}
	assert.False(t, nullValue.Valid)
	assert.Empty(t, nullValue.String)

	// Test non-NULL value
	nonNull := sql.NullString{String: "test", Valid: true}
	assert.True(t, nonNull.Valid)
	assert.Equal(t, "test", nonNull.String)

	// Test assignment
	var mediaTitle MediaTitle
	mediaTitle.SecondarySlug = sql.NullString{String: "subtitle", Valid: true}
	assert.True(t, mediaTitle.SecondarySlug.Valid)

	mediaTitle.SecondarySlug = sql.NullString{Valid: false}
	assert.False(t, mediaTitle.SecondarySlug.Valid)

	// Test empty string vs NULL distinction
	emptyString := sql.NullString{String: "", Valid: true}
	assert.True(t, emptyString.Valid, "empty string is different from NULL")
	assert.NotEqual(t, nullValue, emptyString, "NULL and empty string should be different")
}
