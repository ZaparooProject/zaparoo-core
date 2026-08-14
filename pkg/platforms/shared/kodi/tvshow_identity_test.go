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

package kodi

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveTVShowID_UsesProviderIdentityAfterKodiIDChanges(t *testing.T) {
	t.Parallel()

	reference, err := parseTVShowReference(
		"kodi-show://106/The%20Office?tvdb=73244&tmdb=2316&imdb=tt0386676",
	)
	require.NoError(t, err)

	shows := []TVShow{
		{
			ID:    106,
			Label: "One-Punch Man",
			UniqueIDs: map[string]string{
				"tvdb": "293088",
			},
		},
		{
			ID:    205,
			Label: "The Office",
			UniqueIDs: map[string]string{
				"tvdb": "73244",
				"tmdb": "2316",
				"imdb": "tt0386676",
			},
		},
	}

	showID, err := resolveTVShowID(reference, shows)
	require.NoError(t, err)
	assert.Equal(t, 205, showID)
}

func TestResolveTVShowID_RepairsLegacyPathByExactTitle(t *testing.T) {
	t.Parallel()

	reference, err := parseTVShowReference("kodi-show://106/The%20Office")
	require.NoError(t, err)

	shows := []TVShow{
		{ID: 106, Label: "One-Punch Man"},
		{ID: 205, Label: "The Office"},
	}

	showID, err := resolveTVShowID(reference, shows)
	require.NoError(t, err)
	assert.Equal(t, 205, showID)
}

func TestResolveTVShowID_RejectsAmbiguousLegacyTitle(t *testing.T) {
	t.Parallel()

	reference, err := parseTVShowReference("kodi-show://106/The%20Office")
	require.NoError(t, err)

	shows := []TVShow{
		{ID: 106, Label: "One-Punch Man"},
		{ID: 205, Label: "The Office"},
		{ID: 206, Label: "The Office"},
	}

	_, err = resolveTVShowID(reference, shows)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "multiple Kodi TV shows")
}

func TestParseTVShowReference_RejectsConflictingProviderValues(t *testing.T) {
	t.Parallel()

	_, err := parseTVShowReference("kodi-show://106/The%20Office?tvdb=73244&TVDB=99999")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "conflicting tvdb")
}
