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
	"github.com/rs/zerolog/log"
)

var mediaReadyTimeout = 10 * time.Second

func startMediaReadyProbe(svc *ServiceContext, media *models.ActiveMedia, gen uint64) {
	if media == nil {
		return
	}

	readyFunc := mediaReadyFunc(svc, media)
	if readyFunc == nil {
		svc.State.MarkActiveMediaReady(gen)
		return
	}

	go func() {
		ctx, cancel := context.WithTimeout(svc.State.GetContext(), mediaReadyTimeout)
		defer cancel()

		if err := readyFunc(ctx, svc.Config, media); err != nil {
			if ctx.Err() != nil {
				log.Warn().Err(ctx.Err()).Str("media", media.Name).Msg("media ready wait timed out; continuing")
			} else {
				log.Warn().Err(err).Str("media", media.Name).Msg("media ready wait failed; continuing")
			}
		}
		svc.State.MarkActiveMediaReady(gen)
	}()
}

func mediaReadyFunc(
	svc *ServiceContext,
	media *models.ActiveMedia,
) func(context.Context, *config.Instance, *models.ActiveMedia) error {
	if media.LauncherID != "" {
		launchers := svc.Platform.Launchers(svc.Config)
		for i := range launchers {
			launcher := &launchers[i]
			if launcher.ID == media.LauncherID && launcher.WaitForReady != nil {
				return launcher.WaitForReady
			}
		}
	}

	readyPlatform, ok := svc.Platform.(platforms.MediaReadyPlatform)
	if !ok {
		return nil
	}
	return readyPlatform.WaitForMediaReady
}

func waitForMediaReady(ctx context.Context, svc *ServiceContext, expectedGen uint64) error {
	waitCtx, cancel := context.WithTimeout(ctx, mediaReadyTimeout)
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
