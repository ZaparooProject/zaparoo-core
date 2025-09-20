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
	"sync"
	"testing"
	"time"

	scraperpkg "github.com/ZaparooProject/zaparoo-core/v2/pkg/scraper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewJobQueue(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	// Test with valid capacity
	queue := NewJobQueue(ctx, 10)
	require.NotNil(t, queue)
	assert.Equal(t, 10, queue.Capacity())
	assert.Equal(t, 0, queue.Size())
	assert.False(t, queue.closed)

	// Test with zero capacity (should use default)
	queue2 := NewJobQueue(ctx, 0)
	require.NotNil(t, queue2)
	assert.Equal(t, DefaultQueueSize, queue2.Capacity())

	// Test with negative capacity (should use default)
	queue3 := NewJobQueue(ctx, -5)
	require.NotNil(t, queue3)
	assert.Equal(t, DefaultQueueSize, queue3.Capacity())

	queue.Close()
	queue2.Close()
	queue3.Close()
}

func TestJobQueue_Enqueue(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	queue := NewJobQueue(ctx, 5)
	defer queue.Close()

	job := &scraperpkg.ScraperJob{
		MediaDBID:  1,
		MediaTitle: "Test Game",
		SystemID:   "nes",
	}

	// Enqueue should succeed
	err := queue.Enqueue(job)
	assert.NoError(t, err)
	assert.Equal(t, 1, queue.Size())

	// Enqueue multiple jobs
	for i := 2; i <= 5; i++ {
		job := &scraperpkg.ScraperJob{
			MediaDBID:  int64(i),
			MediaTitle: "Test Game",
			SystemID:   "nes",
		}
		err := queue.Enqueue(job)
		assert.NoError(t, err)
	}

	assert.Equal(t, 5, queue.Size())
	assert.Equal(t, 5, queue.Capacity())
}

func TestJobQueue_EnqueueFull(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	queue := NewJobQueue(ctx, 2)
	defer queue.Close()

	// Fill the queue
	for i := 1; i <= 2; i++ {
		job := &scraperpkg.ScraperJob{
			MediaDBID:  int64(i),
			MediaTitle: "Test Game",
			SystemID:   "nes",
		}
		err := queue.Enqueue(job)
		assert.NoError(t, err)
	}

	// Queue should be full
	assert.Equal(t, 2, queue.Size())

	// Next enqueue should fail
	job := &scraperpkg.ScraperJob{
		MediaDBID:  3,
		MediaTitle: "Test Game",
		SystemID:   "nes",
	}
	err := queue.Enqueue(job)
	assert.ErrorIs(t, err, ErrQueueFull)
}

func TestJobQueue_EnqueueClosed(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	queue := NewJobQueue(ctx, 5)

	// Close the queue
	queue.Close()

	// Enqueue should fail
	job := &scraperpkg.ScraperJob{
		MediaDBID:  1,
		MediaTitle: "Test Game",
		SystemID:   "nes",
	}
	err := queue.Enqueue(job)
	assert.ErrorIs(t, err, ErrQueueClosed)
}

func TestJobQueue_EnqueueContextCancelled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	queue := NewJobQueue(ctx, 5)
	defer queue.Close()

	// Cancel the context
	cancel()

	// Enqueue should fail with context error
	job := &scraperpkg.ScraperJob{
		MediaDBID:  1,
		MediaTitle: "Test Game",
		SystemID:   "nes",
	}
	err := queue.Enqueue(job)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestJobQueue_Channel(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	queue := NewJobQueue(ctx, 5)
	defer queue.Close()

	// Get the channel
	ch := queue.Channel()
	assert.NotNil(t, ch)

	// Enqueue a job
	job := &scraperpkg.ScraperJob{
		MediaDBID:  1,
		MediaTitle: "Test Game",
		SystemID:   "nes",
	}
	err := queue.Enqueue(job)
	assert.NoError(t, err)

	// Receive from channel
	select {
	case receivedJob := <-ch:
		assert.NotNil(t, receivedJob)
		assert.Equal(t, int64(1), receivedJob.MediaDBID)
		assert.Equal(t, "Test Game", receivedJob.MediaTitle)
		assert.Equal(t, "nes", receivedJob.SystemID)
	case <-time.After(100 * time.Millisecond):
		t.Error("Did not receive job from channel")
	}

	// Queue should be empty now
	assert.Equal(t, 0, queue.Size())
}

func TestJobQueue_Close(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	queue := NewJobQueue(ctx, 5)

	// Add some jobs
	for i := 1; i <= 3; i++ {
		job := &scraperpkg.ScraperJob{
			MediaDBID:  int64(i),
			MediaTitle: "Test Game",
			SystemID:   "nes",
		}
		err := queue.Enqueue(job)
		assert.NoError(t, err)
	}

	assert.Equal(t, 3, queue.Size())

	// Close the queue
	queue.Close()

	// Channel should be closed
	ch := queue.Channel()
	select {
	case job, ok := <-ch:
		if !ok {
			// Channel is closed, this is expected eventually
			return
		}
		// We might receive pending jobs before channel closes
		assert.NotNil(t, job)
	case <-time.After(100 * time.Millisecond):
		// Channel might not have pending jobs to drain
	}

	// Multiple closes should be safe
	queue.Close()
}

func TestJobQueue_ConcurrentEnqueue(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	queue := NewJobQueue(ctx, 100)
	defer queue.Close()

	const numWorkers = 10
	const jobsPerWorker = 20

	var wg sync.WaitGroup
	var successCount int64
	var mutex sync.Mutex

	// Start multiple goroutines enqueueing jobs
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < jobsPerWorker; j++ {
				job := &scraperpkg.ScraperJob{
					MediaDBID:  int64(workerID*jobsPerWorker + j + 1),
					MediaTitle: "Test Game",
					SystemID:   "nes",
				}
				err := queue.Enqueue(job)
				if err == nil {
					mutex.Lock()
					successCount++
					mutex.Unlock()
				}
			}
		}(i)
	}

	wg.Wait()

	mutex.Lock()
	totalExpected := int64(numWorkers * jobsPerWorker)
	mutex.Unlock()

	assert.Equal(t, totalExpected, successCount)
	assert.Equal(t, int(totalExpected), queue.Size())
}

func TestJobQueue_ConsumerProducer(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	queue := NewJobQueue(ctx, 10)
	defer queue.Close()

	const numJobs = 50
	var receivedJobs []int64
	var mutex sync.Mutex
	var wg sync.WaitGroup

	// Consumer
	wg.Add(1)
	go func() {
		defer wg.Done()
		ch := queue.Channel()
		for {
			select {
			case job, ok := <-ch:
				if !ok {
					return
				}
				mutex.Lock()
				receivedJobs = append(receivedJobs, job.MediaDBID)
				mutex.Unlock()
			case <-ctx.Done():
				return
			}
		}
	}()

	// Producer
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 1; i <= numJobs; i++ {
			job := &scraperpkg.ScraperJob{
				MediaDBID:  int64(i),
				MediaTitle: "Test Game",
				SystemID:   "nes",
			}
			err := queue.Enqueue(job)
			if err != nil {
				t.Errorf("Failed to enqueue job %d: %v", i, err)
				return
			}
			// Small delay to allow consumer to process
			time.Sleep(1 * time.Millisecond)
		}
	}()

	// Wait for producer to finish
	time.Sleep(200 * time.Millisecond)
	queue.Close()

	// Wait for consumer to finish
	wg.Wait()

	mutex.Lock()
	assert.Equal(t, numJobs, len(receivedJobs))
	mutex.Unlock()

	// Verify all jobs were received (order might vary due to concurrency)
	jobSet := make(map[int64]bool)
	for _, jobID := range receivedJobs {
		jobSet[jobID] = true
	}

	for i := 1; i <= numJobs; i++ {
		assert.True(t, jobSet[int64(i)], "Job %d was not received", i)
	}
}

func TestJobQueue_SizeAndCapacity(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	capacity := 7
	queue := NewJobQueue(ctx, capacity)
	defer queue.Close()

	// Initially empty
	assert.Equal(t, 0, queue.Size())
	assert.Equal(t, capacity, queue.Capacity())

	// Add jobs and verify size
	for i := 1; i <= capacity; i++ {
		job := &scraperpkg.ScraperJob{
			MediaDBID:  int64(i),
			MediaTitle: "Test Game",
			SystemID:   "nes",
		}
		err := queue.Enqueue(job)
		assert.NoError(t, err)
		assert.Equal(t, i, queue.Size())
		assert.Equal(t, capacity, queue.Capacity())
	}

	// Remove jobs by receiving from channel
	ch := queue.Channel()
	for i := capacity; i > 0; i-- {
		select {
		case <-ch:
			assert.Equal(t, i-1, queue.Size())
		case <-time.After(100 * time.Millisecond):
			t.Error("Timeout receiving from channel")
		}
	}

	assert.Equal(t, 0, queue.Size())
	assert.Equal(t, capacity, queue.Capacity())
}