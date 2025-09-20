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

package thegamesdb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/scraper"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/shared/httpclient"
	"github.com/rs/zerolog/log"
)

const (
	baseURL   = "https://api.thegamesdb.net/v1"
	userAgent = "Zaparoo Core/1.0"

	// TheGamesDB API limits
	defaultRateLimit = 1000 // milliseconds between requests
	maxRetries       = 3
)

// TheGamesDB implements the Scraper interface for TheGamesDB API
type TheGamesDB struct {
	lastRequest time.Time
	client      *httpclient.Client
	platformMap *PlatformMapper
	rateLimit   time.Duration
}

// NewTheGamesDB creates a new TheGamesDB instance
func NewTheGamesDB() *TheGamesDB {
	return &TheGamesDB{
		client:      httpclient.NewClientWithTimeout(30 * time.Second),
		platformMap: NewPlatformMapper(),
		rateLimit:   time.Duration(defaultRateLimit) * time.Millisecond,
	}
}

// GetInfo returns scraper information
func (*TheGamesDB) GetInfo() scraper.ScraperInfo {
	return scraper.ScraperInfo{
		Name:         "TheGamesDB",
		Version:      "1.0",
		Description:  "TheGamesDB.net API integration with name-based searching",
		Website:      "https://thegamesdb.net",
		RequiresAuth: true, // Requires API key
	}
}

// IsSupportedPlatform checks if the given system ID is supported
func (tgdb *TheGamesDB) IsSupportedPlatform(systemID string) bool {
	_, supported := tgdb.platformMap.MapToScraperPlatform(systemID)
	return supported
}

// GetSupportedMediaTypes returns the media types supported by TheGamesDB
func (*TheGamesDB) GetSupportedMediaTypes() []scraper.MediaType {
	return []scraper.MediaType{
		scraper.MediaTypeCover,
		scraper.MediaTypeBoxBack,
		scraper.MediaTypeScreenshot,
		scraper.MediaTypeFanArt,
		scraper.MediaTypeMarquee,
	}
}

// Search searches for games matching the query
func (tgdb *TheGamesDB) Search(ctx context.Context, query scraper.ScraperQuery) ([]scraper.ScraperResult, error) {
	// Rate limit check
	if err := tgdb.waitForRateLimit(); err != nil {
		return nil, err
	}

	// Map system ID to TheGamesDB platform
	platformID, ok := tgdb.platformMap.MapToScraperPlatform(query.SystemID)
	if !ok {
		return nil, fmt.Errorf("unsupported platform: %s", query.SystemID)
	}

	// Build search URL
	searchURL, err := tgdb.buildSearchURL(query.Name, platformID)
	if err != nil {
		return nil, fmt.Errorf("failed to build search URL: %w", err)
	}

	log.Debug().Str("url", searchURL).Msg("TheGamesDB search request")

	// Make the request
	resp, err := tgdb.client.Get(ctx, searchURL)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("Failed to close response body")
		}
	}()

	// Handle non-200 responses
	if resp.StatusCode != http.StatusOK {
		return nil, tgdb.handleHTTPError(resp.StatusCode)
	}

	// Parse response
	var apiResp APIResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// Check for API errors
	if apiResp.Code != 200 {
		return nil, fmt.Errorf("API error: %s (code %d)", apiResp.Status, apiResp.Code)
	}

	if apiResp.Data == nil || len(apiResp.Data.Games) == 0 {
		return nil, nil // No results found
	}

	// Convert to scraper results
	results := make([]scraper.ScraperResult, 0, len(apiResp.Data.Games))
	for i := range apiResp.Data.Games {
		result := tgdb.convertGameToResult(&apiResp.Data.Games[i], query.SystemID, apiResp.Include)
		results = append(results, result)
	}

	return results, nil
}

// GetGameInfo gets detailed information about a specific game
func (tgdb *TheGamesDB) GetGameInfo(ctx context.Context, gameID string) (*scraper.GameInfo, error) {
	// Rate limit check
	if err := tgdb.waitForRateLimit(); err != nil {
		return nil, err
	}

	// Build game info URL
	gameURL, err := tgdb.buildGameInfoURL(gameID)
	if err != nil {
		return nil, fmt.Errorf("failed to build game info URL: %w", err)
	}

	log.Debug().Str("url", gameURL).Msg("TheGamesDB game info request")

	// Make the request
	resp, err := tgdb.client.Get(ctx, gameURL)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("Failed to close response body")
		}
	}()

	// Handle non-200 responses
	if resp.StatusCode != http.StatusOK {
		return nil, tgdb.handleHTTPError(resp.StatusCode)
	}

	// Parse response
	var apiResp APIResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// Check for API errors
	if apiResp.Code != 200 {
		return nil, fmt.Errorf("API error: %s (code %d)", apiResp.Status, apiResp.Code)
	}

	if apiResp.Data == nil || len(apiResp.Data.Games) == 0 {
		return nil, errors.New("game not found")
	}

	// Convert to game info
	game := apiResp.Data.Games[0]
	gameInfo := tgdb.convertGameToInfo(&game, apiResp.Include)
	return gameInfo, nil
}

// DownloadMedia downloads media files for a game
func (tgdb *TheGamesDB) DownloadMedia(_ context.Context, media *scraper.MediaItem) error {
	// Rate limit check
	if err := tgdb.waitForRateLimit(); err != nil {
		return err
	}

	log.Debug().
		Str("url", media.URL).
		Str("type", string(media.Type)).
		Msg("TheGamesDB downloading media")

	// TheGamesDB provides direct URLs, so we can download directly
	// This would be used by the scraper service to download to the appropriate path
	return errors.New("media download should be handled by scraper service")
}

// waitForRateLimit ensures we don't exceed rate limits
func (tgdb *TheGamesDB) waitForRateLimit() error {
	now := time.Now()
	timeSinceLastRequest := now.Sub(tgdb.lastRequest)

	if timeSinceLastRequest < tgdb.rateLimit {
		sleepTime := tgdb.rateLimit - timeSinceLastRequest
		log.Debug().Dur("sleep", sleepTime).Msg("Rate limiting TheGamesDB request")
		time.Sleep(sleepTime)
	}

	tgdb.lastRequest = time.Now()
	return nil
}

// buildSearchURL constructs the search URL for TheGamesDB API
func (*TheGamesDB) buildSearchURL(gameName, platformID string) (string, error) {
	u, err := url.Parse(baseURL + "/Games/ByGameName")
	if err != nil {
		return "", fmt.Errorf("failed to parse TheGamesDB search URL: %w", err)
	}

	params := url.Values{}
	params.Set("name", gameName)
	params.Set("platform", platformID)
	params.Set("include", "boxart") // Include boxart data
	params.Set("page", "1")
	params.Set("fields", "players,publishers,genres,overview,last_updated,rating,platform,"+
		"coop,youtube,os,processor,ram,video,sound,alternates")

	// Get API key from auth.toml
	authCfg := config.GetAuthCfg()
	creds := config.LookupAuth(authCfg, "https://api.thegamesdb.net")
	if creds != nil && creds.Bearer != "" {
		params.Set("apikey", creds.Bearer)
	}

	u.RawQuery = params.Encode()
	return u.String(), nil
}

// buildGameInfoURL constructs the game info URL
func (*TheGamesDB) buildGameInfoURL(gameID string) (string, error) {
	u, err := url.Parse(baseURL + "/Games/ByGameID")
	if err != nil {
		return "", fmt.Errorf("failed to parse TheGamesDB game info URL: %w", err)
	}

	params := url.Values{}
	params.Set("id", gameID)
	params.Set("include", "boxart") // Include boxart data
	params.Set("fields", "players,publishers,genres,overview,last_updated,rating,platform,"+
		"coop,youtube,os,processor,ram,video,sound,alternates")

	// Get API key from auth.toml
	authCfg := config.GetAuthCfg()
	creds := config.LookupAuth(authCfg, "https://api.thegamesdb.net")
	if creds != nil && creds.Bearer != "" {
		params.Set("apikey", creds.Bearer)
	}

	u.RawQuery = params.Encode()
	return u.String(), nil
}

// handleHTTPError handles HTTP error responses
func (*TheGamesDB) handleHTTPError(statusCode int) error {
	switch statusCode {
	case http.StatusTooManyRequests:
		return errors.New("rate limited by TheGamesDB (429)")
	case http.StatusUnauthorized:
		return errors.New("authentication failed - check API key in auth.toml")
	case http.StatusForbidden:
		return errors.New("access forbidden - check API permissions")
	case http.StatusNotFound:
		return errors.New("game not found")
	case http.StatusInternalServerError:
		return errors.New("TheGamesDB server error")
	default:
		return fmt.Errorf("HTTP error %d", statusCode)
	}
}

// convertGameToResult converts TheGamesDB game data to ScraperResult
func (*TheGamesDB) convertGameToResult(game *Game, systemID string, _ *APIResponseInclude) scraper.ScraperResult {
	// Calculate relevance score based on available data
	relevance := 0.5 // Base relevance
	if game.GameTitle != "" {
		relevance += 0.2
	}
	if game.Overview != "" {
		relevance += 0.2
	}
	if len(game.Genres) > 0 {
		relevance += 0.1
	}

	return scraper.ScraperResult{
		ID:          strconv.Itoa(game.ID),
		Name:        game.GameTitle,
		Description: game.Overview,
		SystemID:    systemID,
		Region:      "us", // TheGamesDB doesn't have specific region info
		Language:    "en", // TheGamesDB is primarily English
		Relevance:   relevance,
	}
}

// convertGameToInfo converts TheGamesDB game data to detailed GameInfo
func (tgdb *TheGamesDB) convertGameToInfo(game *Game, include *APIResponseInclude) *scraper.GameInfo {
	gameInfo := &scraper.GameInfo{
		ID:          strconv.Itoa(game.ID),
		Name:        game.GameTitle,
		Description: game.Overview,
		ReleaseDate: game.ReleaseDate,
		Rating:      tgdb.parseRating(game.Rating),
		Region:      "us",
		Language:    "en",
		Players:     strconv.Itoa(game.Players),
	}

	// Extract genre information
	if include != nil && include.Genres != nil && len(game.Genres) > 0 {
		var genres []string
		for _, genreID := range game.Genres {
			if genre, exists := include.Genres[strconv.Itoa(genreID)]; exists {
				genres = append(genres, genre.Name)
			}
		}
		gameInfo.Genre = strings.Join(genres, ", ")
	}

	// Extract developer information
	if include != nil && include.Developers != nil && len(game.Developers) > 0 {
		var developers []string
		for _, devID := range game.Developers {
			if developer, exists := include.Developers[strconv.Itoa(devID)]; exists {
				developers = append(developers, developer.Name)
			}
		}
		gameInfo.Developer = strings.Join(developers, ", ")
	}

	// Extract publisher information
	if include != nil && include.Publishers != nil && len(game.Publishers) > 0 {
		var publishers []string
		for _, pubID := range game.Publishers {
			if publisher, exists := include.Publishers[strconv.Itoa(pubID)]; exists {
				publishers = append(publishers, publisher.Name)
			}
		}
		gameInfo.Publisher = strings.Join(publishers, ", ")
	}

	// Convert boxart to media items
	if include != nil && include.Boxart != nil {
		gameInfo.Media = tgdb.convertBoxartToMediaItems(include.Boxart, strconv.Itoa(game.ID))
	}

	return gameInfo
}

// convertBoxartToMediaItems converts TheGamesDB boxart to MediaItems
func (tgdb *TheGamesDB) convertBoxartToMediaItems(boxartMap map[string]Boxart, _ string) []scraper.MediaItem {
	items := make([]scraper.MediaItem, 0, len(boxartMap))

	for _, boxart := range boxartMap {
		mediaType := tgdb.mapMediaType(boxart.Type, boxart.Side)
		if mediaType == "" {
			continue // Skip unknown media types
		}

		// Construct full URL for the image
		imageURL := fmt.Sprintf("https://cdn.thegamesdb.net/images/thumb/%s", boxart.Filename)

		item := scraper.MediaItem{
			Type:        scraper.MediaType(mediaType),
			URL:         imageURL,
			Format:      "jpg", // TheGamesDB typically uses JPG
			Description: fmt.Sprintf("%s %s", boxart.Type, boxart.Side),
		}

		items = append(items, item)
	}

	return items
}

// mapMediaType maps TheGamesDB media types to scraper media types
func (*TheGamesDB) mapMediaType(tgdbType, side string) string {
	switch tgdbType {
	case "boxart":
		switch side {
		case "front":
			return string(scraper.MediaTypeCover)
		case "back":
			return string(scraper.MediaTypeBoxBack)
		}
	case "screenshot":
		return string(scraper.MediaTypeScreenshot)
	case "fanart":
		return string(scraper.MediaTypeFanArt)
	case "banner":
		return string(scraper.MediaTypeMarquee)
	}

	return "" // Unknown type
}

// parseRating converts TheGamesDB rating to a numeric value
func (*TheGamesDB) parseRating(rating string) float64 {
	// TheGamesDB uses ratings like "E - Everyone", "T - Teen", etc.
	// Convert to a simple numeric scale
	switch strings.ToUpper(rating) {
	case "E - EVERYONE", "E":
		return 1.0
	case "E10+ - EVERYONE 10+", "E10+":
		return 2.0
	case "T - TEEN", "T":
		return 3.0
	case "M - MATURE 17+", "M":
		return 4.0
	case "AO - ADULTS ONLY 18+", "AO":
		return 5.0
	default:
		return 0.0
	}
}
