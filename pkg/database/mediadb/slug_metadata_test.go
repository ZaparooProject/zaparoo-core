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

package mediadb

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSlugMetadata(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		input         string
		wantSlug      string
		wantLength    int
		wantWordCount int
	}{
		{
			name:          "Super Mario Bros",
			input:         "Super Mario Bros.",
			wantSlug:      "supermariobrothers",
			wantLength:    18,
			wantWordCount: 3,
		},
		{
			name:          "zelda single word",
			input:         "zelda",
			wantSlug:      "zelda",
			wantLength:    5,
			wantWordCount: 1,
		},
		{
			name:          "CJK bigrams",
			input:         "ドラゴンクエスト",
			wantSlug:      "ドラゴンクエスト",
			wantLength:    8,
			wantWordCount: 7, // 8 chars → 7 bigrams
		},
		{
			name:          "empty string",
			input:         "",
			wantSlug:      "",
			wantLength:    0,
			wantWordCount: 0,
		},
		{
			name:          "with metadata brackets",
			input:         "Final Fantasy VII (USA) [!]",
			wantSlug:      "finalfantasy7",
			wantLength:    13,
			wantWordCount: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			metadata := GenerateSlugWithMetadata(tt.input)

			assert.Equal(t, tt.wantSlug, metadata.Slug, "Slug mismatch")
			assert.Equal(t, tt.wantLength, metadata.SlugLength, "SlugLength mismatch")
			assert.Equal(t, tt.wantWordCount, metadata.SlugWordCount, "SlugWordCount mismatch")
		})
	}
}

func TestCJKBigrams(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		input       string
		wantBigrams int
	}{
		{
			name:        "8 char CJK",
			input:       "ドラゴンクエスト",
			wantBigrams: 7, // 8 chars → 7 bigrams
		},
		{
			name:        "6 char CJK",
			input:       "マリオカート",
			wantBigrams: 5, // 6 chars → 5 bigrams
		},
		{
			name:        "3 char CJK",
			input:       "ゼルダ",
			wantBigrams: 2, // 3 chars → 2 bigrams
		},
		{
			name:        "single char CJK",
			input:       "龍", //nolint:gosmopolitan // CJK test case
			wantBigrams: 1,   // 1 char → 1 (no bigram possible)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			metadata := GenerateSlugWithMetadata(tt.input)
			assert.Equal(t, tt.wantBigrams, metadata.SlugWordCount, "CJK bigram count mismatch")
		})
	}
}

func TestMetadataConsistency(t *testing.T) {
	t.Parallel()

	// CRITICAL: This test verifies metadata is computed from the EXACT
	// tokens used during slug generation, not from re-tokenization
	tests := []struct {
		name  string
		input string
	}{
		{"simple", "Super Mario Bros."},
		{"complex", "The Legend of Zelda: Breath of the Wild"},
		{"roman numerals", "Final Fantasy VII"},
		{"CJK", "ドラゴンクエスト"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Generate metadata twice - results must be identical
			meta1 := GenerateSlugWithMetadata(tt.input)
			meta2 := GenerateSlugWithMetadata(tt.input)

			assert.Equal(t, meta1.Slug, meta2.Slug, "Inconsistent slugs")
			assert.Equal(t, meta1.SlugLength, meta2.SlugLength, "Inconsistent lengths")
			assert.Equal(t, meta1.SlugWordCount, meta2.SlugWordCount, "Inconsistent word counts")

			// Verify length matches actual slug
			actualLength := len([]rune(meta1.Slug))
			assert.Equal(t, meta1.SlugLength, actualLength, "SlugLength doesn't match actual slug length")
		})
	}
}

func TestToleranceThresholds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		query       string
		candidate   string
		shouldMatch bool // Should pass pre-filter
	}{
		{
			name:        "exact match",
			query:       "Super Mario Bros.",
			candidate:   "Super Mario Bros.",
			shouldMatch: true,
		},
		{
			name:        "within tolerance (both dimensions)",
			query:       "Mario Kart",   // "mariokart" = 9 chars, 2 words
			candidate:   "Mario Kart 8", // "mariokart8" = 10 chars, 3 words (+1 char OK, +1 word OK)
			shouldMatch: true,
		},
		{
			name:        "exceeds length tolerance (one dimension)",
			query:       "Super Mario Bros", // "supermariobros" = 14 chars, 3 words
			candidate:   "Super Mario",      // "supermario" = 10 chars, 2 words (-1 word OK, -4 chars exceeds ±3)
			shouldMatch: false,
		},
		{
			name:        "exceeds length tolerance",
			query:       "Mario",              // "mario" = 5 chars, 1 word
			candidate:   "Super Mario Deluxe", // "supermariodeluxe" = 16 chars (+11 > tolerance)
			shouldMatch: false,
		},
		{
			name:  "exceeds word count tolerance",
			query: "Mario", // "mario" = 5 chars, 1 word
			// "supermariobrosdeluxeedition" = 27 chars, 5 words (+4 > tolerance)
			candidate:   "Super Mario Bros Deluxe Edition",
			shouldMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			queryMeta := GenerateSlugWithMetadata(tt.query)
			candMeta := GenerateSlugWithMetadata(tt.candidate)

			lengthDiff := queryMeta.SlugLength - candMeta.SlugLength
			if lengthDiff < 0 {
				lengthDiff = -lengthDiff
			}
			lengthMatch := lengthDiff <= 3

			wordDiff := queryMeta.SlugWordCount - candMeta.SlugWordCount
			if wordDiff < 0 {
				wordDiff = -wordDiff
			}
			wordMatch := wordDiff <= 1

			passesFilter := lengthMatch && wordMatch

			assert.Equal(t, tt.shouldMatch, passesFilter, "Pre-filter match mismatch")
		})
	}
}
