// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
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
	"errors"
	"fmt"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediadb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper"
	"github.com/google/uuid"
	"github.com/spf13/afero"
)

var errScrapeJobRetired = errors.New("scrape job was cancelled or replaced before recovery")

// persistStart rechecks recovery after acquiring the write lease. Cancellation
// and queue acceptance use the same mutex, so stale recovery cannot revive work.
func (s *scrapingStatus) persistStart(db database.MediaDBI, operation *database.ScrapingOperation, resume bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancelRequested {
		return context.Canceled
	}
	stored, found, err := db.GetScrapingOperation()
	if err != nil {
		return fmt.Errorf("read pending scrape jobs: %w", err)
	}
	if resume && (!found || stored.ScraperID != operation.ScraperID || stored.RunID != operation.RunID) {
		return errScrapeJobRetired
	}
	if found {
		status := stored.Status
		if stored.Version == 0 || status == "" {
			status, err = db.GetScrapingStatus()
			if err != nil {
				return fmt.Errorf("read pending scrape status: %w", err)
			}
		}
		if resume {
			if !stored.IsResumable(status) {
				return errScrapeJobRetired
			}
			*operation = stored
		} else if stored.IsResumable(status) {
			return models.ClientErrf("scraping jobs are pending; cancel them before starting another scrape")
		}
	}
	if err := operation.Validate(); err != nil {
		return fmt.Errorf("validate scrape job: %w", err)
	}
	if operation.RunID == "" && (operation.Force || operation.FillMissing) {
		operation.RunID = uuid.NewString()
	}
	// Legacy records are upgraded when admitted, not interpreted as a
	// different kind of worker or given different cancellation semantics.
	operation.Version = 1
	operation.Status = mediadb.IndexingStatusRunning
	if err := db.SetScrapingOperation(*operation); err != nil {
		return fmt.Errorf("persist scrape job: %w", err)
	}
	s.force = operation.Force
	if err := db.SetScrapingStatus(mediadb.IndexingStatusRunning); err != nil {
		return fmt.Errorf("failed to persist scraping status: %w", err)
	}
	return nil
}

func (s *scrapingStatus) cancelPersisted(db database.MediaDBI) (bool, error) {
	if db == nil {
		return s.cancel(), nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	active := s.cancelLocked()
	op, found, err := db.GetScrapingOperation()
	if err != nil {
		return active, fmt.Errorf("read scrape jobs for cancellation: %w", err)
	}
	if !active && !found {
		return false, nil
	}
	if found && op.Version == 1 {
		op.Status = mediadb.IndexingStatusCancelled
		if err := db.SetScrapingOperation(op); err != nil {
			return true, fmt.Errorf("cancel scrape jobs: %w", err)
		}
	}
	statusErr := db.SetScrapingStatus(mediadb.IndexingStatusCancelled)
	clearErr := db.ClearScrapingOperation()
	return true, errors.Join(statusErr, clearErr)
}

// persistTerminal keeps versioned failure/shutdown state in the job record.
// Completed records are deleted before their markers can be retired.
func (s *scrapingStatus) persistTerminal(
	db database.MediaDBI, operation *database.ScrapingOperation, status string,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancelRequested {
		status = mediadb.IndexingStatusCancelled
	}
	if operation.Version == 1 && (status == mediadb.IndexingStatusFailed ||
		status == mediadb.IndexingStatusPending && !operation.IsResumable("")) {
		operation.Status = status
		if err := db.SetScrapingOperation(*operation); err != nil {
			return fmt.Errorf("persist terminal scrape job: %w", err)
		}
	}
	if err := db.SetScrapingStatus(status); err != nil {
		return fmt.Errorf("persist terminal scrape status: %w", err)
	}
	return nil
}

// advance serializes queue persistence with explicit cancellation. The worker
// retains the same context, pauser and write lease across ordinary scrape jobs.
func (s *scrapingStatus) advance(
	ctx context.Context, db database.MediaDBI, current *database.ScrapingOperation,
) (database.ScrapingOperation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancelRequested || ctx.Err() != nil {
		return database.ScrapingOperation{}, context.Canceled
	}
	if len(current.Pending) == 0 {
		return database.ScrapingOperation{}, errors.New("scraper queue is empty")
	}
	job := current.Pending[0]
	next := database.ScrapingOperation{
		Version: 1, Status: mediadb.IndexingStatusPending,
		ScraperID: job.ScraperID, Systems: job.Systems, Force: job.Force,
		FillMissing: job.FillMissing, RunID: job.RunID, Pending: current.Pending[1:],
	}
	if next.RunID == "" && (next.Force || next.FillMissing) {
		next.RunID = uuid.NewString()
	}
	if err := db.SetScrapingOperation(next); err != nil {
		return database.ScrapingOperation{}, fmt.Errorf("persist next scrape job: %w", err)
	}
	s.scraperID, s.force = next.ScraperID, next.Force
	s.countCache = scrapedCountCache{}
	return next, nil
}

func startQueuedScraper(
	ctx context.Context, env *requests.RequestEnv, operation *database.ScrapingOperation,
) chan scraper.ScrapeUpdate {
	ch := make(chan scraper.ScrapeUpdate, 32)
	fail := func(err error) chan scraper.ScrapeUpdate {
		ch <- scraper.ScrapeUpdate{Done: true, FatalErr: err}
		close(ch)
		return ch
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	s, ok := env.Platform.Scrapers(env.Config)[operation.ScraperID]
	if !ok || s.Scrape == nil {
		return fail(fmt.Errorf("scraper %q is unavailable", operation.ScraperID))
	}
	if operation.FillMissing && (operation.Force || !s.SupportsFillMissing) {
		return fail(fmt.Errorf("scraper %q cannot apply requested fill-missing policy", operation.ScraperID))
	}
	publishScrapingStatus(env.State.Notifications, &models.ScrapingStatusResponse{
		ScraperID: operation.ScraperID, Force: operation.Force, Scraping: true, State: scrapeStateRunning,
		Paused:             env.ScrapePauser != nil && env.ScrapePauser.IsPaused(),
		Throttled:          env.ScrapePauser != nil && env.ScrapePauser.IsThrottled(),
		CurrentStepDisplay: ptrIfNotEmpty(preparingMediaScrapeDisplay),
	})
	opts := scraper.ScrapeOptions{
		Systems: operation.Systems, RunID: operation.RunID, Force: operation.Force,
		FillMissing: operation.FillMissing, Pauser: env.ScrapePauser,
	}
	if err := s.Scrape(ctx, env.Config, env.Platform, afero.NewOsFs(), env.Database, opts, nil, ch); err != nil {
		return fail(err)
	}
	return ch
}
