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

package recommend

import (
	"context"
	"fmt"
	"testing"
)

// BenchmarkScoreCandidates measures comparing one seed with 150 candidates:
// the scoring media.similar does once it has read everyone's tags.
func BenchmarkScoreCandidates(b *testing.B) {
	lib := newFakeLibrary()
	lib.add(1, "NES", tagPlatformer, tagMario, tagNintendo, "perspective=side", "players=2", "year=1988")
	for id := int64(2); id <= 151; id++ {
		titleTags := []string{tagPlatformer, fmt.Sprintf("developer=dev%d", id%12), fmt.Sprintf("year=%d", 1980+id%20)}
		if id%5 == 0 {
			titleTags = append(titleTags, tagMario, tagNintendo)
		}
		if id%3 == 0 {
			titleTags = append(titleTags, "perspective=side", "players=2", tagMaze)
		}
		lib.add(id, "NES", titleTags...)
	}
	ctx := context.Background()
	seeds, err := prepareSeeds(ctx, lib, []int64{1})
	if err != nil {
		b.Fatal(err)
	}
	ids := make([]int64, 0, 150)
	for id := int64(2); id <= 151; id++ {
		ids = append(ids, id)
	}
	profiles, _, err := loadCandidates(ctx, lib, ids)
	if err != nil {
		b.Fatal(err)
	}
	prepared := seeds[1]

	b.ReportAllocs()
	for b.Loop() {
		alike := 0
		for _, id := range ids {
			if _, ok := prepared.score(profiles[id]); ok {
				alike++
			}
		}
		if alike == 0 {
			b.Fatal("nothing alike")
		}
	}
}
