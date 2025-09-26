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

package methods

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/notifications"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/assets"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediascanner"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/rs/zerolog/log"
)

const defaultMaxResults = 100

// Search cancellation tracking for rapid search queries
type searchCancelEntry struct {
	cancel context.CancelFunc
	id     uint64
}

var (
	activeSearchCancels = make(map[string]searchCancelEntry)
	searchCancelsMu     sync.RWMutex
	searchCancelSeq     uint64
)

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

const (
	maxTagsCount = 50
	maxTagLength = 128
)

// validateAndNormalizeTags validates and normalizes a slice of tags
// Returns error if validation fails, or normalized tags slice
func validateAndNormalizeTags(tags []string) ([]string, error) {
	if len(tags) > maxTagsCount {
		return nil, fmt.Errorf("exceeded maximum number of tags for search: %d (max: %d)", len(tags), maxTagsCount)
	}

	// Use a map to deduplicate and normalize
	seen := make(map[string]bool)
	normalized := make([]string, 0, len(tags))

	for _, tag := range tags {
		// Trim whitespace
		tag = strings.TrimSpace(tag)

		// Skip empty tags
		if tag == "" {
			continue
		}

		// Check length
		if len(tag) > maxTagLength {
			return nil, fmt.Errorf("tag too long: %q (max: %d characters)", tag, maxTagLength)
		}

		// Deduplicate (case-sensitive)
		if !seen[tag] {
			seen[tag] = true
			normalized = append(normalized, tag)
		}
	}

	return normalized, nil
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
	mu          sync.RWMutex
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

func (s *indexingStatus) start() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.indexing = true
	s.totalSteps = 0
	s.currentStep = 0
	s.currentDesc = ""
	s.totalFiles = 0
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

// cancelPreviousSearch cancels any in-flight search for the given client and stores the new cancel function
// Returns a unique ID that should be used for cleanup to avoid race conditions
func cancelPreviousSearch(clientID string, newCancel context.CancelFunc) uint64 {
	if clientID == "" {
		return 0
	}

	searchCancelsMu.Lock()
	defer searchCancelsMu.Unlock()

	if prevEntry, exists := activeSearchCancels[clientID]; exists {
		prevEntry.cancel()
		log.Debug().Str("client", clientID).Msg("cancelled previous search for client")
	}

	// Generate unique ID for this search request
	id := atomic.AddUint64(&searchCancelSeq, 1)
	activeSearchCancels[clientID] = searchCancelEntry{
		id:     id,
		cancel: newCancel,
	}
	return id
}

// cleanupSearchCancel removes the search cancel function for a client only if the ID matches
// This prevents race conditions where an older request cleans up a newer request's cancel function
func cleanupSearchCancel(clientID string, id uint64) {
	if clientID == "" || id == 0 {
		return
	}

	searchCancelsMu.Lock()
	defer searchCancelsMu.Unlock()

	// Only remove if this is the same request that registered the cancel function
	if entry, exists := activeSearchCancels[clientID]; exists && entry.id == id {
		delete(activeSearchCancels, clientID)
	}
}

func GenerateMediaDB(
	ctx context.Context,
	pl platforms.Platform,
	cfg *config.Instance,
	ns chan<- models.Notification,
	systems []systemdefs.System,
	db *database.Database,
) error {
	if statusInstance.get().indexing {
		return errors.New("indexing already in progress")
	}

	// Also prevent indexing if optimization is running
	optimizationStatus, err := db.MediaDB.GetOptimizationStatus()
	if err != nil {
		// If we can't read the status, assume it might be in an unknown state
		// and prevent indexing to avoid potential conflicts
		return fmt.Errorf("failed to get optimization status during indexing check: %w", err)
	} else if optimizationStatus == "running" {
		return errors.New("database optimization in progress")
	}

	statusInstance.start()
	startTime := time.Now()

	// Create cancellable context for indexing
	indexCtx, cancelFunc := context.WithCancel(ctx)
	statusInstance.setCancelFunc(cancelFunc)

	log.Info().Msg("generating media db")
	notifications.MediaIndexing(ns, models.IndexingStatusResponse{
		Exists:   false,
		Indexing: true,
	})

	go func() {
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
		err := json.Unmarshal(env.Params, &params)
		if err != nil {
			return nil, ErrInvalidParams
		}

		if params.Systems == nil || len(*params.Systems) == 0 {
			systems = systemdefs.AllSystems()
		} else {
			isSelectiveIndexing = true
			// Validate all provided system IDs
			for _, s := range *params.Systems {
				system, err := systemdefs.GetSystem(s)
				if err != nil {
					return nil, fmt.Errorf("invalid system ID %s: %w", s, err)
				}
				systems = append(systems, *system)
			}

			// Check if we're actually doing selective indexing (not all systems)
			allSystems := systemdefs.AllSystems()
			if len(systems) == len(allSystems) {
				// Double-check by comparing system IDs
				systemIDsMap := make(map[string]bool)
				for _, sys := range systems {
					systemIDsMap[sys.ID] = true
				}
				for _, sys := range allSystems {
					if !systemIDsMap[sys.ID] {
						break
					}
				}
				if len(systemIDsMap) == len(allSystems) {
					isSelectiveIndexing = false
				}
			}

			if isSelectiveIndexing {
				log.Info().Msgf("Starting selective media indexing for systems: %v", *params.Systems)
			}
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

	if len(env.Params) == 0 {
		return nil, ErrMissingParams
	}

	var params models.SearchParams
	err := json.Unmarshal(env.Params, &params)
	if err != nil {
		return nil, ErrInvalidParams
	}

	maxResults := defaultMaxResults
	if params.MaxResults != nil && *params.MaxResults > 0 {
		maxResults = *params.MaxResults
	}

	if params.Query == "" && (params.Systems == nil || len(*params.Systems) == 0) {
		return nil, errors.New("query or system is required")
	}

	// Create a cancellable context for this search request
	ctx, cancel := context.WithCancel(env.State.GetContext())
	defer cancel() // Always call cancel to release resources

	// Cancel any previous search for this client and register this one
	searchID := cancelPreviousSearch(env.ClientID, cancel)
	defer cleanupSearchCancel(env.ClientID, searchID)

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
	query := params.Query
	tags := params.Tags

	// Validate and normalize tags parameter
	var normalizedTags []string
	if tags != nil && len(*tags) > 0 {
		var validationErr error
		normalizedTags, validationErr = validateAndNormalizeTags(*tags)
		if validationErr != nil {
			return nil, validationErr
		}
	}

	// Add 1 to limit to check if there are more results
	limit := maxResults + 1
	var searchResults []database.SearchResultWithCursor

	// Use filtered search if tags are provided, otherwise use existing method for backward compatibility
	if len(normalizedTags) > 0 {
		// Prepare systems for filtered search
		var systems []systemdefs.System
		if system == nil || len(*system) == 0 {
			systems = systemdefs.AllSystems()
		} else {
			systems = make([]systemdefs.System, 0, len(*system))
			for _, s := range *system {
				sys, systemErr := systemdefs.GetSystem(s)
				if systemErr != nil {
					return nil, fmt.Errorf("error getting system %s: %w", s, systemErr)
				}
				systems = append(systems, *sys)
			}
		}

		// Use the new filtered search method
		filters := database.SearchFilters{
			Systems: systems,
			Query:   query,
			Tags:    normalizedTags,
			Cursor:  cursor,
			Limit:   limit,
		}

		searchResults, err = env.Database.MediaDB.SearchMediaWithFilters(ctx, &filters)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				log.Info().Str("client", env.ClientID).Msg("filtered search request cancelled by newer request")
				return nil, errors.New("search cancelled by newer request")
			}
			return nil, fmt.Errorf("error searching media with filters: %w", err)
		}
	} else {
		// Use existing search method for backward compatibility
		if system == nil || len(*system) == 0 {
			searchResults, err = env.Database.MediaDB.SearchMediaPathWordsWithCursor(
				ctx, systemdefs.AllSystems(), query, cursor, limit)
			if err != nil {
				if errors.Is(err, context.Canceled) {
					log.Info().Str("client", env.ClientID).Msg("search request cancelled by newer request")
					return nil, errors.New("search cancelled by newer request")
				}
				return nil, fmt.Errorf("error searching all media with cursor: %w", err)
			}
		} else {
			systems := make([]systemdefs.System, 0)
			for _, s := range *system {
				sys, systemErr := systemdefs.GetSystem(s)
				if systemErr != nil {
					return nil, fmt.Errorf("error getting system %s: %w", s, systemErr)
				}
				systems = append(systems, *sys)
			}

			searchResults, err = env.Database.MediaDB.SearchMediaPathWordsWithCursor(ctx, systems, query, cursor, limit)
			if err != nil {
				if errors.Is(err, context.Canceled) {
					log.Info().Str("client", env.ClientID).Msg("search request cancelled by newer request")
					return nil, errors.New("search cancelled by newer request")
				}
				return nil, fmt.Errorf("error searching media with cursor: %w", err)
			}
		}
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
		}

		results = append(results, models.SearchResultMedia{
			System: resultSystem,
			Name:   result.Name,
			Path:   env.Platform.NormalizePath(env.Config, result.Path),
			Tags:   result.Tags,
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

func HandleMediaFacets(env requests.RequestEnv) (any, error) { //nolint:gocritic // single-use parameter in API handler
	log.Info().Msg("received media facets request")

	if len(env.Params) == 0 {
		return nil, ErrMissingParams
	}

	var params models.SearchParams
	err := json.Unmarshal(env.Params, &params)
	if err != nil {
		return nil, ErrInvalidParams
	}

	// Create a cancellable context for this facet request
	ctx, cancel := context.WithCancel(env.State.GetContext())
	defer cancel() // Always call cancel to release resources

	// Cancel any previous search for this client and register this one
	searchID := cancelPreviousSearch(env.ClientID, cancel)
	defer cleanupSearchCancel(env.ClientID, searchID)

	system := params.Systems
	query := params.Query
	tags := params.Tags

	// Validate and normalize tags parameter
	var normalizedTags []string
	if tags != nil && len(*tags) > 0 {
		var validationErr error
		normalizedTags, validationErr = validateAndNormalizeTags(*tags)
		if validationErr != nil {
			return nil, validationErr
		}
	}

	// Prepare systems for facet search
	var systems []systemdefs.System
	if system == nil || len(*system) == 0 {
		systems = systemdefs.AllSystems()
	} else {
		systems = make([]systemdefs.System, 0, len(*system))
		for _, s := range *system {
			sys, systemErr := systemdefs.GetSystem(s)
			if systemErr != nil {
				return nil, fmt.Errorf("error getting system %s: %w", s, systemErr)
			}
			systems = append(systems, *sys)
		}
	}

	// Prepare filters for facet search
	filters := database.SearchFilters{
		Systems: systems,
		Query:   query,
		Tags:    normalizedTags,
	}

	// Get facets from database
	facets, err := env.Database.MediaDB.GetTagFacets(ctx, &filters)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			log.Info().Str("client", env.ClientID).Msg("facet request cancelled")
			return nil, errors.New("facet request cancelled")
		}
		return nil, fmt.Errorf("error getting tag facets: %w", err)
	}

	// Convert to API models
	responseFacets := make([]models.Facet, 0, len(facets))
	for _, facet := range facets {
		values := make([]models.FacetValue, 0, len(facet.Values))
		for _, value := range facet.Values {
			values = append(values, models.FacetValue{
				Tag:   value.Tag,
				Count: value.Count,
			})
		}

		responseFacets = append(responseFacets, models.Facet{
			Type:   facet.Type,
			Values: values,
		})
	}

	return models.FacetsResponse{
		Facets: responseFacets,
	}, nil
}

func HandleMedia(env requests.RequestEnv) (any, error) { //nolint:gocritic // single-use parameter in API handler
	log.Info().Msg("received media request")

	resp := models.MediaResponse{
		Active: make([]models.ActiveMedia, 0),
	}

	activeMedia := env.State.ActiveMedia()
	if activeMedia != nil && activeMedia.Path != "" {
		system, err := assets.GetSystemMetadata(activeMedia.SystemID)
		if err != nil {
			return nil, fmt.Errorf("error getting system metadata: %w", err)
		}

		resp.Active = append(resp.Active, models.ActiveMedia{
			SystemID:   system.ID,
			SystemName: system.Name,
			Name:       activeMedia.Name,
			Path:       env.Platform.NormalizePath(env.Config, activeMedia.Path),
		})
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
	err := json.Unmarshal(env.Params, &params)
	if err != nil {
		return nil, ErrInvalidParams
	}

	system, err := systemdefs.LookupSystem(params.SystemID)
	if err != nil {
		return nil, fmt.Errorf("error looking up system: %w", err)
	}

	systemMeta, err := assets.GetSystemMetadata(system.ID)
	if err != nil {
		return nil, fmt.Errorf("error getting system metadata: %w", err)
	}

	activeMedia := models.ActiveMedia{
		SystemID:   system.ID,
		SystemName: systemMeta.Name,
		Name:       params.MediaName,
		Path:       env.Platform.NormalizePath(env.Config, params.MediaPath),
	}

	env.State.SetActiveMedia(&activeMedia)
	return NoContent{}, nil
}

func HandleActiveMedia(env requests.RequestEnv) (any, error) { //nolint:gocritic // single-use parameter in API handler
	log.Info().Msg("received active media request")

	media := env.State.ActiveMedia()
	if media == nil {
		return nil, nil //nolint:nilnil // nil response means no active media
	}

	return models.ActiveMedia{
		SystemID:   media.SystemID,
		SystemName: media.SystemName,
		Name:       media.Name,
		Path:       media.Path,
	}, nil
}

//nolint:gocritic,revive // single-use parameter in API handler
func HandleMediaGenerateCancel(env requests.RequestEnv) (any, error) {
	log.Info().Msg("received media generate cancel request")

	if !statusInstance.get().indexing {
		return map[string]interface{}{
			"message": "No media indexing operation is currently running",
		}, nil
	}

	cancelled := statusInstance.cancel()
	if !cancelled {
		return map[string]interface{}{
			"message": "Media indexing not active or already cancelled",
		}, nil
	}

	log.Info().Msg("media indexing cancellation requested")

	return map[string]interface{}{
		"message": "Media indexing cancelled successfully",
	}, nil
}
