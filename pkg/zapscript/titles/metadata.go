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

package titles

import (
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/slugs"
)

const (
	// Progressive trim limits
	minProgressiveTrimSlugLength = 6
	minProgressiveTrimWordCount  = 3
)

// GameMatchInfo contains metadata extracted from a game title for intelligent matching.
// This structure supports multi-strategy resolution where the canonical slug may not match
// but fallback strategies (e.g., matching just the main title) can be attempted.
type GameMatchInfo struct {
	CanonicalSlug      string
	MainTitleSlug      string
	SecondaryTitleSlug string
	OriginalInput      string
	HasSecondaryTitle  bool
	HasLeadingArticle  bool
}

// GenerateMatchInfo analyzes a game title and extracts matching metadata.
// It detects secondary titles (using colon, " - ", or "'s " delimiters), leading articles,
// and generates slugs for both the full title and its components.
//
// The mediaType parameter should match the MediaType of the system being queried,
// ensuring consistent slugification between indexing and resolution.
//
// Example:
//
//	info := GenerateMatchInfo(slugs.MediaTypeGame, "The Legend of Zelda: Link's Awakening")
//	// info.CanonicalSlug = "legendofzeldalinksawakening"
//	// info.MainTitleSlug = "legendofzelda"
//	// info.SecondaryTitleSlug = "linksawakening"
//	// info.HasSecondaryTitle = true
//	// info.HasLeadingArticle = true
func GenerateMatchInfo(mediaType slugs.MediaType, title string) GameMatchInfo {
	info := GameMatchInfo{
		OriginalInput: title,
	}

	cleaned := strings.TrimSpace(title)
	strippedTitle := slugs.StripLeadingArticle(cleaned)
	info.HasLeadingArticle = (cleaned != strippedTitle)

	mainTitle, secondaryTitle, hasSecondary := slugs.SplitTitle(strippedTitle)
	info.HasSecondaryTitle = hasSecondary

	if info.HasSecondaryTitle {
		secondaryTitle = slugs.StripLeadingArticle(secondaryTitle)
		info.MainTitleSlug = slugs.Slugify(mediaType, mainTitle)
		info.SecondaryTitleSlug = slugs.Slugify(mediaType, secondaryTitle)
		info.CanonicalSlug = info.MainTitleSlug + info.SecondaryTitleSlug
	} else {
		info.CanonicalSlug = slugs.Slugify(mediaType, mainTitle)
		info.MainTitleSlug = info.CanonicalSlug
	}

	return info
}

// ProgressiveTrimCandidate represents a progressively trimmed title variation for matching.
type ProgressiveTrimCandidate struct {
	Slug          string
	WordCount     int
	IsExactMatch  bool
	IsPrefixMatch bool
}

// GenerateProgressiveTrimCandidates creates progressively trimmed variations of a title.
// This handles overly-verbose queries by removing words from the end one at a time.
// Useful for matching long descriptive titles against shorter canonical names.
//
// The mediaType parameter should match the MediaType of the system being queried,
// ensuring consistent slugification between indexing and resolution.
//
// Example:
//
//	GenerateProgressiveTrimCandidates(slugs.MediaTypeGame, "Super Mario World Special Edition", 3)
//	// Returns candidates for:
//	// - "Super Mario World Special Edition" (full)
//	// - "Super Mario World Special"
//	// - "Super Mario World"
//	// - "Super Mario" (stopped - only 3 iterations with maxDepth=3)
func GenerateProgressiveTrimCandidates(
	mediaType slugs.MediaType, title string, maxDepth int,
) []ProgressiveTrimCandidate {
	cleaned := strings.TrimSpace(title)

	cleaned = slugs.StripMetadataBrackets(cleaned)
	cleaned = slugs.StripEditionAndVersionSuffixes(cleaned)
	cleaned = strings.TrimSpace(cleaned)

	words := strings.Fields(cleaned)
	if len(words) < minProgressiveTrimWordCount {
		return nil
	}

	candidates := make([]ProgressiveTrimCandidate, 0)
	seenSlugs := make(map[string]bool)

	maxTrimCount := len(words) - 1
	if maxDepth > 0 && maxTrimCount > maxDepth {
		maxTrimCount = maxDepth
	}

	for trimCount := 0; trimCount <= maxTrimCount; trimCount++ {
		remainingWords := words[:len(words)-trimCount]
		if len(remainingWords) < 1 {
			break
		}

		trimmedTitle := strings.Join(remainingWords, " ")
		slug := slugs.Slugify(mediaType, trimmedTitle)

		if len(slug) < minProgressiveTrimSlugLength {
			break
		}

		if seenSlugs[slug] {
			continue
		}
		seenSlugs[slug] = true

		candidate := ProgressiveTrimCandidate{
			Slug:          slug,
			WordCount:     len(remainingWords),
			IsExactMatch:  true,
			IsPrefixMatch: false,
		}
		candidates = append(candidates, candidate)

		prefixCandidate := ProgressiveTrimCandidate{
			Slug:          slug,
			WordCount:     len(remainingWords),
			IsExactMatch:  false,
			IsPrefixMatch: true,
		}
		candidates = append(candidates, prefixCandidate)
	}

	return candidates
}
