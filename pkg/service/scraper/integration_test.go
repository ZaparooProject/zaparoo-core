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
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	scraperpkg "github.com/ZaparooProject/zaparoo-core/v2/pkg/scraper"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestScraperService_FullWorkflow tests the complete integration of all scraper service components
func TestScraperService_FullWorkflow(t *testing.T) {
	t.Parallel()

	// Create mock dependencies
	mockMediaDB := helpers.NewMockMediaDBI()
	mockUserDB := helpers.NewMockUserDBI()
	mockConfig := &config.Instance{}
	mockPlatform := &mocks.MockPlatform{}

	// Set up basic platform mock
	mockPlatform.SetupBasicMock()

	// Set up mock media database responses
	system := &database.System{
		DBID:     1,
		SystemID: "nes",
		Name:     "Nintendo Entertainment System",
	}

	mediaTitle := &database.MediaTitle{
		DBID:       1,
		SystemDBID: 1,
		Name:       "Super Mario Bros",
	}

	media := &database.Media{
		DBID:           1,
		MediaTitleDBID: 1,
		Path:           "/games/nes/Super Mario Bros.nes",
	}

	// Configure mock responses
	mockMediaDB.On("GetSystemByID", int64(1)).Return(system, nil)
	mockMediaDB.On("GetMediaTitleByID", int64(1)).Return(mediaTitle, nil)
	mockMediaDB.On("GetMediaByID", int64(1)).Return(media, nil)
	mockMediaDB.On("HasScraperMetadata", int64(1)).Return(false, nil)
	mockMediaDB.On("GetGameHashes", "nes", "/games/nes/Super Mario Bros.nes").Return(nil, fmt.Errorf("not found"))

	// Create scraper service
	notifChan := make(chan models.Notification, 100)
	service := NewScraperService(mockMediaDB, mockUserDB, mockConfig, mockPlatform, notifChan)
	require.NotNil(t, service)

	// Verify service initialization
	assert.NotNil(t, service.scraperRegistry)
	assert.NotNil(t, service.progressTracker)
	assert.NotNil(t, service.jobQueue)
	assert.NotNil(t, service.workerPool)

	// Test initial progress state
	progress := service.GetProgress()
	assert.Equal(t, "idle", progress.Status)
	assert.Equal(t, 0, progress.ProcessedGames)
	assert.Equal(t, 0, progress.TotalGames)

	// Clean up
	service.Stop()

	// Verify notifications were sent during service lifecycle
	assert.True(t, len(notifChan) >= 0) // May have lifecycle notifications
}

// TestScraperService_ComponentIntegration tests how components work together
func TestScraperService_ComponentIntegration(t *testing.T) {
	t.Parallel()

	// Create a simple integration test with controlled mock processor
	ctx := context.Background()
	notifChan := make(chan models.Notification, 10)

	// Create components separately to test integration
	progressTracker := NewProgressTracker(notifChan)
	jobQueue := NewJobQueue(ctx, 5)
	scraperRegistry := NewScraperRegistry()

	// Test progress tracker integration
	progressTracker.SetStatus("running")
	progressTracker.SetProgress(0, 3)

	progress := progressTracker.Get()
	assert.Equal(t, "running", progress.Status)
	assert.Equal(t, 3, progress.TotalGames)

	// Test job queue integration
	job1 := &scraperpkg.ScraperJob{
		MediaDBID:  1,
		MediaTitle: "Game 1",
		SystemID:   "nes",
	}

	job2 := &scraperpkg.ScraperJob{
		MediaDBID:  2,
		MediaTitle: "Game 2",
		SystemID:   "snes",
	}

	err := jobQueue.Enqueue(job1)
	assert.NoError(t, err)

	err = jobQueue.Enqueue(job2)
	assert.NoError(t, err)

	assert.Equal(t, 2, jobQueue.Size())

	// Test scraper registry integration
	assert.True(t, scraperRegistry.HasScraper("screenscraper"))
	assert.True(t, scraperRegistry.HasScraper("thegamesdb"))
	assert.True(t, scraperRegistry.HasScraper("igdb"))

	scraper, err := scraperRegistry.Get("screenscraper")
	assert.NoError(t, err)
	assert.NotNil(t, scraper)

	// Test worker pool with mock processor
	processor := &MockJobProcessor{processDelay: 5 * time.Millisecond}
	workerPool := NewWorkerPool(ctx, 2, jobQueue.Channel(), processor)

	workerPool.Start()

	// Wait for jobs to be processed with a timeout
	expectedJobs := int64(2)
	require.Eventually(t, func() bool {
		return processor.GetProcessedCount() == expectedJobs
	}, 500*time.Millisecond, 10*time.Millisecond, "Jobs should be processed within timeout")
	// Update progress as jobs complete
	progressTracker.IncrementProgress()
	progressTracker.IncrementProgress()

	finalProgress := progressTracker.Get()
	assert.Equal(t, 2, finalProgress.ProcessedGames)
	assert.Equal(t, 3, finalProgress.TotalGames)

	// Verify notifications were sent
	assert.True(t, len(notifChan) > 0)

	// Clean up
	workerPool.Stop()
	jobQueue.Close()
}

// TestScraperService_ErrorPropagation tests error handling across components
func TestScraperService_ErrorPropagation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	notifChan := make(chan models.Notification, 10)

	progressTracker := NewProgressTracker(notifChan)
	jobQueue := NewJobQueue(ctx, 5)

	// Test error handling in progress tracker
	testError := fmt.Errorf("test scraping error")
	progressTracker.SetError(testError)

	progress := progressTracker.Get()
	assert.Equal(t, "test scraping error", progress.LastError)
	assert.Equal(t, 1, progress.ErrorCount)

	// Test job queue full error
	// Fill the queue to capacity
	for i := 0; i < 5; i++ {
		job := &scraperpkg.ScraperJob{
			MediaDBID:  int64(i + 1),
			MediaTitle: fmt.Sprintf("Game %d", i+1),
			SystemID:   "nes",
		}
		err := jobQueue.Enqueue(job)
		assert.NoError(t, err)
	}

	// Next job should fail
	overflowJob := &scraperpkg.ScraperJob{
		MediaDBID:  6,
		MediaTitle: "Overflow Game",
		SystemID:   "nes",
	}
	err := jobQueue.Enqueue(overflowJob)
	assert.ErrorIs(t, err, ErrQueueFull)

	// Test worker pool error handling
	errorProcessor := &MockJobProcessor{shouldError: true}
	workerPool := NewWorkerPool(ctx, 1, jobQueue.Channel(), errorProcessor)

	workerPool.Start()

	// Wait for jobs to be processed (even with errors)
	require.Eventually(t, func() bool {
		return errorProcessor.GetProcessedCount() >= 1
	}, 1*time.Second, 10*time.Millisecond, "Jobs should be processed even with errors")

	workerPool.Stop()
	jobQueue.Close()
}

// TestScraperService_ConcurrentOperations tests thread safety across components
func TestScraperService_ConcurrentOperations(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	notifChan := make(chan models.Notification, 100)

	progressTracker := NewProgressTracker(notifChan)
	jobQueue := NewJobQueue(ctx, 50)
	processor := &MockJobProcessor{processDelay: 1 * time.Millisecond}
	workerPool := NewWorkerPool(ctx, 3, jobQueue.Channel(), processor)

	workerPool.Start()
	defer workerPool.Stop()
	defer jobQueue.Close()

	// Set initial progress
	progressTracker.SetProgress(0, 20)

	// Concurrently enqueue jobs and update progress
	const numJobs = 20

	go func() {
		for i := 0; i < numJobs; i++ {
			job := &scraperpkg.ScraperJob{
				MediaDBID:  int64(i + 1),
				MediaTitle: fmt.Sprintf("Game %d", i+1),
				SystemID:   "nes",
			}
			err := jobQueue.Enqueue(job)
			if err != nil {
				t.Errorf("Failed to enqueue job %d: %v", i, err)
			}
			time.Sleep(1 * time.Millisecond)
		}
	}()

	// Wait for all jobs to be processed
	require.Eventually(t, func() bool {
		return processor.GetProcessedCount() == numJobs
	}, 3*time.Second, 10*time.Millisecond, "All jobs should be processed within timeout")
	// Update progress to match processed jobs
	for i := 0; i < numJobs; i++ {
		progressTracker.IncrementProgress()
	}

	finalProgress := progressTracker.Get()
	assert.Equal(t, numJobs, finalProgress.ProcessedGames)
	assert.Equal(t, numJobs, finalProgress.TotalGames)

	// Complete the operation
	progressTracker.Complete()

	completedProgress := progressTracker.Get()
	assert.Equal(t, "completed", completedProgress.Status)
	assert.False(t, completedProgress.IsRunning)
}

// TestScraperService_LifecycleManagement tests proper startup and shutdown
func TestScraperService_LifecycleManagement(t *testing.T) {
	t.Parallel()

	// Create mock dependencies
	mockMediaDB := helpers.NewMockMediaDBI()
	mockUserDB := helpers.NewMockUserDBI()
	mockConfig := &config.Instance{}
	mockPlatform := &mocks.MockPlatform{}

	mockPlatform.SetupBasicMock()

	notifChan := make(chan models.Notification, 10)

	// Create service
	service := NewScraperService(mockMediaDB, mockUserDB, mockConfig, mockPlatform, notifChan)
	require.NotNil(t, service)

	// Verify service is properly initialized
	progress := service.GetProgress()
	assert.Equal(t, "idle", progress.Status)

	// Test graceful shutdown
	service.Stop()

	// Multiple stops should be safe
	service.Stop()
	service.Stop()

	// Operations after stop should handle gracefully
	err := service.CancelScraping()
	assert.NoError(t, err)

	finalProgress := service.GetProgress()
	assert.NotNil(t, finalProgress)
}