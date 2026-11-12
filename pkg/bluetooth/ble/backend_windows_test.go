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
	"syscall"
	"testing"

	"github.com/saltosystems/winrt-go/windows/devices/bluetooth"
	gatt "github.com/saltosystems/winrt-go/windows/devices/bluetooth/genericattributeprofile"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBluetoothError(t *testing.T) {
	t.Parallel()
	require.NoError(t, bluetoothError("op", bluetooth.BluetoothErrorSuccess))
	require.ErrorIs(t, bluetoothError("op", bluetooth.BluetoothErrorRadioNotAvailable), ErrNoAdapter)
	for _, code := range []bluetooth.BluetoothError{
		bluetooth.BluetoothErrorDisabledByUser,
		bluetooth.BluetoothErrorDisabledByPolicy,
		bluetooth.BluetoothErrorConsentRequired,
	} {
		require.ErrorIs(t, bluetoothError("op", code), ErrUnavailable)
	}
	require.ErrorIs(t, bluetoothError("op", bluetooth.BluetoothErrorNotSupported), ErrRoleUnsupported)
	require.ErrorIs(t, bluetoothError("op", bluetooth.BluetoothErrorTransportNotSupported), ErrRoleUnsupported)

	err := bluetoothError("op", bluetooth.BluetoothErrorOtherError)
	require.EqualError(t, err, "op: bluetooth error 4")
	require.NotErrorIs(t, err, ErrUnavailable)
}

func TestCommunicationError(t *testing.T) {
	t.Parallel()
	require.NoError(t, communicationError("op", gatt.GattCommunicationStatusSuccess))
	require.ErrorIs(t, communicationError("op", gatt.GattCommunicationStatusUnreachable), errDisconnected)
	require.EqualError(t, communicationError("op", gatt.GattCommunicationStatusAccessDenied), "op: gatt status 3")
}

func TestGUIDRoundTrip(t *testing.T) {
	t.Parallel()
	g, err := guidFromUUID(testServiceUUID)
	require.NoError(t, err)
	assert.Equal(t, syscall.GUID{
		Data1: 0x6e400001, Data2: 0xb5a3, Data3: 0xf393,
		Data4: [8]byte{0xe0, 0xa9, 0xe5, 0x0e, 0x24, 0xdc, 0xca, 0x9e},
	}, g)
	assert.Equal(t, "6e400001-b5a3-f393-e0a9-e50e24dcca9e", uuidFromGUID(g))

	_, err = guidFromUUID("not a uuid")
	require.ErrorContains(t, err, "invalid uuid")
}

func TestAdapter(t *testing.T) {
	t.Parallel()
	a, _ := newTestAdapter(newFakeHost())
	assert.Empty(t, a.Address())
	assert.Nil(t, a.Roles())
	p, err := a.Peripheral()
	require.NoError(t, err)
	assert.NotNil(t, p)
	c, err := a.Central()
	require.NoError(t, err)
	assert.NotNil(t, c)

	select {
	case <-a.Gone():
		require.FailNow(t, "gone before Close")
	default:
	}
	require.NoError(t, a.Close())
	require.NoError(t, a.Close())
	<-a.Gone()
}

func TestOpenUsesTheRuntime(t *testing.T) {
	t.Parallel()
	a, err := Open(context.Background())
	require.NoError(t, err)
	impl, ok := a.(*adapter)
	require.True(t, ok)
	assert.IsType(t, winrtHost{}, impl.host)
	require.NoError(t, a.Close())
}
