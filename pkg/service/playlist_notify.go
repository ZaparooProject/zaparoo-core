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
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mediaslot"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript"
)

// openPlaylistsSource is where the playlist notifier reads the playlists
// open now.
type openPlaylistsSource interface {
	GetActivePlaylist() *playlists.Playlist
	GetBackgroundPlaylist() *playlists.Playlist
}

// playlistNotifier publishes playlists.changed whenever what the open
// playlists look like to a client changes. The state observer only signals
// it; the payload is built here, off the state lock, from the playlists as
// they are when it runs, so a burst of changes is reported once with the
// latest state.
type playlistNotifier struct {
	source  openPlaylistsSource
	publish func(models.Notification)
	signal  chan struct{}
	last    []byte
}

func newPlaylistNotifier(source openPlaylistsSource, publish func(models.Notification)) *playlistNotifier {
	return &playlistNotifier{
		source:  source,
		publish: publish,
		signal:  make(chan struct{}, 1),
	}
}

// Signal asks the notifier to look at the playlists again. It never blocks,
// so it is safe to call under the state lock.
func (n *playlistNotifier) Signal() {
	if n == nil {
		return
	}
	select {
	case n.signal <- struct{}{}:
	default:
	}
}

// run publishes on every signal until ctx ends.
func (n *playlistNotifier) run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-n.signal:
			n.publishCurrent()
		}
	}
}

// publishCurrent publishes the open playlists unless they look the same as
// the last ones published.
func (n *playlistNotifier) publishCurrent() {
	payload := models.PlaylistsChangedParams{Playlists: make([]models.PlaylistState, 0, 2)}
	for _, open := range []struct {
		pls  *playlists.Playlist
		slot string
	}{
		{pls: n.source.GetActivePlaylist(), slot: mediaslot.Primary},
		{pls: n.source.GetBackgroundPlaylist(), slot: mediaslot.Background},
	} {
		if open.pls == nil {
			continue
		}
		state := open.pls.APIState()
		// The stored scripts stay as they are for playback; the copy that
		// goes to clients must not carry profile switch IDs.
		for i := range state.Items {
			state.Items[i].ZapScript = zapscript.RedactScript(state.Items[i].ZapScript)
		}
		if state.Slot == "" {
			state.Slot = open.slot
		}
		payload.Playlists = append(payload.Playlists, state)
	}
	notifications.PlaylistsChanged(func(notification models.Notification) {
		if n.last != nil && bytes.Equal(notification.Params, n.last) {
			return
		}
		n.last = notification.Params
		n.publish(notification)
	}, payload)
}
