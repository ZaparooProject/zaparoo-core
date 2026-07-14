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
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mediaslot"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/broker"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/rs/zerolog/log"
)

// watchGameForIndexPause subscribes to the notification broker and throttles
// (or, per config, pauses) the given Pauser when a game starts
// (media.started), resuming it when the game stops (media.stopped). This
// keeps resource-intensive media indexing from interfering with game
// performance while still making progress by default.
func watchGameForIndexPause(
	ctx context.Context,
	b *broker.Broker,
	st *state.State,
	cfg *config.Instance,
	ns chan<- models.Notification,
	pauser *syncutil.Pauser,
) {
	notifChan, subID := b.Subscribe(32, models.NotificationStarted, models.NotificationStopped)
	defer b.Unsubscribe(subID)

	primaryActive := func() bool { return activeMediaPausesMediaWork(st.ActiveMedia()) }
	resolvePolicy := func() config.MediaPausePolicy { return cfg.ResolveMediaPausePolicy(activeSystemID(st)) }
	handleIndexPauseNotifications(
		ctx, notifChan, ns, pauser, primaryActive(), methods.IsIndexing, resolvePolicy, primaryActive,
	)
}

// watchGameForScrapePause mirrors media indexing pause behavior for metadata
// scraping so SQLite and filesystem-heavy scrape work does not compete with gameplay.
func watchGameForScrapePause(
	ctx context.Context,
	b *broker.Broker,
	st *state.State,
	cfg *config.Instance,
	ns chan<- models.Notification,
	pauser *syncutil.Pauser,
) {
	notifChan, subID := b.Subscribe(32, models.NotificationStarted, models.NotificationStopped)
	defer b.Unsubscribe(subID)

	primaryActive := func() bool { return activeMediaPausesMediaWork(st.ActiveMedia()) }
	resolvePolicy := func() config.MediaPausePolicy { return cfg.ResolveMediaPausePolicy(activeSystemID(st)) }
	handleScrapePauseNotifications(
		ctx, notifChan, ns, pauser, primaryActive(), methods.IsScrapingRunning, resolvePolicy, primaryActive,
	)
}

// activeSystemID returns the SystemID of the currently active media, or an
// empty string if there is none. An empty SystemID resolves to a
// non-streaming (Light throttle) policy.
func activeSystemID(st *state.State) string {
	media := st.ActiveMedia()
	if media == nil {
		return ""
	}
	return media.SystemID
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
	resolvePolicy func() config.MediaPausePolicy,
	primaryActive ...func() bool,
) {
	handleMediaPauseNotifications(
		ctx, notifChan, pauser, gameAlreadyActive, isActive, "media indexing",
		func(paused, throttled bool) {
			sendIndexPauseNotification(ns, paused, throttled)
		}, resolvePolicy, primaryActive...,
	)
}

func handleScrapePauseNotifications(
	ctx context.Context,
	notifChan <-chan models.Notification,
	ns chan<- models.Notification,
	pauser *syncutil.Pauser,
	gameAlreadyActive bool,
	isActive func() bool,
	resolvePolicy func() config.MediaPausePolicy,
	primaryActive ...func() bool,
) {
	handleMediaPauseNotifications(
		ctx, notifChan, pauser, gameAlreadyActive, isActive, "media scraping",
		func(paused, throttled bool) {
			sendScrapePauseNotification(ns, paused, throttled)
		}, resolvePolicy, primaryActive...,
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
	sendRestrictNotification func(paused, throttled bool),
	resolvePolicy func() config.MediaPausePolicy,
	primaryActive ...func() bool,
) {
	defer pauser.Resume()

	// restrict slows or stops background work per the configured policy. The
	// policy is resolved at event time (against the active media's SystemID)
	// so a settings change or system switch applies to the next game start
	// without a restart.
	restrict := func(reason string) {
		policy := config.MediaPausePolicy{Mode: config.IndexDuringMediaThrottle, Level: syncutil.ThrottleLight}
		if resolvePolicy != nil {
			policy = resolvePolicy()
		}
		if policy.Mode == config.IndexDuringMediaPause {
			pauser.Pause()
			log.Info().Msg(label + " paused: " + reason)
		} else {
			pauser.Throttle(policy.Level)
			log.Info().Msg(label + " throttled: " + reason)
		}
		if isActive() {
			sendRestrictNotification(pauser.IsPaused(), pauser.IsThrottled())
		}
	}

	if gameAlreadyActive {
		restrict("game already active")
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
				restrict("game started")
			case models.NotificationStopped:
				if !shouldResumeForMediaStopped(notif, primaryActive...) ||
					(!pauser.IsPaused() && !pauser.IsThrottled()) {
					continue
				}
				pauser.Resume()
				log.Info().Msg(label + " resumed: game stopped")
				if isActive() {
					sendRestrictNotification(false, false)
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
	throttled bool,
) {
	notifications.MediaIndexing(ns, models.IndexingStatusResponse{
		Indexing:  true,
		Paused:    paused,
		Throttled: throttled,
	})
}

func sendScrapePauseNotification(
	ns chan<- models.Notification,
	paused bool,
	throttled bool,
) {
	methods.PublishScrapePauseStatus(ns, paused, throttled)
}
