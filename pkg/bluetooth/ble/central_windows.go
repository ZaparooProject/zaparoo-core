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
	"syscall"
	"unsafe"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/rs/zerolog/log"
	"github.com/saltosystems/winrt-go/windows/devices/bluetooth"
	"github.com/saltosystems/winrt-go/windows/devices/bluetooth/advertisement"
	gatt "github.com/saltosystems/winrt-go/windows/devices/bluetooth/genericattributeprofile"
	"github.com/saltosystems/winrt-go/windows/foundation"
	"github.com/saltosystems/winrt-go/windows/foundation/collections"
	"github.com/saltosystems/winrt-go/windows/storage/streams"
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
	watcher, err := advertisement.NewBluetoothLEAdvertisementWatcher()
	if err != nil {
		return nil, fmt.Errorf("%w: new advertisement watcher: %w", ErrUnavailable, err)
	}
	// Active scanning asks each device for its scan response, which is
	// where many keep their name and services.
	if err = watcher.SetScanningMode(advertisement.BluetoothLEScanningModeActive); err != nil {
		watcher.Release()
		return nil, fmt.Errorf("set scanning mode: %w", err)
	}

	out := make(chan ScanResult, scanBuffer)
	var (
		mu     syncutil.Mutex
		seen   = make(map[string]*ScanResult)
		closed bool
	)
	received := typedHandler(
		advertisement.SignatureBluetoothLEAdvertisementWatcher,
		advertisement.SignatureBluetoothLEAdvertisementReceivedEventArgs,
		func(_, args unsafe.Pointer) {
			update := scanResultFromArgs((*advertisement.BluetoothLEAdvertisementReceivedEventArgs)(args))
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
	token, err := watcher.AddReceived(received)
	if err != nil {
		received.Release()
		watcher.Release()
		return nil, fmt.Errorf("watch advertisements: %w", err)
	}
	if err = watcher.Start(); err != nil {
		_ = watcher.RemoveReceived(token)
		received.Release()
		watcher.Release()
		return nil, fmt.Errorf("%w: start scanning: %w", ErrUnavailable, err)
	}

	go func() {
		select {
		case <-ctx.Done():
		case <-c.a.gone:
		}
		if stopErr := watcher.Stop(); stopErr != nil {
			log.Debug().Err(stopErr).Msg("bluetooth stop scanning failed")
		}
		_ = watcher.RemoveReceived(token)
		mu.Lock()
		closed = true
		close(out)
		mu.Unlock()
		received.Release()
		watcher.Release()
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

// scanResultFromArgs reads one advertising packet.
func scanResultFromArgs(args *advertisement.BluetoothLEAdvertisementReceivedEventArgs) ScanResult {
	var r ScanResult
	if addr, err := args.GetBluetoothAddress(); err == nil {
		r.Address = addressFromUint64(addr)
	}
	if rssi, err := args.GetRawSignalStrengthInDBm(); err == nil {
		r.RSSI, r.HasRSSI = rssi, true
	}
	adv, err := args.GetAdvertisement()
	if err != nil || adv == nil {
		return r
	}
	defer adv.Release()
	if name, nameErr := adv.GetLocalName(); nameErr == nil {
		r.Name = name
	}
	if uuids, uuidErr := adv.GetServiceUuids(); uuidErr == nil && uuids != nil {
		r.ServiceUUIDs = guidVector(uuids)
		uuids.Release()
	}
	if items, itemsErr := adv.GetManufacturerData(); itemsErr == nil && items != nil {
		r.ManufacturerData = manufacturerData(items)
		items.Release()
	}
	return r
}

// guidVector reads an IVector<Guid>. Its elements are 16-byte values, which
// the generic binding has no room for, so the method is called directly
// with a GUID to fill in.
func guidVector(v *collections.IVector) []string {
	n, err := v.GetSize()
	if err != nil {
		return nil
	}
	uuids := make([]string, 0, n)
	for i := range n {
		var g syscall.GUID
		hr, _, _ := syscall.SyscallN(
			v.VTable().GetAt,
			uintptr(unsafe.Pointer(v)), //nolint:gosec // COM this pointer
			uintptr(i),
			uintptr(unsafe.Pointer(&g)), //nolint:gosec // COM out parameter
		)
		if hr == 0 {
			uuids = append(uuids, uuidFromGUID(g))
		}
	}
	return uuids
}

func manufacturerData(v *collections.IVector) map[uint16][]byte {
	n, err := v.GetSize()
	if err != nil || n == 0 {
		return nil
	}
	out := make(map[uint16][]byte, n)
	for i := range n {
		item, itemErr := v.GetAt(i)
		if itemErr != nil || item == nil {
			continue
		}
		entry := (*advertisement.BluetoothLEManufacturerData)(item)
		id, idErr := entry.GetCompanyId()
		buf, bufErr := entry.GetData()
		if idErr == nil && bufErr == nil {
			if data, dataErr := bufferBytes(buf); dataErr == nil {
				out[id] = data
			}
		}
		if buf != nil {
			buf.Release()
		}
		entry.Release()
	}
	return out
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
	value, err := addressToUint64(addr)
	if err != nil {
		return nil, err
	}
	op, err := bluetooth.BluetoothLEDeviceFromBluetoothAddressAsync(value)
	if err != nil {
		return nil, fmt.Errorf("%w: look up %s: %w", ErrUnavailable, addr, err)
	}
	defer op.Release()
	if err = await(ctx, op, bluetooth.SignatureBluetoothLEDevice); err != nil {
		return nil, fmt.Errorf("look up %s: %w", addr, err)
	}
	res, err := op.GetResults()
	if err != nil {
		return nil, fmt.Errorf("look up %s: %w", addr, err)
	}
	if res == nil {
		return nil, nil //nolint:nilnil // nil device means not known yet, not an error
	}
	return newDevice(c.a, (*bluetooth.BluetoothLEDevice)(res), addr), nil
}

// device is the Windows Device.
type device struct {
	a            *adapter
	dev          *bluetooth.BluetoothLEDevice
	session      *gatt.GattSession
	statusChange *foundation.TypedEventHandler
	disconnected chan struct{}
	address      string
	services     []*gatt.GattDeviceService
	statusToken  foundation.EventRegistrationToken
	mu           syncutil.Mutex
	dropOnce     sync.Once
	closeOnce    sync.Once
	watching     bool
}

func newDevice(a *adapter, dev *bluetooth.BluetoothLEDevice, address string) *device {
	return &device{a: a, dev: dev, address: address, disconnected: make(chan struct{})}
}

func (d *device) Address() string { return d.address }

func (d *device) Disconnected() <-chan struct{} { return d.disconnected }

func (d *device) drop() {
	d.dropOnce.Do(func() { close(d.disconnected) })
}

// Connect asks Windows to hold a connection to the device and discovers its
// services, which is what actually brings the link up.
func (d *device) Connect(ctx context.Context) error {
	id, err := d.dev.GetBluetoothDeviceId()
	if err != nil {
		return fmt.Errorf("connect %s: device id: %w", d.address, err)
	}
	defer id.Release()

	sessionOp, err := gatt.GattSessionFromDeviceIdAsync(id)
	if err != nil {
		return fmt.Errorf("connect %s: gatt session: %w", d.address, err)
	}
	defer sessionOp.Release()
	if err = await(ctx, sessionOp, gatt.SignatureGattSession); err != nil {
		return fmt.Errorf("connect %s: gatt session: %w", d.address, err)
	}
	res, err := sessionOp.GetResults()
	if err != nil || res == nil {
		return fmt.Errorf("connect %s: no gatt session: %w", d.address, err)
	}
	session := (*gatt.GattSession)(res)
	if err = session.SetMaintainConnection(true); err != nil {
		session.Release()
		return fmt.Errorf("connect %s: maintain connection: %w", d.address, err)
	}

	handler := typedHandler(bluetooth.SignatureBluetoothLEDevice, objectSignature, func(_, _ unsafe.Pointer) {
		if status, statusErr := d.dev.GetConnectionStatus(); statusErr == nil &&
			status == bluetooth.BluetoothConnectionStatusDisconnected {
			d.drop()
		}
	})
	token, err := d.dev.AddConnectionStatusChanged(handler)
	if err != nil {
		handler.Release()
		session.Release()
		return fmt.Errorf("connect %s: watch connection: %w", d.address, err)
	}

	d.mu.Lock()
	d.session, d.statusChange, d.statusToken, d.watching = session, handler, token, true
	d.mu.Unlock()

	servicesOp, err := d.dev.GetGattServicesWithCacheModeAsync(bluetooth.BluetoothCacheModeUncached)
	if err != nil {
		return fmt.Errorf("connect %s: discover services: %w", d.address, err)
	}
	defer servicesOp.Release()
	if err = await(ctx, servicesOp, gatt.SignatureGattDeviceServicesResult); err != nil {
		return fmt.Errorf("connect %s: discover services: %w", d.address, err)
	}
	res, err = servicesOp.GetResults()
	if err != nil || res == nil {
		return fmt.Errorf("connect %s: no services result: %w", d.address, err)
	}
	result := (*gatt.GattDeviceServicesResult)(res)
	defer result.Release()
	status, err := result.GetStatus()
	if err != nil {
		return fmt.Errorf("connect %s: discover services: %w", d.address, err)
	}
	if statusErr := communicationError("connect "+d.address, status); statusErr != nil {
		return statusErr
	}
	vector, err := result.GetServices()
	if err != nil {
		return fmt.Errorf("connect %s: services: %w", d.address, err)
	}
	defer vector.Release()
	n, err := vector.GetSize()
	if err != nil {
		return fmt.Errorf("connect %s: services: %w", d.address, err)
	}
	services := make([]*gatt.GattDeviceService, 0, n)
	for i := range n {
		if item, itemErr := vector.GetAt(i); itemErr == nil && item != nil {
			services = append(services, (*gatt.GattDeviceService)(item))
		}
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
		services, session, handler, token, watching := d.services, d.session, d.statusChange, d.statusToken, d.watching
		d.services, d.session, d.statusChange, d.watching = nil, nil, nil, false
		d.mu.Unlock()

		for _, svc := range services {
			_ = svc.Close()
			svc.Release()
		}
		if watching {
			_ = d.dev.RemoveConnectionStatusChanged(token)
			handler.Release()
		}
		if session != nil {
			_ = session.Close()
			session.Release()
		}
		_ = d.dev.Close()
		d.dev.Release()
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
		g, err := svc.GetUuid()
		if err != nil || !strings.EqualFold(uuidFromGUID(g), serviceUUID) {
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

func findCharacteristic(
	ctx context.Context, svc *gatt.GattDeviceService, charUUID string,
) (*gatt.GattCharacteristic, error) {
	op, err := svc.GetCharacteristicsWithCacheModeAsync(bluetooth.BluetoothCacheModeUncached)
	if err != nil {
		return nil, fmt.Errorf("list characteristics: %w", err)
	}
	defer op.Release()
	if err = await(ctx, op, gatt.SignatureGattCharacteristicsResult); err != nil {
		return nil, fmt.Errorf("list characteristics: %w", err)
	}
	res, err := op.GetResults()
	if err != nil || res == nil {
		return nil, fmt.Errorf("list characteristics: no result: %w", err)
	}
	result := (*gatt.GattCharacteristicsResult)(res)
	defer result.Release()
	status, err := result.GetStatus()
	if err != nil {
		return nil, fmt.Errorf("list characteristics: %w", err)
	}
	if statusErr := communicationError("list characteristics", status); statusErr != nil {
		return nil, statusErr
	}
	vector, err := result.GetCharacteristics()
	if err != nil {
		return nil, fmt.Errorf("list characteristics: %w", err)
	}
	defer vector.Release()
	n, err := vector.GetSize()
	if err != nil {
		return nil, fmt.Errorf("list characteristics: %w", err)
	}
	var found *gatt.GattCharacteristic
	for i := range n {
		item, itemErr := vector.GetAt(i)
		if itemErr != nil || item == nil {
			continue
		}
		ch := (*gatt.GattCharacteristic)(item)
		g, uuidErr := ch.GetUuid()
		if found == nil && uuidErr == nil && strings.EqualFold(uuidFromGUID(g), charUUID) {
			found = ch
			continue
		}
		ch.Release()
	}
	if found == nil {
		return nil, ErrNotFound
	}
	return found, nil
}

// remoteChar is the Windows RemoteCharacteristic.
type remoteChar struct {
	d *device
	c *gatt.GattCharacteristic
}

// configure writes the client characteristic configuration descriptor,
// which is how notifications are turned on and off.
func (rc *remoteChar) configure(
	ctx context.Context, value gatt.GattClientCharacteristicConfigurationDescriptorValue,
) error {
	op, err := rc.c.WriteClientCharacteristicConfigurationDescriptorAsync(value)
	if err != nil {
		return fmt.Errorf("configure notifications: %w", err)
	}
	defer op.Release()
	if err = await(ctx, op, gatt.SignatureGattCommunicationStatus); err != nil {
		return fmt.Errorf("configure notifications: %w", err)
	}
	res, err := op.GetResults()
	if err != nil {
		return fmt.Errorf("configure notifications: %w", err)
	}
	//nolint:gosec // the result of this operation is an enum value, not a pointer
	return communicationError("configure notifications", gatt.GattCommunicationStatus(uintptr(res)))
}

// Subscribe enables notifications and streams values until ctx ends or the
// device disconnects.
func (rc *remoteChar) Subscribe(ctx context.Context) (<-chan []byte, error) {
	out := make(chan []byte, notifyBuffer)
	var (
		mu     syncutil.Mutex
		closed bool
	)
	handler := typedHandler(gatt.SignatureGattCharacteristic, gatt.SignatureGattValueChangedEventArgs,
		func(_, args unsafe.Pointer) {
			buf, err := (*gatt.GattValueChangedEventArgs)(args).GetCharacteristicValue()
			if err != nil || buf == nil {
				return
			}
			value, err := bufferBytes(buf)
			buf.Release()
			if err != nil {
				return
			}
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
	token, err := rc.c.AddValueChanged(handler)
	if err != nil {
		handler.Release()
		return nil, fmt.Errorf("watch notifications: %w", err)
	}
	if err = rc.configure(ctx, gatt.GattClientCharacteristicConfigurationDescriptorValueNotify); err != nil {
		_ = rc.c.RemoveValueChanged(token)
		handler.Release()
		return nil, err
	}

	go func() {
		select {
		case <-ctx.Done():
			rc.stopNotify()
		case <-rc.d.disconnected:
		}
		_ = rc.c.RemoveValueChanged(token)
		mu.Lock()
		closed = true
		close(out)
		mu.Unlock()
		handler.Release()
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
	buf, err := bytesBuffer(value)
	if err != nil {
		return err
	}
	defer buf.Release()
	option := gatt.GattWriteOptionWriteWithoutResponse
	if withResponse {
		option = gatt.GattWriteOptionWriteWithResponse
	}
	op, err := rc.c.WriteValueWithOptionAsync(buf, option)
	if err != nil {
		return fmt.Errorf("write characteristic: %w", err)
	}
	defer op.Release()
	if err = await(ctx, op, gatt.SignatureGattCommunicationStatus); err != nil {
		return fmt.Errorf("write characteristic: %w", err)
	}
	res, err := op.GetResults()
	if err != nil {
		return fmt.Errorf("write characteristic: %w", err)
	}
	//nolint:gosec // the result of this operation is an enum value, not a pointer
	return communicationError("write characteristic", gatt.GattCommunicationStatus(uintptr(res)))
}

// Read reads the characteristic's value from the device.
func (rc *remoteChar) Read(ctx context.Context) ([]byte, error) {
	op, err := rc.c.ReadValueWithCacheModeAsync(bluetooth.BluetoothCacheModeUncached)
	if err != nil {
		return nil, fmt.Errorf("read characteristic: %w", err)
	}
	defer op.Release()
	if err = await(ctx, op, gatt.SignatureGattReadResult); err != nil {
		return nil, fmt.Errorf("read characteristic: %w", err)
	}
	res, err := op.GetResults()
	if err != nil || res == nil {
		return nil, fmt.Errorf("read characteristic: no result: %w", err)
	}
	result := (*gatt.GattReadResult)(res)
	defer result.Release()
	status, err := result.GetStatus()
	if err != nil {
		return nil, fmt.Errorf("read characteristic: %w", err)
	}
	if statusErr := communicationError("read characteristic", status); statusErr != nil {
		return nil, statusErr
	}
	buf, err := result.GetValue()
	if err != nil {
		return nil, fmt.Errorf("read characteristic: %w", err)
	}
	defer releaseBuffer(buf)
	return bufferBytes(buf)
}

func releaseBuffer(buf *streams.IBuffer) {
	if buf != nil {
		buf.Release()
	}
}

// MTU reports the ATT MTU negotiated for the link.
func (rc *remoteChar) MTU(_ context.Context) (int, error) {
	rc.d.mu.Lock()
	session := rc.d.session
	rc.d.mu.Unlock()
	if session == nil {
		return 0, nil
	}
	size, err := session.GetMaxPduSize()
	if err != nil {
		return 0, fmt.Errorf("read mtu: %w", err)
	}
	return int(size), nil
}
