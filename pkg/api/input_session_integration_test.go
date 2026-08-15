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
	"strings"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type trackedAPIInputSession struct {
	blockPress     <-chan struct{}
	started        chan []string
	released       chan struct{}
	unblock        chan struct{}
	keyboard       map[string]struct{}
	gamepad        map[string]struct{}
	calls          [][]string
	mu             syncutil.Mutex
	waitForRelease bool
}

func newTrackedAPIInputSession() *trackedAPIInputSession {
	return &trackedAPIInputSession{
		started:  make(chan []string, 4),
		released: make(chan struct{}, 2),
		unblock:  make(chan struct{}, 1),
		keyboard: make(map[string]struct{}),
		gamepad:  make(map[string]struct{}),
	}
}

func (s *trackedAPIInputSession) KeyboardPressSequence(
	ctx context.Context,
	args []string,
	_ time.Duration,
) error {
	copiedArgs := append([]string(nil), args...)
	s.started <- copiedArgs
	if s.waitForRelease {
		<-s.unblock
	}
	if len(args) > 0 && strings.HasPrefix(args[0], "{press:") && s.blockPress != nil {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-s.blockPress:
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, copiedArgs)
	applyTrackedInputTokens(s.keyboard, args)
	return nil
}

func (s *trackedAPIInputSession) GamepadPressSequence(
	_ context.Context,
	args []string,
	_ time.Duration,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, append([]string(nil), args...))
	applyTrackedInputTokens(s.gamepad, args)
	return nil
}

func (s *trackedAPIInputSession) ReleaseAll() error {
	s.mu.Lock()
	clear(s.keyboard)
	clear(s.gamepad)
	s.mu.Unlock()
	select {
	case s.unblock <- struct{}{}:
	default:
	}
	select {
	case s.released <- struct{}{}:
	default:
	}
	return nil
}

func applyTrackedInputTokens(held map[string]struct{}, args []string) {
	for _, token := range args {
		switch {
		case strings.HasPrefix(token, "{press:") && strings.HasSuffix(token, "}"):
			held[strings.TrimSuffix(strings.TrimPrefix(token, "{press:"), "}")] = struct{}{}
		case strings.HasPrefix(token, "{release:") && strings.HasSuffix(token, "}"):
			delete(held, strings.TrimSuffix(strings.TrimPrefix(token, "{release:"), "}"))
		}
	}
}

func (s *trackedAPIInputSession) keyboardSnapshot() map[string]struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make(map[string]struct{}, len(s.keyboard))
	for key := range s.keyboard {
		result[key] = struct{}{}
	}
	return result
}

func (s *trackedAPIInputSession) gamepadSnapshot() map[string]struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make(map[string]struct{}, len(s.gamepad))
	for button := range s.gamepad {
		result[button] = struct{}{}
	}
	return result
}

func (s *trackedAPIInputSession) callSnapshot() [][]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([][]string, len(s.calls))
	for i := range s.calls {
		result[i] = append([]string(nil), s.calls[i]...)
	}
	return result
}

type inputSessionTestPlatform struct {
	*mocks.MockPlatform
	newSession func() *trackedAPIInputSession
	created    chan *trackedAPIInputSession
}

func newInputSessionTestPlatform(
	factory func() *trackedAPIInputSession,
) *inputSessionTestPlatform {
	return &inputSessionTestPlatform{
		MockPlatform: mocks.NewMockPlatform(),
		newSession:   factory,
		created:      make(chan *trackedAPIInputSession, 4),
	}
}

func (p *inputSessionTestPlatform) NewInputSession() platforms.InputSession {
	session := p.newSession()
	p.created <- session
	return session
}

func sendInputRPC(
	t *testing.T,
	conn *websocket.Conn,
	id int,
	method, paramName, macro string,
) {
	t.Helper()
	payload := fmt.Sprintf(
		`{"jsonrpc":"2.0","id":%d,"method":%q,"params":{%q:%q}}`,
		id,
		method,
		paramName,
		macro,
	)
	require.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte(payload)))
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(2*time.Second)))
	_, message, err := conn.ReadMessage()
	require.NoError(t, err)
	var response models.ResponseObject
	require.NoError(t, json.Unmarshal(message, &response))
	require.Nil(t, response.Error)
	assert.Equal(t, models.NewNumberID(int64(id)), response.ID)
}

func TestWebSocketInputSessionPersistsIsolatesAndReleasesOnDisconnect(t *testing.T) {
	platform := newInputSessionTestPlatform(newTrackedAPIInputSession)
	wsURL, cleanup := startPriorityWSServerWithPlatform(t, NewMethodMap(), platform)
	defer cleanup()

	firstConn := dialWS(t, wsURL)
	sendInputRPC(t, firstConn, 1, models.MethodInputKeyboard, "keys", "{press:up}")
	firstSession := <-platform.created
	assert.Equal(t, map[string]struct{}{"up": {}}, firstSession.keyboardSnapshot())

	sendInputRPC(t, firstConn, 2, models.MethodInputKeyboard, "keys", "{press:left}")
	assert.Equal(t, map[string]struct{}{"up": {}, "left": {}}, firstSession.keyboardSnapshot())
	sendInputRPC(t, firstConn, 5, models.MethodInputGamepad, "buttons", "{press:start}")
	assert.Equal(t, map[string]struct{}{"start": {}}, firstSession.gamepadSnapshot())

	secondConn := dialWS(t, wsURL)
	defer func() { _ = secondConn.Close() }()
	sendInputRPC(t, secondConn, 3, models.MethodInputKeyboard, "keys", "{release:up}")
	secondSession := <-platform.created
	assert.Empty(t, secondSession.keyboardSnapshot())
	assert.Equal(t, map[string]struct{}{"up": {}, "left": {}}, firstSession.keyboardSnapshot(),
		"second WebSocket must not release first WebSocket input")

	sendInputRPC(t, firstConn, 4, models.MethodInputKeyboard, "keys", "{release:up}")
	assert.Equal(t, map[string]struct{}{"left": {}}, firstSession.keyboardSnapshot())

	require.NoError(t, firstConn.Close())
	select {
	case <-firstSession.released:
	case <-time.After(2 * time.Second):
		t.Fatal("input session was not released after WebSocket disconnect")
	}
	assert.Empty(t, firstSession.keyboardSnapshot())
	assert.Empty(t, firstSession.gamepadSnapshot())
}

func TestWebSocketDisconnectCancelsActiveInputBeforeRelease(t *testing.T) {
	blockedPress := make(chan struct{})
	platform := newInputSessionTestPlatform(func() *trackedAPIInputSession {
		session := newTrackedAPIInputSession()
		session.blockPress = blockedPress
		return session
	})
	wsURL, cleanup := startPriorityWSServerWithPlatform(t, NewMethodMap(), platform)
	defer cleanup()

	conn := dialWS(t, wsURL)
	press := []byte(`{"jsonrpc":"2.0","id":1,"method":"input.keyboard","params":{"keys":"{press:up}"}}`)
	require.NoError(t, conn.WriteMessage(websocket.TextMessage, press))
	session := <-platform.created
	assert.Equal(t, []string{"{press:up}"}, <-session.started)

	require.NoError(t, conn.Close())
	select {
	case <-session.released:
	case <-time.After(2 * time.Second):
		t.Fatal("disconnect did not cancel active input and release its session")
	}
	assert.Empty(t, session.keyboardSnapshot())
}

func TestWebSocketDisconnectReleasesBeforeWaitingForInputWorker(t *testing.T) {
	platform := newInputSessionTestPlatform(func() *trackedAPIInputSession {
		session := newTrackedAPIInputSession()
		session.waitForRelease = true
		return session
	})
	wsURL, cleanup := startPriorityWSServerWithPlatform(t, NewMethodMap(), platform)
	defer cleanup()

	conn := dialWS(t, wsURL)
	press := []byte(`{"jsonrpc":"2.0","id":1,"method":"input.keyboard","params":{"keys":"{press:up}"}}`)
	require.NoError(t, conn.WriteMessage(websocket.TextMessage, press))
	session := <-platform.created
	assert.Equal(t, []string{"{press:up}"}, <-session.started)

	require.NoError(t, conn.Close())
	select {
	case <-session.released:
	case <-time.After(2 * time.Second):
		t.Fatal("disconnect waited for input worker before releasing session")
	}
}

func TestWebSocketInputRequestsExecuteInArrivalOrder(t *testing.T) {
	unblockPress := make(chan struct{})
	platform := newInputSessionTestPlatform(func() *trackedAPIInputSession {
		session := newTrackedAPIInputSession()
		session.blockPress = unblockPress
		return session
	})
	wsURL, cleanup := startPriorityWSServerWithPlatform(t, NewMethodMap(), platform)
	defer cleanup()

	conn := dialWS(t, wsURL)
	defer func() { _ = conn.Close() }()
	press := []byte(`{"jsonrpc":"2.0","id":1,"method":"input.keyboard","params":{"keys":"{press:up}"}}`)
	release := []byte(`{"jsonrpc":"2.0","id":2,"method":"input.keyboard","params":{"keys":"{release:up}"}}`)
	require.NoError(t, conn.WriteMessage(websocket.TextMessage, press))
	session := <-platform.created
	assert.Equal(t, []string{"{press:up}"}, <-session.started)
	require.NoError(t, conn.WriteMessage(websocket.TextMessage, release))

	select {
	case started := <-session.started:
		t.Fatalf("second input request started before first completed: %v", started)
	case <-time.After(100 * time.Millisecond):
	}
	close(unblockPress)

	for range 2 {
		require.NoError(t, conn.SetReadDeadline(time.Now().Add(2*time.Second)))
		_, _, err := conn.ReadMessage()
		require.NoError(t, err)
	}
	assert.Equal(t, []string{"{release:up}"}, <-session.started)
	assert.Equal(t, [][]string{{"{press:up}"}, {"{release:up}"}}, session.callSnapshot())
	assert.Empty(t, session.keyboardSnapshot())
}
