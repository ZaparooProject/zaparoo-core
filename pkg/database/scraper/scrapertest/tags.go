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

// Package scrapertest holds assertions shared by the scraper packages' tests.
package scrapertest

import (
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
)

// RequireValidTags fails the test for any tag the vocabulary would refuse at
// write time. Every scraper output test should call it: a scraper that emits
// an unmapped value loses it silently in production.
func RequireValidTags(t testing.TB, infos ...[]database.TagInfo) {
	t.Helper()
	for _, set := range infos {
		for _, ti := range set {
			if err := tags.ValidateTagValue(tags.TagType(ti.Type), ti.Tag); err != nil {
				t.Errorf("scraper emitted a tag the vocabulary refuses: %v", err)
			}
		}
	}
}

// RequireValidWrite checks every tag in a scrape write, including its
// sentinel.
func RequireValidWrite(t testing.TB, write *database.ScrapeWrite) {
	t.Helper()
	if write == nil {
		return
	}
	RequireValidTags(t, write.MediaTags, write.TitleTags, []database.TagInfo{write.Sentinel})
}
