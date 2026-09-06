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
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/stretchr/testify/require"
)

func TestGetScrapeMediaScope(t *testing.T) {
	t.Parallel()
	db, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()
	root := filepath.ToSlash(t.TempDir())
	path := func(parts ...string) string {
		return filepath.ToSlash(filepath.Join(append([]string{root}, parts...)...))
	}
	paths := []string{
		path("foo", "a.nes"), path("foo", "nested", "b.nes"), path("foobar", "c.nes"),
		path("f%_o", "d.nes"), "steam://123", path("foo", "missing.nes"),
	}
	_, clearErr := db.sql.Load().ExecContext(ctx, "DELETE FROM Media")
	require.NoError(t, clearErr)
	for i, p := range paths {
		_, err := db.sql.Load().ExecContext(ctx,
			"INSERT INTO Media (DBID, MediaTitleDBID, SystemDBID, Path, IsMissing) VALUES (?, 1, 1, ?, ?)",
			i+1, p, i == 5)
		require.NoError(t, err)
	}
	for _, tt := range []struct {
		name  string
		ids   []int64
		scope database.ScrapeScope
	}{
		{"id", []int64{1}, database.ScrapeScope{SystemID: "NES", Path: paths[0], MediaID: 1}},
		{"subtree", []int64{1, 2}, database.ScrapeScope{SystemID: "NES", Path: path("foo"), Subtree: true}},
		{"root", []int64{1, 2, 3, 4}, database.ScrapeScope{SystemID: "NES", Path: root, Subtree: true}},
		{"wildcards literal", []int64{4}, database.ScrapeScope{SystemID: "NES", Path: path("f%_o"), Subtree: true}},
		{"uri", []int64{5}, database.ScrapeScope{SystemID: "NES", Path: paths[4], MediaID: 5}},
		{"case sensitive", nil, database.ScrapeScope{SystemID: "NES", Path: path("FOO"), Subtree: true}},
		{"volume root", []int64{1, 2, 3, 4}, database.ScrapeScope{
			SystemID: "NES", Path: filepath.ToSlash(filepath.VolumeName(root)) + "/", Subtree: true,
		}},
		{"zero", nil, database.ScrapeScope{SystemID: "NES", Path: path("absent"), Subtree: true}},
		{"missing", nil, database.ScrapeScope{SystemID: "NES", Path: paths[5], MediaID: 6}},
		{"identity guard", nil, database.ScrapeScope{SystemID: "NES", Path: paths[1], MediaID: 1}},
		{"system guard", nil, database.ScrapeScope{SystemID: "SNES", Path: paths[0], MediaID: 1}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rows, err := db.GetScrapeMedia(ctx, tt.scope)
			require.NoError(t, err)
			var ids []int64
			for _, row := range rows {
				ids = append(ids, row.DBID)
				require.Equal(t, "mario", row.Title.Slug)
			}
			require.ElementsMatch(t, tt.ids, ids)
		})
	}
	_, err := db.GetScrapeMedia(ctx, database.ScrapeScope{})
	require.Error(t, err)
	for _, scope := range []*database.ScrapeScope{
		nil,
		{SystemID: "NES", Path: paths[0], MediaID: 1},
		{SystemID: "NES", Path: path("foo"), Subtree: true},
	} {
		operation := database.ScrapingOperation{Scope: scope, ScraperID: "test", Systems: []string{"NES"}, Force: true}
		require.NoError(t, db.SetScrapingOperation(operation))
		stored, found, getErr := db.GetScrapingOperation()
		require.NoError(t, getErr)
		require.True(t, found)
		require.Equal(t, operation, stored)
	}
	for _, id := range []int64{1, 3} {
		require.NoError(t, db.UpsertMediaTags(ctx, id, []database.TagInfo{
			{Type: "scraper.test", Tag: "scraped"}, {Type: string(tags.ScraperRunType("test")), Tag: "run"},
		}))
	}
	for _, runID := range []string{"", "run", "different"} {
		ids, err := db.GetScopedScrapeMediaIDs(ctx,
			database.ScrapeScope{SystemID: "NES", Path: path("foo"), Subtree: true}, "test", runID)
		require.NoError(t, err)
		if runID == "different" {
			require.Empty(t, ids)
		} else {
			require.Equal(t, map[int64]struct{}{1: {}}, ids)
		}
	}
}
