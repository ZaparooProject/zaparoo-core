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

package slugs

import (
	"strings"
)

// GameMatchInfo contains metadata extracted from a game title for intelligent matching.
// This structure supports multi-strategy resolution where the canonical slug may not match
// but fallback strategies (e.g., matching just the main title) can be attempted.
type GameMatchInfo struct {
	CanonicalSlug     string
	MainTitleSlug     string
	SubtitleSlug      string
	OriginalInput     string
	HasSubtitle       bool
	HasLeadingArticle bool
}

// GenerateMatchInfo analyzes a game title and extracts matching metadata.
// It detects subtitles (using colon or " - " delimiters), leading articles,
// and generates slugs for both the full title and its components.
//
// Example:
//
//	info := GenerateMatchInfo("The Legend of Zelda: Link's Awakening")
//	// info.CanonicalSlug = "legendofzeldalinksawakening"
//	// info.MainTitleSlug = "legendofzelda"
//	// info.SubtitleSlug = "linksawakening"
//	// info.HasSubtitle = true
//	// info.HasLeadingArticle = true
func GenerateMatchInfo(title string) GameMatchInfo {
	info := GameMatchInfo{
		OriginalInput: title,
	}

	cleaned := strings.TrimSpace(title)
	if strings.HasPrefix(strings.ToLower(cleaned), "the ") {
		info.HasLeadingArticle = true
		cleaned = strings.TrimPrefix(cleaned, "The ")
		cleaned = strings.TrimPrefix(cleaned, "the ")
	}

	var mainTitle, subtitle string
	if idx := strings.Index(cleaned, ":"); idx != -1 {
		mainTitle = strings.TrimSpace(cleaned[:idx])
		subtitle = strings.TrimSpace(cleaned[idx+1:])
		info.HasSubtitle = true
	} else if idx := strings.Index(cleaned, " - "); idx != -1 {
		mainTitle = strings.TrimSpace(cleaned[:idx])
		subtitle = strings.TrimSpace(cleaned[idx+3:])
		info.HasSubtitle = true
	} else {
		mainTitle = cleaned
	}

	if info.HasSubtitle {
		info.MainTitleSlug = SlugifyString(mainTitle)
		info.SubtitleSlug = SlugifyString(subtitle)
		info.CanonicalSlug = info.MainTitleSlug + info.SubtitleSlug
	} else {
		info.CanonicalSlug = SlugifyString(mainTitle)
		info.MainTitleSlug = info.CanonicalSlug
	}

	return info
}

type ProgressiveTrimCandidate struct {
	Slug          string
	WordCount     int
	IsExactMatch  bool
	IsPrefixMatch bool
}

func GenerateProgressiveTrimCandidates(title string) []ProgressiveTrimCandidate {
	cleaned := strings.TrimSpace(title)

	cleaned = parenthesesRegex.ReplaceAllString(cleaned, "")
	cleaned = bracketsRegex.ReplaceAllString(cleaned, "")
	cleaned = editionSuffixRegex.ReplaceAllString(cleaned, "")
	cleaned = strings.TrimSpace(cleaned)

	words := strings.Fields(cleaned)
	if len(words) < 3 {
		return nil
	}

	var candidates []ProgressiveTrimCandidate
	seenSlugs := make(map[string]bool)

	maxTrimCount := len(words) - 2
	if maxTrimCount > 10 {
		maxTrimCount = 10
	}

	for trimCount := 0; trimCount <= maxTrimCount; trimCount++ {
		remainingWords := words[:len(words)-trimCount]
		if len(remainingWords) < 2 {
			break
		}

		trimmedTitle := strings.Join(remainingWords, " ")
		slug := SlugifyString(trimmedTitle)

		if len(slug) < 6 {
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
