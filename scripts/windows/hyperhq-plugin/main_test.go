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
	"bufio"
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	siotypes "github.com/zishang520/socket.io/v3/pkg/types"
)

type fakeSocket struct {
	emitErr   error
	listeners map[string]siotypes.EventListener
	emits     []fakeEmit
}

type fakeEmit struct {
	event string
	args  []any
}

func (f *fakeSocket) Emit(event string, args ...any) error {
	f.emits = append(f.emits, fakeEmit{event: event, args: args})
	return f.emitErr
}

func (f *fakeSocket) On(event siotypes.EventName, listeners ...siotypes.EventListener) error {
	if f.listeners == nil {
		f.listeners = make(map[string]siotypes.EventListener)
	}
	if len(listeners) > 0 {
		f.listeners[string(event)] = listeners[0]
	}
	return nil
}

//nolint:revive // hqSocket mirrors the upstream Socket.IO Id method name.
func (f *fakeSocket) Id() string {
	return "fake-socket"
}

func TestClearSessionTokenMakesRequestUnauthenticated(t *testing.T) {
	b := &bridge{
		ctx:          context.Background(),
		socket:       &fakeSocket{},
		sessionToken: "stale-token",
		pendingData:  make(map[string]chan hqDataResponse),
	}

	b.clearSessionToken()

	_, err := b.requestDataCtx(context.Background(), "getSystems", nil)
	if err == nil || !strings.Contains(err.Error(), "no session token") {
		t.Fatalf("requestDataCtx() error = %v, want no session token", err)
	}
}

func TestHandleDataResponseRoutesAndCleansPendingRequest(t *testing.T) {
	ch := make(chan hqDataResponse, 1)
	b := &bridge{pendingData: map[string]chan hqDataResponse{"req-1": ch}}

	b.handleDataResponse(map[string]any{
		"requestId": "req-1",
		"success":   true,
		"data":      []string{"ok"},
	})

	select {
	case resp := <-ch:
		if resp.RequestID != "req-1" || !resp.Success {
			t.Fatalf("response = %+v, want req-1 success", resp)
		}
	default:
		t.Fatal("pending response channel did not receive routed response")
	}
	if _, ok := b.pendingData["req-1"]; ok {
		t.Fatal("pendingData still contains completed request")
	}

	b.handleDataResponse(map[string]any{
		"requestId": "unknown",
		"success":   false,
		"error":     "boom",
	})
	if len(b.pendingData) != 0 {
		t.Fatalf("pendingData length = %d, want 0", len(b.pendingData))
	}
}

func TestRequestDataCtxCleansPendingOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	fake := &fakeSocket{}
	b := &bridge{
		ctx:          context.Background(),
		socket:       fake,
		sessionToken: "token",
		pendingData:  make(map[string]chan hqDataResponse),
	}

	_, err := b.requestDataCtx(ctx, "getSystems", nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("requestDataCtx() error = %v, want context.Canceled", err)
	}
	if len(b.pendingData) != 0 {
		t.Fatalf("pendingData length = %d, want 0", len(b.pendingData))
	}
	if len(fake.emits) != 1 || fake.emits[0].event != "requestData" {
		t.Fatalf("emits = %+v, want one requestData emit", fake.emits)
	}
}

func TestHandleLifecycleRequestEmitsResponseAndCancelsShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fake := &fakeSocket{}
	b := &bridge{
		ctx:          ctx,
		cancel:       cancel,
		socket:       fake,
		pluginID:     "plugin-id",
		sessionToken: "token",
	}

	b.handleLifecycleRequest(map[string]any{
		"id":     "init-1",
		"method": hqMethodInitialize,
	})

	if len(fake.emits) != 1 || fake.emits[0].event != "plugin:response" {
		t.Fatalf("emits = %+v, want plugin:response", fake.emits)
	}
	resp, ok := fake.emits[0].args[0].(hqPluginResponse)
	if !ok {
		t.Fatalf("response type = %T, want hqPluginResponse", fake.emits[0].args[0])
	}
	if resp.ID != "init-1" || resp.SessionToken != "token" {
		t.Fatalf("response = %+v, want id init-1 and token", resp)
	}

	b.handleLifecycleRequest(map[string]any{"method": hqMethodTest})
	if len(fake.emits) != 1 {
		t.Fatalf("missing-id request emitted response; emits = %+v", fake.emits)
	}

	b.handleLifecycleRequest(map[string]any{
		"id":     "shutdown-1",
		"method": hqMethodShutdown,
	})
	select {
	case <-ctx.Done():
	default:
		t.Fatal("shutdown request did not cancel bridge context")
	}
}

func TestPipeSessionWriteUsesCapturedWriterAndDropsAfterCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var oldBuf bytes.Buffer
	var newBuf bytes.Buffer
	b := &bridge{ctx: context.Background(), pipeWriter: bufio.NewWriter(&newBuf)}
	session := &pipeSession{
		ctx:    ctx,
		bridge: b,
		writer: bufio.NewWriter(&oldBuf),
	}

	session.writePipeEvent(&pipeEvent{Event: "Systems"})
	if oldBuf.Len() == 0 {
		t.Fatal("captured session writer received no output")
	}
	if newBuf.Len() != 0 {
		t.Fatal("shared bridge writer received session output")
	}

	oldBuf.Reset()
	cancel()
	session.writePipeEvent(&pipeEvent{Event: "Games"})
	if oldBuf.Len() != 0 {
		t.Fatalf("canceled session wrote %q", oldBuf.String())
	}
}

func TestDecodeFirst(t *testing.T) {
	var req hqLifecycleRequest
	if err := decodeFirst([]any{map[string]any{"id": "1", "method": hqMethodTest}}, &req); err != nil {
		t.Fatalf("decodeFirst() unexpected error: %v", err)
	}
	if req.ID != "1" || req.Method != hqMethodTest {
		t.Fatalf("decoded request = %+v", req)
	}

	if err := decodeFirst(nil, &req); err == nil {
		t.Fatal("decodeFirst(nil) error = nil, want error")
	}
	if err := decodeFirst([]any{map[string]any{"id": make(chan int)}}, &req); err == nil {
		t.Fatal("decodeFirst(unmarshalable) error = nil, want error")
	}
}

func TestUnmarshalIfPresent(t *testing.T) {
	var out []hqRawSystem
	if err := unmarshalIfPresent(nil, &out); err != nil {
		t.Fatalf("unmarshalIfPresent(nil) error = %v", err)
	}
	if err := unmarshalIfPresent(json.RawMessage("null"), &out); err != nil {
		t.Fatalf("unmarshalIfPresent(null) error = %v", err)
	}

	raw := json.RawMessage(`[{"name":"NES","referenceId":"nes","platform":"nes"}]`)
	if err := unmarshalIfPresent(raw, &out); err != nil {
		t.Fatalf("unmarshalIfPresent(valid) error = %v", err)
	}
	if len(out) != 1 || out[0].ReferenceID != "nes" {
		t.Fatalf("decoded systems = %+v", out)
	}

	if err := unmarshalIfPresent(json.RawMessage(`{"bad"`), &out); err == nil {
		t.Fatal("unmarshalIfPresent(invalid) error = nil, want error")
	}
}

func TestNewRequestID(t *testing.T) {
	seen := make(map[string]bool)
	for range 100 {
		id := newRequestID()
		if len(id) != 24 {
			t.Fatalf("newRequestID() length = %d, want 24", len(id))
		}
		if _, err := hex.DecodeString(id); err != nil {
			t.Fatalf("newRequestID() produced non-hex string: %v", err)
		}
		if seen[id] {
			t.Fatalf("newRequestID() duplicate id %q", id)
		}
		seen[id] = true
	}
}
