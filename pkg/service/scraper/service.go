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
	"strings"
	"sync"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/notifications"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	scraperpkg "github.com/ZaparooProject/zaparoo-core/v2/pkg/scraper"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/scraper/igdb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/scraper/screenscraper"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/scraper/thegamesdb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/shared/httpclient"
	"github.com/rs/zerolog/log"
)

const (
	// DefaultWorkerCount is the default number of scraper workers
	DefaultWorkerCount = 3
)

// ScraperService manages scraping operations with MediaDB integration
type ScraperService struct {
	mediaDB         database.MediaDBI
	userDB          database.UserDBI
	platform        platforms.Platform
	ctx             context.Context
	httpClient      *httpclient.Client
	cancelFunc      context.CancelFunc
	mediaStorage    *scraperpkg.MediaStorage
	metadataStorage *scraperpkg.MetadataStorage
	progress        *scraperpkg.ScraperProgress
	jobQueue        chan *scraperpkg.ScraperJob
	scrapers        map[string]scraperpkg.Scraper
	config          *config.Instance
	notifications   chan<- models.Notification
	workerWG        sync.WaitGroup
	workers         int
	progressMu      sync.RWMutex
	stopMu          sync.Mutex
	isRunning       bool
	stopped         bool
}

// NewScraperService creates a new scraper service
func NewScraperService(
	mediaDB database.MediaDBI,
	userDB database.UserDBI,
	cfg *config.Instance,
	pl platforms.Platform,
	notificationsChan chan<- models.Notification,
) *ScraperService {
	ctx, cancel := context.WithCancel(context.Background())

	service := &ScraperService{
		scrapers:        make(map[string]scraperpkg.Scraper),
		mediaDB:         mediaDB,
		userDB:          userDB,
		config:          cfg,
		mediaStorage:    scraperpkg.NewMediaStorage(pl, cfg),
		metadataStorage: scraperpkg.NewMetadataStorage(mediaDB),
		platform:        pl,
		httpClient:      httpclient.NewClient(),
		jobQueue:        make(chan *scraperpkg.ScraperJob, 1000),
		workers:         DefaultWorkerCount,
		ctx:             ctx,
		cancelFunc:      cancel,
		progress:        &scraperpkg.ScraperProgress{Status: "idle"},
		notifications:   notificationsChan,
	}

	// Register available scrapers
	service.registerScrapers()

	// Start worker pool
	service.startWorkers()

	return service
}

// getScraperConfig gets the scraper configuration
func (s *ScraperService) getScraperConfig() config.Scraper {
	return s.config.Scraper()
}

// getDefaultMediaTypes builds the default media types from config flags
func (s *ScraperService) getDefaultMediaTypes() []scraperpkg.MediaType {
	cfg := s.config.Scraper()
	var mediaTypes []scraperpkg.MediaType

	if cfg.DownloadCovers {
		mediaTypes = append(mediaTypes, scraperpkg.MediaTypeCover)
	}
	if cfg.DownloadScreenshots {
		mediaTypes = append(mediaTypes, scraperpkg.MediaTypeScreenshot)
	}
	if cfg.DownloadVideos {
		mediaTypes = append(mediaTypes, scraperpkg.MediaTypeVideo)
	}

	return mediaTypes
}

// registerScrapers registers all available scraper implementations
func (s *ScraperService) registerScrapers() {
	// Register ScreenScraper
	screenScraper := screenscraper.NewScreenScraper()
	s.scrapers["screenscraper"] = screenScraper

	// Register TheGamesDB
	theGamesDB := thegamesdb.NewTheGamesDB()
	s.scrapers["thegamesdb"] = theGamesDB

	// Register IGDB
	igdbScraper := igdb.NewIGDB()
	s.scrapers["igdb"] = igdbScraper

	log.Info().Int("count", len(s.scrapers)).Msg("Registered scrapers")
}

// startWorkers starts the worker pool for processing scraper jobs
func (s *ScraperService) startWorkers() {
	for i := range s.workers {
		s.workerWG.Add(1)
		go s.worker(i)
	}

	log.Info().Int("workers", s.workers).Msg("Started scraper workers")
}

// worker processes scraper jobs from the queue
func (s *ScraperService) worker(id int) {
	defer s.workerWG.Done()

	log.Debug().Int("worker", id).Msg("Scraper worker started")

	for {
		select {
		case <-s.ctx.Done():
			log.Debug().Int("worker", id).Msg("Scraper worker stopping")
			return

		case job := <-s.jobQueue:
			if job == nil {
				continue
			}

			log.Debug().
				Int("worker", id).
				Int64("mediaDBID", job.MediaDBID).
				Str("title", job.MediaTitle).
				Msg("Processing scraper job")

			if err := s.processJob(job); err != nil {
				log.Error().
					Err(err).
					Int64("mediaDBID", job.MediaDBID).
					Str("title", job.MediaTitle).
					Msg("Failed to process scraper job")

				s.updateProgress(func(p *scraperpkg.ScraperProgress) {
					p.ErrorCount++
					p.LastError = err.Error()
					// Don't change status to "failed" for individual job errors
					// Only set to "failed" for critical system errors
				})

				// Send error notification
				if s.notifications != nil {
					errorPayload := map[string]any{
						"mediaDBID": job.MediaDBID,
						"title":     job.MediaTitle,
						"error":     err.Error(),
					}
					notifications.ScraperError(s.notifications, errorPayload)
				}
			}

			s.updateProgress(func(p *scraperpkg.ScraperProgress) {
				p.ProcessedGames++
				p.CurrentGame = ""

				// Check if scraping is completed
				if p.IsRunning && p.ProcessedGames >= p.TotalGames {
					p.IsRunning = false
					p.Status = "completed"
					s.isRunning = false
				}
			})
		}
	}
}

// processJob processes a single scraper job
func (s *ScraperService) processJob(job *scraperpkg.ScraperJob) error {
	// Update current game in progress
	s.updateProgress(func(p *scraperpkg.ScraperProgress) {
		p.CurrentGame = job.MediaTitle
	})

	// Get scraper configuration
	scraperConfig := s.getScraperConfig()

	// Get media and media title from database
	media, err := s.mediaDB.GetMediaByID(job.MediaDBID)
	if err != nil {
		return fmt.Errorf("failed to get media: %w", err)
	}

	mediaTitle, err := s.mediaDB.GetMediaTitleByID(media.MediaTitleDBID)
	if err != nil {
		return fmt.Errorf("failed to get media title: %w", err)
	}

	// Get system information
	systemID := job.SystemID
	if systemID == "" || systemID == "unknown" {
		// Fallback: resolve system ID from database
		system, systemErr := s.mediaDB.GetSystemByID(mediaTitle.SystemDBID)
		if systemErr != nil {
			return fmt.Errorf("failed to get system for fallback: %w", systemErr)
		}
		systemID = system.SystemID
	}

	// Check if we already have scraped metadata and don't need to re-scrape
	if !job.Overwrite {
		hasMetadata, metadataErr := s.mediaDB.HasScraperMetadata(mediaTitle.DBID)
		if metadataErr == nil && hasMetadata {
			log.Debug().
				Str("title", mediaTitle.Name).
				Msg("Game already has scraped metadata, skipping")
			return nil
		}
	}

	// Check if media files already exist and we don't need to re-download
	if !job.Overwrite {
		allExist := true
		for _, mediaType := range job.MediaTypes {
			exists, existsErr := s.mediaStorage.MediaExists(media.Path, systemID, mediaType, ".jpg")
			if existsErr == nil && !exists {
				// Try other common extensions
				exists, _ = s.mediaStorage.MediaExists(media.Path, systemID, mediaType, ".png")
			}
			if !exists {
				allExist = false
				break
			}
		}

		if allExist {
			log.Debug().
				Str("title", mediaTitle.Name).
				Msg("All media files already exist, skipping")
			s.updateProgress(func(p *scraperpkg.ScraperProgress) {
				p.SkippedFiles += len(job.MediaTypes)
			})
			return nil
		}
	}

	// Build scraper query
	query := scraperpkg.ScraperQuery{
		Name:     mediaTitle.Name,
		SystemID: systemID,
		Region:   scraperConfig.Region,
		Language: scraperConfig.Language,
	}

	// Try to get file hash for better matching
	if hash, hashErr := s.getFileHashFromDB(media, systemID); hashErr == nil && hash != nil {
		query.Hash = &scraperpkg.FileHash{
			CRC32:    hash.CRC32,
			MD5:      hash.MD5,
			SHA1:     hash.SHA1,
			FileSize: hash.FileSize,
		}
	}

	// Try scraping with fallback chain
	gameInfo, scraperUsed, err := s.tryScrapingWithFallback(query, scraperConfig)
	if err != nil {
		return fmt.Errorf("failed to scrape game: %w", err)
	}

	if gameInfo == nil {
		log.Warn().
			Str("title", mediaTitle.Name).
			Str("system", systemID).
			Msg("No search results found for game with any scraper")
		return nil
	}

	// Save scraped metadata to database using Tags system
	metadata := &scraperpkg.ScrapedMetadata{
		MediaTitleDBID: mediaTitle.DBID,
		ScraperSource:  scraperUsed,
		Description:    gameInfo.Description,
		Genre:          gameInfo.Genre,
		Players:        gameInfo.Players,
		ReleaseDate:    gameInfo.ReleaseDate,
		Developer:      gameInfo.Developer,
		Publisher:      gameInfo.Publisher,
		Rating:         gameInfo.Rating,
		ScrapedAt:      time.Now(),
	}

	if err := s.metadataStorage.StoreMetadata(s.ctx, metadata); err != nil {
		log.Error().Err(err).Msg("Failed to save scraped metadata")
		// Continue with media download even if metadata save fails
	}

	// Download media files
	downloadedCount := 0
	for _, mediaType := range job.MediaTypes {
		// Find matching media item
		var mediaItem *scraperpkg.MediaItem
		for _, item := range gameInfo.Media {
			if item.Type == mediaType {
				mediaItem = &item
				break
			}
		}

		if mediaItem == nil {
			log.Debug().
				Str("type", string(mediaType)).
				Str("title", mediaTitle.Name).
				Msg("Media type not available for game")
			continue
		}

		// Download the media file
		if err := s.downloadMediaFile(media.Path, systemID, mediaType, mediaItem); err != nil {
			log.Error().
				Err(err).
				Str("type", string(mediaType)).
				Str("title", mediaTitle.Name).
				Msg("Failed to download media file")
			continue
		}

		downloadedCount++
	}

	s.updateProgress(func(p *scraperpkg.ScraperProgress) {
		p.DownloadedFiles += downloadedCount
	})

	log.Info().
		Str("title", mediaTitle.Name).
		Str("system", systemID).
		Int("downloaded", downloadedCount).
		Msg("Successfully scraped game")

	return nil
}

// downloadMediaFile downloads a media file to the appropriate location
func (s *ScraperService) downloadMediaFile(gamePath, systemID string, mediaType scraperpkg.MediaType,
	mediaItem *scraperpkg.MediaItem,
) error {
	// Determine file extension from URL or format
	extension := ".jpg" // Default
	if mediaItem.Format != "" {
		extension = "." + strings.ToLower(mediaItem.Format)
	} else {
		// Try to infer from URL
		switch {
		case strings.Contains(mediaItem.URL, ".png"):
			extension = ".png"
		case strings.Contains(mediaItem.URL, ".gif"):
			extension = ".gif"
		case strings.Contains(mediaItem.URL, ".mp4"):
			extension = ".mp4"
		}
	}

	// Get the target path
	targetPath, err := s.mediaStorage.GetMediaPath(gamePath, systemID, mediaType, extension)
	if err != nil {
		return fmt.Errorf("failed to get media path: %w", err)
	}

	// Ensure directory exists
	if err := s.mediaStorage.EnsureMediaDirectory(targetPath); err != nil {
		return fmt.Errorf("failed to create media directory: %w", err)
	}

	// Download the file
	tempPath := targetPath + ".tmp"
	downloadArgs := httpclient.DownloadFileArgs{
		URL:        mediaItem.URL,
		OutputPath: targetPath,
		TempPath:   tempPath,
	}

	if err := s.httpClient.DownloadFile(s.ctx, downloadArgs); err != nil {
		return fmt.Errorf("failed to download file: %w", err)
	}

	log.Debug().
		Str("url", mediaItem.URL).
		Str("path", targetPath).
		Str("type", string(mediaType)).
		Msg("Downloaded media file")

	return nil
}

// getFileHashFromDB gets existing hash from database only (no computation fallback)
func (s *ScraperService) getFileHashFromDB(media *database.Media, systemID string) (*database.GameHashes, error) {
	// Get existing hash from database
	hash, err := s.mediaDB.GetGameHashes(systemID, media.Path)
	if err != nil {
		// Hash not found or database error - this is fine as hashing may be disabled
		// or the file hasn't been indexed yet with hashing enabled
		log.Debug().Str("system", systemID).Str("path", media.Path).Msg("no hash found in database")
		return nil, fmt.Errorf("failed to get game hashes for %s in %s: %w", media.Path, systemID, err)
	}
	return hash, nil
}

// updateProgress safely updates the progress information
func (s *ScraperService) updateProgress(updateFunc func(*scraperpkg.ScraperProgress)) {
	s.progressMu.Lock()
	defer s.progressMu.Unlock()
	updateFunc(s.progress)

	// Send progress notification if notificationsChan channel is available
	if s.notifications != nil {
		// Create a copy of progress for notification
		progressCopy := *s.progress
		notifications.ScraperProgress(s.notifications, progressCopy)
	}
}

// ScrapeGameByID scrapes a specific game by its Media DBID
func (s *ScraperService) ScrapeGameByID(ctx context.Context, mediaDBID int64) error {
	// Get media and title information
	media, err := s.mediaDB.GetMediaByID(mediaDBID)
	if err != nil {
		return fmt.Errorf("failed to get media: %w", err)
	}

	mediaTitle, err := s.mediaDB.GetMediaTitleByID(media.MediaTitleDBID)
	if err != nil {
		return fmt.Errorf("failed to get media title: %w", err)
	}

	// Get system information
	system, err := s.mediaDB.GetSystemByID(mediaTitle.SystemDBID)
	if err != nil {
		return fmt.Errorf("failed to get system: %w", err)
	}

	// Create scraper job
	job := &scraperpkg.ScraperJob{
		MediaDBID:  mediaDBID,
		MediaTitle: mediaTitle.Name,
		SystemID:   system.SystemID,
		GamePath:   media.Path,
		MediaTypes: s.getDefaultMediaTypes(),
		Overwrite:  false,
		Priority:   1,
	}

	// Start scraping if not already running
	s.progressMu.Lock()
	if !s.isRunning {
		s.isRunning = true
		s.progress = &scraperpkg.ScraperProgress{
			IsRunning:  true,
			TotalGames: 1,
			StartTime:  &time.Time{},
		}
		*s.progress.StartTime = time.Now()
	}
	s.progressMu.Unlock()

	// Queue the job
	select {
	case s.jobQueue <- job:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// ScrapeSystem scrapes all games in a system
func (s *ScraperService) ScrapeSystem(ctx context.Context, systemID string) error {
	// Get all media titles for the system
	titles, err := s.mediaDB.GetMediaTitlesBySystem(systemID)
	if err != nil {
		return fmt.Errorf("failed to get games for system: %w", err)
	}

	if len(titles) == 0 {
		return fmt.Errorf("no games found for system: %s", systemID)
	}

	// Start scraping
	s.progressMu.Lock()
	s.isRunning = true
	now := time.Now()
	s.progress = &scraperpkg.ScraperProgress{
		IsRunning:  true,
		TotalGames: len(titles),
		StartTime:  &now,
		Status:     "running",
	}
	s.progressMu.Unlock()

	// Queue jobs for all games
	for _, title := range titles {
		// Get all media entries for this title to handle multiple versions/regions
		mediaEntries, err := s.getAllMediaForTitle(title.DBID)
		if err != nil {
			log.Error().
				Err(err).
				Int64("titleID", title.DBID).
				Str("title", title.Name).
				Msg("Failed to get media entries for title")
			continue
		}

		if len(mediaEntries) == 0 {
			log.Debug().
				Int64("titleID", title.DBID).
				Str("title", title.Name).
				Msg("No media entries found for title")
			continue
		}

		// Create a job for each media entry
		for _, media := range mediaEntries {
			job := &scraperpkg.ScraperJob{
				MediaDBID:  media.DBID,
				MediaTitle: title.Name,
				SystemID:   systemID,
				GamePath:   media.Path,
				MediaTypes: s.getDefaultMediaTypes(),
				Overwrite:  false,
				Priority:   1,
			}

			select {
			case s.jobQueue <- job:
				// Job queued successfully
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}

	return nil
}

// getAllMediaForTitle gets all media entries for a media title
func (s *ScraperService) getAllMediaForTitle(mediaTitleDBID int64) ([]*database.Media, error) {
	query := `SELECT DBID, MediaTitleDBID, Path FROM Media WHERE MediaTitleDBID = ? ORDER BY DBID`

	db := s.mediaDB.UnsafeGetSQLDb()
	ctx := context.Background()
	rows, err := db.QueryContext(ctx, query, mediaTitleDBID)
	if err != nil {
		return nil, fmt.Errorf("failed to query media for title %d: %w", mediaTitleDBID, err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("Failed to close database rows")
		}
	}()

	mediaEntries := make([]*database.Media, 0, 10)
	for rows.Next() {
		var media database.Media
		err := rows.Scan(&media.DBID, &media.MediaTitleDBID, &media.Path)
		if err != nil {
			return nil, fmt.Errorf("failed to scan media row for title %d: %w", mediaTitleDBID, err)
		}
		mediaEntries = append(mediaEntries, &media)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return mediaEntries, nil
}

// GetProgress returns the current scraping progress
func (s *ScraperService) GetProgress() *scraperpkg.ScraperProgress {
	s.progressMu.RLock()
	defer s.progressMu.RUnlock()

	// Create a copy to avoid race conditions
	progress := *s.progress
	return &progress
}

// CancelScraping cancels the current scraping operation
func (s *ScraperService) CancelScraping() error {
	s.progressMu.Lock()
	s.isRunning = false
	s.progress.IsRunning = false
	s.progress.Status = "cancelled"
	s.progressMu.Unlock()

	// Note: We don't cancel the context here as it would stop all workers
	// Instead, we just mark as not running and let current jobs finish
	log.Info().Msg("Scraping cancelled")
	return nil
}

// Stop stops the scraper service
func (s *ScraperService) Stop() {
	s.stopMu.Lock()
	defer s.stopMu.Unlock()

	if s.stopped {
		return // Already stopped
	}
	s.stopped = true

	log.Info().Msg("Stopping scraper service")

	s.cancelFunc()
	s.workerWG.Wait()
	close(s.jobQueue)

	log.Info().Msg("Scraper service stopped")
}

// tryScrapingWithFallback attempts to scrape game info using fallback chain
func (s *ScraperService) tryScrapingWithFallback(query scraperpkg.ScraperQuery,
	cfg config.Scraper,
) (*scraperpkg.GameInfo, string, error) {
	// Build list of scrapers to try: primary scraper + others as fallbacks
	allScrapers := []string{"screenscraper", "thegamesdb", "igdb"}
	scrapersToTry := []string{cfg.DefaultScraper}

	// Add remaining scrapers as fallbacks (excluding the default)
	for _, scraper := range allScrapers {
		if scraper != cfg.DefaultScraper {
			scrapersToTry = append(scrapersToTry, scraper)
		}
	}

	var lastErr error
	for _, scraperName := range scrapersToTry {
		// Check if scraper exists and supports the platform
		scraperImpl, exists := s.scrapers[scraperName]
		if !exists {
			log.Debug().Str("scraper", scraperName).Msg("Scraper not found, skipping")
			continue
		}

		if !scraperImpl.IsSupportedPlatform(query.SystemID) {
			log.Debug().
				Str("scraper", scraperName).
				Str("platform", query.SystemID).
				Msg("Platform not supported by scraper, skipping")
			continue
		}

		log.Debug().
			Str("scraper", scraperName).
			Str("game", query.Name).
			Str("platform", query.SystemID).
			Msg("Trying scraper")

		// Try to search for the game
		results, err := scraperImpl.Search(s.ctx, query)
		if err != nil {
			log.Warn().
				Err(err).
				Str("scraper", scraperName).
				Str("game", query.Name).
				Msg("Search failed, trying next scraper")
			lastErr = err
			continue
		}

		if len(results) == 0 {
			log.Debug().
				Str("scraper", scraperName).
				Str("game", query.Name).
				Msg("No results found, trying next scraper")
			continue
		}

		// Get detailed info for the best match
		bestMatch := results[0]
		gameInfo, err := scraperImpl.GetGameInfo(s.ctx, bestMatch.ID)
		if err != nil {
			log.Warn().
				Err(err).
				Str("scraper", scraperName).
				Str("game", query.Name).
				Msg("Failed to get game info, trying next scraper")
			lastErr = err
			continue
		}

		// Success! Return the game info and which scraper was used
		log.Info().
			Str("scraper", scraperName).
			Str("game", query.Name).
			Str("platform", query.SystemID).
			Msg("Successfully scraped game")
		return gameInfo, scraperName, nil
	}

	// If we get here, all scrapers failed
	if lastErr != nil {
		return nil, "", lastErr
	}
	return nil, "", nil // No error, just no results found
}

// Search performs a search using the specified scraper
func (s *ScraperService) Search(ctx context.Context, scraperName string,
	query scraperpkg.ScraperQuery,
) ([]scraperpkg.ScraperResult, error) {
	// Get the specified scraper
	scraperImpl, exists := s.scrapers[scraperName]
	if !exists {
		return nil, fmt.Errorf("scraper not found: %s", scraperName)
	}

	// Check if the scraper supports the platform
	if !scraperImpl.IsSupportedPlatform(query.SystemID) {
		return nil, fmt.Errorf("platform %s not supported by scraper %s", query.SystemID, scraperName)
	}

	// Perform the search
	results, err := scraperImpl.Search(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("search failed for scraper %s: %w", scraperName, err)
	}

	return results, nil
}
