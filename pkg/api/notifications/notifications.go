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

func sendNotification(ns chan<- models.Notification, method string, payload any) {
	log.Debug().Msgf("sending notification: %s, %v", method, payload)
	if payload != nil {
		params, err := json.Marshal(payload)
		if err != nil {
			log.Error().Err(err).Msgf("error marshalling notification params: %s", method)
			return
		}
		ns <- models.Notification{
			Method: method,
			Params: params,
		}
	} else {
		ns <- models.Notification{
			Method: method,
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
