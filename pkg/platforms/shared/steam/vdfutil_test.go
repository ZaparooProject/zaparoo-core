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

package steam

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormalizeVDFKeys(t *testing.T) {
	t.Parallel()

	t.Run("lowercases_top_level_keys", func(t *testing.T) {
		t.Parallel()

		m := map[string]any{
			"AppState": "value1",
			"Name":     "value2",
		}

		result := normalizeVDFKeys(m)

		assert.Equal(t, "value1", result["appstate"])
		assert.Equal(t, "value2", result["name"])
		_, hasOriginal := result["AppState"]
		assert.False(t, hasOriginal)
	})

	t.Run("lowercases_nested_keys", func(t *testing.T) {
		t.Parallel()

		m := map[string]any{
			"AppState": map[string]any{
				"AppID": "123",
				"Name":  "Test Game",
			},
		}

		result := normalizeVDFKeys(m)

		nested, ok := result["appstate"].(map[string]any)
		assert.True(t, ok)
		assert.Equal(t, "123", nested["appid"])
		assert.Equal(t, "Test Game", nested["name"])
	})

	t.Run("preserves_values", func(t *testing.T) {
		t.Parallel()

		m := map[string]any{
			"key": "MixedCaseValue",
		}

		result := normalizeVDFKeys(m)

		assert.Equal(t, "MixedCaseValue", result["key"])
	})

	t.Run("handles_empty_map", func(t *testing.T) {
		t.Parallel()

		result := normalizeVDFKeys(map[string]any{})

		assert.Empty(t, result)
	})

	t.Run("is_idempotent", func(t *testing.T) {
		t.Parallel()

		m := map[string]any{
			"AppState": map[string]any{
				"Name": "Test",
			},
		}

		first := normalizeVDFKeys(m)
		second := normalizeVDFKeys(first)

		assert.Equal(t, first, second)
	})
}
