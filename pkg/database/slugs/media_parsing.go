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
		// TODO: Implement ParseMovie in media_parsing_movie.go
		return title
	case MediaTypeMusic:
		// TODO: Implement ParseMusic in media_parsing_music.go
		return title
	case MediaTypeAudio:
		// TODO: Implement ParseAudiobook/ParsePodcast in media_parsing_audio.go
		return title
	case MediaTypeVideo:
		// TODO: Implement ParseVideo in media_parsing_video.go
		return title
	case MediaTypeImage, MediaTypeApplication:
		// No special parsing needed for images and applications
		return title
	default:
		return title
	}
}
