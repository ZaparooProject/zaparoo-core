//go:build windows

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

package windows

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStopGame_UnsupportedWithoutHandshake(t *testing.T) {
	t.Parallel()

	// A plugin predating the Hello handshake cannot stop games; Core must say
	// so immediately so it can fall back to killing the process tree.
	server := NewLaunchBoxPipeServer()
	t.Cleanup(server.Stop)

	err := server.StopGame(context.Background(), "game-1")
	require.ErrorIs(t, err, errLaunchBoxStopUnsupported)
}

func TestHandleEvent_HelloRecordsCapabilities(t *testing.T) {
	t.Parallel()

	server := NewLaunchBoxPipeServer()
	t.Cleanup(server.Stop)

	assert.False(t, server.SupportsStop())

	hello, err := json.Marshal(launchBoxHelloEvent{
		Event:           "Hello",
		PluginVersion:   "1.3.0",
		ProtocolVersion: launchBoxStopProtocolVersion,
	})
	require.NoError(t, err)
	server.handleEvent(string(hello))

	assert.True(t, server.SupportsStop())

	// A reconnecting older plugin must not inherit the newer one's abilities.
	server.resetHandshake()
	assert.False(t, server.SupportsStop())
}

func TestHandleEvent_StopResultCompletesPendingRequest(t *testing.T) {
	t.Parallel()

	server := NewLaunchBoxPipeServer()
	t.Cleanup(server.Stop)

	respChan := make(chan launchBoxStopResultEvent, 1)
	server.pendingStopReqMu.Lock()
	server.pendingStopReq = pendingStopRequest{gameID: "game-1", response: respChan}
	server.pendingStopReqMu.Unlock()

	result, err := json.Marshal(launchBoxStopResultEvent{
		Event:  "MediaStopResult",
		ID:     "game-1",
		Status: "completed",
	})
	require.NoError(t, err)
	server.handleEvent(string(result))

	select {
	case got := <-respChan:
		assert.Equal(t, "completed", got.Status)
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for stop result")
	}
}

func TestHandleEvent_StopResultIgnoresOtherGame(t *testing.T) {
	t.Parallel()

	server := NewLaunchBoxPipeServer()
	t.Cleanup(server.Stop)

	respChan := make(chan launchBoxStopResultEvent, 1)
	server.pendingStopReqMu.Lock()
	server.pendingStopReq = pendingStopRequest{gameID: "game-1", response: respChan}
	server.pendingStopReqMu.Unlock()

	result, err := json.Marshal(launchBoxStopResultEvent{
		Event:  "MediaStopResult",
		ID:     "a-different-game",
		Status: "completed",
	})
	require.NoError(t, err)
	server.handleEvent(string(result))

	select {
	case got := <-respChan:
		t.Fatalf("stop result for %q should not satisfy the pending request", got.ID)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestHandleEvent_ErrorForStopUnblocksPendingRequest(t *testing.T) {
	t.Parallel()

	// The plugin's Error event used to be dropped as an unknown type, leaving
	// a failed stop to wait out the full timeout.
	server := NewLaunchBoxPipeServer()
	t.Cleanup(server.Stop)

	respChan := make(chan launchBoxStopResultEvent, 1)
	server.pendingStopReqMu.Lock()
	server.pendingStopReq = pendingStopRequest{gameID: "game-1", response: respChan}
	server.pendingStopReqMu.Unlock()

	var reportedCommand, reportedError string
	server.SetCommandErrorHandler(func(command, message string) {
		reportedCommand, reportedError = command, message
	})

	errEvent, err := json.Marshal(launchBoxErrorEvent{
		Event:   "Error",
		Command: "Stop",
		Error:   "no game is running",
	})
	require.NoError(t, err)
	server.handleEvent(string(errEvent))

	select {
	case got := <-respChan:
		assert.Equal(t, "failed", got.Status)
		assert.Equal(t, "no game is running", got.Error)
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for failure to propagate")
	}

	assert.Equal(t, "Stop", reportedCommand)
	assert.Equal(t, "no game is running", reportedError)
}

func TestLaunchBoxActiveID_StaleExitDoesNotClearNewerGame(t *testing.T) {
	t.Parallel()

	p := &Platform{}
	p.setLaunchBoxActiveGame("game-1", "")

	// A late exit for a replaced game must not clear the newer launch.
	p.setLaunchBoxActiveGame("game-2", "")
	assert.False(t, p.clearLaunchBoxActiveID("game-1"))
	assert.Equal(t, "game-2", p.launchBoxActiveGameID())

	assert.True(t, p.clearLaunchBoxActiveID("game-2"))
	assert.Empty(t, p.launchBoxActiveGameID())
}

func TestStopLaunchBoxGame_WithoutActiveGame(t *testing.T) {
	t.Parallel()

	p := &Platform{}
	require.Error(t, p.stopLaunchBoxGame(nil))
}

func TestHandleEvent_DuplicateStopResultDoesNotWedgeReader(t *testing.T) {
	t.Parallel()

	// The reader goroutine delivers stop results. A plugin that reports an
	// outcome twice must not block it: the response channel holds one result,
	// and a blocking send would hold pendingStopReqMu forever, stopping every
	// later event from being processed.
	server := NewLaunchBoxPipeServer()
	t.Cleanup(server.Stop)

	respChan := make(chan launchBoxStopResultEvent, 1)
	server.pendingStopReqMu.Lock()
	server.pendingStopReq = pendingStopRequest{gameID: "game-1", response: respChan}
	server.pendingStopReqMu.Unlock()

	result, err := json.Marshal(launchBoxStopResultEvent{
		Event:  "MediaStopResult",
		ID:     "game-1",
		Status: "completed",
	})
	require.NoError(t, err)

	done := make(chan struct{})
	go func() {
		defer close(done)
		server.handleEvent(string(result)) // fills the buffer
		server.handleEvent(string(result)) // must be dropped, not block
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("reader blocked delivering a duplicate stop result")
	}

	assert.Equal(t, "completed", (<-respChan).Status)
}

func TestStopGame_RejectsConcurrentRequests(t *testing.T) {
	t.Parallel()

	server := NewLaunchBoxPipeServer()
	t.Cleanup(server.Stop)
	server.setHandshake("1.3.0", launchBoxStopProtocolVersion)

	server.pendingStopReqMu.Lock()
	server.pendingStopReq = pendingStopRequest{
		gameID:   "game-1",
		response: make(chan launchBoxStopResultEvent, 1),
	}
	server.pendingStopReqMu.Unlock()

	err := server.StopGame(context.Background(), "game-2")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already in flight")
}

func TestStopGame_FailsWhenPluginNotConnected(t *testing.T) {
	t.Parallel()

	// The handshake says stop is supported, but the pipe has since dropped.
	server := NewLaunchBoxPipeServer()
	t.Cleanup(server.Stop)
	server.setHandshake("1.3.0", launchBoxStopProtocolVersion)

	err := server.StopGame(context.Background(), "game-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not connected")
}

func TestHandleEvent_StopResultAfterRequestClearedIsDropped(t *testing.T) {
	t.Parallel()

	// A result arriving after StopGame gave up must be discarded quietly
	// rather than blocking or panicking on a closed request.
	server := NewLaunchBoxPipeServer()
	t.Cleanup(server.Stop)

	result, err := json.Marshal(launchBoxStopResultEvent{
		Event:  "MediaStopResult",
		ID:     "game-1",
		Status: "completed",
	})
	require.NoError(t, err)

	assert.NotPanics(t, func() { server.handleEvent(string(result)) })
}

func TestHandleEvent_MediaProcessReportsResolvedPID(t *testing.T) {
	t.Parallel()

	server := NewLaunchBoxPipeServer()
	t.Cleanup(server.Stop)

	var gotID string
	var gotPID int
	server.SetGameProcessHandler(func(id string, pid int) {
		gotID, gotPID = id, pid
	})

	event, err := json.Marshal(launchBoxProcessEvent{
		Event: "MediaProcess",
		ID:    "game-1",
		Pid:   4321,
	})
	require.NoError(t, err)
	server.handleEvent(string(event))

	assert.Equal(t, "game-1", gotID)
	assert.Equal(t, 4321, gotPID)
}

func TestTrackLaunchBoxProcess_IgnoresReplacedGame(t *testing.T) {
	t.Parallel()

	// A PID resolved for a game that has since been replaced must not be
	// adopted, or the next stop would force-kill the wrong process tree.
	p := &Platform{}
	p.setLaunchBoxActiveGame("game-2", "")

	p.trackLaunchBoxProcess("game-1", 4321)

	p.processMu.RLock()
	tracked := p.trackedProcess
	p.processMu.RUnlock()
	assert.Nil(t, tracked)
}
