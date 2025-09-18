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

package screenscraper

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
	baseURL   = "https://www.screenscraper.fr/api2"
	userAgent = "Zaparoo Core/1.0"

	// ScreenScraper API limits - be conservative to avoid being blocked
	defaultRateLimit = 1000 // milliseconds between requests
	maxRetries       = 3
)

// ScreenScraper implements the Scraper interface for ScreenScraper.fr API
type ScreenScraper struct {
	lastRequest time.Time
	client      *httpclient.Client
	platformMap *PlatformMapper
	rateLimit   time.Duration
}

// NewScreenScraper creates a new ScreenScraper instance
func NewScreenScraper() *ScreenScraper {
	return &ScreenScraper{
		client:      httpclient.NewClientWithTimeout(30 * time.Second),
		platformMap: NewPlatformMapper(),
		rateLimit:   time.Duration(defaultRateLimit) * time.Millisecond,
	}
}

// GetInfo returns scraper information
func (*ScreenScraper) GetInfo() scraper.ScraperInfo {
	return scraper.ScraperInfo{
		Name:         "ScreenScraper",
		Version:      "1.0",
		Description:  "ScreenScraper.fr API integration with hash-based matching",
		Website:      "https://www.screenscraper.fr",
		RequiresAuth: true,
	}
}

// IsSupportedPlatform checks if the given system ID is supported
func (ss *ScreenScraper) IsSupportedPlatform(systemID string) bool {
	_, supported := ss.platformMap.MapToScraperPlatform(systemID)
	return supported
}

// GetSupportedMediaTypes returns the media types supported by ScreenScraper
func (*ScreenScraper) GetSupportedMediaTypes() []scraper.MediaType {
	return []scraper.MediaType{
		scraper.MediaTypeCover,
		scraper.MediaTypeBoxBack,
		scraper.MediaTypeScreenshot,
		scraper.MediaTypeTitleShot,
		scraper.MediaTypeFanArt,
		scraper.MediaTypeMarquee,
		scraper.MediaTypeWheel,
		scraper.MediaTypeVideo,
		scraper.MediaTypeManual,
	}
}

// Search searches for games matching the query
func (ss *ScreenScraper) Search(ctx context.Context, query scraper.ScraperQuery) ([]scraper.ScraperResult, error) {
	// Rate limit check
	if err := ss.waitForRateLimit(); err != nil {
		return nil, err
	}

	// Map system ID to ScreenScraper platform
	platformID, ok := ss.platformMap.MapToScraperPlatform(query.SystemID)
	if !ok {
		return nil, fmt.Errorf("unsupported platform: %s", query.SystemID)
	}

	// Build search URL
	searchURL, err := ss.buildSearchURL(query.Name, platformID, query.Hash, query.Region, query.Language)
	if err != nil {
		return nil, fmt.Errorf("failed to build search URL: %w", err)
	}

	log.Debug().Str("url", searchURL).Msg("ScreenScraper search request")

	// Make the request
	resp, err := ss.client.Get(ctx, searchURL)
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
		return nil, ss.handleHTTPError(resp.StatusCode)
	}

	// Parse response
	var apiResp APIResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// Check for API errors
	if apiResp.Header.Error != "" {
		return nil, fmt.Errorf("API error: %s", apiResp.Header.Error)
	}

	// Convert to scraper results
	var results []scraper.ScraperResult
	if apiResp.Response.Game != nil {
		result := ss.convertGameToResult(apiResp.Response.Game, query.SystemID)
		results = append(results, result)
	}

	return results, nil
}

// GetGameInfo gets detailed information about a specific game
func (ss *ScreenScraper) GetGameInfo(ctx context.Context, gameID string) (*scraper.GameInfo, error) {
	// Rate limit check
	if err := ss.waitForRateLimit(); err != nil {
		return nil, err
	}

	// Build game info URL
	gameURL, err := ss.buildGameInfoURL(gameID)
	if err != nil {
		return nil, fmt.Errorf("failed to build game info URL: %w", err)
	}

	log.Debug().Str("url", gameURL).Msg("ScreenScraper game info request")

	// Make the request
	resp, err := ss.client.Get(ctx, gameURL)
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
		return nil, ss.handleHTTPError(resp.StatusCode)
	}

	// Parse response
	var apiResp APIResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// Check for API errors
	if apiResp.Header.Error != "" {
		return nil, fmt.Errorf("API error: %s", apiResp.Header.Error)
	}

	if apiResp.Response.Game == nil {
		return nil, errors.New("game not found")
	}

	// Convert to game info
	gameInfo := ss.convertGameToInfo(apiResp.Response.Game)
	return gameInfo, nil
}

// DownloadMedia downloads media files for a game
// Note: This method is not typically called directly. The scraper service
// handles downloads using the URLs from GetGameInfo(). This method exists
// for interface compliance but returns an error indicating the proper flow.
func (ss *ScreenScraper) DownloadMedia(_ context.Context, media *scraper.MediaItem) error {
	// Rate limit check
	if err := ss.waitForRateLimit(); err != nil {
		return err
	}

	log.Debug().
		Str("url", media.URL).
		Str("type", string(media.Type)).
		Msg("ScreenScraper DownloadMedia called")

	// ScreenScraper provides direct URLs in the GameInfo response.
	// The scraper service handles the actual downloading using httpclient.
	// This method exists for interface compliance but is not used in the normal flow.
	return errors.New("media download is handled by scraper service using URLs from GetGameInfo()")
}

// waitForRateLimit ensures we don't exceed rate limits
func (ss *ScreenScraper) waitForRateLimit() error {
	now := time.Now()
	timeSinceLastRequest := now.Sub(ss.lastRequest)

	if timeSinceLastRequest < ss.rateLimit {
		sleepTime := ss.rateLimit - timeSinceLastRequest
		log.Debug().Dur("sleep", sleepTime).Msg("Rate limiting ScreenScraper request")
		time.Sleep(sleepTime)
	}

	ss.lastRequest = time.Now()
	return nil
}

// buildSearchURL constructs the search URL for ScreenScraper API
func (*ScreenScraper) buildSearchURL(gameName, platformID string, hash *scraper.FileHash,
	region, language string,
) (string, error) {
	u, err := url.Parse(baseURL + "/jeuInfos.php")
	if err != nil {
		return "", fmt.Errorf("failed to parse ScreenScraper search URL: %w", err)
	}

	params := url.Values{}
	params.Set("output", "json")
	params.Set("softname", userAgent)
	params.Set("ssid", platformID)

	// Get authentication from auth.toml
	authCfg := config.GetAuthCfg()
	creds := config.LookupAuth(authCfg, "https://screenscraper.fr")
	if creds != nil && creds.Username != "" {
		params.Set("ssuser", creds.Username)
		params.Set("sspassword", creds.Password)
	}

	// ScreenScraper dev credentials (these are public dev credentials)
	params.Set("devid", "zaparoo")
	params.Set("devpassword", "zaparoo")

	// Prefer hash-based search if available
	if hash != nil {
		if hash.CRC32 != "" {
			params.Set("crc", strings.ToUpper(hash.CRC32))
		}
		if hash.MD5 != "" {
			params.Set("md5", strings.ToUpper(hash.MD5))
		}
		if hash.SHA1 != "" {
			params.Set("sha1", strings.ToUpper(hash.SHA1))
		}
		if hash.FileSize > 0 {
			params.Set("romsize", strconv.FormatInt(hash.FileSize, 10))
		}
	} else {
		// Fall back to name-based search
		params.Set("recherche", gameName)
	}

	// Set region and language preferences
	if region != "" {
		params.Set("region", region)
	}
	if language != "" {
		params.Set("langue", language)
	}

	u.RawQuery = params.Encode()
	return u.String(), nil
}

// buildGameInfoURL constructs the game info URL
func (*ScreenScraper) buildGameInfoURL(gameID string) (string, error) {
	u, err := url.Parse(baseURL + "/jeuInfos.php")
	if err != nil {
		return "", fmt.Errorf("failed to parse ScreenScraper game info URL: %w", err)
	}

	params := url.Values{}
	params.Set("output", "json")
	params.Set("softname", userAgent)
	params.Set("id", gameID)

	// Get authentication from auth.toml
	authCfg := config.GetAuthCfg()
	creds := config.LookupAuth(authCfg, "https://screenscraper.fr")
	if creds != nil && creds.Username != "" {
		params.Set("ssuser", creds.Username)
		params.Set("sspassword", creds.Password)
	}

	// ScreenScraper dev credentials (these are public dev credentials)
	params.Set("devid", "zaparoo")
	params.Set("devpassword", "zaparoo")

	u.RawQuery = params.Encode()
	return u.String(), nil
}

// handleHTTPError handles HTTP error responses
func (*ScreenScraper) handleHTTPError(statusCode int) error {
	switch statusCode {
	case http.StatusTooManyRequests:
		return errors.New("rate limited by ScreenScraper (429)")
	case http.StatusUnauthorized:
		return errors.New("authentication failed - check credentials in auth.toml")
	case http.StatusForbidden:
		return errors.New("access forbidden - check API permissions")
	case http.StatusNotFound:
		return errors.New("game not found")
	case http.StatusInternalServerError:
		return errors.New("ScreenScraper server error")
	default:
		return fmt.Errorf("HTTP error %d", statusCode)
	}
}

// convertGameToResult converts ScreenScraper game data to ScraperResult
func (ss *ScreenScraper) convertGameToResult(game *Game, systemID string) scraper.ScraperResult {
	// Calculate relevance score based on available data
	relevance := 0.5 // Base relevance
	if len(game.Names) > 0 {
		relevance += 0.2
	}
	if len(game.Descriptions) > 0 {
		relevance += 0.2
	}
	if len(game.Medias) > 0 {
		relevance += 0.1
	}

	return scraper.ScraperResult{
		ID:          strconv.Itoa(game.ID),
		Name:        ss.getPreferredText(game.Names, "us", "en"),
		Description: ss.getPreferredText(game.Descriptions, "us", "en"),
		SystemID:    systemID,
		Region:      "us", // Default, could be improved with region detection
		Language:    "en", // Default, could be improved with language detection
		Relevance:   relevance,
	}
}

// convertGameToInfo converts ScreenScraper game data to detailed GameInfo
func (ss *ScreenScraper) convertGameToInfo(game *Game) *scraper.GameInfo {
	gameInfo := &scraper.GameInfo{
		ID:          strconv.Itoa(game.ID),
		Name:        ss.getPreferredText(game.Names, "us", "en"),
		Description: ss.getPreferredText(game.Descriptions, "us", "en"),
		Genre:       ss.getPreferredText(game.Genres, "us", "en"),
		ReleaseDate: game.ReleaseDate,
		Developer:   game.Developer,
		Publisher:   game.Publisher,
		Players:     game.Players,
		Rating:      game.Rating,
		Region:      "us",
		Language:    "en",
		Media:       ss.convertMediaItems(game.Medias),
	}

	return gameInfo
}

// convertMediaItems converts ScreenScraper media to MediaItems
func (ss *ScreenScraper) convertMediaItems(medias []Media) []scraper.MediaItem {
	items := make([]scraper.MediaItem, 0, len(medias))

	for i := range medias {
		mediaType := ss.mapMediaType(medias[i].Type)
		if mediaType == "" {
			continue // Skip unknown media types
		}

		item := scraper.MediaItem{
			Type:        scraper.MediaType(mediaType),
			URL:         medias[i].URL,
			Width:       medias[i].Width,
			Height:      medias[i].Height,
			Size:        int64(medias[i].Size),
			Format:      medias[i].Format,
			Region:      medias[i].Region,
			Description: medias[i].Type,
		}

		items = append(items, item)
	}

	return items
}

// mapMediaType maps ScreenScraper media types to scraper media types
func (*ScreenScraper) mapMediaType(ssType string) string {
	// Based on ScreenScraper API documentation and Batocera implementation
	typeMap := map[string]string{
		"box-2D":   string(scraper.MediaTypeCover),
		"box-back": string(scraper.MediaTypeBoxBack),
		"ss":       string(scraper.MediaTypeScreenshot),
		"sstitle":  string(scraper.MediaTypeTitleShot),
		"fanart":   string(scraper.MediaTypeFanArt),
		"marquee":  string(scraper.MediaTypeMarquee),
		"wheel":    string(scraper.MediaTypeWheel),
		"video":    string(scraper.MediaTypeVideo),
		"manual":   string(scraper.MediaTypeManual),
		"bezel":    string(scraper.MediaTypeBezel),
		"map":      string(scraper.MediaTypeMap),
	}

	if mapped, ok := typeMap[ssType]; ok {
		return mapped
	}

	// Default fallback for unknown types
	if strings.Contains(ssType, "box") {
		return string(scraper.MediaTypeCover)
	}
	if strings.Contains(ssType, "screenshot") || strings.Contains(ssType, "ss") {
		return string(scraper.MediaTypeScreenshot)
	}

	return "" // Unknown type
}

// getPreferredText extracts text in preferred region/language
func (*ScreenScraper) getPreferredText(texts []Text, preferredRegion, preferredLanguage string) string {
	if len(texts) == 0 {
		return ""
	}

	// First try: exact region/language match
	for _, text := range texts {
		if text.Region == preferredRegion && text.Language == preferredLanguage {
			return text.Text
		}
	}

	// Second try: any text with preferred language
	for _, text := range texts {
		if text.Language == preferredLanguage {
			return text.Text
		}
	}

	// Third try: any text with preferred region
	for _, text := range texts {
		if text.Region == preferredRegion {
			return text.Text
		}
	}

	// Last resort: first available text
	if len(texts) > 0 {
		return texts[0].Text
	}
	return ""
}
