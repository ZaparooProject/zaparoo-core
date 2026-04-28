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

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/publishers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/rs/zerolog/log"
)

// startPublishers initializes and starts all configured publishers.
// Returns a slice of active publishers and a cancel function for graceful shutdown.
func startPublishers(
	st *state.State,
	cfg *config.Instance,
	notifChan <-chan models.Notification,
) ([]publishers.Publisher, context.CancelFunc) {
	activePublishers := make([]publishers.Publisher, 0)

	mqttConfigs := cfg.GetMQTTPublishers()
	if len(mqttConfigs) > 0 {
		for _, mqttCfg := range mqttConfigs {
			// Skip if explicitly disabled (nil = enabled by default)
			if mqttCfg.Enabled != nil && !*mqttCfg.Enabled {
				continue
			}

			log.Info().Msgf("starting MQTT publisher: %s (topic: %s)", mqttCfg.Broker, mqttCfg.Topic)

			publisher := publishers.NewMQTTPublisher(mqttCfg.Broker, mqttCfg.Topic, mqttCfg.Filter)
			if err := publisher.Start(st.GetContext()); err != nil {
				log.Error().Err(err).Msgf("failed to start MQTT publisher for %s", mqttCfg.Broker)
				continue
			}

			activePublishers = append(activePublishers, publisher)
		}
	}

	for _, pcCfg := range cfg.GetPixelCadePublishers() {
		if pcCfg.Enabled != nil && !*pcCfg.Enabled {
			continue
		}

		log.Info().Msgf("starting PixelCade publisher: %s:%d", pcCfg.Host, pcCfg.Port)

		publisher := publishers.NewPixelCadePublisher(
			pcCfg.Host, pcCfg.Port, pcCfg.Mode, pcCfg.Filter,
		)
		if err := publisher.Start(st.GetContext()); err != nil {
			log.Error().Err(err).Msgf("failed to start PixelCade publisher for %s", pcCfg.Host)
			continue
		}

		activePublishers = append(activePublishers, publisher)
	}

	if len(activePublishers) > 0 {
		log.Info().Msgf("started %d publisher(s)", len(activePublishers))
	}

	// CRITICAL: Always start the drain goroutine, even if there are no active publishers.
	// The notifChan MUST be consumed or it will fill up and block the notification system.
	// If there are no publishers, notifications are simply discarded after being consumed.
	ctx, cancel := context.WithCancel(st.GetContext()) //nolint:gosec // G118: cancel returned to caller
	go func() {
		for {
			select {
			case <-ctx.Done():
				log.Debug().Msg("publisher fan-out: stopping")
				return
			case notif, ok := <-notifChan:
				if !ok {
					log.Debug().Msg("publisher fan-out: notification channel closed")
					return
				}
				// Publish to all active publishers sequentially
				// If no publishers, notification is simply discarded
				// Timeout in Publish() prevents blocking indefinitely
				for _, pub := range activePublishers {
					if err := pub.Publish(notif); err != nil {
						log.Warn().Err(err).Msgf("failed to publish %s notification", notif.Method)
					}
				}
			}
		}
	}()

	return activePublishers, cancel
}
