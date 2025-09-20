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
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/shared/httpclient"
	scraperpkg "github.com/ZaparooProject/zaparoo-core/v2/pkg/scraper"
	"github.com/rs/zerolog/log"
)

const (
	// DefaultWorkerCount is the default number of scraper workers
	DefaultWorkerCount = 3
)

// ScraperService orchestrates scraping operations with MediaDB integration
type ScraperService struct {
	mediaDB         database.MediaDBI
	userDB          database.UserDBI
	config          *config.Instance
	platform        platforms.Platform
	ctx             context.Context
	cancelFunc      context.CancelFunc

	// Components
	scraperRegistry *ScraperRegistry
	progressTracker *ProgressTracker
	jobQueue        *JobQueue
	workerPool      *WorkerPool
	mediaStorage    *scraperpkg.MediaStorage
	metadataStorage *scraperpkg.MetadataStorage

	// State management
	stopMu     sync.Mutex
	stopped    bool
	httpClient *httpclient.Client
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

	// Create components
	scraperRegistry := NewScraperRegistry()
	progressTracker := NewProgressTracker(notificationsChan)
	mediaStorage := scraperpkg.NewMediaStorage(pl, cfg)
	metadataStorage := scraperpkg.NewMetadataStorage(mediaDB)
	jobQueue := NewJobQueue(ctx, DefaultQueueSize)

	// Create worker pool (using service as job processor)
	var workerPool *WorkerPool

	service := &ScraperService{
		mediaDB:         mediaDB,
		userDB:          userDB,
		config:          cfg,
		platform:        pl,
		ctx:             ctx,
		cancelFunc:      cancel,
		scraperRegistry: scraperRegistry,
		progressTracker: progressTracker,
		jobQueue:        jobQueue,
		workerPool:      workerPool,
		mediaStorage:    mediaStorage,
		metadataStorage: metadataStorage,
		httpClient:      httpclient.NewClientWithTimeout(30 * time.Second),
	}

	// Create worker pool with service as job processor
	workerPool = NewWorkerPool(ctx, DefaultWorkerCount, jobQueue.Channel(), service)
	service.workerPool = workerPool

	// Start worker pool
	workerPool.Start()

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


// ProcessJob implements the JobProcessor interface
func (s *ScraperService) ProcessJob(job *scraperpkg.ScraperJob) error {
	// Check if context is cancelled before processing
	select {
	case <-s.ctx.Done():
		return s.ctx.Err()
	default:
	}

	// Update current game in progress
	s.progressTracker.SetCurrentGame(job.MediaTitle, job.MediaDBID)

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
		// Check for cancellation before each download
		select {
		case <-s.ctx.Done():
			return s.ctx.Err()
		default:
		}
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

	log.Info().
		Str("title", mediaTitle.Name).
		Str("system", systemID).
		Int("downloaded", downloadedCount).
		Msg("Successfully scraped game")

	// Increment progress
	s.progressTracker.IncrementProgress()

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

	// Update progress
	s.progressTracker.SetStatus("running")
	s.progressTracker.SetProgress(0, 1)

	// Queue the job
	return s.jobQueue.Enqueue(job)
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
	s.progressTracker.SetStatus("running")
	s.progressTracker.SetProgress(0, len(titles))

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

			if err := s.jobQueue.Enqueue(job); err != nil {
				log.Error().
					Err(err).
					Str("title", title.Name).
					Msg("Failed to enqueue job")
				continue
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
	return s.progressTracker.Get()
}

// CancelScraping cancels the current scraping operation
func (s *ScraperService) CancelScraping() error {
	// Cancel the progress tracker
	s.progressTracker.Cancel()

	// Note: We don't cancel the context here as it would stop all workers
	// Instead, we just mark as cancelled and let current jobs finish
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

	// Cancel context to stop workers
	s.cancelFunc()

	// Stop worker pool
	s.workerPool.Stop()

	// Close job queue
	s.jobQueue.Close()

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
		scraperImpl, err := s.scraperRegistry.Get(scraperName)
		if err != nil {
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
	scraperImpl, err := s.scraperRegistry.Get(scraperName)
	if err != nil {
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
