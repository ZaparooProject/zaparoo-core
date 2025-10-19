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

package tags

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestTagMappingsReferenceValidCanonicalTags validates that all canonical tags
// referenced in allTagMappings actually exist in CanonicalTagDefinitions.
func TestTagMappingsReferenceValidCanonicalTags(t *testing.T) {
	// Build a set of all valid canonical tags from CanonicalTagDefinitions
	validTags := make(map[string]bool)
	for tagType, values := range CanonicalTagDefinitions {
		for _, value := range values {
			// Build the full canonical tag string
			canonicalTag := CanonicalTag{Type: tagType, Value: value}
			validTags[canonicalTag.String()] = true
		}
	}

	// Track all invalid references
	var invalidRefs []string

	// Check every canonical tag in allTagMappings
	for filenameTag, canonicalTags := range allTagMappings {
		for _, ct := range canonicalTags {
			fullTag := ct.String()

			// Special case: MAME parent tags are dynamic (ROM names), so skip validation
			if ct.Type == TagTypeMameParent {
				continue
			}

			// Check if this canonical tag exists in the definitions
			if !validTags[fullTag] {
				invalidRefs = append(invalidRefs, fmt.Sprintf(
					"filename tag '%s' → canonical tag '%s' (type=%s, value=%s) not found in CanonicalTagDefinitions",
					filenameTag, fullTag, ct.Type, ct.Value,
				))
			}
		}
	}

	// Assert no invalid references were found
	if len(invalidRefs) > 0 {
		t.Errorf("Found %d invalid canonical tag references in allTagMappings:\n", len(invalidRefs))
		for _, ref := range invalidRefs {
			t.Errorf("  - %s\n", ref)
		}
	}

	assert.Empty(t, invalidRefs, "All canonical tags in allTagMappings must exist in CanonicalTagDefinitions")
}

// TestAllTagMappingsValid checks that CanonicalTagDefinitions
// includes all tag types and that no tag type is empty.
func TestAllTagMappingsValid(t *testing.T) {
	// All tag types that should have definitions (excluding dynamic types)
	requiredTypes := []TagType{
		TagTypeInput,
		TagTypePlayers,
		TagTypeGameGenre,
		TagTypeAddon,
		TagTypeEmbedded,
		TagTypeSave,
		TagTypeArcadeBoard,
		TagTypeCompatibility,
		TagTypeDisc,
		TagTypeDiscTotal,
		TagTypeBased,
		TagTypeSearch,
		TagTypeMultigame,
		TagTypeReboxed,
		TagTypePort,
		TagTypeLang,
		TagTypeUnfinished,
		TagTypeRerelease,
		TagTypeRev,
		TagTypeSet,
		TagTypeAlt,
		TagTypeUnlicensed,
		TagTypeRegion,
		TagTypeYear,
		TagTypeVideo,
		TagTypeCopyright,
		TagTypeDump,
		TagTypeMedia,
		TagTypeExtension,
	}

	for _, tagType := range requiredTypes {
		values, exists := CanonicalTagDefinitions[tagType]
		assert.True(t, exists, "CanonicalTagDefinitions must include tag type: %s", tagType)
		// Extension can be empty (it's dynamically populated)
		if tagType != TagTypeExtension {
			assert.NotEmpty(t, values, "Tag type %s must have at least one value", tagType)
		}
	}

	// TagTypeMameParent should be in CanonicalTagDefinitions but may be empty (values are dynamic ROM names)
	_, exists := CanonicalTagDefinitions[TagTypeMameParent]
	assert.True(t, exists, "TagTypeMameParent should be in CanonicalTagDefinitions (even if empty)")
}

// TestCanonicalTagStringFormat validates that CanonicalTag.String() works correctly.
func TestCanonicalTagStringFormat(t *testing.T) {
	tests := []struct {
		name     string
		expected string
		tag      CanonicalTag
	}{
		{
			name:     "flat tag with no value",
			tag:      CanonicalTag{Type: TagTypeGameGenre, Value: "", Source: TagSourceBracketed},
			expected: "gamegenre",
		},
		{
			name:     "hierarchical tag with value",
			tag:      CanonicalTag{Type: TagTypeGameGenre, Value: "action:platformer", Source: TagSourceBracketed},
			expected: "gamegenre:action:platformer",
		},
		{
			name:     "simple tag with value",
			tag:      CanonicalTag{Type: TagTypeRegion, Value: "us", Source: TagSourceBracketed},
			expected: "region:us",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.tag.String()
			assert.Equal(t, tt.expected, result)
		})
	}
}
