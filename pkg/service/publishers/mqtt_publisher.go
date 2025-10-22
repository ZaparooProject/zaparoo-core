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
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	mqttreader "github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/mqtt"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/rs/zerolog/log"
)

// MQTTPublisher publishes system notifications to an MQTT broker.
type MQTTPublisher struct {
	client mqtt.Client
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
	}
}

// Start connects to the MQTT broker.
func (p *MQTTPublisher) Start() error {
	// Configure MQTT client options using shared helper
	opts := mqttreader.NewClientOptions(p.broker, "zaparoo-publisher-")

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
	// Use WaitTimeout to prevent indefinite blocking (5 second timeout)
	if !token.WaitTimeout(5 * time.Second) {
		// Clean up client on timeout to prevent resource leak
		p.client.Disconnect(0)
		p.client = nil
		return errors.New("failed to connect to MQTT broker: connection timeout")
	}
	if err := token.Error(); err != nil {
		// Clean up client on error to prevent resource leak
		p.client.Disconnect(0)
		p.client = nil
		return fmt.Errorf("failed to connect to MQTT broker: %w", err)
	}

	log.Info().Msgf("mqtt publisher: connected to %s (topic: %s)", p.broker, p.topic)
	return nil
}

// Stop disconnects from the MQTT broker.
func (p *MQTTPublisher) Stop() {
	if p.client != nil && p.client.IsConnected() {
		log.Debug().Msg("mqtt publisher: disconnecting")
		p.client.Disconnect(250)
	}
}

// Publish sends a notification to the MQTT broker if it matches the filter.
// This is a synchronous, blocking call that waits for publish confirmation with a timeout.
// It returns an error if publishing fails or times out.
// This method is safe to call concurrently.
func (p *MQTTPublisher) Publish(notif models.Notification) error {
	// Guard against calls on an uninitialized or disconnected client
	if p.client == nil || !p.client.IsConnected() {
		return fmt.Errorf("mqtt publisher for %s is not connected", p.broker)
	}

	// Apply filter if configured
	if !p.matchesFilter(notif.Method) {
		return nil // Not an error, just filtered out
	}

	// Marshal notification to JSON (includes method and params)
	payload, err := json.Marshal(notif)
	if err != nil {
		log.Error().Err(err).Msg("mqtt publisher: failed to marshal notification")
		return fmt.Errorf("failed to marshal notification: %w", err)
	}

	// Publish to MQTT broker (QoS 1 = at-least-once delivery)
	token := p.client.Publish(p.topic, 1, false, payload)

	// Block until publish is complete or times out
	// 2 second timeout is reasonable for local/cloud MQTT brokers
	if !token.WaitTimeout(2 * time.Second) {
		return fmt.Errorf("publish to %s timed out", p.broker)
	}

	// Check for error from the broker after waiting
	if err := token.Error(); err != nil {
		return fmt.Errorf("failed to publish to %s: %w", p.broker, err)
	}

	log.Debug().Msgf("mqtt publisher: published %s notification to %s", notif.Method, p.broker)
	return nil
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
