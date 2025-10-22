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
	"net/url"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

// ParseMQTTPath parses an MQTT connection path in the format "broker:port/topic"
// and returns the broker address and topic separately.
//
// Examples:
//   - "localhost:1883/zaparoo/tokens" -> ("localhost:1883", "zaparoo/tokens")
//   - "mqtt.example.com:8883/home/zaparoo" -> ("mqtt.example.com:8883", "home/zaparoo")
func ParseMQTTPath(path string) (broker, topic string, err error) {
	if path == "" {
		return "", "", errors.New("path cannot be empty")
	}

	// Add mqtt:// scheme if not present for URL parsing
	urlStr := path
	if !strings.HasPrefix(path, "mqtt://") && !strings.HasPrefix(path, "mqtts://") {
		urlStr = "mqtt://" + path
	}

	u, err := url.Parse(urlStr)
	if err != nil {
		return "", "", fmt.Errorf("failed to parse MQTT URL: %w", err)
	}

	if u.Host == "" {
		return "", "", errors.New("broker address (host:port) is required")
	}

	broker = u.Host

	// Extract topic from path, removing leading slash
	topic = strings.TrimLeft(u.Path, "/")
	if topic == "" {
		return "", "", errors.New("topic is required")
	}

	return broker, topic, nil
}

// MQTTProtocolInfo contains parsed MQTT protocol information.
type MQTTProtocolInfo struct {
	Protocol  string
	Scheme    string
	Remainder string
	UseTLS    bool
}

// ParseMQTTProtocol extracts protocol information from an MQTT URL string.
//
// Examples:
//   - "mqtts://broker:8883" -> {Protocol: "ssl", UseTLS: true, Scheme: "mqtts", Remainder: "broker:8883"}
//   - "ssl://broker:8883" -> {Protocol: "ssl", UseTLS: true, Scheme: "ssl", Remainder: "broker:8883"}
//   - "mqtt://broker:1883" -> {Protocol: "tcp", UseTLS: false, Scheme: "mqtt", Remainder: "broker:1883"}
//   - "broker:1883" -> {Protocol: "tcp", UseTLS: false, Scheme: "", Remainder: "broker:1883"}
func ParseMQTTProtocol(urlStr string) MQTTProtocolInfo {
	info := MQTTProtocolInfo{
		Protocol:  "tcp",
		UseTLS:    false,
		Scheme:    "",
		Remainder: urlStr,
	}

	if strings.Contains(urlStr, "://") {
		parts := strings.SplitN(urlStr, "://", 2)
		info.Scheme = parts[0]
		info.Remainder = parts[1]

		if info.Scheme == "mqtts" || info.Scheme == "ssl" {
			info.Protocol = "ssl"
			info.UseTLS = true
		}
	}

	return info
}

// NewClientOptions creates and configures MQTT client options based on a broker URL.
// The clientIDPrefix is used to generate a unique client ID (e.g., "zaparoo-mqtt-" or "zaparoo-publisher-").
func NewClientOptions(brokerURL, clientIDPrefix string) *mqtt.ClientOptions {
	protocolInfo := ParseMQTTProtocol(brokerURL)
	fullBrokerURL := fmt.Sprintf("%s://%s", protocolInfo.Protocol, protocolInfo.Remainder)

	opts := mqtt.NewClientOptions()
	opts.AddBroker(fullBrokerURL)
	opts.SetClientID(clientIDPrefix + uuid.New().String()[:8])
	opts.SetAutoReconnect(true) // Auto-reconnect if connection is lost after initial success
	opts.SetConnectRetry(false) // Disable background retry on initial connect - caller handles timeout
	opts.SetConnectTimeout(10 * time.Second)
	opts.SetOrderMatters(false) // Allow blocking in message handlers

	// Look up authentication credentials from auth.toml
	creds := config.LookupAuth(config.GetAuthCfg(), brokerURL)
	if creds != nil && creds.Username != "" {
		opts.SetUsername(creds.Username)
		opts.SetPassword(creds.Password)
		log.Debug().Msgf("mqtt: using authentication for %s", protocolInfo.Remainder)
	}

	// Configure TLS if using secure connection
	if protocolInfo.UseTLS {
		tlsConfig := &tls.Config{
			InsecureSkipVerify: false,
			MinVersion:         tls.VersionTLS12,
		}
		opts.SetTLSConfig(tlsConfig)
		log.Debug().Msgf("mqtt: using TLS for %s", protocolInfo.Remainder)
	}

	return opts
}
