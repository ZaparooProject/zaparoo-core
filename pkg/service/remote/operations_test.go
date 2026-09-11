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
	"strings"
	"sync/atomic"
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

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (fn roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func newNoRequestTestManager(t *testing.T) *manager {
	t.Helper()
	m := newHTTPTestManager(t, "https://online.example")
	m.httpClient = &http.Client{Transport: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		t.Errorf("unexpected remote operation request to %s", request.URL.Path)
		return nil, errors.New("unexpected remote operation request")
	})}
	return m
}

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
			t.Errorf("unexpected request path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
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

// TestFinishOperationPostResultFailureDoesNotMarkReported pins that a
// finished operation whose result POST fails is never marked reported: the
// result was durably persisted locally (StoreRemoteCommandResult succeeded),
// but the server never received it, so it must remain eligible for
// replayStoredResults to retry later, not be silently dropped.
func TestFinishOperationPostResultFailureDoesNotMarkReported(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/device/operations/cmd_postfail/result", r.URL.Path)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	m := newHTTPTestManager(t, server.URL)
	userDB := testinghelpers.NewMockUserDBI()
	m.deps.DB = &database.Database{UserDB: userDB}

	userDB.On("StoreRemoteCommandResult", "cmd_postfail", "executing", "succeeded", mock.Anything, "").
		Return(true, nil).Once()

	m.finishOperation(context.Background(), "cmd_postfail", "executing", operationResult{Status: "succeeded"})

	userDB.AssertExpectations(t)
	userDB.AssertNotCalled(t, "MarkRemoteCommandResultReported", mock.Anything)
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

func TestOperationRejectsRedeliveryWithChangedDeadline(t *testing.T) {
	m := newNoRequestTestManager(t)
	userDB := testinghelpers.NewMockUserDBI()
	m.deps.DB = &database.Database{UserDB: userDB}
	params := json.RawMessage(`{"message":"same"}`)
	digest := sha256.Sum256(params)
	stored := &database.RemoteCommand{
		CommandID: "cmd_deadline_conflict", OperationID: "op_deadline_conflict", OperationType: "echo",
		ProtocolVersion: 1, ParamsDigest: hex.EncodeToString(digest[:]),
		Origin:     json.RawMessage(`{"kind":"first_party"}`),
		DeadlineAt: time.Now().UTC().Add(time.Minute), State: "terminal", ResultStatus: "succeeded",
		Result: json.RawMessage(`{"message":"same"}`),
	}
	userDB.On("ClaimRemoteCommand", mock.Anything).Return(stored, false, nil).Once()

	m.handleOperation(context.Background(), &operationEnvelope{
		CommandID: "cmd_deadline_conflict", OperationID: "op_deadline_conflict", OperationType: "echo",
		ProtocolVersion: 1, Params: params, DeadlineAt: stored.DeadlineAt.Add(time.Minute),
		Origin: operationOrigin{Kind: "first_party"},
	}, false)

	userDB.AssertExpectations(t)
	userDB.AssertNotCalled(t, "MarkRemoteCommandResultReported", mock.Anything)
}

func TestOperationCannotExtendRecordedDeadline(t *testing.T) {
	m := newNoRequestTestManager(t)
	userDB := testinghelpers.NewMockUserDBI()
	m.deps.DB = &database.Database{UserDB: userDB}
	params := json.RawMessage(`{"message":"late"}`)
	digest := sha256.Sum256(params)
	stored := &database.RemoteCommand{
		CommandID: "cmd_recorded_deadline", OperationID: "op_recorded_deadline", OperationType: "echo",
		ProtocolVersion: 1, ParamsDigest: hex.EncodeToString(digest[:]),
		Origin:     json.RawMessage(`{"kind":"first_party"}`),
		DeadlineAt: time.Now().UTC().Add(-time.Minute), State: "recorded",
	}
	userDB.On("ClaimRemoteCommand", mock.Anything).Return(stored, false, nil).Once()

	m.handleOperation(context.Background(), &operationEnvelope{
		CommandID: "cmd_recorded_deadline", OperationID: "op_recorded_deadline", OperationType: "echo",
		ProtocolVersion: 1, Params: params, DeadlineAt: time.Now().UTC().Add(time.Minute),
		Origin: operationOrigin{Kind: "first_party"},
	}, false)

	userDB.AssertExpectations(t)
	userDB.AssertNotCalled(t, "TransitionRemoteCommand", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
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
// once the first post completes. The two outcomes are told apart by event
// order, not a wall-clock threshold: if resultMu is released between items,
// finishOperation completes while the remaining four posts are still in
// flight; if it were held for the whole batch, finishOperation could not
// even attempt its own post until after replayStoredResults had already
// finished.
func TestReplayStoredResultsDoesNotBlockConcurrentFinishOperationForWholeBatch(t *testing.T) {
	const resultPostLatency = 100 * time.Millisecond
	firstPostDone := make(chan struct{})
	var posts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(resultPostLatency)
		w.WriteHeader(http.StatusNoContent)
		if posts.Add(1) == 1 {
			close(firstPostDone)
		}
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

	select {
	case <-firstPostDone:
	case <-time.After(2 * time.Second):
		t.Fatal("first replay post never completed")
	}

	finishDone := make(chan struct{})
	go func() {
		m.finishOperation(context.Background(), "cmd_live", "executing", operationResult{Status: "succeeded"})
		close(finishDone)
	}()

	select {
	case <-finishDone:
		select {
		case <-replayDone:
			t.Fatal("finishOperation did not proceed until the whole replay batch finished")
		default:
			// The remaining replay posts are still in flight: resultMu was
			// free for finishOperation to acquire between items.
		}
	case <-time.After(2 * time.Second):
		t.Fatal("finishOperation did not complete promptly; resultMu appears held for the whole replay batch")
	}

	<-replayDone
}

// TestValidateEnvelope pins every rejection reason validateEnvelope enforces
// before an operation envelope is trusted: a malformed or spoofed envelope
// must never reach ClaimRemoteCommand or execution.
func TestValidateEnvelope(t *testing.T) {
	t.Parallel()

	base := func() operationEnvelope {
		return operationEnvelope{
			CommandID: "cmd_1", OperationID: "op_1", OperationType: "echo",
			ProtocolVersion: protocolVersion, DeadlineAt: time.Now().Add(time.Minute),
			Origin: operationOrigin{Kind: "first_party"},
		}
	}

	tests := []struct {
		mutate  func(*operationEnvelope)
		name    string
		wantErr string
	}{
		{
			name:    "missing command id",
			mutate:  func(e *operationEnvelope) { e.CommandID = "" },
			wantErr: "missing remote operation identifier or type",
		},
		{
			name:    "missing operation id",
			mutate:  func(e *operationEnvelope) { e.OperationID = "" },
			wantErr: "missing remote operation identifier or type",
		},
		{
			name:    "missing operation type",
			mutate:  func(e *operationEnvelope) { e.OperationType = "" },
			wantErr: "missing remote operation identifier or type",
		},
		{
			name:    "command id contains path separator",
			mutate:  func(e *operationEnvelope) { e.CommandID = "cmd/1" },
			wantErr: "invalid remote command identifier",
		},
		{
			name:    "command id contains backslash",
			mutate:  func(e *operationEnvelope) { e.CommandID = "cmd\\1" },
			wantErr: "invalid remote command identifier",
		},
		{
			name:    "command id contains control character",
			mutate:  func(e *operationEnvelope) { e.CommandID = "cmd\n1" },
			wantErr: "invalid remote command identifier",
		},
		{
			name:    "operation id contains invalid UTF-8",
			mutate:  func(e *operationEnvelope) { e.OperationID = string([]byte{0xff}) },
			wantErr: "invalid remote operation identifier",
		},
		{
			name: "operation type is oversized",
			mutate: func(e *operationEnvelope) {
				e.OperationType = strings.Repeat("x", remoteOperationTypeMaxLength+1)
			},
			wantErr: "invalid remote operation type",
		},
		{
			name:    "unsupported protocol version",
			mutate:  func(e *operationEnvelope) { e.ProtocolVersion = protocolVersion + 1 },
			wantErr: "unsupported protocol version",
		},
		{
			name:    "missing deadline",
			mutate:  func(e *operationEnvelope) { e.DeadlineAt = time.Time{} },
			wantErr: "missing remote operation deadline",
		},
		{
			name:    "invalid origin kind",
			mutate:  func(e *operationEnvelope) { e.Origin = operationOrigin{Kind: "bogus"} },
			wantErr: "invalid remote operation origin",
		},
		{
			name:    "api_key origin missing key name",
			mutate:  func(e *operationEnvelope) { e.Origin = operationOrigin{Kind: "api_key"} },
			wantErr: "API-key origin missing key name",
		},
		{
			name: "api_key origin contains terminal controls",
			mutate: func(e *operationEnvelope) {
				e.Origin = operationOrigin{Kind: "api_key", KeyName: "owner\n\x1b[31m"}
			},
			wantErr: "invalid remote operation origin key name",
		},
		{
			name: "api_key origin contains bidi override",
			mutate: func(e *operationEnvelope) {
				e.Origin = operationOrigin{Kind: "api_key", KeyName: "owner\u202eexe"}
			},
			wantErr: "invalid remote operation origin key name",
		},
		{
			name: "api_key origin name is oversized",
			mutate: func(e *operationEnvelope) {
				e.Origin = operationOrigin{
					Kind: "api_key", KeyName: strings.Repeat("k", remoteOriginKeyNameMaxLength+1),
				}
			},
			wantErr: "invalid remote operation origin key name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			envelope := base()
			tt.mutate(&envelope)
			err := validateEnvelope(&envelope)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}

	t.Run("valid api_key origin with key name", func(t *testing.T) {
		t.Parallel()
		envelope := base()
		envelope.Origin = operationOrigin{Kind: "api_key", KeyName: "ci-key"}
		assert.NoError(t, validateEnvelope(&envelope))
	})
}

// TestHandleTransitionHTTPError pins how a failed accept/transition HTTP
// call maps to a local ledger transition: only a definitive "the server no
// longer knows this command" response (404/410) may move the command out of
// the caller's state locally, everything else is left for a future retry or
// redelivery to resolve.
func TestHandleTransitionHTTPError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		err         error
		name        string
		wantToState string
		wantCall    bool
	}{
		{name: "not found voids", err: &httpError{status: http.StatusNotFound}, wantCall: true, wantToState: "void"},
		{name: "gone expires", err: &httpError{status: http.StatusGone}, wantCall: true, wantToState: "expired"},
		{name: "conflict is left alone", err: &httpError{status: http.StatusConflict}, wantCall: false},
		{name: "unauthorized is left alone", err: &httpError{status: http.StatusUnauthorized}, wantCall: false},
		{name: "other status is left alone", err: &httpError{status: http.StatusInternalServerError}, wantCall: false},
		{name: "non-http error is left alone", err: errors.New("network unreachable"), wantCall: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := &manager{}
			userDB := testinghelpers.NewMockUserDBI()
			m.deps.DB = &database.Database{UserDB: userDB}
			if tt.wantCall {
				userDB.On("TransitionRemoteCommand", "cmd_x", "recorded", tt.wantToState, (*time.Time)(nil)).
					Return(true, nil).Once()
			}
			m.handleTransitionHTTPError("cmd_x", "recorded", tt.err)
			userDB.AssertExpectations(t)
		})
	}
}

// TestHandleResultPostError pins that only a definitive "the server no
// longer knows this command" response (404/410) closes the result out
// locally (so it stops being replayed forever); any other failure is left
// for the next replay cycle to retry.
func TestHandleResultPostError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		err      error
		name     string
		wantCall bool
	}{
		{name: "not found closes out the command", err: &httpError{status: http.StatusNotFound}, wantCall: true},
		{name: "gone closes out the command", err: &httpError{status: http.StatusGone}, wantCall: true},
		{name: "other status just logs", err: &httpError{status: http.StatusInternalServerError}, wantCall: false},
		{name: "non-http error just logs", err: errors.New("network unreachable"), wantCall: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := &manager{}
			userDB := testinghelpers.NewMockUserDBI()
			m.deps.DB = &database.Database{UserDB: userDB}
			if tt.wantCall {
				userDB.On("MarkRemoteCommandResultReported", "cmd_y").Return(nil).Once()
			}
			m.handleResultPostError("cmd_y", tt.err)
			userDB.AssertExpectations(t)
		})
	}
}

func TestRequireEmptyParams(t *testing.T) {
	t.Parallel()
	assert.NoError(t, requireEmptyParams(nil))
	assert.NoError(t, requireEmptyParams(json.RawMessage(`{}`)))
	assert.Error(t, requireEmptyParams(json.RawMessage(`{"foo":"bar"}`)))
}

func TestSucceedResult_ExceedsLimitFails(t *testing.T) {
	t.Parallel()
	result := succeedResult(map[string]string{"message": "hi"}, 1)
	assert.Equal(t, "failed", result.Status)
	assert.Equal(t, "result_too_large", result.ErrorCode)
}

func TestExecuteEcho_RejectsUnknownFields(t *testing.T) {
	t.Parallel()
	m := &manager{}
	result := m.executeEcho(context.Background(), "echo", json.RawMessage(`{"message":"hi","extra":"field"}`))
	assert.Equal(t, "failed", result.Status)
	assert.Equal(t, "bad_params", result.ErrorCode)
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
// API registry with a non-admin role and scoped params, and that its
// response comes back in the snake_case wire shape — not the model's own
// camelCase.
func TestOperationDispatchesMethodBackedOperationThroughAllowlist(t *testing.T) {
	var sawRole string
	m := &manager{deps: Deps{Methods: fakeMethodResolver{
		"systems": func(env requests.RequestEnv) (any, error) {
			sawRole = env.ClientRole
			var params models.SystemsParams
			require.NoError(t, json.Unmarshal(env.Params, &params))
			require.True(t, params.All)
			return models.SystemsResponse{
				Systems: []models.System{{ID: "SNES", ZapScript: "**launch.system:SNES"}},
			}, nil
		},
	}}}

	result := m.executeOperation(context.Background(), &operationEnvelope{
		OperationType: "systems", Params: json.RawMessage(`{"all":true}`),
	})
	assert.Equal(t, "succeeded", result.Status)
	assert.JSONEq(t, `{"systems":[{"id":"SNES","zap_script":"**launch.system:SNES"}]}`, string(result.Result))
	assert.Equal(t, "remote", sawRole)
}

// TestOperationTranslatesSearchParamsAndResult pins the casing contract end
// to end for a query verb: the API delivers snake_case params, the Core
// method receives camelCase params, and the method's camelCase response is
// reported to the API as snake_case.
func TestOperationTranslatesSearchParamsAndResult(t *testing.T) {
	var sawParams map[string]json.RawMessage
	m := &manager{deps: Deps{Methods: fakeMethodResolver{
		"media.search": func(env requests.RequestEnv) (any, error) {
			require.NoError(t, json.Unmarshal(env.Params, &sawParams))
			return models.SearchResults{Total: 1, Results: []models.SearchResultMedia{{
				Name: "Sonic", Path: "/roms/Genesis/Sonic.md", ZapScript: "**launch:/roms/Genesis/Sonic.md",
				HasCover: true, System: models.System{ID: "Genesis"},
			}}}, nil
		},
	}}}

	result := m.executeOperation(context.Background(), &operationEnvelope{
		OperationType: "media.search",
		Params:        json.RawMessage(`{"query":"sonic","max_results":5,"fuzzy_system":true}`),
	})
	require.Equal(t, "succeeded", result.Status, result.ErrorCode)
	assert.Contains(t, sawParams, "maxResults")
	assert.Contains(t, sawParams, "fuzzySystem")
	assert.NotContains(t, sawParams, "max_results")
	assert.JSONEq(t, `{"total":1,"results":[{
		"name":"Sonic","path":"/roms/Genesis/Sonic.md","zap_script":"**launch:/roms/Genesis/Sonic.md",
		"has_cover":true,"system":{"id":"Genesis"},"tags":null
	}]}`, string(result.Result))

	rejected := m.executeOperation(context.Background(), &operationEnvelope{
		OperationType: "media.search", Params: json.RawMessage(`{"query":"sonic","maxResults":5}`),
	})
	assert.Equal(t, "failed", rejected.Status)
	assert.Equal(t, "bad_params", rejected.ErrorCode, "camelCase params are not the wire contract")
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
