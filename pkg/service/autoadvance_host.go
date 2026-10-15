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

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/autoadvance"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	uievents "github.com/ZaparooProject/zaparoo-core/v2/pkg/ui/events"
	"github.com/jonboulle/clockwork"
)

// autoAdvanceHost gives the auto-advance engine the service's playlist and
// media state.
type autoAdvanceHost struct {
	svc *ServiceContext
}

func (h autoAdvanceHost) ActivePlaylist() *playlists.Playlist {
	return h.svc.State.GetActivePlaylist()
}

func (h autoAdvanceHost) BackgroundPlaylist() *playlists.Playlist {
	return h.svc.State.GetBackgroundPlaylist()
}

func (h autoAdvanceHost) ActiveMediaGeneration() (uint64, bool) {
	return h.svc.State.ActiveMediaReadyGeneration()
}

func (h autoAdvanceHost) PausePlaylistIfCurrent(slot string, pls *playlists.Playlist) bool {
	return h.svc.State.PausePlaylistIfCurrent(slot, pls)
}

func (h autoAdvanceHost) StopSeq() uint64 {
	return h.svc.State.StopEpoch()
}

func (h autoAdvanceHost) StopInProgress() bool {
	return h.svc.State.StopInProgress()
}

func (h autoAdvanceHost) Enqueue(ctx context.Context, pls *playlists.Playlist) bool {
	select {
	case h.svc.PlaylistQueue <- pls:
		return true
	case <-ctx.Done():
		return false
	}
}

// rendersPickers reports whether the host can announce a switch, which the
// engine does while a game is running: it must draw pickers itself, and do so
// over running media. A host that only shows notices leaves its pickers to
// API clients, and one whose display replaces the running program (MiSTer)
// would end the game it is announcing a switch away from, so a switch on
// either is made silently. The countdown after an exit is unaffected: no media
// is running then.
func rendersPickers(pl platforms.Platform, opts *EmbeddedOptions) bool {
	renderer := uiRendererFor(pl, opts)
	return uievents.RendersKind(renderer, models.UIEventKindPicker) && uievents.RendersOverMedia(renderer)
}

// startAutoAdvance creates the engine, registers it for media changes and
// starts it. hasRenderer says whether the platform draws UI events itself.
func startAutoAdvance(svc *ServiceContext, hasRenderer bool) *autoadvance.Engine {
	cfg := &autoadvance.Config{
		Clock:       clockwork.NewRealClock(),
		Host:        autoAdvanceHost{svc: svc},
		Input:       platforms.ResolveInputActivity(svc.Platform),
		HasRenderer: hasRenderer,
	}
	if svc.UI != nil {
		cfg.UI = svc.UI
	}
	if svc.LauncherCache != nil {
		cfg.PublishesLater = func(launcherID string) bool {
			launcher := svc.LauncherCache.GetLauncherByID(launcherID)
			return launcher != nil && launcherPublishesLater(launcher)
		}
	}
	engine := autoadvance.New(cfg)
	svc.State.AddMediaObserver(engine)
	engine.Start(svc.State.GetContext())
	return engine
}
