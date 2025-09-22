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

package methods

import (
	"context"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestHandleMediaGenerateCancel(t *testing.T) {
	tests := []struct {
		name               string
		expectedMessage    string
		indexingActive     bool
		expectError        bool
		setupIndexing      bool
		simulateSlowCancel bool
	}{
		{
			name:            "cancel active indexing",
			indexingActive:  true,
			setupIndexing:   true,
			expectedMessage: "Media indexing cancelled successfully",
			expectError:     false,
		},
		{
			name:            "cancel when no indexing active",
			indexingActive:  false,
			setupIndexing:   false,
			expectedMessage: "No media indexing operation is currently running",
			expectError:     false,
		},
		{
			name:               "cancel with slow response",
			indexingActive:     true,
			setupIndexing:      true,
			simulateSlowCancel: true,
			expectedMessage:    "Media indexing cancelled successfully",
			expectError:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset status between tests
			statusInstance.clear()

			// Create mock environment
			mockPlatform := mocks.NewMockPlatform()
			mockUserDB := &helpers.MockUserDBI{}
			mockMediaDB := &helpers.MockMediaDBI{}

			db := &database.Database{
				UserDB:  mockUserDB,
				MediaDB: mockMediaDB,
			}

			cfg := &config.Instance{}
			appState, _ := state.NewState(mockPlatform)

			env := requests.RequestEnv{
				Platform: mockPlatform,
				Config:   cfg,
				State:    appState,
				Database: db,
				Params:   []byte(`{}`),
			}

			// Setup indexing if needed
			if tt.setupIndexing {
				_, cancelFunc := context.WithCancel(context.Background())
				statusInstance.setCancelFunc(cancelFunc)
				statusInstance.setRunning(true)

				if tt.simulateSlowCancel {
					// Create a context that won't respond immediately
					go func() {
						time.Sleep(100 * time.Millisecond)
						cancelFunc()
					}()
				}
			}

			// Call the handler
			result, err := HandleMediaGenerateCancel(env)

			// Verify expectations
			if tt.expectError {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, result)

			response, ok := result.(map[string]any)
			require.True(t, ok, "Result should be a map")

			message, exists := response["message"]
			require.True(t, exists, "Response should contain message")
			assert.Equal(t, tt.expectedMessage, message)

			// Wait a bit for async cleanup
			if tt.indexingActive {
				time.Sleep(20 * time.Millisecond)
				assert.False(t, statusInstance.isRunning(), "Status should be cleared after cancellation")
			}
		})
	}
}

func TestMediaGenerateCancel_ConcurrentAccess(t *testing.T) {
	// Reset status
	statusInstance.clear()

	// Create mock environment
	mockPlatform := mocks.NewMockPlatform()
	mockUserDB := &helpers.MockUserDBI{}
	mockMediaDB := &helpers.MockMediaDBI{}

	db := &database.Database{
		UserDB:  mockUserDB,
		MediaDB: mockMediaDB,
	}

	cfg := &config.Instance{}
	appState, _ := state.NewState(mockPlatform)

	env := requests.RequestEnv{
		Platform: mockPlatform,
		Config:   cfg,
		State:    appState,
		Database: db,
		Params:   []byte(`{}`),
	}

	// Setup active indexing
	_, cancelFunc := context.WithCancel(context.Background())
	statusInstance.setCancelFunc(cancelFunc)
	statusInstance.setRunning(true)

	// Test concurrent cancel calls
	results := make(chan string, 2)
	errors := make(chan error, 2)

	// Launch two concurrent cancel calls
	for range 2 {
		go func() {
			result, err := HandleMediaGenerateCancel(env)
			if err != nil {
				errors <- err
				return
			}

			response, ok := result.(map[string]any)
			if !ok {
				errors <- assert.AnError
				return
			}

			message, exists := response["message"]
			if !exists {
				errors <- assert.AnError
				return
			}

			if msg, ok := message.(string); ok {
				results <- msg
			}
		}()
	}

	// Collect results
	var messages []string
	var errs []error

	for range 2 {
		select {
		case msg := <-results:
			messages = append(messages, msg)
		case err := <-errors:
			errs = append(errs, err)
		case <-time.After(1 * time.Second):
			t.Fatal("Test timed out")
		}
	}

	// Should have no errors
	assert.Empty(t, errs, "Should have no errors from concurrent cancel calls")

	// Should have two results
	assert.Len(t, messages, 2, "Should have received two results")

	// At least one should indicate successful cancellation
	foundSuccess := false
	for _, msg := range messages {
		if msg == "Media indexing cancelled successfully" || msg == "Media indexing cancellation initiated" {
			foundSuccess = true
			break
		}
	}
	assert.True(t, foundSuccess, "At least one call should indicate successful cancellation")
}

func TestMediaGenerateCancel_StatusManagement(t *testing.T) {
	// Reset status
	statusInstance.clear()

	// Create mock environment
	mockPlatform := mocks.NewMockPlatform()
	mockUserDB := &helpers.MockUserDBI{}
	mockMediaDB := &helpers.MockMediaDBI{}

	db := &database.Database{
		UserDB:  mockUserDB,
		MediaDB: mockMediaDB,
	}

	cfg := &config.Instance{}
	appState, _ := state.NewState(mockPlatform)

	env := requests.RequestEnv{
		Platform: mockPlatform,
		Config:   cfg,
		State:    appState,
		Database: db,
		Params:   []byte(`{}`),
	}

	// Verify initial state
	assert.False(t, statusInstance.isRunning(), "Should not be running initially")

	// Try to cancel when nothing is running
	result, err := HandleMediaGenerateCancel(env)
	require.NoError(t, err)

	response, ok := result.(map[string]any)
	require.True(t, ok, "Result should be a map")
	assert.Equal(t, "No media indexing operation is currently running", response["message"])

	// Setup indexing
	_, cancelFunc := context.WithCancel(context.Background())
	statusInstance.setCancelFunc(cancelFunc)
	statusInstance.setRunning(true)

	assert.True(t, statusInstance.isRunning(), "Should be running after setup")

	// Cancel the indexing
	result, err = HandleMediaGenerateCancel(env)
	require.NoError(t, err)

	response, ok = result.(map[string]any)
	require.True(t, ok, "Result should be a map")
	message, ok := response["message"].(string)
	require.True(t, ok, "Message should be a string")
	assert.True(t,
		message == "Media indexing cancelled successfully" ||
			message == "Media indexing cancellation initiated",
		"Should indicate cancellation was initiated or completed")

	// Wait a bit for cleanup
	time.Sleep(10 * time.Millisecond)

	// Try to cancel again - should indicate nothing is running
	result, err = HandleMediaGenerateCancel(env)
	require.NoError(t, err)

	response, ok = result.(map[string]any)
	require.True(t, ok, "Result should be a map")
	assert.Equal(t, "No media indexing operation is currently running", response["message"])
}

func TestMediaIndexingCancellation_Integration(t *testing.T) {
	// Reset status
	statusInstance.clear()

	// Create mock platform (still needed for non-database interactions)
	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("test-platform")
	mockPlatform.On("Settings").Return(platforms.Settings{})
	mockPlatform.On("RootDirs", mock.Anything).Return([]string{"/test/roms"})

	// Use real database
	db, cleanup := helpers.NewTestDatabase(t)
	defer cleanup()

	cfg := &config.Instance{}
	appState, notifications := state.NewState(mockPlatform)

	generateEnv := requests.RequestEnv{
		Platform: mockPlatform,
		Config:   cfg,
		State:    appState,
		Database: db,
		Params:   []byte(`{"systems": ["NES"]}`), // Single system for faster test
	}

	cancelEnv := requests.RequestEnv{
		Platform: mockPlatform,
		Config:   cfg,
		State:    appState,
		Database: db,
		Params:   []byte(`{}`),
	}

	// Verify initial state
	assert.False(t, statusInstance.isRunning(), "Should not be running initially")

	// Start media generation
	result, err := HandleGenerateMedia(generateEnv)
	require.NoError(t, err)
	assert.Nil(t, result) // HandleGenerateMedia returns nil on success

	// Wait briefly for indexing to start or complete
	time.Sleep(50 * time.Millisecond)

	// Check if indexing is still running or has completed
	isRunning := statusInstance.isRunning()

	if isRunning {
		// Indexing is still running, test cancellation
		t.Log("Indexing is running, testing cancellation")

		// Now try to cancel
		result, err = HandleMediaGenerateCancel(cancelEnv)
		require.NoError(t, err)
		require.NotNil(t, result)

		response, ok := result.(map[string]any)
		require.True(t, ok, "Result should be a map")

		message, exists := response["message"]
		require.True(t, exists, "Response should contain message")
		assert.Equal(t, "Media indexing cancelled successfully", message)

		// Wait for cancellation to complete
		timeout := time.After(2 * time.Second)
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()

		var cancellationCompleted bool
		for !cancellationCompleted {
			select {
			case <-timeout:
				t.Fatal("Cancellation did not complete within timeout")
			case <-ticker.C:
				if !statusInstance.isRunning() {
					cancellationCompleted = true
				}
			}
		}

		assert.False(t, statusInstance.isRunning(), "Status should be cleared after cancellation")
	} else {
		// Indexing completed quickly, test that cancellation indicates nothing running
		t.Log("Indexing completed quickly, testing post-completion cancellation")
		assert.False(t, statusInstance.isRunning(), "Status should be cleared after completion")
	}

	// Drain any remaining notifications to prevent goroutine leak
	go func() {
		for n := range notifications {
			_ = n // Consume and discard notification
		}
	}()

	// Try to cancel when nothing is running - should indicate nothing is running
	result, err = HandleMediaGenerateCancel(cancelEnv)
	require.NoError(t, err)

	response, ok := result.(map[string]any)
	require.True(t, ok, "Result should be a map")
	assert.Equal(t, "No media indexing operation is currently running", response["message"])
}
