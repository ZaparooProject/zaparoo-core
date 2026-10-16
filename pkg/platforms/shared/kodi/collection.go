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
	"fmt"
	"strconv"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/virtualpath"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared"
)

// episodeName and songName match the names the scanners index the same
// media under.
func episodeName(show TVShow, episode Episode) string {
	return show.Label + " - " + episode.Label
}

func songName(song *Song) string {
	return song.Artist + " - " + song.Album + " - " + song.Label
}

// CollectTVShow lists the episodes Kodi holds for the show a path names.
func CollectTVShow(ctx context.Context, client KodiClient, path string) ([]platforms.CollectionItem, error) {
	show, err := resolveShow(ctx, client, path)
	if err != nil {
		return nil, err
	}
	episodes, err := client.GetEpisodes(ctx, show.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get episodes for show %d: %w", show.ID, err)
	}
	items := make([]platforms.CollectionItem, 0, len(episodes))
	for _, episode := range episodes {
		name := episodeName(show, episode)
		items = append(items, platforms.CollectionItem{
			Name: name,
			Path: virtualpath.CreateVirtualPath(shared.SchemeKodiEpisode, strconv.Itoa(episode.ID), name),
		})
	}
	return items, nil
}

// CollectAlbum lists the songs on the album a path names.
func CollectAlbum(ctx context.Context, client KodiClient, path string) ([]platforms.CollectionItem, error) {
	id, err := collectionID(path, shared.SchemeKodiAlbum, "album")
	if err != nil {
		return nil, err
	}
	songs, err := client.GetAlbumSongs(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get songs for album %d: %w", id, err)
	}
	return songItems(songs), nil
}

// CollectArtist lists the songs by the artist a path names.
func CollectArtist(ctx context.Context, client KodiClient, path string) ([]platforms.CollectionItem, error) {
	id, err := collectionID(path, shared.SchemeKodiArtist, "artist")
	if err != nil {
		return nil, err
	}
	songs, err := client.GetArtistSongs(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get songs for artist %d: %w", id, err)
	}
	return songItems(songs), nil
}

func collectionID(path, scheme, kind string) (int, error) {
	idStr, err := virtualpath.ExtractSchemeID(path, scheme)
	if err != nil {
		return 0, fmt.Errorf("failed to extract %s ID from path: %w", kind, err)
	}
	id, err := strconv.Atoi(idStr)
	if err != nil {
		return 0, fmt.Errorf("failed to parse %s ID %q: %w", kind, idStr, err)
	}
	return id, nil
}

func songItems(songs []Song) []platforms.CollectionItem {
	items := make([]platforms.CollectionItem, 0, len(songs))
	for i := range songs {
		song := &songs[i]
		name := songName(song)
		items = append(items, platforms.CollectionItem{
			Name: name,
			Path: virtualpath.CreateVirtualPath(shared.SchemeKodiSong, strconv.Itoa(song.ID), name),
		})
	}
	return items
}
