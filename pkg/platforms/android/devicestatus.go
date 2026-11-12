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

package android

import (
	"context"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/hoststatus"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/power"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
)

// DeviceStatus is the embedding host's report of the device's state. A nil
// section is one the host does not report, and reads as unsupported.
type DeviceStatus struct {
	Power     *power.Detail
	Network   *hoststatus.Network
	Bluetooth *hoststatus.Bluetooth
	Display   *hoststatus.Display
	System    *hoststatus.System
	// Storage and Controllers are nil when the host does not report them; an
	// empty, non-nil slice is a report that there are none.
	Storage     []hoststatus.Volume
	Controllers []hoststatus.Controller
}

var (
	_ platforms.DevicePowerProvider       = (*Platform)(nil)
	_ platforms.DeviceNetworkProvider     = (*Platform)(nil)
	_ platforms.DeviceBluetoothProvider   = (*Platform)(nil)
	_ platforms.DeviceStorageProvider     = (*Platform)(nil)
	_ platforms.DeviceDisplayProvider     = (*Platform)(nil)
	_ platforms.DeviceControllersProvider = (*Platform)(nil)
	_ platforms.DeviceSystemProvider      = (*Platform)(nil)
	_ platforms.PowerControlProvider      = (*Platform)(nil)
	_ platforms.PowerStatusProvider       = (*Platform)(nil)
	_ platforms.DeviceStatusPusher        = (*Platform)(nil)
)

// SetDeviceStatus records the device state the embedding host observed. The
// host calls it whenever something changes; Core never looks for itself here,
// because only the host is allowed to ask the framework.
//
// The network section's reachability is taken as the framework's own answer,
// so Core does not probe for it.
func (p *Platform) SetDeviceStatus(report *DeviceStatus) {
	var status DeviceStatus
	if report != nil {
		status = *report
	}
	if status.Network != nil {
		network := *status.Network
		network.InternetAuthoritative = true
		status.Network = &network
	}

	p.deviceMu.Lock()
	p.deviceStatus = &status
	onChange := p.deviceChanged
	p.deviceMu.Unlock()

	if onChange != nil {
		onChange()
	}
}

// SetDeviceStatusChanged registers the callback fired after each
// SetDeviceStatus.
func (p *Platform) SetDeviceStatusChanged(onChange func()) {
	p.deviceMu.Lock()
	defer p.deviceMu.Unlock()
	p.deviceChanged = onChange
}

func (p *Platform) currentDeviceStatus() DeviceStatus {
	p.deviceMu.RLock()
	defer p.deviceMu.RUnlock()
	if p.deviceStatus == nil {
		return DeviceStatus{}
	}
	return *p.deviceStatus
}

func (p *Platform) DevicePower() (power.Detail, error) {
	status := p.currentDeviceStatus()
	if status.Power == nil {
		return power.Detail{}, hoststatus.ErrUnsupported
	}
	return *status.Power, nil
}

func (p *Platform) DeviceNetwork() (hoststatus.Network, error) {
	status := p.currentDeviceStatus()
	if status.Network == nil {
		return hoststatus.Network{}, hoststatus.ErrUnsupported
	}
	return *status.Network, nil
}

func (p *Platform) DeviceBluetooth() (hoststatus.Bluetooth, error) {
	status := p.currentDeviceStatus()
	if status.Bluetooth == nil {
		return hoststatus.Bluetooth{}, hoststatus.ErrUnsupported
	}
	return *status.Bluetooth, nil
}

func (p *Platform) DeviceStorage() ([]hoststatus.Volume, error) {
	status := p.currentDeviceStatus()
	if status.Storage == nil {
		return nil, hoststatus.ErrUnsupported
	}
	return status.Storage, nil
}

func (p *Platform) DeviceDisplay() (hoststatus.Display, error) {
	status := p.currentDeviceStatus()
	if status.Display == nil {
		return hoststatus.Display{}, hoststatus.ErrUnsupported
	}
	return *status.Display, nil
}

func (p *Platform) DeviceControllers() ([]hoststatus.Controller, error) {
	status := p.currentDeviceStatus()
	if status.Controllers == nil {
		return nil, hoststatus.ErrUnsupported
	}
	return status.Controllers, nil
}

func (p *Platform) DeviceSystem() (hoststatus.System, error) {
	status := p.currentDeviceStatus()
	if status.System == nil {
		return hoststatus.System{}, hoststatus.ErrUnsupported
	}
	return *status.System, nil
}

// PowerStatus answers the update gate from the host's report. Until the host
// has reported, the charge is unknown, which the gate treats as unsafe.
func (p *Platform) PowerStatus() (power.Status, error) {
	status := p.currentDeviceStatus()
	if status.Power == nil {
		return power.Status{Source: power.SourceUnknown}, nil
	}
	detail := status.Power
	switch {
	case !detail.Present:
		return power.Status{Source: power.SourceNoBattery}, nil
	case detail.Source == power.SourceExternal:
		return power.Status{Source: power.SourceExternal}, nil
	case detail.Source == power.SourceBattery && detail.Percent != nil:
		return power.Status{Source: power.SourceBattery, Percent: *detail.Percent}, nil
	default:
		return power.Status{Source: power.SourceUnknown}, nil
	}
}

// PowerActions reports none: an application cannot reboot, shut down or
// suspend an Android device.
func (*Platform) PowerActions(context.Context) map[hoststatus.PowerAction]hoststatus.Availability {
	return map[hoststatus.PowerAction]hoststatus.Availability{}
}

func (*Platform) PreparePowerAction(context.Context, hoststatus.PowerAction) (func() error, error) {
	return nil, hoststatus.ErrUnsupported
}
