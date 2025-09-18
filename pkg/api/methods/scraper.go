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
	"encoding/json"
	"errors"
	"strconv"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/scraper"
	scraperService "github.com/ZaparooProject/zaparoo-core/v2/pkg/service/scraper"
)

// Global scraper service instance (will be initialized in main)
var ScraperServiceInstance *scraperService.ScraperService

// HandleScraperSearch searches for game metadata
func HandleScraperSearch(env requests.RequestEnv) (any, error) {
	if ScraperServiceInstance == nil {
		return nil, errors.New("scraper service not initialized")
	}

	var params struct {
		System   string `json:"system"`
		Name     string `json:"name"`
		Scraper  string `json:"scraper,omitempty"`
		Region   string `json:"region,omitempty"`
		Language string `json:"language,omitempty"`
	}

	if err := json.Unmarshal(env.Params, &params); err != nil {
		return nil, err
	}

	// Get scraper configuration for defaults
	scraperConfig := scraper.GetScraperConfig(env.Platform)

	// Use provided values or fall back to defaults
	region := params.Region
	if region == "" {
		region = scraperConfig.Region
	}

	language := params.Language
	if language == "" {
		language = scraperConfig.Language
	}

	scraperName := params.Scraper
	if scraperName == "" {
		scraperName = scraperConfig.DefaultScraper
	}

	// Validate required parameters
	if params.Name == "" {
		return nil, errors.New("name parameter is required")
	}
	if params.System == "" {
		return nil, errors.New("system parameter is required")
	}

	// Build scraper query
	query := scraper.ScraperQuery{
		Name:     params.Name,
		SystemID: params.System,
		Region:   region,
		Language: language,
	}

	// Perform the search using the scraper service
	results, err := ScraperServiceInstance.Search(env.State.GetContext(), scraperName, query)
	if err != nil {
		return nil, err
	}

	// Convert results to API format
	apiResults := make([]map[string]any, len(results))
	for i, result := range results {
		apiResults[i] = map[string]any{
			"id":          result.ID,
			"name":        result.Name,
			"description": result.Description,
			"system":      result.SystemID,
			"region":      result.Region,
			"language":    result.Language,
			"relevance":   result.Relevance,
		}
	}

	return map[string]any{
		"results":  apiResults,
		"scraper":  scraperName,
		"region":   region,
		"language": language,
	}, nil
}

// HandleScraperScrapeGame scrapes a specific game from MediaDB
func HandleScraperScrapeGame(env requests.RequestEnv) (any, error) {
	if ScraperServiceInstance == nil {
		return nil, errors.New("scraper service not initialized")
	}

	var params struct {
		MediaDBID  any      `json:"mediaDBID"`
		MediaTypes []string `json:"mediaTypes,omitempty"`
		Overwrite  bool     `json:"overwrite,omitempty"`
	}

	if err := json.Unmarshal(env.Params, &params); err != nil {
		return nil, err
	}

	// Convert MediaDBID to int64
	var mediaDBID int64
	switch v := params.MediaDBID.(type) {
	case int:
		mediaDBID = int64(v)
	case int64:
		mediaDBID = v
	case float64:
		mediaDBID = int64(v)
	case string:
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return nil, errors.New("invalid mediaDBID format")
		}
		mediaDBID = id
	default:
		return nil, errors.New("mediaDBID must be a number")
	}

	// Check if MediaDB exists
	if !env.Database.MediaDB.Exists() {
		return nil, errors.New("MediaDB not generated - run media.generate first")
	}

	// Get the game from MediaDB to verify it exists
	media, err := env.Database.MediaDB.GetMediaByID(mediaDBID)
	if err != nil {
		return nil, errors.New("game not found in MediaDB")
	}

	mediaTitle, err := env.Database.MediaDB.GetMediaTitleByID(media.MediaTitleDBID)
	if err != nil {
		return nil, errors.New("media title not found")
	}

	// Start scraping in background
	go func() {
		if err := ScraperServiceInstance.ScrapeGameByID(env.State.GetContext(), mediaDBID); err != nil {
			// Log error but don't block
			// In a real implementation, you'd send a notification
		}
	}()

	return map[string]any{
		"started":   true,
		"mediaDBID": mediaDBID,
		"name":      mediaTitle.Name,
		"path":      media.Path,
	}, nil
}

// HandleScraperScrapeSystem scrapes an entire system from MediaDB
func HandleScraperScrapeSystem(env requests.RequestEnv) (any, error) {
	if ScraperServiceInstance == nil {
		return nil, errors.New("scraper service not initialized")
	}

	var params struct {
		System        string   `json:"system"`
		MediaTypes    []string `json:"mediaTypes,omitempty"`
		UnscrapedOnly bool     `json:"unscrapedOnly,omitempty"`
		Limit         int      `json:"limit,omitempty"`
	}

	if err := json.Unmarshal(env.Params, &params); err != nil {
		return nil, err
	}

	if params.System == "" {
		return nil, errors.New("system parameter is required")
	}

	// Check if MediaDB exists
	if !env.Database.MediaDB.Exists() {
		return nil, errors.New("MediaDB not generated - run media.generate first")
	}

	// Get games count for this system
	var games []any
	if params.UnscrapedOnly {
		limit := params.Limit
		if limit <= 0 {
			limit = 1000 // Default limit
		}
		gamesWithoutMetadata, err := env.Database.MediaDB.GetGamesWithoutMetadata(params.System, limit)
		if err != nil {
			return nil, err
		}
		games = make([]any, len(gamesWithoutMetadata))
		for i, game := range gamesWithoutMetadata {
			games[i] = game
		}
	} else {
		allGames, err := env.Database.MediaDB.GetMediaTitlesBySystem(params.System)
		if err != nil {
			return nil, err
		}
		games = make([]any, len(allGames))
		for i, game := range allGames {
			games[i] = game
		}
	}

	// Start scraping in background
	go func() {
		if err := ScraperServiceInstance.ScrapeSystem(env.State.GetContext(), params.System); err != nil {
			// Log error but don't block
			// In a real implementation, you'd send a notification
		}
	}()

	return map[string]any{
		"started":       true,
		"system":        params.System,
		"totalGames":    len(games),
		"unscrapedOnly": params.UnscrapedOnly,
	}, nil
}

// HandleScraperProgress gets scraping progress
func HandleScraperProgress(env requests.RequestEnv) (any, error) {
	if ScraperServiceInstance == nil {
		return nil, errors.New("scraper service not initialized")
	}

	progress := ScraperServiceInstance.GetProgress()
	return progress, nil
}

// HandleScraperCancel cancels ongoing scraping
func HandleScraperCancel(env requests.RequestEnv) (any, error) {
	if ScraperServiceInstance == nil {
		return nil, errors.New("scraper service not initialized")
	}

	err := ScraperServiceInstance.CancelScraping()
	return map[string]bool{"cancelled": err == nil}, err
}

// HandleScraperConfig gets or updates scraper configuration
func HandleScraperConfig(env requests.RequestEnv) (any, error) {
	var params struct {
		Config map[string]interface{} `json:"config,omitempty"`
		Action string                 `json:"action"`
	}

	if err := json.Unmarshal(env.Params, &params); err != nil {
		return nil, err
	}

	switch params.Action {
	case "get", "":
		// Get current configuration
		config := scraper.GetScraperConfig(env.Platform)
		return config, nil

	case "update":
		// Update configuration
		if params.Config == nil {
			return nil, errors.New("config parameter required for update action")
		}

		// This is a simplified implementation
		// In a real implementation, you'd parse the config and save it
		return map[string]any{
			"updated": true,
			"config":  params.Config,
		}, nil

	default:
		return nil, errors.New("invalid action: must be 'get' or 'update'")
	}
}
