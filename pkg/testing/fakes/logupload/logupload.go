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

// Package logupload is an offline fake of the log paste service Core's "upload
// log" sends to. It takes the multipart upload Core sends, answers with the
// link Core shows the user, serves the log at that link and keeps every upload
// for inspection. Point Core at it with helpers.SetLogUploadURL.
package logupload

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/fakes/fakectl"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/fakes/internal/fakehttp"
)

// Name is the fake's name, and the mount the sim gives its control API.
const Name = "logupload"

// FormField is the multipart field Core sends the log in.
const FormField = "file"

// maxUpload caps one upload; Core sends at most config.LogBundleMaxBytes.
const maxUpload = 32 << 20

// Fault is a failure the fake applies to its service requests until cleared:
// a status to answer with, a delay, or both, optionally limited to a path
// prefix.
type Fault = fakehttp.Fault

// Options configures a fake log service.
type Options struct {
	// Limit is how many uploads are kept, oldest dropped first; 0 keeps 100.
	Limit int
	// RequestLogLimit bounds the request log; 0 keeps 1000.
	RequestLogLimit int
}

// Upload is one stored log.
type Upload struct {
	Time     time.Time `json:"time"`
	ID       string    `json:"id"`
	Filename string    `json:"filename"`
	URL      string    `json:"url"`
	Content  []byte    `json:"-"`
	Size     int       `json:"size"`
}

// Server is a fake log paste service.
type Server struct {
	service  http.Handler
	control  http.Handler
	log      *fakectl.RequestLog
	listener fakehttp.Listener
	uploads  []Upload
	faults   fakehttp.Faults
	limit    int
	next     int
	mu       syncutil.Mutex
}

// New returns a fake log service.
func New(opts Options) *Server {
	limit := opts.Limit
	if limit <= 0 {
		limit = 100
	}
	s := &Server{limit: limit, log: fakectl.NewRequestLog(opts.RequestLogLimit)}
	s.service = fakehttp.Service(s.log, &s.faults, http.HandlerFunc(s.serve))
	s.control = s.controlMux()
	return s
}

// Handler serves the fake log service.
func (s *Server) Handler() http.Handler { return s.service }

// Start listens on addr, such as "127.0.0.1:0", and returns the base URL.
// Core posts to the base URL with a trailing slash.
func (s *Server) Start(addr string) (string, error) {
	return s.listener.Start(addr, s.service) //nolint:wrapcheck // Already descriptive.
}

// URL is the base URL from Start, or "" before it.
func (s *Server) URL() string { return s.listener.URL() }

// Close stops the server started by Start.
func (s *Server) Close() error { return s.listener.Close() } //nolint:wrapcheck // Already descriptive.

// Requests returns the logged requests.
func (s *Server) Requests() []fakectl.Request { return s.log.List() }

// SetFault applies a fault to every later request.
func (s *Server) SetFault(f Fault) error {
	if err := s.faults.Set(f); err != nil {
		return fmt.Errorf("set fault: %w", err)
	}
	return nil
}

// ClearFault removes the fault.
func (s *Server) ClearFault() { s.faults.Clear() }

// Uploads lists the stored uploads, oldest first, content included.
func (s *Server) Uploads() []Upload {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Upload, len(s.uploads))
	for i, u := range s.uploads {
		u.Content = append([]byte(nil), u.Content...)
		out[i] = u
	}
	return out
}

// Upload returns one stored upload by ID.
func (s *Server) Upload(id string) (Upload, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, u := range s.uploads {
		if u.ID == id {
			u.Content = append([]byte(nil), u.Content...)
			return u, true
		}
	}
	return Upload{}, false
}

// ClearUploads drops every stored upload.
func (s *Server) ClearUploads() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.uploads = nil
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/":
		s.receive(w, r)
	case (r.Method == http.MethodGet || r.Method == http.MethodHead) && strings.HasSuffix(r.URL.Path, ".log"):
		u, ok := s.Upload(strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/"), ".log"))
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write(u.Content)
	default:
		http.NotFound(w, r)
	}
}

// receive stores a multipart upload and answers with its link as plain text,
// the way the real service does.
func (s *Server) receive(w http.ResponseWriter, r *http.Request) {
	mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/form-data" || params["boundary"] == "" {
		http.Error(w, "expected a multipart/form-data upload", http.StatusBadRequest)
		return
	}
	reader := multipart.NewReader(http.MaxBytesReader(w, r.Body, maxUpload), params["boundary"])
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			http.Error(w, "missing form field "+strconv.Quote(FormField), http.StatusBadRequest)
			return
		}
		if err != nil {
			http.Error(w, "bad multipart body: "+err.Error(), http.StatusBadRequest)
			return
		}
		if part.FormName() != FormField {
			continue
		}
		content, err := io.ReadAll(part)
		if err != nil {
			http.Error(w, "read upload: "+err.Error(), http.StatusBadRequest)
			return
		}
		link := s.store(baseURL(s.URL(), r), part.FileName(), content)
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = io.WriteString(w, link+"\n") //nolint:gosec // A plain-text link this fake built.
		return
	}
}

func baseURL(started string, r *http.Request) string {
	if started != "" {
		return started
	}
	return "http://" + r.Host
}

func (s *Server) store(base, filename string, content []byte) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.next++
	id := strconv.Itoa(s.next)
	u := Upload{
		ID: id, Filename: filename, Size: len(content), Time: time.Now().UTC(),
		URL: base + "/" + id + ".log", Content: content,
	}
	s.uploads = append(s.uploads, u)
	if over := len(s.uploads) - s.limit; over > 0 {
		s.uploads = append([]Upload(nil), s.uploads[over:]...)
	}
	return u.URL
}

// ControlHandler serves the control API, mounted by the sim at
// /fakes/logs/:
//
//	GET    requests       logged requests; DELETE clears them
//	GET    fault          current fault; POST sets one, DELETE clears it
//	GET    uploads        stored uploads, without content; DELETE clears them
//	GET    uploads/{id}   one upload's content as {"upload":…, "content":"…"}
func (s *Server) ControlHandler() http.Handler { return s.control }

type uploadContent struct {
	Content string `json:"content"`
	Upload  Upload `json:"upload"`
}

func (s *Server) controlMux() http.Handler {
	mux := fakehttp.ControlMux(s.log, &s.faults)
	mux.HandleFunc("GET /uploads", func(w http.ResponseWriter, _ *http.Request) {
		fakectl.WriteJSON(w, http.StatusOK, s.Uploads())
	})
	mux.HandleFunc("DELETE /uploads", func(w http.ResponseWriter, _ *http.Request) {
		s.ClearUploads()
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /uploads/{id}", func(w http.ResponseWriter, r *http.Request) {
		u, ok := s.Upload(r.PathValue("id"))
		if !ok {
			fakehttp.WriteError(w, http.StatusNotFound, fmt.Errorf("no upload %q", r.PathValue("id")))
			return
		}
		fakectl.WriteJSON(w, http.StatusOK, uploadContent{Upload: u, Content: string(u.Content)})
	})
	return mux
}

// CLI runs a control command against the fake through call and prints the
// JSON reply:
//
//	requests [clear]
//	fault [set <status> [path-prefix] | delay <ms> [path-prefix] | clear]
//	uploads [get <id> | clear]
func CLI(ctx context.Context, args []string, call fakectl.Caller, stdout io.Writer) error {
	//nolint:wrapcheck // The shared CLI's errors are already descriptive.
	return fakehttp.CLI(ctx, Name, args, call, stdout, map[string]fakehttp.Command{
		"uploads": func(ctx context.Context, args []string, call fakectl.Caller, stdout io.Writer) error {
			switch {
			case len(args) == 0:
				return fakehttp.Print(ctx, call, stdout, http.MethodGet, "uploads", nil)
			case len(args) == 1 && args[0] == "clear":
				return fakehttp.Print(ctx, call, stdout, http.MethodDelete, "uploads", nil)
			case len(args) == 2 && args[0] == "get":
				return fakehttp.Print(ctx, call, stdout, http.MethodGet, "uploads/"+args[1], nil)
			default:
				return errors.New("usage: uploads [get <id> | clear]")
			}
		},
	})
}
