//go:build linux

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

package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/stretchr/testify/require"
)

const comparisonConfig = `debug_logging = true
error_reporting = false
[readers]
auto_detect = false
connect = []
[service]
api_listen = "127.0.0.1"
api_port = 7498
encryption = false
[service.discovery]
enabled = false
[service.remote_control]
enabled = false
[audio]
scan_feedback = false
[input]
gamepad_enabled = false
[updates]
check = false
install = false
`

func copyStartupFixture(t *testing.T, source, target string, mode os.FileMode) {
	t.Helper()
	//nolint:gosec // Explicit operator-selected fixture/binary source.
	in, err := os.Open(source)
	require.NoError(t, err)
	defer func() { _ = in.Close() }()
	//nolint:gosec // Target is inside t.TempDir, never a live installation.
	out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	require.NoError(t, err)
	_, err = io.Copy(out, in)
	require.NoError(t, err)
	require.NoError(t, out.Close())
}

func startupComparisonGet(ctx context.Context, t *testing.T, client *http.Client, path string) []byte {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:7498"+path, http.NoBody)
	require.NoError(t, err)
	response, err := client.Do(req)
	require.NoError(t, err)
	defer func() { _ = response.Body.Close() }()
	require.Equal(t, http.StatusOK, response.StatusCode)
	body, err := io.ReadAll(io.LimitReader(response.Body, 8<<20))
	require.NoError(t, err)
	return body
}

func startupComparisonRPC(ctx context.Context, t *testing.T, client *http.Client, method string) json.RawMessage {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": method, "params": map[string]any{},
	})
	require.NoError(t, err)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"http://127.0.0.1:7498/api/v0", bytes.NewReader(payload))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	response, err := client.Do(req)
	require.NoError(t, err)
	defer func() { _ = response.Body.Close() }()
	var result models.ResponseObject
	require.NoError(t, json.NewDecoder(response.Body).Decode(&result))
	require.Nil(t, result.Error)
	encoded, err := json.Marshal(result.Result)
	require.NoError(t, err)
	return encoded
}

// This opt-in hardware harness starts the unmodified MiSTer executable with a
// portable user directory. The operator must stop the original service and set
// aside /tmp/zaparoo before installing the explicit ownership marker. It never
// reads or replaces the original UserDB/config/binary. Real-time polling measures
// an external process; it is not synchronization for ordinary unit tests.
func TestPortableServiceStartupComparison(t *testing.T) {
	binary, fixture := os.Getenv("ZAPAROO_SERVICE_COMPARE_BINARY"), os.Getenv("ZAPAROO_SLUG_LIBRARY_DB")
	if binary == "" || fixture == "" {
		t.Skip("explicit portable native service comparison not requested")
	}
	marker, err := os.ReadFile("/tmp/zaparoo/.comparison-1287")
	require.NoError(t, err, "original service/runtime directory must be set aside first")
	require.Equal(t, "1287\n", string(marker))
	root := t.TempDir()
	user := filepath.Join(root, config.UserDir)
	require.NoError(t, os.MkdirAll(filepath.Join(user, config.CacheDir), 0o700))
	copyStartupFixture(t, binary, filepath.Join(root, "zaparoo.sh"), 0o700)
	copyStartupFixture(t, fixture, filepath.Join(user, "media.db"), 0o600)
	require.NoError(t, os.WriteFile(filepath.Join(user, config.CfgFile), []byte(comparisonConfig), 0o600))
	mode := "build"
	if cache := os.Getenv("ZAPAROO_SLUG_LIBRARY_CACHE"); cache != "" {
		copyStartupFixture(t, cache, filepath.Join(user, config.CacheDir, "slug_search_cache.gob"), 0o600)
		mode = "load"
	}
	// This file belongs to the marker-protected comparison directory, not the
	// original runtime directory. Clear it so readiness cannot match an old run.
	if removeErr := os.Remove("/tmp/zaparoo/core.log"); removeErr != nil {
		require.ErrorIs(t, removeErr, os.ErrNotExist)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 120*time.Second)
	defer cancel()
	//nolint:gosec // Explicit operator-selected native test build, copied into t.TempDir.
	cmd := exec.CommandContext(ctx, filepath.Join(root, "zaparoo.sh"), "-service", "exec")
	cmd.Env = append(os.Environ(), "ZAPAROO_APP="+filepath.Join(root, "zaparoo.sh"))
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	started := time.Now()
	require.NoError(t, cmd.Start())
	finished := make(chan error, 1)
	go func() { finished <- cmd.Wait() }()
	defer func() {
		_ = cmd.Process.Signal(syscall.SIGTERM)
		select {
		case waitErr := <-finished:
			require.NoError(t, waitErr, "service stderr: %s", stderr.String())
		case <-time.After(15 * time.Second):
			cancel()
			<-finished
			t.Error("portable service failed graceful shutdown")
		}
	}()
	client := &http.Client{Timeout: 5 * time.Second}
	defer client.CloseIdleConnections()
	var apiReady, cacheReady time.Duration
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for cacheReady == 0 || apiReady == 0 {
		select {
		case <-ctx.Done():
			t.Fatalf("startup readiness deadline exceeded: api=%s cache=%s", apiReady, cacheReady)
		case waitErr := <-finished:
			finished <- waitErr
			t.Fatal("portable service exited before startup readiness")
		case <-ticker.C:
		}
		logBytes, readErr := os.ReadFile("/tmp/zaparoo/core.log")
		if readErr != nil {
			require.ErrorIs(t, readErr, os.ErrNotExist)
			continue
		}
		if apiReady == 0 && bytes.Contains(logBytes, []byte("HTTP server bound to port, ready to accept connections")) {
			startupComparisonGet(ctx, t, client, "/health")
			apiReady = time.Since(started)
		}
		needle := "slug search cache built"
		if mode == "load" {
			needle = "slug search cache loaded from disk"
		}
		if cacheReady == 0 && bytes.Contains(logBytes, []byte(needle)) {
			cacheReady = time.Since(started)
		}
	}
	var idleSince time.Time
	for {
		var status models.MediaResponse
		require.NoError(t, json.Unmarshal(startupComparisonRPC(ctx, t, client, "media"), &status))
		require.False(t, status.Database.Indexing, "comparison fixture must not launch indexing")
		require.Empty(t, status.Active)
		if expected := os.Getenv("ZAPAROO_SLUG_LIBRARY_TITLES"); expected != "" {
			count, parseErr := strconv.Atoi(expected)
			require.NoError(t, parseErr)
			require.NotNil(t, status.Database.TotalMedia)
			require.Equal(t, count, *status.Database.TotalMedia)
		}
		if !status.Database.Optimizing {
			if idleSince.IsZero() {
				idleSince = time.Now()
			} else if time.Since(idleSince) >= 2*time.Second {
				break
			}
		} else {
			idleSince = time.Time{}
		}
		select {
		case <-ctx.Done():
			t.Fatal("startup maintenance did not settle")
		case <-ticker.C:
		}
	}
	var readers models.ReadersResponse
	require.NoError(t, json.Unmarshal(startupComparisonRPC(ctx, t, client, "readers"), &readers))
	require.Empty(t, readers.Readers, "portable comparison must not connect hardware readers")
	heap := startupComparisonGet(ctx, t, client, "/debug/pprof/heap?gc=1&debug=1")
	var memory []string
	for _, line := range strings.Split(string(heap), "\n") {
		if strings.HasPrefix(line, "# HeapAlloc =") || strings.HasPrefix(line, "# HeapInuse =") ||
			strings.HasPrefix(line, "# NumGC =") {
			memory = append(memory, line)
		}
	}
	require.NotEmpty(t, memory)
	statusPath := filepath.Join(string(filepath.Separator), "proc", strconv.Itoa(cmd.Process.Pid), "status")
	//nolint:gosec // Kernel status of the exact child process owned by this test.
	status, err := os.ReadFile(statusPath)
	require.NoError(t, err)
	var rss []string
	for _, line := range strings.Split(string(status), "\n") {
		if strings.HasPrefix(line, "VmRSS:") || strings.HasPrefix(line, "VmHWM:") {
			rss = append(rss, line)
		}
	}
	t.Logf("SERVICE_MEASURE mode=%s api_ns=%d cache_ns=%d settled_ns=%d heap=%q rss=%q",
		mode, apiReady, cacheReady, time.Since(started), strings.Join(memory, ";"), strings.Join(rss, ";"))
}
