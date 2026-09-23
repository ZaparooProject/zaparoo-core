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

package misterarcade

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper/scrapertest"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These tests read the catalog MiSTer bundles and fail when it holds a value
// the scraper has no decision for. That is their purpose: a catalog update
// that adds a category, board, series or button count must be curated into
// the tables (or the explicit skip sets) before it ships, rather than being
// dropped silently at scrape time.

// catalogValues reads the pinned distinct values of the catalog columns the
// mapper reads (testdata/arcade_catalog_values.tsv), keyed by column. The
// catalog itself is downloaded at build time from a moving upstream, so the
// tests pin a snapshot instead; refresh it and curate what they report.
func catalogValues(t *testing.T) map[string][]string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "arcade_catalog_values.tsv"))
	require.NoError(t, err)
	values := make(map[string][]string)
	for _, line := range strings.Split(string(data), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		column, value, ok := strings.Cut(line, "\t")
		require.True(t, ok, "malformed line %q", line)
		values[column] = append(values[column], value)
	}
	require.Greater(t, len(values["category"]), 100, "the snapshot should hold the whole catalog vocabulary")
	return values
}

// distinct returns the cleaned, non-empty values of one column, sorted.
func distinct(values map[string][]string, column string) []string {
	set := make(map[string]struct{})
	for _, raw := range values[column] {
		if value := field(raw); value != "" {
			set[value] = struct{}{}
		}
	}
	out := make([]string, 0, len(set))
	for value := range set {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

// catalogKeys returns the skipKey of every distinct value of one column.
func catalogKeys(values map[string][]string, column string, fold func(string) string) map[string]struct{} {
	keys := make(map[string]struct{})
	for _, value := range distinct(values, column) {
		keys[fold(value)] = struct{}{}
	}
	return keys
}

func TestEveryCatalogCategoryHasAGenreDecision(t *testing.T) {
	t.Parallel()
	values := catalogValues(t)
	for _, category := range distinct(values, "category") {
		values, known := genreTags(category)
		_, skipped := notGenres[categoryKey(category)]
		assert.True(t, known, "category %q is in neither genreTable nor notGenres", category)
		assert.True(t, len(values) > 0 || skipped, "category %q maps to no genre", category)
		for _, value := range values {
			assert.NoError(t, tags.ValidateTagValue(tags.TagTypeGenre, string(value)), "category %q", category)
		}
	}
}

func TestGenreSkipsAreNotStale(t *testing.T) {
	t.Parallel()
	inCatalog := catalogKeys(catalogValues(t), "category", categoryKey)
	for key := range notGenres {
		_, mapped := genreTable[key]
		assert.False(t, mapped, "%q is both mapped and skipped", key)
		_, present := inCatalog[key]
		assert.True(t, present, "%q is skipped but no longer in the catalog", key)
	}
	for key, values := range genreTable {
		assert.NotEmpty(t, values, "%q maps to nothing; list it in notGenres instead", key)
	}
}

func TestEveryCatalogPlatformHasABoardDecision(t *testing.T) {
	t.Parallel()
	for _, platform := range distinct(catalogValues(t), "platform") {
		_, mapped := tags.LookupArcadeBoard(platform)
		_, skipped := boardSkips[skipKey(platform)]
		assert.True(t, mapped || skipped,
			"platform %q maps to no arcadeboard and is not in boardSkips", platform)
	}
}

func TestBoardSkipsAreNotStale(t *testing.T) {
	t.Parallel()
	inCatalog := catalogKeys(catalogValues(t), "platform", skipKey)
	for key := range boardSkips {
		value, mapped := tags.LookupArcadeBoard(key)
		assert.False(t, mapped, "skipped platform %q now maps to %s", key, value)
		_, present := inCatalog[key]
		assert.True(t, present, "%q is skipped but no longer in the catalog", key)
	}
}

func TestEveryCatalogSeriesHasAFranchiseDecision(t *testing.T) {
	t.Parallel()
	for _, series := range distinct(catalogValues(t), "series") {
		_, mapped := tags.LookupFranchise(series)
		_, skipped := notFranchises[skipKey(series)]
		assert.True(t, mapped || skipped,
			"series %q maps to no franchise and is not in notFranchises", series)
	}
}

func TestFranchiseSkipsAreNotStale(t *testing.T) {
	t.Parallel()
	inCatalog := catalogKeys(catalogValues(t), "series", skipKey)
	for key := range notFranchises {
		value, mapped := tags.LookupFranchise(key)
		assert.False(t, mapped, "skipped series %q now maps to %s", key, value)
		_, present := inCatalog[key]
		assert.True(t, present, "%q is skipped but no longer in the catalog", key)
	}
}

// Zero buttons writes nothing on purpose; every other count the catalog uses
// must be a canonical buttons value.
func TestEveryCatalogButtonCountMaps(t *testing.T) {
	t.Parallel()
	for _, count := range distinct(catalogValues(t), "num_buttons") {
		if count == "0" {
			continue
		}
		value, ok := buttonTag(count)
		assert.True(t, ok, "num_buttons %q has no canonical input value", count)
		assert.NoError(t, tags.ValidateTagValue(tags.TagTypeInput, string(value)))
	}
}

func TestEveryCatalogPlayerValueMaps(t *testing.T) {
	t.Parallel()
	for _, players := range distinct(catalogValues(t), "players") {
		values, ok := playerTags(players)
		assert.True(t, ok, "players %q is not fully representable", players)
		assert.NotEmpty(t, values, "players %q", players)
	}
}

// Every value the catalog uses builds a write the vocabulary accepts in full,
// and the only values it drops are the ones the tables decide to drop.
func TestEveryCatalogValueBuildsAValidWrite(t *testing.T) {
	t.Parallel()
	unmapped := &scraper.UnmappedValues{}
	setters := map[string]func(*Entry, string){
		"region": func(e *Entry, v string) { e.Region = v }, "version": func(e *Entry, v string) { e.Version = v },
		"alternative":      func(e *Entry, v string) { e.Alternative = v },
		"platform":         func(e *Entry, v string) { e.Platform = v },
		"series":           func(e *Entry, v string) { e.Series = v },
		"homebrew":         func(e *Entry, v string) { e.Homebrew = v },
		"bootleg":          func(e *Entry, v string) { e.Bootleg = v },
		"year":             func(e *Entry, v string) { e.Year = v },
		"manufacturer":     func(e *Entry, v string) { e.Manufacturer = v },
		"category":         func(e *Entry, v string) { e.Category = v },
		"resolution":       func(e *Entry, v string) { e.Resolution = v },
		"rotation":         func(e *Entry, v string) { e.Rotation = v },
		"flip":             func(e *Entry, v string) { e.Flip = v },
		"players":          func(e *Entry, v string) { e.Players = v },
		"move_inputs":      func(e *Entry, v string) { e.MoveInputs = v },
		"special_controls": func(e *Entry, v string) { e.SpecialControls = v },
		"num_buttons":      func(e *Entry, v string) { e.NumButtons = v },
	}
	values := catalogValues(t)
	for column, set := range setters {
		require.NotEmpty(t, values[column], "the snapshot lacks column %q", column)
		for _, value := range values[column] {
			entry := Entry{SetName: "test"}
			set(&entry, value)
			scrapertest.RequireValidWrite(t, buildWrite(&entry, "", unmapped))
		}
	}
	for _, tagType := range []tags.TagType{
		tags.TagTypeGenre, tags.TagTypeArcadeBoard, tags.TagTypeSearch, tags.TagTypePlayers,
		tags.TagTypeInput, tags.TagTypeYear, tags.TagTypeVideo, tags.TagTypeDeveloper,
	} {
		assert.Zero(t, unmapped.Count(tagType), "the catalog drops unexpected %s values", tagType)
	}
}
