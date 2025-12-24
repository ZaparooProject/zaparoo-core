// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
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
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/require"
)

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

			st, notifications := state.NewState(platform, "test-boot-uuid")

			db := &database.Database{
				UserDB:  helpers.NewMockUserDBI(),
				MediaDB: helpers.NewMockMediaDBI(),
			}

			tokenQueue := make(chan tokens.Token, 1)

			// Start server in a separate goroutine
			serverDone := make(chan struct{})
			go func() {
				defer close(serverDone)
				Start(platform, cfg, st, tokenQueue, db, nil, notifications, "")
			}()
			// Cleanup: stop service first, then wait for server goroutine to fully exit
			defer func() {
				st.StopService()
				close(tokenQueue)
				<-serverDone
			}()

			// Test that server becomes available and responds correctly
			// The server should properly synchronize startup internally
			port := cfg.APIPort()
			client := &http.Client{Timeout: 50 * time.Millisecond}
			url := fmt.Sprintf("http://localhost:%d/api/v0.1", port)

			// Give server reasonable time to start (should be very quick due to internal sync)
			var resp *http.Response
			var connectErr error
			for range 50 { // Try for up to 2.5 seconds
				req, reqErr := http.NewRequestWithContext(context.Background(), http.MethodGet, url, http.NoBody)
				require.NoError(t, reqErr)

				resp, connectErr = client.Do(req)
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

	st, notifications := state.NewState(platform, "test-boot-uuid")

	db := &database.Database{
		UserDB:  helpers.NewMockUserDBI(),
		MediaDB: helpers.NewMockMediaDBI(),
	}

	tokenQueue := make(chan tokens.Token, 1)

	// Start server in a separate goroutine
	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		Start(platform, cfg, st, tokenQueue, db, nil, notifications, "")
	}()
	// Cleanup: stop service first, then wait for server goroutine to fully exit
	defer func() {
		st.StopService()
		close(tokenQueue)
		<-serverDone
	}()

	// Create connection attempt channel
	connectionResult := make(chan error, 1)

	// Immediately try to connect (no delay)
	go func() {
		port := cfg.APIPort()
		client := &http.Client{Timeout: 10 * time.Millisecond}
		url := fmt.Sprintf("http://localhost:%d/api/v0.1", port)
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, http.NoBody)
		if err != nil {
			connectionResult <- err
			return
		}
		resp, err := client.Do(req)
		if resp != nil {
			_ = resp.Body.Close()
		}
		connectionResult <- err
	}()

	// Wait for connection result
	select {
	case err := <-connectionResult:
		if err != nil && strings.Contains(err.Error(), "connection refused") {
			t.Logf("Connection refused detected during startup race test: %v", err)
			// This logs potential race condition behavior for analysis
		}
	case <-time.After(100 * time.Millisecond):
		// Connection attempt timed out - this is fine
		t.Log("Connection attempt timed out - server may not have started yet")
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
	st, notifications := state.NewState(platform, "test-boot-uuid")

	// Cancel the state context immediately to test context cancellation during listen
	st.StopService()

	db := &database.Database{
		UserDB:  helpers.NewMockUserDBI(),
		MediaDB: helpers.NewMockMediaDBI(),
	}

	tokenQueue := make(chan tokens.Token, 1)
	defer close(tokenQueue) // Safe here since context is already cancelled

	// This should fail quickly because the context is already cancelled
	// If net.Listen is used (not context-aware), it will succeed in binding
	// If net.ListenConfig.Listen is used with context, it will fail fast
	done := make(chan struct{})
	start := time.Now()

	go func() {
		defer close(done)
		Start(platform, cfg, st, tokenQueue, db, nil, notifications, "")
	}()

	// Wait for completion or timeout
	select {
	case <-done:
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

func TestCheckWebSocketOrigin(t *testing.T) {
	t.Parallel()

	allowedOrigins := []string{
		"capacitor://localhost",
		"ionic://localhost",
		"http://localhost",
		"https://localhost",
		"http://localhost:7497",
		"http://192.168.1.100:7497",
		"http://MiSTer.local:7497", // Mixed case hostname
	}
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
			name:     "localhost_any_port_allowed",
			origin:   "http://localhost:8100",
			expected: true,
		},
		{
			name:     "localhost_https_any_port_allowed",
			origin:   "https://localhost:3000",
			expected: true,
		},
		{
			name:     "127_0_0_1_any_port_allowed",
			origin:   "http://127.0.0.1:8100",
			expected: true,
		},
		{
			name:     "private_ip_correct_port_allowed",
			origin:   "http://192.168.1.50:7497",
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
			result := checkWebSocketOrigin(tt.origin, allowedOrigins, apiPort)
			require.Equal(t, tt.expected, result, "checkWebSocketOrigin result mismatch for %s", tt.origin)
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
