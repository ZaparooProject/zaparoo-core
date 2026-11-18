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

package plex

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTVClientDefaults(t *testing.T) {
	t.Parallel()
	c := NewTVClient("", "", testClientID, nil)
	assert.Equal(t, NormalizeURL(ClientsBaseURL), c.ClientsURL())
	assert.Equal(t, NormalizeURL(TVBaseURL), c.baseURL)
	assert.NotNil(t, c.http)
}

func TestTVClientPinLifecycle(t *testing.T) {
	t.Parallel()
	tv := newFakeTV(t)
	c := NewTVClient(tv.URL, tv.URL, testClientID, tv.Client())
	ctx := context.Background()

	pin, err := c.CreatePin(ctx)
	require.NoError(t, err)
	assert.Equal(t, "AB12", pin.Code)
	assert.Equal(t, FlexInt(900), pin.ExpiresIn)

	pending, err := c.CheckPin(ctx, int64(pin.ID))
	require.NoError(t, err)
	assert.Empty(t, pending.AuthToken)

	tv.approve(int64(pin.ID), "account-token")
	approved, err := c.CheckPin(ctx, int64(pin.ID))
	require.NoError(t, err)
	assert.Equal(t, "account-token", approved.AuthToken)

	tv.expire(int64(pin.ID))
	_, err = c.CheckPin(ctx, int64(pin.ID))
	require.ErrorIs(t, err, ErrPinExpired)
}

func TestTVClientCreatePinRejectsIncomplete(t *testing.T) {
	t.Parallel()
	for _, body := range []string{`{"id":0,"code":"AB12"}`, `{"id":5,"code":" "}`} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(body))
		}))
		t.Cleanup(srv.Close)
		c := NewTVClient(srv.URL, srv.URL, testClientID, srv.Client())
		_, err := c.CreatePin(context.Background())
		require.ErrorContains(t, err, "incomplete link code", body)
	}
}

func TestTVClientResourcesIncludeRelays(t *testing.T) {
	t.Parallel()
	tv := newFakeTV(t)
	tv.resources = []Resource{{
		Name: "Server", Provides: "server", ClientIdentifier: "srv", Owned: true,
		Connections: []Connection{
			{URI: "http://192.168.1.5:32400", Local: true},
			{URI: "https://relay.example:8443", Relay: true},
		},
	}}
	c := NewTVClient(tv.URL, tv.URL, testClientID, tv.Client())
	resources, err := c.Resources(context.Background(), "account-token")
	require.NoError(t, err)
	require.Len(t, resources, 1)
	assert.Len(t, resources[0].Connections, 2)
	tv.mu.Lock()
	defer tv.mu.Unlock()
	assert.Equal(t, []string{"account-token"}, tv.tokens)
}
