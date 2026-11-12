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
	"errors"
	"fmt"
	"sync"
	"syscall"

	"github.com/go-ole/go-ole"
	"github.com/jonboulle/clockwork"
	"github.com/saltosystems/winrt-go/windows/devices/bluetooth"
	gatt "github.com/saltosystems/winrt-go/windows/devices/bluetooth/genericattributeprofile"
)

// The Windows backend reaches WinRT only through the interfaces below.
// winrt_windows.go implements them over the real API; tests use fakes, so
// the backend's own logic runs without a radio.

// winHost is the entry point into the Windows Bluetooth API.
type winHost interface {
	// createServiceProvider creates a GATT service provider for the
	// service. A non-success code comes with a nil provider.
	createServiceProvider(ctx context.Context, serviceUUID string) (serviceProvider, bluetooth.BluetoothError, error)
	// newWatcher creates an active advertisement watcher that reports
	// every packet heard to onReceived until it is unregistered.
	newWatcher(onReceived func(ScanResult)) (advWatcher, error)
	// lookupDevice returns the device object of an address, or nil, nil
	// when Windows has never seen it.
	lookupDevice(ctx context.Context, address string) (leDevice, error)
}

// eventHandle is a registered WinRT event handler. remove unregisters it;
// release frees the handler once no callback can still be running.
type eventHandle interface {
	remove()
	release()
}

// serviceProvider is a GattServiceProvider and its local service.
type serviceProvider interface {
	createCharacteristic(
		ctx context.Context, uuid string, props gatt.GattCharacteristicProperties,
	) (localCharacteristic, bluetooth.BluetoothError, error)
	// startAdvertising starts connectable, discoverable advertising.
	startAdvertising() error
	advertisementStatus() (gatt.GattServiceProviderAdvertisementStatus, error)
	stopAdvertising() error
	release()
}

// charEvents are the callbacks of one local characteristic. write is
// called off the WinRT callback, with the request held open until it
// returns; read and subscribersChanged are called on it.
type charEvents struct {
	write              func(writeEvent)
	read               func(readEvent)
	subscribersChanged func()
}

// localCharacteristic is a GattLocalCharacteristic.
type localCharacteristic interface {
	// watch registers events and returns the function that unregisters
	// them.
	watch(events charEvents) (unwatch func(), err error)
	// notifyAll sends value to every subscriber and waits for it to go
	// out.
	notifyAll(ctx context.Context, value []byte) error
	// notifyClient queues value for one subscriber. wait blocks until it
	// has gone out and frees the operation; it must be called once.
	notifyClient(value []byte, client subscribedClient) (wait func(context.Context) error, err error)
	subscribedClients() ([]subscribedClient, error)
	release()
}

// subscribedClient is a GattSubscribedClient.
type subscribedClient interface {
	deviceID() (string, error)
	release()
}

// serverSession is the GattSession of a central connected to the local
// service.
type serverSession interface {
	deviceID() (string, error)
	maxPduSize() (uint16, error)
	// onClosed calls fn when the session closes.
	onClosed(fn func()) (eventHandle, error)
	release()
}

// writeEvent is a GattWriteRequestedEventArgs.
type writeEvent interface {
	session() (serverSession, error)
	request(ctx context.Context) (writeRequest, error)
}

// writeRequest is a GattWriteRequest.
type writeRequest interface {
	value() ([]byte, error)
	option() (gatt.GattWriteOption, error)
	respond() error
	release()
}

// readEvent is a GattReadRequestedEventArgs.
type readEvent interface {
	session() (serverSession, error)
	request(ctx context.Context) (readRequest, error)
}

// readRequest is a GattReadRequest.
type readRequest interface {
	offset() (uint32, error)
	// respondWithValue answers with value. It fails when the value could
	// not be prepared or Windows refused the answer; either way the client
	// has no value and is still owed a reply.
	respondWithValue(value []byte) error
	respondWithError(code byte) error
	release()
}

// advWatcher is a BluetoothLEAdvertisementWatcher with its handler
// registered.
type advWatcher interface {
	start() error
	stop() error
	// unregister removes the received handler; release frees the handler
	// and the watcher.
	unregister()
	release()
}

// leDevice is a BluetoothLEDevice.
type leDevice interface {
	// openSession opens a GATT session that keeps the link up.
	openSession(ctx context.Context) (clientSession, error)
	// onConnectionStatusChanged calls fn with each new connection status.
	onConnectionStatusChanged(fn func(bluetooth.BluetoothConnectionStatus)) (eventHandle, error)
	// discoverServices lists the device's services, uncached. Services are
	// only listed when the status is success.
	discoverServices(ctx context.Context) (gatt.GattCommunicationStatus, []remoteService, error)
	close()
}

// clientSession is the GattSession held to a remote device.
type clientSession interface {
	maxPduSize() (uint16, error)
	close()
}

// remoteService is a GattDeviceService.
type remoteService interface {
	uuid() (string, error)
	// characteristics lists the service's characteristics, uncached. They
	// are only listed when the status is success.
	characteristics(ctx context.Context) (gatt.GattCommunicationStatus, []gattCharacteristic, error)
	close()
}

// gattCharacteristic is a remote GattCharacteristic.
type gattCharacteristic interface {
	uuid() (string, error)
	configure(
		ctx context.Context, value gatt.GattClientCharacteristicConfigurationDescriptorValue,
	) (gatt.GattCommunicationStatus, error)
	onValueChanged(fn func([]byte)) (eventHandle, error)
	write(ctx context.Context, value []byte, option gatt.GattWriteOption) (gatt.GattCommunicationStatus, error)
	// read reads the value, uncached. The value is only read when the
	// status is success.
	read(ctx context.Context) (gatt.GattCommunicationStatus, []byte, error)
	release()
}

// adapter is the Windows Adapter. The WinRT API has no adapter object to
// hold: it always works on the system's default radio, and says whether
// there is one, and what it can do, only when a role is actually used.
type adapter struct {
	host     winHost
	clock    clockwork.Clock
	gone     chan struct{}
	goneOnce sync.Once
}

func newAdapter(host winHost, clock clockwork.Clock) *adapter {
	return &adapter{host: host, clock: clock, gone: make(chan struct{})}
}

// Open prepares the Windows Bluetooth API. Whether a radio is present, on,
// and able to take a role is reported by Serve and by the central calls.
func Open(_ context.Context, opts ...Option) (Adapter, error) {
	_ = applyOptions(opts)
	if err := ensureRuntime(); err != nil {
		return nil, err
	}
	return newAdapter(winrtHost{}, clockwork.NewRealClock()), nil
}

func (*adapter) Address() string { return "" }

func (*adapter) Roles() []Role { return nil }

func (a *adapter) Gone() <-chan struct{} { return a.gone }

func (a *adapter) Peripheral() (Peripheral, error) { return newPeripheral(a), nil }

func (a *adapter) Central() (Central, error) { return &central{a: a}, nil }

func (a *adapter) markGone() {
	a.goneOnce.Do(func() { close(a.gone) })
}

func (a *adapter) Close() error {
	a.markGone()
	return nil
}

// bluetoothError turns a WinRT BluetoothError into one of this package's
// errors, or nil for success.
func bluetoothError(op string, code bluetooth.BluetoothError) error {
	switch code {
	case bluetooth.BluetoothErrorSuccess:
		return nil
	case bluetooth.BluetoothErrorRadioNotAvailable:
		return fmt.Errorf("%w: %s: no bluetooth radio available", ErrNoAdapter, op)
	case bluetooth.BluetoothErrorDisabledByUser, bluetooth.BluetoothErrorDisabledByPolicy,
		bluetooth.BluetoothErrorConsentRequired:
		return fmt.Errorf("%w: %s: bluetooth is turned off or not permitted (error %d)", ErrUnavailable, op, code)
	case bluetooth.BluetoothErrorNotSupported, bluetooth.BluetoothErrorTransportNotSupported:
		return fmt.Errorf("%w: %s", ErrRoleUnsupported, op)
	default:
		return fmt.Errorf("%s: bluetooth error %d", op, code)
	}
}

// communicationError turns a GATT status into an error, or nil for success.
func communicationError(op string, status gatt.GattCommunicationStatus) error {
	switch status {
	case gatt.GattCommunicationStatusSuccess:
		return nil
	case gatt.GattCommunicationStatusUnreachable:
		return fmt.Errorf("%s: %w", op, errDisconnected)
	default:
		return fmt.Errorf("%s: gatt status %d", op, status)
	}
}

var errDisconnected = errors.New("bluetooth: device disconnected")

// guidFromUUID converts a textual UUID into the GUID the API takes.
func guidFromUUID(uuid string) (syscall.GUID, error) {
	g := ole.NewGUID(uuid)
	if g == nil {
		return syscall.GUID{}, fmt.Errorf("invalid uuid %q", uuid)
	}
	return syscall.GUID{Data1: g.Data1, Data2: g.Data2, Data3: g.Data3, Data4: g.Data4}, nil
}

// uuidFromGUID is the inverse of guidFromUUID, in lower case.
func uuidFromGUID(g syscall.GUID) string {
	return fmt.Sprintf("%08x-%04x-%04x-%02x%02x-%02x%02x%02x%02x%02x%02x",
		g.Data1, g.Data2, g.Data3,
		g.Data4[0], g.Data4[1], g.Data4[2], g.Data4[3], g.Data4[4], g.Data4[5], g.Data4[6], g.Data4[7])
}
