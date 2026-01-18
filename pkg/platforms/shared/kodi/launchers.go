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
	"os"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared"
)

// NewKodiLocalLauncher creates a standard KodiLocalVideo launcher for direct video file playback
func NewKodiLocalLauncher() platforms.Launcher {
	id := shared.LauncherKodiLocalVideo
	groups := []string{shared.GroupKodi}
	return platforms.Launcher{
		ID:                  id,
		Groups:              groups,
		SystemID:            systemdefs.SystemVideo,
		Folders:             []string{"videos", "tvshows"},
		UsesRunningInstance: platforms.InstanceKodi, // Sends commands to running Kodi via JSON-RPC
		Extensions: []string{
			".avi", ".mp4", ".mkv", ".iso", ".bdmv", ".ifo", ".mpeg", ".mpg",
			".mov", ".wmv", ".flv", ".webm", ".m4v", ".3gp", ".ts", ".m2ts", ".mts",
			".m3u", ".m3u8",
		},
		Launch: func(cfg *config.Instance, path string, _ *platforms.LaunchOptions) (*os.Process, error) {
			client := NewClientWithLauncherID(cfg, id, groups)
			return nil, client.LaunchFile(path)
		},
	}
}

// NewKodiMovieLauncher creates a standard KodiMovie launcher for library movie playback
func NewKodiMovieLauncher() platforms.Launcher {
	id := shared.LauncherKodiMovie
	groups := []string{shared.GroupKodi}
	return platforms.Launcher{
		ID:                  id,
		Groups:              groups,
		SystemID:            systemdefs.SystemMovie,
		Schemes:             []string{shared.SchemeKodiMovie},
		SkipFilesystemScan:  true,                   // Uses Kodi API via Scanner, no filesystem scanning needed
		UsesRunningInstance: platforms.InstanceKodi, // Sends commands to running Kodi via JSON-RPC
		Launch: func(cfg *config.Instance, path string, _ *platforms.LaunchOptions) (*os.Process, error) {
			client := NewClientWithLauncherID(cfg, id, groups)
			return nil, client.LaunchMovie(path)
		},
		Scanner: func(
			ctx context.Context,
			cfg *config.Instance,
			path string,
			results []platforms.ScanResult,
		) ([]platforms.ScanResult, error) {
			client := NewClientWithLauncherID(cfg, id, groups)
			return ScanMovies(ctx, client, cfg, path, results)
		},
	}
}

// NewKodiTVLauncher creates a standard KodiTVEpisode launcher for library TV episode playback
func NewKodiTVLauncher() platforms.Launcher {
	id := shared.LauncherKodiTVEpisode
	groups := []string{shared.GroupKodi, shared.GroupKodiTV}
	return platforms.Launcher{
		ID:                  id,
		Groups:              groups,
		SystemID:            systemdefs.SystemTVEpisode,
		Schemes:             []string{shared.SchemeKodiEpisode},
		SkipFilesystemScan:  true,                   // Uses Kodi API via Scanner, no filesystem scanning needed
		UsesRunningInstance: platforms.InstanceKodi, // Sends commands to running Kodi via JSON-RPC
		Launch: func(cfg *config.Instance, path string, _ *platforms.LaunchOptions) (*os.Process, error) {
			client := NewClientWithLauncherID(cfg, id, groups)
			return nil, client.LaunchTVEpisode(path)
		},
		Scanner: func(
			ctx context.Context,
			cfg *config.Instance,
			path string,
			results []platforms.ScanResult,
		) ([]platforms.ScanResult, error) {
			client := NewClientWithLauncherID(cfg, id, groups)
			return ScanTV(ctx, client, cfg, path, results)
		},
	}
}

// NewKodiMusicLauncher creates a KodiLocalAudio launcher for local music files
func NewKodiMusicLauncher() platforms.Launcher {
	id := shared.LauncherKodiLocalAudio
	groups := []string{shared.GroupKodi, shared.GroupKodiMusic}
	return platforms.Launcher{
		ID:                  id,
		Groups:              groups,
		SystemID:            systemdefs.SystemMusicTrack,
		Folders:             []string{"music"},
		UsesRunningInstance: platforms.InstanceKodi, // Sends commands to running Kodi via JSON-RPC
		Extensions: []string{
			".mp3", ".flac", ".ogg", ".m4a", ".wav", ".wma", ".aac", ".opus",
		},
		Launch: func(cfg *config.Instance, path string, _ *platforms.LaunchOptions) (*os.Process, error) {
			client := NewClientWithLauncherID(cfg, id, groups)
			return nil, client.LaunchFile(path)
		},
	}
}

// NewKodiAlbumLauncher creates a KodiAlbum launcher for album collection playback
func NewKodiAlbumLauncher() platforms.Launcher {
	id := shared.LauncherKodiAlbum
	groups := []string{shared.GroupKodi, shared.GroupKodiMusic}
	return platforms.Launcher{
		ID:                  id,
		Groups:              groups,
		SystemID:            systemdefs.SystemMusicAlbum,
		Schemes:             []string{shared.SchemeKodiAlbum},
		SkipFilesystemScan:  true,                   // Uses Kodi API via Scanner, no filesystem scanning needed
		UsesRunningInstance: platforms.InstanceKodi, // Sends commands to running Kodi via JSON-RPC
		Launch: func(cfg *config.Instance, path string, _ *platforms.LaunchOptions) (*os.Process, error) {
			client := NewClientWithLauncherID(cfg, id, groups)
			return nil, client.LaunchAlbum(path)
		},
		Scanner: func(
			ctx context.Context,
			cfg *config.Instance,
			path string,
			results []platforms.ScanResult,
		) ([]platforms.ScanResult, error) {
			client := NewClientWithLauncherID(cfg, id, groups)
			return ScanAlbums(ctx, client, cfg, path, results)
		},
	}
}

// NewKodiArtistLauncher creates a KodiArtist launcher for artist collection playback
func NewKodiArtistLauncher() platforms.Launcher {
	id := shared.LauncherKodiArtist
	groups := []string{shared.GroupKodi, shared.GroupKodiMusic}
	return platforms.Launcher{
		ID:                  id,
		Groups:              groups,
		SystemID:            systemdefs.SystemMusicArtist,
		Schemes:             []string{shared.SchemeKodiArtist},
		SkipFilesystemScan:  true,                   // Uses Kodi API via Scanner, no filesystem scanning needed
		UsesRunningInstance: platforms.InstanceKodi, // Sends commands to running Kodi via JSON-RPC
		Launch: func(cfg *config.Instance, path string, _ *platforms.LaunchOptions) (*os.Process, error) {
			client := NewClientWithLauncherID(cfg, id, groups)
			return nil, client.LaunchArtist(path)
		},
		Scanner: func(
			ctx context.Context,
			cfg *config.Instance,
			path string,
			results []platforms.ScanResult,
		) ([]platforms.ScanResult, error) {
			client := NewClientWithLauncherID(cfg, id, groups)
			return ScanArtists(ctx, client, cfg, path, results)
		},
	}
}

// NewKodiTVShowLauncher creates a KodiTVShow launcher for TV show collection playback
func NewKodiTVShowLauncher() platforms.Launcher {
	id := shared.LauncherKodiTVShow
	groups := []string{shared.GroupKodi, shared.GroupKodiTV}
	return platforms.Launcher{
		ID:                  id,
		Groups:              groups,
		SystemID:            systemdefs.SystemTVShow,
		Schemes:             []string{shared.SchemeKodiShow},
		SkipFilesystemScan:  true,                   // Uses Kodi API via Scanner, no filesystem scanning needed
		UsesRunningInstance: platforms.InstanceKodi, // Sends commands to running Kodi via JSON-RPC
		Launch: func(cfg *config.Instance, path string, _ *platforms.LaunchOptions) (*os.Process, error) {
			client := NewClientWithLauncherID(cfg, id, groups)
			return nil, client.LaunchTVShow(path)
		},
		Scanner: func(
			ctx context.Context,
			cfg *config.Instance,
			path string,
			results []platforms.ScanResult,
		) ([]platforms.ScanResult, error) {
			client := NewClientWithLauncherID(cfg, id, groups)
			return ScanTVShows(ctx, client, cfg, path, results)
		},
	}
}

// NewKodiSongLauncher creates a KodiSong launcher for individual song playback
func NewKodiSongLauncher() platforms.Launcher {
	id := shared.LauncherKodiSong
	groups := []string{shared.GroupKodi, shared.GroupKodiMusic}
	return platforms.Launcher{
		ID:                  id,
		Groups:              groups,
		SystemID:            systemdefs.SystemMusicTrack,
		Schemes:             []string{shared.SchemeKodiSong},
		SkipFilesystemScan:  true,                   // Uses Kodi API via Scanner, no filesystem scanning needed
		UsesRunningInstance: platforms.InstanceKodi, // Sends commands to running Kodi via JSON-RPC
		Launch: func(cfg *config.Instance, path string, _ *platforms.LaunchOptions) (*os.Process, error) {
			client := NewClientWithLauncherID(cfg, id, groups)
			return nil, client.LaunchSong(path)
		},
		Scanner: func(
			ctx context.Context,
			cfg *config.Instance,
			path string,
			results []platforms.ScanResult,
		) ([]platforms.ScanResult, error) {
			client := NewClientWithLauncherID(cfg, id, groups)
			return ScanSongs(ctx, client, cfg, path, results)
		},
	}
}
