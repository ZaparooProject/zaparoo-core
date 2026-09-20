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
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestStartupServer binds a startup server on an OS-assigned port.
func newTestStartupServer(t *testing.T) *StartupServer {
	t.Helper()

	cfg, err := testhelpers.NewTestConfigWithListenAndPort(nil, t.TempDir(), "127.0.0.1", 0)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	srv, err := NewStartupServer(ctx, cfg)
	require.NoError(t, err)
	t.Cleanup(func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		assert.NoError(t, srv.Shutdown(shutdownCtx))
	})

	return srv
}

func getBody(t *testing.T, url string) (status int, body string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	require.NoError(t, err)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { assert.NoError(t, resp.Body.Close()) }()

	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, string(raw)
}

func healthState(t *testing.T, srv *StartupServer) (status, state string) {
	t.Helper()

	code, body := getBody(t, fmt.Sprintf("http://127.0.0.1:%d/health", srv.Port()))
	// Always 200: the process really is alive and serving in each of these
	// states, so anything checking only the status code keeps working.
	require.Equal(t, http.StatusOK, code)

	var payload struct {
		Status string `json:"status"`
		State  string `json:"state"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &payload), "body was %q", body)
	return payload.Status, payload.State
}

// The whole point of binding before the databases open is that something
// answers while they are being opened.
func TestStartupServer_AnswersBeforeTheDatabasesAreOpen(t *testing.T) {
	t.Parallel()

	srv := newTestStartupServer(t)

	status, state := healthState(t, srv)
	assert.Equal(t, "starting", status)
	assert.Equal(t, "starting", state)

	code, body := getBody(t, fmt.Sprintf("http://127.0.0.1:%d/app/", srv.Port()))
	assert.Equal(t, http.StatusOK, code)
	assert.Contains(t, body, "Zaparoo is starting")
}

// A page that can only ever say "starting" becomes the thing people sit
// staring at, which is the problem it was meant to solve. The failed state has
// to be reachable and has to say what happened.
func TestStartupServer_ReportsAFinalFailedState(t *testing.T) {
	t.Parallel()

	srv := newTestStartupServer(t)
	srv.SetFailed(
		"Zaparoo cannot open your saved data",
		"Your data was upgraded by a newer version of Zaparoo.",
		"/media/fat/zaparoo/core.log",
	)

	status, state := healthState(t, srv)
	assert.Equal(t, "error", status)
	assert.Equal(t, "failed", state)

	code, body := getBody(t, fmt.Sprintf("http://127.0.0.1:%d/", srv.Port()))
	assert.Equal(t, http.StatusOK, code)
	assert.Contains(t, body, "Zaparoo cannot open your saved data")
	assert.Contains(t, body, "upgraded by a newer version")
	assert.Contains(t, body, "/media/fat/zaparoo/core.log",
		"the page has to say where the log is, because that is what support asks for")
}

// Callers waiting for a usable service must keep waiting: the startup phase
// answers the page and health only, never the API.
func TestStartupServer_RefusesEverythingElseWhileStarting(t *testing.T) {
	t.Parallel()

	srv := newTestStartupServer(t)

	for _, path := range []string{"/api", "/api/v0.1", "/api/events", "/run/something"} {
		code, _ := getBody(t, fmt.Sprintf("http://127.0.0.1:%d%s", srv.Port(), path))
		assert.Equal(t, http.StatusServiceUnavailable, code, "%s must not answer yet", path)
	}
}

// Swapping the handler is how the full router takes over without rebinding,
// which is what keeps the port held continuously across startup.
func TestStartupServer_SwapHandlerTakesOverAndReportsReady(t *testing.T) {
	t.Parallel()

	srv := newTestStartupServer(t)

	mux := http.NewServeMux()
	mux.HandleFunc("/api", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("real router"))
	})
	srv.SwapHandler(mux)

	assert.Equal(t, ServiceStateReady, srv.State())

	code, body := getBody(t, fmt.Sprintf("http://127.0.0.1:%d/api", srv.Port()))
	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, "real router", body)
}

// A second bind on the same address has to fail rather than quietly succeed,
// because that is the one startup failure with nowhere to report itself.
func TestStartupServer_RefusesAnOccupiedPort(t *testing.T) {
	t.Parallel()

	srv := newTestStartupServer(t)

	cfg, err := testhelpers.NewTestConfigWithListenAndPort(nil, t.TempDir(), "127.0.0.1", srv.Port())
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	_, err = NewStartupServer(ctx, cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to bind API listener")
}
