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

package kodi

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestCollectTVShow_ListsEpisodesOfTheResolvedShow(t *testing.T) {
	t.Parallel()

	client := &MockKodiClient{}
	client.On("GetTVShows", mock.Anything).Return([]TVShow{
		{ID: 7, Label: "Other Show"},
		{ID: 42, Label: "Purpley"},
	}, nil)
	client.On("GetEpisodes", mock.Anything, 42).Return([]Episode{
		{ID: 100, Label: "Pilot"},
		{ID: 101, Label: "Second"},
	}, nil)

	items, err := CollectTVShow(context.Background(), client, "kodi-show://42/Purpley")
	require.NoError(t, err)
	assert.Equal(t, []platforms.CollectionItem{
		{Name: "Purpley - Pilot", Path: "kodi-episode://100/Purpley%20-%20Pilot"},
		{Name: "Purpley - Second", Path: "kodi-episode://101/Purpley%20-%20Second"},
	}, items)
	client.AssertExpectations(t)
}

func TestCollectTVShow_FollowsCurrentShowIDWhenLegacyIDMoved(t *testing.T) {
	t.Parallel()

	client := &MockKodiClient{}
	client.On("GetTVShows", mock.Anything).Return([]TVShow{
		{ID: 9, Label: "Purpley", UniqueIDs: map[string]string{"tvdb": "555"}},
	}, nil)
	client.On("GetEpisodes", mock.Anything, 9).Return([]Episode{{ID: 1, Label: "Pilot"}}, nil)

	items, err := CollectTVShow(context.Background(), client, "kodi-show://42/Purpley?tvdb=555")
	require.NoError(t, err)
	require.Len(t, items, 1)
	client.AssertExpectations(t)
}

func TestCollectTVShow_Errors(t *testing.T) {
	t.Parallel()

	t.Run("show missing from library", func(t *testing.T) {
		t.Parallel()
		client := &MockKodiClient{}
		client.On("GetTVShows", mock.Anything).Return([]TVShow{{ID: 1, Label: "Else"}}, nil)
		_, err := CollectTVShow(context.Background(), client, "kodi-show://42/Purpley?tvdb=555")
		require.Error(t, err)
		client.AssertNotCalled(t, "GetEpisodes", mock.Anything, mock.Anything)
	})

	t.Run("episode lookup fails", func(t *testing.T) {
		t.Parallel()
		client := &MockKodiClient{}
		client.On("GetTVShows", mock.Anything).Return([]TVShow{{ID: 42, Label: "Purpley"}}, nil)
		client.On("GetEpisodes", mock.Anything, 42).Return(nil, errors.New("kodi down"))
		_, err := CollectTVShow(context.Background(), client, "kodi-show://42/Purpley")
		require.ErrorContains(t, err, "kodi down")
	})

	t.Run("wrong scheme", func(t *testing.T) {
		t.Parallel()
		_, err := CollectTVShow(context.Background(), &MockKodiClient{}, "kodi-album://42/Purpley")
		require.Error(t, err)
	})
}

func TestCollectAlbumAndArtist_ListSongs(t *testing.T) {
	t.Parallel()

	songs := []Song{
		{ID: 5, Label: "Track", Artist: "Band", Album: "Record"},
		{ID: 6, Label: "Other", Artist: "Band", Album: "Record"},
	}
	want := []platforms.CollectionItem{
		{Name: "Band - Record - Track", Path: "kodi-song://5/Band%20-%20Record%20-%20Track"},
		{Name: "Band - Record - Other", Path: "kodi-song://6/Band%20-%20Record%20-%20Other"},
	}

	t.Run("album", func(t *testing.T) {
		t.Parallel()
		client := &MockKodiClient{}
		client.On("GetAlbumSongs", mock.Anything, 17).Return(songs, nil)
		items, err := CollectAlbum(context.Background(), client, "kodi-album://17/Record")
		require.NoError(t, err)
		assert.Equal(t, want, items)
		client.AssertExpectations(t)
	})

	t.Run("artist", func(t *testing.T) {
		t.Parallel()
		client := &MockKodiClient{}
		client.On("GetArtistSongs", mock.Anything, 9).Return(songs, nil)
		items, err := CollectArtist(context.Background(), client, "kodi-artist://9/Band")
		require.NoError(t, err)
		assert.Equal(t, want, items)
		client.AssertExpectations(t)
	})
}

func TestCollectAlbumAndArtist_Errors(t *testing.T) {
	t.Parallel()

	t.Run("non-numeric album ID", func(t *testing.T) {
		t.Parallel()
		_, err := CollectAlbum(context.Background(), &MockKodiClient{}, "kodi-album://abc/Record")
		require.Error(t, err)
	})

	t.Run("wrong scheme", func(t *testing.T) {
		t.Parallel()
		_, err := CollectArtist(context.Background(), &MockKodiClient{}, "kodi-album://9/Band")
		require.Error(t, err)
	})

	t.Run("lookup fails", func(t *testing.T) {
		t.Parallel()
		client := &MockKodiClient{}
		client.On("GetAlbumSongs", mock.Anything, 17).Return(nil, errors.New("kodi down"))
		_, err := CollectAlbum(context.Background(), client, "kodi-album://17/Record")
		require.ErrorContains(t, err, "kodi down")
	})

	t.Run("empty album lists nothing", func(t *testing.T) {
		t.Parallel()
		client := &MockKodiClient{}
		client.On("GetAlbumSongs", mock.Anything, 17).Return([]Song{}, nil)
		items, err := CollectAlbum(context.Background(), client, "kodi-album://17/Record")
		require.NoError(t, err)
		assert.Empty(t, items)
	})
}

func TestLaunchers_ExposeCollections(t *testing.T) {
	t.Parallel()

	assert.NotNil(t, NewKodiTVShowLauncher().Collection)
	assert.NotNil(t, NewKodiAlbumLauncher().Collection)
	assert.NotNil(t, NewKodiArtistLauncher().Collection)
	assert.Nil(t, NewKodiTVLauncher().Collection)
	assert.Nil(t, NewKodiSongLauncher().Collection)
}

func TestCollectTVShow_OrdersEpisodesBySeasonAndEpisode(t *testing.T) {
	t.Parallel()

	client := &MockKodiClient{}
	client.On("GetTVShows", mock.Anything).Return([]TVShow{{ID: 42, Label: "Purpley"}}, nil)
	returned := []Episode{
		{ID: 5, Label: "S2E1", Season: 2, Episode: 1},
		{ID: 3, Label: "S1E2", Season: 1, Episode: 2},
		{ID: 9, Label: "Special", Season: 0, Episode: 1},
		{ID: 4, Label: "S1E1", Season: 1, Episode: 1},
	}
	client.On("GetEpisodes", mock.Anything, 42).Return(returned, nil)

	items, err := CollectTVShow(context.Background(), client, "kodi-show://42/Purpley")

	require.NoError(t, err)
	names := make([]string, 0, len(items))
	for _, item := range items {
		names = append(names, item.Name)
	}
	assert.Equal(t, []string{
		"Purpley - Special", "Purpley - S1E1", "Purpley - S1E2", "Purpley - S2E1",
	}, names)
	assert.Equal(t, 5, returned[0].ID, "the list Kodi returned is left as it was")
}

func TestCollectSongs_OrderByAlbumDiscAndTrack(t *testing.T) {
	t.Parallel()

	songs := []Song{
		{ID: 1, Label: "B2", Album: "Beta", AlbumID: 2, Track: 2},
		{ID: 2, Label: "A-d2t1", Album: "Alpha", AlbumID: 1, Disc: 2, Track: 1},
		{ID: 3, Label: "B1", Album: "Beta", AlbumID: 2, Track: 1},
		{ID: 4, Label: "A-d1t2", Album: "Alpha", AlbumID: 1, Disc: 1, Track: 2},
		{ID: 5, Label: "A-d1t1", Album: "Alpha", AlbumID: 1, Disc: 1, Track: 1},
	}
	check := func(t *testing.T, items []platforms.CollectionItem, want []string) {
		t.Helper()
		labels := make([]string, 0, len(items))
		for _, item := range items {
			labels = append(labels, item.Name[strings.LastIndex(item.Name, " - ")+3:])
		}
		assert.Equal(t, want, labels)
	}

	t.Run("artist", func(t *testing.T) {
		t.Parallel()
		client := &MockKodiClient{}
		client.On("GetArtistSongs", mock.Anything, 9).Return(songs, nil)
		items, err := CollectArtist(context.Background(), client, "kodi-artist://9/Band")
		require.NoError(t, err)
		check(t, items, []string{"A-d1t1", "A-d1t2", "A-d2t1", "B1", "B2"})
	})

	t.Run("album", func(t *testing.T) {
		t.Parallel()
		client := &MockKodiClient{}
		client.On("GetAlbumSongs", mock.Anything, 1).Return([]Song{songs[1], songs[3], songs[4]}, nil)
		items, err := CollectAlbum(context.Background(), client, "kodi-album://1/Alpha")
		require.NoError(t, err)
		check(t, items, []string{"A-d1t1", "A-d1t2", "A-d2t1"})
	})
}
