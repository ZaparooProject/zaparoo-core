//go:build windows

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
package ble

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/rs/zerolog/log"
	"github.com/saltosystems/winrt-go/windows/devices/bluetooth"
	gatt "github.com/saltosystems/winrt-go/windows/devices/bluetooth/genericattributeprofile"
)

const (
	// scanBuffer is how many scan results a consumer may leave unread
	// before the oldest are dropped.
	scanBuffer = 64
	// notifyBuffer is how many notifications a subscriber may leave unread.
	notifyBuffer = 64
)

// central is the Windows Central.
type central struct {
	a *adapter
}

// Scan reports matching devices in range until ctx ends. Windows hands over
// every advertising packet; a device is reported when first heard and again
// when its name or services become known.
func (c *central) Scan(ctx context.Context, filter ScanFilter) (<-chan ScanResult, error) {
	return c.watch(ctx, func(r *ScanResult) bool {
		return advertisesAny(r.ServiceUUIDs, filter.ServiceUUIDs)
	})
}

// advertisesAny reports whether have contains one of want; an empty want
// accepts everything.
func advertisesAny(have, want []string) bool {
	if len(want) == 0 {
		return true
	}
	for _, w := range want {
		if slices.ContainsFunc(have, func(h string) bool { return strings.EqualFold(h, w) }) {
			return true
		}
	}
	return false
}

// watch runs an advertisement watcher and reports every device that passes
// match.
func (c *central) watch(ctx context.Context, match func(*ScanResult) bool) (<-chan ScanResult, error) {
	out := make(chan ScanResult, scanBuffer)
	var (
		mu     syncutil.Mutex
		seen   = make(map[string]*ScanResult)
		closed bool
	)
	watcher, err := c.a.host.newWatcher(func(update ScanResult) {
		mu.Lock()
		defer mu.Unlock()
		if closed {
			return
		}
		known := seen[update.Address]
		if known == nil {
			known = &ScanResult{Address: update.Address}
			seen[update.Address] = known
		}
		if !mergeScanResult(known, &update) {
			// Nothing new but the signal strength.
			return
		}
		if match(known) {
			emitScanResult(out, known)
		}
	})
	if err != nil {
		return nil, fmt.Errorf("scan: %w", err)
	}
	if err = watcher.start(); err != nil {
		watcher.unregister()
		watcher.release()
		return nil, fmt.Errorf("%w: start scanning: %w", ErrUnavailable, err)
	}

	go func() {
		select {
		case <-ctx.Done():
		case <-c.a.gone:
		}
		if stopErr := watcher.stop(); stopErr != nil {
			log.Debug().Err(stopErr).Msg("bluetooth stop scanning failed")
		}
		watcher.unregister()
		mu.Lock()
		closed = true
		close(out)
		mu.Unlock()
		watcher.release()
	}()
	return out, nil
}

// mergeScanResult folds a new sighting into what is known and reports
// whether anything but the signal strength changed.
func mergeScanResult(known, update *ScanResult) bool {
	changed := !known.HasRSSI
	known.RSSI, known.HasRSSI = update.RSSI, true
	if update.Name != "" && update.Name != known.Name {
		known.Name = update.Name
		changed = true
	}
	for _, uuid := range update.ServiceUUIDs {
		if !slices.Contains(known.ServiceUUIDs, uuid) {
			known.ServiceUUIDs = append(known.ServiceUUIDs, uuid)
			changed = true
		}
	}
	for id, data := range update.ManufacturerData {
		if known.ManufacturerData == nil {
			known.ManufacturerData = make(map[uint16][]byte)
		}
		if !slices.Equal(known.ManufacturerData[id], data) {
			known.ManufacturerData[id] = data
			changed = true
		}
	}
	return changed
}

// emitScanResult delivers a copy of r, dropping the oldest undelivered
// result when the consumer is behind.
func emitScanResult(out chan ScanResult, r *ScanResult) {
	result := *r
	result.ServiceUUIDs = slices.Clone(r.ServiceUUIDs)
	result.ManufacturerData = make(map[uint16][]byte, len(r.ManufacturerData))
	for id, data := range r.ManufacturerData {
		result.ManufacturerData[id] = slices.Clone(data)
	}
	for {
		select {
		case out <- result:
			return
		default:
		}
		select {
		case <-out:
		default:
		}
	}
}

// Find returns the device with the given address. Windows hands back a
// device it already knows at once, and connecting to it waits for it to
// appear; one it has never seen is scanned for first.
func (c *central) Find(ctx context.Context, address string) (Device, error) {
	addr, err := NormalizeAddress(address)
	if err != nil {
		return nil, err
	}
	if dev, lookupErr := c.lookup(ctx, addr); lookupErr != nil || dev != nil {
		return dev, lookupErr
	}

	scanCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	results, err := c.watch(scanCtx, func(r *ScanResult) bool { return strings.EqualFold(r.Address, addr) })
	if err != nil {
		return nil, err
	}
	if _, found := <-results; !found {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("find %s: %w", addr, ctx.Err())
		}
		return nil, ErrUnavailable
	}
	dev, err := c.lookup(ctx, addr)
	if err != nil {
		return nil, err
	}
	if dev == nil {
		return nil, fmt.Errorf("%w: %s was heard but windows has no device for it", ErrNotFound, addr)
	}
	return dev, nil
}

// lookup asks Windows for the device object of an address. It returns nil,
// nil when Windows has never seen the device.
func (c *central) lookup(ctx context.Context, addr string) (Device, error) {
	dev, err := c.a.host.lookupDevice(ctx, addr)
	if err != nil {
		return nil, fmt.Errorf("look up %s: %w", addr, err)
	}
	if dev == nil {
		return nil, nil //nolint:nilnil // nil device means not known yet, not an error
	}
	return newDevice(dev, addr), nil
}

// device is the Windows Device.
type device struct {
	dev          leDevice
	session      clientSession
	statusChange eventHandle
	disconnected chan struct{}
	address      string
	services     []remoteService
	mu           syncutil.Mutex
	dropOnce     sync.Once
	closeOnce    sync.Once
}

func newDevice(dev leDevice, address string) *device {
	return &device{dev: dev, address: address, disconnected: make(chan struct{})}
}

func (d *device) Address() string { return d.address }

func (d *device) Disconnected() <-chan struct{} { return d.disconnected }

func (d *device) drop() {
	d.dropOnce.Do(func() { close(d.disconnected) })
}

// Connect asks Windows to hold a connection to the device and discovers its
// services, which is what actually brings the link up. A device that fails
// to connect is released on the spot: the caller finds the device again for
// the next attempt, and nothing else would free what this one holds.
func (d *device) Connect(ctx context.Context) error {
	if err := d.openAndDiscover(ctx); err != nil {
		_ = d.Disconnect(ctx)
		return err
	}
	return nil
}

func (d *device) openAndDiscover(ctx context.Context) error {
	session, err := d.dev.openSession(ctx)
	if err != nil {
		return fmt.Errorf("connect %s: %w", d.address, err)
	}

	statusChange, err := d.dev.onConnectionStatusChanged(func(status bluetooth.BluetoothConnectionStatus) {
		if status == bluetooth.BluetoothConnectionStatusDisconnected {
			d.drop()
		}
	})
	if err != nil {
		session.close()
		return fmt.Errorf("connect %s: watch connection: %w", d.address, err)
	}

	d.mu.Lock()
	d.session, d.statusChange = session, statusChange
	d.mu.Unlock()

	status, services, err := d.dev.discoverServices(ctx)
	if err != nil {
		return fmt.Errorf("connect %s: %w", d.address, err)
	}
	if statusErr := communicationError("connect "+d.address, status); statusErr != nil {
		return statusErr
	}
	d.mu.Lock()
	d.services = services
	d.mu.Unlock()
	return nil
}

// Disconnect releases everything held for the device, which lets Windows
// drop the link.
func (d *device) Disconnect(_ context.Context) error {
	d.closeOnce.Do(func() {
		d.mu.Lock()
		services, session, statusChange := d.services, d.session, d.statusChange
		d.services, d.session, d.statusChange = nil, nil, nil
		d.mu.Unlock()

		for _, svc := range services {
			svc.close()
		}
		if statusChange != nil {
			statusChange.remove()
			statusChange.release()
		}
		if session != nil {
			session.close()
		}
		d.dev.close()
		d.drop()
	})
	return nil
}

// Characteristic finds a characteristic among the services Connect found.
func (d *device) Characteristic(serviceUUID, charUUID string) (RemoteCharacteristic, error) {
	d.mu.Lock()
	services := slices.Clone(d.services)
	d.mu.Unlock()

	for _, svc := range services {
		uuid, err := svc.uuid()
		if err != nil || !strings.EqualFold(uuid, serviceUUID) {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), DefaultCallTimeout)
		ch, err := findCharacteristic(ctx, svc, charUUID)
		cancel()
		if err != nil {
			return nil, fmt.Errorf("characteristic %s in service %s on %s: %w", charUUID, serviceUUID, d.address, err)
		}
		return &remoteChar{d: d, c: ch}, nil
	}
	return nil, fmt.Errorf("%w: service %s on %s", ErrNotFound, serviceUUID, d.address)
}

func findCharacteristic(ctx context.Context, svc remoteService, charUUID string) (gattCharacteristic, error) {
	status, chars, err := svc.characteristics(ctx)
	if err != nil {
		return nil, fmt.Errorf("list characteristics: %w", err)
	}
	if statusErr := communicationError("list characteristics", status); statusErr != nil {
		return nil, statusErr
	}
	var found gattCharacteristic
	for _, ch := range chars {
		if found == nil {
			if uuid, uuidErr := ch.uuid(); uuidErr == nil && strings.EqualFold(uuid, charUUID) {
				found = ch
				continue
			}
		}
		ch.release()
	}
	if found == nil {
		return nil, ErrNotFound
	}
	return found, nil
}

// remoteChar is the Windows RemoteCharacteristic.
type remoteChar struct {
	d *device
	c gattCharacteristic
}

// configure writes the client characteristic configuration descriptor,
// which is how notifications are turned on and off.
func (rc *remoteChar) configure(
	ctx context.Context, value gatt.GattClientCharacteristicConfigurationDescriptorValue,
) error {
	status, err := rc.c.configure(ctx, value)
	if err != nil {
		return fmt.Errorf("configure notifications: %w", err)
	}
	return communicationError("configure notifications", status)
}

// Subscribe enables notifications and streams values until ctx ends or the
// device disconnects.
func (rc *remoteChar) Subscribe(ctx context.Context) (<-chan []byte, error) {
	out := make(chan []byte, notifyBuffer)
	var (
		mu     syncutil.Mutex
		closed bool
	)
	changed, err := rc.c.onValueChanged(func(value []byte) {
		mu.Lock()
		defer mu.Unlock()
		if closed {
			return
		}
		select {
		case out <- value:
		default:
			log.Warn().Msg("bluetooth notification subscriber is behind, dropping a value")
		}
	})
	if err != nil {
		return nil, fmt.Errorf("watch notifications: %w", err)
	}
	if err = rc.configure(ctx, gatt.GattClientCharacteristicConfigurationDescriptorValueNotify); err != nil {
		changed.remove()
		changed.release()
		return nil, err
	}

	go func() {
		select {
		case <-ctx.Done():
			rc.stopNotify()
		case <-rc.d.disconnected:
		}
		changed.remove()
		mu.Lock()
		closed = true
		close(out)
		mu.Unlock()
		changed.release()
	}()
	return out, nil
}

// stopNotify turns notifications off, best effort. The subscription's own
// context has ended by now, so this runs on a short one of its own.
func (rc *remoteChar) stopNotify() {
	ctx, cancel := context.WithTimeout(context.Background(), DefaultCallTimeout)
	defer cancel()
	none := gatt.GattClientCharacteristicConfigurationDescriptorValueNone
	if err := rc.configure(ctx, none); err != nil {
		log.Debug().Err(err).Msg("bluetooth stop notify failed")
	}
}

// Write sends value as a write command or, with withResponse, a write
// request that waits for the peripheral's acknowledgement.
func (rc *remoteChar) Write(ctx context.Context, value []byte, withResponse bool) error {
	option := gatt.GattWriteOptionWriteWithoutResponse
	if withResponse {
		option = gatt.GattWriteOptionWriteWithResponse
	}
	status, err := rc.c.write(ctx, value, option)
	if err != nil {
		return fmt.Errorf("write characteristic: %w", err)
	}
	return communicationError("write characteristic", status)
}

// Read reads the characteristic's value from the device.
func (rc *remoteChar) Read(ctx context.Context) ([]byte, error) {
	status, value, err := rc.c.read(ctx)
	if err != nil {
		return nil, fmt.Errorf("read characteristic: %w", err)
	}
	if statusErr := communicationError("read characteristic", status); statusErr != nil {
		return nil, statusErr
	}
	return value, nil
}

// MTU reports the ATT MTU negotiated for the link.
func (rc *remoteChar) MTU(_ context.Context) (int, error) {
	rc.d.mu.Lock()
	session := rc.d.session
	rc.d.mu.Unlock()
	if session == nil {
		return 0, nil
	}
	size, err := session.maxPduSize()
	if err != nil {
		return 0, fmt.Errorf("read mtu: %w", err)
	}
	return int(size), nil
}
