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

package mocks

import (
	"context"
	"strings"
	"sync"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/bluetooth/bluez"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
)

// FakeAdapter is an in-memory bluez.Adapter.
type FakeAdapter struct {
	Periph   *FakePeripheral
	Cent     *FakeCentral
	gone     chan struct{}
	Addr     string
	RoleList []bluez.Role
	goneOnce sync.Once
	mu       syncutil.Mutex
	closed   bool
}

// NewFakeAdapter returns an adapter reporting the given roles.
func NewFakeAdapter(roles ...bluez.Role) *FakeAdapter {
	return &FakeAdapter{
		Periph:   NewFakePeripheral(),
		Cent:     NewFakeCentral(),
		gone:     make(chan struct{}),
		Addr:     "AA:BB:CC:DD:EE:FF",
		RoleList: roles,
	}
}

func (a *FakeAdapter) Address() string { return a.Addr }

func (a *FakeAdapter) Roles() []bluez.Role { return append([]bluez.Role(nil), a.RoleList...) }

func (a *FakeAdapter) Peripheral() (bluez.Peripheral, error) {
	if !bluez.SupportsRole(a.RoleList, bluez.RolePeripheral) {
		return nil, bluez.ErrRoleUnsupported
	}
	return a.Periph, nil
}

func (a *FakeAdapter) Central() (bluez.Central, error) {
	if !bluez.SupportsRole(a.RoleList, bluez.RoleCentral) {
		return nil, bluez.ErrRoleUnsupported
	}
	return a.Cent, nil
}

func (a *FakeAdapter) Gone() <-chan struct{} { return a.gone }

// MarkGone simulates the adapter being unplugged.
func (a *FakeAdapter) MarkGone() {
	a.goneOnce.Do(func() { close(a.gone) })
}

func (a *FakeAdapter) Close() error {
	a.mu.Lock()
	a.closed = true
	a.mu.Unlock()
	a.MarkGone()
	return nil
}

// Closed reports whether Close was called.
func (a *FakeAdapter) Closed() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.closed
}

// FakeNotification is one value a FakePeripheral was asked to notify.
type FakeNotification struct {
	CharUUID string
	Value    []byte
}

// FakePeripheral is an in-memory bluez.Peripheral. Serve blocks until its
// context ends; tests drive the handler it was given through Handler.
type FakePeripheral struct {
	handler       bluez.PeripheralHandler
	Notifications chan FakeNotification
	app           bluez.Application
	adv           bluez.Advertisement
	disconnects   []bluez.Peer
	mu            syncutil.Mutex
}

// NewFakePeripheral returns a peripheral whose Notifications channel
// receives everything Notify is called with.
func NewFakePeripheral() *FakePeripheral {
	return &FakePeripheral{Notifications: make(chan FakeNotification, 256)}
}

func (p *FakePeripheral) Serve(
	ctx context.Context, app bluez.Application, adv bluez.Advertisement, h bluez.PeripheralHandler,
) error {
	p.mu.Lock()
	p.handler = h
	p.app = app
	p.adv = adv
	p.mu.Unlock()
	<-ctx.Done()
	p.mu.Lock()
	p.handler = nil
	p.mu.Unlock()
	return nil
}

// Handler returns the handler of the active Serve call, or nil.
func (p *FakePeripheral) Handler() bluez.PeripheralHandler {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.handler
}

// Application returns what the active Serve call registered.
func (p *FakePeripheral) Application() bluez.Application {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.app
}

// Advertisement returns what the active Serve call advertised.
func (p *FakePeripheral) Advertisement() bluez.Advertisement {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.adv
}

func (p *FakePeripheral) Notify(charUUID string, value []byte) error {
	n := FakeNotification{CharUUID: charUUID, Value: append([]byte(nil), value...)}
	select {
	case p.Notifications <- n:
	default:
	}
	return nil
}

func (p *FakePeripheral) Disconnect(_ context.Context, peer bluez.Peer) error {
	p.mu.Lock()
	p.disconnects = append(p.disconnects, peer)
	p.mu.Unlock()
	return nil
}

// Disconnects returns every peer Disconnect was called with.
func (p *FakePeripheral) Disconnects() []bluez.Peer {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]bluez.Peer(nil), p.disconnects...)
}

// FakeFind records one Find call.
type FakeFind struct {
	Address      string
	ServiceUUIDs []string
}

// FakeCentral is an in-memory bluez.Central serving the devices it knows.
// Find for an unknown address blocks until the context ends.
type FakeCentral struct {
	devices map[string]*FakeDevice
	finds   []FakeFind
	mu      syncutil.Mutex
}

func NewFakeCentral() *FakeCentral {
	return &FakeCentral{devices: make(map[string]*FakeDevice)}
}

// AddDevice makes a device findable by address.
func (c *FakeCentral) AddDevice(d *FakeDevice) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.devices[strings.ToUpper(d.Addr)] = d
}

func (c *FakeCentral) Find(ctx context.Context, address string, serviceUUIDs []string) (bluez.Device, error) {
	c.mu.Lock()
	c.finds = append(c.finds, FakeFind{Address: address, ServiceUUIDs: append([]string(nil), serviceUUIDs...)})
	d := c.devices[strings.ToUpper(address)]
	c.mu.Unlock()
	if d != nil {
		return d, nil
	}
	<-ctx.Done()
	return nil, ctx.Err()
}

// Finds returns every Find call.
func (c *FakeCentral) Finds() []FakeFind {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]FakeFind(nil), c.finds...)
}

// FakeDevice is an in-memory bluez.Device.
type FakeDevice struct {
	ConnectErr   error
	chars        map[string]*FakeCharacteristic
	disconnected chan struct{}
	Addr         string
	dropOnce     sync.Once
	mu           syncutil.Mutex
	connected    bool
}

func NewFakeDevice(address string) *FakeDevice {
	return &FakeDevice{
		chars:        make(map[string]*FakeCharacteristic),
		disconnected: make(chan struct{}),
		Addr:         address,
	}
}

// AddCharacteristic registers a characteristic under a service.
func (d *FakeDevice) AddCharacteristic(serviceUUID, charUUID string, c *FakeCharacteristic) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.chars[charKey(serviceUUID, charUUID)] = c
}

func charKey(serviceUUID, charUUID string) string {
	return strings.ToLower(serviceUUID) + "/" + strings.ToLower(charUUID)
}

func (d *FakeDevice) Address() string { return d.Addr }

func (d *FakeDevice) Connect(_ context.Context) error {
	if d.ConnectErr != nil {
		return d.ConnectErr
	}
	d.mu.Lock()
	d.connected = true
	d.mu.Unlock()
	return nil
}

// Connected reports whether Connect succeeded and Disconnect was not called.
func (d *FakeDevice) Connected() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.connected
}

func (d *FakeDevice) Disconnect(_ context.Context) error {
	d.mu.Lock()
	d.connected = false
	d.mu.Unlock()
	d.Drop()
	return nil
}

func (d *FakeDevice) Characteristic(serviceUUID, charUUID string) (bluez.RemoteCharacteristic, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	c, ok := d.chars[charKey(serviceUUID, charUUID)]
	if !ok {
		return nil, bluez.ErrNotFound
	}
	return c, nil
}

func (d *FakeDevice) Disconnected() <-chan struct{} { return d.disconnected }

// Drop simulates the link going down.
func (d *FakeDevice) Drop() {
	d.mu.Lock()
	d.connected = false
	d.mu.Unlock()
	d.dropOnce.Do(func() { close(d.disconnected) })
}

// FakeCharacteristic is an in-memory bluez.RemoteCharacteristic. Push feeds
// notifications to subscribers.
type FakeCharacteristic struct {
	in chan []byte
}

func NewFakeCharacteristic() *FakeCharacteristic {
	return &FakeCharacteristic{in: make(chan []byte, 64)}
}

// Push delivers a notification value to the subscriber.
func (c *FakeCharacteristic) Push(value []byte) {
	c.in <- append([]byte(nil), value...)
}

func (c *FakeCharacteristic) Subscribe(ctx context.Context) (<-chan []byte, error) {
	out := make(chan []byte, 64)
	go func() {
		defer close(out)
		for {
			select {
			case <-ctx.Done():
				return
			case v := <-c.in:
				select {
				case out <- v:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return out, nil
}

func (*FakeCharacteristic) Write(_ context.Context, _ []byte, _ bool) error {
	return nil
}
