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
	"bytes"
	"context"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/notifications"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/audio"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mediaslot"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
)

// playbackChangeRegistrar is the part of the long-form playback manager that
// reports playback changes. It is not part of audio.PlaybackManager.
type playbackChangeRegistrar interface {
	SetChangeCallback(fn func(slot string))
}

// playbackNotifier publishes media.playback for native audio in a slot when
// a track starts, pauses, resumes or seeks, or its tags finish reading. The
// playback manager and the media observer only signal it; the payload is
// built here, off their goroutines and locks, from the slot as it is when it
// runs, so a burst of changes in one slot is reported once with the latest
// state.
type playbackNotifier struct {
	// entry builds what media.active reports for a slot, or nil.
	entry    func(slot string) *models.ActiveMediaResponse
	playback audio.PlaybackManager
	publish  func(models.Notification)
	primary  chan struct{}
	bg       chan struct{}
	// last holds the params last published per slot. Only run touches it.
	last map[string][]byte
}

func newPlaybackNotifier(
	entry func(slot string) *models.ActiveMediaResponse,
	playback audio.PlaybackManager,
	publish func(models.Notification),
) *playbackNotifier {
	return &playbackNotifier{
		entry:    entry,
		playback: playback,
		publish:  publish,
		primary:  make(chan struct{}, 1),
		bg:       make(chan struct{}, 1),
		last:     make(map[string][]byte, 2),
	}
}

// Signal asks the notifier to report slot, a normalized slot key, again. It
// never blocks, so it is safe to call from the playback manager and under
// State's observer contract.
func (n *playbackNotifier) Signal(slot string) {
	if n == nil {
		return
	}
	var ch chan struct{}
	switch slot {
	case mediaslot.Primary:
		ch = n.primary
	case mediaslot.Background:
		ch = n.bg
	default:
		return
	}
	select {
	case ch <- struct{}{}:
	default:
	}
}

// MediaStarted implements state.MediaObserver. A track's playback goes live
// before its media is published, so the start is reported again once both
// are in place.
func (n *playbackNotifier) MediaStarted(start state.MediaStart) { //nolint:gocritic // the interface passes it by value
	if start.Media.LauncherID == platforms.NativeAudioLauncherID {
		n.Signal(start.Slot)
	}
}

// MediaEnded implements state.MediaObserver. media.stopped reports the end.
func (*playbackNotifier) MediaEnded(state.MediaEnd) {}

// run publishes on every signal until ctx ends.
func (n *playbackNotifier) run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-n.primary:
			n.publishSlot(mediaslot.Primary)
		case <-n.bg:
			n.publishSlot(mediaslot.Background)
		}
	}
}

// publishSlot publishes the slot's media when it is native audio and the
// track playing there is the one published for it. Between a track going
// live and its media being published, the slot still holds the previous
// track or nothing, and that is not reported. A start is signalled both by
// the track going live and by its media being published, so a payload equal
// to the last one for the slot is not sent again.
func (n *playbackNotifier) publishSlot(slot string) {
	if n.playback == nil {
		return
	}
	entry := n.entry(slot)
	if entry == nil || entry.LauncherID != platforms.NativeAudioLauncherID {
		return
	}
	if entry.Path == "" || n.playback.State(slot).Path != entry.Path {
		return
	}
	notifications.MediaPlayback(func(notification models.Notification) {
		if bytes.Equal(notification.Params, n.last[slot]) {
			return
		}
		n.last[slot] = notification.Params
		n.publish(notification)
	}, *entry)
}
