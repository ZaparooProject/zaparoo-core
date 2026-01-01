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

package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiscoveryEnabled(t *testing.T) {
	t.Parallel()

	tests := []struct {
		enabled *bool
		name    string
		want    bool
	}{
		{
			name:    "nil returns true (default enabled)",
			enabled: nil,
			want:    true,
		},
		{
			name:    "true returns true",
			enabled: boolPtr(true),
			want:    true,
		},
		{
			name:    "false returns false",
			enabled: boolPtr(false),
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			inst := &Instance{
				vals: Values{
					Service: Service{
						Discovery: Discovery{
							Enabled: tt.enabled,
						},
					},
				},
			}

			got := inst.DiscoveryEnabled()
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestDiscoveryInstanceName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		instanceName string
		name         string
		want         string
	}{
		{
			name:         "empty string returns empty",
			instanceName: "",
			want:         "",
		},
		{
			name:         "custom name is returned",
			instanceName: "Living Room MiSTer",
			want:         "Living Room MiSTer",
		},
		{
			name:         "simple hostname",
			instanceName: "my-device",
			want:         "my-device",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			inst := &Instance{
				vals: Values{
					Service: Service{
						Discovery: Discovery{
							InstanceName: tt.instanceName,
						},
					},
				},
			}

			got := inst.DiscoveryInstanceName()
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestDeviceID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		deviceID string
		name     string
		want     string
	}{
		{
			name:     "empty string returns empty",
			deviceID: "",
			want:     "",
		},
		{
			name:     "uuid is returned",
			deviceID: "550e8400-e29b-41d4-a716-446655440000",
			want:     "550e8400-e29b-41d4-a716-446655440000",
		},
		{
			name:     "short id is returned",
			deviceID: "abc123",
			want:     "abc123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			inst := &Instance{
				vals: Values{
					Service: Service{
						DeviceID: tt.deviceID,
					},
				},
			}

			got := inst.DeviceID()
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestIsValidAPIPort(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		port int
		want bool
	}{
		{"min valid", MinAPIPort, true},
		{"max valid", MaxAPIPort, true},
		{"typical high port", 8080, true},
		{"default port", DefaultAPIPort, true},
		{"below min", MinAPIPort - 1, false},
		{"privileged port 80", 80, false},
		{"privileged port 443", 443, false},
		{"zero", 0, false},
		{"negative", -1, false},
		{"above max", MaxAPIPort + 1, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := isValidAPIPort(tt.port)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestAPIPort_Validation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		apiPort *int
		name    string
		want    int
	}{
		{
			name:    "nil returns default",
			apiPort: nil,
			want:    DefaultAPIPort,
		},
		{
			name:    "valid port 8080",
			apiPort: intPtr(8080),
			want:    8080,
		},
		{
			name:    "minimum valid port",
			apiPort: intPtr(MinAPIPort),
			want:    MinAPIPort,
		},
		{
			name:    "maximum valid port",
			apiPort: intPtr(MaxAPIPort),
			want:    MaxAPIPort,
		},
		{
			name:    "privileged port 80 returns default",
			apiPort: intPtr(80),
			want:    DefaultAPIPort,
		},
		{
			name:    "privileged port 443 returns default",
			apiPort: intPtr(443),
			want:    DefaultAPIPort,
		},
		{
			name:    "negative port returns default",
			apiPort: intPtr(-1),
			want:    DefaultAPIPort,
		},
		{
			name:    "port over max returns default",
			apiPort: intPtr(MaxAPIPort + 1),
			want:    DefaultAPIPort,
		},
		{
			name:    "zero port returns default",
			apiPort: intPtr(0),
			want:    DefaultAPIPort,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			inst := &Instance{
				vals: Values{
					Service: Service{
						APIPort: tt.apiPort,
					},
				},
			}

			got := inst.APIPort()
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestSetAPIPort_Validation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		port      int
		wantErr   bool
		wantValue int
	}{
		{
			name:      "valid port 8080",
			port:      8080,
			wantErr:   false,
			wantValue: 8080,
		},
		{
			name:      "minimum valid port",
			port:      MinAPIPort,
			wantErr:   false,
			wantValue: MinAPIPort,
		},
		{
			name:      "maximum valid port",
			port:      MaxAPIPort,
			wantErr:   false,
			wantValue: MaxAPIPort,
		},
		{
			name:    "privileged port 80 rejected",
			port:    80,
			wantErr: true,
		},
		{
			name:    "negative port rejected",
			port:    -1,
			wantErr: true,
		},
		{
			name:    "port over max rejected",
			port:    MaxAPIPort + 1,
			wantErr: true,
		},
		{
			name:    "zero port rejected",
			port:    0,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			inst := &Instance{
				vals: Values{},
			}

			err := inst.SetAPIPort(tt.port)

			if tt.wantErr {
				require.Error(t, err)
				assert.Nil(t, inst.vals.Service.APIPort)
			} else {
				require.NoError(t, err)
				require.NotNil(t, inst.vals.Service.APIPort)
				assert.Equal(t, tt.wantValue, *inst.vals.Service.APIPort)
			}
		})
	}
}

func TestAPIListen(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		apiListen string
		apiPort   *int
		want      string
	}{
		{
			name:      "empty uses all interfaces with default port",
			apiListen: "",
			apiPort:   nil,
			want:      ":7497",
		},
		{
			name:      "empty with custom port",
			apiListen: "",
			apiPort:   intPtr(8080),
			want:      ":8080",
		},
		{
			name:      "IPv4 host combines with default port",
			apiListen: "127.0.0.1",
			apiPort:   nil,
			want:      "127.0.0.1:7497",
		},
		{
			name:      "IPv4 host combines with custom port",
			apiListen: "127.0.0.1",
			apiPort:   intPtr(9000),
			want:      "127.0.0.1:9000",
		},
		{
			name:      "IPv6 host combines with port",
			apiListen: "::1",
			apiPort:   intPtr(8080),
			want:      "[::1]:8080",
		},
		{
			name:      "bind all IPv4 interfaces",
			apiListen: "0.0.0.0",
			apiPort:   intPtr(9000),
			want:      "0.0.0.0:9000",
		},
		{
			name:      "bind all IPv6 interfaces",
			apiListen: "::",
			apiPort:   intPtr(9000),
			want:      "[::]:9000",
		},
		{
			name:      "localhost hostname",
			apiListen: "localhost",
			apiPort:   nil,
			want:      "localhost:7497",
		},
		{
			name:      "localhost with custom port",
			apiListen: "localhost",
			apiPort:   intPtr(9000),
			want:      "localhost:9000",
		},
		{
			name:      "custom hostname",
			apiListen: "myserver.local",
			apiPort:   intPtr(8080),
			want:      "myserver.local:8080",
		},
		{
			name:      "host:port format is rejected, falls back",
			apiListen: "127.0.0.1:9000",
			apiPort:   nil,
			want:      ":7497",
		},
		{
			name:      "port only format is rejected, falls back",
			apiListen: ":8888",
			apiPort:   nil,
			want:      ":7497",
		},
		{
			name:      "host:port with custom api_port falls back using custom port",
			apiListen: "127.0.0.1:9000",
			apiPort:   intPtr(8080),
			want:      ":8080",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			inst := &Instance{
				vals: Values{
					Service: Service{
						APIListen: tt.apiListen,
						APIPort:   tt.apiPort,
					},
				},
			}

			got := inst.APIListen()
			assert.Equal(t, tt.want, got)
		})
	}
}
