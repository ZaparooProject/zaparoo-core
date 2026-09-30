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

package client

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeHold struct {
	lost   chan struct{}
	closed atomic.Int32
}

func newFakeHold() *fakeHold { return &fakeHold{lost: make(chan struct{})} }

func (f *fakeHold) hold() *zapScriptHold {
	return &zapScriptHold{lost: f.lost, close: func() { f.closed.Add(1) }}
}

func noRequest(context.Context, *config.Instance, string, string) (string, error) {
	return "", errors.New("unexpected request")
}

func TestDisableZapScriptWithHoldFallsBackForOlderService(t *testing.T) {
	t.Parallel()
	var params []string
	enable := disableZapScriptWithHold(nil,
		func(context.Context, *config.Instance) (*zapScriptHold, error) {
			return nil, errZapScriptHoldUnsupported
		},
		func(_ context.Context, _ *config.Instance, method, p string) (string, error) {
			assert.Equal(t, models.MethodSettingsUpdate, method)
			params = append(params, p)
			return "", nil
		}, time.Millisecond)
	require.Len(t, params, 1)
	enable()
	require.Len(t, params, 2)
	assert.JSONEq(t, `{"runZapScript":false}`, params[0])
	assert.JSONEq(t, `{"runZapScript":true}`, params[1])
}

func TestDisableZapScriptWithHoldOpenFailureReturnsNoop(t *testing.T) {
	t.Parallel()
	enable := disableZapScriptWithHold(nil,
		func(context.Context, *config.Instance) (*zapScriptHold, error) {
			return nil, errors.New("boom")
		}, noRequest, time.Millisecond)
	require.NotNil(t, enable)
	enable()
}

func TestDisableZapScriptWithHoldReleaseClosesConnection(t *testing.T) {
	t.Parallel()
	fake := newFakeHold()
	enable := disableZapScriptWithHold(nil,
		func(context.Context, *config.Instance) (*zapScriptHold, error) {
			return fake.hold(), nil
		}, noRequest, time.Millisecond)
	assert.Equal(t, int32(0), fake.closed.Load())
	enable()
	assert.Equal(t, int32(1), fake.closed.Load())
}

func TestDisableZapScriptWithHoldRedialsAfterConnectionLoss(t *testing.T) {
	t.Parallel()
	first, second := newFakeHold(), newFakeHold()
	var opens atomic.Int32
	reopened := make(chan struct{})
	open := func(context.Context, *config.Instance) (*zapScriptHold, error) {
		switch opens.Add(1) {
		case 1:
			return first.hold(), nil
		case 2:
			return nil, errors.New("service still starting")
		default:
			close(reopened)
			return second.hold(), nil
		}
	}
	enable := disableZapScriptWithHold(nil, open, noRequest, time.Millisecond)

	close(first.lost)
	select {
	case <-reopened:
	case <-time.After(2 * time.Second):
		t.Fatal("hold was not re-established after the connection dropped")
	}
	enable()
	assert.Equal(t, int32(1), first.closed.Load())
	assert.Equal(t, int32(1), second.closed.Load())
}

func TestOpenZapScriptHoldAgainstServer(t *testing.T) {
	t.Parallel()
	upgrader := websocket.Upgrader{}
	var replyError atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = c.Close() }()
		var req models.RequestObject
		if c.ReadJSON(&req) != nil {
			return
		}
		assert.Equal(t, models.MethodSettingsZapScriptHold, req.Method)
		resp := models.ResponseObject{JSONRPC: "2.0", ID: req.ID}
		if replyError.Load() {
			resp.Error = &models.ErrorObject{Code: jsonRPCMethodNotFound, Message: "Method not found"}
		}
		if c.WriteJSON(resp) != nil {
			return
		}
		for {
			if _, _, err := c.ReadMessage(); err != nil {
				return
			}
		}
	}))
	defer srv.Close()

	cfg := testConfigWithPort(t, parseServerPort(t, srv))

	hold, err := openZapScriptHold(t.Context(), cfg)
	require.NoError(t, err)
	select {
	case <-hold.lost:
		t.Fatal("hold ended while the connection was open")
	default:
	}
	hold.close()
	select {
	case <-hold.lost:
	case <-time.After(2 * time.Second):
		t.Fatal("lost was not signalled after close")
	}

	replyError.Store(true)
	_, err = openZapScriptHold(t.Context(), cfg)
	require.ErrorIs(t, err, errZapScriptHoldUnsupported)
}
