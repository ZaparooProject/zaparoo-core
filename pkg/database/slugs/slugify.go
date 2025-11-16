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

// MediaType categorizes the type of media content being slugified.
// This determines which media-specific parsing rules are applied before slugification.
type MediaType string

const (
	// MediaTypeGame represents gaming systems (consoles, computers, arcade).
	MediaTypeGame MediaType = "Game"
	// MediaTypeMovie represents film and movie content.
	MediaTypeMovie MediaType = "Movie"
	// MediaTypeTVShow represents TV episodes and shows.
	MediaTypeTVShow MediaType = "TVShow"
	// MediaTypeMusic represents music and song content.
	MediaTypeMusic MediaType = "Music"
	// MediaTypeImage represents image files.
	MediaTypeImage MediaType = "Image"
	// MediaTypeAudio represents general audio content (audiobooks, podcasts).
	MediaTypeAudio MediaType = "Audio"
	// MediaTypeVideo represents general video content (music videos).
	MediaTypeVideo MediaType = "Video"
	// MediaTypeApplication represents application/software content.
	MediaTypeApplication MediaType = "Application"
)

// Slugify converts a media title to a normalized slug for cross-platform matching.
//
// Two-Phase Architecture:
//   Phase 1: Media-Specific Parsing (ParseWithMediaType)
//     - Applies format-specific normalization based on media type
//     - Games: Strips brackets, editions, expands abbreviations, converts roman numerals
//     - TV Shows: Normalizes episode formats (S01E02/1x02 → s01e02)
//     - Movies: (TODO) Extracts years, strips quality tags
//     - Music: (TODO) Normalizes track numbers, featured artists
//
//   Phase 2: Universal Normalization Pipeline (normalizeInternal + final filtering)
//     Stage 1-3: Width, punctuation, and unicode normalization
//     Stage 4-5: Article stripping (leading/trailing)
//     Stage 6-7: Symbol/separator normalization, period conversion
//     Stage 8: Lowercasing
//     Stage 9: Final character filtering (multi-script aware)
//
// This function is deterministic and idempotent:
//   Slugify(mt, Slugify(mt, x)) == Slugify(mt, x)
//
// Examples:
//   Slugify(MediaTypeGame, "The Legend of Zelda: Ocarina of Time (USA) [!]")
//   → "legendofzeldaocarinaoftime"
//
//   Slugify(MediaTypeTVShow, "Breaking Bad - S01E02 - Gray Matter")
//   → same as Slugify(MediaTypeTVShow, "Breaking Bad - 1x02 - Gray Matter")

// pipelineContext caches computed values across normalization stages to reduce redundant work.
// This enables optimizations like avoiding multiple ASCII checks, script detection caching,
// and combining word-level processing into a single pass.
type pipelineContext struct {
	isASCII      *bool
	script       ScriptType
	scriptCached bool
}

var (
	nonAlphanumRegex            = regexp.MustCompile(`[^a-z0-9]+`)
	nonAlphanumKeepUnicodeRegex = regexp.MustCompile(
		`[^a-z0-9` +
			`\p{Latin}` + // Latin script including diacritics (é, ñ, etc.)
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
)

// SlugifyResult contains the slug and tokens generated during slugification.
// This ensures metadata is computed from the EXACT tokens used during slug generation,
// not from re-tokenization.
type SlugifyResult struct {
	Slug   string
	Tokens []string
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
//   - Conjunction detection (" 'n' " patterns in Stage 7)
//   - Separator normalization (dash handling in Stage 7)
//   - Abbreviation expansion (word boundary detection in Stage 9)
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
// This is Stage 2 of the normalization pipeline (character-level normalization).
// Must be called BEFORE Stage 3 (Unicode normalization) and Stage 7 (symbol/separator processing).
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
// This is Stage 3 of the normalization pipeline.
// Returns the input unchanged if normalization fails or if input is pure ASCII.
//
// The optional ctx parameter enables caching optimizations during pipeline processing.
// When ctx is nil, caching is skipped (useful for standalone calls or tests).
// When ctx is provided, ASCII check and script detection results are cached for reuse.
func NormalizeUnicode(s string, ctx *pipelineContext) string {
	// Check if already marked as ASCII in context (avoids redundant check)
	if ctx != nil && ctx.isASCII != nil && *ctx.isASCII {
		return s
	}

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

	// Apply script-specific normalization and optionally cache the result
	script := DetectScript(s)
	if ctx != nil {
		ctx.script = script
		ctx.scriptCached = true
	}
	return normalizeByScript(s, script)
}


// tokenizeNormalized extracts word tokens from a normalized string (after Stage 13).
// Shared by both NormalizeToWords and SlugifyWithTokens to ensure consistency.
//
// Preserves apostrophes and hyphens that are part of words (possessives, contractions, compound words):
//   - "Link's Awakening" → ["link's", "awakening"] (2 tokens, apostrophe preserved)
//   - "Spider-Man" → ["spider-man"] (1 token, hyphen preserved)
//   - "can't stop" → ["can't", "stop"] (2 tokens, apostrophe preserved)
//
// Note: Unicode variants (curly apostrophes \u2019, Unicode hyphens \u2010/\u2011) have already
// been normalized to ASCII (' and -) by Stage 2 (NormalizePunctuation), so we only check ASCII forms.
// Note: " - " (space-hyphen-space) is already handled in Stage 5 (SplitTitle) as a separator.
func tokenizeNormalized(s string) []string {
	// Convert to lowercase and preserve word-internal characters
	result := strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			return unicode.ToLower(r)
		}
		// Preserve apostrophes - part of possessives/contractions
		// Unicode variants already normalized to ' in Stage 2
		if r == '\'' {
			return r
		}
		// Preserve hyphens - part of compound words (Spider-Man, X-Men)
		// Unicode variants already normalized to - in Stage 2
		if r == '-' {
			return r
		}
		// Everything else becomes a space (word boundary)
		return ' '
	}, s)

	// Split on spaces to get tokens
	// strings.Fields automatically handles multiple spaces and trims
	tokens := strings.Fields(result)

	// Clean up tokens: remove ONLY leading/trailing apostrophes and hyphens
	// Keep them when they're internal to the word
	cleaned := make([]string, 0, len(tokens))
	for _, token := range tokens {
		// Trim leading and trailing apostrophes and hyphens
		// NOTE: strings.Trim only removes from edges, not internal characters
		token = strings.Trim(token, "'- ")
		if token != "" {
			cleaned = append(cleaned, token)
		}
	}

	return cleaned
}

// SlugifyWithTokens performs 14-stage normalization and returns both slug and tokens.
// This is the core implementation - it returns tokens extracted DURING slug generation
// to ensure metadata is computed from the EXACT same tokenization that produces the slug.
//
// Use this function when you need both the slug and token-based metadata (e.g., word count).
// For simple slug generation, use Slugify() instead.
//
// Example:
//
//	result := SlugifyWithTokens("The Legend of Zelda: Ocarina of Time (USA)")
//	result.Slug   → "legendofzeldaocarinaoftime"
//	result.Tokens → []string{"legend", "of", "zelda", "ocarina", "of", "time"}
func SlugifyWithTokens(input string) SlugifyResult {
	// Run Stages 1-13 of normalization (via existing normalizeInternal)
	s, ctx := normalizeInternal(input)
	if s == "" {
		return SlugifyResult{Slug: "", Tokens: []string{}}
	}

	// Extract tokens using shared tokenization logic
	tokens := tokenizeNormalized(s)

	// Stage 14: Apply final character filtering to create slug
	// This is the existing Stage 14 logic from Slugify()
	asciiSlug := nonAlphanumRegex.ReplaceAllString(s, "")
	unicodeSlug := nonAlphanumKeepUnicodeRegex.ReplaceAllString(s, "")

	var slug string
	var script ScriptType
	if ctx.scriptCached {
		script = ctx.script
	} else {
		script = DetectScript(s)
	}

	if needsUnicodeSlug(script) {
		slug = strings.TrimSpace(unicodeSlug)
	} else {
		slug = strings.TrimSpace(asciiSlug)
	}

	return SlugifyResult{
		Slug:   slug,
		Tokens: tokens,
	}
}

// Slugify applies media-type-aware parsing before slugification.
// It normalizes media titles based on their type (TV shows, movies, music, etc.)
// to ensure consistent matching across different format variations.
//
// Media type should be a string matching one of the MediaType constants from systemdefs:
// "TVShow", "Movie", "Music", "Audio", "Video", "Game", "Image", "Application"
//
// For TV shows, this normalizes episode markers:
//
//	"Show - S01E02 - Title" and "Show - 1x02 - Title" both normalize to the same slug
//
// For other media types, parsing is applied based on the type, or the title
// passes through to the standard slugification pipeline.
//
// Example:
//
//	Slugify(MediaTypeTVShow, "Breaking Bad - S01E02 - Gray Matter")
//	→ same as Slugify(MediaTypeTVShow, "Breaking Bad - 1x02 - Gray Matter")
func Slugify(mediaType MediaType, input string) string {
	// Apply media-type-specific parsing before slugification
	normalized := ParseWithMediaType(input, string(mediaType))

	// Run through the standard 14-stage slugification pipeline
	result := SlugifyWithTokens(normalized)
	return result.Slug
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
// This is Stage 7 of the normalization pipeline.
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
	// NOTE: Hyphens WITHOUT spaces around them are kept (for compound words like "Spider-Man")
	// " - " (space-hyphen-space) was already handled by Stage 5 (SplitTitle)
	// Process character by character to handle context-sensitive hyphen normalization
	var result strings.Builder
	result.Grow(len(s))

	inputRunes := []rune(s)
	for i, r := range inputRunes {
		switch r {
		case ':', '_', '/', '\\', ',', ';':
			// Always convert these to spaces
			_, _ = result.WriteRune(' ')
		case '-':
			// Note: Unicode hyphen variants (\u2010, \u2011) already normalized to - in Stage 2
			// Keep hyphen if it's between letters/numbers (compound word like "Spider-Man")
			// Convert to space if it's isolated or has spaces around it
			prevIsAlnum := i > 0 &&
				(unicode.IsLetter(inputRunes[i-1]) || unicode.IsNumber(inputRunes[i-1]))
			nextIsAlnum := i < len(inputRunes)-1 &&
				(unicode.IsLetter(inputRunes[i+1]) || unicode.IsNumber(inputRunes[i+1]))

			if prevIsAlnum && nextIsAlnum {
				// Keep hyphen for compound words: "Spider-Man", "F-Zero"
				_, _ = result.WriteRune(r)
			} else {
				// Convert to space for standalone or spaced hyphens
				_, _ = result.WriteRune(' ')
			}
		default:
			_, _ = result.WriteRune(r)
		}
	}

	return result.String()
}

// NormalizeToWords converts a game title to a normalized form with preserved word boundaries.
// This function applies game-specific parsing followed by universal normalization,
// then returns word tokens for scoring and ranking operations.
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
// Note: For database queries and slug matching, use Slugify() instead.
// This function is for scoring and ranking operations only.
func NormalizeToWords(input string) []string {
	// Apply game-specific parsing first (for backward compatibility)
	parsed := ParseGame(input)
	s, _ := normalizeInternal(parsed)
	if s == "" {
		return []string{}
	}

	return tokenizeNormalized(s)
}

// normalizeInternal performs universal normalization stages of the slug pipeline.
// This function contains ONLY truly universal operations that apply to all media types.
// Media-specific normalization (games, movies, TV shows, etc.) is handled by parsers
// in ParseWithMediaType() BEFORE calling this function.
//
// The returned context enables optimizations by caching computed values like
// ASCII checks and script detection for reuse in the final stage.
//
// Universal Normalization Pipeline (execution order):
//
//	Stage 1: Width Normalization - Fullwidth↔Halfwidth conversion
//	Stage 2: Punctuation Normalization - Curly quotes, dashes → ASCII forms
//	Stage 3: Unicode Normalization - Symbols, diacritics removed; script-aware processing
//	Stage 4: Symbol/Separator Normalization - "&"→"and", "+"→"plus", etc.; separators→spaces
//	Stage 5: Period Conversion - All periods → spaces
//	Stage 6: Lowercasing - Convert to lowercase
//
// Media-specific operations (removed from this pipeline):
//   - Title splitting and article stripping - now in media parsers via SplitAndStripArticles()
//   - Trailing article removal - now in media parsers via StripTrailingArticle()
//   - Metadata bracket stripping (USA), [!] - now in ParseGame via StripMetadataBrackets()
//   - Episode format normalization - now in ParseTVShow()
//   - Edition/version suffix stripping - now in ParseGame via StripEditionAndVersionSuffixes()
//   - Abbreviation expansion - now in ParseGame via ExpandAbbreviations()
//   - Number word expansion - now in ParseGame via ExpandNumberWords()
//   - Ordinal normalization - now in ParseGame via NormalizeOrdinals()
//   - Roman numeral conversion - now in ParseGame via ConvertRomanNumerals()
//
// Returns the normalized string with preserved spaces, lowercase text, and optimization context.
// Final character filtering (Stage 7 in Slugify) is applied separately by calling function.
func normalizeInternal(input string) (string, *pipelineContext) {
	ctx := &pipelineContext{}
	s := strings.TrimSpace(input)
	if s == "" {
		return "", ctx
	}

	// Check ASCII once and cache for later stages
	isASCIIVal := isASCII(s)
	ctx.isASCII = &isASCIIVal

	// CHARACTER NORMALIZATION (Stages 1-3)
	// Note: Stages 1-3 are skipped for ASCII-only strings (optimization)
	if !isASCIIVal {
		// Stage 1: Width Normalization
		s = NormalizeWidth(s)

		// Stage 2: Punctuation Normalization (must happen before Stage 3)
		// This ensures curly quotes/dashes normalize before unicode processing
		s = NormalizePunctuation(s)

		// Stage 3: Unicode Normalization (pass context for caching)
		s = NormalizeUnicode(s, ctx)
	}

	// SEPARATOR NORMALIZATION (Stages 4-5)
	// Stage 4: Normalize Symbols and Separators (: _ - / \ , ; but NOT .)
	s = NormalizeSymbolsAndSeparators(s)

	// Stage 5: Convert all periods to spaces
	s = strings.ReplaceAll(s, ".", " ")

	// Stage 6: Lowercase conversion
	s = strings.ToLower(s)

	return strings.TrimSpace(s), ctx
}
