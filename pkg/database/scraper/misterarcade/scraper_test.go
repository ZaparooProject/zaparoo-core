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

package misterarcade

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper/scrapertest"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const arcadeRoot = "/media/fat/_Arcade"

// descriptor writes a minimal MRA declaring setName.
func descriptor(t *testing.T, fs afero.Fs, name, setName string) string {
	t.Helper()
	path := filepath.Join(arcadeRoot, name+".mra")
	body := "<misterromdescription><name>" + name + "</name><setname>" + setName +
		"</setname><rom index=\"0\"><part>AAAA</part></rom></misterromdescription>"
	require.NoError(t, fs.MkdirAll(arcadeRoot, 0o750))
	require.NoError(t, afero.WriteFile(fs, path, []byte(body), 0o600))
	return path
}

func arcadeMedia(dbID, titleID int64, path string) database.MediaWithFullPath {
	return database.MediaWithFullPath{
		DBID: dbID, MediaTitleDBID: titleID, Path: path, SystemID: systemdefs.SystemArcade,
	}
}

// captureWrites records every per-record write the scraper makes, failing the
// test for any tag in one that the vocabulary would refuse.
func captureWrites(t *testing.T, mediaDB *testhelpers.MockMediaDBI) *[]database.ScrapeWriteTarget {
	t.Helper()
	writes := &[]database.ScrapeWriteTarget{}
	mediaDB.On("ApplyScrapeResult", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			mediaDBID, ok := args.Get(1).(int64)
			if !ok {
				return
			}
			titleDBID, ok := args.Get(2).(int64)
			if !ok {
				return
			}
			write, ok := args.Get(3).(*database.ScrapeWrite)
			if !ok {
				return
			}
			scrapertest.RequireValidWrite(t, write)
			*writes = append(*writes, database.ScrapeWriteTarget{
				MediaDBID: mediaDBID, MediaTitleDBID: titleDBID, Write: write,
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

func fixtureCatalog(entries ...Entry) Catalog {
	return func(platforms.Platform) ([]Entry, error) { return entries, nil }
}

func run(
	t *testing.T, s *platforms.Scraper, fs afero.Fs, db database.MediaDBI, opts scraper.ScrapeOptions,
) []scraper.ScrapeUpdate {
	t.Helper()
	ch := make(chan scraper.ScrapeUpdate, 32)
	require.NoError(t, s.Scrape(
		context.Background(), nil, nil, fs, &database.Database{MediaDB: db}, opts, nil, ch,
	))
	return drain(t, ch)
}

func arcadeTitles() []database.TitleWithSystem {
	return []database.TitleWithSystem{{DBID: 1, SystemID: systemdefs.SystemArcade, SystemDBID: 7}}
}

func TestScraperIsBoundToItsArcadeLaunchers(t *testing.T) {
	t.Parallel()
	systems := []string{systemdefs.SystemArcade, systemdefs.SystemCPS1}
	s := NewPlatformScraper(systems, fixtureCatalog(), nil)

	assert.Equal(t, "mister-arcade", s.ID)
	assert.Equal(t, systems, s.SupportedSystemIDs)
	assert.True(t, s.SupportsFillMissing)
	assert.Equal(t, systems, s.AutoScrapeLaunchers,
		"every arcade launcher's rows deserve the catalog's metadata")
}

func TestScrapeWritesCatalogMetadataToMatchedDescriptors(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	matched := descriptor(t, fs, "1941 - Counter Attack (World)", "1941")
	uncatalogued := descriptor(t, fs, "Some Alternate", "notinthecatalog")
	nameless := filepath.Join(arcadeRoot, "Broken.mra")
	require.NoError(t, afero.WriteFile(fs, nameless, []byte("not xml at all"), 0o600))

	mediaDB := testhelpers.NewMockMediaDBI()
	mediaDB.On("IndexedSystems").Return([]string{systemdefs.SystemArcade}, nil)
	mediaDB.On("GetTitlesBySystemID", systemdefs.SystemArcade).Return(arcadeTitles(), nil)
	mediaDB.On("GetMediaBySystemID", systemdefs.SystemArcade).Return([]database.MediaWithFullPath{
		arcadeMedia(100, 1, matched),
		arcadeMedia(101, 2, uncatalogued),
		arcadeMedia(102, 3, nameless),
		arcadeMedia(103, 4, filepath.Join(arcadeRoot, "Menu.mgl")),
		{DBID: 104, MediaTitleDBID: 5, Path: filepath.Join(arcadeRoot, "Gone.mra"), IsMissing: true},
	}, nil)
	mediaDB.On("GetScrapedMediaIDs", mock.Anything, scraperID, int64(7)).
		Return(map[int64]struct{}{}, nil)
	writes := captureWrites(t, mediaDB)

	entry := cps1Entry()
	s := NewPlatformScraper([]string{systemdefs.SystemArcade}, fixtureCatalog(entry), nil)
	final := run(t, &s, fs, mediaDB, scraper.ScrapeOptions{})

	last := final[len(final)-1]
	assert.Equal(t, 3, last.Processed, "only live .mra rows are candidates")
	assert.Equal(t, 1, last.Matched)
	assert.Equal(t, 2, last.Skipped, "an uncatalogued set and an unreadable descriptor")

	require.Len(t, *writes, 1)
	assert.Equal(t, int64(100), (*writes)[0].MediaDBID)
	assert.Equal(t, int64(1), (*writes)[0].MediaTitleDBID)
	title := (*writes)[0].Write.TitleTags
	assert.Equal(t, []string{"1990"}, tagValues(title, tags.TagTypeYear))
	assert.Equal(t, []string{"shmup:v", "shmup"}, tagValues(title, tags.TagTypeGenre))
	assert.Equal(t, []string{"capcom:cps"}, tagValues(title, tags.TagTypeArcadeBoard))
	assert.Contains(t, tagValues(title, tags.TagTypeSearch), "franchise:19xx")
	assert.False(t, (*writes)[0].Write.FillMissing)
}

func TestScrapePrefersThePlatformSetNameCache(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	// The descriptor on disk declares a different set: a cache hit must win, so
	// a scrape does not re-read thousands of files from slow storage.
	path := descriptor(t, fs, "Cached", "ondisk")

	mediaDB := testhelpers.NewMockMediaDBI()
	mediaDB.On("IndexedSystems").Return([]string{systemdefs.SystemArcade}, nil)
	mediaDB.On("GetTitlesBySystemID", systemdefs.SystemArcade).Return(arcadeTitles(), nil)
	mediaDB.On("GetMediaBySystemID", systemdefs.SystemArcade).
		Return([]database.MediaWithFullPath{arcadeMedia(100, 1, path)}, nil)
	mediaDB.On("GetScrapedMediaIDs", mock.Anything, scraperID, int64(7)).
		Return(map[int64]struct{}{}, nil)
	writes := captureWrites(t, mediaDB)

	calls := 0
	cache := func(queried string) (string, bool) {
		calls++
		assert.Equal(t, path, queried)
		return "Cached", true
	}
	s := NewPlatformScraper(
		[]string{systemdefs.SystemArcade},
		fixtureCatalog(Entry{SetName: "cached", Year: "1984"}),
		cache,
	)
	run(t, &s, fs, mediaDB, scraper.ScrapeOptions{})

	assert.Equal(t, 1, calls)
	require.Len(t, *writes, 1)
	assert.Equal(t, []string{"1984"}, tagValues((*writes)[0].Write.TitleTags, tags.TagTypeYear))
}

func TestScrapeReadsTheDescriptorWhenTheCacheMisses(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	path := descriptor(t, fs, "Uncached", "ondisk")

	mediaDB := testhelpers.NewMockMediaDBI()
	mediaDB.On("IndexedSystems").Return([]string{systemdefs.SystemArcade}, nil)
	mediaDB.On("GetTitlesBySystemID", systemdefs.SystemArcade).Return(arcadeTitles(), nil)
	mediaDB.On("GetMediaBySystemID", systemdefs.SystemArcade).
		Return([]database.MediaWithFullPath{arcadeMedia(100, 1, path)}, nil)
	mediaDB.On("GetScrapedMediaIDs", mock.Anything, scraperID, int64(7)).
		Return(map[int64]struct{}{}, nil)
	writes := captureWrites(t, mediaDB)

	s := NewPlatformScraper(
		[]string{systemdefs.SystemArcade},
		fixtureCatalog(Entry{SetName: "ondisk", Year: "1987"}),
		func(string) (string, bool) { return "", false },
	)
	run(t, &s, fs, mediaDB, scraper.ScrapeOptions{})

	require.Len(t, *writes, 1)
	assert.Equal(t, []string{"1987"}, tagValues((*writes)[0].Write.TitleTags, tags.TagTypeYear))
}

func TestScrapeSkipsRowsItHasAlreadyWritten(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	done := descriptor(t, fs, "Done", "1941")
	pending := descriptor(t, fs, "Pending", "1941")

	mediaDB := testhelpers.NewMockMediaDBI()
	mediaDB.On("IndexedSystems").Return([]string{systemdefs.SystemArcade}, nil)
	mediaDB.On("GetTitlesBySystemID", systemdefs.SystemArcade).Return(arcadeTitles(), nil)
	mediaDB.On("GetMediaBySystemID", systemdefs.SystemArcade).Return([]database.MediaWithFullPath{
		arcadeMedia(100, 1, done), arcadeMedia(101, 1, pending),
	}, nil)
	mediaDB.On("GetScrapedMediaIDs", mock.Anything, scraperID, int64(7)).
		Return(map[int64]struct{}{100: {}}, nil)
	writes := captureWrites(t, mediaDB)

	entry := cps1Entry()
	s := NewPlatformScraper([]string{systemdefs.SystemArcade}, fixtureCatalog(entry), nil)
	updates := run(t, &s, fs, mediaDB, scraper.ScrapeOptions{})

	assert.Equal(t, 1, updates[len(updates)-1].Processed, "a sentinel row is not a candidate")
	require.Len(t, *writes, 1)
	assert.Equal(t, int64(101), (*writes)[0].MediaDBID)
}

func TestFillMissingRevisitsSentinelRowsAndMarksTheWrite(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	path := descriptor(t, fs, "Filled", "1941")

	mediaDB := testhelpers.NewMockMediaDBI()
	mediaDB.On("IndexedSystems").Return([]string{systemdefs.SystemArcade}, nil)
	mediaDB.On("GetTitlesBySystemID", systemdefs.SystemArcade).Return(arcadeTitles(), nil)
	mediaDB.On("GetMediaBySystemID", systemdefs.SystemArcade).
		Return([]database.MediaWithFullPath{arcadeMedia(100, 1, path)}, nil)
	// A fill-missing run consults its own run markers, never the permanent
	// sentinel, so a later index can fill fields the catalog only just gained.
	mediaDB.On("GetScrapeRunMediaIDs", mock.Anything, scraperID, "run-1", int64(7)).
		Return(map[int64]struct{}{}, nil)
	writes := captureWrites(t, mediaDB)

	entry := cps1Entry()
	s := NewPlatformScraper([]string{systemdefs.SystemArcade}, fixtureCatalog(entry), nil)
	run(t, &s, fs, mediaDB, scraper.ScrapeOptions{FillMissing: true, RunID: "run-1"})

	require.Len(t, *writes, 1)
	assert.True(t, (*writes)[0].Write.FillMissing)
	assert.Equal(t, []string{"run-1"}, tagValues((*writes)[0].Write.MediaTags, tags.ScraperRunType(scraperID)))
	mediaDB.AssertNotCalled(t, "GetScrapedMediaIDs", mock.Anything, mock.Anything, mock.Anything)
}

func TestScrapeRejectsFillMissingWithForce(t *testing.T) {
	t.Parallel()
	s := NewPlatformScraper([]string{systemdefs.SystemArcade}, fixtureCatalog(), nil)
	err := s.Scrape(
		context.Background(), nil, nil, afero.NewMemMapFs(),
		&database.Database{MediaDB: testhelpers.NewMockMediaDBI()},
		scraper.ScrapeOptions{FillMissing: true, Force: true}, nil, make(chan scraper.ScrapeUpdate, 1),
	)
	require.Error(t, err)
}

func TestScrapeFailsWhenTheCatalogCannotBeRead(t *testing.T) {
	t.Parallel()
	// A MiSTer that has not yet cached or embedded the catalog must fail the
	// request rather than write an empty result over real metadata.
	s := NewPlatformScraper([]string{systemdefs.SystemArcade}, func(platforms.Platform) ([]Entry, error) {
		return nil, errors.New("no catalog available")
	}, nil)
	err := s.Scrape(
		context.Background(), nil, nil, afero.NewMemMapFs(),
		&database.Database{MediaDB: testhelpers.NewMockMediaDBI()},
		scraper.ScrapeOptions{}, nil, make(chan scraper.ScrapeUpdate, 1),
	)
	require.ErrorContains(t, err, "no catalog available")
}

func TestScrapeStopsOnAWriteFailure(t *testing.T) {
	t.Parallel()
	// Fill-missing work that committed only part of its rows must not report
	// success, or the sentinel would keep the rest from ever being filled.
	fs := afero.NewMemMapFs()
	path := descriptor(t, fs, "Doomed", "1941")

	mediaDB := testhelpers.NewMockMediaDBI()
	mediaDB.On("IndexedSystems").Return([]string{systemdefs.SystemArcade}, nil)
	mediaDB.On("GetTitlesBySystemID", systemdefs.SystemArcade).Return(arcadeTitles(), nil)
	mediaDB.On("GetMediaBySystemID", systemdefs.SystemArcade).
		Return([]database.MediaWithFullPath{arcadeMedia(100, 1, path)}, nil)
	mediaDB.On("GetScrapedMediaIDs", mock.Anything, scraperID, int64(7)).
		Return(map[int64]struct{}{}, nil)
	mediaDB.On("ApplyScrapeResult", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(errors.New("disk full"))

	entry := cps1Entry()
	s := NewPlatformScraper([]string{systemdefs.SystemArcade}, fixtureCatalog(entry), nil)
	updates := run(t, &s, fs, mediaDB, scraper.ScrapeOptions{})

	last := updates[len(updates)-1]
	require.Error(t, last.FatalErr)
	require.ErrorContains(t, last.FatalErr, "disk full")
	assert.Zero(t, last.Matched)
}

func TestTargetSystems(t *testing.T) {
	t.Parallel()
	supported := []string{systemdefs.SystemArcade, systemdefs.SystemCPS1, systemdefs.SystemCPS2}

	assert.Equal(t, []string{systemdefs.SystemArcade, systemdefs.SystemCPS1},
		targetSystems(supported, []string{systemdefs.SystemCPS1, systemdefs.SystemArcade, "snes"}, nil),
		"only indexed systems, in the scraper's own order")

	assert.Equal(t, []string{systemdefs.SystemCPS2},
		targetSystems(supported, supported, []string{"cps2"}),
		"a request narrows the set, case-insensitively")

	assert.Empty(t, targetSystems(supported, supported, []string{"snes"}),
		"a request for an unrelated system selects nothing")
}

func TestSharedTitleWritesAreOrderedByPath(t *testing.T) {
	t.Parallel()
	// Regional variants of one game share a title, and a fill-missing run gives
	// that title's exclusive tags to whichever variant is written first. The
	// catalog disagrees with itself for a handful of set names, so which one
	// that is must not depend on the order the database happened to return.
	fs := afero.NewMemMapFs()
	japan := descriptor(t, fs, "1941 - Counter Attack (Japan)", "1941j")
	world := descriptor(t, fs, "1941 - Counter Attack (World)", "1941")

	mediaDB := testhelpers.NewMockMediaDBI()
	mediaDB.On("IndexedSystems").Return([]string{systemdefs.SystemArcade}, nil)
	mediaDB.On("GetTitlesBySystemID", systemdefs.SystemArcade).Return(arcadeTitles(), nil)
	mediaDB.On("GetMediaBySystemID", systemdefs.SystemArcade).Return([]database.MediaWithFullPath{
		arcadeMedia(101, 1, world),
		arcadeMedia(100, 1, japan),
	}, nil)
	mediaDB.On("GetScrapeRunMediaIDs", mock.Anything, scraperID, "run-1", int64(7)).
		Return(map[int64]struct{}{}, nil)
	writes := captureWrites(t, mediaDB)

	world1941 := cps1Entry()
	japan1941 := cps1Entry()
	japan1941.SetName = "1941j"
	japan1941.Year = "1991"
	s := NewPlatformScraper([]string{systemdefs.SystemArcade}, fixtureCatalog(world1941, japan1941), nil)
	run(t, &s, fs, mediaDB, scraper.ScrapeOptions{FillMissing: true, RunID: "run-1"})

	require.Len(t, *writes, 2)
	assert.Equal(t, []int64{100, 101},
		[]int64{(*writes)[0].MediaDBID, (*writes)[1].MediaDBID},
		"writes follow descriptor path order, not the order the database returned")
	assert.Equal(t, []string{"1991"}, tagValues((*writes)[0].Write.TitleTags, tags.TagTypeYear),
		"the lexicographically first descriptor is the one whose title tags land first")
	assert.True(t, (*writes)[0].Write.FillMissing)
}

func TestScrapeRunCollectsValuesWithNoMapping(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	path := descriptor(t, fs, "Odd Game", "oddgame")

	mediaDB := testhelpers.NewMockMediaDBI()
	mediaDB.On("GetTitlesBySystemID", systemdefs.SystemArcade).Return(arcadeTitles(), nil)
	mediaDB.On("GetMediaBySystemID", systemdefs.SystemArcade).
		Return([]database.MediaWithFullPath{arcadeMedia(100, 1, path)}, nil)
	mediaDB.On("GetScrapedMediaIDs", mock.Anything, scraperID, int64(7)).
		Return(map[int64]struct{}{}, nil)
	writes := captureWrites(t, mediaDB)

	entry := Entry{SetName: "oddgame", Category: "Unheard Of - Genre", Platform: "Mystery Board", Year: "1990"}
	impl := &scraperImpl{
		fs: fs, db: mediaDB, entries: index([]Entry{entry}), unmapped: &scraper.UnmappedValues{},
	}
	ch := make(chan scraper.ScrapeUpdate, 32)
	impl.scrapeLoop(context.Background(), scraper.ScrapeOptions{}, []string{systemdefs.SystemArcade}, ch)
	drain(t, ch)

	require.Len(t, *writes, 1)
	assert.Equal(t, []string{"1990"}, tagValues((*writes)[0].Write.TitleTags, tags.TagTypeYear))
	assert.Equal(t, 1, impl.unmapped.Count(tags.TagTypeGenre), "the run keeps the dropped category")
	assert.Equal(t, 1, impl.unmapped.Count(tags.TagTypeArcadeBoard), "the run keeps the dropped board")
}
