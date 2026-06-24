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
	"net/http"
	"net/http/httptest"
	"net/url"
	"runtime"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/gorilla/websocket"
	"github.com/olahol/melody"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func indexRPCID(ids []models.RPCID, target models.RPCID) int {
	for i, id := range ids {
		if id.Equal(target) {
			return i
		}
	}
	return -1
}

func startPriorityWSServer(t *testing.T, methodMap *MethodMap) (wsURL string, cleanup func()) {
	t.Helper()

	cfg, err := config.NewConfig(t.TempDir(), config.BaseDefaults)
	require.NoError(t, err)
	st, _ := state.NewState(nil, "test-boot")

	m := newWebSocketSession()
	m.HandleDisconnect(func(s *melody.Session) {
		closeWSDispatcher(s)
	})
	m.HandleMessage(handleWSMessage(
		methodMap, nil, cfg, st, nil, nil,
		nil, nil, nil, nil, nil,
		nil, nil, nil, nil,
	))

	mux := http.NewServeMux()
	mux.HandleFunc("/api", func(w http.ResponseWriter, r *http.Request) {
		_ = m.HandleRequest(w, r)
	})

	srv := httptest.NewServer(mux)
	u, err := url.Parse(srv.URL)
	require.NoError(t, err)

	cleanup = func() {
		st.StopService()
		_ = m.Close()
		srv.Close()
	}
	return "ws://" + u.Host + "/api", cleanup
}

func TestWebSocketPriorityDispatcherHighPriorityBypassesSlowImage(t *testing.T) {
	imageStarted := make(chan struct{}, wsLowConcurrency+1)
	releaseImages := make(chan struct{})

	var methodMap MethodMap
	require.NoError(t, methodMap.AddMethod(models.MethodMediaImage, func(env requests.RequestEnv) (any, error) {
		imageStarted <- struct{}{}
		select {
		case <-releaseImages:
			return map[string]string{"kind": "image"}, nil
		case <-env.Context.Done():
			return nil, env.Context.Err()
		}
	}))
	require.NoError(t, methodMap.AddMethod(models.MethodMediaTagsUpdate, func(requests.RequestEnv) (any, error) {
		return map[string]string{"kind": "favorite"}, nil
	}))

	wsURL, cleanup := startPriorityWSServer(t, &methodMap)
	defer cleanup()

	conn := dialWS(t, wsURL)
	defer func() { _ = conn.Close() }()

	for id := 1; id <= 3; id++ {
		require.NoError(t, conn.WriteMessage(websocket.TextMessage,
			[]byte(fmt.Sprintf(`{"jsonrpc":"2.0","method":"media.image","id":%d}`, id))))
	}
	for range wsLowConcurrency {
		select {
		case <-imageStarted:
		case <-time.After(2 * time.Second):
			t.Fatal("media.image did not start")
		}
	}
	select {
	case <-imageStarted:
		t.Fatal("third media.image ran before a low-priority worker was free")
	default:
	}

	require.NoError(t, conn.WriteMessage(websocket.TextMessage,
		[]byte(`{"jsonrpc":"2.0","method":"media.tags.update","params":{"mediaId":1,"add":["user:favorite"]},"id":4}`)))
	waitForMediaDBWriterPending(t)
	close(releaseImages)

	require.NoError(t, conn.SetReadDeadline(time.Now().Add(2*time.Second)))
	seen := make([]models.RPCID, 0, 4)
	for range 4 {
		_, msg, err := conn.ReadMessage()
		require.NoError(t, err)
		var resp models.ResponseObject
		require.NoError(t, json.Unmarshal(msg, &resp))
		seen = append(seen, resp.ID)
	}

	favoriteIndex := indexRPCID(seen, models.NewNumberID(4))
	thirdImageIndex := indexRPCID(seen, models.NewNumberID(3))
	require.NotEqual(t, -1, favoriteIndex)
	require.NotEqual(t, -1, thirdImageIndex)
	assert.Less(t, favoriteIndex, thirdImageIndex, "mutation should bypass queued image work")
}

func waitForMediaDBWriterPending(t *testing.T) {
	t.Helper()

	deadline := time.After(2 * time.Second)
	for {
		if !wsMediaDBMu.TryRLock() {
			return
		}
		wsMediaDBMu.RUnlock()

		select {
		case <-deadline:
			t.Fatal("media.tags.update did not wait for the media DB write lock")
		default:
			runtime.Gosched()
		}
	}
}

func TestWebSocketPriorityDispatcherPreservesHighPriorityOrder(t *testing.T) {
	t.Parallel()

	firstDone := make(chan struct{})
	var methodMap MethodMap
	require.NoError(t, methodMap.AddMethod(models.MethodRun, func(env requests.RequestEnv) (any, error) {
		select {
		case <-time.After(150 * time.Millisecond):
			close(firstDone)
			return map[string]string{"kind": "run"}, nil
		case <-env.Context.Done():
			return nil, env.Context.Err()
		}
	}))
	require.NoError(t, methodMap.AddMethod(models.MethodStop, func(requests.RequestEnv) (any, error) {
		select {
		case <-firstDone:
			return map[string]string{"kind": "stop"}, nil
		default:
			return map[string]string{"kind": "stop-before-run"}, nil
		}
	}))

	wsURL, cleanup := startPriorityWSServer(t, &methodMap)
	defer cleanup()

	conn := dialWS(t, wsURL)
	defer func() { _ = conn.Close() }()

	require.NoError(t, conn.WriteMessage(websocket.TextMessage,
		[]byte(`{"jsonrpc":"2.0","method":"run","id":1}`)))
	require.NoError(t, conn.WriteMessage(websocket.TextMessage,
		[]byte(`{"jsonrpc":"2.0","method":"stop","id":2}`)))

	require.NoError(t, conn.SetReadDeadline(time.Now().Add(2*time.Second)))
	_, msg, err := conn.ReadMessage()
	require.NoError(t, err)

	var resp models.ResponseObject
	require.NoError(t, json.Unmarshal(msg, &resp))
	assert.Equal(t, models.NewNumberID(1), resp.ID)

	_, msg, err = conn.ReadMessage()
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(msg, &resp))
	assert.Equal(t, models.NewNumberID(2), resp.ID)
}

func TestWebSocketPriorityDispatcherMediaTransactionBlocksMediaReads(t *testing.T) {
	t.Parallel()

	txStarted := make(chan struct{})
	releaseTx := make(chan struct{})
	metaStarted := make(chan struct{}, 1)
	var methodMap MethodMap
	require.NoError(t, methodMap.AddMethod(models.MethodMediaTagsUpdate, func(env requests.RequestEnv) (any, error) {
		close(txStarted)
		select {
		case <-releaseTx:
			return map[string]string{"kind": "favorite"}, nil
		case <-env.Context.Done():
			return nil, env.Context.Err()
		}
	}))
	require.NoError(t, methodMap.AddMethod(models.MethodMediaMeta, func(requests.RequestEnv) (any, error) {
		metaStarted <- struct{}{}
		return map[string]string{"kind": "meta"}, nil
	}))

	wsURL, cleanup := startPriorityWSServer(t, &methodMap)
	defer cleanup()

	conn := dialWS(t, wsURL)
	defer func() { _ = conn.Close() }()

	require.NoError(t, conn.WriteMessage(websocket.TextMessage,
		[]byte(`{"jsonrpc":"2.0","method":"media.tags.update","id":1}`)))
	select {
	case <-txStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("media.tags.update did not start")
	}
	require.NoError(t, conn.WriteMessage(websocket.TextMessage,
		[]byte(`{"jsonrpc":"2.0","method":"media.meta","id":2}`)))

	select {
	case <-metaStarted:
		t.Fatal("media.meta ran while media.tags.update transaction lane was active")
	case <-time.After(150 * time.Millisecond):
	}

	close(releaseTx)
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(2*time.Second)))
	_, _, err := conn.ReadMessage()
	require.NoError(t, err)
	_, _, err = conn.ReadMessage()
	require.NoError(t, err)
}

func TestWebSocketPriorityDispatcherNotificationsDoNotReply(t *testing.T) {
	t.Parallel()

	var methodMap MethodMap
	require.NoError(t, methodMap.AddMethod("test.notify", func(requests.RequestEnv) (any, error) {
		return map[string]string{"ok": "true"}, nil
	}))

	wsURL, cleanup := startPriorityWSServer(t, &methodMap)
	defer cleanup()

	conn := dialWS(t, wsURL)
	defer func() { _ = conn.Close() }()

	require.NoError(t, conn.WriteMessage(websocket.TextMessage,
		[]byte(`{"jsonrpc":"2.0","method":"test.notify"}`)))

	require.NoError(t, conn.SetReadDeadline(time.Now().Add(200*time.Millisecond)))
	_, _, err := conn.ReadMessage()
	require.Error(t, err, "JSON-RPC notifications must not receive responses")
}

func TestWebSocketRunJobStartsTimeoutAtExecution(t *testing.T) {
	t.Parallel()

	parentCtx, parentCancel := context.WithCancel(t.Context())
	defer parentCancel()
	d := &wsSessionDispatcher{
		ctx:       parentCtx,
		responses: make(chan *wsResponseJob, 1),
	}

	deadlineCh := make(chan time.Time, 1)
	var methodMap MethodMap
	require.NoError(t, methodMap.AddMethod("test.timeout", func(env requests.RequestEnv) (any, error) {
		deadline, ok := env.Context.Deadline()
		require.True(t, ok, "runJob should install per-request deadline")
		deadlineCh <- deadline
		return map[string]string{"ok": "true"}, nil
	}))

	enqueuedCtx, enqueuedCancel := context.WithTimeout(parentCtx, time.Millisecond)
	defer enqueuedCancel()
	job := &wsRequestJob{
		methodMap: &methodMap,
		env:       &requests.RequestEnv{Context: enqueuedCtx},
		method:    "test.timeout",
		msg:       []byte(`{"jsonrpc":"2.0","method":"test.timeout","id":1}`),
	}
	time.Sleep(25 * time.Millisecond)
	require.Error(t, enqueuedCtx.Err(), "pre-existing enqueue-time context should be expired")

	beforeRun := time.Now()
	d.runJob(job)
	require.NotNil(t, job.cancel, "runJob should install cancel func")
	defer job.cancel()

	select {
	case deadline := <-deadlineCh:
		assert.True(t, deadline.After(beforeRun), "deadline should not reuse expired enqueue-time context")
		assert.True(t, deadline.Before(beforeRun.Add(config.APIRequestTimeout+time.Second)))
	case <-time.After(time.Second):
		t.Fatal("handler did not run")
	}

	select {
	case resp := <-d.responses:
		require.NotNil(t, resp.cancel)
		assert.True(t, resp.result.ShouldReply)
	case <-time.After(time.Second):
		t.Fatal("response was not queued")
	}
}

func TestCloseWSDispatcherCancelsQueuedRequests(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	d := &wsSessionDispatcher{
		ctx:       ctx,
		cancel:    cancel,
		high:      make(chan *wsRequestJob, wsQueueSize),
		normal:    make(chan *wsRequestJob, wsQueueSize),
		low:       make(chan *wsRequestJob, wsQueueSize),
		responses: make(chan *wsResponseJob, wsResponseQueueSize),
	}

	tracker := &countingRequestTracker{}
	tracker.RequestStarted()
	jobCtx, jobCancel := context.WithCancel(ctx)
	require.NoError(t, d.enqueue(&wsRequestJob{
		env:     &requests.RequestEnv{Context: jobCtx},
		tracker: tracker,
		cancel:  jobCancel,
	}, apiPriorityHigh))

	d.close()
	assert.Equal(t, 0, tracker.inFlight())
	assert.Error(t, jobCtx.Err())
}

type countingRequestTracker struct {
	count int
}

func (t *countingRequestTracker) RequestStarted() { t.count++ }

func (t *countingRequestTracker) RequestEnded() { t.count-- }

func (t *countingRequestTracker) inFlight() int { return t.count }
