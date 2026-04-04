/*
Zaparoo Core
Copyright (c) 2026 The Zaparoo Project Contributors.
SPDX-License-Identifier: GPL-3.0-or-later

This file is part of Zaparoo Core.

Zaparoo Core is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

Zaparoo Core is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with Zaparoo Core.  If not, see <http://www.gnu.org/licenses/>.
*/

package service

import (
	"context"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/methods"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/notifications"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/broker"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/rs/zerolog/log"
)

// watchGameForIndexPause subscribes to the notification broker and pauses the
// given Pauser when a game starts (media.started), resuming it when the game
// stops (media.stopped). This keeps resource-intensive media indexing from
// interfering with game performance.
func watchGameForIndexPause(
	ctx context.Context,
	b *broker.Broker,
	st *state.State,
	ns chan<- models.Notification,
	pauser *syncutil.Pauser,
) {
	notifChan, subID := b.Subscribe(10)
	defer b.Unsubscribe(subID)

	gameActive := st.ActiveMedia() != nil
	handleIndexPauseNotifications(ctx, notifChan, ns, pauser, gameActive, methods.IsIndexing)
}

// handleIndexPauseNotifications is the core loop that pauses/resumes the
// pauser based on game lifecycle notifications. Separated from
// watchGameForIndexPause so it can be tested without a broker.
//
// isActive is called to check if indexing is currently running before sending
// notifications. The pauser is always toggled regardless, but notifications
// are only sent when there is an active indexing operation to report on.
func handleIndexPauseNotifications(
	ctx context.Context,
	notifChan <-chan models.Notification,
	ns chan<- models.Notification,
	pauser *syncutil.Pauser,
	gameAlreadyActive bool,
	isActive func() bool,
) {
	if gameAlreadyActive {
		pauser.Pause()
		log.Info().Msg("media indexing paused: game already active")
		if isActive() {
			sendPauseNotification(ns, true)
		}
	}

	for {
		select {
		case notif, ok := <-notifChan:
			if !ok {
				return
			}
			switch notif.Method {
			case models.NotificationStarted:
				pauser.Pause()
				log.Info().Msg("media indexing paused: game started")
				if isActive() {
					sendPauseNotification(ns, true)
				}
			case models.NotificationStopped:
				pauser.Resume()
				log.Info().Msg("media indexing resumed: game stopped")
				if isActive() {
					sendPauseNotification(ns, false)
				}
			}
		case <-ctx.Done():
			// Resume on shutdown so a paused indexer can see the context
			// cancellation and exit cleanly.
			pauser.Resume()
			return
		}
	}
}

func sendPauseNotification(
	ns chan<- models.Notification,
	paused bool,
) {
	notifications.MediaIndexing(ns, models.IndexingStatusResponse{
		Indexing: true,
		Paused:   paused,
	})
}
