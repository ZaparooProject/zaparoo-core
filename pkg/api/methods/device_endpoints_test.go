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

package methods

import (
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/permissions"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleDeviceEndpoints(t *testing.T) {
	t.Parallel()

	cfg, err := testhelpers.NewTestConfig(nil, t.TempDir())
	require.NoError(t, err)
	require.NotEmpty(t, cfg.DeviceID())

	tests := []struct {
		name       string
		clientRole string
		endpoints  DeviceEndpoints
		want       models.DeviceEndpointsResponse
		isLocal    bool
		forbidden  bool
	}{
		{
			name:       "paired member sees the network and Bluetooth endpoints",
			clientRole: string(permissions.RoleMember),
			endpoints: DeviceEndpoints{
				Addresses: func() []string { return []string{"192.168.1.20", "10.0.0.5"} },
				BLE:       func() bool { return true },
				Name:      func() string { return "MiSTer" },
				Port:      7497,
			},
			want: models.DeviceEndpointsResponse{
				DeviceID:  cfg.DeviceID(),
				Name:      "MiSTer",
				Scheme:    "ws",
				Addresses: []string{"192.168.1.20", "10.0.0.5"},
				Port:      7497,
				BLE:       true,
			},
		},
		{
			name:    "local request is allowed",
			isLocal: true,
			endpoints: DeviceEndpoints{
				Addresses: func() []string { return nil },
				Port:      7497,
			},
			want: models.DeviceEndpointsResponse{
				DeviceID:  cfg.DeviceID(),
				Scheme:    "ws",
				Addresses: []string{},
				Port:      7497,
			},
		},
		{
			name:       "missing providers answer empty",
			clientRole: string(permissions.RoleAdmin),
			want: models.DeviceEndpointsResponse{
				DeviceID:  cfg.DeviceID(),
				Scheme:    "ws",
				Addresses: []string{},
			},
		},
		{name: "unpaired remote is refused", forbidden: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			env := requests.RequestEnv{
				Context:    t.Context(),
				Config:     cfg,
				IsLocal:    tt.isLocal,
				ClientRole: tt.clientRole,
			}
			result, err := HandleDeviceEndpoints(tt.endpoints)(env)
			if tt.forbidden {
				require.ErrorIs(t, err, ErrForbidden)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, result)
		})
	}
}

func TestHandleDeviceEndpoints_NoConfig(t *testing.T) {
	t.Parallel()

	result, err := HandleDeviceEndpoints(DeviceEndpoints{Port: 1})(requests.RequestEnv{
		Context: t.Context(),
		IsLocal: true,
	})
	require.NoError(t, err)
	response, ok := result.(models.DeviceEndpointsResponse)
	require.True(t, ok)
	assert.Empty(t, response.DeviceID)
}
