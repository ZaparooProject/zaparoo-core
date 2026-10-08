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

package platforms

import (
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/hoststatus"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/power"
)

// The Device*Provider interfaces are optionally implemented by platforms that
// know something about the machine the generic reader for their operating
// system does not: a battery behind a bus the kernel has no driver for, or a
// host application that is the only thing allowed to ask. Core discovers them
// by type assertion and uses the generic reader for every section a platform
// leaves unimplemented.
//
// A provider returns hoststatus.ErrUnsupported to say the device has no such
// section at all.

// DevicePowerProvider reports the batteries the device runs on.
type DevicePowerProvider interface {
	DevicePower() (power.Detail, error)
}

// DeviceNetworkProvider reports the device's network links.
type DeviceNetworkProvider interface {
	DeviceNetwork() (hoststatus.Network, error)
}

// DeviceBluetoothProvider reports the Bluetooth adapter's state.
type DeviceBluetoothProvider interface {
	DeviceBluetooth() (hoststatus.Bluetooth, error)
}

// DeviceStorageProvider reports the volumes holding the device's media and
// data.
type DeviceStorageProvider interface {
	DeviceStorage() ([]hoststatus.Volume, error)
}

// DeviceDisplayProvider reports what the device is showing its picture on.
type DeviceDisplayProvider interface {
	DeviceDisplay() (hoststatus.Display, error)
}

// DeviceControllersProvider reports the connected game controllers.
type DeviceControllersProvider interface {
	DeviceControllers() ([]hoststatus.Controller, error)
}

// DeviceSystemProvider reports what the device is.
type DeviceSystemProvider interface {
	DeviceSystem() (hoststatus.System, error)
}

// PowerControlProvider is implemented by platforms that reboot, shut down or
// suspend the device their own way.
type PowerControlProvider interface {
	hoststatus.PowerController
}

// DeviceStatusPusher is implemented by platforms that are told about device
// state changes instead of having to look for them. Core registers a callback
// and re-reads the providers whenever it fires. The callback must not be
// invoked with any lock held that a provider method takes.
type DeviceStatusPusher interface {
	SetDeviceStatusChanged(onChange func())
}

// ResolveDeviceReaders returns the reader for each section of device status,
// preferring the platform's own where it has one.
func ResolveDeviceReaders(pl Platform, defaults hoststatus.Readers) hoststatus.Readers {
	readers := defaults
	if provider, ok := pl.(DevicePowerProvider); ok {
		readers.Power = provider.DevicePower
	}
	if provider, ok := pl.(DeviceNetworkProvider); ok {
		readers.Network = provider.DeviceNetwork
	}
	if provider, ok := pl.(DeviceBluetoothProvider); ok {
		readers.Bluetooth = provider.DeviceBluetooth
	}
	if provider, ok := pl.(DeviceStorageProvider); ok {
		readers.Storage = func([]hoststatus.StorageRoot) ([]hoststatus.Volume, error) {
			return provider.DeviceStorage()
		}
	}
	if provider, ok := pl.(DeviceDisplayProvider); ok {
		readers.Display = provider.DeviceDisplay
	}
	if provider, ok := pl.(DeviceControllersProvider); ok {
		readers.Controllers = provider.DeviceControllers
	}
	if provider, ok := pl.(DeviceSystemProvider); ok {
		readers.System = provider.DeviceSystem
	}
	if provider, ok := pl.(PowerControlProvider); ok {
		readers.PowerControl = provider
	}
	if readers.PowerControl == nil {
		readers.PowerControl = hoststatus.NoPowerControl{}
	}
	return readers
}
