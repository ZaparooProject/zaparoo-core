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
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/width"
)

// SlugifyString converts a game title to a normalized slug for cross-platform matching.
//
// 10-Stage Normalization Pipeline:
//   Stage 1:  Width Normalization - Fullwidth→Halfwidth (ASCII), Halfwidth→Fullwidth (CJK)
//   Stage 2:  Unicode Normalization - Symbol removal, NFKC/NFC, diacritic removal
//             "Sonic™" → "Sonic", "Pokémon" → "Pokemon"
//   Stage 3:  Leading Number Prefix Stripping - "1. Game" / "01 - Game" → "Game"
//   Stage 4:  Secondary Title Decomposition - Split on ":", " - ", or "'s "
//             Strip leading articles from both main and secondary titles
//             "Zelda: The Minish Cap" → "Zelda Minish Cap"
//   Stage 5:  Trailing Article Normalization - "Legend, The" → "Legend"
//   Stage 6:  Symbol and Separator Normalization - "&"→"and", ":"→space, etc.
//   Stage 7:  Metadata Stripping - "(USA) [!]" removed
//   Stage 8:  Edition/Version Suffix Stripping - "Game Deluxe Edition" → "Game"
//   Stage 9:  Roman Numeral Conversion - "VII" → "7"
//   Stage 10: Final Slugification - Lowercase, alphanumeric (preserves CJK when detected)
//
// This function is deterministic and idempotent:
//   SlugifyString(SlugifyString(x)) == SlugifyString(x)
//
// Example:
//   SlugifyString("The Legend of Zelda: Ocarina of Time (USA) [!]")
//   → "legendofzeldaocarinaoftime"

var (
	editionSuffixRegex = regexp.MustCompile(
		`(?i)\s+(Version|Edition|GOTY\s+Edition|Game\s+of\s+the\s+Year\s+Edition|` +
			`Deluxe\s+Edition|Special\s+Edition|Definitive\s+Edition|Ultimate\s+Edition)$`,
	)
	versionSuffixRegex          = regexp.MustCompile(`\s+v[.]?(?:\d{1,3}(?:[.]\d{1,4})*|[IVX]{1,5})$`)
	leadingNumPrefixRegex       = regexp.MustCompile(`^\d+[.\s\-]+`)
	nonAlphanumRegex            = regexp.MustCompile(`[^a-z0-9]+`)
	nonAlphanumKeepUnicodeRegex = regexp.MustCompile(
		`[^a-z0-9` +
			`\p{Han}\p{Hiragana}\p{Katakana}\p{Hangul}\x{30FC}\x{30FB}\x{3005}` +
			`\p{Cyrillic}` +
			`\p{Greek}` +
			`\p{Devanagari}\p{Bengali}\p{Tamil}\p{Telugu}\p{Kannada}\p{Malayalam}` +
			`\p{Gurmukhi}\p{Gujarati}\p{Oriya}\p{Sinhala}` +
			`\p{Arabic}` +
			`\p{Hebrew}` +
			`\p{Thai}\p{Myanmar}\p{Khmer}\p{Lao}` +
			`\p{Ethiopic}` +
			`]+`,
	)
	trailingArticleRegex = regexp.MustCompile(`(?i),\s*the\s*($|[\s:\-\(\[])`)
	romanNumeralI        = regexp.MustCompile(`\sI($|[\s:_\-])`)
	romanNumeralOrder    = []string{
		"XIX", "XVIII", "XVII", "XVI", "XV", "XIV", "XIII",
		"XII", "XI", "IX", "VIII", "VII", "VI", "V", "IV", "III", "II",
	}
	romanNumeralPatterns = map[string]*regexp.Regexp{
		"XIX":   regexp.MustCompile(`\bXIX\b`),
		"XVIII": regexp.MustCompile(`\bXVIII\b`),
		"XVII":  regexp.MustCompile(`\bXVII\b`),
		"XVI":   regexp.MustCompile(`\bXVI\b`),
		"XIV":   regexp.MustCompile(`\bXIV\b`),
		"XV":    regexp.MustCompile(`\bXV\b`),
		"XIII":  regexp.MustCompile(`\bXIII\b`),
		"XII":   regexp.MustCompile(`\bXII\b`),
		"XI":    regexp.MustCompile(`\bXI\b`),
		"IX":    regexp.MustCompile(`\bIX\b`),
		"VIII":  regexp.MustCompile(`\bVIII\b`),
		"VII":   regexp.MustCompile(`\bVII\b`),
		"VI":    regexp.MustCompile(`\bVI\b`),
		"IV":    regexp.MustCompile(`\bIV\b`),
		"V":     regexp.MustCompile(`\bV\b`),
		"III":   regexp.MustCompile(`\bIII\b`),
		"II":    regexp.MustCompile(`\bII\b`),
	}
	romanNumeralReplacements = map[string]string{
		"XIX":   "19",
		"XVIII": "18",
		"XVII":  "17",
		"XVI":   "16",
		"XIV":   "14",
		"XV":    "15",
		"XIII":  "13",
		"XII":   "12",
		"XI":    "11",
		"IX":    "9",
		"VIII":  "8",
		"VII":   "7",
		"VI":    "6",
		"IV":    "4",
		"V":     "5",
		"III":   "3",
		"II":    "2",
	}
)

func SlugifyString(input string) string {
	s := normalizeInternal(input)
	if s == "" {
		return ""
	}

	// Stage 10: Final Slugification (Multi-Script Aware)
	// Create both ASCII-only and Unicode-preserving versions
	asciiSlug := nonAlphanumRegex.ReplaceAllString(s, "")
	unicodeSlug := nonAlphanumKeepUnicodeRegex.ReplaceAllString(s, "")

	// Lowercase the slugs with Turkish-aware case folding if needed
	if containsTurkishChars(s) {
		asciiSlug = strings.ToLowerSpecial(unicode.TurkishCase, asciiSlug)
		unicodeSlug = strings.ToLowerSpecial(unicode.TurkishCase, unicodeSlug)
	} else {
		asciiSlug = strings.ToLower(asciiSlug)
		unicodeSlug = strings.ToLower(unicodeSlug)
	}

	// For any title containing non-Latin characters, use the Unicode slug which preserves
	// both Latin and non-Latin portions. This enables matching on either part:
	//   - "ドラゴンクエストIII" → "ドラゴンクエスト3" (pure CJK)
	//   - "Street Fighter ストリート" → "streetfighterストリート" (mixed: searchable by either part)
	//   - "Тетрис" → "тетрис" (Cyrillic preserved)
	//   - "Super Mario Bros" → "supermariobros" (pure Latin, ASCII)
	//
	// The Unicode slug already contains both parts concatenated, so mixed-language
	// titles remain searchable by either their Latin OR non-Latin portions without
	// requiring schema changes or alternate slug columns.
	script := detectScript(s)
	if needsUnicodeSlug(script) {
		return strings.TrimSpace(unicodeSlug)
	}

	// For pure Latin titles, return the clean ASCII slug
	return strings.TrimSpace(asciiSlug)
}

// SplitTitle splits a title into main and secondary parts based on delimiter priority.
// Delimiter priority (highest to lowest): ":", " - ", "'s "
// Returns (mainTitle, secondaryTitle, hasSecondary).
// Note: For "'s " delimiter, the "'s" is retained in the main title.
//
// Examples:
//   - "Zelda: Link's Awakening" → ("Zelda", "Link's Awakening", true)
//   - "Game - Subtitle" → ("Game", "Subtitle", true)
//   - "Mario's Adventure" → ("Mario's", "Adventure", true)
//   - "Simple Title" → ("Simple Title", "", false)
func SplitTitle(title string) (mainTitle, secondaryTitle string, hasSecondary bool) {
	cleaned := strings.TrimSpace(title)

	// Delimiter priority: ":" highest, then " - ", then "'s "
	if idx := strings.Index(cleaned, ":"); idx != -1 {
		return strings.TrimSpace(cleaned[:idx]), strings.TrimSpace(cleaned[idx+1:]), true
	}
	if idx := strings.Index(cleaned, " - "); idx != -1 {
		return strings.TrimSpace(cleaned[:idx]), strings.TrimSpace(cleaned[idx+3:]), true
	}
	if idx := strings.Index(cleaned, "'s "); idx != -1 {
		// Retain "'s" in the main title
		return strings.TrimSpace(cleaned[:idx+2]), strings.TrimSpace(cleaned[idx+3:]), true
	}

	return cleaned, "", false
}

func splitAndStripArticles(s string) string {
	mainTitle, secondaryTitle, hasSecondary := SplitTitle(s)

	mainTitle = StripLeadingArticle(mainTitle)

	if hasSecondary {
		secondaryTitle = StripLeadingArticle(secondaryTitle)
		return strings.TrimSpace(mainTitle + " " + secondaryTitle)
	}

	return mainTitle
}

// StripLeadingArticle removes leading articles ("The", "A", "An") from a string.
// This is a utility function used by both slug normalization and word-level matching.
// It preserves the original case of non-article portions.
//
// Examples:
//   - "The Legend of Zelda" → "Legend of Zelda"
//   - "A New Hope" → "New Hope"
//   - "An American Tail" → "American Tail"
func StripLeadingArticle(s string) string {
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

// StripMetadataBrackets removes all bracket types (parentheses, square brackets, braces, angle brackets)
// from a string. This is used to clean metadata like region codes, dump info, and tags from game titles.
//
// Examples:
//   - "Game (USA) [!]" → "Game"
//   - "Title {Europe} <Beta>" → "Title"
//   - "Game ((nested)) [test]" → "Game"
func StripMetadataBrackets(s string) string {
	var result strings.Builder
	result.Grow(len(s))

	// Track nesting depth for each bracket type: 0=(), 1=[], 2={}, 3=<>
	depth := [4]int{}

	for _, r := range s {
		switch r {
		case '(':
			depth[0]++
		case ')':
			if depth[0] > 0 {
				depth[0]--
			}
		case '[':
			depth[1]++
		case ']':
			if depth[1] > 0 {
				depth[1]--
			}
		case '{':
			depth[2]++
		case '}':
			if depth[2] > 0 {
				depth[2]--
			}
		case '<':
			depth[3]++
		case '>':
			if depth[3] > 0 {
				depth[3]--
			}
		default:
			// Only write runes when we're not inside any brackets
			if depth[0] == 0 && depth[1] == 0 && depth[2] == 0 && depth[3] == 0 {
				result.WriteRune(r)
			}
		}
	}

	return strings.TrimSpace(result.String())
}

// StripEditionAndVersionSuffixes removes edition and version suffixes from game titles.
// This includes patterns like "Deluxe Edition", "GOTY Edition", "v1.2", "vIII", etc.
//
// Examples:
//   - "Game Special Edition" → "Game"
//   - "Title v1.2" → "Title"
//   - "Final Fantasy VII Ultimate Edition" → "Final Fantasy VII"
func StripEditionAndVersionSuffixes(s string) string {
	s = editionSuffixRegex.ReplaceAllString(s, "")
	s = strings.TrimSpace(s)
	s = versionSuffixRegex.ReplaceAllString(s, "")
	s = strings.TrimSpace(s)
	return s
}

// normalizeSymbolsAndSeparators converts conjunctions and separators to normalized forms.
// Handles conjunctions: "&", " + ", " 'n' " variants → "and"
// Handles separators: ":", "_", "-" → space
//
// Examples:
//   - "Sonic & Knuckles" → "Sonic and Knuckles"
//   - "Rock + Roll Racing" → "Rock and Roll Racing"
//   - "Zelda:Link" → "Zelda Link"
//   - "Super_Mario_Bros" → "Super Mario Bros"
//
// This is an internal function used by the normalization pipeline.
func normalizeSymbolsAndSeparators(s string) string {
	s = strings.ReplaceAll(s, "&", " and ")
	s = strings.ReplaceAll(s, " + ", " and ")
	s = strings.ReplaceAll(s, " 'n' ", " and ")
	s = strings.ReplaceAll(s, " 'n ", " and ")
	s = strings.ReplaceAll(s, " n' ", " and ")
	s = strings.ReplaceAll(s, " n ", " and ")

	return strings.Map(func(r rune) rune {
		switch r {
		case ':', '_', '-':
			return ' '
		default:
			return r
		}
	}, s)
}

// convertRomanNumerals converts Roman numerals (II-XIX) to Arabic numbers.
// Note: X is intentionally NOT converted to avoid "Mega Man X" → "Mega Man 10".
//
// Examples:
//   - "Final Fantasy VII" → "Final Fantasy 7"
//   - "Street Fighter II" → "Street Fighter 2"
//   - "Mega Man X" → "Mega Man X" (unchanged)
//
// This is an internal function used by the normalization pipeline.
func convertRomanNumerals(s string) string {
	upperS := strings.ToUpper(s)
	upperS = romanNumeralI.ReplaceAllString(upperS, " 1$1")

	for _, roman := range romanNumeralOrder {
		upperS = romanNumeralPatterns[roman].ReplaceAllString(upperS, romanNumeralReplacements[roman])
	}

	return strings.ToLower(upperS)
}

// NormalizeToWords converts a game title to a normalized form with preserved word boundaries.
// This function runs Stages 1-9 of SlugifyString but STOPS before Stage 10 (final alphanumeric collapse).
//
// The result preserves spaces between words, enabling word-level operations like:
//   - Token-based similarity matching
//   - Word sequence validation
//   - Sequel suffix detection
//   - Weighted word scoring
//
// Example:
//
//	NormalizeToWords("The Legend of Zelda: Ocarina of Time (USA)")
//	→ "legend of zelda ocarina of time"
//	→ []string{"legend", "of", "zelda", "ocarina", "of", "time"}
//
// Note: For database queries and slug matching, use SlugifyString() instead.
// This function is for scoring and ranking operations only.
func NormalizeToWords(input string) []string {
	s := normalizeInternal(input)
	if s == "" {
		return []string{}
	}

	// Final cleanup: preserve letters, numbers, and spaces for word splitting.
	// Unlike SlugifyString, we preserve spaces here to enable Fields() to work.
	// Using unicode.IsLetter and unicode.IsNumber makes this robust for CJK text.
	s = strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsNumber(r) || r == ' ' {
			return unicode.ToLower(r)
		}
		// Replace non-preserved characters with space to ensure word boundaries
		return ' '
	}, s)

	return strings.Fields(s)
}

// normalizeInternal performs Stages 1-9 of the slug normalization pipeline.
// This function is shared by both SlugifyString and NormalizeToWords to eliminate
// code duplication and ensure consistent normalization behavior.
//
// Stages performed:
//
//	Stage 1: Width Normalization - Fullwidth→Halfwidth (ASCII), Halfwidth→Fullwidth (CJK)
//	Stage 2: Unicode Normalization - Symbol removal, NFKC/NFC, diacritic removal
//	Stage 3: Leading Number Prefix Stripping
//	Stage 4: Secondary Title Decomposition and Article Stripping
//	Stage 5: Trailing Article Normalization
//	Stage 6: Symbol and Separator Normalization
//	Stage 7: Metadata Stripping
//	Stage 8: Edition/Version Suffix Stripping
//	Stage 9: Roman Numeral Conversion
//
// Returns the normalized string with preserved spaces and case changes.
// The final Stage 10 (character filtering) is applied separately by the calling function.
func normalizeInternal(input string) string {
	s := strings.TrimSpace(input)
	if s == "" {
		return ""
	}

	// Stage 1: Width Normalization
	// For CJK characters (katakana/hangul), normalize halfwidth → fullwidth for consistency
	// For ASCII characters, normalize fullwidth → halfwidth for Latin matching
	// The width.Fold transformer does exactly this: narrows Latin, widens CJK
	if normalized, _, err := transform.String(width.Fold, s); err == nil {
		s = normalized
	}

	// Stage 2: Unicode Normalization
	// Remove symbols (trademark, copyright, currency)
	symbolPredicate := runes.Predicate(func(r rune) bool {
		return unicode.Is(unicode.So, r) || unicode.Is(unicode.Sc, r)
	})
	symbolRemover := runes.Remove(symbolPredicate)
	if cleaned, _, err := transform.String(symbolRemover, s); err == nil {
		s = cleaned
	}

	// Unicode normalization: script-specific strategies
	script := detectScript(s)
	s = normalizeByScript(s, script)

	// Stages 3-9: Text transformations
	s = leadingNumPrefixRegex.ReplaceAllString(s, "")
	s = strings.TrimSpace(s)

	s = splitAndStripArticles(s)

	if trailingArticleRegex.MatchString(s) {
		s = trailingArticleRegex.ReplaceAllString(s, "$1")
		s = strings.TrimSpace(s)
	}

	s = normalizeSymbolsAndSeparators(s)

	s = StripMetadataBrackets(s)
	s = strings.TrimSpace(s)

	s = StripEditionAndVersionSuffixes(s)

	s = convertRomanNumerals(s)

	s = strings.TrimSpace(s)

	return s
}
