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

// defaultStopIntentTTL bounds how long a recorded Core stop stays attached to
// the media it was meant for. Some platforms only notice a stop on their next
// poll, a couple of seconds later; a stop that never took effect must not
// label an unrelated exit minutes afterwards.
const defaultStopIntentTTL = 15 * time.Second

// mediaStopIntent is a stop Core asked for and has not yet seen take effect.
type mediaStopIntent struct {
	at     time.Time
	reason string
	gen    uint64
	// seq identifies the note so the caller that recorded it can withdraw
	// exactly that note and nothing recorded since.
	seq uint64
}

// MediaStart describes media that has just become active in a slot.
type MediaStart struct {
	Slot  string
	Media models.ActiveMedia
	// Gen identifies this publication of primary-slot media. It is zero for
	// the background slot.
	Gen uint64
}

// MediaEnd describes media that has just stopped being active in a slot.
type MediaEnd struct {
	Slot   string
	Reason string
	Media  models.ActiveMedia
	// Gen is the generation the ended media was published under. It is zero
	// for the background slot.
	Gen uint64
}

// MediaObserver is told about every change of active media. Callbacks run on
// the goroutine that changed the media, outside State's lock, and must return
// quickly: hand the event to another goroutine rather than doing work or
// calling back into State's launch and stop gates.
type MediaObserver interface {
	MediaStarted(MediaStart)
	MediaEnded(MediaEnd)
}

// AddMediaObserver registers an observer for media starts and ends in both
// slots. Observers are never removed.
func (s *State) AddMediaObserver(observer MediaObserver) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Copied so a notifier iterating the old slice outside the lock never
	// sees it written.
	observers := make([]MediaObserver, 0, len(s.mediaObservers)+1)
	observers = append(observers, s.mediaObservers...)
	observers = append(observers, observer)
	s.mediaObservers = observers
}

// RecordMediaStop notes that Core is about to stop, or has just asked for the
// stop of, the current primary media, so the clear that follows is reported
// with reason (models.MediaStopReasonStopped or MediaStopReasonReplaced)
// instead of as the media quitting by itself. It does nothing when no media
// is active. The note covers only the media active now and lapses after a few
// seconds if nothing clears it. The returned function withdraws the note, for
// a stop that did not take effect, and leaves any later note alone.
func (s *State) RecordMediaStop(reason string) (cancel func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if reason == models.MediaStopReasonStopped {
		// Counted with or without media: a stop that finds nothing to stop
		// can still land between one launch ending and the next publishing,
		// and has to cancel that launch.
		s.userStopSeq++
	}
	if s.activeMedia == nil {
		return func() {}
	}
	s.stopSeq++
	seq := s.stopSeq
	s.stopIntent = mediaStopIntent{
		reason: reason,
		gen:    s.activeMediaReadyGen,
		at:     time.Now(),
		seq:    seq,
	}
	return func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.stopIntent.seq == seq {
			s.stopIntent = mediaStopIntent{}
		}
	}
}

// UserStopSeq counts the stops Core has been asked to make of primary media,
// as opposed to media it replaced to launch something else. Work that was
// decided before a stop and has not launched yet compares the count it saw
// with the current one to learn the stop came first.
func (s *State) UserStopSeq() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.userStopSeq
}

// FinishActiveMedia reports that the primary media published under gen
// reached its natural end. It records the end as finished and clears the
// media, and does nothing when other media has taken its place.
func (s *State) FinishActiveMedia(gen uint64) bool {
	// The condition runs with s.mu held, so it reads the generation directly.
	return s.updateActiveMediaStateAs(nil, func(*models.ActiveMedia) bool {
		return s.activeMediaReadyGen == gen
	}, models.MediaStopReasonFinished)
}

// ExitActiveMedia reports that the primary media published under gen ended
// without reaching its natural end and without Core's involvement, such as
// a player stopped from its own remote. It clears the media with whatever stop
// Core recorded for it, or as exited, and does nothing when other media has
// taken its place.
func (s *State) ExitActiveMedia(gen uint64) bool {
	// The condition runs with s.mu held, so it reads the generation directly.
	return s.updateActiveMediaState(nil, func(*models.ActiveMedia) bool {
		return s.activeMediaReadyGen == gen
	})
}

// ActiveMediaGenerationIs reports whether the primary media currently active
// was published under gen.
func (s *State) ActiveMediaGenerationIs(gen uint64) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.activeMedia != nil && s.activeMediaReadyGen == gen
}

// takeStopReasonLocked returns the recorded reason for the media about to
// leave the primary slot, or fallback when Core did not ask for the stop. It
// consumes the note. The caller holds s.mu.
func (s *State) takeStopReasonLocked(fallback string) string {
	intent := s.stopIntent
	s.stopIntent = mediaStopIntent{}
	if intent.reason == "" || intent.gen != s.activeMediaReadyGen {
		return fallback
	}
	if time.Since(intent.at) > s.stopIntentTTL {
		return fallback
	}
	return intent.reason
}

func notifyMediaStarted(observers []MediaObserver, media *models.ActiveMedia, slot string, gen uint64) {
	if len(observers) == 0 {
		return
	}
	event := MediaStart{Media: *media, Slot: slot, Gen: gen}
	for _, observer := range observers {
		observer.MediaStarted(event)
	}
}

func notifyMediaEnded(
	observers []MediaObserver, media *models.ActiveMedia, slot, reason string, gen uint64,
) {
	if len(observers) == 0 {
		return
	}
	event := MediaEnd{Media: *media, Slot: slot, Reason: reason, Gen: gen}
	for _, observer := range observers {
		observer.MediaEnded(event)
	}
}
