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
	"errors"
	"sync"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/permissions"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/hoststatus"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/device"
	"github.com/rs/zerolog/log"
)

// powerAfterWriteFallbackDelay is how long a power action waits for its
// response to be written before going ahead anyway.
const powerAfterWriteFallbackDelay = 5 * time.Second

//nolint:gochecknoglobals // immutable lookup table
var devicePowerMethods = map[hoststatus.PowerAction]string{
	hoststatus.PowerReboot:   models.MethodDevicePowerReboot,
	hoststatus.PowerShutdown: models.MethodDevicePowerShutdown,
	hoststatus.PowerSuspend:  models.MethodDevicePowerSuspend,
}

func deviceMonitor(env *requests.RequestEnv) (*device.Monitor, error) {
	if env.State == nil || env.State.DeviceMonitor() == nil {
		return nil, models.CategorizedErr(
			models.ErrorCategoryUnavailable,
			"device status is not available",
			errors.New("device monitor is not running"),
		)
	}
	return env.State.DeviceMonitor(), nil
}

// HandleDeviceStatus returns everything Core knows about the device it is
// running on, and which power actions this caller may use.
//
//nolint:gocritic // single-use parameter in API handler
func HandleDeviceStatus(env requests.RequestEnv) (any, error) {
	if err := requireAuthenticated(&env); err != nil {
		return nil, err
	}
	monitor, err := deviceMonitor(&env)
	if err != nil {
		return nil, err
	}

	snapshot := monitor.Snapshot(env.Context)
	mayControl := requestGrant(&env).Has(permissions.CapDevicePower)
	actions := map[string]string{}
	for action, availability := range monitor.PowerControl().PowerActions(env.Context) {
		method, known := devicePowerMethods[action]
		if !known || availability == hoststatus.Unsupported {
			continue
		}
		if !mayControl {
			availability = hoststatus.NotPermitted
		}
		actions[method] = string(availability)
	}
	return device.StatusResponse(&snapshot, actions), nil
}

// HandleDevicePower returns the handler for one power action. The action only
// goes ahead once the response has been written, so the caller hears that it
// was accepted before the device goes away.
func HandleDevicePower(action hoststatus.PowerAction) func(requests.RequestEnv) (any, error) {
	//nolint:gocritic // single-use parameter in API handler
	return func(env requests.RequestEnv) (any, error) {
		if err := requireCapability(&env, permissions.CapDevicePower); err != nil {
			return nil, err
		}
		monitor, err := deviceMonitor(&env)
		if err != nil {
			return nil, err
		}
		// A backup or restore cut off part-way leaves the device's data in
		// whatever state it had reached.
		if kind, _, active := env.State.BackupCoordinator().Active(); active {
			return nil, models.CategorizedErr(
				models.ErrorCategoryBusy,
				"a backup or restore is running",
				models.ClientErrf("device is busy with %s", kind),
			)
		}

		commit, err := monitor.PowerControl().PreparePowerAction(env.Context, action)
		switch {
		case errors.Is(err, hoststatus.ErrNotPermitted):
			return nil, models.CategorizedErr(
				models.ErrorCategoryNotPermitted,
				"the operating system does not allow Zaparoo to "+string(action)+" this device",
				err,
			)
		case err != nil:
			return nil, models.CategorizedErr(
				models.ErrorCategoryUnsupported,
				"this device cannot "+string(action),
				err,
			)
		}

		log.Info().Str("action", string(action)).Msg("device power action requested")
		var once sync.Once
		run := func() {
			once.Do(func() {
				// The commit can block until the device comes back, as a
				// suspend does, so it never runs on the response path.
				go func() {
					if commitErr := commit(); commitErr != nil {
						log.Error().Err(commitErr).Str("action", string(action)).Msg("device power action failed")
					}
				}()
			})
		}
		fallback := time.AfterFunc(powerAfterWriteFallbackDelay, run)
		return models.ResponseWithCallback{
			Result: NoContent{},
			AfterWrite: func() {
				fallback.Stop()
				run()
			},
		}, nil
	}
}
