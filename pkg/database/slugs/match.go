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
// Example:
//
//	info := GenerateMatchInfo("The Legend of Zelda: Link's Awakening")
//	// info.CanonicalSlug = "legendofzeldalinksawakening"
//	// info.MainTitleSlug = "legendofzelda"
//	// info.SecondaryTitleSlug = "linksawakening"
//	// info.HasSecondaryTitle = true
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

	var mainTitle, secondaryTitle string
	if idx := strings.Index(cleaned, ":"); idx != -1 {
		mainTitle = strings.TrimSpace(cleaned[:idx])
		secondaryTitle = strings.TrimSpace(cleaned[idx+1:])
		info.HasSecondaryTitle = true
	} else if idx := strings.Index(cleaned, " - "); idx != -1 {
		mainTitle = strings.TrimSpace(cleaned[:idx])
		secondaryTitle = strings.TrimSpace(cleaned[idx+3:])
		info.HasSecondaryTitle = true
	} else if idx := strings.Index(cleaned, "'s "); idx != -1 {
		mainTitle = strings.TrimSpace(cleaned[:idx+2])
		secondaryTitle = strings.TrimSpace(cleaned[idx+3:])
		info.HasSecondaryTitle = true
	} else {
		mainTitle = cleaned
	}

	if info.HasSecondaryTitle {
		secondaryTitle = stripLeadingArticle(secondaryTitle)
		info.MainTitleSlug = SlugifyString(mainTitle)
		info.SecondaryTitleSlug = SlugifyString(secondaryTitle)
		info.CanonicalSlug = info.MainTitleSlug + info.SecondaryTitleSlug
	} else {
		info.CanonicalSlug = SlugifyString(mainTitle)
		info.MainTitleSlug = info.CanonicalSlug
	}

	return info
}

func stripLeadingArticle(s string) string {
	s = strings.TrimSpace(s)
	lower := strings.ToLower(s)

	if strings.HasPrefix(lower, "the ") {
		return strings.TrimSpace(s[4:])
	}
	if strings.HasPrefix(lower, "a ") {
		return strings.TrimSpace(s[2:])
	}
	if strings.HasPrefix(lower, "an ") {
		return strings.TrimSpace(s[3:])
	}

	return s
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
	cleaned = bracesRegex.ReplaceAllString(cleaned, "")
	cleaned = angleBracketsRegex.ReplaceAllString(cleaned, "")
	cleaned = editionSuffixRegex.ReplaceAllString(cleaned, "")
	cleaned = strings.TrimSpace(cleaned)

	words := strings.Fields(cleaned)
	if len(words) < 3 {
		return nil
	}

	candidates := make([]ProgressiveTrimCandidate, 0)
	seenSlugs := make(map[string]bool)

	maxTrimCount := len(words) - 1
	if maxTrimCount > 10 {
		maxTrimCount = 10
	}

	for trimCount := 0; trimCount <= maxTrimCount; trimCount++ {
		remainingWords := words[:len(words)-trimCount]
		if len(remainingWords) < 1 {
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

type PrefixMatchCandidate struct {
	Slug  string
	Score int
}

func TokenizeSlugWords(slug string) []string {
	return strings.Fields(strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == ' ' {
			return r
		}
		if r >= 'A' && r <= 'Z' {
			return r + 32
		}
		return ' '
	}, slug))
}

func StartsWithWordSequence(candidate, query []string) bool {
	if len(candidate) < len(query) {
		return false
	}
	for i, word := range query {
		if candidate[i] != word {
			return false
		}
	}
	return true
}

func ScorePrefixCandidate(querySlug, candidateSlug string) int {
	score := 0
	lowerCandidate := strings.ToLower(candidateSlug)

	if hasEditionLikeSuffix(lowerCandidate) {
		score += 100
	}

	if hasSequelLikeSuffix(lowerCandidate) {
		score -= 10
	} else {
		score += 20
	}

	lengthDiff := len(candidateSlug) - len(querySlug)
	if lengthDiff < 0 {
		lengthDiff = -lengthDiff
	}
	score -= lengthDiff

	return score
}

func hasEditionLikeSuffix(slug string) bool {
	editionPatterns := []string{
		"se", "specialedition", "remaster", "remastered",
		"directorscut", "ultimate", "gold", "goty",
		"deluxe", "definitive", "enhanced", "cd32",
		"cdtv", "aga", "missiondisk", "expansion", "addon",
	}

	for _, pattern := range editionPatterns {
		if strings.Contains(slug, pattern) {
			return true
		}
	}
	return false
}

func hasSequelLikeSuffix(slug string) bool {
	sequelPatterns := []string{
		"2", "3", "4", "5", "6", "7", "8", "9",
		"ii", "iii", "iv", "v", "vi", "vii", "viii", "ix", "x",
	}

	words := NormalizeToWords(slug)
	if len(words) == 0 {
		return false
	}

	lastWord := words[len(words)-1]
	for _, pattern := range sequelPatterns {
		if lastWord == pattern {
			return true
		}
	}
	return false
}

// ScoreTokenMatch scores how well a candidate matches a query using word-by-word matching.
// This implements a Picard-style token-based similarity algorithm with asymmetric penalties.
//
// Algorithm:
//  1. Break both query and candidate into normalized words
//  2. For each query word, find the best matching candidate word
//  3. Remove matched words from pool (prevents double-counting)
//  4. Calculate score: matched_words / (query_words + unmatched_candidate_words * 0.4)
//  5. Asymmetric penalty: unmatched words in longer string penalized at 0.4x
//
// This handles word order variations: "Link Awakening" matches "Awakening of Link"
func ScoreTokenMatch(queryTitle, candidateTitle string) float64 {
	queryWords := NormalizeToWords(queryTitle)
	candidateWords := NormalizeToWords(candidateTitle)

	if len(queryWords) == 0 || len(candidateWords) == 0 {
		return 0.0
	}

	matchedQueryWords := 0
	usedCandidateIndices := make(map[int]bool)

	for _, queryWord := range queryWords {
		bestMatchIdx := -1
		bestMatchScore := 0.0

		for candIdx, candWord := range candidateWords {
			if usedCandidateIndices[candIdx] {
				continue
			}

			score := wordSimilarity(queryWord, candWord)
			if score > bestMatchScore {
				bestMatchScore = score
				bestMatchIdx = candIdx
			}
		}

		if bestMatchIdx >= 0 && bestMatchScore > 0.8 {
			matchedQueryWords++
			usedCandidateIndices[bestMatchIdx] = true
		}
	}

	unmatchedCandidateWords := len(candidateWords) - len(usedCandidateIndices)
	denominator := float64(len(queryWords)) + float64(unmatchedCandidateWords)*0.4

	if denominator == 0 {
		return 0.0
	}

	return float64(matchedQueryWords) / denominator
}

// wordSimilarity calculates similarity between two words (0.0 to 1.0)
func wordSimilarity(word1, word2 string) float64 {
	if word1 == word2 {
		return 1.0
	}

	if len(word1) == 0 || len(word2) == 0 {
		return 0.0
	}

	shorter, longer := word1, word2
	if len(word1) > len(word2) {
		shorter, longer = word2, word1
	}

	if len(longer) > len(shorter)*2 {
		return 0.0
	}

	if hasCommonPrefix(word1, word2, 3) {
		return 0.85
	}

	return 0.0
}

// hasCommonPrefix checks if two words share a common prefix of at least minLen characters
func hasCommonPrefix(word1, word2 string, minLen int) bool {
	shorter := len(word1)
	if len(word2) < shorter {
		shorter = len(word2)
	}

	if shorter < minLen {
		return false
	}

	for i := 0; i < shorter && i < minLen; i++ {
		if word1[i] != word2[i] {
			return false
		}
	}

	return true
}
