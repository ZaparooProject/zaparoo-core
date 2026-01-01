// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
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

package fixtures

import "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/kodi"

// TestMovies provides sample movie data for Kodi testing
var TestMovies = []kodi.Movie{
	{ID: 1, Label: "The Matrix"},
	{ID: 2, Label: "Inception"},
	{ID: 3, Label: "The Dark Knight"},
}

// TestTVShows provides sample TV show data for Kodi testing
var TestTVShows = []kodi.TVShow{
	{ID: 1, Label: "Breaking Bad"},
	{ID: 2, Label: "The Office"},
	{ID: 3, Label: "Game of Thrones"},
}

// TestEpisodes provides sample episode data for Kodi testing, keyed by TV show ID
var TestEpisodes = map[int][]kodi.Episode{
	1: {
		{ID: 101, Label: "S01E01 - Pilot", Season: 1, Episode: 1, TVShowID: 1},
		{ID: 102, Label: "S01E02 - Cat's in the Bag", Season: 1, Episode: 2, TVShowID: 1},
		{ID: 103, Label: "S01E03 - ...And the Bag's in the River", Season: 1, Episode: 3, TVShowID: 1},
	},
	2: {
		{ID: 201, Label: "S01E01 - Pilot", Season: 1, Episode: 1, TVShowID: 2},
		{ID: 202, Label: "S01E02 - Diversity Day", Season: 1, Episode: 2, TVShowID: 2},
		{ID: 203, Label: "S01E03 - Health Care", Season: 1, Episode: 3, TVShowID: 2},
	},
	3: {
		{ID: 301, Label: "S01E01 - Winter Is Coming", Season: 1, Episode: 1, TVShowID: 3},
		{ID: 302, Label: "S01E02 - The Kingsroad", Season: 1, Episode: 2, TVShowID: 3},
	},
}

// TestActivePlayers provides sample active player data for Kodi testing
var TestActivePlayers = []kodi.Player{
	{Type: "video", ID: 1},
}

// TestNoActivePlayers provides empty player data for Kodi testing
var TestNoActivePlayers = []kodi.Player{}
