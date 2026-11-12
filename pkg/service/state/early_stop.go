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
)

// defaultEarlyStopWindow is how long after a launch began a game can still
// turn up that a stop was meant for. Emulators started through a frontend
// can take well over ten seconds to appear after the launch returns.
const defaultEarlyStopWindow = 30 * time.Second

// earlyStop is a stop that landed before the game it was meant for was
// running. Platforms whose stop needs a running game to act on cannot end a
// game that has not appeared yet, so the game shows up afterwards and would
// play on untracked.
type earlyStop struct {
	until time.Time
	// path is the media the stop cleared, or empty when the launch had not
	// published anything yet.
	path string
}

// noteEarlyStopLocked remembers a stop of media that was not running yet. The
// caller holds s.mu.
func (s *State) noteEarlyStopLocked(now time.Time) {
	var since time.Time
	var path string
	switch {
	case s.activeMedia != nil && !s.activeMediaReady:
		since = s.activeMediaPublishedAt
		path = s.activeMedia.Path
	case s.activeMedia == nil && !s.lastLaunchAt.IsZero() && !s.publishedSinceLaunch:
		since = s.lastLaunchAt
	default:
		return
	}
	until := since.Add(s.earlyStopWindow)
	if !now.Before(until) {
		return
	}
	s.earlyStop = earlyStop{path: path, until: until}
}

// takeEarlyStopFor reports whether media is the game an early stop was meant
// for, and consumes the note when it is. Any other publication ends the note:
// something else is running now.
func (s *State) takeEarlyStopFor(media *models.ActiveMedia) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	pending := s.earlyStop
	if pending.until.IsZero() {
		return false
	}
	s.earlyStop = earlyStop{}
	if !time.Now().Before(pending.until) {
		return false
	}
	return pending.path == "" || pending.path == media.Path
}
