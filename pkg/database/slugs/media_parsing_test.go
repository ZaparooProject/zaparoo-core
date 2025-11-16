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
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestParseTVShow_EpisodeFormatNormalization tests that different episode formats
// normalize to the same canonical format (s##e##)
func TestParseTVShow_EpisodeFormatNormalization(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     string
		wantMatch []string // All these should produce the same normalized output
	}{
		{
			name:  "S01E02 uppercase",
			input: "Breaking Bad - S01E02 - Gray Matter",
			wantMatch: []string{
				"Breaking Bad - s01e02 - Gray Matter",
				"Breaking Bad - 1x02 - Gray Matter",
				"Breaking Bad - 1X02 - Gray Matter",
				"Breaking Bad - 01x02 - Gray Matter",
			},
		},
		{
			name:  "1x02 format",
			input: "Attack on Titan - 1x02 - That Day",
			wantMatch: []string{
				"Attack on Titan - S01E02 - That Day",
				"Attack on Titan - s01e02 - That Day",
				"Attack on Titan - 01x02 - That Day",
			},
		},
		{
			name:  "Lowercase s01e02",
			input: "Game of Thrones - s01e02 - The Kingsroad",
			wantMatch: []string{
				"Game of Thrones - S01E02 - The Kingsroad",
				"Game of Thrones - 1x02 - The Kingsroad",
				"Game of Thrones - 1X02 - The Kingsroad",
			},
		},
		{
			name:  "Zero-padded 01x02",
			input: "The Mandalorian - 01x02 - The Child",
			wantMatch: []string{
				"The Mandalorian - S01E02 - The Child",
				"The Mandalorian - s01e02 - The Child",
				"The Mandalorian - 1x02 - The Child",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Parse the input
			result := ParseTVShow(tt.input)

			// Parse all variations that should match
			for _, variant := range tt.wantMatch {
				variantResult := ParseTVShow(variant)

				// After normalization, they should all be the same
				assert.Equal(t, result, variantResult,
					"Normalized formats should match:\n  Input: %q → %q\n  Variant: %q → %q",
					tt.input, result, variant, variantResult)
			}
		})
	}
}

// TestParseTVShow_BatoceraIssue specifically tests the Batocera duplicate notification issue
// where "Show - S01E02 - Title" and "Show - 1x02 - Title" were treated as different items
func TestParseTVShow_BatoceraIssue(t *testing.T) {
	t.Parallel()

	// These are the exact formats that caused the Batocera duplicate issue
	batoceraFormat := "Attack on Titan - S01E02 - That Day"
	mediaDBFormat := "Attack on Titan - 1x02. That Day"

	result1 := ParseTVShow(batoceraFormat)
	result2 := ParseTVShow(mediaDBFormat)

	// After parsing, the episode markers should be normalized
	assert.Contains(t, result1, "s01e02", "S01E02 should normalize to s01e02")
	assert.Contains(t, result2, "s01e02", "1x02 should normalize to s01e02")

	// The normalized forms should be very similar (episode marker is the same)
	// Note: Full slug matching will be tested in integration tests with the full pipeline
}

// TestParseTVShow_MultiEpisode tests multi-episode format handling
func TestParseTVShow_MultiEpisode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		wantHave string // What the normalized output should contain
	}{
		{
			name:     "S01E01-E02 format",
			input:    "Show - S01E01-E02 - Two-Parter",
			wantHave: "s01e01e02",
		},
		{
			name:     "S01E01E02 format (no dash)",
			input:    "Show - S01E01E02 - Two-Parter",
			wantHave: "s01e01e02",
		},
		{
			name:     "1x01-02 format",
			input:    "Show - 1x01-02 - Two-Parter",
			wantHave: "s01e01e02",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := ParseTVShow(tt.input)
			assert.Contains(t, result, tt.wantHave,
				"Multi-episode format should normalize to %s in %q", tt.wantHave, result)
		})
	}
}

// TestParseTVShow_SpecialEpisodes tests special episode handling (S00E##)
func TestParseTVShow_SpecialEpisodes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		wantHave string
	}{
		{
			name:     "S00E01 special",
			input:    "Show - S00E01 - Christmas Special",
			wantHave: "s00e01",
		},
		{
			name:     "0x01 special",
			input:    "Show - 0x01 - Pilot",
			wantHave: "s00e01",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := ParseTVShow(tt.input)
			assert.Contains(t, result, tt.wantHave,
				"Special episode format should normalize to %s in %q", tt.wantHave, result)
		})
	}
}

// TestParseTVShow_NoEpisodeMarker tests that ParseTVShow normalizes titles even without episode markers
// (article stripping and title splitting still applies)
func TestParseTVShow_NoEpisodeMarker(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Just show name",
			input:    "Breaking Bad",
			expected: "Breaking Bad",
		},
		{
			name:     "Show with season only",
			input:    "Breaking Bad - Season 1",
			expected: "Breaking Bad Season 1", // " - " removed by SplitAndStripArticles
		},
		{
			name:     "Show with description",
			input:    "Breaking Bad - The Complete Series",
			expected: "Breaking Bad Complete Series", // Split on " - ", "The" stripped from secondary title
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := ParseTVShow(tt.input)
			assert.Equal(t, tt.expected, result,
				"ParseTVShow should normalize titles (article stripping, title splitting)")
		})
	}
}

// TestParseWithMediaType tests the media-type-aware parsing dispatcher
func TestParseWithMediaType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     string
		mediaType string
		wantHave  string
	}{
		{
			name:      "TV show with S01E02",
			input:     "Show - S01E02 - Title",
			mediaType: "TVShow",
			wantHave:  "s01e02",
		},
		{
			name:      "TV show with 1x02",
			input:     "Show - 1x02 - Title",
			mediaType: "TVShow",
			wantHave:  "s01e02",
		},
		{
			name:      "Game title (calls ParseGame)",
			input:     "Super Mario Bros",
			mediaType: "Game",
			wantHave:  "super mario brothers", // ParseGame expands "Bros" -> "brothers"
		},
		{
			name:      "Movie title (no parsing yet)",
			input:     "The Matrix (1999)",
			mediaType: "Movie",
			wantHave:  "The Matrix (1999)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := ParseWithMediaType(tt.input, tt.mediaType)
			assert.Contains(t, result, tt.wantHave,
				"Parsed result should contain %q", tt.wantHave)
		})
	}
}

// TestParseTVShow_DelimiterVariations tests different delimiter styles
func TestParseTVShow_DelimiterVariations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		wantHave string
	}{
		{
			name:     "Space-dash-space delimiter",
			input:    "Show - S01E02 - Title",
			wantHave: "s01e02",
		},
		{
			name:     "Period delimiter",
			input:    "Show.S01E02.Title",
			wantHave: "s01e02",
		},
		{
			name:     "Underscore delimiter",
			input:    "Show_S01E02_Title",
			wantHave: "s01e02",
		},
		{
			name:     "Mixed delimiters",
			input:    "Show - S01E02.Title",
			wantHave: "s01e02",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := ParseTVShow(tt.input)
			assert.Contains(t, result, tt.wantHave,
				"Episode marker should normalize regardless of delimiter style")
		})
	}
}

// TestParseGame_BracketStripping tests that metadata brackets are removed
func TestParseGame_BracketStripping(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     string
		wantMatch []string // All should normalize to same result
	}{
		{
			name:  "USA region code",
			input: "Sonic (USA)",
			wantMatch: []string{
				"Sonic",
				"Sonic [!]",
				"Sonic {Europe}",
			},
		},
		{
			name:  "Multiple brackets",
			input: "The Legend of Zelda (USA) [!]",
			wantMatch: []string{
				"The Legend of Zelda",
				"The Legend of Zelda (USA)",
				"The Legend of Zelda [!]",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := ParseGame(tt.input)

			for _, variant := range tt.wantMatch {
				variantResult := ParseGame(variant)
				assert.Equal(t, result, variantResult,
					"After bracket stripping, results should match:\n  Input: %q → %q\n  Variant: %q → %q",
					tt.input, result, variant, variantResult)
			}
		})
	}
}

// TestParseGame_EditionVersionStripping tests edition and version suffix removal
func TestParseGame_EditionVersionStripping(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Version suffix",
			input:    "Pokemon Red Version",
			expected: "pokemon red",
		},
		{
			name:     "Edition suffix",
			input:    "Game Special Edition",
			expected: "game special",
		},
		{
			name:     "Version number",
			input:    "Street Fighter II v2.0",
			expected: "street fighter 2",
		},
		{
			name:     "Keeps special modifiers",
			input:    "Game Deluxe Edition",
			expected: "game deluxe",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := ParseGame(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestParseGame_AbbreviationExpansion tests abbreviation expansion
func TestParseGame_AbbreviationExpansion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Bros expansion",
			input:    "Super Mario Bros.",
			expected: "super mario brothers",
		},
		{
			name:     "vs expansion",
			input:    "Mario vs Donkey Kong",
			expected: "mario versus donkey kong",
		},
		{
			name:     "Dr expansion",
			input:    "Dr. Mario",
			expected: "doctor mario",
		},
		{
			name:     "Jr expansion",
			input:    "Donkey Kong Jr.",
			expected: "donkey kong junior",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := ParseGame(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestParseGame_NumberWordExpansion tests number word expansion
func TestParseGame_NumberWordExpansion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "One to 1",
			input:    "Game One",
			expected: "game 1",
		},
		{
			name:     "Two to 2",
			input:    "Street Fighter Two",
			expected: "street fighter 2",
		},
		{
			name:     "Three to 3",
			input:    "Crash Bandicoot Three",
			expected: "crash bandicoot 3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := ParseGame(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestParseGame_OrdinalNormalization tests ordinal suffix removal
func TestParseGame_OrdinalNormalization(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "2nd to 2",
			input:    "Street Fighter 2nd Impact",
			expected: "street fighter 2 impact",
		},
		{
			name:     "3rd to 3",
			input:    "3rd Strike",
			expected: "3 strike",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := ParseGame(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestParseGame_RomanNumeralConversion tests roman numeral to arabic conversion
func TestParseGame_RomanNumeralConversion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "VII to 7",
			input:    "Final Fantasy VII",
			expected: "final fantasy 7",
		},
		{
			name:     "II to 2",
			input:    "Street Fighter II",
			expected: "street fighter 2",
		},
		{
			name:     "III to 3",
			input:    "Super Mario Bros. III",
			expected: "super mario brothers 3",
		},
		{
			name:     "X preserved (Mega Man)",
			input:    "Mega Man X",
			expected: "mega man x",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := ParseGame(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestParseGame_FullPipeline tests the complete parsing pipeline
func TestParseGame_FullPipeline(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Complex game title with all transformations",
			input:    "Super Mario Bros. III (USA) [!] Edition",
			expected: "super mario brothers 3",
		},
		{
			name:     "Street Fighter with version and roman numeral",
			input:    "Street Fighter II v2.0",
			expected: "street fighter 2",
		},
		{
			name:     "Final Fantasy with region and roman numeral",
			input:    "Final Fantasy VII (USA)",
			expected: "final fantasy 7",
		},
		{
			name:     "Game with abbreviations and number words",
			input:    "Dr. Mario vs. Donkey Kong Jr. Two",
			expected: "doctor mario versus donkey kong junior 2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := ParseGame(tt.input)
			assert.Equal(t, tt.expected, result,
				"Full pipeline should apply all transformations correctly")
		})
	}
}

// TestParseGame_CrossFormatMatching tests that different format variations match
func TestParseGame_CrossFormatMatching(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     string
		wantMatch []string
	}{
		{
			name:  "Super Mario Bros variations",
			input: "Super Mario Bros.",
			wantMatch: []string{
				"Super Mario Brothers",
				"Super Mario Bros",
				"SUPER MARIO BROS.",
			},
		},
		{
			name:  "Final Fantasy VII variations",
			input: "Final Fantasy VII",
			wantMatch: []string{
				"Final Fantasy 7",
				"Final Fantasy vii",
				"FINAL FANTASY VII",
			},
		},
		{
			name:  "Street Fighter II variations",
			input: "Street Fighter II",
			wantMatch: []string{
				"Street Fighter 2",
				"Street Fighter Two",
				"Street Fighter 2nd",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := ParseGame(tt.input)

			for _, variant := range tt.wantMatch {
				variantResult := ParseGame(variant)
				assert.Equal(t, result, variantResult,
					"Variations should normalize to same result:\n  Input: %q → %q\n  Variant: %q → %q",
					tt.input, result, variant, variantResult)
			}
		})
	}
}

// TestParseGame_NoTransformations tests titles that don't need any transformations
func TestParseGame_NoTransformations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "Simple title",
			input: "Sonic",
		},
		{
			name:  "Title with numbers",
			input: "Game 123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := ParseGame(tt.input)
			expected := strings.ToLower(tt.input)
			assert.Equal(t, expected, result,
				"Simple titles should only be lowercased")
		})
	}
}
