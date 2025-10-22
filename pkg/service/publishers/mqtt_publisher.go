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

package publishers

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	mqttreader "github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/mqtt"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

// MQTTPublisher publishes system notifications to an MQTT broker.
type MQTTPublisher struct {
	client mqtt.Client
	stopCh chan struct{}
	broker string
	topic  string
	filter []string
}

// NewMQTTPublisher creates a new MQTT publisher for the given broker, topic, and optional filter.
// If filter is empty, all notifications are published. Otherwise, only notifications matching
// the filter list are published.
func NewMQTTPublisher(broker, topic string, filter []string) *MQTTPublisher {
	return &MQTTPublisher{
		broker: broker,
		topic:  topic,
		filter: filter,
		stopCh: make(chan struct{}),
	}
}

// Start connects to the MQTT broker and begins publishing notifications.
func (p *MQTTPublisher) Start(notifications <-chan models.Notification) error {
	// Parse protocol and TLS settings from broker URL
	protocolInfo := mqttreader.ParseMQTTProtocol(p.broker)
	brokerURL := fmt.Sprintf("%s://%s", protocolInfo.Protocol, protocolInfo.Remainder)

	// Configure MQTT client options
	opts := mqtt.NewClientOptions()
	opts.AddBroker(brokerURL)
	opts.SetClientID("zaparoo-publisher-" + uuid.New().String()[:8])
	opts.SetAutoReconnect(true)
	opts.SetConnectRetry(true)
	opts.SetConnectTimeout(10 * time.Second)
	opts.SetOrderMatters(false) // Allow blocking in message handlers

	// Look up authentication credentials from auth.toml using user-provided broker URL
	creds := config.LookupAuth(config.GetAuthCfg(), p.broker)
	if creds != nil {
		if creds.Username != "" {
			opts.SetUsername(creds.Username)
			opts.SetPassword(creds.Password)
			log.Debug().Msgf("mqtt publisher: using authentication for %s", protocolInfo.Remainder)
		}
	}

	// Configure TLS if using secure connection
	if protocolInfo.UseTLS {
		tlsConfig := &tls.Config{
			InsecureSkipVerify: false,
			MinVersion:         tls.VersionTLS12,
		}
		opts.SetTLSConfig(tlsConfig)
		log.Debug().Msgf("mqtt publisher: using TLS for %s", protocolInfo.Remainder)
	}

	// Set up connection handlers
	opts.OnConnect = func(_ mqtt.Client) {
		log.Info().Msgf("mqtt publisher: connected to %s", p.broker)
	}

	opts.OnConnectionLost = func(_ mqtt.Client, err error) {
		log.Warn().Err(err).Msg("mqtt publisher: connection lost")
	}

	// Create and connect client
	p.client = mqtt.NewClient(opts)

	token := p.client.Connect()
	if token.Wait() && token.Error() != nil {
		return fmt.Errorf("failed to connect to MQTT broker: %w", token.Error())
	}

	log.Info().Msgf("mqtt publisher: connected to %s (topic: %s)", p.broker, p.topic)

	// Start publishing goroutine
	go p.publishNotifications(notifications)

	return nil
}

// Stop disconnects from the MQTT broker and stops publishing.
func (p *MQTTPublisher) Stop() {
	close(p.stopCh)

	if p.client != nil && p.client.IsConnected() {
		log.Debug().Msg("mqtt publisher: disconnecting")
		p.client.Disconnect(250)
	}
}

// publishNotifications is the main loop that forwards notifications to MQTT.
func (p *MQTTPublisher) publishNotifications(notifications <-chan models.Notification) {
	log.Debug().Msg("mqtt publisher: starting notification publisher goroutine")

	for {
		select {
		case <-p.stopCh:
			log.Debug().Msg("mqtt publisher: stopping notification publisher")
			return
		case notif, ok := <-notifications:
			if !ok {
				log.Debug().Msg("mqtt publisher: notification channel closed")
				return
			}

			// Apply filter if configured
			if !p.matchesFilter(notif.Method) {
				continue
			}

			// Marshal params to JSON (direct payload, no JSON-RPC wrapper)
			payload, err := json.Marshal(notif.Params)
			if err != nil {
				log.Error().Err(err).Msgf("mqtt publisher: failed to marshal notification")
				continue
			}

			// Publish to MQTT broker
			// QoS 1 = at-least-once delivery for reliability
			token := p.client.Publish(p.topic, 1, false, payload)
			if token.Wait() && token.Error() != nil {
				log.Error().Err(token.Error()).Msgf("mqtt publisher: failed to publish message")
				continue
			}

			log.Debug().Msgf("mqtt publisher: published %s notification", notif.Method)
		}
	}
}

// matchesFilter checks if a notification method matches the configured filter.
// If filter is empty, all notifications pass. Otherwise, only notifications
// in the filter list are published.
func (p *MQTTPublisher) matchesFilter(method string) bool {
	// If no filter configured, publish everything
	if len(p.filter) == 0 {
		return true
	}

	// Check if method is in the filter list
	for _, f := range p.filter {
		if f == method {
			return true
		}
	}

	return false
}
