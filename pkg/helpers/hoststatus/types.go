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

// Package hoststatus reads the state of the machine Core runs on: its network
// links, Bluetooth adapter, storage, displays and controllers, and asks the
// operating system to reboot, shut down or suspend it.
//
// Every reader here is the generic one for its operating system. A platform
// that knows better supplies its own through the provider interfaces in the
// platforms package.
package hoststatus

import (
	"context"
	"errors"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/power"
)

var (
	// ErrUnsupported means the platform has no way to answer at all.
	ErrUnsupported = errors.New("not supported on this platform")
	// ErrNotPermitted means the platform could act but the operating system
	// refuses this process.
	ErrNotPermitted = errors.New("not permitted by the operating system")
)

// Availability is whether a section or an action can be used on this device.
type Availability string

const (
	Unsupported  Availability = "unsupported"
	Supported    Availability = "supported"
	NotPermitted Availability = "notPermitted"
)

// LinkType is the kind of network link an interface is.
type LinkType string

const (
	LinkWifi  LinkType = "wifi"
	LinkWired LinkType = "wired"
	LinkOther LinkType = "other"
	// LinkNone is the device-level answer when no interface carries a
	// default route.
	LinkNone LinkType = "none"
)

// InternetState is how far beyond the local network the device can reach. The
// empty value means it has not been determined.
type InternetState string

const (
	InternetFull InternetState = "full"
	// InternetPortal means something answered in place of the internet, as a
	// hotel or cafe sign-in page does.
	InternetPortal InternetState = "portal"
	InternetNone   InternetState = "none"
)

// Interface is one network link.
type Interface struct {
	Name string
	Type LinkType
	// Addresses excludes loopback and link-local addresses.
	Addresses []string
	Up        bool
}

// Network is the device's links and which of them carries its traffic.
type Network struct {
	// Type and Interface describe the link holding the default route.
	Type      LinkType
	Interface string
	// Internet is filled in by the reader only when InternetAuthoritative is
	// set; otherwise the caller decides it by probing.
	Internet   InternetState
	Interfaces []Interface
	// InternetAuthoritative means the operating system or the embedding host
	// already checks reachability and Internet is its answer.
	InternetAuthoritative bool
}

// Bluetooth is the adapter's state. Core only ever reads it.
type Bluetooth struct {
	// Powered is nil when the adapter's power state cannot be read.
	Powered *bool
	Present bool
}

const (
	RoleMedia = "media"
	RoleData  = "data"
)

// StorageRoot is a directory whose filesystem should be reported.
type StorageRoot struct {
	Path string
	Role string
}

// Volume is one filesystem holding at least one storage root.
type Volume struct {
	// Path is where the filesystem is mounted, or the first root found on it
	// when that is not known.
	Path  string
	Roles []string
	Total uint64
	// Free is the space available to the user Core runs as.
	Free uint64
	Used uint64
}

// Display is what the device is showing its picture on.
type Display struct {
	// Docked is nil on a device with no built-in panel, where the question
	// has no meaning.
	Docked            *bool
	InternalPanel     bool
	InternalActive    bool
	ExternalConnected bool
	ExternalActive    bool
}

const (
	ConnectionUSB       = "usb"
	ConnectionBluetooth = "bluetooth"
	ConnectionUnknown   = "unknown"
)

const (
	BatteryLevelEmpty  = "empty"
	BatteryLevelLow    = "low"
	BatteryLevelMedium = "medium"
	BatteryLevelFull   = "full"
)

// ControllerBattery is a controller's own battery.
type ControllerBattery struct {
	// Percent is nil when the hardware reports only a coarse level.
	Percent *int
	Level   string
}

// Controller is one physical game controller.
type Controller struct {
	Battery *ControllerBattery
	// ID identifies the controller only for as long as it stays connected.
	ID   string
	Name string
	// VendorID and ProductID are four lowercase hex digits, or empty.
	VendorID   string
	ProductID  string
	Connection string
}

// System is what the device is.
type System struct {
	Hostname string
	Model    string
}

// PowerAction is a request to change the machine's power state.
type PowerAction string

const (
	PowerReboot   PowerAction = "reboot"
	PowerShutdown PowerAction = "shutdown"
	PowerSuspend  PowerAction = "suspend"
)

// PowerActions lists every action in a stable order.
var PowerActions = []PowerAction{PowerReboot, PowerShutdown, PowerSuspend}

// PowerController carries out power actions.
type PowerController interface {
	// PowerActions reports which actions this device can carry out for this
	// process. An action missing from the map is unsupported.
	PowerActions(ctx context.Context) map[PowerAction]Availability
	// PreparePowerAction checks that the action can go ahead and returns the
	// function that performs it. Refusals are reported here, with
	// ErrUnsupported or ErrNotPermitted, because by the time commit runs the
	// caller has already been answered.
	PreparePowerAction(ctx context.Context, action PowerAction) (commit func() error, err error)
}

// Readers is one reader per section. A nil reader, or one returning
// ErrUnsupported, means the device does not have that section.
type Readers struct {
	Power        func() (power.Detail, error)
	Network      func() (Network, error)
	Bluetooth    func() (Bluetooth, error)
	Storage      func(roots []StorageRoot) ([]Volume, error)
	Display      func() (Display, error)
	Controllers  func() ([]Controller, error)
	System       func() (System, error)
	PowerControl PowerController
}

// NoPowerControl is the controller for a device that supports no power
// action.
type NoPowerControl struct{}

func (NoPowerControl) PowerActions(context.Context) map[PowerAction]Availability {
	return map[PowerAction]Availability{}
}

func (NoPowerControl) PreparePowerAction(context.Context, PowerAction) (func() error, error) {
	return nil, ErrUnsupported
}

// BatteryLevel names the coarse level a charge percentage falls in.
func BatteryLevel(percent int) string {
	switch {
	case percent >= 75:
		return BatteryLevelFull
	case percent >= 40:
		return BatteryLevelMedium
	case percent >= 15:
		return BatteryLevelLow
	default:
		return BatteryLevelEmpty
	}
}
