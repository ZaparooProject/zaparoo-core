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
	scrapeTotalScrapedFailureBackoff  = 60 * time.Second
	scrapeTotalScrapedStatusTimeout   = 2 * time.Second
	scrapeStateIdle                   = "idle"
	scrapeStateRunning                = "running"
	scrapeStatePaused                 = "paused"
	scrapeStateCompleted              = "completed"
	scrapeStateCancelled              = "cancelled"
	scrapeStateFailed                 = "failed"
)

type scrapedCountCache struct {
	lastRefresh time.Time
	lastFailure time.Time
	scraperID   string
	count       int
	valid       bool
}

type scrapingStatus struct {
	cancelFunc      context.CancelFunc
	scraperID       string
	countCache      scrapedCountCache
	latest          models.ScrapingStatusResponse
	mu              syncutil.RWMutex
	force           bool
	running         bool
	cancelRequested bool
}

func (s *scrapingStatus) startIfNotRunning(scraperID string, force bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return false
	}
	s.running = true
	s.cancelRequested = false
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
	if s.cancelRequested {
		cancelFunc()
	}
}

func (s *scrapingStatus) cancel() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cancelLocked()
}

func (s *scrapingStatus) cancelLocked() bool {
	if s.running {
		s.cancelRequested = true
		if s.cancelFunc != nil {
			s.cancelFunc()
		}
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

func (s *scrapingStatus) userCancelled() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cancelRequested
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

func (s *scrapingStatus) countRefreshBackedOff(scraperID string, now time.Time) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.countCache.scraperID != scraperID || s.countCache.lastFailure.IsZero() {
		return false
	}
	return now.Sub(s.countCache.lastFailure) < scrapeTotalScrapedFailureBackoff
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

func (s *scrapingStatus) updateCountFailure(scraperID string, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.countCache.scraperID != scraperID {
		s.countCache = scrapedCountCache{scraperID: scraperID}
	}
	s.countCache.lastFailure = now
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
	populateScrapedMediaCountCached(ctx, db, status)
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
	now := time.Now()
	if cached, ok := scrapingStatusInstance.getFreshCountCache(status.ScraperID, now); ok {
		status.TotalScraped = cached
		return
	}
	if scrapingStatusInstance.countRefreshBackedOff(status.ScraperID, now) {
		if cached, cachedOK := scrapingStatusInstance.getAnyCountCache(status.ScraperID); cachedOK {
			status.TotalScraped = cached
		}
		return
	}

	count, ok := queryScrapedMediaCount(ctx, db, status.ScraperID)
	queryDone := time.Now()
	if !ok {
		scrapingStatusInstance.updateCountFailure(status.ScraperID, queryDone)
		if cached, cachedOK := scrapingStatusInstance.getAnyCountCache(status.ScraperID); cachedOK {
			status.TotalScraped = cached
		}
		return
	}
	if cached, cachedOK := scrapingStatusInstance.getAnyCountCache(status.ScraperID); cachedOK && count < cached {
		count = cached
	}
	status.TotalScraped = count
	scrapingStatusInstance.updateCountCache(status.ScraperID, count, queryDone)
}

func scrapeCountStatusContext(parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	//nolint:gosec // Caller owns and invokes returned cancel function.
	return context.WithTimeout(parent, scrapeTotalScrapedStatusTimeout)
}

func invalidateChangedScrapeThumbnails(mediaDB database.MediaDBI) {
	systems, all := mediaDB.ConsumeScrapeImageChanges()
	if all {
		WipeMediaThumbCache()
		return
	}
	WipeMediaThumbCacheSystems(systems)
}

func queryScrapedMediaCount(ctx context.Context, db *database.Database, scraperID string) (int, bool) {
	if db == nil || db.MediaDB == nil {
		return 0, false
	}

	started := time.Now()
	queryKind := "total"
	var (
		count int
		err   error
	)
	if scraperID != "" {
		queryKind = "per_scraper"
		count, err = db.MediaDB.GetScrapedMediaCount(ctx, scraperID)
	} else {
		count, err = db.MediaDB.GetTotalScrapedMediaCount(ctx)
	}
	duration := time.Since(started)
	if err != nil {
		timedOut := errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded)
		log.Warn().Err(err).
			Str("scraper", scraperID).
			Str("queryKind", queryKind).
			Int64("durationMs", duration.Milliseconds()).
			Bool("timeout", timedOut).
			Msg("failed to get scraped media count")
		return 0, false
	}
	log.Debug().
		Str("scraper", scraperID).
		Str("queryKind", queryKind).
		Int64("durationMs", duration.Milliseconds()).
		Int("count", count).
		Msg("got scraped media count")
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
	throttled bool,
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
		Throttled:          throttled && !update.Done,
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

func PublishScrapePauseStatus(ns chan<- models.Notification, paused, throttled bool) {
	status := scrapingStatusInstance.getLatest()
	status.Scraping = true
	status.Paused = paused
	status.Throttled = throttled
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

//nolint:gocritic // Preserve the existing service resume contract.
func ResumeMediaScrape(env *requests.RequestEnv, operation database.ScrapingOperation) error {
	_, err := startMediaScrapeOperation(env, &operation, true)
	return err
}

func startMediaScrape(env *requests.RequestEnv, params models.MediaScrapeParams) (any, error) {
	return startMediaScrapeWithRunID(env, params, "")
}

func startMediaScrapeWithRunID(env *requests.RequestEnv, params models.MediaScrapeParams, runID string) (any, error) {
	return startMediaScrapeOperation(env, &database.ScrapingOperation{
		ScraperID: params.ScraperID, Systems: params.Systems, Force: params.Force, RunID: runID,
	}, false)
}

func startMediaScrapeOperation(
	env *requests.RequestEnv, request *database.ScrapingOperation, resume bool,
) (any, error) {
	operation := *request
	if err := operation.Validate(); err != nil {
		return nil, models.ClientErrf("invalid scraping operation options: %w", err)
	}
	params := models.MediaScrapeParams{
		ScraperID: operation.ScraperID, Systems: operation.Systems, Force: operation.Force,
	}
	// A resumed or queued job already carries a resolved scope; a fresh request
	// resolves one from its parameters below.
	scope := operation.Scope
	platformScrapers := env.Platform.Scrapers(env.Config)
	s, ok := platformScrapers[params.ScraperID]
	var startErr error
	switch {
	case !ok:
		startErr = models.ClientErrf("unknown scraper: %s", params.ScraperID)
	case s.Scrape == nil:
		startErr = fmt.Errorf("scraper %q has no Scrape function", s.ID)
	case operation.FillMissing && !s.SupportsFillMissing:
		startErr = models.ClientErrf("scraper %q does not support fill-missing", s.ID)
	}
	if startErr != nil && len(operation.Pending) == 0 && !resume {
		return nil, startErr
	}

	lease, err := startScraping(env.Database.MediaDB, params.ScraperID, params.Force)
	if err != nil {
		return nil, err
	}
	leaseOwned := true
	defer func() {
		if leaseOwned {
			lease.Release()
		}
	}()

	// Resolve while holding the indexing exclusion lease so IDs cannot change
	// between validation and the background scraper's selection queries.
	if scope == nil {
		scope, err = resolveScrapeScope(env, params)
	} else {
		err = scope.Validate()
		if err == nil && !scope.Subtree {
			var rows []database.MediaFullRow
			rows, err = env.Database.MediaDB.GetScrapeMedia(env.Context, *scope)
			if err == nil && len(rows) != 1 {
				err = models.ClientErrf("stored scrape media no longer exists")
			}
		}
	}
	if err != nil {
		scrapingStatusInstance.clear()
		return nil, err
	}
	if scope != nil {
		params.Systems = []string{scope.SystemID}
	}

	ns := env.State.Notifications
	db := env.Database
	preparingStatus := models.ScrapingStatusResponse{
		ScraperID:          params.ScraperID,
		State:              scrapeStateRunning,
		Scraping:           true,
		Force:              params.Force,
		CurrentStepDisplay: ptrIfNotEmpty(preparingMediaScrapeDisplay),
	}
	publishScrapingStatus(ns, &preparingStatus)

	// The durable record has to carry the resolved scope, or a resume after a
	// restart would widen the request back to its whole system.
	operation.Scope, operation.Systems = scope, params.Systems
	if err := scrapingStatusInstance.persistStart(env.Database.MediaDB, &operation, resume); err != nil {
		scrapingStatusInstance.clearIfOwner(params.ScraperID)
		publishScrapingStatus(ns, &models.ScrapingStatusResponse{
			ScraperID: params.ScraperID,
			State:     scrapeStateFailed,
			Force:     params.Force,
			Done:      true,
			Error:     "failed to start media scrape",
		})
		return nil, fmt.Errorf("failed to persist scraping operation: %w", err)
	}
	runID := operation.RunID
	params.Systems, params.Force = operation.Systems, operation.Force

	// Use app-scoped context — scraping outlives the API request.
	scrapeCtx, cancelFunc := context.WithCancel(env.State.GetContext())
	scrapingStatusInstance.setCancelFunc(cancelFunc)

	// Reconcile with current primary-media state before reporting initial
	// paused status. This clears stale pauses left by non-primary media events.
	syncMediaWorkPauserWithActiveMedia(env.Config, env.State.ActiveMedia(), env.ScrapePauser)

	paused := env.ScrapePauser != nil && env.ScrapePauser.IsPaused()
	throttled := env.ScrapePauser != nil && env.ScrapePauser.IsThrottled()
	opts := scraper.ScrapeOptions{
		Scope: scope, Systems: params.Systems, RunID: runID, Force: params.Force,
		FillMissing: operation.FillMissing, Pauser: env.ScrapePauser,
	}
	ch := make(chan scraper.ScrapeUpdate, 32)
	// Scraper startup can read/write the database before it returns a channel
	// producer. Register that work before shutdown can begin draining it.
	db.MediaDB.TrackBackgroundOperation()
	startupOwned := true
	defer func() {
		if startupOwned {
			db.MediaDB.BackgroundOperationDone()
		}
	}()
	if startErr == nil {
		startErr = s.Scrape(scrapeCtx, env.Config, env.Platform, afero.NewOsFs(), env.Database, opts, nil, ch)
	}
	if startErr != nil && len(operation.Pending) > 0 {
		ch <- scraper.ScrapeUpdate{FatalErr: startErr, Done: true}
		close(ch)
	} else if startErr != nil {
		cancelFunc()
		scrapingStatusInstance.clear()
		failureStatus := mediadb.IndexingStatusFailed
		if env.State.GetContext().Err() != nil && !scrapingStatusInstance.userCancelled() {
			failureStatus = mediadb.IndexingStatusPending
		}
		statusErr := scrapingStatusInstance.persistTerminal(env.Database.MediaDB, &operation, failureStatus)
		if statusErr != nil {
			log.Warn().Err(statusErr).Msg("failed to persist scraping failure status")
		}
		publishScrapingStatus(ns, &models.ScrapingStatusResponse{
			ScraperID: params.ScraperID,
			State:     scrapeStateFailed,
			Force:     params.Force,
			Done:      true,
			Error:     "failed to start media scrape",
		})
		return nil, fmt.Errorf("failed to start scraper: %w", startErr)
	}

	initialState := scrapeStateRunning
	if paused {
		initialState = scrapeStatePaused
	}
	initialStatus := models.ScrapingStatusResponse{
		ScraperID: params.ScraperID,
		State:     initialState,
		Scraping:  true,
		Paused:    paused,
		Throttled: throttled,
		Force:     params.Force,
	}
	populateScrapedMediaCountExact(env.State.GetContext(), db, &initialStatus)
	publishScrapingStatus(ns, &initialStatus)

	scraperID := params.ScraperID
	startupOwned = false
	leaseOwned = false
	go func() {
		defer lease.Release()
		defer func() { scrapingStatusInstance.clearIfOwner(scraperID) }()
		defer cancelFunc()
		defer db.MediaDB.BackgroundOperationDone()

		for {
			finalStatus := mediadb.IndexingStatusCompleted
			var receivedDone bool
			for update := range ch {
				if update.Done {
					receivedDone = true
				}
				paused := env.ScrapePauser != nil && env.ScrapePauser.IsPaused()
				throttled := env.ScrapePauser != nil && env.ScrapePauser.IsThrottled()
				status := scrapingStatusFromUpdate(scrapeCtx, scraperID, params.Force, &update, paused, throttled)
				if update.FatalErr != nil {
					finalStatus = mediadb.IndexingStatusFailed
				}
				if update.Done && scrapeCtx.Err() != nil {
					finalStatus = mediadb.IndexingStatusCancelled
				}
				if update.Done {
					populateScrapedMediaCountExact(env.State.GetContext(), db, &status)
				} else {
					populateScrapedMediaCountCached(env.State.GetContext(), db, &status)
				}
				publishScrapingStatus(ns, &status)
			}

			if scrapeCtx.Err() != nil {
				finalStatus = mediadb.IndexingStatusCancelled
				if env.State.GetContext().Err() != nil && !scrapingStatusInstance.userCancelled() {
					finalStatus = mediadb.IndexingStatusPending
				}
			}
			// Scrape writes commit incrementally, so changed artwork must be
			// invalidated on every terminal outcome, including failure/cancellation.
			mediaImageNoImages.clear()
			invalidateChangedScrapeThumbnails(db.MediaDB)

			// Only synthesize a completed notification if the channel closed without
			// a Done=true update and no failure/cancel status was observed.
			// Otherwise the channel already delivered the terminal state, or there is
			// no successful completion to announce.
			if !receivedDone && finalStatus == mediadb.IndexingStatusCompleted {
				terminalStatus := scrapingStatusInstance.getLatest()
				terminalStatus.ScraperID = scraperID
				terminalStatus.Force = params.Force
				terminalStatus.Scraping = false
				terminalStatus.Done = true
				terminalStatus.Paused = false
				terminalStatus.State = scrapeStateCompleted
				populateScrapedMediaCountExact(env.State.GetContext(), db, &terminalStatus)
				publishScrapingStatus(ns, &terminalStatus)
			}
			persistedStatus := finalStatus
			if len(operation.Pending) > 0 && scrapeCtx.Err() == nil {
				// Until the next queue position is durable, recovery must retain this
				// job's markers and replay its completion rather than lose pending work.
				persistedStatus = mediadb.IndexingStatusPending
			}
			if err := scrapingStatusInstance.persistTerminal(db.MediaDB, &operation, persistedStatus); err != nil {
				log.Warn().Err(err).Str("scraper", scraperID).Msg("failed to persist scraping terminal status")
				return
			}
			if finalStatus == mediadb.IndexingStatusCancelled ||
				(finalStatus == mediadb.IndexingStatusCompleted && len(operation.Pending) == 0) {
				if err := db.MediaDB.ClearScrapingOperation(); err != nil {
					log.Warn().Err(err).Str("scraper", scraperID).Msg("failed to clear scraping operation")
					return
				}
				if (operation.Force || operation.FillMissing) && runID != "" {
					cleanupErr := db.MediaDB.ClearScrapeRunMarkers(env.State.GetContext(), scraperID, runID)
					if cleanupErr != nil {
						log.Warn().Err(cleanupErr).Str("scraper", scraperID).Msg("failed to clear scrape run markers")
					}
				}
			}
			if checkpointScrapingWAL(db.MediaDB, scraperID) {
				// Wake the corruption-recovery watcher, which only observes media-indexing
				// notifications. Scraping status is already terminal here, so recovery won't defer.
				notifications.MediaIndexing(ns, models.IndexingStatusResponse{Exists: true, Indexing: false})
				return
			}
			log.Info().Str("scraper", scraperID).Str("status", finalStatus).Msg("scraper run complete")
			if scrapeCtx.Err() != nil || len(operation.Pending) == 0 {
				return
			}
			next, nextErr := scrapingStatusInstance.advance(scrapeCtx, db.MediaDB, &operation)
			if nextErr != nil {
				log.Error().Err(nextErr).Msg("failed to advance scraper queue")
				return
			}
			if (operation.Force || operation.FillMissing) && runID != "" {
				cleanupErr := db.MediaDB.ClearScrapeRunMarkers(env.State.GetContext(), scraperID, runID)
				if cleanupErr != nil {
					log.Warn().Err(cleanupErr).Msg("failed to clear completed scrape markers")
				}
			}
			operation = next
			scraperID, runID = operation.ScraperID, operation.RunID
			params.Force = operation.Force
			ch = startQueuedScraper(scrapeCtx, env, &operation)
		}
	}()

	return NoContent{}, nil
}

// checkpointScrapingWAL flushes the WAL after a scraper run. It returns true when the
// checkpoint failure flagged the database corrupt, so the caller can wake the recovery
// watcher; the scrape flow otherwise emits only scraping notifications, which the watcher
// does not observe.
func checkpointScrapingWAL(mediaDB database.MediaDBI, scraperID string) (corrupt bool) {
	started := time.Now()
	if err := mediaDB.WALCheckpoint(); err != nil {
		// A malformed-page failure during the post-scrape checkpoint flags the database
		// corrupt so the recovery flow rebuilds it rather than serving a broken cache.
		corrupt = mediaDB.NoteCorruption(err)
		log.Warn().Err(err).Str("scraper", scraperID).Msg("failed to checkpoint WAL after scraper run")
		return corrupt
	}
	log.Debug().Str("scraper", scraperID).Dur("duration", time.Since(started)).Msg("checkpointed WAL after scraper run")
	return false
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
		status.Throttled = env.ScrapePauser.IsThrottled()
		status.State = scrapeStateRunning
		if status.Paused {
			status.State = scrapeStatePaused
		}
	}
	if env.Database == nil || env.Database.MediaDB == nil {
		return status, nil
	}

	countCtx, cancel := scrapeCountStatusContext(env.Context)
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
	var db database.MediaDBI
	if env.Database != nil {
		db = env.Database.MediaDB
	}
	cancelled, err := scrapingStatusInstance.cancelPersisted(db)
	if err != nil {
		return nil, fmt.Errorf("failed to persist scraping cancellation: %w", err)
	}
	if cancelled {
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
		PublishScrapePauseStatus(env.State.Notifications, false, false)
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
