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
	"unicode/utf8"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/slugs"
)

// SlugMetadata contains pre-filter columns computed from a slug.
// This metadata enables efficient fuzzy matching by reducing the candidate
// set before applying expensive string similarity algorithms.
type SlugMetadata struct {
	Slug          string
	SlugLength    int
	SlugWordCount int
}

// GenerateSlugWithMetadata normalizes input and computes all prefilter metadata.
// CRITICAL: Uses SlugifyWithTokens to ensure metadata matches the actual slug.
//
// The metadata is computed from the EXACT tokens extracted during the 14-stage
// normalization pipeline, not from re-tokenization. This ensures consistency
// between the slug and its metadata.
//
// Example:
//
//	metadata := GenerateSlugWithMetadata("The Legend of Zelda: Ocarina of Time")
//	metadata.Slug         → "legendofzeldaocarinaoftime"
//	metadata.SlugLength   → 26 (character count)
//	metadata.SlugWordCount → 6 (token count: legend, of, zelda, ocarina, of, time)
func GenerateSlugWithMetadata(input string) SlugMetadata {
	// Run slugification and get the tokens that were ACTUALLY used
	result := slugs.SlugifyWithTokens(input)

	return SlugMetadata{
		Slug:          result.Slug,
		SlugLength:    utf8.RuneCountInString(result.Slug),
		SlugWordCount: computeWordCount(result.Slug, result.Tokens),
	}
}

// computeWordCount returns token count for Latin, bigram count for CJK.
// This provides orthogonal filtering dimension from character length.
//
// For CJK scripts (Chinese, Japanese, Korean), uses bigram count which is
// the industry standard approach (character count - 1) used by Elasticsearch,
// Solr, and OpenSearch for CJK text tokenization.
//
// For all other scripts (Latin, Cyrillic, Greek, Arabic, etc.), uses the
// standard token count from word boundary detection.
func computeWordCount(slug string, tokens []string) int {
	// Detect script type to determine counting method
	script := slugs.DetectScript(slug)

	// For CJK scripts, use bigram count (character count - 1)
	// This is the industry standard (Elasticsearch, Solr, OpenSearch)
	if script == slugs.ScriptCJK {
		runes := []rune(slug)
		if len(runes) < 2 {
			return len(runes)
		}
		// Bigram count = character count - 1
		// Example: "ドラゴンクエスト" (8 chars) → 7 bigrams
		return len(runes) - 1
	}

	// For all other scripts (Latin, Cyrillic, Greek, Arabic, etc.), use token count
	return len(tokens)
}
