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
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDisplayNames(t *testing.T) {
	t.Parallel()
	tests := []struct {
		kind *mediaKind
		want string
		item Item
	}{
		{kind: kindMovie, want: "Heat (1995)", item: Item{Name: "Heat", ProductionYear: 1995}},
		{kind: kindMovie, want: "Heat", item: Item{Name: "Heat"}},
		{kind: kindEpisode, want: "Bluey - S01E02 - Hospital", item: Item{
			Name: "Hospital", SeriesName: "Bluey", ParentIndexNumber: intPtr(1), IndexNumber: intPtr(2),
		}},
		{kind: kindEpisode, want: "Show - Pilot", item: Item{Name: "Pilot", SeriesName: "Show"}},
		{kind: kindShow, want: "Bluey", item: Item{Name: "Bluey", ProductionYear: 2018}},
		{kind: kindSong, want: "George Harrison - Abbey Road - Something", item: Item{
			Name: "Something", Album: "Abbey Road", AlbumArtist: "The Beatles", Artists: []string{"George Harrison"},
		}},
		{kind: kindSong, want: "The Beatles - Something", item: Item{Name: "Something", AlbumArtist: "The Beatles"}},
		{kind: kindAlbum, want: "The Beatles - Abbey Road (1969)", item: Item{
			Name: "Abbey Road", AlbumArtist: "The Beatles", ProductionYear: 1969,
		}},
		{kind: kindArtist, want: "The Beatles", item: Item{Name: "The Beatles"}},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, tt.kind.name(&tt.item))
	}
}

func TestPathRoundTrip(t *testing.T) {
	t.Parallel()
	item := &Item{
		ID:   "f9a3c1e2d4b5a6978877665544332211",
		Name: "Amélie / Le Fabuleux Destin? #1 & more",
		ProviderIDs: map[string]string{
			"Tmdb": "194", "imdb": "tt0211915", "Tvdb": "bad,value", "Zap2it": "ignored",
		},
	}
	path := itemPath(kindMovie, item, kindMovie.name(item))
	ref, err := parseReference(path, kindMovie)
	require.NoError(t, err)
	assert.Equal(t, item.ID, ref.ID)
	assert.Equal(t, item.Name, ref.Name)
	assert.Equal(t, map[string]string{"tmdb": "194", "imdb": "tt0211915"}, ref.ProviderIDs)
}

func TestPathWithoutProviders(t *testing.T) {
	t.Parallel()
	path := itemPath(kindArtist, &Item{ID: "abc", Name: "Björk"}, "Björk")
	assert.NotContains(t, path, "?")
	ref, err := parseReference(path, kindArtist)
	require.NoError(t, err)
	assert.Equal(t, "Björk", ref.Name)
	assert.Empty(t, ref.ProviderIDs)
}

func TestParseReferenceRejects(t *testing.T) {
	t.Parallel()
	for _, path := range []string{
		"",
		"jellyfin-movie://",
		"jellyfin-show://abc/Name",
		"kodi-movie://1/Name",
		"jellyfin-movie://not-an-id!/Name",
		"jellyfin-movie://" + strings.Repeat("a", 65) + "/Name",
		"jellyfin-movie://abc/Name?tmdb=1&tmdb=2",
		"jellyfin-movie://abc/Name?tmdb=%zz",
	} {
		_, err := parseReference(path, kindMovie)
		assert.Error(t, err, path)
	}
}

func TestParseReferenceIgnoresOtherKeys(t *testing.T) {
	t.Parallel()
	ref, err := parseReference("jellyfin-song://abc/Name?tmdb=1&MB_TRACK=x&mb_track=x", kindSong)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"mb_track": "x"}, ref.ProviderIDs)
}

func TestSearchTerms(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "Heat", kindMovie.searchTerm("Heat (1995)"))
	assert.Equal(t, "Hospital", kindEpisode.searchTerm("Bluey - S01E02 - Hospital"))
	assert.Equal(t, "Abbey Road", kindAlbum.searchTerm("The Beatles - Abbey Road (1969)"))
	assert.Equal(t, "Something", kindSong.searchTerm("The Beatles - Abbey Road - Something"))
	assert.Equal(t, "The Beatles", kindArtist.searchTerm("The Beatles"))
}
