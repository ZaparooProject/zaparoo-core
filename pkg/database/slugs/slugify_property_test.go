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

package slugs

import (
	"strings"
	"testing"
	"unicode"

	"pgregory.net/rapid"
)

// mediaTypeGen generates random MediaType values.
func mediaTypeGen() *rapid.Generator[MediaType] {
	return rapid.SampledFrom([]MediaType{
		MediaTypeGame,
		MediaTypeMovie,
		MediaTypeTVShow,
		MediaTypeMusic,
		MediaTypeImage,
		MediaTypeAudio,
		MediaTypeVideo,
		MediaTypeApplication,
	})
}

// realisticTitleGen generates strings using character sets found in real media titles.
// This focuses on characters that actually appear in game/movie/music titles,
// avoiding exotic Unicode that would never realistically occur.
func realisticTitleGen() *rapid.Generator[string] {
	//nolint:gosmopolitan // Intentional multi-script for testing international title support
	chars := []rune(
		// ASCII letters and digits
		"ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789" +
			// Common punctuation and separators
			" -:.'\"&!?(),[]" +
			// Common European diacritics (French, Spanish, German, Portuguese)
			// Note: Accented I/V/X excluded to avoid Roman numeral edge cases
			"àáâãäåæçèéêëñòóôõöøùúûüýÿ" +
			"ÀÁÂÃÄÅÆÇÈÉÊËÑÒÓÔÕÖØÙÚÛÜÝ" +
			// Nordic/Slavic special letters
			"łŁøØßðÐþÞ" +
			// Common CJK characters (Japanese game titles)
			"日本語中文韓国ドラゴンクエスト" +
			// Cyrillic (Russian)
			"АБВГДЕЖЗИЙКЛМНОПРСТУФХЦЧШЩЪЫЬЭЮЯабвгдежзийклмнопрстуфхцчшщъыьэюя" +
			// Greek (basic alphabet only)
			"ΑΒΓΔΕΖΗΘΙΚΛΜΝΞΟΠΡΣΤΥΦΧΨΩαβγδεζηθικλμνξοπρστυφχψω" +
			// Arabic sample
			"العربية" +
			// Hebrew sample
			"עברית",
	)
	return rapid.StringOfN(rapid.SampledFrom(chars), 0, 100, -1)
}

// cjkOnlyGen generates strings containing only CJK characters.
func cjkOnlyGen() *rapid.Generator[string] {
	//nolint:gosmopolitan // Intentional CJK for testing multi-script support
	chars := []rune("日本語中文韓国語ドラゴンクエストファイナルファンタジー")
	return rapid.StringOfN(rapid.SampledFrom(chars), 1, 50, -1)
}

// TestPropertySlugifyDeterministic verifies same input always produces same output.
func TestPropertySlugifyDeterministic(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		mediaType := mediaTypeGen().Draw(t, "mediaType")
		input := realisticTitleGen().Draw(t, "input")

		result1 := Slugify(mediaType, input)
		result2 := Slugify(mediaType, input)

		if result1 != result2 {
			t.Fatalf("Slugify not deterministic: %q vs %q (input=%q)",
				result1, result2, input)
		}
	})
}

// TestPropertySlugifyOutputBounded verifies output length is bounded.
// Slugs should never be dramatically larger than input due to expansions.
func TestPropertySlugifyOutputBounded(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		mediaType := mediaTypeGen().Draw(t, "mediaType")
		input := realisticTitleGen().Draw(t, "input")

		result := Slugify(mediaType, input)

		// Allow some growth for expansions like & -> and, but cap it
		// A 3x multiplier is generous - most expansions are small
		maxLen := len(input)*3 + 100
		if len(result) > maxLen {
			t.Fatalf("Slug unexpectedly large: input len=%d, output len=%d",
				len(input), len(result))
		}
	})
}

// TestPropertySlugifyEmptyInput verifies empty/whitespace inputs produce empty slugs.
func TestPropertySlugifyEmptyInput(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		mediaType := mediaTypeGen().Draw(t, "mediaType")
		// Generate whitespace-only strings
		spaces := rapid.IntRange(0, 10).Draw(t, "spaces")
		input := strings.Repeat(" ", spaces)

		result := Slugify(mediaType, input)

		if result != "" {
			t.Fatalf("Whitespace-only input should produce empty slug, got %q", result)
		}
	})
}

// TestPropertySlugifyLowercase verifies output contains only lowercase letters.
func TestPropertySlugifyLowercase(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		mediaType := mediaTypeGen().Draw(t, "mediaType")
		input := realisticTitleGen().Draw(t, "input")

		result := Slugify(mediaType, input)

		for _, r := range result {
			if unicode.IsUpper(r) {
				t.Fatalf("Slug contains uppercase: %q in %q", string(r), result)
			}
		}
	})
}

// TestPropertySlugifyNoWhitespace verifies output contains no whitespace.
func TestPropertySlugifyNoWhitespace(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		mediaType := mediaTypeGen().Draw(t, "mediaType")
		input := realisticTitleGen().Draw(t, "input")

		result := Slugify(mediaType, input)

		for _, r := range result {
			if unicode.IsSpace(r) {
				t.Fatalf("Slug contains whitespace: %q in %q", string(r), result)
			}
		}
	})
}

// TestPropertySlugifyNeverPanics verifies Slugify never panics on any input.
func TestPropertySlugifyNeverPanics(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		mediaType := mediaTypeGen().Draw(t, "mediaType")
		// Use completely random strings to stress test
		input := rapid.String().Draw(t, "input")

		// Should not panic
		_ = Slugify(mediaType, input)
	})
}

// TestPropertySlugifyWithTokensConsistent verifies SlugifyWithTokens produces
// consistent results between slug and tokens.
func TestPropertySlugifyWithTokensConsistent(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		mediaType := mediaTypeGen().Draw(t, "mediaType")
		input := realisticTitleGen().Draw(t, "input")

		result := SlugifyWithTokens(mediaType, input)
		directSlug := Slugify(mediaType, input)

		if result.Slug != directSlug {
			t.Fatalf("SlugifyWithTokens.Slug != Slugify: %q vs %q",
				result.Slug, directSlug)
		}
	})
}

// TestPropertySlugifyCJKPreserved verifies CJK characters are preserved in slugs.
func TestPropertySlugifyCJKPreserved(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		cjkInput := cjkOnlyGen().Draw(t, "cjkInput")

		result := Slugify(MediaTypeGame, cjkInput)

		// CJK slugs should preserve the characters
		if result == "" && cjkInput != "" {
			t.Fatalf("CJK input produced empty slug: input=%q", cjkInput)
		}

		// Check that result contains CJK characters or related marks
		// Note: Some CJK punctuation (ー, ・, 々) may not be in main unicode categories
		hasCJK := false
		for _, r := range result {
			// Check main CJK categories
			if unicode.Is(unicode.Han, r) ||
				unicode.Is(unicode.Hiragana, r) ||
				unicode.Is(unicode.Katakana, r) ||
				unicode.Is(unicode.Hangul, r) {
				hasCJK = true
				break
			}
			// Check CJK-related marks (prolonged sound mark ー, middle dot ・, repeat mark 々)
			if r == 0x30FC || r == 0x30FB || r == 0x3005 {
				hasCJK = true
				break
			}
		}
		if !hasCJK && cjkInput != "" {
			t.Fatalf("CJK characters not preserved: input=%q, output=%q", cjkInput, result)
		}
	})
}

// TestPropertySlugifyLatinDiacriticsStripped verifies Latin diacritics are removed.
func TestPropertySlugifyLatinDiacriticsStripped(t *testing.T) {
	t.Parallel()

	// Test specific known diacritics that should be stripped
	cases := []struct {
		input    string
		expected string
	}{
		{"Pokémon", "pokemon"},
		{"Café", "cafe"},
		{"naïve", "naive"},
		{"résumé", "resume"},
		{"Ångström", "angstrom"},
	}

	for _, tc := range cases {
		result := Slugify(MediaTypeGame, tc.input)
		if result != tc.expected {
			t.Errorf("Diacritic not stripped: %q → %q, expected %q",
				tc.input, result, tc.expected)
		}
	}
}

// TestPropertyNormalizeWidthDeterministic verifies width normalization is deterministic.
func TestPropertyNormalizeWidthDeterministic(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		input := rapid.String().Draw(t, "input")

		result1 := NormalizeWidth(input)
		result2 := NormalizeWidth(input)

		if result1 != result2 {
			t.Fatalf("NormalizeWidth not deterministic: %q vs %q", result1, result2)
		}
	})
}

// TestPropertyNormalizePunctuationDeterministic verifies punctuation normalization is deterministic.
func TestPropertyNormalizePunctuationDeterministic(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		input := rapid.String().Draw(t, "input")

		result1 := NormalizePunctuation(input)
		result2 := NormalizePunctuation(input)

		if result1 != result2 {
			t.Fatalf("NormalizePunctuation not deterministic: %q vs %q", result1, result2)
		}
	})
}

// TestPropertyNormalizeSymbolsAndSeparatorsDeterministic verifies symbol normalization is deterministic.
func TestPropertyNormalizeSymbolsAndSeparatorsDeterministic(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		input := rapid.String().Draw(t, "input")

		result1 := NormalizeSymbolsAndSeparators(input)
		result2 := NormalizeSymbolsAndSeparators(input)

		if result1 != result2 {
			t.Fatalf("NormalizeSymbolsAndSeparators not deterministic: %q vs %q", result1, result2)
		}
	})
}

// TestPropertyNormalizeToWordsDeterministic verifies NormalizeToWords is deterministic.
func TestPropertyNormalizeToWordsDeterministic(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		input := realisticTitleGen().Draw(t, "input")

		result1 := NormalizeToWords(input)
		result2 := NormalizeToWords(input)

		if len(result1) != len(result2) {
			t.Fatalf("NormalizeToWords length mismatch: %d vs %d", len(result1), len(result2))
		}
		for i := range result1 {
			if result1[i] != result2[i] {
				t.Fatalf("NormalizeToWords token mismatch at %d: %q vs %q",
					i, result1[i], result2[i])
			}
		}
	})
}

// TestPropertyNormalizeToWordsNonEmpty verifies non-empty input produces tokens.
func TestPropertyNormalizeToWordsNonEmpty(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		// Generate strings with at least some alphanumeric content
		alpha := rapid.StringMatching(`[a-zA-Z0-9]+`).Draw(t, "alpha")
		if alpha == "" {
			return // Skip if regex produced empty
		}

		result := NormalizeToWords(alpha)

		if len(result) == 0 {
			t.Fatalf("Alphanumeric input produced no tokens: %q", alpha)
		}
	})
}

// TestPropertySlugifyCyrillicPreserved verifies Cyrillic characters are preserved.
func TestPropertySlugifyCyrillicPreserved(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		chars := []rune("АБВГДЕЖЗИЙКЛМНОПРСТУФХЦЧШЩЪЫЬЭЮЯабвгдежзийклмнопрстуфхцчшщъыьэюя")
		input := rapid.StringOfN(rapid.SampledFrom(chars), 1, 20, -1).Draw(t, "cyrillic")

		result := Slugify(MediaTypeGame, input)

		if result == "" {
			t.Fatalf("Cyrillic input produced empty slug: input=%q", input)
		}

		// Check that result contains Cyrillic
		hasCyrillic := false
		for _, r := range result {
			if unicode.Is(unicode.Cyrillic, r) {
				hasCyrillic = true
				break
			}
		}
		if !hasCyrillic {
			t.Fatalf("Cyrillic characters not preserved: input=%q, output=%q", input, result)
		}
	})
}

// TestPropertySlugifyGreekPreserved verifies Greek characters are preserved.
func TestPropertySlugifyGreekPreserved(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		chars := []rune("ΑΒΓΔΕΖΗΘΙΚΛΜΝΞΟΠΡΣΤΥΦΧΨΩαβγδεζηθικλμνξοπρστυφχψω")
		input := rapid.StringOfN(rapid.SampledFrom(chars), 1, 20, -1).Draw(t, "greek")

		result := Slugify(MediaTypeGame, input)

		if result == "" {
			t.Fatalf("Greek input produced empty slug: input=%q", input)
		}

		// Check that result contains Greek
		hasGreek := false
		for _, r := range result {
			if unicode.Is(unicode.Greek, r) {
				hasGreek = true
				break
			}
		}
		if !hasGreek {
			t.Fatalf("Greek characters not preserved: input=%q, output=%q", input, result)
		}
	})
}

// TestPropertySlugifyArabicPreserved verifies Arabic characters are preserved.
func TestPropertySlugifyArabicPreserved(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		chars := []rune("العربية")
		input := rapid.StringOfN(rapid.SampledFrom(chars), 1, 20, -1).Draw(t, "arabic")

		result := Slugify(MediaTypeGame, input)

		if result == "" {
			t.Fatalf("Arabic input produced empty slug: input=%q", input)
		}

		// Check that result contains Arabic
		hasArabic := false
		for _, r := range result {
			if unicode.Is(unicode.Arabic, r) {
				hasArabic = true
				break
			}
		}
		if !hasArabic {
			t.Fatalf("Arabic characters not preserved: input=%q, output=%q", input, result)
		}
	})
}

// TestPropertySlugifyHebrewPreserved verifies Hebrew characters are preserved.
func TestPropertySlugifyHebrewPreserved(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		chars := []rune("עברית")
		input := rapid.StringOfN(rapid.SampledFrom(chars), 1, 20, -1).Draw(t, "hebrew")

		result := Slugify(MediaTypeGame, input)

		if result == "" {
			t.Fatalf("Hebrew input produced empty slug: input=%q", input)
		}

		// Check that result contains Hebrew
		hasHebrew := false
		for _, r := range result {
			if unicode.Is(unicode.Hebrew, r) {
				hasHebrew = true
				break
			}
		}
		if !hasHebrew {
			t.Fatalf("Hebrew characters not preserved: input=%q, output=%q", input, result)
		}
	})
}

// TestPropertySlugifySpecialLatinTransliterated verifies special Latin letters
// are transliterated to ASCII, not stripped.
func TestPropertySlugifySpecialLatinTransliterated(t *testing.T) {
	t.Parallel()

	// These special letters should be transliterated, not stripped
	cases := []struct {
		input       string
		shouldMatch string // substring that should appear in output
	}{
		{"Łódź", "lodz"},     // Polish ł → l
		{"Ørsted", "orsted"}, // Nordic ø → o
		{"Größe", "grosse"},  // German ß → ss
		{"Æther", "aether"},  // Ligature æ → ae
		{"Œuvre", "oeuvre"},  // Ligature œ → oe
		{"Þór", "thor"},      // Icelandic þ → th
		{"Đorđe", "dorde"},   // Croatian đ → d
	}

	for _, tc := range cases {
		result := Slugify(MediaTypeGame, tc.input)
		if result != tc.shouldMatch {
			t.Errorf("Special Latin not transliterated correctly: %q → %q, expected %q",
				tc.input, result, tc.shouldMatch)
		}
	}
}

// TestPropertySlugifySymbolExpansion verifies symbols are expanded consistently.
func TestPropertySlugifySymbolExpansion(t *testing.T) {
	t.Parallel()

	cases := []struct {
		input    string
		contains string
	}{
		{"Sonic & Knuckles", "and"},
		{"Rock + Roll", "and"},
		{"Game+", "plus"},
	}

	for _, tc := range cases {
		result := Slugify(MediaTypeGame, tc.input)
		if !strings.Contains(result, tc.contains) {
			t.Errorf("Symbol not expanded: %q → %q, should contain %q",
				tc.input, result, tc.contains)
		}
	}
}
