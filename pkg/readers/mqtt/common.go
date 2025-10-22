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
	"errors"
	"fmt"
	"net/url"
	"strings"
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
