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

// ParseWithMediaType is the entry point for media-type-aware parsing.
// It delegates to the appropriate parser based on media type.
// Each parser applies media-specific normalization BEFORE the universal pipeline.
//
// Media-specific parsers are implemented in separate files:
//   - ParseTVShow → media_parsing_tv.go
//   - ParseGame → media_parsing_game.go
//   - ParseMovie, ParseMusic, etc. → TODO (return unchanged for now)
func ParseWithMediaType(mediaType MediaType, title string) string {
	switch mediaType {
	case MediaTypeTVShow:
		return ParseTVShow(title)
	case MediaTypeGame:
		return ParseGame(title)
	case MediaTypeMovie:
		return ParseMovie(title)
	case MediaTypeMusic:
		return ParseMusic(title)
	case MediaTypeAudio:
		// TODO: Implement ParseAudio in media_parsing_audio.go
		// Future work needed:
		//   - Audiobooks: Extract chapter/part numbers, strip "Unabridged"/"Abridged" markers
		//   - Podcasts: Normalize episode numbers, strip date formats
		//   - General: Strip narrator names if in metadata format
		// Example: "Book Title (Unabridged) - Chapter 1" → "Book Title Chapter 1"
		return title
	case MediaTypeVideo:
		// TODO: Implement ParseVideo in media_parsing_video.go
		// Future work needed:
		//   - Music videos: Normalize "Artist - Song (Official Video)" formats
		//   - Strip quality tags (1080p, 4K, etc.) similar to movies
		//   - Handle "feat." and artist collaborations
		// Example: "Artist - Song Title (Official Music Video) [4K]" → "Artist Song Title"
		return title
	case MediaTypeImage, MediaTypeApplication:
		// No special parsing needed for images and applications
		return title
	default:
		return title
	}
}
