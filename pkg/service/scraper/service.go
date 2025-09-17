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
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	scraperPkg "github.com/ZaparooProject/zaparoo-core/v2/pkg/scraper"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/scraper/hasher"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/scraper/screenscraper"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/shared/httpclient"
	"github.com/rs/zerolog/log"
)

// ScraperService manages scraping operations with MediaDB integration
type ScraperService struct {
	scrapers      map[string]scraperPkg.Scraper
	mediaDB       database.MediaDBI
	userDB        database.UserDBI
	config        *config.Instance
	mediaStorage  *scraperPkg.MediaStorage
	platform      platforms.Platform
	httpClient    *httpclient.Client

	// Job management
	jobQueue      chan *scraperPkg.ScraperJob
	workers       int
	workerWG      sync.WaitGroup
	ctx           context.Context
	cancelFunc    context.CancelFunc

	// Progress tracking
	progressMu    sync.RWMutex
	progress      *scraperPkg.ScraperProgress
	isRunning     bool
}

// NewScraperService creates a new scraper service
func NewScraperService(
	mediaDB database.MediaDBI,
	userDB database.UserDBI,
	cfg *config.Instance,
	pl platforms.Platform,
) *ScraperService {
	ctx, cancel := context.WithCancel(context.Background())

	service := &ScraperService{
		scrapers:     make(map[string]scraperPkg.Scraper),
		mediaDB:      mediaDB,
		userDB:       userDB,
		config:       cfg,
		mediaStorage: scraperPkg.NewMediaStorage(pl, cfg),
		platform:     pl,
		httpClient:   httpclient.NewClient(),
		jobQueue:     make(chan *scraperPkg.ScraperJob, 1000),
		workers:      3, // Default worker count
		ctx:          ctx,
		cancelFunc:   cancel,
		progress:     &scraperPkg.ScraperProgress{},
	}

	// Register available scrapers
	service.registerScrapers()

	// Start worker pool
	service.startWorkers()

	return service
}

// registerScrapers registers all available scraper implementations
func (s *ScraperService) registerScrapers() {
	// Register ScreenScraper
	screenScraper := screenscraper.NewScreenScraper()
	s.scrapers["screenscraper"] = screenScraper

	log.Info().Int("count", len(s.scrapers)).Msg("Registered scrapers")
}

// startWorkers starts the worker pool for processing scraper jobs
func (s *ScraperService) startWorkers() {
	for i := 0; i < s.workers; i++ {
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

				s.updateProgress(func(p *scraperPkg.ScraperProgress) {
					p.ErrorCount++
				})
			}

			s.updateProgress(func(p *scraperPkg.ScraperProgress) {
				p.ProcessedGames++
				p.CurrentGame = ""
			})
		}
	}
}

// processJob processes a single scraper job
func (s *ScraperService) processJob(job *scraperPkg.ScraperJob) error {
	// Update current game in progress
	s.updateProgress(func(p *scraperPkg.ScraperProgress) {
		p.CurrentGame = job.MediaTitle
	})

	// Get scraper configuration
	scraperConfig := scraperPkg.GetScraperConfig(s.config)
	scraperName := scraperConfig.DefaultScraper

	// Get the scraper
	scraperImpl, exists := s.scrapers[scraperName]
	if !exists {
		return fmt.Errorf("scraper not found: %s", scraperName)
	}

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
	// For now, we'll extract it from the job or infer it
	systemID := job.SystemID

	// Check if we already have scraped metadata and don't need to re-scrape
	if !job.Overwrite {
		existing, err := s.mediaDB.GetScrapedMetadata(mediaTitle.DBID)
		if err == nil && existing != nil {
			log.Debug().
				Str("title", mediaTitle.Name).
				Str("source", existing.ScraperSource).
				Msg("Game already has scraped metadata, skipping")
			return nil
		}
	}

	// Check if media files already exist and we don't need to re-download
	if !job.Overwrite {
		allExist := true
		for _, mediaType := range job.MediaTypes {
			exists, err := s.mediaStorage.MediaExists(media.Path, systemID, mediaType, ".jpg")
			if err == nil && !exists {
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
			s.updateProgress(func(p *scraperPkg.ScraperProgress) {
				p.SkippedFiles += len(job.MediaTypes)
			})
			return nil
		}
	}

	// Build scraper query
	query := scraperPkg.ScraperQuery{
		Name:     mediaTitle.Name,
		SystemID: systemID,
		Region:   scraperConfig.Region,
		Language: scraperConfig.Language,
	}

	// Try to get file hash for better matching
	if hash, err := s.getOrComputeFileHash(media); err == nil && hash != nil {
		query.Hash = &scraperPkg.FileHash{
			CRC32:    hash.CRC32,
			MD5:      hash.MD5,
			SHA1:     hash.SHA1,
			FileSize: hash.FileSize,
		}
	}

	// Search for the game
	results, err := scraperImpl.Search(s.ctx, query)
	if err != nil {
		return fmt.Errorf("failed to search for game: %w", err)
	}

	if len(results) == 0 {
		log.Warn().
			Str("title", mediaTitle.Name).
			Str("system", systemID).
			Msg("No search results found for game")
		return nil
	}

	// Get detailed info for the best match (first result)
	bestMatch := results[0]
	gameInfo, err := scraperImpl.GetGameInfo(s.ctx, bestMatch.ID)
	if err != nil {
		return fmt.Errorf("failed to get game info: %w", err)
	}

	// Save scraped metadata to database
	metadata := &database.ScrapedMetadata{
		MediaTitleDBID: mediaTitle.DBID,
		ScraperSource:  scraperName,
		Description:    gameInfo.Description,
		Genre:          gameInfo.Genre,
		Players:        gameInfo.Players,
		ReleaseDate:    gameInfo.ReleaseDate,
		Developer:      gameInfo.Developer,
		Publisher:      gameInfo.Publisher,
		Rating:         gameInfo.Rating,
		ScrapedAt:      time.Now(),
	}

	if err := s.mediaDB.SaveScrapedMetadata(metadata); err != nil {
		log.Error().Err(err).Msg("Failed to save scraped metadata")
		// Continue with media download even if metadata save fails
	}

	// Download media files
	downloadedCount := 0
	for _, mediaType := range job.MediaTypes {
		// Find matching media item
		var mediaItem *scraperPkg.MediaItem
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
		if err := s.downloadMediaFile(media.Path, systemID, mediaType, *mediaItem); err != nil {
			log.Error().
				Err(err).
				Str("type", string(mediaType)).
				Str("title", mediaTitle.Name).
				Msg("Failed to download media file")
			continue
		}

		downloadedCount++
	}

	s.updateProgress(func(p *scraperPkg.ScraperProgress) {
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
func (s *ScraperService) downloadMediaFile(gamePath, systemID string, mediaType scraperPkg.MediaType, mediaItem scraperPkg.MediaItem) error {
	// Determine file extension from URL or format
	extension := ".jpg" // Default
	if mediaItem.Format != "" {
		extension = "." + strings.ToLower(mediaItem.Format)
	} else {
		// Try to infer from URL
		if strings.Contains(mediaItem.URL, ".png") {
			extension = ".png"
		} else if strings.Contains(mediaItem.URL, ".gif") {
			extension = ".gif"
		} else if strings.Contains(mediaItem.URL, ".mp4") {
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

// getOrComputeFileHash gets existing hash from database or computes it
func (s *ScraperService) getOrComputeFileHash(media *database.Media) (*database.GameHashes, error) {
	// Try to get existing hash from database
	if hash, err := s.mediaDB.GetGameHashes(media.DBID); err == nil {
		return hash, nil
	}

	// Compute hash if not exists
	fileHash, err := hasher.ComputeFileHashes(media.Path)
	if err != nil {
		return nil, fmt.Errorf("failed to compute file hash: %w", err)
	}

	// Save to database
	dbHash := &database.GameHashes{
		MediaDBID:  media.DBID,
		CRC32:      fileHash.CRC32,
		MD5:        fileHash.MD5,
		SHA1:       fileHash.SHA1,
		FileSize:   fileHash.FileSize,
		ComputedAt: time.Now(),
	}

	if err := s.mediaDB.SaveGameHashes(dbHash); err != nil {
		log.Warn().Err(err).Msg("Failed to save computed hash to database")
	}

	return dbHash, nil
}

// updateProgress safely updates the progress information
func (s *ScraperService) updateProgress(updateFunc func(*scraperPkg.ScraperProgress)) {
	s.progressMu.Lock()
	defer s.progressMu.Unlock()
	updateFunc(s.progress)
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

	// Create scraper job
	scraperConfig := scraperPkg.GetScraperConfig(s.config)
	job := &scraperPkg.ScraperJob{
		MediaDBID:  mediaDBID,
		MediaTitle: mediaTitle.Name,
		SystemID:   "unknown", // TODO: Get from database
		GamePath:   media.Path,
		MediaTypes: scraperConfig.DefaultMediaTypes,
		Overwrite:  false,
		Priority:   1,
	}

	// Start scraping if not already running
	s.progressMu.Lock()
	if !s.isRunning {
		s.isRunning = true
		s.progress = &scraperPkg.ScraperProgress{
			IsRunning:    true,
			TotalGames:   1,
			StartTime:    &time.Time{},
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
	s.progress = &scraperPkg.ScraperProgress{
		IsRunning:  true,
		TotalGames: len(titles),
		StartTime:  &now,
	}
	s.progressMu.Unlock()

	// Get scraper config
	scraperConfig := scraperPkg.GetScraperConfig(s.config)

	// Queue jobs for all games
	for _, title := range titles {
		// Get first media entry for this title
		// TODO: This should be improved to handle multiple media entries per title
		media, err := s.getFirstMediaForTitle(title.DBID)
		if err != nil {
			log.Error().
				Err(err).
				Int64("titleID", title.DBID).
				Str("title", title.Name).
				Msg("Failed to get media for title")
			continue
		}

		job := &scraperPkg.ScraperJob{
			MediaDBID:  media.DBID,
			MediaTitle: title.Name,
			SystemID:   systemID,
			GamePath:   media.Path,
			MediaTypes: scraperConfig.DefaultMediaTypes,
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

	return nil
}

// getFirstMediaForTitle gets the first media entry for a media title
func (s *ScraperService) getFirstMediaForTitle(mediaTitleDBID int64) (*database.Media, error) {
	// This is a simplified implementation - in a real scenario, we'd want a proper query
	// For now, we'll use a SQL query to get the first media entry
	query := `SELECT DBID, MediaTitleDBID, Path FROM Media WHERE MediaTitleDBID = ? LIMIT 1`

	db := s.mediaDB.UnsafeGetSQLDb()
	row := db.QueryRow(query, mediaTitleDBID)

	var media database.Media
	err := row.Scan(&media.DBID, &media.MediaTitleDBID, &media.Path)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("no media found for title ID %d", mediaTitleDBID)
		}
		return nil, err
	}

	return &media, nil
}

// GetProgress returns the current scraping progress
func (s *ScraperService) GetProgress() *scraperPkg.ScraperProgress {
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
	s.progressMu.Unlock()

	// Note: We don't cancel the context here as it would stop all workers
	// Instead, we just mark as not running and let current jobs finish
	log.Info().Msg("Scraping cancelled")
	return nil
}

// Stop stops the scraper service
func (s *ScraperService) Stop() {
	log.Info().Msg("Stopping scraper service")

	s.cancelFunc()
	s.workerWG.Wait()
	close(s.jobQueue)

	log.Info().Msg("Scraper service stopped")
}