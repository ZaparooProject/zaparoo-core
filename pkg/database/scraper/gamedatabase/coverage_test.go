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

package gamedatabase

import (
	"maps"
	"slices"
	"strings"
	"testing"

	gdb "github.com/ZaparooProject/zaparoo-core/v2/pkg/database/gamedatabase"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper/scrapertest"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// forEachRow reads every embedded row.
func forEachRow(t *testing.T, fn func(bundle string, row *gdb.Row)) {
	t.Helper()
	bundles := gdb.Bundles()
	require.NotEmpty(t, bundles)
	for _, bundle := range bundles {
		require.NoError(t, gdb.ForEach(bundle, func(row *gdb.Row) error {
			fn(bundle, row)
			return nil
		}))
	}
}

// used tracks which entries of a table the embedded data needs.
type used[K comparable] map[K]struct{}

func (u used[K]) mark(key K) { u[key] = struct{}{} }

// requireAllUsed fails for a table entry no embedded row needs any more.
func requireAllUsed[K comparable, V any](t *testing.T, name string, table map[K]V, seen used[K]) {
	t.Helper()
	for key := range table {
		if _, ok := seen[key]; !ok {
			t.Errorf("%s lists %v, which no embedded row needs: remove it", name, key)
		}
	}
}

// TestEveryEmbeddedValueIsCovered reads the whole embedded dataset against the
// vocabulary tables. Refreshing the data to a new upstream commit fails here
// for every tag, region, date and ID that is not a Core value and is in no
// table, and for every table entry the new data no longer needs.
func TestEveryEmbeddedValueIsCovered(t *testing.T) {
	t.Parallel()
	tokensUsed := used[string]{}
	renamesUsed := used[typedValue]{}
	recoveriesUsed := used[typedValue]{}
	bareUsed := used[string]{}
	bareSkipsUsed := used[string]{}
	idsUsed := used[string]{}
	idSkipsUsed := used[string]{}
	datesUsed := used[string]{}
	regionWordsUsed := used[string]{}
	regionSkipsUsed := used[string]{}

	unknownTags := map[string]int{}
	unknownRegions := map[string]int{}
	unknownDates := map[string]int{}
	unknownIDs := map[string]int{}

	forEachRow(t, func(_ string, row *gdb.Row) {
		for _, field := range strings.Fields(row.Tags) {
			if _, ok := tokenRecoveries[field]; ok {
				tokensUsed.mark(field)
			}
		}
		tokens, _ := splitTokens(row.Tags)
		for i := range tokens {
			tok := &tokens[i]
			if len(tok.subs) == 0 {
				if _, ok := bareValues[tok.tagType]; ok {
					bareUsed.mark(tok.tagType)
				}
				if _, ok := bareSkips[tok.tagType]; ok {
					bareSkipsUsed.mark(tok.tagType)
				}
				continue
			}
			switch tok.tagType {
			case typeMameROM, typeMameParent, typeDisc:
				continue
			case typeCompilation:
				bareUsed.mark(typeCompilation)
				continue
			}
			for _, value := range rawValues(tok) {
				if renamed, ok := valueRenames[value]; ok {
					renamesUsed.mark(value)
					value = renamed
				}
				if _, ok := valueRecoveries[value]; ok {
					recoveriesUsed.mark(value)
				}
			}
		}
		parsed := readTags(row.Tags)
		for _, value := range parsed.unknown {
			unknownTags[string(value.Type)+":"+string(value.Value)]++
		}

		for word := range strings.SplitSeq(row.Region, "/") {
			word = strings.TrimSpace(word)
			if _, ok := regionWords[word]; ok {
				regionWordsUsed.mark(word)
			}
			if _, ok := regionSkips[word]; ok {
				regionSkipsUsed.mark(word)
			}
		}
		_, badRegions := regionValues(row.Region)
		for _, word := range badRegions {
			unknownRegions[word]++
		}

		date := strings.TrimSpace(row.Date)
		if _, ok := releaseYear(date); !ok && date != "" {
			if _, skip := dateSkips[date]; skip {
				datesUsed.mark(date)
			} else {
				unknownDates[date]++
			}
		}

		if _, ok := idRecoveries[row.ID]; ok {
			idsUsed.mark(row.ID)
		}
		if _, ok := datasetID(row.ID); !ok {
			if _, skip := idSkips[row.ID]; skip {
				idSkipsUsed.mark(row.ID)
			} else {
				unknownIDs[row.ID]++
			}
		}
	})

	assert.Empty(t, unknownTags, "tags with no Core value: add the value, a rename, a recovery or a skip")
	assert.Empty(t, unknownRegions, "region words with no Core region: map or skip them")
	assert.Empty(t, unknownDates, "release dates with no readable year: list them in dateSkips")
	assert.Empty(t, unknownIDs, "IDs Core cannot store: repair them in idRecoveries or list them in idSkips")

	requireAllUsed(t, "tokenRecoveries", tokenRecoveries, tokensUsed)
	requireAllUsed(t, "valueRenames", valueRenames, renamesUsed)
	requireAllUsed(t, "valueRecoveries", valueRecoveries, recoveriesUsed)
	requireAllUsed(t, "bareValues", bareValues, bareUsed)
	requireAllUsed(t, "bareSkips", bareSkips, bareSkipsUsed)
	requireAllUsed(t, "idRecoveries", idRecoveries, idsUsed)
	requireAllUsed(t, "idSkips", idSkips, idSkipsUsed)
	requireAllUsed(t, "dateSkips", dateSkips, datesUsed)
	requireAllUsed(t, "regionWords", regionWords, regionWordsUsed)
	requireAllUsed(t, "regionSkips", regionSkips, regionSkipsUsed)
}

// TestTableTargetsAreCoreValues guards the tables themselves: a rename or a
// recovery must land on a value the vocabulary accepts.
func TestTableTargetsAreCoreValues(t *testing.T) {
	t.Parallel()
	valid := func(value typedValue) {
		assert.NoError(t, tags.ValidateTagValue(value.Type, string(value.Value)))
	}
	for _, value := range valueRenames {
		valid(value)
	}
	for _, values := range valueRecoveries {
		for _, value := range values {
			valid(value)
		}
	}
	for _, value := range bareValues {
		valid(value)
	}
	for _, value := range regionWords {
		valid(typedValue{Type: tags.TagTypeRegion, Value: value})
	}
	for raw, id := range idRecoveries {
		assert.True(t, tags.IsValidTagValue(tags.TagTypeGameDatabase, id), "%q repairs to %q", raw, id)
		assert.False(t, tags.IsValidTagValue(tags.TagTypeGameDatabase, raw), "%q needs no repair", raw)
	}
	for raw, fixed := range tokenRecoveries {
		_, ok := parseToken(raw)
		assert.False(t, ok, "%q is already well formed", raw)
		for _, replacement := range fixed {
			_, ok := parseToken(replacement)
			assert.True(t, ok, "%q does not parse", replacement)
		}
	}
}

// TestEveryEmbeddedRowBuildsAValidWrite builds the write for every embedded
// row, as a title match and as a set match would, and checks what a database
// would be asked to store.
func TestEveryEmbeddedRowBuildsAValidWrite(t *testing.T) {
	t.Parallel()
	unmapped := &scraper.UnmappedValues{}
	rows := 0
	forEachRow(t, func(_ string, row *gdb.Row) {
		rows++
		parsed := readTags(row.Tags)
		b := newWriteBuilder("run", unmapped)
		b.addTitle(row, &parsed)
		b.addSet(&parsed)
		scrapertest.RequireValidWrite(t, b.write)

		perType := map[string]int{}
		for _, info := range b.write.TitleTags {
			perType[info.Type]++
		}
		for tagType, count := range perType {
			if tags.IsExclusiveType(tags.TagType(tagType)) && count > 1 {
				t.Errorf("%q writes %d %s values to one title", row.Title, count, tagType)
			}
		}
	})
	assert.Greater(t, rows, 25_000)
	for tagType := range tags.TagRules {
		assert.Zero(t, unmapped.Count(tagType), "%s values were dropped", tagType)
	}
}

func TestEveryBundleServesASystemAndEverySystemExists(t *testing.T) {
	t.Parallel()
	served := map[string]struct{}{arcadeBundle: {}}
	for systemID, src := range consoleSources {
		_, err := systemdefs.GetSystem(systemID)
		require.NoError(t, err, "%s is not a Core system", systemID)
		for _, bundle := range slices.Concat(src.primary, src.secondary) {
			served[bundle] = struct{}{}
		}
	}
	assert.ElementsMatch(t, gdb.Bundles(), slices.Collect(maps.Keys(served)),
		"every embedded bundle serves a system, and every bundle a system names is embedded")

	supported := SupportedSystems()
	assert.True(t, slices.IsSorted(supported))
	assert.Contains(t, supported, systemdefs.SystemArcade)
	assert.Contains(t, supported, systemdefs.SystemCPS1)
	assert.Contains(t, supported, systemdefs.SystemSNES)
	assert.NotContains(t, supported, systemdefs.SystemPinball, "pinball tables are not MAME sets")
	assert.NotContains(t, supported, systemdefs.SystemPSX)
}
