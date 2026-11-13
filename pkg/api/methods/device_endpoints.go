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
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
)

// DeviceEndpoints says where the network API can be reached.
type DeviceEndpoints struct {
	// Addresses returns the LAN addresses the network API answers on, or
	// nil when it is not reachable from the network.
	Addresses func() []string
	// BLE reports whether the API is being served over Bluetooth LE.
	BLE func() bool
	// Name returns the name Core goes by on the network, the one network
	// discovery publishes.
	Name func() string
	Port int
}

// HandleDeviceEndpoints returns where else a client can reach this Core.
func HandleDeviceEndpoints(endpoints DeviceEndpoints) func(requests.RequestEnv) (any, error) {
	//nolint:gocritic // single-use parameter in API handler
	return func(env requests.RequestEnv) (any, error) {
		if err := requireAuthenticated(&env); err != nil {
			return nil, err
		}
		response := models.DeviceEndpointsResponse{
			Scheme:    "ws",
			Port:      endpoints.Port,
			Addresses: []string{},
		}
		if env.Config != nil {
			response.DeviceID = env.Config.DeviceID()
		}
		if endpoints.Addresses != nil {
			if addresses := endpoints.Addresses(); addresses != nil {
				response.Addresses = addresses
			}
		}
		if endpoints.BLE != nil {
			response.BLE = endpoints.BLE()
		}
		if endpoints.Name != nil {
			response.Name = endpoints.Name()
		}
		return response, nil
	}
}
