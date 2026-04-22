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

package methods

import (
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/stretchr/testify/assert"
)

func TestCapTagsByCategory_Empty(t *testing.T) {
	t.Parallel()
	result := capTagsByCategory(nil, 10)
	assert.Empty(t, result)

	result = capTagsByCategory([]database.TagInfo{}, 10)
	assert.Empty(t, result)
}

func TestCapTagsByCategory_UnderLimit(t *testing.T) {
	t.Parallel()
	tags := []database.TagInfo{
		{Type: "year", Tag: "1990", Count: 5},
		{Type: "year", Tag: "1991", Count: 3},
		{Type: "genre", Tag: "RPG", Count: 10},
	}
	result := capTagsByCategory(tags, 100)
	assert.Len(t, result, 3, "all tags pass through when under limit")
}

func TestCapTagsByCategory_OverLimit_TopNSurvive(t *testing.T) {
	t.Parallel()
	tags := make([]database.TagInfo, 0, 5)
	for i := range 5 {
		tags = append(tags, database.TagInfo{
			Type:  "credit",
			Tag:   "person" + string(rune('A'+i)),
			Count: int64(i + 1),
		})
	}

	result := capTagsByCategory(tags, 3)
	assert.Len(t, result, 3, "only top 3 per category survive")
	assert.Equal(t, "personE", result[0].Tag)
	assert.Equal(t, int64(5), result[0].Count, "highest count is first")
	assert.Equal(t, "personD", result[1].Tag)
	assert.Equal(t, int64(4), result[1].Count)
	assert.Equal(t, "personC", result[2].Tag)
	assert.Equal(t, int64(3), result[2].Count)
}

func TestCapTagsByCategory_TieBreakByTagAsc(t *testing.T) {
	t.Parallel()
	tags := []database.TagInfo{
		{Type: "genre", Tag: "Zebra", Count: 5},
		{Type: "genre", Tag: "Alpha", Count: 5},
		{Type: "genre", Tag: "Middle", Count: 5},
	}
	result := capTagsByCategory(tags, 2)
	assert.Len(t, result, 2)
	assert.Equal(t, "Alpha", result[0].Tag, "ties broken alphabetically asc")
	assert.Equal(t, "Middle", result[1].Tag)
}

func TestCapTagsByCategory_MultiCategory(t *testing.T) {
	t.Parallel()
	tags := []database.TagInfo{
		{Type: "credit", Tag: "c1", Count: 10},
		{Type: "credit", Tag: "c2", Count: 9},
		{Type: "credit", Tag: "c3", Count: 8},
		{Type: "year", Tag: "1985", Count: 50},
		{Type: "year", Tag: "1990", Count: 40},
	}
	result := capTagsByCategory(tags, 2)
	assert.Len(t, result, 4, "2 from credit + 2 from year")

	creditTags := make([]database.TagInfo, 0)
	yearTags := make([]database.TagInfo, 0)
	for _, tag := range result {
		switch tag.Type {
		case "credit":
			creditTags = append(creditTags, tag)
		case "year":
			yearTags = append(yearTags, tag)
		}
	}
	assert.Len(t, creditTags, 2, "credit capped at 2")
	assert.Len(t, yearTags, 2, "year capped at 2")
}

func TestCapTagsByCategory_ExactlyAtLimit(t *testing.T) {
	t.Parallel()
	tags := make([]database.TagInfo, 0, 3)
	for i := range 3 {
		tags = append(tags, database.TagInfo{
			Type:  "genre",
			Tag:   "g" + string(rune('0'+i)),
			Count: int64(i + 1),
		})
	}
	result := capTagsByCategory(tags, 3)
	assert.Len(t, result, 3, "exactly at limit — all pass through")
}

func TestCapTagsByCategory_CategoryOrderPreserved(t *testing.T) {
	t.Parallel()
	tags := []database.TagInfo{
		{Type: "year", Tag: "1990", Count: 1},
		{Type: "genre", Tag: "RPG", Count: 1},
		{Type: "credit", Tag: "Alice", Count: 1},
	}
	result := capTagsByCategory(tags, 100)
	assert.Len(t, result, 3)
	assert.Equal(t, "year", result[0].Type, "first-seen type comes first")
	assert.Equal(t, "genre", result[1].Type)
	assert.Equal(t, "credit", result[2].Type)
}
