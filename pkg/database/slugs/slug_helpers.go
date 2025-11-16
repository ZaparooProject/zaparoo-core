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
)

// Shared helper functions for media-specific parsing.
// These functions can be called by any media parser (ParseGame, ParseMovie, ParseMusic, etc.)
// to perform common normalization operations.

var (
	editionSuffixRegex = regexp.MustCompile(
		`(?i)\s+(version|edition|ausgabe|versione|edizione|versao|edicao|` +
			`バージョン|エディション|ヴァージョン)$`,
	)
	versionSuffixRegex   = regexp.MustCompile(`\s+v[.]?(?:\d{1,3}(?:[.]\d{1,4})*|[IVX]{1,5})$`)
	ordinalSuffixRegex   = regexp.MustCompile(`\b(\d+)(?:st|nd|rd|th)\b`)
	trailingArticleRegex = regexp.MustCompile(`(?i),\s*the\s*($|[\s:\-\(\[])`)
)

// periodRequiredAbbreviations maps period-required abbreviations to their expansions
var periodRequiredAbbreviations = map[string]string{
	"feat.": "featuring", // "feat" alone is a real word (achievement)
	"no.":   "number",    // "no" alone is a word
	"st.":   "saint",     // "st" usually means "street"
}

// withOrWithoutPeriodAbbreviations maps flexible abbreviations to their expansions
var withOrWithoutPeriodAbbreviations = map[string]string{
	"vs":   "versus",    // Strong evidence: fighting games, crossovers
	"bros": "brothers",  // Strong evidence: Super Mario Bros/Brothers
	"dr":   "doctor",    // Moderate evidence: Dr. Mario / Doctor Mario
	"mr":   "mister",    // Similar pattern to dr
	"vol":  "volume",    // Serialized content: Vol. 2 / Volume 2
	"pt":   "part",      // Episodic titles: Pt. 2 / Part 2
	"ft":   "featuring", // Music: ft. Artist / featuring Artist
	"jr":   "junior",    // Common in game titles: Donkey Kong Jr. / Junior
	"sr":   "senior",    // Less common but follows same pattern as jr
}

// romanNumeralReplacementTable defines pattern-to-number mappings for roman numeral conversion.
// X is intentionally omitted to avoid conversions like "Mega Man X" → "Mega Man 10".
var romanNumeralReplacementTable = []struct{ pattern, replacement string }{
	{"XIX", "19"},
	{"XVIII", "18"},
	{"XVII", "17"},
	{"XVI", "16"},
	{"XIV", "14"},
	{"XV", "15"},
	{"XIII", "13"},
	{"XII", "12"},
	{"XI", "11"},
	{"IX", "9"},
	{"VIII", "8"},
	{"VII", "7"},
	{"VI", "6"},
	{"IV", "4"},
	{"V", "5"},
	{"III", "3"},
	{"II", "2"},
	{"I", "1"},
}

// Number words (1-20)
var numberWords = map[string]string{
	"one": "1", "two": "2", "three": "3", "four": "4", "five": "5",
	"six": "6", "seven": "7", "eight": "8", "nine": "9", "ten": "10",
	"eleven": "11", "twelve": "12", "thirteen": "13", "fourteen": "14", "fifteen": "15",
	"sixteen": "16", "seventeen": "17", "eighteen": "18", "nineteen": "19", "twenty": "20",
}

// StripMetadataBrackets removes all bracket types (parentheses, square brackets, braces, angle brackets)
// from a string. Commonly used to clean metadata like region codes, dump info, and tags.
//
// Useful for:
//   - Games: "Sonic (USA) [!]" → "Sonic"
//   - Movies: "Movie (2024) [Remastered]" → "Movie (2024)" (year preserved, quality tag removed)
//   - TV shows: "Show - S01E02 [720p]" → "Show - S01E02"
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
			depth[0]++ //nolint:gosec // G602 - array size is 4, index 0 is safe
		case ')':
			if depth[0] > 0 { //nolint:gosec // G602 - array size is 4, index 0 is safe
				depth[0]--
			}
		case '[':
			depth[1]++ //nolint:gosec // G602 - array size is 4, index 1 is safe
		case ']':
			if depth[1] > 0 { //nolint:gosec // G602 - array size is 4, index 1 is safe
				depth[1]--
			}
		case '{':
			depth[2]++ //nolint:gosec // G602 - array size is 4, index 2 is safe
		case '}':
			if depth[2] > 0 { //nolint:gosec // G602 - array size is 4, index 2 is safe
				depth[2]--
			}
		case '<':
			depth[3]++ //nolint:gosec // G602 - array size is 4, index 3 is safe
		case '>':
			if depth[3] > 0 { //nolint:gosec // G602 - array size is 4, index 3 is safe
				depth[3]--
			}
		default:
			// Only write runes when we're not inside any brackets
			//nolint:gosec // G602 - array size is 4, all indices are safe
			if depth[0] == 0 && depth[1] == 0 && depth[2] == 0 && depth[3] == 0 {
				_, _ = result.WriteRune(r)
			}
		}
	}

	return strings.TrimSpace(result.String())
}

// StripEditionAndVersionSuffixes removes edition/version words and version numbers from titles.
// Strips standalone words ("version", "edition") and their multi-language equivalents.
// Does NOT strip semantic edition markers like "Special", "Ultimate", "Remastered" - these
// represent different products and users may want to target them specifically.
//
// Useful for:
//   - Games: "Pokemon Red Version" → "Pokemon Red"
//   - Applications: "Photoshop v2024" → "Photoshop"
//   - Movies: "Blade Runner Director's Cut Edition" → "Blade Runner Director's Cut"
//
// Supported languages:
//   - English: version, edition
//   - German: ausgabe (edition)
//   - Italian: versione, edizione
//   - Portuguese: versao, edicao (after diacritic normalization)
//   - Japanese: バージョン (version), エディション (edition), ヴァージョン (version alt.)
//
// Examples:
//   - "Pokemon Red Version" → "Pokemon Red"
//   - "Game Edition" → "Game"
//   - "Super Mario Edition" → "Super Mario"
//   - "ドラゴンクエストバージョン" → "ドラゴンクエスト" (CJK)
//   - "Game Special Edition" → "Game Special" (Edition stripped, Special kept)
func StripEditionAndVersionSuffixes(s string) string {
	// Strip edition/version suffix words
	s = editionSuffixRegex.ReplaceAllString(s, "")
	s = strings.TrimSpace(s)

	// Strip version numbers (v1.0, v2.3, vII, etc.)
	s = versionSuffixRegex.ReplaceAllString(s, "")
	s = strings.TrimSpace(s)

	return s
}

// checkAbbreviation checks if a word (in lowercase) matches a known abbreviation.
// Returns (expansion, found).
func checkAbbreviation(lowerWord string) (string, bool) {
	// First check: period-required abbreviations (before stripping period)
	if expansion, found := periodRequiredAbbreviations[lowerWord]; found {
		return expansion, true
	}

	// Second check: strip period and check general abbreviations
	lowerWord = strings.TrimSuffix(lowerWord, ".")
	if expansion, found := withOrWithoutPeriodAbbreviations[lowerWord]; found {
		return expansion, true
	}

	return "", false
}

// ExpandAbbreviations expands common abbreviations found in titles.
// Uses word boundaries to avoid false matches (e.g., "versus" won't become "versuersus").
// Handles two types of abbreviations:
//  1. Period-required: Only expand when period is present (e.g., "feat." but not "feat")
//  2. Flexible: Expand with or without period (e.g., "vs" or "vs.")
//
// Useful for:
//   - Games: "Super Mario Bros." → "Super Mario Brothers", "Mario vs DK" → "Mario versus DK"
//   - Music: "Song feat. Artist" → "Song featuring Artist"
//   - Movies: "Dr. Strangelove" → "Doctor Strangelove"
//
// Examples:
//   - "Mario vs Donkey Kong" → "Mario versus Donkey Kong"
//   - "Super Mario Bros." → "Super Mario Brothers"
//   - "Dr. Mario" → "Doctor Mario"
//   - "St. Louis Blues" → "Saint Louis Blues"
//   - "Song feat. Artist" → "Song featuring Artist"
//   - "A great feat" → "A great feat" (not expanded - no period)
func ExpandAbbreviations(s string) string {
	words := strings.Fields(s)
	for i, word := range words {
		lowerWord := strings.ToLower(word)
		if expansion, found := checkAbbreviation(lowerWord); found {
			words[i] = expansion
		}
	}
	return strings.Join(words, " ")
}

// checkNumberWord checks if a word (in lowercase) matches a known number word.
// Returns (expansion, found).
func checkNumberWord(lowerWord string) (string, bool) {
	// Check number words (before period stripping)
	if expansion, found := numberWords[lowerWord]; found {
		return expansion, true
	}

	// Strip period and check again (e.g., "two." → "2")
	lowerWord = strings.TrimSuffix(lowerWord, ".")
	if expansion, found := numberWords[lowerWord]; found {
		return expansion, true
	}

	return "", false
}

// ExpandNumberWords expands number words (one, two, three, etc.) to their numeric forms.
// Handles words 1-20 in both forms:
//   - "one" or "one." → "1"
//   - "twenty" or "twenty." → "20"
//
// Useful for:
//   - Games: "Street Fighter Two" → "Street Fighter 2"
//   - Movies: "Ocean's Eleven" → "Ocean's 11"
//   - TV: "Chapter One" → "Chapter 1"
//
// Examples:
//   - "Game One" → "Game 1"
//   - "Part Two" → "Part 2"
//   - "Street Fighter Two" → "Street Fighter 2"
func ExpandNumberWords(s string) string {
	words := strings.Fields(s)
	for i, word := range words {
		lowerWord := strings.ToLower(word)
		if expansion, found := checkNumberWord(lowerWord); found {
			words[i] = expansion
		}
	}
	return strings.Join(words, " ")
}

// NormalizeOrdinals removes ordinal suffixes from numbers.
// This allows "2nd" and "II" to both normalize to "2" for consistent matching.
//
// Useful for:
//   - Games: "Sonic the Hedgehog 2nd" → "Sonic the Hedgehog 2"
//   - Movies: "21st Century" → "21 Century"
//
// Examples:
//   - "Street Fighter 2nd Impact" → "Street Fighter 2 Impact"
//   - "21st Century" → "21 Century"
//   - "3rd Strike" → "3 Strike"
func NormalizeOrdinals(s string) string {
	return ordinalSuffixRegex.ReplaceAllString(s, "$1")
}

// ConvertRomanNumerals converts Roman numerals (II-XIX) to Arabic numbers.
// Note: X is intentionally NOT converted to avoid "Mega Man X" → "Mega Man 10".
//
// Useful for:
//   - Games: "Final Fantasy VII" → "Final Fantasy 7", "Street Fighter II" → "Street Fighter 2"
//   - Movies: "Rocky III" → "Rocky 3"
//   - Music: "Symphony No. IX" → "Symphony No. 9"
//
// Examples:
//   - "Final Fantasy VII" → "Final Fantasy 7"
//   - "Street Fighter II" → "Street Fighter 2"
//   - "Mega Man X" → "Mega Man X" (unchanged - X preserved)
//
// Optimization: Performs case-insensitive matching without full-string case conversions,
// converting to lowercase directly during output.
func ConvertRomanNumerals(s string) string {
	// Early exit: skip processing if no Roman numeral characters present
	// Always lowercase before returning
	if !strings.ContainsAny(s, "ivxIVX") {
		return strings.ToLower(s)
	}

	var result strings.Builder
	result.Grow(len(s))

	// Convert to rune slice to handle UTF-8 properly (e.g., CJK characters)
	runeSlice := []rune(s)

	// Manual scan to replace roman numerals only at Latin word boundaries.
	// We use isLatinWordCharForRoman which only considers ASCII letters/digits as word chars,
	// allowing Roman numerals to convert even when adjacent to CJK text.
	i := 0
	for i < len(runeSlice) {
		// Check if we're at a potential roman numeral start
		// Word boundary: start of string or previous char is not a Latin word character
		atWordBoundary := i == 0 || !isLatinWordCharForRoman(runeSlice[i-1])

		// Additional check: if we're in a Latin word with diacritics, don't convert roman numerals
		// This prevents "Václav" → "5aclav" and "Şişli" → "Ş1şli"
		// while still allowing "ドラゴンクエストVII" → "ドラゴンクエスト7"
		if i > 0 && unicode.Is(unicode.Latin, runeSlice[i-1]) && !isLatinWordCharForRoman(runeSlice[i-1]) {
			atWordBoundary = false
		}
		// Also check if the NEXT character is a Latin diacritic (for cases like "Václav")
		if i < len(runeSlice)-1 && unicode.Is(unicode.Latin, runeSlice[i+1]) && !isLatinWordCharForRoman(runeSlice[i+1]) {
			atWordBoundary = false
		}

		if !atWordBoundary {
			_, _ = result.WriteRune(unicode.ToLower(runeSlice[i]))
			i++
			continue
		}

		// Try to match roman numerals (case-insensitive)
		matched := false
		for _, num := range romanNumeralReplacementTable {
			if matchesRomanNumeralPattern(runeSlice, i, num.pattern) {
				// Check word boundary after numeral
				endIdx := i + len(num.pattern)
				atEnd := endIdx == len(runeSlice) || !isLatinWordCharForRoman(runeSlice[endIdx])

				if atEnd {
					_, _ = result.WriteString(num.replacement)
					i += len(num.pattern)
					matched = true
					break
				}
			}
		}

		if !matched {
			_, _ = result.WriteRune(unicode.ToLower(runeSlice[i]))
			i++
		}
	}

	return result.String()
}

// matchesRomanNumeralPattern performs a case-insensitive comparison of rune slice
// elements at the given position against a Roman numeral pattern string.
func matchesRomanNumeralPattern(runeSlice []rune, pos int, pattern string) bool {
	patternRunes := []rune(pattern)
	if pos+len(patternRunes) > len(runeSlice) {
		return false
	}
	// Case-insensitive comparison
	for i, p := range patternRunes {
		if unicode.ToUpper(runeSlice[pos+i]) != unicode.ToUpper(p) {
			return false
		}
	}
	return true
}

// isLatinWordCharForRoman checks if a rune is a Latin word character for Roman numeral boundary detection.
// Only ASCII letters, digits, and underscore are considered word chars for Roman numerals.
// CJK and other scripts are NOT considered word chars, allowing Roman numerals to be converted
// even when adjacent to non-Latin text (e.g., "ドラゴンクエストVII" → "ドラゴンクエスト7").
func isLatinWordCharForRoman(r rune) bool {
	return (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') ||
		(r >= '0' && r <= '9') || r == '_'
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

// SplitAndStripArticles splits a title into main and secondary parts, then strips leading articles from both.
// This combines title splitting and article removal into a single operation.
//
// Delimiter priority (highest to lowest): ":", " - ", "'s "
// Note: For "'s " delimiter, the "'s" is retained in the main title.
//
// Examples:
//   - "The Legend of Zelda: Link's Awakening" → "Legend of Zelda Link's Awakening"
//   - "The Game - A Subtitle" → "Game Subtitle"
//   - "Mario's Adventure" → "Mario's Adventure" (no leading article)
//
// This function is shared by all media parsers to ensure consistent article handling.
func SplitAndStripArticles(s string) string {
	cleaned := strings.TrimSpace(s)

	// Delimiter priority: ":" highest, then " - ", then "'s "
	var mainTitle, secondaryTitle string
	var hasSecondary bool

	if idx := strings.Index(cleaned, ":"); idx != -1 {
		mainTitle = strings.TrimSpace(cleaned[:idx])
		secondaryTitle = strings.TrimSpace(cleaned[idx+1:])
		hasSecondary = true
	} else if idx := strings.Index(cleaned, " - "); idx != -1 {
		mainTitle = strings.TrimSpace(cleaned[:idx])
		secondaryTitle = strings.TrimSpace(cleaned[idx+3:])
		hasSecondary = true
	} else if idx := strings.Index(cleaned, "'s "); idx != -1 {
		// Retain "'s" in the main title
		mainTitle = strings.TrimSpace(cleaned[:idx+2])
		secondaryTitle = strings.TrimSpace(cleaned[idx+3:])
		hasSecondary = true
	} else {
		mainTitle = cleaned
	}

	mainTitle = StripLeadingArticle(mainTitle)

	if hasSecondary {
		secondaryTitle = StripLeadingArticle(secondaryTitle)
		return strings.TrimSpace(mainTitle + " " + secondaryTitle)
	}

	return mainTitle
}

// StripTrailingArticle removes trailing articles like ", The" from the end of a string.
//
// Pattern: `, The` followed by end of string or separator characters (space, colon, dash, parenthesis, bracket)
//
// Examples:
//   - "Legend, The" → "Legend"
//   - "Mega Man, The" → "Mega Man"
//   - "Story, the:" → "Story:" (case insensitive)
func StripTrailingArticle(s string) string {
	if trailingArticleRegex.MatchString(s) {
		s = trailingArticleRegex.ReplaceAllString(s, "$1")
		return strings.TrimSpace(s)
	}
	return s
}
