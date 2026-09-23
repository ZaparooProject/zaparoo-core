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
package scraper

import (
	"sort"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/rs/zerolog/log"
)

// A scraper writes a tag only when it can map the source's value onto the tag
// vocabulary (see tags.ValidateTagValue). Anything it cannot map is dropped
// and noted here, so the tables can be grown from what real sources say.

// maxUnmappedExamples bounds the example values kept per tag type.
const maxUnmappedExamples = 8

// UnmappedValues counts the source values a scraper run dropped because they
// had no mapping. Safe for concurrent use.
type UnmappedValues struct {
	byType map[tags.TagType]*unmappedType
	mu     syncutil.Mutex
}

type unmappedType struct {
	distinct map[string]struct{}
	total    int
}

// Note records one dropped value. The first sighting of each value is logged
// at debug, with the scraper's name, so a single value can be traced.
func (u *UnmappedValues) Note(scraperID string, tagType tags.TagType, raw string) {
	if raw == "" {
		return
	}
	u.mu.Lock()
	if u.byType == nil {
		u.byType = make(map[tags.TagType]*unmappedType)
	}
	entry, ok := u.byType[tagType]
	if !ok {
		entry = &unmappedType{distinct: make(map[string]struct{})}
		u.byType[tagType] = entry
	}
	entry.total++
	_, seen := entry.distinct[raw]
	if !seen {
		entry.distinct[raw] = struct{}{}
	}
	u.mu.Unlock()
	if !seen {
		log.Debug().Str("scraper", scraperID).Str("type", string(tagType)).Str("value", raw).
			Msg("scraper value has no tag mapping; dropped")
	}
}

// LogSummary logs one info line per tag type that had unmapped values, with
// counts and a few examples. Call it once at the end of a run.
func (u *UnmappedValues) LogSummary(scraperID string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	types := make([]string, 0, len(u.byType))
	for tagType := range u.byType {
		types = append(types, string(tagType))
	}
	sort.Strings(types)
	for _, tagType := range types {
		entry := u.byType[tags.TagType(tagType)]
		examples := make([]string, 0, len(entry.distinct))
		for value := range entry.distinct {
			examples = append(examples, value)
		}
		sort.Strings(examples)
		if len(examples) > maxUnmappedExamples {
			examples = examples[:maxUnmappedExamples]
		}
		log.Info().Str("scraper", scraperID).Str("type", tagType).
			Int("values", len(entry.distinct)).Int("occurrences", entry.total).
			Strs("examples", examples).
			Msg("scraper values with no tag mapping were dropped")
	}
}

// Count returns how many distinct values were dropped for a tag type.
func (u *UnmappedValues) Count(tagType tags.TagType) int {
	u.mu.Lock()
	defer u.mu.Unlock()
	if entry, ok := u.byType[tagType]; ok {
		return len(entry.distinct)
	}
	return 0
}
