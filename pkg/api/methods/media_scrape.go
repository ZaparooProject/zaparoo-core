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
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
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
	latest     models.ScrapingStatusResponse
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
	s.latest = models.ScrapingStatusResponse{
		ScraperID: scraperID,
		Scraping:  true,
	}
	return true
}

func (s *scrapingStatus) clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.running = false
	s.scraperID = ""
	s.cancelFunc = nil
	s.latest = models.ScrapingStatusResponse{}
}

// clearIfOwner clears state only when the caller's scraperID matches the stored one.
// This prevents a cancelled goroutine from clobbering a freshly-started scrape that
// reused the slot after cancel() returned but before the old goroutine reached clear().
func (s *scrapingStatus) clearIfOwner(scraperID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.scraperID != scraperID {
		return
	}
	s.running = false
	s.scraperID = ""
	s.cancelFunc = nil
}

func (s *scrapingStatus) setLatest(status *models.ScrapingStatusResponse) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.latest = *status
}

func (s *scrapingStatus) getLatest() models.ScrapingStatusResponse {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.latest
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
		s.latest.Scraping = false
		s.latest.Done = true
		s.latest.Paused = false
		// Do NOT clear running/scraperID here. The goroutine's deferred
		// clearIfOwner call is the single writer for those fields, preventing
		// a new scrape from starting only to have its state cleared by the
		// old goroutine winding down.
		return true
	}
	return false
}

func (s *scrapingStatus) isRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

func publishScrapingStatus(ns chan<- models.Notification, status *models.ScrapingStatusResponse) {
	scrapingStatusInstance.setLatest(status)
	notifications.MediaScraping(ns, status)
}

func populateScrapedMediaCount(
	ctx context.Context,
	db *database.Database,
	status *models.ScrapingStatusResponse,
) {
	if db == nil || db.MediaDB == nil {
		return
	}

	var (
		count int
		err   error
	)
	if status.ScraperID != "" {
		count, err = db.MediaDB.GetScrapedMediaCount(ctx, status.ScraperID)
	} else {
		count, err = db.MediaDB.GetTotalScrapedMediaCount(ctx)
	}
	if err != nil {
		log.Warn().Err(err).Str("scraper", status.ScraperID).Msg("failed to get scraped media count")
		return
	}
	status.TotalScraped = count
}

func PublishScrapePauseStatus(ns chan<- models.Notification, paused bool) {
	status := scrapingStatusInstance.getLatest()
	status.Scraping = true
	status.Paused = paused
	publishScrapingStatus(ns, &status)
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

	if err := startScrapingIfNoIndex(params.ScraperID); err != nil {
		return nil, err
	}

	// Use app-scoped context — scraping outlives the API request.
	scrapeCtx, cancelFunc := context.WithCancel(env.State.GetContext())
	scrapingStatusInstance.setCancelFunc(cancelFunc)

	paused := env.ScrapePauser != nil && env.ScrapePauser.IsPaused()
	opts := scraper.ScrapeOptions{Systems: params.Systems, Force: params.Force, Pauser: env.ScrapePauser}
	ch, err := s.Scrape(scrapeCtx, env.ScrapeEnv, opts)
	if err != nil {
		cancelFunc()
		scrapingStatusInstance.clear()
		return nil, fmt.Errorf("failed to start scraper: %w", err)
	}

	ns := env.State.Notifications
	db := env.Database

	initialStatus := models.ScrapingStatusResponse{
		ScraperID: params.ScraperID,
		Scraping:  true,
		Paused:    paused,
	}
	populateScrapedMediaCount(env.State.GetContext(), db, &initialStatus)
	publishScrapingStatus(ns, &initialStatus)

	scraperID := params.ScraperID
	db.MediaDB.TrackBackgroundOperation()
	go func() {
		defer db.MediaDB.BackgroundOperationDone()
		defer cancelFunc()
		defer scrapingStatusInstance.clearIfOwner(scraperID)

		var receivedDone bool
		for update := range ch {
			if update.Done {
				receivedDone = true
			}
			status := models.ScrapingStatusResponse{
				ScraperID: scraperID,
				SystemID:  update.SystemID,
				Processed: update.Processed,
				Total:     update.Total,
				Matched:   update.Matched,
				Skipped:   update.Skipped,
				Scraping:  !update.Done,
				Done:      update.Done,
				Paused:    env.ScrapePauser != nil && env.ScrapePauser.IsPaused() && !update.Done,
			}
			populateScrapedMediaCount(env.State.GetContext(), db, &status)
			publishScrapingStatus(ns, &status)
		}

		// Only send a terminal notification if the channel closed without a
		// Done=true update (e.g. scraper returned a pre-closed empty channel).
		// Otherwise the channel already delivered the final counters and sending
		// another zeroed-out Done would overwrite them for consumers.
		if !receivedDone {
			status := scrapingStatusInstance.getLatest()
			status.ScraperID = scraperID
			status.Scraping = false
			status.Done = true
			status.Paused = false
			terminalStatus := models.ScrapingStatusResponse{
				ScraperID: status.ScraperID,
				SystemID:  status.SystemID,
				Processed: status.Processed,
				Total:     status.Total,
				Matched:   status.Matched,
				Skipped:   status.Skipped,
				Scraping:  status.Scraping,
				Done:      status.Done,
				Paused:    status.Paused,
			}
			populateScrapedMediaCount(env.State.GetContext(), db, &terminalStatus)
			publishScrapingStatus(ns, &terminalStatus)
		}
		log.Info().Str("scraper", scraperID).Msg("scraper run complete")
	}()

	return nil, nil //nolint:nilnil // API handler returns nil result and nil error for async start
}

// HandleMediaScrapeStatus returns the latest known media.scrape status snapshot.
//
//nolint:gocritic // API handler signature; large env param cannot be passed by pointer
func HandleMediaScrapeStatus(env requests.RequestEnv) (any, error) {
	status := scrapingStatusInstance.getLatest()
	if status.Scraping && env.ScrapePauser != nil {
		status.Paused = env.ScrapePauser.IsPaused()
	}
	if env.Database == nil || env.Database.MediaDB == nil {
		return status, nil
	}

	populateScrapedMediaCount(env.Context, env.Database, &status)

	return status, nil
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

// HandleMediaScrapeResume manually resumes a paused media.scrape operation.
//
//nolint:gocritic // API handler signature; large env param cannot be passed by pointer
func HandleMediaScrapeResume(env requests.RequestEnv) (any, error) {
	log.Info().Msg("received media scrape resume request")

	if env.ScrapePauser == nil || !env.ScrapePauser.IsPaused() {
		return map[string]any{"message": "Media scraping is not paused"}, nil
	}

	env.ScrapePauser.Resume()
	if scrapingStatusInstance.isRunning() {
		PublishScrapePauseStatus(env.State.Notifications, false)
	}
	log.Info().Msg("media scraping manually resumed")

	return map[string]any{"message": "Media scraping resumed"}, nil
}

// HandleScrapers returns the list of registered scrapers with their IDs and
// human-readable names.
//
//nolint:gocritic // API handler signature; large env param cannot be passed by pointer
func HandleScrapers(env requests.RequestEnv) (any, error) {
	infos := make([]models.ScraperInfo, 0, len(env.Scrapers))
	for _, s := range env.Scrapers {
		infos = append(infos, models.ScraperInfo{
			ID:               s.ID(),
			Name:             s.Name(),
			SupportedSystems: s.SupportedSystems(),
		})
	}
	return models.ScrapersResponse{Scrapers: infos}, nil
}
