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
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/permissions"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
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

func TestIsUnauthorized(t *testing.T) {
	t.Parallel()

	assert.True(t, isUnauthorized(errUnauthorized))
	assert.True(t, isUnauthorized(fmt.Errorf("wrapped: %w", errUnauthorized)))
	assert.True(t, isUnauthorized(&httpError{status: http.StatusUnauthorized}))
	assert.True(t, isUnauthorized(&unauthorizedError{bearer: "zpd1_x"}))
	assert.False(t, isUnauthorized(&httpError{status: http.StatusForbidden}))
	assert.False(t, isUnauthorized(errors.New("network unreachable")))
	assert.False(t, isUnauthorized(nil))
}

// TestWaitUnauthorizedCarriesRejectedBearer pins that a 401 on the wait
// endpoint reports which bearer was rejected, and still unwraps to the
// HTTP error the transition/result handlers classify on.
func TestWaitUnauthorizedCarriesRejectedBearer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"code":"unauthorized","message":"bad token"}}`))
	}))
	defer server.Close()
	m := newHTTPTestManager(t, server.URL)

	_, _, err := m.waitOnce(context.Background(), "zpd1_test")
	require.True(t, isUnauthorized(err))
	assert.Equal(t, "zpd1_test", rejectedBearer(err))
	var httpErr *httpError
	require.ErrorAs(t, err, &httpErr)
	assert.Equal(t, http.StatusUnauthorized, httpErr.status)
	assert.Equal(t, "unauthorized", errorCodeOf(err))

	err = m.doJSON(context.Background(), http.MethodPost, "/v1/device/heartbeat", map[string]any{}, nil)
	require.True(t, isUnauthorized(err))
	assert.Equal(t, "zpd1_test", rejectedBearer(err))
}

// TestMarkUnlinkedIfSharedEndpointIgnoresSupersededBearer pins the re-link
// race: a 401 for a bearer that is no longer the stored credential is a
// late answer about the old token and must not flag the fresh link as
// unlinked; a 401 for the current bearer (or one of unknown provenance)
// still does.
func TestMarkUnlinkedIfSharedEndpointIgnoresSupersededBearer(t *testing.T) {
	cfg := &config.Instance{}
	require.NoError(t, cfg.SetRemoteControlBaseURL("https://online.example.com"))
	require.NoError(t, cfg.SetBackupRemoteBaseURL("https://online.example.com"))
	config.SetAuthCfgForTesting(map[string]config.CredentialEntry{
		config.RemoteAuthLookupURL("https://online.example.com"): {Bearer: "zpd1_new"},
	})
	t.Cleanup(config.ClearAuthCfgForTesting)

	calls := 0
	m := &manager{deps: Deps{Config: cfg}, markUnlinked: func() { calls++ }}

	m.markUnlinkedIfSharedEndpoint("zpd1_old")
	assert.Equal(t, 0, calls, "a superseded bearer's 401 must be ignored")

	m.markUnlinkedIfSharedEndpoint("zpd1_new")
	assert.Equal(t, 1, calls, "the current bearer's 401 marks the account unlinked")

	m.markUnlinkedIfSharedEndpoint("")
	assert.Equal(t, 2, calls, "a 401 of unknown provenance is treated as current")
}

// TestSupersededRejection pins which 401s the poller is allowed to act on.
// A verdict about a bearer that a re-link has already replaced must not be
// shown as credential_rejected, nor hold the new credential behind the
// one-minute rejection back-off.
func TestSupersededRejection(t *testing.T) {
	cfg := &config.Instance{}
	require.NoError(t, cfg.SetRemoteControlBaseURL("https://online.example.com"))
	config.SetAuthCfgForTesting(map[string]config.CredentialEntry{
		config.RemoteAuthLookupURL("https://online.example.com"): {Bearer: "zpd1_new"},
	})
	t.Cleanup(config.ClearAuthCfgForTesting)

	m := &manager{deps: Deps{Config: cfg}}

	unauthorized := func(bearer string) error {
		return &unauthorizedError{httpErr: &httpError{status: 401}, bearer: bearer}
	}

	assert.True(t, m.supersededRejection(unauthorized("zpd1_old")),
		"a 401 for a replaced bearer is a late answer about the old token")
	assert.False(t, m.supersededRejection(unauthorized("zpd1_new")),
		"a 401 for the current bearer is a real rejection")
	assert.False(t, m.supersededRejection(unauthorized("")),
		"a 401 of unknown provenance is treated as current")
	assert.False(t, m.supersededRejection(errors.New("boom")),
		"a non-401 carries no bearer and is never superseded")
}

func TestErrorCodeOf(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "remote_slot_required", errorCodeOf(&httpError{status: 404, code: "remote_slot_required"}))
	assert.Equal(t, "http_503", errorCodeOf(&httpError{status: 503}))
	assert.Equal(t, "unauthorized", errorCodeOf(errUnauthorized))
	assert.Equal(t, "unreachable", errorCodeOf(errors.New("dial tcp: connection refused")))
}

// TestRunRecordsRemoteStatus pins the owner-facing status the poll loop
// records on the shared state: a slot-required 404 is reported as "not this
// account's remote device" with the server's code, distinct from the
// feature-dark 404, and the loop still stops cleanly on cancel while backed
// off on the long retry.
func TestRunRecordsRemoteStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/device/heartbeat":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/device/remote-sessions/wait":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":{"code":"remote_slot_required","message":"not entitled"}}`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()
	m := newHTTPTestManager(t, server.URL)
	st, notifications := state.NewState(m.deps.Platform, "")
	t.Cleanup(func() {
		for len(notifications) > 0 {
			<-notifications
		}
	})
	m.deps.State = st
	userDB := testinghelpers.NewMockUserDBI()
	userDB.On("PruneRemoteCommands", mock.Anything).Return(int64(0), nil).Once()
	userDB.On("ListUnreportedRemoteCommands", resultReplayBatchLimit).
		Return([]database.RemoteCommand{}, nil).Maybe()
	m.deps.DB = &database.Database{UserDB: userDB}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		m.run(ctx)
		close(done)
	}()

	require.Eventually(t, func() bool {
		return st.RemoteStatus().State == state.RemoteStateNotRemoteDevice
	}, 2*time.Second, 10*time.Millisecond, "slot-required 404 was never reported as status")
	status := st.RemoteStatus()
	assert.Equal(t, "remote_slot_required", status.LastErrorCode)
	assert.False(t, status.LastContactAt.IsZero(), "the successful heartbeat counts as contact")

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("run did not stop after context cancellation")
	}
	userDB.AssertExpectations(t)
}

// TestRunRecordsDisabledStatus pins that the not-eligible branch reports
// "disabled" (consent off) rather than leaving the status unknown.
func TestRunRecordsDisabledStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		t.Errorf("disabled remote control must not make requests, got %s", r.URL.Path)
	}))
	defer server.Close()
	m := newHTTPTestManager(t, server.URL)
	m.deps.Config.SetRemoteControl(false)
	st, notifications := state.NewState(m.deps.Platform, "")
	t.Cleanup(func() {
		for len(notifications) > 0 {
			<-notifications
		}
	})
	m.deps.State = st
	userDB := testinghelpers.NewMockUserDBI()
	userDB.On("PruneRemoteCommands", mock.Anything).Return(int64(0), nil).Once()
	m.deps.DB = &database.Database{UserDB: userDB}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		m.run(ctx)
		close(done)
	}()
	require.Eventually(t, func() bool {
		return st.RemoteStatus().State == state.RemoteStateDisabled
	}, 2*time.Second, 10*time.Millisecond)
	cancel()
	<-done
}

// TestJitter pins the backoff jitter contract: the result always falls in
// [duration/2, duration], and it isn't degenerate (always returning the same
// endpoint) across repeated calls, since jitter's whole purpose is to spread
// out retries from many devices instead of retrying in lockstep.
func TestJitter(t *testing.T) {
	t.Parallel()

	m := &manager{}
	assert.Equal(t, time.Millisecond, m.jitter(time.Millisecond), "at or below the floor, jitter is a no-op")

	const duration = 10 * time.Second
	floor := duration / 2
	seen := make(map[time.Duration]bool)
	for range 50 {
		got := m.jitter(duration)
		require.GreaterOrEqual(t, got, floor)
		require.LessOrEqual(t, got, duration)
		seen[got] = true
	}
	assert.Greater(t, len(seen), 1, "jitter must vary across calls, not always return the same value")
}

func TestSleepWhileEligible(t *testing.T) {
	t.Parallel()

	t.Run("returns after the requested duration", func(t *testing.T) {
		t.Parallel()
		cfg := &config.Instance{}
		m := &manager{deps: Deps{Config: cfg}}
		start := time.Now()
		m.sleepWhileEligible(context.Background(), 50*time.Millisecond, false)
		assert.GreaterOrEqual(t, time.Since(start), 50*time.Millisecond)
	})

	t.Run("returns immediately when context is already cancelled", func(t *testing.T) {
		t.Parallel()
		cfg := &config.Instance{}
		m := &manager{deps: Deps{Config: cfg}}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		start := time.Now()
		m.sleepWhileEligible(ctx, time.Minute, false)
		assert.Less(t, time.Since(start), time.Second)
	})

	t.Run("returns early when consent is disabled and requireEnabled is set", func(t *testing.T) {
		t.Parallel()
		cfg := &config.Instance{}
		cfg.SetRemoteControl(false)
		m := &manager{deps: Deps{Config: cfg}}
		start := time.Now()
		m.sleepWhileEligible(context.Background(), time.Minute, true)
		// Eligibility is checked once per up-to-one-second tick, not
		// continuously, so this returns after about one tick rather than
		// instantly, but well short of the full requested minute.
		assert.Less(t, time.Since(start), 2*time.Second)
	})

	t.Run("does not return early on disabled consent when requireEnabled is unset", func(t *testing.T) {
		t.Parallel()
		cfg := &config.Instance{}
		cfg.SetRemoteControl(false)
		m := &manager{deps: Deps{Config: cfg}}
		start := time.Now()
		m.sleepWhileEligible(context.Background(), 50*time.Millisecond, false)
		assert.GreaterOrEqual(t, time.Since(start), 50*time.Millisecond)
	})
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

// TestStartRunLoopDispatchesOperationThenStopsOnCancel exercises Start and
// run together end to end against a real HTTP server: advertise capability,
// long-poll for work, accept a delivered operation, execute it, and report
// the result, then confirm the loop actually stops (via wg) once its context
// is cancelled while blocked in a second, still-outstanding long poll, which
// is the steady-state shape of the loop for as long as the device stays linked.
func TestStartRunLoopDispatchesOperationThenStopsOnCancel(t *testing.T) {
	acceptedExpiry := time.Now().UTC().Add(time.Minute)
	var acceptedCalls, resultCalls, waitCalls int32
	params := json.RawMessage(`{"message":"hi"}`)
	digest := sha256.Sum256(params)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/device/heartbeat":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/device/remote-sessions/wait":
			if atomic.AddInt32(&waitCalls, 1) == 1 {
				w.Header().Set("Content-Type", "application/json")
				envelope := waitEnvelope{
					Type: "operation_target",
					Operation: &operationEnvelope{
						CommandID: "cmd_run", OperationID: "op_run", OperationType: "echo",
						ProtocolVersion: 1, Params: params,
						DeadlineAt: time.Now().UTC().Add(time.Minute),
						Origin:     operationOrigin{Kind: "first_party"},
					},
				}
				data, err := json.Marshal(envelope)
				if !assert.NoError(t, err) {
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				_, _ = w.Write(data)
				return
			}
			// Every later poll behaves like a real long poll: it blocks until
			// the caller's context (run's ctx, cancelled at test teardown)
			// goes away, rather than returning immediately and spinning.
			<-r.Context().Done()
		case r.Method == http.MethodPost && r.URL.Path == "/v1/device/operations/cmd_run/accepted":
			atomic.AddInt32(&acceptedCalls, 1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"execution_expires_at":"` + acceptedExpiry.Format(time.RFC3339Nano) + `"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/device/operations/cmd_run/result":
			atomic.AddInt32(&resultCalls, 1)
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	cfg := &config.Instance{}
	require.NoError(t, cfg.SetRemoteControlBaseURL(server.URL))
	cfg.SetRemoteControl(true)
	config.SetAuthCfgForTesting(map[string]config.CredentialEntry{
		config.RemoteAuthLookupURL(server.URL): {Bearer: "zpd1_test"},
	})
	t.Cleanup(config.ClearAuthCfgForTesting)
	platform := mocks.NewMockPlatform()
	platform.On("ID").Return("linux")

	userDB := testinghelpers.NewMockUserDBI()
	userDB.On("PruneRemoteCommands", mock.Anything).Return(int64(0), nil).Once()
	userDB.On("ListUnreportedRemoteCommands", resultReplayBatchLimit).
		Return([]database.RemoteCommand{}, nil).Once()
	stored := &database.RemoteCommand{
		CommandID: "cmd_run", OperationID: "op_run", OperationType: "echo",
		ProtocolVersion: 1, ParamsDigest: hex.EncodeToString(digest[:]),
		Origin:     json.RawMessage(`{"kind":"first_party"}`),
		DeadlineAt: time.Now().UTC().Add(time.Minute), State: "recorded",
	}
	userDB.On("ClaimRemoteCommand", mock.Anything).Return(stored, true, nil).Once()
	userDB.On("TransitionRemoteCommand", "cmd_run", "recorded", "accepted", mock.Anything).
		Return(true, nil).Once()
	userDB.On("TransitionRemoteCommand", "cmd_run", "accepted", "executing", mock.Anything).
		Return(true, nil).Once()
	userDB.On("StoreRemoteCommandResult", "cmd_run", "executing", "succeeded", mock.Anything, "").
		Return(true, nil).Once()
	userDB.On("MarkRemoteCommandResultReported", "cmd_run").Return(nil).Once()

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	Start(ctx, &Deps{Platform: platform, Config: cfg, DB: &database.Database{UserDB: userDB}}, &wg)

	require.Eventually(t, func() bool {
		return atomic.LoadInt32(&resultCalls) == 1
	}, 2*time.Second, 10*time.Millisecond, "operation result was never reported")
	assert.Equal(t, int32(1), acceptedCalls)

	cancel()
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("run loop did not stop after context cancellation")
	}
	userDB.AssertExpectations(t)
}

// TestRunStopsPromptlyOnCancelWhileDisabled pins that the loop's "not
// eligible" branch (consent off) never touches the network and still stops
// cleanly on context cancellation, rather than looping or blocking forever.
func TestRunStopsPromptlyOnCancelWhileDisabled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		t.Errorf("disabled remote control must not make requests, got %s", r.URL.Path)
	}))
	defer server.Close()
	m := newHTTPTestManager(t, server.URL)
	m.deps.Config.SetRemoteControl(false)
	userDB := testinghelpers.NewMockUserDBI()
	userDB.On("PruneRemoteCommands", mock.Anything).Return(int64(0), nil).Once()
	m.deps.DB = &database.Database{UserDB: userDB}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		m.run(ctx)
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("run did not stop after context cancellation while disabled")
	}
	userDB.AssertExpectations(t)
}

// TestRunRetriesImmediatelyAfterSupersededHeartbeatRejection pins the
// re-link race end to end. sleepWhileEligible only wakes early when remote
// control is switched off or the credential is cleared, so a rotated bearer
// used to sit out the full one-minute rejection back-off before the new
// credential got its first try.
func TestRunRetriesImmediatelyAfterSupersededHeartbeatRejection(t *testing.T) {
	var heartbeatCalls int32
	var rotated int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/v1/device/heartbeat" {
			atomic.AddInt32(&heartbeatCalls, 1)
			// Re-link lands while this request is in flight, so the 401 below
			// answers a bearer that is already gone.
			if atomic.CompareAndSwapInt32(&rotated, 0, 1) {
				config.SetAuthCfgForTesting(map[string]config.CredentialEntry{
					config.RemoteAuthLookupURL(r.Host): {Bearer: "zpd1_new"},
				})
			}
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		t.Errorf("unexpected request after unauthorized heartbeat: %s %s", r.Method, r.URL.Path)
	}))
	defer server.Close()
	m := newHTTPTestManager(t, server.URL)
	// The rotation above rewrites the whole auth config, so key it the same
	// way newHTTPTestManager does.
	config.SetAuthCfgForTesting(map[string]config.CredentialEntry{
		config.RemoteAuthLookupURL(server.URL): {Bearer: "zpd1_test"},
	})
	userDB := testinghelpers.NewMockUserDBI()
	userDB.On("PruneRemoteCommands", mock.Anything).Return(int64(0), nil).Maybe()
	m.deps.DB = &database.Database{UserDB: userDB}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		m.run(ctx)
		close(done)
	}()

	require.Eventually(t, func() bool {
		return atomic.LoadInt32(&heartbeatCalls) >= 2
	}, 3*time.Second, 10*time.Millisecond,
		"the new credential was held behind the rejection back-off")

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("run did not stop after context cancellation")
	}
}

// TestRunUnauthorizedHeartbeatStopsCleanlyOnCancel pins the credential-
// rejected branch of the capability heartbeat: it must not panic or spin
// tightly (markUnlinkedIfSharedEndpoint is a safe no-op here since
// RemoteControlBaseURL and BackupRemoteBaseURL differ by default), and the
// loop must still stop cleanly once its context is cancelled while backed off.
func TestRunUnauthorizedHeartbeatStopsCleanlyOnCancel(t *testing.T) {
	var heartbeatCalls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/v1/device/heartbeat" {
			atomic.AddInt32(&heartbeatCalls, 1)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		t.Errorf("unexpected request after unauthorized heartbeat: %s %s", r.Method, r.URL.Path)
	}))
	defer server.Close()
	m := newHTTPTestManager(t, server.URL)
	userDB := testinghelpers.NewMockUserDBI()
	userDB.On("PruneRemoteCommands", mock.Anything).Return(int64(0), nil).Once()
	m.deps.DB = &database.Database{UserDB: userDB}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		m.run(ctx)
		close(done)
	}()

	require.Eventually(t, func() bool {
		return atomic.LoadInt32(&heartbeatCalls) >= 1
	}, 2*time.Second, 10*time.Millisecond, "heartbeat was never attempted")

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("run did not stop after context cancellation following an unauthorized heartbeat")
	}
	userDB.AssertExpectations(t)
}
