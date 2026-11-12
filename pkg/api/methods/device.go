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
	"bytes"
	"errors"
	"sync"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/permissions"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/validation"
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

// deviceBluetoothActions are the Bluetooth methods device.status lists in
// its actions.
//
//nolint:gochecknoglobals // immutable lookup table
var deviceBluetoothActions = []string{
	models.MethodDeviceBluetoothScanStart,
	models.MethodDeviceBluetoothPair,
	models.MethodDeviceBluetoothForget,
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
	if bluetooth := monitor.Bluetooth(); bluetooth != nil && bluetooth.Available(env.Context) {
		availability := models.DeviceSupported
		if !requestGrant(&env).Has(permissions.CapDeviceBluetooth) {
			availability = models.DeviceNotPermitted
		}
		for _, method := range deviceBluetoothActions {
			actions[method] = availability
		}
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

// deviceBluetooth returns the Bluetooth manager, refusing the request when
// this device has none.
func deviceBluetooth(env *requests.RequestEnv) (*device.Bluetooth, error) {
	monitor, err := deviceMonitor(env)
	if err != nil {
		return nil, err
	}
	bluetooth := monitor.Bluetooth()
	if bluetooth == nil {
		return nil, bluetoothError(hoststatus.ErrUnsupported)
	}
	return bluetooth, nil
}

// bluetoothError turns a Bluetooth failure into the error the client sees.
func bluetoothError(err error) error {
	switch {
	case errors.Is(err, hoststatus.ErrUnsupported):
		return models.CategorizedErr(
			models.ErrorCategoryUnsupported,
			"bluetooth is not supported on this device",
			models.ClientErr(err),
		)
	case errors.Is(err, hoststatus.ErrBluetoothNoAdapter):
		return models.CategorizedErr(
			models.ErrorCategoryUnsupported,
			"this device has no bluetooth adapter",
			models.ClientErr(err),
		)
	case errors.Is(err, hoststatus.ErrBluetoothNotPowered):
		return models.CategorizedErr(
			models.ErrorCategoryUnavailable,
			"bluetooth adapter is powered off",
			models.ClientErr(err),
		)
	case errors.Is(err, hoststatus.ErrBluetoothNotFound):
		return models.ClientErrf("bluetooth device not found")
	case errors.Is(err, device.ErrBluetoothBusy):
		return models.CategorizedErr(
			models.ErrorCategoryBusy,
			"a bluetooth pairing attempt is already running",
			models.ClientErr(err),
		)
	default:
		return models.CategorizedErr(
			models.ErrorCategoryExecutionFailed,
			"the bluetooth request failed",
			err,
		)
	}
}

// bluetoothDeviceID reads and checks the id param of a Bluetooth method.
func bluetoothDeviceID(env *requests.RequestEnv) (string, error) {
	var params models.DeviceBluetoothDeviceParams
	if err := validation.ValidateAndUnmarshal(env.Params, &params); err != nil {
		return "", models.ClientErrf("invalid params: %w", err)
	}
	address, ok := hoststatus.NormalizeBluetoothAddress(params.ID)
	if !ok {
		return "", models.ClientErrf("invalid params: id is not a bluetooth address")
	}
	return address, nil
}

// HandleDeviceBluetoothDevices lists paired Bluetooth devices and the ones
// the current or last scan found. It needs the same capability as
// device.bluetooth.changed, which carries the same names and addresses.
//
//nolint:gocritic // single-use parameter in API handler
func HandleDeviceBluetoothDevices(env requests.RequestEnv) (any, error) {
	if err := requireCapability(&env, permissions.CapDeviceBluetooth); err != nil {
		return nil, err
	}
	bluetooth, err := deviceBluetooth(&env)
	if err != nil {
		return nil, err
	}
	state, err := bluetooth.State(env.Context)
	if err != nil {
		return nil, bluetoothError(err)
	}
	return state, nil
}

// HandleDeviceBluetoothScanStart starts looking for Bluetooth devices. The
// scan stops by itself after its timeout.
//
//nolint:gocritic // single-use parameter in API handler
func HandleDeviceBluetoothScanStart(env requests.RequestEnv) (any, error) {
	var params models.DeviceBluetoothScanParams
	if len(env.Params) > 0 && !bytes.Equal(bytes.TrimSpace(env.Params), []byte("null")) {
		if err := validation.ValidateAndUnmarshal(env.Params, &params); err != nil {
			return nil, models.ClientErrf("invalid params: %w", err)
		}
	}
	if capErr := requireCapability(&env, permissions.CapDeviceBluetooth); capErr != nil {
		return nil, capErr
	}
	bluetooth, err := deviceBluetooth(&env)
	if err != nil {
		return nil, err
	}
	timeout := device.BluetoothScanDefault
	if params.TimeoutSeconds != nil {
		timeout = time.Duration(*params.TimeoutSeconds) * time.Second
	}
	state, err := bluetooth.StartScan(env.Context, params.All, timeout)
	if err != nil {
		return nil, bluetoothError(err)
	}
	return state, nil
}

// HandleDeviceBluetoothScanStop stops looking for Bluetooth devices.
//
//nolint:gocritic // single-use parameter in API handler
func HandleDeviceBluetoothScanStop(env requests.RequestEnv) (any, error) {
	if capErr := requireCapability(&env, permissions.CapDeviceBluetooth); capErr != nil {
		return nil, capErr
	}
	bluetooth, err := deviceBluetooth(&env)
	if err != nil {
		return nil, err
	}
	state, err := bluetooth.StopScan(env.Context)
	if err != nil {
		return nil, bluetoothError(err)
	}
	return state, nil
}

// HandleDeviceBluetoothPair starts pairing with a device and answers at once.
// The outcome arrives through device.bluetooth.changed.
//
//nolint:gocritic // single-use parameter in API handler
func HandleDeviceBluetoothPair(env requests.RequestEnv) (any, error) {
	address, err := bluetoothDeviceID(&env)
	if err != nil {
		return nil, err
	}
	if capErr := requireCapability(&env, permissions.CapDeviceBluetooth); capErr != nil {
		return nil, capErr
	}
	bluetooth, err := deviceBluetooth(&env)
	if err != nil {
		return nil, err
	}
	state, err := bluetooth.Pair(env.Context, address)
	if err != nil {
		return nil, bluetoothError(err)
	}
	return state, nil
}

// HandleDeviceBluetoothForget unpairs a device and removes it.
//
//nolint:gocritic // single-use parameter in API handler
func HandleDeviceBluetoothForget(env requests.RequestEnv) (any, error) {
	address, err := bluetoothDeviceID(&env)
	if err != nil {
		return nil, err
	}
	if capErr := requireCapability(&env, permissions.CapDeviceBluetooth); capErr != nil {
		return nil, capErr
	}
	bluetooth, err := deviceBluetooth(&env)
	if err != nil {
		return nil, err
	}
	state, err := bluetooth.Forget(env.Context, address)
	if err != nil {
		return nil, bluetoothError(err)
	}
	return state, nil
}
