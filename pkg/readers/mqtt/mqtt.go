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

package mqtt

import (
	"crypto/tls"
	"errors"
	"fmt"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

const TokenType = "mqtt"

type Reader struct {
	client        mqtt.Client
	cfg           *config.Instance
	scanCh        chan<- readers.Scan
	clientFactory ClientFactory
	device        config.ReadersConnect
	broker        string
	topic         string
}

func NewReader(cfg *config.Instance) *Reader {
	return &Reader{
		cfg:           cfg,
		clientFactory: DefaultClientFactory,
	}
}

func (*Reader) Metadata() readers.DriverMetadata {
	return readers.DriverMetadata{
		ID:                "mqtt",
		Description:       "MQTT client reader",
		DefaultEnabled:    true,
		DefaultAutoDetect: false,
	}
}

func (*Reader) IDs() []string {
	return []string{"mqtt"}
}

func (r *Reader) Open(device config.ReadersConnect, scanQueue chan<- readers.Scan) error {
	if !helpers.Contains(r.IDs(), device.Driver) {
		return errors.New("invalid reader id: " + device.Driver)
	}

	// Parse broker and topic from path
	broker, topic, err := ParseMQTTPath(device.Path)
	if err != nil {
		return fmt.Errorf("failed to parse MQTT path: %w", err)
	}

	r.device = device
	r.broker = broker
	r.topic = topic
	r.scanCh = scanQueue

	// Parse protocol and TLS settings from device path
	protocolInfo := ParseMQTTProtocol(device.Path)

	// Build auth lookup URL (scheme://broker:port or just broker:port)
	authLookupURL := broker
	if protocolInfo.Scheme != "" {
		authLookupURL = fmt.Sprintf("%s://%s", protocolInfo.Scheme, broker)
	}

	brokerURL := fmt.Sprintf("%s://%s", protocolInfo.Protocol, broker)

	// Configure MQTT client options
	opts := mqtt.NewClientOptions()
	opts.AddBroker(brokerURL)
	opts.SetClientID("zaparoo-mqtt-" + uuid.New().String()[:8])
	opts.SetAutoReconnect(true)
	opts.SetConnectRetry(true)
	opts.SetConnectTimeout(10 * time.Second)
	opts.SetOrderMatters(false) // Allow blocking in message handlers

	// Look up authentication credentials from auth.toml
	creds := config.LookupAuth(config.GetAuthCfg(), authLookupURL)
	if creds != nil {
		if creds.Username != "" {
			opts.SetUsername(creds.Username)
			opts.SetPassword(creds.Password)
			log.Debug().Msgf("mqtt reader: using authentication for %s", broker)
		}
	}

	// Configure TLS if using secure connection
	if protocolInfo.UseTLS {
		tlsConfig := &tls.Config{
			InsecureSkipVerify: false,
			MinVersion:         tls.VersionTLS12,
		}
		opts.SetTLSConfig(tlsConfig)
		log.Debug().Msgf("mqtt reader: using TLS for %s", broker)
	}

	// Set up connection handlers
	opts.OnConnect = func(client mqtt.Client) {
		log.Info().Msgf("mqtt reader: connected to %s", broker)

		// Subscribe to topic on connect (auto re-subscribes on reconnect)
		// QoS 1 = at-least-once delivery for reliability
		token := client.Subscribe(topic, 1, r.createMessageHandler())
		if token.Wait() && token.Error() != nil {
			log.Error().Err(token.Error()).Msgf("mqtt reader: failed to subscribe to %s", topic)
			scanQueue <- readers.Scan{
				Source: device.ConnectionString(),
				Error:  fmt.Errorf("failed to subscribe to topic: %w", token.Error()),
			}
			return
		}

		log.Info().Msgf("mqtt reader: subscribed to topic %s", topic)
	}

	opts.OnConnectionLost = func(_ mqtt.Client, err error) {
		log.Warn().Err(err).Msg("mqtt reader: connection lost")
	}

	// Create and connect client
	r.client = r.clientFactory(opts)

	token := r.client.Connect()
	if token.Wait() && token.Error() != nil {
		return fmt.Errorf("failed to connect to MQTT broker: %w", token.Error())
	}

	log.Info().Msgf("mqtt reader: opened connection to %s (topic: %s)", broker, topic)
	return nil
}

func (r *Reader) Close() error {
	if r.client != nil && r.client.IsConnected() {
		log.Debug().Msg("mqtt reader: disconnecting")
		r.client.Disconnect(250)
	}
	return nil
}

func (*Reader) Detect(_ []string) string {
	return "" // MQTT doesn't support auto-detection
}

func (r *Reader) Device() string {
	return r.device.ConnectionString()
}

func (r *Reader) Connected() bool {
	return r.client != nil && r.client.IsConnected()
}

func (r *Reader) Info() string {
	return fmt.Sprintf("MQTT: %s", r.topic)
}

func (*Reader) Write(_ string) (*tokens.Token, error) {
	return nil, errors.New("writing not supported on MQTT reader")
}

func (*Reader) CancelWrite() {
	// no-op, writing not supported
}

func (*Reader) Capabilities() []readers.Capability {
	return []readers.Capability{} // No special capabilities
}

func (*Reader) OnMediaChange(*models.ActiveMedia) error {
	return nil // MQTT reader doesn't react to media changes
}

// createMessageHandler returns a MessageHandler that converts MQTT messages to tokens.
func (r *Reader) createMessageHandler() mqtt.MessageHandler {
	return func(_ mqtt.Client, msg mqtt.Message) {
		payload := string(msg.Payload())

		// Ignore empty messages
		if payload == "" {
			log.Debug().Msg("mqtt reader: ignoring empty message")
			return
		}

		log.Debug().Msgf("mqtt reader: received message: %s", payload)

		// Create token with ZapScript content
		token := &tokens.Token{
			Type:     TokenType,
			Text:     payload, // ZapScript content
			ScanTime: time.Now(),
			Source:   r.device.ConnectionString(),
		}

		// Send to scan channel
		r.scanCh <- readers.Scan{
			Source: r.device.ConnectionString(),
			Token:  token,
		}
	}
}
