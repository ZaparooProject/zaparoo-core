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

import "encoding/json"

// KodiClient defines the interface for Kodi API operations.
// This interface enables proper mocking and TDD for Kodi integration.
type KodiClient interface {
	// LaunchFile launches a local file or URL in Kodi
	LaunchFile(path string) error

	// LaunchMovie launches a movie by ID from Kodi's library
	// Path format: "kodi-movie://[id]/[name]"
	LaunchMovie(path string) error

	// LaunchTVEpisode launches a TV episode by ID from Kodi's library
	// Path format: "kodi-episode://[id]/[name]"
	LaunchTVEpisode(path string) error

	// Stop stops all active players in Kodi
	Stop() error

	// GetActivePlayers retrieves all active players in Kodi
	GetActivePlayers() ([]Player, error)

	// GetMovies retrieves all movies from Kodi's library
	GetMovies() ([]Movie, error)

	// GetTVShows retrieves all TV shows from Kodi's library
	GetTVShows() ([]TVShow, error)

	// GetEpisodes retrieves all episodes for a specific TV show from Kodi's library
	GetEpisodes(tvShowID int) ([]Episode, error)

	// GetURL returns the current Kodi API URL
	GetURL() string

	// SetURL sets the Kodi API URL
	SetURL(url string)

	// APIRequest makes a raw JSON-RPC request to Kodi API
	APIRequest(method APIMethod, params any) (json.RawMessage, error)
}
