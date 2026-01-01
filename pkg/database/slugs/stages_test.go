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

	"github.com/stretchr/testify/assert"
)

// TestNormalizeWidth tests Stage 1 of the normalization pipeline
func TestNormalizeWidth(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "fullwidth ASCII letters",
			input:    "ＡＢＣＤＥＦ",
			expected: "ABCDEF",
		},
		{
			name:     "fullwidth numbers",
			input:    "１２３４５",
			expected: "12345",
		},
		{
			name:     "halfwidth katakana to fullwidth",
			input:    "ｳｴｯｼﾞ",
			expected: "ウエッジ",
		},
		{
			name:     "mixed fullwidth ASCII and normal",
			input:    "Super Ｍario １２３",
			expected: "Super Mario 123",
		},
		{
			name:     "fullwidth spaces",
			input:    "Game　Title",
			expected: "Game Title",
		},
		{
			name:     "pure ASCII unchanged",
			input:    "Super Mario Bros",
			expected: "Super Mario Bros",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "fullwidth punctuation",
			input:    "Game！？",
			expected: "Game!?",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := NormalizeWidth(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestNormalizeUnicode tests Stage 2 of the normalization pipeline
func TestNormalizeUnicode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "trademark symbol removal",
			input:    "Sonic™",
			expected: "Sonic",
		},
		{
			name:     "copyright symbol removal",
			input:    "Game©",
			expected: "Game",
		},
		{
			name:     "currency symbols removal",
			input:    "Price$100€50¥1000",
			expected: "Price100501000",
		},
		{
			name:     "diacritics removal (Latin)",
			input:    "Pokémon",
			expected: "Pokemon",
		},
		{
			name:     "multiple diacritics",
			input:    "Café Münchën",
			expected: "Cafe Munchen",
		},
		{
			name:     "ligatures normalization",
			input:    "ﬁnal ﬂight",
			expected: "final flight",
		},
		{
			name:     "CJK preserved",
			input:    "ドラゴンクエスト",
			expected: "ドラゴンクエスト",
		},
		{
			name:     "mixed Latin diacritics and CJK",
			input:    "Pokémon ポケモン",
			expected: "Pokémon ポケモン", // CJK present uses NFC, preserves diacritics
		},
		{
			name:     "pure ASCII unchanged (fast path)",
			input:    "Super Mario Bros",
			expected: "Super Mario Bros",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "Cyrillic preserved",
			input:    "Тетрис",
			expected: "Тетрис",
		},
		{
			name:     "Arabic preserved",
			input:    "العاب",
			expected: "العاب",
		},
		{
			name:     "Hebrew preserved",
			input:    "משחק",
			expected: "משחק",
		},
		{
			name:     "multiple symbols",
			input:    "Game™©®",
			expected: "Game",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := NormalizeUnicode(tt.input, nil)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestStripTrailingArticle tests Stage 4 of the normalization pipeline
func TestStripTrailingArticle(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple trailing article",
			input:    "Legend, The",
			expected: "Legend",
		},
		{
			name:     "trailing article with space",
			input:    "Mega Man, The",
			expected: "Mega Man",
		},
		{
			name:     "case insensitive",
			input:    "Story, the",
			expected: "Story",
		},
		{
			name:     "trailing article before colon",
			input:    "Game, The:",
			expected: "Game:",
		},
		{
			name:     "trailing article before dash",
			input:    "Title, The-",
			expected: "Title-",
		},
		{
			name:     "trailing article before parenthesis",
			input:    "Movie, The(",
			expected: "Movie(",
		},
		{
			name:     "trailing article before bracket",
			input:    "Series, The[",
			expected: "Series[",
		},
		{
			name:     "no trailing article",
			input:    "Super Mario Bros",
			expected: "Super Mario Bros",
		},
		{
			name:     "comma without article",
			input:    "Game, Part 2",
			expected: "Game, Part 2",
		},
		{
			name:     "article before separator",
			input:    "The Legend, The Best",
			expected: "The Legend Best",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := StripTrailingArticle(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestStageIdempotence tests that each stage is idempotent
func TestStageIdempotence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		stageFn  func(string) string
		input    string
		expected string
	}{
		{
			name:     "NormalizeWidth idempotent",
			stageFn:  NormalizeWidth,
			input:    "ＡＢＣＤＥＦ",
			expected: "ABCDEF",
		},
		{
			name:     "StripTrailingArticle idempotent",
			stageFn:  StripTrailingArticle,
			input:    "Legend, The",
			expected: "Legend",
		},
	}

	// Test NormalizeUnicode separately since it has a different signature with optional context
	unicodeTests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "NormalizeUnicode idempotent",
			input:    "Pokémon™",
			expected: "Pokemon",
		},
	}

	// Test NormalizeUnicode idempotence
	for _, tt := range unicodeTests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// Apply once
			result1 := NormalizeUnicode(tt.input, nil)
			assert.Equal(t, tt.expected, result1)

			// Apply again to verify idempotence
			result2 := NormalizeUnicode(result1, nil)
			assert.Equal(t, result1, result2, "Stage should be idempotent: f(f(x)) == f(x)")
		})
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// Apply once
			result1 := tt.stageFn(tt.input)
			assert.Equal(t, tt.expected, result1)

			// Apply again to verify idempotence
			result2 := tt.stageFn(result1)
			assert.Equal(t, result1, result2, "Stage should be idempotent: f(f(x)) == f(x)")
		})
	}
}

// TestStageComposition tests that stages can be composed in sequence
func TestStageComposition(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "width + unicode normalization",
			input:    "Ｐokémon",
			expected: "Pokemon",
		},
		{
			name:     "unicode + trailing article",
			input:    "Légend, The™",
			expected: "Legend",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// Apply stages in sequence
			result := tt.input
			result = NormalizeWidth(result)
			result = NormalizeUnicode(result, nil)
			result = StripTrailingArticle(result)

			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestStageEdgeCases tests edge cases for all stages
func TestStageEdgeCases(t *testing.T) {
	t.Parallel()

	t.Run("empty strings", func(t *testing.T) {
		t.Parallel()
		assert.Empty(t, NormalizeWidth(""))
		assert.Empty(t, NormalizeUnicode("", nil))
		assert.Empty(t, StripTrailingArticle(""))
	})

	t.Run("whitespace only", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, " ", NormalizeWidth(" "))
		assert.Equal(t, " ", NormalizeUnicode(" ", nil))
		assert.Equal(t, "   ", StripTrailingArticle("   ")) // No match, returns as-is
	})

	t.Run("very long strings", func(t *testing.T) {
		t.Parallel()
		longInput := "ＡＢＣＤＥＦ" + "X" + string(make([]byte, 1000))
		result := NormalizeWidth(longInput)
		assert.NotEmpty(t, result)
	})

	t.Run("special unicode ranges", func(t *testing.T) {
		t.Parallel()
		// Emoji (should be handled gracefully)
		assert.NotEmpty(t, NormalizeUnicode("Game 🎮", nil))

		// Zero-width characters
		assert.NotEmpty(t, NormalizeUnicode("Game\u200bTitle", nil)) // Zero-width space
	})
}

// TestNormalizeUnicodeWithContext tests NormalizeUnicode with real pipelineContext
func TestNormalizeUnicodeWithContext(t *testing.T) {
	t.Parallel()

	tests := []struct {
		setupContext        func() *pipelineContext
		expectedASCII       *bool
		name                string
		input               string
		expectedResult      string
		expectedScript      ScriptType
		expectedScriptCache bool
	}{
		{
			name:  "ASCII string with empty context",
			input: "Super Mario Bros",
			setupContext: func() *pipelineContext {
				return &pipelineContext{}
			},
			expectedResult:      "Super Mario Bros",
			expectedASCII:       nil,         // Context not modified for ASCII fast path
			expectedScript:      ScriptLatin, // ASCII defaults to Latin
			expectedScriptCache: false,
		},
		{
			name:  "ASCII string with pre-cached ASCII=true",
			input: "Super Mario Bros",
			setupContext: func() *pipelineContext {
				isASCII := true
				return &pipelineContext{isASCII: &isASCII}
			},
			expectedResult:      "Super Mario Bros",
			expectedASCII:       boolPtr(true),
			expectedScript:      ScriptLatin,
			expectedScriptCache: false,
		},
		{
			name:  "Latin with diacritics - caches script detection",
			input: "Pokémon",
			setupContext: func() *pipelineContext {
				return &pipelineContext{}
			},
			expectedResult:      "Pokemon",
			expectedASCII:       nil,
			expectedScript:      ScriptLatin,
			expectedScriptCache: true,
		},
		{
			name:  "CJK text - caches script detection",
			input: "ドラゴンクエスト",
			setupContext: func() *pipelineContext {
				return &pipelineContext{}
			},
			expectedResult:      "ドラゴンクエスト",
			expectedASCII:       nil,
			expectedScript:      ScriptCJK,
			expectedScriptCache: true,
		},
		{
			name:  "Cyrillic text - caches script detection",
			input: "Тетрис",
			setupContext: func() *pipelineContext {
				return &pipelineContext{}
			},
			expectedResult:      "Тетрис",
			expectedASCII:       nil,
			expectedScript:      ScriptCyrillic,
			expectedScriptCache: true,
		},
		{
			name:  "Arabic text - caches script detection",
			input: "العاب",
			setupContext: func() *pipelineContext {
				return &pipelineContext{}
			},
			expectedResult:      "العاب",
			expectedASCII:       nil,
			expectedScript:      ScriptArabic,
			expectedScriptCache: true,
		},
		{
			name:  "Hebrew text - caches script detection",
			input: "משחק",
			setupContext: func() *pipelineContext {
				return &pipelineContext{}
			},
			expectedResult:      "משחק",
			expectedASCII:       nil,
			expectedScript:      ScriptHebrew,
			expectedScriptCache: true,
		},
		{
			name:  "Mixed Latin/CJK - caches script detection",
			input: "Pokémon ポケモン",
			setupContext: func() *pipelineContext {
				return &pipelineContext{}
			},
			expectedResult:      "Pokémon ポケモン",
			expectedASCII:       nil,
			expectedScript:      ScriptCJK,
			expectedScriptCache: true,
		},
		{
			name:  "Symbol removal with script caching",
			input: "Game™©®",
			setupContext: func() *pipelineContext {
				return &pipelineContext{}
			},
			expectedResult:      "Game",
			expectedASCII:       nil,
			expectedScript:      ScriptLatin,
			expectedScriptCache: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := tt.setupContext()
			result := NormalizeUnicode(tt.input, ctx)

			assert.Equal(t, tt.expectedResult, result, "Result mismatch")

			if tt.expectedASCII != nil {
				assert.NotNil(t, ctx.isASCII, "Context should have isASCII set")
				assert.Equal(t, *tt.expectedASCII, *ctx.isASCII, "ASCII cache mismatch")
			}

			if tt.expectedScriptCache {
				assert.True(t, ctx.scriptCached, "Script should be cached")
				assert.Equal(t, tt.expectedScript, ctx.script, "Cached script type mismatch")
			} else {
				assert.False(t, ctx.scriptCached, "Script should not be cached")
			}
		})
	}
}

// TestNormalizeInternalContextCaching tests that normalizeInternal properly creates and populates context
func TestNormalizeInternalContextCaching(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                string
		input               string
		expectedNormalized  string
		expectedScript      ScriptType
		expectedASCII       bool
		expectedScriptCache bool
	}{
		{
			name:                "ASCII string - caches ASCII check",
			input:               "Super Mario Bros",
			expectedNormalized:  "super mario bros",
			expectedASCII:       true,
			expectedScript:      ScriptLatin, // ASCII defaults to Latin
			expectedScriptCache: false,       // ASCII fast path skips script detection
		},
		{
			name:                "Latin with diacritics - caches both",
			input:               "Pokémon",
			expectedNormalized:  "pokemon",
			expectedASCII:       false,
			expectedScript:      ScriptLatin,
			expectedScriptCache: true,
		},
		{
			name:                "CJK text - caches both",
			input:               "ドラゴンクエスト VII",
			expectedNormalized:  "ドラゴンクエスト vii",
			expectedASCII:       false,
			expectedScript:      ScriptCJK,
			expectedScriptCache: true,
		},
		{
			name:                "Mixed text - caches both",
			input:               "Final Fantasy VII",
			expectedNormalized:  "final fantasy vii",
			expectedASCII:       true,
			expectedScript:      ScriptLatin,
			expectedScriptCache: false,
		},
		{
			name:                "Fullwidth text - caches both",
			input:               "ＦＩＮＡＬ ＦＡＮＴＡＳＹ",
			expectedNormalized:  "final fantasy",
			expectedASCII:       false,
			expectedScript:      ScriptLatin,
			expectedScriptCache: false, // After width normalization, becomes ASCII
		},
		{
			name:  "Complex game title with abbreviations",
			input: "Street Fighter II: The World Warrior",
			// Article stripping now in ParseGame, not normalizeInternal
			expectedNormalized:  "street fighter ii  the world warrior",
			expectedASCII:       true,
			expectedScript:      ScriptLatin,
			expectedScriptCache: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, ctx := normalizeInternal(tt.input)

			assert.Equal(t, tt.expectedNormalized, result, "Normalized result mismatch")
			assert.NotNil(t, ctx, "Context should not be nil")
			assert.NotNil(t, ctx.isASCII, "Context should have isASCII set")
			assert.Equal(t, tt.expectedASCII, *ctx.isASCII, "ASCII cache mismatch")

			if tt.expectedScriptCache {
				assert.True(t, ctx.scriptCached, "Script should be cached")
				assert.Equal(t, tt.expectedScript, ctx.script, "Cached script type mismatch")
			} else {
				assert.Equal(t, tt.expectedScriptCache, ctx.scriptCached, "Script cache flag mismatch")
			}
		})
	}
}

// TestSlugifyContextReuse tests that Slugify properly reuses cached context
func TestSlugifyContextReuse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		input          string
		expectedSlug   string
		expectedScript ScriptType
	}{
		{
			name:           "ASCII game title",
			input:          "Super Mario Bros",
			expectedSlug:   "supermariobrothers",
			expectedScript: ScriptLatin,
		},
		{
			name:           "Latin with diacritics",
			input:          "Pokémon Red",
			expectedSlug:   "pokemonred",
			expectedScript: ScriptLatin,
		},
		{
			name:           "CJK game title",
			input:          "ドラゴンクエストVII",
			expectedSlug:   "ドラゴンクエスト7",
			expectedScript: ScriptCJK,
		},
		{
			name:           "Mixed Latin/CJK",
			input:          "Pokémon ポケモン",
			expectedSlug:   "pokémonポケモン",
			expectedScript: ScriptCJK,
		},
		{
			name:           "Roman numerals in ASCII",
			input:          "Final Fantasy VII",
			expectedSlug:   "finalfantasy7",
			expectedScript: ScriptLatin,
		},
		{
			name:           "Roman numerals in CJK",
			input:          "ドラゴンクエストVII",
			expectedSlug:   "ドラゴンクエスト7",
			expectedScript: ScriptCJK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := Slugify(MediaTypeGame, tt.input)
			assert.Equal(t, tt.expectedSlug, result, "Slugified result mismatch")

			// Verify the internal context was used correctly by checking
			// that the result matches what we'd expect from the script type
			// Note: ParseGame applies game-specific transformations first
			parsed := ParseGame(tt.input)
			normalized, ctx := normalizeInternal(parsed)
			assert.NotNil(t, ctx, "Context should be created")

			// For non-ASCII inputs, script should be cached
			if ctx.isASCII != nil && !*ctx.isASCII {
				if ctx.scriptCached {
					assert.Equal(t, tt.expectedScript, ctx.script, "Script detection mismatch")
				}
			}

			// Verify that using the context produces the same result
			script := DetectScript(normalized)
			if needsUnicodeSlug(script) {
				// Should preserve Unicode in slug (without spaces)
				expectedContent := strings.ReplaceAll(normalized, " ", "")
				assert.Equal(t, expectedContent, result, "Unicode slug should match normalized form without spaces")
			}
		})
	}
}

// TestContextNilVsPopulated verifies that passing nil context vs populated context produces same results
func TestContextNilVsPopulated(t *testing.T) {
	t.Parallel()

	tests := []string{
		"Super Mario Bros",
		"Pokémon",
		"ドラゴンクエスト",
		"Street Fighter II",
		"Final Fantasy VII: Advent Children",
		"Café Münchën",
		"Game™©®",
		"Тетрис",
		"العاب",
		"משחק",
	}

	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			t.Parallel()

			// Test with nil context
			resultNil := NormalizeUnicode(input, nil)

			// Test with empty context
			ctxEmpty := &pipelineContext{}
			resultEmpty := NormalizeUnicode(input, ctxEmpty)

			// Test with pre-populated ASCII context
			isASCII := isASCII(input)
			ctxPrePopulated := &pipelineContext{isASCII: &isASCII}
			resultPrePopulated := NormalizeUnicode(input, ctxPrePopulated)

			// All should produce the same result
			assert.Equal(t, resultNil, resultEmpty, "nil vs empty context should produce same result")
			assert.Equal(t, resultNil, resultPrePopulated, "nil vs pre-populated context should produce same result")
		})
	}
}

// TestExpandWordsAndNumbersWithContext has been removed
// This functionality is now game-specific and integrated into ParseGame
// Tests for abbreviation and number expansion are in:
// - slug_helpers_test.go: TestExpandAbbreviations, TestExpandNumberWords
// - media_parsing_test.go: TestParseGame_AbbreviationExpansion, TestParseGame_NumberWordExpansion

// boolPtr is a helper to create bool pointers for test assertions
func boolPtr(b bool) *bool {
	return &b
}
