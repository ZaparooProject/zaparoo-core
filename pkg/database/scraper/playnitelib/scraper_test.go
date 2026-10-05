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

package playnitelib

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper/scrapertest"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/playnite"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const (
	idRich   = "513cf1fd-4866-4fa0-911d-6217b8712ded"
	idSparse = "67feee56-a90d-4022-9be8-7bec24e4fac0"
	idGone   = "0a2f7f0c-6a51-4a8e-8f65-0f2f5a3a8d09"
	idPC     = "0a2f7f0c-6a51-4a8e-8f65-0f2f5a3a8d05"
)

// fixture is a Playnite library and the image files it points at.
type fixture struct {
	fs         afero.Fs
	cover      string
	background string
	games      []playnite.Game
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "library", "files")
	f := &fixture{
		fs:         afero.NewMemMapFs(),
		cover:      filepath.Join(dir, "cover.png"),
		background: filepath.Join(dir, "background.jpg"),
	}
	require.NoError(t, afero.WriteFile(f.fs, f.cover, []byte("img"), 0o600))
	require.NoError(t, f.fs.MkdirAll(f.background, 0o755))
	nes := []playnite.Platform{{SpecificationID: "nintendo_nes"}}
	f.games = []playnite.Game{
		{
			ID: strings.ToUpper(idRich), Name: "Rich Game", IsInstalled: true, Platforms: nes,
			ReleaseYear: 1989,
			Developers:  []string{"  Test   Developer ", "Test Developer", ""},
			Publishers:  []string{"Test Publisher"},
			Genres:      []string{"Platform", "Shooter", "Zaparoo Nonsense Genre"},
			Description: "<p>A <b>test</b>&nbsp;description.</p><script>alert(1)</script><br/>Second&amp;line",
			Cover:       f.cover, Background: f.background, Icon: f.cover,
		},
		{ID: idSparse, Name: "Sparse Game", IsInstalled: true, Platforms: nes, Cover: "relative.png"},
		{ID: idPC, Name: "PC Game", IsInstalled: true, ReleaseYear: 2020},
		{ID: "not-a-guid", Name: "Broken", IsInstalled: true, Platforms: nes},
	}
	return f
}

func (f *fixture) library(context.Context) ([]playnite.Game, error) {
	return f.games, nil
}

func media(dbID, titleID int64, systemID, path string) database.MediaWithFullPath {
	return database.MediaWithFullPath{DBID: dbID, MediaTitleDBID: titleID, Path: path, SystemID: systemID}
}

// captureWrites records every per-record write the scraper makes and checks
// each against the tag vocabulary.
func captureWrites(t *testing.T, mediaDB *testhelpers.MockMediaDBI) *[]database.ScrapeWriteTarget {
	t.Helper()
	writes := &[]database.ScrapeWriteTarget{}
	mediaDB.On("ApplyScrapeResult", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			write, ok := args.Get(3).(*database.ScrapeWrite)
			require.True(t, ok)
			scrapertest.RequireValidWrite(t, write)
			mediaID, ok := args.Get(1).(int64)
			require.True(t, ok)
			titleID, ok := args.Get(2).(int64)
			require.True(t, ok)
			*writes = append(*writes, database.ScrapeWriteTarget{
				MediaDBID: mediaID, MediaTitleDBID: titleID, Write: write,
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

func tagValue(write *database.ScrapeWrite, tagType tags.TagType) []string {
	values := make([]string, 0, 2)
	for _, tag := range write.TitleTags {
		if tag.Type == string(tagType) {
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

func expectSystem(
	mediaDB *testhelpers.MockMediaDBI, systemID string, systemDBID int64, rows ...database.MediaWithFullPath,
) {
	mediaDB.On("GetTitlesBySystemID", systemID).Return([]database.TitleWithSystem{
		{DBID: 1, SystemID: systemID, SystemDBID: systemDBID},
	}, nil)
	mediaDB.On("GetMediaBySystemID", systemID).Return(rows, nil)
}

func TestScrapeWritesMetadataAndArtwork(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	mediaDB := testhelpers.NewMockMediaDBI()
	expectSystem(mediaDB, systemdefs.SystemNES, 7,
		media(100, 1, systemdefs.SystemNES, playnite.GamePath(idRich, "Rich Game")),
		media(101, 2, systemdefs.SystemNES, playnite.GamePath(idSparse, "Sparse Game")),
		media(102, 3, systemdefs.SystemNES, playnite.GamePath(idGone, "Gone")),
		media(103, 4, systemdefs.SystemNES, "C:/roms/other.nes"),
		media(104, 5, systemdefs.SystemNES, playnite.GamePath(idRich, "Scraped already")),
		media(105, 6, systemdefs.SystemNES, "playnite://not-a-guid/Broken"),
		database.MediaWithFullPath{
			DBID: 106, MediaTitleDBID: 7, SystemID: systemdefs.SystemNES, IsMissing: true,
			Path: playnite.GamePath(idRich, "Missing row"),
		},
	)
	expectSystem(mediaDB, systemdefs.SystemPC, 9,
		media(200, 20, systemdefs.SystemPC, playnite.GamePath(idPC, "PC Game")),
		media(201, 21, systemdefs.SystemPC, "steam://1/Other"),
	)
	mediaDB.On("GetScrapedMediaIDs", mock.Anything, scraperID, int64(7)).
		Return(map[int64]struct{}{104: {}}, nil)
	mediaDB.On("GetScrapedMediaIDs", mock.Anything, scraperID, int64(9)).
		Return(map[int64]struct{}{}, nil)
	writes := captureWrites(t, mediaDB)

	s := NewPlatformScraper(f.library)
	assert.Equal(t, "playnite", s.ID)
	assert.Equal(t, playnite.Systems(), s.SupportedSystemIDs)
	assert.Equal(t, []string{playnite.LauncherID}, s.AutoScrapeLaunchers)
	assert.True(t, s.SupportsFillMissing)

	ch := make(chan scraper.ScrapeUpdate, 16)
	require.NoError(t, s.Scrape(
		context.Background(), nil, nil, f.fs, &database.Database{MediaDB: mediaDB},
		scraper.ScrapeOptions{}, nil, ch,
	))
	updates := drain(t, ch)
	final := updates[len(updates)-1]
	assert.Equal(t, 5, final.Processed, "playnite rows not yet scraped, across both systems")
	assert.Equal(t, 3, final.Matched)
	assert.Equal(t, 2, final.Skipped, "the game gone from Playnite and the unparseable path")
	assert.Equal(t, 2, final.TotalSteps)
	for _, update := range updates {
		require.NoError(t, update.FatalErr)
	}

	require.Len(t, *writes, 3)
	byMedia := make(map[int64]database.ScrapeWriteTarget)
	for _, w := range *writes {
		byMedia[w.MediaDBID] = w
	}

	rich := byMedia[100]
	require.NotNil(t, rich.Write)
	assert.Equal(t, int64(1), rich.MediaTitleDBID)
	assert.Equal(t, scraper.SentinelTagInfo(scraperID), rich.Write.Sentinel)
	assert.Empty(t, rich.Write.MediaTags, "no run tag without a run ID")
	assert.Equal(t, []string{"1989"}, tagValue(rich.Write, tags.TagTypeYear))
	developer := tags.NormalizeTagValue(string(tags.TagTypeDeveloper), "Test Developer")
	assert.Equal(t, []string{developer}, tagValue(rich.Write, tags.TagTypeDeveloper),
		"repeated and blank companies collapse")
	assert.Equal(t, []string{tags.NormalizeTagValue(string(tags.TagTypePublisher), "Test Publisher")},
		tagValue(rich.Write, tags.TagTypePublisher))
	genres := tagValue(rich.Write, tags.TagTypeGenre)
	assert.NotEmpty(t, genres, "Platform and Shooter map onto the vocabulary")
	for _, genre := range genres {
		assert.NotContains(t, genre, "nonsense", "an unknown genre is dropped, not invented")
	}
	for _, tag := range rich.Write.TitleTags {
		if tag.Type == string(tags.TagTypeDeveloper) || tag.Type == string(tags.TagTypePublisher) {
			assert.NotEmpty(t, tag.Label)
			continue
		}
		assert.Emptyf(t, tag.Label, "closed-type tag %s:%s carries a label", tag.Type, tag.Tag)
	}
	assert.Equal(t, "A test description. Second&line",
		propText(rich.Write.TitleProps, tags.TagPropertyDescription), "HTML is reduced to its text")
	assert.Equal(t, filepath.ToSlash(f.cover), propText(rich.Write.MediaProps, tags.TagPropertyImageBoxart))
	assert.Len(t, rich.Write.MediaProps, 1, "a background that is not a file is left out; the icon is not imported")

	sparse := byMedia[101]
	require.NotNil(t, sparse.Write)
	assert.Empty(t, sparse.Write.TitleTags, "blank fields produce no tags")
	assert.Empty(t, sparse.Write.TitleProps)
	assert.Empty(t, sparse.Write.MediaProps, "a relative image path is not trusted")

	pc := byMedia[200]
	require.NotNil(t, pc.Write)
	assert.Equal(t, []string{"2020"}, tagValue(pc.Write, tags.TagTypeYear))
	mediaDB.AssertExpectations(t)
}

func TestScrapeForceRescrapesAndMarksRun(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	mediaDB := testhelpers.NewMockMediaDBI()
	expectSystem(mediaDB, systemdefs.SystemNES, 7,
		media(100, 1, systemdefs.SystemNES, playnite.GamePath(idRich, "Rich Game")))
	mediaDB.On("GetScrapeRunMediaIDs", mock.Anything, scraperID, "run-1", int64(7)).
		Return(map[int64]struct{}{}, nil).Once()
	writes := captureWrites(t, mediaDB)

	ch := make(chan scraper.ScrapeUpdate, 16)
	require.NoError(t, NewPlatformScraper(f.library).Scrape(
		context.Background(), nil, nil, f.fs, &database.Database{MediaDB: mediaDB},
		scraper.ScrapeOptions{Force: true, RunID: "run-1", Systems: []string{"nes"}}, nil, ch,
	))
	drain(t, ch)
	require.Len(t, *writes, 1)
	assert.Contains(t, (*writes)[0].Write.MediaTags, scraper.RunTagInfo(scraperID, "run-1"))
	assert.False(t, (*writes)[0].Write.FillMissing)
	mediaDB.AssertNotCalled(t, "GetScrapedMediaIDs", mock.Anything, mock.Anything, mock.Anything)
	mediaDB.AssertNotCalled(t, "GetMediaBySystemID", systemdefs.SystemPC)
}

func TestScrapeFillMissingResumesOnlyUnfinishedRows(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	mediaDB := testhelpers.NewMockMediaDBI()
	expectSystem(mediaDB, systemdefs.SystemNES, 7,
		media(100, 1, systemdefs.SystemNES, playnite.GamePath(idRich, "Done")),
		media(101, 2, systemdefs.SystemNES, playnite.GamePath(idSparse, "Pending")))
	mediaDB.On("GetScrapeRunMediaIDs", mock.Anything, scraperID, "resume", int64(7)).
		Return(map[int64]struct{}{100: {}}, nil).Once()
	writes := captureWrites(t, mediaDB)

	ch := make(chan scraper.ScrapeUpdate, 16)
	require.NoError(t, NewPlatformScraper(f.library).Scrape(
		t.Context(), nil, nil, f.fs, &database.Database{MediaDB: mediaDB},
		scraper.ScrapeOptions{FillMissing: true, RunID: "resume", Systems: []string{systemdefs.SystemNES}}, nil, ch,
	))
	drain(t, ch)
	require.Len(t, *writes, 1)
	require.Equal(t, int64(101), (*writes)[0].MediaDBID)
	require.True(t, (*writes)[0].Write.FillMissing)
	mediaDB.AssertExpectations(t)
}

func TestScrapeSkipsSystemsPlayniteHasNoGamesFor(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	mediaDB := testhelpers.NewMockMediaDBI()

	ch := make(chan scraper.ScrapeUpdate, 4)
	require.NoError(t, NewPlatformScraper(f.library).Scrape(
		context.Background(), nil, nil, f.fs, &database.Database{MediaDB: mediaDB},
		scraper.ScrapeOptions{Systems: []string{systemdefs.SystemSNES}}, nil, ch,
	))
	updates := drain(t, ch)
	assert.Len(t, updates, 1)
	assert.Equal(t, 0, updates[0].Processed)
	mediaDB.AssertNotCalled(t, "GetTitlesBySystemID", mock.Anything)
}

func TestScrapeSkipsSystemNotInTheIndex(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	mediaDB := testhelpers.NewMockMediaDBI()
	mediaDB.On("GetTitlesBySystemID", mock.Anything).Return([]database.TitleWithSystem{}, nil)

	ch := make(chan scraper.ScrapeUpdate, 4)
	require.NoError(t, NewPlatformScraper(f.library).Scrape(
		context.Background(), nil, nil, f.fs, &database.Database{MediaDB: mediaDB},
		scraper.ScrapeOptions{}, nil, ch,
	))
	updates := drain(t, ch)
	assert.Equal(t, 0, updates[len(updates)-1].Processed)
	mediaDB.AssertNotCalled(t, "GetMediaBySystemID", mock.Anything)
}

func TestScrapeErrors(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	db := func() *database.Database { return &database.Database{MediaDB: testhelpers.NewMockMediaDBI()} }
	run := func(s Library, d *database.Database, opts scraper.ScrapeOptions) error {
		return NewPlatformScraper(s).Scrape(
			context.Background(), nil, nil, f.fs, d, opts, nil, make(chan scraper.ScrapeUpdate, 1),
		)
	}

	t.Run("playnite not reachable", func(t *testing.T) {
		t.Parallel()
		unreachable := func(context.Context) ([]playnite.Game, error) { return nil, playnite.ErrNotConnected }
		require.ErrorIs(t, run(unreachable, db(), scraper.ScrapeOptions{}), playnite.ErrNotConnected)
	})
	t.Run("no library reader", func(t *testing.T) {
		t.Parallel()
		require.Error(t, run(nil, db(), scraper.ScrapeOptions{}))
	})
	t.Run("no media database", func(t *testing.T) {
		t.Parallel()
		require.Error(t, run(f.library, nil, scraper.ScrapeOptions{}))
		require.Error(t, run(f.library, &database.Database{}, scraper.ScrapeOptions{}))
	})
	t.Run("fill-missing with force", func(t *testing.T) {
		t.Parallel()
		require.Error(t, run(f.library, db(), scraper.ScrapeOptions{FillMissing: true, Force: true}))
	})
}

func TestScrapeWriteFailureIsFatal(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	mediaDB := testhelpers.NewMockMediaDBI()
	expectSystem(mediaDB, systemdefs.SystemNES, 7,
		media(100, 1, systemdefs.SystemNES, playnite.GamePath(idRich, "Rich Game")))
	mediaDB.On("GetScrapedMediaIDs", mock.Anything, scraperID, int64(7)).
		Return(map[int64]struct{}{}, nil)
	mediaDB.On("ApplyScrapeResult", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(errors.New("disk full"))

	ch := make(chan scraper.ScrapeUpdate, 16)
	require.NoError(t, NewPlatformScraper(f.library).Scrape(
		context.Background(), nil, nil, f.fs, &database.Database{MediaDB: mediaDB},
		scraper.ScrapeOptions{Systems: []string{systemdefs.SystemNES}}, nil, ch,
	))
	updates := drain(t, ch)
	final := updates[len(updates)-1]
	require.ErrorContains(t, final.FatalErr, "disk full")
	assert.Zero(t, final.Matched, "a failed write is not reported as enriched")
}

func TestScrapeLoadFailureIsFatal(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	mediaDB := testhelpers.NewMockMediaDBI()
	mediaDB.On("GetTitlesBySystemID", mock.Anything).Return([]database.TitleWithSystem{}, errors.New("locked"))

	ch := make(chan scraper.ScrapeUpdate, 16)
	require.NoError(t, NewPlatformScraper(f.library).Scrape(
		context.Background(), nil, nil, f.fs, &database.Database{MediaDB: mediaDB},
		scraper.ScrapeOptions{}, nil, ch,
	))
	updates := drain(t, ch)
	require.ErrorContains(t, updates[len(updates)-1].FatalErr, "locked")
}

func TestDescriptionText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "empty", raw: "  ", want: ""},
		{name: "plain text", raw: "Just  text\n here", want: "Just text here"},
		{name: "block elements separate words", raw: "<h1>Title</h1><p>Body</p>", want: "Title Body"},
		{name: "entities decode", raw: "Tom &amp; Jerry&nbsp;&lt;3", want: "Tom & Jerry <3"},
		{name: "script and style are dropped", raw: "a<style>p{}</style>b<script>x()</script>c", want: "a b c"},
		{name: "unclosed markup", raw: "<p>Open <b>bold", want: "Open bold"},
		{name: "image only", raw: `<img src="x.png"/>`, want: ""},
		{name: "control characters", raw: "a\x00b\x07c", want: "a b c"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, descriptionText(tt.raw))
		})
	}
}

func TestDescriptionTextIsBounded(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("word ", maxDescriptionLength)
	got := descriptionText("<p>" + long + "</p>")
	assert.LessOrEqual(t, len([]rune(got)), maxDescriptionLength)
	assert.True(t, strings.HasSuffix(got, "word"), "cut at a word boundary")

	unbroken := strings.Repeat("é", maxDescriptionLength+10)
	assert.Len(t, []rune(descriptionText(unbroken)), maxDescriptionLength, "cut by characters, not bytes")
}
