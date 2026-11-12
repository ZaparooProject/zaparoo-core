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

package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	apimiddleware "github.com/ZaparooProject/zaparoo-core/v2/pkg/api/middleware"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/hoststatus"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/power"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/device"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// A Bluetooth client gets gated notifications on the same terms as a
// WebSocket client with the same paired role.
func TestBLESession_GatedNotificationsFollowTheRole(t *testing.T) {
	t.Parallel()

	first := newTestEncryptionFirstFrameFor(t, apimiddleware.TransportBLE)
	rig := newBLETestRig(t, bleTestRigOptions{methodMap: versionMethodMap(t), encGateway: first.gateway})
	client := rig.client("11:22:33:44:55:66", 7, 185)
	client.authenticate(t, first)
	s := client.session()

	// The fixture pairs a member, which may not manage Bluetooth.
	s.sendNotification(models.NotificationDeviceBluetooth,
		[]byte(`{"jsonrpc":"2.0","method":"device.bluetooth","params":{}}`))
	client.expectNothing(t)

	changed := []byte(`{"jsonrpc":"2.0","method":"device.changed","params":{}}`)
	s.sendNotification(models.NotificationDeviceChanged, changed)
	assert.JSONEq(t, string(changed), string(decryptS2C(t, first.secrets, client.recv(t), 1)))
}

// Bluetooth clients count as listeners, so the device monitor runs for them
// and stops counting them when they leave.
func TestBLESession_ClientsDriveTheDeviceMonitor(t *testing.T) {
	t.Parallel()

	pl := mocks.NewMockPlatform()
	pl.On("ID").Return("test")
	pl.On("Settings").Return(platforms.Settings{})
	pl.On("RootDirs", mock.Anything).Return([]string{})

	read := make(chan struct{}, 8)
	monitor := device.NewMonitor(&device.Options{
		Platform: pl,
		Clock:    clockwork.NewFakeClockAt(time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)),
		Readers: hoststatus.Readers{
			Power: func() (power.Detail, error) {
				select {
				case read <- struct{}{}:
				default:
				}
				return power.Detail{Source: power.SourceExternal}, nil
			},
		},
	})

	rig := newBLETestRig(t, bleTestRigOptions{})
	rig.st.SetDeviceMonitor(monitor)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		monitor.Run(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})

	awaitRead := func(what string) {
		t.Helper()
		select {
		case <-read:
		case <-time.After(5 * time.Second):
			t.Fatal(what)
		}
	}

	first := rig.client("11:22:33:44:55:66", 7, 185)
	first.openPending(t)
	awaitRead("a Bluetooth client connecting did not start the monitor")

	rig.peripheral.Handler().OnDisconnect(first.peer)
	first.waitClosed(t)

	// Only a first listener makes the monitor read again at once, so this
	// read shows the one that left was counted out.
	second := rig.client("77:88:99:AA:BB:CC", 8, 185)
	second.openPending(t)
	awaitRead("the monitor still counted the Bluetooth client that left")
}

// pairingError reads a pairing error reply and returns its ID and the HTTP
// status and message the pairing endpoints would have answered with.
func pairingError(t *testing.T, wire []byte) (id models.RPCID, status float64, message any) {
	t.Helper()
	var resp models.ResponseErrorObject
	require.NoError(t, json.Unmarshal(wire, &resp))
	require.NotNil(t, resp.Error)
	assert.Equal(t, JSONRPCErrorPairingFailed.Code, resp.Error.Code)
	data, ok := resp.Error.Data.(map[string]any)
	require.True(t, ok)
	got, ok := data["status"].(float64)
	require.True(t, ok)
	return resp.ID, got, data["message"]
}

func TestBLESession_PairingUnavailable(t *testing.T) {
	t.Parallel()

	rig := newBLETestRig(t, bleTestRigOptions{})
	client := rig.client("11:22:33:44:55:66", 7, 185)

	// A notification-style request still gets an answer, with a null ID.
	client.send(t, []byte(`{"jsonrpc":"2.0","method":"pair.start","params":{}}`))
	id, status, _ := pairingError(t, client.recv(t))
	assert.Equal(t, models.NullRPCID, id)
	assert.InDelta(t, float64(http.StatusServiceUnavailable), status, 0)
}

func TestBLESession_PairingRejectsMalformedParams(t *testing.T) {
	t.Parallel()

	harness := newPairingHarness(t)
	rig := newBLETestRig(t, bleTestRigOptions{pairing: harness.mgr})

	for i, request := range []string{
		`{"jsonrpc":"2.0","id":1,"method":"pair.start","params":"nope"}`,
		`{"jsonrpc":"2.0","id":1,"method":"PAIR.FINISH","params":[1]}`,
	} {
		client := rig.client(fmt.Sprintf("11:22:33:44:55:%02X", i), uint16(10+i), 185)
		client.send(t, []byte(request))
		_, status, message := pairingError(t, client.recv(t))
		assert.InDelta(t, float64(http.StatusBadRequest), status, 0, request)
		assert.Equal(t, "invalid request body", message, request)
	}
}

func TestBLESession_TamperedFrameEndsTheSession(t *testing.T) {
	t.Parallel()

	first := newTestEncryptionFirstFrameFor(t, apimiddleware.TransportBLE)
	rig := newBLETestRig(t, bleTestRigOptions{methodMap: versionMethodMap(t), encGateway: first.gateway})
	client := rig.client("11:22:33:44:55:66", 7, 185)
	client.authenticate(t, first)

	frame := first.secrets.encryptSubsequentBinary(t, []byte("ping"), 1)
	frame[len(frame)-1] ^= 0xff
	client.send(t, frame)
	client.expectSessionClosed(t)
	client.waitClosed(t)
}
