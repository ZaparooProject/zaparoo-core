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

	// notifyBuffer is how many notifications a subscriber may leave unread
	// before the stream blocks bluetoothd's signal delivery to us.
	notifyBuffer = 64
)

// central is the Linux Central.
type central struct {
	a *adapter
}

// Find scans until the device with the given address appears.
func (c *central) Find(ctx context.Context, address string, serviceUUIDs []string) (Device, error) {
	addr, err := NormalizeAddress(address)
	if err != nil {
		return nil, err
	}

	if dev, err := c.knownDevice(ctx, addr); err != nil || dev != nil {
		return dev, err
	}

	prefix := c.a.devicePathPrefix()
	added, unsubscribe := c.a.signals.subscribe(func(sig *dbus.Signal) bool {
		path, ifaces, ok := interfacesAdded(sig)
		if !ok {
			return false
		}
		_, isDevice := ifaces[deviceIface]
		return isDevice && strings.HasPrefix(string(path), prefix)
	})
	defer unsubscribe()

	filter := map[string]dbus.Variant{"Transport": dbus.MakeVariant("le")}
	if len(serviceUUIDs) > 0 {
		filter["UUIDs"] = dbus.MakeVariant(serviceUUIDs)
	}
	if err := c.a.call(ctx, c.a.obj, adapterIface+".SetDiscoveryFilter", filter); err != nil {
		return nil, err
	}
	if err := c.a.call(ctx, c.a.obj, adapterIface+".StartDiscovery"); err != nil {
		return nil, err
	}
	defer c.stopDiscovery()

	// A device that appeared between the first lookup and the subscription
	// would otherwise be missed.
	if dev, err := c.knownDevice(ctx, addr); err != nil || dev != nil {
		return dev, err
	}

	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("find %s: %w", addr, ctx.Err())
		case <-c.a.gone:
			return nil, ErrUnavailable
		case sig, ok := <-added:
			if !ok {
				return nil, ErrUnavailable
			}
			path, ifaces, _ := interfacesAdded(sig)
			if strings.EqualFold(stringProp(ifaces[deviceIface], "Address"), addr) {
				return c.a.newDevice(path, addr), nil
			}
		}
	}
}

// knownDevice returns the device if bluetoothd already has an object for it.
func (c *central) knownDevice(ctx context.Context, addr string) (Device, error) {
	objs, err := c.a.managedObjects(ctx)
	if err != nil {
		return nil, err
	}
	prefix := c.a.devicePathPrefix()
	for path, ifaces := range objs {
		props, ok := ifaces[deviceIface]
		if !ok || !strings.HasPrefix(string(path), prefix) {
			continue
		}
		if strings.EqualFold(stringProp(props, "Address"), addr) {
			return c.a.newDevice(path, addr), nil
		}
	}
	return nil, nil //nolint:nilnil // nil device means not known yet, not an error
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
		return sig.Path == d.path && (sig.Name == signalPropertiesChanged || sig.Name == signalInterfacesRemoved)
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
			if _, _, ok := interfacesRemoved(sig); ok {
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

// Subscribe enables notifications and streams values until ctx ends or the
// device disconnects.
func (rc *remoteChar) Subscribe(ctx context.Context) (<-chan []byte, error) {
	values, unsubscribe := rc.d.a.signals.subscribe(func(sig *dbus.Signal) bool {
		return sig.Path == rc.path && sig.Name == signalPropertiesChanged
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
