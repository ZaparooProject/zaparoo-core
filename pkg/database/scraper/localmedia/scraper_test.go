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
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/container"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/scantest"
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

func TestScrape_ImportsArtworkForUncollapsedDirectory(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	collectionDir := filepath.Join(root, "Collection")
	boxartPath := filepath.Join(root, "media", "boxart", "Collection.png")
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll(filepath.Dir(boxartPath), 0o750))
	require.NoError(t, afero.WriteFile(fs, boxartPath, []byte("folder"), 0o600))

	mockDB := testhelpers.NewMockMediaDBI()
	mockDB.On("GetMediaBySystemID", "NES").Return([]database.MediaWithFullPath{
		{
			DBID: 11, MediaTitleDBID: 101, Path: filepath.Join(collectionDir, "One.nes"),
			ParentDir: filepath.ToSlash(collectionDir) + "/", SystemID: "NES",
		},
		{
			DBID: 12, MediaTitleDBID: 102, Path: filepath.Join(collectionDir, "Two.nes"),
			ParentDir: filepath.ToSlash(collectionDir) + "/", SystemID: "NES",
		},
	}, nil)
	mockDB.On(
		"ReplaceDirectoryProperties",
		mock.Anything,
		int64(1),
		[]database.DirectoryProperty{{
			Path:    filepath.ToSlash(collectionDir),
			TypeTag: tags.PropertyTypeTag(tags.TagPropertyImageBoxart),
			Text:    filepath.ToSlash(boxartPath),
		}},
	).Return(true, nil).Once()

	ch := make(chan scraper.ScrapeUpdate, 16)
	s := &scraperImpl{db: mockDB, fs: fs}
	go s.scrapeLoop(context.Background(), scraper.ScrapeOptions{}, []scraper.ScrapeSystem{{
		DBID: 1, ID: "NES", ROMPaths: []string{root},
	}}, ch)

	var final scraper.ScrapeUpdate
	for update := range ch {
		final = update
	}
	assert.True(t, final.Done)
	mockDB.AssertExpectations(t)
	mockDB.AssertNotCalled(t, "ApplyScrapeResult", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func TestIndexedDirectoryPaths_DedupesAncestorsAndSkipsMissing(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	db := testhelpers.NewMockMediaDBI()
	db.On("GetMediaBySystemID", "NES").Return([]database.MediaWithFullPath{
		{Path: filepath.Join(root, "RPGs", "Game", "Disc 1.chd")},
		{Path: filepath.Join(root, "RPGs", "Game", "Disc 2.chd")},
		{Path: filepath.Join(root, "Missing", "Game.chd"), IsMissing: true},
		{Path: filepath.Join(filepath.Dir(root), "Outside", "Game.chd")},
		{Path: filepath.Join(root, "Root Game.nes")},
	}, nil)
	s := &scraperImpl{db: db, fs: afero.NewMemMapFs()}
	scan, err := s.scanSystem(context.Background(), scraper.ScrapeSystem{ID: "NES", ROMPaths: []string{root}})
	require.NoError(t, err)
	paths := scan.directoryPaths
	assert.Equal(t, 5, scan.rows)

	assert.Equal(t, []string{
		filepath.ToSlash(filepath.Join(root, "RPGs")),
		filepath.ToSlash(filepath.Join(root, "RPGs", "Game")),
	}, paths)
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
	dirs := s.availableDirsByRoot(t.Context(), []string{root})
	props := s.mediaPropsForPath(t.Context(), romPath, []string{root}, dirs, false)

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
	props := s.mediaPropsForPath(t.Context(), romPath, roots, s.availableDirsByRoot(t.Context(), roots), false)

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
	props := s.mediaPropsForPath(t.Context(), romPath, roots, s.availableDirsByRoot(t.Context(), roots), false)

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
	props := s.mediaPropsForPath(t.Context(), romPath, roots, s.availableDirsByRoot(t.Context(), roots), false)

	require.Len(t, props, 1)
	assert.Equal(t, tags.PropertyTypeTag(tags.TagPropertyImageImage), props[0].TypeTag)
	assert.Equal(t, filepath.ToSlash(mirroredPath), props[0].Text)
}

func TestDeleteStaleLocalMediaProps_DeletesStaleCrossRootProp(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	romRoot := filepath.Join(base, "cifs", "nes")
	artRoot := filepath.Join(base, "fat", "nes")
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

// A directory that has since gained nested media no longer collapses to one
// game, but the folder artwork written while it did still has to be cleared.
func TestDeleteStaleLocalMediaProps_DeletesFormerContainerArtwork(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	mediaPath := filepath.Join(root, "Cool Game", "Disc 1.cue")
	staleFolderArt := filepath.Join(root, "media", "boxart", "Cool Game.png")

	mockDB := testhelpers.NewMockMediaDBI()
	mockDB.On("GetMediaPropertyMetadata", mock.Anything, int64(12)).Return([]database.MediaProperty{
		{
			TypeTag:     tags.PropertyTypeTag(tags.TagPropertyImageBoxart),
			TypeTagDBID: 102,
			Text:        filepath.ToSlash(staleFolderArt),
		},
	}, nil)
	mockDB.On("DeleteMediaProperty", mock.Anything, int64(12), int64(102)).Return(nil).Once()

	s := &scraperImpl{db: mockDB, fs: afero.NewMemMapFs()}
	media := &database.MediaWithFullPath{DBID: 12, MediaTitleDBID: 102, Path: mediaPath, SystemID: "psx"}
	deleted, err := s.deleteStaleLocalMediaProps(context.Background(), media, []string{root}, nil)

	require.NoError(t, err)
	assert.Equal(t, 1, deleted)
	mockDB.AssertExpectations(t)
}

// A disc folder's artwork is stored under the folder's name, not the inner
// file's, so the file the folder launches has to answer to both. See #1263.
func TestMediaPropsForPath_FindsFolderNamedArtworkForContainerTarget(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	cuePath := filepath.Join(root, "Cool Game", "Disc 1.cue")
	boxartPath := filepath.Join(root, "media", "boxart", "Cool Game.png")
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll(filepath.Dir(cuePath), 0o750))
	require.NoError(t, fs.MkdirAll(filepath.Dir(boxartPath), 0o750))
	require.NoError(t, afero.WriteFile(fs, boxartPath, []byte("boxart"), 0o600))

	s := &scraperImpl{fs: fs}
	roots := []string{root}

	assert.Empty(t, s.mediaPropsForPath(t.Context(), cuePath, roots, s.availableDirsByRoot(t.Context(), roots), false),
		"an ordinary file must not borrow its folder's artwork")

	props := s.mediaPropsForPath(t.Context(), cuePath, roots, s.availableDirsByRoot(t.Context(), roots), true)
	require.Len(t, props, 1)
	assert.Equal(t, tags.PropertyTypeTag(tags.TagPropertyImageBoxart), props[0].TypeTag)
	assert.Equal(t, filepath.ToSlash(boxartPath), props[0].Text)
}

func TestMediaPropsForPath_PrefersOwnArtworkOverFolderArtwork(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	cuePath := filepath.Join(root, "Cool Game", "Disc 1.cue")
	ownArt := filepath.Join(root, "media", "boxart", "Cool Game", "Disc 1.png")
	folderArt := filepath.Join(root, "media", "boxart", "Cool Game.png")
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll(filepath.Dir(cuePath), 0o750))
	require.NoError(t, fs.MkdirAll(filepath.Dir(ownArt), 0o750))
	require.NoError(t, afero.WriteFile(fs, ownArt, []byte("own"), 0o600))
	require.NoError(t, afero.WriteFile(fs, folderArt, []byte("folder"), 0o600))

	s := &scraperImpl{fs: fs}
	roots := []string{root}
	props := s.mediaPropsForPath(t.Context(), cuePath, roots, s.availableDirsByRoot(t.Context(), roots), true)
	require.Len(t, props, 1)
	assert.Equal(t, filepath.ToSlash(ownArt), props[0].Text)
}

func TestIsContainerLaunchTarget(t *testing.T) {
	t.Parallel()

	// Host separators, so the empty-ParentDir fallback is exercised the way a
	// row written by filepath.Join reaches it on Windows.
	root := filepath.Join(string(filepath.Separator), "roms", "PSX")
	cue := database.MediaWithFullPath{DBID: 1, Path: filepath.Join(root, "Cool Game", "Disc 1.cue")}
	bin := database.MediaWithFullPath{DBID: 2, Path: filepath.Join(root, "Cool Game", "Disc 1.bin")}
	loose := database.MediaWithFullPath{DBID: 3, Path: filepath.Join(root, "Other.chd")}
	containers := scannedLaunchTargets(t, []database.MediaWithFullPath{cue, bin, loose})

	assert.True(t, isContainerLaunchTarget(containers, &cue))
	assert.False(t, isContainerLaunchTarget(containers, &bin))
	assert.False(t, isContainerLaunchTarget(containers, &loose),
		"the system root holds nested media, so it is not a container")
}

// A system whose roots hold no artwork directory can match nothing, so an
// ordinary run never reads its media. It still replaces the folder artwork
// snapshot with the empty set, as a full pass over the rows would.
func TestScrape_SkipsMediaLoadWithoutArtworkDirs(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	fs := afero.NewMemMapFs()
	// A media directory no artwork lookup searches.
	require.NoError(t, fs.MkdirAll(filepath.Join(root, "media", "videos"), 0o750))

	mockDB := testhelpers.NewMockMediaDBI()
	mockDB.On("GetMediaBySystemID", "NES").Return([]database.MediaWithFullPath{
		{DBID: 11, MediaTitleDBID: 101, Path: filepath.Join(root, "Game.nes")},
	}, nil).Maybe()
	mockDB.On("ReplaceDirectoryProperties", mock.Anything, int64(1), []database.DirectoryProperty{}).
		Return(false, nil).Once()

	ch := make(chan scraper.ScrapeUpdate, 16)
	s := &scraperImpl{db: mockDB, fs: fs}
	s.scrapeLoop(context.Background(), scraper.ScrapeOptions{}, []scraper.ScrapeSystem{{
		DBID: 1, ID: "NES", ROMPaths: []string{root},
	}}, ch)

	var updates []scraper.ScrapeUpdate
	for update := range ch {
		require.NoError(t, update.FatalErr)
		updates = append(updates, update)
	}
	require.NotEmpty(t, updates)
	assert.True(t, updates[len(updates)-1].Done)
	mockDB.AssertNotCalled(t, "GetMediaBySystemID", mock.Anything)
	mockDB.AssertNotCalled(t, "GetMediaSourcesForScrape", mock.Anything, mock.Anything, mock.Anything)
	mockDB.AssertNotCalled(t, "ApplyScrapeResult", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	mockDB.AssertExpectations(t)
}

func TestScrape_StopsWhenCancelledWhileLoadingMedia(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	fs := afero.NewMemMapFs()
	boxart := filepath.Join(root, "media", "boxart", "Game.png")
	require.NoError(t, fs.MkdirAll(filepath.Dir(boxart), 0o750))
	require.NoError(t, afero.WriteFile(fs, boxart, []byte("boxart"), 0o600))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mockDB := testhelpers.NewMockMediaDBI()
	mockDB.On("GetMediaBySystemID", "NES").Run(func(mock.Arguments) { cancel() }).
		Return([]database.MediaWithFullPath{
			{DBID: 11, MediaTitleDBID: 101, Path: filepath.Join(root, "Game.nes")},
		}, nil).Once()

	ch := make(chan scraper.ScrapeUpdate, 16)
	s := &scraperImpl{db: mockDB, fs: fs}
	s.scrapeLoop(ctx, scraper.ScrapeOptions{}, []scraper.ScrapeSystem{{
		DBID: 1, ID: "NES", ROMPaths: []string{root},
	}}, ch)

	var updates []scraper.ScrapeUpdate
	for update := range ch {
		updates = append(updates, update)
	}
	require.Len(t, updates, 1)
	assert.True(t, updates[0].Done)
	require.ErrorIs(t, updates[0].FatalErr, context.Canceled)
	mockDB.AssertNotCalled(t, "ApplyScrapeResult", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	mockDB.AssertNotCalled(t, "ReplaceDirectoryProperties", mock.Anything, mock.Anything, mock.Anything)
	mockDB.AssertExpectations(t)
}

// scannedLaunchTargets is the container view an ordinary run builds while it
// scans media.
func scannedLaunchTargets(t *testing.T, media []database.MediaWithFullPath) launchTargetResolver {
	t.Helper()
	db := testhelpers.NewMockMediaDBI()
	db.On("GetMediaBySystemID", "PSX").Return(media, nil)
	scan, err := (&scraperImpl{db: db, fs: afero.NewMemMapFs()}).scanSystem(
		context.Background(), scraper.ScrapeSystem{ID: "PSX"})
	require.NoError(t, err)
	return scan.targets
}

// The per-directory container view built while scanning must answer as the
// full index over the same rows does.
func TestScanSystemLaunchTargets_MatchFullIndex(t *testing.T) {
	t.Parallel()

	media := []database.MediaWithFullPath{
		{DBID: 1, MediaTitleDBID: 10, Path: "/roms/PSX/Cool Game/Disc 1.cue"},
		{DBID: 2, MediaTitleDBID: 10, Path: "/roms/PSX/Cool Game/Disc 1.bin"},
		{DBID: 3, MediaTitleDBID: 11, Path: "/roms/PSX/Loose.chd"},
		{DBID: 4, MediaTitleDBID: 12, Path: "/roms/PSX/Solo/Solo.chd"},
		{DBID: 5, MediaTitleDBID: 13, Path: "/roms/PSX/Multi/Multi.m3u"},
		{DBID: 6, MediaTitleDBID: 13, Path: "/roms/PSX/Multi/Multi (Disc 1).chd"},
		{DBID: 7, MediaTitleDBID: 13, Path: "/roms/PSX/Multi/Multi (Disc 2).chd"},
		{DBID: 8, MediaTitleDBID: 14, Path: "/roms/PSX/Set/A.iso"},
		{DBID: 9, MediaTitleDBID: 14, Path: "/roms/PSX/Set/B.iso"},
		{DBID: 10, MediaTitleDBID: 15, Path: "/roms/PSX/Mixed/A.iso"},
		{DBID: 11, MediaTitleDBID: 16, Path: "/roms/PSX/Mixed/B.iso"},
		{DBID: 12, MediaTitleDBID: 17, Path: "/roms/PSX/Gone/Gone.chd", IsMissing: true},
		{DBID: 13, MediaTitleDBID: 18, Path: "/roms/PSX/Half/Kept.chd"},
		{DBID: 14, MediaTitleDBID: 19, Path: "/roms/PSX/Half/Lost.chd", IsMissing: true},
		{DBID: 15, MediaTitleDBID: 20, Path: "/roms/PSX/Nest/Outer.chd"},
		{DBID: 16, MediaTitleDBID: 21, Path: "/roms/PSX/Nest/Inner/Inner.chd"},
		{DBID: 17, MediaTitleDBID: 22, Path: "/roms/PSX/Stored/Game.chd", ParentDir: "/roms/PSX/Stored/"},
		{DBID: 18, MediaTitleDBID: 23, Path: "/roms/PSX/Moved/Game.chd", ParentDir: "/roms/PSX/Elsewhere/"},
		{DBID: 19, MediaTitleDBID: 24, Path: "steam://123/Game"},
	}
	full := make([]database.Media, 0, len(media))
	for i := range media {
		full = append(full, database.Media{
			DBID: media[i].DBID, MediaTitleDBID: media[i].MediaTitleDBID, Path: media[i].Path,
			ParentDir: media[i].ParentDir, IsMissing: media[i].IsMissing,
		})
	}
	want := container.NewIndex(full)
	got := scannedLaunchTargets(t, media)

	targets := 0
	for i := range media {
		expected := isContainerLaunchTarget(want, &media[i])
		if expected {
			targets++
		}
		assert.Equal(t, expected, isContainerLaunchTarget(got, &media[i]), media[i].Path)
	}
	assert.Positive(t, targets)
}

// An ordinary run over a real media database streams the system's rows and
// writes what it finds only after the read has finished.
func TestScrape_WritesArtworkFromStreamedRows(t *testing.T) {
	t.Parallel()

	db, cleanup := testhelpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)
	root := t.TempDir()
	gamePath := filepath.Join(root, "Game.nes")
	otherPath := filepath.Join(root, "Sub", "Other.nes")
	scantest.IndexScanResults(t, db, systemdefs.SystemNES, database.ScanReconcileOpts{},
		platforms.ScanResult{Path: gamePath}, platforms.ScanResult{Path: otherPath})

	fs := afero.NewMemMapFs()
	gameArt := filepath.Join(root, "media", "boxart", "Game.png")
	otherArt := filepath.Join(root, "media", "boxart", "Sub", "Other.png")
	folderArt := filepath.Join(root, "media", "boxart", "Sub.png")
	for _, art := range []string{gameArt, otherArt, folderArt} {
		require.NoError(t, fs.MkdirAll(filepath.Dir(art), 0o750))
		require.NoError(t, afero.WriteFile(fs, art, []byte("image"), 0o600))
	}
	system, err := db.FindSystemBySystemID(systemdefs.SystemNES)
	require.NoError(t, err)

	ch := make(chan scraper.ScrapeUpdate, 64)
	s := &scraperImpl{db: db, fs: fs}
	s.scrapeLoop(context.Background(), scraper.ScrapeOptions{}, []scraper.ScrapeSystem{{
		DBID: system.DBID, ID: systemdefs.SystemNES, ROMPaths: []string{root},
	}}, ch)
	var last scraper.ScrapeUpdate
	for update := range ch {
		require.NoError(t, update.FatalErr)
		require.NoError(t, update.Err)
		last = update
	}
	require.True(t, last.Done)

	rows, err := db.GetMediaBySystemID(systemdefs.SystemNES)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	wantArt := map[string]string{gamePath: gameArt, otherPath: otherArt}
	for i := range rows {
		props, err := db.GetMediaPropertyMetadata(context.Background(), rows[i].DBID)
		require.NoError(t, err)
		texts := make([]string, 0, len(props))
		for _, prop := range props {
			texts = append(texts, prop.Text)
		}
		assert.Contains(t, texts, filepath.ToSlash(wantArt[filepath.FromSlash(rows[i].Path)]), rows[i].Path)
	}
}

// An ordinary run reads rows a page at a time; every row, including those on
// either side of a page boundary, must still be scraped.
func TestScrape_PagesThroughLargeSystems(t *testing.T) {
	t.Parallel()

	db, cleanup := testhelpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)
	root := t.TempDir()
	count := 2*mediaPageSize + 3
	results := make([]platforms.ScanResult, 0, count)
	for i := range count {
		results = append(results, platforms.ScanResult{Path: filepath.Join(root, fmt.Sprintf("Game %04d.nes", i))})
	}
	scantest.IndexScanResults(t, db, systemdefs.SystemNES, database.ScanReconcileOpts{}, results...)

	fs := afero.NewMemMapFs()
	want := map[string]string{}
	for _, i := range []int{0, mediaPageSize - 1, mediaPageSize, count - 1} {
		art := filepath.Join(root, "media", "boxart", fmt.Sprintf("Game %04d.png", i))
		require.NoError(t, fs.MkdirAll(filepath.Dir(art), 0o750))
		require.NoError(t, afero.WriteFile(fs, art, []byte("image"), 0o600))
		want[filepath.ToSlash(results[i].Path)] = filepath.ToSlash(art)
	}
	system, err := db.FindSystemBySystemID(systemdefs.SystemNES)
	require.NoError(t, err)

	ch := make(chan scraper.ScrapeUpdate, 4*count)
	s := &scraperImpl{db: db, fs: fs}
	s.scrapeLoop(context.Background(), scraper.ScrapeOptions{}, []scraper.ScrapeSystem{{
		DBID: system.DBID, ID: systemdefs.SystemNES, ROMPaths: []string{root},
	}}, ch)
	var lastProgress scraper.ScrapeUpdate
	for update := range ch {
		require.NoError(t, update.FatalErr)
		if !update.Done {
			lastProgress = update
		}
	}
	assert.Equal(t, count, lastProgress.Processed)
	assert.Equal(t, len(want), lastProgress.Matched)

	rows, err := db.GetMediaBySystemID(systemdefs.SystemNES)
	require.NoError(t, err)
	for i := range rows {
		art, ok := want[rows[i].Path]
		if !ok {
			continue
		}
		props, err := db.GetMediaPropertyMetadata(context.Background(), rows[i].DBID)
		require.NoError(t, err)
		require.Len(t, props, 1, rows[i].Path)
		assert.Equal(t, art, props[0].Text)
	}
}
