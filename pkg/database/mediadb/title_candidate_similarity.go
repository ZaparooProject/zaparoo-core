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

import "github.com/hbollon/go-edlib"

// candidateDistanceLowerBound only distinguishes zero, one and at least two
// edits. For equal-length ASCII strings, one edit can change at most two adjacent
// positions (a transposition), or one position (a substitution). Other inputs
// return zero rather than applying byte-based bounds to rune-aware distance.
func candidateDistanceLowerBound(a, b string) int {
	if len(a) != len(b) {
		return 0
	}
	first, second, count := -1, -1, 0
	for i := range len(a) {
		if a[i] >= 128 || b[i] >= 128 {
			return 0
		}
		if a[i] != b[i] {
			count++
			if first < 0 {
				first = i
			} else if second < 0 {
				second = i
			}
		}
	}
	if count <= 1 {
		return count
	}
	if count == 2 && second == first+1 && a[first] == b[second] && a[second] == b[first] {
		return 1
	}
	return 2
}

// candidateSimilarity preserves go-edlib's greedy Jaro-Winkler matching and
// float32 evaluation order. Short ASCII slugs use bitsets instead of allocated
// rune slices and correspondence tables. Other inputs keep the existing scorer.
func candidateSimilarity(a, b string) float32 {
	if a == "" || b == "" {
		return 0
	}
	if a == b {
		return 1
	}
	if len(a) > 32 || len(b) > 32 {
		return edlib.JaroWinklerSimilarity(a, b)
	}
	for _, text := range []string{a, b} {
		for i := range len(text) {
			if text[i] >= 128 {
				return edlib.JaroWinklerSimilarity(a, b)
			}
		}
	}
	window := max(len(a), len(b))/2 - 1
	var left, right uint32
	matched := 0
	for i := range len(a) {
		for j := max(0, i-window); j < min(len(b), i+window+1); j++ {
			if a[i] == b[j] && right&(uint32(1)<<j) == 0 {
				left |= uint32(1) << i
				right |= uint32(1) << j
				matched++
				break
			}
		}
	}
	if matched == 0 {
		return 0
	}
	outOfOrder, next := float32(0), 0
	for i := range len(a) {
		if left&(uint32(1)<<i) == 0 {
			continue
		}
		for right&(uint32(1)<<next) == 0 {
			next++
		}
		if a[i] != b[next] {
			outOfOrder++
		}
		next++
	}
	transpositions := outOfOrder / 2
	count := float32(matched)
	score := (count/float32(len(a)) + count/float32(len(b)) + (count-transpositions)/count) / 3
	if score == 1 {
		return score
	}
	prefix := 0
	for prefix < min(4, len(a), len(b)) && a[prefix] == b[prefix] {
		prefix++
	}
	return score + 0.1*float32(prefix)*(1-score)
}
