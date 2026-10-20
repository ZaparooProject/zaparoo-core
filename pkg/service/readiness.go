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
	"errors"
	"fmt"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/rs/zerolog/log"
)

var mediaReadyTimeout = 10 * time.Second

func startMediaReadyProbe(svc *ServiceContext, media *models.ActiveMedia, gen uint64) {
	if media == nil {
		return
	}

	launcher := readyLauncher(svc, media)
	readyFunc := launcherReadyFunc(svc, launcher)
	if readyFunc == nil {
		svc.State.MarkActiveMediaReady(gen)
		return
	}

	go func() {
		// Sizing the wait can read files, so it stays off the launch path.
		ctx, cancel := context.WithTimeout(svc.State.GetContext(), launcherReadyTimeout(launcher, media))
		defer cancel()

		// Replaced or stopped media ends the probe: it must not keep asking
		// an emulator about a game that is gone.
		go func() {
			if err := svc.State.WaitForActiveMediaReady(ctx, gen); err != nil &&
				(errors.Is(err, state.ErrActiveMediaChanged) || errors.Is(err, state.ErrNoActiveMedia)) {
				cancel()
			}
		}()

		started := time.Now()
		err := readyFunc(ctx, svc.Config, media)
		switch {
		case err == nil:
			log.Info().Str("media", media.Name).Dur("elapsed", time.Since(started)).Msg("media ready")
		case errors.Is(err, platforms.ErrReadinessUnavailable):
			log.Warn().Err(err).Str("media", media.Name).
				Msg("media ready integration unavailable; continuing")
		case errors.Is(err, context.DeadlineExceeded):
			log.Warn().Err(err).Str("media", media.Name).Msg("media ready wait timed out; continuing")
		case errors.Is(err, context.Canceled):
			log.Debug().Str("media", media.Name).Msg("media ready wait cancelled")
		default:
			log.Warn().Err(err).Str("media", media.Name).Msg("media ready wait failed; continuing")
		}
		svc.State.MarkActiveMediaReady(gen)
	}()
}

// readyLauncher returns the launcher that started media, or nil.
func readyLauncher(svc *ServiceContext, media *models.ActiveMedia) *platforms.Launcher {
	if media == nil || media.LauncherID == "" {
		return nil
	}
	launchers := svc.Platform.Launchers(svc.Config)
	for i := range launchers {
		if launchers[i].ID == media.LauncherID {
			return &launchers[i]
		}
	}
	return nil
}

// mediaReadyTimeoutFor is how long to wait for media to become ready: the
// service default, or longer when the launcher says this media needs it, as
// with an arcade set whose ROMs take many seconds to load.
func mediaReadyTimeoutFor(svc *ServiceContext, media *models.ActiveMedia) time.Duration {
	return launcherReadyTimeout(readyLauncher(svc, media), media)
}

func launcherReadyTimeout(launcher *platforms.Launcher, media *models.ActiveMedia) time.Duration {
	if launcher == nil || launcher.ReadyTimeout == nil {
		return mediaReadyTimeout
	}
	return max(mediaReadyTimeout, launcher.ReadyTimeout(media))
}

func mediaReadyFunc(
	svc *ServiceContext,
	media *models.ActiveMedia,
) func(context.Context, *config.Instance, *models.ActiveMedia) error {
	return launcherReadyFunc(svc, readyLauncher(svc, media))
}

func launcherReadyFunc(
	svc *ServiceContext,
	launcher *platforms.Launcher,
) func(context.Context, *config.Instance, *models.ActiveMedia) error {
	if launcher != nil && launcher.WaitForReady != nil {
		return launcher.WaitForReady
	}

	readyPlatform, ok := svc.Platform.(platforms.MediaReadyPlatform)
	if !ok {
		return nil
	}
	return readyPlatform.WaitForMediaReady
}

// mediaAppearPoll is how often a wait for not-yet-published media looks for it.
const mediaAppearPoll = 100 * time.Millisecond

// waitForMediaReady waits for the media the script is running to be ready. With
// awaitMedia the script has just launched something and no media is published
// yet, as with a launcher whose game is announced by a tracker once it has
// started: the wait first gives that media up to one readiness timeout to
// appear, then waits for it as usual.
func waitForMediaReady(ctx context.Context, svc *ServiceContext, expectedGen uint64, awaitMedia bool) error {
	if awaitMedia {
		gen, err := waitForMediaToStart(ctx, svc)
		if err != nil {
			return err
		}
		expectedGen = gen
	}

	waitCtx, cancel := context.WithTimeout(ctx, mediaReadyTimeoutFor(svc, svc.State.ActiveMedia()))
	defer cancel()

	if err := svc.State.WaitForActiveMediaReady(waitCtx, expectedGen); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			log.Warn().Msg("media ready wait timed out; continuing")
			return nil
		}
		return fmt.Errorf("wait for active media ready: %w", err)
	}
	return nil
}

// waitForMediaToStart returns the readiness generation of the media that
// appears, or state.ErrNoActiveMedia if none does within one readiness
// timeout.
func waitForMediaToStart(ctx context.Context, svc *ServiceContext) (uint64, error) {
	timeout := time.NewTimer(mediaReadyTimeout)
	defer timeout.Stop()
	ticker := time.NewTicker(mediaAppearPoll)
	defer ticker.Stop()

	for {
		if gen, ok := svc.State.ActiveMediaReadyGeneration(); ok {
			return gen, nil
		}
		select {
		case <-ctx.Done():
			return 0, fmt.Errorf("wait for media to start: %w", ctx.Err())
		case <-timeout.C:
			return 0, fmt.Errorf("wait for active media ready: %w", state.ErrNoActiveMedia)
		case <-ticker.C:
		}
	}
}
