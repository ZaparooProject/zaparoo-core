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
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/fakes/fakectl"
)

const (
	// StandardQuotaBytes is the backup storage quota of an account that is
	// not limited.
	StandardQuotaBytes int64 = 10 << 30
	// LimitedQuotaBytes is the backup storage quota of a limited account,
	// small enough for a sim to reach.
	LimitedQuotaBytes int64 = 1 << 20

	stateFile   = "state.json"
	packsDir    = "packs"
	maxJSONBody = 32 << 20
	maxPackBody = 512 << 20
	maxWait     = 300 * time.Second
	requestLog  = 1000

	headerOS       = "Zaparoo-OS"
	headerArch     = "Zaparoo-Arch"
	headerPlatform = "Zaparoo-Platform"
	headerHint     = "Zaparoo-Device-Hint"
)

// Options configures a Server.
type Options struct {
	// StateDir keeps the fake's state in a JSON file and its backup packs
	// next to it, so a restart picks up where it left off. Empty keeps
	// everything in memory.
	StateDir string
	// SlowDelay is how long the "slow" fault holds a request before serving
	// it. Zero means two seconds.
	SlowDelay time.Duration
	// MaxWait caps how long the remote-session wait is held open with no
	// work. Zero means the timeout Core asks for, at most 300 seconds.
	MaxWait time.Duration
}

// Server is the fake Zaparoo Online service.
type Server struct {
	loadErr    error
	listener   net.Listener
	packs      map[string][]byte
	httpServer *http.Server
	wake       chan struct{}
	closed     chan struct{}
	log        *fakectl.RequestLog
	state      *store
	mux        *http.ServeMux
	url        string
	faults     []Fault
	opts       Options
	closeOnce  sync.Once
	muxOnce    sync.Once
	mu         syncutil.Mutex
}

// New returns a fake service. With Options.StateDir set it loads any state
// saved there; a load failure is reported by Err and Start.
func New(opts Options) *Server {
	if opts.SlowDelay <= 0 {
		opts.SlowDelay = 2 * time.Second
	}
	if opts.MaxWait <= 0 || opts.MaxWait > maxWait {
		opts.MaxWait = maxWait
	}
	s := &Server{
		opts:   opts,
		log:    fakectl.NewRequestLog(requestLog),
		packs:  make(map[string][]byte),
		wake:   make(chan struct{}),
		closed: make(chan struct{}),
		state:  newStore(),
	}
	s.loadErr = s.load()
	return s
}

// Err returns the error from loading saved state, if any.
func (s *Server) Err() error { return s.loadErr }

// Handler serves the device API, recording every request in the request log
// and applying any fault set.
func (s *Server) Handler() http.Handler {
	return s.log.Middleware(s.faultMiddleware(http.HandlerFunc(s.serveDevice)))
}

// Start listens on addr, such as "127.0.0.1:0", and serves the device API.
// It returns the base URL to put in [service] online_base_url.
func (s *Server) Start(addr string) (string, error) {
	if s.loadErr != nil {
		return "", s.loadErr
	}
	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", addr)
	if err != nil {
		return "", fmt.Errorf("listening for the fake online service: %w", err)
	}
	s.mu.Lock()
	s.listener = ln
	s.url = "http://" + ln.Addr().String()
	s.httpServer = &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second}
	srv := s.httpServer
	s.mu.Unlock()
	go func() { _ = srv.Serve(ln) }()
	return s.URL(), nil
}

// URL returns the base URL Start is serving on, or "" before Start.
func (s *Server) URL() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.url
}

// Close stops serving, releases any held wait and saves state.
func (s *Server) Close() error {
	var err error
	s.closeOnce.Do(func() {
		close(s.closed)
		err = s.shutdown()
	})
	return err
}

func (s *Server) shutdown() error {
	s.mu.Lock()
	srv := s.httpServer
	s.mu.Unlock()
	var errs []error
	if srv != nil {
		if err := srv.Close(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errs = append(errs, fmt.Errorf("closing the fake online service: %w", err))
		}
	}
	if err := s.save(); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// baseURL is the URL links and image addresses are built on: the URL Start
// serves on, or the address the request came in on when the handler is
// mounted elsewhere, as in tests using httptest.
func (s *Server) baseURL(r *http.Request) string {
	if u := s.URL(); u != "" {
		return u
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

// notifyWaiters wakes every held remote-session wait. Call with s.mu held.
func (s *Server) notifyWaiters() {
	close(s.wake)
	s.wake = make(chan struct{})
}

func (s *Server) load() error {
	if s.opts.StateDir == "" {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(s.opts.StateDir, stateFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("reading fake online state: %w", err)
	}
	loaded := newStore()
	if err := json.Unmarshal(data, loaded); err != nil {
		return fmt.Errorf("decoding fake online state: %w", err)
	}
	loaded.fill()
	for hash := range loaded.PackSizes {
		if !validPackHash(hash) {
			return fmt.Errorf("fake online state names an invalid pack %q", hash)
		}
		path := filepath.Join(s.opts.StateDir, packsDir, hash)
		body, readErr := os.ReadFile(path) //nolint:gosec // The name is checked to be a hex hash.
		if readErr != nil {
			return fmt.Errorf("reading fake online pack %s: %w", hash, readErr)
		}
		s.packs[hash] = body
	}
	s.state = loaded
	return nil
}

// save writes the state file when the fake has a state directory.
func (s *Server) save() error {
	if s.opts.StateDir == "" {
		return nil
	}
	s.mu.Lock()
	data, err := json.MarshalIndent(s.state, "", "  ")
	s.mu.Unlock()
	if err != nil {
		return fmt.Errorf("encoding fake online state: %w", err)
	}
	if err := os.MkdirAll(s.opts.StateDir, 0o750); err != nil {
		return fmt.Errorf("creating fake online state directory: %w", err)
	}
	path := filepath.Join(s.opts.StateDir, stateFile)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("writing fake online state: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("replacing fake online state: %w", err)
	}
	return nil
}

// savePack writes a pack next to the state file when the fake has a state
// directory.
func (s *Server) savePack(hash string, body []byte) error {
	if s.opts.StateDir == "" {
		return nil
	}
	dir := filepath.Join(s.opts.StateDir, packsDir)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("creating fake online pack directory: %w", err)
	}
	if !validPackHash(hash) {
		return fmt.Errorf("invalid pack hash %q", hash)
	}
	//nolint:gosec // The name is checked to be a hex hash.
	if err := os.WriteFile(filepath.Join(dir, hash), body, 0o600); err != nil {
		return fmt.Errorf("writing fake online pack: %w", err)
	}
	return nil
}

// validPackHash reports whether name is a sha256 in hex, so it is safe to
// use as a file name.
func validPackHash(name string) bool {
	decoded, err := hex.DecodeString(name)
	return err == nil && len(decoded) == sha256.Size
}

// apiError is the error body Core's clients decode.
type apiError struct {
	Fields  map[string]string `json:"fields,omitempty"`
	Code    string            `json:"code"`
	Message string            `json:"message"`
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeErrorFields(w, status, code, message, nil)
}

func writeErrorFields(w http.ResponseWriter, status int, code, message string, fields map[string]string) {
	if message == "" {
		message = strings.ReplaceAll(code, "_", " ")
	}
	fakectl.WriteJSON(w, status, map[string]apiError{
		"error": {Code: code, Message: message, Fields: fields},
	})
}

// decodeJSON reads a JSON request body into out.
func decodeJSON(r *http.Request, out any) error {
	data, err := io.ReadAll(io.LimitReader(r.Body, maxJSONBody+1))
	if err != nil {
		return fmt.Errorf("reading request body: %w", err)
	}
	if len(data) > maxJSONBody {
		return errors.New("request body too large")
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("decoding request body: %w", err)
	}
	return nil
}

// randomToken returns prefix followed by 16 random bytes in hex.
func randomToken(prefix string) string {
	var buf [16]byte
	_, _ = rand.Read(buf[:])
	return prefix + hex.EncodeToString(buf[:])
}

// nextID returns a new identifier with the given prefix. Call with s.mu held.
func (s *Server) nextID(prefix string) string {
	s.state.Sequence++
	return fmt.Sprintf("%s%d", prefix, s.state.Sequence)
}

func now() time.Time { return time.Now().UTC() }
