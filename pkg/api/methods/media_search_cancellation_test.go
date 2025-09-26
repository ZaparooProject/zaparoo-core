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
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// MockMediaDBWithDelay simulates slow database queries for testing cancellation
type MockMediaDBWithDelay struct {
	*helpers.MockMediaDBI
	delay time.Duration
}

func (m *MockMediaDBWithDelay) SearchMediaPathWordsWithCursor(
	ctx context.Context, systems []systemdefs.System, query string, cursor *int64, limit int,
) ([]database.SearchResultWithCursor, error) {
	// Simulate slow query with controllable delay
	select {
	case <-time.After(m.delay):
		// Normal execution after delay
		args := m.Called(ctx, systems, query, cursor, limit)
		results, ok := args.Get(0).([]database.SearchResultWithCursor)
		if !ok {
			if err := args.Error(1); err != nil {
				return nil, fmt.Errorf("mock SearchMediaPathWordsWithCursor error: %w", err)
			}
			return nil, nil
		}
		if err := args.Error(1); err != nil {
			return nil, fmt.Errorf("mock SearchMediaPathWordsWithCursor error: %w", err)
		}
		return results, nil
	case <-ctx.Done():
		// Context was cancelled during delay
		return nil, ctx.Err()
	}
}

func (m *MockMediaDBWithDelay) SearchMediaWithFilters(
	ctx context.Context,
	filters *database.SearchFilters,
) ([]database.SearchResultWithCursor, error) {
	// Simulate slow query with controllable delay
	select {
	case <-time.After(m.delay):
		// Delegate to embedded MockMediaDBI
		result, err := m.MockMediaDBI.SearchMediaWithFilters(ctx, filters)
		if err != nil {
			return nil, fmt.Errorf("mock SearchMediaWithFilters failed: %w", err)
		}
		return result, nil
	case <-ctx.Done():
		// Context was cancelled during delay
		return nil, ctx.Err()
	}
}

func (m *MockMediaDBWithDelay) GetTagFacets(
	ctx context.Context,
	filters *database.SearchFilters,
) ([]database.TagTypeFacet, error) {
	// Simulate slow query with controllable delay
	select {
	case <-time.After(m.delay):
		// Delegate to embedded MockMediaDBI
		result, err := m.MockMediaDBI.GetTagFacets(ctx, filters)
		if err != nil {
			return nil, fmt.Errorf("mock GetTagFacets failed: %w", err)
		}
		return result, nil
	case <-ctx.Done():
		// Context was cancelled during delay
		return nil, ctx.Err()
	}
}

func TestMediaSearchCancellation_RapidSearches(t *testing.T) {
	// Setup mocks with delay to simulate slow queries
	mockUserDB := &helpers.MockUserDBI{}
	mockMediaDB := &MockMediaDBWithDelay{
		MockMediaDBI: &helpers.MockMediaDBI{},
		delay:        100 * time.Millisecond, // 100ms delay to simulate slow query
	}
	mockPlatform := mocks.NewMockPlatform()

	// Setup expected results for searches
	expectedResults := []database.SearchResultWithCursor{
		{SystemID: "NES", Name: "Apple Game", Path: "/games/apple.nes", MediaID: 1},
	}

	// The first search should be cancelled, so we don't expect it to complete
	// Only the last search should complete successfully
	mockMediaDB.On("SearchMediaPathWordsWithCursor",
		mock.Anything, // context (will be cancelled for first search)
		mock.Anything, // systems
		"apple",       // final query
		(*int64)(nil), // cursor
		101,           // limit
	).Return(expectedResults, nil).Once()

	mockPlatform.On("NormalizePath", mock.Anything, "/games/apple.nes").Return("/games/apple.nes")

	// Create state
	appState, _ := state.NewState(mockPlatform)

	clientID := "127.0.0.1:12345"

	// First search: "a" - should be cancelled
	params1 := models.SearchParams{Query: "a"}
	paramsJSON1, err := json.Marshal(params1)
	require.NoError(t, err)

	env1 := requests.RequestEnv{
		Params: paramsJSON1,
		Database: &database.Database{
			UserDB:  mockUserDB,
			MediaDB: mockMediaDB,
		},
		Platform: mockPlatform,
		State:    appState,
		Config:   &config.Instance{},
		ClientID: clientID,
	}

	// Start first search in goroutine
	var wg sync.WaitGroup
	var firstResult any
	var firstErr error

	wg.Add(1)
	go func() {
		defer wg.Done()
		firstResult, firstErr = HandleMediaSearch(env1)
	}()

	// Give first search time to start
	time.Sleep(10 * time.Millisecond)

	// Second search: "apple" - should complete successfully and cancel the first
	params2 := models.SearchParams{Query: "apple"}
	paramsJSON2, err := json.Marshal(params2)
	require.NoError(t, err)

	env2 := requests.RequestEnv{
		Params: paramsJSON2,
		Database: &database.Database{
			UserDB:  mockUserDB,
			MediaDB: mockMediaDB,
		},
		Platform: mockPlatform,
		State:    appState,
		Config:   &config.Instance{},
		ClientID: clientID, // Same client
	}

	// Execute second search
	secondResult, secondErr := HandleMediaSearch(env2)

	// Wait for first search to complete (should be cancelled)
	wg.Wait()

	// Verify first search was cancelled
	require.Error(t, firstErr, "First search should be cancelled")
	assert.Contains(t, firstErr.Error(), "search cancelled by newer request", "First search should be cancelled")
	assert.Nil(t, firstResult, "First search should return nil result")

	// Verify second search completed successfully
	require.NoError(t, secondErr, "Second search should complete successfully")
	require.NotNil(t, secondResult, "Second search should return results")

	searchResults, ok := secondResult.(models.SearchResults)
	require.True(t, ok, "Should return SearchResults")
	assert.Len(t, searchResults.Results, 1, "Should return 1 result")
	assert.Equal(t, "Apple Game", searchResults.Results[0].Name)

	// Verify mocks
	mockPlatform.AssertExpectations(t)
}

func TestMediaSearchCancellation_DifferentClients(t *testing.T) {
	// Setup mocks
	mockUserDB := &helpers.MockUserDBI{}
	mockMediaDB := &MockMediaDBWithDelay{
		MockMediaDBI: &helpers.MockMediaDBI{},
		delay:        50 * time.Millisecond,
	}
	mockPlatform := mocks.NewMockPlatform()

	// Setup expected results for both searches
	expectedResults1 := []database.SearchResultWithCursor{
		{SystemID: "NES", Name: "Mario Game", Path: "/games/mario.nes", MediaID: 1},
	}
	expectedResults2 := []database.SearchResultWithCursor{
		{SystemID: "SNES", Name: "Zelda Game", Path: "/games/zelda.sfc", MediaID: 2},
	}

	// Both searches should complete (different clients)
	mockMediaDB.On("SearchMediaPathWordsWithCursor",
		mock.Anything, // context
		mock.Anything, // systems
		"mario",       // query
		(*int64)(nil), // cursor
		101,           // limit
	).Return(expectedResults1, nil).Once()

	mockMediaDB.On("SearchMediaPathWordsWithCursor",
		mock.Anything, // context
		mock.Anything, // systems
		"zelda",       // query
		(*int64)(nil), // cursor
		101,           // limit
	).Return(expectedResults2, nil).Once()

	mockPlatform.On("NormalizePath", mock.Anything, "/games/mario.nes").Return("/games/mario.nes")
	mockPlatform.On("NormalizePath", mock.Anything, "/games/zelda.sfc").Return("/games/zelda.sfc")

	// Create state
	appState, _ := state.NewState(mockPlatform)

	// Different client IDs
	clientID1 := "127.0.0.1:12345"
	clientID2 := "127.0.0.1:12346"

	// First search from client 1
	params1 := models.SearchParams{Query: "mario"}
	paramsJSON1, err := json.Marshal(params1)
	require.NoError(t, err)

	env1 := requests.RequestEnv{
		Params: paramsJSON1,
		Database: &database.Database{
			UserDB:  mockUserDB,
			MediaDB: mockMediaDB,
		},
		Platform: mockPlatform,
		State:    appState,
		Config:   &config.Instance{},
		ClientID: clientID1,
	}

	// Second search from client 2
	params2 := models.SearchParams{Query: "zelda"}
	paramsJSON2, err := json.Marshal(params2)
	require.NoError(t, err)

	env2 := requests.RequestEnv{
		Params: paramsJSON2,
		Database: &database.Database{
			UserDB:  mockUserDB,
			MediaDB: mockMediaDB,
		},
		Platform: mockPlatform,
		State:    appState,
		Config:   &config.Instance{},
		ClientID: clientID2,
	}

	// Execute both searches concurrently
	var wg sync.WaitGroup
	var result1 any
	var err1 error
	var result2 any
	var err2 error

	wg.Add(2)

	go func() {
		defer wg.Done()
		result1, err1 = HandleMediaSearch(env1)
	}()

	go func() {
		defer wg.Done()
		result2, err2 = HandleMediaSearch(env2)
	}()

	wg.Wait()

	// Both searches should complete successfully (different clients)
	require.NoError(t, err1, "First search should complete successfully")
	require.NoError(t, err2, "Second search should complete successfully")
	require.NotNil(t, result1, "First search should return results")
	require.NotNil(t, result2, "Second search should return results")

	searchResults1, ok := result1.(models.SearchResults)
	require.True(t, ok, "Should return SearchResults for first client")
	assert.Len(t, searchResults1.Results, 1, "Should return 1 result for first client")
	assert.Equal(t, "Mario Game", searchResults1.Results[0].Name)

	searchResults2, ok := result2.(models.SearchResults)
	require.True(t, ok, "Should return SearchResults for second client")
	assert.Len(t, searchResults2.Results, 1, "Should return 1 result for second client")
	assert.Equal(t, "Zelda Game", searchResults2.Results[0].Name)

	// Verify mocks
	mockPlatform.AssertExpectations(t)
}

func TestMediaSearchCancellation_EmptyClientID(t *testing.T) {
	// Setup mocks
	mockUserDB := &helpers.MockUserDBI{}
	mockMediaDB := &helpers.MockMediaDBI{}
	mockPlatform := mocks.NewMockPlatform()

	expectedResults := []database.SearchResultWithCursor{
		{SystemID: "NES", Name: "Test Game", Path: "/games/test.nes", MediaID: 1},
	}

	mockMediaDB.On("SearchMediaPathWordsWithCursor",
		mock.Anything, // context
		mock.Anything, // systems
		"test",        // query
		(*int64)(nil), // cursor
		101,           // limit
	).Return(expectedResults, nil)

	mockPlatform.On("NormalizePath", mock.Anything, "/games/test.nes").Return("/games/test.nes")

	// Create state
	appState, _ := state.NewState(mockPlatform)

	params := models.SearchParams{Query: "test"}
	paramsJSON, err := json.Marshal(params)
	require.NoError(t, err)

	env := requests.RequestEnv{
		Params: paramsJSON,
		Database: &database.Database{
			UserDB:  mockUserDB,
			MediaDB: mockMediaDB,
		},
		Platform: mockPlatform,
		State:    appState,
		Config:   &config.Instance{},
		ClientID: "", // Empty client ID
	}

	// Execute search - should work without cancellation tracking
	result, err := HandleMediaSearch(env)
	require.NoError(t, err, "Search should complete even without client ID")
	require.NotNil(t, result, "Should return results")

	searchResults, ok := result.(models.SearchResults)
	require.True(t, ok, "Should return SearchResults")
	assert.Len(t, searchResults.Results, 1, "Should return 1 result")
	assert.Equal(t, "Test Game", searchResults.Results[0].Name)

	// Verify mocks
	mockMediaDB.AssertExpectations(t)
	mockPlatform.AssertExpectations(t)
}

func TestMediaSearchCancellation_CleanupOnCompletion(t *testing.T) {
	// Setup mocks
	mockUserDB := &helpers.MockUserDBI{}
	mockMediaDB := &helpers.MockMediaDBI{}
	mockPlatform := mocks.NewMockPlatform()

	expectedResults := []database.SearchResultWithCursor{
		{SystemID: "NES", Name: "Cleanup Test", Path: "/games/cleanup.nes", MediaID: 1},
	}

	mockMediaDB.On("SearchMediaPathWordsWithCursor",
		mock.Anything, // context
		mock.Anything, // systems
		"cleanup",     // query
		(*int64)(nil), // cursor
		101,           // limit
	).Return(expectedResults, nil)

	mockPlatform.On("NormalizePath", mock.Anything, "/games/cleanup.nes").Return("/games/cleanup.nes")

	// Create state
	appState, _ := state.NewState(mockPlatform)

	clientID := "127.0.0.1:99999"
	params := models.SearchParams{Query: "cleanup"}
	paramsJSON, err := json.Marshal(params)
	require.NoError(t, err)

	env := requests.RequestEnv{
		Params: paramsJSON,
		Database: &database.Database{
			UserDB:  mockUserDB,
			MediaDB: mockMediaDB,
		},
		Platform: mockPlatform,
		State:    appState,
		Config:   &config.Instance{},
		ClientID: clientID,
	}

	// Verify client is not in the map before search
	searchCancelsMu.RLock()
	_, existsBefore := activeSearchCancels[clientID]
	searchCancelsMu.RUnlock()
	assert.False(t, existsBefore, "Client should not be in cancellation map before search")

	// Execute search
	result, err := HandleMediaSearch(env)
	require.NoError(t, err, "Search should complete successfully")
	require.NotNil(t, result, "Should return results")

	// Verify client is cleaned up from the map after search completion
	searchCancelsMu.RLock()
	_, existsAfter := activeSearchCancels[clientID]
	searchCancelsMu.RUnlock()
	assert.False(t, existsAfter, "Client should be cleaned up from cancellation map after search completion")

	// Verify mocks
	mockMediaDB.AssertExpectations(t)
	mockPlatform.AssertExpectations(t)
}

func TestMediaSearchCancellation_MultipleRapidSearchesSameClient(t *testing.T) {
	// Test rapid succession of searches: "a" -> "ap" -> "app" -> "apple"
	// Only the last one should complete

	mockUserDB := &helpers.MockUserDBI{}
	mockMediaDB := &MockMediaDBWithDelay{
		MockMediaDBI: &helpers.MockMediaDBI{},
		delay:        200 * time.Millisecond, // Longer delay to ensure cancellation
	}
	mockPlatform := mocks.NewMockPlatform()

	expectedResults := []database.SearchResultWithCursor{
		{SystemID: "NES", Name: "Apple Pie Game", Path: "/games/apple-pie.nes", MediaID: 1},
	}

	// Any search that does complete should return results
	mockMediaDB.On("SearchMediaPathWordsWithCursor",
		mock.Anything, // context
		mock.Anything, // systems
		mock.Anything, // any query (a, ap, app, apple)
		(*int64)(nil), // cursor
		101,           // limit
	).Return(expectedResults, nil).Maybe()

	mockPlatform.On("NormalizePath", mock.Anything, "/games/apple-pie.nes").Return("/games/apple-pie.nes").Maybe()

	// Create state
	appState, _ := state.NewState(mockPlatform)

	clientID := "127.0.0.1:rapid"
	queries := []string{"a", "ap", "app", "apple"}

	var wg sync.WaitGroup
	results := make([]any, len(queries))
	errors := make([]error, len(queries))

	// Start all searches with small delays between them
	for i, query := range queries {
		wg.Add(1)
		go func(index int, q string) {
			defer wg.Done()

			// Small delay to simulate typing
			time.Sleep(time.Duration(index) * 20 * time.Millisecond)

			params := models.SearchParams{Query: q}
			paramsJSON, err := json.Marshal(params)
			if err != nil {
				t.Errorf("Failed to marshal params: %v", err)
				return
			}

			env := requests.RequestEnv{
				Params: paramsJSON,
				Database: &database.Database{
					UserDB:  mockUserDB,
					MediaDB: mockMediaDB,
				},
				Platform: mockPlatform,
				State:    appState,
				Config:   &config.Instance{},
				ClientID: clientID,
			}

			results[index], errors[index] = HandleMediaSearch(env)
		}(i, query)
	}

	wg.Wait()

	// Count how many searches were cancelled vs successful
	cancelledCount := 0
	successfulCount := 0

	for i := range queries {
		if errors[i] != nil && errors[i].Error() == "search cancelled by newer request" {
			cancelledCount++
			assert.Nil(t, results[i], "Search %d should return nil result when cancelled", i)
		} else if errors[i] == nil {
			successfulCount++
			assert.NotNil(t, results[i], "Search %d should return results when successful", i)
		}
	}

	// We should have at least one cancellation and at least one success
	assert.Positive(t, cancelledCount, "At least one search should be cancelled")
	assert.Positive(t, successfulCount, "At least one search should complete successfully")

	// Total should be all queries
	expected := cancelledCount + successfulCount
	assert.Len(t, queries, expected, "All queries should either be cancelled or successful")

	// Verify mocks
	mockPlatform.AssertExpectations(t)
}
