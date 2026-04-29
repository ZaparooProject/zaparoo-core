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

package api

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/broker"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestBroker(ctx context.Context, source <-chan models.Notification) *broker.Broker {
	b := broker.NewBroker(ctx, source)
	b.Start()
	return b
}

func TestStartWithReadyReportsBindFailure(t *testing.T) {
	t.Parallel()

	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { require.NoError(t, listener.Close()) }()

	tcpAddr, ok := listener.Addr().(*net.TCPAddr)
	require.True(t, ok)

	platform := mocks.NewMockPlatform()
	platform.SetupBasicMock()

	fs := helpers.NewMemoryFS()
	configDir := t.TempDir()
	cfg, err := helpers.NewTestConfigWithListenAndPort(fs, configDir, "127.0.0.1", tcpAddr.Port)
	require.NoError(t, err)

	st, notifCh := state.NewState(platform, "test-boot-uuid")
	notifBroker := newTestBroker(st.GetContext(), notifCh)
	db := &database.Database{
		UserDB:  helpers.NewMockUserDBI(),
		MediaDB: helpers.NewMockMediaDBI(),
	}
	tokenQueue := make(chan tokens.Token, 1)
	ready := make(chan error, 1)

	serverErr := make(chan error, 1)
	go func() {
		serverErr <- StartWithReady(platform, cfg, st, tokenQueue, nil, db, nil, notifBroker, "", nil, nil, ready)
	}()

	select {
	case err = <-serverErr:
	case <-time.After(2 * time.Second):
		st.StopService()
		t.Fatal("StartWithReady did not return after bind failure")
	}
	require.Error(t, err)
	select {
	case readyErr := <-ready:
		require.Error(t, readyErr)
		assert.Contains(t, readyErr.Error(), "bind")
	case <-time.After(time.Second):
		t.Fatal("StartWithReady returned an error without signaling ready")
	}
	assert.ErrorIs(t, st.GetContext().Err(), context.Canceled)
}

// TestServerStartupConcurrency validates that the API server properly synchronizes
// startup and is ready to accept connections when multiple goroutines attempt
// to connect during server initialization.
func TestServerStartupConcurrency(t *testing.T) {
	t.Parallel()

	// This test validates that the server properly synchronizes startup
	// and handles concurrent connection attempts gracefully

	// Try multiple times to ensure consistent behavior
	for attempt := range 10 {
		t.Run(fmt.Sprintf("attempt_%d", attempt), func(t *testing.T) {
			t.Parallel()

			// Setup test environment
			platform := mocks.NewMockPlatform()
			platform.SetupBasicMock()

			// Use a specific port to avoid conflicts
			testPort := 8000 + attempt
			fs := helpers.NewMemoryFS()
			configDir := t.TempDir()
			cfg, err := helpers.NewTestConfigWithPort(fs, configDir, testPort)
			require.NoError(t, err)

			st, notifCh := state.NewState(platform, "test-boot-uuid")
			notifBroker := newTestBroker(st.GetContext(), notifCh)

			db := &database.Database{
				UserDB:  helpers.NewMockUserDBI(),
				MediaDB: helpers.NewMockMediaDBI(),
			}

			tokenQueue := make(chan tokens.Token, 1)

			// Start server in a separate goroutine
			serverDone := make(chan struct{})
			serverErr := make(chan error, 1)
			go func() {
				defer close(serverDone)
				serverErr <- Start(platform, cfg, st, tokenQueue, nil, db, nil, notifBroker, "", nil, nil)
			}()
			// Cleanup: stop service first, then wait for server goroutine to fully exit
			defer func() {
				st.StopService()
				close(tokenQueue)
				<-serverDone
				require.NoError(t, <-serverErr)
			}()

			// Test that server becomes available and responds correctly
			// The server should properly synchronize startup internally
			port := cfg.APIPort()
			transport := &http.Transport{}
			client := &http.Client{Timeout: 50 * time.Millisecond, Transport: transport}
			defer transport.CloseIdleConnections()
			url := fmt.Sprintf("http://localhost:%d/api/v0.1", port)

			// Give server reasonable time to start (should be very quick due to internal sync)
			var resp *http.Response
			var connectErr error
			for range 50 { // Try for up to 2.5 seconds
				req, reqErr := http.NewRequestWithContext(context.Background(), http.MethodGet, url, http.NoBody)
				require.NoError(t, reqErr)

				resp, connectErr = client.Do(req) //nolint:gosec // G704: test hitting local test server
				if connectErr == nil {
					break // Server responded successfully
				}
				time.Sleep(50 * time.Millisecond)
			}

			// Server should be available within reasonable time due to proper synchronization
			require.NoError(t, connectErr, "Server should be available after startup synchronization")
			if resp != nil {
				_ = resp.Body.Close()
			}
		})
	}
}

// TestServerStartupImmediateConnection tests the most aggressive case:
// connecting with zero delay after the Start function is called
func TestServerStartupImmediateConnection(t *testing.T) {
	t.Parallel()

	// Setup test environment
	platform := mocks.NewMockPlatform()
	platform.SetupBasicMock()

	// Use port 0 for dynamic allocation
	fs := helpers.NewMemoryFS()
	configDir := t.TempDir()
	cfg, err := helpers.NewTestConfigWithPort(fs, configDir, 0)
	require.NoError(t, err)

	st, notifCh := state.NewState(platform, "test-boot-uuid")
	notifBroker := newTestBroker(st.GetContext(), notifCh)

	db := &database.Database{
		UserDB:  helpers.NewMockUserDBI(),
		MediaDB: helpers.NewMockMediaDBI(),
	}

	tokenQueue := make(chan tokens.Token, 1)

	// Start server in a separate goroutine
	serverDone := make(chan struct{})
	serverErr := make(chan error, 1)
	go func() {
		defer close(serverDone)
		serverErr <- Start(platform, cfg, st, tokenQueue, nil, db, nil, notifBroker, "", nil, nil)
	}()
	// Cleanup: stop service first, then wait for server goroutine to fully exit
	defer func() {
		st.StopService()
		close(tokenQueue)
		<-serverDone
		require.NoError(t, <-serverErr)
	}()

	// Create connection attempt channel
	connectionResult := make(chan error, 1)

	// Create transport that we can close to cancel in-flight dials
	transport := &http.Transport{}
	defer transport.CloseIdleConnections()

	// Immediately try to connect (no delay)
	go func() {
		port := cfg.APIPort()
		client := &http.Client{Timeout: 10 * time.Millisecond, Transport: transport}
		url := fmt.Sprintf("http://localhost:%d/api/v0.1", port)
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, http.NoBody)
		if err != nil {
			connectionResult <- err
			return
		}
		resp, err := client.Do(req) //nolint:gosec // G704: test hitting local test server
		if resp != nil {
			_ = resp.Body.Close()
		}
		connectionResult <- err
	}()

	// Wait for connection result - always wait for the goroutine to complete
	select {
	case err := <-connectionResult:
		if err != nil && strings.Contains(err.Error(), "connection refused") {
			t.Logf("Connection refused detected during startup race test: %v", err)
			// This logs potential race condition behavior for analysis
		}
	case <-time.After(100 * time.Millisecond):
		// Connection attempt timed out - this is fine
		t.Log("Connection attempt timed out - server may not have started yet")
		// Wait for the goroutine to finish to avoid leaking it
		<-connectionResult
	}
}

// TestServerListenContextCancellation tests that the server properly respects context cancellation
// during the listen phase. This test validates that net.Listen operations are context-aware.
func TestServerListenContextCancellation(t *testing.T) {
	t.Parallel()

	platform := mocks.NewMockPlatform()
	platform.SetupBasicMock()

	fs := helpers.NewMemoryFS()
	configDir := t.TempDir()
	cfg, err := helpers.NewTestConfigWithPort(fs, configDir, 9000)
	require.NoError(t, err)

	// Create a state with a context that we can cancel
	st, notifCh := state.NewState(platform, "test-boot-uuid")
	notifBroker := newTestBroker(st.GetContext(), notifCh)

	// Cancel the state context immediately to test context cancellation during listen
	st.StopService()

	db := &database.Database{
		UserDB:  helpers.NewMockUserDBI(),
		MediaDB: helpers.NewMockMediaDBI(),
	}

	tokenQueue := make(chan tokens.Token, 1)
	defer close(tokenQueue) // Safe here since context is already cancelled

	// This should return quickly because the context is already cancelled.
	// If startup ignores the context, it would take longer or hang.
	done := make(chan struct{})
	serverErr := make(chan error, 1)
	start := time.Now()

	go func() {
		defer close(done)
		serverErr <- Start(platform, cfg, st, tokenQueue, nil, db, nil, notifBroker, "", nil, nil)
	}()

	// Wait for completion or timeout
	select {
	case <-done:
		require.NoError(t, <-serverErr)
		elapsed := time.Since(start)
		// With context cancellation, this should complete very quickly (< 100ms)
		// Without context awareness, it would take longer or hang
		if elapsed > 500*time.Millisecond {
			t.Errorf("Server took too long to respond to context cancellation: %v", elapsed)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Server did not respect context cancellation - likely using non-context-aware net.Listen")
	}
}

func TestIsPrivateIP(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		ip       string
		expected bool
	}{
		{
			name:     "private_10_0_0_1",
			ip:       "10.0.0.1",
			expected: true,
		},
		{
			name:     "private_192_168_1_1",
			ip:       "192.168.1.1",
			expected: true,
		},
		{
			name:     "private_172_16_0_1",
			ip:       "172.16.0.1",
			expected: true,
		},
		{
			name:     "link_local_169_254",
			ip:       "169.254.1.1",
			expected: true,
		},
		{
			name:     "public_8_8_8_8",
			ip:       "8.8.8.8",
			expected: false,
		},
		{
			name:     "localhost",
			ip:       "127.0.0.1",
			expected: false,
		},
		{
			name:     "invalid_ip",
			ip:       "not.an.ip",
			expected: false,
		},
		{
			name:     "empty_string",
			ip:       "",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := isPrivateIP(tt.ip)
			require.Equal(t, tt.expected, result, "isPrivateIP result mismatch for %s", tt.ip)
		})
	}
}

func TestIsAllowedOrigin_WebSocketPolicy(t *testing.T) {
	t.Parallel()

	staticOrigins := []string{
		"capacitor://localhost",
		"ionic://localhost",
		"http://localhost",
		"https://localhost",
		"http://localhost:7497",
		"http://192.168.1.100:7497",
		"http://MiSTer.local:7497", // Mixed case hostname
	}
	customOriginsProvider := func() []string { return nil }
	apiPort := 7497

	tests := []struct {
		name     string
		origin   string
		expected bool
	}{
		{
			name:     "empty_origin_allowed",
			origin:   "",
			expected: true,
		},
		{
			name:     "localhost_other_port_rejected",
			origin:   "http://localhost:8100",
			expected: false,
		},
		{
			name:     "localhost_https_other_port_rejected",
			origin:   "https://localhost:3000",
			expected: false,
		},
		{
			name:     "127_0_0_1_other_port_rejected",
			origin:   "http://127.0.0.1:8100",
			expected: false,
		},
		{
			name:     "implicit_private_ip_correct_port_rejected",
			origin:   "http://192.168.1.50:7497",
			expected: false,
		},
		{
			name:     "explicit_private_ip_allowed",
			origin:   "http://192.168.1.100:7497",
			expected: true,
		},
		{
			name:     "private_ip_wrong_port_rejected",
			origin:   "http://192.168.1.50:8100",
			expected: false,
		},
		{
			name:     "public_ip_rejected",
			origin:   "http://8.8.8.8:7497",
			expected: false,
		},
		{
			name:     "capacitor_origin_allowed",
			origin:   "capacitor://localhost",
			expected: true,
		},
		{
			name:     "explicit_allowed_origin",
			origin:   "http://localhost:7497",
			expected: true,
		},
		{
			name:     "case_insensitive_hostname_match",
			origin:   "http://mister.local:7497", // lowercase, but allowed list has MiSTer.local
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := isAllowedOrigin(tt.origin, staticOrigins, customOriginsProvider, apiPort, true, "websocket")
			require.Equal(t, tt.expected, result, "isAllowedOrigin result mismatch for %s", tt.origin)
		})
	}
}

func TestMultipleLocalIPsInAllowedOrigins(t *testing.T) {
	t.Parallel()

	// This test demonstrates the current limitation: server only uses first IP
	// from GetLocalIP() instead of all IPs from GetAllLocalIPs()

	// Simulate scenario where we have multiple local IPs
	testIPs := []string{"192.168.1.100", "10.0.0.50"}
	port := 7497

	// Current server logic (single IP) - this is what currently happens
	var currentLogicOrigins []string
	if len(testIPs) > 0 {
		localIP := testIPs[0] // Only first IP (simulating GetLocalIP behavior)
		currentLogicOrigins = append(currentLogicOrigins,
			fmt.Sprintf("http://%s:%d", localIP, port),
			fmt.Sprintf("https://%s:%d", localIP, port),
		)
	}

	// Desired logic (all IPs) - this is what should happen
	improvedLogicOrigins := make([]string, 0, len(testIPs)*2)
	for _, localIP := range testIPs { // ALL IPs (using GetAllLocalIPs)
		improvedLogicOrigins = append(improvedLogicOrigins,
			fmt.Sprintf("http://%s:%d", localIP, port),
			fmt.Sprintf("https://%s:%d", localIP, port),
		)
	}

	// Current logic only includes first IP
	require.Len(t, currentLogicOrigins, 2, "Current logic includes only 2 origins (first IP)")
	require.Contains(t, currentLogicOrigins, "http://192.168.1.100:7497")
	require.NotContains(t, currentLogicOrigins, "http://10.0.0.50:7497")

	// Improved logic includes all IPs
	require.Len(t, improvedLogicOrigins, 4, "Improved logic should include all IPs")
	require.Contains(t, improvedLogicOrigins, "http://192.168.1.100:7497")
	require.Contains(t, improvedLogicOrigins, "http://10.0.0.50:7497")

	// This demonstrates why we need to change from GetLocalIP() to GetAllLocalIPs()
	t.Logf("Current: %d origins, Improved: %d origins", len(currentLogicOrigins), len(improvedLogicOrigins))
}

func TestBuildDynamicAllowedOrigins(t *testing.T) {
	t.Parallel()

	// Test that the allowed origins builder correctly handles multiple local IPs
	baseOrigins := []string{
		"capacitor://localhost",
		"ionic://localhost",
		"http://localhost",
		"https://localhost",
	}

	localIPs := []string{"192.168.1.100", "10.0.0.50"}
	port := 7497
	// Test various custom origin formats
	customOrigins := []string{
		"example.com",                // hostname only
		"http://batocera.local:7497", // full URL with port
		"https://myhost.local:8080",  // full URL with different port
		"http://noport.local",        // full URL without port
		"capacitor://localhost",      // mobile app scheme
		"  http://whitespace.local ", // with whitespace (should be trimmed)
		"http://trailing.local/",     // with trailing slash (should be trimmed)
	}

	// Test that buildDynamicAllowedOrigins correctly builds allowed origins list
	result := buildDynamicAllowedOrigins(baseOrigins, localIPs, port, customOrigins)

	// Should include base origins
	require.Contains(t, result, "capacitor://localhost")

	// Should include all local IPs with port
	require.Contains(t, result, "http://192.168.1.100:7497")
	require.Contains(t, result, "https://192.168.1.100:7497")
	require.Contains(t, result, "http://10.0.0.50:7497")
	require.Contains(t, result, "https://10.0.0.50:7497")

	// Hostname-only custom origins should get all variants (with and without port)
	// This supports both direct access and reverse proxy scenarios
	require.Contains(t, result, "http://example.com")
	require.Contains(t, result, "https://example.com")
	require.Contains(t, result, "http://example.com:7497")
	require.Contains(t, result, "https://example.com:7497")

	// Full URL with port should be used as-is
	require.Contains(t, result, "http://batocera.local:7497")
	require.Contains(t, result, "https://myhost.local:8080")

	// Full URL without port should include both as-is AND with port appended
	require.Contains(t, result, "http://noport.local")
	require.Contains(t, result, "http://noport.local:7497")

	// Other schemes (capacitor://, ionic://) should be used as-is
	require.Contains(t, result, "capacitor://localhost")

	// Whitespace should be trimmed
	require.Contains(t, result, "http://whitespace.local")
	require.NotContains(t, result, "  http://whitespace.local ")

	// Trailing slashes should be trimmed
	require.Contains(t, result, "http://trailing.local")
	require.NotContains(t, result, "http://trailing.local/")
}

func TestDefaultAllowedOriginsIncludesHostedApp(t *testing.T) {
	t.Parallel()

	require.Contains(t, allowedOrigins, "https://zaparoo.app")
}

func TestOriginPolicy_HappyPaths(t *testing.T) {
	t.Parallel()

	port := 7497
	staticOrigins := buildStaticAllowedOrigins(allowedOrigins, []string{"10.0.0.50"}, port)
	provider := func() []string { return nil }

	// Browser loading the bundled web UI from the device uses the device URL as Origin.
	assert.True(t, isAllowedOrigin("http://10.0.0.50:7497", staticOrigins, provider, port, true, "websocket"))
	assert.True(t, isAllowedOrigin("http://10.0.0.50:7497", staticOrigins, provider, port, false, "cors"))

	// Native clients may omit Origin on WebSocket connections.
	assert.True(t, isAllowedOrigin("", staticOrigins, provider, port, true, "websocket"))

	// The hosted app is trusted by default.
	assert.True(t, isAllowedOrigin("https://zaparoo.app", staticOrigins, provider, port, true, "websocket"))
	assert.True(t, isAllowedOrigin("https://zaparoo.app", staticOrigins, provider, port, false, "cors"))
}

// TestServerBindFailureStopsService verifies that when the API server fails to bind
// to its port (e.g., port already in use), it calls StopService() to trigger a
// graceful shutdown of the entire service. This is a regression test for issue #448.
func TestServerBindFailureStopsService(t *testing.T) {
	t.Parallel()

	// Setup first server that will occupy the port
	platform1 := mocks.NewMockPlatform()
	platform1.SetupBasicMock()

	testPort := 9100 // Use a fixed port for this test
	fs1 := helpers.NewMemoryFS()
	configDir1 := t.TempDir()
	cfg1, err := helpers.NewTestConfigWithListenAndPort(fs1, configDir1, "127.0.0.1", testPort)
	require.NoError(t, err)

	st1, notifCh1 := state.NewState(platform1, "test-boot-uuid-1")
	notifBroker1 := newTestBroker(st1.GetContext(), notifCh1)
	db1 := &database.Database{
		UserDB:  helpers.NewMockUserDBI(),
		MediaDB: helpers.NewMockMediaDBI(),
	}
	tokenQueue1 := make(chan tokens.Token, 1)

	// Start first server
	server1Done := make(chan struct{})
	server1Err := make(chan error, 1)
	go func() {
		defer close(server1Done)
		server1Err <- Start(platform1, cfg1, st1, tokenQueue1, nil, db1, nil, notifBroker1, "", nil, nil)
	}()

	// Wait for first server to be ready
	client := &http.Client{Timeout: 100 * time.Millisecond}
	url := fmt.Sprintf("http://localhost:%d/api/v0.1", testPort)
	var server1Ready bool
	for range 50 {
		req, reqErr := http.NewRequestWithContext(context.Background(), http.MethodGet, url, http.NoBody)
		if reqErr != nil {
			continue
		}
		resp, connErr := client.Do(req) //nolint:gosec // G704: test hitting local test server
		if connErr == nil {
			_ = resp.Body.Close()
			server1Ready = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	require.True(t, server1Ready, "First server should start successfully")

	// Now try to start second server on the same port - this should fail
	platform2 := mocks.NewMockPlatform()
	platform2.SetupBasicMock()

	fs2 := helpers.NewMemoryFS()
	configDir2 := t.TempDir()
	cfg2, err := helpers.NewTestConfigWithListenAndPort(fs2, configDir2, "127.0.0.1", testPort) // Same port!
	require.NoError(t, err)

	st2, notifCh2 := state.NewState(platform2, "test-boot-uuid-2")
	notifBroker2 := newTestBroker(st2.GetContext(), notifCh2)
	db2 := &database.Database{
		UserDB:  helpers.NewMockUserDBI(),
		MediaDB: helpers.NewMockMediaDBI(),
	}
	tokenQueue2 := make(chan tokens.Token, 1)

	// Start second server - it should fail to bind and call StopService
	server2Done := make(chan struct{})
	server2Err := make(chan error, 1)
	go func() {
		defer close(server2Done)
		server2Err <- Start(platform2, cfg2, st2, tokenQueue2, nil, db2, nil, notifBroker2, "", nil, nil)
	}()

	// Wait for the second server's context to be cancelled (StopService called)
	// or timeout if it doesn't happen
	select {
	case <-st2.GetContext().Done():
		// Success - StopService was called due to bind failure
		t.Log("StopService was called after bind failure - test passed")
	case <-time.After(5 * time.Second):
		t.Fatal("StopService was not called after bind failure - expected service to stop")
	}

	// Cleanup
	st1.StopService()
	close(tokenQueue1)
	close(tokenQueue2)
	<-server1Done
	require.NoError(t, <-server1Err)
	<-server2Done
	bindErr := <-server2Err
	require.Error(t, bindErr)
	assert.Contains(t, bindErr.Error(), "bind")
}

// TestBuildDynamicAllowedOrigins_HTTPURLWithoutPortAddsPortVariant is a regression
// test for GitHub issue #371 where users couldn't connect from .local DNS hostnames
// like batocera.local because the browser sends origin with port but users configure
// URLs without port.
func TestBuildDynamicAllowedOrigins_HTTPURLWithoutPortAddsPortVariant(t *testing.T) {
	t.Parallel()

	// Simulate the exact scenario from issue #371:
	// User configured: allowed_origins = ['http://batocera.local', 'http://ko.rhino-dragon.ts.net']
	// Browser sent origin: http://batocera.local:7497
	// Expected: Connection should be allowed

	baseOrigins := []string{"http://localhost"}
	localIPs := []string{"192.168.1.100"}
	port := 7497

	// User's config from issue #371
	customOrigins := []string{
		"http://batocera.local",
		"http://ko.rhino-dragon.ts.net",
	}

	result := buildDynamicAllowedOrigins(baseOrigins, localIPs, port, customOrigins)

	// The browser sends origin WITH port, so we need to match it
	// Before the fix: only "http://http://batocera.local" was added (double prefix bug)
	// After the fix: both with and without port variants should be present
	require.Contains(t, result, "http://batocera.local:7497",
		"Issue #371: browser sends origin with port, must be in allowed list")
	require.Contains(t, result, "http://ko.rhino-dragon.ts.net:7497",
		"Issue #371: Tailscale hostname must also work with port")

	// Also include the user's original config values (for reverse proxy scenarios)
	require.Contains(t, result, "http://batocera.local",
		"Original config value should also be preserved")
	require.Contains(t, result, "http://ko.rhino-dragon.ts.net",
		"Original config value should also be preserved")

	// Verify the old bug is fixed - should NOT have double http:// prefix
	require.NotContains(t, result, "http://http://batocera.local",
		"Bug fix: should not have double http:// prefix")
	require.NotContains(t, result, "https://http://batocera.local",
		"Bug fix: should not have https:// prepended to http:// URL")
}

func TestIsAllowedOrigin_WebSocketHotReload(t *testing.T) {
	t.Parallel()

	staticOrigins := []string{
		"http://localhost:7497",
	}
	apiPort := 7497

	// Mutable custom origins to simulate config reload
	customOrigins := []string{"http://myapp.example.com"}
	provider := func() []string { return customOrigins }

	// Initial state: custom origin allowed
	assert.True(t, isAllowedOrigin("http://myapp.example.com", staticOrigins, provider, apiPort, true, "websocket"))
	assert.True(t, isAllowedOrigin("http://myapp.example.com:7497", staticOrigins, provider, apiPort, true, "websocket"))
	assert.False(t, isAllowedOrigin("http://other.example.com:7497", staticOrigins, provider, apiPort, true, "websocket"))

	// Simulate config reload: change custom origins
	customOrigins = []string{"http://other.example.com"}

	// Old custom origin should now be rejected (not private IP, not localhost)
	assert.False(t, isAllowedOrigin("http://myapp.example.com:7497", staticOrigins, provider, apiPort, true, "websocket"))
	// New custom origin should be allowed
	assert.True(t, isAllowedOrigin("http://other.example.com", staticOrigins, provider, apiPort, true, "websocket"))
	assert.True(t, isAllowedOrigin("http://other.example.com:7497", staticOrigins, provider, apiPort, true, "websocket"))

	// Static origins should always work regardless of custom origins
	assert.True(t, isAllowedOrigin("http://localhost:7497", staticOrigins, provider, apiPort, true, "websocket"))
}

func TestMakeOriginValidator_HotReload(t *testing.T) {
	t.Parallel()

	staticOrigins := []string{
		"http://localhost:7497",
		"http://192.168.1.100:7497",
	}
	port := 7497

	// Mutable custom origins to simulate config reload
	customOrigins := []string{"myapp.local"}
	provider := func() []string { return customOrigins }

	validator := makeOriginValidator(staticOrigins, provider, port)

	// Static origins always work
	assert.True(t, validator(nil, "http://localhost:7497"))
	assert.True(t, validator(nil, "http://192.168.1.100:7497"))

	// Custom origin (expanded) works
	assert.True(t, validator(nil, "http://myapp.local:7497"))
	assert.True(t, validator(nil, "https://myapp.local:7497"))

	// Unknown origin rejected
	assert.False(t, validator(nil, "http://unknown.com:7497"))

	// Simulate config reload: change custom origins
	customOrigins = []string{"newapp.local"}

	// Old custom origin should now be rejected
	assert.False(t, validator(nil, "http://myapp.local:7497"))

	// New custom origin should work
	assert.True(t, validator(nil, "http://newapp.local:7497"))

	// Static origins still work
	assert.True(t, validator(nil, "http://localhost:7497"))
}

func TestMakeOriginValidator_RejectsImplicitLocalhostPorts(t *testing.T) {
	t.Parallel()

	staticOrigins := []string{
		"http://localhost:7497",
		"http://127.0.0.1:7497",
		"http://192.168.1.100:7497",
	}
	port := 7497
	provider := func() []string { return nil }
	validator := makeOriginValidator(staticOrigins, provider, port)

	tests := []struct {
		name     string
		origin   string
		expected bool
	}{
		{
			name:     "localhost_other_port_rejected",
			origin:   "http://localhost:8100",
			expected: false,
		},
		{
			name:     "localhost_https_other_port_rejected",
			origin:   "https://localhost:3000",
			expected: false,
		},
		{
			name:     "127_0_0_1_other_port_rejected",
			origin:   "http://127.0.0.1:8100",
			expected: false,
		},
		{
			name:     "explicit_localhost_port_allowed",
			origin:   "http://localhost:7497",
			expected: true,
		},
		{
			name:     "explicit_127_0_0_1_port_allowed",
			origin:   "http://127.0.0.1:7497",
			expected: true,
		},
		{
			name:     "implicit_private_ip_correct_port_rejected",
			origin:   "http://192.168.1.50:7497",
			expected: false,
		},
		{
			name:     "explicit_private_ip_allowed",
			origin:   "http://192.168.1.100:7497",
			expected: true,
		},
		{
			name:     "private_ip_wrong_port_rejected",
			origin:   "http://192.168.1.50:8100",
			expected: false,
		},
		{
			name:     "public_ip_rejected",
			origin:   "http://8.8.8.8:7497",
			expected: false,
		},
		{
			name:     "empty_origin_rejected",
			origin:   "",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := validator(nil, tt.origin)
			require.Equal(t, tt.expected, result, "makeOriginValidator result mismatch for %s", tt.origin)
		})
	}
}

func TestMakeOriginValidator_ExplicitCustomLocalhostPort(t *testing.T) {
	t.Parallel()

	staticOrigins := []string{"http://localhost:7497"}
	customOrigins := []string{"http://localhost:8100", "127.0.0.1:8100"}
	provider := func() []string { return customOrigins }
	validator := makeOriginValidator(staticOrigins, provider, 7497)

	assert.True(t, validator(nil, "http://localhost:8100"))
	assert.True(t, validator(nil, "http://127.0.0.1:8100"))
	assert.False(t, validator(nil, "http://localhost:3000"))
}

func TestSSE_ReceivesNotifications(t *testing.T) {
	t.Parallel()

	platform := mocks.NewMockPlatform()
	platform.SetupBasicMock()

	fs := helpers.NewMemoryFS()
	configDir := t.TempDir()
	cfg, err := helpers.NewTestConfigWithPort(fs, configDir, 0)
	require.NoError(t, err)

	st, notifCh := state.NewState(platform, "test-boot-uuid")
	notifBroker := newTestBroker(st.GetContext(), notifCh)

	db := &database.Database{
		UserDB:  helpers.NewMockUserDBI(),
		MediaDB: helpers.NewMockMediaDBI(),
	}

	tokenQueue := make(chan tokens.Token, 1)

	serverDone := make(chan struct{})
	serverErr := make(chan error, 1)
	go func() {
		defer close(serverDone)
		serverErr <- Start(platform, cfg, st, tokenQueue, nil, db, nil, notifBroker, "", nil, nil)
	}()
	defer func() {
		st.StopService()
		close(tokenQueue)
		<-serverDone
		require.NoError(t, <-serverErr)
	}()

	// Wait for server to bind and update config with actual port
	client := &http.Client{Timeout: 100 * time.Millisecond}
	var serverReady bool
	for range 100 {
		port := cfg.APIPort()
		if port == 0 {
			time.Sleep(50 * time.Millisecond)
			continue
		}
		healthURL := fmt.Sprintf("http://localhost:%d/health", port)
		req, reqErr := http.NewRequestWithContext(context.Background(), http.MethodGet, healthURL, http.NoBody)
		if reqErr != nil {
			continue
		}
		resp, connErr := client.Do(req) //nolint:gosec // test hitting local test server
		if connErr == nil {
			_ = resp.Body.Close()
			serverReady = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	require.NotEqual(t, 0, cfg.APIPort(), "server should have bound to a port")
	require.True(t, serverReady, "server should start successfully")

	port := cfg.APIPort()
	sseURL := fmt.Sprintf("http://localhost:%d/api/v0.1/events", port)

	// Connect to SSE endpoint (no timeout for the streaming connection)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sseURL, http.NoBody)
	require.NoError(t, err)

	sseClient := &http.Client{}
	resp, err := sseClient.Do(req) //nolint:gosec // test hitting local test server
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))

	// Start reading SSE events before sending the notification to avoid
	// a race where the notification is delivered before the reader starts.
	scanner := bufio.NewScanner(resp.Body)
	var eventData string
	deadline := time.After(5 * time.Second)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "data: ") {
				eventData = strings.TrimPrefix(line, "data: ")
				return
			}
		}
	}()

	// Send a notification through the broker
	payload, _ := json.Marshal(map[string]string{"uid": "test-123", "text": "**launch:game.rom"})
	st.Notifications <- models.Notification{
		Method: "tokens.staged",
		Params: payload,
	}

	select {
	case <-done:
	case <-deadline:
		cancel()
		t.Fatal("timed out waiting for SSE event")
	}

	require.NotEmpty(t, eventData)

	var obj models.NotificationObject
	require.NoError(t, json.Unmarshal([]byte(eventData), &obj))
	assert.Equal(t, "2.0", obj.JSONRPC)
	assert.Equal(t, "tokens.staged", obj.Method)
}
