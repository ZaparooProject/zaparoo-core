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

package mediadb

import (
	"cmp"
	"context"
	"slices"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/slugs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
)

type mediaSearchTypeGroup struct {
	systems       []systemdefs.System
	variantGroups [][]string
	includeName   bool
}

func buildMediaSearchTypeGroups(
	systems []systemdefs.System,
	queryWords []string,
) []mediaSearchTypeGroup {
	groups := make([]mediaSearchTypeGroup, 0, 8)
	groupIndexes := make(map[slugs.MediaType]int, 8)

	for i := range systems {
		mediaType := systems[i].GetMediaType()
		groupIndex, ok := groupIndexes[mediaType]
		if !ok {
			groupIndex = len(groups)
			groupIndexes[mediaType] = groupIndex
			groups = append(groups, mediaSearchTypeGroup{})
		}
		groups[groupIndex].systems = append(groups[groupIndex].systems, systems[i])
	}

	for i := range groups {
		mediaType := groups[i].systems[0].GetMediaType()
		groups[i].variantGroups = make([][]string, len(queryWords))
		for wordIndex, word := range queryWords {
			variant := slugs.Slugify(mediaType, word)
			if variant == "" {
				groups[i].includeName = true
				continue
			}
			groups[i].variantGroups[wordIndex] = []string{variant}
		}
	}

	return groups
}

func mediaSearchTypeGroupsCacheable(groups []mediaSearchTypeGroup) bool {
	if len(groups) == 0 {
		return false
	}
	for i := range groups {
		if groups[i].includeName || len(groups[i].variantGroups) == 0 {
			return false
		}
	}
	return true
}

func searchMediaTypeGroupsInCache(
	cache *SlugSearchCache,
	groups []mediaSearchTypeGroup,
) []int64 {
	seen := make(map[int64]struct{})
	candidates := make([]int64, 0, 1024)

	for i := range groups {
		systemIDs := make([]string, len(groups[i].systems))
		for j := range groups[i].systems {
			systemIDs[j] = groups[i].systems[j].ID
		}
		systemDBIDs := cache.ResolveSystemDBIDs(systemIDs)
		if len(systemDBIDs) == 0 {
			continue
		}

		byteGroups := make([][][]byte, len(groups[i].variantGroups))
		for wordIndex, variants := range groups[i].variantGroups {
			byteGroups[wordIndex] = make([][]byte, len(variants))
			for variantIndex, variant := range variants {
				byteGroups[wordIndex][variantIndex] = []byte(variant)
			}
		}

		for _, candidate := range cache.Search(systemDBIDs, byteGroups) {
			if _, ok := seen[candidate]; ok {
				continue
			}
			seen[candidate] = struct{}{}
			candidates = append(candidates, candidate)
		}
	}

	return candidates
}

func titleDBIDQueryParamCount(candidateCount int, filters *database.SearchFilters) int {
	_, tagArgs := buildCandidateTagFilterSQL(filters.Tags)
	_, letterArgs := BuildLetterFilterSQL(filters.Letter, "MediaTitles.Name")

	count := candidateCount + len(tagArgs) + len(letterArgs) + 1 // LIMIT
	if filters.Cursor != nil {
		count++
	}
	return count
}

func (db *MediaDB) searchMediaTypeGroupsWithSQL(
	ctx context.Context,
	groups []mediaSearchTypeGroup,
	rawWords []string,
	filters *database.SearchFilters,
) ([]database.SearchResultWithCursor, error) {
	results := make([]database.SearchResultWithCursor, 0, filters.Limit)
	for i := range groups {
		groupResults, err := sqlSearchMediaWithFilters(
			ctx,
			db.sql.Load(),
			groups[i].systems,
			groups[i].variantGroups,
			rawWords,
			filters.Tags,
			filters.Letter,
			filters.Cursor,
			filters.Limit,
			groups[i].includeName,
		)
		if err != nil {
			return nil, err
		}
		results = append(results, groupResults...)
	}

	slices.SortFunc(results, func(a, b database.SearchResultWithCursor) int {
		return cmp.Compare(a.MediaID, b.MediaID)
	})
	results = slices.CompactFunc(results, func(a, b database.SearchResultWithCursor) bool {
		return a.MediaID == b.MediaID
	})
	if len(results) > filters.Limit {
		results = results[:filters.Limit]
	}
	return results, nil
}
