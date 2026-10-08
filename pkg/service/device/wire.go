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

package device

import (
	"runtime"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/hoststatus"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/power"
)

// optional returns nil for the empty string, which is how the wire format
// says a value is not known.
func optional[T ~string](value T) *string {
	if value == "" {
		return nil
	}
	text := string(value)
	return &text
}

func seconds(duration time.Duration) *int64 {
	if duration <= 0 {
		return nil
	}
	value := int64(duration / time.Second)
	return &value
}

func powerSource(detail *power.Detail) *string {
	switch detail.Source {
	case power.SourceBattery, power.SourceExternal:
		return optional(detail.Source)
	default:
		return nil
	}
}

func controllersWire(snap *Snapshot) *models.DeviceControllers {
	if !snap.ControllersKnown {
		return nil
	}
	items := make([]models.DeviceController, 0, len(snap.Controllers))
	for i := range snap.Controllers {
		controller := &snap.Controllers[i]
		item := models.DeviceController{
			ID:         controller.ID,
			Name:       optional(controller.Name),
			VendorID:   optional(controller.VendorID),
			ProductID:  optional(controller.ProductID),
			Connection: controller.Connection,
		}
		if item.Connection == "" {
			item.Connection = hoststatus.ConnectionUnknown
		}
		if controller.Battery != nil {
			item.Battery = &models.DeviceControllerBattery{
				Percent: controller.Battery.Percent,
				Level:   controller.Battery.Level,
			}
		}
		items = append(items, item)
	}
	return &models.DeviceControllers{Count: len(items), Items: items}
}

func bluetoothWire(snap *Snapshot) *models.DeviceBluetooth {
	if snap.Bluetooth == nil {
		return nil
	}
	return &models.DeviceBluetooth{Present: snap.Bluetooth.Present, Powered: snap.Bluetooth.Powered}
}

// StatusResponse builds the device.status result. actions is keyed by method
// name and already reflects what the caller is allowed to do.
func StatusResponse(snap *Snapshot, actions map[string]string) models.DeviceStatusResponse {
	sections := make(map[string]string, len(snap.Sections))
	for name, availability := range snap.Sections {
		sections[name] = string(availability)
	}
	if actions == nil {
		actions = map[string]string{}
	}
	response := models.DeviceStatusResponse{
		Capabilities: models.DeviceCapabilities{Sections: sections, Actions: actions},
		Bluetooth:    bluetoothWire(snap),
		Controllers:  controllersWire(snap),
		Time: &models.DeviceTime{
			ClockReliable: snap.ClockReliable,
			Timezone:      optional(snap.Timezone),
			UTCOffset:     snap.UTCOffset,
		},
	}

	if detail := snap.Power; detail != nil {
		batteries := make([]models.DeviceBattery, 0, len(detail.Batteries))
		for i := range detail.Batteries {
			battery := &detail.Batteries[i]
			batteries = append(batteries, models.DeviceBattery{
				ID:            battery.ID,
				Percent:       battery.Percent,
				ChargeState:   optional(battery.State),
				TimeRemaining: seconds(battery.TimeRemaining),
			})
		}
		response.Power = &models.DevicePower{
			Present:       detail.Present,
			Percent:       detail.Percent,
			Source:        powerSource(detail),
			ChargeState:   optional(detail.State),
			TimeRemaining: seconds(detail.TimeRemaining),
			Batteries:     batteries,
		}
	}

	if network := snap.Network; network != nil {
		interfaces := make([]models.DeviceInterface, 0, len(network.Interfaces))
		for i := range network.Interfaces {
			iface := &network.Interfaces[i]
			addresses := iface.Addresses
			if addresses == nil {
				addresses = []string{}
			}
			interfaces = append(interfaces, models.DeviceInterface{
				Name:      iface.Name,
				Type:      string(iface.Type),
				Up:        iface.Up,
				Addresses: addresses,
			})
		}
		response.Network = &models.DeviceNetwork{
			Type:       string(network.Type),
			Interface:  optional(network.Interface),
			Internet:   optional(network.Internet),
			Interfaces: interfaces,
		}
	}

	if snap.StorageKnown {
		volumes := make([]models.DeviceVolume, 0, len(snap.Storage))
		for i := range snap.Storage {
			volume := &snap.Storage[i]
			roles := volume.Roles
			if roles == nil {
				roles = []string{}
			}
			volumes = append(volumes, models.DeviceVolume{
				Path:       volume.Path,
				Roles:      roles,
				TotalBytes: volume.Total,
				FreeBytes:  volume.Free,
				UsedBytes:  volume.Used,
			})
		}
		response.Storage = &models.DeviceStorage{Volumes: volumes}
	}

	if display := snap.Display; display != nil {
		response.Display = &models.DeviceDisplay{
			InternalPanel:     display.InternalPanel,
			InternalActive:    display.InternalActive,
			ExternalConnected: display.ExternalConnected,
			ExternalActive:    display.ExternalActive,
			Docked:            display.Docked,
		}
	}

	if system := snap.System; system != nil {
		response.System = &models.DeviceSystem{
			Hostname: system.Hostname,
			Platform: snap.PlatformID,
			OS:       runtime.GOOS,
			Arch:     runtime.GOARCH,
			Model:    optional(system.Model),
		}
	}
	return response
}

// ChangedParams builds the device.changed payload: the state a status display
// draws from, with nothing in it that identifies the device on its network.
func ChangedParams(snap *Snapshot) models.DeviceChangedNotification {
	params := models.DeviceChangedNotification{
		Bluetooth:   bluetoothWire(snap),
		Controllers: controllersWire(snap),
		Time:        &models.DeviceChangedTime{ClockReliable: snap.ClockReliable},
	}
	if detail := snap.Power; detail != nil {
		params.Power = &models.DeviceChangedPower{
			Present:     detail.Present,
			Percent:     detail.Percent,
			Source:      powerSource(detail),
			ChargeState: optional(detail.State),
		}
	}
	if network := snap.Network; network != nil {
		params.Network = &models.DeviceChangedNetwork{
			Type:     string(network.Type),
			Internet: optional(network.Internet),
		}
	}
	if display := snap.Display; display != nil {
		params.Display = &models.DeviceChangedDisplay{Docked: display.Docked}
	}
	return params
}

// localZone names the device's time zone. Go calls the system zone "Local"
// when it was not named through the environment, which is not a name a client
// can use, so that is reported as unknown.
func localZone(now time.Time) (name string, offset int) {
	_, offset = now.Zone()
	name = now.Location().String()
	if name == "Local" {
		name = systemZoneName()
	}
	return name, offset
}
