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
	"errors"
	"fmt"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// streamTestRows is more than one cancellation-check interval, so the
// cancellation tests see a check happen mid-stream.
const streamTestRows = 2*streamCancelCheckInterval + 10

func seedStreamSystem(t *testing.T, mediaDB *MediaDB) database.System {
	t.Helper()
	require.NoError(t, mediaDB.BeginTransaction(false))
	system, err := mediaDB.InsertSystem(database.System{SystemID: "C64", Name: "C64"})
	require.NoError(t, err)
	for i := range streamTestRows {
		title, err := mediaDB.InsertMediaTitle(&database.MediaTitle{
			SystemDBID: system.DBID, Slug: fmt.Sprintf("game%05d", i), Name: fmt.Sprintf("Game %05d", i),
		})
		require.NoError(t, err)
		// Inserted out of path order, so the stream's ordering is visible.
		_, err = mediaDB.InsertMedia(database.Media{
			SystemDBID: system.DBID, MediaTitleDBID: title.DBID,
			Path: fmt.Sprintf("/roms/c64/%05d.d64", streamTestRows-i), IsMissing: i%7 == 0,
		})
		require.NoError(t, err)
	}
	require.NoError(t, mediaDB.CommitTransaction())
	return system
}

func TestForEachSystemRowsMatchSliceLoads(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()
	system := seedStreamSystem(t, mediaDB)
	ctx := context.Background()

	wantMedia, err := mediaDB.GetMediaBySystemID("C64")
	require.NoError(t, err)
	var gotMedia []database.MediaWithFullPath
	require.NoError(t, mediaDB.ForEachMediaBySystemID(ctx, "C64", func(m *database.MediaWithFullPath) error {
		gotMedia = append(gotMedia, *m)
		return nil
	}))
	require.Len(t, wantMedia, streamTestRows)
	assert.Equal(t, wantMedia, gotMedia)

	wantTitles, err := mediaDB.GetTitlesBySystemID("C64")
	require.NoError(t, err)
	var gotTitles []database.TitleWithSystem
	require.NoError(t, mediaDB.ForEachTitleBySystemID(ctx, "C64", func(title *database.TitleWithSystem) error {
		gotTitles = append(gotTitles, *title)
		return nil
	}))
	require.Len(t, wantTitles, streamTestRows)
	assert.Equal(t, wantTitles, gotTitles)

	wantUnscraped, err := mediaDB.FindMediaTitlesWithoutSentinel(ctx, system.DBID, "scraper.test:scraped")
	require.NoError(t, err)
	var gotUnscraped []database.MediaTitle
	require.NoError(t, mediaDB.ForEachMediaTitleWithoutSentinel(ctx, system.DBID, "scraper.test:scraped",
		func(title *database.MediaTitle) error {
			gotUnscraped = append(gotUnscraped, *title)
			return nil
		}))
	require.Len(t, wantUnscraped, streamTestRows)
	assert.Equal(t, wantUnscraped, gotUnscraped)
}

func TestForEachSystemRowsStopOnCallbackError(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()
	system := seedStreamSystem(t, mediaDB)
	ctx := context.Background()
	stop := errors.New("stop")

	streams := map[string]func(fn func() error) error{
		"media": func(fn func() error) error {
			return mediaDB.ForEachMediaBySystemID(ctx, "C64", func(*database.MediaWithFullPath) error { return fn() })
		},
		"titles": func(fn func() error) error {
			return mediaDB.ForEachTitleBySystemID(ctx, "C64", func(*database.TitleWithSystem) error { return fn() })
		},
		"unscraped titles": func(fn func() error) error {
			return mediaDB.ForEachMediaTitleWithoutSentinel(ctx, system.DBID, "scraper.test:scraped",
				func(*database.MediaTitle) error { return fn() })
		},
	}
	for name, stream := range streams {
		calls := 0
		err := stream(func() error {
			calls++
			if calls == 3 {
				return stop
			}
			return nil
		})
		require.ErrorIs(t, err, stop, name)
		assert.Equal(t, 3, calls, "%s must stop at the failing row", name)
	}
}

// A cancelled scrape must not have to read the rest of a large system first.
func TestForEachSystemRowsStopOnCancel(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()
	system := seedStreamSystem(t, mediaDB)

	streams := map[string]func(ctx context.Context, fn func()) error{
		"media": func(ctx context.Context, fn func()) error {
			return mediaDB.ForEachMediaBySystemID(ctx, "C64", func(*database.MediaWithFullPath) error {
				fn()
				return nil
			})
		},
		"titles": func(ctx context.Context, fn func()) error {
			return mediaDB.ForEachTitleBySystemID(ctx, "C64", func(*database.TitleWithSystem) error {
				fn()
				return nil
			})
		},
		"unscraped titles": func(ctx context.Context, fn func()) error {
			return mediaDB.ForEachMediaTitleWithoutSentinel(ctx, system.DBID, "scraper.test:scraped",
				func(*database.MediaTitle) error {
					fn()
					return nil
				})
		},
	}
	for name, stream := range streams {
		ctx, cancel := context.WithCancel(context.Background())
		calls := 0
		err := stream(ctx, func() {
			calls++
			if calls == 5 {
				cancel()
			}
		})
		cancel()
		require.ErrorIs(t, err, context.Canceled, name)
		assert.Less(t, calls, streamTestRows, "%s must stop before the end of the system", name)
	}
}

// Paging from the empty path to the end yields exactly the full load's rows.
func TestGetMediaPageBySystemIDCoversSystemInOrder(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()
	seedStreamSystem(t, mediaDB)
	ctx := context.Background()

	want, err := mediaDB.GetMediaBySystemID("C64")
	require.NoError(t, err)
	const pageSize = 500
	var got []database.MediaWithFullPath
	after := ""
	for {
		page, pageErr := mediaDB.GetMediaPageBySystemID(ctx, "C64", after, pageSize)
		require.NoError(t, pageErr)
		require.LessOrEqual(t, len(page), pageSize)
		got = append(got, page...)
		if len(page) < pageSize {
			break
		}
		after = page[len(page)-1].Path
	}
	assert.Equal(t, want, got)

	empty, err := mediaDB.GetMediaPageBySystemID(ctx, "NES", "", pageSize)
	require.NoError(t, err)
	assert.Empty(t, empty)
}
