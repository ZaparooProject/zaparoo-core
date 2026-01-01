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

package slugs

// GenerateBigrams creates overlapping 2-character chunks from a string.
// This is used for matching scripts that don't have word boundaries (Thai, Burmese, Khmer, Lao).
//
// Example:
//
//	GenerateBigrams("เพลงไทย") → ["เพ", "พล", "ลง", "งไ", "ไท", "ทย"]
//
// For strings shorter than 2 characters, returns the original string as a single-element slice.
func GenerateBigrams(s string) []string {
	runes := []rune(s)
	if len(runes) < 2 {
		return []string{s}
	}

	bigrams := make([]string, 0, len(runes)-1)
	for i := range len(runes) - 1 {
		bigrams = append(bigrams, string(runes[i:i+2]))
	}
	return bigrams
}

// JaccardSimilarity computes the Jaccard similarity coefficient between two sets of strings.
// This is defined as the size of the intersection divided by the size of the union.
//
// Returns a value between 0.0 (no overlap) and 1.0 (identical sets).
//
// Example:
//
//	set1 := []string{"a", "b", "c"}
//	set2 := []string{"b", "c", "d"}
//	similarity := JaccardSimilarity(set1, set2)  // Returns 0.5 (2 common / 4 total)
func JaccardSimilarity(set1, set2 []string) float64 {
	// Handle edge cases
	if len(set1) == 0 && len(set2) == 0 {
		return 1.0 // Both empty = identical
	}
	if len(set1) == 0 || len(set2) == 0 {
		return 0.0 // One empty = no similarity
	}

	// Build sets using maps for efficient lookup
	union := make(map[string]bool)
	intersection := make(map[string]bool)

	// Add all elements from set1 to union
	for _, elem := range set1 {
		union[elem] = true
	}

	// For each element in set2:
	// - If it exists in set1, add to intersection
	// - Add to union
	for _, elem := range set2 {
		if union[elem] {
			intersection[elem] = true
		}
		union[elem] = true
	}

	// Jaccard similarity = |intersection| / |union|
	return float64(len(intersection)) / float64(len(union))
}

// GenerateTrigrams creates overlapping 3-character chunks from a string.
// This is an alternative to bigrams that may provide better accuracy for longer queries.
//
// Example:
//
//	GenerateTrigrams("เพลงไทย") → ["เพล", "พลง", "ลงไ", "งไท", "ไทย"]
//
// For strings shorter than 3 characters, falls back to bigrams or returns the original string.
func GenerateTrigrams(s string) []string {
	runes := []rune(s)
	if len(runes) < 2 {
		return []string{s}
	}
	if len(runes) < 3 {
		return GenerateBigrams(s)
	}

	trigrams := make([]string, 0, len(runes)-2)
	for i := range len(runes) - 2 {
		trigrams = append(trigrams, string(runes[i:i+3]))
	}
	return trigrams
}
