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
		mediaType MediaType
		wantHave  string
	}{
		{
			name:      "TV show with S01E02",
			input:     "Show - S01E02 - Title",
			mediaType: MediaTypeTVShow,
			wantHave:  "s01e02",
		},
		{
			name:      "TV show with 1x02",
			input:     "Show - 1x02 - Title",
			mediaType: MediaTypeTVShow,
			wantHave:  "s01e02",
		},
		{
			name:      "Game title (calls ParseGame)",
			input:     "Super Mario Bros",
			mediaType: MediaTypeGame,
			wantHave:  "super mario brothers", // ParseGame expands "Bros" -> "brothers"
		},
		{
			name:      "Movie title (calls ParseMovie)",
			input:     "The Matrix (1999)",
			mediaType: MediaTypeMovie,
			wantHave:  "Matrix", // ParseMovie strips articles and years in parentheses
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := ParseWithMediaType(tt.mediaType, tt.input)
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

// TestParseTVShow_SceneReleaseTags tests scene release tag stripping
func TestParseTVShow_SceneReleaseTags(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     string
		wantMatch []string // All should produce same slug
	}{
		{
			name:  "Scene release with quality and codec",
			input: "Breaking.Bad.S01E02.1080p.BluRay.x264-GROUP",
			wantMatch: []string{
				"Breaking Bad - S01E02",
				"Breaking.Bad.S01E02.720p.WEB-DL.AAC2.0.H.264",
				"Breaking Bad - 1x02",
			},
		},
		{
			name:  "Multiple scene tags",
			input: "Show.Name.S01E02.720p.WEB-DL.AAC2.0.H.264-RELEASE",
			wantMatch: []string{
				"Show Name - S01E02",
				"Show.Name.S01E02.1080p.BluRay.x265-DIFFERENT",
			},
		},
		{
			name:  "Scene tags with PROPER/REPACK",
			input: "Episode.S01E02.1080p.PROPER.REPACK.HDTV.x264",
			wantMatch: []string{
				"Episode - S01E02",
				"Episode.S01E02",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := ParseTVShow(tt.input)

			for _, variant := range tt.wantMatch {
				variantResult := ParseTVShow(variant)
				assert.Equal(t, result, variantResult,
					"Scene tags should be stripped for matching:\n  Input: %q → %q\n  Variant: %q → %q",
					tt.input, result, variant, variantResult)
			}
		})
	}
}

// TestParseTVShow_DotSeparators tests dot normalization for scene releases
func TestParseTVShow_DotSeparators(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     string
		wantMatch []string
	}{
		{
			name:  "Dot-separated show name",
			input: "Show.Name.S01E02",
			wantMatch: []string{
				"Show Name - S01E02",
				"Show Name S01E02",
			},
		},
		{
			name:  "Dot-separated with episode title",
			input: "Breaking.Bad.S01E02.Gray.Matter",
			wantMatch: []string{
				"Breaking Bad - S01E02 - Gray Matter",
				"Breaking Bad S01E02 Gray Matter",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := ParseTVShow(tt.input)

			for _, variant := range tt.wantMatch {
				variantResult := ParseTVShow(variant)
				// Should normalize to similar structure
				assert.Contains(t, result, "s01e02", "Should contain normalized episode marker")
				assert.Contains(t, variantResult, "s01e02", "Variant should contain normalized episode marker")
			}
		})
	}
}

// TestParseTVShow_ExtendedEpisodeFormats tests additional episode format patterns
func TestParseTVShow_ExtendedEpisodeFormats(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     string
		wantMatch []string
	}{
		{
			name:  "S01.E02 dot separator format",
			input: "Show - S01.E02",
			wantMatch: []string{
				"Show - S01E02",
				"Show - 1x02",
			},
		},
		{
			name:  "S01_E02 underscore separator format",
			input: "Show - S01_E02",
			wantMatch: []string{
				"Show - S01E02",
				"Show - s01e02",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := ParseTVShow(tt.input)

			for _, variant := range tt.wantMatch {
				variantResult := ParseTVShow(variant)
				assert.Equal(t, result, variantResult,
					"Extended formats should normalize:\n  Input: %q → %q\n  Variant: %q → %q",
					tt.input, result, variant, variantResult)
			}
		})
	}
}

// TestParseTVShow_DateBasedEpisodes tests date-based episode support for daily shows
func TestParseTVShow_DateBasedEpisodes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     string
		wantMatch []string // All should produce same canonical date
	}{
		{
			name:  "YYYY-MM-DD format",
			input: "The Daily Show - 2024-01-15",
			wantMatch: []string{
				"The Daily Show - 15-01-2024",
				"The Daily Show - 2024.01.15",
				"The Daily Show - 15.01.2024",
			},
		},
		{
			name:  "DD-MM-YYYY format",
			input: "Show - 15-01-2024",
			wantMatch: []string{
				"Show - 2024-01-15",
				"Show - 2024.01.15",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := ParseTVShow(tt.input)

			for _, variant := range tt.wantMatch {
				variantResult := ParseTVShow(variant)
				assert.Equal(t, result, variantResult,
					"Dates should normalize to canonical format:\n  Input: %q → %q\n  Variant: %q → %q",
					tt.input, result, variant, variantResult)
			}
		})
	}
}

// TestParseTVShow_AbsoluteNumbering tests anime absolute numbering support
func TestParseTVShow_AbsoluteNumbering(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     string
		wantHave  string
		wantMatch []string
	}{
		{
			name:     "Episode ### format",
			input:    "One Piece - Episode 001",
			wantHave: "e001",
			wantMatch: []string{
				"One Piece - Ep 001",
				"One Piece - Ep001",
				"One Piece - E001",
			},
		},
		{
			name:     "Hash format",
			input:    "Naruto - #150",
			wantHave: "e150",
			wantMatch: []string{
				"Naruto - Episode 150",
				"Naruto - Ep 150",
			},
		},
		{
			name:     "Leading number format",
			input:    "001 - Show Name - Title",
			wantHave: "e001",
			wantMatch: []string{
				"Show Name - Episode 001 - Title",
				"Show Name - Ep001 - Title",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := ParseTVShow(tt.input)
			assert.Contains(t, result, tt.wantHave,
				"Should contain absolute episode marker %s in %q", tt.wantHave, result)

			for _, variant := range tt.wantMatch {
				variantResult := ParseTVShow(variant)
				assert.Equal(t, result, variantResult,
					"Absolute numbering should normalize:\n  Input: %q → %q\n  Variant: %q → %q",
					tt.input, result, variant, variantResult)
			}
		})
	}
}

// TestParseTVShow_ComponentReordering tests component reordering for consistent slug generation
func TestParseTVShow_ComponentReordering(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		wantOrder string
		inputs    []string
		wantHave  []string
	}{
		{
			name: "Episode marker in different positions",
			inputs: []string{
				"S01E02 - Attack on Titan - That Day",
				"Attack on Titan - S01E02 - That Day",
				"Attack on Titan - That Day - S01E02",
			},
			wantHave:  []string{"Attack on Titan", "s01e02", "That Day"},
			wantOrder: "show marker title",
		},
		{
			name: "Episode marker with show name only",
			inputs: []string{
				"S01E02 - Breaking Bad",
				"Breaking Bad - S01E02",
			},
			wantHave:  []string{"Breaking Bad", "s01e02"},
			wantOrder: "show marker",
		},
		{
			name: "Date-based episode reordering",
			inputs: []string{
				"2024-01-15 - Daily Show",
				"Daily Show - 2024-01-15",
			},
			wantHave:  []string{"Daily Show", "2024-01-15"},
			wantOrder: "show marker",
		},
		{
			name: "Absolute numbering reordering",
			inputs: []string{
				"Episode 001 - One Piece - I'm Luffy",
				"One Piece - Episode 001 - I'm Luffy",
				"One Piece - I'm Luffy - Episode 001",
			},
			wantHave:  []string{"One Piece", "e001", "I'm Luffy"},
			wantOrder: "show marker title",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// All inputs should produce the same result
			var results []string
			for _, input := range tt.inputs {
				result := ParseTVShow(input)
				results = append(results, result)

				// Check that all required substrings are present
				for _, substring := range tt.wantHave {
					assert.Contains(t, result, substring,
						"Result should contain %q: %q", substring, result)
				}
			}

			// All results should be equal (component reordering ensures consistency)
			for i := 1; i < len(results); i++ {
				assert.Equal(t, results[0], results[i],
					"All inputs should produce same slug:\n  Input 1: %q → %q\n  Input %d: %q → %q",
					tt.inputs[0], results[0], i+1, tt.inputs[i], results[i])
			}
		})
	}
}

// TestParseTVShow_RealWorldIntegration tests real-world formats end-to-end
func TestParseTVShow_RealWorldIntegration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		inputs []string // All should produce same or very similar slugs
	}{
		{
			name: "Scene release variations WITHOUT episode titles",
			inputs: []string{
				"Breaking.Bad.S01E02.1080p.BluRay.x264-GROUP",
				"Breaking Bad - S01E02",
				"Breaking Bad - 1x02",
				"S01E02 - Breaking Bad",
				"Breaking.Bad.S01E02.720p.WEB-DL.AAC2.0.H.264-OTHER",
			},
		},
		{
			name: "Scene release WITH episode title (title preserved)",
			inputs: []string{
				"Breaking Bad - S01E02 - Gray Matter",
				"Breaking Bad - 1x02 - Gray Matter",
				"S01E02 - Breaking Bad - Gray Matter",
				"Breaking.Bad.S01E02.Gray.Matter",
			},
		},
		{
			name: "Anime absolute numbering variations",
			inputs: []string{
				"One Piece - Episode 001",
				"One Piece - Ep001",
				"One Piece - E001",
				"One Piece #001",
				"001 - One Piece",
				"Episode 001 - One Piece",
			},
		},
		{
			name: "Daily show variations",
			inputs: []string{
				"The Daily Show - 2024-01-15",
				"Daily Show - 15-01-2024",
				"The Daily Show - 2024.01.15",
				"2024-01-15 - The Daily Show",
			},
		},
		{
			name: "Batocera issue (original bug)",
			inputs: []string{
				"Attack on Titan - S01E02 - That Day",
				"Attack on Titan - 1x02 - That Day",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// All inputs should produce the same result
			var results []string
			for _, input := range tt.inputs {
				result := ParseTVShow(input)
				results = append(results, result)
			}

			// All results should be equal
			for i := 1; i < len(results); i++ {
				assert.Equal(t, results[0], results[i],
					"All variations should produce same slug:\n  Input 1: %q → %q\n  Input %d: %q → %q",
					tt.inputs[0], results[0], i+1, tt.inputs[i], results[i])
			}
		})
	}
}

// TestParseTVShow_SceneGroupRegexRegression ensures scene group regex never strips episode markers.
// This is a regression test for a bug where "-S01E02" at the end of a title would be
// incorrectly matched as a scene group and stripped, causing loss of episode information.
func TestParseTVShow_SceneGroupRegexRegression(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "bare dash before episode marker",
			input:    "ShowName-S01E02",
			expected: "ShowName s01e02",
		},
		{
			name:     "lowercase episode marker",
			input:    "ShowName-s01e02",
			expected: "ShowName s01e02",
		},
		{
			name:     "absolute numbering with E prefix",
			input:    "Anime-E001",
			expected: "Anime e001",
		},
		{
			name:     "scene release with episode and group",
			input:    "Show.S01E02.1080p-RELEASE",
			expected: "Show s01e02", // 1080p and RELEASE both stripped by scene tags
		},
		{
			name:     "episode marker should be preserved and normalized",
			input:    "Show-S01E02-RELEASE",
			expected: "Show s01e02",
		},
		{
			name:     "scene group without episode",
			input:    "Movie Name-RELEASE",
			expected: "Movie Name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := ParseTVShow(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestParseMovie_YearExtraction tests year extraction from various formats
func TestParseMovie_YearExtraction(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		// Parentheses format (highest priority - Plex/Kodi standard)
		{
			name:     "year in parentheses",
			input:    "The Matrix (1999)",
			expected: "Matrix",
		},
		{
			name:     "year in parentheses with quality",
			input:    "Avatar (2009) 1080p",
			expected: "Avatar",
		},

		// Bracket format
		{
			name:     "year in brackets",
			input:    "Inception [2010]",
			expected: "Inception",
		},
		{
			name:     "year in brackets with metadata",
			input:    "Movie Name [2024] [Remastered]",
			expected: "Movie Name",
		},

		// Dot-separated format (scene releases)
		// Note: Bare years (not in brackets) are kept in slug
		{
			name:     "year with dots scene release",
			input:    "The.Matrix.1999.1080p.BluRay",
			expected: "Matrix 1999",
		},
		{
			name:     "year dots full scene release",
			input:    "Avatar.2009.Extended.1080p.BluRay.x264-GROUP",
			expected: "Avatar 2009 Extended",
		},

		// Bare year format
		// Note: Bare years (not in brackets) are kept in slug
		{
			name:     "bare year at end",
			input:    "Blade Runner 1982",
			expected: "Blade Runner 1982",
		},
		{
			name:     "bare year in middle",
			input:    "The Dark Knight 2008 IMAX",
			expected: "Dark Knight 2008 IMAX",
		},

		// Multiple years (pick first)
		{
			name:     "multiple years pick first",
			input:    "Blade Runner (1982) (2007 Final Cut)",
			expected: "Blade Runner",
		},

		// Missing year
		{
			name:     "no year present",
			input:    "The Matrix",
			expected: "Matrix",
		},
		{
			name:     "no year with quality",
			input:    "Avatar 1080p BluRay",
			expected: "Avatar",
		},

		// Year validation (reject invalid years)
		{
			name:     "reject too old year",
			input:    "Movie (1800)",
			expected: "Movie", // Year kept as-is if invalid
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := ParseMovie(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestParseMovie_SceneReleaseTags tests removal of scene release quality/source tags
func TestParseMovie_SceneReleaseTags(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		// Quality tags
		{
			name:     "720p quality tag",
			input:    "Movie (2024) 720p",
			expected: "Movie",
		},
		{
			name:     "1080p quality tag",
			input:    "The Matrix (1999) 1080p",
			expected: "Matrix",
		},
		{
			name:     "4K quality tag",
			input:    "Avatar (2009) 4K",
			expected: "Avatar",
		},
		{
			name:     "2160p UHD tag",
			input:    "Film (2020) 2160p UHD",
			expected: "Film",
		},

		// Source tags
		{
			name:     "BluRay source",
			input:    "Movie.2024.BluRay.x264",
			expected: "Movie 2024",
		},
		{
			name:     "WEB-DL source",
			input:    "Film.2020.WEB-DL.1080p",
			expected: "Film 2020",
		},
		{
			name:     "HDTV source",
			input:    "Show.Movie.2019.HDTV.720p",
			expected: "Show Movie 2019",
		},
		{
			name:     "Remux source",
			input:    "Movie (2024) Remux 2160p",
			expected: "Movie",
		},

		// Codec tags
		{
			name:     "x264 codec",
			input:    "Movie.2024.1080p.x264",
			expected: "Movie 2024",
		},
		{
			name:     "x265 HEVC codec",
			input:    "Film.2020.2160p.x265.HEVC",
			expected: "Film 2020",
		},
		{
			name:     "H.264 codec",
			input:    "Movie (2024) H.264 1080p",
			expected: "Movie",
		},
		{
			name:     "10bit codec",
			input:    "Film.2020.10bit.x265",
			expected: "Film 2020",
		},

		// Audio tags
		{
			name:     "DTS audio",
			input:    "Matrix.1999.DTS.5.1",
			expected: "Matrix 1999",
		},
		{
			name:     "Atmos audio",
			input:    "Movie (2024) Atmos DD7.1",
			expected: "Movie",
		},
		{
			name:     "TrueHD audio",
			input:    "Film.2020.TrueHD.7.1",
			expected: "Film 2020",
		},
		{
			name:     "AAC audio",
			input:    "Movie (2024) AAC2.0",
			expected: "Movie",
		},

		// HDR tags
		{
			name:     "HDR10 tag",
			input:    "Movie.2024.2160p.HDR10",
			expected: "Movie 2024",
		},
		{
			name:     "Dolby Vision tag",
			input:    "Film.2020.DV.HDR10Plus",
			expected: "Film 2020",
		},
		{
			name:     "Dolby.Vision with dot",
			input:    "Movie (2024) Dolby.Vision 4K",
			expected: "Movie",
		},

		// 3D tags
		{
			name:     "3D tag",
			input:    "Avatar (2009) 3D 1080p",
			expected: "Avatar",
		},
		{
			name:     "HSBS half side-by-side",
			input:    "Movie.2024.3D.HSBS.1080p",
			expected: "Movie 2024",
		},
		{
			name:     "Half-SBS tag",
			input:    "Film (2020) Half-SBS 1080p",
			expected: "Film",
		},

		// Multiple tags
		{
			name:     "full scene release",
			input:    "The.Dark.Knight.2008.1080p.BluRay.x264.DTS.5.1-GROUP",
			expected: "Dark Knight 2008",
		},
		{
			name:     "complex scene release",
			input:    "Movie.2024.REMASTERED.2160p.WEB-DL.DV.HDR10.HEVC.DDP5.1.Atmos-RELEASE",
			expected: "Movie 2024 REMASTERED",
		},
		{
			name:     "3D with HDR",
			input:    "Avatar.2009.3D.HSBS.1080p.BluRay.x264.DTS.HDR10",
			expected: "Avatar 2009",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := ParseMovie(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestParseMovie_EditionMarkers tests that edition suffix words are stripped while qualifiers are kept
func TestParseMovie_EditionMarkers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "director's cut",
			input:    "Blade Runner (1982) Director's Cut",
			expected: "Blade Runner  Director's",
		},
		{
			name:     "directors cut no apostrophe",
			input:    "Movie (2024) Directors Cut",
			expected: "Movie  Directors",
		},
		{
			name:     "extended edition",
			input:    "Lord of the Rings (2001) Extended Edition",
			expected: "Lord of the Rings  Extended",
		},
		{
			name:     "extended only",
			input:    "Avatar (2009) Extended",
			expected: "Avatar  Extended",
		},
		{
			name:     "theatrical cut",
			input:    "Film (2020) Theatrical Cut",
			expected: "Film  Theatrical",
		},
		{
			name:     "unrated edition",
			input:    "Movie (2024) Unrated",
			expected: "Movie  Unrated",
		},
		{
			name:     "final cut",
			input:    "Blade Runner (1982) Final Cut",
			expected: "Blade Runner  Final",
		},
		{
			name:     "ultimate edition",
			input:    "Film (2020) Ultimate Edition",
			expected: "Film  Ultimate",
		},
		{
			name:     "special edition",
			input:    "Star Wars (1977) Special Edition",
			expected: "Star Wars  Special",
		},
		{
			name:     "remastered",
			input:    "The Matrix (1999) Remastered",
			expected: "Matrix  Remastered",
		},
		{
			name:     "IMAX edition",
			input:    "Dark Knight (2008) IMAX Edition",
			expected: "Dark Knight  IMAX",
		},
		{
			name:     "IMAX only",
			input:    "Movie (2024) IMAX",
			expected: "Movie  IMAX",
		},
		{
			name:     "collector's edition",
			input:    "Film (2020) Collector's Edition",
			expected: "Film  Collector's",
		},
		{
			name:     "anniversary edition",
			input:    "Movie (1999) Anniversary Edition",
			expected: "Movie  Anniversary",
		},
		{
			name:     "edition in brackets",
			input:    "Blade Runner (1982) [Director's Cut]",
			expected: "Blade Runner",
		},
		{
			name:     "multiple editions",
			input:    "Movie (2024) Extended Unrated Director's Cut",
			expected: "Movie  Extended Unrated Director's",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := ParseMovie(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestParseMovie_ArticleHandling tests leading and trailing article removal
func TestParseMovie_ArticleHandling(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		// Leading articles
		{
			name:     "leading the",
			input:    "The Matrix (1999)",
			expected: "Matrix",
		},
		{
			name:     "leading a",
			input:    "A Beautiful Mind (2001)",
			expected: "Beautiful Mind",
		},
		{
			name:     "leading an",
			input:    "An American Tail (1986)",
			expected: "American Tail",
		},

		// Trailing articles
		{
			name:     "trailing the",
			input:    "King, The (2019)",
			expected: "King",
		},
		{
			name:     "matrix the",
			input:    "Matrix, The (1999)",
			expected: "Matrix",
		},

		// Both leading and trailing
		{
			name:     "the movie the",
			input:    "The Movie, The (2024)",
			expected: "Movie",
		},

		// With subtitles
		{
			name:     "the with subtitle",
			input:    "The Legend of Zelda: Breath of the Wild (2017)",
			expected: "Legend of Zelda Breath of the Wild",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := ParseMovie(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestParseMovie_CrossFormatMatching tests ParseMovie output for various formats
func TestParseMovie_CrossFormatMatching(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		// Matrix variants
		{
			name:     "Matrix standard",
			input:    "The Matrix (1999)",
			expected: "Matrix",
		},
		{
			name:     "Matrix scene release",
			input:    "The.Matrix.1999.1080p.BluRay.x264.DTS-WAF",
			expected: "Matrix 1999",
		},
		{
			name:     "Matrix with quality",
			input:    "Matrix (1999) 720p",
			expected: "Matrix",
		},
		{
			name:     "Matrix brackets",
			input:    "The Matrix [1999]",
			expected: "Matrix",
		},
		{
			name:     "Matrix trailing article",
			input:    "Matrix, The (1999)",
			expected: "Matrix",
		},
		{
			name:     "Matrix bare year with Remastered",
			input:    "The Matrix 1999 Remastered",
			expected: "Matrix 1999 Remastered",
		},

		// Avatar variants
		{
			name:     "Avatar standard",
			input:    "Avatar (2009)",
			expected: "Avatar",
		},
		{
			name:     "Avatar scene with Extended",
			input:    "Avatar.2009.Extended.Edition.1080p.BluRay.x264-GROUP",
			expected: "Avatar 2009 Extended",
		},
		{
			name:     "Avatar with Extended",
			input:    "Avatar (2009) Extended",
			expected: "Avatar  Extended",
		},
		{
			name:     "Avatar brackets Extended",
			input:    "Avatar [2009] [Extended]",
			expected: "Avatar",
		},
		{
			name:     "Avatar bare year 3D",
			input:    "Avatar 2009 3D HSBS",
			expected: "Avatar 2009",
		},

		// Blade Runner variants
		{
			name:     "Blade Runner standard",
			input:    "Blade Runner (1982)",
			expected: "Blade Runner",
		},
		{
			name:     "Blade Runner Director's Cut",
			input:    "Blade Runner (1982) Director's Cut",
			expected: "Blade Runner  Director's",
		},
		{
			name:     "Blade Runner Final Cut",
			input:    "Blade Runner (1982) Final Cut",
			expected: "Blade Runner  Final",
		},
		{
			name:     "Blade Runner Theatrical",
			input:    "Blade Runner (1982) Theatrical",
			expected: "Blade Runner  Theatrical",
		},
		{
			name:     "Blade Runner scene release",
			input:    "Blade.Runner.1982.1080p.BluRay.x264-RELEASE",
			expected: "Blade Runner 1982",
		},
		{
			name:     "Blade Runner brackets Director's Cut",
			input:    "Blade Runner [1982] [Director's Cut]",
			expected: "Blade Runner",
		},

		// Dark Knight variants
		{
			name:     "Dark Knight standard",
			input:    "The Dark Knight (2008)",
			expected: "Dark Knight",
		},
		{
			name:     "Dark Knight scene REMASTERED",
			input:    "The.Dark.Knight.2008.REMASTERED.1080p.BluRay.x264.DTS-GROUP",
			expected: "Dark Knight 2008 REMASTERED",
		},
		{
			name:     "Dark Knight trailing article",
			input:    "Dark Knight, The (2008)",
			expected: "Dark Knight",
		},
		{
			name:     "Dark Knight with IMAX",
			input:    "The Dark Knight (2008) IMAX",
			expected: "Dark Knight  IMAX",
		},
		{
			name:     "Dark Knight brackets",
			input:    "The Dark Knight [2008]",
			expected: "Dark Knight",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := ParseMovie(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestParseMovie_RealWorldExamples tests real-world naming from Plex, Kodi, and scene releases
func TestParseMovie_RealWorldExamples(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		// Plex naming convention
		{
			name:     "plex standard",
			input:    "Avatar (2009)",
			expected: "Avatar",
		},
		{
			name:     "plex with edition",
			input:    "Blade Runner (1982) {edition-Director's Cut}",
			expected: "Blade Runner",
		},
		{
			name:     "plex with imdb id",
			input:    "The Matrix (1999) {imdb-tt0133093}",
			expected: "Matrix",
		},
		{
			name:     "plex with tmdb id",
			input:    "Inception (2010) {tmdb-27205}",
			expected: "Inception",
		},

		// Kodi naming convention
		{
			name:     "kodi standard",
			input:    "The Dark Knight (2008)",
			expected: "Dark Knight",
		},
		{
			name:     "kodi with brackets",
			input:    "Avatar [2009]",
			expected: "Avatar",
		},

		// Scene releases
		{
			name:     "scene basic",
			input:    "The.Matrix.1999.1080p.BluRay.x264.DTS-WAF",
			expected: "Matrix 1999",
		},
		{
			name:     "scene with extended",
			input:    "Avatar.2009.Extended.Edition.1080p.BluRay.x264-GROUP",
			expected: "Avatar 2009 Extended",
		},
		{
			name:     "scene complex",
			input:    "The.Dark.Knight.2008.REMASTERED.1080p.BluRay.x264.DTS.5.1.PROPER-GROUP",
			expected: "Dark Knight 2008 REMASTERED",
		},
		{
			name:     "scene 4K HDR",
			input:    "Movie.2024.2160p.WEB-DL.DV.HDR10.HEVC-GROUP",
			expected: "Movie 2024",
		},
		{
			name:     "scene 3D",
			input:    "Avatar.2009.3D.HSBS.1080p.BluRay.x264-RELEASE",
			expected: "Avatar 2009",
		},

		// Radarr/Sonarr outputs
		{
			name:     "radarr naming",
			input:    "The Movie Title (2010) [Bluray-1080p Proper][DV HDR10][DTS 5.1][x264]-RlsGrp",
			expected: "Movie Title",
		},
		{
			name:     "radarr with edition",
			input:    "Blade Runner (1982) {edition-Final Cut} [Bluray-2160p][DV][TrueHD 7.1][x265]-GROUP",
			expected: "Blade Runner",
		},

		// Edge cases
		{
			name:     "multiple spaces and dots",
			input:    "The...Matrix...1999...1080p",
			expected: "Matrix   1999",
		},
		{
			name:     "mixed case",
			input:    "ThE MaTrIx (1999)",
			expected: "MaTrIx",
		},
		{
			name:     "trailing garbage",
			input:    "Movie (2024) 1080p BluRay x264 DTS-HD MA 5.1-GROUP",
			expected: "Movie     -",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := ParseMovie(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestParseMovie_IDTagStripping tests that IMDb and TMDb ID tags are stripped
func TestParseMovie_IDTagStripping(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "imdb tag curly braces",
			input:    "The Matrix (1999) {imdb-tt0133093}",
			expected: "Matrix",
		},
		{
			name:     "tmdb tag curly braces",
			input:    "Inception (2010) {tmdb-27205}",
			expected: "Inception",
		},
		{
			name:     "imdbid tag square brackets",
			input:    "Avatar (2009) [imdbid-tt0499549]",
			expected: "Avatar",
		},
		{
			name:     "tmdbid tag square brackets",
			input:    "Blade Runner (1982) [tmdbid-78]",
			expected: "Blade Runner",
		},
		{
			name:     "multiple id tags",
			input:    "Movie (2024) {imdb-tt1234567} {tmdb-12345}",
			expected: "Movie",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := ParseMovie(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestParseMovie_EdgeCases tests edge cases and potential parsing issues
func TestParseMovie_EdgeCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "only whitespace",
			input:    "   ",
			expected: "",
		},
		{
			name:     "only year",
			input:    "(2024)",
			expected: "",
		},
		{
			name:     "year only no parens",
			input:    "2024",
			expected: "2024",
		},
		{
			name:     "no year just title",
			input:    "Some Movie",
			expected: "Some Movie",
		},
		{
			name:     "special characters",
			input:    "Movie: The Beginning (2024)",
			expected: "Movie Beginning",
		},
		{
			name:     "numbers in title",
			input:    "2001: A Space Odyssey (1968)",
			expected: "2001 Space Odyssey",
		},
		{
			name:     "very long title",
			input:    strings.Repeat("Very Long Movie Title ", 10) + "(2024)",
			expected: strings.TrimSpace(strings.Repeat("Very Long Movie Title ", 10)),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := ParseMovie(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestParseMusic_SceneReleaseFormats tests that music scene release formats are properly normalized
func TestParseMusic_SceneReleaseFormats(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "scene release with dots - FLAC",
			input:    "Pink.Floyd-The.Wall-1979-CD-FLAC-GROUP",
			expected: "Pink Floyd The Wall 1979",
		},
		{
			name:     "scene release with dots - MP3 V0",
			input:    "The.Beatles-Abbey.Road-1969-CD-MP3-V0-GROUP",
			expected: "Beatles Abbey Road 1969",
		},
		{
			name:     "scene release - WEB source",
			input:    "Artist.Name-Album.Title-2024-WEB-FLAC-GROUP",
			expected: "Artist Name Album Title 2024",
		},
		{
			name:     "scene release - Vinyl source",
			input:    "Miles.Davis-Kind.of.Blue-1959-Vinyl-FLAC-24bit-96kHz-GROUP",
			expected: "Miles Davis Kind of Blue 1959",
		},
		{
			name:     "scene release - multiple quality tags",
			input:    "Artist-Album-2020-CD-FLAC-24bit-88.2khz-GROUP",
			expected: "Artist Album 2020",
		},
		{
			name:     "scene release with underscores",
			input:    "The_Beatles-Abbey_Road-1969-CD-FLAC-GROUP",
			expected: "Beatles Abbey Road 1969",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := ParseMusic(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestParseMusic_UserFriendlyFormats tests that user-friendly music naming formats work correctly
func TestParseMusic_UserFriendlyFormats(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "artist dash album with year in parens",
			input:    "The Beatles - Abbey Road (1969)",
			expected: "Beatles Abbey Road",
		},
		{
			name:     "artist dash album with year in brackets",
			input:    "Pink Floyd - The Wall [1979]",
			expected: "Pink Floyd The Wall",
		},
		{
			name:     "artist dash album no year",
			input:    "Miles Davis - Kind of Blue",
			expected: "Miles Davis Kind of Blue",
		},
		{
			name:     "artist dash album with quality tag",
			input:    "Radiohead - OK Computer (1997) [FLAC 24bit 96kHz]",
			expected: "Radiohead OK Computer",
		},
		{
			name:     "album only with year",
			input:    "Abbey Road (1969)",
			expected: "Abbey Road",
		},
		{
			name:     "album only no year",
			input:    "Kind of Blue",
			expected: "Kind of Blue",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := ParseMusic(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestParseMusic_VariousArtists tests that Various Artists compilations are normalized correctly
func TestParseMusic_VariousArtists(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "VA short form",
			input:    "VA - Best of 2024",
			expected: "VA Best of 2024",
		},
		{
			name:     "V.A. with periods",
			input:    "V.A. - Compilation Album",
			expected: "V A Compilation Album",
		},
		{
			name:     "Various Artists full",
			input:    "Various Artists - Greatest Hits (2024)",
			expected: "Various Artists Greatest Hits",
		},
		{
			name:     "Various short form",
			input:    "Various - Dance Music 2024",
			expected: "Various Dance Music 2024",
		},
		{
			name:     "VA scene release",
			input:    "VA-Best.of.2024-WEB-FLAC-GROUP",
			expected: "VA Best of 2024",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := ParseMusic(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestParseMusic_EditionQualifiers tests that edition qualifiers are preserved
func TestParseMusic_EditionQualifiers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "remastered edition",
			input:    "The Beatles - Abbey Road (1969) Remastered",
			expected: "Beatles Abbey Road Remastered",
		},
		{
			name:     "deluxe edition",
			input:    "Pink Floyd - The Wall (1979) Deluxe Edition",
			expected: "Pink Floyd The Wall Deluxe Edition",
		},
		{
			name:     "limited edition",
			input:    "Artist - Album (2024) Limited Edition",
			expected: "Artist Album Limited Edition",
		},
		{
			name:     "expanded edition",
			input:    "Miles Davis - Kind of Blue (1959) Expanded Edition",
			expected: "Miles Davis Kind of Blue Expanded Edition",
		},
		{
			name:     "anniversary edition",
			input:    "Album Title (2000) 25th Anniversary Edition",
			expected: "Album Title 25th Anniversary Edition",
		},
		{
			name:     "multiple qualifiers",
			input:    "Artist - Album (2020) Deluxe Remastered Edition",
			expected: "Artist Album Deluxe Remastered Edition",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := ParseMusic(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestParseMusic_DiscNumbers tests that disc numbers are stripped correctly
func TestParseMusic_DiscNumbers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "CD1 format",
			input:    "The Beatles - Abbey Road (1969) CD1",
			expected: "Beatles Abbey Road",
		},
		{
			name:     "CD2 format",
			input:    "Pink Floyd - The Wall (1979) CD2",
			expected: "Pink Floyd The Wall",
		},
		{
			name:     "Disc 1 with space",
			input:    "Artist - Album (2024) Disc 1",
			expected: "Artist Album",
		},
		{
			name:     "Disc 2 with space",
			input:    "Miles Davis - Kind of Blue Disc 2",
			expected: "Miles Davis Kind of Blue",
		},
		{
			name:     "disc1 lowercase",
			input:    "Album Title (2020) disc1",
			expected: "Album Title",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := ParseMusic(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestParseMusic_ArticleStripping tests that leading and trailing articles are stripped
func TestParseMusic_ArticleStripping(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "leading article The",
			input:    "The Beatles - The Abbey Road",
			expected: "Beatles The Abbey Road",
		},
		{
			name:     "leading article A",
			input:    "Artist - A New Beginning",
			expected: "Artist A New Beginning",
		},
		{
			name:     "trailing article",
			input:    "Artist - Album, The",
			expected: "Artist Album",
		},
		{
			name:     "both leading and trailing",
			input:    "The Artist - The Album, The",
			expected: "Artist The Album",
		},
		{
			name:     "article in middle preserved",
			input:    "Artist - The Middle Album",
			expected: "Artist The Middle Album",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := ParseMusic(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestParseMusic_RealWorldExamples tests with real-world music naming examples
func TestParseMusic_RealWorldExamples(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Pink Floyd - The Wall",
			input:    "Pink Floyd - The Wall (1979)",
			expected: "Pink Floyd The Wall",
		},
		{
			name:     "The Beatles - Abbey Road",
			input:    "The Beatles - Abbey Road (1969)",
			expected: "Beatles Abbey Road",
		},
		{
			name:     "Miles Davis - Kind of Blue",
			input:    "Miles Davis - Kind of Blue (1959)",
			expected: "Miles Davis Kind of Blue",
		},
		{
			name:     "Radiohead - OK Computer",
			input:    "Radiohead - OK Computer (1997)",
			expected: "Radiohead OK Computer",
		},
		{
			name:     "Led Zeppelin - IV",
			input:    "Led Zeppelin - IV (1971)",
			expected: "Led Zeppelin IV",
		},
		{
			name:     "The Dark Side of the Moon",
			input:    "Pink Floyd - The Dark Side of the Moon (1973)",
			expected: "Pink Floyd The Dark Side of the Moon",
		},
		{
			name:     "Fleetwood Mac - Rumours",
			input:    "Fleetwood Mac - Rumours (1977)",
			expected: "Fleetwood Mac Rumours",
		},
		{
			name:     "Nirvana - Nevermind",
			input:    "Nirvana - Nevermind (1991)",
			expected: "Nirvana Nevermind",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := ParseMusic(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestParseMusic_EdgeCases tests edge cases and corner scenarios
func TestParseMusic_EdgeCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "only whitespace",
			input:    "   ",
			expected: "",
		},
		{
			name:     "only year",
			input:    "(1979)",
			expected: "",
		},
		{
			name:     "year only no parens",
			input:    "1979",
			expected: "1979",
		},
		{
			name:     "album with numbers in title",
			input:    "Artist - 1984 (1984)",
			expected: "Artist 1984",
		},
		{
			name:     "album with year in name",
			input:    "Various Artists - Best of 2024 (2024)",
			expected: "Various Artists Best of 2024",
		},
		{
			name:     "very long title",
			input:    strings.Repeat("Very Long Album Title ", 10) + "(2024)",
			expected: strings.TrimSpace(strings.Repeat("Very Long Album Title ", 10)),
		},
		{
			name:     "special characters in title",
			input:    "Artist - Album: The Beginning (2024)",
			expected: "Artist Album: The Beginning",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := ParseMusic(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestParseMusic_FormatNormalization tests that similar formats normalize consistently
// Note: Conservative implementation keeps artist names and bare years from scene releases,
// so perfect cross-format matching isn't expected. This tests what DOES match.
func TestParseMusic_FormatNormalization(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     string
		wantMatch []string // All these should produce the same normalized output
	}{
		{
			name:  "Abbey Road user-friendly formats match",
			input: "The Beatles - Abbey Road (1969)",
			wantMatch: []string{
				"Beatles - Abbey Road [1969]",
				"Beatles - Abbey Road (1969) [FLAC]",
				"The Beatles - Abbey Road",
			},
		},
		{
			name:  "Abbey Road scene formats match",
			input: "The.Beatles-Abbey.Road-1969-CD-FLAC-GROUP",
			wantMatch: []string{
				"Beatles-Abbey.Road-1969-WEB-MP3-V0-GROUP",
			},
		},
		{
			name:  "The Wall user-friendly formats match",
			input: "Pink Floyd - The Wall (1979)",
			wantMatch: []string{
				"Pink Floyd - The Wall [1979]",
			},
		},
		{
			name:  "The Wall scene formats match",
			input: "Pink.Floyd-The.Wall-1979-CD-FLAC-GROUP",
			wantMatch: []string{
				"Pink.Floyd-The.Wall-1979-Vinyl-FLAC-24bit-96kHz-GROUP",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Parse the input
			result := ParseMusic(tt.input)

			// Parse all variations that should match
			for _, variant := range tt.wantMatch {
				variantResult := ParseMusic(variant)

				// After normalization, they should all be the same
				assert.Equal(t, result, variantResult,
					"Normalized formats should match:\n  Input: %q → %q\n  Variant: %q → %q",
					tt.input, result, variant, variantResult)
			}
		})
	}
}
