/*
Zaparoo Core
Copyright (c) 2026 The Zaparoo Project Contributors.
SPDX-License-Identifier: GPL-3.0-or-later

This file is part of Zaparoo Core.

Zaparoo Core is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

Zaparoo Core is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with Zaparoo Core.  If not, see <http://www.gnu.org/licenses/>.
*/

package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/rs/zerolog/log"
)

// ServiceState is the coarse lifecycle state Core reports on /health. It
// exists because everything Core normally uses to talk to a user sits behind
// the databases being open, so a slow or failed database start is invisible.
// The listener binds before any database work and answers with one of these
// from that moment on.
type ServiceState string

const (
	// ServiceStateStarting means the process is alive and working through
	// startup. Nothing but the startup page and /health answers yet.
	ServiceStateStarting ServiceState = "starting"
	// ServiceStateReady means the full API is serving.
	ServiceStateReady ServiceState = "ready"
	// ServiceStateFailed means startup stopped on something a person has to
	// resolve. Core stays bound so it can say what happened.
	ServiceStateFailed ServiceState = "failed"
)

// healthStatus maps a state onto the "status" field. That field exists only so
// callers matching the historical `"status":"ok"` keep working; "state" is the
// field new callers should read.
func healthStatus(state ServiceState) string {
	switch state {
	case ServiceStateReady:
		return "ok"
	case ServiceStateStarting:
		return "starting"
	case ServiceStateFailed:
		return "error"
	default:
		return "error"
	}
}

// serviceStatus is the state plus the human wording the startup page shows.
// The wording never reaches /health: that route is unauthenticated from any
// address by design, so it carries the coarse state and nothing else.
type serviceStatus struct {
	Headline string
	Detail   string
	LogPath  string
	State    ServiceState
}

type handlerHolder struct {
	handler http.Handler
}

// StartupServer owns the API listener from before the databases open until
// shutdown. It serves a startup page and /health until the full router is
// swapped in, so a slow migration or a refused database has somewhere to be
// reported.
type StartupServer struct {
	listener net.Listener
	server   *http.Server
	done     chan error
	handler  atomic.Pointer[handlerHolder]
	status   atomic.Pointer[serviceStatus]
	port     int
}

// NewStartupServer binds the configured API address and immediately begins
// serving the startup page. It is called before any database work so that
// every later failure has a surface to report on.
func NewStartupServer(ctx context.Context, cfg *config.Instance) (*StartupServer, error) {
	listenAddr := cfg.APIListen()
	port := cfg.APIPort()
	if _, portStr, err := net.SplitHostPort(listenAddr); err == nil && portStr != "" {
		if p, convErr := strconv.Atoi(portStr); convErr == nil {
			port = p
		}
	}

	lc := &net.ListenConfig{}
	listener, err := lc.Listen(ctx, "tcp", listenAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to bind API listener: %w", err)
	}

	// Adopt the real port when zero was requested so callers and the allowed
	// origin lists agree on what was bound.
	if port == 0 {
		if addr, ok := listener.Addr().(*net.TCPAddr); ok {
			port = addr.Port
			_ = cfg.SetAPIPort(port)
		}
	}

	s := &StartupServer{
		listener: listener,
		done:     make(chan error, 1),
		port:     port,
	}
	s.status.Store(&serviceStatus{
		State:    ServiceStateStarting,
		Headline: "Zaparoo is starting",
		Detail:   "Setting up and opening databases.",
	})
	s.handler.Store(&handlerHolder{handler: http.HandlerFunc(s.serveStartup)})

	s.server = &http.Server{
		Addr:              listenAddr,
		Handler:           s,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       config.APIRequestTimeout,
	}

	go func() {
		serveErr := s.server.Serve(listener)
		if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			log.Error().Err(serveErr).Msg("HTTP server error")
			s.done <- serveErr
			return
		}
		log.Debug().Msg("HTTP server stopped normally")
		s.done <- nil
	}()

	log.Info().Str("listen", listenAddr).Msg("starting HTTP server")
	return s, nil
}

// ServeHTTP dispatches to whichever handler is currently installed. The
// handler is swapped once, when the full router is ready.
func (s *StartupServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	holder := s.handler.Load()
	if holder == nil || holder.handler == nil {
		http.Error(w, "service unavailable", http.StatusServiceUnavailable)
		return
	}
	holder.handler.ServeHTTP(w, r)
}

// Port returns the port actually bound.
func (s *StartupServer) Port() int {
	return s.port
}

// Listener returns the bound listener.
func (s *StartupServer) Listener() net.Listener {
	return s.listener
}

// Done reports the result of the serving goroutine.
func (s *StartupServer) Done() <-chan error {
	return s.done
}

// State returns the current coarse state.
func (s *StartupServer) State() ServiceState {
	if status := s.status.Load(); status != nil {
		return status.State
	}
	return ServiceStateStarting
}

// SetStartingDetail updates the sentence shown while startup is still working.
func (s *StartupServer) SetStartingDetail(detail string) {
	if s == nil {
		return
	}
	s.status.Store(&serviceStatus{
		State:    ServiceStateStarting,
		Headline: "Zaparoo is starting",
		Detail:   detail,
	})
}

// SetFailed puts Core into the failed state. Core stays bound and keeps
// serving the page so the reason is readable without a log or a terminal.
func (s *StartupServer) SetFailed(headline, detail, logPath string) {
	if s == nil {
		return
	}
	s.status.Store(&serviceStatus{
		State:    ServiceStateFailed,
		Headline: headline,
		Detail:   detail,
		LogPath:  logPath,
	})
	log.Error().Str("headline", headline).Str("detail", detail).Msg("service entered failed state")
}

// SwapHandler installs the full API router and marks the service ready. It is
// called once, after startup completes.
func (s *StartupServer) SwapHandler(h http.Handler) {
	if s == nil {
		return
	}
	s.status.Store(&serviceStatus{
		State:    ServiceStateReady,
		Headline: "Zaparoo is running",
	})
	s.handler.Store(&handlerHolder{handler: h})
	log.Debug().Msg("API router installed, service ready")
}

// Shutdown stops the HTTP server and waits for it to stop serving.
//
// The wait is what makes the port free when this returns, and callers depend
// on that: the failed state has to give the port back when it is stopped, and
// a start that ends after the listener was bound has to leave it for the next
// attempt. http.Server.Shutdown alone does not promise it. It closes the
// listeners it has been told about, and Serve registers the listener after it
// starts, so a shutdown landing in that window closes nothing and leaves
// Serve's own deferred close to do it — after Shutdown has returned. Measured
// at 1714 of 2000 immediate shutdowns.
func (s *StartupServer) Shutdown(ctx context.Context) error {
	if s == nil {
		return nil
	}
	if err := s.server.Shutdown(ctx); err != nil {
		return fmt.Errorf("HTTP server shutdown error: %w", err)
	}
	select {
	case <-s.done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("waiting for the HTTP server to stop: %w", ctx.Err())
	}
}

// WriteHealth renders the /health body for the current state. It is used by
// both the startup handler and the full router so the two never disagree.
func (s *StartupServer) WriteHealth(w http.ResponseWriter) {
	state := ServiceStateReady
	if s != nil {
		state = s.State()
	}
	// Always 200: the process really is alive and serving in every one of
	// these states. Readiness is what the body carries, so callers checking
	// only the status code keep working.
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(models.HealthCheckResponse{
		Status: healthStatus(state),
		State:  string(state),
	}); err != nil {
		log.Debug().Err(err).Msg("failed writing health response")
	}
}

// refusal is what an unserved route says. The state matters: a caller told
// "still starting" reasonably retries, and in the failed state that is a wait
// that never ends. The page and /health carry the detail; this is the one
// line a client has room to show.
func (s *StartupServer) refusal() string {
	if s.State() == ServiceStateFailed {
		return "Zaparoo could not start; open it in a browser for details"
	}
	return "Zaparoo is still starting"
}

// serveStartup is the handler installed until the full router is ready. It
// answers the routes that need nothing but the listener and refuses
// everything else, so callers waiting on the real API keep waiting.
func (s *StartupServer) serveStartup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, s.refusal(), http.StatusServiceUnavailable)
		return
	}

	switch {
	case r.URL.Path == "/health":
		s.WriteHealth(w)
	case r.URL.Path == "/" || r.URL.Path == "/app" || strings.HasPrefix(r.URL.Path, "/app/"):
		s.servePage(w)
	default:
		http.Error(w, s.refusal(), http.StatusServiceUnavailable)
	}
}

func (s *StartupServer) servePage(w http.ResponseWriter) {
	status := s.status.Load()
	if status == nil {
		status = &serviceStatus{State: ServiceStateStarting, Headline: "Zaparoo is starting"}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	if err := startupPageTemplate.Execute(w, startupPageData{
		Headline: status.Headline,
		Detail:   status.Detail,
		LogPath:  status.LogPath,
		Failed:   status.State == ServiceStateFailed,
		Version:  config.AppVersion,
	}); err != nil {
		log.Debug().Err(err).Msg("failed rendering startup page")
	}
}

type startupPageData struct {
	Headline string
	Detail   string
	LogPath  string
	Version  string
	Failed   bool
}

// startupPageTemplate is deliberately self-contained: no external assets, no
// fonts, no scripts from anywhere. It has to render on a device whose only
// working component is a TCP listener.
var startupPageTemplate = template.Must(template.New("startup").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover">
<title>Zaparoo</title>
<style>
:root { color-scheme: light dark; }
body {
  margin: 0; min-height: 100vh; display: flex; align-items: center; justify-content: center;
  padding: 24px; box-sizing: border-box; background: #f6f6f8; color: #16161a;
  font: 16px/1.5 system-ui, -apple-system, "Segoe UI", Roboto, sans-serif;
}
@media (prefers-color-scheme: dark) { body { background: #16161a; color: #f2f2f4; } }
main { max-width: 34rem; width: 100%; }
h1 { font-size: 1.4rem; margin: 0 0 .6rem; }
p { margin: 0 0 .8rem; }
.detail { opacity: .85; }
.log { font-size: .9rem; opacity: .75; }
code { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; word-break: break-all; }
.spinner {
  width: 1rem; height: 1rem; margin-right: .5rem; display: inline-block; vertical-align: -2px;
  border: 2px solid currentColor; border-right-color: transparent; border-radius: 50%;
  animation: spin 1s linear infinite; opacity: .6;
}
@keyframes spin { to { transform: rotate(360deg); } }
@media (prefers-reduced-motion: reduce) { .spinner { animation: none; } }
.bad { color: #b3261e; }
@media (prefers-color-scheme: dark) { .bad { color: #f2b8b5; } }
footer { margin-top: 1.5rem; font-size: .85rem; opacity: .6; }
</style>
</head>
<body>
<main>
<h1>{{if not .Failed}}<span class="spinner"></span>{{end}}
<span{{if .Failed}} class="bad"{{end}}>{{.Headline}}</span></h1>
{{if .Detail}}<p class="detail">{{.Detail}}</p>{{end}}
{{if .LogPath}}<p class="log">Full details are in the log file: <code>{{.LogPath}}</code></p>{{end}}
<footer>Zaparoo Core v{{.Version}}</footer>
</main>
<script>
(function () {
  var failed = {{.Failed}};
  function poll() {
    fetch("/health", { cache: "no-store" }).then(function (r) { return r.json(); }).then(function (b) {
      if (b && b.state === "ready") { window.location.reload(); return; }
      if (b && ((b.state === "failed") !== failed)) { window.location.reload(); return; }
      setTimeout(poll, 2000);
    }).catch(function () { setTimeout(poll, 2000); });
  }
  setTimeout(poll, 2000);
})();
</script>
</body>
</html>
`))
