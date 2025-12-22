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
	"strings"
	"testing"
	"unicode"

	"pgregory.net/rapid"
)

// tagInputGen generates realistic tag input strings.
func tagInputGen() *rapid.Generator[string] {
	// Characters commonly found in tag strings
	chars := []rune(
		"ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789" +
			" -_.:,+()[]{}",
	)
	return rapid.StringOfN(rapid.SampledFrom(chars), 0, 50, -1)
}

// ============================================================================
// NormalizeTag Property Tests
// ============================================================================

// TestPropertyNormalizeTagIdempotent verifies NormalizeTag is idempotent.
// Applying it twice should give the same result as applying it once.
func TestPropertyNormalizeTagIdempotent(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		input := tagInputGen().Draw(t, "input")
		once := NormalizeTag(input)
		twice := NormalizeTag(once)

		if once != twice {
			t.Fatalf("NormalizeTag is not idempotent: %q → %q → %q",
				input, once, twice)
		}
	})
}

// TestPropertyNormalizeTagDeterministic verifies same input always produces same output.
func TestPropertyNormalizeTagDeterministic(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		input := tagInputGen().Draw(t, "input")
		result1 := NormalizeTag(input)
		result2 := NormalizeTag(input)

		if result1 != result2 {
			t.Fatalf("NormalizeTag is not deterministic: %q → %q vs %q",
				input, result1, result2)
		}
	})
}

// TestPropertyNormalizeTagLowercase verifies output is always lowercase.
func TestPropertyNormalizeTagLowercase(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		input := tagInputGen().Draw(t, "input")
		result := NormalizeTag(input)

		for _, r := range result {
			if unicode.IsUpper(r) {
				t.Fatalf("NormalizeTag produced uppercase character: %q → %q",
					input, result)
			}
		}
	})
}

// TestPropertyNormalizeTagNoLeadingTrailingWhitespace verifies no leading/trailing whitespace.
func TestPropertyNormalizeTagNoLeadingTrailingWhitespace(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		input := tagInputGen().Draw(t, "input")
		result := NormalizeTag(input)

		if result != strings.TrimSpace(result) {
			t.Fatalf("NormalizeTag has leading/trailing whitespace: %q → %q",
				input, result)
		}
	})
}

// TestPropertyNormalizeTagNoSpacesAroundColons verifies colons have no surrounding spaces.
func TestPropertyNormalizeTagNoSpacesAroundColons(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		input := tagInputGen().Draw(t, "input")
		result := NormalizeTag(input)

		if strings.Contains(result, " :") || strings.Contains(result, ": ") {
			t.Fatalf("NormalizeTag has spaces around colons: %q → %q",
				input, result)
		}
	})
}

// TestPropertyNormalizeTagNoSpaces verifies spaces are converted to dashes.
func TestPropertyNormalizeTagNoSpaces(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		input := tagInputGen().Draw(t, "input")
		result := NormalizeTag(input)

		if strings.Contains(result, " ") {
			t.Fatalf("NormalizeTag still contains spaces: %q → %q",
				input, result)
		}
	})
}

// TestPropertyNormalizeTagNoPeriods verifies periods are converted to dashes.
func TestPropertyNormalizeTagNoPeriods(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		input := tagInputGen().Draw(t, "input")
		result := NormalizeTag(input)

		if strings.Contains(result, ".") {
			t.Fatalf("NormalizeTag still contains periods: %q → %q",
				input, result)
		}
	})
}

// TestPropertyNormalizeTagOnlyValidChars verifies output contains only allowed characters.
func TestPropertyNormalizeTagOnlyValidChars(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		input := tagInputGen().Draw(t, "input")
		result := NormalizeTag(input)

		for _, r := range result {
			// Allowed: a-z, 0-9, colon, dash, comma, plus
			isLower := r >= 'a' && r <= 'z'
			isDigit := r >= '0' && r <= '9'
			isAllowed := r == ':' || r == '-' || r == ',' || r == '+'

			if !isLower && !isDigit && !isAllowed {
				t.Fatalf("NormalizeTag produced invalid character %q in: %q → %q",
					string(r), input, result)
			}
		}
	})
}

// TestPropertyNormalizeTagNeverPanics verifies NormalizeTag never panics.
func TestPropertyNormalizeTagNeverPanics(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		// Generate any string including edge cases
		input := rapid.String().Draw(t, "input")

		// Should not panic
		_ = NormalizeTag(input)
	})
}

// ============================================================================
// Canonical Tag Mapping Property Tests
// ============================================================================

// TestPropertyAllMappingsHaveValidTypes verifies all mapped tags have valid types.
func TestPropertyAllMappingsHaveValidTypes(t *testing.T) {
	t.Parallel()

	// Build set of valid types from CanonicalTagDefinitions
	validTypes := make(map[TagType]bool)
	for tagType := range CanonicalTagDefinitions {
		validTypes[tagType] = true
	}
	// Also add types that might not have definitions but are valid
	validTypes[TagTypeMameParent] = true // Dynamic values
	validTypes[TagTypeExtension] = true  // Dynamic values
	validTypes[TagTypeSeason] = true     // Dynamic values
	validTypes[TagTypeEpisode] = true    // Dynamic values
	validTypes[TagTypeTrack] = true      // Dynamic values
	validTypes[TagTypeIssue] = true      // Dynamic values
	validTypes[TagTypeVolume] = true     // Dynamic values
	validTypes[TagTypeUnknown] = true    // Unknown tags

	for key, tags := range allTagMappings {
		for i, tag := range tags {
			if !validTypes[tag.Type] {
				t.Errorf("Mapping %q[%d] has invalid type: %q", key, i, tag.Type)
			}
		}
	}
}

// TestPropertyRegionMappingsWithLanguageAreConsistent verifies region+language mappings are valid.
func TestPropertyRegionMappingsWithLanguageAreConsistent(t *testing.T) {
	t.Parallel()

	// Build set of valid language values
	validLangs := make(map[TagValue]bool)
	for _, lang := range CanonicalTagDefinitions[TagTypeLang] {
		validLangs[lang] = true
	}

	for key, tags := range allTagMappings {
		var hasRegion bool
		for _, tag := range tags {
			if tag.Type == TagTypeRegion {
				hasRegion = true
			}
			// If it's a language tag, it should be a valid language value
			if tag.Type == TagTypeLang && !validLangs[tag.Value] {
				t.Errorf("Mapping %q has invalid language value: %q", key, tag.Value)
			}
		}
		// If we have a region, any accompanying language should be valid
		if hasRegion {
			for _, tag := range tags {
				if tag.Type == TagTypeLang && !validLangs[tag.Value] {
					t.Errorf("Region mapping %q has invalid language: %q", key, tag.Value)
				}
			}
		}
	}
}

// TestPropertyCanonicalTagStringFormat verifies CanonicalTag.String() format.
func TestPropertyCanonicalTagStringFormat(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		tagType := TagType(rapid.StringMatching(`[a-z]+`).Draw(t, "type"))
		tagValue := TagValue(rapid.StringMatching(`[a-z0-9\-]*`).Draw(t, "value"))

		tag := CanonicalTag{Type: tagType, Value: tagValue}
		str := tag.String()

		if tagValue == "" {
			// Type-only format
			if str != string(tagType) {
				t.Fatalf("Expected %q, got %q", tagType, str)
			}
		} else {
			// Type:Value format
			expected := string(tagType) + ":" + string(tagValue)
			if str != expected {
				t.Fatalf("Expected %q, got %q", expected, str)
			}
		}
	})
}

// TestPropertyMapFilenameTagToCanonicalNeverPanics verifies mapping never panics.
func TestPropertyMapFilenameTagToCanonicalNeverPanics(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		input := rapid.String().Draw(t, "input")

		// Should not panic
		_ = mapFilenameTagToCanonical(input)
	})
}

// TestPropertyMapFilenameTagToCanonicalReturnsNilForUnknown verifies unknown tags return nil.
func TestPropertyMapFilenameTagToCanonicalReturnsNilForUnknown(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		// Generate random strings that are unlikely to be real tags
		input := rapid.StringMatching(`[xyz]{10,20}`).Draw(t, "input")

		result := mapFilenameTagToCanonical(input)
		if result != nil {
			t.Fatalf("Expected nil for unknown tag %q, got %v", input, result)
		}
	})
}

// TestPropertyMapFilenameTagToCanonicalReturnsCopy verifies returned slice is a copy.
func TestPropertyMapFilenameTagToCanonicalReturnsCopy(t *testing.T) {
	t.Parallel()

	// Pick a known mapping that returns multiple tags
	result1 := mapFilenameTagToCanonical("usa")
	if len(result1) == 0 {
		t.Skip("No 'usa' mapping found")
	}

	// Modify the result
	originalType := result1[0].Type
	result1[0].Type = "modified"

	// Get a fresh copy
	result2 := mapFilenameTagToCanonical("usa")

	// The original map should not be affected
	if result2[0].Type != originalType {
		t.Fatal("mapFilenameTagToCanonical does not return a copy - modifying result affected original")
	}
}

// TestPropertyMappingKeysAreNormalized verifies all mapping keys are already normalized.
func TestPropertyMappingKeysAreNormalized(t *testing.T) {
	t.Parallel()

	// These keys are intentional special cases from TOSEC/GoodTools conventions
	// They contain characters that would be stripped by NormalizeTag but are
	// intentionally kept as-is for compatibility
	specialKeys := map[string]bool{
		"!":  true, // Verified good dump
		"!p": true, // Pending verification
	}

	for key := range allTagMappings {
		if specialKeys[key] {
			continue // Skip known special cases
		}
		normalized := NormalizeTag(key)
		if key != normalized {
			t.Errorf("Mapping key %q is not normalized, should be %q", key, normalized)
		}
	}
}

// ============================================================================
// Tag Definition Consistency Tests
// ============================================================================

// TestPropertyCanonicalTagDefinitionsHaveValidValues verifies all defined values are TagValue type.
func TestPropertyCanonicalTagDefinitionsHaveValidValues(t *testing.T) {
	t.Parallel()

	for tagType, values := range CanonicalTagDefinitions {
		for i, value := range values {
			// Value should not be empty (except for types with dynamic values)
			if value == "" {
				// Some types allow empty values (dynamic types)
				if tagType != TagTypeMameParent && tagType != TagTypeExtension {
					t.Errorf("Type %q has empty value at index %d", tagType, i)
				}
			}
		}
	}
}

// TestPropertyNoDuplicateValuesInDefinitions verifies no duplicate values within a type.
func TestPropertyNoDuplicateValuesInDefinitions(t *testing.T) {
	t.Parallel()

	for tagType, values := range CanonicalTagDefinitions {
		seen := make(map[TagValue]bool)
		for _, value := range values {
			if seen[value] {
				t.Errorf("Type %q has duplicate value: %q", tagType, value)
			}
			seen[value] = true
		}
	}
}
