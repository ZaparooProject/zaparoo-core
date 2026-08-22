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
	"errors"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	assertmock "github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestPlatformScraper_EndToEndLocalArtwork(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	root := filepath.Join("media", "fat")
	dir := filepath.Join(root, "docs", "SNES", "Artwork")
	require.NoError(t, fs.MkdirAll(dir, 0o750))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "index.tsv"),
		[]byte("#name\tkey\nGame (USA)\tGame\n"), 0o600))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "Game.jpg"), []byte("image"), 0o600))

	pl := mocks.NewMockPlatform()
	pl.On("RootDirs", assertmock.Anything).Return([]string{root})
	mediaDB := testhelpers.NewMockMediaDBI()
	mediaDB.On("IndexedSystems").Return([]string{systemdefs.SystemSNES}, nil)
	mediaDB.On("GetTitlesBySystemID", systemdefs.SystemSNES).Return([]database.TitleWithSystem{
		{DBID: 10, Slug: "game", Name: "Game", SystemID: systemdefs.SystemSNES},
	}, nil)
	mediaDB.On("GetMediaBySystemID", systemdefs.SystemSNES).Return([]database.MediaWithFullPath{
		{DBID: 100, MediaTitleDBID: 10, Path: "/games/SNES/Game (USA).sfc"},
	}, nil)
	expectedImagePath := filepath.ToSlash(filepath.Join(dir, "Game.jpg"))
	mediaDB.On("ApplyScrapeResult", assertmock.Anything, int64(100), int64(10), assertmock.MatchedBy(
		func(write *database.ScrapeWrite) bool {
			return len(write.MediaProps) == 1 && write.MediaProps[0].Text == expectedImagePath
		},
	)).Return(nil).Once()

	platformScraper := NewPlatformScraper()
	ch := make(chan scraper.ScrapeUpdate, 8)
	err := platformScraper.Scrape(
		context.Background(), nil, pl, fs, &database.Database{MediaDB: mediaDB},
		scraper.ScrapeOptions{}, nil, ch,
	)
	require.NoError(t, err)

	var updates []scraper.ScrapeUpdate
	for update := range ch {
		updates = append(updates, update)
	}
	require.NotEmpty(t, updates)
	assert.True(t, updates[len(updates)-1].Done)
	assert.Equal(t, 1, updates[len(updates)-1].Matched)
	mediaDB.AssertExpectations(t)
	pl.AssertExpectations(t)
}

func TestScrapeLoop_ForceSkipsCleanupAfterSourceLoadFailure(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	docsRoot := filepath.Join("media", "fat", "docs")
	sourcePath := filepath.Join(docsRoot, "SNES", artworkDirName)
	require.NoError(t, fs.MkdirAll(sourcePath, 0o750))

	mediaDB := testhelpers.NewMockMediaDBI()
	mediaDB.On("GetTitlesBySystemID", systemdefs.SystemSNES).Return([]database.TitleWithSystem{
		{DBID: 10, Slug: "game", Name: "Game", SystemID: systemdefs.SystemSNES},
	}, nil)
	mediaDB.On("GetMediaBySystemID", systemdefs.SystemSNES).Return([]database.MediaWithFullPath{
		{DBID: 100, MediaTitleDBID: 10, Path: "/games/SNES/Game.sfc"},
	}, nil)

	impl := &scraperImpl{
		fs:        fs,
		db:        mediaDB,
		docsRoots: []string{docsRoot},
		sources: map[string][]sourceDir{
			systemdefs.SystemSNES: {{Path: sourcePath, SystemID: systemdefs.SystemSNES, Kind: sourceArtwork}},
		},
	}
	ch := make(chan scraper.ScrapeUpdate, 4)
	impl.scrapeLoop(context.Background(), scraper.ScrapeOptions{Force: true}, []string{systemdefs.SystemSNES}, ch)

	var updates []scraper.ScrapeUpdate
	for update := range ch {
		updates = append(updates, update)
	}
	require.Len(t, updates, 2)
	require.Error(t, updates[0].Err)
	require.ErrorContains(t, updates[0].Err, "parse artwork index")
	assert.True(t, updates[1].Done)
	mediaDB.AssertNotCalled(t, "GetMediaPropertyMetadata", assertmock.Anything, assertmock.Anything)
	mediaDB.AssertNotCalled(t, "GetMediaTitlePropertyMetadata", assertmock.Anything, assertmock.Anything)
	mediaDB.AssertExpectations(t)
}

func TestScrapeLoop_WriteFailureEmitsSingleStepUpdate(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	docsRoot := filepath.Join("media", "fat", "docs")
	sourcePath := filepath.Join(docsRoot, "SNES", artworkDirName)
	require.NoError(t, fs.MkdirAll(sourcePath, 0o750))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(sourcePath, indexFileName),
		[]byte("#name\tkey\nGame\tGame\n"), 0o600))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(sourcePath, "Game.jpg"), []byte("image"), 0o600))

	mediaDB := testhelpers.NewMockMediaDBI()
	mediaDB.On("GetTitlesBySystemID", systemdefs.SystemSNES).Return([]database.TitleWithSystem{
		{DBID: 10, Slug: "game", Name: "Game", SystemID: systemdefs.SystemSNES},
	}, nil)
	mediaDB.On("GetMediaBySystemID", systemdefs.SystemSNES).Return([]database.MediaWithFullPath{
		{DBID: 100, MediaTitleDBID: 10, Path: "/games/SNES/Game.sfc"},
	}, nil)
	mediaDB.On("ApplyScrapeResult", assertmock.Anything, int64(100), int64(10), assertmock.Anything).
		Return(errors.New("write failed")).Once()

	impl := &scraperImpl{
		fs:        fs,
		db:        mediaDB,
		docsRoots: []string{docsRoot},
		sources: map[string][]sourceDir{
			systemdefs.SystemSNES: {{Path: sourcePath, SystemID: systemdefs.SystemSNES, Kind: sourceArtwork}},
		},
	}
	ch := make(chan scraper.ScrapeUpdate, 4)
	impl.scrapeLoop(context.Background(), scraper.ScrapeOptions{}, []string{systemdefs.SystemSNES}, ch)

	var updates []scraper.ScrapeUpdate
	for update := range ch {
		updates = append(updates, update)
	}
	require.Len(t, updates, 2)
	require.Error(t, updates[0].Err)
	require.ErrorContains(t, updates[0].Err, "write failed")
	assert.Equal(t, 1, updates[0].CurrentStep)
	assert.True(t, updates[1].Done)
	mediaDB.AssertExpectations(t)
}
