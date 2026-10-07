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
	gatt "github.com/saltosystems/winrt-go/windows/devices/bluetooth/genericattributeprofile"
	"github.com/saltosystems/winrt-go/windows/foundation"
	"github.com/saltosystems/winrt-go/windows/foundation/collections"
	"github.com/saltosystems/winrt-go/windows/storage/streams"
)

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

// adapter is the Windows Adapter. The WinRT API has no adapter object to
// hold: it always works on the system's default radio, and says whether
// there is one, and what it can do, only when a role is actually used.
type adapter struct {
	gone     chan struct{}
	goneOnce sync.Once
}

// Open prepares the Windows Bluetooth API. Whether a radio is present, on,
// and able to take a role is reported by Serve and by the central calls.
func Open(_ context.Context, opts ...Option) (Adapter, error) {
	_ = applyOptions(opts)
	if err := ensureRuntime(); err != nil {
		return nil, err
	}
	return &adapter{gone: make(chan struct{})}, nil
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

// typedHandler builds the event handler WinRT expects for an event raised by
// a sender type with an argument type, both named by their signatures.
func typedHandler(
	senderSignature, argsSignature string, fn func(sender, args unsafe.Pointer),
) *foundation.TypedEventHandler {
	iid := winrt.ParameterizedInstanceGUID(foundation.GUIDTypedEventHandler, senderSignature, argsSignature)
	return foundation.NewTypedEventHandler(ole.NewGUID(iid),
		func(_ *foundation.TypedEventHandler, sender, args unsafe.Pointer) { fn(sender, args) })
}

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

// peerFromSession identifies the remote device of a session. The device
// identifier stands in for the object path BlueZ would give.
func peerFromSession(session *gatt.GattSession) (Peer, error) {
	id, err := sessionDeviceID(session)
	if err != nil {
		return Peer{}, err
	}
	return Peer{Path: id, Address: addressFromDeviceID(id)}, nil
}
