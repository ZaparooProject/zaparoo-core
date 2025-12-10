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

package steam

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewClient(t *testing.T) {
	t.Parallel()

	t.Run("creates_client_with_options", func(t *testing.T) {
		t.Parallel()

		opts := Options{
			FallbackPath: "/test/path",
			UseXdgOpen:   true,
			CheckFlatpak: true,
		}

		client := NewClient(opts)

		assert.NotNil(t, client)
		assert.Equal(t, "/test/path", client.opts.FallbackPath)
	})
}

func TestNormalizePath(t *testing.T) {
	t.Parallel()

	t.Run("normalizes_rungameid_format", func(t *testing.T) {
		t.Parallel()

		result := NormalizePath("steam://rungameid/123")

		assert.Equal(t, "steam://123", result)
	})

	t.Run("preserves_standard_format", func(t *testing.T) {
		t.Parallel()

		result := NormalizePath("steam://123/GameName")

		assert.Equal(t, "steam://123/GameName", result)
	})

	t.Run("preserves_other_schemes", func(t *testing.T) {
		t.Parallel()

		result := NormalizePath("http://example.com")

		assert.Equal(t, "http://example.com", result)
	})
}

func TestExtractAndValidateID(t *testing.T) {
	t.Parallel()

	t.Run("extracts_valid_id_from_standard_format", func(t *testing.T) {
		t.Parallel()

		id, err := ExtractAndValidateID("steam://123/GameName")

		require.NoError(t, err)
		assert.Equal(t, "123", id)
	})

	t.Run("extracts_valid_id_from_rungameid_format", func(t *testing.T) {
		t.Parallel()

		id, err := ExtractAndValidateID("steam://rungameid/456")

		require.NoError(t, err)
		assert.Equal(t, "456", id)
	})

	t.Run("extracts_large_bpid", func(t *testing.T) {
		t.Parallel()

		// Big Picture ID format for non-Steam games
		id, err := ExtractAndValidateID("steam://2305843009213693952/NonSteamGame")

		require.NoError(t, err)
		assert.Equal(t, "2305843009213693952", id)
	})

	t.Run("rejects_non_numeric_id", func(t *testing.T) {
		t.Parallel()

		_, err := ExtractAndValidateID("steam://not-a-number/game")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid Steam game ID")
	})

	t.Run("rejects_empty_path", func(t *testing.T) {
		t.Parallel()

		_, err := ExtractAndValidateID("steam://")

		require.Error(t, err)
	})

	t.Run("rejects_malformed_path", func(t *testing.T) {
		t.Parallel()

		_, err := ExtractAndValidateID("not-a-steam-path")

		require.Error(t, err)
	})

	t.Run("rejects_negative_id", func(t *testing.T) {
		t.Parallel()

		_, err := ExtractAndValidateID("steam://-123/game")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid Steam game ID")
	})
}

func TestBuildSteamURL(t *testing.T) {
	t.Parallel()

	t.Run("builds_correct_url", func(t *testing.T) {
		t.Parallel()

		url := BuildSteamURL("123")

		assert.Equal(t, "steam://rungameid/123", url)
	})

	t.Run("handles_large_id", func(t *testing.T) {
		t.Parallel()

		url := BuildSteamURL("2305843009213693952")

		assert.Equal(t, "steam://rungameid/2305843009213693952", url)
	})
}
