//go:build !windows

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
	"encoding/json"
	"net"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/bluetooth"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeBluetoothHost stands in for the Bluetooth manager: it hands its
// peripheral to whoever registers and says it is serving.
type fakeBluetoothHost struct {
	peripheral   *mocks.FakePeripheral
	registered   int
	unregistered int
	mu           syncutil.Mutex
}

func (h *fakeBluetoothHost) OnPeripheral(fn bluetooth.PeripheralFunc) func() {
	h.mu.Lock()
	h.registered++
	h.mu.Unlock()
	fn(h.peripheral)
	return func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.unregistered++
	}
}

func (*fakeBluetoothHost) Serving() bool { return true }

func (h *fakeBluetoothHost) counts() (registered, unregistered int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.registered, h.unregistered
}

func TestServerServesTheAPIOverBluetooth(t *testing.T) {
	t.Parallel()

	host := &fakeBluetoothHost{peripheral: mocks.NewFakePeripheral()}
	server := startAPITestServer(t, helpers.NewMemoryFS(), "127.0.0.1", 0, apiTestStandalone,
		func(opts *ListenerOptions) { opts.Bluetooth = host })
	require.NoError(t, server.ready)

	require.Eventually(t, func() bool { return host.peripheral.Handler() != nil },
		5*time.Second, time.Millisecond, "the API never served on the peripheral")
	registered, _ := host.counts()
	assert.Equal(t, 1, registered)

	tcp := &http.Client{Timeout: 2 * time.Second}
	t.Cleanup(tcp.CloseIdleConnections)
	url := "http://" + net.JoinHostPort("127.0.0.1", strconv.Itoa(server.cfg.APIPort())) + "/api/v0.1"
	status, reply := networkTestRPC(t, tcp, url, "", models.MethodDeviceEndpoints)
	assert.Equal(t, http.StatusOK, status)
	require.Contains(t, reply, "result", "device.endpoints: %s", reply["error"])
	var endpoints models.DeviceEndpointsResponse
	require.NoError(t, json.Unmarshal(reply["result"], &endpoints))
	assert.True(t, endpoints.BLE)

	server.stop(t)
	_, unregistered := host.counts()
	assert.Equal(t, 1, unregistered, "stopping the server lets go of the peripheral")
	assert.Eventually(t, func() bool { return host.peripheral.Handler() == nil },
		5*time.Second, time.Millisecond, "the peripheral is still being served")
}
