// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
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

package notifications

import (
	"encoding/json"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/rs/zerolog/log"
)

// criticalNotifications are user-facing events that should never be silently dropped.
// These are logged at ERROR level if the channel is full.
var criticalNotifications = map[string]bool{
	models.NotificationTokensAdded:         true,
	models.NotificationTokensRemoved:       true,
	models.NotificationReadersConnected:    true,
	models.NotificationReadersDisconnected: true,
	models.NotificationStarted:             true,
	models.NotificationStopped:             true,
}

func sendNotification(ns chan<- models.Notification, method string, payload any) {
	var notification models.Notification

	if payload != nil {
		params, err := json.Marshal(payload)
		if err != nil {
			log.Error().Err(err).Msgf("error marshalling notification params: %s", method)
			return
		}
		notification = models.Notification{
			Method: method,
			Params: params,
		}
	} else {
		notification = models.Notification{
			Method: method,
		}
	}

	// Use non-blocking send to prevent back-pressure from freezing callers.
	// If the buffer is full, the notification is dropped and logged.
	select {
	case ns <- notification:
		log.Debug().Msgf("notification sent: %s", method)
	default:
		// Log at ERROR level for critical user-facing notifications
		if criticalNotifications[method] {
			log.Error().Msgf("notification channel full, dropping CRITICAL: %s", method)
		} else {
			log.Warn().Msgf("notification channel full, dropping: %s", method)
		}
	}
}

func MediaIndexing(ns chan<- models.Notification, payload models.IndexingStatusResponse) {
	sendNotification(ns, models.NotificationMediaIndexing, payload)
}

func MediaStopped(ns chan<- models.Notification) {
	sendNotification(ns, models.NotificationStopped, nil)
}

func MediaStarted(ns chan<- models.Notification, payload models.MediaStartedParams) {
	sendNotification(ns, models.NotificationStarted, payload)
}

//nolint:gocritic // single-use parameter in notification
func TokensAdded(ns chan<- models.Notification, payload models.TokenResponse) {
	sendNotification(ns, models.NotificationTokensAdded, payload)
}

func TokensRemoved(ns chan<- models.Notification) {
	sendNotification(ns, models.NotificationTokensRemoved, nil)
}

func ReadersAdded(ns chan<- models.Notification, payload models.ReaderResponse) {
	sendNotification(ns, models.NotificationReadersConnected, payload)
}

func ReadersRemoved(ns chan<- models.Notification, payload models.ReaderResponse) {
	sendNotification(ns, models.NotificationReadersDisconnected, payload)
}

func PlaytimeLimitReached(ns chan<- models.Notification, payload models.PlaytimeLimitReachedParams) {
	sendNotification(ns, models.NotificationPlaytimeLimitReached, payload)
}

func PlaytimeLimitWarning(ns chan<- models.Notification, payload models.PlaytimeLimitWarningParams) {
	sendNotification(ns, models.NotificationPlaytimeLimitWarning, payload)
}

func InboxAdded(ns chan<- models.Notification, payload *models.InboxMessage) {
	sendNotification(ns, models.NotificationInboxAdded, payload)
}
