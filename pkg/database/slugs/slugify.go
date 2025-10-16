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
//   Stage 1: Width Normalization - Fullwidth→Halfwidth (ASCII), Halfwidth→Fullwidth (CJK)
//   Stage 2: Unicode Normalization - Symbol removal, NFKC/NFC, diacritic removal
//            "Sonic™" → "Sonic", "Pokémon" → "Pokemon"
//   Stage 3: Secondary Title Decomposition - Split on ":", " - ", or "'s "
//            Strip leading articles from both main and secondary titles
//            "Zelda: The Minish Cap" → "Zelda Minish Cap"
//   Stage 4: Trailing Article Normalization - "Legend, The" → "Legend"
//   Stage 5: Symbol and Separator Normalization - "&"→"and", "+"→"plus", "vs"→"versus", etc.
//            "Mario vs DK" → "Mario versus DK", "Bros."→"Brothers", "Dr."→"Doctor"
//   Stage 6: Metadata Stripping - "(USA) [!]" removed
//   Stage 7: Edition/Version Suffix Stripping - "Game Deluxe Edition" → "Game"
//   Stage 8: Ordinal Number Normalization - "1st"→"1", "2nd"→"2", "3rd"→"3"
//   Stage 9: Roman Numeral Conversion - "VII" → "7"
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
		`(?i)\s+(version|edition|ausgabe|versione|edizione|versao|edicao|` +
			`バージョン|エディション|ヴァージョン)$`,
	)
	versionSuffixRegex          = regexp.MustCompile(`\s+v[.]?(?:\d{1,3}(?:[.]\d{1,4})*|[IVX]{1,5})$`)
	ordinalSuffixRegex          = regexp.MustCompile(`\b(\d+)(?:st|nd|rd|th)\b`)
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
)

// romanNumeralReplacementTable defines pattern-to-number mappings for roman numeral conversion.
// - X is intentionally omitted to avoid games like "Mega Man X" being converted to "Mega Man 10".
// This is basically a best effort approach to cover common misspellings but not need to have
// some exhausting full roman numeral parser.
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

// isASCII checks if a string contains only ASCII characters (bytes < 128).
// This is used for the ASCII fast-path optimization to skip expensive Unicode processing.
func isASCII(s string) bool {
	for i := range s {
		if s[i] >= 128 {
			return false
		}
	}
	return true
}

// NormalizeWidth performs width normalization on a string.
// Converts fullwidth ASCII characters to halfwidth (for Latin text processing).
// Converts halfwidth CJK characters to fullwidth (for consistent display and matching).
//
// Examples:
//   - Fullwidth ASCII: "ＡＢＣＤＥＦ" → "ABCDEF"
//   - Fullwidth numbers: "１２３" → "123"
//   - Halfwidth katakana: "ｳｴｯｼﾞ" → "ウエッジ"
//   - Mixed: "Super Ｍario １２３" → "Super Mario 123"
//
// This is Stage 1 of the normalization pipeline.
// Returns the input unchanged if normalization fails.
func NormalizeWidth(s string) string {
	if normalized, _, err := transform.String(width.Fold, s); err == nil {
		return normalized
	}
	return s
}

// NormalizePunctuation normalizes Unicode punctuation variants to their ASCII equivalents.
// This ensures consistent behavior across all pipeline stages, particularly for:
//   - Conjunction detection (" 'n' " patterns in Stage 5)
//   - Separator normalization (dash handling in Stage 5)
//   - Abbreviation expansion (word boundary detection in Stage 5)
//
// Normalized characters:
//   - Curly quotes: ' ' " " → ' "
//   - Prime marks: ′ ″ → ' "
//   - Grave/acute: ` ´ → '
//   - Dashes: – — ― − → -
//   - Ellipsis: … → ...
//
// Examples:
//   - "Link's Awakening" → "Link's Awakening" (curly apostrophe → straight)
//   - "Super–Bros." → "Super-Bros." (en dash → hyphen, enables "Bros" expansion)
//   - "Rock 'n' Roll" → "Rock 'n' Roll" (curly quotes → straight, enables conjunction)
//
// This is part of Stage 1 of the normalization pipeline (character-level normalization).
// Must be called BEFORE Stage 2 (NFKC) and Stage 5 (symbol/separator processing).
func NormalizePunctuation(s string) string {
	// Quote and apostrophe variants
	s = strings.ReplaceAll(s, "\u2018", "'")  // Left single quotation mark
	s = strings.ReplaceAll(s, "\u2019", "'")  // Right single quotation mark
	s = strings.ReplaceAll(s, "\u201C", "\"") // Left double quotation mark
	s = strings.ReplaceAll(s, "\u201D", "\"") // Right double quotation mark
	s = strings.ReplaceAll(s, "\u2032", "'")  // Prime (minute mark)
	s = strings.ReplaceAll(s, "\u2033", "\"") // Double prime (second mark)
	s = strings.ReplaceAll(s, "`", "'")       // Grave accent (often used as quote)
	s = strings.ReplaceAll(s, "\u00B4", "'")  // Acute accent (often used as apostrophe)

	// Dash variants
	s = strings.ReplaceAll(s, "\u2013", "-") // En dash
	s = strings.ReplaceAll(s, "\u2014", "-") // Em dash
	s = strings.ReplaceAll(s, "\u2015", "-") // Horizontal bar
	s = strings.ReplaceAll(s, "\u2212", "-") // Minus sign
	s = strings.ReplaceAll(s, "\u2012", "-") // Figure dash

	// Ellipsis
	s = strings.ReplaceAll(s, "\u2026", "...") // Horizontal ellipsis

	return s
}

// NormalizeUnicode performs Unicode normalization with symbol removal and script-aware processing.
// This combines several operations:
//   - Removes Unicode symbols (trademark ™, copyright ©, currency $€¥)
//   - Applies script-specific normalization (NFKC for Latin, NFC for CJK, etc.)
//   - Removes diacritics for Latin scripts (Pokémon → Pokemon)
//   - Preserves essential marks for CJK scripts
//
// Examples:
//   - Symbols: "Sonic™" → "Sonic", "Game©" → "Game"
//   - Diacritics (Latin): "Pokémon" → "Pokemon", "Café" → "Cafe"
//   - Ligatures: "ﬁnal" → "final"
//   - CJK preserved: "ドラゴンクエスト" → "ドラゴンクエスト"
//
// This is Stage 2 of the normalization pipeline.
// Returns the input unchanged if normalization fails or if input is pure ASCII.
func NormalizeUnicode(s string) string {
	// Skip Unicode processing for pure ASCII strings (optimization)
	if isASCII(s) {
		return s
	}

	// Remove Unicode symbols (trademark, copyright, currency)
	symbolPredicate := runes.Predicate(func(r rune) bool {
		return unicode.Is(unicode.So, r) || unicode.Is(unicode.Sc, r)
	})
	symbolRemover := runes.Remove(symbolPredicate)
	if cleaned, _, err := transform.String(symbolRemover, s); err == nil {
		s = cleaned
	}

	// Apply script-specific normalization
	script := detectScript(s)
	return normalizeByScript(s, script)
}

// StripTrailingArticle removes trailing articles like ", The" from the end of a string.
//
// Pattern: `, The` followed by end of string or separator characters (space, colon, dash, parenthesis, bracket)
//
// Examples:
//   - "Legend, The" → "Legend"
//   - "Mega Man, The" → "Mega Man"
//   - "Story, the:" → "Story:" (case insensitive)
//
// This is Stage 4 of the normalization pipeline.
func StripTrailingArticle(s string) string {
	if trailingArticleRegex.MatchString(s) {
		s = trailingArticleRegex.ReplaceAllString(s, "$1")
		return strings.TrimSpace(s)
	}
	return s
}

func SlugifyString(input string) string {
	s := normalizeInternal(input)
	if s == "" {
		return ""
	}

	// Stage 10: Final Slugification (Multi-Script Aware)
	// Note: s is already lowercase from Stage 9 (ConvertRomanNumerals)

	// Create both ASCII-only and Unicode-preserving versions
	// Note: Even for ASCII strings, we need both slugs for proper script detection logic
	asciiSlug := nonAlphanumRegex.ReplaceAllString(s, "")
	unicodeSlug := nonAlphanumKeepUnicodeRegex.ReplaceAllString(s, "")

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

// StripEditionAndVersionSuffixes removes edition/version words and version numbers from game titles.
// Strips standalone words ("version", "edition") and their multi-language equivalents from all titles.
// Does NOT strip semantic edition markers like "Special", "Ultimate", "Remastered" - these
// represent different products and users may want to target them specifically.
//
// Supported languages:
//   - English: version, edition
//   - German: ausgabe (edition)
//   - Italian: versione, edizione
//   - Portuguese: versao, edicao (after diacritic normalization in Stage 2)
//   - Japanese: バージョン (version), エディション (edition), ヴァージョン (version alt.)
//
// Examples:
//   - "Pokemon Red Version" → "Pokemon Red"
//   - "Game Edition" → "Game"
//   - "Super Mario Edition" → "Super Mario"
//   - "ドラゴンクエストバージョン" → "ドラゴンクエスト" (CJK)
//   - "Game Special Edition" → "Game Special" (Edition stripped, Special kept)
func StripEditionAndVersionSuffixes(s string) string {
	// Strip edition/version suffix words regardless of word count
	s = editionSuffixRegex.ReplaceAllString(s, "")
	s = strings.TrimSpace(s)

	// Version numbers (v1.0, v2.3, etc.)
	s = versionSuffixRegex.ReplaceAllString(s, "")
	s = strings.TrimSpace(s)

	return s
}

// NormalizeSymbolsAndSeparators converts conjunctions and separators to normalized forms.
// Handles conjunctions: "&", " + ", " 'n' " variants → "and"
// Handles plus symbol: "+" → "plus"
// Handles separators: ":", "_", "-", "/", "\", ",", ";" → space
// NOTE: Period "." is NOT converted here; it's handled after abbreviation expansion
//
// Examples:
//   - "Sonic & Knuckles" → "Sonic and Knuckles"
//   - "Rock + Roll Racing" → "Rock and Roll Racing"
//   - "Game+" → "Game plus"
//   - "Zelda:Link" → "Zelda Link"
//   - "Super_Mario_Bros" → "Super Mario Bros"
//   - "Game/Part\One" → "Game Part One"
//
// This is Stage 3 of the normalization pipeline.
func NormalizeSymbolsAndSeparators(s string) string {
	// Conjunction normalization
	// Note: We handle "&" and "+" which may have surrounding spaces
	s = strings.ReplaceAll(s, " & ", " and ")
	s = strings.ReplaceAll(s, "&", " and ")
	s = strings.ReplaceAll(s, " + ", " and ")
	s = strings.ReplaceAll(s, " 'n' ", " and ")
	s = strings.ReplaceAll(s, " 'n ", " and ")
	s = strings.ReplaceAll(s, " n' ", " and ")
	s = strings.ReplaceAll(s, " n ", " and ")

	// Plus symbol normalization (for titles like "Game+" or "Mario Kart 8+")
	s = strings.ReplaceAll(s, "+", " plus ")

	// Separator normalization (excluding period, which is handled after abbreviation expansion)
	s = strings.Map(func(r rune) rune {
		switch r {
		case ':', '_', '-', '/', '\\', ',', ';':
			return ' '
		default:
			return r
		}
	}, s)

	return s
}

// ExpandAbbreviations expands common abbreviations found in game titles.
// Uses word boundaries to avoid false matches (e.g., "versus" won't become "versuersus").
// Handles two types of abbreviations:
//  1. Period-required: Only expand when period is present (e.g., "feat." but not "feat")
//  2. Flexible: Expand with or without period (e.g., "vs" or "vs.")
//
// Examples:
//   - "Mario vs Donkey Kong" → "Mario versus Donkey Kong"
//   - "Super Mario Bros." → "Super Mario Brothers"
//   - "Dr. Mario" → "Doctor Mario"
//   - "St. Louis Blues" → "Saint Louis Blues"
//   - "Song feat. Artist" → "Song featuring Artist"
//   - "A great feat" → "A great feat" (not expanded)
func ExpandAbbreviations(s string) string {
	// Abbreviations that REQUIRE a period (checked first, before stripping)
	periodRequired := map[string]string{
		"feat.": "featuring", // "feat" alone is a real word (achievement)
		"no.":   "number",    // "no" alone is a word
		"st.":   "saint",     // "st" usually means "street"
	}

	// Abbreviations that work with OR without period
	withOrWithoutPeriod := map[string]string{
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

	words := strings.Fields(s)
	for i, word := range words {
		lowerWord := strings.ToLower(word)

		// First check: period-required abbreviations (before stripping period)
		if expansion, found := periodRequired[lowerWord]; found {
			words[i] = expansion
			continue
		}

		// Second check: strip period and check general abbreviations
		lowerWord = strings.TrimSuffix(lowerWord, ".")
		if expansion, found := withOrWithoutPeriod[lowerWord]; found {
			words[i] = expansion
		}
	}

	return strings.Join(words, " ")
}

// ExpandNumberWords expands number words (one, two, three, etc.) to their numeric forms.
// Handles words 1-20 in both forms:
//   - "one" or "one." → "1"
//   - "twenty" or "twenty." → "20"
//
// Examples:
//   - "Game One" → "Game 1"
//   - "Part Two" → "Part 2"
//   - "Street Fighter Two" → "Street Fighter 2"
func ExpandNumberWords(s string) string {
	words := strings.Fields(s)
	for i, word := range words {
		lowerWord := strings.ToLower(word)

		// Check number words (before period stripping)
		if expansion, found := numberWords[lowerWord]; found {
			words[i] = expansion
			continue
		}

		// Strip period and check again (e.g., "two." → "2")
		lowerWord = strings.TrimSuffix(lowerWord, ".")
		if expansion, found := numberWords[lowerWord]; found {
			words[i] = expansion
		}
	}

	return strings.Join(words, " ")
}

// NormalizeOrdinals removes ordinal suffixes from numbers.
// This allows "2nd" and "II" to both normalize to "2" for consistent matching.
//
// Examples:
//   - "Street Fighter 2nd Impact" → "Street Fighter 2 Impact"
//   - "21st Century" → "21 Century"
//   - "3rd Strike" → "3 Strike"
//
// This is Stage 8 of the normalization pipeline (after edition stripping, before roman numerals).
func NormalizeOrdinals(s string) string {
	return ordinalSuffixRegex.ReplaceAllString(s, "$1")
}

// ConvertRomanNumerals converts Roman numerals (II-XIX) to Arabic numbers.
// Note: X is intentionally NOT converted to avoid "Mega Man X" → "Mega Man 10".
//
// Examples:
//   - "Final Fantasy VII" → "Final Fantasy 7"
//   - "Street Fighter II" → "Street Fighter 2"
//   - "Mega Man X" → "Mega Man X" (unchanged)
//
// This is Stage 9 of the normalization pipeline.
func ConvertRomanNumerals(s string) string {
	// Early exit: skip processing if no Roman numeral characters present
	// Always lowercase before returning since this is the last stage of normalizeInternal
	if !strings.ContainsAny(s, "ivxIVX") {
		return strings.ToLower(s)
	}

	upperS := strings.ToUpper(s)

	var result strings.Builder
	result.Grow(len(upperS))

	// Manual scan to replace roman numerals only at word boundaries
	// A word boundary is: start of string or previous char is not a word char (letter/digit/_)
	// and end of string or next char is not a word char
	i := 0
	for i < len(upperS) {
		// Check if we're at a potential roman numeral start
		// Word boundary: start of string or previous char is not a word character
		atWordBoundary := i == 0 || !isWordChar(rune(upperS[i-1]))

		if !atWordBoundary {
			result.WriteByte(upperS[i])
			i++
			continue
		}

		// Try to match roman numerals
		matched := false
		for _, num := range romanNumeralReplacementTable {
			if i+len(num.pattern) <= len(upperS) && upperS[i:i+len(num.pattern)] == num.pattern {
				// Check word boundary after numeral
				endIdx := i + len(num.pattern)
				atEnd := endIdx == len(upperS) || !isWordChar(rune(upperS[endIdx]))

				if atEnd {
					result.WriteString(num.replacement)
					i += len(num.pattern)
					matched = true
					break
				}
			}
		}

		if !matched {
			result.WriteByte(upperS[i])
			i++
		}
	}

	return strings.ToLower(result.String())
}

// isWordChar checks if a rune is a word character (letter, digit, or underscore)
// This matches regex \w behavior for word boundaries
func isWordChar(r rune) bool {
	return (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') ||
		(r >= '0' && r <= '9') || r == '_' || unicode.IsLetter(r)
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
// Reorganized Pipeline (6 Phases):
//
//	PHASE 1: CHARACTER NORMALIZATION
//	  Stage 1: Width Normalization - Fullwidth→Halfwidth (ASCII), Halfwidth→Fullwidth (CJK)
//	  Stage 2: Unicode Normalization - Symbol removal, NFKC/NFC, diacritic removal
//	  Stage 3: Punctuation Normalization - Curly quotes, dashes to standard forms
//
//	PHASE 2: STRUCTURAL CLEANUP (remove noise, detect structure)
//	  Stage 4: Metadata Stripping - Remove brackets (USA), [!], etc.
//	  Stage 5: Edition/Version Suffix Stripping - "Game Edition" → "Game"
//	  Stage 6: Secondary Title Decomposition - Split on :, -, 's and strip articles
//	  Stage 7: Trailing Article Removal - "Legend, The" → "Legend"
//
//	PHASE 3: SEMANTIC EXPANSION (preserves punctuation for abbreviation detection)
//	  Stage 8: Expand Abbreviations - Bros. → Brothers, Dr. → Doctor (needs periods)
//	  Stage 9: Normalize Conjunctions - & → and (already in NormalizeSymbolsAndSeparators)
//
//	PHASE 4: SEPARATOR NORMALIZATION (destroy all punctuation)
//	  Stage 10: Normalize Symbols and Separators - & + conjunctions, : _ - / \ , ; separators
//	  Stage 11: Convert Periods to Space - Now safe, abbreviations already expanded
//
//	PHASE 5: NUMBER NORMALIZATION (clean word boundaries)
//	  Stage 12: Expand Number Words - one → 1, two → 2 (all separators now spaces)
//	  Stage 13: Normalize Ordinals - 1st → 1, 2nd → 2
//	  Stage 14: Convert Roman Numerals - VII → 7 (includes lowercasing)
//
// Returns the normalized string with preserved spaces and lowercase text.
// The final Stage 15 (character filtering) is applied separately by the calling function.
func normalizeInternal(input string) string {
	s := strings.TrimSpace(input)
	if s == "" {
		return ""
	}

	// PHASE 1: CHARACTER NORMALIZATION
	if !isASCII(s) {
		// Stage 1: Width Normalization (skip for ASCII-only strings)
		s = NormalizeWidth(s)

		// Stage 3: Punctuation Normalization (must happen before Phase 2)
		// This ensures curly quotes/dashes normalize before conjunction/separator detection
		s = NormalizePunctuation(s)

		// Stage 2: Unicode Normalization (skip for ASCII-only strings)
		s = NormalizeUnicode(s)
	}

	// PHASE 2: STRUCTURAL CLEANUP
	// Stage 4: Metadata Stripping (before other text processing)
	s = StripMetadataBrackets(s)
	s = strings.TrimSpace(s)

	// Stage 6: Secondary Title Decomposition and Article Stripping
	s = splitAndStripArticles(s)

	// Stage 7: Trailing Article Removal
	s = StripTrailingArticle(s)

	// PHASE 3: SEMANTIC EXPANSION (must happen BEFORE period conversion)
	// PHASE 4: SEPARATOR NORMALIZATION
	// Stage 10: Normalize Symbols and Separators (: _ - / \ , ; but NOT .)
	// This must happen BEFORE abbreviation expansion so "Bros-" becomes "Bros "
	s = NormalizeSymbolsAndSeparators(s)

	// Stage 5: Edition/Version Suffix Stripping (after separators normalized)
	// This must happen AFTER separator normalization so "Subtitle-Edition" becomes "Subtitle Edition"
	// and the regex can match the space before "Edition"
	s = StripEditionAndVersionSuffixes(s)

	// Stage 8: Expand Abbreviations (now runs after separators converted to spaces)
	// This allows "Bros-" → "Bros " → "brothers"
	// Periods are still intact for period-required abbreviations like "feat."
	s = ExpandAbbreviations(s)

	// Stage 11: Convert Periods to Space (now safe, abbreviations already expanded)
	s = strings.ReplaceAll(s, ".", " ")

	// PHASE 5: NUMBER NORMALIZATION (clean word boundaries now)
	// Stage 12: Expand Number Words (all separators now spaces, so word boundaries are clean)
	s = ExpandNumberWords(s)

	// Stage 13: Ordinal Number Normalization
	s = NormalizeOrdinals(s)

	// Stage 14: Roman Numeral Conversion (includes lowercasing)
	s = ConvertRomanNumerals(s)

	return strings.TrimSpace(s)
}
