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

package online

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/fakes/fakectl"
)

const (
	protocolVersion   = 1
	operationDeadline = 2 * time.Minute
)

// OperationKinds are the operation types Core's remote allowlist runs. The
// fake refuses to queue anything else, since Core would answer it with
// unknown_type.
var OperationKinds = []string{
	"echo", "stop", "systems", "launchers", "version", "media.search", "media.versions",
	"media.similar", "media.recommended", "media.browse", "launch", "launch.system", "mister.script",
}

//nolint:tagliatelle // Wire shape follows the Zaparoo Online device API.
type operationOrigin struct {
	Kind string `json:"kind"`
}

//nolint:tagliatelle // Wire shape follows the Zaparoo Online device API.
type operationEnvelope struct {
	DeadlineAt      time.Time       `json:"deadline_at"`
	Origin          operationOrigin `json:"origin"`
	CommandID       string          `json:"command_id"`
	OperationID     string          `json:"operation_id"`
	OperationType   string          `json:"operation_type"`
	Params          json.RawMessage `json:"params"`
	ProtocolVersion int             `json:"protocol_version"`
}

type waitEnvelope struct {
	Operation *operationEnvelope `json:"operation"`
	Type      string             `json:"type"`
}

// EnqueueOperation queues a remote operation for the next device that
// waits. Params may be nil for an operation that takes none.
func (s *Server) EnqueueOperation(kind string, params json.RawMessage) (Operation, error) {
	if !slices.Contains(OperationKinds, kind) {
		return Operation{}, fmt.Errorf("unknown operation kind %q", kind)
	}
	if len(params) == 0 {
		params = json.RawMessage("{}")
	}
	if !json.Valid(params) {
		return Operation{}, errors.New("operation params are not valid JSON")
	}
	s.mu.Lock()
	enqueued := now()
	op := &Operation{
		EnqueuedAt: enqueued, DeadlineAt: enqueued.Add(operationDeadline), Params: params,
		CommandID: s.nextID("cmd-"), OperationID: s.nextID("op-"), Kind: kind, Status: OpQueued,
	}
	s.state.Operations = append(s.state.Operations, op)
	s.notifyWaiters()
	out := *op
	s.mu.Unlock()
	return out, s.save()
}

// Operations returns every queued operation and its result, oldest first.
func (s *Server) Operations() []Operation {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Operation, 0, len(s.state.Operations))
	for _, op := range s.state.Operations {
		out = append(out, *op)
	}
	return out
}

// takeOperation hands the oldest queued operation to a device. Call with
// s.mu held.
func (s *Server) takeOperation(deviceID string) *Operation {
	for _, op := range s.state.Operations {
		if op.Status != OpQueued || (op.DeviceID != "" && op.DeviceID != deviceID) {
			continue
		}
		at := now()
		op.Status = OpDelivered
		op.DeviceID = deviceID
		op.DeliveredAt = &at
		return op
	}
	return nil
}

func (s *Server) operation(commandID, deviceID string) *Operation {
	for _, op := range s.state.Operations {
		if op.CommandID == commandID && op.DeviceID == deviceID {
			return op
		}
	}
	return nil
}

// handleWait holds the device's wait until an operation is queued for it,
// the timeout it asked for passes (capped by Options.MaxWait), or the fake
// closes. No work is a 204.
func (s *Server) handleWait(w http.ResponseWriter, r *http.Request, deviceID string) {
	hold := s.opts.MaxWait
	if seconds, err := strconv.Atoi(r.URL.Query().Get("timeout")); err == nil && seconds >= 0 {
		hold = min(time.Duration(seconds)*time.Second, s.opts.MaxWait)
	}
	timer := time.NewTimer(hold)
	defer timer.Stop()
	for {
		s.mu.Lock()
		op := s.takeOperation(deviceID)
		wake := s.wake
		s.mu.Unlock()
		if op != nil {
			_ = s.save()
			fakectl.WriteJSON(w, http.StatusOK, waitEnvelope{
				Type: "operation_target",
				Operation: &operationEnvelope{
					Params: op.Params, DeadlineAt: op.DeadlineAt, Origin: operationOrigin{Kind: "first_party"},
					CommandID: op.CommandID, OperationID: op.OperationID, OperationType: op.Kind,
					ProtocolVersion: protocolVersion,
				},
			})
			return
		}
		select {
		case <-wake:
		case <-timer.C:
			w.WriteHeader(http.StatusNoContent)
			return
		case <-s.closed:
			w.WriteHeader(http.StatusNoContent)
			return
		case <-r.Context().Done():
			return
		}
	}
}

//nolint:tagliatelle // Wire shape follows the Zaparoo Online device API.
type acceptedResponse struct {
	ExecutionExpiresAt time.Time `json:"execution_expires_at"`
}

func (s *Server) handleOpAccepted(w http.ResponseWriter, r *http.Request, deviceID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	op := s.operation(r.PathValue("id"), deviceID)
	if op == nil {
		writeError(w, http.StatusNotFound, "not_found", "no such operation for this device")
		return
	}
	if op.Status == OpDelivered {
		at := now()
		op.Status = OpAccepted
		op.AcceptedAt = &at
	}
	fakectl.WriteJSON(w, http.StatusOK, acceptedResponse{ExecutionExpiresAt: op.DeadlineAt})
}

//nolint:tagliatelle // Wire shape follows the Zaparoo Online device API.
type operationResult struct {
	Status    string          `json:"status"`
	ErrorCode string          `json:"error_code"`
	Result    json.RawMessage `json:"result"`
}

// handleOpResult records the result Core reports. A replayed result for an
// operation that already has one is accepted and leaves it as it was.
func (s *Server) handleOpResult(w http.ResponseWriter, r *http.Request, deviceID string) {
	var body operationResult
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	if body.Status != OpSucceeded && body.Status != OpFailed && body.Status != OpBusy {
		writeErrorFields(w, http.StatusBadRequest, "validation_error", "unknown result status",
			map[string]string{"status": "must be succeeded, failed or busy"})
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	op := s.operation(r.PathValue("id"), deviceID)
	if op == nil {
		writeError(w, http.StatusNotFound, "not_found", "no such operation for this device")
		return
	}
	if op.CompletedAt == nil {
		at := now()
		op.Status = body.Status
		op.Result = body.Result
		op.ErrorCode = body.ErrorCode
		op.CompletedAt = &at
	}
	w.WriteHeader(http.StatusNoContent)
}
