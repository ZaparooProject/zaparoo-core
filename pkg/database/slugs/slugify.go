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
	"golang.org/x/text/unicode/norm"
	"golang.org/x/text/width"
)

// SlugifyString converts a game title to a normalized slug for cross-platform matching.
//
// Multi-Stage Normalization Pipeline:
//   Stage 1: Unicode Normalization (Symbol Removal + NFKC + Diacritic Removal)
//            - Symbol removal: "Sonic™" → "Sonic", "Game®" → "Game" (So: Other Symbol, Sc: Currency Symbol)
//            - Compatibility normalization: "１. Game" → "1. Game", "ﬁ" → "fi"
//            - Diacritic removal: "Pokémon" → "Pokemon", "Café" → "Cafe"
//   Stage 2: Leading Number Prefix Stripping - "1. Game" / "01 - Game" → "Game"
//   Stage 3: Secondary Title Decomposition - Split on ":", " - ", or "'s "
//            Strip leading articles ("The", "A", "An") from both main and secondary titles
//            "Zelda: The Minish Cap" → "Zelda Minish Cap"
//            "Disney's The Lion King" → "Disney's Lion King"
//   Stage 4: Trailing Article Normalization - "Legend, The" → "Legend"
//   Stage 5: Conjunction Normalization - "&", "+", "'n'" variants → "and"
//            "Sonic & Knuckles" / "Rock + Roll" / "Rock 'n' Roll" → "...and..."
//   Stage 6: Metadata Stripping - "(USA) [!]" removed
//   Stage 7: Edition/Version Suffix Stripping - "Game Version" / "Game Deluxe Edition" → "Game"
//   Stage 8: Separator Normalization - Remaining separators converted to spaces
//   Stage 9: Roman Numeral Conversion - "VII" → "7"
//   Stage 10: Final Slugification - Lowercase, alphanumeric only
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
	versionSuffixRegex      = regexp.MustCompile(`\s+v[.]?(?:\d{1,3}(?:[.]\d{1,4})*|[IVX]{1,5})$`)
	leadingNumPrefixRegex   = regexp.MustCompile(`^\d+[.\s\-]+`)
	parenthesesRegex        = regexp.MustCompile(`\s*\([^)]*\)`)
	bracketsRegex           = regexp.MustCompile(`\s*\[[^\]]*\]`)
	bracesRegex             = regexp.MustCompile(`\s*\{[^}]*\}`)
	angleBracketsRegex      = regexp.MustCompile(`\s*<[^>]*>`)
	separatorsRegex         = regexp.MustCompile(`[:_\-]+`)
	nonAlphanumRegex        = regexp.MustCompile(`[^a-z0-9]+`)
	cjkRegex                = regexp.MustCompile(`[\p{Han}\p{Hiragana}\p{Katakana}\p{Hangul}\x{30FC}\x{30FB}\x{3005}]`)
	nonAlphanumKeepCJKRegex = regexp.MustCompile(`[^a-z0-9\p{Han}\p{Hiragana}\p{Katakana}\p{Hangul}\x{30FC}\x{30FB}\x{3005}]+`)
	trailingArticleRegex    = regexp.MustCompile(`(?i),\s*the\s*($|[\s:\-\(\[])`)
	plusRegex               = regexp.MustCompile(`\s+\+\s+`)
	nWithApostrophesRegex   = regexp.MustCompile(`\s+'n'\s+`)
	nWithLeftApostrophe     = regexp.MustCompile(`\s+'n\s+`)
	nWithRightApostrophe    = regexp.MustCompile(`\s+n'\s+`)
	nAloneRegex             = regexp.MustCompile(`\s+n\s+`)
	romanNumeralI           = regexp.MustCompile(`\sI($|[\s:_\-])`)
	romanNumeralOrder       = []string{
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

	// Stage 10: Final Slugification (CJK-aware)
	// Create both ASCII-only and Unicode-preserving versions
	asciiSlug := nonAlphanumRegex.ReplaceAllString(s, "")
	unicodeSlug := nonAlphanumKeepCJKRegex.ReplaceAllString(s, "")

	// Intelligent selection: If the ASCII slug is very short (< 4 chars) and
	// the original string contained CJK characters, the ASCII version is likely
	// ambiguous and not useful for matching. In this case, prefer the Unicode slug.
	//
	// This handles cases like:
	//   - "ドラゴンクエストIII" → "ドラゴンクエスト3" (not "3")
	//   - "Super Mario Bros" → "supermariobros" (not "supermariobrosスーパーマリオ")
	//   - "1942" → "1942" (game title, kept even though short)
	if len(asciiSlug) < 4 && cjkRegex.MatchString(s) {
		return strings.TrimSpace(unicodeSlug)
	}

	// For titles with meaningful Latin components, return the clean ASCII slug
	// for maximum matching compatibility across different language inputs
	return strings.TrimSpace(asciiSlug)
}

func splitAndStripArticles(s string) string {
	cleaned := strings.TrimSpace(s)

	var mainTitle, secondaryTitle string
	hasSecondary := false

	if idx := strings.Index(cleaned, ":"); idx != -1 {
		mainTitle = strings.TrimSpace(cleaned[:idx])
		secondaryTitle = strings.TrimSpace(cleaned[idx+1:])
		hasSecondary = true
	} else if idx := strings.Index(cleaned, " - "); idx != -1 {
		mainTitle = strings.TrimSpace(cleaned[:idx])
		secondaryTitle = strings.TrimSpace(cleaned[idx+3:])
		hasSecondary = true
	} else if idx := strings.Index(cleaned, "'s "); idx != -1 {
		mainTitle = strings.TrimSpace(cleaned[:idx+2])
		secondaryTitle = strings.TrimSpace(cleaned[idx+3:])
		hasSecondary = true
	} else {
		mainTitle = cleaned
	}

	mainTitle = stripLeadingArticle(mainTitle)

	if hasSecondary {
		secondaryTitle = stripLeadingArticle(secondaryTitle)
		return strings.TrimSpace(mainTitle + " " + secondaryTitle)
	}

	return mainTitle
}

// stripLeadingArticle removes leading articles ("The", "A", "An") from a string.
// This is a utility function used by both slug normalization and word-level matching.
// It preserves the original case of non-article portions.
//
// Examples:
//   - "The Legend of Zelda" → "Legend of Zelda"
//   - "A New Hope" → "New Hope"
//   - "An American Tail" → "American Tail"
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

// stripMetadataBrackets removes all bracket types (parentheses, square brackets, braces, angle brackets)
// from a string. This is used to clean metadata like region codes, dump info, and tags from game titles.
//
// Examples:
//   - "Game (USA) [!]" → "Game"
//   - "Title {Europe} <Beta>" → "Title"
func stripMetadataBrackets(s string) string {
	s = parenthesesRegex.ReplaceAllString(s, "")
	s = bracketsRegex.ReplaceAllString(s, "")
	s = bracesRegex.ReplaceAllString(s, "")
	s = angleBracketsRegex.ReplaceAllString(s, "")
	return s
}

// stripEditionAndVersionSuffixes removes edition and version suffixes from game titles.
// This includes patterns like "Deluxe Edition", "GOTY Edition", "v1.2", "vIII", etc.
//
// Examples:
//   - "Game Special Edition" → "Game"
//   - "Title v1.2" → "Title"
//   - "Final Fantasy VII Ultimate Edition" → "Final Fantasy VII"
func stripEditionAndVersionSuffixes(s string) string {
	s = editionSuffixRegex.ReplaceAllString(s, "")
	s = strings.TrimSpace(s)
	s = versionSuffixRegex.ReplaceAllString(s, "")
	s = strings.TrimSpace(s)
	return s
}

// normalizeConjunctions converts various conjunction forms to the word "and".
// Handles: "&", " + ", " 'n' ", " 'n ", " n' ", " n "
//
// Examples:
//   - "Sonic & Knuckles" → "Sonic and Knuckles"
//   - "Rock + Roll Racing" → "Rock and Roll Racing"
//   - "Rock 'n' Roll" → "Rock and Roll"
func normalizeConjunctions(s string) string {
	s = strings.ReplaceAll(s, "&", " and ")
	s = plusRegex.ReplaceAllString(s, " and ")
	s = nWithApostrophesRegex.ReplaceAllString(s, " and ")
	s = nWithLeftApostrophe.ReplaceAllString(s, " and ")
	s = nWithRightApostrophe.ReplaceAllString(s, " and ")
	s = nAloneRegex.ReplaceAllString(s, " and ")
	return s
}

// convertRomanNumerals converts Roman numerals (II-XIX) to Arabic numbers.
// Note: X is intentionally NOT converted to avoid "Mega Man X" → "Mega Man 10".
//
// Examples:
//   - "Final Fantasy VII" → "Final Fantasy 7"
//   - "Street Fighter II" → "Street Fighter 2"
//   - "Mega Man X" → "Mega Man X" (unchanged)
func convertRomanNumerals(s string) string {
	upperS := strings.ToUpper(s)
	upperS = romanNumeralI.ReplaceAllString(upperS, " 1$1")

	for _, roman := range romanNumeralOrder {
		upperS = romanNumeralPatterns[roman].ReplaceAllString(upperS, romanNumeralReplacements[roman])
	}

	return strings.ToLower(upperS)
}

// normalizeSeparators converts various separator characters (colons, underscores, hyphens) to spaces.
//
// Examples:
//   - "Zelda:Link" → "Zelda Link"
//   - "Super_Mario_Bros" → "Super Mario Bros"
func normalizeSeparators(s string) string {
	return separatorsRegex.ReplaceAllString(s, " ")
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

// normalizeInternal performs Stages 0-9 of the slug normalization pipeline.
// This function is shared by both SlugifyString and NormalizeToWords to eliminate
// code duplication and ensure consistent normalization behavior.
//
// Stages performed:
//
//	Stage 0: Width Normalization - Converts fullwidth characters to halfwidth
//	Stage 1: Unicode Normalization (Symbol Removal + NFKC + Diacritic Removal)
//	Stage 2: Leading Number Prefix Stripping
//	Stage 3: Secondary Title Decomposition and Article Stripping
//	Stage 4: Trailing Article Normalization
//	Stage 5: Conjunction Normalization
//	Stage 6: Metadata Stripping
//	Stage 7: Edition/Version Suffix Stripping
//	Stage 8: Separator Normalization
//	Stage 9: Roman Numeral Conversion
//
// Returns the normalized string with preserved spaces and case changes.
// The final Stage 10 (character filtering) is applied separately by the calling function.
func normalizeInternal(input string) string {
	s := strings.TrimSpace(input)
	if s == "" {
		return ""
	}

	// Stage 0: Width normalization
	// For CJK characters (katakana/hangul), normalize halfwidth → fullwidth for consistency
	// For ASCII characters, normalize fullwidth → halfwidth for Latin matching
	// The width.Fold transformer does exactly this: narrows Latin, widens CJK
	if normalized, _, err := transform.String(width.Fold, s); err == nil {
		s = normalized
	}

	// Stage 1: Unicode Normalization
	// Remove symbols (trademark, copyright, currency)
	symbolPredicate := runes.Predicate(func(r rune) bool {
		return unicode.Is(unicode.So, r) || unicode.Is(unicode.Sc, r)
	})
	symbolRemover := runes.Remove(symbolPredicate)
	if cleaned, _, err := transform.String(symbolRemover, s); err == nil {
		s = cleaned
	}

	// Unicode normalization: different strategies for CJK vs Latin
	if !cjkRegex.MatchString(s) {
		// Latin text: Apply NFKC compatibility normalization and diacritic removal
		s = norm.NFKC.String(s)

		// Diacritic removal for Latin scripts (Pokémon → Pokemon)
		t := transform.Chain(
			norm.NFD,
			runes.Remove(runes.In(unicode.Mn)),
			norm.NFC,
		)
		if normalized, _, err := transform.String(t, s); err == nil {
			s = normalized
		}
	} else {
		// CJK text: Only apply NFC (canonical composition)
		// This is safe and necessary to compose combining marks from width.Fold
		// Avoid NFKC (mangles katakana) and NFD+mark removal (strips dakuten/handakuten)
		s = norm.NFC.String(s)
	}

	// Stages 2-9: Text transformations
	s = leadingNumPrefixRegex.ReplaceAllString(s, "")
	s = strings.TrimSpace(s)

	s = splitAndStripArticles(s)

	if trailingArticleRegex.MatchString(s) {
		s = trailingArticleRegex.ReplaceAllString(s, "$1")
		s = strings.TrimSpace(s)
	}

	s = normalizeConjunctions(s)

	s = stripMetadataBrackets(s)
	s = strings.TrimSpace(s)

	s = stripEditionAndVersionSuffixes(s)

	s = normalizeSeparators(s)

	s = convertRomanNumerals(s)

	s = strings.TrimSpace(s)

	return s
}
