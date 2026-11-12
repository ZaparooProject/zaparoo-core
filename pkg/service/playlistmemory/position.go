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

package playlistmemory

import (
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/audio"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mediaslot"
	"github.com/jonboulle/clockwork"
)

const (
	// MinResumeLength is the shortest track whose position is kept. Shorter
	// tracks are songs, which always play from the start.
	MinResumeLength = 10 * time.Minute
	// resumeMargin is how close to either end of a track a position is
	// treated as not started or finished.
	resumeMargin = 30 * time.Second
	// pollInterval is how often the playing track is sampled. Pauses, stops
	// and track changes are saved at the next sample.
	pollInterval = 5 * time.Second
	// saveInterval is how often the position of a track that keeps playing
	// is written.
	saveInterval = 30 * time.Second
)

// Resumable reports whether a track of length stopped at position picks up
// there next time.
func Resumable(position, length time.Duration) bool {
	return length >= MinResumeLength && position >= resumeMargin && position <= length-resumeMargin
}

// PositionTracker keeps the position of long native audio tracks on the
// primary slot. A track belongs to the profile active when it started. A nil
// tracker does nothing.
type PositionTracker struct {
	memory   *Memory
	playback audio.PlaybackManager
	media    func() *models.ActiveMedia
	clock    clockwork.Clock
	stop     chan struct{}
	done     chan struct{}
	current  *trackedTrack
	mu       syncutil.Mutex
	stopped  bool
}

type trackedTrack struct {
	lastSaved time.Time
	profileID string
	systemID  string
	path      string
	position  time.Duration
	length    time.Duration
	paused    bool
}

// NewPositionTracker returns a tracker sampling playback for the media media
// reports. clock may be nil for the real clock.
func NewPositionTracker(
	memory *Memory, playback audio.PlaybackManager, media func() *models.ActiveMedia, clock clockwork.Clock,
) *PositionTracker {
	if clock == nil {
		clock = clockwork.NewRealClock()
	}
	return &PositionTracker{
		memory:   memory,
		playback: playback,
		media:    media,
		clock:    clock,
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
	}
}

// Start samples playback until Stop. It does nothing without memory or
// playback.
func (t *PositionTracker) Start() {
	if t == nil {
		return
	}
	if t.memory == nil || t.playback == nil {
		close(t.done)
		return
	}
	go func() {
		defer close(t.done)
		ticker := t.clock.NewTicker(pollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.Chan():
				t.Sample()
			case <-t.stop:
				return
			}
		}
	}()
}

// Stop saves where the playing track is and stops sampling. Call it before
// playback is torn down.
func (t *PositionTracker) Stop() {
	if t == nil {
		return
	}
	t.mu.Lock()
	if t.stopped {
		t.mu.Unlock()
		<-t.done
		return
	}
	t.stopped = true
	t.mu.Unlock()
	close(t.stop)
	<-t.done
	if t.memory == nil || t.playback == nil {
		return
	}
	t.Sample()
	t.mu.Lock()
	if t.current != nil {
		t.checkpoint(t.current)
	}
	t.mu.Unlock()
}

// Finished forgets the position of a track that played to its end.
func (t *PositionTracker) Finished(path string) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.current == nil || t.current.path != path {
		return
	}
	cur := t.current
	t.current = nil
	if cur.length >= MinResumeLength {
		t.memory.ForgetPosition(cur.profileID, cur.systemID, cur.path)
	}
}

// Sample reads the playing track and saves its position when it paused,
// changed or stopped, or has played on for a while since the last save.
func (t *PositionTracker) Sample() {
	if t == nil || t.memory == nil || t.playback == nil {
		return
	}
	media := t.media()
	var state audio.PlaybackState
	if media != nil && media.LauncherID == platforms.NativeAudioLauncherID {
		state = t.playback.State(mediaslot.Primary)
	}
	now := t.clock.Now()

	t.mu.Lock()
	defer t.mu.Unlock()
	cur := t.current
	if state.Path == "" || media == nil || state.Path != media.Path {
		if cur != nil {
			t.checkpoint(cur)
			t.current = nil
		}
		return
	}
	if cur == nil || cur.path != state.Path {
		if cur != nil {
			t.checkpoint(cur)
		}
		cur = &trackedTrack{
			profileID: t.memory.activeProfile(),
			systemID:  systemdefs.SystemAudio,
			path:      state.Path,
			lastSaved: now,
		}
		t.current = cur
	}
	cur.position = state.Position
	cur.length = state.Duration
	pausedNow := state.Paused && !cur.paused
	cur.paused = state.Paused
	if pausedNow || (!state.Paused && now.Sub(cur.lastSaved) >= saveInterval) {
		t.checkpoint(cur)
		cur.lastSaved = now
	}
}

// checkpoint writes where cur is. A long track near either end is forgotten
// rather than saved, so starting it over or nearly finishing it does not
// resume it there.
func (t *PositionTracker) checkpoint(cur *trackedTrack) {
	if cur.length < MinResumeLength {
		return
	}
	if Resumable(cur.position, cur.length) {
		t.memory.SavePosition(cur.profileID, cur.systemID, cur.path, cur.position, cur.length)
		return
	}
	t.memory.ForgetPosition(cur.profileID, cur.systemID, cur.path)
}
