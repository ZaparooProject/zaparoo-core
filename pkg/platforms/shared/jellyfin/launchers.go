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
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared"
	"github.com/jonboulle/clockwork"
	"github.com/rs/zerolog/log"
)

const (
	// readyPollInterval is how often a readiness probe asks Jellyfin what
	// the player is playing.
	readyPollInterval = 500 * time.Millisecond
	// launchTimeout bounds a launch, which may list every track by an artist.
	launchTimeout = 2 * time.Minute
)

// clientFactory builds the client a launcher call uses from the current
// config, so a relink or player change applies without a restart.
type clientFactory func(*config.Instance) (Client, error)

func launcherClient(id string, groups []string) clientFactory {
	return func(cfg *config.Instance) (Client, error) {
		c, err := NewClientWithLauncherID(cfg, id, groups)
		if err != nil {
			return nil, err
		}
		return c, nil
	}
}

// launchPath plays the item a card names on the configured player. Shows,
// albums and artists play all their episodes or tracks in order.
func launchPath(ctx context.Context, c Client, kind *mediaKind, path string) error {
	ref, err := parseReference(path, kind)
	if err != nil {
		return err
	}
	item, err := resolveItem(ctx, c, kind, ref)
	if err != nil {
		return err
	}
	session, err := selectSession(ctx, c)
	if err != nil {
		return err
	}
	_, tracks, err := collectionTracks(ctx, c, kind, item)
	if err != nil {
		return err
	}
	ids := make([]string, 0, len(tracks))
	for i := range tracks {
		if validItemID(tracks[i].ID) {
			ids = append(ids, tracks[i].ID)
		}
	}
	if len(ids) == 0 {
		return fmt.Errorf("jellyfin %s %q has nothing to play", kind.label, kind.name(item))
	}
	return playItems(ctx, c, session.ID, ids)
}

// collect lists the episodes or tracks a show, album or artist card plays.
func collect(ctx context.Context, c Client, kind *mediaKind, path string) ([]platforms.CollectionItem, error) {
	ref, err := parseReference(path, kind)
	if err != nil {
		return nil, err
	}
	item, err := resolveItem(ctx, c, kind, ref)
	if err != nil {
		return nil, err
	}
	trackKind, tracks, err := collectionTracks(ctx, c, kind, item)
	if err != nil {
		return nil, err
	}
	items := make([]platforms.CollectionItem, 0, len(tracks))
	for i := range tracks {
		t := &tracks[i]
		if !validItemID(t.ID) {
			continue
		}
		name := trackKind.name(t)
		items = append(items, platforms.CollectionItem{Name: name, Path: itemPath(trackKind, t, name)})
	}
	return items, nil
}

// waitForPlaying returns a readiness probe that polls the player until it
// plays the launched item. The play command returns before the player acts
// on it, so a player that was already playing something else is ready once
// its item changes.
func waitForPlaying(
	newClient clientFactory, kind *mediaKind, clock clockwork.Clock,
) func(context.Context, *config.Instance, *models.ActiveMedia) error {
	return func(ctx context.Context, cfg *config.Instance, media *models.ActiveMedia) error {
		c, err := newClient(cfg)
		if err != nil {
			return fmt.Errorf("%w: %w", platforms.ErrReadinessUnavailable, err)
		}
		var launchedID string
		if media != nil {
			if ref, refErr := parseReference(media.Path, kind); refErr == nil {
				launchedID = ref.ID
			}
		}
		var before string
		first := true
		return platforms.PollReady(ctx, clock, readyPollInterval, func(ctx context.Context) (bool, error) {
			session, err := selectSession(ctx, c)
			if err != nil {
				if ctxErr := ctx.Err(); ctxErr != nil {
					return false, ctxErr
				}
				return false, fmt.Errorf("%w: %w", platforms.ErrReadinessUnavailable, err)
			}
			playing := session.NowPlayingItem
			if playing == nil || playing.ID == "" {
				first = false
				return false, nil
			}
			if media == nil || (launchedID != "" && strings.EqualFold(playing.ID, launchedID)) {
				return true, nil
			}
			if first {
				first = false
				before = playing.ID
				return false, nil
			}
			return before == "" || !strings.EqualFold(before, playing.ID), nil
		})
	}
}

// playstateControls maps Core controls to Jellyfin playstate commands. The
// player is looked up on every call, since it may have restarted.
func playstateControls(newClient clientFactory, exit *shared.LaunchExit) map[string]platforms.Control {
	commands := map[string]string{
		platforms.ControlTogglePause: "PlayPause",
		platforms.ControlPause:       "Pause",
		platforms.ControlResume:      "Unpause",
		platforms.ControlStop:        "Stop",
		platforms.ControlFastForward: "FastForward",
		platforms.ControlRewind:      "Rewind",
		platforms.ControlNext:        "NextTrack",
		platforms.ControlPrevious:    "PreviousTrack",
	}
	controls := make(map[string]platforms.Control, len(commands))
	for control, command := range commands {
		controls[control] = platforms.Control{
			Func: func(ctx context.Context, cfg *config.Instance, _ platforms.ControlParams) error {
				c, err := newClient(cfg)
				if err != nil {
					return err
				}
				if err := sendPlaystate(ctx, c, command); err != nil {
					return err
				}
				if control == platforms.ControlStop {
					exit.Report()
				}
				return nil
			},
		}
	}
	return controls
}

func sendPlaystate(ctx context.Context, c Client, command string) error {
	session, err := selectSession(ctx, c)
	if err != nil {
		return err
	}
	if err := c.SendPlaystate(ctx, session.ID, command); err != nil {
		return fmt.Errorf("failed to send %s to jellyfin player: %w", command, err)
	}
	return nil
}

// newLauncher builds the launcher for one kind of library item.
func newLauncher(kind *mediaKind, collection bool) platforms.Launcher {
	groups := []string{shared.LauncherGroupJellyfin}
	newClient := launcherClient(kind.launcherID, groups)
	exit := &shared.LaunchExit{}
	l := platforms.Launcher{
		Controls:            playstateControls(newClient, exit),
		WaitForReady:        waitForPlaying(newClient, kind, clockwork.NewRealClock()),
		ID:                  kind.launcherID,
		Groups:              groups,
		SystemID:            kind.systemID,
		Schemes:             []string{kind.scheme},
		SkipFilesystemScan:  true,
		UsesRunningInstance: platforms.InstanceJellyfin,
		Test:                shared.SchemeIDTest(kind.scheme),
		Launch: func(cfg *config.Instance, path string, opts *platforms.LaunchOptions) (*os.Process, error) {
			c, err := newClient(cfg)
			if err != nil {
				return nil, err
			}
			ctx, cancel := context.WithTimeout(context.Background(), launchTimeout)
			defer cancel()
			if err := launchPath(ctx, c, kind, path); err != nil {
				return nil, err
			}
			exit.Set(opts)
			return nil, nil //nolint:nilnil // playback runs on a remote player, not a local process
		},
		Scanner: func(
			ctx context.Context, cfg *config.Instance, _ string, results []platforms.ScanResult,
		) ([]platforms.ScanResult, error) {
			c, err := newClient(cfg)
			if errors.Is(err, ErrNotConfigured) || errors.Is(err, ErrNotLinked) {
				log.Debug().Err(err).Str("launcher", kind.launcherID).Msg("skipping jellyfin scan")
				return results, nil
			}
			if err != nil {
				return results, err
			}
			return scan(ctx, c, kind, results)
		},
	}
	if collection {
		l.Collection = func(
			ctx context.Context, cfg *config.Instance, path string,
		) ([]platforms.CollectionItem, error) {
			c, err := newClient(cfg)
			if err != nil {
				return nil, err
			}
			return collect(ctx, c, kind, path)
		}
	}
	return l
}

// NewJellyfinMovieLauncher plays a library movie.
func NewJellyfinMovieLauncher() platforms.Launcher { return newLauncher(kindMovie, false) }

// NewJellyfinTVEpisodeLauncher plays a library TV episode.
func NewJellyfinTVEpisodeLauncher() platforms.Launcher { return newLauncher(kindEpisode, false) }

// NewJellyfinTVShowLauncher plays every episode of a TV show in order.
func NewJellyfinTVShowLauncher() platforms.Launcher { return newLauncher(kindShow, true) }

// NewJellyfinSongLauncher plays a library song.
func NewJellyfinSongLauncher() platforms.Launcher { return newLauncher(kindSong, false) }

// NewJellyfinAlbumLauncher plays every track of an album in order.
func NewJellyfinAlbumLauncher() platforms.Launcher { return newLauncher(kindAlbum, true) }

// NewJellyfinArtistLauncher plays every track by an artist, album by album.
func NewJellyfinArtistLauncher() platforms.Launcher { return newLauncher(kindArtist, true) }

// Launchers returns all six Jellyfin launchers.
func Launchers() []platforms.Launcher {
	return []platforms.Launcher{
		NewJellyfinMovieLauncher(),
		NewJellyfinTVEpisodeLauncher(),
		NewJellyfinTVShowLauncher(),
		NewJellyfinSongLauncher(),
		NewJellyfinAlbumLauncher(),
		NewJellyfinArtistLauncher(),
	}
}
