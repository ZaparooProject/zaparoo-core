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

package plex

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/virtualpath"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDisplayName(t *testing.T) {
	t.Parallel()
	m := movie("1", "Alien", 1979)
	assert.Equal(t, "Alien (1979)", displayName(kindMovie, &m))
	m.Year = 0
	assert.Equal(t, "Alien", displayName(kindMovie, &m))
	e := episode("2", "Firefly", 1, 3, "Bushwhacked")
	assert.Equal(t, "Firefly - S01E03 - Bushwhacked", displayName(kindEpisode, &e))
	a := Metadata{Title: "OK Computer", ParentTitle: "Radiohead", Year: 1997}
	assert.Equal(t, "Radiohead - OK Computer (1997)", displayName(kindAlbum, &a))
	tr := track("3", "Radiohead", "OK Computer", "9", 1, 2, "Paranoid Android")
	assert.Equal(t, "Radiohead - OK Computer - Paranoid Android", displayName(kindTrack, &tr))
	s := Metadata{Title: "Firefly"}
	assert.Equal(t, "Firefly", displayName(kindShow, &s))
}

func TestScanKindPagesAcrossSections(t *testing.T) {
	// Not parallel: uses the global auth config.
	srv := newFakeServer(t)
	useTokens(t, srv.URL, "", "")
	const count = pageSize*2 + 7
	items := make([]Metadata, 0, count)
	for i := range count {
		key := strconv.Itoa(1000 + i)
		items = append(items, movie(key, fmt.Sprintf("Movie %d", i), 2000, fmt.Sprintf("tmdb://%d", i)))
	}
	srv.addSection("1", "movie", "Movies", items...)
	srv.addSection("2", "movie", "Kids", movie("5", "Up", 2009), Metadata{RatingKey: "6", Type: "movie"})
	srv.addSection("3", "show", "TV", Metadata{RatingKey: "7", Type: "show", Title: "Firefly"})

	results, err := scanKind(context.Background(), srv.client(""), kindMovie, nil)
	require.NoError(t, err)
	require.Len(t, results, count+1, "the untitled movie and the show are skipped")
	assert.Equal(t, "Movie 0 (2000)", results[0].Name)
	assert.Equal(t,
		virtualpath.CreateVirtualPath(shared.SchemePlexMovie, "1000", "Movie 0 (2000)")+";tmdb=0", results[0].Path)
	assert.True(t, results[0].NoExt)
	assert.Equal(t, "Up (2009)", results[count].Name)

	pages := srv.seenPath("/library/sections/1/all")
	require.Len(t, pages, 3)
	assert.Equal(t, "1000", pages[2].URL.Query().Get("X-Plex-Container-Start"))
	assert.Equal(t, "1", pages[0].URL.Query().Get("type"))
	assert.Equal(t, testClientID, pages[0].Header.Get("X-Plex-Client-Identifier"))
	assert.Empty(t, srv.seenPath("/library/sections/3/all"), "a TV section is not scanned for movies")
}

func TestScanKindSkipsCompilationArtists(t *testing.T) {
	// Not parallel: uses the global auth config.
	srv := newFakeServer(t)
	useTokens(t, srv.URL, "", "")
	srv.addSection("1", "artist", "Music",
		Metadata{RatingKey: "1", Type: "artist", Title: "Radiohead"},
		Metadata{RatingKey: "2", Type: "artist", Title: "Various Artists"},
	)
	results, err := scanKind(context.Background(), srv.client(""), kindArtist, nil)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "Radiohead", results[0].Name)
}

func TestScanKindUnreachable(t *testing.T) {
	// Not parallel: uses the global auth config.
	srv := newFakeServer(t)
	useTokens(t, "http://other.invalid", "", "")
	_, err := scanKind(context.Background(), srv.client(""), kindMovie, nil)
	require.Error(t, err, "a refused token fails the scan")
}

func TestCollectOrdersEpisodesAndTracks(t *testing.T) {
	// Not parallel: uses the global auth config.
	srv := newFakeServer(t)
	useTokens(t, srv.URL, "", "")
	srv.addSection("1", "show", "TV", Metadata{RatingKey: "10", Type: "show", Title: "Firefly"})
	srv.leaves["10"] = []Metadata{
		episode("13", "Firefly", 2, 1, "Later"),
		episode("12", "Firefly", 1, 2, "Train Job"),
		episode("11", "Firefly", 1, 1, "Serenity"),
	}
	items, err := collect(context.Background(), srv.client(""), kindShow, shared.SchemePlexShow+"://10/Firefly")
	require.NoError(t, err)
	require.Len(t, items, 3)
	assert.Equal(t, "Firefly - S01E01 - Serenity", items[0].Name)
	assert.Equal(t,
		virtualpath.CreateVirtualPath(shared.SchemePlexEpisode, "11", "Firefly - S01E01 - Serenity"), items[0].Path)
	assert.Equal(t, "Firefly - S02E01 - Later", items[2].Name)

	srv.addSection("2", "artist", "Music", Metadata{RatingKey: "20", Type: "artist", Title: "Radiohead"})
	srv.leaves["20"] = []Metadata{
		track("23", "Radiohead", "OK Computer", "31", 1, 1, "Airbag"),
		track("22", "Radiohead", "Kid A", "30", 1, 2, "Kid A"),
		track("21", "Radiohead", "Kid A", "30", 1, 1, "Everything In Its Right Place"),
	}
	items, err = collect(context.Background(), srv.client(""), kindArtist, shared.SchemePlexArtist+"://20/Radiohead")
	require.NoError(t, err)
	require.Len(t, items, 3)
	assert.Equal(t, "Radiohead - Kid A - Everything In Its Right Place", items[0].Name)
	assert.Equal(t, virtualpath.CreateVirtualPath(shared.SchemePlexSong, "21",
		"Radiohead - Kid A - Everything In Its Right Place"), items[0].Path)
	assert.Equal(t, "Radiohead - OK Computer - Airbag", items[2].Name)
}

func TestPagedListing(t *testing.T) {
	t.Parallel()
	items := make([]Metadata, 10)
	for i := range items {
		items[i] = movie(strconv.Itoa(i+1), "M"+strconv.Itoa(i), 2000)
	}
	tests := []struct {
		respond func(start int) mediaContainer
		name    string
		wantErr string
		want    int
	}{
		{
			name: "server caps the page size below the one asked for",
			respond: func(start int) mediaContainer {
				end := min(start+3, len(items))
				return mediaContainer{Metadata: items[start:end], TotalSize: FlexInt(len(items))}
			},
			want: 10,
		},
		{
			name: "empty page before the total",
			respond: func(start int) mediaContainer {
				if start >= 4 {
					return mediaContainer{TotalSize: 10}
				}
				return mediaContainer{Metadata: items[start:4], TotalSize: 10}
			},
			want:    4,
			wantErr: "stopped at 4 of 10",
		},
		{
			name: "server ignores paging and repeats the first page",
			respond: func(int) mediaContainer {
				return mediaContainer{Metadata: items[:4], TotalSize: 10}
			},
			want:    4,
			wantErr: "stopped at 4 of 10",
		},
		{
			name: "no total and repeated first page ends the listing",
			respond: func(int) mediaContainer {
				return mediaContainer{Metadata: items[:4]}
			},
			want: 4,
		},
		{
			name:    "empty listing",
			respond: func(int) mediaContainer { return mediaContainer{} },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				start, _ := strconv.Atoi(r.URL.Query().Get("X-Plex-Container-Start"))
				mc := tt.respond(start)
				writeContainer(w, &mc)
			}))
			t.Cleanup(srv.Close)
			c := NewClient(srv.URL, srv.URL, testClientID, "", srv.Client())
			got := 0
			err := c.paged(context.Background(), "/library/sections/1/all", url.Values{}, func(page []Metadata) error {
				got += len(page)
				return nil
			})
			if tt.wantErr != "" {
				require.ErrorIs(t, err, platforms.ErrScannerSourceUnreachable)
				require.ErrorContains(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tt.want, got)
		})
	}
}
