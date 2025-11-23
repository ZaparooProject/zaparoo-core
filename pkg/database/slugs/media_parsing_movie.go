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
)

// Regex patterns for movie filename parsing
var (
	// Edition suffix pattern - strips meaningless suffix words while preserving qualifiers
	// Matches "edition", "version", "cut", "release" ONLY as trailing suffixes (end of string)
	// This preserves the qualifier words (Director's, Extended, Theatrical, etc.) that
	// identify the specific edition type, which are different products users may want to target.
	// Examples:
	//   - "Director's Cut Edition" → "Director's Cut" → "Director's"
	//   - "Extended Edition" → "Extended"
	//   - "Theatrical Release" → "Theatrical"
	movieEditionSuffixPattern = regexp.MustCompile(
		`(?i)\s+(edition|version|cut|release)$`,
	)
)

// ParseMovie normalizes movie titles to a canonical format.
// Handles scene release tags, edition suffix stripping, and article stripping.
// Years are stripped from the slug (like games) and extracted as tags by the tag parser.
//
// Transformations applied (in order):
//  1. Width normalization: Convert fullwidth characters to ASCII
//  2. Scene tag stripping: Remove quality, codec, source, HDR, 3D tags
//  3. Scene group stripping: Remove trailing release group tags (-GROUP)
//  4. Dot normalization: Convert scene release dots to spaces
//  5. Edition suffix stripping: Remove "Edition", "Version", "Cut", "Release" suffixes
//     (preserves qualifiers like "Director's", "Extended", "Theatrical")
//  6. Bracket stripping: Remove metadata brackets including years (extracted as tags)
//  7. Split titles and strip articles: "The Movie: Subtitle" → "Movie Subtitle"
//  8. Strip trailing articles: "Movie, The" → "Movie"
//
// Supported formats:
// - Standard: "Movie Name (2024)"
// - Scene: "Movie.Name.2024.1080p.BluRay.x264-GROUP"
// - With edition: "Movie Name (2024) Director's Cut Edition" → "Movie Name Director's"
// - With ID: "Movie Name (2024) {imdb-tt1234567}"
//
// Examples:
//   - "The.Matrix.1999.1080p.BluRay.x264.DTS-WAF" → "Matrix 1999"
//   - "Blade Runner (1982) Director's Cut" → "Blade Runner  Director's"
//   - "Avatar.2009.Extended.Edition.1080p" → "Avatar 2009 Extended"
//   - "The Dark Knight (2008)" → "Dark Knight"
//   - "Lord of the Rings (2001) Extended Edition" → "Lord of Rings Extended"
//   - "Movie, The (2024)" → "Movie"
//
// Note: Years like (1999) are extracted as tags (year:1999) by the tag parser,
// allowing users to filter by year when needed: launch.title Movie/Matrix (+year:1999)
//
// TODO: Scene releases use bare years without parentheses (Movie.Name.1999.1080p),
// but we can't safely strip them without breaking movies with years in their titles
// (e.g., "2001: A Space Odyssey", "1917", "1984"). For now, we only strip years in
// parentheses/brackets. This means scene releases will include the year in the slug
// (e.g., "Matrix 1999" vs "Matrix" from standard naming). Cross-format matching happens
// at the Slugify level where lowercasing provides some normalization.
func ParseMovie(title string) string {
	s := title

	// 1. Normalize width first so fullwidth separators are detected by later functions
	s = NormalizeWidth(s)
	s = strings.TrimSpace(s)

	// 2. Strip scene release tags EARLY (before dot normalization)
	// This removes quality, codec, source, HDR, 3D tags
	s = StripMovieSceneTags(s)
	s = strings.TrimSpace(s)

	// 3. Normalize dot separators (scene releases use dots)
	// "Movie.Name.2024" → "Movie Name 2024"
	s = NormalizeDotSeparators(s)
	s = strings.TrimSpace(s)

	// 4. Strip edition suffix words
	// This strips trailing "Edition", "Version", "Cut", "Release" while preserving qualifiers
	s = stripEditionMarkers(s)
	s = strings.TrimSpace(s)

	// 5. Strip metadata brackets (including years)
	// Years like (1999) are stripped from slug and extracted as tags (year:1999) by tag parser
	// This matches game behavior where years are tags, not part of the slug
	s = StripMetadataBrackets(s)
	s = strings.TrimSpace(s)

	// 6. Split titles and strip articles
	// "The Movie: Subtitle" → "Movie Subtitle"
	s = SplitAndStripArticles(s)
	s = strings.TrimSpace(s)

	// 7. Strip trailing articles
	// "Movie, The" → "Movie"
	s = StripTrailingArticle(s)
	s = strings.TrimSpace(s)

	return s
}

// stripEditionMarkers removes edition suffix words from movie titles while preserving qualifiers.
// Strips only the meaningless suffix words (edition, version, cut, release) as trailing suffixes.
// Preserves qualifier words (Director's, Extended, Theatrical, etc.) that identify the edition type.
//
// This follows the same pattern as StripEditionAndVersionSuffixes() in the game parser.
// Iteratively strips suffixes to handle multiple occurrences like "Director's Cut Edition".
//
// Examples:
//   - "Blade Runner Director's Cut" → "Blade Runner Director's"
//   - "Movie Extended Edition" → "Movie Extended"
//   - "Film Theatrical Release" → "Film Theatrical"
//   - "Title IMAX Version" → "Title IMAX"
//   - "Director's Cut Edition" → "Director's Cut" → "Director's"
func stripEditionMarkers(s string) string {
	// Iteratively strip trailing edition suffix words
	for {
		before := s
		s = movieEditionSuffixPattern.ReplaceAllString(s, "")
		s = strings.TrimSpace(s)
		if s == before {
			break // No more changes
		}
	}
	return s
}

// StripMovieSceneTags removes scene release tags specific to movies.
// Unlike the shared StripSceneTags(), this function excludes edition qualifiers
// (Extended, Unrated, Director's Cut, Remastered) which identify different movie editions.
//
// Removed tags include:
//   - Quality: 480p, 720p, 1080p, 2160p, 4K, 8K, UHD, HD, SD
//   - Source: BluRay, WEB-DL, HDTV, DVDRip, Remux, etc.
//   - Codec: x264, x265, H.264, H.265, HEVC, XviD, AVC, VC-1, 10bit, 8bit
//   - Audio: AC3, AAC, DTS, DD5.1, DD7.1, Atmos, TrueHD, etc.
//   - HDR: HDR, HDR10, HDR10+, Dolby Vision, HLG
//   - 3D: 3D, HSBS, HOU, Half-SBS, Half-OU
//   - Tags: PROPER, REPACK, INTERNAL, LIMITED, MULTI, KORSUB (but NOT Extended, Unrated, etc.)
//   - Group: -GROUP at end
//
// Preserved edition qualifiers:
//   - Extended, Unrated, Director's Cut, Remastered (these identify different editions)
//
// Examples:
//   - "Movie.2024.2160p.WEB-DL.DV.HDR10.HEVC-GROUP" → "Movie 2024"
//   - "Avatar.2009.Extended.3D.HSBS.1080p.BluRay" → "Avatar 2009 Extended"
//   - "Film.2020.Unrated.1080p.BluRay.x264.DTS" → "Film 2020 Unrated"
func StripMovieSceneTags(s string) string {
	// Strip quality tags
	s = sceneQualityRegex.ReplaceAllString(s, "")
	s = strings.TrimSpace(s)

	// Strip source tags
	s = sceneSourceRegex.ReplaceAllString(s, "")
	s = strings.TrimSpace(s)

	// Strip codec tags
	s = sceneCodecRegex.ReplaceAllString(s, "")
	s = strings.TrimSpace(s)

	// Strip audio tags
	s = sceneAudioRegex.ReplaceAllString(s, "")
	s = strings.TrimSpace(s)

	// Strip movie-specific HDR tags
	s = sceneHDRRegex.ReplaceAllString(s, "")
	s = strings.TrimSpace(s)

	// Strip movie-specific 3D tags
	s = scene3DRegex.ReplaceAllString(s, "")
	s = strings.TrimSpace(s)

	// Strip non-edition scene tags (PROPER, REPACK, etc.)
	// NOTE: We skip sceneTagsRegex because it contains edition qualifiers
	// (extended, unrated, director's cut, remastered) that we want to preserve for movies
	movieSceneTagsRegex := regexp.MustCompile(`(?i)\b(proper|repack|internal|limited|multi|korsub)\b`)
	s = movieSceneTagsRegex.ReplaceAllString(s, "")
	s = strings.TrimSpace(s)

	// Strip trailing group tag (e.g., "-GROUP" at the end)
	s = sceneGroupRegex.ReplaceAllString(s, "")
	s = strings.TrimSpace(s)

	return s
}
