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
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/methods"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const zapScriptProbeMethod = "test.zapscript.enabled"

func newZapScriptHoldMethodMap(t *testing.T) *MethodMap {
	t.Helper()
	methodMap := &MethodMap{}
	require.NoError(t, methodMap.AddMethod(
		models.MethodSettingsZapScriptHold, methods.HandleSettingsZapScriptHold,
	))
	require.NoError(t, methodMap.AddMethod(zapScriptProbeMethod, func(env requests.RequestEnv) (any, error) {
		return env.State.RunZapScriptEnabled(), nil
	}))
	return methodMap
}

func callZapScriptRPC(t *testing.T, conn *websocket.Conn, id int, method string) models.ResponseObject {
	t.Helper()
	payload, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method})
	require.NoError(t, err)
	require.NoError(t, conn.WriteMessage(websocket.TextMessage, payload))
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(2*time.Second)))
	_, message, err := conn.ReadMessage()
	require.NoError(t, err)
	var response models.ResponseObject
	require.NoError(t, json.Unmarshal(message, &response))
	return response
}

func zapScriptEnabled(t *testing.T, conn *websocket.Conn, id int) bool {
	t.Helper()
	response := callZapScriptRPC(t, conn, id, zapScriptProbeMethod)
	require.Nil(t, response.Error)
	enabled, ok := response.Result.(bool)
	require.True(t, ok)
	return enabled
}

func TestWebSocketZapScriptHoldReleasesWhenConnectionDrops(t *testing.T) {
	wsURL, cleanup := startPriorityWSServerWithPlatform(
		t, newZapScriptHoldMethodMap(t), mocks.NewMockPlatform(),
	)
	defer cleanup()

	probe := dialWS(t, wsURL)
	defer func() { _ = probe.Close() }()
	holder := dialWS(t, wsURL)

	require.True(t, zapScriptEnabled(t, probe, 1))
	require.Nil(t, callZapScriptRPC(t, holder, 1, models.MethodSettingsZapScriptHold).Error)
	assert.False(t, zapScriptEnabled(t, probe, 2), "hold must disable ZapScript")

	// A second request on the same connection must not stack a second hold.
	require.Nil(t, callZapScriptRPC(t, holder, 2, models.MethodSettingsZapScriptHold).Error)

	// The holder is killed: no re-enable request is ever sent.
	require.NoError(t, holder.Close())
	deadline := time.Now().Add(2 * time.Second)
	id := 3
	for !zapScriptEnabled(t, probe, id) {
		require.True(t, time.Now().Before(deadline), "ZapScript stayed disabled after the holder disconnected")
		id++
		time.Sleep(10 * time.Millisecond)
	}
}

func TestWebSocketZapScriptHoldIsPerConnection(t *testing.T) {
	wsURL, cleanup := startPriorityWSServerWithPlatform(
		t, newZapScriptHoldMethodMap(t), mocks.NewMockPlatform(),
	)
	defer cleanup()

	probe := dialWS(t, wsURL)
	defer func() { _ = probe.Close() }()
	first := dialWS(t, wsURL)
	second := dialWS(t, wsURL)
	defer func() { _ = second.Close() }()

	require.Nil(t, callZapScriptRPC(t, first, 1, models.MethodSettingsZapScriptHold).Error)
	require.Nil(t, callZapScriptRPC(t, second, 1, models.MethodSettingsZapScriptHold).Error)
	require.NoError(t, first.Close())

	// Poll across the window in which the server processes the disconnect.
	deadline := time.Now().Add(300 * time.Millisecond)
	for id := 1; time.Now().Before(deadline); id++ {
		require.False(t, zapScriptEnabled(t, probe, id), "one holder closing must not release another holder")
		time.Sleep(20 * time.Millisecond)
	}
}

func TestHTTPZapScriptHoldRejected(t *testing.T) {
	t.Parallel()
	_, err := methods.HandleSettingsZapScriptHold(requests.RequestEnv{IsLocal: true})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "WebSocket")
}
