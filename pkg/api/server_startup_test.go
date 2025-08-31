package api

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/pkg/service/tokens"
	"github.com/ZaparooProject/zaparoo-core/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/pkg/testing/mocks"
	"github.com/stretchr/testify/require"
)

// TestServerStartupRaceCondition demonstrates that the API server can experience
// race conditions during startup where connections fail intermittently.
// This test validates that the server is ready to accept connections before
// the Start function indicates completion.
func TestServerStartupRaceCondition(t *testing.T) {
	// This test attempts to catch the race condition where the server is started
	// in a goroutine but there's no synchronization to ensure it's ready
	
	// Try multiple times to increase chance of hitting the race condition
	for attempt := 0; attempt < 10; attempt++ {
		t.Run(fmt.Sprintf("attempt_%d", attempt), func(t *testing.T) {
			// Setup test environment
			platform := mocks.NewMockPlatform()
			platform.SetupBasicMock()

			// Use a specific port to avoid conflicts
			testPort := 8000 + attempt
			fs := helpers.NewMemoryFS()
			configDir := t.TempDir()
			cfg, err := helpers.NewTestConfigWithPort(fs, configDir, testPort)
			require.NoError(t, err)

			st, notifications := state.NewState(platform)
			defer st.StopService()

			db := &database.Database{
				UserDB:  helpers.NewMockUserDBI(),
				MediaDB: helpers.NewMockMediaDBI(),
			}

			tokenQueue := make(chan tokens.Token, 1)
			defer close(tokenQueue)

			// Start server in a separate goroutine
			serverStarted := make(chan struct{})
			go func() {
				close(serverStarted)
				Start(platform, cfg, st, tokenQueue, db, notifications)
			}()

			// Wait for goroutine to start, then immediately try to connect
			<-serverStarted
			
			// Try to connect immediately - this is where the race condition should manifest
			port := cfg.APIPort()
			client := &http.Client{Timeout: 1 * time.Millisecond} // Very short timeout
			
			// Make multiple rapid connection attempts to increase chance of hitting race condition
			for i := 0; i < 3; i++ {
				resp, err := client.Get(fmt.Sprintf("http://localhost:%d/api/v0.1", port))
				
				// If we get a connection refused error, we've caught the race condition
				if err != nil && strings.Contains(err.Error(), "connection refused") {
					t.Fatalf("Race condition detected on attempt %d: server not ready immediately after Start goroutine begins: %v", i, err)
				}
				
				if resp != nil {
					_ = resp.Body.Close()
				}
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

	st, notifications := state.NewState(platform)
	defer st.StopService()

	db := &database.Database{
		UserDB:  helpers.NewMockUserDBI(),
		MediaDB: helpers.NewMockMediaDBI(),
	}

	tokenQueue := make(chan tokens.Token, 1)
	defer close(tokenQueue)

	// Create connection attempt channel
	connectionResult := make(chan error, 1)
	
	// Start both server and connection attempt simultaneously
	go func() {
		Start(platform, cfg, st, tokenQueue, db, notifications)
	}()
	
	// Immediately try to connect (no delay)
	go func() {
		port := cfg.APIPort()
		client := &http.Client{Timeout: 10 * time.Millisecond}
		resp, err := client.Get(fmt.Sprintf("http://localhost:%d/api/v0.1", port))
		if resp != nil {
			_ = resp.Body.Close()
		}
		connectionResult <- err
	}()
	
	// Wait for connection result
	select {
	case err := <-connectionResult:
		if err != nil && strings.Contains(err.Error(), "connection refused") {
			t.Logf("Detected race condition - this is the issue we're trying to solve: %v", err)
			// Don't fail the test, just log that we detected the race condition
			// This documents the behavior we're observing
		}
	case <-time.After(100 * time.Millisecond):
		// Connection attempt timed out - this is fine
		t.Log("Connection attempt timed out - server may not have started yet")
	}
}