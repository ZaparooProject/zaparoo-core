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

package mqtt

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseMQTTPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		path        string
		wantBroker  string
		wantTopic   string
		errContains string
		wantErr     bool
	}{
		{
			name:       "valid path with port",
			path:       "localhost:1883/zaparoo/tokens",
			wantBroker: "localhost:1883",
			wantTopic:  "zaparoo/tokens",
			wantErr:    false,
		},
		{
			name:       "valid path with domain",
			path:       "mqtt.example.com:8883/home/zaparoo",
			wantBroker: "mqtt.example.com:8883",
			wantTopic:  "home/zaparoo",
			wantErr:    false,
		},
		{
			name:       "valid path with single-level topic",
			path:       "broker.local:1883/topic",
			wantBroker: "broker.local:1883",
			wantTopic:  "topic",
			wantErr:    false,
		},
		{
			name:       "valid path with multi-level topic",
			path:       "test.broker:1883/level1/level2/level3",
			wantBroker: "test.broker:1883",
			wantTopic:  "level1/level2/level3",
			wantErr:    false,
		},
		{
			name:       "valid path with mqtt:// scheme",
			path:       "mqtt://broker.example.com:1883/test/topic",
			wantBroker: "broker.example.com:1883",
			wantTopic:  "test/topic",
			wantErr:    false,
		},
		{
			name:       "valid path with mqtts:// scheme",
			path:       "mqtts://secure.broker.com:8883/secure/topic",
			wantBroker: "secure.broker.com:8883",
			wantTopic:  "secure/topic",
			wantErr:    false,
		},
		{
			name:       "valid IP address with port",
			path:       "192.168.1.100:1883/zaparoo",
			wantBroker: "192.168.1.100:1883",
			wantTopic:  "zaparoo",
			wantErr:    false,
		},
		{
			name:        "empty path",
			path:        "",
			wantErr:     true,
			errContains: "path cannot be empty",
		},
		{
			name:        "missing topic",
			path:        "localhost:1883",
			wantErr:     true,
			errContains: "topic is required",
		},
		{
			name:        "missing topic with trailing slash",
			path:        "localhost:1883/",
			wantErr:     true,
			errContains: "topic is required",
		},
		{
			name:        "missing broker",
			path:        "/topic/only",
			wantErr:     true,
			errContains: "broker address",
		},
		{
			name:        "invalid format - no separator",
			path:        "invalidpath",
			wantErr:     true,
			errContains: "topic is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			broker, topic, err := ParseMQTTPath(tt.path)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
				assert.Empty(t, broker)
				assert.Empty(t, topic)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantBroker, broker)
				assert.Equal(t, tt.wantTopic, topic)
			}
		})
	}
}

func TestParseMQTTProtocol(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		urlStr        string
		wantProtocol  string
		wantScheme    string
		wantRemainder string
		wantUseTLS    bool
	}{
		{
			name:          "mqtts scheme",
			urlStr:        "mqtts://broker:8883/topic",
			wantProtocol:  "ssl",
			wantUseTLS:    true,
			wantScheme:    "mqtts",
			wantRemainder: "broker:8883/topic",
		},
		{
			name:          "ssl scheme",
			urlStr:        "ssl://broker:8883",
			wantProtocol:  "ssl",
			wantUseTLS:    true,
			wantScheme:    "ssl",
			wantRemainder: "broker:8883",
		},
		{
			name:          "mqtt scheme",
			urlStr:        "mqtt://broker:1883/topic",
			wantProtocol:  "tcp",
			wantUseTLS:    false,
			wantScheme:    "mqtt",
			wantRemainder: "broker:1883/topic",
		},
		{
			name:          "no scheme",
			urlStr:        "broker:1883/topic",
			wantProtocol:  "tcp",
			wantUseTLS:    false,
			wantScheme:    "",
			wantRemainder: "broker:1883/topic",
		},
		{
			name:          "no scheme simple",
			urlStr:        "localhost:1883",
			wantProtocol:  "tcp",
			wantUseTLS:    false,
			wantScheme:    "",
			wantRemainder: "localhost:1883",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			info := ParseMQTTProtocol(tt.urlStr)

			assert.Equal(t, tt.wantProtocol, info.Protocol)
			assert.Equal(t, tt.wantUseTLS, info.UseTLS)
			assert.Equal(t, tt.wantScheme, info.Scheme)
			assert.Equal(t, tt.wantRemainder, info.Remainder)
		})
	}
}
