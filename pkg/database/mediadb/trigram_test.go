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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEncodeTrigramID_ValidChars(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		b0   byte
		b1   byte
		b2   byte
	}{
		{name: "all digits", b0: '0', b1: '5', b2: '9'},
		{name: "all lowercase", b0: 'a', b1: 'm', b2: 'z'},
		{name: "with dash", b0: 'a', b1: '-', b2: 'b'},
		{name: "mixed", b0: '3', b1: 'k', b2: '-'},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			id, ok := encodeTrigramID(tt.b0, tt.b1, tt.b2)
			assert.True(t, ok)
			assert.Less(t, id, uint32(trigramCount))
		})
	}
}

func TestEncodeTrigramID_InvalidChars(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		b0   byte
		b1   byte
		b2   byte
	}{
		{name: "uppercase", b0: 'A', b1: 'b', b2: 'c'},
		{name: "space", b0: 'a', b1: ' ', b2: 'c'},
		{name: "underscore", b0: 'a', b1: '_', b2: 'c'},
		{name: "null byte", b0: 0, b1: 'a', b2: 'b'},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, ok := encodeTrigramID(tt.b0, tt.b1, tt.b2)
			assert.False(t, ok)
		})
	}
}

func TestEncodeTrigramID_Uniqueness(t *testing.T) {
	t.Parallel()
	seen := make(map[uint32]struct{})
	alphabet := []byte("0123456789abcdefghijklmnopqrstuvwxyz-")

	for _, a := range alphabet {
		for _, b := range alphabet {
			for _, c := range alphabet {
				id, ok := encodeTrigramID(a, b, c)
				require.True(t, ok)
				_, dup := seen[id]
				require.False(t, dup, "duplicate ID for %c%c%c", a, b, c)
				seen[id] = struct{}{}
			}
		}
	}
	assert.Len(t, seen, trigramCount)
}

func TestExtractQueryTrigrams_Basic(t *testing.T) {
	t.Parallel()

	trigrams := extractQueryTrigrams([]byte("abcd"))
	assert.Len(t, trigrams, 2) // "abc", "bcd"
}

func TestExtractQueryTrigrams_TooShort(t *testing.T) {
	t.Parallel()

	assert.Nil(t, extractQueryTrigrams([]byte("ab")))
	assert.Nil(t, extractQueryTrigrams([]byte("a")))
	assert.Nil(t, extractQueryTrigrams(nil))
}

func TestExtractQueryTrigrams_Dedup(t *testing.T) {
	t.Parallel()

	// "aaa" repeated — only one unique trigram
	trigrams := extractQueryTrigrams([]byte("aaaa"))
	assert.Len(t, trigrams, 1)
}

func TestExtractQueryTrigrams_SkipsInvalidChars(t *testing.T) {
	t.Parallel()

	// 'A' is invalid, so "aAb" and "Ab?" won't produce trigrams
	trigrams := extractQueryTrigrams([]byte("aAbcd"))
	// valid trigrams: "bcd" only (a-A breaks, A-b breaks, b-c-d valid)
	assert.Len(t, trigrams, 1)
}

func TestSortedIntersection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		a    []uint32
		b    []uint32
		want []uint32
	}{
		{
			name: "overlap",
			a:    []uint32{1, 3, 5, 7},
			b:    []uint32{2, 3, 5, 8},
			want: []uint32{3, 5},
		},
		{
			name: "no overlap",
			a:    []uint32{1, 2, 3},
			b:    []uint32{4, 5, 6},
			want: []uint32{},
		},
		{
			name: "identical",
			a:    []uint32{1, 2, 3},
			b:    []uint32{1, 2, 3},
			want: []uint32{1, 2, 3},
		},
		{
			name: "a empty",
			a:    []uint32{},
			b:    []uint32{1, 2},
			want: []uint32{},
		},
		{
			name: "b empty",
			a:    []uint32{1, 2},
			b:    []uint32{},
			want: []uint32{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// Clone a to avoid mutation affecting test expectations.
			aClone := make([]uint32, len(tt.a))
			copy(aClone, tt.a)
			got := sortedIntersection(aClone, tt.b)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestSortedUnion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		a    []uint32
		b    []uint32
		want []uint32
	}{
		{
			name: "overlap",
			a:    []uint32{1, 3, 5},
			b:    []uint32{2, 3, 6},
			want: []uint32{1, 2, 3, 5, 6},
		},
		{
			name: "no overlap",
			a:    []uint32{1, 2},
			b:    []uint32{3, 4},
			want: []uint32{1, 2, 3, 4},
		},
		{
			name: "a empty",
			a:    []uint32{},
			b:    []uint32{1, 2},
			want: []uint32{1, 2},
		},
		{
			name: "b empty",
			a:    []uint32{1, 2},
			b:    []uint32{},
			want: []uint32{1, 2},
		},
		{
			name: "both empty",
			a:    []uint32{},
			b:    []uint32{},
			want: []uint32{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := sortedUnion(tt.a, tt.b)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestTrigramCharIndex(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 0, trigramCharIndex('0'))
	assert.Equal(t, 9, trigramCharIndex('9'))
	assert.Equal(t, 10, trigramCharIndex('a'))
	assert.Equal(t, 35, trigramCharIndex('z'))
	assert.Equal(t, 36, trigramCharIndex('-'))
	assert.Equal(t, -1, trigramCharIndex('A'))
	assert.Equal(t, -1, trigramCharIndex(' '))
}

func TestBuildTrigramIndex_EmptyCache(t *testing.T) {
	t.Parallel()
	cache := &SlugSearchCache{entryCount: 0}
	buildTrigramIndex(cache)
	assert.Nil(t, cache.trigramPostings)
	assert.Nil(t, cache.trigramOffsets)
}

func TestTrigramSearch_FallbackToLinear_ShortVariant(t *testing.T) {
	t.Parallel()

	// Build a cache where the search query has a 2-byte variant (too short for trigrams).
	cache := buildTestCache([]struct {
		slug       string
		secSlug    string
		titleDBID  int64
		systemDBID int64
	}{
		{slug: "super-mario-bros", titleDBID: 1, systemDBID: 1},
		{slug: "mario-kart", titleDBID: 2, systemDBID: 1},
		{slug: "zelda", titleDBID: 3, systemDBID: 1},
	}, map[int64]string{1: "nes"})

	// Search with a short variant ("ma" = 2 bytes) forces linear fallback.
	results := cache.Search(nil, [][][]byte{
		{[]byte("ma")},
	})
	assert.Len(t, results, 2) // mario entries
	assert.Contains(t, results, int64(1))
	assert.Contains(t, results, int64(2))
}

func TestTrigramSearch_LongVariant(t *testing.T) {
	t.Parallel()

	cache := buildTestCache([]struct {
		slug       string
		secSlug    string
		titleDBID  int64
		systemDBID int64
	}{
		{slug: "super-mario-bros", titleDBID: 1, systemDBID: 1},
		{slug: "mario-kart-64", titleDBID: 2, systemDBID: 1},
		{slug: "zelda-ocarina", titleDBID: 3, systemDBID: 1},
	}, map[int64]string{1: "nes"})

	// Search with a long variant uses trigram index.
	results := cache.Search(nil, [][][]byte{
		{[]byte("mario")},
	})
	assert.Len(t, results, 2)
	assert.Contains(t, results, int64(1))
	assert.Contains(t, results, int64(2))
}
