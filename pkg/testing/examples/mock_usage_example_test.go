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
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/fixtures"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// TestMockReaderUsage demonstrates how to use the MockReader with fixtures
func TestMockReaderUsage(t *testing.T) {
	t.Parallel()
	// Create a mock reader
	mockReader := &mocks.MockReader{}

	// Verify it implements the Reader interface
	var _ readers.Reader = mockReader

	// Set up the basic mock behavior
	mockReader.SetupBasicMock()

	// Test basic functionality - call all methods that were set up in SetupBasicMock
	metadata := mockReader.Metadata()
	assert.Equal(t, "mock-reader", metadata.ID)
	assert.True(t, metadata.DefaultEnabled)

	ids := mockReader.IDs()
	assert.Contains(t, ids, "mock:")

	connected := mockReader.Connected()
	assert.True(t, connected)

	device := mockReader.Device()
	assert.Equal(t, "mock://test-device", device)

	info := mockReader.Info()
	assert.Equal(t, "Mock Reader Test Device", info)

	capabilities := mockReader.Capabilities()
	assert.Contains(t, capabilities, readers.CapabilityWrite)

	// Test with fixtures
	testToken := fixtures.NewNFCToken()

	// Mock a successful write operation
	mockReader.On("Write", "test:game").Return(testToken, nil)

	// Perform the write
	result, err := mockReader.Write("test:game")
	require.NoError(t, err)
	assert.Equal(t, testToken, result)

	// Verify the mock was called as expected
	mockReader.AssertExpectations(t)
}

// TestMockPlatformUsage demonstrates how to use the MockPlatform with fixtures
func TestMockPlatformUsage(t *testing.T) {
	t.Parallel()
	// Create a mock platform
	mockPlatform := &mocks.MockPlatform{}

	// Verify it implements the Platform interface
	var _ platforms.Platform = mockPlatform

	// Set up specific mock expectations for this test (don't use SetupBasicMock)
	mockPlatform.On("ID").Return("mock-platform")

	// Test basic functionality
	id := mockPlatform.ID()
	assert.Equal(t, "mock-platform", id)

	// Test with fixtures
	testMedia := fixtures.NewRetroGame()

	// Mock a successful media launch
	mockPlatform.On("LaunchMedia", mock.AnythingOfType("*config.Instance"), testMedia.Path).Return(nil)

	// Perform the launch (we'd need a config instance in a real test)
	err := mockPlatform.LaunchMedia(nil, testMedia.Path)
	require.NoError(t, err)

	// Verify the launch was tracked
	launchedMedia := mockPlatform.GetLaunchedMedia()
	assert.Contains(t, launchedMedia, testMedia.Path)

	// Test keyboard press tracking
	mockPlatform.On("KeyboardPress", "enter").Return(nil)

	err = mockPlatform.KeyboardPress("enter")
	require.NoError(t, err)

	presses := mockPlatform.GetKeyboardPresses()
	assert.Contains(t, presses, "enter")

	// Verify the mock was called as expected
	mockPlatform.AssertExpectations(t)
}

// TestFixtureCollections demonstrates using the fixture collections
func TestFixtureCollections(t *testing.T) {
	t.Parallel()
	// Test token collections
	tokenCollection := fixtures.NewTokenCollection()

	allTokens := tokenCollection.AllTokens()
	assert.Len(t, allTokens, 6) // Should have 6 tokens

	safeTokens := tokenCollection.SafeTokens()
	assert.Len(t, safeTokens, 5) // Should have 5 safe tokens (excluding unsafe)

	hardwareTokens := tokenCollection.HardwareTokens()
	assert.Len(t, hardwareTokens, 5) // Should have 5 hardware tokens (excluding API)

	// Verify the unsafe token is properly marked
	assert.True(t, tokenCollection.Unsafe.Unsafe)
	assert.True(t, tokenCollection.API.FromAPI)

	// Test media collections
	mediaCollection := fixtures.NewMediaCollection()

	allMedia := mediaCollection.AllMedia()
	assert.Len(t, allMedia, 5) // Should have 5 media entries

	retroMedia := mediaCollection.RetroMedia()
	assert.Len(t, retroMedia, 4) // Should have 4 retro media entries (excluding modern)

	systemIDs := mediaCollection.SystemIDs()
	assert.Contains(t, systemIDs, "nes")
	assert.Contains(t, systemIDs, "arcade")

	// Test lookup functionality
	nesMedia := mediaCollection.MediaBySystemID("nes")
	assert.NotNil(t, nesMedia)
	assert.Equal(t, "Super Mario Bros.", nesMedia.Name)

	mameMedia := mediaCollection.MediaByLauncherID("mame")
	assert.NotNil(t, mameMedia)
	assert.Equal(t, "Pac-Man", mameMedia.Name)
}

// TestMockAndFixtureIntegration demonstrates using mocks and fixtures together
func TestMockAndFixtureIntegration(t *testing.T) {
	t.Parallel()
	// Create mocks
	mockReader := &mocks.MockReader{}
	mockPlatform := &mocks.MockPlatform{}

	// Create fixtures
	tokenCollection := fixtures.NewTokenCollection()
	mediaCollection := fixtures.NewMediaCollection()

	// Set up mock behavior using fixtures
	testToken := tokenCollection.NFC
	testMedia := mediaCollection.RetroGame

	// Mock reader scanning the NFC token
	mockReader.On("Connected").Return(true)
	mockReader.On("Write", testToken.Text).Return(testToken, nil)

	// Mock platform launching the media
	mockPlatform.On("LaunchMedia", mock.AnythingOfType("*config.Instance"), testMedia.Path).Return(nil)

	// Simulate a token-to-media workflow
	assert.True(t, mockReader.Connected())

	scannedToken, err := mockReader.Write(testToken.Text)
	require.NoError(t, err)
	assert.Equal(t, testToken.Text, scannedToken.Text)

	err = mockPlatform.LaunchMedia(nil, testMedia.Path)
	require.NoError(t, err)

	// Verify tracking worked
	launchedMedia := mockPlatform.GetLaunchedMedia()
	assert.Contains(t, launchedMedia, testMedia.Path)

	// Verify all expectations
	mockReader.AssertExpectations(t)
	mockPlatform.AssertExpectations(t)
}
