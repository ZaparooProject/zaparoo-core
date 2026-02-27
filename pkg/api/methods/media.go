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
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/notifications"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/validation"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/assets"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/filters"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediascanner"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/rs/zerolog/log"
)

const defaultMaxResults = 100

// searchSem limits concurrent media.search requests to 3 to avoid saturating
// SQLite with long-running LIKE queries. Additional callers block until a slot
// opens or their request context is cancelled.
var searchSem = make(chan struct{}, 3)

func resolveSystem(id string, fuzzyMatch bool) (*systemdefs.System, error) {
	if fuzzyMatch {
		sys, err := systemdefs.LookupSystem(id)
		if err != nil {
			return nil, fmt.Errorf("error looking up system %q: %w", id, err)
		}
		return sys, nil
	}
	sys, err := systemdefs.GetSystem(id)
	if err != nil {
		return nil, fmt.Errorf("error getting system %q: %w", id, err)
	}
	return sys, nil
}

// resolveSystems resolves a list of system ID strings to canonical System
// values. Handles fuzzy matching, deduplication, and falls back to AllSystems
// when the input list is nil or empty.
func resolveSystems(ids []string, fuzzy bool) ([]systemdefs.System, error) {
	if len(ids) == 0 {
		return systemdefs.AllSystems(), nil
	}
	seen := make(map[string]bool, len(ids))
	systems := make([]systemdefs.System, 0, len(ids))
	for _, id := range ids {
		sys, err := resolveSystem(id, fuzzy)
		if err != nil {
			return nil, fmt.Errorf("invalid system ID %s: %w", id, err)
		}
		if seen[sys.ID] {
			continue
		}
		seen[sys.ID] = true
		systems = append(systems, *sys)
	}
	return systems, nil
}

type cursorData struct {
	LastID int64 `json:"lastId"`
}

func encodeCursor(lastID int64) (string, error) {
	data := cursorData{LastID: lastID}
	bytes, err := json.Marshal(data)
	if err != nil {
		return "", fmt.Errorf("failed to marshal cursor data: %w", err)
	}
	return base64.StdEncoding.EncodeToString(bytes), nil
}

func decodeCursor(cursor string) (*int64, error) {
	if cursor == "" {
		return nil, nil //nolint:nilnil // empty cursor is valid and should return nil
	}

	bytes, err := base64.StdEncoding.DecodeString(cursor)
	if err != nil {
		return nil, fmt.Errorf("invalid cursor format: %w", err)
	}

	var data cursorData
	err = json.Unmarshal(bytes, &data)
	if err != nil {
		return nil, fmt.Errorf("invalid cursor data: %w", err)
	}

	return &data.LastID, nil
}

type indexingStatusVals struct {
	currentDesc string
	totalSteps  int
	currentStep int
	totalFiles  int
	indexing    bool
}

type indexingStatus struct {
	cancelFunc  context.CancelFunc
	currentDesc string
	totalSteps  int
	currentStep int
	totalFiles  int
	mu          syncutil.RWMutex
	indexing    bool
}

func (s *indexingStatus) get() indexingStatusVals {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return indexingStatusVals{
		indexing:    s.indexing,
		totalSteps:  s.totalSteps,
		currentStep: s.currentStep,
		currentDesc: s.currentDesc,
		totalFiles:  s.totalFiles,
	}
}

func (s *indexingStatus) startIfNotRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.indexing {
		return false
	}
	s.indexing = true
	s.totalSteps = 0
	s.currentStep = 0
	s.currentDesc = ""
	s.totalFiles = 0
	return true
}

func (s *indexingStatus) clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.indexing = false
	s.totalSteps = 0
	s.currentStep = 0
	s.currentDesc = ""
	s.totalFiles = 0
	s.cancelFunc = nil
}

// ClearIndexingStatus clears the global indexing status - used for testing
func ClearIndexingStatus() {
	statusInstance.clear()
}

// CancelIndexing cancels the currently running indexing operation - used for testing
func CancelIndexing() bool {
	return statusInstance.cancel()
}

func (s *indexingStatus) set(vals indexingStatusVals) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.indexing = vals.indexing
	s.totalSteps = vals.totalSteps
	s.currentStep = vals.currentStep
	s.currentDesc = vals.currentDesc
	s.totalFiles = vals.totalFiles
}

func (s *indexingStatus) setCancelFunc(cancelFunc context.CancelFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cancelFunc = cancelFunc
}

func (s *indexingStatus) cancel() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancelFunc != nil && s.indexing {
		s.cancelFunc()
		s.indexing = false // Set indexing to false after canceling
		return true
	}
	return false
}

func (s *indexingStatus) isRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.indexing
}

func (s *indexingStatus) setRunning(running bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.indexing = running
}

func (s *indexingStatus) getCancelFunc() context.CancelFunc {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cancelFunc
}

func newIndexingStatus() *indexingStatus {
	return &indexingStatus{}
}

var statusInstance = newIndexingStatus()

func GenerateMediaDB(
	ctx context.Context,
	pl platforms.Platform,
	cfg *config.Instance,
	ns chan<- models.Notification,
	systems []systemdefs.System,
	db *database.Database,
) error {
	if !statusInstance.startIfNotRunning() {
		return errors.New("indexing already in progress")
	}

	// Also prevent indexing if optimization is running
	optimizationStatus, err := db.MediaDB.GetOptimizationStatus()
	if err != nil {
		statusInstance.clear()
		return fmt.Errorf("failed to get optimization status during indexing check: %w", err)
	} else if optimizationStatus == "running" {
		statusInstance.clear()
		return errors.New("database optimization in progress")
	}
	startTime := time.Now()

	// Create cancellable context for indexing
	indexCtx, cancelFunc := context.WithCancel(ctx)
	statusInstance.setCancelFunc(cancelFunc)

	log.Info().Msg("generating media db")
	notifications.MediaIndexing(ns, models.IndexingStatusResponse{
		Exists:   false,
		Indexing: true,
	})

	db.MediaDB.TrackBackgroundOperation()
	go func() {
		defer db.MediaDB.BackgroundOperationDone()
		total, err := mediascanner.NewNamesIndex(indexCtx, pl, cfg, systems, db, func(status mediascanner.IndexStatus) {
			var desc string
			switch status.Step {
			case 1:
				desc = "Finding media folders"
			case status.Total:
				desc = "Writing database"
			default:
				system, err := systemdefs.GetSystem(status.SystemID)
				if err != nil {
					desc = status.SystemID
				} else {
					md, err := assets.GetSystemMetadata(system.ID)
					if err != nil {
						desc = system.ID
					} else {
						desc = md.Name
					}
				}
			}
			statusInstance.set(indexingStatusVals{
				indexing:    true,
				totalSteps:  status.Total,
				currentStep: status.Step,
				currentDesc: desc,
				totalFiles:  status.Files,
			})

			notifications.MediaIndexing(ns, models.IndexingStatusResponse{
				Exists:             false,
				Indexing:           true,
				TotalSteps:         &status.Total,
				CurrentStep:        &status.Step,
				CurrentStepDisplay: &desc,
				TotalFiles:         &status.Files,
			})

			log.Debug().Msgf("indexing status: %v", indexingStatusVals{
				indexing:    true,
				totalSteps:  status.Total,
				currentStep: status.Step,
				currentDesc: desc,
				totalFiles:  status.Files,
			})
		})
		if err != nil {
			if errors.Is(err, context.Canceled) {
				log.Info().Msg("media indexing was cancelled")
				notifications.MediaIndexing(ns, models.IndexingStatusResponse{
					Exists:     false,
					Indexing:   false,
					TotalFiles: &total,
				})
			} else {
				log.Error().Err(err).Msg("error generating media db")
				// TODO: error notification to client
				notifications.MediaIndexing(ns, models.IndexingStatusResponse{
					Exists:     false,
					Indexing:   false,
					TotalFiles: &total,
				})
			}
			statusInstance.clear()
			return
		}
		log.Info().Msg("finished generating media db successfully")
		notifications.MediaIndexing(ns, models.IndexingStatusResponse{
			Exists:     true,
			Indexing:   false,
			TotalFiles: &total,
		})

		if cacheErr := db.MediaDB.RebuildSlugSearchCache(); cacheErr != nil {
			log.Warn().Err(cacheErr).Msg("failed to rebuild slug search cache after indexing")
		}

		// Start background optimization with notification callback
		// Track the optimization operation BEFORE starting the goroutine to prevent a race
		// where Close() → Wait() could return between this goroutine's Done() and
		// RunBackgroundOptimization's internal Add(). The wrapper ensures Done() is called
		// even if RunBackgroundOptimization skips (e.g., already optimizing).
		db.MediaDB.TrackBackgroundOperation()
		go func() {
			defer db.MediaDB.BackgroundOperationDone()
			db.MediaDB.RunBackgroundOptimization(func(optimizing bool) {
				notifications.MediaIndexing(ns, models.IndexingStatusResponse{
					Exists:     true,
					Indexing:   false,
					Optimizing: optimizing,
					TotalFiles: &total,
				})
			})
		}()

		statusInstance.clear()
		log.Info().Msgf("finished generating media db in %v", time.Since(startTime))
	}()

	return nil
}

//nolint:gocritic // single-use parameter in API handler
func HandleGenerateMedia(env requests.RequestEnv) (any, error) {
	log.Info().Msg("received generate media request")

	var systems []systemdefs.System
	var isSelectiveIndexing bool

	if len(env.Params) > 0 {
		var params models.MediaIndexParams
		if unmarshalErr := json.Unmarshal(env.Params, &params); unmarshalErr != nil {
			return nil, validation.ErrInvalidParams
		}

		// Validate params (systems are validated by struct tags)
		if validateErr := validation.DefaultValidator.Validate(&params); validateErr != nil {
			log.Warn().Err(validateErr).Msg("invalid params")
			return nil, fmt.Errorf("invalid params: %w", validateErr)
		}

		fuzzy := params.FuzzySystem != nil && *params.FuzzySystem
		var ids []string
		if params.Systems != nil {
			ids = *params.Systems
		}

		var resolveErr error
		systems, resolveErr = resolveSystems(ids, fuzzy)
		if resolveErr != nil {
			return nil, resolveErr
		}

		if len(ids) > 0 && len(systems) < len(systemdefs.AllSystems()) {
			isSelectiveIndexing = true
			log.Info().Msgf("Starting selective media indexing for systems: %v", ids)
		}
	} else {
		systems = systemdefs.AllSystems()
	}

	// Additional validation for selective indexing
	if isSelectiveIndexing {
		// Check if optimization is running - this would conflict with selective indexing
		optimizationStatus, err := env.Database.MediaDB.GetOptimizationStatus()
		if err != nil {
			return nil, fmt.Errorf("unable to verify optimization status for selective indexing: %w", err)
		}
		if optimizationStatus == "running" {
			return nil, errors.New("selective indexing cannot be performed while database optimization is running")
		}

		// Ensure at least one system is specified for selective indexing
		if len(systems) == 0 {
			return nil, errors.New("at least one system must be specified for selective indexing")
		}
	}

	// Use app-scoped context — indexing outlives the API request
	err := GenerateMediaDB(
		env.State.GetContext(),
		env.Platform,
		env.Config,
		env.State.Notifications,
		systems,
		env.Database,
	)

	return nil, err
}

func HandleMediaSearch(env requests.RequestEnv) (any, error) { //nolint:gocritic // single-use parameter in API handler
	log.Info().Msg("received media search request")

	select {
	case searchSem <- struct{}{}:
		defer func() { <-searchSem }()
	case <-env.Context.Done():
		return nil, env.Context.Err()
	}

	var params models.SearchParams
	if err := validation.ValidateAndUnmarshal(env.Params, &params); err != nil {
		log.Warn().Err(err).Msg("invalid params")
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	maxResults := defaultMaxResults
	if params.MaxResults != nil && *params.MaxResults > 0 {
		maxResults = *params.MaxResults
	}

	ctx := env.Context

	// Handle cursor-based pagination
	var cursorStr string
	if params.Cursor != nil {
		cursorStr = *params.Cursor
	}
	cursor, err := decodeCursor(cursorStr)
	if err != nil {
		return nil, fmt.Errorf("invalid cursor: %w", err)
	}

	system := params.Systems
	var query string
	if params.Query != nil {
		query = *params.Query
	}
	tagParams := params.Tags

	// Validate and parse tags parameter - requires type:value format
	// Supports operator prefixes: "+" (AND), "-" (NOT), "~" (OR)
	var tagFilters []zapscript.TagFilter
	if tagParams != nil && len(*tagParams) > 0 {
		var parseErr error
		tagFilters, parseErr = filters.ParseTagFilters(*tagParams)
		if parseErr != nil {
			return nil, fmt.Errorf("failed to parse tag filters: %w", parseErr)
		}
	}

	// Normalize letter parameter (validation already done by struct tag)
	var validatedLetter *string
	if params.Letter != nil && *params.Letter != "" {
		letterValue := strings.ToUpper(strings.TrimSpace(*params.Letter))
		validatedLetter = &letterValue
	}

	// Add 1 to limit to check if there are more results
	limit := maxResults + 1
	var searchResults []database.SearchResultWithCursor

	// Prepare systems for search
	fuzzy := params.FuzzySystem != nil && *params.FuzzySystem
	var systemIDs []string
	if system != nil {
		systemIDs = *system
	}
	systems, resolveErr := resolveSystems(systemIDs, fuzzy)
	if resolveErr != nil {
		return nil, resolveErr
	}

	searchFilters := database.SearchFilters{
		Systems: systems,
		Query:   query,
		Tags:    tagFilters, // Will be empty if no tags provided
		Letter:  validatedLetter,
		Cursor:  cursor,
		Limit:   limit,
	}

	searchResults, err = env.Database.MediaDB.SearchMediaWithFilters(ctx, &searchFilters)
	if err != nil {
		return nil, fmt.Errorf("error searching media with filters: %w", err)
	}

	// Check if there are more results
	hasNextPage := len(searchResults) > maxResults
	if hasNextPage {
		searchResults = searchResults[:maxResults]
	}

	// Convert to API models
	results := make([]models.SearchResultMedia, 0, len(searchResults))
	for _, result := range searchResults {
		system, err := systemdefs.GetSystem(result.SystemID)
		if err != nil {
			continue
		}

		resultSystem := models.System{
			ID: system.ID,
		}

		metadata, err := assets.GetSystemMetadata(system.ID)
		if err != nil {
			resultSystem.Name = system.ID
			log.Err(err).Msg("error getting system metadata")
		} else {
			resultSystem.Name = metadata.Name
			resultSystem.Category = metadata.Category
			if metadata.ReleaseDate != "" {
				resultSystem.ReleaseDate = &metadata.ReleaseDate
			}
			if metadata.Manufacturer != "" {
				resultSystem.Manufacturer = &metadata.Manufacturer
			}
		}

		zapScript := result.ZapScript()

		results = append(results, models.SearchResultMedia{
			System:    resultSystem,
			Name:      result.Name,
			Path:      result.Path,
			ZapScript: zapScript,
			Tags:      result.Tags,
		})
	}

	// Build pagination info
	var pagination *models.PaginationInfo
	if len(results) > 0 {
		var nextCursor *string
		if hasNextPage {
			lastResult := searchResults[len(searchResults)-1]
			cursorStr, err := encodeCursor(lastResult.MediaID)
			if err != nil {
				log.Error().Err(err).Msg("failed to encode next cursor")
				return nil, fmt.Errorf("failed to generate next page cursor: %w", err)
			}
			nextCursor = &cursorStr
		}

		pagination = &models.PaginationInfo{
			NextCursor:  nextCursor,
			HasNextPage: hasNextPage,
			PageSize:    maxResults,
		}
	}

	return models.SearchResults{
		Results:    results,
		Total:      len(results), // Deprecated: returns count of results in response
		Pagination: pagination,
	}, nil
}

func HandleMediaTags(env requests.RequestEnv) (any, error) { //nolint:gocritic // single-use parameter in API handler
	log.Info().Msg("received media tags request")

	var params models.SearchParams
	if len(env.Params) > 0 {
		err := json.Unmarshal(env.Params, &params)
		if err != nil {
			return nil, validation.ErrInvalidParams
		}

		// Validate params (systems are validated by struct tags)
		if err := validation.DefaultValidator.Validate(&params); err != nil {
			log.Warn().Err(err).Msg("invalid params")
			return nil, fmt.Errorf("invalid params: %w", err)
		}
	}

	ctx := env.Context

	system := params.Systems

	var tagList []database.TagInfo
	var err error

	// Optimize for "all systems" case
	fuzzy := params.FuzzySystem != nil && *params.FuzzySystem
	switch {
	case system == nil || len(*system) == 0:
		tagList, err = env.Database.MediaDB.GetAllUsedTags(ctx)
	default:
		systems, resolveErr := resolveSystems(*system, fuzzy)
		if resolveErr != nil {
			return nil, resolveErr
		}
		tagList, err = env.Database.MediaDB.GetSystemTagsCached(ctx, systems)
	}
	if err != nil {
		return nil, fmt.Errorf("error getting tags: %w", err)
	}

	return models.TagsResponse{
		Tags: tagList,
	}, nil
}

func HandleMedia(env requests.RequestEnv) (any, error) { //nolint:gocritic // single-use parameter in API handler
	log.Info().Msg("received media request")

	resp := models.MediaResponse{
		Active: make([]models.ActiveMediaResponse, 0),
	}

	activeMedia := env.State.ActiveMedia()
	if activeMedia != nil && activeMedia.Path != "" {
		system, err := assets.GetSystemMetadata(activeMedia.SystemID)
		if err != nil {
			return nil, fmt.Errorf("error getting system metadata: %w", err)
		}

		// Build zapScript with optional year from MediaDB
		var year *string
		if env.Database.MediaDB != nil {
			y, yearErr := env.Database.MediaDB.GetYearBySystemAndPath(
				env.Context, system.ID, activeMedia.Path,
			)
			if yearErr != nil {
				log.Debug().Err(yearErr).Msgf("could not get year for %s:%s", system.ID, activeMedia.Path)
			} else if y != "" {
				year = &y
			}
		}
		zapScript := database.BuildTitleZapScript(system.ID, activeMedia.Name, year)

		activeResp := models.ActiveMediaResponse{
			ActiveMedia: models.ActiveMedia{
				Started:          activeMedia.Started,
				LauncherID:       activeMedia.LauncherID,
				SystemID:         system.ID,
				SystemName:       system.Name,
				Name:             activeMedia.Name,
				Path:             activeMedia.Path,
				LauncherControls: activeMedia.LauncherControls,
			},
			ZapScript: zapScript,
		}

		resp.Active = append(resp.Active, activeResp)
	}

	status := statusInstance.get()
	resp.Database.Indexing = status.indexing

	// Get optimization status
	optimizationStatus, err := env.Database.MediaDB.GetOptimizationStatus()
	if err != nil {
		log.Warn().Err(err).Msg("failed to get optimization status for media response")
		optimizationStatus = ""
	}

	switch {
	case resp.Database.Indexing:
		// During indexing, don't show optimizing even if optimization is running
		resp.Database.Optimizing = false
		resp.Database.Exists = false
		resp.Database.TotalSteps = &status.totalSteps
		resp.Database.CurrentStep = &status.currentStep
		resp.Database.CurrentStepDisplay = &status.currentDesc
		resp.Database.TotalFiles = &status.totalFiles
	case optimizationStatus == "running":
		resp.Database.Optimizing = true
		// If optimizing, show the current optimization step
		optimizationStep, stepErr := env.Database.MediaDB.GetOptimizationStep()
		if stepErr != nil {
			log.Warn().Err(stepErr).Msg("failed to get optimization step")
		} else if optimizationStep != "" {
			resp.Database.CurrentStepDisplay = &optimizationStep
		}

		// Database exists but is being optimized
		resp.Database.Exists = true
	default:
		// Not indexing and not optimizing
		resp.Database.Optimizing = false
		// Try to get last generated time, but don't fail if database is locked
		lastGenerated, err := env.Database.MediaDB.GetLastGenerated()
		if err != nil {
			// Database might be locked during indexing transition - don't fail completely
			log.Warn().Err(err).Msg("failed to get last generated time, assuming database doesn't exist")
			resp.Database.Exists = false
		} else {
			resp.Database.Exists = !time.Unix(0, 0).Equal(lastGenerated) && !status.indexing
		}
	}

	// Get total media count if database exists and is not indexing
	if resp.Database.Exists && !resp.Database.Indexing {
		totalCount, err := env.Database.MediaDB.GetTotalMediaCount()
		if err != nil {
			log.Warn().Err(err).Msg("failed to get total media count")
		} else {
			resp.Database.TotalMedia = &totalCount
		}
	}

	return resp, nil
}

//nolint:gocritic // single-use parameter in API handler
func HandleUpdateActiveMedia(env requests.RequestEnv) (any, error) {
	log.Info().Msg("received update active media request")

	if len(env.Params) == 0 {
		log.Info().Msg("clearing active media")
		env.State.SetActiveMedia(nil)
		return NoContent{}, nil
	}

	var params models.UpdateActiveMediaParams
	if err := validation.ValidateAndUnmarshal(env.Params, &params); err != nil {
		log.Warn().Err(err).Msg("invalid params")
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	system, err := systemdefs.LookupSystem(params.SystemID)
	if err != nil {
		return nil, fmt.Errorf("error looking up system: %w", err)
	}

	systemMeta, err := assets.GetSystemMetadata(system.ID)
	if err != nil {
		return nil, fmt.Errorf("error getting system metadata: %w", err)
	}

	activeMedia := models.NewActiveMedia(
		system.ID,
		systemMeta.Name,
		params.MediaPath,
		params.MediaName,
		"", // LauncherID unknown when set via API
	)

	env.State.SetActiveMedia(activeMedia)
	return NoContent{}, nil
}

func HandleActiveMedia(env requests.RequestEnv) (any, error) { //nolint:gocritic // single-use parameter in API handler
	log.Info().Msg("received active media request")

	media := env.State.ActiveMedia()
	if media == nil {
		return nil, nil //nolint:nilnil // nil response means no active media
	}

	// Build zapScript with optional year from MediaDB
	var year *string
	if env.Database.MediaDB != nil {
		y, yearErr := env.Database.MediaDB.GetYearBySystemAndPath(
			env.Context, media.SystemID, media.Path,
		)
		if yearErr != nil {
			log.Debug().Err(yearErr).Msgf("could not get year for %s:%s", media.SystemID, media.Path)
		} else if y != "" {
			year = &y
		}
	}
	zapScript := database.BuildTitleZapScript(media.SystemID, media.Name, year)

	resp := models.ActiveMediaResponse{
		ActiveMedia: models.ActiveMedia{
			Started:          media.Started,
			LauncherID:       media.LauncherID,
			SystemID:         media.SystemID,
			SystemName:       media.SystemName,
			Name:             media.Name,
			Path:             media.Path,
			LauncherControls: media.LauncherControls,
		},
		ZapScript: zapScript,
	}

	return resp, nil
}

//nolint:gocritic,revive // single-use parameter in API handler
func HandleMediaGenerateCancel(env requests.RequestEnv) (any, error) {
	log.Info().Msg("received media generate cancel request")

	if statusInstance.cancel() {
		log.Info().Msg("media indexing cancellation requested")
		return map[string]any{
			"message": "Media indexing cancelled successfully",
		}, nil
	}

	return map[string]any{
		"message": "No media indexing operation is currently running or it has already been cancelled",
	}, nil
}
