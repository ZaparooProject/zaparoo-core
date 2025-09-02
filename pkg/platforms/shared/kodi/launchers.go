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
	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
)

// NewKodiLocalLauncher creates a standard KodiLocal launcher for direct video file playback
func NewKodiLocalLauncher() platforms.Launcher {
	return platforms.Launcher{
		ID:       "KodiLocal",
		SystemID: systemdefs.SystemVideo,
		Folders:  []string{"videos", "tvshows"},
		Extensions: []string{
			".avi", ".mp4", ".mkv", ".iso", ".bdmv", ".ifo", ".mpeg", ".mpg",
			".mov", ".wmv", ".flv", ".webm", ".m4v", ".3gp", ".ts", ".m2ts", ".mts",
		},
		Launch: func(cfg *config.Instance, path string) error {
			client := NewClient(cfg)
			return client.LaunchFile(path)
		},
	}
}

// NewKodiMovieLauncher creates a standard KodiMovie launcher for library movie playback
func NewKodiMovieLauncher() platforms.Launcher {
	return platforms.Launcher{
		ID:       "KodiMovie",
		SystemID: systemdefs.SystemMovie,
		Schemes:  []string{SchemeKodiMovie},
		Launch: func(cfg *config.Instance, path string) error {
			client := NewClient(cfg)
			return client.LaunchMovie(path)
		},
		Scanner: func(
			cfg *config.Instance,
			path string,
			results []platforms.ScanResult,
		) ([]platforms.ScanResult, error) {
			client := NewClient(cfg)
			return ScanMovies(client, cfg, path, results)
		},
	}
}

// NewKodiTVLauncher creates a standard KodiTV launcher for library TV episode playback
func NewKodiTVLauncher() platforms.Launcher {
	return platforms.Launcher{
		ID:       "KodiTV",
		SystemID: systemdefs.SystemTV,
		Schemes:  []string{SchemeKodiEpisode},
		Launch: func(cfg *config.Instance, path string) error {
			client := NewClient(cfg)
			return client.LaunchTVEpisode(path)
		},
		Scanner: func(
			cfg *config.Instance,
			path string,
			results []platforms.ScanResult,
		) ([]platforms.ScanResult, error) {
			client := NewClient(cfg)
			return ScanTV(client, cfg, path, results)
		},
	}
}

// NewKodiMusicLauncher creates a KodiMusic launcher for local music files
func NewKodiMusicLauncher() platforms.Launcher {
	return platforms.Launcher{
		ID:       "KodiMusic",
		SystemID: systemdefs.SystemMusic,
		Folders:  []string{"music"},
		Extensions: []string{
			".mp3", ".flac", ".ogg", ".m4a", ".wav", ".wma", ".aac", ".opus",
		},
		Launch: func(cfg *config.Instance, path string) error {
			client := NewClient(cfg)
			return client.LaunchFile(path)
		},
	}
}

// NewKodiAlbumLauncher creates a KodiAlbum launcher for album collection playback
func NewKodiAlbumLauncher() platforms.Launcher {
	return platforms.Launcher{
		ID:       "KodiAlbum",
		SystemID: systemdefs.SystemMusic,
		Schemes:  []string{SchemeKodiAlbum},
		Launch: func(cfg *config.Instance, path string) error {
			client := NewClient(cfg)
			return client.LaunchAlbum(path)
		},
		Scanner: func(
			cfg *config.Instance,
			path string,
			results []platforms.ScanResult,
		) ([]platforms.ScanResult, error) {
			client := NewClient(cfg)
			return ScanAlbums(client, cfg, path, results)
		},
	}
}

// NewKodiArtistLauncher creates a KodiArtist launcher for artist collection playback
func NewKodiArtistLauncher() platforms.Launcher {
	return platforms.Launcher{
		ID:       "KodiArtist",
		SystemID: systemdefs.SystemMusic,
		Schemes:  []string{SchemeKodiArtist},
		Launch: func(cfg *config.Instance, path string) error {
			client := NewClient(cfg)
			return client.LaunchArtist(path)
		},
		Scanner: func(
			cfg *config.Instance,
			path string,
			results []platforms.ScanResult,
		) ([]platforms.ScanResult, error) {
			client := NewClient(cfg)
			return ScanArtists(client, cfg, path, results)
		},
	}
}

// NewKodiTVShowLauncher creates a KodiTVShow launcher for TV show collection playback
func NewKodiTVShowLauncher() platforms.Launcher {
	return platforms.Launcher{
		ID:       "KodiTVShow",
		SystemID: systemdefs.SystemTV,
		Schemes:  []string{SchemeKodiShow},
		Launch: func(cfg *config.Instance, path string) error {
			client := NewClient(cfg)
			return client.LaunchTVShow(path)
		},
		Scanner: func(
			cfg *config.Instance,
			path string,
			results []platforms.ScanResult,
		) ([]platforms.ScanResult, error) {
			client := NewClient(cfg)
			return ScanTVShows(client, cfg, path, results)
		},
	}
}

// NewKodiSongLauncher creates a KodiSong launcher for individual song playback
func NewKodiSongLauncher() platforms.Launcher {
	return platforms.Launcher{
		ID:       "KodiSong",
		SystemID: systemdefs.SystemMusic,
		Schemes:  []string{SchemeKodiSong},
		Launch: func(cfg *config.Instance, path string) error {
			client := NewClient(cfg)
			return client.LaunchSong(path)
		},
		Scanner: func(
			cfg *config.Instance,
			path string,
			results []platforms.ScanResult,
		) ([]platforms.ScanResult, error) {
			client := NewClient(cfg)
			return ScanSongs(client, cfg, path, results)
		},
	}
}
