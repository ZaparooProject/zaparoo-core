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
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/rs/zerolog/log"
)

//nolint:govet,tagliatelle // Wire shape follows remote Online API contract.
type operationOrigin struct {
	Kind    string `json:"kind"`
	KeyName string `json:"key_name,omitempty"`
	KeyID   *int64 `json:"key_id,omitempty"`
}

//nolint:govet,tagliatelle // Wire shape follows remote Online API contract.
type operationEnvelope struct {
	Params          json.RawMessage `json:"params"`
	DeadlineAt      time.Time       `json:"deadline_at"`
	Origin          operationOrigin `json:"origin"`
	CommandID       string          `json:"command_id"`
	OperationID     string          `json:"operation_id"`
	OperationType   string          `json:"operation_type"`
	ProtocolVersion int             `json:"protocol_version"`
}

type waitEnvelope struct {
	Operation *operationEnvelope `json:"operation,omitempty"`
	Type      string             `json:"type"`
}

//nolint:tagliatelle // Wire shape follows remote Online API contract.
type acceptedResponse struct {
	ExecutionExpiresAt time.Time `json:"execution_expires_at"`
}

//nolint:govet,tagliatelle // Wire shape follows remote Online API contract.
type operationResult struct {
	Result    json.RawMessage `json:"result,omitempty"`
	Status    string          `json:"status"`
	ErrorCode string          `json:"error_code,omitempty"`
}

func (m *manager) handleOperation(
	ctx context.Context, operation *operationEnvelope, busy bool,
) {
	if err := validateEnvelope(operation); err != nil {
		log.Warn().Err(err).Str("command_id", operation.CommandID).Msg("invalid remote operation envelope")
		return
	}

	params := operation.Params
	if len(params) == 0 {
		params = json.RawMessage(`{}`)
	}
	digest := sha256.Sum256(params)
	origin, err := json.Marshal(operation.Origin)
	if err != nil {
		log.Error().Err(err).Str("command_id", operation.CommandID).Msg("encode remote operation origin")
		return
	}
	candidate := &database.RemoteCommand{
		CommandID:       operation.CommandID,
		OperationID:     operation.OperationID,
		OperationType:   operation.OperationType,
		ProtocolVersion: operation.ProtocolVersion,
		ParamsDigest:    hex.EncodeToString(digest[:]),
		Origin:          origin,
		DeadlineAt:      operation.DeadlineAt,
		State:           "recorded",
	}
	stored, fresh, err := m.deps.DB.UserDB.ClaimRemoteCommand(candidate)
	if err != nil {
		log.Error().Err(err).Str("command_id", operation.CommandID).Msg("claim remote command")
		return
	}
	if stored.ParamsDigest != candidate.ParamsDigest || stored.OperationID != candidate.OperationID ||
		stored.OperationType != candidate.OperationType || stored.ProtocolVersion != candidate.ProtocolVersion ||
		!bytes.Equal(stored.Origin, candidate.Origin) {
		log.Error().Str("command_id", operation.CommandID).
			Msg("remote command ID reused with conflicting payload")
		return
	}
	if !fresh {
		switch stored.State {
		case "terminal":
			m.postStoredResult(ctx, stored)
			return
		case "executing", "void", "expired":
			return
		case "accepted":
			// run() dispatches sequentially, but a redelivery of this exact
			// command ID can still land here synchronously (the busy path)
			// while a separate goroutine is concurrently mid-flight between
			// its own recorded->accepted and accepted->executing transitions
			// for the same command, holding the sole execution slot. Only a
			// redelivery (!fresh) can observe "accepted" here; a fresh claim
			// always starts at "recorded". Finalizing it "busy" here would
			// race that in-flight transition and finalize the command
			// terminal out from under it, so defer entirely to whichever
			// call owns the live transition instead. A busy=false redelivery
			// (e.g. after a crash/restart, with nothing else holding the
			// slot) is the resume case already handled below and must fall
			// through.
			if busy {
				return
			}
		}
	}
	if !time.Now().Before(operation.DeadlineAt) {
		_, _ = m.deps.DB.UserDB.TransitionRemoteCommand(operation.CommandID, stored.State, "expired", nil)
		return
	}

	if stored.State == "recorded" {
		accepted, acceptErr := m.postAccepted(ctx, operation.CommandID)
		if acceptErr != nil {
			m.handleTransitionHTTPError(operation.CommandID, "recorded", acceptErr)
			return
		}
		if !accepted.ExecutionExpiresAt.After(time.Now()) {
			_, _ = m.deps.DB.UserDB.TransitionRemoteCommand(operation.CommandID, "recorded", "expired", nil)
			return
		}
		changed, transitionErr := m.deps.DB.UserDB.TransitionRemoteCommand(
			operation.CommandID, "recorded", "accepted", &accepted.ExecutionExpiresAt)
		if transitionErr != nil || !changed {
			log.Error().Err(transitionErr).Str("command_id", operation.CommandID).
				Msg("persist remote command acceptance")
			return
		}
		stored.State = "accepted"
		stored.ExecutionExpiresAt = &accepted.ExecutionExpiresAt
	}

	// A crash or restart between the accepted->executing transition and the
	// server receiving a result causes the same command ID to be redelivered
	// while still in "accepted" state. stored.ExecutionExpiresAt then reflects
	// the execution lease granted on the original acceptance, which may have
	// since lapsed; without this check that stale lease reaches executionCtx
	// below already cancelled, and the local launch/script handlers don't
	// check ctx before acting, so a stale command could still run.
	if stored.ExecutionExpiresAt != nil && !stored.ExecutionExpiresAt.After(time.Now()) {
		_, _ = m.deps.DB.UserDB.TransitionRemoteCommand(operation.CommandID, "accepted", "expired", nil)
		return
	}

	if busy {
		m.finishOperation(ctx, operation.CommandID, "accepted", operationResult{Status: "busy"})
		return
	}
	changed, err := m.deps.DB.UserDB.TransitionRemoteCommand(
		operation.CommandID, "accepted", "executing", stored.ExecutionExpiresAt)
	if err != nil || !changed {
		log.Error().Err(err).Str("command_id", operation.CommandID).Msg("start remote command execution")
		return
	}
	executionDeadline := operation.DeadlineAt
	if stored.ExecutionExpiresAt != nil && stored.ExecutionExpiresAt.Before(executionDeadline) {
		executionDeadline = *stored.ExecutionExpiresAt
	}
	executionCtx, cancel := context.WithDeadline(ctx, executionDeadline)
	defer cancel()
	execute := m.executeOperation
	if m.execute != nil {
		execute = m.execute
	}
	result := execute(executionCtx, operation)
	m.finishOperation(ctx, operation.CommandID, "executing", result)
}

func validateEnvelope(operation *operationEnvelope) error {
	if operation.CommandID == "" || operation.OperationID == "" || operation.OperationType == "" {
		return errors.New("missing remote operation identifier or type")
	}
	if strings.ContainsAny(operation.CommandID, "/\\") {
		return errors.New("invalid remote command identifier")
	}
	if operation.ProtocolVersion != protocolVersion {
		return fmt.Errorf("unsupported protocol version %d", operation.ProtocolVersion)
	}
	if operation.DeadlineAt.IsZero() {
		return errors.New("missing remote operation deadline")
	}
	if operation.Origin.Kind != "first_party" && operation.Origin.Kind != "api_key" {
		return errors.New("invalid remote operation origin")
	}
	if operation.Origin.Kind == "api_key" && operation.Origin.KeyName == "" {
		return errors.New("API-key origin missing key name")
	}
	return nil
}

func (m *manager) postAccepted(
	ctx context.Context, commandID string,
) (acceptedResponse, error) {
	var accepted acceptedResponse
	err := m.doJSON(
		ctx, http.MethodPost,
		"/v1/device/operations/"+url.PathEscape(commandID)+"/accepted", nil, &accepted,
	)
	return accepted, err
}

// resultPersistRetries and resultPersistRetryDelay bound a retry of the
// initial StoreRemoteCommandResult write in finishOperation. The userdb
// connection already carries a 5s SQLite busy_timeout, so ordinary lock
// contention is absorbed before it ever surfaces here as an error; a
// returned error is more likely a short-lived spike beyond that timeout or a
// momentary I/O condition. This is the operation's only chance to persist
// its result: once finishOperation returns, the in-memory result is gone
// and nothing else can retry it (a row stuck outside "terminal" is invisible
// to replayStoredResults, and a same-command redelivery just no-ops on
// State == "executing"), so a few bounded attempts are worth it. Vars, not
// consts, so tests can shrink the delay.
var (
	resultPersistRetries    = 3
	resultPersistRetryDelay = 200 * time.Millisecond
)

func (m *manager) finishOperation(
	ctx context.Context, commandID, fromState string, result operationResult,
) {
	m.resultMu.Lock()
	defer m.resultMu.Unlock()

	changed, err := m.storeRemoteCommandResultWithRetry(commandID, fromState, result)
	if err != nil || !changed {
		log.Error().Err(err).Str("command_id", commandID).Msg("persist remote command result")
		return
	}
	if err := m.postResult(ctx, commandID, &result); err != nil {
		m.handleResultPostError(commandID, err)
		return
	}
	if err := m.deps.DB.UserDB.MarkRemoteCommandResultReported(commandID); err != nil {
		log.Error().Err(err).Str("command_id", commandID).Msg("mark remote command result reported")
	}
}

// storeRemoteCommandResultWithRetry retries only on a write error. A clean
// changed=false (no error) means the row already moved to some other
// terminal state via a concurrent transition, which correctly supersedes
// this result; retrying that case would never match and just wastes time.
func (m *manager) storeRemoteCommandResultWithRetry(
	commandID, fromState string, result operationResult,
) (changed bool, err error) {
	for attempt := range resultPersistRetries {
		if attempt > 0 {
			time.Sleep(resultPersistRetryDelay)
		}
		changed, err = m.deps.DB.UserDB.StoreRemoteCommandResult(
			commandID, fromState, result.Status, result.Result, result.ErrorCode)
		if err == nil {
			return changed, nil
		}
	}
	return changed, fmt.Errorf("persist remote command result after %d attempts: %w", resultPersistRetries, err)
}

func (m *manager) postStoredResult(ctx context.Context, command *database.RemoteCommand) {
	m.resultMu.Lock()
	defer m.resultMu.Unlock()
	m.postStoredResultLocked(ctx, command)
}

func (m *manager) postStoredResultLocked(ctx context.Context, command *database.RemoteCommand) {
	result := operationResult{
		Status: command.ResultStatus, Result: command.Result, ErrorCode: command.ErrorCode,
	}
	if err := m.postResult(ctx, command.CommandID, &result); err != nil {
		m.handleResultPostError(command.CommandID, err)
		return
	}
	if err := m.deps.DB.UserDB.MarkRemoteCommandResultReported(command.CommandID); err != nil {
		log.Error().Err(err).Str("command_id", command.CommandID).Msg("mark replayed remote result reported")
	}
}

// replayStoredResults reports up to resultReplayBatchLimit unreported
// results per call, one HTTP post at a time. A live operation's
// finishOperation needs resultMu too, so each post here acquires and
// releases it individually (via postStoredResult) rather than holding it for
// the whole batch: holding a single lock across an unbounded, possibly
// unresponsive sequence of network posts would block every concurrently
// executing operation from recording its own result for as long as the
// backlog took to drain. The batch cap bounds how much of that backlog a
// single call can walk; run() calls this again on its next replay interval,
// so a larger backlog just drains over more cycles instead of one long one.
func (m *manager) replayStoredResults(ctx context.Context) {
	commands, err := m.deps.DB.UserDB.ListUnreportedRemoteCommands(resultReplayBatchLimit)
	if err != nil {
		log.Warn().Err(err).Msg("load unreported remote command results")
		return
	}
	for i := range commands {
		if ctx.Err() != nil {
			return
		}
		m.postStoredResult(ctx, &commands[i])
	}
}

func (m *manager) handleTransitionHTTPError(commandID, fromState string, err error) {
	var httpErr *httpError
	if !errors.As(err, &httpErr) {
		log.Warn().Err(err).Str("command_id", commandID).Msg("remote command transition failed")
		return
	}
	switch httpErr.status {
	case http.StatusNotFound:
		_, _ = m.deps.DB.UserDB.TransitionRemoteCommand(commandID, fromState, "void", nil)
	case http.StatusGone:
		_, _ = m.deps.DB.UserDB.TransitionRemoteCommand(commandID, fromState, "expired", nil)
	case http.StatusConflict:
		log.Error().Str("command_id", commandID).Msg("remote command transition conflict")
	case http.StatusUnauthorized:
		log.Warn().Str("command_id", commandID).Msg("remote command credential rejected")
	default:
		log.Warn().Err(err).Str("command_id", commandID).Msg("remote command transition rejected")
	}
}

func (m *manager) handleResultPostError(commandID string, err error) {
	var httpErr *httpError
	if errors.As(err, &httpErr) && (httpErr.status == http.StatusNotFound || httpErr.status == http.StatusGone) {
		if markErr := m.deps.DB.UserDB.MarkRemoteCommandResultReported(commandID); markErr != nil {
			log.Warn().Err(markErr).Str("command_id", commandID).Msg("close expired remote result")
		}
		return
	}
	log.Warn().Err(err).Str("command_id", commandID).Msg("post remote command result")
}

func (m *manager) postResult(
	ctx context.Context, commandID string, result *operationResult,
) error {
	return m.doJSON(ctx, http.MethodPost, "/v1/device/operations/"+url.PathEscape(commandID)+"/result", result, nil)
}

// executeOperation dispatches an operation against operationAllowlist —
// deny-by-default: an operation_type with no entry is refused before
// anything else runs. See allowlist.go for the table, wire.go for the
// params translation every entry goes through first, and dispatch.go for
// runMethod, which handles every method-backed entry generically.
func (m *manager) executeOperation(
	ctx context.Context, operation *operationEnvelope,
) operationResult {
	spec, ok := operationAllowlist[operation.OperationType]
	if !ok {
		return failResult("unknown_type")
	}
	params, err := spec.translate(operation.Params)
	if err != nil {
		return failResult("bad_params")
	}
	if spec.local != nil {
		return spec.local(m, ctx, operation.OperationType, params)
	}
	return m.runMethod(ctx, spec, params)
}

// executeEcho is the "echo" operation's local handler: a fixed round-trip
// with no side effects, used to verify the remote operations path end to
// end without touching device state.
func (*manager) executeEcho(_ context.Context, _ string, raw json.RawMessage) operationResult {
	var params wireEchoParams
	if err := decodeParams(raw, &params); err != nil {
		return failResult("bad_params")
	}
	return succeedResult(map[string]string{"message": params.Message}, resultLimit)
}

func decodeParams(raw json.RawMessage, out any) error {
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return fmt.Errorf("decode remote operation params: %w", err)
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return errors.New("trailing params JSON")
	}
	return nil
}

func requireEmptyParams(raw json.RawMessage) error {
	var params map[string]json.RawMessage
	if err := decodeParams(raw, &params); err != nil {
		return err
	}
	if len(params) != 0 {
		return errors.New("operation takes no params")
	}
	return nil
}

func succeedResult(value any, limit int) operationResult {
	converted, err := json.Marshal(value)
	if err != nil || len(converted) > limit {
		return failResult("result_too_large")
	}
	return operationResult{Status: "succeeded", Result: converted}
}

func failResult(code string) operationResult {
	return operationResult{Status: "failed", ErrorCode: code}
}
