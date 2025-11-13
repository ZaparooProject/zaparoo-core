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

package kodi

import (
	"context"
	"fmt"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared"
)

// ScanMovies scans movies from Kodi library using the provided client
func ScanMovies(
	ctx context.Context,
	client KodiClient,
	_ *config.Instance,
	_ string,
	results []platforms.ScanResult,
) ([]platforms.ScanResult, error) {
	movies, err := client.GetMovies(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get movies: %w", err)
	}

	for _, movie := range movies {
		results = append(results, platforms.ScanResult{
			Name:  movie.Label,
			Path:  helpers.CreateVirtualPath(shared.SchemeKodiMovie, fmt.Sprintf("%d", movie.ID), movie.Label),
			NoExt: true,
		})
	}

	return results, nil
}

// ScanTV scans TV shows and episodes from Kodi library using the provided client
func ScanTV(
	ctx context.Context,
	client KodiClient,
	_ *config.Instance,
	_ string,
	results []platforms.ScanResult,
) ([]platforms.ScanResult, error) {
	tvShows, err := client.GetTVShows(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get TV shows: %w", err)
	}

	for _, show := range tvShows {
		episodes, err := client.GetEpisodes(ctx, show.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to get episodes for show %d: %w", show.ID, err)
		}

		for _, ep := range episodes {
			label := show.Label + " - " + ep.Label
			results = append(results, platforms.ScanResult{
				Name:  label,
				Path:  helpers.CreateVirtualPath(shared.SchemeKodiEpisode, fmt.Sprintf("%d", ep.ID), label),
				NoExt: true,
			})
		}
	}

	return results, nil
}

// ScanSongs scans songs from Kodi library using the provided client
func ScanSongs(
	ctx context.Context,
	client KodiClient,
	_ *config.Instance,
	_ string,
	results []platforms.ScanResult,
) ([]platforms.ScanResult, error) {
	songs, err := client.GetSongs(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get songs: %w", err)
	}

	for _, song := range songs {
		name := song.Artist + " - " + song.Label
		results = append(results, platforms.ScanResult{
			Name:  name,
			Path:  helpers.CreateVirtualPath(shared.SchemeKodiSong, fmt.Sprintf("%d", song.ID), name),
			NoExt: true,
		})
	}

	return results, nil
}

// ScanAlbums scans albums from Kodi library using the provided client
func ScanAlbums(
	ctx context.Context,
	client KodiClient,
	_ *config.Instance,
	_ string,
	results []platforms.ScanResult,
) ([]platforms.ScanResult, error) {
	albums, err := client.GetAlbums(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get albums: %w", err)
	}

	for _, album := range albums {
		name := album.Artist + " - " + album.Label
		if album.Year > 0 {
			name = fmt.Sprintf("%s (%d)", name, album.Year)
		}
		results = append(results, platforms.ScanResult{
			Name:  name,
			Path:  helpers.CreateVirtualPath(shared.SchemeKodiAlbum, fmt.Sprintf("%d", album.ID), name),
			NoExt: true,
		})
	}

	return results, nil
}

// ScanArtists scans artists from Kodi library using the provided client
func ScanArtists(
	ctx context.Context,
	client KodiClient,
	_ *config.Instance,
	_ string,
	results []platforms.ScanResult,
) ([]platforms.ScanResult, error) {
	artists, err := client.GetArtists(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get artists: %w", err)
	}

	for _, artist := range artists {
		// Skip "Various Artists" and compilation artists
		if artist.Label == "Various Artists" || artist.Label == "Various" {
			continue
		}

		results = append(results, platforms.ScanResult{
			Name:  artist.Label,
			Path:  helpers.CreateVirtualPath(shared.SchemeKodiArtist, fmt.Sprintf("%d", artist.ID), artist.Label),
			NoExt: true,
		})
	}

	return results, nil
}

// ScanTVShows scans TV shows from Kodi library using the provided client
func ScanTVShows(
	ctx context.Context,
	client KodiClient,
	_ *config.Instance,
	_ string,
	results []platforms.ScanResult,
) ([]platforms.ScanResult, error) {
	shows, err := client.GetTVShows(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get TV shows: %w", err)
	}

	for _, show := range shows {
		results = append(results, platforms.ScanResult{
			Name:  show.Label,
			Path:  helpers.CreateVirtualPath(shared.SchemeKodiShow, fmt.Sprintf("%d", show.ID), show.Label),
			NoExt: true,
		})
	}

	return results, nil
}
