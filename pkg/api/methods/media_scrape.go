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
	"fmt"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/notifications"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/validation"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/rs/zerolog/log"
)

// scrapingStatus tracks the lifecycle of an active media.scrape operation.
// It mirrors the indexingStatus pattern in media.go for consistent state
// management and safe concurrent access.
type scrapingStatus struct {
	cancelFunc context.CancelFunc
	scraperID  string
	mu         syncutil.RWMutex
	running    bool
}

func (s *scrapingStatus) startIfNotRunning(scraperID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return false
	}
	s.running = true
	s.scraperID = scraperID
	return true
}

func (s *scrapingStatus) clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.running = false
	s.scraperID = ""
	s.cancelFunc = nil
}

func (s *scrapingStatus) setCancelFunc(cancelFunc context.CancelFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cancelFunc = cancelFunc
}

func (s *scrapingStatus) cancel() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancelFunc != nil && s.running {
		s.cancelFunc()
		s.running = false
		return true
	}
	return false
}

func (s *scrapingStatus) isRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

var scrapingStatusInstance = &scrapingStatus{}

// ClearScrapingStatus resets the global scraping status — used for testing.
func ClearScrapingStatus() {
	scrapingStatusInstance.clear()
}

// IsScrapingRunning reports whether a media.scrape operation is currently active.
func IsScrapingRunning() bool {
	return scrapingStatusInstance.isRunning()
}

// HandleMediaScrape starts a named scraper as a tracked background operation.
// Returns immediately with a null result; progress is broadcast as
// "media.scraping" notifications. Scraping and indexing are mutually exclusive.
func HandleMediaScrape(env requests.RequestEnv) (any, error) { //nolint:gocritic // single-use parameter in API handler
	var params models.MediaScrapeParams
	if err := validation.ValidateAndUnmarshal(env.Params, &params); err != nil {
		return nil, models.ClientErrf("invalid params: %w", err)
	}

	s, ok := env.Scrapers[params.ScraperID]
	if !ok {
		return nil, models.ClientErrf("unknown scraper: %s", params.ScraperID)
	}

	if statusInstance.isRunning() {
		return nil, models.ClientErrf("media indexing is in progress")
	}
	if !scrapingStatusInstance.startIfNotRunning(params.ScraperID) {
		return nil, models.ClientErrf("scraping already in progress")
	}

	// Use app-scoped context — scraping outlives the API request.
	scrapeCtx, cancelFunc := context.WithCancel(env.State.GetContext())
	scrapingStatusInstance.setCancelFunc(cancelFunc)

	opts := scraper.ScrapeOptions{Systems: params.Systems, Force: params.Force}
	ch, err := s.Scrape(scrapeCtx, opts)
	if err != nil {
		cancelFunc()
		scrapingStatusInstance.clear()
		return nil, fmt.Errorf("failed to start scraper: %w", err)
	}

	ns := env.State.Notifications
	db := env.Database

	notifications.MediaScraping(ns, models.ScrapingStatusResponse{
		ScraperID: params.ScraperID,
		Scraping:  true,
	})

	db.MediaDB.TrackBackgroundOperation()
	go func() {
		defer db.MediaDB.BackgroundOperationDone()
		defer cancelFunc()

		for update := range ch {
			notifications.MediaScraping(ns, models.ScrapingStatusResponse{
				ScraperID: params.ScraperID,
				SystemID:  update.SystemID,
				Processed: update.Processed,
				Total:     update.Total,
				Matched:   update.Matched,
				Skipped:   update.Skipped,
				Scraping:  !update.Done,
				Done:      update.Done,
			})
		}

		scrapingStatusInstance.clear()
		notifications.MediaScraping(ns, models.ScrapingStatusResponse{
			ScraperID: params.ScraperID,
			Scraping:  false,
			Done:      true,
		})
		log.Info().Str("scraper", params.ScraperID).Msg("scraper run complete")
	}()

	return nil, nil //nolint:nilnil // API handler returns nil result and nil error for async start
}

// HandleMediaScrapeCancel cancels the currently running media.scrape operation.
//
//nolint:gocritic // API handler signature; large env param cannot be passed by pointer
func HandleMediaScrapeCancel(_ requests.RequestEnv) (any, error) {
	if scrapingStatusInstance.cancel() {
		return map[string]any{"message": "scraping cancelled"}, nil
	}
	return map[string]any{"message": "no scraping operation is currently running"}, nil
}
