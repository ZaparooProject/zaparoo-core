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

package hoststatus

import (
	"runtime"
	"syscall"
	"time"
	"unsafe"

	"github.com/go-ole/go-ole"
)

const (
	clsidRadio = "Windows.Devices.Radios.Radio"

	// WinRT interface method slots: the three IUnknown methods and three
	// IInspectable methods come first, so a type's own methods start at 6.
	slotQueryInterface = 0
	slotRelease        = 2

	slotRadioStaticsGetRadiosAsync = 6
	slotAsyncOperationGetResults   = 8
	slotAsyncInfoStatus            = 7
	slotVectorViewGetAt            = 6
	slotVectorViewSize             = 7
	slotRadioState                 = 9
	slotRadioKind                  = 11

	// asyncStatusCompleted and asyncStatusStarted are AsyncStatus values.
	asyncStatusStarted   = 0
	asyncStatusCompleted = 1

	radioKindBluetooth = 3

	hresultFailureBit = 0x80000000

	// radioQueryTimeout leaves the radio query well inside the monitor's own
	// bound on a read.
	radioQueryTimeout = time.Second
	radioPollInterval = 10 * time.Millisecond
)

var (
	// iidRadioStatics is IRadioStatics from windows.devices.radios.h.
	iidRadioStatics = ole.NewGUID("{5FB6A12E-67CB-46AE-AAE9-65919F86EFF4}")
	// iidAsyncInfo is IAsyncInfo.
	iidAsyncInfo = ole.NewGUID("{00000036-0000-0000-C000-000000000046}")
)

// comFailed reports whether an HRESULT has its severity bit set.
func comFailed(hr uintptr) bool {
	return hr&hresultFailureBit != 0
}

// comCall invokes the method in the given vtable slot of a COM object.
func comCall(object unsafe.Pointer, slot int, args ...uintptr) uintptr {
	vtable := *(*unsafe.Pointer)(object)
	method := *(*uintptr)(unsafe.Add(vtable, slot*int(unsafe.Sizeof(uintptr(0)))))
	all := make([]uintptr, 0, len(args)+1)
	all = append(all, uintptr(object))
	all = append(all, args...)
	hr, _, _ := syscall.SyscallN(method, all...)
	return hr
}

func comRelease(object unsafe.Pointer) {
	if object != nil {
		comCall(object, slotRelease)
	}
}

// awaitAsyncOperation waits for a WinRT async operation to finish by polling
// its IAsyncInfo, so no completion handler has to be registered.
func awaitAsyncOperation(operation unsafe.Pointer) bool {
	var info unsafe.Pointer
	//nolint:gosec // G103: pointers are to locals the call fills in
	hr := comCall(operation, slotQueryInterface, uintptr(unsafe.Pointer(iidAsyncInfo)), uintptr(unsafe.Pointer(&info)))
	if comFailed(hr) || info == nil {
		return false
	}
	defer comRelease(info)

	deadline := time.Now().Add(radioQueryTimeout)
	for {
		var status int32
		//nolint:gosec // G103: pointer is to a local the call fills in
		if comFailed(comCall(info, slotAsyncInfoStatus, uintptr(unsafe.Pointer(&status)))) {
			return false
		}
		switch status {
		case asyncStatusCompleted:
			return true
		case asyncStatusStarted:
		default:
			return false
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(radioPollInterval)
	}
}

// readWindowsBluetoothPowered asks Windows whether its Bluetooth radios are
// switched on. It is nil when that cannot be read. Only the state is read:
// nothing here asks for access to a radio or changes one.
func readWindowsBluetoothPowered() *bool {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	release, ok := initCOM()
	if !ok {
		return nil
	}
	defer release()

	factory, err := ole.RoGetActivationFactory(clsidRadio, iidRadioStatics)
	if err != nil || factory == nil {
		return nil
	}
	defer factory.Release()

	var operation unsafe.Pointer
	//nolint:gosec // G103: the factory is a COM object and the pointer is to a local
	hr := comCall(unsafe.Pointer(factory), slotRadioStaticsGetRadiosAsync, uintptr(unsafe.Pointer(&operation)))
	if comFailed(hr) || operation == nil {
		return nil
	}
	defer comRelease(operation)
	if !awaitAsyncOperation(operation) {
		return nil
	}

	var radios unsafe.Pointer
	//nolint:gosec // G103: pointer is to a local the call fills in
	if comFailed(comCall(operation, slotAsyncOperationGetResults, uintptr(unsafe.Pointer(&radios)))) || radios == nil {
		return nil
	}
	defer comRelease(radios)

	var size uint32
	//nolint:gosec // G103: pointer is to a local the call fills in
	if comFailed(comCall(radios, slotVectorViewSize, uintptr(unsafe.Pointer(&size)))) {
		return nil
	}

	var states []int32
	for i := range size {
		state, isBluetooth := readRadio(radios, i)
		if isBluetooth {
			states = append(states, state)
		}
	}
	return combineRadioStates(states)
}

// readRadio reads the state of the radio at an index of a radio list. The
// second result is false when it is not a Bluetooth radio or cannot be read.
func readRadio(radios unsafe.Pointer, index uint32) (state int32, isBluetooth bool) {
	var radio unsafe.Pointer
	//nolint:gosec // G103: pointer is to a local the call fills in
	hr := comCall(radios, slotVectorViewGetAt, uintptr(index), uintptr(unsafe.Pointer(&radio)))
	if comFailed(hr) || radio == nil {
		return 0, false
	}
	defer comRelease(radio)

	var kind int32
	//nolint:gosec // G103: pointer is to a local the call fills in
	if comFailed(comCall(radio, slotRadioKind, uintptr(unsafe.Pointer(&kind)))) || kind != radioKindBluetooth {
		return 0, false
	}
	//nolint:gosec // G103: pointer is to a local the call fills in
	if comFailed(comCall(radio, slotRadioState, uintptr(unsafe.Pointer(&state)))) {
		return 0, false
	}
	return state, true
}
