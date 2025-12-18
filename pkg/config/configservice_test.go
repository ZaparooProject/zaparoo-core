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

package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
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
