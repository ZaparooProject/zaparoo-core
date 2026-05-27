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
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeSocket struct {
	emitErr          error
	connectListener  func()
	errorListener    func(any)
	disconnectListen func(any)
	listeners        map[string]func(any)
	emits            []fakeEmit
}

type fakeEmit struct {
	event string
	args  []any
}

func (f *fakeSocket) Emit(event string, args ...any) error {
	f.emits = append(f.emits, fakeEmit{event: event, args: args})
	return f.emitErr
}

func (f *fakeSocket) OnEvent(event string, listener func(any)) {
	if f.listeners == nil {
		f.listeners = make(map[string]func(any))
	}
	f.listeners[event] = listener
}

func (f *fakeSocket) OnConnect(listener func()) {
	f.connectListener = listener
}

func (f *fakeSocket) OnConnectError(listener func(any)) {
	f.errorListener = listener
}

func (f *fakeSocket) OnDisconnect(listener func(any)) {
	f.disconnectListen = listener
}

func (f *fakeSocket) Connect() {}

func (f *fakeSocket) ID() string {
	return "fake-socket"
}

type manifestSocketIO struct {
	Namespace string `json:"namespace"`
	Enabled   bool   `json:"enabled"`
}

type manifestCommunicationSocketIO struct {
	Enabled bool `json:"enabled"`
}

type manifestCommunication struct {
	SocketIO  manifestCommunicationSocketIO `json:"socketio"`
	Preferred string                        `json:"preferred"`
	Fallback  string                        `json:"fallback"`
}

type pluginManifest struct {
	SocketIO      manifestSocketIO      `json:"socketio"`
	Communication manifestCommunication `json:"communication"`
	Type          string                `json:"type"`
	Executable    string                `json:"executable"`
}

func TestPluginManifestMatchesHyperHQExecutableSocketIODocs(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile("plugin.json")
	if err != nil {
		t.Fatalf("read plugin.json: %v", err)
	}

	var manifest pluginManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("unmarshal plugin.json: %v", err)
	}

	if manifest.Type != "executable" {
		t.Fatalf("manifest type = %q, want executable", manifest.Type)
	}
	if manifest.Executable != "zaparoo-hyperhq.exe" {
		t.Fatalf("manifest executable = %q, want zaparoo-hyperhq.exe", manifest.Executable)
	}
	if manifest.Communication.Preferred != "socketio" || manifest.Communication.Fallback != "stdio" {
		t.Fatalf("manifest communication = %+v, want socketio preferred with stdio fallback", manifest.Communication)
	}
	if !manifest.Communication.SocketIO.Enabled {
		t.Fatal("manifest communication.socketio.enabled = false, want true")
	}
	if !manifest.SocketIO.Enabled || manifest.SocketIO.Namespace != "/plugin" {
		t.Fatalf("manifest socketio = %+v, want enabled namespace /plugin", manifest.SocketIO)
	}
}

func TestSocketIOManagerURLUsesDefaultEnginePath(t *testing.T) {
	got := socketIOManagerURL("52789")
	want := "http://localhost:52789/socket.io/"
	if got != want {
		t.Fatalf("socketIOManagerURL() = %q, want %q", got, want)
	}
}

func TestPluginLogPathPrefersPluginDataDir(t *testing.T) {
	t.Setenv("PLUGIN_DATA_DIR", filepath.Join(t.TempDir(), "plugin-data"))

	path := pluginLogPath()
	if filepath.Base(path) != "zaparoo-hyperhq.log" {
		t.Fatalf("pluginLogPath() = %q, want zaparoo-hyperhq.log file", path)
	}
	if !strings.Contains(path, "plugin-data") {
		t.Fatalf("pluginLogPath() = %q, want PLUGIN_DATA_DIR path", path)
	}
}

func TestPluginRegisterIncludesAuthFields(t *testing.T) {
	payload := hqPluginRegister{
		ID:           "zaparoo-hyperhq",
		Version:      pluginVersion,
		Type:         "executable",
		SessionToken: "session-token",
		Capabilities: []string{"games"},
	}

	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal register payload: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal register payload: %v", err)
	}
	if got["type"] != "executable" || got["sessionToken"] != "session-token" {
		t.Fatalf("register payload = %v, want executable type and sessionToken", got)
	}
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

	if len(fake.emits) != 2 || fake.emits[0].event != "plugin:response" || fake.emits[1].event != "response" {
		t.Fatalf("emits = %+v, want plugin:response and response", fake.emits)
	}
	resp, ok := fake.emits[0].args[0].(hqPluginResponse)
	if !ok {
		t.Fatalf("response type = %T, want hqPluginResponse", fake.emits[0].args[0])
	}
	if resp.ID != "init-1" || resp.SessionToken != "token" || resp.Data != "initialized" || resp.Timestamp == 0 {
		t.Fatalf("response = %+v, want id init-1, token, initialized data, and timestamp", resp)
	}

	b.handleLifecycleRequest(map[string]any{
		"id":     "test-1",
		"method": hqMethodTest,
	})
	if len(fake.emits) != 4 || fake.emits[2].event != "plugin:response" || fake.emits[3].event != "response" {
		t.Fatalf("emits = %+v, want second plugin:response and response", fake.emits)
	}
	resp, ok = fake.emits[2].args[0].(hqPluginResponse)
	if !ok {
		t.Fatalf("test response type = %T, want hqPluginResponse", fake.emits[2].args[0])
	}
	if resp.ID != "test-1" || resp.Data != true {
		t.Fatalf("test response = %+v, want id test-1 and true data", resp)
	}

	b.handleLifecycleRequest(map[string]any{"method": hqMethodTest})
	if len(fake.emits) != 4 {
		t.Fatalf("missing-id request emitted response; emits = %+v", fake.emits)
	}

	b.handleLifecycleRequest(map[string]any{
		"id":     "shutdown-1",
		"method": hqMethodShutdown,
	})
	if len(fake.emits) != 6 || fake.emits[4].event != "plugin:response" || fake.emits[5].event != "response" {
		t.Fatalf("emits = %+v, want shutdown plugin:response and response", fake.emits)
	}
	resp, ok = fake.emits[4].args[0].(hqPluginResponse)
	if !ok {
		t.Fatalf("shutdown response type = %T, want hqPluginResponse", fake.emits[4].args[0])
	}
	if resp.ID != "shutdown-1" || resp.Data != "ok" {
		t.Fatalf("shutdown response = %+v, want id shutdown-1 and ok data", resp)
	}
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

func TestDecodeSystemsDataAcceptsArrayAndWrappedObject(t *testing.T) {
	systems, err := decodeSystemsData(json.RawMessage(`[{"id":"sys-1","name":"NES","referenceId":"nes","platform":"nes"}]`))
	if err != nil {
		t.Fatalf("decodeSystemsData(array) error = %v", err)
	}
	if len(systems) != 1 || systems[0].ID != "sys-1" || systems[0].ReferenceID != "nes" {
		t.Fatalf("decodeSystemsData(array) = %+v, want NES system with id", systems)
	}

	systems, err = decodeSystemsData(json.RawMessage(`{"systems":[{"id":"sys-2","name":"SNES","referenceId":"snes","platform":"snes"}]}`))
	if err != nil {
		t.Fatalf("decodeSystemsData(wrapped) error = %v", err)
	}
	if len(systems) != 1 || systems[0].ID != "sys-2" || systems[0].ReferenceID != "snes" {
		t.Fatalf("decodeSystemsData(wrapped) = %+v, want SNES system with id", systems)
	}
}

func TestGameRequestVariantsUsesNameAsSystemIDCandidate(t *testing.T) {
	variants := gameRequestVariants(systemQueryTarget{ID: "sys-id", Name: "System Name", ReferenceID: "ref-id"})
	if len(variants) != 1 {
		t.Fatalf("variants length = %d, want 1", len(variants))
	}
	if variants[0].Method != gameListMethod || variants[0].ParamKey != gameListParamKey {
		t.Fatalf("variant = %+v, want getGamesForSystem/systemId", variants[0])
	}
	if variants[0].Label != "name" || variants[0].ParamValue != "System Name" {
		t.Fatalf("variant = %+v, want name", variants[0])
	}

	variants = gameRequestVariants(systemQueryTarget{ReferenceID: "ref-id"})
	if len(variants) != 1 || variants[0].Label != "referenceId" || variants[0].ParamValue != "ref-id" {
		t.Fatalf("reference-only variants = %+v, want referenceId", variants)
	}
}

func TestDecodeGamesDataAcceptsArrayAndWrappedObject(t *testing.T) {
	games, err := decodeGamesData(json.RawMessage(`[{"id":"1","title":"Game","platform":"nes"}]`))
	if err != nil {
		t.Fatalf("decodeGamesData(array) error = %v", err)
	}
	if len(games) != 1 || games[0].ID != "1" || games[0].Title != "Game" {
		t.Fatalf("decodeGamesData(array) = %+v, want game 1 title", games)
	}

	games, err = decodeGamesData(json.RawMessage(`[{"id":"name-1","name":"Named Game","platform":"nes"}]`))
	if err != nil {
		t.Fatalf("decodeGamesData(name array) error = %v", err)
	}
	if len(games) != 1 || games[0].ID != "name-1" || games[0].Title != "Named Game" {
		t.Fatalf("decodeGamesData(name array) = %+v, want title from name", games)
	}

	games, err = decodeGamesData(json.RawMessage(`{"games":[{"id":"2","title":"Game 2","platform":"snes"}]}`))
	if err != nil {
		t.Fatalf("decodeGamesData(wrapped) error = %v", err)
	}
	if len(games) != 1 || games[0].ID != "2" || games[0].Title != "Game 2" {
		t.Fatalf("decodeGamesData(wrapped) = %+v, want game 2 title", games)
	}

	games, err = decodeGamesData(json.RawMessage(`{"games":[{"id":"name-2","name":"Wrapped Named Game","platform":"snes"}]}`))
	if err != nil {
		t.Fatalf("decodeGamesData(name wrapped) error = %v", err)
	}
	if len(games) != 1 || games[0].ID != "name-2" || games[0].Title != "Wrapped Named Game" {
		t.Fatalf("decodeGamesData(name wrapped) = %+v, want title from name", games)
	}

	games, err = decodeGamesData(json.RawMessage(`[{"gameId":"game-1","referenceId":"ref-1","fileName":"rom.sfc","systemName":"SNES"}]`))
	if err != nil {
		t.Fatalf("decodeGamesData(rom fields) error = %v", err)
	}
	if len(games) != 1 || games[0].ID != "game-1" || games[0].Title != "rom.sfc" || games[0].Platform != "SNES" {
		t.Fatalf("decodeGamesData(rom fields) = %+v, want normalized ROM fields", games)
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
