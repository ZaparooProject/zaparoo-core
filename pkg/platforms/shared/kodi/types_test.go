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

package kodi_test

import (
	"testing"

	"github.com/ZaparooProject/zaparoo-core/pkg/platforms/shared/kodi"
	"github.com/stretchr/testify/assert"
)

func TestTypes_CanBeCreated(t *testing.T) {
	t.Parallel()

	// This test drives the creation of basic Kodi types that we need

	// Test Player type
	player := kodi.Player{
		ID:   1,
		Type: "video",
	}
	assert.Equal(t, 1, player.ID)
	assert.Equal(t, "video", player.Type)

	// Test Movie type
	movie := kodi.Movie{
		ID:    123,
		Label: "Test Movie",
		File:  "/path/to/movie.mp4",
	}
	assert.Equal(t, 123, movie.ID)
	assert.Equal(t, "Test Movie", movie.Label)
	assert.Equal(t, "/path/to/movie.mp4", movie.File)

	// Test TVShow type
	show := kodi.TVShow{
		ID:    456,
		Label: "Test Show",
	}
	assert.Equal(t, 456, show.ID)
	assert.Equal(t, "Test Show", show.Label)

	// Test Episode type
	episode := kodi.Episode{
		ID:       789,
		TVShowID: 456,
		Label:    "Test Episode",
		File:     "/path/to/episode.mp4",
	}
	assert.Equal(t, 789, episode.ID)
	assert.Equal(t, 456, episode.TVShowID)
	assert.Equal(t, "Test Episode", episode.Label)
	assert.Equal(t, "/path/to/episode.mp4", episode.File)
}
