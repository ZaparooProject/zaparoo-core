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

package mediadb

import (
	"math/rand"
	"strings"
	"testing"

	"github.com/ZaparooProject/go-zapscript"
	"pgregory.net/rapid"
)

// ============================================================================
// Generators
// ============================================================================

// systemIDGen generates realistic system IDs.
func systemIDGen() *rapid.Generator[string] {
	return rapid.StringMatching(`[a-z0-9_]{1,20}`)
}

// slugGen generates realistic slug strings.
func slugGen() *rapid.Generator[string] {
	return rapid.StringMatching(`[a-z0-9\-]{1,50}`)
}

// tagFilterGen generates random TagFilter values.
func tagFilterGen() *rapid.Generator[zapscript.TagFilter] {
	return rapid.Custom(func(t *rapid.T) zapscript.TagFilter {
		tagTypes := []string{"lang", "region", "year", "players", "edition", "demo", "proto"}
		tagType := rapid.SampledFrom(tagTypes).Draw(t, "tagType")
		tagValue := rapid.StringMatching(`[a-z0-9\-]{1,15}`).Draw(t, "tagValue")
		operator := rapid.SampledFrom([]zapscript.TagOperator{
			zapscript.TagOperatorAND,
			zapscript.TagOperatorNOT,
			zapscript.TagOperatorOR,
		}).Draw(t, "operator")

		return zapscript.TagFilter{
			Type:     tagType,
			Value:    tagValue,
			Operator: operator,
		}
	})
}

// tagFiltersGen generates a slice of tag filters.
func tagFiltersGen() *rapid.Generator[[]zapscript.TagFilter] {
	return rapid.SliceOfN(tagFilterGen(), 0, 10)
}

// ============================================================================
// Cache Key Determinism Tests
// ============================================================================

// TestPropertySlugCacheKeyDeterministic verifies same inputs produce same key.
func TestPropertySlugCacheKeyDeterministic(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		systemID := systemIDGen().Draw(t, "systemID")
		slug := slugGen().Draw(t, "slug")
		tagFilters := tagFiltersGen().Draw(t, "tagFilters")

		key1, err1 := generateSlugCacheKey(systemID, slug, tagFilters)
		key2, err2 := generateSlugCacheKey(systemID, slug, tagFilters)

		if err1 != nil || err2 != nil {
			t.Fatalf("Expected no errors: err1=%v, err2=%v", err1, err2)
		}

		if key1 != key2 {
			t.Fatalf("Same inputs should produce same key: %q vs %q", key1, key2)
		}
	})
}

// TestPropertySlugCacheKeyNeverEmpty verifies key is never empty.
func TestPropertySlugCacheKeyNeverEmpty(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		systemID := systemIDGen().Draw(t, "systemID")
		slug := slugGen().Draw(t, "slug")
		tagFilters := tagFiltersGen().Draw(t, "tagFilters")

		key, err := generateSlugCacheKey(systemID, slug, tagFilters)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if key == "" {
			t.Fatal("Cache key should never be empty")
		}
	})
}

// TestPropertySlugCacheKeyIsHex verifies key is valid hex string (SHA256).
func TestPropertySlugCacheKeyIsHex(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		systemID := systemIDGen().Draw(t, "systemID")
		slug := slugGen().Draw(t, "slug")
		tagFilters := tagFiltersGen().Draw(t, "tagFilters")

		key, err := generateSlugCacheKey(systemID, slug, tagFilters)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		// SHA256 hex is 64 characters
		if len(key) != 64 {
			t.Fatalf("Expected 64 character hex (SHA256), got %d: %s", len(key), key)
		}

		// All characters should be hex
		for _, c := range key {
			if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
				t.Fatalf("Invalid hex character %q in key: %s", c, key)
			}
		}
	})
}

// ============================================================================
// Tag Order Independence Tests
// ============================================================================

// TestPropertySlugCacheKeyTagOrderIndependent verifies tag order doesn't affect key.
func TestPropertySlugCacheKeyTagOrderIndependent(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		systemID := systemIDGen().Draw(t, "systemID")
		slug := slugGen().Draw(t, "slug")
		tagFilters := rapid.SliceOfN(tagFilterGen(), 2, 10).Draw(t, "tagFilters")

		// Create a shuffled copy
		shuffled := make([]zapscript.TagFilter, len(tagFilters))
		copy(shuffled, tagFilters)
		// Fisher-Yates shuffle
		for i := len(shuffled) - 1; i > 0; i-- {
			j := rand.Intn(i + 1) //nolint:gosec // Not cryptographic
			shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
		}

		key1, err1 := generateSlugCacheKey(systemID, slug, tagFilters)
		key2, err2 := generateSlugCacheKey(systemID, slug, shuffled)

		if err1 != nil || err2 != nil {
			t.Fatalf("Expected no errors: err1=%v, err2=%v", err1, err2)
		}

		if key1 != key2 {
			t.Fatalf("Tag order should not affect key: original=%q, shuffled=%q", key1, key2)
		}
	})
}

// ============================================================================
// Normalization Tests
// ============================================================================

// TestPropertySlugCacheKeyCaseInsensitive verifies case doesn't affect key.
func TestPropertySlugCacheKeyCaseInsensitive(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		// Use lowercase generators
		systemID := systemIDGen().Draw(t, "systemID")
		slug := slugGen().Draw(t, "slug")

		// Keys should match regardless of case
		key1, err1 := generateSlugCacheKey(systemID, slug, nil)
		key2, err2 := generateSlugCacheKey(strings.ToUpper(systemID), strings.ToUpper(slug), nil)

		if err1 != nil || err2 != nil {
			t.Fatalf("Expected no errors: err1=%v, err2=%v", err1, err2)
		}

		if key1 != key2 {
			t.Fatalf("Case should not affect key: lower=%q, upper=%q", key1, key2)
		}
	})
}

// TestPropertySlugCacheKeyWhitespaceInsensitive verifies trimmed whitespace doesn't affect key.
func TestPropertySlugCacheKeyWhitespaceInsensitive(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		systemID := systemIDGen().Draw(t, "systemID")
		slug := slugGen().Draw(t, "slug")

		// Add leading/trailing whitespace
		paddedSystemID := "  " + systemID + "  "
		paddedSlug := "\t" + slug + "\n"

		key1, err1 := generateSlugCacheKey(systemID, slug, nil)
		key2, err2 := generateSlugCacheKey(paddedSystemID, paddedSlug, nil)

		if err1 != nil || err2 != nil {
			t.Fatalf("Expected no errors: err1=%v, err2=%v", err1, err2)
		}

		if key1 != key2 {
			t.Fatalf("Whitespace should be trimmed: clean=%q, padded=%q", key1, key2)
		}
	})
}

// ============================================================================
// Key Uniqueness Tests (Probabilistic)
// ============================================================================

// TestPropertySlugCacheKeyDifferentSystemsDifferentKeys verifies different systems get different keys.
func TestPropertySlugCacheKeyDifferentSystemsDifferentKeys(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		systemID1 := systemIDGen().Draw(t, "systemID1")
		systemID2 := systemIDGen().Draw(t, "systemID2")

		// Skip if systems are the same after normalization
		if strings.EqualFold(strings.TrimSpace(systemID1), strings.TrimSpace(systemID2)) {
			return
		}

		slug := slugGen().Draw(t, "slug")
		tagFilters := tagFiltersGen().Draw(t, "tagFilters")

		key1, err1 := generateSlugCacheKey(systemID1, slug, tagFilters)
		key2, err2 := generateSlugCacheKey(systemID2, slug, tagFilters)

		if err1 != nil || err2 != nil {
			t.Fatalf("Expected no errors: err1=%v, err2=%v", err1, err2)
		}

		if key1 == key2 {
			t.Fatalf("Different systems should produce different keys: "+
				"system1=%q, system2=%q, key=%q", systemID1, systemID2, key1)
		}
	})
}

// TestPropertySlugCacheKeyDifferentSlugsDifferentKeys verifies different slugs get different keys.
func TestPropertySlugCacheKeyDifferentSlugsDifferentKeys(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		slug1 := slugGen().Draw(t, "slug1")
		slug2 := slugGen().Draw(t, "slug2")

		// Skip if slugs are the same after normalization
		if strings.EqualFold(strings.TrimSpace(slug1), strings.TrimSpace(slug2)) {
			return
		}

		systemID := systemIDGen().Draw(t, "systemID")
		tagFilters := tagFiltersGen().Draw(t, "tagFilters")

		key1, err1 := generateSlugCacheKey(systemID, slug1, tagFilters)
		key2, err2 := generateSlugCacheKey(systemID, slug2, tagFilters)

		if err1 != nil || err2 != nil {
			t.Fatalf("Expected no errors: err1=%v, err2=%v", err1, err2)
		}

		if key1 == key2 {
			t.Fatalf("Different slugs should produce different keys: "+
				"slug1=%q, slug2=%q, key=%q", slug1, slug2, key1)
		}
	})
}

// TestPropertySlugCacheKeyDifferentTagsDifferentKeys verifies different tags get different keys.
func TestPropertySlugCacheKeyDifferentTagsDifferentKeys(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		systemID := systemIDGen().Draw(t, "systemID")
		slug := slugGen().Draw(t, "slug")

		// Generate two different tag sets
		tags1 := rapid.SliceOfN(tagFilterGen(), 1, 5).Draw(t, "tags1")
		tags2 := rapid.SliceOfN(tagFilterGen(), 1, 5).Draw(t, "tags2")

		key1, err1 := generateSlugCacheKey(systemID, slug, tags1)
		key2, err2 := generateSlugCacheKey(systemID, slug, tags2)

		if err1 != nil || err2 != nil {
			t.Fatalf("Expected no errors: err1=%v, err2=%v", err1, err2)
		}

		// If tags are different, keys should be different
		// (Skip comparison if tags happened to be equivalent)
		if tagsEqual(tags1, tags2) {
			return
		}

		if key1 == key2 {
			t.Fatalf("Different tags should produce different keys: "+
				"tags1=%v, tags2=%v, key=%q", tags1, tags2, key1)
		}
	})
}

// ============================================================================
// Empty Input Tests
// ============================================================================

// TestPropertySlugCacheKeyEmptyInputs verifies empty inputs work correctly.
func TestPropertySlugCacheKeyEmptyInputs(t *testing.T) {
	t.Parallel()

	// Empty system ID and slug should still produce a key
	key, err := generateSlugCacheKey("", "", nil)
	if err != nil {
		t.Fatalf("Unexpected error with empty inputs: %v", err)
	}
	if key == "" {
		t.Fatal("Empty inputs should still produce a cache key")
	}
	if len(key) != 64 {
		t.Fatalf("Expected 64 character hex, got %d: %s", len(key), key)
	}
}

// TestPropertySlugCacheKeyEmptyTagFilters verifies empty tag filters work.
func TestPropertySlugCacheKeyEmptyTagFilters(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		systemID := systemIDGen().Draw(t, "systemID")
		slug := slugGen().Draw(t, "slug")

		// nil and empty slice should produce same key
		key1, err1 := generateSlugCacheKey(systemID, slug, nil)
		key2, err2 := generateSlugCacheKey(systemID, slug, []zapscript.TagFilter{})

		if err1 != nil || err2 != nil {
			t.Fatalf("Expected no errors: err1=%v, err2=%v", err1, err2)
		}

		if key1 != key2 {
			t.Fatalf("nil and empty tags should produce same key: nil=%q, empty=%q", key1, key2)
		}
	})
}

// ============================================================================
// Helper Functions
// ============================================================================

// tagsEqual checks if two tag filter slices have equivalent content.
func tagsEqual(a, b []zapscript.TagFilter) bool {
	if len(a) != len(b) {
		return false
	}

	// Count occurrences of each tag
	countA := make(map[string]int)
	countB := make(map[string]int)

	for _, tag := range a {
		key := tag.Type + ":" + tag.Value + ":" + string(tag.Operator)
		countA[key]++
	}
	for _, tag := range b {
		key := tag.Type + ":" + tag.Value + ":" + string(tag.Operator)
		countB[key]++
	}

	if len(countA) != len(countB) {
		return false
	}

	for k, v := range countA {
		if countB[k] != v {
			return false
		}
	}

	return true
}
