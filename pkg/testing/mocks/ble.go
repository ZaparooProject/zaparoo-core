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

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/bluetooth/ble"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
)

// FakeAdapter is an in-memory ble.Adapter.
type FakeAdapter struct {
	Periph   *FakePeripheral
	Cent     *FakeCentral
	gone     chan struct{}
	Addr     string
	RoleList []ble.Role
	goneOnce sync.Once
	mu       syncutil.Mutex
	closed   bool
}

// NewFakeAdapter returns an adapter reporting the given roles.
func NewFakeAdapter(roles ...ble.Role) *FakeAdapter {
	return &FakeAdapter{
		Periph:   NewFakePeripheral(),
		Cent:     NewFakeCentral(),
		gone:     make(chan struct{}),
		Addr:     "AA:BB:CC:DD:EE:FF",
		RoleList: roles,
	}
}

func (a *FakeAdapter) Address() string { return a.Addr }

func (a *FakeAdapter) Roles() []ble.Role { return append([]ble.Role(nil), a.RoleList...) }

func (a *FakeAdapter) Peripheral() (ble.Peripheral, error) {
	if !ble.SupportsRole(a.RoleList, ble.RolePeripheral) {
		return nil, ble.ErrRoleUnsupported
	}
	return a.Periph, nil
}

func (a *FakeAdapter) Central() (ble.Central, error) {
	if !ble.SupportsRole(a.RoleList, ble.RoleCentral) {
		return nil, ble.ErrRoleUnsupported
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

// FakePeripheral is an in-memory ble.Peripheral. Serve blocks until its
// context ends; tests drive the handler it was given through Handler.
type FakePeripheral struct {
	handler ble.PeripheralHandler
	// ServeErr, when set, is returned by Serve at once, as when the
	// adapter refuses to register the application.
	ServeErr error
	// NotifyErr, when set, is returned by every Notify.
	NotifyErr     error
	Notifications chan FakeNotification
	app           ble.Application
	adv           ble.Advertisement
	failServeErr  error
	disconnects   []ble.Peer
	mu            syncutil.Mutex
	serves        int
	failServes    int
	advOff        bool
}

// FailNextServes makes the next n Serve calls return err at once, as when
// BlueZ is too busy after an adapter reset to register the application.
func (p *FakePeripheral) FailNextServes(n int, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.failServes = n
	p.failServeErr = err
}

// Serves returns how many times Serve was called.
func (p *FakePeripheral) Serves() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.serves
}

// NewFakePeripheral returns a peripheral whose Notifications channel
// receives everything Notify is called with.
func NewFakePeripheral() *FakePeripheral {
	return &FakePeripheral{Notifications: make(chan FakeNotification, 256)}
}

func (p *FakePeripheral) Serve(
	ctx context.Context, app ble.Application, adv ble.Advertisement, h ble.PeripheralHandler,
) error {
	p.mu.Lock()
	p.handler = h
	p.app = app
	p.adv = adv
	p.serves++
	if p.failServes > 0 {
		p.failServes--
		p.mu.Unlock()
		return p.failServeErr
	}
	p.mu.Unlock()
	if p.ServeErr != nil {
		return p.ServeErr
	}
	<-ctx.Done()
	p.mu.Lock()
	p.handler = nil
	p.mu.Unlock()
	return nil
}

// Handler returns the handler of the active Serve call, or nil.
func (p *FakePeripheral) Handler() ble.PeripheralHandler {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.handler
}

// Application returns what the active Serve call registered.
func (p *FakePeripheral) Application() ble.Application {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.app
}

// Advertisement returns what the active Serve call advertised.
func (p *FakePeripheral) Advertisement() ble.Advertisement {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.adv
}

func (p *FakePeripheral) Notify(_ ble.Peer, charUUID string, value []byte) error {
	if p.NotifyErr != nil {
		return p.NotifyErr
	}
	n := FakeNotification{CharUUID: charUUID, Value: append([]byte(nil), value...)}
	select {
	case p.Notifications <- n:
	default:
	}
	return nil
}

func (p *FakePeripheral) Disconnect(_ context.Context, peer ble.Peer) error {
	p.mu.Lock()
	p.disconnects = append(p.disconnects, peer)
	p.mu.Unlock()
	return nil
}

// SetAdvertising implements ble.Peripheral.
func (p *FakePeripheral) SetAdvertising(on bool) {
	p.mu.Lock()
	p.advOff = !on
	p.mu.Unlock()
}

// Advertising reports whether advertising is switched on.
func (p *FakePeripheral) Advertising() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return !p.advOff
}

// Disconnects returns every peer Disconnect was called with.
func (p *FakePeripheral) Disconnects() []ble.Peer {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]ble.Peer(nil), p.disconnects...)
}

// FakeFind records one Find call.
type FakeFind struct {
	Address string
}

// FakeCentral is an in-memory ble.Central serving the devices it knows.
// Find for an unknown address blocks until the device is added or the
// context ends, as a real scan does.
type FakeCentral struct {
	// ScanErr, when set, is returned by Scan.
	ScanErr error
	devices map[string]*FakeDevice
	// added is closed and replaced whenever a device is added.
	added chan struct{}
	finds []FakeFind
	mu    syncutil.Mutex
}

func NewFakeCentral() *FakeCentral {
	return &FakeCentral{devices: make(map[string]*FakeDevice), added: make(chan struct{})}
}

// AddDevice makes a device findable by address and wakes any Find waiting
// for it.
func (c *FakeCentral) AddDevice(d *FakeDevice) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.devices[strings.ToUpper(d.Addr)] = d
	close(c.added)
	c.added = make(chan struct{})
}

func (c *FakeCentral) Find(ctx context.Context, address string) (ble.Device, error) {
	c.mu.Lock()
	c.finds = append(c.finds, FakeFind{Address: address})
	c.mu.Unlock()
	for {
		c.mu.Lock()
		d := c.devices[strings.ToUpper(address)]
		added := c.added
		c.mu.Unlock()
		if d != nil {
			return d, nil
		}
		select {
		case <-added:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

// Scan reports every known device once, then waits for the context.
func (c *FakeCentral) Scan(ctx context.Context, _ ble.ScanFilter) (<-chan ble.ScanResult, error) {
	if c.ScanErr != nil {
		return nil, c.ScanErr
	}
	c.mu.Lock()
	results := make([]ble.ScanResult, 0, len(c.devices))
	for addr, d := range c.devices {
		results = append(results, ble.ScanResult{Address: addr, Name: d.Name, HasRSSI: true})
	}
	c.mu.Unlock()

	out := make(chan ble.ScanResult, len(results))
	for _, r := range results {
		out <- r
	}
	go func() {
		<-ctx.Done()
		close(out)
	}()
	return out, nil
}

// Finds returns every Find call.
func (c *FakeCentral) Finds() []FakeFind {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]FakeFind(nil), c.finds...)
}

// FakeDevice is an in-memory ble.Device.
type FakeDevice struct {
	ConnectErr   error
	chars        map[string]*FakeCharacteristic
	disconnected chan struct{}
	Addr         string
	// Name is the local name the device advertises to a scan.
	Name        string
	disconnects int
	dropOnce    sync.Once
	mu          syncutil.Mutex
	connected   bool
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
	d.disconnects++
	d.mu.Unlock()
	d.Drop()
	return nil
}

// Disconnects returns how many times Disconnect was called.
func (d *FakeDevice) Disconnects() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.disconnects
}

func (d *FakeDevice) Characteristic(serviceUUID, charUUID string) (ble.RemoteCharacteristic, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	c, ok := d.chars[charKey(serviceUUID, charUUID)]
	if !ok {
		return nil, ble.ErrNotFound
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

// FakeCharacteristic is an in-memory ble.RemoteCharacteristic. Push feeds
// notifications to subscribers.
type FakeCharacteristic struct {
	// OnWrite, when set, sees every Write; its error is Write's.
	OnWrite func(value []byte, withResponse bool) error
	// ReadErr, MTUErr and SubscribeErr, when set, fail those calls.
	ReadErr      error
	MTUErr       error
	SubscribeErr error
	in           chan []byte
	// Value is what Read returns; Link is the MTU reported.
	Value []byte
	Link  int
}

func NewFakeCharacteristic() *FakeCharacteristic {
	return &FakeCharacteristic{in: make(chan []byte, 64)}
}

// Push delivers a notification value to the subscriber.
func (c *FakeCharacteristic) Push(value []byte) {
	c.in <- append([]byte(nil), value...)
}

func (c *FakeCharacteristic) Subscribe(ctx context.Context) (<-chan []byte, error) {
	if c.SubscribeErr != nil {
		return nil, c.SubscribeErr
	}
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

func (c *FakeCharacteristic) Write(_ context.Context, value []byte, withResponse bool) error {
	if c.OnWrite != nil {
		return c.OnWrite(append([]byte(nil), value...), withResponse)
	}
	return nil
}

func (c *FakeCharacteristic) Read(_ context.Context) ([]byte, error) {
	if c.ReadErr != nil {
		return nil, c.ReadErr
	}
	return append([]byte(nil), c.Value...), nil
}

func (c *FakeCharacteristic) MTU(_ context.Context) (int, error) {
	if c.MTUErr != nil {
		return 0, c.MTUErr
	}
	return c.Link, nil
}
