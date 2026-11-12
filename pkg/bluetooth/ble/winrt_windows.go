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
	"runtime"
	"sync"
	"syscall"
	"unsafe"

	"github.com/go-ole/go-ole"
	"github.com/saltosystems/winrt-go"
	"github.com/saltosystems/winrt-go/windows/devices/bluetooth"
	"github.com/saltosystems/winrt-go/windows/devices/bluetooth/advertisement"
	gatt "github.com/saltosystems/winrt-go/windows/devices/bluetooth/genericattributeprofile"
	"github.com/saltosystems/winrt-go/windows/foundation"
	"github.com/saltosystems/winrt-go/windows/foundation/collections"
	"github.com/saltosystems/winrt-go/windows/storage/streams"
)

// This file implements the interfaces in backend_windows.go over the real
// Windows Runtime. Each method translates directly to the API and decides
// nothing of its own.

const (
	// roInitMultiThreaded is RO_INIT_MULTITHREADED.
	roInitMultiThreaded = 1
	// hresultFalse (S_FALSE) and hresultChangedMode (RPC_E_CHANGED_MODE)
	// both mean the runtime was already initialised on this thread, which
	// is fine.
	hresultFalse       = 0x00000001
	hresultChangedMode = 0x80010106
)

var (
	runtimeOnce sync.Once
	errRuntime  error
)

// ensureRuntime initialises the Windows Runtime once for the process. The
// thread that does it is parked for good: the multithreaded apartment lives
// only while a thread that joined it does, and Go threads come and go.
func ensureRuntime() error {
	runtimeOnce.Do(func() {
		ready := make(chan error, 1)
		go func() {
			runtime.LockOSThread()
			err := ole.RoInitialize(roInitMultiThreaded)
			var oleErr *ole.OleError
			if errors.As(err, &oleErr) && (oleErr.Code() == hresultFalse || oleErr.Code() == hresultChangedMode) {
				err = nil
			}
			ready <- err
			if err == nil {
				select {}
			}
		}()
		if err := <-ready; err != nil {
			errRuntime = fmt.Errorf("%w: initialise windows runtime: %w", ErrUnavailable, err)
		}
	})
	return errRuntime
}

// await waits for a WinRT async operation whose result type has the given
// signature, or for ctx to end. The operation itself cannot be cancelled
// through these bindings, so on ctx ending it is left to finish unobserved.
func await(ctx context.Context, op *foundation.IAsyncOperation, resultSignature string) error {
	iid := winrt.ParameterizedInstanceGUID(foundation.GUIDAsyncOperationCompletedHandler, resultSignature)
	done := make(chan foundation.AsyncStatus, 1)
	handler := foundation.NewAsyncOperationCompletedHandler(ole.NewGUID(iid),
		func(
			_ *foundation.AsyncOperationCompletedHandler, _ *foundation.IAsyncOperation, status foundation.AsyncStatus,
		) {
			done <- status
		})
	if err := op.SetCompleted(handler); err != nil {
		handler.Release()
		return fmt.Errorf("watch async operation: %w", err)
	}

	select {
	case status := <-done:
		handler.Release()
		if status != foundation.AsyncStatusCompleted {
			return fmt.Errorf("async operation ended with status %d", status)
		}
		return nil
	case <-ctx.Done():
		// The handler is still referenced by the operation; release our
		// reference once it has run.
		go func() {
			<-done
			handler.Release()
		}()
		return fmt.Errorf("waiting for bluetooth: %w", ctx.Err())
	}
}

// awaitResult awaits op and returns its result, which must not be nil.
func awaitResult(ctx context.Context, op *foundation.IAsyncOperation, resultSignature string) (unsafe.Pointer, error) {
	if err := await(ctx, op, resultSignature); err != nil {
		return nil, err
	}
	res, err := op.GetResults()
	if err != nil {
		return nil, fmt.Errorf("result: %w", err)
	}
	if res == nil {
		return nil, errors.New("no result")
	}
	return res, nil
}

// typedHandler builds the event handler WinRT expects for an event raised by
// a sender type with an argument type, both named by their signatures.
func typedHandler(
	senderSignature, argsSignature string, fn func(sender, args unsafe.Pointer),
) *foundation.TypedEventHandler {
	iid := winrt.ParameterizedInstanceGUID(foundation.GUIDTypedEventHandler, senderSignature, argsSignature)
	return foundation.NewTypedEventHandler(ole.NewGUID(iid),
		func(_ *foundation.TypedEventHandler, sender, args unsafe.Pointer) { fn(sender, args) })
}

// winrtEvent is a registered event handler.
type winrtEvent struct {
	handler *foundation.TypedEventHandler
	remover func()
}

func (e *winrtEvent) remove() { e.remover() }

func (e *winrtEvent) release() { e.handler.Release() }

const (
	// objectSignature is the signature of a plain object argument.
	objectSignature = "cinterface(IInspectable)"
	// collectionsVectorViewGUID is IVectorView's interface identifier, for
	// building the signature of an operation that returns one.
	collectionsVectorViewGUID = collections.GUIDIVectorView
)

// bufferBytes copies a WinRT buffer into a Go slice.
func bufferBytes(buf *streams.IBuffer) ([]byte, error) {
	if buf == nil {
		return nil, nil
	}
	n, err := buf.GetLength()
	if err != nil {
		return nil, fmt.Errorf("buffer length: %w", err)
	}
	if n == 0 {
		return nil, nil
	}
	reader, err := streams.DataReaderFromBuffer(buf)
	if err != nil {
		return nil, fmt.Errorf("read buffer: %w", err)
	}
	defer reader.Release()
	data, err := reader.ReadBytes(n)
	if err != nil {
		return nil, fmt.Errorf("read buffer: %w", err)
	}
	return data, nil
}

// bytesBuffer copies a Go slice into a new WinRT buffer the caller releases.
func bytesBuffer(data []byte) (*streams.IBuffer, error) {
	writer, err := streams.NewDataWriter()
	if err != nil {
		return nil, fmt.Errorf("new data writer: %w", err)
	}
	defer writer.Release()
	if len(data) > 0 {
		//nolint:gosec // values written here are far below 4 GiB
		if writeErr := writer.WriteBytes(uint32(len(data)), data); writeErr != nil {
			return nil, fmt.Errorf("write buffer: %w", writeErr)
		}
	}
	buf, err := writer.DetachBuffer()
	if err != nil {
		return nil, fmt.Errorf("detach buffer: %w", err)
	}
	return buf, nil
}

func releaseBuffer(buf *streams.IBuffer) {
	if buf != nil {
		buf.Release()
	}
}

// gattSessionVtbl is the start of IGattSession's method table. The bindings
// leave DeviceId out, and it is the only thing that says which device a
// session, and so a write or a subscription, belongs to.
type gattSessionVtbl struct {
	ole.IInspectableVtbl
	GetDeviceID uintptr
}

// sessionDeviceID returns the identifier of the device at the other end of a
// GATT session.
func sessionDeviceID(session *gatt.GattSession) (string, error) {
	itf, err := session.QueryInterface(ole.NewGUID(gatt.GUIDiGattSession))
	if err != nil {
		return "", fmt.Errorf("query gatt session: %w", err)
	}
	defer itf.Release()

	vtbl := (*gattSessionVtbl)(unsafe.Pointer(itf.RawVTable)) //nolint:gosec // reading a COM method table
	var id *bluetooth.BluetoothDeviceId
	hr, _, _ := syscall.SyscallN(
		vtbl.GetDeviceID,
		uintptr(unsafe.Pointer(itf)), //nolint:gosec // COM this pointer
		uintptr(unsafe.Pointer(&id)), //nolint:gosec // COM out parameter
	)
	if hr != 0 {
		return "", fmt.Errorf("session device id: %w", ole.NewError(hr))
	}
	if id == nil {
		return "", errors.New("session has no device id")
	}
	defer id.Release()
	text, err := id.GetId()
	if err != nil {
		return "", fmt.Errorf("session device id: %w", err)
	}
	return text, nil
}

// winrtHost is the winHost of the real Windows Runtime.
type winrtHost struct{}

func (winrtHost) createServiceProvider(
	ctx context.Context, serviceUUID string,
) (serviceProvider, bluetooth.BluetoothError, error) {
	svcGUID, err := guidFromUUID(serviceUUID)
	if err != nil {
		return nil, 0, err
	}
	op, err := gatt.GattServiceProviderCreateAsync(svcGUID)
	if err != nil {
		return nil, 0, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	defer op.Release()
	res, err := awaitResult(ctx, op, gatt.SignatureGattServiceProviderResult)
	if err != nil {
		return nil, 0, err
	}
	result := (*gatt.GattServiceProviderResult)(res)
	defer result.Release()
	code, err := result.GetError()
	if err != nil {
		return nil, 0, fmt.Errorf("result error: %w", err)
	}
	if code != bluetooth.BluetoothErrorSuccess {
		return nil, code, nil
	}
	provider, err := result.GetServiceProvider()
	if err != nil {
		return nil, 0, fmt.Errorf("gatt service provider: %w", err)
	}
	local, err := provider.GetService()
	if err != nil {
		provider.Release()
		return nil, 0, fmt.Errorf("gatt local service: %w", err)
	}
	return &winrtProvider{provider: provider, local: local}, code, nil
}

func (winrtHost) newWatcher(onReceived func(ScanResult)) (advWatcher, error) {
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
	received := typedHandler(
		advertisement.SignatureBluetoothLEAdvertisementWatcher,
		advertisement.SignatureBluetoothLEAdvertisementReceivedEventArgs,
		func(_, args unsafe.Pointer) {
			onReceived(scanResultFromArgs((*advertisement.BluetoothLEAdvertisementReceivedEventArgs)(args)))
		})
	token, err := watcher.AddReceived(received)
	if err != nil {
		received.Release()
		watcher.Release()
		return nil, fmt.Errorf("watch advertisements: %w", err)
	}
	return &winrtWatcher{watcher: watcher, received: received, token: token}, nil
}

func (winrtHost) lookupDevice(ctx context.Context, address string) (leDevice, error) {
	value, err := addressToUint64(address)
	if err != nil {
		return nil, err
	}
	op, err := bluetooth.BluetoothLEDeviceFromBluetoothAddressAsync(value)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	defer op.Release()
	if awaitErr := await(ctx, op, bluetooth.SignatureBluetoothLEDevice); awaitErr != nil {
		return nil, awaitErr
	}
	res, err := op.GetResults()
	if err != nil {
		return nil, fmt.Errorf("result: %w", err)
	}
	if res == nil {
		return nil, nil //nolint:nilnil // nil device means not known yet, not an error
	}
	return &winrtLEDevice{dev: (*bluetooth.BluetoothLEDevice)(res)}, nil
}

// winrtProvider is a GattServiceProvider and its local service.
type winrtProvider struct {
	provider *gatt.GattServiceProvider
	local    *gatt.GattLocalService
}

func (p *winrtProvider) createCharacteristic(
	ctx context.Context, uuid string, props gatt.GattCharacteristicProperties,
) (localCharacteristic, bluetooth.BluetoothError, error) {
	params, err := gatt.NewGattLocalCharacteristicParameters()
	if err != nil {
		return nil, 0, fmt.Errorf("characteristic parameters: %w", err)
	}
	defer params.Release()
	if err = params.SetCharacteristicProperties(props); err != nil {
		return nil, 0, fmt.Errorf("characteristic properties: %w", err)
	}
	charGUID, err := guidFromUUID(uuid)
	if err != nil {
		return nil, 0, err
	}
	op, err := p.local.CreateCharacteristicAsync(charGUID, params)
	if err != nil {
		return nil, 0, fmt.Errorf("create: %w", err)
	}
	defer op.Release()
	res, err := awaitResult(ctx, op, gatt.SignatureGattLocalCharacteristicResult)
	if err != nil {
		return nil, 0, err
	}
	result := (*gatt.GattLocalCharacteristicResult)(res)
	defer result.Release()
	code, err := result.GetError()
	if err != nil {
		return nil, 0, fmt.Errorf("result error: %w", err)
	}
	if code != bluetooth.BluetoothErrorSuccess {
		return nil, code, nil
	}
	c, err := result.GetCharacteristic()
	if err != nil {
		return nil, 0, fmt.Errorf("characteristic: %w", err)
	}
	return &winrtLocalChar{c: c}, code, nil
}

func (p *winrtProvider) startAdvertising() error {
	params, err := gatt.NewGattServiceProviderAdvertisingParameters()
	if err != nil {
		return fmt.Errorf("advertising parameters: %w", err)
	}
	defer params.Release()
	if err = params.SetIsConnectable(true); err != nil {
		return fmt.Errorf("advertising parameters: %w", err)
	}
	if err = params.SetIsDiscoverable(true); err != nil {
		return fmt.Errorf("advertising parameters: %w", err)
	}
	if err = p.provider.StartAdvertisingWithParameters(params); err != nil {
		return fmt.Errorf("%w: %w", ErrRoleUnsupported, err)
	}
	return nil
}

func (p *winrtProvider) advertisementStatus() (gatt.GattServiceProviderAdvertisementStatus, error) {
	status, err := p.provider.GetAdvertisementStatus()
	if err != nil {
		return status, fmt.Errorf("advertisement status: %w", err)
	}
	return status, nil
}

func (p *winrtProvider) stopAdvertising() error {
	if err := p.provider.StopAdvertising(); err != nil {
		return fmt.Errorf("stop advertising: %w", err)
	}
	return nil
}

func (p *winrtProvider) release() {
	p.local.Release()
	p.provider.Release()
}

// winrtLocalChar is a GattLocalCharacteristic.
type winrtLocalChar struct {
	c *gatt.GattLocalCharacteristic
}

func (lc *winrtLocalChar) watch(events charEvents) (unwatch func(), err error) {
	// Windows raises these one at a time and waits for the handler, and
	// each write takes a round trip to fetch. Handled inline, a client
	// writing without responses outruns that and writes are lost, so each
	// one is taken off the callback: the event is held with a reference
	// and a deferral, and the chunks' own sequence numbers restore order.
	writeHandler := typedHandler(gatt.SignatureGattLocalCharacteristic, gatt.SignatureGattWriteRequestedEventArgs,
		func(_, args unsafe.Pointer) {
			event := (*gatt.GattWriteRequestedEventArgs)(args)
			event.AddRef()
			deferral, deferErr := event.GetDeferral()
			go func() {
				defer event.Release()
				if deferErr == nil {
					defer func() {
						_ = deferral.Complete()
						deferral.Release()
					}()
				}
				events.write(&winrtWriteEvent{args: event})
			}()
		})
	readHandler := typedHandler(gatt.SignatureGattLocalCharacteristic, gatt.SignatureGattReadRequestedEventArgs,
		func(_, args unsafe.Pointer) {
			event := (*gatt.GattReadRequestedEventArgs)(args)
			deferral, deferErr := event.GetDeferral()
			if deferErr == nil {
				defer func() {
					_ = deferral.Complete()
					deferral.Release()
				}()
			}
			events.read(&winrtReadEvent{args: event})
		})
	subHandler := typedHandler(gatt.SignatureGattLocalCharacteristic, objectSignature,
		func(_, _ unsafe.Pointer) { events.subscribersChanged() })
	releaseHandlers := func() {
		writeHandler.Release()
		readHandler.Release()
		subHandler.Release()
	}

	writeToken, err := lc.c.AddWriteRequested(writeHandler)
	if err != nil {
		releaseHandlers()
		return nil, fmt.Errorf("writes: %w", err)
	}
	readToken, err := lc.c.AddReadRequested(readHandler)
	if err != nil {
		_ = lc.c.RemoveWriteRequested(writeToken)
		releaseHandlers()
		return nil, fmt.Errorf("reads: %w", err)
	}
	subToken, err := lc.c.AddSubscribedClientsChanged(subHandler)
	if err != nil {
		_ = lc.c.RemoveWriteRequested(writeToken)
		_ = lc.c.RemoveReadRequested(readToken)
		releaseHandlers()
		return nil, fmt.Errorf("subscriptions: %w", err)
	}
	return func() {
		_ = lc.c.RemoveWriteRequested(writeToken)
		_ = lc.c.RemoveReadRequested(readToken)
		_ = lc.c.RemoveSubscribedClientsChanged(subToken)
		releaseHandlers()
	}, nil
}

func (lc *winrtLocalChar) notifyAll(ctx context.Context, value []byte) error {
	buf, err := bytesBuffer(value)
	if err != nil {
		return err
	}
	defer buf.Release()
	op, err := lc.c.NotifyValueAsync(buf)
	if err != nil {
		return fmt.Errorf("notify value: %w", err)
	}
	defer op.Release()
	signature := fmt.Sprintf("pinterface({%s};%s)",
		collectionsVectorViewGUID, gatt.SignatureGattClientNotificationResult)
	return await(ctx, op, signature)
}

func (lc *winrtLocalChar) notifyClient(
	value []byte, client subscribedClient,
) (wait func(context.Context) error, err error) {
	sub, ok := client.(*winrtSubscribedClient)
	if !ok {
		return nil, fmt.Errorf("subscribed client of type %T", client)
	}
	buf, err := bytesBuffer(value)
	if err != nil {
		return nil, err
	}
	defer buf.Release()
	op, err := lc.c.NotifyValueForSubscribedClientAsync(buf, sub.client)
	if err != nil {
		return nil, fmt.Errorf("notify value: %w", err)
	}
	return func(ctx context.Context) error {
		defer op.Release()
		return await(ctx, op, gatt.SignatureGattClientNotificationResult)
	}, nil
}

func (lc *winrtLocalChar) subscribedClients() ([]subscribedClient, error) {
	clients, err := lc.c.GetSubscribedClients()
	if err != nil {
		return nil, fmt.Errorf("get: %w", err)
	}
	defer clients.Release()
	n, err := clients.GetSize()
	if err != nil {
		return nil, fmt.Errorf("size: %w", err)
	}
	out := make([]subscribedClient, 0, n)
	for i := range n {
		if item, itemErr := clients.GetAt(i); itemErr == nil && item != nil {
			out = append(out, &winrtSubscribedClient{client: (*gatt.GattSubscribedClient)(item)})
		}
	}
	return out, nil
}

func (lc *winrtLocalChar) release() { lc.c.Release() }

// winrtSubscribedClient is a GattSubscribedClient.
type winrtSubscribedClient struct {
	client *gatt.GattSubscribedClient
}

func (sc *winrtSubscribedClient) deviceID() (string, error) {
	session, err := sc.client.GetSession()
	if err != nil {
		return "", fmt.Errorf("session: %w", err)
	}
	defer session.Release()
	return sessionDeviceID(session)
}

func (sc *winrtSubscribedClient) release() { sc.client.Release() }

// winrtServerSession is the GattSession of a connected central.
type winrtServerSession struct {
	session *gatt.GattSession
}

func (s *winrtServerSession) deviceID() (string, error) { return sessionDeviceID(s.session) }

func (s *winrtServerSession) maxPduSize() (uint16, error) {
	size, err := s.session.GetMaxPduSize()
	if err != nil {
		return 0, fmt.Errorf("max pdu size: %w", err)
	}
	return size, nil
}

func (s *winrtServerSession) onClosed(fn func()) (eventHandle, error) {
	handler := typedHandler(gatt.SignatureGattSession, gatt.SignatureGattSessionStatusChangedEventArgs,
		func(_, args unsafe.Pointer) {
			status, statusErr := (*gatt.GattSessionStatusChangedEventArgs)(args).GetStatus()
			if statusErr == nil && status == gatt.GattSessionStatusClosed {
				fn()
			}
		})
	token, err := s.session.AddSessionStatusChanged(handler)
	if err != nil {
		handler.Release()
		return nil, fmt.Errorf("watch session: %w", err)
	}
	return &winrtEvent{
		handler: handler,
		remover: func() { _ = s.session.RemoveSessionStatusChanged(token) },
	}, nil
}

func (s *winrtServerSession) release() { s.session.Release() }

// winrtWriteEvent is a GattWriteRequestedEventArgs.
type winrtWriteEvent struct {
	args *gatt.GattWriteRequestedEventArgs
}

func (e *winrtWriteEvent) session() (serverSession, error) {
	session, err := e.args.GetSession()
	if err != nil {
		return nil, fmt.Errorf("session: %w", err)
	}
	return &winrtServerSession{session: session}, nil
}

func (e *winrtWriteEvent) request(ctx context.Context) (writeRequest, error) {
	op, err := e.args.GetRequestAsync()
	if err != nil {
		return nil, fmt.Errorf("get: %w", err)
	}
	defer op.Release()
	res, err := awaitResult(ctx, op, gatt.SignatureGattWriteRequest)
	if err != nil {
		return nil, err
	}
	return &winrtWriteRequest{req: (*gatt.GattWriteRequest)(res)}, nil
}

// winrtWriteRequest is a GattWriteRequest.
type winrtWriteRequest struct {
	req *gatt.GattWriteRequest
}

func (r *winrtWriteRequest) value() ([]byte, error) {
	buf, err := r.req.GetValue()
	if err != nil {
		return nil, fmt.Errorf("get: %w", err)
	}
	defer releaseBuffer(buf)
	return bufferBytes(buf)
}

func (r *winrtWriteRequest) option() (gatt.GattWriteOption, error) {
	option, err := r.req.GetOption()
	if err != nil {
		return option, fmt.Errorf("write option: %w", err)
	}
	return option, nil
}

func (r *winrtWriteRequest) respond() error {
	if err := r.req.Respond(); err != nil {
		return fmt.Errorf("respond: %w", err)
	}
	return nil
}

func (r *winrtWriteRequest) release() { r.req.Release() }

// winrtReadEvent is a GattReadRequestedEventArgs.
type winrtReadEvent struct {
	args *gatt.GattReadRequestedEventArgs
}

func (e *winrtReadEvent) session() (serverSession, error) {
	session, err := e.args.GetSession()
	if err != nil {
		return nil, fmt.Errorf("session: %w", err)
	}
	return &winrtServerSession{session: session}, nil
}

func (e *winrtReadEvent) request(ctx context.Context) (readRequest, error) {
	op, err := e.args.GetRequestAsync()
	if err != nil {
		return nil, fmt.Errorf("get: %w", err)
	}
	defer op.Release()
	res, err := awaitResult(ctx, op, gatt.SignatureGattReadRequest)
	if err != nil {
		return nil, err
	}
	return &winrtReadRequest{req: (*gatt.GattReadRequest)(res)}, nil
}

// winrtReadRequest is a GattReadRequest.
type winrtReadRequest struct {
	req *gatt.GattReadRequest
}

func (r *winrtReadRequest) offset() (uint32, error) {
	offset, err := r.req.GetOffset()
	if err != nil {
		return 0, fmt.Errorf("offset: %w", err)
	}
	return offset, nil
}

func (r *winrtReadRequest) respondWithValue(value []byte) error {
	buf, err := bytesBuffer(value)
	if err != nil {
		return err
	}
	defer buf.Release()
	if err := r.req.RespondWithValue(buf); err != nil {
		return fmt.Errorf("respond with value: %w", err)
	}
	return nil
}

func (r *winrtReadRequest) respondWithError(code byte) error {
	if err := r.req.RespondWithProtocolError(code); err != nil {
		return fmt.Errorf("respond: %w", err)
	}
	return nil
}

func (r *winrtReadRequest) release() { r.req.Release() }

// winrtWatcher is a BluetoothLEAdvertisementWatcher with its handler.
type winrtWatcher struct {
	watcher  *advertisement.BluetoothLEAdvertisementWatcher
	received *foundation.TypedEventHandler
	token    foundation.EventRegistrationToken
}

func (w *winrtWatcher) start() error {
	if err := w.watcher.Start(); err != nil {
		return fmt.Errorf("start watcher: %w", err)
	}
	return nil
}

func (w *winrtWatcher) stop() error {
	if err := w.watcher.Stop(); err != nil {
		return fmt.Errorf("stop watcher: %w", err)
	}
	return nil
}

func (w *winrtWatcher) unregister() { _ = w.watcher.RemoveReceived(w.token) }

func (w *winrtWatcher) release() {
	w.received.Release()
	w.watcher.Release()
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
		releaseBuffer(buf)
		entry.Release()
	}
	return out
}

// winrtLEDevice is a BluetoothLEDevice.
type winrtLEDevice struct {
	dev *bluetooth.BluetoothLEDevice
}

func (d *winrtLEDevice) openSession(ctx context.Context) (clientSession, error) {
	id, err := d.dev.GetBluetoothDeviceId()
	if err != nil {
		return nil, fmt.Errorf("device id: %w", err)
	}
	defer id.Release()

	op, err := gatt.GattSessionFromDeviceIdAsync(id)
	if err != nil {
		return nil, fmt.Errorf("gatt session: %w", err)
	}
	defer op.Release()
	res, err := awaitResult(ctx, op, gatt.SignatureGattSession)
	if err != nil {
		return nil, fmt.Errorf("gatt session: %w", err)
	}
	session := (*gatt.GattSession)(res)
	if err = session.SetMaintainConnection(true); err != nil {
		session.Release()
		return nil, fmt.Errorf("maintain connection: %w", err)
	}
	return &winrtClientSession{session: session}, nil
}

func (d *winrtLEDevice) onConnectionStatusChanged(
	fn func(bluetooth.BluetoothConnectionStatus),
) (eventHandle, error) {
	handler := typedHandler(bluetooth.SignatureBluetoothLEDevice, objectSignature, func(_, _ unsafe.Pointer) {
		if status, statusErr := d.dev.GetConnectionStatus(); statusErr == nil {
			fn(status)
		}
	})
	token, err := d.dev.AddConnectionStatusChanged(handler)
	if err != nil {
		handler.Release()
		return nil, fmt.Errorf("add handler: %w", err)
	}
	return &winrtEvent{
		handler: handler,
		remover: func() { _ = d.dev.RemoveConnectionStatusChanged(token) },
	}, nil
}

func (d *winrtLEDevice) discoverServices(
	ctx context.Context,
) (gatt.GattCommunicationStatus, []remoteService, error) {
	op, err := d.dev.GetGattServicesWithCacheModeAsync(bluetooth.BluetoothCacheModeUncached)
	if err != nil {
		return 0, nil, fmt.Errorf("discover services: %w", err)
	}
	defer op.Release()
	res, err := awaitResult(ctx, op, gatt.SignatureGattDeviceServicesResult)
	if err != nil {
		return 0, nil, fmt.Errorf("discover services: %w", err)
	}
	result := (*gatt.GattDeviceServicesResult)(res)
	defer result.Release()
	status, err := result.GetStatus()
	if err != nil {
		return 0, nil, fmt.Errorf("discover services: %w", err)
	}
	if status != gatt.GattCommunicationStatusSuccess {
		return status, nil, nil
	}
	vector, err := result.GetServices()
	if err != nil {
		return 0, nil, fmt.Errorf("services: %w", err)
	}
	defer vector.Release()
	n, err := vector.GetSize()
	if err != nil {
		return 0, nil, fmt.Errorf("services: %w", err)
	}
	services := make([]remoteService, 0, n)
	for i := range n {
		if item, itemErr := vector.GetAt(i); itemErr == nil && item != nil {
			services = append(services, &winrtService{svc: (*gatt.GattDeviceService)(item)})
		}
	}
	return status, services, nil
}

func (d *winrtLEDevice) close() {
	_ = d.dev.Close()
	d.dev.Release()
}

// winrtClientSession is the GattSession held to a remote device.
type winrtClientSession struct {
	session *gatt.GattSession
}

func (s *winrtClientSession) maxPduSize() (uint16, error) {
	size, err := s.session.GetMaxPduSize()
	if err != nil {
		return 0, fmt.Errorf("max pdu size: %w", err)
	}
	return size, nil
}

func (s *winrtClientSession) close() {
	_ = s.session.Close()
	s.session.Release()
}

// winrtService is a GattDeviceService.
type winrtService struct {
	svc *gatt.GattDeviceService
}

func (s *winrtService) uuid() (string, error) {
	g, err := s.svc.GetUuid()
	if err != nil {
		return "", fmt.Errorf("service uuid: %w", err)
	}
	return uuidFromGUID(g), nil
}

func (s *winrtService) characteristics(
	ctx context.Context,
) (gatt.GattCommunicationStatus, []gattCharacteristic, error) {
	op, err := s.svc.GetCharacteristicsWithCacheModeAsync(bluetooth.BluetoothCacheModeUncached)
	if err != nil {
		return 0, nil, fmt.Errorf("get: %w", err)
	}
	defer op.Release()
	res, err := awaitResult(ctx, op, gatt.SignatureGattCharacteristicsResult)
	if err != nil {
		return 0, nil, err
	}
	result := (*gatt.GattCharacteristicsResult)(res)
	defer result.Release()
	status, err := result.GetStatus()
	if err != nil {
		return 0, nil, fmt.Errorf("status: %w", err)
	}
	if status != gatt.GattCommunicationStatusSuccess {
		return status, nil, nil
	}
	vector, err := result.GetCharacteristics()
	if err != nil {
		return 0, nil, fmt.Errorf("characteristics: %w", err)
	}
	defer vector.Release()
	n, err := vector.GetSize()
	if err != nil {
		return 0, nil, fmt.Errorf("characteristics: %w", err)
	}
	chars := make([]gattCharacteristic, 0, n)
	for i := range n {
		if item, itemErr := vector.GetAt(i); itemErr == nil && item != nil {
			chars = append(chars, &winrtCharacteristic{c: (*gatt.GattCharacteristic)(item)})
		}
	}
	return status, chars, nil
}

func (s *winrtService) close() {
	_ = s.svc.Close()
	s.svc.Release()
}

// winrtCharacteristic is a remote GattCharacteristic.
type winrtCharacteristic struct {
	c *gatt.GattCharacteristic
}

func (c *winrtCharacteristic) uuid() (string, error) {
	g, err := c.c.GetUuid()
	if err != nil {
		return "", fmt.Errorf("characteristic uuid: %w", err)
	}
	return uuidFromGUID(g), nil
}

// statusResult awaits an operation whose result is a
// GattCommunicationStatus, which is an enum value rather than an object.
func statusResult(ctx context.Context, op *foundation.IAsyncOperation) (gatt.GattCommunicationStatus, error) {
	if err := await(ctx, op, gatt.SignatureGattCommunicationStatus); err != nil {
		return 0, err
	}
	res, err := op.GetResults()
	if err != nil {
		return 0, fmt.Errorf("result: %w", err)
	}
	//nolint:gosec // the result of this operation is an enum value, not a pointer
	return gatt.GattCommunicationStatus(uintptr(res)), nil
}

func (c *winrtCharacteristic) configure(
	ctx context.Context, value gatt.GattClientCharacteristicConfigurationDescriptorValue,
) (gatt.GattCommunicationStatus, error) {
	op, err := c.c.WriteClientCharacteristicConfigurationDescriptorAsync(value)
	if err != nil {
		return 0, fmt.Errorf("write descriptor: %w", err)
	}
	defer op.Release()
	return statusResult(ctx, op)
}

func (c *winrtCharacteristic) onValueChanged(fn func([]byte)) (eventHandle, error) {
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
			fn(value)
		})
	token, err := c.c.AddValueChanged(handler)
	if err != nil {
		handler.Release()
		return nil, fmt.Errorf("add handler: %w", err)
	}
	return &winrtEvent{
		handler: handler,
		remover: func() { _ = c.c.RemoveValueChanged(token) },
	}, nil
}

func (c *winrtCharacteristic) write(
	ctx context.Context, value []byte, option gatt.GattWriteOption,
) (gatt.GattCommunicationStatus, error) {
	buf, err := bytesBuffer(value)
	if err != nil {
		return 0, err
	}
	defer buf.Release()
	op, err := c.c.WriteValueWithOptionAsync(buf, option)
	if err != nil {
		return 0, fmt.Errorf("write value: %w", err)
	}
	defer op.Release()
	return statusResult(ctx, op)
}

func (c *winrtCharacteristic) read(ctx context.Context) (gatt.GattCommunicationStatus, []byte, error) {
	op, err := c.c.ReadValueWithCacheModeAsync(bluetooth.BluetoothCacheModeUncached)
	if err != nil {
		return 0, nil, fmt.Errorf("read value: %w", err)
	}
	defer op.Release()
	res, err := awaitResult(ctx, op, gatt.SignatureGattReadResult)
	if err != nil {
		return 0, nil, err
	}
	result := (*gatt.GattReadResult)(res)
	defer result.Release()
	status, err := result.GetStatus()
	if err != nil {
		return 0, nil, fmt.Errorf("status: %w", err)
	}
	if status != gatt.GattCommunicationStatusSuccess {
		return status, nil, nil
	}
	buf, err := result.GetValue()
	if err != nil {
		return 0, nil, fmt.Errorf("value: %w", err)
	}
	defer releaseBuffer(buf)
	value, err := bufferBytes(buf)
	if err != nil {
		return 0, nil, err
	}
	return status, value, nil
}

func (c *winrtCharacteristic) release() { c.c.Release() }
