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

package localmedia

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestScrape_ImportsLocalMediaFolderArtwork(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	systemRoot := filepath.Join(root, "nes")
	romPath := filepath.Join(systemRoot, "Subdir", "Game.nes")
	missingPath := filepath.Join(systemRoot, "Missing.nes")
	boxartPath := filepath.Join(systemRoot, "media", "boxart", "Subdir", "Game.png")
	screenshotPath := filepath.Join(systemRoot, "media", "screenshots", "Subdir", "Game.jpg")
	fs := afero.NewMemMapFs()
	require.NoError(t, os.MkdirAll(systemRoot, 0o750))
	require.NoError(t, fs.MkdirAll(filepath.Dir(boxartPath), 0o750))
	require.NoError(t, fs.MkdirAll(filepath.Dir(screenshotPath), 0o750))
	require.NoError(t, afero.WriteFile(fs, boxartPath, []byte("boxart"), 0o600))
	require.NoError(t, afero.WriteFile(fs, screenshotPath, []byte("screenshot"), 0o600))

	cfg, err := testhelpers.NewTestConfig(nil, t.TempDir())
	require.NoError(t, err)
	pl := mocks.NewMockPlatform()
	pl.On("RootDirs", mock.AnythingOfType("*config.Instance")).Return([]string{root})
	pl.On("Launchers", mock.AnythingOfType("*config.Instance")).Return([]platforms.Launcher{{
		ID:         "nes",
		SystemID:   "NES",
		Folders:    []string{"nes"},
		Extensions: []string{".nes"},
	}})

	mockDB := testhelpers.NewMockMediaDBI()
	mockDB.On("IndexedSystems").Return([]string{"NES"}, nil)
	mockDB.On("FindSystemBySystemID", "NES").Return(database.System{DBID: 1, SystemID: "NES", Name: "NES"}, nil)
	mockDB.On("GetMediaBySystemID", "NES").Return([]database.MediaWithFullPath{
		{DBID: 11, MediaTitleDBID: 101, Path: romPath, SystemID: "NES"},
		{DBID: 12, MediaTitleDBID: 102, Path: missingPath, SystemID: "NES"},
	}, nil)
	mockDB.On(
		"ApplyScrapeResult",
		mock.Anything,
		int64(11),
		int64(101),
		mock.MatchedBy(func(write *database.ScrapeWrite) bool {
			if write == nil {
				return false
			}
			assert.Equal(t, scraper.SentinelTagInfo(scraperID), write.Sentinel)
			require.Len(t, write.MediaProps, 2)
			assert.Equal(t, tags.PropertyTypeTag(tags.TagPropertyImageBoxart), write.MediaProps[0].TypeTag)
			assert.Equal(t, filepath.ToSlash(boxartPath), write.MediaProps[0].Text)
			assert.Equal(t, "image/png", write.MediaProps[0].ContentType)
			assert.Equal(t, tags.PropertyTypeTag(tags.TagPropertyImageScreenshot), write.MediaProps[1].TypeTag)
			assert.Equal(t, filepath.ToSlash(screenshotPath), write.MediaProps[1].Text)
			assert.Equal(t, "image/jpeg", write.MediaProps[1].ContentType)
			return true
		}),
	).Return(nil).Once()

	ch := make(chan scraper.ScrapeUpdate, 32)
	s := NewPlatformScraper()
	err = s.Scrape(
		context.Background(), cfg, pl, fs, &database.Database{MediaDB: mockDB},
		scraper.ScrapeOptions{}, nil, ch,
	)
	require.NoError(t, err)

	var last scraper.ScrapeUpdate
	for update := range ch {
		last = update
	}
	assert.True(t, last.Done)
	mockDB.AssertExpectations(t)
	pl.AssertExpectations(t)
}

func TestOrderedScrapeSystemIDs(t *testing.T) {
	t.Parallel()

	assert.Equal(t,
		[]string{"SNES", "NES"},
		orderedScrapeSystemIDs([]string{"NES", "SNES", "GB"}, []string{"SNES", "NES", "SNES", "PSX"}),
	)
	assert.Equal(t,
		[]string{"NES", "SNES"},
		orderedScrapeSystemIDs([]string{"NES", "SNES"}, nil),
	)
}

func TestDeleteStaleLocalMediaProps_DeletesOnlyMissingLocalConventionProps(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	mediaPath := filepath.Join(root, "Game.nes")
	staleLocalPath := filepath.Join(root, "media", "boxart", "Game.png")
	keptLocalPath := filepath.Join(root, "media", "screenshots", "Game.jpg")
	foreignPath := filepath.Join(root, "custom-art", "Game.png")
	mockDB := testhelpers.NewMockMediaDBI()
	mockDB.On("GetMediaPropertyMetadata", mock.Anything, int64(11)).Return([]database.MediaProperty{
		{
			TypeTag:     tags.PropertyTypeTag(tags.TagPropertyImageBoxart),
			TypeTagDBID: 101,
			Text:        filepath.ToSlash(staleLocalPath),
		},
		{
			TypeTag:     tags.PropertyTypeTag(tags.TagPropertyImageScreenshot),
			TypeTagDBID: 102,
			Text:        filepath.ToSlash(keptLocalPath),
		},
		{
			TypeTag:     tags.PropertyTypeTag(tags.TagPropertyImageImage),
			TypeTagDBID: 103,
			Text:        filepath.ToSlash(foreignPath),
		},
	}, nil)
	mockDB.On("DeleteMediaProperty", mock.Anything, int64(11), int64(101)).Return(nil).Once()

	s := &scraperImpl{db: mockDB, fs: afero.NewMemMapFs()}
	media := &database.MediaWithFullPath{
		DBID:           11,
		MediaTitleDBID: 101,
		Path:           mediaPath,
		SystemID:       "NES",
	}
	deleted, err := s.deleteStaleLocalMediaProps(context.Background(), media, []string{root}, []database.MediaProperty{{
		TypeTag: tags.PropertyTypeTag(tags.TagPropertyImageScreenshot),
		Text:    filepath.ToSlash(keptLocalPath),
	}})

	require.NoError(t, err)
	assert.Equal(t, 1, deleted)
	mockDB.AssertExpectations(t)
}

func TestScrape_WriteErrorIncrementsProcessed(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	systemRoot := filepath.Join(root, "nes")
	romPath := filepath.Join(systemRoot, "Game.nes")
	boxartPath := filepath.Join(systemRoot, "media", "boxart", "Game.png")
	fs := afero.NewMemMapFs()
	require.NoError(t, os.MkdirAll(systemRoot, 0o750))
	require.NoError(t, fs.MkdirAll(filepath.Dir(boxartPath), 0o750))
	require.NoError(t, afero.WriteFile(fs, boxartPath, []byte("boxart"), 0o600))

	cfg, err := testhelpers.NewTestConfig(nil, t.TempDir())
	require.NoError(t, err)
	pl := mocks.NewMockPlatform()
	pl.On("RootDirs", mock.AnythingOfType("*config.Instance")).Return([]string{root})
	pl.On("Launchers", mock.AnythingOfType("*config.Instance")).Return([]platforms.Launcher{{
		ID:         "nes",
		SystemID:   "NES",
		Folders:    []string{"nes"},
		Extensions: []string{".nes"},
	}})

	mockDB := testhelpers.NewMockMediaDBI()
	mockDB.On("IndexedSystems").Return([]string{"NES"}, nil)
	mockDB.On("FindSystemBySystemID", "NES").Return(database.System{DBID: 1, SystemID: "NES", Name: "NES"}, nil)
	mockDB.On("GetMediaBySystemID", "NES").Return([]database.MediaWithFullPath{
		{DBID: 11, MediaTitleDBID: 101, Path: romPath, SystemID: "NES"},
	}, nil)
	mockDB.On("ApplyScrapeResult", mock.Anything, int64(11), int64(101), mock.Anything).
		Return(assert.AnError).Once()

	ch := make(chan scraper.ScrapeUpdate, 32)
	err = NewPlatformScraper().Scrape(
		context.Background(), cfg, pl, fs, &database.Database{MediaDB: mockDB}, scraper.ScrapeOptions{}, nil, ch,
	)
	require.NoError(t, err)

	var errUpdate scraper.ScrapeUpdate
	for update := range ch {
		if update.Err != nil {
			errUpdate = update
		}
	}
	assert.Equal(t, 1, errUpdate.Processed)
	assert.Equal(t, 1, errUpdate.Skipped)
	mockDB.AssertExpectations(t)
}

func TestScrape_CleanupErrorIncrementsProcessed(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	systemRoot := filepath.Join(root, "nes")
	romPath := filepath.Join(systemRoot, "Game.nes")
	require.NoError(t, os.MkdirAll(systemRoot, 0o750))

	cfg, err := testhelpers.NewTestConfig(nil, t.TempDir())
	require.NoError(t, err)
	pl := mocks.NewMockPlatform()
	pl.On("RootDirs", mock.AnythingOfType("*config.Instance")).Return([]string{root})
	pl.On("Launchers", mock.AnythingOfType("*config.Instance")).Return([]platforms.Launcher{{
		ID:         "nes",
		SystemID:   "NES",
		Folders:    []string{"nes"},
		Extensions: []string{".nes"},
	}})

	mockDB := testhelpers.NewMockMediaDBI()
	mockDB.On("IndexedSystems").Return([]string{"NES"}, nil)
	mockDB.On("FindSystemBySystemID", "NES").Return(database.System{DBID: 1, SystemID: "NES", Name: "NES"}, nil)
	mockDB.On("GetMediaBySystemID", "NES").Return([]database.MediaWithFullPath{
		{DBID: 11, MediaTitleDBID: 101, Path: romPath, SystemID: "NES"},
	}, nil)
	mockDB.On("GetMediaPropertyMetadata", mock.Anything, int64(11)).Return(nil, assert.AnError).Once()

	ch := make(chan scraper.ScrapeUpdate, 32)
	err = NewPlatformScraper().Scrape(
		context.Background(), cfg, pl, afero.NewMemMapFs(), &database.Database{MediaDB: mockDB},
		scraper.ScrapeOptions{Force: true}, nil, ch,
	)
	require.NoError(t, err)

	var errUpdate scraper.ScrapeUpdate
	for update := range ch {
		if update.Err != nil {
			errUpdate = update
		}
	}
	assert.Equal(t, 1, errUpdate.Processed)
	assert.Equal(t, 1, errUpdate.Skipped)
	mockDB.AssertExpectations(t)
}

func TestMediaPropsForPath_UsesFlatFallbackAfterMirroredPath(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	root := filepath.Join("roms", "nes")
	romPath := filepath.Join(root, "Subdir", "Game.nes")
	flatCoverPath := filepath.Join(root, "media", "covers", "Game.webp")
	require.NoError(t, fs.MkdirAll(filepath.Dir(flatCoverPath), 0o750))
	require.NoError(t, afero.WriteFile(fs, flatCoverPath, []byte("cover"), 0o600))

	s := &scraperImpl{fs: fs}
	props := s.mediaPropsForPath(romPath, []string{root}, s.availableDirsByRoot([]string{root}))

	require.Len(t, props, 1)
	assert.Equal(t, tags.PropertyTypeTag(tags.TagPropertyImageBoxart), props[0].TypeTag)
	assert.Equal(t, filepath.ToSlash(flatCoverPath), props[0].Text)
	assert.Equal(t, "image/webp", props[0].ContentType)
}

func TestMediaPropsForPath_FindsArtworkOnDifferentRoot(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	romRoot := filepath.Join(base, "cifs", "nes")
	artRoot := filepath.Join(base, "fat", "nes")
	romPath := filepath.Join(romRoot, "Game.nes")
	boxartPath := filepath.Join(artRoot, "media", "boxart", "Game.png")
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll(filepath.Join(romRoot, "media"), 0o750))
	require.NoError(t, fs.MkdirAll(filepath.Dir(boxartPath), 0o750))
	require.NoError(t, afero.WriteFile(fs, boxartPath, []byte("boxart"), 0o600))

	roots := []string{romRoot, artRoot}
	s := &scraperImpl{fs: fs}
	props := s.mediaPropsForPath(romPath, roots, s.availableDirsByRoot(roots))

	require.Len(t, props, 1)
	assert.Equal(t, tags.PropertyTypeTag(tags.TagPropertyImageBoxart), props[0].TypeTag)
	assert.Equal(t, filepath.ToSlash(boxartPath), props[0].Text)
}

func TestMediaPropsForPath_PrefersEarlierRootInOrder(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	firstRoot := filepath.Join(base, "usb0", "nes")
	secondRoot := filepath.Join(base, "cifs", "nes")
	romPath := filepath.Join(secondRoot, "Game.nes")
	firstBoxart := filepath.Join(firstRoot, "media", "boxart", "Game.png")
	secondBoxart := filepath.Join(secondRoot, "media", "boxart", "Game.png")
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll(filepath.Dir(firstBoxart), 0o750))
	require.NoError(t, fs.MkdirAll(filepath.Dir(secondBoxart), 0o750))
	require.NoError(t, afero.WriteFile(fs, firstBoxart, []byte("first"), 0o600))
	require.NoError(t, afero.WriteFile(fs, secondBoxart, []byte("second"), 0o600))

	roots := []string{firstRoot, secondRoot}
	s := &scraperImpl{fs: fs}
	props := s.mediaPropsForPath(romPath, roots, s.availableDirsByRoot(roots))

	require.Len(t, props, 1)
	assert.Equal(t, filepath.ToSlash(firstBoxart), props[0].Text)
}

func TestMediaPropsForPath_MirroredSubfolderCrossRoot(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	romRoot := filepath.Join(base, "cifs", "nes")
	artRoot := filepath.Join(base, "fat", "nes")
	romPath := filepath.Join(romRoot, "Japan", "Game.nes")
	mirroredPath := filepath.Join(artRoot, "media", "images", "Japan", "Game.png")
	flatPath := filepath.Join(artRoot, "media", "images", "Game.png")
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll(filepath.Dir(mirroredPath), 0o750))
	require.NoError(t, afero.WriteFile(fs, mirroredPath, []byte("mirror"), 0o600))
	require.NoError(t, afero.WriteFile(fs, flatPath, []byte("flat"), 0o600))

	roots := []string{romRoot, artRoot}
	s := &scraperImpl{fs: fs}
	props := s.mediaPropsForPath(romPath, roots, s.availableDirsByRoot(roots))

	require.Len(t, props, 1)
	assert.Equal(t, tags.PropertyTypeTag(tags.TagPropertyImageImage), props[0].TypeTag)
	assert.Equal(t, filepath.ToSlash(mirroredPath), props[0].Text)
}

func TestDeleteStaleLocalMediaProps_DeletesStaleCrossRootProp(t *testing.T) {
	t.Parallel()

	romRoot := filepath.Join(t.TempDir(), "cifs", "nes")
	artRoot := filepath.Join(t.TempDir(), "fat", "nes")
	mediaPath := filepath.Join(romRoot, "Game.nes")
	staleCrossRootPath := filepath.Join(artRoot, "media", "boxart", "Game.png")

	mockDB := testhelpers.NewMockMediaDBI()
	mockDB.On("GetMediaPropertyMetadata", mock.Anything, int64(11)).Return([]database.MediaProperty{
		{
			TypeTag:     tags.PropertyTypeTag(tags.TagPropertyImageBoxart),
			TypeTagDBID: 101,
			Text:        filepath.ToSlash(staleCrossRootPath),
		},
	}, nil)
	mockDB.On("DeleteMediaProperty", mock.Anything, int64(11), int64(101)).Return(nil).Once()

	s := &scraperImpl{db: mockDB, fs: afero.NewMemMapFs()}
	media := &database.MediaWithFullPath{DBID: 11, MediaTitleDBID: 101, Path: mediaPath, SystemID: "NES"}
	deleted, err := s.deleteStaleLocalMediaProps(
		context.Background(), media, []string{romRoot, artRoot}, nil,
	)

	require.NoError(t, err)
	assert.Equal(t, 1, deleted)
	mockDB.AssertExpectations(t)
}
