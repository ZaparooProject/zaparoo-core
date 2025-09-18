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

package igdb

import (
	"bytes"
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
	baseURL   = "https://api.igdb.com/v4"
	tokenURL  = "https://id.twitch.tv/oauth2/token" // #nosec G101 - Public OAuth endpoint URL, not a credential
	userAgent = "Zaparoo Core/1.0"

	// IGDB API limits
	defaultRateLimit = 250 // milliseconds between requests (4 requests per second)
	maxRetries       = 3
)

// IGDB implements the Scraper interface for IGDB API
type IGDB struct {
	lastRequest time.Time
	client      *httpclient.Client
	platformMap *PlatformMapper
	tokenInfo   *TokenInfo
	rateLimit   time.Duration
}

// NewIGDB creates a new IGDB instance
func NewIGDB() *IGDB {
	return &IGDB{
		client:      httpclient.NewClientWithTimeout(30 * time.Second),
		platformMap: NewPlatformMapper(),
		rateLimit:   time.Duration(defaultRateLimit) * time.Millisecond,
	}
}

// GetInfo returns scraper information
func (*IGDB) GetInfo() scraper.ScraperInfo {
	return scraper.ScraperInfo{
		Name:         "IGDB",
		Version:      "1.0",
		Description:  "IGDB.com API integration with advanced query capabilities",
		Website:      "https://igdb.com",
		RequiresAuth: true, // Requires Twitch client credentials
	}
}

// IsSupportedPlatform checks if the given system ID is supported
func (igdb *IGDB) IsSupportedPlatform(systemID string) bool {
	_, supported := igdb.platformMap.MapToScraperPlatform(systemID)
	return supported
}

// GetSupportedMediaTypes returns the media types supported by IGDB
func (*IGDB) GetSupportedMediaTypes() []scraper.MediaType {
	return []scraper.MediaType{
		scraper.MediaTypeCover,
		scraper.MediaTypeScreenshot,
		scraper.MediaTypeFanArt,
		scraper.MediaTypeVideo,
	}
}

// Search searches for games matching the query
func (igdb *IGDB) Search(ctx context.Context, query scraper.ScraperQuery) ([]scraper.ScraperResult, error) {
	// Rate limit check
	if err := igdb.waitForRateLimit(); err != nil {
		return nil, err
	}

	// Ensure we have a valid token
	if err := igdb.ensureValidToken(ctx); err != nil {
		return nil, fmt.Errorf("failed to get valid token: %w", err)
	}

	// Map system ID to IGDB platform
	platformID, ok := igdb.platformMap.MapToScraperPlatform(query.SystemID)
	if !ok {
		return nil, fmt.Errorf("unsupported platform: %s", query.SystemID)
	}

	// Build IGDB query
	igdbQuery := igdb.buildSearchQuery(query.Name, platformID)

	log.Debug().Str("query", igdbQuery).Msg("IGDB search request")

	// Make the request
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/games", bytes.NewBufferString(igdbQuery))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Client-ID", igdb.getClientID())
	req.Header.Set("Authorization", "Bearer "+igdb.tokenInfo.AccessToken)
	req.Header.Set("Content-Type", "text/plain")

	resp, err := igdb.client.Do(req)
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
		return nil, igdb.handleHTTPError(resp.StatusCode)
	}

	// Parse response
	var games []Game
	if err := json.NewDecoder(resp.Body).Decode(&games); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if len(games) == 0 {
		return nil, nil // No results found
	}

	// Convert to scraper results
	results := make([]scraper.ScraperResult, 0, len(games))
	for i := range games {
		result := igdb.convertGameToResult(&games[i], query.SystemID)
		results = append(results, result)
	}

	return results, nil
}

// GetGameInfo gets detailed information about a specific game
func (igdb *IGDB) GetGameInfo(ctx context.Context, gameID string) (*scraper.GameInfo, error) {
	// Rate limit check
	if err := igdb.waitForRateLimit(); err != nil {
		return nil, err
	}

	// Ensure we have a valid token
	if err := igdb.ensureValidToken(ctx); err != nil {
		return nil, fmt.Errorf("failed to get valid token: %w", err)
	}

	// Build IGDB query for detailed game info
	igdbQuery := igdb.buildGameInfoQuery(gameID)

	log.Debug().Str("query", igdbQuery).Msg("IGDB game info request")

	// Make the request
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/games", bytes.NewBufferString(igdbQuery))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Client-ID", igdb.getClientID())
	req.Header.Set("Authorization", "Bearer "+igdb.tokenInfo.AccessToken)
	req.Header.Set("Content-Type", "text/plain")

	resp, err := igdb.client.Do(req)
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
		return nil, igdb.handleHTTPError(resp.StatusCode)
	}

	// Parse response
	var games []Game
	if decodeErr := json.NewDecoder(resp.Body).Decode(&games); decodeErr != nil {
		return nil, fmt.Errorf("failed to decode response: %w", decodeErr)
	}

	if len(games) == 0 {
		return nil, errors.New("game not found")
	}

	// Convert to game info (get additional data in parallel)
	game := games[0]
	gameInfo, err := igdb.convertGameToInfo(ctx, &game)
	if err != nil {
		return nil, fmt.Errorf("failed to convert game info: %w", err)
	}

	return gameInfo, nil
}

// DownloadMedia downloads media files for a game
func (igdb *IGDB) DownloadMedia(_ context.Context, media *scraper.MediaItem) error {
	// Rate limit check
	if err := igdb.waitForRateLimit(); err != nil {
		return err
	}

	log.Debug().
		Str("url", media.URL).
		Str("type", string(media.Type)).
		Msg("IGDB downloading media")

	// IGDB provides direct URLs, so we can download directly
	// This would be used by the scraper service to download to the appropriate path
	return errors.New("media download should be handled by scraper service")
}

// ensureValidToken ensures we have a valid OAuth2 token
func (igdb *IGDB) ensureValidToken(ctx context.Context) error {
	// Check if we have a valid token
	if igdb.tokenInfo != nil && time.Now().Before(igdb.tokenInfo.ExpiresAt) {
		return nil // Token is still valid
	}

	// Get credentials from auth.toml
	authCfg := config.GetAuthCfg()
	creds := config.LookupAuth(authCfg, "https://api.igdb.com")
	if creds == nil || creds.Username == "" || creds.Password == "" {
		return errors.New("IGDB requires Twitch client credentials in auth.toml - " +
			"set username=client_id and password=client_secret for https://api.igdb.com")
	}

	// Request new token from Twitch
	params := url.Values{}
	params.Set("client_id", creds.Username)
	params.Set("client_secret", creds.Password)
	params.Set("grant_type", "client_credentials")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(params.Encode()))
	if err != nil {
		return fmt.Errorf("failed to create token request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := igdb.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to request token: %w", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("Failed to close response body")
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("token request failed with status %d", resp.StatusCode)
	}

	var tokenResp TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return fmt.Errorf("failed to decode token response: %w", err)
	}

	// Store token info
	igdb.tokenInfo = &TokenInfo{
		AccessToken: tokenResp.AccessToken,
		ExpiresAt:   time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second),
		TokenType:   tokenResp.TokenType,
	}

	log.Info().Msg("Successfully obtained IGDB access token")
	return nil
}

// getClientID gets the Twitch client ID from auth config
func (*IGDB) getClientID() string {
	authCfg := config.GetAuthCfg()
	creds := config.LookupAuth(authCfg, "https://api.igdb.com")
	if creds != nil {
		return creds.Username // client_id is stored as username
	}
	return ""
}

// waitForRateLimit ensures we don't exceed rate limits
func (igdb *IGDB) waitForRateLimit() error {
	now := time.Now()
	timeSinceLastRequest := now.Sub(igdb.lastRequest)

	if timeSinceLastRequest < igdb.rateLimit {
		sleepTime := igdb.rateLimit - timeSinceLastRequest
		log.Debug().Dur("sleep", sleepTime).Msg("Rate limiting IGDB request")
		time.Sleep(sleepTime)
	}

	igdb.lastRequest = time.Now()
	return nil
}

// buildSearchQuery builds an IGDB query for searching games
func (*IGDB) buildSearchQuery(gameName, platformID string) string {
	// IGDB uses a custom query language
	return fmt.Sprintf(`fields id,name,summary,first_release_date,rating,platforms,genres,cover.image_id;
		search "%s";
		where platforms = (%s) & category = 0;
		limit 10;`, gameName, platformID)
}

// buildGameInfoQuery builds an IGDB query for detailed game information
func (*IGDB) buildGameInfoQuery(gameID string) string {
	return fmt.Sprintf(`fields id,name,summary,storyline,first_release_date,rating,aggregated_rating,platforms,genres,`+
		`involved_companies,cover.image_id,screenshots.image_id,artworks.image_id,videos.video_id,videos.name,`+
		`alternative_names.name; where id = %s;`, gameID)
}

// handleHTTPError handles HTTP error responses
func (*IGDB) handleHTTPError(statusCode int) error {
	switch statusCode {
	case http.StatusTooManyRequests:
		return errors.New("rate limited by IGDB (429)")
	case http.StatusUnauthorized:
		return errors.New("authentication failed - check Twitch credentials in auth.toml")
	case http.StatusForbidden:
		return errors.New("access forbidden - check API permissions")
	case http.StatusNotFound:
		return errors.New("game not found")
	case http.StatusInternalServerError:
		return errors.New("IGDB server error")
	default:
		return fmt.Errorf("HTTP error %d", statusCode)
	}
}

// convertGameToResult converts IGDB game data to ScraperResult
func (*IGDB) convertGameToResult(game *Game, systemID string) scraper.ScraperResult {
	// Calculate relevance score based on available data
	relevance := 0.6 // Base relevance (IGDB typically has good data)
	if game.Name != "" {
		relevance += 0.2
	}
	if game.Summary != "" {
		relevance += 0.1
	}
	if game.Rating > 0 {
		relevance += 0.1
	}

	return scraper.ScraperResult{
		ID:          strconv.Itoa(game.ID),
		Name:        game.Name,
		Description: game.Summary,
		SystemID:    systemID,
		Region:      "global", // IGDB is global
		Language:    "en",     // IGDB is primarily English
		Relevance:   relevance,
	}
}

// convertGameToInfo converts IGDB game data to detailed GameInfo
func (*IGDB) convertGameToInfo(_ context.Context, game *Game) (*scraper.GameInfo, error) {
	gameInfo := &scraper.GameInfo{
		ID:          strconv.Itoa(game.ID),
		Name:        game.Name,
		Description: game.Summary,
		Rating:      game.Rating / 20, // IGDB uses 0-100, convert to 0-5
		Region:      "global",
		Language:    "en",
	}

	// Convert release date
	if game.FirstReleaseDate > 0 {
		releaseTime := time.Unix(game.FirstReleaseDate, 0)
		gameInfo.ReleaseDate = releaseTime.Format("2006-01-02")
	}

	// Use storyline if available (more detailed than summary)
	if game.Storyline != "" {
		gameInfo.Description = game.Storyline
	}

	// Convert cover and media
	mediaItems := make([]scraper.MediaItem, 0, 10)

	// Add cover art
	if game.Cover != nil {
		coverURL := fmt.Sprintf("https://images.igdb.com/igdb/image/upload/t_cover_big/%s.jpg", game.Cover.ImageID)
		mediaItems = append(mediaItems, scraper.MediaItem{
			Type:   scraper.MediaTypeCover,
			URL:    coverURL,
			Width:  game.Cover.Width,
			Height: game.Cover.Height,
			Format: "jpg",
		})
	}

	// Add screenshots
	for _, screenshot := range game.Screenshots {
		screenshotURL := fmt.Sprintf("https://images.igdb.com/igdb/image/upload/t_screenshot_big/%s.jpg",
			screenshot.ImageID)
		mediaItems = append(mediaItems, scraper.MediaItem{
			Type:   scraper.MediaTypeScreenshot,
			URL:    screenshotURL,
			Width:  screenshot.Width,
			Height: screenshot.Height,
			Format: "jpg",
		})
	}

	// Add artworks
	for _, artwork := range game.Artworks {
		artworkURL := fmt.Sprintf("https://images.igdb.com/igdb/image/upload/t_1080p/%s.jpg", artwork.ImageID)
		mediaItems = append(mediaItems, scraper.MediaItem{
			Type:   scraper.MediaTypeFanArt,
			URL:    artworkURL,
			Width:  artwork.Width,
			Height: artwork.Height,
			Format: "jpg",
		})
	}

	// Add videos
	for _, video := range game.Videos {
		if video.VideoID != "" {
			videoURL := fmt.Sprintf("https://www.youtube.com/watch?v=%s", video.VideoID)
			mediaItems = append(mediaItems, scraper.MediaItem{
				Type:        scraper.MediaTypeVideo,
				URL:         videoURL,
				Format:      "mp4",
				Description: video.Name,
			})
		}
	}

	gameInfo.Media = mediaItems

	// Note: Getting genre, developer, publisher info would require additional API calls
	// For now, we'll leave these empty to keep the implementation simple
	// In a full implementation, you'd make parallel requests to get this data

	return gameInfo, nil
}
