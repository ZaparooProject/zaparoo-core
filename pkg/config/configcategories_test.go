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

	primary, categories := resolver.VirtualSystem("other",
		[]string{"favorite systems", "FAVORITE SYSTEMS", "Niños & Family", "OTHER"})
	assert.Equal(t, "Other", primary)
	assert.Equal(t, []string{"Other", "Favorite Systems", "Niños & Family"}, categories)
}

func TestSystemCategoriesVirtualSystemIgnoresUndeclared(t *testing.T) {
	t.Parallel()

	cfg := &Instance{}
	require.NoError(t, cfg.LoadTOML(`
[[systems.category]]
name = "Kids"
`))

	primary, categories := cfg.SystemCategoryResolver().VirtualSystem("Removed", []string{"Gone", "kids", ""})
	assert.Equal(t, "Other", primary)
	assert.Equal(t, []string{"Other", "Kids"}, categories)

	primary, categories = cfg.SystemCategoryResolver().VirtualSystem("", []string{"other", "Gone"})
	assert.Equal(t, "Other", primary)
	assert.Equal(t, []string{"Other"}, categories, "the primary category is always listed")
}

func TestSystemCategoriesSaveLoadRoundTrip(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	cfg, err := NewConfigWithFs("/config", BaseDefaults, fs)
	require.NoError(t, err)
	require.NoError(t, cfg.LoadTOML(`
[[systems.category]]
name = "Favorite Systems"
systems = ["SNES", "SNESS"]

[[systems.category]]
name = "Kids"
systems = ["NES", "SNES"]
`))
	require.NoError(t, cfg.Save())

	saved, err := afero.ReadFile(fs, "/config/"+CfgFile)
	require.NoError(t, err)
	assert.Contains(t, string(saved), "SNESS", "unknown systems stay in the file as the user wrote them")

	require.NoError(t, cfg.Load())
	resolver := cfg.SystemCategoryResolver()
	assert.Equal(t, []string{"Console", "Favorite Systems", "Kids"},
		resolver.ForSystem("SNES", "Console"))
	assert.Equal(t, []string{"Console", "Kids"}, resolver.ForSystem("NES", "Console"))
}

func TestSystemCategoriesReportInvalidDeclarations(t *testing.T) {
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
			name:       "full-width slash",
			categories: []SystemsCategory{{Name: "Favorites\uff0fGames"}},
			errText:    "path separators",
		},
		{
			name:       "zero-width space only",
			categories: []SystemsCategory{{Name: "\u200b"}},
			errText:    "invisible formatting characters",
		},
		{
			name:       "bidi override",
			categories: []SystemsCategory{{Name: "Fav\u202eorites"}},
			errText:    "invisible formatting characters",
		},
		{
			name:       "no visible character",
			categories: []SystemsCategory{{Name: "\u200d"}},
			errText:    "visible character",
		},
		{
			name: "built-in collision",
			categories: []SystemsCategory{
				{Name: "console", Systems: []string{"SNES"}},
			},
			errText: "conflicts with a built-in",
		},
		{
			name:       "full-width built-in collision",
			categories: []SystemsCategory{{Name: "\uff23onsole"}},
			errText:    "conflicts with a built-in",
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
			name: "lookalike duplicate",
			categories: []SystemsCategory{
				{Name: "Kids"},
				{Name: "\u212aids"},
			},
			errText: "duplicate name",
		},
		{
			name: "unknown system",
			categories: []SystemsCategory{
				{Name: "Favorites", Systems: []string{"NotARealSystem"}},
			},
			errText: "unknown system",
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
			_, problems := newCategoryResolver(tt.categories)
			require.Len(t, problems, 1)
			require.ErrorContains(t, problems[0], tt.errText)
		})
	}
}

func TestSystemCategoriesAcceptVisibleUnicodeNames(t *testing.T) {
	t.Parallel()

	names := []string{
		"Niños & Family",
		"\U0001F468\u200d\U0001F469\u200d\U0001F467 Family",
		"\u0645\u06cc\u200c\u062e\u0648\u0627\u0647\u0645",
		"レトロ",
		strings.Repeat("\U0001F3AE", maxSystemCategoryNameRunes),
	}
	for _, name := range names {
		require.NoError(t, validateSystemCategoryName(name), "%q", name)
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
name = "Kids"
systems = ["NES"
`)
	require.Error(t, err)
	assert.Equal(t, []string{"Console", "Favorites"},
		cfg.SystemCategoryResolver().ForSystem("SNES", "Console"))
	assert.Equal(t, []string{"Console"}, cfg.SystemCategoryResolver().ForSystem("NES", "Console"))
}

func TestSystemCategoriesSchemaMismatchRetainsPreviousResolver(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	cfg, err := NewConfigWithFs("/config", BaseDefaults, fs)
	require.NoError(t, err)
	cfgPath := "/config/" + CfgFile
	base, err := afero.ReadFile(fs, cfgPath)
	require.NoError(t, err)

	require.NoError(t, afero.WriteFile(fs, cfgPath, append(append([]byte(nil), base...), `
[[systems.category]]
name = "Favorites"
systems = ["SNES"]
`...), 0o600))
	require.NoError(t, cfg.Load())

	mismatched := strings.Replace(string(base), "config_schema = ", "config_schema = 9", 1) + `
[[systems.category]]
name = "Kids"
systems = ["SNES"]
`
	require.NoError(t, afero.WriteFile(fs, cfgPath, []byte(mismatched), 0o600))
	require.Error(t, cfg.Load())
	assert.Equal(t, []string{"Console", "Favorites"},
		cfg.SystemCategoryResolver().ForSystem("SNES", "Console"))
}

func TestSetSystemDefaultsPreservesSystemCategories(t *testing.T) {
	t.Parallel()

	cfg := &Instance{}
	require.NoError(t, cfg.LoadTOML(`
[[systems.category]]
name = "Favorites"
systems = ["SNES"]
`))

	cfg.SetSystemDefaults([]SystemsDefault{{System: "SNES", Launcher: "snes9x"}})
	assert.Equal(t, []string{"Console", "Favorites"},
		cfg.SystemCategoryResolver().ForSystem("SNES", "Console"))
}

func TestSystemCategoriesInvalidEntriesDoNotFailLoad(t *testing.T) {
	t.Parallel()

	cfg := &Instance{}
	require.NoError(t, cfg.LoadTOML(`
[[systems.category]]
name = "Favorites"
systems = ["SNES", "SNESS", "Super Nintendo"]

[[systems.category]]
name = "console"
systems = ["NES"]

[[systems.category]]
name = "FAVORITES"
systems = ["NES"]

[[systems.category]]
name = " Padded "
systems = ["NES"]

[[systems.category]]
name = "Kids"
systems = ["Genesis"]
`))

	resolver := cfg.SystemCategoryResolver()
	assert.Equal(t, []string{"Console", "Favorites"}, resolver.ForSystem("SNES", "Console"))
	assert.Equal(t, []string{"Console"}, resolver.ForSystem("NES", "Console"))
	assert.Equal(t, []string{"Console", "Kids"}, resolver.ForSystem("Genesis", "Console"))
}
