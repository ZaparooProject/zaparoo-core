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
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/fakes/fakectl"
)

const maxPlaySessions = 500

type watermarkResponse struct {
	Watermark *time.Time `json:"watermark"`
}

type playUploadResponse struct {
	Watermark *time.Time `json:"watermark"`
	Accepted  int64      `json:"accepted"`
}

// watermark is the newest core_updated_at the device has synced, or nil.
// Call with s.mu held.
func (s *Server) watermark(deviceID string) *time.Time {
	var newest *time.Time
	for _, session := range s.state.PlaySessions[deviceID] {
		if newest == nil || session.CoreUpdatedAt.After(*newest) {
			at := session.CoreUpdatedAt
			newest = &at
		}
	}
	return newest
}

func (s *Server) handlePlayWatermark(w http.ResponseWriter, _ *http.Request, deviceID string) {
	s.mu.Lock()
	resp := watermarkResponse{Watermark: s.watermark(deviceID)}
	s.mu.Unlock()
	fakectl.WriteJSON(w, http.StatusOK, resp)
}

// handlePlayUpload upserts sessions by UUID. A session missing its UUID or
// start, or with negative play time, refuses the batch with a field naming
// its position, the way Core isolates refused rows.
func (s *Server) handlePlayUpload(w http.ResponseWriter, r *http.Request, deviceID string) {
	var body struct {
		Sessions []PlaySession `json:"sessions"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	if len(body.Sessions) > maxPlaySessions {
		writeErrorFields(w, http.StatusBadRequest, "validation_error", "too many sessions",
			map[string]string{"Sessions": "at most 500"})
		return
	}
	fields := make(map[string]string)
	for i := range body.Sessions {
		session := &body.Sessions[i]
		switch {
		case session.SessionUUID == "":
			fields[fmt.Sprintf("Sessions[%d].SessionUUID", i)] = "required"
		case session.StartedAt.IsZero():
			fields[fmt.Sprintf("Sessions[%d].StartedAt", i)] = "required"
		case session.PlayTimeSecs < 0:
			fields[fmt.Sprintf("Sessions[%d].PlayTimeSecs", i)] = "must not be negative"
		}
	}
	if len(fields) > 0 {
		writeErrorFields(w, http.StatusBadRequest, "validation_error", "invalid sessions", fields)
		return
	}
	s.mu.Lock()
	sessions := s.state.PlaySessions[deviceID]
	if sessions == nil {
		sessions = make(map[string]*PlaySession)
		s.state.PlaySessions[deviceID] = sessions
	}
	for i := range body.Sessions {
		session := body.Sessions[i]
		sessions[session.SessionUUID] = &session
	}
	resp := playUploadResponse{Watermark: s.watermark(deviceID), Accepted: int64(len(body.Sessions))}
	s.mu.Unlock()
	fakectl.WriteJSON(w, http.StatusOK, resp)
}

// PlaySessions returns the sessions a device synced, oldest start first.
func (s *Server) PlaySessions(deviceID string) []PlaySession {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]PlaySession, 0, len(s.state.PlaySessions[deviceID]))
	for _, session := range s.state.PlaySessions[deviceID] {
		out = append(out, *session)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.Before(out[j].StartedAt) })
	return out
}
