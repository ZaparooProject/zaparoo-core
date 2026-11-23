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

// Package fakehttp holds the plumbing the offline fakes share: fault
// injection, the listener lifecycle, the common control routes and the common
// CLI commands.
package fakehttp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/fakes/fakectl"
)

// maxControlBody caps a control API request body.
const maxControlBody = 1 << 20

// Fault is a failure applied to the fake service's requests until cleared.
//
//nolint:tagliatelle // The control API spells its fields in snake_case.
type Fault struct {
	// PathPrefix limits the fault to request paths starting with it; empty
	// applies it to every request.
	PathPrefix string `json:"path_prefix,omitempty"`
	// Status answers the request with this status instead of serving it.
	Status int `json:"status,omitempty"`
	// DelayMS holds the request this long before it is served or failed.
	DelayMS int `json:"delay_ms,omitempty"`
}

// Validate reports whether the fault does anything and is well formed.
func (f Fault) Validate() error {
	if f.Status == 0 && f.DelayMS == 0 {
		return errors.New("fault needs a status or a delay_ms")
	}
	if f.Status != 0 && (f.Status < 100 || f.Status > 599) {
		return fmt.Errorf("fault status %d is not an HTTP status", f.Status)
	}
	if f.DelayMS < 0 {
		return errors.New("fault delay_ms must not be negative")
	}
	return nil
}

// Faults holds the fault currently set on a fake, if any.
type Faults struct {
	current *Fault
	mu      syncutil.RWMutex
}

// Set replaces the current fault.
func (f *Faults) Set(fault Fault) error {
	if err := fault.Validate(); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.current = &fault
	return nil
}

// Clear removes the current fault.
func (f *Faults) Clear() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.current = nil
}

// Get returns the current fault, if one is set.
func (f *Faults) Get() (Fault, bool) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	if f.current == nil {
		return Fault{}, false
	}
	return *f.current, true
}

// Middleware applies the current fault to requests before next sees them.
func (f *Faults) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fault, ok := f.Get()
		if !ok || !strings.HasPrefix(r.URL.Path, fault.PathPrefix) {
			next.ServeHTTP(w, r)
			return
		}
		if fault.DelayMS > 0 {
			timer := time.NewTimer(time.Duration(fault.DelayMS) * time.Millisecond)
			select {
			case <-timer.C:
			case <-r.Context().Done():
				timer.Stop()
				return
			}
		}
		if fault.Status != 0 {
			http.Error(w, http.StatusText(fault.Status), fault.Status)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Handler serves the fault over a control API: GET shows it, POST sets it
// from a JSON Fault, DELETE clears it.
func (f *Faults) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			fault, ok := f.Get()
			if !ok {
				fakectl.WriteJSON(w, http.StatusOK, map[string]any{"fault": nil})
				return
			}
			fakectl.WriteJSON(w, http.StatusOK, map[string]any{"fault": fault})
		case http.MethodPost, http.MethodPut:
			var fault Fault
			if err := DecodeJSON(r, &fault); err != nil {
				WriteError(w, http.StatusBadRequest, err)
				return
			}
			if err := f.Set(fault); err != nil {
				WriteError(w, http.StatusBadRequest, err)
				return
			}
			fakectl.WriteJSON(w, http.StatusOK, map[string]any{"fault": fault})
		case http.MethodDelete:
			f.Clear()
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
}

// DecodeJSON reads a bounded JSON request body into v.
func DecodeJSON(r *http.Request, v any) error {
	data, err := io.ReadAll(io.LimitReader(r.Body, maxControlBody+1))
	if err != nil {
		return fmt.Errorf("read body: %w", err)
	}
	if len(data) > maxControlBody {
		return errors.New("body too large")
	}
	if len(data) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("decode body: %w", err)
	}
	return nil
}

// WriteError writes {"error": "..."} with the given status.
func WriteError(w http.ResponseWriter, status int, err error) {
	fakectl.WriteJSON(w, status, map[string]string{"error": err.Error()})
}

// Service wraps a fake's service handler with fault injection inside request
// logging, so faulted requests are logged with the status they got.
func Service(log *fakectl.RequestLog, faults *Faults, h http.Handler) http.Handler {
	return log.Middleware(faults.Middleware(h))
}

// ControlMux returns a mux with the routes every fake shares, "requests" and
// "fault", for the fake to add its own routes to. Routes are relative to the
// fake's mount point, which the sim strips before the request arrives.
func ControlMux(log *fakectl.RequestLog, faults *Faults) *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("/requests", log.RequestsHandler())
	mux.Handle("/fault", faults.Handler())
	return mux
}

// Listener runs a handler on a real socket.
type Listener struct {
	srv *http.Server
	url string
	mu  syncutil.Mutex
}

// Start listens on addr and serves h, returning the base URL.
func (l *Listener) Start(addr string, h http.Handler) (string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.srv != nil {
		return "", errors.New("fake server already started")
	}
	if addr == "" {
		addr = "127.0.0.1:0"
	}
	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", addr)
	if err != nil {
		return "", fmt.Errorf("listen on %s: %w", addr, err)
	}
	l.srv = &http.Server{Handler: h, ReadHeaderTimeout: 10 * time.Second}
	l.url = "http://" + ln.Addr().String()
	go func(srv *http.Server) { _ = srv.Serve(ln) }(l.srv)
	return l.url, nil
}

// URL is the base URL from Start, or "" before it.
func (l *Listener) URL() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.url
}

// Close stops the server. It is safe to call when it never started.
func (l *Listener) Close() error {
	l.mu.Lock()
	srv := l.srv
	l.srv = nil
	l.mu.Unlock()
	if srv == nil {
		return nil
	}
	if err := srv.Close(); err != nil {
		return fmt.Errorf("close fake server: %w", err)
	}
	return nil
}

// Command runs one fake-specific CLI command with the arguments after its
// name.
type Command func(ctx context.Context, args []string, call fakectl.Caller, stdout io.Writer) error

// CLI runs the commands every fake shares, then the fake's own:
//
//	requests                  list logged requests
//	requests clear            clear the log
//	fault                     show the current fault
//	fault set <status> [path-prefix]
//	fault delay <ms> [path-prefix]
//	fault clear
func CLI(
	ctx context.Context, name string, args []string, call fakectl.Caller, stdout io.Writer,
	commands map[string]Command,
) error {
	if len(args) == 0 {
		return fmt.Errorf("%s: no command given; try requests, fault%s", name, commandNames(commands))
	}
	switch args[0] {
	case "requests":
		return requestsCommand(ctx, args[1:], call, stdout)
	case "fault":
		return faultCommand(ctx, args[1:], call, stdout)
	}
	if cmd, ok := commands[args[0]]; ok {
		return cmd(ctx, args[1:], call, stdout)
	}
	return fmt.Errorf("%s: unknown command %q; try requests, fault%s", name, args[0], commandNames(commands))
}

func commandNames(commands map[string]Command) string {
	var b strings.Builder
	for name := range commands {
		b.WriteString(", " + name)
	}
	return b.String()
}

func requestsCommand(ctx context.Context, args []string, call fakectl.Caller, stdout io.Writer) error {
	switch {
	case len(args) == 0:
		return Print(ctx, call, stdout, http.MethodGet, "requests", nil)
	case len(args) == 1 && args[0] == "clear":
		return Print(ctx, call, stdout, http.MethodDelete, "requests", nil)
	default:
		return errors.New("usage: requests [clear]")
	}
}

func faultCommand(ctx context.Context, args []string, call fakectl.Caller, stdout io.Writer) error {
	if len(args) == 0 {
		return Print(ctx, call, stdout, http.MethodGet, "fault", nil)
	}
	if args[0] == "clear" && len(args) == 1 {
		return Print(ctx, call, stdout, http.MethodDelete, "fault", nil)
	}
	if (args[0] != "set" && args[0] != "delay") || len(args) < 2 || len(args) > 3 {
		return errors.New("usage: fault [set <status> [path-prefix] | delay <ms> [path-prefix] | clear]")
	}
	n, err := strconv.Atoi(args[1])
	if err != nil {
		return fmt.Errorf("fault %s: %q is not a number", args[0], args[1])
	}
	var fault Fault
	if args[0] == "set" {
		fault.Status = n
	} else {
		fault.DelayMS = n
	}
	if len(args) == 3 {
		fault.PathPrefix = args[2]
	}
	if invalid := fault.Validate(); invalid != nil {
		return invalid
	}
	body, err := json.Marshal(fault)
	if err != nil {
		return fmt.Errorf("encode fault: %w", err)
	}
	return Print(ctx, call, stdout, http.MethodPost, "fault", body)
}

// Print sends one control request and writes its JSON reply to stdout. An
// empty reply, such as a 204, prints {"ok":true}.
func Print(ctx context.Context, call fakectl.Caller, stdout io.Writer, method, path string, body []byte) error {
	out, err := call(ctx, method, path, body)
	if err != nil {
		return err
	}
	out = []byte(strings.TrimSpace(string(out)))
	if len(out) == 0 {
		out = []byte(`{"ok":true}`)
	}
	if _, err := fmt.Fprintf(stdout, "%s\n", out); err != nil {
		return fmt.Errorf("write output: %w", err)
	}
	return nil
}

// HandlerCaller is a fakectl.Caller that serves calls straight from a control
// handler, for tests and in-process use.
func HandlerCaller(h http.Handler) fakectl.Caller {
	return func(ctx context.Context, method, path string, body []byte) ([]byte, error) {
		var reader io.Reader = http.NoBody
		if body != nil {
			reader = bytes.NewReader(body)
		}
		req, err := http.NewRequestWithContext(ctx, method, "/"+strings.TrimLeft(path, "/"), reader)
		if err != nil {
			return nil, fmt.Errorf("build control request: %w", err)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code < 200 || rec.Code > 299 {
			return nil, fmt.Errorf("control %s %s: status %d: %s", method, path, rec.Code,
				strings.TrimSpace(rec.Body.String()))
		}
		return rec.Body.Bytes(), nil
	}
}
