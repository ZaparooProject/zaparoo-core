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

package arcadegenre

import (
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTablesAreDisjointAndValid(t *testing.T) {
	t.Parallel()
	mapped, skipped := Keys()
	require.NotEmpty(t, mapped)
	for _, key := range skipped {
		_, isMapped := genreTable[key]
		assert.False(t, isMapped, "%q is both mapped and skipped", key)
	}
	for _, key := range mapped {
		values, known := Lookup(key)
		assert.True(t, known)
		assert.NotEmpty(t, values, "%q maps to nothing; list it in notGenres instead", key)
		for _, value := range values {
			assert.NoError(t, tags.ValidateTagValue(tags.TagTypeGenre, string(value)), "%q", key)
		}
	}
}

// An earlier Core stored the catalog category slugified as a free-form genre.
// Upgrading maps it through the same table a scrape uses now.
func TestLegacyGenreReadsSlugifiedCategories(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		value   string
		want    []string
		unknown bool
	}{
		{value: "shooter-flying-vertical", want: []string{"shmup:v", "shmup"}},
		{value: "fighter-2-5d", want: []string{"brawler"}},
		{value: "quiz-questions-in-japanese", want: []string{"quiz"}},
		{value: "shooter"},
		{value: "system-bios"},
		{value: "race,-driving", unknown: true},
	} {
		values, known := LegacyGenre(tc.value)
		got := make([]string, 0, len(values))
		for _, value := range values {
			got = append(got, string(value))
		}
		assert.Equal(t, !tc.unknown, known, "%q", tc.value)
		if tc.want == nil {
			assert.Empty(t, got, "%q", tc.value)
			continue
		}
		assert.Equal(t, tc.want, got, "%q", tc.value)
	}
}
