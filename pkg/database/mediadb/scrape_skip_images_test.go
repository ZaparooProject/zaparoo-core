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

package mediadb_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediadb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func propText(t *testing.T, props []database.MediaProperty) map[string]string {
	t.Helper()
	out := make(map[string]string, len(props))
	for _, p := range props {
		out[p.TypeTag] = p.Text
	}
	return out
}

// skipImagesWrite carries art the run must drop, metadata it must keep, the
// sentinel it must not write and the run marker it must still write.
func skipImagesWrite() *database.ScrapeWrite {
	return &database.ScrapeWrite{
		Sentinel:  database.TagInfo{Type: string(tags.ScraperType("test")), Tag: string(tags.TagScraperScraped)},
		MediaTags: []database.TagInfo{{Type: string(tags.ScraperRunType("test")), Tag: "run-1"}},
		TitleTags: []database.TagInfo{{Type: string(tags.TagTypeDeveloper), Tag: "konami"}},
		MediaProps: []database.MediaProperty{
			{TypeTag: tags.PropertyTypeTag(tags.TagPropertyImageBoxart), Text: "/new/boxart.png"},
			{TypeTag: tags.PropertyTypeTag(tags.TagPropertyVideo), Text: "/new/video.mp4"},
		},
		TitleProps: []database.MediaProperty{
			{TypeTag: tags.PropertyTypeTag(tags.TagPropertyDescription), Text: "new description"},
			{TypeTag: tags.PropertyTypeTag(tags.TagPropertyManual), Text: "/new/manual.pdf"},
			{TypeTag: tags.PropertyTypeTag(tags.TagPropertyImageMarquee), Text: "/new/marquee.png"},
		},
		SkipImages: true,
	}
}

// TestApplyScrapeSkipImagesWritesMetadataOnly covers the write-path guard for
// a metadata-only run: art is dropped whatever the scraper sent, the art
// already stored stays, metadata and the run marker land, and the record is
// not marked done so a later run can still add the art.
func TestApplyScrapeSkipImagesWritesMetadataOnly(t *testing.T) {
	t.Parallel()
	type applyFunc func(ctx context.Context, db *mediadb.MediaDB, mediaID, titleID int64, w *database.ScrapeWrite) error
	apply := map[string]applyFunc{
		"single": func(ctx context.Context, db *mediadb.MediaDB, media, title int64, w *database.ScrapeWrite) error {
			return db.ApplyScrapeResult(ctx, media, title, w)
		},
		"batch": func(ctx context.Context, db *mediadb.MediaDB, media, title int64, w *database.ScrapeWrite) error {
			return db.ApplyScrapeResults(ctx, []database.ScrapeWriteTarget{
				{MediaDBID: media, MediaTitleDBID: title, Write: w},
			})
		},
	}
	for name, applyWrite := range apply {
		for _, fillMissing := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/fillMissing=%t", name, fillMissing), func(t *testing.T) {
				t.Parallel()
				ctx := context.Background()
				mediaDB, cleanup := helpers.NewInMemoryMediaDB(t)
				t.Cleanup(cleanup)
				mediaID, titleID := vocabularyTestMedia(t, mediaDB)
				require.NoError(t, mediaDB.ApplyScrapeResult(ctx, mediaID, titleID, &database.ScrapeWrite{
					MediaProps: []database.MediaProperty{
						{TypeTag: tags.PropertyTypeTag(tags.TagPropertyImageBoxart), Text: "/old/boxart.png"},
					},
				}))

				write := skipImagesWrite()
				write.FillMissing = fillMissing
				require.NoError(t, applyWrite(ctx, mediaDB, mediaID, titleID, write))

				mediaProps, err := mediaDB.GetMediaProperties(ctx, mediaID)
				require.NoError(t, err)
				assert.Equal(t, map[string]string{
					tags.PropertyTypeTag(tags.TagPropertyImageBoxart): "/old/boxart.png",
				}, propText(t, mediaProps), "art already stored stays and no new art is written")
				titleProps, err := mediaDB.GetMediaTitleProperties(ctx, titleID)
				require.NoError(t, err)
				assert.Equal(t, map[string]string{
					tags.PropertyTypeTag(tags.TagPropertyDescription): "new description",
				}, propText(t, titleProps))
				assert.Equal(t, []string{"developer:konami"},
					storedTags(t, mediaDB, "MediaTitleTags", "MediaTitleDBID", titleID))
				assert.Equal(t, []string{"scraper-run.test:run-1"},
					storedTags(t, mediaDB, "MediaTags", "MediaDBID", mediaID),
					"the run marker is written and the sentinel is not")
				assert.Len(t, write.MediaProps, 2, "the caller's write must not be modified")
				assert.NotEmpty(t, write.Sentinel.Type, "the caller's write must not be modified")
			})
		}
	}
}
