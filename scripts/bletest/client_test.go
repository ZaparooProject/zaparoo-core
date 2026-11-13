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

package main

import (
	"context"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	apimiddleware "github.com/ZaparooProject/zaparoo-core/v2/pkg/api/middleware"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/bluetooth/apigatt"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/bluetooth/ble"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testCallTimeout = 10 * time.Second

// adapterFor returns an env whose adapter reaches the fake core.
func adapterFor(core *fakeCore) (env, *mocks.FakeAdapter) {
	adapter := mocks.NewFakeAdapter()
	adapter.Cent = core.central
	return env{
		clock: clockwork.NewFakeClock(),
		open:  func(context.Context) (ble.Adapter, error) { return adapter, nil },
	}, adapter
}

func TestClientPairsThenExercisesEveryMode(t *testing.T) {
	t.Parallel()
	core := newFakeCore(t, &fakeCoreOptions{chatty: true})
	pin, _, err := core.pairing.StartPairing("")
	require.NoError(t, err)
	e, adapter := adapterFor(core)
	credsPath := filepath.Join(t.TempDir(), "creds.json")

	err = run(t.Context(), e, []string{
		"client", "-addr", fakeCoreAddr, "-creds", credsPath, "-pair", pin, "-fast-writes",
		"-req", "version", "-req", `media.search {"query":"mario"}`, "-req", "big",
		"-upload", "500", "-pipeline", "5", "-listen", "300ms", "-ping-every", "40ms",
	})
	require.NoError(t, err)

	creds, err := loadCredentials(credsPath)
	require.NoError(t, err)
	assert.Equal(t, fakeCoreDeviceID, creds.DeviceID)
	assert.NotNil(t, core.client(creds.AuthToken))
	assert.Positive(t, core.pings.Load(), "the heartbeat ran while listening")
	assert.True(t, adapter.Closed())
	assert.False(t, core.dev.Connected())
}

func TestRunClientSetupFailures(t *testing.T) {
	t.Parallel()

	opened := errors.New("no adapter")
	err := run(t.Context(), env{open: func(context.Context) (ble.Adapter, error) { return nil, opened }},
		[]string{"client"})
	require.ErrorIs(t, err, opened)

	peripheralOnly := mocks.NewFakeAdapter(ble.RolePeripheral)
	err = run(t.Context(), env{open: func(context.Context) (ble.Adapter, error) { return peripheralOnly, nil }},
		[]string{"client"})
	require.ErrorIs(t, err, ble.ErrRoleUnsupported)
	assert.True(t, peripheralOnly.Closed())

	core := newFakeCore(t, &fakeCoreOptions{})
	core.dev.ConnectErr = errors.New("refused")
	e, _ := adapterFor(core)
	err = run(t.Context(), e, []string{"client", "-addr", fakeCoreAddr})
	require.ErrorContains(t, err, "refused")
}

func TestClientFaults(t *testing.T) {
	t.Parallel()
	var midMessage [][]byte
	var midMessageMu syncutil.Mutex
	tests := []struct {
		check   func(t *testing.T, core *fakeCore)
		name    string
		fault   string
		wantErr string
		reqs    []string
		core    fakeCoreOptions
		pair    bool
	}{
		{name: "plaintext dropped", fault: faultPlaintext},
		{name: "plaintext ended with a notice", fault: faultPlaintext, core: fakeCoreOptions{onPlaintext: plainNotice}},
		{
			name: "plaintext answered", fault: faultPlaintext, core: fakeCoreOptions{onPlaintext: plainAnswer},
			wantErr: "core answered a plaintext method call with",
		},
		{
			name: "plaintext write fails", fault: faultPlaintext, wantErr: "write chunk",
			core: fakeCoreOptions{rxWrite: func([]byte) error { return errors.New("gone") }},
		},
		{
			name: "mid message", fault: faultMidMessage,
			core: fakeCoreOptions{rxWrite: func(chunk []byte) error {
				midMessageMu.Lock()
				defer midMessageMu.Unlock()
				midMessage = append(midMessage, chunk)
				return nil
			}},
			check: func(t *testing.T, _ *fakeCore) {
				midMessageMu.Lock()
				defer midMessageMu.Unlock()
				require.Len(t, midMessage, 1, "only the first chunk was sent")
				h, _, err := apigatt.ParseChunk(midMessage[0])
				require.NoError(t, err)
				assert.True(t, h.First)
				assert.False(t, h.Last)
			},
		},
		{
			name: "mid message write fails", fault: faultMidMessage, wantErr: "send partial message",
			core: fakeCoreOptions{rxWrite: func([]byte) error { return errors.New("gone") }},
		},
		{name: "bad ack dropped", fault: faultBadAck},
		{
			name: "bad ack write fails", fault: faultBadAck, wantErr: "write chunk",
			core: fakeCoreOptions{rxWrite: func([]byte) error { return errors.New("gone") }},
		},
		{name: "wrong pin refused", fault: faultWrongPIN, pair: true},
		{name: "websocket label refused", fault: faultWSLabel},
		{
			name: "websocket label accepted", fault: faultWSLabel,
			core:    fakeCoreOptions{transport: apimiddleware.TransportWebSocket},
			wantErr: "a frame bound to the websocket transport was accepted",
		},
		{
			name: "no ack dropped", fault: faultNoAck, reqs: []string{"big"},
			core: fakeCoreOptions{stall: 50 * time.Millisecond},
			check: func(t *testing.T, core *fakeCore) {
				assert.False(t, core.dev.Connected())
			},
		},
		{
			name: "no ack but answered in full", fault: faultNoAck, reqs: []string{"version"},
			wantErr: "a response arrived in full without any acknowledgement",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			core := newFakeCore(t, &tt.core)
			o := &clientOptions{fault: tt.fault, name: "bletest", requests: tt.reqs, timeout: testCallTimeout}
			if tt.pair {
				pin, _, err := core.pairing.StartPairing("")
				require.NoError(t, err)
				o.pin = pin
				o.creds = filepath.Join(t.TempDir(), "creds.json")
			} else {
				o.creds = core.seedCredentials(t)
			}
			l := core.dial(t)

			err := o.run(t.Context(), l)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}
			if tt.check != nil {
				tt.check(t, core)
			}
		})
	}
}

func TestClientExpectDroppedHonoursInterrupt(t *testing.T) {
	t.Parallel()
	core := newFakeCore(t, &fakeCoreOptions{onPlaintext: plainIgnore})
	l := core.dial(t)
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()

	o := &clientOptions{fault: faultPlaintext}
	require.ErrorIs(t, o.run(ctx, l), context.DeadlineExceeded)
}

func TestExpectDroppedTakesAClosedLinkAsTheDrop(t *testing.T) {
	t.Parallel()
	// The receive loop can end, closing msgs, before the device reports the
	// disconnect.
	l := &link{dev: mocks.NewFakeDevice(fakeCoreAddr), msgs: make(chan []byte)}
	close(l.msgs)

	require.NoError(t, expectDropped(t.Context(), l, "a fault"))
}

func TestClientRunFailures(t *testing.T) {
	t.Parallel()
	tests := []struct {
		setup   func(t *testing.T, core *fakeCore, o *clientOptions)
		name    string
		wantErr string
		core    fakeCoreOptions
	}{
		{name: "no credentials", wantErr: "read credentials", setup: func(t *testing.T, _ *fakeCore, o *clientOptions) {
			o.creds = filepath.Join(t.TempDir(), "none.json")
		}},
		{
			name: "credentials for another device", wantErr: "credentials are for device other",
			setup: func(t *testing.T, core *fakeCore, o *clientOptions) {
				creds, err := loadCredentials(core.seedCredentials(t))
				require.NoError(t, err)
				creds.DeviceID = "other"
				o.creds = filepath.Join(t.TempDir(), "other.json")
				require.NoError(t, creds.save(o.creds))
			},
		},
		{
			name: "bad pairing key", wantErr: "decode pairing key",
			setup: func(t *testing.T, _ *fakeCore, o *clientOptions) {
				o.creds = filepath.Join(t.TempDir(), "bad.json")
				require.NoError(t, (&credentials{DeviceID: fakeCoreDeviceID, PairingKey: "!!"}).save(o.creds))
			},
		},
		{
			name: "pairing without a pin", wantErr: "no pairing in progress",
			setup: func(t *testing.T, _ *fakeCore, o *clientOptions) {
				o.pin = "000000"
				o.creds = filepath.Join(t.TempDir(), "creds.json")
			},
		},
		{
			name: "credentials cannot be saved", wantErr: "write credentials",
			setup: func(t *testing.T, core *fakeCore, o *clientOptions) {
				pin, _, err := core.pairing.StartPairing("")
				require.NoError(t, err)
				o.pin = pin
				o.creds = filepath.Join(t.TempDir(), "missing-dir", "creds.json")
			},
		},
		{name: "first call refused", wantErr: "rpc error -32001", core: fakeCoreOptions{transport: "ws"}},
		{name: "bad request spec", wantErr: "not valid JSON", setup: func(_ *testing.T, _ *fakeCore, o *clientOptions) {
			o.requests = stringList{"media.search {nope"}
		}},
		{name: "upload fails", wantErr: "version", setup: func(_ *testing.T, _ *fakeCore, o *clientOptions) {
			o.upload = 100
			o.requests = stringList{"drop"}
		}},
		{
			name: "pipeline fails", wantErr: "pipeline", core: fakeCoreOptions{version: "error-later"},
			setup: func(_ *testing.T, _ *fakeCore, o *clientOptions) {
				o.pipeline = 2
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			core := newFakeCore(t, &tt.core)
			o := &clientOptions{name: "bletest", timeout: testCallTimeout}
			if tt.setup != nil {
				tt.setup(t, core, o)
			}
			if o.creds == "" {
				o.creds = core.seedCredentials(t)
			}
			l := core.dial(t)
			require.ErrorContains(t, o.run(t.Context(), l), tt.wantErr)
		})
	}
}

// newTestCaller opens an encrypted session to the fake core.
func newTestCaller(t *testing.T, core *fakeCore) *caller {
	t.Helper()
	creds, err := loadCredentials(core.seedCredentials(t))
	require.NoError(t, err)
	s, err := newSession(core.dial(t), creds, transportLabel)
	require.NoError(t, err)
	return &caller{s: s, timeout: testCallTimeout}
}

func TestCallerCall(t *testing.T) {
	t.Parallel()
	c := newTestCaller(t, newFakeCore(t, &fakeCoreOptions{chatty: true}))

	result, err := c.call(t.Context(), "version", nil)
	require.NoError(t, err, "a notification ahead of the response is skipped")
	assert.JSONEq(t, `{"version":"fake"}`, string(result))

	_, err = c.call(t.Context(), "nope", nil)
	var rpcErr *rpcError
	require.ErrorAs(t, err, &rpcErr)
	assert.Equal(t, -32601, rpcErr.Code)

	_, err = c.call(t.Context(), "garbage", nil)
	require.ErrorContains(t, err, `decode message "not json"`)

	_, err = c.call(t.Context(), "version", make(chan int))
	require.ErrorContains(t, err, "encode version")

	_, err = c.call(t.Context(), "drop", nil)
	require.ErrorContains(t, err, "drop: core disconnected")
	assert.Equal(t, 5, c.nextID)
}

func TestCallerCallSendFails(t *testing.T) {
	t.Parallel()
	c := newTestCaller(t, newFakeCore(t, &fakeCoreOptions{}))
	c.s.token = ""
	_, err := c.call(t.Context(), "version", nil)
	require.ErrorContains(t, err, "want 1-255")

	c = newTestCaller(t, newFakeCore(t, &fakeCoreOptions{}))
	token := c.s.token
	c.s.token = ""
	require.ErrorContains(t, c.pipeline(t.Context(), 1), "want 1-255")

	// The refused first frame never went out, so the next one still
	// carries the header Core needs to open the session.
	c.s.token = token
	_, err = c.call(t.Context(), "version", nil)
	require.NoError(t, err)
}

func TestCallerPipeline(t *testing.T) {
	t.Parallel()
	c := newTestCaller(t, newFakeCore(t, &fakeCoreOptions{chatty: true}))
	require.NoError(t, c.pipeline(t.Context(), 6))
	assert.Equal(t, 6, c.nextID)

	_, err := c.call(t.Context(), "version", nil)
	require.NoError(t, err, "the session stays in step after a pipeline")
}

func TestCallerPipelineFailures(t *testing.T) {
	t.Parallel()
	c := newTestCaller(t, newFakeCore(t, &fakeCoreOptions{version: "error"}))
	require.ErrorContains(t, c.pipeline(t.Context(), 3), "pipeline: request 1: rpc error -32000: busy")

	c = newTestCaller(t, newFakeCore(t, &fakeCoreOptions{version: "drop"}))
	require.ErrorContains(t, c.pipeline(t.Context(), 3), "pipeline: 3 of 3 unanswered")

	_, _, err := c.next(t.Context())
	require.Error(t, err)
}

func TestCallerListen(t *testing.T) {
	t.Parallel()
	core := newFakeCore(t, &fakeCoreOptions{})
	c := newTestCaller(t, core)
	_, err := c.call(t.Context(), "version", nil)
	require.NoError(t, err)

	require.NoError(t, c.listen(t.Context(), 200*time.Millisecond, 20*time.Millisecond))
	assert.Positive(t, core.pings.Load())

	core = newFakeCore(t, &fakeCoreOptions{dropOnPing: true})
	c = newTestCaller(t, core)
	_, err = c.call(t.Context(), "version", nil)
	require.NoError(t, err)
	require.EqualError(t, c.listen(t.Context(), time.Minute, 10*time.Millisecond), "core disconnected")
}

func TestCallerListenStopsWhenThePingCannotBeSent(t *testing.T) {
	t.Parallel()
	var failing atomic.Bool
	// Only data chunks fail: an acknowledgement of the reply that failed
	// first would end the link before the ping is tried.
	c := newTestCaller(t, newFakeCore(t, &fakeCoreOptions{rxWrite: func(chunk []byte) error {
		h, _, err := apigatt.ParseChunk(chunk)
		if failing.Load() && err == nil && !h.Ack {
			return errors.New("not connected")
		}
		return nil
	}}))
	_, err := c.call(t.Context(), "version", nil)
	require.NoError(t, err)
	failing.Store(true)

	require.ErrorContains(t, c.listen(t.Context(), time.Minute, time.Millisecond), "write chunk")
}

func TestParseRequest(t *testing.T) {
	t.Parallel()
	method, params, err := parseRequest("  version  ")
	require.NoError(t, err)
	assert.Equal(t, "version", method)
	assert.Nil(t, params)

	method, params, err = parseRequest(`media.search  {"query": "mario kart"} `)
	require.NoError(t, err)
	assert.Equal(t, "media.search", method)
	assert.JSONEq(t, `{"query": "mario kart"}`, string(params))

	_, _, err = parseRequest("media.search {")
	require.EqualError(t, err, `params of "media.search" are not valid JSON`)
}
