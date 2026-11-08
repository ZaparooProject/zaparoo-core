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

package service

import (
	"context"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	gozapscript "github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript"
	"github.com/jonboulle/clockwork"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
)

const (
	// musicAutoplaySettle is how long autoplay waits once Core is ready, so
	// the device has finished starting up before music begins.
	musicAutoplaySettle = 3 * time.Second
	// musicAutoplayRetryDelay is how long autoplay waits for a track that is
	// not there yet, such as one on a USB drive that mounts late, before it
	// looks once more.
	musicAutoplayRetryDelay = 10 * time.Second
)

// musicAutoplay starts the background playlist restored from the last run
// on the first Core start of a boot, when [audio] music_autoplay is on.
type musicAutoplay struct {
	svc *ServiceContext
	// waitReady blocks until Core is ready to launch: the launcher cache is
	// filled and the platform reports the service ready.
	waitReady  func(ctx context.Context)
	clock      clockwork.Clock
	fs         afero.Fs
	settle     time.Duration
	retryDelay time.Duration
	// pending is set when a game was running at startup with music paused
	// for games, so the music starts once the game exits instead.
	pending atomic.Bool
}

func newMusicAutoplay(svc *ServiceContext, waitReady func(ctx context.Context)) *musicAutoplay {
	return &musicAutoplay{
		svc:        svc,
		waitReady:  waitReady,
		clock:      clockwork.NewRealClock(),
		fs:         afero.NewOsFs(),
		settle:     musicAutoplaySettle,
		retryDelay: musicAutoplayRetryDelay,
	}
}

// run waits for Core to be ready and settled, then starts the music.
func (a *musicAutoplay) run(ctx context.Context) {
	if a.waitReady != nil {
		a.waitReady(ctx)
	}
	if !sleepOnClock(ctx, a.clock, a.settle) {
		return
	}
	a.start(ctx)
}

// start plays the restored background playlist, or leaves it for later when
// a game is running and music pauses for games.
func (a *musicAutoplay) start(ctx context.Context) {
	pls, ok := a.playable()
	if !ok {
		return
	}
	if path, missing, _ := a.missingTrack(pls); missing {
		log.Info().Str("path", path).Dur("retryIn", a.retryDelay).
			Msg("music autoplay: track not found yet, retrying")
		if !sleepOnClock(ctx, a.clock, a.retryDelay) {
			return
		}
		if pls, ok = a.playable(); !ok {
			return
		}
		var absolute bool
		if path, missing, absolute = a.missingTrack(pls); missing {
			if absolute {
				log.Warn().Str("path", path).Msg("music autoplay: track not found, not starting music")
				return
			}
			log.Debug().Str("path", path).Msg("music autoplay: track not found as a file, leaving it to the launch")
		}
	}

	log.Info().Str("playlist", pls.ID).Msg("music autoplay: starting background playlist")
	select {
	case a.svc.PlaylistQueue <- playlists.Play(*pls):
	case <-ctx.Done():
	}
}

// playable returns the background playlist to start, or false when there is
// none, it is already playing, or it has to wait for a running game to exit.
func (a *musicAutoplay) playable() (*playlists.Playlist, bool) {
	pls := a.svc.State.GetBackgroundPlaylist()
	if pls == nil {
		log.Debug().Msg("music autoplay: no background playlist to start")
		return nil, false
	}
	if pls.Playing {
		return nil, false
	}
	if a.svc.State.ActiveMedia() != nil && a.svc.Config.AudioPauseOnLaunch() {
		log.Info().Msg("music autoplay: a game is running, music starts when it exits")
		a.pending.Store(true)
		return nil, false
	}
	return pls, true
}

// missingTrack reports the current track of pls when it is a launched file
// that is not there, and whether the path is absolute. An absolute path is a
// definite miss; a relative one may still resolve through the title or
// system formats, so only its first miss is worth a retry. Entries that are
// not a plain file launch are never missing.
func (a *musicAutoplay) missingTrack(pls *playlists.Playlist) (path string, missing, absolute bool) {
	path, ok := launchPath(pls.Current().ZapScript)
	if !ok || helpers.ReURI.MatchString(path) {
		return "", false, false
	}
	absolute = filepath.IsAbs(path)
	if !absolute && filepath.Ext(path) == "" {
		return "", false, false
	}
	_, err := zapscript.FindLaunchFile(a.fs, a.svc.Platform, a.svc.Config, &platforms.LauncherSnapshot{}, path)
	if err != nil {
		return path, true, absolute
	}
	return "", false, false
}

// launchPath returns the path argument of a script that is a launch, whether
// written with the launch command or as a bare path.
func launchPath(script string) (string, bool) {
	script = strings.TrimSpace(script)
	if script == "" {
		return "", false
	}
	parsed, err := gozapscript.NewParser(script).ParseScript()
	if err != nil || len(parsed.Cmds) != 1 {
		return "", false
	}
	cmd := parsed.Cmds[0]
	if cmd.Name != gozapscript.ZapScriptCmdLaunch || len(cmd.Args) == 0 || cmd.Args[0] == "" {
		return "", false
	}
	return cmd.Args[0], true
}

// mediaStopped starts music left pending by a game that was running at
// startup. It waits to settle first: a stop that is part of one game
// replacing another leaves the new game running by then, and the music stays
// pending for it.
func (a *musicAutoplay) mediaStopped() {
	if a == nil || !a.pending.CompareAndSwap(true, false) {
		return
	}
	if a.svc.BackgroundWG != nil {
		a.svc.BackgroundWG.Add(1)
	}
	go func() {
		if a.svc.BackgroundWG != nil {
			defer a.svc.BackgroundWG.Done()
		}
		ctx := a.svc.State.GetContext()
		if !sleepOnClock(ctx, a.clock, a.settle) {
			return
		}
		a.start(ctx)
	}()
}

// sleepOnClock waits d on clock, reporting false when ctx ends first.
func sleepOnClock(ctx context.Context, clock clockwork.Clock, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	timer := clock.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.Chan():
		return true
	}
}
