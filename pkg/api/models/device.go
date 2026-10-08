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

package models

// Availability values reported in DeviceCapabilities.
const (
	DeviceUnsupported  = "unsupported"
	DeviceSupported    = "supported"
	DeviceNotPermitted = "notPermitted"
)

// Section names used as keys in DeviceCapabilities.Sections.
const (
	DeviceSectionPower       = "power"
	DeviceSectionNetwork     = "network"
	DeviceSectionBluetooth   = "bluetooth"
	DeviceSectionStorage     = "storage"
	DeviceSectionDisplay     = "display"
	DeviceSectionControllers = "controllers"
	DeviceSectionTime        = "time"
	DeviceSectionSystem      = "system"
)

// DeviceCapabilities says what this device can report and do. Sections is
// keyed by section name and Actions by method name. A key that is absent means
// unsupported, so a client built against a newer Core still reads an older
// one correctly.
type DeviceCapabilities struct {
	Sections map[string]string `json:"sections"`
	Actions  map[string]string `json:"actions"`
}

// DeviceBattery is one battery the device runs on.
type DeviceBattery struct {
	Percent       *int    `json:"percent"`
	ChargeState   *string `json:"chargeState"`
	TimeRemaining *int64  `json:"timeRemaining,omitempty"`
	ID            string  `json:"id"`
}

// DevicePower is the device's battery state. Percent is the lowest readable
// battery and is reported whether or not the device is charging.
type DevicePower struct {
	Percent       *int            `json:"percent"`
	Source        *string         `json:"source"`
	ChargeState   *string         `json:"chargeState"`
	TimeRemaining *int64          `json:"timeRemaining,omitempty"`
	Batteries     []DeviceBattery `json:"batteries"`
	Present       bool            `json:"present"`
}

type DeviceInterface struct {
	Name      string   `json:"name"`
	Type      string   `json:"type"`
	Addresses []string `json:"addresses"`
	Up        bool     `json:"up"`
}

// DeviceNetwork is the device's links. Type and Interface describe the link
// carrying the default route.
type DeviceNetwork struct {
	Interface  *string           `json:"interface"`
	Internet   *string           `json:"internet"`
	Type       string            `json:"type"`
	Interfaces []DeviceInterface `json:"interfaces"`
}

type DeviceBluetooth struct {
	Powered *bool `json:"powered"`
	Present bool  `json:"present"`
}

type DeviceVolume struct {
	Path       string   `json:"path"`
	Roles      []string `json:"roles"`
	TotalBytes uint64   `json:"totalBytes"`
	FreeBytes  uint64   `json:"freeBytes"`
	UsedBytes  uint64   `json:"usedBytes"`
}

type DeviceStorage struct {
	Volumes []DeviceVolume `json:"volumes"`
}

type DeviceDisplay struct {
	Docked            *bool `json:"docked"`
	InternalPanel     bool  `json:"internalPanel"`
	InternalActive    bool  `json:"internalActive"`
	ExternalConnected bool  `json:"externalConnected"`
	ExternalActive    bool  `json:"externalActive"`
}

type DeviceControllerBattery struct {
	Percent *int   `json:"percent"`
	Level   string `json:"level"`
}

type DeviceController struct {
	Name       *string                  `json:"name"`
	VendorID   *string                  `json:"vendorId"`
	ProductID  *string                  `json:"productId"`
	Battery    *DeviceControllerBattery `json:"battery"`
	ID         string                   `json:"id"`
	Connection string                   `json:"connection"`
}

type DeviceControllers struct {
	Items []DeviceController `json:"items"`
	Count int                `json:"count"`
}

type DeviceTime struct {
	Timezone      *string `json:"timezone"`
	UTCOffset     int     `json:"utcOffset"`
	ClockReliable bool    `json:"clockReliable"`
}

type DeviceSystem struct {
	Model    *string `json:"model"`
	Hostname string  `json:"hostname"`
	Platform string  `json:"platform"`
	OS       string  `json:"os"`
	Arch     string  `json:"arch"`
}

// DeviceStatusResponse is the result of device.status. Every section is
// always present; one that is null is either unsupported on this device or
// has no reading yet, and Capabilities.Sections says which.
type DeviceStatusResponse struct {
	Power        *DevicePower       `json:"power"`
	Network      *DeviceNetwork     `json:"network"`
	Bluetooth    *DeviceBluetooth   `json:"bluetooth"`
	Storage      *DeviceStorage     `json:"storage"`
	Display      *DeviceDisplay     `json:"display"`
	Controllers  *DeviceControllers `json:"controllers"`
	Time         *DeviceTime        `json:"time"`
	System       *DeviceSystem      `json:"system"`
	Capabilities DeviceCapabilities `json:"capabilities"`
}

type DeviceChangedPower struct {
	Percent     *int    `json:"percent"`
	Source      *string `json:"source"`
	ChargeState *string `json:"chargeState"`
	Present     bool    `json:"present"`
}

type DeviceChangedNetwork struct {
	Internet *string `json:"internet"`
	Type     string  `json:"type"`
}

type DeviceChangedDisplay struct {
	Docked *bool `json:"docked"`
}

type DeviceChangedTime struct {
	ClockReliable bool `json:"clockReliable"`
}

// DeviceChangedNotification is the payload of device.changed. It is the whole
// of the state it covers, never a difference, and it leaves out everything
// that identifies the device on its network: notifications reach every
// connected client, including ones on an unencrypted connection.
type DeviceChangedNotification struct {
	Power       *DeviceChangedPower   `json:"power"`
	Network     *DeviceChangedNetwork `json:"network"`
	Bluetooth   *DeviceBluetooth      `json:"bluetooth"`
	Display     *DeviceChangedDisplay `json:"display"`
	Controllers *DeviceControllers    `json:"controllers"`
	Time        *DeviceChangedTime    `json:"time"`
}
