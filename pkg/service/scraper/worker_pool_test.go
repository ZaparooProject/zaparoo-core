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
	"sync/atomic"
	"testing"
	"time"

	scraperpkg "github.com/ZaparooProject/zaparoo-core/v2/pkg/scraper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MockJobProcessor for testing
type MockJobProcessor struct {
	processedJobs int64
	shouldError   bool
	processDelay  time.Duration
}

func (m *MockJobProcessor) ProcessJob(job *scraperpkg.ScraperJob) error {
	if m.processDelay > 0 {
		time.Sleep(m.processDelay)
	}

	atomic.AddInt64(&m.processedJobs, 1)

	if m.shouldError {
		return assert.AnError
	}
	return nil
}

func (m *MockJobProcessor) GetProcessedCount() int64 {
	return atomic.LoadInt64(&m.processedJobs)
}

func TestNewWorkerPool(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	jobQueue := NewJobQueue(ctx, 10)
	processor := &MockJobProcessor{}

	pool := NewWorkerPool(ctx, 3, jobQueue.Channel(), processor)
	require.NotNil(t, pool)

	assert.Equal(t, 3, pool.workerCount)
	assert.NotNil(t, pool.ctx)
	assert.NotNil(t, pool.jobQueue)
	assert.NotNil(t, pool.processor)

	jobQueue.Close()
}

func TestWorkerPool_StartStop(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	jobQueue := NewJobQueue(ctx, 10)
	processor := &MockJobProcessor{}

	pool := NewWorkerPool(ctx, 2, jobQueue.Channel(), processor)
	require.NotNil(t, pool)

	// Start the pool
	pool.Start()

	// Give workers time to start
	time.Sleep(50 * time.Millisecond)

	// Close queue first to allow workers to exit cleanly
	jobQueue.Close()

	// Stop the pool
	pool.Stop()

	// Multiple stops should be safe
	pool.Stop()
}

func TestWorkerPool_JobProcessing(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	jobQueue := NewJobQueue(ctx, 10)
	processor := &MockJobProcessor{processDelay: 10 * time.Millisecond}

	pool := NewWorkerPool(ctx, 2, jobQueue.Channel(), processor)
	require.NotNil(t, pool)

	pool.Start()
	defer pool.Stop()

	// Create test jobs
	jobs := []*scraperpkg.ScraperJob{
		{
			MediaDBID:  1,
			MediaTitle: "Test Game 1",
			SystemID:   "nes",
		},
		{
			MediaDBID:  2,
			MediaTitle: "Test Game 2",
			SystemID:   "snes",
		},
		{
			MediaDBID:  3,
			MediaTitle: "Test Game 3",
			SystemID:   "genesis",
		},
	}

	// Enqueue jobs
	for _, job := range jobs {
		err := jobQueue.Enqueue(job)
		require.NoError(t, err)
	}

	// Wait for jobs to be processed
	require.Eventually(t, func() bool {
		return processor.GetProcessedCount() == int64(len(jobs))
	}, 2*time.Second, 10*time.Millisecond, "All jobs should be processed")

	// Close queue and stop pool
	jobQueue.Close()
	pool.Stop()
}

func TestWorkerPool_ErrorHandling(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	jobQueue := NewJobQueue(ctx, 10)
	processor := &MockJobProcessor{shouldError: true}

	pool := NewWorkerPool(ctx, 1, jobQueue.Channel(), processor)
	require.NotNil(t, pool)

	pool.Start()
	defer pool.Stop()

	// Create a job that will cause an error
	job := &scraperpkg.ScraperJob{
		MediaDBID:  1,
		MediaTitle: "Error Job",
		SystemID:   "nes",
	}

	err := jobQueue.Enqueue(job)
	require.NoError(t, err)

	// Wait for job to be processed (even with error)
	require.Eventually(t, func() bool {
		return processor.GetProcessedCount() == 1
	}, 1*time.Second, 10*time.Millisecond, "Job should be processed even with error")

	// Close queue and stop pool
	jobQueue.Close()
	pool.Stop()
}

func TestWorkerPool_ContextCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	jobQueue := NewJobQueue(ctx, 10)
	processor := &MockJobProcessor{processDelay: 100 * time.Millisecond}

	pool := NewWorkerPool(ctx, 2, jobQueue.Channel(), processor)
	require.NotNil(t, pool)

	pool.Start()

	// Add a job
	job := &scraperpkg.ScraperJob{
		MediaDBID:  1,
		MediaTitle: "Test Job",
		SystemID:   "nes",
	}
	err := jobQueue.Enqueue(job)
	require.NoError(t, err)

	// Cancel context immediately
	cancel()

	// Give workers time to stop
	time.Sleep(200 * time.Millisecond)

	// Workers should have stopped due to context cancellation
	// The job might or might not be processed depending on timing
	pool.Stop()
}

func TestWorkerPool_ZeroWorkers(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	jobQueue := NewJobQueue(ctx, 10)
	processor := &MockJobProcessor{}

	// Test with zero workers
	pool := NewWorkerPool(ctx, 0, jobQueue.Channel(), processor)
	require.NotNil(t, pool)

	pool.Start()
	defer pool.Stop()

	// Add a job
	job := &scraperpkg.ScraperJob{
		MediaDBID:  1,
		MediaTitle: "Test Job",
		SystemID:   "nes",
	}
	err := jobQueue.Enqueue(job)
	require.NoError(t, err)

	// Wait a bit and verify no jobs were processed (no workers)
	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, int64(0), processor.GetProcessedCount())

	jobQueue.Close()
}

func TestWorkerPool_ConcurrentJobProcessing(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	jobQueue := NewJobQueue(ctx, 100)
	processor := &MockJobProcessor{processDelay: 5 * time.Millisecond}

	// Use multiple workers for concurrent processing
	pool := NewWorkerPool(ctx, 5, jobQueue.Channel(), processor)
	require.NotNil(t, pool)

	pool.Start()
	defer pool.Stop()

	// Create many jobs
	jobCount := 50
	for i := 0; i < jobCount; i++ {
		job := &scraperpkg.ScraperJob{
			MediaDBID:  int64(i + 1),
			MediaTitle: "Test Game",
			SystemID:   "nes",
		}
		err := jobQueue.Enqueue(job)
		require.NoError(t, err)
	}

	// Wait for all jobs to be processed
	require.Eventually(t, func() bool {
		return processor.GetProcessedCount() == int64(jobCount)
	}, 5*time.Second, 10*time.Millisecond, "All jobs should be processed")

	// Close queue and stop pool
	jobQueue.Close()
	pool.Stop()
}