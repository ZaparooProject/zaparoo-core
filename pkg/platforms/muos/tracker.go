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

package muos

import (
	"context"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/assets"
	"github.com/rs/zerolog/log"
)

const trackerInterval = time.Second

func (p *Platform) startTracker(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	p.trackerMu.Lock()
	p.cancel = cancel
	p.trackerDone = done
	p.trackerMu.Unlock()

	ticker := p.getClock().NewTicker(trackerInterval)
	go func() {
		defer close(done)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.Chan():
				p.pollOverlay()
			}
		}
	}()
}

func (p *Platform) stopTracker() {
	p.trackerMu.Lock()
	cancel, done := p.cancel, p.trackerDone
	p.cancel, p.trackerDone = nil, nil
	p.trackerMu.Unlock()
	if cancel == nil {
		return
	}
	cancel()
	<-done
}

// pollOverlay follows the content the frontend reports as running, so
// games started from the muOS menus show up as active media too.
func (p *Platform) pollOverlay() {
	state, running := readOverlay(p.filesystem())
	current := ""
	if running {
		current = state.name + "\n" + state.system + "\n" + state.core
	}

	p.trackerMu.Lock()
	if p.launching || current == p.lastOverlay {
		p.trackerMu.Unlock()
		return
	}
	p.lastOverlay = current
	if !running {
		p.pending = ""
		p.trackerMu.Unlock()
		if p.setActiveMedia != nil && p.activeMedia != nil && p.activeMedia() != nil {
			log.Info().Msg("muOS content exited")
			p.setActiveMedia(nil)
		}
		return
	}
	ours := p.pending != "" && p.pending == state.name
	p.pending = ""
	p.trackerMu.Unlock()

	if ours || p.setActiveMedia == nil {
		return
	}
	if media := p.mediaFromOverlay(state); media != nil {
		if p.activeMedia != nil {
			if active := p.activeMedia(); active != nil && active.Path == media.Path {
				return
			}
		}
		log.Info().Str("path", media.Path).Str("system", media.SystemID).Msg("muOS content started")
		p.setActiveMedia(media)
	}
}

func (p *Platform) mediaFromOverlay(state overlayState) *models.ActiveMedia {
	path, ok := lastPlayedPath(p.filesystem(), state.name)
	if !ok {
		log.Debug().Str("name", state.name).Msg("running muOS content not found in last played record")
		return nil
	}
	systemID, ok := systemKeyMap[state.system]
	if !ok {
		log.Debug().Str("system", state.system).Msg("running muOS content has an unmapped system")
		return nil
	}
	systemName := systemID
	if meta, err := assets.GetSystemMetadata(systemID); err == nil && meta.Name != "" {
		systemName = meta.Name
	}
	return models.NewActiveMedia(systemID, systemName, path, state.name, systemID)
}
