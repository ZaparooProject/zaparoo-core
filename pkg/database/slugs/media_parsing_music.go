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
	"regexp"
	"strings"
)

// Regex patterns for music filename parsing
var (
	// Music-specific scene tag patterns
	// Audio format tags: FLAC, MP3, AAC, ALAC, APE, WAV, OGG, WMA, M4A, OPUS
	musicFormatRegex = regexp.MustCompile(`(?i)\b(flac|mp3|aac|alac|ape|wav|ogg|wma|m4a|opus)\b`)

	// Audio quality tags: V0, V2, 320, 192, 256, CBR, VBR, LAME, APE, etc.
	musicQualityRegex = regexp.MustCompile(
		`(?i)\b(v[0-9]|320|256|192|128|cbr|vbr|abr|lame[0-9.]*|ape|apx|aps|` +
			`24bit|16bit|96khz|88\.2khz|48khz|44\.1khz|44khz|` +
			`24-96|24-88|24-48|24-44|16-44)\b`,
	)

	// Music source tags: CD, WEB, Vinyl, SACD, DVD, Blu-ray, Bluray, DAT, Cassette, Radio, FM
	musicSourceRegex = regexp.MustCompile(
		`(?i)\b(cd|web|vinyl|sacd|dvd|blu-?ray|dat|cassette|tape|radio|fm|live|bootleg)\b`,
	)

	// Disc/CD numbers: CD1, CD2, Disc1, Disc2, Disc 1, Disc 2
	musicDiscRegex = regexp.MustCompile(`(?i)\b(cd|disc)\s*\d{1,2}\b`)

	// Multiple consecutive spaces
	multipleSpacesRegex = regexp.MustCompile(`\s+`)
)

// ParseMusic normalizes music album titles to a canonical format.
// This is a CONSERVATIVE implementation that focuses on cleaning scene release tags
// while preserving artist names for uniqueness.
//
// Transformations applied (in order):
//  1. Width normalization: Convert fullwidth characters to ASCII
//  2. Scene tag stripping: Remove format, quality, source tags and release group
//  3. Separator normalization: Convert dots, underscores, and dashes to spaces
//  4. Bracket stripping: Remove metadata brackets including years (extracted as tags)
//  5. Disc number stripping: Remove CD1, CD2, Disc 1, etc.
//  6. Split titles and strip articles: "The Album: Subtitle" → "Album Subtitle"
//  7. Strip trailing articles: "Album, The" → "Album"
//
// Supported formats:
// - Scene release: "Artist-Album-2024-CD-FLAC-GROUP" → "Artist Album 2024"
// - User-friendly: "Artist - Album (2024)" → "Artist Album"
// - With quality: "Artist - Album (2024) [FLAC 24bit]" → "Artist Album"
// - With disc: "Artist - Album CD1" → "Artist Album"
//
// Examples:
//   - "Pink.Floyd-The.Wall-1979-CD-FLAC-GROUP" → "Pink Floyd Wall 1979"
//   - "The Beatles - Abbey Road (1969)" → "Beatles Abbey Road"
//   - "VA - Best of 2024 [FLAC]" → "VA Best of 2024"
//   - "Miles Davis - Kind of Blue (1959)" → "Miles Davis Kind of Blue"
//
// Note: Years in parentheses/brackets are extracted as tags (year:1997) by the tag parser.
// Bare years (from scene releases) are kept in the slug.
//
// Design note: This implementation intentionally keeps artist names to preserve uniqueness.
// Many albums share the same title across different artists ("IV", "Nevermind", etc.).
// More sophisticated artist/album extraction can be added later if needed.
func ParseMusic(title string) string {
	s := title

	// 1. Normalize width first so fullwidth separators are detected by later functions
	s = NormalizeWidth(s)
	s = strings.TrimSpace(s)

	// 2. Strip scene release tags EARLY (before separator normalization)
	// This removes format, quality, source, group tags
	s = StripMusicSceneTags(s)
	s = strings.TrimSpace(s)

	// 3. Normalize ALL separators to spaces
	// Convert dots, underscores, and dashes to spaces for consistency
	s = normalizeMusicSeparators(s)
	s = strings.TrimSpace(s)

	// 4. Strip metadata brackets (including years)
	// Years like (1997) are stripped from slug and extracted as tags (year:1997) by tag parser
	s = StripMetadataBrackets(s)
	s = strings.TrimSpace(s)

	// 5. Strip disc numbers (CD1, CD2, Disc 1, etc.)
	// These identify different discs of the same album, not different albums
	s = musicDiscRegex.ReplaceAllString(s, "")
	s = strings.TrimSpace(s)

	// 6. Strip only the leading article from the entire string
	// Conservative approach: only strip the very first "The/A/An"
	// "The Beatles Abbey Road" → "Beatles Abbey Road"
	// "Pink Floyd The Wall" → "Pink Floyd The Wall" (keep "The" in the middle)
	s = StripLeadingArticle(s)
	s = strings.TrimSpace(s)

	// 7. Strip trailing articles
	// "Album, The" → "Album"
	s = StripTrailingArticle(s)
	s = strings.TrimSpace(s)

	// 8. Collapse multiple consecutive spaces into single spaces
	// This is needed after separator normalization and tag stripping
	s = multipleSpacesRegex.ReplaceAllString(s, " ")
	s = strings.TrimSpace(s)

	return s
}

// StripMusicSceneTags removes scene release tags specific to music.
// Unlike movie scene tags, music preserves edition qualifiers (Remastered, Deluxe, etc.)
// as these identify different album editions.
//
// Removed tags include:
//   - Format: FLAC, MP3, AAC, ALAC, APE, WAV, OGG, WMA, M4A, OPUS
//   - Quality: V0, V2, 320, 192, 256, CBR, VBR, LAME, 24bit, 96kHz, etc.
//   - Source: CD, WEB, Vinyl, SACD, DVD, Blu-ray, DAT, Cassette
//   - Disc numbers: CD1, CD2, Disc1, Disc2
//   - Group: -GROUP at end
//
// Preserved edition qualifiers:
//   - Remastered, Deluxe, Limited, Expanded, Anniversary, Bonus, Special
//
// Examples:
//   - "Artist-Album-2024-CD-FLAC-V0-GROUP" → "Artist-Album 2024"
//   - "Album.Title.1979.Vinyl.FLAC.24bit.96kHz" → "Album Title 1979"
//   - "Album.2020.Remastered.WEB.FLAC" → "Album 2020 Remastered"
func StripMusicSceneTags(s string) string {
	// Strip audio format tags
	s = musicFormatRegex.ReplaceAllString(s, "")
	s = strings.TrimSpace(s)

	// Strip audio quality tags
	s = musicQualityRegex.ReplaceAllString(s, "")
	s = strings.TrimSpace(s)

	// Strip source tags
	s = musicSourceRegex.ReplaceAllString(s, "")
	s = strings.TrimSpace(s)

	// Strip disc numbers (these are metadata, not album identifiers)
	s = musicDiscRegex.ReplaceAllString(s, "")
	s = strings.TrimSpace(s)

	// Strip trailing group tag (e.g., "-GROUP" at the end)
	s = sceneGroupRegex.ReplaceAllString(s, "")
	s = strings.TrimSpace(s)

	return s
}

// normalizeMusicSeparators normalizes all separators to spaces.
// Converts dots, underscores, and dashes to spaces for consistent formatting.
// This is a conservative approach that treats all separators equally.
//
// Examples:
//   - "Artist.Name-Album.Title" → "Artist Name Album Title"
//   - "The_Beatles-Abbey_Road" → "The Beatles Abbey Road"
//   - "Pink.Floyd-The.Wall-1979" → "Pink Floyd The Wall 1979"
func normalizeMusicSeparators(s string) string {
	// Normalize dots (preserves dates and episode markers)
	s = NormalizeDotSeparators(s)
	s = strings.TrimSpace(s)

	// Replace underscores with spaces
	s = strings.ReplaceAll(s, "_", " ")
	s = strings.TrimSpace(s)

	// Replace dashes with spaces
	s = strings.ReplaceAll(s, "-", " ")
	s = strings.TrimSpace(s)

	return s
}
