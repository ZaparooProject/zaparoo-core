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

package publishers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	mqttreader "github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/mqtt"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/rs/zerolog/log"
)

const publisherRetryInterval = 30 * time.Second

// MQTTPublisher publishes system notifications to an MQTT broker.
type MQTTPublisher struct {
	client mqtt.Client
	ctx    context.Context
	cancel context.CancelFunc
	broker string
	topic  string
	filter []string
	mu     syncutil.RWMutex
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

// Start connects to the MQTT broker. If the initial connection fails, a background
// retry loop is started and nil is returned. Only config validation errors (empty
// broker or topic) cause Start to return an error.
func (p *MQTTPublisher) Start(ctx context.Context) error {
	if p.broker == "" {
		return errors.New("mqtt publisher: broker address is required")
	}
	if p.topic == "" {
		return errors.New("mqtt publisher: topic is required")
	}

	p.ctx, p.cancel = context.WithCancel(ctx)

	if err := p.connect(); err != nil {
		log.Warn().Err(err).Msgf("mqtt publisher: initial connection to %s failed, retrying in background", p.broker)
		go p.retryConnect()
		return nil
	}

	return nil
}

// connect attempts a single connection to the MQTT broker. On success it sets
// p.client under write lock. On failure it cleans up and returns an error.
func (p *MQTTPublisher) connect() error {
	opts := mqttreader.NewClientOptions(p.broker, "zaparoo-publisher-")

	opts.OnConnect = func(_ mqtt.Client) {
		log.Info().Msgf("mqtt publisher: connected to %s", p.broker)
	}
	opts.OnConnectionLost = func(_ mqtt.Client, err error) {
		log.Warn().Err(err).Msg("mqtt publisher: connection lost")
	}

	client := mqtt.NewClient(opts)

	token := client.Connect()
	if !token.WaitTimeout(5 * time.Second) {
		client.Disconnect(0)
		return errors.New("connection timeout")
	}
	if err := token.Error(); err != nil {
		client.Disconnect(0)
		return fmt.Errorf("connection error: %w", err)
	}

	p.mu.Lock()
	p.client = client
	p.mu.Unlock()

	log.Info().Msgf("mqtt publisher: connected to %s (topic: %s)", p.broker, p.topic)
	return nil
}

// retryConnect periodically attempts to connect until success or context cancellation.
func (p *MQTTPublisher) retryConnect() {
	ticker := time.NewTicker(publisherRetryInterval)
	defer ticker.Stop()

	for {
		select {
		case <-p.ctx.Done():
			log.Debug().Msgf("mqtt publisher: retry cancelled for %s", p.broker)
			return
		case <-ticker.C:
			if p.IsConnected() {
				return
			}

			if err := p.connect(); err != nil {
				log.Warn().Err(err).Msgf("mqtt publisher: retry connection to %s failed", p.broker)
				continue
			}

			return
		}
	}
}

// IsConnected returns true if the publisher has an active MQTT connection.
func (p *MQTTPublisher) IsConnected() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.client != nil && p.client.IsConnected()
}

// Stop disconnects from the MQTT broker and cancels any retry loop.
func (p *MQTTPublisher) Stop() {
	if p.cancel != nil {
		p.cancel()
	}

	p.mu.Lock()
	defer p.mu.Unlock()

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
	p.mu.RLock()
	client := p.client
	p.mu.RUnlock()

	if client == nil || !client.IsConnected() {
		return fmt.Errorf("mqtt publisher for %s is not connected", p.broker)
	}

	if !p.matchesFilter(notif.Method) {
		return nil
	}

	payload, err := json.Marshal(notif)
	if err != nil {
		log.Error().Err(err).Msg("mqtt publisher: failed to marshal notification")
		return fmt.Errorf("failed to marshal notification: %w", err)
	}

	token := client.Publish(p.topic, 1, false, payload)

	if !token.WaitTimeout(2 * time.Second) {
		return fmt.Errorf("publish to %s timed out", p.broker)
	}

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
	if len(p.filter) == 0 {
		return true
	}

	for _, f := range p.filter {
		if f == method {
			return true
		}
	}

	return false
}
