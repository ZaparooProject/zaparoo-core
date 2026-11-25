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

package api

import (
	"encoding/json"
	"math"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/audio"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/olahol/melody"
	"github.com/rs/zerolog/log"
)

const levelsHubSessionKey = "api.ws.levels"

// levelsHub sends media.levels to the WebSocket connections that asked for
// it. Levels are measured only while at least one connection is
// subscribed. They go straight to those connections rather than through
// the notification broker, so no other transport carries them.
type levelsHub struct {
	source audio.LevelSource
	subs   map[*melody.Session]struct{}
	// toggleMu orders turning measurement on and off; mu guards subs. They
	// are separate because turning measurement off waits for a send in
	// progress, which takes mu.
	toggleMu  syncutil.Mutex
	mu        syncutil.Mutex
	listening bool
}

// newLevelsHub returns a hub for playback, or nil when playback cannot
// report levels.
func newLevelsHub(playback audio.PlaybackManager) *levelsHub {
	source, ok := playback.(audio.LevelSource)
	if !ok || source == nil {
		return nil
	}
	return &levelsHub{source: source, subs: make(map[*melody.Session]struct{})}
}

// attach makes the hub reachable from requests on s.
func (h *levelsHub) attach(s *melody.Session) {
	if h != nil {
		s.Set(levelsHubSessionKey, h)
	}
}

func getLevelsHub(s *melody.Session) *levelsHub {
	v, ok := s.Get(levelsHubSessionKey)
	if !ok {
		return nil
	}
	h, ok := v.(*levelsHub)
	if !ok {
		return nil
	}
	return h
}

// subscribe turns levels on or off for s. A connection that has already
// closed is never added, so a request racing its disconnect cannot leave
// it subscribed.
func (h *levelsHub) subscribe(s *melody.Session, enabled bool) {
	h.update(func() {
		if enabled && !s.IsClosed() {
			h.subs[s] = struct{}{}
		} else {
			delete(h.subs, s)
		}
	})
}

// remove drops s when its connection closes.
func (h *levelsHub) remove(s *melody.Session) {
	if h == nil {
		return
	}
	h.update(func() { delete(h.subs, s) })
}

func (h *levelsHub) update(change func()) {
	h.toggleMu.Lock()
	defer h.toggleMu.Unlock()
	h.mu.Lock()
	change()
	want := len(h.subs) > 0
	h.mu.Unlock()
	if want == h.listening {
		return
	}
	h.listening = want
	if want {
		h.source.SetLevelListener(h.send)
	} else {
		h.source.SetLevelListener(nil)
	}
}

// roundLevel keeps three decimal places, which is finer than any meter
// draws, so each notification stays small.
func roundLevel(v float64) float64 {
	return math.Round(v*1000) / 1000
}

func (h *levelsHub) send(slot string, levels *audio.Levels) {
	payload := models.MediaLevelsNotification{
		Slot:  slot,
		Peak:  roundLevel(levels.Peak),
		Bands: make([]float64, len(levels.Bands)),
	}
	for i, v := range levels.Bands {
		payload.Bands[i] = roundLevel(v)
	}
	params, err := json.Marshal(payload)
	if err != nil {
		log.Error().Err(err).Msg("marshalling media levels")
		return
	}
	data, err := json.Marshal(models.NotificationObject{
		JSONRPC: "2.0",
		Method:  models.NotificationMediaLevels,
		Params:  params,
	})
	if err != nil {
		log.Error().Err(err).Msg("marshalling media levels notification")
		return
	}
	h.mu.Lock()
	sessions := make([]*melody.Session, 0, len(h.subs))
	for s := range h.subs {
		sessions = append(sessions, s)
	}
	h.mu.Unlock()
	for _, s := range sessions {
		if !s.IsClosed() {
			writeNotificationToSession(s, data)
		}
	}
}
