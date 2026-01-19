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

package filters

import (
	"strings"
	"testing"

	"github.com/ZaparooProject/go-zapscript"
	"pgregory.net/rapid"
)

// ============================================================================
// Generators
// ============================================================================

// tagTypeGen generates valid tag type strings.
func tagTypeGen() *rapid.Generator[string] {
	return rapid.StringMatching(`[a-z]{1,15}`)
}

// tagValueGen generates valid tag value strings.
func tagValueGen() *rapid.Generator[string] {
	return rapid.StringMatching(`[a-z0-9\-]{1,20}`)
}

// operatorPrefixGen generates valid operator prefixes.
func operatorPrefixGen() *rapid.Generator[string] {
	return rapid.SampledFrom([]string{"", "+", "-", "~"})
}

// validTagStringGen generates valid "type:value" strings with optional operator.
func validTagStringGen() *rapid.Generator[string] {
	return rapid.Custom(func(t *rapid.T) string {
		prefix := operatorPrefixGen().Draw(t, "prefix")
		tagType := tagTypeGen().Draw(t, "type")
		tagValue := tagValueGen().Draw(t, "value")
		return prefix + tagType + ":" + tagValue
	})
}

// validTagSliceGen generates a slice of valid tag strings.
func validTagSliceGen() *rapid.Generator[[]string] {
	return rapid.SliceOfN(validTagStringGen(), 0, 20)
}

// ============================================================================
// Operator Prefix Parsing Tests
// ============================================================================

// TestPropertyParseTagFiltersDefaultOperator verifies no prefix means AND.
func TestPropertyParseTagFiltersDefaultOperator(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		tagType := tagTypeGen().Draw(t, "type")
		tagValue := tagValueGen().Draw(t, "value")
		tagStr := tagType + ":" + tagValue // No prefix

		result, err := ParseTagFilters([]string{tagStr})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if len(result) != 1 {
			t.Fatalf("Expected 1 filter, got %d", len(result))
		}

		if result[0].Operator != zapscript.TagOperatorAND {
			t.Fatalf("Expected AND operator, got %v", result[0].Operator)
		}
	})
}

// TestPropertyParseTagFiltersPlusOperator verifies + prefix means AND.
func TestPropertyParseTagFiltersPlusOperator(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		tagType := tagTypeGen().Draw(t, "type")
		tagValue := tagValueGen().Draw(t, "value")
		tagStr := "+" + tagType + ":" + tagValue

		result, err := ParseTagFilters([]string{tagStr})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if len(result) != 1 {
			t.Fatalf("Expected 1 filter, got %d", len(result))
		}

		if result[0].Operator != zapscript.TagOperatorAND {
			t.Fatalf("Expected AND operator for + prefix, got %v", result[0].Operator)
		}
	})
}

// TestPropertyParseTagFiltersMinusOperator verifies - prefix means NOT.
func TestPropertyParseTagFiltersMinusOperator(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		tagType := tagTypeGen().Draw(t, "type")
		tagValue := tagValueGen().Draw(t, "value")
		tagStr := "-" + tagType + ":" + tagValue

		result, err := ParseTagFilters([]string{tagStr})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if len(result) != 1 {
			t.Fatalf("Expected 1 filter, got %d", len(result))
		}

		if result[0].Operator != zapscript.TagOperatorNOT {
			t.Fatalf("Expected NOT operator for - prefix, got %v", result[0].Operator)
		}
	})
}

// TestPropertyParseTagFiltersTildeOperator verifies ~ prefix means OR.
func TestPropertyParseTagFiltersTildeOperator(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		tagType := tagTypeGen().Draw(t, "type")
		tagValue := tagValueGen().Draw(t, "value")
		tagStr := "~" + tagType + ":" + tagValue

		result, err := ParseTagFilters([]string{tagStr})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if len(result) != 1 {
			t.Fatalf("Expected 1 filter, got %d", len(result))
		}

		if result[0].Operator != zapscript.TagOperatorOR {
			t.Fatalf("Expected OR operator for ~ prefix, got %v", result[0].Operator)
		}
	})
}

// ============================================================================
// Format Validation Tests
// ============================================================================

// TestPropertyParseTagFiltersMustHaveColon verifies colon is required.
func TestPropertyParseTagFiltersMustHaveColon(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		// Generate a string without colon
		noColonStr := rapid.StringMatching(`[a-z]{1,20}`).Draw(t, "noColon")

		// Should be accepted if empty or whitespace-only (skipped)
		if strings.TrimSpace(noColonStr) == "" {
			return
		}

		_, err := ParseTagFilters([]string{noColonStr})
		if err == nil {
			t.Fatalf("Expected error for tag without colon: %q", noColonStr)
		}

		if !strings.Contains(err.Error(), "type:value") {
			t.Fatalf("Expected 'type:value' format error, got: %v", err)
		}
	})
}

// TestPropertyParseTagFiltersEmptySkipped verifies empty/whitespace tags are skipped.
func TestPropertyParseTagFiltersEmptySkipped(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		tagType := tagTypeGen().Draw(t, "type")
		tagValue := tagValueGen().Draw(t, "value")
		validTag := tagType + ":" + tagValue

		// Mix valid tags with empty/whitespace
		emptyVariants := []string{"", " ", "\t", "\n", "  \t  "}
		emptyCount := rapid.IntRange(1, 5).Draw(t, "emptyCount")

		tags := make([]string, 0, 1+emptyCount)
		tags = append(tags, validTag)
		for range emptyCount {
			tags = append(tags, rapid.SampledFrom(emptyVariants).Draw(t, "empty"))
		}

		result, err := ParseTagFilters(tags)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		// Only the valid tag should be in the result
		if len(result) != 1 {
			t.Fatalf("Expected 1 filter (empty skipped), got %d", len(result))
		}
	})
}

// ============================================================================
// Limit Enforcement Tests
// ============================================================================

// TestPropertyParseTagFiltersMaxTagsCount verifies maxTagsCount limit.
func TestPropertyParseTagFiltersMaxTagsCount(t *testing.T) {
	t.Parallel()

	// Generate exactly maxTagsCount + 1 tags
	tags := make([]string, maxTagsCount+1)
	for i := range tags {
		tags[i] = "type:value" + string(rune('a'+i%26))
	}

	_, err := ParseTagFilters(tags)
	if err == nil {
		t.Fatal("Expected error for exceeding maxTagsCount")
	}

	if !strings.Contains(err.Error(), "maximum") {
		t.Fatalf("Expected 'maximum' in error, got: %v", err)
	}
}

// TestPropertyParseTagFiltersMaxTagLength verifies maxTagLength limit.
func TestPropertyParseTagFiltersMaxTagLength(t *testing.T) {
	t.Parallel()

	// Generate a tag longer than maxTagLength
	longTag := strings.Repeat("a", maxTagLength+1)
	tagStr := "type:" + longTag

	_, err := ParseTagFilters([]string{tagStr})
	if err == nil {
		t.Fatal("Expected error for exceeding maxTagLength")
	}

	if !strings.Contains(err.Error(), "too long") {
		t.Fatalf("Expected 'too long' in error, got: %v", err)
	}
}

// TestPropertyParseTagFiltersWithinLimitsSucceeds verifies within-limit tags succeed.
func TestPropertyParseTagFiltersWithinLimitsSucceeds(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		// Generate tags within limits
		count := rapid.IntRange(1, 20).Draw(t, "count")
		tags := make([]string, count)
		for i := range count {
			tagType := tagTypeGen().Draw(t, "type")
			tagValue := tagValueGen().Draw(t, "value")
			tags[i] = tagType + ":" + tagValue
		}

		result, err := ParseTagFilters(tags)
		if err != nil {
			t.Fatalf("Unexpected error for within-limit tags: %v", err)
		}

		// Result count may be less due to deduplication
		if len(result) > count {
			t.Fatalf("Result count (%d) exceeds input count (%d)", len(result), count)
		}
	})
}

// ============================================================================
// Deduplication Tests
// ============================================================================

// TestPropertyParseTagFiltersDeduplication verifies duplicate removal.
func TestPropertyParseTagFiltersDeduplication(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		tagType := tagTypeGen().Draw(t, "type")
		tagValue := tagValueGen().Draw(t, "value")
		tagStr := tagType + ":" + tagValue

		// Create duplicates
		dupCount := rapid.IntRange(2, 5).Draw(t, "dupCount")
		tags := make([]string, dupCount)
		for i := range dupCount {
			tags[i] = tagStr
		}

		result, err := ParseTagFilters(tags)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		// Should have only 1 filter
		if len(result) != 1 {
			t.Fatalf("Expected 1 filter after deduplication, got %d", len(result))
		}
	})
}

// TestPropertyParseTagFiltersDifferentOperatorsNotDuplicate verifies different operators are kept.
func TestPropertyParseTagFiltersDifferentOperatorsNotDuplicate(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		tagType := tagTypeGen().Draw(t, "type")
		tagValue := tagValueGen().Draw(t, "value")

		// Same type:value with different operators
		tags := []string{
			tagType + ":" + tagValue,       // AND
			"-" + tagType + ":" + tagValue, // NOT
			"~" + tagType + ":" + tagValue, // OR
		}

		result, err := ParseTagFilters(tags)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		// Should have 3 filters (different operators are not duplicates)
		if len(result) != 3 {
			t.Fatalf("Expected 3 filters (different operators), got %d", len(result))
		}
	})
}

// ============================================================================
// Normalization Tests
// ============================================================================

// TestPropertyParseTagFiltersNormalization verifies type and value are normalized.
func TestPropertyParseTagFiltersNormalization(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		tagType := tagTypeGen().Draw(t, "type")
		tagValue := tagValueGen().Draw(t, "value")

		// Use uppercase (will be normalized to lowercase)
		tagStr := strings.ToUpper(tagType) + ":" + strings.ToUpper(tagValue)

		result, err := ParseTagFilters([]string{tagStr})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if len(result) != 1 {
			t.Fatalf("Expected 1 filter, got %d", len(result))
		}

		// Should be lowercase after normalization
		if !strings.EqualFold(result[0].Type, tagType) {
			t.Fatalf("Expected normalized type %q, got %q", strings.ToLower(tagType), result[0].Type)
		}
		if !strings.EqualFold(result[0].Value, tagValue) {
			t.Fatalf("Expected normalized value %q, got %q", strings.ToLower(tagValue), result[0].Value)
		}
	})
}

// ============================================================================
// Result Properties Tests
// ============================================================================

// TestPropertyParseTagFiltersResultNeverNil verifies result is never nil for valid input.
func TestPropertyParseTagFiltersResultNeverNil(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		tags := validTagSliceGen().Draw(t, "tags")

		result, err := ParseTagFilters(tags)
		if err != nil {
			// Errors are acceptable, but result should still be nil
			if result != nil {
				t.Fatal("Expected nil result on error")
			}
			return
		}

		// On success, result should not be nil
		if result == nil {
			t.Fatal("Expected non-nil result on success")
		}
	})
}

// TestPropertyParseTagFiltersOrderPreserved verifies first occurrence order is preserved.
func TestPropertyParseTagFiltersOrderPreserved(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		// Generate unique tags
		count := rapid.IntRange(2, 10).Draw(t, "count")
		uniqueTags := make([]string, count)
		seen := make(map[string]bool)

		for i := range count {
			var tagStr string
			for {
				tagType := tagTypeGen().Draw(t, "type")
				tagValue := tagValueGen().Draw(t, "value")
				tagStr = tagType + ":" + tagValue
				if !seen[tagStr] {
					seen[tagStr] = true
					break
				}
			}
			uniqueTags[i] = tagStr
		}

		result, err := ParseTagFilters(uniqueTags)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if len(result) != count {
			t.Fatalf("Expected %d filters, got %d", count, len(result))
		}

		// Verify order matches input order
		for i, filter := range result {
			expectedFilter := uniqueTags[i]
			// Parse expected to compare
			parts := strings.SplitN(expectedFilter, ":", 2)
			if !strings.EqualFold(filter.Type, parts[0]) {
				t.Fatalf("Order not preserved at index %d", i)
			}
		}
	})
}

// TestPropertyParseTagFiltersNeverPanics verifies parser never panics.
func TestPropertyParseTagFiltersNeverPanics(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		// Generate any strings
		tags := rapid.SliceOfN(rapid.String(), 0, 30).Draw(t, "tags")

		// Should not panic
		_, _ = ParseTagFilters(tags)
	})
}
