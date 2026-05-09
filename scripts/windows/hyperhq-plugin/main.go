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

// hyperhq-plugin is the Zaparoo bridge for HyperHQ. HyperHQ launches this
// executable as a plugin and exposes a Socket.IO endpoint on localhost; the
// plugin connects to that endpoint, authenticates, and forwards game events to
// Zaparoo Core via a named pipe. Commands flow the other way: Zaparoo Core
// requests system/game lists and game launches over the pipe, and this bridge
// translates them into HyperHQ Socket.IO requestData calls.
//
// HyperHQ wire protocol (per https://docs.hyperai.io/docs/plugins/):
//   - authenticate {pluginId, challenge} -> authenticated {success, sessionToken}
//   - plugin:register {id, version, capabilities}
//   - subscribeEvents [event names] -> eventsSubscribed {events}
//   - request {id, method, data} -> emit plugin:response {id, type, data, sessionToken}
//   - emit requestData {method, params, requestId, sessionToken} -> dataResponse {requestId, success, data?, error?}
//   - hyperHqEvent {type, data, timestamp} carries gameLaunched / gameClosed / ...
package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/Microsoft/go-winio"
	socketio "github.com/zishang520/socket.io/clients/socket/v3"
	siotypes "github.com/zishang520/socket.io/v3/pkg/types"
)

const (
	pipeName        = `\\.\pipe\zaparoo-hyperhq-ipc`
	pluginNamespace = "/plugin"
	pluginVersion   = "0.1.0"

	pipeReconnectDelay = 2 * time.Second
	pipeBufferMax      = 16 * 1024 * 1024 // 16MB to match Zaparoo Core scanner buffer
	requestTimeout     = 30 * time.Second
	launchAckTimeout   = 5 * time.Second
)

// HyperHQ event types carried on the hyperHqEvent envelope.
const (
	hqEventGameLaunched = "gameLaunched"
	hqEventGameClosed   = "gameClosed"
)

// HyperHQ request methods we handle on the lifecycle `request` channel.
const (
	hqMethodInitialize = "initialize"
	hqMethodExecute    = "execute"
	hqMethodTest       = "test"
	hqMethodShutdown   = "shutdown"
)

// Pipe wire-protocol types — mirror the Zaparoo Core side in
// pkg/platforms/windows/hyperhq.go. PascalCase keys.
//
//nolint:tagliatelle // PascalCase tags must match the Zaparoo Core pipe peer.
type pipeEvent struct {
	Event             string         `json:"Event"`
	ID                string         `json:"Id,omitempty"`
	Title             string         `json:"Title,omitempty"`
	Platform          string         `json:"Platform,omitempty"`
	SystemReferenceID string         `json:"SystemReferenceId,omitempty"`
	Error             string         `json:"Error,omitempty"`
	Systems           []hqSystemInfo `json:"Systems,omitempty"`
	Games             []hqGameInfo   `json:"Games,omitempty"`
}

//nolint:tagliatelle // PascalCase tags must match the Zaparoo Core pipe peer.
type pipeCommand struct {
	Command           string `json:"Command"`
	ID                string `json:"Id,omitempty"`
	SystemReferenceID string `json:"SystemReferenceId,omitempty"`
}

//nolint:tagliatelle // PascalCase tags must match the Zaparoo Core pipe peer.
type hqSystemInfo struct {
	Name        string `json:"Name"`
	ReferenceID string `json:"ReferenceId"`
	Platform    string `json:"Platform"`
}

//nolint:tagliatelle // PascalCase tags must match the Zaparoo Core pipe peer.
type hqGameInfo struct {
	ID       string `json:"Id"`
	Title    string `json:"Title"`
	Platform string `json:"Platform"`
}

// HyperHQ Socket.IO payload shapes (camelCase per the API reference).

type hqAuthRequest struct {
	PluginID  string `json:"pluginId"`
	Challenge string `json:"challenge"`
}

type hqAuthResponse struct {
	SessionToken string `json:"sessionToken"`
	Error        string `json:"error"`
	Success      bool   `json:"success"`
}

type hqPluginRegister struct {
	ID           string   `json:"id"`
	Version      string   `json:"version"`
	Capabilities []string `json:"capabilities"`
}

// hqLifecycleRequest is the envelope HyperHQ uses for plugin-directed calls
// (initialize / execute / test / shutdown).
type hqLifecycleRequest struct {
	ID     string          `json:"id"`
	Method string          `json:"method"`
	Data   json.RawMessage `json:"data,omitempty"`
}

// hqPluginResponse is the reply sent back via plugin:response.
type hqPluginResponse struct {
	Data         any    `json:"data,omitempty"`
	ID           string `json:"id"`
	Type         string `json:"type"`
	SessionToken string `json:"sessionToken,omitempty"`
}

// hqRequestData is the envelope plugins send to call HyperHQ data methods.
type hqRequestData struct {
	Params       map[string]any `json:"params,omitempty"`
	Method       string         `json:"method"`
	RequestID    string         `json:"requestId"`
	SessionToken string         `json:"sessionToken"`
}

// hqDataResponse is HyperHQ's reply on the dataResponse channel.
type hqDataResponse struct {
	RequestID string          `json:"requestId"`
	Error     string          `json:"error,omitempty"`
	Data      json.RawMessage `json:"data,omitempty"`
	Success   bool            `json:"success"`
}

// hqEventEnvelope is the wrapper HyperHQ uses to deliver media events on the
// hyperHqEvent channel.
type hqEventEnvelope struct {
	Type      string          `json:"type"`
	Timestamp string          `json:"timestamp"`
	Data      json.RawMessage `json:"data"`
}

type hqGameLaunchedPayload struct {
	GameID    string `json:"gameId"`
	GameName  string `json:"gameName"`
	SystemID  string `json:"systemId"`
	Timestamp string `json:"timestamp"`
}

type hqGameClosedPayload struct {
	GameID    string `json:"gameId"`
	GameName  string `json:"gameName"`
	SystemID  string `json:"systemId"`
	Timestamp string `json:"timestamp"`
	ExitCode  int    `json:"exitCode"`
	PlayTime  int64  `json:"playTime"`
}

type hqRawSystem struct {
	Name        string `json:"name"`
	ReferenceID string `json:"referenceId"`
	Platform    string `json:"platform"`
}

type hqRawGame struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Platform string `json:"platform"`
}

// bridge owns the HyperHQ Socket.IO connection and forwards activity to the
// pipe writer. All pipe writes go through writePipeEvent which serialises on
// pipeMu so the framing stays line-delimited. dataResponse routing keeps a
// per-requestId channel in pendingData; the dataResponse listener looks up the
// channel by requestId and hands the payload over.
type bridge struct {
	ctx           context.Context
	cancel        context.CancelFunc
	socket        *socketio.Socket
	pipeWriter    *bufio.Writer
	pendingData   map[string]chan hqDataResponse
	pluginID      string
	authChallenge string
	sessionToken  string
	sessionMu     sync.RWMutex
	pendingMu     sync.Mutex
	pipeMu        sync.Mutex
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	log.SetPrefix("[zaparoo-hyperhq] ")

	if err := run(); err != nil {
		log.Printf("fatal: %v", err)
		os.Exit(1)
	}
}

func run() error {
	pluginID := os.Getenv("HYPERHQ_PLUGIN_ID")
	authChallenge := os.Getenv("HYPERHQ_AUTH_CHALLENGE")
	socketPort := os.Getenv("HYPERHQ_SOCKET_PORT")

	if pluginID == "" || authChallenge == "" || socketPort == "" {
		return fmt.Errorf(
			"missing required HyperHQ env vars "+
				"(HYPERHQ_PLUGIN_ID present=%t "+
				"HYPERHQ_AUTH_CHALLENGE present=%t "+
				"HYPERHQ_SOCKET_PORT present=%t)",
			pluginID != "", authChallenge != "", socketPort != "",
		)
	}

	if _, err := strconv.Atoi(socketPort); err != nil {
		return fmt.Errorf("HYPERHQ_SOCKET_PORT is not a valid port: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		log.Printf("received signal %s, shutting down", sig)
		cancel()
	}()

	b := &bridge{
		ctx:           ctx,
		cancel:        cancel,
		pluginID:      pluginID,
		authChallenge: authChallenge,
		pendingData:   make(map[string]chan hqDataResponse),
	}

	if err := b.connectSocket(socketPort); err != nil {
		return fmt.Errorf("failed to connect to HyperHQ: %w", err)
	}

	// pipe loop runs until ctx is cancelled. Reconnects on disconnect.
	b.runPipeLoop()
	log.Print("plugin exiting")
	return nil
}

// onSocket registers a typed listener on the underlying event emitter without
// repeating the EventName/EventListener cast at every call site.
func onSocket(sock *socketio.Socket, event string, listener func(...any)) {
	if err := sock.On(siotypes.EventName(event), siotypes.EventListener(listener)); err != nil {
		log.Printf("warning: register listener for %q failed: %v", event, err)
	}
}

// connectSocket establishes the Socket.IO connection to HyperHQ and registers
// all event handlers. It blocks until the initial connect+authenticate cycle
// completes (or fails). After that the socket runs in the background and
// reconnects on its own.
func (b *bridge) connectSocket(port string) error {
	url := fmt.Sprintf("http://localhost:%s%s", port, pluginNamespace)
	// #nosec G706 -- port is validated as numeric in run() before reaching here.
	log.Printf("connecting to HyperHQ at %s", url)

	opts := socketio.DefaultOptions()
	opts.SetReconnection(true)
	opts.SetReconnectionDelay(1000)
	opts.SetReconnectionDelayMax(5000)

	sock, err := socketio.Connect(url, opts)
	if err != nil {
		return fmt.Errorf("socket.io connect: %w", err)
	}
	b.socket = sock

	authDone := make(chan error, 1)
	authOnce := sync.Once{}
	// signalAuth reports the first auth result via authDone (used by the
	// initial connect). Subsequent reconnect failures are logged so operators
	// can see them, since Socket.IO re-fires "connect"/"authenticated" on each
	// reconnect but the channel send is consumed only once.
	signalAuth := func(err error) {
		delivered := false
		authOnce.Do(func() {
			authDone <- err
			delivered = true
		})
		if !delivered && err != nil {
			log.Printf("post-reconnect auth error: %v", err)
		}
	}

	onSocket(sock, "connect", func(_ ...any) {
		// #nosec G706 -- sock.Id() is a Socket.IO-generated session token, not user input.
		log.Printf("HyperHQ socket connected (id=%s)", sock.Id())
		req := hqAuthRequest{PluginID: b.pluginID, Challenge: b.authChallenge}
		if emitErr := sock.Emit("authenticate", req); emitErr != nil {
			signalAuth(fmt.Errorf("emit authenticate: %w", emitErr))
		}
	})

	onSocket(sock, "authenticated", func(args ...any) {
		var resp hqAuthResponse
		if err := decodeFirst(args, &resp); err != nil {
			signalAuth(fmt.Errorf("decode authenticated: %w", err))
			return
		}
		if !resp.Success {
			signalAuth(fmt.Errorf("authentication rejected: %s", resp.Error))
			return
		}
		log.Printf("HyperHQ authenticated (sessionToken length=%d)", len(resp.SessionToken))

		b.sessionMu.Lock()
		b.sessionToken = resp.SessionToken
		b.sessionMu.Unlock()

		// After auth, register the plugin and subscribe to media events.
		// Either failure leaves the bridge unable to receive game events, so
		// fail the connect cycle and let Socket.IO reconnect to retry.
		registerPayload := hqPluginRegister{
			ID:           b.pluginID,
			Version:      pluginVersion,
			Capabilities: []string{"games", "launch", "events"},
		}
		if err := sock.Emit("plugin:register", registerPayload); err != nil {
			signalAuth(fmt.Errorf("plugin:register emit: %w", err))
			return
		}

		// HyperHQ expects subscribeEvents as a bare array of event names, not
		// an enveloped {events:[...]} object.
		if err := sock.Emit("subscribeEvents", []string{hqEventGameLaunched, hqEventGameClosed}); err != nil {
			signalAuth(fmt.Errorf("subscribeEvents emit: %w", err))
			return
		}

		signalAuth(nil)
	})

	onSocket(sock, "eventsSubscribed", func(args ...any) {
		log.Printf("HyperHQ confirmed event subscription: %v", args)
	})

	onSocket(sock, "connect_error", func(args ...any) {
		log.Printf("HyperHQ connect_error: %v", args)
		signalAuth(fmt.Errorf("connect error: %v", args))
	})

	onSocket(sock, "disconnect", func(args ...any) {
		log.Printf("HyperHQ socket disconnected: %v", args)
	})

	onSocket(sock, "request", b.handleLifecycleRequest)
	onSocket(sock, "dataResponse", b.handleDataResponse)
	onSocket(sock, "hyperHqEvent", b.handleHyperHqEvent)

	select {
	case err := <-authDone:
		return err
	case <-time.After(requestTimeout):
		return errors.New("timeout waiting for HyperHQ authentication")
	case <-b.ctx.Done():
		return b.ctx.Err()
	}
}

// handleLifecycleRequest decodes a `request` event from HyperHQ, dispatches by
// method, and replies via plugin:response. A missing id means HyperHQ wasn't
// expecting a reply, so we still process the side effect (e.g. shutdown) but
// skip the response emit.
func (b *bridge) handleLifecycleRequest(args ...any) {
	var req hqLifecycleRequest
	if err := decodeFirst(args, &req); err != nil {
		log.Printf("request decode failed: %v", err)
		return
	}
	log.Printf("HyperHQ request: id=%s method=%s", req.ID, req.Method)

	respData := map[string]any{"success": true}

	switch req.Method {
	case hqMethodInitialize:
		respData["pluginId"] = b.pluginID
		respData["version"] = pluginVersion
	case hqMethodExecute, hqMethodTest:
		// Nothing extra — Zaparoo's bridge doesn't implement test commands.
	case hqMethodShutdown:
		log.Print("HyperHQ requested shutdown")
		defer b.cancel()
	default:
		log.Printf("unknown request method: %s", req.Method)
	}

	if req.ID == "" {
		return
	}

	b.sessionMu.RLock()
	token := b.sessionToken
	b.sessionMu.RUnlock()

	resp := hqPluginResponse{
		ID:           req.ID,
		Type:         "response",
		Data:         respData,
		SessionToken: token,
	}
	if err := b.socket.Emit("plugin:response", resp); err != nil {
		log.Printf("plugin:response emit (id=%s): %v", req.ID, err)
	}
}

// handleDataResponse routes a dataResponse to whichever requestData call is
// waiting on this requestId. Unknown requestIds are logged and dropped; this
// covers fire-and-forget replies (e.g. launchGame) and stale responses that
// arrived after a timeout.
func (b *bridge) handleDataResponse(args ...any) {
	var resp hqDataResponse
	if err := decodeFirst(args, &resp); err != nil {
		log.Printf("dataResponse decode failed: %v", err)
		return
	}

	b.pendingMu.Lock()
	ch, ok := b.pendingData[resp.RequestID]
	if ok {
		delete(b.pendingData, resp.RequestID)
	}
	b.pendingMu.Unlock()

	if !ok {
		// No waiter — likely the launchGame fire-and-forget path. Surface
		// errors so operators can spot bad game IDs.
		if !resp.Success && resp.Error != "" {
			log.Printf("dataResponse (no waiter) error for %s: %s", resp.RequestID, resp.Error)
		}
		return
	}

	// Buffered channel of size 1; this never blocks.
	ch <- resp
}

// handleHyperHqEvent dispatches the unified hyperHqEvent envelope by type.
func (b *bridge) handleHyperHqEvent(args ...any) {
	var env hqEventEnvelope
	if err := decodeFirst(args, &env); err != nil {
		log.Printf("hyperHqEvent decode failed: %v", err)
		return
	}

	switch env.Type {
	case hqEventGameLaunched:
		b.handleGameLaunched(env.Data)
	case hqEventGameClosed:
		b.handleGameClosed(env.Data)
	default:
		log.Printf("ignoring hyperHqEvent type=%s", env.Type)
	}
}

func (b *bridge) handleGameLaunched(data json.RawMessage) {
	var payload hqGameLaunchedPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		log.Printf("gameLaunched decode failed: %v", err)
		return
	}
	log.Printf(
		"HyperHQ gameLaunched: id=%s name=%q systemId=%s",
		payload.GameID, payload.GameName, payload.SystemID,
	)
	b.writePipeEvent(&pipeEvent{
		Event:             "MediaStarted",
		ID:                payload.GameID,
		Title:             payload.GameName,
		SystemReferenceID: payload.SystemID,
	})
}

func (b *bridge) handleGameClosed(data json.RawMessage) {
	var payload hqGameClosedPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		log.Printf("gameClosed decode failed: %v", err)
		return
	}
	log.Printf(
		"HyperHQ gameClosed: id=%s name=%q exitCode=%d",
		payload.GameID, payload.GameName, payload.ExitCode,
	)
	b.writePipeEvent(&pipeEvent{
		Event: "MediaStopped",
		ID:    payload.GameID,
		Title: payload.GameName,
	})
}

// runPipeLoop maintains a persistent connection to the Zaparoo Core named pipe
// and reconnects on failure until the context is cancelled.
func (b *bridge) runPipeLoop() {
	for {
		select {
		case <-b.ctx.Done():
			return
		default:
		}

		if err := b.servePipeOnce(); err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
			log.Printf("pipe session ended: %v", err)
		}

		select {
		case <-b.ctx.Done():
			return
		case <-time.After(pipeReconnectDelay):
		}
	}
}

func (b *bridge) servePipeOnce() error {
	dialCtx, cancel := context.WithTimeout(b.ctx, requestTimeout)
	defer cancel()

	conn, err := winio.DialPipeContext(dialCtx, pipeName)
	if err != nil {
		return fmt.Errorf("dial pipe: %w", err)
	}
	defer func() {
		if closeErr := conn.Close(); closeErr != nil {
			log.Printf("pipe close error: %v", closeErr)
		}
	}()

	log.Printf("connected to Zaparoo Core pipe %s", pipeName)

	writer := bufio.NewWriter(conn)
	b.pipeMu.Lock()
	b.pipeWriter = writer
	b.pipeMu.Unlock()

	defer func() {
		b.pipeMu.Lock()
		b.pipeWriter = nil
		b.pipeMu.Unlock()
	}()

	// On every (re)connect, push the current systems list so Zaparoo Core can
	// refresh its mapping. Best-effort: if HyperHQ isn't ready we log and move on.
	go b.pushSystems()

	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 4096), pipeBufferMax)

	for scanner.Scan() {
		select {
		case <-b.ctx.Done():
			return b.ctx.Err()
		default:
		}
		b.handlePipeCommand(scanner.Text())
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("pipe scanner: %w", err)
	}
	return errors.New("pipe closed by peer")
}

func (b *bridge) handlePipeCommand(line string) {
	if line == "" {
		return
	}

	var cmd pipeCommand
	if err := json.Unmarshal([]byte(line), &cmd); err != nil {
		log.Printf("invalid pipe command %q: %v", line, err)
		return
	}

	switch cmd.Command {
	case "Ping":
		// Heartbeat — no response required, the connection liveness is enough.
	case "GetSystems":
		go b.pushSystems()
	case "GetGamesForSystem":
		go b.pushGames(cmd.SystemReferenceID)
	case "Launch":
		go b.launchGame(cmd.ID)
	default:
		log.Printf("unknown pipe command: %s", cmd.Command)
	}
}

func (b *bridge) pushSystems() {
	systems, err := b.requestSystems()
	if err != nil {
		log.Printf("getSystems failed: %v", err)
		b.writePipeEvent(&pipeEvent{Event: "Systems", Error: err.Error()})
		return
	}

	out := make([]hqSystemInfo, 0, len(systems))
	for _, sys := range systems {
		out = append(out, hqSystemInfo(sys))
	}
	b.writePipeEvent(&pipeEvent{Event: "Systems", Systems: out})
}

func (b *bridge) pushGames(systemReferenceID string) {
	if systemReferenceID == "" {
		b.writePipeEvent(&pipeEvent{
			Event: "Games",
			Error: "missing SystemReferenceId",
		})
		return
	}

	games, err := b.requestGames(systemReferenceID)
	if err != nil {
		log.Printf("getGamesForSystem(%s) failed: %v", systemReferenceID, err)
		b.writePipeEvent(&pipeEvent{
			Event:             "Games",
			SystemReferenceID: systemReferenceID,
			Error:             err.Error(),
		})
		return
	}

	out := make([]hqGameInfo, 0, len(games))
	for _, g := range games {
		out = append(out, hqGameInfo(g))
	}
	b.writePipeEvent(&pipeEvent{
		Event:             "Games",
		SystemReferenceID: systemReferenceID,
		Games:             out,
	})
}

// launchGame is fire-and-forget: HyperHQ acknowledges via the next
// gameLaunched event, not via the immediate dataResponse. We still emit
// through requestData (with a short waiter) so that errors like an unknown
// gameId surface in logs.
func (b *bridge) launchGame(id string) {
	if id == "" {
		log.Print("launchGame called with empty id")
		return
	}

	ctx, cancel := context.WithTimeout(b.ctx, launchAckTimeout)
	defer cancel()

	if _, err := b.requestDataCtx(ctx, "launchGame", map[string]any{"gameId": id}); err != nil {
		// Timeout here is expected — HyperHQ doesn't always send a synchronous
		// dataResponse for launchGame. Only log non-timeout failures.
		if !errors.Is(err, context.DeadlineExceeded) {
			log.Printf("launchGame(%s) failed: %v", id, err)
		}
	}
}

// requestSystems / requestGames issue HyperHQ's `requestData` envelope and
// decode the data portion of the dataResponse.
func (b *bridge) requestSystems() ([]hqRawSystem, error) {
	resp, err := b.requestData("getSystems", nil)
	if err != nil {
		return nil, err
	}
	var systems []hqRawSystem
	if err := unmarshalIfPresent(resp, &systems); err != nil {
		return nil, fmt.Errorf("decode systems: %w", err)
	}
	return systems, nil
}

func (b *bridge) requestGames(systemID string) ([]hqRawGame, error) {
	resp, err := b.requestData("getGamesForSystem", map[string]any{
		"systemId": systemID,
	})
	if err != nil {
		return nil, err
	}
	var games []hqRawGame
	if err := unmarshalIfPresent(resp, &games); err != nil {
		return nil, fmt.Errorf("decode games: %w", err)
	}
	return games, nil
}

// requestData wraps HyperHQ's documented requestData(method, params) RPC. It
// emits a requestData with a fresh requestId, then waits for the matching
// dataResponse on the dataResponse channel. The default timeout comes from
// requestTimeout and the bridge context.
func (b *bridge) requestData(method string, params map[string]any) (json.RawMessage, error) {
	ctx, cancel := context.WithTimeout(b.ctx, requestTimeout)
	defer cancel()
	return b.requestDataCtx(ctx, method, params)
}

func (b *bridge) requestDataCtx(
	ctx context.Context, method string, params map[string]any,
) (json.RawMessage, error) {
	if b.socket == nil {
		return nil, errors.New("socket not connected")
	}

	b.sessionMu.RLock()
	token := b.sessionToken
	b.sessionMu.RUnlock()
	if token == "" {
		return nil, errors.New("no session token (not authenticated)")
	}

	requestID := newRequestID()
	respCh := make(chan hqDataResponse, 1)

	b.pendingMu.Lock()
	b.pendingData[requestID] = respCh
	b.pendingMu.Unlock()

	cleanup := func() {
		b.pendingMu.Lock()
		delete(b.pendingData, requestID)
		b.pendingMu.Unlock()
	}

	envelope := hqRequestData{
		Method:       method,
		Params:       params,
		RequestID:    requestID,
		SessionToken: token,
	}
	if emitErr := b.socket.Emit("requestData", envelope); emitErr != nil {
		cleanup()
		return nil, fmt.Errorf("emit requestData: %w", emitErr)
	}

	select {
	case resp := <-respCh:
		if !resp.Success {
			if resp.Error != "" {
				return nil, fmt.Errorf("HyperHQ error: %s", resp.Error)
			}
			return nil, errors.New("HyperHQ reported failure with no error message")
		}
		return resp.Data, nil
	case <-ctx.Done():
		cleanup()
		return nil, ctx.Err()
	case <-b.ctx.Done():
		cleanup()
		return nil, b.ctx.Err()
	}
}

func (b *bridge) writePipeEvent(evt *pipeEvent) {
	data, err := json.Marshal(evt)
	if err != nil {
		log.Printf("marshal pipe event %s: %v", evt.Event, err)
		return
	}

	b.pipeMu.Lock()
	defer b.pipeMu.Unlock()

	if b.pipeWriter == nil {
		return
	}
	if _, err := b.pipeWriter.Write(append(data, '\n')); err != nil {
		log.Printf("write pipe event: %v", err)
		return
	}
	if err := b.pipeWriter.Flush(); err != nil {
		log.Printf("flush pipe event: %v", err)
	}
}

// decodeFirst takes the variadic args from a Socket.IO listener, picks the
// first one, and re-marshals it into target via JSON. Socket.IO surfaces JSON
// payloads as map[string]any / []any soup, so the round-trip is the simplest
// way to land the data into a typed struct.
func decodeFirst(args []any, target any) error {
	if len(args) == 0 {
		return errors.New("no args")
	}
	raw, err := json.Marshal(args[0])
	if err != nil {
		return fmt.Errorf("marshal intermediate: %w", err)
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return fmt.Errorf("unmarshal target: %w", err)
	}
	return nil
}

// unmarshalIfPresent unmarshals raw into target, returning nil if raw is empty
// (HyperHQ may send dataResponse.success=true with no data field for void
// methods).
func unmarshalIfPresent(raw json.RawMessage, target any) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return fmt.Errorf("unmarshal: %w", err)
	}
	return nil
}

// newRequestID generates a short hex id for matching requestData/dataResponse
// pairs. Using crypto/rand keeps ids unique across reconnects so a stray late
// response can't be matched to a future request.
func newRequestID() string {
	var buf [12]byte
	if _, err := rand.Read(buf[:]); err != nil {
		// Extremely unlikely; fall back to a time-based id.
		return fmt.Sprintf("req-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf[:])
}
