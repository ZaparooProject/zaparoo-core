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
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/notifications"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/validation"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/assets"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediadb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
)

// scrapingStatus tracks the lifecycle of an active media.scrape operation.
// It mirrors the indexingStatus pattern in media.go for consistent state
// management and safe concurrent access.
const (
	scrapeTotalScrapedRefreshInterval = 5 * time.Second
	scrapeStateIdle                   = "idle"
	scrapeStateRunning                = "running"
	scrapeStatePaused                 = "paused"
	scrapeStateCompleted              = "completed"
	scrapeStateCancelled              = "cancelled"
	scrapeStateFailed                 = "failed"
)

type scrapedCountCache struct {
	lastRefresh time.Time
	scraperID   string
	count       int
	valid       bool
}

type scrapingStatus struct {
	cancelFunc context.CancelFunc
	scraperID  string
	countCache scrapedCountCache
	latest     models.ScrapingStatusResponse
	mu         syncutil.RWMutex
	force      bool
	running    bool
}

func (s *scrapingStatus) startIfNotRunning(scraperID string, force bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return false
	}
	s.running = true
	s.scraperID = scraperID
	s.force = force
	s.countCache = scrapedCountCache{}
	s.latest = models.ScrapingStatusResponse{
		ScraperID: scraperID,
		State:     scrapeStateRunning,
		Scraping:  true,
		Force:     force,
	}
	return true
}

func (s *scrapingStatus) clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.running = false
	s.scraperID = ""
	s.force = false
	s.cancelFunc = nil
	s.latest = models.ScrapingStatusResponse{}
	s.countCache = scrapedCountCache{}
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
	s.force = false
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
	status := s.latest
	status.Force = s.force
	return status
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
		s.latest.State = scrapeStateCancelled
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

func (s *scrapingStatus) getFreshCountCache(scraperID string, now time.Time) (int, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.countCache.valid || s.countCache.scraperID != scraperID {
		return 0, false
	}
	if now.Sub(s.countCache.lastRefresh) >= scrapeTotalScrapedRefreshInterval {
		return 0, false
	}
	return s.countCache.count, true
}

func (s *scrapingStatus) getAnyCountCache(scraperID string) (int, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.countCache.valid || s.countCache.scraperID != scraperID {
		return 0, false
	}
	return s.countCache.count, true
}

func (s *scrapingStatus) updateCountCache(scraperID string, count int, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.countCache.valid && s.countCache.scraperID == scraperID && count < s.countCache.count {
		count = s.countCache.count
	}
	s.countCache = scrapedCountCache{
		scraperID:   scraperID,
		count:       count,
		lastRefresh: now,
		valid:       true,
	}
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
	populateScrapedMediaCountExact(ctx, db, status)
}

func populateScrapedMediaCountExact(
	ctx context.Context,
	db *database.Database,
	status *models.ScrapingStatusResponse,
) {
	count, ok := queryScrapedMediaCount(ctx, db, status.ScraperID)
	if !ok {
		if cached, cachedOK := scrapingStatusInstance.getAnyCountCache(status.ScraperID); cachedOK {
			status.TotalScraped = cached
		}
		return
	}
	status.TotalScraped = count
	scrapingStatusInstance.updateCountCache(status.ScraperID, count, time.Now())
}

func populateScrapedMediaCountCached(
	ctx context.Context,
	db *database.Database,
	status *models.ScrapingStatusResponse,
) {
	if cached, ok := scrapingStatusInstance.getFreshCountCache(status.ScraperID, time.Now()); ok {
		status.TotalScraped = cached
		return
	}

	count, ok := queryScrapedMediaCount(ctx, db, status.ScraperID)
	if !ok {
		if cached, cachedOK := scrapingStatusInstance.getAnyCountCache(status.ScraperID); cachedOK {
			status.TotalScraped = cached
		}
		return
	}
	if cached, cachedOK := scrapingStatusInstance.getAnyCountCache(status.ScraperID); cachedOK && count < cached {
		count = cached
	}
	status.TotalScraped = count
	scrapingStatusInstance.updateCountCache(status.ScraperID, count, time.Now())
}

func queryScrapedMediaCount(ctx context.Context, db *database.Database, scraperID string) (int, bool) {
	if db == nil || db.MediaDB == nil {
		return 0, false
	}

	var (
		count int
		err   error
	)
	if scraperID != "" {
		count, err = db.MediaDB.GetScrapedMediaCount(ctx, scraperID)
	} else {
		count, err = db.MediaDB.GetTotalScrapedMediaCount(ctx)
	}
	if err != nil {
		log.Warn().Err(err).Str("scraper", scraperID).Msg("failed to get scraped media count")
		return 0, false
	}
	return count, true
}

func systemProgressDisplay(systemID string) string {
	if systemID == "" {
		return ""
	}
	md, err := assets.GetSystemMetadata(systemID)
	if err != nil || md.Name == "" {
		return systemID
	}
	return md.Name
}

func ptrIfPositive(v int) *int {
	if v <= 0 {
		return nil
	}
	return &v
}

func ptrIfNotEmpty(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}

func scrapeState(scrapeCtx context.Context, update *scraper.ScrapeUpdate, paused bool) string {
	switch {
	case update.FatalErr != nil:
		return scrapeStateFailed
	case update.Done && scrapeCtx != nil && scrapeCtx.Err() != nil:
		return scrapeStateCancelled
	case update.Done:
		return scrapeStateCompleted
	case paused:
		return scrapeStatePaused
	default:
		return scrapeStateRunning
	}
}

func scrapingStatusFromUpdate(
	scrapeCtx context.Context,
	scraperID string,
	force bool,
	update *scraper.ScrapeUpdate,
	paused bool,
) models.ScrapingStatusResponse {
	display := systemProgressDisplay(update.SystemID)
	status := models.ScrapingStatusResponse{
		ScraperID:          scraperID,
		SystemID:           update.SystemID,
		Processed:          update.Processed,
		Total:              update.Total,
		Matched:            update.Matched,
		Skipped:            update.Skipped,
		Scraping:           !update.Done,
		Done:               update.Done,
		Paused:             paused && !update.Done,
		State:              scrapeState(scrapeCtx, update, paused && !update.Done),
		Force:              force,
		TotalSteps:         ptrIfPositive(update.TotalSteps),
		CurrentStep:        ptrIfPositive(update.CurrentStep),
		CurrentStepDisplay: ptrIfNotEmpty(display),
	}
	if update.FatalErr != nil {
		status.Error = update.FatalErr.Error()
	}
	if update.SystemID != "" {
		status.CurrentSystem = &models.ScrapeSystemProgressResponse{
			SystemID:   update.SystemID,
			SystemName: display,
			Processed:  update.Processed,
			Total:      update.Total,
			Matched:    update.Matched,
			Skipped:    update.Skipped,
		}
	}
	return status
}

func PublishScrapePauseStatus(ns chan<- models.Notification, paused bool) {
	status := scrapingStatusInstance.getLatest()
	status.Scraping = true
	status.Paused = paused
	status.State = scrapeStateRunning
	if paused {
		status.State = scrapeStatePaused
	}
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

	return startMediaScrape(&env, params)
}

func ResumeMediaScrape(env *requests.RequestEnv, operation database.ScrapingOperation) error {
	params := models.MediaScrapeParams{
		ScraperID: operation.ScraperID,
		Systems:   operation.Systems,
		Force:     operation.Force,
	}
	_, err := startMediaScrape(env, params)
	return err
}

func startMediaScrape(env *requests.RequestEnv, params models.MediaScrapeParams) (any, error) {
	platformScrapers := env.Platform.Scrapers(env.Config)
	s, ok := platformScrapers[params.ScraperID]
	if !ok {
		return nil, models.ClientErrf("unknown scraper: %s", params.ScraperID)
	}
	if s.Scrape == nil {
		return nil, fmt.Errorf("scraper %q has no Scrape function", s.ID)
	}

	if err := startScrapingIfNoIndex(params.ScraperID, params.Force); err != nil {
		return nil, err
	}

	operation := database.ScrapingOperation{
		ScraperID: params.ScraperID,
		Systems:   params.Systems,
		Force:     params.Force,
	}
	if err := env.Database.MediaDB.SetScrapingOperation(operation); err != nil {
		scrapingStatusInstance.clearIfOwner(params.ScraperID)
		return nil, fmt.Errorf("failed to persist scraping operation: %w", err)
	}
	if err := env.Database.MediaDB.SetScrapingStatus(mediadb.IndexingStatusRunning); err != nil {
		scrapingStatusInstance.clearIfOwner(params.ScraperID)
		return nil, fmt.Errorf("failed to persist scraping status: %w", err)
	}

	// Use app-scoped context — scraping outlives the API request.
	scrapeCtx, cancelFunc := context.WithCancel(env.State.GetContext())
	scrapingStatusInstance.setCancelFunc(cancelFunc)

	paused := env.ScrapePauser != nil && env.ScrapePauser.IsPaused()
	opts := scraper.ScrapeOptions{Systems: params.Systems, Force: params.Force, Pauser: env.ScrapePauser}
	ch := make(chan scraper.ScrapeUpdate, 32)
	if err := s.Scrape(scrapeCtx, env.Config, env.Platform, afero.NewOsFs(), env.Database, opts, nil, ch); err != nil {
		cancelFunc()
		scrapingStatusInstance.clear()
		if statusErr := env.Database.MediaDB.SetScrapingStatus(mediadb.IndexingStatusFailed); statusErr != nil {
			log.Warn().Err(statusErr).Msg("failed to persist scraping failure status")
		}
		return nil, fmt.Errorf("failed to start scraper: %w", err)
	}

	ns := env.State.Notifications
	db := env.Database

	initialState := scrapeStateRunning
	if paused {
		initialState = scrapeStatePaused
	}
	initialStatus := models.ScrapingStatusResponse{
		ScraperID: params.ScraperID,
		State:     initialState,
		Scraping:  true,
		Paused:    paused,
		Force:     params.Force,
	}
	populateScrapedMediaCountExact(env.State.GetContext(), db, &initialStatus)
	publishScrapingStatus(ns, &initialStatus)

	scraperID := params.ScraperID
	db.MediaDB.TrackBackgroundOperation()
	go func() {
		defer scrapingStatusInstance.clearIfOwner(scraperID)
		defer cancelFunc()
		defer db.MediaDB.BackgroundOperationDone()

		finalStatus := mediadb.IndexingStatusCompleted
		var receivedDone bool
		for update := range ch {
			if update.Done {
				receivedDone = true
			}
			paused := env.ScrapePauser != nil && env.ScrapePauser.IsPaused()
			status := scrapingStatusFromUpdate(scrapeCtx, scraperID, params.Force, &update, paused)
			if update.FatalErr != nil {
				finalStatus = mediadb.IndexingStatusFailed
			}
			if update.Done && scrapeCtx.Err() != nil {
				finalStatus = mediadb.IndexingStatusCancelled
			}
			if update.Done {
				populateScrapedMediaCountExact(env.State.GetContext(), db, &status)
				mediaImageNoImages.clear()
			} else {
				populateScrapedMediaCountCached(env.State.GetContext(), db, &status)
			}
			publishScrapingStatus(ns, &status)
		}

		// Only send a terminal notification if the channel closed without a
		// Done=true update (e.g. scraper returned a pre-closed empty channel).
		// Otherwise the channel already delivered the final counters and sending
		// another zeroed-out Done would overwrite them for consumers.
		if !receivedDone {
			terminalStatus := scrapingStatusInstance.getLatest()
			terminalStatus.ScraperID = scraperID
			terminalStatus.Force = params.Force
			terminalStatus.Scraping = false
			terminalStatus.Done = true
			terminalStatus.Paused = false
			terminalStatus.State = scrapeStateCompleted
			if scrapeCtx.Err() != nil {
				terminalStatus.State = scrapeStateCancelled
				finalStatus = mediadb.IndexingStatusCancelled
			}
			populateScrapedMediaCountExact(env.State.GetContext(), db, &terminalStatus)
			mediaImageNoImages.clear()
			publishScrapingStatus(ns, &terminalStatus)
		}
		if err := db.MediaDB.SetScrapingStatus(finalStatus); err != nil {
			log.Warn().Err(err).Str("scraper", scraperID).Msg("failed to persist scraping terminal status")
		}
		if finalStatus == mediadb.IndexingStatusCompleted || finalStatus == mediadb.IndexingStatusCancelled {
			if err := db.MediaDB.ClearScrapingOperation(); err != nil {
				log.Warn().Err(err).Str("scraper", scraperID).Msg("failed to clear scraping operation")
			}
		}
		log.Info().Str("scraper", scraperID).Str("status", finalStatus).Msg("scraper run complete")
	}()

	return nil, nil //nolint:nilnil // API handler returns nil result and nil error for async start
}

// HandleMediaScrapeStatus returns the latest known media.scrape status snapshot.
//
//nolint:gocritic // API handler signature; large env param cannot be passed by pointer
func HandleMediaScrapeStatus(env requests.RequestEnv) (any, error) {
	status := scrapingStatusInstance.getLatest()
	if status.State == "" {
		status.State = scrapeStateIdle
		if status.Scraping {
			status.State = scrapeStateRunning
		} else if persisted, ok := persistedScrapingStatus(env); ok {
			status = persisted
		}
	}
	if status.Scraping && env.ScrapePauser != nil {
		status.Paused = env.ScrapePauser.IsPaused()
		status.State = scrapeStateRunning
		if status.Paused {
			status.State = scrapeStatePaused
		}
	}
	if env.Database == nil || env.Database.MediaDB == nil {
		return status, nil
	}

	countCtx, cancel := optionalDBEnrichmentContext(env.Context)
	defer cancel()
	populateScrapedMediaCount(countCtx, env.Database, &status)

	return status, nil
}

// HandleMediaScrapeCancel cancels the currently running media.scrape operation.
//
//nolint:gocritic // API handler signature; large env param cannot be passed by pointer
func persistedScrapingStatus(env requests.RequestEnv) (models.ScrapingStatusResponse, bool) {
	if env.Database == nil || env.Database.MediaDB == nil {
		return models.ScrapingStatusResponse{}, false
	}
	status, err := env.Database.MediaDB.GetScrapingStatus()
	if err != nil || (status != mediadb.IndexingStatusRunning && status != mediadb.IndexingStatusPending) {
		return models.ScrapingStatusResponse{}, false
	}
	operation, found, err := env.Database.MediaDB.GetScrapingOperation()
	if err != nil || !found || operation.ScraperID == "" {
		return models.ScrapingStatusResponse{}, false
	}
	return models.ScrapingStatusResponse{
		ScraperID: operation.ScraperID,
		Scraping:  true,
		State:     scrapeStateRunning,
		Force:     operation.Force,
	}, true
}

func HandleMediaScrapeCancel(env requests.RequestEnv) (any, error) { //nolint:gocritic // API handler signature
	if scrapingStatusInstance.cancel() {
		if env.Database != nil && env.Database.MediaDB != nil {
			if err := env.Database.MediaDB.SetScrapingStatus(mediadb.IndexingStatusCancelled); err != nil {
				log.Warn().Err(err).Msg("failed to persist scraping cancellation status")
			}
			if err := env.Database.MediaDB.ClearScrapingOperation(); err != nil {
				log.Warn().Err(err).Msg("failed to clear scraping operation after cancellation")
			}
		}
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
	platformScrapers := env.Platform.Scrapers(env.Config)
	infos := make([]models.ScraperInfo, 0, len(platformScrapers))
	for _, s := range platformScrapers {
		infos = append(infos, models.ScraperInfo{
			ID:               s.ID,
			Name:             s.Name,
			SupportedSystems: s.SupportedSystemIDs,
		})
	}
	return models.ScrapersResponse{Scrapers: infos}, nil
}
