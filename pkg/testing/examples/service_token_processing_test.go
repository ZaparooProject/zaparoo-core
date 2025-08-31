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

package examples

import (
	"errors"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/pkg/service/tokens"
	"github.com/ZaparooProject/zaparoo-core/pkg/testing/fixtures"
	"github.com/ZaparooProject/zaparoo-core/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTokenProcessingMockIntegration demonstrates how all the mocks work together
// This test shows the TDD infrastructure capabilities without testing actual service logic
func TestTokenProcessingMockIntegration(t *testing.T) {
	t.Parallel()
	// Setup all mocks
	mockUserDB := helpers.NewMockUserDBI()
	mockMediaDB := helpers.NewMockMediaDBI()
	mockPlatform := mocks.NewMockPlatform()
	mockReader := mocks.NewMockReader()

	// Use a minimal in-memory config instance to avoid filesystem writes
	cfg := &config.Instance{}

	// Create test fixtures
	testTokens := fixtures.NewTokenCollection()
	testMedia := fixtures.NewMediaCollection()
	nfcToken := testTokens.NFC

	// Test 1: Reader operations
	mockReader.On("Write", nfcToken.Text).Return(nfcToken, nil)
	mockReader.On("Connected").Return(true)

	// Verify reader works
	assert.True(t, mockReader.Connected())
	scannedToken, err := mockReader.Write(nfcToken.Text)
	require.NoError(t, err)
	assert.Equal(t, nfcToken.Text, scannedToken.Text)

	// Test 2: Database operations
	testDBMedia := database.Media{Path: testMedia.RetroGame.Path, DBID: 1}
	mockMediaDB.On("GetMediaByText", nfcToken.Text).Return(testDBMedia, nil)

	// Verify database lookup works
	foundMedia, err := mockMediaDB.GetMediaByText(nfcToken.Text)
	require.NoError(t, err)
	assert.Equal(t, testDBMedia.Path, foundMedia.Path)
	assert.Equal(t, testDBMedia.DBID, foundMedia.DBID)

	// Test 3: Platform operations
	mockPlatform.On("LaunchMedia", cfg, testDBMedia.Path).Return(nil)
	mockPlatform.On("ID").Return("test-platform")

	// Verify platform works
	assert.Equal(t, "test-platform", mockPlatform.ID())
	err = mockPlatform.LaunchMedia(cfg, testDBMedia.Path)
	require.NoError(t, err)

	// Verify launch tracking
	launchedMedia := mockPlatform.GetLaunchedMedia()
	assert.Len(t, launchedMedia, 1)
	assert.Contains(t, launchedMedia, testDBMedia.Path)

	// Test 4: Database history tracking
	historyEntry := database.HistoryEntry{
		Time:       nfcToken.ScanTime,
		Type:       "launch",
		TokenID:    nfcToken.UID,
		TokenValue: nfcToken.Text,
		Success:    true,
	}
	mockUserDB.On("AddHistory", helpers.HistoryEntryMatcher()).Return(nil)

	// Verify history tracking works
	err = mockUserDB.AddHistory(&historyEntry)
	require.NoError(t, err)

	// Cleanup: Close the reader (important for proper resource cleanup in success cases)
	err = mockReader.Close()
	require.NoError(t, err)

	// Verify all expectations were met
	mockReader.AssertExpectations(t)
	mockMediaDB.AssertExpectations(t)
	mockPlatform.AssertExpectations(t)
	mockUserDB.AssertExpectations(t)
}

// TestErrorHandlingWithMocks demonstrates testing error conditions with mocks
func TestErrorHandlingWithMocks(t *testing.T) {
	t.Parallel()
	tests := []struct {
		testFunc func(t *testing.T)
		name     string
	}{
		{
			name: "Database connection error",
			testFunc: func(t *testing.T) {
				mockUserDB := helpers.NewMockUserDBI()
				mockUserDB.On("Open").Return(errors.New("connection failed"))

				err := mockUserDB.Open()
				require.Error(t, err)
				assert.Contains(t, err.Error(), "connection failed")
				mockUserDB.AssertExpectations(t)
			},
		},
		{
			name: "Media search error",
			testFunc: func(t *testing.T) {
				mockMediaDB := helpers.NewMockMediaDBI()
				mockMediaDB.On("GetMediaByText", "nonexistent").Return(database.Media{}, errors.New("media not found"))

				_, err := mockMediaDB.GetMediaByText("nonexistent")
				require.Error(t, err)
				assert.Contains(t, err.Error(), "media not found")
				mockMediaDB.AssertExpectations(t)
			},
		},
		{
			name: "Platform launch failure",
			testFunc: func(t *testing.T) {
				mockPlatform := mocks.NewMockPlatform()
				// Use minimal config to avoid disk I/O
				cfg := &config.Instance{}

				mockPlatform.On("LaunchMedia", cfg, "/invalid/path").Return(errors.New("launch failed"))

				err := mockPlatform.LaunchMedia(cfg, "/invalid/path")
				require.Error(t, err)
				assert.Contains(t, err.Error(), "launch failed")
				mockPlatform.AssertExpectations(t)
			},
		},
		{
			name: "Reader write failure",
			testFunc: func(t *testing.T) {
				mockReader := mocks.NewMockReader()
				mockReader.On("Write", "invalid").Return((*tokens.Token)(nil), errors.New("write failed"))

				_, err := mockReader.Write("invalid")
				require.Error(t, err)
				assert.Contains(t, err.Error(), "write failed")

				// In error scenarios, Close() might not be called depending on the error handling strategy
				mockReader.AssertExpectations(t)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tt.testFunc(t)
		})
	}
}

// TestFixtureAndMockCompatibility verifies fixtures work correctly with mocks
func TestFixtureAndMockCompatibility(t *testing.T) {
	t.Parallel()
	// Test that fixtures provide the right types for mocks
	tokenCollection := fixtures.NewTokenCollection()
	mediaCollection := fixtures.NewMediaCollection()

	// Verify token fixtures
	assert.NotNil(t, tokenCollection.NFC)
	assert.NotNil(t, tokenCollection.Mifare)
	assert.NotNil(t, tokenCollection.Amiibo)
	assert.True(t, tokenCollection.Unsafe.Unsafe)

	// Verify media fixtures
	allMedia := mediaCollection.AllMedia()
	assert.Len(t, allMedia, 5)
	assert.NotEmpty(t, mediaCollection.RetroGame.Path)
	assert.NotEmpty(t, mediaCollection.RetroGame.Name)

	// Test using fixtures with mocks
	mockMediaDB := helpers.NewMockMediaDBI()
	testToken := tokenCollection.NFC

	// Convert ActiveMedia fixture to database.Media for mock compatibility
	expectedMedia := database.Media{
		Path: mediaCollection.RetroGame.Path,
		DBID: 1,
	}

	mockMediaDB.On("GetMediaByText", testToken.Text).Return(expectedMedia, nil)

	// Verify the integration works
	foundMedia, err := mockMediaDB.GetMediaByText(testToken.Text)
	require.NoError(t, err)
	assert.Equal(t, expectedMedia.Path, foundMedia.Path)
	assert.Equal(t, expectedMedia.DBID, foundMedia.DBID)

	mockMediaDB.AssertExpectations(t)
}
