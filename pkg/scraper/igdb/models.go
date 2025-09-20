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

import "time"

// Game represents a game from IGDB API
type Game struct {
	Cover              *Cover            `json:"cover"`
	URL                string            `json:"url"`
	Name               string            `json:"name"`
	Summary            string            `json:"summary"`
	Storyline          string            `json:"storyline"`
	PlayerPerspectives []int             `json:"playerPerspectives"`
	Artworks           []Artwork         `json:"artworks"`
	AlternativeNames   []AlternativeName `json:"alternativeNames"`
	Videos             []Video           `json:"videos"`
	Screenshots        []Screenshot      `json:"screenshots"`
	Themes             []int             `json:"themes"`
	Platforms          []int             `json:"platforms"`
	Genres             []int             `json:"genres"`
	InvolvedCompanies  []int             `json:"involvedCompanies"`
	GameModes          []int             `json:"gameModes"`
	ID                 int               `json:"id"`
	Rating             float64           `json:"rating"`
	RatingCount        int               `json:"ratingCount"`
	AggregatedRating   float64           `json:"aggregatedRating"`
	FirstReleaseDate   int64             `json:"firstReleaseDate"`
	Status             int               `json:"status"`
	Category           int               `json:"category"`
}

// Screenshot represents a screenshot from IGDB
type Screenshot struct {
	ImageID  string `json:"imageId"`
	URL      string `json:"url"`
	Checksum string `json:"checksum"`
	ID       int    `json:"id"`
	Game     int    `json:"game"`
	Height   int    `json:"height"`
	Width    int    `json:"width"`
}

// Artwork represents artwork from IGDB
type Artwork struct {
	ImageID  string `json:"imageId"`
	URL      string `json:"url"`
	Checksum string `json:"checksum"`
	ID       int    `json:"id"`
	Game     int    `json:"game"`
	Height   int    `json:"height"`
	Width    int    `json:"width"`
}

// Cover represents cover art from IGDB
type Cover struct {
	ImageID  string `json:"imageId"`
	URL      string `json:"url"`
	Checksum string `json:"checksum"`
	ID       int    `json:"id"`
	Game     int    `json:"game"`
	Height   int    `json:"height"`
	Width    int    `json:"width"`
}

// Video represents a video from IGDB
type Video struct {
	Name     string `json:"name"`
	VideoID  string `json:"videoId"`
	Checksum string `json:"checksum"`
	ID       int    `json:"id"`
	Game     int    `json:"game"`
}

// AlternativeName represents an alternative name for a game
type AlternativeName struct {
	Name    string `json:"name"`
	Comment string `json:"comment"`
	ID      int    `json:"id"`
	Game    int    `json:"game"`
}

// Platform represents a gaming platform from IGDB
type Platform struct {
	Name         string `json:"name"`
	Abbreviation string `json:"abbreviation"`
	Alternative  string `json:"alternativeName"`
	Summary      string `json:"summary"`
	URL          string `json:"url"`
	Checksum     string `json:"checksum"`
	ID           int    `json:"id"`
	Category     int    `json:"category"`
}

// Genre represents a game genre from IGDB
type Genre struct {
	Name     string `json:"name"`
	URL      string `json:"url"`
	Checksum string `json:"checksum"`
	ID       int    `json:"id"`
}

// InvolvedCompany represents a company involved in game development
type InvolvedCompany struct {
	ID         int  `json:"id"`
	Company    int  `json:"company"`
	Game       int  `json:"game"`
	Developer  bool `json:"developer"`
	Publisher  bool `json:"publisher"`
	Porting    bool `json:"porting"`
	Supporting bool `json:"supporting"`
}

// Company represents a game company from IGDB
type Company struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	URL         string `json:"url"`
	Checksum    string `json:"checksum"`
	ID          int    `json:"id"`
	Country     int    `json:"country"`
}

// GameMode represents a game mode from IGDB
type GameMode struct {
	Name     string `json:"name"`
	URL      string `json:"url"`
	Checksum string `json:"checksum"`
	ID       int    `json:"id"`
}

// PlayerPerspective represents a player perspective from IGDB
type PlayerPerspective struct {
	Name     string `json:"name"`
	URL      string `json:"url"`
	Checksum string `json:"checksum"`
	ID       int    `json:"id"`
}

// Theme represents a game theme from IGDB
type Theme struct {
	Name     string `json:"name"`
	URL      string `json:"url"`
	Checksum string `json:"checksum"`
	ID       int    `json:"id"`
}

// IGDBError represents an error response from IGDB API
type IGDBError struct {
	Type   string `json:"type"`
	Title  string `json:"title"`
	Cause  string `json:"cause"`
	Status int    `json:"status"`
}

// TokenResponse represents the OAuth2 token response from Twitch
type TokenResponse struct {
	AccessToken string `json:"accessToken"`
	TokenType   string `json:"tokenType"`
	ExpiresIn   int    `json:"expiresIn"`
}

// TokenInfo stores token information for IGDB authentication
type TokenInfo struct {
	AccessToken string
	ExpiresAt   time.Time
	TokenType   string
}
