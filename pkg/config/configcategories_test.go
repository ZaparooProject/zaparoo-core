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

package config

import (
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSystemCategoriesResolveLiteralMemberships(t *testing.T) {
	t.Parallel()

	cfg := &Instance{}
	require.NoError(t, cfg.LoadTOML(`
[[systems.category]]
name = "Favorite Systems"
systems = ["megadrive", "SNES"]

[[systems.category]]
name = "Niños & Family"
systems = ["SNES"]
`))

	resolver := cfg.SystemCategoryResolver()
	assert.Equal(t, []string{"Console", "Favorite Systems"}, resolver.ForSystem("Genesis", "Console"))
	assert.Equal(t,
		[]string{"Console", "Favorite Systems", "Niños & Family"},
		resolver.ForSystem("SNES", "Console"))
	assert.Equal(t,
		[]string{"Other", "Favorite Systems", "Niños & Family"},
		resolver.Combine("Other", []string{"favorite systems", "FAVORITE SYSTEMS", "Niños & Family"}))
}

func TestSystemCategoriesSaveLoadRoundTrip(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	cfg, err := NewConfigWithFs("/config", BaseDefaults, fs)
	require.NoError(t, err)
	require.NoError(t, cfg.LoadTOML(`
[[systems.category]]
name = "Favorite Systems"
systems = ["SNES"]

[[systems.category]]
name = "Kids"
systems = ["NES", "SNES"]
`))
	require.NoError(t, cfg.Save())
	require.NoError(t, cfg.Load())

	resolver := cfg.SystemCategoryResolver()
	assert.Equal(t, []string{"Console", "Favorite Systems", "Kids"},
		resolver.ForSystem("SNES", "Console"))
	assert.Equal(t, []string{"Console", "Kids"}, resolver.ForSystem("NES", "Console"))
}

func TestSystemCategoriesRejectInvalidDeclarations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		errText    string
		categories []SystemsCategory
	}{
		{
			name:       "missing name",
			categories: []SystemsCategory{{Systems: []string{"SNES"}}},
			errText:    "name is required",
		},
		{
			name:       "surrounding whitespace",
			categories: []SystemsCategory{{Name: " Favorites "}},
			errText:    "surrounding whitespace",
		},
		{
			name:       "too long",
			categories: []SystemsCategory{{Name: strings.Repeat("x", maxSystemCategoryNameRunes+1)}},
			errText:    "must not exceed",
		},
		{
			name:       "control character",
			categories: []SystemsCategory{{Name: "Favorites\nGames"}},
			errText:    "control characters",
		},
		{
			name:       "slash",
			categories: []SystemsCategory{{Name: "Favorites/Games"}},
			errText:    "path separators",
		},
		{
			name:       "backslash",
			categories: []SystemsCategory{{Name: `Favorites\Games`}},
			errText:    "path separators",
		},
		{
			name: "built-in collision",
			categories: []SystemsCategory{
				{Name: "console", Systems: []string{"SNES"}},
			},
			errText: "conflicts with a built-in",
		},
		{
			name: "case-insensitive duplicate",
			categories: []SystemsCategory{
				{Name: "Favorites", Systems: []string{"SNES"}},
				{Name: "FAVORITES", Systems: []string{"NES"}},
			},
			errText: "duplicate name",
		},
		{
			name: "unknown system",
			categories: []SystemsCategory{
				{Name: "Favorites", Systems: []string{"NotARealSystem"}},
			},
			errText: "invalid system",
		},
		{
			name: "duplicate system alias",
			categories: []SystemsCategory{
				{Name: "Favorites", Systems: []string{"Genesis", "megadrive"}},
			},
			errText: "duplicate system",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.ErrorContains(t, validateSystemCategories(tt.categories), tt.errText)
		})
	}
}

func TestSystemCategoriesFailedLoadRetainsPreviousConfig(t *testing.T) {
	t.Parallel()

	cfg := &Instance{}
	require.NoError(t, cfg.LoadTOML(`
[[systems.category]]
name = "Favorites"
systems = ["SNES"]
`))

	err := cfg.LoadTOML(`
[[systems.category]]
name = "Favorites"
systems = ["UnknownSystem"]
`)
	require.Error(t, err)
	assert.Equal(t, []string{"Console", "Favorites"},
		cfg.SystemCategoryResolver().ForSystem("SNES", "Console"))
}

func TestSetSystemDefaultsPreservesSystemCategories(t *testing.T) {
	t.Parallel()

	cfg := &Instance{
		vals: Values{Systems: Systems{Category: []SystemsCategory{
			{Name: "Favorites", Systems: []string{"SNES"}},
		}}},
	}

	cfg.SetSystemDefaults([]SystemsDefault{{System: "SNES", Launcher: "snes9x"}})
	assert.Equal(t, []string{"Console", "Favorites"},
		cfg.SystemCategoryResolver().ForSystem("SNES", "Console"))
}
