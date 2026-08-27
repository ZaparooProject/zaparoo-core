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
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	assertmock "github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type batchMockMediaDB struct {
	*testhelpers.MockMediaDBI
	batchErr error
	batches  [][]database.ScrapeWriteTarget
}

func (m *batchMockMediaDB) ApplyScrapeResults(
	_ context.Context,
	targets []database.ScrapeWriteTarget,
) error {
	batch := append([]database.ScrapeWriteTarget(nil), targets...)
	m.batches = append(m.batches, batch)
	return m.batchErr
}

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
	pl.On("RootDirs", assertmock.Anything).Return([]string{root}).Once()
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

func TestPlatformScraper_ValidatesDependenciesAndIndexLookup(t *testing.T) {
	t.Parallel()

	platformScraper := NewPlatformScraper()
	ch := make(chan scraper.ScrapeUpdate, 1)
	err := platformScraper.Scrape(
		context.Background(), nil, nil, afero.NewMemMapFs(), &database.Database{},
		scraper.ScrapeOptions{}, nil, ch,
	)
	require.ErrorContains(t, err, "platform and media database are required")

	pl := mocks.NewMockPlatform()
	pl.On("RootDirs", assertmock.Anything).Return([]string{}).Once()
	mediaDB := testhelpers.NewMockMediaDBI()
	mediaDB.On("IndexedSystems").Return(nil, errors.New("index lookup failed")).Once()
	err = platformScraper.Scrape(
		context.Background(), nil, pl, afero.NewMemMapFs(), &database.Database{MediaDB: mediaDB},
		scraper.ScrapeOptions{}, nil, ch,
	)
	require.ErrorContains(t, err, "list indexed systems")
	require.ErrorContains(t, err, "index lookup failed")
	pl.AssertExpectations(t)
	mediaDB.AssertExpectations(t)
}

func TestScrapeLoop_ReportsFatalDatabaseLoadErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		wantMessage string
		titleFails  bool
	}{
		{name: "title load", wantMessage: "load titles", titleFails: true},
		{name: "media load", wantMessage: "load media"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mediaDB := testhelpers.NewMockMediaDBI()
			if tt.titleFails {
				mediaDB.On("GetTitlesBySystemID", systemdefs.SystemSNES).
					Return(nil, errors.New("load failed")).Once()
			} else {
				mediaDB.On("GetTitlesBySystemID", systemdefs.SystemSNES).
					Return([]database.TitleWithSystem{}, nil).Once()
				mediaDB.On("GetMediaBySystemID", systemdefs.SystemSNES).
					Return(nil, errors.New("load failed")).Once()
			}
			impl := &scraperImpl{
				fs: afero.NewMemMapFs(), db: mediaDB,
				sources: map[string][]sourceDir{
					systemdefs.SystemSNES: {{Path: "source", SystemID: systemdefs.SystemSNES}},
				},
			}
			ch := make(chan scraper.ScrapeUpdate, 2)

			impl.scrapeLoop(context.Background(), scraper.ScrapeOptions{}, []string{systemdefs.SystemSNES}, ch)

			updates := make([]scraper.ScrapeUpdate, 0, 1)
			for update := range ch {
				updates = append(updates, update)
			}
			require.Len(t, updates, 1)
			require.ErrorContains(t, updates[0].FatalErr, tt.wantMessage)
			assert.True(t, updates[0].Done)
			mediaDB.AssertExpectations(t)
		})
	}
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
	mediaDB.AssertNotCalled(t, "GetMediaPropertyMetadataByMediaDBIDs", assertmock.Anything, assertmock.Anything)
	mediaDB.AssertNotCalled(
		t, "GetMediaTitlePropertyMetadataByMediaTitleDBIDs", assertmock.Anything, assertmock.Anything,
	)
	mediaDB.AssertExpectations(t)
}

func TestScrapeLoop_AccumulatesSourceLoadFailures(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	docsRoot := filepath.Join("media", "fat", "docs")
	firstSource := filepath.Join(docsRoot, "SNES", "Artwork One")
	secondSource := filepath.Join(docsRoot, "SNES", "Artwork Two")
	require.NoError(t, fs.MkdirAll(firstSource, 0o750))
	require.NoError(t, fs.MkdirAll(secondSource, 0o750))

	mediaDB := testhelpers.NewMockMediaDBI()
	mediaDB.On("GetTitlesBySystemID", systemdefs.SystemSNES).Return([]database.TitleWithSystem{}, nil)
	mediaDB.On("GetMediaBySystemID", systemdefs.SystemSNES).Return([]database.MediaWithFullPath{}, nil)

	impl := &scraperImpl{
		fs: fs, db: mediaDB, docsRoots: []string{docsRoot},
		sources: map[string][]sourceDir{systemdefs.SystemSNES: {
			{Path: firstSource, SystemID: systemdefs.SystemSNES, Kind: sourceArtwork},
			{Path: secondSource, SystemID: systemdefs.SystemSNES, Kind: sourceArtwork},
		}},
	}
	ch := make(chan scraper.ScrapeUpdate, 4)
	impl.scrapeLoop(context.Background(), scraper.ScrapeOptions{}, []string{systemdefs.SystemSNES}, ch)

	var updates []scraper.ScrapeUpdate
	for update := range ch {
		updates = append(updates, update)
	}
	require.Len(t, updates, 2)
	require.Error(t, updates[0].Err)
	require.ErrorContains(t, updates[0].Err, firstSource)
	require.ErrorContains(t, updates[0].Err, secondSource)
	mediaDB.AssertExpectations(t)
}

func TestScrapeLoop_ForceCleanupCountsDeletionAsProcessed(t *testing.T) {
	t.Parallel()

	docsRoot := filepath.Join("media", "fat", "docs")
	stalePath := filepath.Join(docsRoot, "SNES", artworkDirName, "Stale.jpg")
	staleManual := filepath.Join(docsRoot, "SNES", "Manuals", "Stale.pdf")
	media := []database.MediaWithFullPath{{DBID: 100, MediaTitleDBID: 10, Path: "Game.sfc"}}
	titles := []database.TitleWithSystem{{DBID: 10, Slug: "game", Name: "Game", SystemID: systemdefs.SystemSNES}}

	mediaDB := testhelpers.NewMockMediaDBI()
	mediaDB.On("GetTitlesBySystemID", systemdefs.SystemSNES).Return(titles, nil)
	mediaDB.On("GetMediaBySystemID", systemdefs.SystemSNES).Return(media, nil)
	mediaDB.On("GetMediaPropertyMetadataByMediaDBIDs", assertmock.Anything, []int64{100}).Return(
		map[int64][]database.MediaProperty{100: {{
			TypeTagDBID: 1,
			TypeTag:     tags.PropertyTypeTag(tags.TagPropertyImageBoxart),
			Text:        filepath.ToSlash(stalePath),
		}}}, nil,
	).Once()
	mediaDB.On("GetMediaTitlePropertyMetadataByMediaTitleDBIDs", assertmock.Anything, []int64{10}).Return(
		map[int64][]database.MediaProperty{10: {{
			TypeTagDBID: 2,
			TypeTag:     tags.PropertyTypeTag(tags.TagPropertyManual),
			Text:        filepath.ToSlash(staleManual),
		}}}, nil,
	).Once()
	mediaDB.On("DeleteMediaProperty", assertmock.Anything, int64(100), int64(1)).Return(nil).Once()
	mediaDB.On("DeleteMediaTitleProperty", assertmock.Anything, int64(10), int64(2)).Return(nil).Once()

	impl := &scraperImpl{
		fs: afero.NewMemMapFs(), db: mediaDB, docsRoots: []string{docsRoot},
		sources: map[string][]sourceDir{},
	}
	ch := make(chan scraper.ScrapeUpdate, 4)
	impl.scrapeLoop(
		context.Background(), scraper.ScrapeOptions{Force: true}, []string{systemdefs.SystemSNES}, ch,
	)

	var updates []scraper.ScrapeUpdate
	for update := range ch {
		updates = append(updates, update)
	}
	require.Len(t, updates, 2)
	assert.Equal(t, 2, updates[0].Processed)
	assert.Equal(t, 2, updates[0].Total)
	assert.Equal(t, 2, updates[0].Matched)
	assert.LessOrEqual(t, updates[0].Matched, updates[0].Total)
	mediaDB.AssertNotCalled(t, "GetMediaPropertyMetadata", assertmock.Anything, assertmock.Anything)
	mediaDB.AssertNotCalled(t, "GetMediaTitlePropertyMetadata", assertmock.Anything, assertmock.Anything)
	mediaDB.AssertExpectations(t)
}

func TestIsStaleDocsProperty_RestrictsCleanupScope(t *testing.T) {
	t.Parallel()

	docsRoot := filepath.Join("media", "fat", "docs")
	artworkPath := filepath.Join(docsRoot, "SNES", artworkDirName, "Game.jpg")
	manualPath := filepath.Join(docsRoot, "SNES", "Manuals", "Game.pdf")
	tests := []struct {
		name    string
		text    string
		typeTag string
		found   bool
		want    bool
	}{
		{name: "empty path", typeTag: tags.PropertyTypeTag(tags.TagPropertyImageBoxart)},
		{name: "unmanaged type", text: filepath.ToSlash(artworkPath), typeTag: "property:description"},
		{
			name: "current artwork", text: filepath.ToSlash(artworkPath),
			typeTag: tags.PropertyTypeTag(tags.TagPropertyImageBoxart), found: true,
		},
		{
			name: "outside docs", text: filepath.ToSlash(filepath.Join("media", "other", artworkDirName, "Game.jpg")),
			typeTag: tags.PropertyTypeTag(tags.TagPropertyImageBoxart),
		},
		{
			name: "wrong artwork folder", text: filepath.ToSlash(filepath.Join(docsRoot, "SNES", "Images", "Game.jpg")),
			typeTag: tags.PropertyTypeTag(tags.TagPropertyImageBoxart),
		},
		{
			name: "stale artwork", text: filepath.ToSlash(artworkPath),
			typeTag: tags.PropertyTypeTag(tags.TagPropertyImageBoxart), want: true,
		},
		{
			name: "stale manual", text: filepath.ToSlash(manualPath),
			typeTag: tags.PropertyTypeTag(tags.TagPropertyManual), want: true,
		},
	}
	impl := &scraperImpl{docsRoots: []string{docsRoot}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			found := make(map[string]struct{})
			if tt.found {
				found[filepath.Clean(filepath.FromSlash(tt.text))] = struct{}{}
			}
			prop := database.MediaProperty{TypeTag: tt.typeTag, Text: tt.text}
			assert.Equal(t, tt.want, impl.isStaleDocsProperty(&prop, found))
		})
	}
}

func TestDeleteStaleProperties_StopsForCancellation(t *testing.T) {
	t.Parallel()

	docsRoot := filepath.Join("media", "fat", "docs")
	staleArtwork := database.MediaProperty{
		TypeTagDBID: 1,
		TypeTag:     tags.PropertyTypeTag(tags.TagPropertyImageBoxart),
		Text:        filepath.ToSlash(filepath.Join(docsRoot, "SNES", artworkDirName, "Stale.jpg")),
	}
	staleManual := database.MediaProperty{
		TypeTagDBID: 2,
		TypeTag:     tags.PropertyTypeTag(tags.TagPropertyManual),
		Text:        filepath.ToSlash(filepath.Join(docsRoot, "SNES", "Manuals", "Stale.pdf")),
	}
	media := []database.MediaWithFullPath{{DBID: 100, MediaTitleDBID: 10}}
	titles := []database.TitleWithSystem{{DBID: 10}}

	t.Run("before metadata lookup", func(t *testing.T) {
		mediaDB := testhelpers.NewMockMediaDBI()
		impl := &scraperImpl{db: mediaDB, docsRoots: []string{docsRoot}}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		deleted, err := impl.deleteStaleProperties(ctx, scraper.ScrapeOptions{}, media, titles, nil)
		assert.Zero(t, deleted)
		require.ErrorIs(t, err, context.Canceled)
		mediaDB.AssertNotCalled(t, "GetMediaPropertyMetadataByMediaDBIDs", assertmock.Anything, assertmock.Anything)
		mediaDB.AssertNotCalled(
			t, "GetMediaTitlePropertyMetadataByMediaTitleDBIDs", assertmock.Anything, assertmock.Anything,
		)
	})

	t.Run("before media iteration", func(t *testing.T) {
		mediaDB := testhelpers.NewMockMediaDBI()
		ctx, cancel := context.WithCancel(context.Background())
		mediaDB.On("GetMediaPropertyMetadataByMediaDBIDs", assertmock.Anything, []int64{100}).Return(
			map[int64][]database.MediaProperty{100: {staleArtwork}}, nil,
		).Once()
		mediaDB.On("GetMediaTitlePropertyMetadataByMediaTitleDBIDs", assertmock.Anything, []int64{10}).Run(
			func(_ assertmock.Arguments) { cancel() },
		).Return(map[int64][]database.MediaProperty{10: {staleManual}}, nil).Once()
		impl := &scraperImpl{db: mediaDB, docsRoots: []string{docsRoot}}

		deleted, err := impl.deleteStaleProperties(ctx, scraper.ScrapeOptions{}, media, titles, nil)
		assert.Zero(t, deleted)
		require.ErrorIs(t, err, context.Canceled)
		mediaDB.AssertNotCalled(t, "DeleteMediaProperty", assertmock.Anything, assertmock.Anything, assertmock.Anything)
		mediaDB.AssertExpectations(t)
	})

	t.Run("before title iteration", func(t *testing.T) {
		mediaDB := testhelpers.NewMockMediaDBI()
		ctx, cancel := context.WithCancel(context.Background())
		mediaDB.On("GetMediaPropertyMetadataByMediaDBIDs", assertmock.Anything, []int64{100}).Return(
			map[int64][]database.MediaProperty{100: {staleArtwork}}, nil,
		).Once()
		mediaDB.On("GetMediaTitlePropertyMetadataByMediaTitleDBIDs", assertmock.Anything, []int64{10}).Return(
			map[int64][]database.MediaProperty{10: {staleManual}}, nil,
		).Once()
		mediaDB.On("DeleteMediaProperty", assertmock.Anything, int64(100), int64(1)).Run(
			func(_ assertmock.Arguments) { cancel() },
		).Return(nil).Once()
		impl := &scraperImpl{db: mediaDB, docsRoots: []string{docsRoot}}

		deleted, err := impl.deleteStaleProperties(ctx, scraper.ScrapeOptions{}, media, titles, nil)
		assert.Equal(t, 1, deleted)
		require.ErrorIs(t, err, context.Canceled)
		mediaDB.AssertNotCalled(
			t, "DeleteMediaTitleProperty", assertmock.Anything, assertmock.Anything, assertmock.Anything,
		)
		mediaDB.AssertExpectations(t)
	})
}

func TestScrapeLoop_BatchWritesAndFallback(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		wantSkipped int
		batchFails  bool
		writeFails  bool
	}{
		{name: "batch success"},
		{
			name: "batch failure falls back and preserves write error", wantSkipped: 1,
			batchFails: true, writeFails: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := afero.NewMemMapFs()
			docsRoot := filepath.Join("media", "fat", "docs")
			sourcePath := filepath.Join(docsRoot, "SNES", artworkDirName)
			require.NoError(t, fs.MkdirAll(sourcePath, 0o750))
			require.NoError(t, afero.WriteFile(fs, filepath.Join(sourcePath, indexFileName),
				[]byte("#name\tkey\nGame\tGame\n"), 0o600))
			require.NoError(t, afero.WriteFile(fs, filepath.Join(sourcePath, "Game.jpg"), []byte("image"), 0o600))

			baseDB := testhelpers.NewMockMediaDBI()
			baseDB.On("GetTitlesBySystemID", systemdefs.SystemSNES).Return([]database.TitleWithSystem{
				{DBID: 10, Slug: "game", Name: "Game", SystemID: systemdefs.SystemSNES},
			}, nil)
			baseDB.On("GetMediaBySystemID", systemdefs.SystemSNES).Return([]database.MediaWithFullPath{
				{DBID: 100, MediaTitleDBID: 10, Path: "/games/SNES/Game.sfc"},
			}, nil)
			var writeErr error
			if tt.writeFails {
				writeErr = errors.New("write failed")
				baseDB.On("ApplyScrapeResult", assertmock.Anything, int64(100), int64(10), assertmock.Anything).
					Return(writeErr).Once()
			}
			var batchErr error
			if tt.batchFails {
				batchErr = errors.New("batch failed")
			}
			mediaDB := &batchMockMediaDB{MockMediaDBI: baseDB, batchErr: batchErr}

			impl := &scraperImpl{
				fs: fs, db: mediaDB, docsRoots: []string{docsRoot},
				sources: map[string][]sourceDir{
					systemdefs.SystemSNES: {{
						Path: sourcePath, SystemID: systemdefs.SystemSNES, Kind: sourceArtwork,
					}},
				},
			}
			ch := make(chan scraper.ScrapeUpdate, 4)
			impl.scrapeLoop(context.Background(), scraper.ScrapeOptions{}, []string{systemdefs.SystemSNES}, ch)

			var updates []scraper.ScrapeUpdate
			for update := range ch {
				updates = append(updates, update)
			}
			require.Len(t, updates, 2)
			require.Len(t, mediaDB.batches, 1)
			require.Len(t, mediaDB.batches[0], 1)
			if writeErr == nil {
				require.NoError(t, updates[0].Err)
				baseDB.AssertNotCalled(
					t, "ApplyScrapeResult",
					assertmock.Anything, assertmock.Anything, assertmock.Anything, assertmock.Anything,
				)
			} else {
				require.ErrorContains(t, updates[0].Err, writeErr.Error())
			}
			assert.Equal(t, tt.wantSkipped, updates[0].Skipped)
			assert.Equal(t, 1, updates[0].CurrentStep)
			assert.True(t, updates[1].Done)
			baseDB.AssertExpectations(t)
		})
	}
}
