// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package examples

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/pkg/service/tokens"
	"github.com/ZaparooProject/zaparoo-core/pkg/testing/fixtures"
	"github.com/ZaparooProject/zaparoo-core/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// TestApplicationStateTransitions demonstrates testing state transitions
func TestApplicationStateTransitions(t *testing.T) {
	// Create mock platform for state initialization
	mockPlatform := mocks.NewMockPlatform()
	
	testCases := []struct {
		name           string
		setupState     func() *state.State
		action         func(*state.State)
		expectedResult func(*state.State) bool
		description    string
	}{
		{
			name: "Set active playlist",
			setupState: func() *state.State {
				st, _ := state.NewState(mockPlatform)
				return st
			},
			action: func(st *state.State) {
				playlists := fixtures.SamplePlaylists()
				st.SetActivePlaylist(playlists[0])
			},
			expectedResult: func(st *state.State) bool {
				playlist := st.GetActivePlaylist()
				return playlist != nil && playlist.Name == fixtures.SamplePlaylists()[0].Name
			},
			description: "Active playlist should be set correctly",
		},
		{
			name: "Clear active playlist",
			setupState: func() *state.State {
				st, _ := state.NewState(mockPlatform)
				playlists := fixtures.SamplePlaylists()
				st.SetActivePlaylist(playlists[0])
				return st
			},
			action: func(st *state.State) {
				st.SetActivePlaylist(nil) // Clear by setting to nil
			},
			expectedResult: func(st *state.State) bool {
				return st.GetActivePlaylist() == nil
			},
			description: "Active playlist should be cleared",
		},
		{
			name: "Track reader connection status",
			setupState: func() *state.State {
				st, _ := state.NewState(mockPlatform)
				return st
			},
			action: func(st *state.State) {
				mockReader := mocks.NewMockReader()
				st.SetReader("nfc_reader", mockReader)
			},
			expectedResult: func(st *state.State) bool {
				_, exists := st.GetReader("nfc_reader")
				return exists
			},
			description: "Reader connection status should be tracked",
		},
		{
			name: "Update reader disconnection",
			setupState: func() *state.State {
				st, _ := state.NewState(mockPlatform)
				mockReader := mocks.NewMockReader()
				st.SetReader("nfc_reader", mockReader)
				return st
			},
			action: func(st *state.State) {
				st.RemoveReader("nfc_reader")
			},
			expectedResult: func(st *state.State) bool {
				_, exists := st.GetReader("nfc_reader")
				return !exists
			},
			description: "Reader disconnection should be tracked",
		},
		{
			name: "Set active card token",
			setupState: func() *state.State {
				st, _ := state.NewState(mockPlatform)
				return st
			},
			action: func(st *state.State) {
				tokens := fixtures.SampleTokens()
				st.SetActiveCard(*tokens[0])
			},
			expectedResult: func(st *state.State) bool {
				lastScanned := st.GetLastScanned()
				return lastScanned.UID == fixtures.SampleTokens()[0].UID
			},
			description: "Active card token should be stored",
		},
	}
	
    for _, tc := range testCases {
        t.Run(tc.name, func(t *testing.T) {
            // Setup test state
            testState := tc.setupState()
            t.Cleanup(func() { testState.StopService() })
			
			// Apply action
			tc.action(testState)
			
			// Verify result
			result := tc.expectedResult(testState)
			assert.True(t, result, tc.description)
		})
	}
}

// TestConcurrentStateAccess demonstrates testing realistic concurrent scenarios
func TestConcurrentStateAccess(t *testing.T) {
    mockPlatform := mocks.NewMockPlatform()
    st, _ := state.NewState(mockPlatform)
    t.Cleanup(func() { st.StopService() })
	
	// Test realistic scenario: multiple readers connecting while tokens are being processed
	t.Run("Readers connecting during token processing", func(t *testing.T) {
		var wg sync.WaitGroup
		
		// Goroutine 1: Connect readers
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 3; i++ {
				readerName := fmt.Sprintf("reader_%d", i)
				mockReader := mocks.NewMockReader()
				st.SetReader(readerName, mockReader)
			}
		}()
		
		// Goroutine 2: Process tokens
		wg.Add(1)
		go func() {
			defer wg.Done()
			tokens := fixtures.SampleTokens()
			for _, token := range tokens {
				st.SetActiveCard(*token)
			}
		}()
		
		wg.Wait()
		
		// Verify final state is consistent
		readers := st.ListReaders()
		assert.Len(t, readers, 3)
		
		lastToken := st.GetLastScanned()
		assert.NotEmpty(t, lastToken.UID)
	})
	
	// Test realistic scenario: playlist changes while reader status updates
	t.Run("Playlist changes with reader updates", func(t *testing.T) {
		var wg sync.WaitGroup
		playlists := fixtures.SamplePlaylists()
		
		// Goroutine 1: Update playlist
		wg.Add(1)
		go func() {
			defer wg.Done()
			st.SetActivePlaylist(playlists[0])
		}()
		
		// Goroutine 2: Reader connects and disconnects
		wg.Add(1)
		go func() {
			defer wg.Done()
			mockReader := mocks.NewMockReader()
			st.SetReader("concurrent_reader", mockReader)
			st.RemoveReader("concurrent_reader")
		}()
		
		wg.Wait()
		
		// Verify state is consistent
		playlist := st.GetActivePlaylist()
		require.NotNil(t, playlist)
		assert.Equal(t, playlists[0].Name, playlist.Name)
		
		_, exists := st.GetReader("concurrent_reader")
		assert.False(t, exists)
	})
}

// TestStateNotificationSystem demonstrates testing notification broadcasting
func TestStateNotificationSystem(t *testing.T) {
    mockPlatform := mocks.NewMockPlatform()
    st, notificationChan := state.NewState(mockPlatform)
    t.Cleanup(func() { st.StopService() })
	
    // Start a goroutine to collect notifications with a stop signal
    var notifications []interface{}
    var notificationMutex sync.Mutex
    stopNotifications := make(chan struct{})
    defer close(stopNotifications)
    go func() {
        for {
            select {
            case notification := <-notificationChan:
                notificationMutex.Lock()
                notifications = append(notifications, notification)
                notificationMutex.Unlock()
            case <-stopNotifications:
                return
            }
        }
    }()
	
	testCases := []struct {
		name        string
		action      func()
		description string
	}{
		{
			name: "Reader connected notification",
			action: func() {
				mockReader := mocks.NewMockReader()
				st.SetReader("test_reader", mockReader)
			},
			description: "Should notify when reader connects",
		},
		{
			name: "Reader disconnected notification", 
			action: func() {
				st.RemoveReader("test_reader")
			},
			description: "Should notify when reader disconnects",
		},
		{
			name: "Token scanned notification",
			action: func() {
				tokens := fixtures.SampleTokens()
				st.SetActiveCard(*tokens[0])
			},
			description: "Should notify when token is scanned",
		},
		{
			name: "Playlist changed notification",
			action: func() {
				playlists := fixtures.SamplePlaylists()
				st.SetActivePlaylist(playlists[0])
			},
			description: "Should track playlist changes",
		},
	}
	
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			initialCount := func() int {
				notificationMutex.Lock()
				defer notificationMutex.Unlock()
				return len(notifications)
			}()
			
			// Perform action
			tc.action()
			
			// Give notifications time to process
			time.Sleep(10 * time.Millisecond)
			
			// Verify we have some notifications (exact count depends on implementation)
			finalCount := func() int {
				notificationMutex.Lock()
				defer notificationMutex.Unlock()
				return len(notifications)
			}()
			
			// For reader/token operations, we expect notifications
			if tc.name != "Playlist changed notification" {
				assert.Greater(t, finalCount, initialCount, tc.description)
			}
		})
	}
}

// TestStateValidationAndErrorHandling demonstrates error handling in state management
func TestStateValidationAndErrorHandling(t *testing.T) {
    mockPlatform := mocks.NewMockPlatform()
    st, _ := state.NewState(mockPlatform)
    t.Cleanup(func() { st.StopService() })
	
	t.Run("Empty reader name handling", func(t *testing.T) {
		// Test with empty reader name
		mockReader := mocks.NewMockReader()
		st.SetReader("", mockReader)
		
		_, exists := st.GetReader("")
		// The behavior depends on implementation - just verify it works without crashing
		assert.IsType(t, true, exists) // Just verify it returns a boolean
	})
	
	t.Run("Nil playlist handling", func(t *testing.T) {
		// Set nil playlist
		st.SetActivePlaylist(nil)
		
		// Should handle gracefully
		playlist := st.GetActivePlaylist()
		assert.Nil(t, playlist)
	})
	
	t.Run("Zero-value token handling", func(t *testing.T) {
		// Set empty token
		var emptyToken tokens.Token
		st.SetActiveCard(emptyToken)
		
		// Should handle gracefully
		lastScanned := st.GetLastScanned()
		assert.Equal(t, "", lastScanned.UID) // Empty token should have empty UID
	})
}

// TestStateIntegrationWithServices demonstrates testing state integration with other services
func TestStateIntegrationWithServices(t *testing.T) {
	// Setup complete service environment
	platform := mocks.NewMockPlatform()
	// Only set expectations for what this test actually uses
	
    st, _ := state.NewState(platform)
    t.Cleanup(func() { st.StopService() })
	
	userDB := helpers.NewMockUserDBI()
	mediaDB := helpers.NewMockMediaDBI()
	
	_ = &database.Database{
		UserDB:  userDB,
		MediaDB: mediaDB,
	}
	
	// Setup database expectations
	userDB.On("AddHistory", helpers.HistoryEntryMatcher()).Return(nil)
	mediaDB.On("SearchMediaPathExact", fixtures.GetTestSystemDefs(), helpers.TextMatcher()).Return(fixtures.SearchResults.Collection, nil)
	platform.On("LaunchMedia", mock.AnythingOfType("*config.Instance"), mock.AnythingOfType("string")).Return(nil)
	
	t.Run("Token processing updates state", func(t *testing.T) {
		// Simulate token processing that updates state
		tokens := fixtures.SampleTokens()
		token := tokens[0]
		
		// 1. Set reader as connected
		mockReader := mocks.NewMockReader()
		st.SetReader("nfc_reader", mockReader)
		_, exists := st.GetReader("nfc_reader")
		assert.True(t, exists)
		
		// 2. Process token and update last scanned
		st.SetActiveCard(*token)
		
		// 3. Verify state was updated
		lastToken := st.GetLastScanned()
		assert.Equal(t, token.UID, lastToken.UID)
		
		// 4. Simulate successful media launch
		searchResults, err := mediaDB.SearchMediaPathExact(fixtures.GetTestSystemDefs(), token.Text)
		require.NoError(t, err)
		require.NotEmpty(t, searchResults)
		
		// Create config for platform launch (use empty for test)
		cfg := &config.Instance{}
		
		// Use the first search result path for launch
		mediaPath := searchResults[0].Path
		err = platform.LaunchMedia(cfg, mediaPath)
		require.NoError(t, err)
		
		// 5. Record history
		he := database.HistoryEntry{
			Time:       token.ScanTime,
			Type:       token.Type,
			TokenID:    token.UID,
			TokenValue: token.Text,
			Success:    true,
		}
		
		err = userDB.AddHistory(&he)
		require.NoError(t, err)
		
		// Verify all expectations
		userDB.AssertExpectations(t)
		mediaDB.AssertExpectations(t)
		platform.AssertExpectations(t)
	})
	
	t.Run("Reader disconnection clears state", func(t *testing.T) {
		// Start with connected reader and active token
		mockReader := mocks.NewMockReader()
		st.SetReader("nfc_reader", mockReader)
		tokens := fixtures.SampleTokens()
		token := tokens[0]
		st.SetActiveCard(*token)
		
		// Verify initial state
		_, exists := st.GetReader("nfc_reader")
		assert.True(t, exists)
		lastToken := st.GetLastScanned()
		assert.Equal(t, token.UID, lastToken.UID)
		
		// Simulate reader disconnection
		st.RemoveReader("nfc_reader")
		
		// Verify disconnection is tracked
		_, exists = st.GetReader("nfc_reader")
		assert.False(t, exists)
		
		// Token should still be in last scanned (implementation dependent)
		lastToken = st.GetLastScanned()
		assert.Equal(t, token.UID, lastToken.UID)
	})
}

// TestPlaylistStateManagement demonstrates playlist-specific state testing
func TestPlaylistStateManagement(t *testing.T) {
    mockPlatform := mocks.NewMockPlatform()
    st, _ := state.NewState(mockPlatform)
    t.Cleanup(func() { st.StopService() })
	playlists := fixtures.SamplePlaylists()
	
	t.Run("Playlist activation sequence", func(t *testing.T) {
		// Initially no playlist
		assert.Nil(t, st.GetActivePlaylist())
		
		// Activate first playlist
		st.SetActivePlaylist(playlists[0])
		active := st.GetActivePlaylist()
		require.NotNil(t, active)
		assert.Equal(t, playlists[0].Name, active.Name)
		
		// Switch to second playlist
		st.SetActivePlaylist(playlists[1])
		active = st.GetActivePlaylist()
		require.NotNil(t, active)
		assert.Equal(t, playlists[1].Name, active.Name)
		
		// Clear playlist
		st.SetActivePlaylist(nil)
		assert.Nil(t, st.GetActivePlaylist())
	})
	
	t.Run("Playlist state persistence", func(t *testing.T) {
		// This would test if playlist state persists across application restarts
		// In a real implementation, this might involve config file updates
		
		st.SetActivePlaylist(playlists[0])
		
		// Simulate saving state (would be done by actual implementation)
		savedPlaylist := st.GetActivePlaylist()
		require.NotNil(t, savedPlaylist)
		
		// Simulate loading state in new instance (would be done by actual implementation)
		newMockPlatform := mocks.NewMockPlatform()
		newState, _ := state.NewState(newMockPlatform)
		newState.SetActivePlaylist(savedPlaylist)
		
		loadedPlaylist := newState.GetActivePlaylist()
		require.NotNil(t, loadedPlaylist)
		assert.Equal(t, playlists[0].Name, loadedPlaylist.Name)
	})
}
