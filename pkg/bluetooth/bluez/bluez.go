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

// Package bluez is a thin layer over the BlueZ D-Bus API. It exposes the two
// Bluetooth Low Energy roles Core needs: a peripheral (GATT server plus
// advertising, used by the app transport) and a central (scan, connect,
// subscribe, used by reader drivers). Only Linux has an implementation; the
// other platforms report ErrUnsupported.
package bluez

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"
)

var (
	// ErrUnsupported is returned on platforms without a BlueZ implementation.
	ErrUnsupported = errors.New("bluez: not supported on this platform")
	// ErrUnavailable is returned when the system bus or bluetoothd cannot be
	// reached.
	ErrUnavailable = errors.New("bluez: bluetoothd not reachable")
	// ErrNoAdapter is returned when bluetoothd is running but no adapter is
	// plugged in.
	ErrNoAdapter = errors.New("bluez: no bluetooth adapter")
	// ErrRoleUnsupported is returned when the adapter cannot take the
	// requested role.
	ErrRoleUnsupported = errors.New("bluez: adapter does not support this role")
	// ErrNotFound is returned when a remote device, service, or
	// characteristic is not present.
	ErrNotFound = errors.New("bluez: not found")
)

// Role is a BLE link-layer role an adapter can take.
type Role string

const (
	// RoleCentral scans and initiates connections.
	RoleCentral Role = "central"
	// RolePeripheral advertises and accepts connections.
	RolePeripheral Role = "peripheral"
)

// Characteristic flags, as BlueZ names them.
const (
	FlagRead                 = "read"
	FlagWrite                = "write"
	FlagWriteWithoutResponse = "write-without-response"
	FlagNotify               = "notify"
)

// DefaultCallTimeout bounds a single D-Bus round trip.
const DefaultCallTimeout = 3 * time.Second

// NormalizeAddress validates a Bluetooth address and returns it in the
// upper-case colon form BlueZ uses.
func NormalizeAddress(address string) (string, error) {
	hw, err := net.ParseMAC(strings.TrimSpace(address))
	if err != nil || len(hw) != 6 {
		return "", fmt.Errorf("invalid bluetooth address %q", address)
	}
	return strings.ToUpper(hw.String()), nil
}

// SupportsRole reports whether roles allows role. An empty list means the
// adapter did not say, which callers treat as "try it".
func SupportsRole(roles []Role, role Role) bool {
	if len(roles) == 0 {
		return true
	}
	for _, r := range roles {
		if r == role || r == RoleCentral+"-"+RolePeripheral {
			return true
		}
	}
	return false
}

// Option configures Open.
type Option func(*options)

type options struct {
	busAddress  string
	callTimeout time.Duration
	powerOn     bool
}

// WithBusAddress connects to the bus at addr instead of the system bus. Tests
// use it to point the layer at a private bus hosting a fake bluetoothd.
func WithBusAddress(addr string) Option {
	return func(o *options) { o.busAddress = addr }
}

// WithPowerOn powers the adapter on if it is off. Only a caller acting on
// an explicit user choice should ask for this; a background retry must not
// keep switching a radio back on that the user turned off.
func WithPowerOn() Option {
	return func(o *options) { o.powerOn = true }
}

// WithCallTimeout overrides DefaultCallTimeout.
func WithCallTimeout(d time.Duration) Option {
	return func(o *options) { o.callTimeout = d }
}

func applyOptions(opts []Option) options {
	o := options{callTimeout: DefaultCallTimeout}
	for _, opt := range opts {
		opt(&o)
	}
	if o.callTimeout <= 0 {
		o.callTimeout = DefaultCallTimeout
	}
	return o
}

// Adapter is one local Bluetooth controller.
type Adapter interface {
	// Address is the controller's own Bluetooth address.
	Address() string
	// Roles lists what the controller supports; empty when BlueZ did not
	// report it.
	Roles() []Role
	// Peripheral returns the GATT server and advertising side.
	Peripheral() (Peripheral, error)
	// Central returns the scanning and connecting side.
	Central() (Central, error)
	// Gone is closed when the adapter disappears or the bus connection
	// drops, so owners can start over.
	Gone() <-chan struct{}
	// Close releases the bus connection. Everything obtained from the
	// adapter stops working.
	Close() error
}

// Peer identifies a remote central connected to the local GATT server.
type Peer struct {
	// Path is the BlueZ Device1 object path, unique per connection.
	Path string
	// Address is the peer's Bluetooth address as BlueZ reports it, which
	// for phones is usually a rotating private address.
	Address string
}

// Characteristic describes one characteristic of a local service.
type Characteristic struct {
	UUID  string
	Flags []string
}

// Service describes one local GATT service.
type Service struct {
	UUID            string
	Characteristics []Characteristic
	Primary         bool
}

// Application is the set of local services registered together.
type Application struct {
	Services []Service
}

// Advertisement describes what the peripheral broadcasts. It is always a
// connectable advertisement.
type Advertisement struct {
	LocalName    string
	ServiceUUIDs []string
}

// PeripheralHandler receives GATT server events. Calls for one peer may
// arrive out of order because BlueZ delivers each one on its own goroutine;
// consumers must tolerate that.
type PeripheralHandler interface {
	// OnWrite is called once per write to a characteristic. mtu is the
	// negotiated ATT MTU when BlueZ reports it, otherwise 0.
	OnWrite(peer Peer, charUUID string, value []byte, mtu int)
	// OnRead returns the value a peer reads from a characteristic.
	OnRead(peer Peer, charUUID string) ([]byte, error)
	// OnSubscribe reports notification subscriptions. BlueZ does not say
	// which peer subscribed, so peer is zero-valued.
	OnSubscribe(peer Peer, charUUID string, subscribed bool)
	// OnDisconnect is called when a peer's connection ends.
	OnDisconnect(peer Peer)
}

// Peripheral is the GATT server and advertising side of an adapter.
type Peripheral interface {
	// Serve registers the application and advertisement, then blocks until
	// ctx ends or the adapter is gone. Both are unregistered on return.
	Serve(ctx context.Context, app Application, adv Advertisement, h PeripheralHandler) error
	// Notify sends value to every peer subscribed to the characteristic.
	// BlueZ fans notifications out; callers tag the payload if they need
	// to address one peer.
	Notify(charUUID string, value []byte) error
	// Disconnect drops a peer's connection.
	Disconnect(ctx context.Context, peer Peer) error
}

// Central is the scanning and connecting side of an adapter.
type Central interface {
	// Find scans for the device with the given address, filtering on the
	// service UUIDs, until it appears or ctx ends.
	Find(ctx context.Context, address string, serviceUUIDs []string) (Device, error)
}

// Device is a remote peripheral.
type Device interface {
	Address() string
	// Connect connects and waits for service discovery to finish.
	Connect(ctx context.Context) error
	Disconnect(ctx context.Context) error
	// Characteristic looks up a characteristic of a resolved service.
	Characteristic(serviceUUID, charUUID string) (RemoteCharacteristic, error)
	// Disconnected is closed once the connection has ended.
	Disconnected() <-chan struct{}
}

// RemoteCharacteristic is one characteristic on a connected device.
type RemoteCharacteristic interface {
	// Subscribe enables notifications and streams their values until ctx
	// ends or the device disconnects, after which the channel is closed.
	Subscribe(ctx context.Context) (<-chan []byte, error)
	// Write writes the value, with or without waiting for the peripheral's
	// acknowledgement.
	Write(ctx context.Context, value []byte, withResponse bool) error
}
