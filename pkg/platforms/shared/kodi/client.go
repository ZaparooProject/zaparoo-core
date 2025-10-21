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

package kodi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/google/uuid"
)

// Client implements the KodiClient interface
type Client struct {
	url string
}

// Ensure Client implements KodiClient at compile time
var _ KodiClient = (*Client)(nil)

// NewClient creates a new Kodi client with configuration-based URL
func NewClient(cfg *config.Instance) KodiClient {
	return NewClientWithLauncherID(cfg, "Kodi")
}

// NewClientWithLauncherID creates a new Kodi client with hierarchical configuration lookup
func NewClientWithLauncherID(cfg *config.Instance, launcherID string) KodiClient {
	var serverURL string

	// Try specific launcher ID first, then fall back to generic "Kodi"
	if cfg != nil {
		if defaults, found := cfg.LookupLauncherDefaults(launcherID); found && defaults.ServerURL != "" {
			serverURL = defaults.ServerURL
		} else if defaults, found := cfg.LookupLauncherDefaults("Kodi"); found && defaults.ServerURL != "" {
			serverURL = defaults.ServerURL
		}
	}

	// Fall back to hardcoded localhost if no config found
	if serverURL == "" {
		serverURL = "http://localhost:8080"
	}

	// Ensure URL has a scheme
	if serverURL != "" && !strings.HasPrefix(serverURL, "http://") && !strings.HasPrefix(serverURL, "https://") {
		serverURL = "http://" + serverURL
	}

	// Handle trailing slashes and ensure /jsonrpc endpoint
	serverURL = strings.TrimSuffix(serverURL, "/")
	if !strings.HasSuffix(serverURL, "/jsonrpc") {
		serverURL += "/jsonrpc"
	}

	return &Client{url: serverURL}
}

// LaunchFile launches a local file or URL in Kodi
func (c *Client) LaunchFile(path string) error {
	_, err := c.APIRequest(context.Background(), APIMethodPlayerOpen, PlayerOpenParams{
		Item: Item{
			File: path,
		},
		Options: ItemOptions{
			Resume: true,
		},
	})
	return err
}

// LaunchMovie launches a movie by ID from Kodi's library
func (c *Client) LaunchMovie(path string) error {
	pathID := strings.TrimPrefix(path, SchemeKodiMovie+"://")
	pathID = strings.SplitN(pathID, "/", 2)[0]

	movieID, err := strconv.Atoi(pathID)
	if err != nil {
		return fmt.Errorf("failed to parse movie ID %q: %w", pathID, err)
	}

	_, err = c.APIRequest(context.Background(), APIMethodPlayerOpen, PlayerOpenParams{
		Item: Item{
			MovieID: movieID,
		},
		Options: ItemOptions{
			Resume: true,
		},
	})
	return err
}

// LaunchTVEpisode launches a TV episode by ID from Kodi's library
func (c *Client) LaunchTVEpisode(path string) error {
	if !strings.HasPrefix(path, SchemeKodiEpisode+"://") {
		return fmt.Errorf("invalid path: %s", path)
	}

	id := strings.TrimPrefix(path, SchemeKodiEpisode+"://")
	id = strings.SplitN(id, "/", 2)[0]

	intID, err := strconv.Atoi(id)
	if err != nil {
		return fmt.Errorf("failed to parse episode ID %q: %w", id, err)
	}

	_, err = c.APIRequest(context.Background(), APIMethodPlayerOpen, PlayerOpenParams{
		Item: Item{
			EpisodeID: intID,
		},
		Options: ItemOptions{
			Resume: true,
		},
	})
	return err
}

// Stop stops all active players in Kodi
func (c *Client) Stop() error {
	players, err := c.GetActivePlayers(context.Background())
	if err != nil {
		return err
	}

	for _, player := range players {
		_, err := c.APIRequest(context.Background(), APIMethodPlayerStop, PlayerStopParams{
			PlayerID: player.ID,
		})
		if err != nil {
			return err
		}
	}

	return nil
}

// GetActivePlayers retrieves all active players in Kodi
func (c *Client) GetActivePlayers(ctx context.Context) ([]Player, error) {
	result, err := c.APIRequest(ctx, APIMethodPlayerGetActivePlayers, nil)
	if err != nil {
		return nil, err
	}

	var players []Player
	err = json.Unmarshal(result, &players)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal GetActivePlayers response: %w", err)
	}

	return players, nil
}

// GetMovies retrieves all movies from Kodi's library
func (c *Client) GetMovies(ctx context.Context) ([]Movie, error) {
	result, err := c.APIRequest(ctx, APIMethodVideoLibraryGetMovies, nil)
	if err != nil {
		return nil, err
	}

	var response VideoLibraryGetMoviesResponse
	err = json.Unmarshal(result, &response)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal GetMovies response: %w", err)
	}

	return response.Movies, nil
}

// GetTVShows retrieves all TV shows from Kodi's library
func (c *Client) GetTVShows(ctx context.Context) ([]TVShow, error) {
	result, err := c.APIRequest(ctx, APIMethodVideoLibraryGetTVShows, nil)
	if err != nil {
		return nil, err
	}

	var response VideoLibraryGetTVShowsResponse
	err = json.Unmarshal(result, &response)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal GetTVShows response: %w", err)
	}

	return response.TVShows, nil
}

// GetEpisodes retrieves all episodes for a specific TV show from Kodi's library
func (c *Client) GetEpisodes(ctx context.Context, tvShowID int) ([]Episode, error) {
	params := VideoLibraryGetEpisodesParams{
		TVShowID: tvShowID,
	}

	result, err := c.APIRequest(ctx, APIMethodVideoLibraryGetEpisodes, params)
	if err != nil {
		return nil, err
	}

	var response VideoLibraryGetEpisodesResponse
	err = json.Unmarshal(result, &response)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal GetEpisodes response: %w", err)
	}

	return response.Episodes, nil
}

// GetSongs retrieves all songs from Kodi's library
func (c *Client) GetSongs(ctx context.Context) ([]Song, error) {
	result, err := c.APIRequest(ctx, APIMethodAudioLibraryGetSongs, nil)
	if err != nil {
		return nil, err
	}

	var response AudioLibraryGetSongsResponse
	err = json.Unmarshal(result, &response)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal GetSongs response: %w", err)
	}

	return response.Songs, nil
}

// GetAlbums retrieves all albums from Kodi's library
func (c *Client) GetAlbums(ctx context.Context) ([]Album, error) {
	result, err := c.APIRequest(ctx, APIMethodAudioLibraryGetAlbums, nil)
	if err != nil {
		return nil, err
	}

	var response AudioLibraryGetAlbumsResponse
	err = json.Unmarshal(result, &response)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal GetAlbums response: %w", err)
	}

	return response.Albums, nil
}

// GetArtists retrieves all artists from Kodi's library
func (c *Client) GetArtists(ctx context.Context) ([]Artist, error) {
	result, err := c.APIRequest(ctx, APIMethodAudioLibraryGetArtists, nil)
	if err != nil {
		return nil, err
	}

	var response AudioLibraryGetArtistsResponse
	err = json.Unmarshal(result, &response)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal GetArtists response: %w", err)
	}

	return response.Artists, nil
}

// LaunchSong launches a song by ID from Kodi's library
func (c *Client) LaunchSong(path string) error {
	pathID := strings.TrimPrefix(path, SchemeKodiSong+"://")
	pathID = strings.SplitN(pathID, "/", 2)[0]

	songID, err := strconv.Atoi(pathID)
	if err != nil {
		return fmt.Errorf("failed to parse song ID %q: %w", pathID, err)
	}

	_, err = c.APIRequest(context.Background(), APIMethodPlayerOpen, PlayerOpenParams{
		Item: Item{
			SongID: songID,
		},
		Options: ItemOptions{
			Resume: true,
		},
	})
	return err
}

// LaunchAlbum launches an album by ID using playlist generation
func (c *Client) LaunchAlbum(path string) error {
	pathID := strings.TrimPrefix(path, SchemeKodiAlbum+"://")
	pathID = strings.SplitN(pathID, "/", 2)[0]

	albumID, err := strconv.Atoi(pathID)
	if err != nil {
		return fmt.Errorf("failed to parse album ID %q: %w", pathID, err)
	}

	// Step 1: Clear music playlist
	_, err = c.APIRequest(context.Background(), APIMethodPlaylistClear, PlaylistClearParams{
		PlaylistID: 0,
	})
	if err != nil {
		return err
	}

	// Step 2: Get songs with album filter
	filter := &FilterRule{
		Field:    "albumid",
		Operator: "is",
		Value:    albumID,
	}
	params := AudioLibraryGetSongsParams{Filter: filter}

	result, err := c.APIRequest(context.Background(), APIMethodAudioLibraryGetSongs, params)
	if err != nil {
		return err
	}

	var response AudioLibraryGetSongsResponse
	err = json.Unmarshal(result, &response)
	if err != nil {
		return fmt.Errorf("failed to unmarshal GetSongs response: %w", err)
	}

	allSongs := response.Songs

	// Convert to playlist items - no filtering needed since API filtered
	albumSongs := make([]PlaylistItemSongID, 0, len(allSongs))
	for _, song := range allSongs {
		albumSongs = append(albumSongs, PlaylistItemSongID{SongID: song.ID})
	}

	// Step 3: Add to playlist
	_, err = c.APIRequest(context.Background(), APIMethodPlaylistAdd, PlaylistAddParams{
		PlaylistID: 0,
		Item:       albumSongs,
	})
	if err != nil {
		return err
	}

	// Step 4: Start playback
	_, err = c.APIRequest(context.Background(), APIMethodPlayerOpen, PlayerOpenParams{
		Item: Item{
			PlaylistID: 0,
		},
	})
	return err
}

// LaunchArtist launches an artist by ID using playlist generation
func (c *Client) LaunchArtist(path string) error {
	pathID := strings.TrimPrefix(path, SchemeKodiArtist+"://")
	pathID = strings.SplitN(pathID, "/", 2)[0]

	artistID, err := strconv.Atoi(pathID)
	if err != nil {
		return fmt.Errorf("failed to parse artist ID %q: %w", pathID, err)
	}

	// Step 1: Clear music playlist
	_, err = c.APIRequest(context.Background(), APIMethodPlaylistClear, PlaylistClearParams{
		PlaylistID: 0,
	})
	if err != nil {
		return err
	}

	// Step 2: Get songs for specific artist using API filtering
	filter := &FilterRule{
		Field:    "artistid",
		Operator: "is",
		Value:    artistID,
	}
	params := AudioLibraryGetSongsParams{Filter: filter}

	result, err := c.APIRequest(context.Background(), APIMethodAudioLibraryGetSongs, params)
	if err != nil {
		return err
	}

	var response AudioLibraryGetSongsResponse
	err = json.Unmarshal(result, &response)
	if err != nil {
		return fmt.Errorf("failed to unmarshal GetSongs response: %w", err)
	}

	allSongs := response.Songs

	// Convert to playlist items - no filtering needed since API filtered
	artistSongs := make([]PlaylistItemSongID, 0, len(allSongs))
	for _, song := range allSongs {
		artistSongs = append(artistSongs, PlaylistItemSongID{SongID: song.ID})
	}

	if len(artistSongs) == 0 {
		return fmt.Errorf("no songs found for artist ID %d", artistID)
	}

	// Step 3: Add songs to playlist
	_, err = c.APIRequest(context.Background(), APIMethodPlaylistAdd, PlaylistAddParams{
		PlaylistID: 0,
		Item:       artistSongs,
	})
	if err != nil {
		return err
	}

	// Step 4: Start playback
	_, err = c.APIRequest(context.Background(), APIMethodPlayerOpen, PlayerOpenParams{
		Item: Item{
			PlaylistID: 0,
		},
	})
	return err
}

// LaunchTVShow launches a TV show by ID using playlist generation
func (c *Client) LaunchTVShow(path string) error {
	// Parse show ID
	pathID := strings.TrimPrefix(path, SchemeKodiShow+"://")
	pathID = strings.SplitN(pathID, "/", 2)[0]

	showID, err := strconv.Atoi(pathID)
	if err != nil {
		return fmt.Errorf("failed to parse show ID %q: %w", pathID, err)
	}

	// Step 1: Clear video playlist (playlistid=1)
	_, err = c.APIRequest(context.Background(), APIMethodPlaylistClear, PlaylistClearParams{
		PlaylistID: 1,
	})
	if err != nil {
		return err
	}

	// Step 2: Get episodes for the show
	episodes, err := c.GetEpisodes(context.Background(), showID)
	if err != nil {
		return err
	}

	if len(episodes) == 0 {
		return fmt.Errorf("no episodes found for show ID %d", showID)
	}

	// Step 3: Add episodes to playlist
	episodeItems := make([]PlaylistItemEpisodeID, 0, len(episodes))
	for _, episode := range episodes {
		episodeItems = append(episodeItems, PlaylistItemEpisodeID{EpisodeID: episode.ID})
	}

	_, err = c.APIRequest(context.Background(), APIMethodPlaylistAdd, PlaylistAddEpisodesParams{
		PlaylistID: 1,
		Item:       episodeItems,
	})
	if err != nil {
		return err
	}

	// Step 4: Start playback
	_, err = c.APIRequest(context.Background(), APIMethodPlayerOpen, PlayerOpenParams{
		Item: Item{
			PlaylistID: 1,
		},
	})
	return err
}

// GetURL returns the current Kodi API URL
func (c *Client) GetURL() string {
	return c.url
}

// SetURL sets the Kodi API URL
func (c *Client) SetURL(url string) {
	c.url = url
}

// APIRequest makes a raw JSON-RPC request to Kodi API
func (c *Client) APIRequest(ctx context.Context, method APIMethod, params any) (json.RawMessage, error) {
	req := APIPayload{
		JSONRPC: "2.0",
		ID:      uuid.New().String(),
		Method:  method,
		Params:  params,
	}

	reqJSON, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Combine cancellation with timeout - whichever comes first wins
	reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	kodiReq, err := http.NewRequestWithContext(reqCtx, http.MethodPost, c.url, bytes.NewBuffer(reqJSON))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	kodiReq.Header.Set("Content-Type", "application/json")
	kodiReq.Header.Set("Accept", "application/json")

	// Add authentication if configured
	authCfg := config.GetAuthCfg()
	if cred := config.LookupAuth(authCfg, c.url); cred != nil {
		if cred.Bearer != "" {
			kodiReq.Header.Set("Authorization", "Bearer "+cred.Bearer)
		} else if cred.Username != "" && cred.Password != "" {
			auth := base64.StdEncoding.EncodeToString([]byte(cred.Username + ":" + cred.Password))
			kodiReq.Header.Set("Authorization", "Basic "+auth)
		}
	}

	client := &http.Client{}
	resp, err := client.Do(kodiReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer func() {
		_ = resp.Body.Close() // Ignore close error in defer
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var apiResp APIResponse
	err = json.Unmarshal(body, &apiResp)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}
	if apiResp.Error != nil {
		return nil, fmt.Errorf("error from kodi api: %s", apiResp.Error.Message)
	}

	return apiResp.Result, nil
}
