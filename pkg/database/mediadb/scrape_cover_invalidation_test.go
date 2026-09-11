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
	"context"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/slugs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestApplyScrapeResult_InvalidatesCoverAvailabilityIndex covers a scrape that
// adds artwork to a title the cover index has already been built for.
//
// The index is what media.search and media.history answer hasCover from. A
// scrape writes image properties straight through ApplyScrapeResult, and the
// only invalidation on that path recorded systems for the *thumbnail* cache,
// which is a different cache. The index therefore kept its pre-scrape answer
// and every freshly scraped cover reported hasCover=false until Core restarted.
//
// Observed on a MiSTer: after the mister-docs scraper matched 5 titles and
// wrote image-boxart properties for all of them, media.search reported
// hasCover=false for each; restarting Core flipped them all to true.
func TestApplyScrapeResult_InvalidatesCoverAvailabilityIndex(t *testing.T) {
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()
	seedImagePropertyTags(t, mediaDB)

	ctx := context.Background()
	sys, err := mediaDB.FindOrInsertSystem(database.System{SystemID: "NES", Name: "NES"})
	require.NoError(t, err)
	nesSystem, err := systemdefs.GetSystem("NES")
	require.NoError(t, err)

	require.NoError(t, mediaDB.BeginTransaction(false))
	title, err := mediaDB.InsertMediaTitle(&database.MediaTitle{
		SystemDBID: sys.DBID,
		Slug:       slugs.Slugify(nesSystem.GetMediaType(), "Scraped Cover"),
		Name:       "Scraped Cover",
	})
	require.NoError(t, err)
	media, err := mediaDB.InsertMedia(database.Media{
		SystemDBID:     sys.DBID,
		MediaTitleDBID: title.DBID,
		Path:           filepath.Join("roms", "nes", "scraped_cover.nes"),
		ParentDir:      filepath.ToSlash(filepath.Join("roms", "nes")) + "/",
	})
	require.NoError(t, err)
	require.NoError(t, mediaDB.CommitTransaction())

	// Build the index while the title has no artwork, so it caches "no cover".
	firstPass := []database.SearchResultWithCursor{{MediaID: media.DBID, MediaTitleID: title.DBID}}
	require.NoError(t, fetchAndAttachCoverFlags(ctx, mediaDB.sql.Load(), firstPass))
	require.False(t, firstPass[0].HasCover)
	mediaDB.WaitForBackgroundOperations()
	require.NotNil(t, cachedCoverAvailabilityIndex(mediaDB.sql.Load()),
		"the index must be built for this test to mean anything")

	// A scrape writes title artwork, exactly as the mister-docs scraper does.
	require.NoError(t, mediaDB.ApplyScrapeResult(ctx, media.DBID, title.DBID, &database.ScrapeWrite{
		Sentinel: database.TagInfo{Type: "scraper.test", Tag: "scraped"},
		TitleProps: []database.MediaProperty{{
			TypeTag: tags.PropertyTypeTag(tags.TagPropertyImageBoxart),
			Text:    filepath.ToSlash(filepath.Join("docs", "NES", "Artwork", "scraped.png")),
		}},
	}))

	assert.Nil(t, cachedCoverAvailabilityIndex(mediaDB.sql.Load()),
		"a scrape that writes artwork must invalidate the cover index")

	statuses, err := mediaDB.GetMediaCoverStatus(ctx, []database.MediaRef{
		{MediaDBID: media.DBID, MediaTitleDBID: title.DBID},
	})
	require.NoError(t, err)
	assert.Equal(t, map[int64]bool{media.DBID: true}, statuses,
		"scraped artwork must be visible without restarting Core")
}
