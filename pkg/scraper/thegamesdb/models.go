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

import "time"

// APIResponse represents the root response structure from TheGamesDB API
type APIResponse struct {
	Data              *APIResponseData    `json:"data,omitempty"`
	Include           *APIResponseInclude `json:"include,omitempty"`
	Status            string              `json:"status"`
	AllowanceRefresh  string              `json:"allowance_refresh_timer"`
	Code              int                 `json:"code"`
	RemainingRequests int                 `json:"remaining_monthly_allowance"`
	ExtraAllowance    int                 `json:"extra_allowance"`
}

// APIResponseData contains the main data from API responses
type APIResponseData struct {
	Games  []Game `json:"games,omitempty"`
	Count  int    `json:"count,omitempty"`
	Offset int    `json:"offset,omitempty"`
}

// APIResponseInclude contains additional data referenced by IDs
type APIResponseInclude struct {
	Boxart     map[string]Boxart    `json:"boxart,omitempty"`
	Platforms  map[string]Platform  `json:"platforms,omitempty"`
	Genres     map[string]Genre     `json:"genres,omitempty"`
	Developers map[string]Developer `json:"developers,omitempty"`
	Publishers map[string]Publisher `json:"publishers,omitempty"`
}

// Game represents a game from TheGamesDB
type Game struct {
	LastUpdated    time.Time `json:"last_updated"`
	Youtube        string    `json:"youtube"`
	ReleaseDate    string    `json:"release_date"`
	Overview       string    `json:"overview"`
	GameTitle      string    `json:"game_title"`
	Rating         string    `json:"rating"`
	CoopPlay       string    `json:"coop"`
	AlternateNames []string  `json:"alternates,omitempty"`
	Genres         []int     `json:"genres,omitempty"`
	Developers     []int     `json:"developers,omitempty"`
	Publishers     []int     `json:"publishers,omitempty"`
	Platform       int       `json:"platform"`
	Players        int       `json:"players"`
	ID             int       `json:"id"`
}

// Boxart represents boxart/cover art information
type Boxart struct {
	Type       string `json:"type"`
	Side       string `json:"side"`
	Filename   string `json:"filename"`
	Resolution string `json:"resolution"`
	ID         int    `json:"id"`
}

// Platform represents a gaming platform
type Platform struct {
	Name     string `json:"name"`
	Alias    string `json:"alias"`
	Overview string `json:"overview"`
	ID       int    `json:"id"`
}

// Genre represents a game genre
type Genre struct {
	Name string `json:"name"`
	ID   int    `json:"id"`
}

// Developer represents a game developer
type Developer struct {
	Name string `json:"name"`
	ID   int    `json:"id"`
}

// Publisher represents a game publisher
type Publisher struct {
	Name string `json:"name"`
	ID   int    `json:"id"`
}
