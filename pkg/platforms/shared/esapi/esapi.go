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

package esapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/rs/zerolog/log"
)

const apiURL = "http://localhost:1234"

func APIRequest(path, body string, timeout time.Duration) ([]byte, error) {
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	client := &http.Client{}

	var req *http.Request
	var err error
	if body != "" {
		req, err = http.NewRequestWithContext(ctx, http.MethodPost, apiURL+path, bytes.NewBuffer([]byte(body)))
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}
	} else {
		req, err = http.NewRequestWithContext(ctx, http.MethodGet, apiURL+path, http.NoBody)
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	if resp == nil {
		return nil, errors.New("received nil response")
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			log.Error().Err(closeErr).Msg("error closing response body")
		}
	}()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	log.Debug().Msgf("response body %s: %s", path, string(respBody))

	return respBody, nil
}

func APIEmuKill() error {
	_, err := APIRequest("/emukill", "", 1*time.Second)
	if err != nil {
		return fmt.Errorf("failed to kill emulator: %w", err)
	}
	return nil
}

func APILaunch(path string) error {
	_, err := APIRequest("/launch", path, 0)
	if err != nil {
		return fmt.Errorf("failed to launch game at path %s: %w", path, err)
	}
	return nil
}

func APINotify(msg string) error {
	_, err := APIRequest("/notify", msg, 0)
	if err != nil {
		return fmt.Errorf("failed to send notification: %w", err)
	}
	return nil
}

const noGameRunning = "{\"msg\":\"NO GAME RUNNING\"}"

type RunningGameResponse struct {
	ID          string `json:"id"`
	Path        string `json:"path"`
	Name        string `json:"name"`
	SystemName  string `json:"systemName"`
	Desc        string `json:"desc"`
	Image       string `json:"image"`
	Video       string `json:"video"`
	Marquee     string `json:"marquee"`
	Thumbnail   string `json:"thumbnail"`
	Rating      string `json:"rating"`
	ReleaseDate string `json:"releaseDate"`
	Developer   string `json:"developer"`
	Genre       string `json:"genre"`
	Genres      string `json:"genres"`
	Players     string `json:"players"`
	Favorite    string `json:"favorite"`
	KidGame     string `json:"kidgame"`
	LastPlayed  string `json:"lastplayed"`
	CRC32       string `json:"crc32"`
	MD5         string `json:"md5"`
	GameTime    string `json:"gametime"`
	Lang        string `json:"lang"`
	CheevosHash string `json:"cheevosHash"`
}

func APIRunningGame() (RunningGameResponse, bool, error) {
	// for some reason this is more accurate if we do a fake request first
	_, _ = APIRequest("/runningGame", "", 500*time.Millisecond)

	resp, err := APIRequest("/runningGame", "", 1*time.Second)
	if err != nil {
		return RunningGameResponse{}, false, fmt.Errorf("failed to check running game: %w", err)
	}

	if string(resp) == noGameRunning {
		return RunningGameResponse{}, false, nil
	}

	var game RunningGameResponse
	err = json.Unmarshal(resp, &game)
	if err != nil {
		return RunningGameResponse{}, false, fmt.Errorf("failed to unmarshal running game response: %w", err)
	}

	return game, true, nil
}

// IsAvailable checks if the EmulationStation API server is running
func IsAvailable() bool {
	_, err := APIRequest("/runningGame", "", 1*time.Second)
	return err == nil
}
