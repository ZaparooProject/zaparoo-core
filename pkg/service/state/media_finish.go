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

package state

import (
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
)

// launchFinish carries one launch's MediaFinished signal to the primary media
// that launch publishes. The launcher may report the end before or after
// Core has published the media, so an early report is held and applied at
// publication; a launch that never publishes drops it.
type launchFinish struct {
	state     *State
	mu        syncutil.Mutex
	gen       uint64
	published bool
	pending   bool
	called    bool
	// exited records which kind of end the launcher reported first.
	exited bool
}

func newLaunchFinish(s *State) *launchFinish {
	return &launchFinish{state: s}
}

// bind ties the launch to media it has just published. It does nothing when
// the media is not the active media, because the publication was rejected or
// something else already replaced it.
func (f *launchFinish) bind(media *models.ActiveMedia) {
	if media == nil {
		return
	}
	gen, ok := f.state.activeMediaGenerationOf(media)
	if !ok {
		return
	}

	f.mu.Lock()
	if f.called && f.published {
		f.mu.Unlock()
		return
	}
	f.gen = gen
	f.published = true
	apply := f.pending
	exited := f.exited
	f.pending = false
	f.mu.Unlock()

	if apply {
		f.end(gen, exited)
	}
}

func (f *launchFinish) end(gen uint64, exited bool) {
	if exited {
		f.state.ExitActiveMedia(gen)
		return
	}
	f.state.FinishActiveMedia(gen)
}

// finish is the launcher-facing MediaFinished callback. Only the first call
// of finish or exit has any effect.
func (f *launchFinish) finish() { f.report(false) }

// exit is the launcher-facing MediaExited callback, sharing finish's once.
func (f *launchFinish) exit() { f.report(true) }

func (f *launchFinish) report(exited bool) {
	f.mu.Lock()
	if f.called {
		f.mu.Unlock()
		return
	}
	f.called = true
	if !f.published {
		f.pending = true
		f.exited = exited
		f.mu.Unlock()
		return
	}
	gen := f.gen
	f.mu.Unlock()

	f.end(gen, exited)
}

// activeMediaGenerationOf returns the generation media was published under,
// or false when media is not the active media.
func (s *State) activeMediaGenerationOf(media *models.ActiveMedia) (uint64, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.activeMedia == nil || !s.activeMedia.Equal(media) {
		return 0, false
	}
	return s.activeMediaReadyGen, true
}

// FinishActiveMediaIf reports the natural end of the primary media when owns
// holds for it, and does nothing otherwise. The check and the clear are one
// step, so media another launcher published meanwhile is left alone.
func (s *State) FinishActiveMediaIf(owns func(*models.ActiveMedia) bool) bool {
	s.mu.Lock()
	if s.activeMedia == nil || !owns(s.activeMedia) {
		s.mu.Unlock()
		return false
	}
	gen := s.activeMediaReadyGen
	s.stopIntent = mediaStopIntent{
		reason: models.MediaStopReasonFinished,
		gen:    gen,
		at:     time.Now(),
	}
	s.mu.Unlock()

	return s.updateActiveMediaState(nil, func(media *models.ActiveMedia) bool {
		return s.activeMediaReadyGen == gen && media != nil && owns(media)
	})
}
