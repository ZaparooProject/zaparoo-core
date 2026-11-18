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
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/virtualpath"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateItemPathRoundTrip(t *testing.T) {
	t.Parallel()
	md := movie("123", "Alien", 1979, "imdb://tt0078748", "tmdb://348", "plex://movie/abc")
	name := displayName(kindMovie, &md)
	path := createItemPath(kindMovie, &md, name)
	want := virtualpath.CreateVirtualPath(shared.SchemePlexMovie, "123", "Alien (1979)") + ";tmdb=348;imdb=tt0078748"
	assert.Equal(t, want, path)
	assert.True(t, shared.SchemeIDTest(shared.SchemePlexMovie)(nil, path))

	ref, err := parseItemRef(path, kindMovie)
	require.NoError(t, err)
	assert.Equal(t, "123", ref.RatingKey)
	assert.Equal(t, "Alien (1979)", ref.Name)
	assert.Equal(t, map[string]string{"tmdb": "348", "imdb": "tt0078748"}, ref.IDs)
}

func TestCreateItemPathWithoutIdentities(t *testing.T) {
	t.Parallel()
	md := Metadata{RatingKey: "9", Title: "Home Video"}
	path := createItemPath(kindMovie, &md, "Home Video")
	assert.Equal(t, virtualpath.CreateVirtualPath(shared.SchemePlexMovie, "9", "Home Video"), path)
	ref, err := parseItemRef(path, kindMovie)
	require.NoError(t, err)
	assert.Empty(t, ref.IDs)
}

func TestParseItemRefRejects(t *testing.T) {
	t.Parallel()
	_, err := parseItemRef(shared.SchemePlexEpisode+"://1/X", kindMovie)
	require.ErrorIs(t, err, virtualpath.ErrSchemeMismatch)

	for _, path := range []string{
		shared.SchemePlexMovie + "://../library/sections/X",
		shared.SchemePlexMovie + "://12a/X",
		shared.SchemePlexMovie + "://123456789012345678901234567890123/X",
		shared.SchemePlexMovie + "://1/X?tmdb=1&tmdb=2",
	} {
		_, err := parseItemRef(path, kindMovie)
		require.Error(t, err, path)
	}
}

func TestResolveItem(t *testing.T) {
	// Not parallel: uses the global auth config.
	srv := newFakeServer(t)
	useTokens(t, srv.URL, "", "")
	alien := movie("200", "Alien", 1979, "tmdb://348", "imdb://tt0078748")
	aliens := movie("201", "Aliens", 1986, "tmdb://679")
	other := movie("300", "Something Else", 2001, "tmdb://999")
	remakeA := movie("400", "The Thing", 1982, "tmdb://1091")
	remakeB := movie("401", "The Thing", 1982, "tmdb://1091")
	noIDs := movie("500", "Home Video", 0)
	heatLocal := movie("700", "Heat", 1995)
	heat := movie("701", "Heat", 1995, "tmdb://949")
	solo := movie("702", "Solo", 2018)
	srv.addSection("1", "movie", "Movies", alien, aliens, other, remakeA, noIDs, heatLocal, heat, solo)
	srv.addSection("2", "movie", "Movies 4K", remakeB)
	srv.addSection("3", "artist", "Music", Metadata{RatingKey: "600", Type: "artist", Title: "Alien"})
	client := srv.client("")
	ctx := context.Background()

	pathFor := func(key, name, query string) string {
		p := virtualpath.CreateVirtualPath(shared.SchemePlexMovie, key, name)
		if query != "" {
			p += "?" + query
		}
		return p
	}

	t.Run("stored key still matches", func(t *testing.T) {
		md, err := resolveItem(ctx, client, kindMovie, pathFor("200", "Alien (1979)", "tmdb=348"))
		require.NoError(t, err)
		assert.Equal(t, "200", md.RatingKey)
	})

	t.Run("plain path uses the key only", func(t *testing.T) {
		md, err := resolveItem(ctx, client, kindMovie, pathFor("500", "Renamed", ""))
		require.NoError(t, err)
		assert.Equal(t, "500", md.RatingKey)

		_, err = resolveItem(ctx, client, kindMovie, pathFor("999", "Gone", ""))
		require.ErrorContains(t, err, "no longer available")

		_, err = resolveItem(ctx, client, kindMovie, pathFor("600", "Alien", ""))
		require.ErrorContains(t, err, "not a movie")
	})

	t.Run("stale key falls back to guid", func(t *testing.T) {
		md, err := resolveItem(ctx, client, kindMovie, pathFor("999", "Alien (1979)", "tmdb=348"))
		require.NoError(t, err)
		assert.Equal(t, "200", md.RatingKey)
	})

	t.Run("reused key falls back to guid", func(t *testing.T) {
		md, err := resolveItem(ctx, client, kindMovie, pathFor("300", "Alien (1979)", "tmdb=348"))
		require.NoError(t, err)
		assert.Equal(t, "200", md.RatingKey)
	})

	t.Run("key holding only the name defers to the guid", func(t *testing.T) {
		md, err := resolveItem(ctx, client, kindMovie, pathFor("700", "Heat (1995)", "tmdb=949"))
		require.NoError(t, err)
		assert.Equal(t, "701", md.RatingKey)
	})

	t.Run("key holding only the name is kept when no guid matches", func(t *testing.T) {
		md, err := resolveItem(ctx, client, kindMovie, pathFor("702", "Solo (2018)", "tmdb=348348"))
		require.NoError(t, err)
		assert.Equal(t, "702", md.RatingKey)
	})

	t.Run("stale key falls back to title", func(t *testing.T) {
		md, err := resolveItem(ctx, client, kindMovie, pathFor("999", "Aliens (1986)", "imdb=tt0090605"))
		require.NoError(t, err)
		assert.Equal(t, "201", md.RatingKey)
	})

	t.Run("ambiguous guid is an error", func(t *testing.T) {
		_, err := resolveItem(ctx, client, kindMovie, pathFor("999", "", "tmdb=1091"))
		require.ErrorContains(t, err, "multiple plex movies")
	})

	t.Run("ambiguous guid narrowed by name is still ambiguous", func(t *testing.T) {
		_, err := resolveItem(ctx, client, kindMovie, pathFor("999", "The Thing (1982)", "tmdb=1091"))
		require.ErrorContains(t, err, "multiple plex movies")
	})

	t.Run("nothing matches", func(t *testing.T) {
		_, err := resolveItem(ctx, client, kindMovie, pathFor("999", "Nope (2000)", "tmdb=1"))
		require.ErrorContains(t, err, "no longer available")
	})
}

func TestResolveItemServerIgnoresGUIDFilter(t *testing.T) {
	// Not parallel: uses the global auth config.
	srv := newFakeServer(t)
	srv.filtersGUID = false
	useTokens(t, srv.URL, "", "")
	srv.addSection("1", "movie", "Movies",
		movie("200", "Alien", 1979, "tmdb://348"),
		movie("201", "Aliens", 1986, "tmdb://679"),
	)
	md, err := resolveItem(context.Background(), srv.client(""), kindMovie,
		shared.SchemePlexMovie+"://999/Whatever?tmdb=679")
	require.NoError(t, err)
	assert.Equal(t, "201", md.RatingKey)
}

func FuzzParseItemRef(f *testing.F) {
	f.Add(shared.SchemePlexMovie + "://123/Alien (1979)?tmdb=348&imdb=tt0078748")
	f.Add(shared.SchemePlexMovie + "://1/X?tmdb=1&tmdb=2")
	f.Add(shared.SchemePlexMovie + "://123/Alien%20%281979%29;tmdb=348;imdb=tt0078748")
	f.Add(shared.SchemePlexMovie + "://1/X;tmdb=1?tmdb=2")
	f.Add(shared.SchemePlexMovie + "://../x")
	f.Add(shared.SchemePlexMovie + "://12/%ZZ?mb=%")
	f.Add("")
	f.Fuzz(func(t *testing.T, path string) {
		ref, err := parseItemRef(path, kindMovie)
		if err != nil {
			return
		}
		if !validKey(ref.RatingKey) {
			t.Fatalf("parsed key %q is not a plain number", ref.RatingKey)
		}
		for key, v := range ref.IDs {
			if v == "" || virtualpath.ContainsControlChar(v) {
				t.Fatalf("identity %s=%q should have been dropped", key, v)
			}
		}
	})
}
