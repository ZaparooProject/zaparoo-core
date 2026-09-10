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
package pinuppopper

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/pinup"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	_ "github.com/mattn/go-sqlite3" // Fixture database driver.
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const fixtureSQL = `
CREATE TABLE Emulators (
	EMUID INTEGER PRIMARY KEY, EmuName VARCHAR(100), EmuDisplay VARCHAR(200), Visible INTEGER DEFAULT 1,
	DirMedia VARCHAR(255), GamesExt VARCHAR(200), LaunchScript TEXT, ProcessName VARCHAR(50), WinTitle VARCHAR(50)
);
CREATE TABLE Games (
	GameID INTEGER PRIMARY KEY, EMUID INTEGER, GameName VARCHAR(200), GameFileName VARCHAR(250),
	GameDisplay VARCHAR(200), Visible INTEGER DEFAULT 1, Notes TEXT, GameYear INTEGER, Manufact VARCHAR(200),
	NumPlayers INTEGER, GameType VARCHAR(50), Category VARCHAR(200), Author VARCHAR(200), GameTheme VARCHAR(100),
	GameRating INTEGER, ALTEXE VARCHAR(250)
);
INSERT INTO Emulators (EMUID, EmuName, EmuDisplay, Visible, DirMedia, GamesExt, ProcessName) VALUES
	(1, 'Visual Pinball X', 'VPX', 1, ?, 'vpx', 'VPinballX'),
	(2, 'Future Pinball', 'FP', 1, 'D:\vPinball\PinUPSystem\POPMedia\Future Pinball', 'fpt', 'Future Pinball');
INSERT INTO Games (GameID, EMUID, GameName, GameFileName, GameDisplay, Visible, Notes, GameYear, Manufact,
	NumPlayers, GameType, Category, GameTheme) VALUES
	(10, 1, 'afm', 'afm.vpx', 'Attack from Mars', 1, '  Great   table ', 1995, 'Bally', 4, 'SS', 'Recreation',
	 'Aliens'),
	(12, 2, 'fp_table', 'fp_table.fpt', 'FP Table', 1, NULL, NULL, NULL, NULL, NULL, NULL, NULL),
	(14, 1, 'sparse', 'sparse.vpx', NULL, 1, NULL, 0, '', 0, '', '', '');
`

// writeFixture builds a Popper install with a database and some media files
// and returns the install description the scraper is given.
func writeFixture(t *testing.T) pinup.Install {
	t.Helper()
	dir := t.TempDir()
	mediaDir := filepath.Join(dir, "POPMedia")
	vpxMedia := filepath.Join(mediaDir, "Visual Pinball X")
	for _, sub := range []string{"Wheel", "PlayField", "BackGlass", "GameInfo", "Topper"} {
		require.NoError(t, os.MkdirAll(filepath.Join(vpxMedia, sub), 0o750))
	}
	require.NoError(t, os.MkdirAll(filepath.Join(mediaDir, "Future Pinball", "Wheel"), 0o750))
	for _, file := range []string{
		filepath.Join(vpxMedia, "Wheel", "afm.png"),
		filepath.Join(vpxMedia, "PlayField", "afm.jpg"),
		filepath.Join(vpxMedia, "PlayField", "afm.mp4"),
		filepath.Join(vpxMedia, "BackGlass", "afm.mp4"),
		filepath.Join(vpxMedia, "GameInfo", "afm.png"),
		filepath.Join(vpxMedia, "Topper", "afm.png"),
		filepath.Join(mediaDir, "Future Pinball", "Wheel", "fp_table.png"),
	} {
		require.NoError(t, os.WriteFile(file, []byte("img"), 0o600))
	}

	dbPath := filepath.Join(dir, "PUPDatabase.db")
	db, err := sql.Open("sqlite3", dbPath)
	require.NoError(t, err)
	_, err = db.ExecContext(context.Background(), fixtureSQL, vpxMedia)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	return pinup.Install{Dir: dir, DBPath: dbPath, MediaDir: mediaDir}
}

func fixtureInstall(t *testing.T) *pinup.Install {
	t.Helper()
	inst := writeFixture(t)
	return &inst
}

func pinballMedia(dbID, titleID int64, path string) database.MediaWithFullPath {
	return database.MediaWithFullPath{
		DBID: dbID, MediaTitleDBID: titleID, Path: path, SystemID: systemdefs.SystemPinball,
	}
}

func fixtureLocate(inst *pinup.Install) Locate {
	return func(*config.Instance) (pinup.Install, error) { return *inst, nil }
}

// captureWrites records every per-record write the scraper makes.
func captureWrites(mediaDB *testhelpers.MockMediaDBI) *[]database.ScrapeWriteTarget {
	writes := &[]database.ScrapeWriteTarget{}
	mediaDB.On("ApplyScrapeResult", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			*writes = append(*writes, database.ScrapeWriteTarget{
				MediaDBID:      args.Get(1).(int64),
				MediaTitleDBID: args.Get(2).(int64),
				Write:          args.Get(3).(*database.ScrapeWrite),
			})
		}).Return(nil)
	return writes
}

func drain(t *testing.T, ch <-chan scraper.ScrapeUpdate) []scraper.ScrapeUpdate {
	t.Helper()
	updates := make([]scraper.ScrapeUpdate, 0, 4)
	for update := range ch {
		updates = append(updates, update)
	}
	require.NotEmpty(t, updates)
	assert.True(t, updates[len(updates)-1].Done)
	return updates
}

func tagValue(write *database.ScrapeWrite, tagType string) []string {
	values := make([]string, 0, 2)
	for _, tag := range write.TitleTags {
		if tag.Type == tagType {
			values = append(values, tag.Tag)
		}
	}
	return values
}

func propText(props []database.MediaProperty, value tags.TagValue) string {
	for _, prop := range props {
		if prop.TypeTag == tags.PropertyTypeTag(value) {
			return prop.Text
		}
	}
	return ""
}

func TestScrapeWritesMetadataAndArtwork(t *testing.T) {
	t.Parallel()
	inst := writeFixture(t)
	mediaDB := testhelpers.NewMockMediaDBI()
	mediaDB.On("GetTitlesBySystemID", systemdefs.SystemPinball).Return([]database.TitleWithSystem{
		{
			DBID: 1, Slug: "attack-from-mars", Name: "Attack from Mars", SystemID: systemdefs.SystemPinball,
			SystemDBID: 7,
		},
	}, nil)
	mediaDB.On("GetMediaBySystemID", systemdefs.SystemPinball).Return([]database.MediaWithFullPath{
		pinballMedia(100, 1, pinup.TablePath(10, "Attack from Mars")),
		pinballMedia(101, 2, pinup.TablePath(12, "FP Table")),
		pinballMedia(102, 3, pinup.TablePath(14, "sparse")),
		pinballMedia(103, 4, pinup.TablePath(999, "Gone")),
		pinballMedia(104, 5, "C:/tables/other.vpx"),
		pinballMedia(105, 6, pinup.TablePath(10, "Scraped already")),
	}, nil)
	mediaDB.On("GetScrapedMediaIDs", mock.Anything, scraperID, int64(7)).
		Return(map[int64]struct{}{105: {}}, nil)
	writes := captureWrites(mediaDB)

	s := NewPlatformScraper(fixtureLocate(&inst))
	assert.Equal(t, "pinup-popper", s.ID)
	assert.Equal(t, []string{systemdefs.SystemPinball}, s.SupportedSystemIDs)
	ch := make(chan scraper.ScrapeUpdate, 16)
	require.NoError(t, s.Scrape(
		context.Background(), nil, nil, afero.NewOsFs(), &database.Database{MediaDB: mediaDB},
		scraper.ScrapeOptions{}, nil, ch,
	))
	updates := drain(t, ch)
	final := updates[len(updates)-1]
	assert.Equal(t, 4, final.Processed, "popper rows not yet scraped")
	assert.Equal(t, 3, final.Matched)
	assert.Equal(t, 1, final.Skipped, "the table missing from Popper")

	require.Len(t, *writes, 3)
	byMedia := make(map[int64]database.ScrapeWriteTarget)
	for _, w := range *writes {
		byMedia[w.MediaDBID] = w
	}

	afm := byMedia[100]
	require.NotNil(t, afm.Write)
	assert.Equal(t, int64(1), afm.MediaTitleDBID)
	assert.Equal(t, scraper.SentinelTagInfo(scraperID), afm.Write.Sentinel)
	assert.Empty(t, afm.Write.MediaTags, "no run tag without a run ID")
	assert.Equal(t, []string{"1995"}, tagValue(afm.Write, string(tags.TagTypeYear)))
	assert.Equal(t, []string{"4"}, tagValue(afm.Write, string(tags.TagTypePlayers)))
	assert.Equal(t, []string{tags.NormalizeTagValue(string(tags.TagTypeDeveloper), "Bally")},
		tagValue(afm.Write, string(tags.TagTypeDeveloper)))
	assert.Equal(t, []string{
		tags.NormalizeTagValue(string(tags.TagTypeGenre), "SS"),
		tags.NormalizeTagValue(string(tags.TagTypeGenre), "Recreation"),
		tags.NormalizeTagValue(string(tags.TagTypeGenre), "Aliens"),
	}, tagValue(afm.Write, string(tags.TagTypeGenre)))
	assert.Equal(t, "Great table", propText(afm.Write.TitleProps, tags.TagPropertyDescription))
	vpxMedia := filepath.ToSlash(filepath.Join(inst.MediaDir, "Visual Pinball X"))
	assert.Equal(t, vpxMedia+"/Wheel/afm.png", propText(afm.Write.MediaProps, tags.TagPropertyImageWheel))
	assert.Equal(t, vpxMedia+"/PlayField/afm.jpg", propText(afm.Write.MediaProps, tags.TagPropertyImageScreenshot))
	assert.Empty(t, propText(afm.Write.MediaProps, tags.TagPropertyImageMarquee), "a backglass video is not an image")
	assert.Equal(t, vpxMedia+"/GameInfo/afm.png", propText(afm.Write.MediaProps, tags.TagPropertyImageImage))
	assert.Len(t, afm.Write.MediaProps, 3, "the topper is not imported")

	fp := byMedia[101]
	require.NotNil(t, fp.Write)
	assert.Empty(t, fp.Write.TitleTags)
	assert.Empty(t, fp.Write.TitleProps)
	fpMedia := filepath.ToSlash(filepath.Join(inst.MediaDir, "Future Pinball"))
	assert.Equal(t, fpMedia+"/Wheel/fp_table.png", propText(fp.Write.MediaProps, tags.TagPropertyImageWheel),
		"a media directory that does not exist falls back to POPMedia/<emulator>")

	sparse := byMedia[102]
	require.NotNil(t, sparse.Write)
	assert.Empty(t, sparse.Write.TitleTags, "zero and blank fields produce no tags")
	assert.Empty(t, sparse.Write.MediaProps)
	mediaDB.AssertExpectations(t)
}

func TestScrapeForceRescrapesAndMarksRun(t *testing.T) {
	t.Parallel()
	inst := writeFixture(t)
	mediaDB := testhelpers.NewMockMediaDBI()
	mediaDB.On("GetTitlesBySystemID", systemdefs.SystemPinball).Return([]database.TitleWithSystem{
		{DBID: 1, SystemID: systemdefs.SystemPinball, SystemDBID: 7},
	}, nil)
	mediaDB.On("GetMediaBySystemID", systemdefs.SystemPinball).Return([]database.MediaWithFullPath{
		pinballMedia(100, 1, pinup.TablePath(10, "Attack from Mars")),
	}, nil)
	mediaDB.On("GetScrapeRunMediaIDs", mock.Anything, scraperID, "run-1", int64(7)).
		Return(map[int64]struct{}{}, nil).Once()
	writes := captureWrites(mediaDB)

	ch := make(chan scraper.ScrapeUpdate, 16)
	require.NoError(t, NewPlatformScraper(fixtureLocate(&inst)).Scrape(
		context.Background(), nil, nil, afero.NewOsFs(), &database.Database{MediaDB: mediaDB},
		scraper.ScrapeOptions{Force: true, RunID: "run-1"}, nil, ch,
	))
	drain(t, ch)
	require.Len(t, *writes, 1)
	assert.Contains(t, (*writes)[0].Write.MediaTags, scraper.RunTagInfo(scraperID, "run-1"))
	mediaDB.AssertNotCalled(t, "GetScrapedMediaIDs", mock.Anything, mock.Anything, mock.Anything)
}

func TestScrapeFillMissingResumesOnlyUnfinishedRows(t *testing.T) {
	t.Parallel()
	inst := writeFixture(t)
	mediaDB := testhelpers.NewMockMediaDBI()
	mediaDB.On("GetTitlesBySystemID", systemdefs.SystemPinball).Return([]database.TitleWithSystem{
		{DBID: 1, SystemID: systemdefs.SystemPinball, SystemDBID: 7},
	}, nil)
	mediaDB.On("GetMediaBySystemID", systemdefs.SystemPinball).Return([]database.MediaWithFullPath{
		pinballMedia(100, 1, pinup.TablePath(10, "Done")),
		pinballMedia(101, 2, pinup.TablePath(12, "Pending")),
	}, nil)
	mediaDB.On("GetScrapeRunMediaIDs", mock.Anything, scraperID, "resume", int64(7)).
		Return(map[int64]struct{}{100: {}}, nil).Once()
	writes := captureWrites(mediaDB)
	ch := make(chan scraper.ScrapeUpdate, 16)
	require.NoError(t, NewPlatformScraper(fixtureLocate(&inst)).Scrape(
		t.Context(), nil, nil, afero.NewOsFs(), &database.Database{MediaDB: mediaDB},
		scraper.ScrapeOptions{FillMissing: true, RunID: "resume"}, nil, ch,
	))
	drain(t, ch)
	require.Len(t, *writes, 1)
	require.Equal(t, int64(101), (*writes)[0].MediaDBID)
	require.True(t, (*writes)[0].Write.FillMissing)
	require.Contains(t, (*writes)[0].Write.MediaTags, scraper.RunTagInfo(scraperID, "resume"))
	mediaDB.AssertExpectations(t)
	mediaDB.AssertNotCalled(t, "GetScrapedMediaIDs", mock.Anything, mock.Anything, mock.Anything)
}

func TestScrapeSkipsOtherSystems(t *testing.T) {
	t.Parallel()
	inst := writeFixture(t)
	mediaDB := testhelpers.NewMockMediaDBI()

	ch := make(chan scraper.ScrapeUpdate, 4)
	require.NoError(t, NewPlatformScraper(fixtureLocate(&inst)).Scrape(
		context.Background(), nil, nil, afero.NewOsFs(), &database.Database{MediaDB: mediaDB},
		scraper.ScrapeOptions{Systems: []string{systemdefs.SystemSNES}}, nil, ch,
	))
	updates := drain(t, ch)
	assert.Len(t, updates, 1)
	assert.Equal(t, 0, updates[0].Processed)
	mediaDB.AssertNotCalled(t, "GetMediaBySystemID", mock.Anything)
}

func TestScrapeErrors(t *testing.T) {
	t.Parallel()

	t.Run("not installed", func(t *testing.T) {
		t.Parallel()
		locate := func(*config.Instance) (pinup.Install, error) { return pinup.Install{}, pinup.ErrNotInstalled }
		err := NewPlatformScraper(locate).Scrape(
			context.Background(), nil, nil, afero.NewOsFs(),
			&database.Database{MediaDB: testhelpers.NewMockMediaDBI()}, scraper.ScrapeOptions{}, nil,
			make(chan scraper.ScrapeUpdate, 1),
		)
		require.ErrorIs(t, err, pinup.ErrNotInstalled)
	})

	t.Run("no database", func(t *testing.T) {
		t.Parallel()
		err := NewPlatformScraper(fixtureLocate(fixtureInstall(t))).Scrape(
			context.Background(), nil, nil, afero.NewOsFs(), &database.Database{}, scraper.ScrapeOptions{}, nil,
			make(chan scraper.ScrapeUpdate, 1),
		)
		require.Error(t, err)
	})

	t.Run("media load fails", func(t *testing.T) {
		t.Parallel()
		mediaDB := testhelpers.NewMockMediaDBI()
		mediaDB.On("GetTitlesBySystemID", systemdefs.SystemPinball).Return([]database.TitleWithSystem{}, nil)
		mediaDB.On("GetMediaBySystemID", systemdefs.SystemPinball).Return(nil, errors.New("load failed"))
		ch := make(chan scraper.ScrapeUpdate, 4)
		require.NoError(t, NewPlatformScraper(fixtureLocate(fixtureInstall(t))).Scrape(
			context.Background(), nil, nil, afero.NewOsFs(), &database.Database{MediaDB: mediaDB},
			scraper.ScrapeOptions{}, nil, ch,
		))
		updates := drain(t, ch)
		require.ErrorContains(t, updates[0].FatalErr, "load media")
	})

	t.Run("write failure is reported and counted", func(t *testing.T) {
		t.Parallel()
		inst := writeFixture(t)
		mediaDB := testhelpers.NewMockMediaDBI()
		mediaDB.On("GetTitlesBySystemID", systemdefs.SystemPinball).Return([]database.TitleWithSystem{
			{DBID: 1, SystemID: systemdefs.SystemPinball, SystemDBID: 7},
		}, nil)
		mediaDB.On("GetMediaBySystemID", systemdefs.SystemPinball).Return([]database.MediaWithFullPath{
			pinballMedia(100, 1, pinup.TablePath(10, "Attack from Mars")),
		}, nil)
		mediaDB.On("GetScrapedMediaIDs", mock.Anything, scraperID, int64(7)).Return(map[int64]struct{}{}, nil)
		mediaDB.On("ApplyScrapeResult", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
			Return(errors.New("disk full"))
		ch := make(chan scraper.ScrapeUpdate, 8)
		require.NoError(t, NewPlatformScraper(fixtureLocate(&inst)).Scrape(
			context.Background(), nil, nil, afero.NewOsFs(), &database.Database{MediaDB: mediaDB},
			scraper.ScrapeOptions{}, nil, ch,
		))
		updates := drain(t, ch)
		terminal := updates[len(updates)-1]
		require.ErrorContains(t, terminal.FatalErr, "disk full")
		require.True(t, terminal.Done)
		require.Zero(t, terminal.Matched)
		require.Zero(t, terminal.Processed, "failed writes are not completed work")
	})
}

func TestCleanText(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "Great table", cleanText("  Great \t table\r\n"))
	assert.Equal(t, "a b", cleanText("a\x01b"))
	assert.Empty(t, cleanText("   "))
}
