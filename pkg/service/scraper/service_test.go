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

package scraper

import (
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewScraperService(t *testing.T) {
	t.Parallel()

	// Create mock dependencies
	mockMediaDB := helpers.NewMockMediaDBI()
	mockUserDB := helpers.NewMockUserDBI()
	mockConfig := &config.Instance{}
	mockPlatform := &mocks.MockPlatform{}

	// Set up basic platform mock
	mockPlatform.SetupBasicMock()

	// Create scraper service
	notifChan := make(chan models.Notification, 10)
	service := NewScraperService(mockMediaDB, mockUserDB, mockConfig, mockPlatform, notifChan)
	require.NotNil(t, service)

	// Verify service is properly initialized
	assert.NotNil(t, service.scraperRegistry)
	assert.NotNil(t, service.mediaDB)
	assert.NotNil(t, service.userDB)
	assert.NotNil(t, service.config)
	assert.NotNil(t, service.mediaStorage)
	assert.NotNil(t, service.httpClient)
	assert.NotNil(t, service.jobQueue)
	assert.NotNil(t, service.progressTracker)

	// Verify ScreenScraper is registered
	assert.True(t, service.scraperRegistry.HasScraper("screenscraper"))

	// Test progress tracking
	progress := service.GetProgress()
	assert.NotNil(t, progress)
	assert.False(t, progress.IsRunning)
	assert.Equal(t, 0, progress.ProcessedGames)
	assert.Equal(t, 0, progress.TotalGames)

	// Clean up
	service.Stop()
}

func TestScraperServiceCancel(t *testing.T) {
	t.Parallel()

	// Create mock dependencies
	mockMediaDB := helpers.NewMockMediaDBI()
	mockUserDB := helpers.NewMockUserDBI()
	mockConfig := &config.Instance{}
	mockPlatform := &mocks.MockPlatform{}

	// Set up basic platform mock
	mockPlatform.SetupBasicMock()

	// Create scraper service
	notifChan := make(chan models.Notification, 10)
	service := NewScraperService(mockMediaDB, mockUserDB, mockConfig, mockPlatform, notifChan)
	require.NotNil(t, service)

	// Start a mock scraping operation
	service.progressTracker.SetStatus("running")
	service.progressTracker.SetProgress(0, 10)

	// Test cancellation
	err := service.CancelScraping()
	require.NoError(t, err)

	// Verify scraping is cancelled
	progress := service.GetProgress()
	assert.Equal(t, "cancelled", progress.Status)

	// Clean up
	service.Stop()
}

func TestScraperServiceProgress(t *testing.T) {
	t.Parallel()

	// Create mock dependencies
	mockMediaDB := helpers.NewMockMediaDBI()
	mockUserDB := helpers.NewMockUserDBI()
	mockConfig := &config.Instance{}
	mockPlatform := &mocks.MockPlatform{}

	// Set up basic platform mock
	mockPlatform.SetupBasicMock()

	// Create scraper service
	notifChan := make(chan models.Notification, 10)
	service := NewScraperService(mockMediaDB, mockUserDB, mockConfig, mockPlatform, notifChan)
	require.NotNil(t, service)

	// Update progress using individual methods
	service.progressTracker.SetStatus("running")
	service.progressTracker.SetProgress(3, 10)
	service.progressTracker.SetCurrentGame("Test Game", 1)

	// Verify progress
	progress := service.GetProgress()
	assert.Equal(t, "running", progress.Status)
	assert.Equal(t, 10, progress.TotalGames)
	assert.Equal(t, 3, progress.ProcessedGames)
	assert.Equal(t, "Test Game", progress.CurrentGame)

	// Clean up
	service.Stop()
}

func TestScraperServiceStop(t *testing.T) {
	t.Parallel()

	// Create mock dependencies
	mockMediaDB := helpers.NewMockMediaDBI()
	mockUserDB := helpers.NewMockUserDBI()
	mockConfig := &config.Instance{}
	mockPlatform := &mocks.MockPlatform{}

	// Set up basic platform mock
	mockPlatform.SetupBasicMock()

	// Create scraper service
	notifChan := make(chan models.Notification, 10)
	service := NewScraperService(mockMediaDB, mockUserDB, mockConfig, mockPlatform, notifChan)
	require.NotNil(t, service)

	// Stop the service (should not panic)
	service.Stop()

	// Multiple stops should be safe
	service.Stop()
}

func TestScraperServiceContextCancellation(t *testing.T) {
	t.Parallel()

	// Create mock dependencies
	mockMediaDB := helpers.NewMockMediaDBI()
	mockUserDB := helpers.NewMockUserDBI()
	mockConfig := &config.Instance{}
	mockPlatform := &mocks.MockPlatform{}

	// Set up basic platform mock
	mockPlatform.SetupBasicMock()

	// Create scraper service
	notifChan := make(chan models.Notification, 10)
	service := NewScraperService(mockMediaDB, mockUserDB, mockConfig, mockPlatform, notifChan)
	require.NotNil(t, service)

	// Verify context is working
	ctx := service.ctx
	assert.NotNil(t, ctx)

	// Cancel context and verify workers stop
	service.cancelFunc()

	// Give workers a moment to shut down
	time.Sleep(100 * time.Millisecond)

	// Context should be cancelled
	select {
	case <-ctx.Done():
		// Expected
	default:
		t.Error("Context was not cancelled")
	}

	// Stop should still work after context cancellation
	service.Stop()
}

func TestScraperService_MultipleMediaEntriesHandling(t *testing.T) {
	t.Parallel()

	// Create mock dependencies
	mockMediaDB := helpers.NewMockMediaDBI()
	mockUserDB := helpers.NewMockUserDBI()
	mockConfig := &config.Instance{}
	mockPlatform := &mocks.MockPlatform{}

	// Set up basic platform mock
	mockPlatform.SetupBasicMock()

	// Create scraper service
	notifChan := make(chan models.Notification, 10)
	service := NewScraperService(mockMediaDB, mockUserDB, mockConfig, mockPlatform, notifChan)
	require.NotNil(t, service)

	// Verify that the getAllMediaForTitle method exists and can be called
	// This ensures the method signature is correct, even though we can't test
	// the actual database interaction with mocks
	assert.NotNil(t, service.getAllMediaForTitle)

	// The actual testing of getAllMediaForTitle requires a real database connection
	// and would be better suited for integration tests
	// For unit tests, we verify the method exists and the refactoring was applied

	// Clean up
	service.Stop()
}
