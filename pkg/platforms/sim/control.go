//go:build linux && !android

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

package sim

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	simreader "github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/sim"
	"github.com/rs/zerolog/log"
)

// maxSocketPath is the longest Unix socket path Linux accepts.
const maxSocketPath = 107

// TapRequest presents a tag. HoldMS removes it after that long; zero leaves
// it on the reader until a remove.
type TapRequest struct {
	simreader.Tag
	HoldMS int `json:"holdMs,omitempty"`
}

// AddReaderRequest creates a sim reader device.
type AddReaderRequest struct {
	Name         string `json:"name"`
	WriteWaitMS  int    `json:"writeWaitMs,omitempty"`
	NotRemovable bool   `json:"notRemovable,omitempty"`
	NoWrite      bool   `json:"noWrite,omitempty"`
	NoAutoDetect bool   `json:"noAutoDetect,omitempty"`
}

// ScanErrorRequest sends a scan carrying an error.
type ScanErrorRequest struct {
	Message string `json:"message"`
}

// WriteCompleteRequest presents a tag to a waiting write.
type WriteCompleteRequest struct {
	UID string `json:"uid,omitempty"`
}

// WriteFailRequest injects a failure into the next write.
type WriteFailRequest struct {
	Mode simreader.WriteFailure `json:"mode"`
}

// Status is the sim's state as the control socket reports it.
type Status struct {
	ActiveMedia *models.ActiveMedia      `json:"activeMedia,omitempty"`
	Children    map[string]string        `json:"children"`
	Root        string                   `json:"root"`
	Readers     []simreader.DeviceStatus `json:"readers"`
	Faults      []Fault                  `json:"faults"`
	JournalSeq  int64                    `json:"journalSeq"`
	PID         int                      `json:"pid"`
	TrackedPID  int                      `json:"trackedPid,omitempty"`
}

// ErrorResponse is the body of every failed control request.
type ErrorResponse struct {
	Error string `json:"error"`
}

// LaunchKinds are the journal kinds the launches view shows.
var LaunchKinds = []string{
	EventLaunchRequest, EventLaunch, EventLaunchError, EventProcess, EventProcessExit,
	EventTracked, EventStop, EventStopError, EventKill, EventExternal, EventFinished,
}

type controlServer struct {
	srv  *http.Server
	ln   net.Listener
	path string
}

func startControl(p *Platform, path string) (*controlServer, error) {
	if len(path) > maxSocketPath {
		return nil, fmt.Errorf("sim: socket path is %d bytes, over the %d Linux allows: use a shorter --root",
			len(path), maxSocketPath)
	}
	if err := removeStaleSocket(path); err != nil {
		return nil, err
	}
	var lc net.ListenConfig
	ln, err := lc.Listen(context.Background(), "unix", path)
	if err != nil {
		return nil, fmt.Errorf("sim: listen on control socket: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = ln.Close()
		return nil, fmt.Errorf("sim: restrict control socket: %w", err)
	}
	s := &controlServer{
		ln:   ln,
		path: path,
		srv:  &http.Server{Handler: p.ControlHandler(), ReadHeaderTimeout: 5 * time.Second},
	}
	go func() {
		if err := s.srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error().Err(err).Msg("sim control socket stopped")
		}
	}()
	return s, nil
}

// removeStaleSocket removes a socket file no process is listening on.
func removeStaleSocket(path string) error {
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	dialer := net.Dialer{Timeout: 500 * time.Millisecond}
	conn, err := dialer.DialContext(context.Background(), "unix", path)
	if err == nil {
		_ = conn.Close()
		return fmt.Errorf("sim: another sim is already serving %s", path)
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("sim: remove stale socket: %w", err)
	}
	return nil
}

func (s *controlServer) close() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = s.srv.Shutdown(ctx)
	_ = os.Remove(s.path)
}

// ControlHandler returns the control API. The sim serves it on its socket;
// tests may mount it anywhere.
func (p *Platform) ControlHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /status", p.handleStatus)
	mux.HandleFunc("GET /readers", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, p.hub.List())
	})
	mux.HandleFunc("POST /readers", p.handleAddReader)
	mux.HandleFunc("DELETE /readers/{name}", func(w http.ResponseWriter, r *http.Request) {
		respond(w, nil, p.hub.Remove(r.PathValue("name")))
	})
	mux.HandleFunc("POST /readers/{name}/tap", p.handleTap)
	p.deviceRoute(mux, "remove", func(d *simreader.Device, _ *http.Request) (any, error) {
		return nil, d.RemoveTag()
	})
	p.deviceRoute(mux, "error", func(d *simreader.Device, _ *http.Request) (any, error) {
		return nil, d.ReaderError()
	})
	p.deviceRoute(mux, "scan-error", func(d *simreader.Device, r *http.Request) (any, error) {
		var req ScanErrorRequest
		if err := decode(r, &req); err != nil {
			return nil, err
		}
		return nil, d.ScanError(req.Message)
	})
	p.deviceRoute(mux, "unplug", func(d *simreader.Device, _ *http.Request) (any, error) {
		d.Unplug()
		return okBody, nil
	})
	p.deviceRoute(mux, "plug", func(d *simreader.Device, _ *http.Request) (any, error) {
		d.Plug()
		return okBody, nil
	})
	p.deviceRoute(mux, "write-complete", func(d *simreader.Device, r *http.Request) (any, error) {
		var req WriteCompleteRequest
		if err := decode(r, &req); err != nil {
			return nil, err
		}
		return d.CompleteWrite(req.UID)
	})
	p.deviceRoute(mux, "write-fail", func(d *simreader.Device, r *http.Request) (any, error) {
		var req WriteFailRequest
		if err := decode(r, &req); err != nil {
			return nil, err
		}
		d.FailNextWrite(req.Mode)
		return okBody, nil
	})
	mux.HandleFunc("GET /journal", func(w http.ResponseWriter, r *http.Request) {
		p.handleJournal(w, r, nil)
	})
	mux.HandleFunc("GET /launches", func(w http.ResponseWriter, r *http.Request) {
		p.handleJournal(w, r, LaunchKinds)
	})
	mux.HandleFunc("GET /faults", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, p.faults.List())
	})
	mux.HandleFunc("POST /faults", func(w http.ResponseWriter, r *http.Request) {
		var f Fault
		if err := decode(r, &f); err != nil {
			respond(w, nil, err)
			return
		}
		p.faults.Set(f)
		p.journal.Record(&Event{Kind: EventFault, Launcher: f.Launcher, Detail: "set"})
		writeJSON(w, http.StatusOK, p.faults.List())
	})
	mux.HandleFunc("DELETE /faults", func(w http.ResponseWriter, r *http.Request) {
		p.faults.Clear(r.URL.Query().Get("launcher"))
		writeJSON(w, http.StatusOK, p.faults.List())
	})
	mux.HandleFunc("POST /stop", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]bool{"stopping": true})
		if p.opts.OnStop != nil {
			go p.opts.OnStop()
		}
	})
	for prefix, h := range p.opts.Mounts {
		prefix = "/" + strings.Trim(prefix, "/")
		mux.Handle(prefix+"/", http.StripPrefix(prefix, h))
	}
	return mux
}

func (p *Platform) deviceRoute(
	mux *http.ServeMux,
	action string,
	fn func(*simreader.Device, *http.Request) (any, error),
) {
	mux.HandleFunc("POST /readers/{name}/"+action, func(w http.ResponseWriter, r *http.Request) {
		d, err := p.hub.Device(r.PathValue("name"))
		if err != nil {
			respond(w, nil, err)
			return
		}
		out, err := fn(d, r)
		respond(w, out, err)
	})
}

// StatusNow returns the sim's current state.
func (p *Platform) StatusNow() Status {
	p.mu.Lock()
	tracked := p.trackedPID
	p.mu.Unlock()
	children := map[string]string{}
	for pid, l := range p.Children() {
		children[strconv.Itoa(pid)] = l
	}
	return Status{
		Root:        p.layout.Root,
		PID:         os.Getpid(),
		ActiveMedia: p.ActiveMedia(),
		TrackedPID:  tracked,
		Children:    children,
		Readers:     p.hub.List(),
		Faults:      p.faults.List(),
		JournalSeq:  p.journal.Last(),
	}
}

func (p *Platform) handleStatus(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, p.StatusNow())
}

func (p *Platform) handleAddReader(w http.ResponseWriter, r *http.Request) {
	var req AddReaderRequest
	if err := decode(r, &req); err != nil {
		respond(w, nil, err)
		return
	}
	d, err := p.hub.Add(req.Name, simreader.DeviceOptions{
		WriteWait:    ms(req.WriteWaitMS),
		NotRemovable: req.NotRemovable,
		NoWrite:      req.NoWrite,
		NoAutoDetect: req.NoAutoDetect,
	})
	if err != nil {
		respond(w, nil, err)
		return
	}
	writeJSON(w, http.StatusOK, d.Status())
}

func (p *Platform) handleTap(w http.ResponseWriter, r *http.Request) {
	d, err := p.hub.Device(r.PathValue("name"))
	if err != nil {
		respond(w, nil, err)
		return
	}
	var req TapRequest
	if err = decode(r, &req); err != nil {
		respond(w, nil, err)
		return
	}
	tag, err := d.Tap(req.Tag, ms(req.HoldMS))
	respond(w, tag, err)
}

// handleJournal lists events after ?since=, optionally of ?kind=a,b. With
// ?follow=1 it streams JSON lines until the client leaves or ?timeout_ms=
// passes.
func (p *Platform) handleJournal(w http.ResponseWriter, r *http.Request, kinds []string) {
	q := r.URL.Query()
	since, _ := strconv.ParseInt(q.Get("since"), 10, 64)
	if k := q.Get("kind"); k != "" {
		kinds = strings.Split(k, ",")
	}
	if q.Get("follow") == "" {
		writeJSON(w, http.StatusOK, p.journal.Since(since, kinds...))
		return
	}
	want := map[string]bool{}
	for _, k := range kinds {
		want[k] = true
	}
	ctx := r.Context()
	if t, err := strconv.Atoi(q.Get("timeout_ms")); err == nil && t > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, ms(t))
		defer cancel()
	}
	ch, unsubscribe := p.journal.Subscribe()
	defer unsubscribe()
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.WriteHeader(http.StatusOK)
	flusher, canFlush := w.(http.Flusher)
	enc := json.NewEncoder(w)
	last := since
	backlog := p.journal.Since(since, kinds...)
	for i := range backlog {
		_ = enc.Encode(&backlog[i])
		last = backlog[i].Seq
	}
	if canFlush {
		flusher.Flush()
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-p.done:
			return
		case e, ok := <-ch:
			if !ok {
				return
			}
			if e.Seq <= last || (len(want) > 0 && !want[e.Kind]) {
				continue
			}
			last = e.Seq
			if err := enc.Encode(e); err != nil {
				return
			}
			if canFlush {
				flusher.Flush()
			}
		}
	}
}

func decode(r *http.Request, v any) error {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("read request: %w", err)
	}
	if len(body) == 0 {
		return nil
	}
	if err := json.Unmarshal(body, v); err != nil {
		return fmt.Errorf("decode request: %w", err)
	}
	return nil
}

// okBody is the reply to a request that succeeds with nothing to return.
var okBody = map[string]bool{"ok": true}

func respond(w http.ResponseWriter, out any, err error) {
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, simreader.ErrNoDevice) {
			status = http.StatusNotFound
		}
		writeJSON(w, status, ErrorResponse{Error: err.Error()})
		return
	}
	if out == nil {
		out = okBody
	}
	writeJSON(w, http.StatusOK, out)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
