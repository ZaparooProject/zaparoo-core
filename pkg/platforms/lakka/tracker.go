//go:build linux && !android

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

package lakka

import (
	"context"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/rs/zerolog/log"
)

const trackInterval = 2 * time.Second

// tracker clears active media once RetroArch stops playing what Core
// launched: the player closed it from the quick menu, quit the game instance,
// or loaded something else from the RetroArch menu.
type tracker struct {
	frontend       *frontend
	activeMedia    func() *models.ActiveMedia
	setActiveMedia func(*models.ActiveMedia)
	mediaKey       string
	confirmed      bool
}

func (t *tracker) run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.frontend.clock.After(trackInterval):
		}
		t.poll(ctx)
	}
}

// poll checks one GET_STATUS. Media is only cleared after RetroArch was seen
// playing it, so a launch still loading, or an instance being swapped, is
// never mistaken for a game that ended. A launch that never starts is left to
// the launcher's GameRunning check.
func (t *tracker) poll(ctx context.Context) {
	media := t.activeMedia()
	launched := t.frontend.currentLaunch()
	if media == nil || launched == "" || media.Path != launched {
		t.mediaKey = ""
		t.confirmed = false
		return
	}

	key := media.Path + "\x00" + media.Started.String()
	if key != t.mediaKey {
		t.mediaKey = key
		t.confirmed = false
	}

	status, err := t.frontend.status(ctx)
	if err != nil {
		return
	}
	if isLoaded(status) && statusNamesContent(status, launched) {
		t.confirmed = true
		return
	}
	if !t.confirmed {
		return
	}

	t.mediaKey = ""
	t.confirmed = false
	// The status query takes time; a launch that landed meanwhile owns the
	// media now.
	if current := t.activeMedia(); current == nil ||
		current.Path+"\x00"+current.Started.String() != key || t.frontend.currentLaunch() != launched {
		return
	}
	log.Info().Str("status", status).Str("media", media.Path).Msg("retroarch is no longer playing the launched media")
	t.frontend.clearLaunch(launched)
	t.setActiveMedia(nil)
}
