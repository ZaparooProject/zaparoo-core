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

// Package fakectl holds what every offline fake server shares: the request
// log the sim reports for assertions, and the call signature its CLI uses to
// reach a fake's control API through the sim control socket.
package fakectl

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
)

// Caller sends one request to a fake's control API. Path is relative to the
// fake's mount on the control socket, such as "requests" or "fault". Body is
// JSON or nil. It returns the response body, or an error for a non-2xx reply.
type Caller func(ctx context.Context, method, path string, body []byte) ([]byte, error)

// Request is one request a fake served.
type Request struct {
	Time   time.Time `json:"time"`
	Method string    `json:"method"`
	Path   string    `json:"path"`
	Query  string    `json:"query,omitempty"`
	Status int       `json:"status"`
}

// RequestLog records the requests a fake serves, newest last, keeping at most
// a fixed number.
type RequestLog struct {
	items []Request
	mu    syncutil.Mutex
	limit int
}

// NewRequestLog returns a log that keeps the last limit requests.
func NewRequestLog(limit int) *RequestLog {
	if limit <= 0 {
		limit = 1000
	}
	return &RequestLog{limit: limit}
}

// Add records a request.
func (l *RequestLog) Add(r *Request) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.items = append(l.items, *r)
	if over := len(l.items) - l.limit; over > 0 {
		l.items = append([]Request(nil), l.items[over:]...)
	}
}

// List returns a copy of the recorded requests.
func (l *RequestLog) List() []Request {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]Request(nil), l.items...)
}

// Clear drops every recorded request.
func (l *RequestLog) Clear() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.items = nil
}

// Middleware wraps next so each request it serves is recorded in l.
func (l *RequestLog) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		l.Add(&Request{
			Time:   time.Now().UTC(),
			Method: r.Method,
			Path:   r.URL.Path,
			Query:  r.URL.RawQuery,
			Status: rec.status,
		})
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// WriteJSON writes v as a JSON response with the given status.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// RequestsHandler serves the log over a control API: GET lists, DELETE clears.
func (l *RequestLog) RequestsHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			WriteJSON(w, http.StatusOK, l.List())
		case http.MethodDelete:
			l.Clear()
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
}
