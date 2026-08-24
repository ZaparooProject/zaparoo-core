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

package remote

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/permissions"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	testinghelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func newHTTPTestManager(t *testing.T, serverURL string) *manager {
	t.Helper()
	cfg := &config.Instance{}
	require.NoError(t, cfg.SetRemoteControlBaseURL(serverURL))
	cfg.SetRemoteControl(true)
	config.SetAuthCfgForTesting(map[string]config.CredentialEntry{
		config.RemoteAuthLookupURL(serverURL): {Bearer: "zpd1_test"},
	})
	t.Cleanup(config.ClearAuthCfgForTesting)
	platform := mocks.NewMockPlatform()
	platform.On("ID").Return("linux")
	return &manager{
		deps: Deps{Config: cfg, Platform: platform}, httpClient: http.DefaultClient,
		executionSlot: make(chan struct{}, 1),
	}
}

// TestRequestEnv_NeverResolvesToAdmin is the regression guard for the
// EffectiveRole admin trap: requestEnv always sets IsLocal false and a
// non-empty ClientRole, so a resulting Grant never falls into the
// unpaired-remote-is-admin backward-compatibility rule.
func TestRequestEnv_NeverResolvesToAdmin(t *testing.T) {
	t.Parallel()

	m := &manager{}
	for _, role := range []permissions.Role{permissions.RoleRemote, "", "unexpected"} {
		env := m.requestEnv(context.Background(), role, nil)
		assert.False(t, env.IsLocal)
		assert.NotEmpty(t, env.ClientRole)
		grant := permissions.Grant{Role: permissions.Role(env.ClientRole), IsLocal: env.IsLocal}
		assert.NotEqual(t, permissions.RoleAdmin, grant.EffectiveRole())
		assert.Empty(t, grant.Capabilities())
	}
}

func TestHeartbeatUsesConfiguredEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/v1/device/heartbeat", r.URL.Path)
		var body map[string]any
		if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&body)) {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		capabilities, ok := body["capabilities"].(map[string]any)
		if !assert.True(t, ok) {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		assert.NotContains(t, capabilities, "backup")
		assert.Contains(t, capabilities, "remote_operations")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	m := newHTTPTestManager(t, server.URL)

	require.NoError(t, m.sendCapabilityHeartbeat(context.Background()))
}

func TestWaitUsesContractRequest(t *testing.T) {
	var sawRequest bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawRequest = true
		assert.Equal(t, "/v1/device/remote-sessions/wait", r.URL.Path)
		assert.Equal(t, "25", r.URL.Query().Get("timeout"))
		assert.Equal(t, "Bearer zpd1_test", r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	m := newHTTPTestManager(t, server.URL)

	_, hasWork, err := m.waitOnce(context.Background(), "zpd1_test")
	require.NoError(t, err)
	assert.False(t, hasWork)
	assert.True(t, sawRequest)
}

func TestRetryContract(t *testing.T) {
	assert.GreaterOrEqual(t, longRetry, 5*time.Minute)
	assert.Equal(t, 3*time.Second, parseRetryAfter("3"))
	assert.Zero(t, parseRetryAfter("invalid"))
}

func TestResultsReplayDue(t *testing.T) {
	now := time.Now()
	assert.True(t, resultsReplayDue(false, now, now))
	assert.False(t, resultsReplayDue(true, now, now.Add(resultReplayInterval-time.Nanosecond)))
	assert.True(t, resultsReplayDue(true, now, now.Add(resultReplayInterval)))
}

// TestDispatchOperationHandlesBusyInlineWithoutGoroutine pins that a busy
// rejection (the execution slot already taken) is handled synchronously by
// dispatchOperation, not handed to its own goroutine. If it ran in a
// goroutine instead, dispatchOperation would return almost immediately
// regardless of how long its own HTTP result post takes; forcing that post
// to take postLatency and asserting the call took at least that long is
// only explained by the busy path running inline.
func TestDispatchOperationHandlesBusyInlineWithoutGoroutine(t *testing.T) {
	const postLatency = 100 * time.Millisecond
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(postLatency)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	m := newHTTPTestManager(t, server.URL)
	userDB := testinghelpers.NewMockUserDBI()
	m.deps.DB = &database.Database{UserDB: userDB}
	m.executionSlot <- struct{}{} // slot already taken: this dispatch must be treated as busy

	future := time.Now().UTC().Add(time.Minute)
	emptyDigest := sha256.Sum256(json.RawMessage(`{}`))
	stored := &database.RemoteCommand{
		CommandID: "cmd_busy", OperationID: "op_busy", OperationType: "echo",
		ProtocolVersion: 1, ParamsDigest: hex.EncodeToString(emptyDigest[:]),
		Origin:     json.RawMessage(`{"kind":"first_party"}`),
		DeadlineAt: future, State: "accepted", ExecutionExpiresAt: &future,
	}
	userDB.On("ClaimRemoteCommand", mock.Anything).Return(stored, true, nil).Once()
	userDB.On("StoreRemoteCommandResult", "cmd_busy", "accepted", "busy", mock.Anything, "").
		Return(true, nil).Once()
	userDB.On("MarkRemoteCommandResultReported", "cmd_busy").Return(nil).Once()

	var workers sync.WaitGroup
	envelope := &operationEnvelope{
		CommandID: "cmd_busy", OperationID: "op_busy", OperationType: "echo", ProtocolVersion: 1,
		DeadlineAt: future, Origin: operationOrigin{Kind: "first_party"},
	}

	start := time.Now()
	m.dispatchOperation(context.Background(), &workers, envelope)
	elapsed := time.Since(start)

	assert.GreaterOrEqual(t, elapsed, postLatency,
		"a busy dispatch must complete its own round trip inline, not return before it via a goroutine")
	workers.Wait()
	userDB.AssertExpectations(t)
}

// TestDispatchOperationRunsRealWorkInGoroutine pins the other half of the
// same fix: an operation that DOES acquire the execution slot still runs
// concurrently in its own goroutine, so dispatchOperation returns promptly
// even though the operation's own handling takes postLatency.
func TestDispatchOperationRunsRealWorkInGoroutine(t *testing.T) {
	const postLatency = 100 * time.Millisecond
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(postLatency)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	m := newHTTPTestManager(t, server.URL)
	userDB := testinghelpers.NewMockUserDBI()
	m.deps.DB = &database.Database{UserDB: userDB}

	future := time.Now().UTC().Add(time.Minute)
	params := json.RawMessage(`{"message":"hi"}`)
	digest := sha256.Sum256(params)
	stored := &database.RemoteCommand{
		CommandID: "cmd_real", OperationID: "op_real", OperationType: "echo",
		ProtocolVersion: 1, ParamsDigest: hex.EncodeToString(digest[:]),
		Origin:     json.RawMessage(`{"kind":"first_party"}`),
		DeadlineAt: future, State: "accepted", ExecutionExpiresAt: &future,
	}
	userDB.On("ClaimRemoteCommand", mock.Anything).Return(stored, true, nil).Once()
	userDB.On("TransitionRemoteCommand", "cmd_real", "accepted", "executing", mock.Anything).
		Return(true, nil).Once()
	userDB.On("StoreRemoteCommandResult", "cmd_real", "executing", "succeeded", mock.Anything, "").
		Return(true, nil).Once()
	userDB.On("MarkRemoteCommandResultReported", "cmd_real").Return(nil).Once()

	var workers sync.WaitGroup
	envelope := &operationEnvelope{
		CommandID: "cmd_real", OperationID: "op_real", OperationType: "echo", ProtocolVersion: 1,
		Params: json.RawMessage(`{"message":"hi"}`), DeadlineAt: future, Origin: operationOrigin{Kind: "first_party"},
	}

	start := time.Now()
	m.dispatchOperation(context.Background(), &workers, envelope)
	elapsed := time.Since(start)

	assert.Less(t, elapsed, postLatency, "acquiring the slot must hand off to a goroutine, not block dispatch")
	workers.Wait()
	userDB.AssertExpectations(t)
}

func TestWaitClassifiesSlot404(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"code":"remote_slot_required","message":"not entitled"}}`))
	}))
	defer server.Close()
	m := newHTTPTestManager(t, server.URL)

	_, _, err := m.waitOnce(context.Background(), "zpd1_test")
	var httpErr *httpError
	require.ErrorAs(t, err, &httpErr)
	assert.Equal(t, http.StatusNotFound, httpErr.status)
	assert.Equal(t, "remote_slot_required", httpErr.code)
}

func TestWaitCancelsWhenConsentDisabled(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	}))
	defer server.Close()
	m := newHTTPTestManager(t, server.URL)

	done := make(chan error, 1)
	go func() {
		_, _, err := m.waitOnce(context.Background(), "zpd1_test")
		done <- err
	}()
	<-started
	m.deps.Config.SetRemoteControl(false)
	select {
	case err := <-done:
		require.Error(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("wait did not cancel after consent was disabled")
	}
}
