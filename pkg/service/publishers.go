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
	"slices"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/publishers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/rs/zerolog/log"
)

// publisherSet holds the running publishers. A settings change rebuilds it
// from the config, so publishers can be turned on, off or refiltered
// without restarting Core.
type publisherSet struct {
	st     *state.State
	cfg    *config.Instance
	active []publishers.Publisher
	// mu is held for reading while a notification is published and for
	// writing while the set is swapped, so a publisher is never stopped in
	// the middle of a publish.
	mu syncutil.RWMutex
}

// snapshot returns the publishers running now.
func (p *publisherSet) snapshot() []publishers.Publisher {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return slices.Clone(p.active)
}

// rebuild stops the running publishers and starts the ones the config
// describes now. The old ones stop first, so a broker never sees the same
// client connected twice.
func (p *publisherSet) rebuild() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, publisher := range p.active {
		publisher.Stop()
	}
	p.active = buildPublishers(p.st, p.cfg)
}

// stop stops every running publisher.
func (p *publisherSet) stop() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, publisher := range p.active {
		publisher.Stop()
	}
	p.active = nil
}

func (p *publisherSet) publish(notif models.Notification) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	// Publish to all active publishers sequentially. With none, the
	// notification is simply discarded. The timeout in Publish keeps one
	// slow publisher from blocking indefinitely.
	for _, pub := range p.active {
		if err := pub.Publish(notif); err != nil {
			log.Warn().Err(err).Msgf("failed to publish %s notification", notif.Method)
		}
	}
}

// buildPublishers starts every enabled publisher in the config.
func buildPublishers(st *state.State, cfg *config.Instance) []publishers.Publisher {
	activePublishers := make([]publishers.Publisher, 0)

	for _, mqttCfg := range cfg.GetMQTTPublishers() {
		// Skip if explicitly disabled (nil = enabled by default)
		if mqttCfg.Enabled != nil && !*mqttCfg.Enabled {
			continue
		}

		redactedBroker := config.RedactBroker(mqttCfg.Broker)
		log.Info().Msgf("starting MQTT publisher: %s (topic: %s)", redactedBroker, mqttCfg.Topic)

		publisher := publishers.NewMQTTPublisher(mqttCfg.Broker, mqttCfg.Topic, mqttCfg.Filter)
		if err := publisher.Start(st.GetContext()); err != nil {
			log.Error().Err(err).Msgf("failed to start MQTT publisher for %s", redactedBroker)
			continue
		}

		activePublishers = append(activePublishers, publisher)
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
	return activePublishers
}

// startPublishers starts all configured publishers and the fan-out that
// feeds them. It returns the set, a cancel function for graceful shutdown
// and a channel closed when the fan-out has stopped.
func startPublishers(
	st *state.State,
	cfg *config.Instance,
	notifChan <-chan models.Notification,
) (*publisherSet, context.CancelFunc, <-chan struct{}) {
	set := &publisherSet{st: st, cfg: cfg}
	set.active = buildPublishers(st, cfg)

	// CRITICAL: Always start the drain goroutine, even if there are no active publishers.
	// The notifChan MUST be consumed or it will fill up and block the notification system.
	// If there are no publishers, notifications are simply discarded after being consumed.
	ctx, cancel := context.WithCancel(st.GetContext()) //nolint:gosec // G118: cancel returned to caller
	done := make(chan struct{})
	go func() {
		defer close(done)
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
				set.publish(notif)
			}
		}
	}()

	return set, cancel, done
}
