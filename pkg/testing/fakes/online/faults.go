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
	"bytes"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"
)

// Fault kinds. A status kind answers matching requests with that status and
// an error body Core decodes (429 and 503 also carry Retry-After: 1).
// "timeout" holds the request until the client gives up, and "slow" holds it
// for Options.SlowDelay before serving it as normal.
const (
	FaultUnauthorized = "401"
	FaultForbidden    = "403"
	FaultRateLimited  = "429"
	FaultServerError  = "500"
	FaultUnavailable  = "503"
	FaultTimeout      = "timeout"
	FaultSlow         = "slow"
)

// FaultKinds lists every fault kind SetFault takes.
var FaultKinds = []string{
	FaultUnauthorized, FaultForbidden, FaultRateLimited, FaultServerError,
	FaultUnavailable, FaultTimeout, FaultSlow,
}

// Fault is a failure applied to device API requests whose path starts with
// PathPrefix until it is cleared.
type Fault struct {
	PathPrefix string `json:"pathPrefix"`
	Kind       string `json:"kind"`
}

// SetFault applies a fault, replacing any fault on the same prefix. When
// several prefixes match a request, the longest wins.
func (s *Server) SetFault(f Fault) error {
	if !strings.HasPrefix(f.PathPrefix, "/") {
		return fmt.Errorf("fault path prefix %q must start with /", f.PathPrefix)
	}
	if !slices.Contains(FaultKinds, f.Kind) {
		return fmt.Errorf("unknown fault kind %q, want one of %s", f.Kind, strings.Join(FaultKinds, ", "))
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.faults = slices.DeleteFunc(s.faults, func(existing Fault) bool {
		return existing.PathPrefix == f.PathPrefix
	})
	s.faults = append(s.faults, f)
	return nil
}

// ClearFault removes the fault on prefix, if any.
func (s *Server) ClearFault(prefix string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.faults = slices.DeleteFunc(s.faults, func(existing Fault) bool {
		return existing.PathPrefix == prefix
	})
}

// ClearFaults removes every fault.
func (s *Server) ClearFaults() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.faults = nil
}

// Faults returns the faults set, in the order they were set.
func (s *Server) Faults() []Fault {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Fault{}, s.faults...)
}

func (s *Server) matchFault(path string) (Fault, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var best Fault
	found := false
	for _, f := range s.faults {
		if strings.HasPrefix(path, f.PathPrefix) && (!found || len(f.PathPrefix) > len(best.PathPrefix)) {
			best, found = f, true
		}
	}
	return best, found
}

func (s *Server) faultMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fault, ok := s.matchFault(r.URL.Path)
		if !ok {
			next.ServeHTTP(w, r)
			return
		}
		if fault.Kind == FaultTimeout || fault.Kind == FaultSlow {
			// The server only notices a client hanging up once the body is
			// read, so read it before holding the request.
			body, err := io.ReadAll(io.LimitReader(r.Body, maxPackBody))
			if err != nil {
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(body))
		}
		switch fault.Kind {
		case FaultTimeout:
			select {
			case <-r.Context().Done():
			case <-s.closed:
			}
			return
		case FaultSlow:
			timer := time.NewTimer(s.opts.SlowDelay)
			defer timer.Stop()
			select {
			case <-timer.C:
				next.ServeHTTP(w, r)
			case <-r.Context().Done():
			case <-s.closed:
			}
			return
		case FaultUnauthorized:
			writeError(w, http.StatusUnauthorized, "unauthorized", "fault: unauthorized")
		case FaultForbidden:
			writeError(w, http.StatusForbidden, "forbidden", "fault: forbidden")
		case FaultRateLimited:
			w.Header().Set("Retry-After", "1")
			writeError(w, http.StatusTooManyRequests, "rate_limited", "fault: rate limited")
		case FaultServerError:
			writeError(w, http.StatusInternalServerError, "internal_error", "fault: internal error")
		case FaultUnavailable:
			w.Header().Set("Retry-After", "1")
			writeError(w, http.StatusServiceUnavailable, "unavailable", "fault: unavailable")
		default:
			next.ServeHTTP(w, r)
		}
	})
}
