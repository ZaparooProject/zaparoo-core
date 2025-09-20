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

	scraperpkg "github.com/ZaparooProject/zaparoo-core/v2/pkg/scraper"
	"github.com/rs/zerolog/log"
)

const (
	// DefaultQueueSize is the default size of the job queue
	DefaultQueueSize = 1000
)

// JobQueue manages a queue of scraper jobs
type JobQueue struct {
	queue  chan *scraperpkg.ScraperJob
	ctx    context.Context
	closed bool
}

// NewJobQueue creates a new job queue with the specified capacity
func NewJobQueue(ctx context.Context, capacity int) *JobQueue {
	if capacity <= 0 {
		capacity = DefaultQueueSize
	}

	return &JobQueue{
		queue: make(chan *scraperpkg.ScraperJob, capacity),
		ctx:   ctx,
	}
}

// Enqueue adds a job to the queue
func (jq *JobQueue) Enqueue(job *scraperpkg.ScraperJob) error {
	if jq.closed {
		return ErrQueueClosed
	}

	select {
	case <-jq.ctx.Done():
		return jq.ctx.Err()
	case jq.queue <- job:
		log.Debug().
			Int64("media_id", job.MediaDBID).
			Str("system", job.SystemID).
			Msg("Job enqueued")
		return nil
	default:
		return ErrQueueFull
	}
}

// Channel returns the underlying channel for workers to consume from
func (jq *JobQueue) Channel() <-chan *scraperpkg.ScraperJob {
	return jq.queue
}

// Close closes the job queue
func (jq *JobQueue) Close() {
	if !jq.closed {
		jq.closed = true
		close(jq.queue)
		log.Debug().Msg("Job queue closed")
	}
}

// Size returns the current number of jobs in the queue
func (jq *JobQueue) Size() int {
	return len(jq.queue)
}

// Capacity returns the maximum capacity of the queue
func (jq *JobQueue) Capacity() int {
	return cap(jq.queue)
}