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

	scraperpkg "github.com/ZaparooProject/zaparoo-core/v2/pkg/scraper"
	"github.com/rs/zerolog/log"
)

// WorkerPool manages a pool of worker goroutines for processing scraper jobs
type WorkerPool struct {
	workerCount int
	ctx         context.Context
	jobQueue    <-chan *scraperpkg.ScraperJob
	workerWG    sync.WaitGroup
	processor   JobProcessor
}

// JobProcessor defines the interface for processing scraper jobs
type JobProcessor interface {
	ProcessJob(job *scraperpkg.ScraperJob) error
}

// NewWorkerPool creates a new worker pool
func NewWorkerPool(ctx context.Context, workerCount int, jobQueue <-chan *scraperpkg.ScraperJob, processor JobProcessor) *WorkerPool {
	return &WorkerPool{
		workerCount: workerCount,
		ctx:         ctx,
		jobQueue:    jobQueue,
		processor:   processor,
	}
}

// Start starts all workers in the pool
func (wp *WorkerPool) Start() {
	log.Info().Int("workers", wp.workerCount).Msg("Starting worker pool")
	for i := range wp.workerCount {
		wp.workerWG.Add(1)
		go wp.worker(i)
	}
}

// Stop stops all workers and waits for them to finish
func (wp *WorkerPool) Stop() {
	log.Info().Msg("Stopping worker pool")
	wp.workerWG.Wait()
	log.Info().Msg("Worker pool stopped")
}

// worker processes jobs from the job queue
func (wp *WorkerPool) worker(id int) {
	defer wp.workerWG.Done()
	log.Debug().Int("worker_id", id).Msg("Worker started")

	for {
		select {
		case <-wp.ctx.Done():
			log.Debug().Int("worker_id", id).Msg("Worker stopped due to context cancellation")
			return
		case job, ok := <-wp.jobQueue:
			if !ok {
				log.Debug().Int("worker_id", id).Msg("Worker stopped due to closed job queue")
				return
			}

			log.Debug().Int("worker_id", id).
				Int64("media_id", job.MediaDBID).
				Str("system", job.SystemID).
				Msg("Processing job")

			if err := wp.processor.ProcessJob(job); err != nil {
				log.Error().Err(err).
					Int("worker_id", id).
					Int64("media_id", job.MediaDBID).
					Str("system", job.SystemID).
					Msg("Failed to process job")
			}
		}
	}
}