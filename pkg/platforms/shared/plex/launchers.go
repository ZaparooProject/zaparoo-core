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
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared"
	"github.com/jonboulle/clockwork"
	"github.com/rs/zerolog/log"
)

const (
	// readyPollInterval is how often a readiness probe asks the player what
	// it is playing.
	readyPollInterval = 500 * time.Millisecond
	// launchTimeout covers resolving the item, finding the player, which can
	// include a Cast browse, and starting playback.
	launchTimeout = time.Minute
	// controlTimeout covers one transport command.
	controlTimeout = 15 * time.Second
)

// deps are what the launchers reach Plex and players through.
type deps struct {
	newClient clientForConfig
	finder    *playerFinder
	clock     clockwork.Clock
}

func newDeps() *deps {
	return &deps{
		newClient: NewClientWithLauncherID,
		finder: &playerFinder{
			http:       newHTTPClient(),
			browseCast: discoverCast,
			dialCast:   dialCastTLS,
		},
		clock: clockwork.NewRealClock(),
	}
}

// launchRecord is the last thing Core started on a Plex player, which
// readiness and controls act on.
type launchRecord struct {
	player    player
	kind      *mediaKind
	ratingKey string
}

var (
	lastLaunchMu syncutil.Mutex
	lastLaunch   *launchRecord
)

func setLastLaunch(rec *launchRecord) {
	lastLaunchMu.Lock()
	defer lastLaunchMu.Unlock()
	lastLaunch = rec
}

func currentLaunch() *launchRecord {
	lastLaunchMu.Lock()
	defer lastLaunchMu.Unlock()
	return lastLaunch
}

// launch resolves the item a path names, has the server queue it, and
// starts the queue on the configured player.
func (d *deps) launch(ctx context.Context, client PlexClient, kind *mediaKind, path string) error {
	md, err := resolveItem(ctx, client, kind, path)
	if err != nil {
		return err
	}
	identity, err := client.Identity(ctx)
	if err != nil {
		return fmt.Errorf("failed to launch plex %s: %w", kind.typeName, err)
	}
	server, err := newServerInfo(client, identity)
	if err != nil {
		return err
	}
	queue, err := client.CreatePlayQueue(ctx, identity.MachineIdentifier, md.RatingKey, kind.queueType)
	if err != nil {
		return fmt.Errorf("failed to launch plex %s: %w", kind.typeName, err)
	}
	first := queue.SelectedItem()
	if first == nil || !validKey(first.RatingKey) {
		return errors.New("plex returned a play queue without a playable item")
	}
	p, err := d.finder.find(ctx, client)
	if err != nil {
		return err
	}
	if err := p.Play(ctx, &playRequest{Queue: queue, First: first, Server: server, Kind: kind}); err != nil {
		return fmt.Errorf("failed to start plex playback on %q: %w", p.Name(), err)
	}
	setLastLaunch(&launchRecord{player: p, ratingKey: first.RatingKey, kind: kind})
	log.Info().Str("player", p.Name()).Str("type", p.Kind()).Str("kind", kind.typeName).
		Msg("started plex playback")
	return nil
}

// waitForReady returns a probe that polls the player Core last started
// media on until it plays the launched item.
func (d *deps) waitForReady() func(context.Context, *config.Instance, *models.ActiveMedia) error {
	return func(ctx context.Context, _ *config.Instance, _ *models.ActiveMedia) error {
		rec := currentLaunch()
		if rec == nil {
			return fmt.Errorf("%w: no plex playback was started", platforms.ErrReadinessUnavailable)
		}
		return platforms.PollReady(ctx, d.clock, readyPollInterval, func(ctx context.Context) (bool, error) {
			state, err := rec.player.State(ctx)
			if err != nil {
				return false, fmt.Errorf("%w: %w", platforms.ErrReadinessUnavailable, err)
			}
			if !state.Playing {
				return false, nil
			}
			return state.RatingKey == "" || state.RatingKey == rec.ratingKey, nil
		})
	}
}

// controlPlayer is the player a control acts on: the one Core last started
// media on, or the configured one.
func (d *deps) controlPlayer(
	ctx context.Context, cfg *config.Instance, id string, groups []string, kind *mediaKind,
) (player, *mediaKind, error) {
	if rec := currentLaunch(); rec != nil {
		return rec.player, rec.kind, nil
	}
	p, err := d.finder.find(ctx, d.newClient(cfg, id, groups))
	if err != nil {
		return nil, kind, err
	}
	return p, kind, nil
}

func (d *deps) controls(
	id string, groups []string, kind *mediaKind, exit *shared.LaunchExit,
) map[string]platforms.Control {
	command := func(cmd string) platforms.ControlFunc {
		return func(ctx context.Context, cfg *config.Instance, _ platforms.ControlParams) error {
			ctx, cancel := context.WithTimeout(ctx, controlTimeout)
			defer cancel()
			p, k, err := d.controlPlayer(ctx, cfg, id, groups, kind)
			if err != nil {
				return err
			}
			return p.Command(ctx, cmd, k)
		}
	}
	toggle := func(ctx context.Context, cfg *config.Instance, _ platforms.ControlParams) error {
		ctx, cancel := context.WithTimeout(ctx, controlTimeout)
		defer cancel()
		p, k, err := d.controlPlayer(ctx, cfg, id, groups, kind)
		if err != nil {
			return err
		}
		state, err := p.State(ctx)
		if err != nil {
			return fmt.Errorf("failed to read plex player state: %w", err)
		}
		if state.Playing {
			return p.Command(ctx, cmdPause, k)
		}
		return p.Command(ctx, cmdPlay, k)
	}
	stop := func(ctx context.Context, cfg *config.Instance, params platforms.ControlParams) error {
		report := exit.Capture()
		if err := command(cmdStop)(ctx, cfg, params); err != nil {
			return err
		}
		report()
		return nil
	}
	return map[string]platforms.Control{
		platforms.ControlTogglePause: {Func: toggle},
		platforms.ControlPause:       {Func: command(cmdPause)},
		platforms.ControlResume:      {Func: command(cmdPlay)},
		platforms.ControlStop:        {Func: stop},
		platforms.ControlFastForward: {Func: command(cmdStepForward)},
		platforms.ControlRewind:      {Func: command(cmdStepBack)},
		platforms.ControlNext:        {Func: command(cmdNext)},
		platforms.ControlPrevious:    {Func: command(cmdPrevious)},
	}
}

func (d *deps) newLauncher(id, systemID string, kind *mediaKind) platforms.Launcher {
	groups := []string{shared.LauncherGroupPlex}
	exit := &shared.LaunchExit{}
	launcher := platforms.Launcher{
		Controls:            d.controls(id, groups, kind, exit),
		WaitForReady:        d.waitForReady(),
		ID:                  id,
		Groups:              groups,
		SystemID:            systemID,
		Schemes:             []string{kind.scheme},
		SkipFilesystemScan:  true,
		UsesRunningInstance: platforms.InstancePlex,
		Test:                shared.SchemeIDTest(kind.scheme),
		Launch: func(cfg *config.Instance, path string, opts *platforms.LaunchOptions) (*os.Process, error) {
			ctx, cancel := context.WithTimeout(context.Background(), launchTimeout)
			defer cancel()
			if err := d.launch(ctx, d.newClient(cfg, id, groups), kind, path); err != nil {
				return nil, err
			}
			exit.Set(opts)
			return nil, nil //nolint:nilnil // playback runs on a remote player, not a local process
		},
		Scanner: func(
			ctx context.Context, cfg *config.Instance, _ string, results []platforms.ScanResult,
		) ([]platforms.ScanResult, error) {
			return scanKind(ctx, d.newClient(cfg, id, groups), kind, results)
		},
	}
	if kind.collection {
		launcher.Collection = func(ctx context.Context, cfg *config.Instance, path string) (
			[]platforms.CollectionItem, error,
		) {
			return collect(ctx, d.newClient(cfg, id, groups), kind, path)
		}
	}
	return launcher
}

// NewPlexMovieLauncher plays movies from a Plex library.
func NewPlexMovieLauncher() platforms.Launcher {
	return newDeps().newLauncher(shared.LauncherPlexMovie, systemdefs.SystemMovie, kindMovie)
}

// NewPlexTVLauncher plays TV episodes from a Plex library.
func NewPlexTVLauncher() platforms.Launcher {
	return newDeps().newLauncher(shared.LauncherPlexTVEpisode, systemdefs.SystemTVEpisode, kindEpisode)
}

// NewPlexTVShowLauncher plays every episode of a Plex TV show in order.
func NewPlexTVShowLauncher() platforms.Launcher {
	return newDeps().newLauncher(shared.LauncherPlexTVShow, systemdefs.SystemTVShow, kindShow)
}

// NewPlexSongLauncher plays songs from a Plex music library.
func NewPlexSongLauncher() platforms.Launcher {
	return newDeps().newLauncher(shared.LauncherPlexSong, systemdefs.SystemMusicTrack, kindTrack)
}

// NewPlexAlbumLauncher plays a Plex album in track order.
func NewPlexAlbumLauncher() platforms.Launcher {
	return newDeps().newLauncher(shared.LauncherPlexAlbum, systemdefs.SystemMusicAlbum, kindAlbum)
}

// NewPlexArtistLauncher plays every track by a Plex artist.
func NewPlexArtistLauncher() platforms.Launcher {
	return newDeps().newLauncher(shared.LauncherPlexArtist, systemdefs.SystemMusicArtist, kindArtist)
}

// Launchers returns every Plex launcher.
func Launchers() []platforms.Launcher {
	return []platforms.Launcher{
		NewPlexMovieLauncher(),
		NewPlexTVLauncher(),
		NewPlexTVShowLauncher(),
		NewPlexSongLauncher(),
		NewPlexAlbumLauncher(),
		NewPlexArtistLauncher(),
	}
}
