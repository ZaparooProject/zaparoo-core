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

package libreelec

var testMovies = []KodiItem{
	{MovieID: 1, Label: "The Matrix"},
	{MovieID: 2, Label: "Inception"},
	{MovieID: 3, Label: "The Dark Knight"},
}

var testTVShows = []KodiItem{
	{TVShowID: 1, Label: "Breaking Bad"},
	{TVShowID: 2, Label: "The Office"},
	{TVShowID: 3, Label: "Game of Thrones"},
}

var testEpisodes = map[int][]KodiItem{
	1: {
		{EpisodeID: 101, Label: "S01E01 - Pilot"},
		{EpisodeID: 102, Label: "S01E02 - Cat's in the Bag"},
		{EpisodeID: 103, Label: "S01E03 - ...And the Bag's in the River"},
	},
	2: {
		{EpisodeID: 201, Label: "S01E01 - Pilot"},
		{EpisodeID: 202, Label: "S01E02 - Diversity Day"},
		{EpisodeID: 203, Label: "S01E03 - Health Care"},
	},
	3: {
		{EpisodeID: 301, Label: "S01E01 - Winter Is Coming"},
		{EpisodeID: 302, Label: "S01E02 - The Kingsroad"},
	},
}

var testActivePlayers = []KodiPlayer{
	{Type: "video", ID: 1},
}

var testNoActivePlayers = []KodiPlayer{}
