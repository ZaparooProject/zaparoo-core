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
	"encoding/json"
	"testing"

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

func TestCursorEncodeDecycle(t *testing.T) {
	tests := []struct {
		expected *int64
		name     string
		lastID   int64
	}{
		{
			name:     "positive ID",
			lastID:   12345,
			expected: &[]int64{12345}[0],
		},
		{
			name:     "zero ID",
			lastID:   0,
			expected: &[]int64{0}[0],
		},
		{
			name:     "large ID",
			lastID:   9223372036854775807, // max int64
			expected: &[]int64{9223372036854775807}[0],
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Encode the cursor
			cursor, err := encodeCursor(tt.lastID)
			require.NoError(t, err, "Should encode cursor without error")
			assert.NotEmpty(t, cursor, "Encoded cursor should not be empty")

			// Decode the cursor
			decoded, err := decodeCursor(cursor)
			require.NoError(t, err, "Should decode without error")
			require.NotNil(t, decoded, "Decoded cursor should not be nil")
			assert.Equal(t, *tt.expected, *decoded, "Decoded value should match original")
		})
	}
}

func TestDecodeCursor_InvalidInputs(t *testing.T) {
	tests := []struct {
		name        string
		cursor      string
		expectError bool
	}{
		{
			name:        "empty cursor",
			cursor:      "",
			expectError: false, // empty cursor is valid (returns nil)
		},
		{
			name:        "invalid base64",
			cursor:      "invalid-base64!",
			expectError: true,
		},
		{
			name:        "invalid JSON",
			cursor:      "aW52YWxpZCBqc29u", // base64 for "invalid json"
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decoded, err := decodeCursor(tt.cursor)
			if tt.expectError {
				require.Error(t, err, "Should return error for invalid cursor")
				assert.Nil(t, decoded, "Should return nil for invalid cursor")
			} else {
				assert.NoError(t, err, "Should not return error for valid cursor")
				if tt.cursor == "" {
					assert.Nil(t, decoded, "Should return nil for empty cursor")
				}
			}
		})
	}
}

func TestHandleMediaSearch_WithoutCursor(t *testing.T) {
	// Setup mocks
	mockUserDB := &helpers.MockUserDBI{}
	mockMediaDB := &helpers.MockMediaDBI{}
	mockPlatform := mocks.NewMockPlatform()

	// Setup search results with cursor data
	expectedResults := []database.SearchResultWithCursor{
		{SystemID: "NES", Name: "Mario Bros", Path: "/games/mario.nes", MediaID: 1},
		{SystemID: "SNES", Name: "Super Mario", Path: "/games/super-mario.sfc", MediaID: 2},
	}

	mockMediaDB.On("SearchMediaWithFilters",
		mock.Anything, // context
		mock.MatchedBy(func(filters *database.SearchFilters) bool {
			// Check the filter parameters match what we expect
			return filters.Query == "mario" &&
				filters.Cursor == nil &&
				filters.Limit == 101 &&
				len(filters.Tags) == 0 // No tags for this test
		}),
	).Return(expectedResults, nil)

	mockPlatform.On("NormalizePath", mock.Anything, "/games/mario.nes").Return("/games/mario.nes")
	mockPlatform.On("NormalizePath", mock.Anything, "/games/super-mario.sfc").Return("/games/super-mario.sfc")

	// Create request without cursor (initial request)
	params := models.SearchParams{
		Query: "mario",
	}
	paramsJSON, err := json.Marshal(params)
	require.NoError(t, err)

	// Create state
	appState, _ := state.NewState(mockPlatform)

	env := requests.RequestEnv{
		Params: paramsJSON,
		Database: &database.Database{
			UserDB:  mockUserDB,
			MediaDB: mockMediaDB,
		},
		Platform: mockPlatform,
		State:    appState,
		Config:   &config.Instance{},
		ClientID: "127.0.0.1:12345",
	}

	// Execute
	result, err := HandleMediaSearch(env)
	require.NoError(t, err)

	// Check if mocks were called at all
	t.Logf("Mock calls made: %v", mockMediaDB.Calls)

	// Verify response format with cursor-based pagination
	searchResults, ok := result.(models.SearchResults)
	require.True(t, ok, "Should return SearchResults")

	// Log the actual results for debugging
	t.Logf("Got %d results, expected 2", len(searchResults.Results))
	t.Logf("Total: %d", searchResults.Total)

	assert.Len(t, searchResults.Results, 2, "Should return 2 results")
	assert.Equal(t, len(searchResults.Results), searchResults.Total,
		"Total should equal result count (deprecated field)")
	assert.NotNil(t, searchResults.Pagination, "Pagination should be present")
	assert.False(t, searchResults.Pagination.HasNextPage, "Should not have next page with only 2 results")
	assert.Nil(t, searchResults.Pagination.NextCursor, "NextCursor should be nil when no more pages")

	// Verify mock was called
	mockMediaDB.AssertExpectations(t)
	mockPlatform.AssertExpectations(t)

	// Verify first result (only if we have results)
	if len(searchResults.Results) > 0 {
		assert.Equal(t, "NES", searchResults.Results[0].System.ID)
		assert.Equal(t, "Mario Bros", searchResults.Results[0].Name)
		assert.Equal(t, "/games/mario.nes", searchResults.Results[0].Path)
	}
}

func TestHandleMediaSearch_WithCursor(t *testing.T) {
	// Setup mocks
	mockUserDB := &helpers.MockUserDBI{}
	mockMediaDB := &helpers.MockMediaDBI{}
	mockPlatform := mocks.NewMockPlatform()

	// Setup cursor-based search results
	expectedResults := []database.SearchResultWithCursor{
		{SystemID: "NES", Name: "Mario Bros", Path: "/games/mario.nes", MediaID: 100},
		{SystemID: "SNES", Name: "Super Mario", Path: "/games/super-mario.sfc", MediaID: 101},
		// Extra result to test hasNextPage
		{SystemID: "Nintendo64", Name: "Mario 64", Path: "/games/mario64.n64", MediaID: 102},
	}

	cursor := int64(50)
	limit := 3 // maxResults + 1

	mockMediaDB.On("SearchMediaWithFilters",
		mock.Anything, // context
		mock.MatchedBy(func(filters *database.SearchFilters) bool {
			// Check the filter parameters match what we expect
			return filters.Query == "mario" &&
				filters.Cursor != nil && *filters.Cursor == cursor &&
				filters.Limit == limit &&
				len(filters.Tags) == 0 && // No tags for this test
				len(filters.Systems) > 0 // Should have systems
		}),
	).Return(expectedResults, nil)

	mockPlatform.On("NormalizePath", mock.Anything, "/games/mario.nes").Return("/games/mario.nes")
	mockPlatform.On("NormalizePath", mock.Anything, "/games/super-mario.sfc").Return("/games/super-mario.sfc")
	mockPlatform.On("NormalizePath", mock.Anything, "/games/mario64.n64").Return("/games/mario64.n64")

	// Create request with cursor
	cursorStr, err := encodeCursor(50)
	require.NoError(t, err)
	params := models.SearchParams{
		Query:      "mario",
		MaxResults: &[]int{2}[0], // Request 2 results
		Cursor:     &cursorStr,
	}
	paramsJSON, err := json.Marshal(params)
	require.NoError(t, err)

	// Create state
	appState, _ := state.NewState(mockPlatform)

	env := requests.RequestEnv{
		Params: paramsJSON,
		Database: &database.Database{
			UserDB:  mockUserDB,
			MediaDB: mockMediaDB,
		},
		Platform: mockPlatform,
		State:    appState,
		Config:   &config.Instance{},
		ClientID: "127.0.0.1:12345",
	}

	// Execute
	result, err := HandleMediaSearch(env)
	require.NoError(t, err)

	// Verify cursor-based response format
	searchResults, ok := result.(models.SearchResults)
	require.True(t, ok, "Should return SearchResults")

	assert.Len(t, searchResults.Results, 2, "Should return 2 results (maxResults)")
	assert.Equal(t, len(searchResults.Results), searchResults.Total,
		"Total should equal result count (deprecated field)")
	assert.NotNil(t, searchResults.Pagination, "Pagination should not be nil for cursor requests")

	// Verify pagination info
	assert.True(t, searchResults.Pagination.HasNextPage, "Should have next page")
	assert.Equal(t, 2, searchResults.Pagination.PageSize, "Page size should match maxResults")
	assert.NotNil(t, searchResults.Pagination.NextCursor, "Should have next cursor")

	// Verify next cursor contains last result's MediaID
	decodedCursor, err := decodeCursor(*searchResults.Pagination.NextCursor)
	require.NoError(t, err)
	assert.Equal(t, int64(101), *decodedCursor, "Next cursor should contain last returned result's MediaID")
}

func TestHandleMediaSearch_InvalidCursor(t *testing.T) {
	// Create request with invalid cursor
	invalidCursor := "invalid-cursor"
	params := models.SearchParams{
		Query:  "mario",
		Cursor: &invalidCursor,
	}
	paramsJSON, err := json.Marshal(params)
	require.NoError(t, err)

	// Create a minimal state for the test
	mockPlatform := mocks.NewMockPlatform()
	appState, _ := state.NewState(mockPlatform)

	env := requests.RequestEnv{
		Params:   paramsJSON,
		ClientID: "127.0.0.1:12345",
		State:    appState,
	}

	// Execute
	result, err := HandleMediaSearch(env)
	require.Error(t, err, "Should return error for invalid cursor")
	assert.Nil(t, result, "Should return nil result for invalid cursor")
	assert.Contains(t, err.Error(), "invalid cursor", "Error should mention invalid cursor")
}

func TestHandleMediaTags_Success(t *testing.T) {
	// Setup mocks
	mockUserDB := &helpers.MockUserDBI{}
	mockMediaDB := &helpers.MockMediaDBI{}
	mockPlatform := mocks.NewMockPlatform()

	// Setup expected tag results
	expectedTags := []database.TagInfo{
		{Type: "genre", Tag: "Action"},
		{Type: "genre", Tag: "Adventure"},
		{Type: "genre", Tag: "RPG"},
		{Type: "year", Tag: "1990"},
		{Type: "year", Tag: "1991"},
	}

	mockMediaDB.On("GetTags",
		mock.Anything, // context
		mock.MatchedBy(func(systems []systemdefs.System) bool {
			// Verify systems are set correctly
			return len(systems) > 0
		}),
	).Return(expectedTags, nil)

	// Create request with systems
	params := models.SearchParams{
		Systems: &[]string{"NES", "SNES"},
	}
	paramsJSON, err := json.Marshal(params)
	require.NoError(t, err)

	// Create state
	appState, _ := state.NewState(mockPlatform)

	env := requests.RequestEnv{
		Params: paramsJSON,
		Database: &database.Database{
			UserDB:  mockUserDB,
			MediaDB: mockMediaDB,
		},
		Platform: mockPlatform,
		State:    appState,
		Config:   &config.Instance{},
		ClientID: "127.0.0.1:12345",
	}

	// Execute
	result, err := HandleMediaTags(env)
	require.NoError(t, err)

	// Verify response format
	tagsResponse, ok := result.(models.TagsResponse)
	require.True(t, ok, "Should return TagsResponse")

	// Verify tags structure
	assert.Len(t, tagsResponse.Tags, 5, "Should return 5 tags")
	assert.Equal(t, expectedTags, tagsResponse.Tags, "Should return expected tags")

	// Verify mock was called
	mockMediaDB.AssertExpectations(t)
}

func TestHandleMediaTags_NoParams(t *testing.T) {
	// Setup mocks
	mockUserDB := &helpers.MockUserDBI{}
	mockMediaDB := &helpers.MockMediaDBI{}
	mockPlatform := mocks.NewMockPlatform()

	// Setup expected tag results for all systems
	expectedTags := []database.TagInfo{
		{Type: "genre", Tag: "Action"},
		{Type: "genre", Tag: "RPG"},
	}

	mockMediaDB.On("GetTags",
		mock.Anything, // context
		mock.MatchedBy(func(systems []systemdefs.System) bool {
			// When no systems are specified, should get all systems
			return len(systems) > 0
		}),
	).Return(expectedTags, nil)

	// Create state
	appState, _ := state.NewState(mockPlatform)

	env := requests.RequestEnv{
		Params: []byte("{}"), // Empty params should still work
		Database: &database.Database{
			UserDB:  mockUserDB,
			MediaDB: mockMediaDB,
		},
		Platform: mockPlatform,
		State:    appState,
		Config:   &config.Instance{},
		ClientID: "127.0.0.1:12345",
	}

	// Execute
	result, err := HandleMediaTags(env)
	require.NoError(t, err)

	// Verify response format
	tagsResponse, ok := result.(models.TagsResponse)
	require.True(t, ok, "Should return TagsResponse")
	assert.Equal(t, expectedTags, tagsResponse.Tags)

	// Verify mock was called
	mockMediaDB.AssertExpectations(t)
}
