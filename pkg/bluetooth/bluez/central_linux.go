//go:build linux

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

package bluez

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/rs/zerolog/log"
)

const (
	// connectTimeout bounds Device1.Connect, which bluetoothd may hold for
	// its own connection attempt window.
	connectTimeout = 30 * time.Second

	bluezErrInProgress = "org.bluez.Error.InProgress"

	// How often, and how many times, starting discovery is retried while
	// bluetoothd is still stopping the previous session.
	discoveryStartRetry    = 200 * time.Millisecond
	discoveryStartAttempts = 10

	// scanBuffer is how many scan results a consumer may leave unread
	// before the oldest are dropped.
	scanBuffer = 64

	// notifyBuffer is how many notifications a subscriber may leave unread
	// before the stream blocks bluetoothd's signal delivery to us.
	notifyBuffer = 64
)

// central is the Linux Central.
type central struct {
	a *adapter
}

// Scan reports matching devices in range until ctx ends.
func (c *central) Scan(ctx context.Context, filter ScanFilter) (<-chan ScanResult, error) {
	return c.discover(ctx, filter.ServiceUUIDs, func(r *ScanResult) bool {
		// A device BlueZ only remembers has no signal strength; it is
		// reported once it is actually heard.
		return r.HasRSSI && advertisesAny(r.ServiceUUIDs, filter.ServiceUUIDs)
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

// discover runs one discovery session, handing bluetoothd serviceUUIDs as its
// filter and reporting every device that passes match.
func (c *central) discover(
	ctx context.Context, serviceUUIDs []string, match func(*ScanResult) bool,
) (<-chan ScanResult, error) {
	prefix := c.a.devicePathPrefix()
	events, unsubscribe := c.a.signals.subscribe(func(sig *dbus.Signal) bool {
		if !strings.HasPrefix(string(signalObject(sig)), prefix) {
			return false
		}
		if _, ifaces, ok := interfacesAdded(sig); ok {
			_, isDevice := ifaces[deviceIface]
			return isDevice
		}
		iface, _, ok := propertiesChanged(sig)
		return ok && iface == deviceIface
	})

	filter := map[string]dbus.Variant{"Transport": dbus.MakeVariant("le")}
	if len(serviceUUIDs) > 0 {
		filter["UUIDs"] = dbus.MakeVariant(serviceUUIDs)
	}
	if err := c.a.call(ctx, c.a.obj, adapterIface+".SetDiscoveryFilter", filter); err != nil {
		unsubscribe()
		return nil, err
	}
	if err := c.startDiscovery(ctx); err != nil {
		unsubscribe()
		return nil, err
	}
	// Read after subscribing, so a device that appears in between is in
	// one or the other.
	objs, err := c.a.managedObjects(ctx)
	if err != nil {
		c.stopDiscovery()
		unsubscribe()
		return nil, err
	}

	out := make(chan ScanResult, scanBuffer)
	go func() {
		defer close(out)
		defer c.stopDiscovery()
		defer unsubscribe()

		seen := make(map[dbus.ObjectPath]*ScanResult)
		update := func(path dbus.ObjectPath, props map[string]dbus.Variant) {
			r := seen[path]
			if r == nil {
				r = &ScanResult{Address: addressFromPath(path)}
				seen[path] = r
			}
			r.merge(props)
			if match(r) {
				emitScanResult(out, r)
			}
		}
		for path, ifaces := range objs {
			if props, ok := ifaces[deviceIface]; ok && strings.HasPrefix(string(path), prefix) {
				update(path, props)
			}
		}
		for {
			select {
			case <-ctx.Done():
				return
			case <-c.a.gone:
				return
			case sig, ok := <-events:
				if !ok {
					return
				}
				if path, ifaces, isAdded := interfacesAdded(sig); isAdded {
					update(path, ifaces[deviceIface])
				} else if _, changed, isChanged := propertiesChanged(sig); isChanged {
					update(sig.Path, changed)
				}
			}
		}
	}()
	return out, nil
}

// startDiscovery starts our discovery session. A session that was only just
// stopped is still winding down inside bluetoothd for a moment, during
// which a new one is refused as already in progress, so that is retried.
func (c *central) startDiscovery(ctx context.Context) error {
	var err error
	for range discoveryStartAttempts {
		err = c.a.call(ctx, c.a.obj, adapterIface+".StartDiscovery")
		var dbusErr dbus.Error
		if err == nil || !errors.As(err, &dbusErr) || dbusErr.Name != bluezErrInProgress {
			return err
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("start discovery: %w", ctx.Err())
		case <-time.After(discoveryStartRetry):
		}
	}
	return err
}

// emitScanResult delivers r, making room by dropping the oldest undelivered
// result when the consumer is behind: a newer sighting is worth more.
func emitScanResult(out chan ScanResult, r *ScanResult) {
	for {
		select {
		case out <- r.clone():
			return
		default:
		}
		select {
		case <-out:
		default:
		}
	}
}

// merge folds Device1 properties into the result.
func (r *ScanResult) merge(props map[string]dbus.Variant) {
	if v := stringProp(props, "Address"); v != "" {
		r.Address = v
	}
	if v := stringProp(props, "Alias"); v != "" && r.Name == "" {
		r.Name = v
	}
	if v := stringProp(props, "Name"); v != "" {
		r.Name = v
	}
	if v, ok := props["UUIDs"].Value().([]string); ok {
		r.ServiceUUIDs = v
	}
	if v, ok := props["RSSI"].Value().(int16); ok {
		r.RSSI = v
		r.HasRSSI = true
	}
	if v, ok := props["ManufacturerData"].Value().(map[uint16]dbus.Variant); ok {
		r.ManufacturerData = make(map[uint16][]byte, len(v))
		for id, data := range v {
			if b, isBytes := data.Value().([]byte); isBytes {
				r.ManufacturerData[id] = b
			}
		}
	}
	if v, ok := props["ServiceData"].Value().(map[string]dbus.Variant); ok {
		r.ServiceData = make(map[string][]byte, len(v))
		for uuid, data := range v {
			if b, isBytes := data.Value().([]byte); isBytes {
				r.ServiceData[uuid] = b
			}
		}
	}
}

// clone copies the result so the consumer never shares the scan's maps.
func (r *ScanResult) clone() ScanResult {
	out := *r
	out.ServiceUUIDs = slices.Clone(r.ServiceUUIDs)
	out.ManufacturerData = make(map[uint16][]byte, len(r.ManufacturerData))
	for id, data := range r.ManufacturerData {
		out.ManufacturerData[id] = slices.Clone(data)
	}
	out.ServiceData = make(map[string][]byte, len(r.ServiceData))
	for uuid, data := range r.ServiceData {
		out.ServiceData[uuid] = slices.Clone(data)
	}
	return out
}

// Find scans until the device with the given address is heard, unless it is
// already connected. The scan is not filtered by service: the address is
// what identifies the device, and plenty of devices leave their services
// out of the advertisement.
func (c *central) Find(ctx context.Context, address string) (Device, error) {
	addr, err := NormalizeAddress(address)
	if err != nil {
		return nil, err
	}
	path := dbus.ObjectPath(c.a.devicePathPrefix() + strings.ReplaceAll(addr, ":", "_"))

	connected, err := c.connected(ctx, path)
	if err != nil {
		return nil, err
	}
	if connected {
		return c.a.newDevice(path, addr), nil
	}

	scanCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	results, err := c.discover(scanCtx, nil, func(r *ScanResult) bool {
		return r.HasRSSI && strings.EqualFold(r.Address, addr)
	})
	if err != nil {
		return nil, err
	}
	if _, found := <-results; found {
		return c.a.newDevice(path, addr), nil
	}
	if ctx.Err() != nil {
		return nil, fmt.Errorf("find %s: %w", addr, ctx.Err())
	}
	return nil, ErrUnavailable
}

// connected reports whether bluetoothd has the device at path connected.
func (c *central) connected(ctx context.Context, path dbus.ObjectPath) (bool, error) {
	objs, err := c.a.managedObjects(ctx)
	if err != nil {
		return false, err
	}
	props, ok := objs[path][deviceIface]
	if !ok {
		return false, nil
	}
	connected, _ := props["Connected"].Value().(bool)
	return connected, nil
}

// stopDiscovery releases our discovery session; failures are expected when
// bluetoothd already stopped it.
func (c *central) stopDiscovery() {
	ctx, cancel := context.WithTimeout(context.Background(), c.a.callTimeout)
	defer cancel()
	if err := c.a.call(ctx, c.a.obj, adapterIface+".StopDiscovery"); err != nil {
		log.Debug().Err(err).Msg("bluetooth stop discovery failed")
	}
}

// device is the Linux Device.
type device struct {
	a            *adapter
	obj          dbus.BusObject
	disconnected chan struct{}
	path         dbus.ObjectPath
	address      string
	dropOnce     sync.Once
}

func (a *adapter) newDevice(path dbus.ObjectPath, address string) *device {
	d := &device{
		a:            a,
		obj:          a.conn.Object(bluezService, path),
		disconnected: make(chan struct{}),
		path:         path,
		address:      address,
	}
	go d.watch()
	return d
}

// watch closes disconnected once bluetoothd reports the link down or the
// device object vanishes.
func (d *device) watch() {
	events, unsubscribe := d.a.signals.subscribe(func(sig *dbus.Signal) bool {
		return signalObject(sig) == d.path &&
			(sig.Name == signalPropertiesChanged || sig.Name == signalInterfacesRemoved)
	})
	defer unsubscribe()
	for {
		select {
		case <-d.a.gone:
			d.drop()
			return
		case <-d.disconnected:
			return
		case sig, ok := <-events:
			if !ok {
				d.drop()
				return
			}
			if iface, changed, ok := propertiesChanged(sig); ok && iface == deviceIface {
				if connected, present := changedBool(changed, "Connected"); present && !connected {
					d.drop()
					return
				}
				continue
			}
			if _, ifaces, ok := interfacesRemoved(sig); ok && slices.Contains(ifaces, deviceIface) {
				d.drop()
				return
			}
		}
	}
}

func (d *device) drop() {
	d.dropOnce.Do(func() { close(d.disconnected) })
}

func (d *device) Address() string { return d.address }

func (d *device) Disconnected() <-chan struct{} { return d.disconnected }

// Connect connects and waits until bluetoothd has resolved the device's
// services, so Characteristic can find them.
func (d *device) Connect(ctx context.Context) error {
	changes, unsubscribe := d.a.signals.subscribe(func(sig *dbus.Signal) bool {
		return sig.Path == d.path && sig.Name == signalPropertiesChanged
	})
	defer unsubscribe()

	cctx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()
	if call := d.obj.CallWithContext(cctx, deviceIface+".Connect", 0); call.Err != nil {
		return mapBusError("connect "+d.address, call.Err)
	}

	resolved, err := d.a.getProperty(ctx, d.obj, deviceIface, "ServicesResolved")
	if err != nil {
		return err
	}
	if v, ok := resolved.Value().(bool); ok && v {
		return nil
	}

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("resolve services of %s: %w", d.address, ctx.Err())
		case <-d.disconnected:
			return fmt.Errorf("resolve services of %s: %w", d.address, errDisconnected)
		case sig, ok := <-changes:
			if !ok {
				return ErrUnavailable
			}
			iface, changed, ok := propertiesChanged(sig)
			if !ok || iface != deviceIface {
				continue
			}
			if v, present := changedBool(changed, "ServicesResolved"); present && v {
				return nil
			}
			if v, present := changedBool(changed, "Connected"); present && !v {
				return fmt.Errorf("resolve services of %s: %w", d.address, errDisconnected)
			}
		}
	}
}

var errDisconnected = errors.New("bluez: device disconnected")

func (d *device) Disconnect(ctx context.Context) error {
	return d.a.call(ctx, d.obj, deviceIface+".Disconnect")
}

// Characteristic finds a characteristic by service and characteristic UUID
// among the device's resolved services.
func (d *device) Characteristic(serviceUUID, charUUID string) (RemoteCharacteristic, error) {
	ctx, cancel := context.WithTimeout(context.Background(), d.a.callTimeout)
	defer cancel()
	objs, err := d.a.managedObjects(ctx)
	if err != nil {
		return nil, err
	}

	devicePrefix := string(d.path) + "/"
	var servicePath string
	for path, ifaces := range objs {
		props, ok := ifaces[gattServiceIface]
		if !ok || !strings.HasPrefix(string(path), devicePrefix) {
			continue
		}
		if strings.EqualFold(stringProp(props, "UUID"), serviceUUID) {
			servicePath = string(path)
			break
		}
	}
	if servicePath == "" {
		return nil, fmt.Errorf("%w: service %s on %s", ErrNotFound, serviceUUID, d.address)
	}

	for path, ifaces := range objs {
		props, ok := ifaces[gattCharIface]
		if !ok || !strings.HasPrefix(string(path), servicePath+"/") {
			continue
		}
		if strings.EqualFold(stringProp(props, "UUID"), charUUID) {
			return &remoteChar{d: d, obj: d.a.conn.Object(bluezService, path), path: path}, nil
		}
	}
	return nil, fmt.Errorf("%w: characteristic %s in service %s on %s", ErrNotFound, charUUID, serviceUUID, d.address)
}

// remoteChar is the Linux RemoteCharacteristic.
type remoteChar struct {
	d    *device
	obj  dbus.BusObject
	path dbus.ObjectPath
}

// Subscribe enables notifications and streams values until ctx ends, the
// device disconnects, or the characteristic itself goes away: a device can
// rebuild its services without dropping the link, and the old
// characteristic never speaks again.
func (rc *remoteChar) Subscribe(ctx context.Context) (<-chan []byte, error) {
	values, unsubscribe := rc.d.a.signals.subscribe(func(sig *dbus.Signal) bool {
		return signalObject(sig) == rc.path &&
			(sig.Name == signalPropertiesChanged || sig.Name == signalInterfacesRemoved)
	})
	if err := rc.d.a.call(ctx, rc.obj, gattCharIface+".StartNotify"); err != nil {
		unsubscribe()
		return nil, err
	}

	out := make(chan []byte, notifyBuffer)
	go func() {
		defer close(out)
		defer unsubscribe()
		defer rc.stopNotify()
		for {
			select {
			case <-ctx.Done():
				return
			case <-rc.d.disconnected:
				return
			case sig, ok := <-values:
				if !ok {
					return
				}
				if _, ifaces, removed := interfacesRemoved(sig); removed {
					if slices.Contains(ifaces, gattCharIface) {
						return
					}
					continue
				}
				iface, changed, ok := propertiesChanged(sig)
				if !ok || iface != gattCharIface {
					continue
				}
				value, present := changedBytes(changed, "Value")
				if !present {
					continue
				}
				select {
				case out <- append([]byte(nil), value...):
				case <-ctx.Done():
					return
				case <-rc.d.disconnected:
					return
				}
			}
		}
	}()
	return out, nil
}

// Read reads the characteristic's value from the device.
func (rc *remoteChar) Read(ctx context.Context) ([]byte, error) {
	cctx, cancel := rc.d.a.callCtx(ctx)
	defer cancel()
	call := rc.obj.CallWithContext(cctx, gattCharIface+".ReadValue", 0, map[string]dbus.Variant{})
	if call.Err != nil {
		return nil, mapBusError("read characteristic", call.Err)
	}
	var value []byte
	if err := call.Store(&value); err != nil {
		return nil, fmt.Errorf("decode characteristic value: %w", err)
	}
	return value, nil
}

// MTU reports the negotiated ATT MTU, or 0 when this bluetoothd does not
// expose it.
func (rc *remoteChar) MTU(ctx context.Context) (int, error) {
	v, err := rc.d.a.getProperty(ctx, rc.obj, gattCharIface, "MTU")
	if err != nil {
		if errors.Is(err, ErrUnavailable) || errors.Is(err, ErrNotFound) || ctx.Err() != nil {
			return 0, err
		}
		return 0, nil
	}
	mtu, ok := v.Value().(uint16)
	if !ok {
		return 0, nil
	}
	return int(mtu), nil
}

// stopNotify is best effort: a disconnected device has already stopped.
func (rc *remoteChar) stopNotify() {
	select {
	case <-rc.d.disconnected:
		return
	default:
	}
	ctx, cancel := context.WithTimeout(context.Background(), rc.d.a.callTimeout)
	defer cancel()
	if err := rc.d.a.call(ctx, rc.obj, gattCharIface+".StopNotify"); err != nil {
		log.Debug().Err(err).Msg("bluetooth stop notify failed")
	}
}

// Write sends value as a write command or, with withResponse, a write
// request that waits for the peripheral's acknowledgement.
func (rc *remoteChar) Write(ctx context.Context, value []byte, withResponse bool) error {
	kind := "command"
	if withResponse {
		kind = "request"
	}
	options := map[string]dbus.Variant{"type": dbus.MakeVariant(kind)}
	return rc.d.a.call(ctx, rc.obj, gattCharIface+".WriteValue", value, options)
}
