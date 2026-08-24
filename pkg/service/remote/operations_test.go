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
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	testinghelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestOperationAcceptExecuteReportLifecycle(t *testing.T) {
	var acceptedCalls, resultCalls int
	acceptedExpiry := time.Now().UTC().Add(time.Minute)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/device/operations/cmd_test/accepted":
			acceptedCalls++
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"execution_expires_at":"` + acceptedExpiry.Format(time.RFC3339Nano) + `"}`))
		case "/v1/device/operations/cmd_test/result":
			resultCalls++
			var result operationResult
			if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&result)) {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			assert.Equal(t, "succeeded", result.Status)
			assert.JSONEq(t, `{"message":"hello"}`, string(result.Result))
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	m := newHTTPTestManager(t, server.URL)
	userDB := testinghelpers.NewMockUserDBI()
	m.deps.DB = &database.Database{UserDB: userDB}
	params := json.RawMessage(`{"message":"hello"}`)
	digest := sha256.Sum256(params)
	origin := json.RawMessage(`{"kind":"first_party"}`)
	stored := &database.RemoteCommand{
		CommandID: "cmd_test", OperationID: "op_test", OperationType: "echo",
		ProtocolVersion: 1, ParamsDigest: hex.EncodeToString(digest[:]), Origin: origin,
		DeadlineAt: time.Now().UTC().Add(2 * time.Minute), State: "recorded",
	}
	userDB.On("ClaimRemoteCommand", mock.Anything).Return(stored, true, nil).Once()
	userDB.On("TransitionRemoteCommand", "cmd_test", "recorded", "accepted", mock.Anything).
		Return(true, nil).Once()
	userDB.On("TransitionRemoteCommand", "cmd_test", "accepted", "executing", mock.Anything).
		Return(true, nil).Once()
	userDB.On("StoreRemoteCommandResult", "cmd_test", "executing", "succeeded", mock.Anything, "").
		Return(true, nil).Once()
	userDB.On("MarkRemoteCommandResultReported", "cmd_test").Return(nil).Once()

	m.handleOperation(context.Background(), &operationEnvelope{
		CommandID: "cmd_test", OperationID: "op_test", OperationType: "echo",
		ProtocolVersion: 1, Params: params, DeadlineAt: stored.DeadlineAt,
		Origin: operationOrigin{Kind: "first_party"},
	}, false)

	assert.Equal(t, 1, acceptedCalls)
	assert.Equal(t, 1, resultCalls)
	userDB.AssertExpectations(t)
}

func TestOperationReportsAfterExecutionDeadline(t *testing.T) {
	var resultCalls int
	acceptedExpiry := time.Now().UTC().Add(250 * time.Millisecond)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/device/operations/cmd_deadline/accepted":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"execution_expires_at":"` + acceptedExpiry.Format(time.RFC3339Nano) + `"}`))
		case "/v1/device/operations/cmd_deadline/result":
			resultCalls++
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	m := newHTTPTestManager(t, server.URL)
	m.execute = func(ctx context.Context, _ *operationEnvelope) operationResult {
		<-ctx.Done()
		return failResult("execution_timeout")
	}
	userDB := testinghelpers.NewMockUserDBI()
	m.deps.DB = &database.Database{UserDB: userDB}
	params := json.RawMessage(`{"message":"slow"}`)
	digest := sha256.Sum256(params)
	stored := &database.RemoteCommand{
		CommandID: "cmd_deadline", OperationID: "op_deadline", OperationType: "echo",
		ProtocolVersion: 1, ParamsDigest: hex.EncodeToString(digest[:]),
		Origin:     json.RawMessage(`{"kind":"first_party"}`),
		DeadlineAt: time.Now().UTC().Add(time.Minute), State: "recorded",
	}
	userDB.On("ClaimRemoteCommand", mock.Anything).Return(stored, true, nil).Once()
	userDB.On("TransitionRemoteCommand", "cmd_deadline", "recorded", "accepted", mock.Anything).
		Return(true, nil).Once()
	userDB.On("TransitionRemoteCommand", "cmd_deadline", "accepted", "executing", mock.Anything).
		Return(true, nil).Once()
	userDB.On("StoreRemoteCommandResult", "cmd_deadline", "executing", "failed", mock.Anything, "execution_timeout").
		Return(true, nil).Once()
	userDB.On("MarkRemoteCommandResultReported", "cmd_deadline").Return(nil).Once()

	m.handleOperation(context.Background(), &operationEnvelope{
		CommandID: "cmd_deadline", OperationID: "op_deadline", OperationType: "echo",
		ProtocolVersion: 1, Params: params, DeadlineAt: stored.DeadlineAt,
		Origin: operationOrigin{Kind: "first_party"},
	}, false)

	assert.Equal(t, 1, resultCalls)
	userDB.AssertExpectations(t)
}

func TestOperationExpiredEnvelopeNeverAcceptsOrExecutes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		t.Errorf("expired operation made unexpected request to %s", r.URL.Path)
	}))
	defer server.Close()
	m := newHTTPTestManager(t, server.URL)
	userDB := testinghelpers.NewMockUserDBI()
	m.deps.DB = &database.Database{UserDB: userDB}
	params := json.RawMessage(`{"message":"late"}`)
	digest := sha256.Sum256(params)
	deadline := time.Now().UTC().Add(-time.Second)
	stored := &database.RemoteCommand{
		CommandID: "cmd_late", OperationID: "op_late", OperationType: "echo",
		ProtocolVersion: 1, ParamsDigest: hex.EncodeToString(digest[:]),
		Origin: json.RawMessage(`{"kind":"first_party"}`), DeadlineAt: deadline, State: "recorded",
	}
	userDB.On("ClaimRemoteCommand", mock.Anything).Return(stored, true, nil).Once()
	userDB.On("TransitionRemoteCommand", "cmd_late", "recorded", "expired", (*time.Time)(nil)).
		Return(true, nil).Once()

	m.handleOperation(context.Background(), &operationEnvelope{
		CommandID: "cmd_late", OperationID: "op_late", OperationType: "echo",
		ProtocolVersion: 1, Params: params, DeadlineAt: deadline,
		Origin: operationOrigin{Kind: "first_party"},
	}, false)
	userDB.AssertExpectations(t)
}

// TestOperationRedeliveredAcceptedWithLapsedExecutionWindowExpires covers a
// crash/restart between the accepted->executing transition and the server
// receiving a result: the server redelivers the same command ID while the
// stored row is still "accepted", carrying an ExecutionExpiresAt from the
// original acceptance that has since lapsed. That stale lease must not be
// reused to execute the command; it must transition straight to "expired".
func TestOperationRedeliveredAcceptedWithLapsedExecutionWindowExpires(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		t.Errorf("redelivered expired-lease operation made unexpected request to %s", r.URL.Path)
	}))
	defer server.Close()
	m := newHTTPTestManager(t, server.URL)
	userDB := testinghelpers.NewMockUserDBI()
	m.deps.DB = &database.Database{UserDB: userDB}
	params := json.RawMessage(`{"message":"stale"}`)
	digest := sha256.Sum256(params)
	lapsedExpiry := time.Now().UTC().Add(-time.Minute)
	stored := &database.RemoteCommand{
		CommandID: "cmd_redelivered", OperationID: "op_redelivered", OperationType: "echo",
		ProtocolVersion: 1, ParamsDigest: hex.EncodeToString(digest[:]),
		Origin:             json.RawMessage(`{"kind":"first_party"}`),
		DeadlineAt:         time.Now().UTC().Add(time.Minute),
		State:              "accepted",
		ExecutionExpiresAt: &lapsedExpiry,
	}
	userDB.On("ClaimRemoteCommand", mock.Anything).Return(stored, false, nil).Once()
	userDB.On("TransitionRemoteCommand", "cmd_redelivered", "accepted", "expired", (*time.Time)(nil)).
		Return(true, nil).Once()

	m.handleOperation(context.Background(), &operationEnvelope{
		CommandID: "cmd_redelivered", OperationID: "op_redelivered", OperationType: "echo",
		ProtocolVersion: 1, Params: params, DeadlineAt: stored.DeadlineAt,
		Origin: operationOrigin{Kind: "first_party"},
	}, false)
	userDB.AssertExpectations(t)
}

// TestOperationBusyRedeliveryOfInFlightAcceptedNeverFinalizes covers the
// concurrent counterpart to the crash/restart resume case above: a
// redelivery of the same command ID arrives via the synchronous busy path
// while a separate goroutine is genuinely still mid-flight between its own
// recorded->accepted and accepted->executing transitions (holding the sole
// execution slot). The busy redelivery must not finalize the command
// terminal "busy" out from under that in-flight transition; it must do
// nothing at all and let the real owner report the eventual result.
func TestOperationBusyRedeliveryOfInFlightAcceptedNeverFinalizes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		t.Errorf("busy redelivery of in-flight accepted command made unexpected request to %s", r.URL.Path)
	}))
	defer server.Close()
	m := newHTTPTestManager(t, server.URL)
	userDB := testinghelpers.NewMockUserDBI()
	m.deps.DB = &database.Database{UserDB: userDB}
	params := json.RawMessage(`{"message":"racing"}`)
	digest := sha256.Sum256(params)
	leaseExpiry := time.Now().UTC().Add(time.Minute)
	stored := &database.RemoteCommand{
		CommandID: "cmd_racing", OperationID: "op_racing", OperationType: "echo",
		ProtocolVersion: 1, ParamsDigest: hex.EncodeToString(digest[:]),
		Origin:             json.RawMessage(`{"kind":"first_party"}`),
		DeadlineAt:         time.Now().UTC().Add(time.Minute),
		State:              "accepted",
		ExecutionExpiresAt: &leaseExpiry,
	}
	userDB.On("ClaimRemoteCommand", mock.Anything).Return(stored, false, nil).Once()

	m.handleOperation(context.Background(), &operationEnvelope{
		CommandID: "cmd_racing", OperationID: "op_racing", OperationType: "echo",
		ProtocolVersion: 1, Params: params, DeadlineAt: stored.DeadlineAt,
		Origin: operationOrigin{Kind: "first_party"},
	}, true)
	userDB.AssertExpectations(t)
}

// TestFinishOperationRetriesTransientPersistFailureAndSucceeds pins that a
// StoreRemoteCommandResult write error is retried rather than dropping the
// already-computed result immediately: two transient failures followed by a
// success must still report the result and mark it reported, not give up
// after the first error.
func TestFinishOperationRetriesTransientPersistFailureAndSucceeds(t *testing.T) {
	origDelay := resultPersistRetryDelay
	resultPersistRetryDelay = time.Millisecond
	t.Cleanup(func() { resultPersistRetryDelay = origDelay })

	var resultCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/device/operations/cmd_retry/result", r.URL.Path)
		resultCalls++
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	m := newHTTPTestManager(t, server.URL)
	userDB := testinghelpers.NewMockUserDBI()
	m.deps.DB = &database.Database{UserDB: userDB}

	userDB.On("StoreRemoteCommandResult", "cmd_retry", "executing", "succeeded", mock.Anything, "").
		Return(false, errors.New("database is locked")).Twice()
	userDB.On("StoreRemoteCommandResult", "cmd_retry", "executing", "succeeded", mock.Anything, "").
		Return(true, nil).Once()
	userDB.On("MarkRemoteCommandResultReported", "cmd_retry").Return(nil).Once()

	m.finishOperation(context.Background(), "cmd_retry", "executing", operationResult{Status: "succeeded"})

	assert.Equal(t, 1, resultCalls)
	userDB.AssertExpectations(t)
}

// TestFinishOperationGivesUpAfterPersistRetriesExhausted pins the retry
// bound: a persistently failing write must not retry forever, and once
// exhausted must never post a result the device could not durably record,
// since a redelivery has no way to recover an unrecorded result either.
func TestFinishOperationGivesUpAfterPersistRetriesExhausted(t *testing.T) {
	origDelay := resultPersistRetryDelay
	resultPersistRetryDelay = time.Millisecond
	t.Cleanup(func() { resultPersistRetryDelay = origDelay })

	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		t.Errorf("result must not be posted after persist retries are exhausted, got request to %s", r.URL.Path)
	}))
	defer server.Close()
	m := newHTTPTestManager(t, server.URL)
	userDB := testinghelpers.NewMockUserDBI()
	m.deps.DB = &database.Database{UserDB: userDB}

	userDB.On("StoreRemoteCommandResult", "cmd_stuck", "executing", "succeeded", mock.Anything, "").
		Return(false, errors.New("disk full")).Times(resultPersistRetries)

	m.finishOperation(context.Background(), "cmd_stuck", "executing", operationResult{Status: "succeeded"})
	userDB.AssertExpectations(t)
}

func TestOperationReplaysStoredResultWithoutAcceptance(t *testing.T) {
	var resultCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/device/operations/cmd_replay/result", r.URL.Path)
		resultCalls++
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	m := newHTTPTestManager(t, server.URL)
	userDB := testinghelpers.NewMockUserDBI()
	m.deps.DB = &database.Database{UserDB: userDB}
	params := json.RawMessage(`{"message":"same"}`)
	digest := sha256.Sum256(params)
	stored := &database.RemoteCommand{
		CommandID: "cmd_replay", OperationID: "op_replay", OperationType: "echo",
		ProtocolVersion: 1, ParamsDigest: hex.EncodeToString(digest[:]),
		Origin:     json.RawMessage(`{"kind":"first_party"}`),
		DeadlineAt: time.Now().Add(time.Minute), State: "terminal", ResultStatus: "succeeded",
		Result: json.RawMessage(`{"message":"same"}`),
	}
	userDB.On("ClaimRemoteCommand", mock.Anything).Return(stored, false, nil).Once()
	userDB.On("MarkRemoteCommandResultReported", "cmd_replay").Return(nil).Once()

	m.handleOperation(context.Background(), &operationEnvelope{
		CommandID: "cmd_replay", OperationID: "op_replay", OperationType: "echo",
		ProtocolVersion: 1, Params: params, DeadlineAt: stored.DeadlineAt,
		Origin: operationOrigin{Kind: "first_party"},
	}, false)
	assert.Equal(t, 1, resultCalls)
	userDB.AssertExpectations(t)
}

// TestReplayStoredResultsUsesBatchLimit pins that a replay cycle asks the
// database for at most resultReplayBatchLimit unreported results, not an
// unbounded list: a large backlog is meant to drain over several replay
// cycles (see run()'s resultReplayInterval), not be walked in one call.
func TestReplayStoredResultsUsesBatchLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	m := newHTTPTestManager(t, server.URL)
	userDB := testinghelpers.NewMockUserDBI()
	m.deps.DB = &database.Database{UserDB: userDB}
	userDB.On("ListUnreportedRemoteCommands", resultReplayBatchLimit).
		Return([]database.RemoteCommand{}, nil).Once()

	m.replayStoredResults(context.Background())
	userDB.AssertExpectations(t)
}

// TestReplayStoredResultsDoesNotBlockConcurrentFinishOperationForWholeBatch
// pins the resultMu fairness fix: replayStoredResults must not hold resultMu
// across its entire batch, or a live operation's finishOperation call could
// be stuck behind an arbitrarily long backlog. Five unreported results are
// replayed, each taking resultPostLatency to post; a concurrent
// finishOperation call for an unrelated, already-executing command starts
// partway through the first post. If resultMu is released between items (as
// opposed to held for the whole loop), the waiting finishOperation call gets
// it back as soon as the first post finishes and does not have to wait for
// the other four; the elapsed time it actually takes is only explainable by
// that per-item release.
func TestReplayStoredResultsDoesNotBlockConcurrentFinishOperationForWholeBatch(t *testing.T) {
	const resultPostLatency = 100 * time.Millisecond
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(resultPostLatency)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	m := newHTTPTestManager(t, server.URL)
	userDB := testinghelpers.NewMockUserDBI()
	m.deps.DB = &database.Database{UserDB: userDB}

	commands := make([]database.RemoteCommand, 5)
	for i, id := range []string{"cmd_1", "cmd_2", "cmd_3", "cmd_4", "cmd_5"} {
		commands[i] = database.RemoteCommand{CommandID: id, State: "terminal", ResultStatus: "succeeded"}
		userDB.On("MarkRemoteCommandResultReported", id).Return(nil).Maybe()
	}
	userDB.On("ListUnreportedRemoteCommands", resultReplayBatchLimit).Return(commands, nil).Once()
	userDB.On("StoreRemoteCommandResult", "cmd_live", "executing", "succeeded", mock.Anything, "").
		Return(true, nil).Once()
	userDB.On("MarkRemoteCommandResultReported", "cmd_live").Return(nil).Once()

	replayDone := make(chan struct{})
	go func() {
		m.replayStoredResults(context.Background())
		close(replayDone)
	}()

	time.Sleep(resultPostLatency / 2)
	finishStart := time.Now()
	m.finishOperation(context.Background(), "cmd_live", "executing", operationResult{Status: "succeeded"})
	finishElapsed := time.Since(finishStart)

	// Held-for-the-whole-batch would need ~5*resultPostLatency before
	// finishOperation even gets a chance at resultMu, plus its own post:
	// ~550ms. Released between items lets it in right after the first post:
	// ~150ms. This threshold sits well clear of both.
	assert.Less(t, finishElapsed, 3*resultPostLatency,
		"finishOperation must not wait for the whole replay batch, only until resultMu frees up between items")

	<-replayDone
}

func TestOperationEchoAndUnknownType(t *testing.T) {
	m := &manager{}
	echo := m.executeOperation(context.Background(), &operationEnvelope{
		OperationType: "echo", Params: []byte(`{"message":"hello"}`),
	})
	assert.Equal(t, "succeeded", echo.Status)
	assert.JSONEq(t, `{"message":"hello"}`, string(echo.Result))

	unknown := m.executeOperation(context.Background(), &operationEnvelope{OperationType: "run"})
	assert.Equal(t, "failed", unknown.Status)
	assert.Equal(t, "unknown_type", unknown.ErrorCode)
}

// TestOperationDispatchesMethodBackedOperationThroughAllowlist is an
// end-to-end check that a method-backed operation_type reaches the shared
// API registry with a non-admin role, scoped params, and the exact
// camelCase result shape — not a bespoke adapter.
func TestOperationDispatchesMethodBackedOperationThroughAllowlist(t *testing.T) {
	var sawRole string
	m := &manager{deps: Deps{Methods: fakeMethodResolver{
		"systems": func(env requests.RequestEnv) (any, error) {
			sawRole = env.ClientRole
			var params models.SystemsParams
			require.NoError(t, json.Unmarshal(env.Params, &params))
			require.True(t, params.All)
			return models.SystemsResponse{Systems: []models.System{{ID: "SNES"}}}, nil
		},
	}}}

	result := m.executeOperation(context.Background(), &operationEnvelope{
		OperationType: "systems", Params: json.RawMessage(`{"all":true}`),
	})
	assert.Equal(t, "succeeded", result.Status)
	assert.JSONEq(t, `{"systems":[{"id":"SNES"}]}`, string(result.Result))
	assert.Equal(t, "remote", sawRole)
}

// TestOperationRejectsUnscopedLaunchersParams verifies the bad_params gate
// runs before dispatch: launchers has no shrink path, so an unscoped
// request must be refused rather than reaching the registry at all.
func TestOperationRejectsUnscopedLaunchersParams(t *testing.T) {
	called := false
	m := &manager{deps: Deps{Methods: fakeMethodResolver{
		"launchers": func(requests.RequestEnv) (any, error) {
			called = true
			return models.LaunchersResponse{}, nil
		},
	}}}

	result := m.executeOperation(context.Background(), &operationEnvelope{
		OperationType: "launchers", Params: json.RawMessage(`{}`),
	})
	assert.Equal(t, "failed", result.Status)
	assert.Equal(t, "bad_params", result.ErrorCode)
	assert.False(t, called, "handler must not be reached when params fail the allowlist gate")
}
