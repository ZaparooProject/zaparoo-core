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
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/pkg/config"
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
	// TODO: Implement proper config-based URL resolution
	// For now, use hardcoded default
	url := "http://localhost:8080/jsonrpc"
	return &Client{url: url}
}

// LaunchFile launches a local file or URL in Kodi
func (c *Client) LaunchFile(path string) error {
	_, err := c.APIRequest(APIMethodPlayerOpen, PlayerOpenParams{
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

	_, err = c.APIRequest(APIMethodPlayerOpen, PlayerOpenParams{
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

	_, err = c.APIRequest(APIMethodPlayerOpen, PlayerOpenParams{
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
	players, err := c.GetActivePlayers()
	if err != nil {
		return err
	}

	for _, player := range players {
		_, err := c.APIRequest(APIMethodPlayerStop, PlayerStopParams{
			PlayerID: player.ID,
		})
		if err != nil {
			return err
		}
	}

	return nil
}

// GetActivePlayers retrieves all active players in Kodi
func (c *Client) GetActivePlayers() ([]Player, error) {
	result, err := c.APIRequest(APIMethodPlayerGetActivePlayers, nil)
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
func (c *Client) GetMovies() ([]Movie, error) {
	result, err := c.APIRequest(APIMethodVideoLibraryGetMovies, nil)
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
func (c *Client) GetTVShows() ([]TVShow, error) {
	result, err := c.APIRequest(APIMethodVideoLibraryGetTVShows, nil)
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
func (c *Client) GetEpisodes(tvShowID int) ([]Episode, error) {
	params := VideoLibraryGetEpisodesParams{
		TVShowID: tvShowID,
	}

	result, err := c.APIRequest(APIMethodVideoLibraryGetEpisodes, params)
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

// GetURL returns the current Kodi API URL
func (c *Client) GetURL() string {
	return c.url
}

// SetURL sets the Kodi API URL
func (c *Client) SetURL(url string) {
	c.url = url
}

// APIRequest makes a raw JSON-RPC request to Kodi API
func (c *Client) APIRequest(method APIMethod, params any) (json.RawMessage, error) {
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

	kodiReq, err := http.NewRequest(http.MethodPost, c.url, bytes.NewBuffer(reqJSON))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	kodiReq.Header.Set("Content-Type", "application/json")
	kodiReq.Header.Set("Accept", "application/json")

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
