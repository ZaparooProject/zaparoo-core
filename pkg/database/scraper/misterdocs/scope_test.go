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

package misterdocs

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestScopedMiSTerDocsForceCleanup(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	path := filepath.ToSlash(filepath.Join(root, "Game.sfc"))
	docsRoot := filepath.Join(root, "docs")
	sourcePath := filepath.Join(docsRoot, "SNES", "Manuals")
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll(sourcePath, 0o750))
	mdb := helpers.NewMockMediaDBI()
	scope := &database.ScrapeScope{SystemID: "SNES", Path: path, MediaID: 1}
	mdb.On("GetScrapeMedia", mock.Anything, *scope).Return([]database.MediaFullRow{{
		Media:  database.Media{DBID: 1, MediaTitleDBID: 10, Path: path},
		Title:  database.MediaTitle{DBID: 10, Name: "Game", Slug: "game"},
		System: database.System{DBID: 100, SystemID: "SNES"},
	}}, nil).Once()
	mdb.On("GetMediaPropertyMetadataByMediaDBIDs", mock.Anything, []int64{1}).Return(
		map[int64][]database.MediaProperty{1: {{
			TypeTagDBID: 1,
			TypeTag:     tags.PropertyTypeTag(tags.TagPropertyImageBoxart),
			Text:        filepath.ToSlash(filepath.Join(docsRoot, "SNES", artworkDirName, "Stale.jpg")),
		}}}, nil).Once()
	mdb.On("GetMediaTitlePropertyMetadataByMediaTitleDBIDs", mock.Anything, []int64{10}).Return(
		map[int64][]database.MediaProperty{}, nil).Once()
	mdb.On("DeleteMediaProperty", mock.Anything, int64(1), int64(1)).Return(nil).Once()
	s := &scraperImpl{fs: fs, db: mdb, docsRoots: []string{docsRoot}, sources: map[string][]sourceDir{
		"SNES": {{Path: sourcePath, SystemID: "SNES", Kind: sourceManuals}},
	}}
	ch := make(chan scraper.ScrapeUpdate, 16)
	s.scrapeLoop(context.Background(), scraper.ScrapeOptions{Scope: scope, Force: true}, []string{"SNES"}, ch)
	var final scraper.ScrapeUpdate
	for update := range ch {
		require.NoError(t, update.FatalErr)
		final = update
	}
	require.True(t, final.Done)
	require.Equal(t, 1, final.Total)
	require.Equal(t, 1, final.Processed)
	mdb.AssertExpectations(t)
}
