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

package jellyfin

import (
	"testing"
)

// FuzzParseReference feeds card paths, which come from untrusted tokens,
// through the parser and checks that anything it accepts is safe to send.
func FuzzParseReference(f *testing.F) {
	for _, seed := range []string{
		"jellyfin-movie://abc123/Heat%20%281995%29?tmdb=949&imdb=tt0113277",
		"jellyfin-episode://0a-1b/Bluey%20-%20S01E02%20-%20Hospital?tvdb=6772970",
		"jellyfin-artist://abc/The%20Beatles?mb_artist=x",
		"jellyfin-movie://abc/Name?tmdb=1&tmdb=2",
		"jellyfin-movie://abc/Name?tmdb=a%2Cb",
		"jellyfin-movie://abc123/Heat%20%281995%29;tmdb=949;imdb=tt0113277",
		"jellyfin-movie://abc/Name;tmdb=1?imdb=tt2",
		"jellyfin-movie://abc/Up;Down",
		"jellyfin-movie:///x",
		"",
	} {
		f.Add(seed)
	}
	kinds := []*mediaKind{kindMovie, kindEpisode, kindShow, kindSong, kindAlbum, kindArtist}
	f.Fuzz(func(t *testing.T, path string) {
		for _, kind := range kinds {
			ref, err := parseReference(path, kind)
			if err != nil {
				continue
			}
			if !validItemID(ref.ID) {
				t.Fatalf("accepted invalid id %q", ref.ID)
			}
			for key, v := range ref.ProviderIDs {
				if !usableProviderID(v) {
					t.Fatalf("accepted unusable %s id %q", key, v)
				}
			}
		}
	})
}
