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
	"encoding/json"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/methods"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/notifications"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mediaslot"
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
	notifChan, subID := b.Subscribe(32, models.NotificationStarted, models.NotificationStopped)
	defer b.Unsubscribe(subID)

	primaryActive := func() bool { return activeMediaPausesMediaWork(st.ActiveMedia()) }
	handleIndexPauseNotifications(ctx, notifChan, ns, pauser, primaryActive(), methods.IsIndexing, primaryActive)
}

// watchGameForScrapePause mirrors media indexing pause behavior for metadata
// scraping so SQLite and filesystem-heavy scrape work does not compete with gameplay.
func watchGameForScrapePause(
	ctx context.Context,
	b *broker.Broker,
	st *state.State,
	ns chan<- models.Notification,
	pauser *syncutil.Pauser,
) {
	notifChan, subID := b.Subscribe(32, models.NotificationStarted, models.NotificationStopped)
	defer b.Unsubscribe(subID)

	primaryActive := func() bool { return activeMediaPausesMediaWork(st.ActiveMedia()) }
	handleScrapePauseNotifications(
		ctx, notifChan, ns, pauser, primaryActive(), methods.IsScrapingRunning, primaryActive,
	)
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
	primaryActive ...func() bool,
) {
	handleMediaPauseNotifications(
		ctx, notifChan, pauser, gameAlreadyActive, isActive, "media indexing",
		func(paused bool) {
			sendIndexPauseNotification(ns, paused)
		}, primaryActive...,
	)
}

func handleScrapePauseNotifications(
	ctx context.Context,
	notifChan <-chan models.Notification,
	ns chan<- models.Notification,
	pauser *syncutil.Pauser,
	gameAlreadyActive bool,
	isActive func() bool,
	primaryActive ...func() bool,
) {
	handleMediaPauseNotifications(
		ctx, notifChan, pauser, gameAlreadyActive, isActive, "media scraping",
		func(paused bool) {
			sendScrapePauseNotification(ns, paused)
		}, primaryActive...,
	)
}

func activeMediaPausesMediaWork(media *models.ActiveMedia) bool {
	if media == nil {
		return false
	}
	slot, err := mediaslot.Normalize(media.Slot)
	if err != nil {
		log.Warn().Err(err).Str("slot", media.Slot).Msg("active media has invalid slot; pausing media work")
		return true
	}
	return slot == mediaslot.Primary
}

func notificationIsPrimarySlot(notif models.Notification) bool {
	if len(notif.Params) == 0 {
		return true
	}

	var payload struct {
		Slot string `json:"slot"`
	}
	if err := json.Unmarshal(notif.Params, &payload); err != nil {
		log.Warn().Err(err).Str("method", notif.Method).Msg("media pause notification has invalid payload")
		return true
	}

	slot, err := mediaslot.Normalize(payload.Slot)
	if err != nil {
		log.Warn().Err(err).Str("method", notif.Method).Str("slot", payload.Slot).
			Msg("media pause notification has invalid slot")
		return true
	}
	return slot == mediaslot.Primary
}

func primaryActiveFunc(primaryActive []func() bool) func() bool {
	if len(primaryActive) == 0 || primaryActive[0] == nil {
		return nil
	}
	return primaryActive[0]
}

func shouldPauseForMediaStarted(notif models.Notification, primaryActive ...func() bool) bool {
	if isPrimaryActive := primaryActiveFunc(primaryActive); isPrimaryActive != nil {
		return isPrimaryActive()
	}
	return notificationIsPrimarySlot(notif)
}

func shouldResumeForMediaStopped(notif models.Notification, primaryActive ...func() bool) bool {
	if isPrimaryActive := primaryActiveFunc(primaryActive); isPrimaryActive != nil {
		return !isPrimaryActive()
	}
	return notificationIsPrimarySlot(notif)
}

func handleMediaPauseNotifications(
	ctx context.Context,
	notifChan <-chan models.Notification,
	pauser *syncutil.Pauser,
	gameAlreadyActive bool,
	isActive func() bool,
	label string,
	sendPauseNotification func(bool),
	primaryActive ...func() bool,
) {
	defer pauser.Resume()

	if gameAlreadyActive {
		pauser.Pause()
		log.Info().Msg(label + " paused: game already active")
		if isActive() {
			sendPauseNotification(true)
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
				if !shouldPauseForMediaStarted(notif, primaryActive...) {
					continue
				}
				pauser.Pause()
				log.Info().Msg(label + " paused: game started")
				if isActive() {
					sendPauseNotification(true)
				}
			case models.NotificationStopped:
				if !shouldResumeForMediaStopped(notif, primaryActive...) || !pauser.IsPaused() {
					continue
				}
				pauser.Resume()
				log.Info().Msg(label + " resumed: game stopped")
				if isActive() {
					sendPauseNotification(false)
				}
			}
		case <-ctx.Done():
			return
		}
	}
}

func sendIndexPauseNotification(
	ns chan<- models.Notification,
	paused bool,
) {
	notifications.MediaIndexing(ns, models.IndexingStatusResponse{
		Indexing: true,
		Paused:   paused,
	})
}

func sendScrapePauseNotification(
	ns chan<- models.Notification,
	paused bool,
) {
	methods.PublishScrapePauseStatus(ns, paused)
}
