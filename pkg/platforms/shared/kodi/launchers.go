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
	"os"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared"
	"github.com/jonboulle/clockwork"
)

// readyPollInterval is how often a readiness probe asks Kodi for its players.
const readyPollInterval = 250 * time.Millisecond

// kodiReady builds the WaitForReady probe shared by the Kodi launchers.
func kodiReady(id string, groups []string) func(context.Context, *config.Instance, *models.ActiveMedia) error {
	return waitForPlayer(func(cfg *config.Instance) KodiClient {
		return NewClientWithLauncherID(cfg, id, groups)
	}, clockwork.NewRealClock())
}

// waitForPlayer returns a launcher readiness probe that polls Kodi until the
// launched media is playing, which is when playback controls act on it.
// Player.Open returns before playback starts, and Kodi may not be reachable
// at all yet, so a failed query is retried and reported as unavailable if it
// is still failing when the wait ends.
//
// A player that is already running when the probe starts is playing something
// else. Its item is remembered, and the probe is ready once the item changes
// or is the launched media by name.
func waitForPlayer(
	newClient func(*config.Instance) KodiClient,
	clock clockwork.Clock,
) func(context.Context, *config.Instance, *models.ActiveMedia) error {
	return func(ctx context.Context, cfg *config.Instance, media *models.ActiveMedia) error {
		client := newClient(cfg)
		var before *PlayerItem
		first := true
		return platforms.PollReady(ctx, clock, readyPollInterval, func(ctx context.Context) (bool, error) {
			players, err := client.GetActivePlayers(ctx)
			if err != nil {
				return false, fmt.Errorf("%w: %w", platforms.ErrReadinessUnavailable, err)
			}
			if len(players) == 0 {
				first = false
				return false, nil
			}
			if media == nil {
				return true, nil
			}
			item, itemErr := client.GetPlayerItem(ctx, players[0].ID)
			if itemErr != nil || item == nil {
				// An item that cannot be read cannot be told apart from the
				// last one; an active player is the best signal left.
				return true, nil //nolint:nilerr // see above
			}
			if media.Name != "" && (item.Label == media.Name || item.Title == media.Name) {
				return true, nil
			}
			if first && before == nil {
				before = item
				first = false
				return false, nil
			}
			first = false
			return before == nil || !sameKodiItem(before, item), nil
		})
	}
}

// sameKodiItem reports whether two player items are the same media.
func sameKodiItem(a, b *PlayerItem) bool {
	return a.File == b.File && a.Label == b.Label && a.Type == b.Type
}

// kodiControls builds a Controls map for Kodi launchers with standard
// playback controls. The launcherID and groups are captured by closures
// so each control creates the correct Kodi client at invocation time.
func kodiControls(
	id string, groups []string,
) map[string]platforms.Control {
	cf := func(
		fn func(context.Context, KodiClient) error,
	) platforms.ControlFunc {
		return func(
			ctx context.Context, cfg *config.Instance,
			_ platforms.ControlParams,
		) error {
			return fn(ctx, NewClientWithLauncherID(cfg, id, groups))
		}
	}
	return map[string]platforms.Control{
		platforms.ControlTogglePause: {
			Func: cf(func(ctx context.Context, c KodiClient) error {
				return c.PlayPause(ctx)
			}),
		},
		platforms.ControlPause: {
			Func: cf(func(ctx context.Context, c KodiClient) error {
				return c.SetPaused(ctx, true)
			}),
		},
		platforms.ControlResume: {
			Func: cf(func(ctx context.Context, c KodiClient) error {
				return c.SetPaused(ctx, false)
			}),
		},
		platforms.ControlStop: {
			Func: cf(func(ctx context.Context, c KodiClient) error {
				return c.Stop(ctx)
			}),
		},
		platforms.ControlFastForward: {
			Func: cf(func(ctx context.Context, c KodiClient) error {
				return c.FastForward(ctx)
			}),
		},
		platforms.ControlRewind: {
			Func: cf(func(ctx context.Context, c KodiClient) error {
				return c.Rewind(ctx)
			}),
		},
		platforms.ControlNext: {
			Func: cf(func(ctx context.Context, c KodiClient) error {
				return c.GoTo(ctx, "next")
			}),
		},
		platforms.ControlPrevious: {
			Func: cf(func(ctx context.Context, c KodiClient) error {
				return c.GoTo(ctx, "previous")
			}),
		},
	}
}

// NewKodiLocalLauncher creates a standard KodiLocalVideo launcher for direct video file playback
func NewKodiLocalLauncher() platforms.Launcher {
	id := shared.LauncherKodiLocalVideo
	groups := []string{shared.LauncherGroupKodi}
	return platforms.Launcher{
		Controls:            kodiControls(id, groups),
		WaitForReady:        kodiReady(id, groups),
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
		Launch: func(cfg *config.Instance, path string, opts *platforms.LaunchOptions) (*os.Process, error) {
			client := NewClientWithLauncherID(cfg, id, groups)
			return nil, launchTracked(client, opts, expectation{}, func(c KodiClient) error {
				return c.LaunchFile(path)
			})
		},
	}
}

// NewKodiMovieLauncher creates a standard KodiMovie launcher for library movie playback
func NewKodiMovieLauncher() platforms.Launcher {
	id := shared.LauncherKodiMovie
	groups := []string{shared.LauncherGroupKodi}
	return platforms.Launcher{
		Controls:            kodiControls(id, groups),
		WaitForReady:        kodiReady(id, groups),
		ID:                  id,
		Groups:              groups,
		SystemID:            systemdefs.SystemMovie,
		Schemes:             []string{shared.SchemeKodiMovie},
		SkipFilesystemScan:  true,                   // Uses Kodi API via Scanner, no filesystem scanning needed
		UsesRunningInstance: platforms.InstanceKodi, // Sends commands to running Kodi via JSON-RPC
		Launch: func(cfg *config.Instance, path string, opts *platforms.LaunchOptions) (*os.Process, error) {
			client := NewClientWithLauncherID(cfg, id, groups)
			exp := libraryExpectation(path, shared.SchemeKodiMovie, "movie")
			return nil, launchTracked(client, opts, exp, func(c KodiClient) error {
				return c.LaunchMovie(path)
			})
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
	groups := []string{shared.LauncherGroupKodi, shared.LauncherGroupKodiTV}
	return platforms.Launcher{
		Controls:            kodiControls(id, groups),
		WaitForReady:        kodiReady(id, groups),
		ID:                  id,
		Groups:              groups,
		SystemID:            systemdefs.SystemTVEpisode,
		Schemes:             []string{shared.SchemeKodiEpisode},
		SkipFilesystemScan:  true,                   // Uses Kodi API via Scanner, no filesystem scanning needed
		UsesRunningInstance: platforms.InstanceKodi, // Sends commands to running Kodi via JSON-RPC
		Launch: func(cfg *config.Instance, path string, opts *platforms.LaunchOptions) (*os.Process, error) {
			client := NewClientWithLauncherID(cfg, id, groups)
			exp := libraryExpectation(path, shared.SchemeKodiEpisode, "episode")
			return nil, launchTracked(client, opts, exp, func(c KodiClient) error {
				return c.LaunchTVEpisode(path)
			})
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
	groups := []string{shared.LauncherGroupKodi, shared.LauncherGroupKodiMusic}
	return platforms.Launcher{
		Controls:            kodiControls(id, groups),
		WaitForReady:        kodiReady(id, groups),
		ID:                  id,
		Groups:              groups,
		SystemID:            systemdefs.SystemMusicTrack,
		Folders:             []string{"music"},
		UsesRunningInstance: platforms.InstanceKodi, // Sends commands to running Kodi via JSON-RPC
		Extensions: []string{
			".mp3", ".flac", ".ogg", ".m4a", ".wav", ".wma", ".aac", ".opus",
		},
		Launch: func(cfg *config.Instance, path string, opts *platforms.LaunchOptions) (*os.Process, error) {
			client := NewClientWithLauncherID(cfg, id, groups)
			return nil, launchTracked(client, opts, expectation{}, func(c KodiClient) error {
				return c.LaunchFile(path)
			})
		},
	}
}

// NewKodiAlbumLauncher creates a KodiAlbum launcher for album collection playback
func NewKodiAlbumLauncher() platforms.Launcher {
	id := shared.LauncherKodiAlbum
	groups := []string{shared.LauncherGroupKodi, shared.LauncherGroupKodiMusic}
	return platforms.Launcher{
		Controls:            kodiControls(id, groups),
		WaitForReady:        kodiReady(id, groups),
		ID:                  id,
		Groups:              groups,
		SystemID:            systemdefs.SystemMusicAlbum,
		Schemes:             []string{shared.SchemeKodiAlbum},
		SkipFilesystemScan:  true,                   // Uses Kodi API via Scanner, no filesystem scanning needed
		UsesRunningInstance: platforms.InstanceKodi, // Sends commands to running Kodi via JSON-RPC
		Launch: func(cfg *config.Instance, path string, opts *platforms.LaunchOptions) (*os.Process, error) {
			client := NewClientWithLauncherID(cfg, id, groups)
			return nil, launchTracked(client, opts, expectation{queue: true}, func(c KodiClient) error {
				return c.LaunchAlbum(path)
			})
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
		Collection: func(
			ctx context.Context, cfg *config.Instance, path string,
		) ([]platforms.CollectionItem, error) {
			return CollectAlbum(ctx, NewClientWithLauncherID(cfg, id, groups), path)
		},
	}
}

// NewKodiArtistLauncher creates a KodiArtist launcher for artist collection playback
func NewKodiArtistLauncher() platforms.Launcher {
	id := shared.LauncherKodiArtist
	groups := []string{shared.LauncherGroupKodi, shared.LauncherGroupKodiMusic}
	return platforms.Launcher{
		Controls:            kodiControls(id, groups),
		WaitForReady:        kodiReady(id, groups),
		ID:                  id,
		Groups:              groups,
		SystemID:            systemdefs.SystemMusicArtist,
		Schemes:             []string{shared.SchemeKodiArtist},
		SkipFilesystemScan:  true,                   // Uses Kodi API via Scanner, no filesystem scanning needed
		UsesRunningInstance: platforms.InstanceKodi, // Sends commands to running Kodi via JSON-RPC
		Launch: func(cfg *config.Instance, path string, opts *platforms.LaunchOptions) (*os.Process, error) {
			client := NewClientWithLauncherID(cfg, id, groups)
			return nil, launchTracked(client, opts, expectation{queue: true}, func(c KodiClient) error {
				return c.LaunchArtist(path)
			})
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
		Collection: func(
			ctx context.Context, cfg *config.Instance, path string,
		) ([]platforms.CollectionItem, error) {
			return CollectArtist(ctx, NewClientWithLauncherID(cfg, id, groups), path)
		},
	}
}

// NewKodiTVShowLauncher creates a KodiTVShow launcher for TV show collection playback
func NewKodiTVShowLauncher() platforms.Launcher {
	id := shared.LauncherKodiTVShow
	groups := []string{shared.LauncherGroupKodi, shared.LauncherGroupKodiTV}
	return platforms.Launcher{
		Controls:            kodiControls(id, groups),
		WaitForReady:        kodiReady(id, groups),
		ID:                  id,
		Groups:              groups,
		SystemID:            systemdefs.SystemTVShow,
		Schemes:             []string{shared.SchemeKodiShow},
		SkipFilesystemScan:  true,                   // Uses Kodi API via Scanner, no filesystem scanning needed
		UsesRunningInstance: platforms.InstanceKodi, // Sends commands to running Kodi via JSON-RPC
		Launch: func(cfg *config.Instance, path string, opts *platforms.LaunchOptions) (*os.Process, error) {
			client := NewClientWithLauncherID(cfg, id, groups)
			return nil, launchTracked(client, opts, expectation{queue: true}, func(c KodiClient) error {
				return c.LaunchTVShow(path)
			})
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
		Collection: func(
			ctx context.Context, cfg *config.Instance, path string,
		) ([]platforms.CollectionItem, error) {
			return CollectTVShow(ctx, NewClientWithLauncherID(cfg, id, groups), path)
		},
	}
}

// NewKodiSongLauncher creates a KodiSong launcher for individual song playback
func NewKodiSongLauncher() platforms.Launcher {
	id := shared.LauncherKodiSong
	groups := []string{shared.LauncherGroupKodi, shared.LauncherGroupKodiMusic}
	return platforms.Launcher{
		Controls:            kodiControls(id, groups),
		WaitForReady:        kodiReady(id, groups),
		ID:                  id,
		Groups:              groups,
		SystemID:            systemdefs.SystemMusicTrack,
		Schemes:             []string{shared.SchemeKodiSong},
		SkipFilesystemScan:  true,                   // Uses Kodi API via Scanner, no filesystem scanning needed
		UsesRunningInstance: platforms.InstanceKodi, // Sends commands to running Kodi via JSON-RPC
		Launch: func(cfg *config.Instance, path string, opts *platforms.LaunchOptions) (*os.Process, error) {
			client := NewClientWithLauncherID(cfg, id, groups)
			exp := libraryExpectation(path, shared.SchemeKodiSong, "song")
			return nil, launchTracked(client, opts, exp, func(c KodiClient) error {
				return c.LaunchSong(path)
			})
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
