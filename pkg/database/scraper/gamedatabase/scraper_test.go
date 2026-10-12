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

package gamedatabase

import (
	"context"
	"path/filepath"
	"slices"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediadb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/scantest"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These tests run the scraper against the embedded data and a real media
// database indexed through the production scan pipeline, so a title is
// matched on the slug the indexer really gives its file.

func rom(system, name string) string {
	return filepath.Join(string(filepath.Separator), "roms", system, name)
}

func newLibrary(t *testing.T) *mediadb.MediaDB {
	t.Helper()
	db, cleanup := testhelpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)
	return db
}

func runScraper(
	t *testing.T, db database.MediaDBI, fs afero.Fs, cache SetNameCache, opts scraper.ScrapeOptions,
) scraper.ScrapeUpdate {
	t.Helper()
	s := NewPlatformScraper(cache)
	ch := make(chan scraper.ScrapeUpdate, 32)
	require.NoError(t, s.Scrape(
		context.Background(), nil, nil, fs, &database.Database{MediaDB: db}, opts, nil, ch,
	))
	var last scraper.ScrapeUpdate
	for update := range ch {
		last = update
	}
	require.True(t, last.Done)
	require.NoError(t, last.FatalErr)
	return last
}

// library reads back what the scraper stored.
type library struct {
	t     *testing.T
	db    *mediadb.MediaDB
	media map[string]database.MediaWithFullPath
}

func read(t *testing.T, db *mediadb.MediaDB, systemID string) *library {
	t.Helper()
	rows, err := db.GetMediaBySystemID(systemID)
	require.NoError(t, err)
	lib := &library{t: t, db: db, media: make(map[string]database.MediaWithFullPath, len(rows))}
	for i := range rows {
		lib.media[rows[i].Path] = rows[i]
	}
	return lib
}

func values(infos []database.TagInfo, tagType tags.TagType) []string {
	var out []string
	for i := range infos {
		if infos[i].Type == string(tagType) {
			out = append(out, infos[i].Tag)
		}
	}
	slices.Sort(out)
	return out
}

// title returns the title-level tags of the title a path belongs to.
func (l *library) title(path string, tagType tags.TagType) []string {
	l.t.Helper()
	row, ok := l.media[path]
	require.True(l.t, ok, "%s is not indexed", path)
	infos, err := l.db.GetMediaTitleTagsByMediaTitleDBID(context.Background(), row.MediaTitleDBID)
	require.NoError(l.t, err)
	return values(infos, tagType)
}

// file returns the media-level tags of a path.
func (l *library) file(path string, tagType tags.TagType) []string {
	l.t.Helper()
	row, ok := l.media[path]
	require.True(l.t, ok, "%s is not indexed", path)
	byMedia, err := l.db.GetMediaTagsByMediaDBIDs(context.Background(), []int64{row.DBID})
	require.NoError(l.t, err)
	return values(byMedia[row.DBID], tagType)
}

// scraped reports whether a path carries the scraper's sentinel.
func (l *library) scraped(path string) bool {
	l.t.Helper()
	sentinel := scraper.SentinelTagInfo(scraperID)
	return slices.Contains(l.file(path, tags.TagType(sentinel.Type)), sentinel.Tag)
}

func (l *library) setName(path string) string {
	l.t.Helper()
	row, ok := l.media[path]
	require.True(l.t, ok, "%s is not indexed", path)
	props, err := l.db.GetMediaPropertiesByMediaDBIDs(context.Background(), []int64{row.DBID})
	require.NoError(l.t, err)
	for _, prop := range props[row.DBID] {
		if prop.TypeTag == tags.PropertyTypeTag(tags.TagPropertyMAMESetName) {
			return prop.Text
		}
	}
	return ""
}

func TestScraperRunsAfterEveryIndexOnTheSystemsItCovers(t *testing.T) {
	t.Parallel()
	s := NewPlatformScraper(nil)
	assert.Equal(t, "gamedatabase", s.ID)
	assert.True(t, s.SupportsFillMissing)
	assert.True(t, s.AutoScrapeAllLaunchers)
	assert.Equal(t, scraper.AutoOrderCatalog, s.AutoScrapeOrder)
	assert.Equal(t, SupportedSystems(), s.SupportedSystemIDs)
}

func TestScrapeDescribesATitleFromTheRowForItsFilesRegion(t *testing.T) {
	t.Parallel()
	db := newLibrary(t)
	europe := rom("SNES", "Super Mario World (Europe).sfc")
	unknown := rom("SNES", "Definitely Not A Real Game (USA).sfc")
	scantest.IndexMediaPaths(t, db, systemdefs.SystemSNES, europe, unknown)

	last := runScraper(t, db, nil, nil, scraper.ScrapeOptions{})
	assert.Equal(t, 2, last.Processed)
	assert.Equal(t, 1, last.Matched)
	assert.Equal(t, 1, last.Skipped)

	lib := read(t, db, systemdefs.SystemSNES)
	assert.Equal(t, []string{"1992"}, lib.title(europe, tags.TagTypeYear), "the European release, not the first one")
	assert.Equal(t, []string{"supermarioworld"}, lib.title(europe, tags.TagTypeGameDatabase))
	assert.Equal(t, []string{"action", "action:platformer"}, lib.title(europe, tags.TagTypeGenre))
	assert.Equal(t, []string{"2", "alt"}, lib.title(europe, tags.TagTypePlayers))
	assert.Equal(t, []string{"backup"}, lib.title(europe, tags.TagTypeSave))
	assert.Equal(t, []string{"feature:mario"}, lib.title(europe, tags.TagTypeSearch))
	assert.Equal(t, []string{"nintendo"}, lib.title(europe, tags.TagTypePublisher))
	assert.Len(t, lib.title(europe, tags.TagTypeDeveloper), 1, "a title holds one developer")
	assert.Len(t, lib.title(europe, tags.TagTypeCredit), 1, "the second studio named is kept as a credit")
	assert.True(t, lib.scraped(europe))

	assert.Empty(t, lib.title(europe, tags.TagTypeLang), "a release's language is not a fact about the title")
	assert.Empty(t, lib.title(europe, tags.TagTypeRegion))
	assert.False(t, lib.scraped(unknown))
	assert.Empty(t, lib.title(unknown, tags.TagTypeGenre))
}

func TestScrapeFallsBackToTheConfiguredRegions(t *testing.T) {
	t.Parallel()
	db := newLibrary(t)
	bare := rom("SNES", "Super Mario World.sfc")
	scantest.IndexMediaPaths(t, db, systemdefs.SystemSNES, bare)

	runScraper(t, db, nil, nil, scraper.ScrapeOptions{})

	lib := read(t, db, systemdefs.SystemSNES)
	assert.Equal(t, []string{"1991"}, lib.title(bare, tags.TagTypeYear),
		"with no region on the file, the default regions pick the USA retail release over its prototype")
}

func TestScrapeMatchesOnTheTitleScreenName(t *testing.T) {
	t.Parallel()
	db := newLibrary(t)
	// Upstream titles this "Super Alfred Chicken"; the cartridge and its dump
	// are named for the title screen.
	path := rom("SNES", "Alfred Chicken (USA).sfc")
	scantest.IndexMediaPaths(t, db, systemdefs.SystemSNES, path)

	last := runScraper(t, db, nil, nil, scraper.ScrapeOptions{})
	assert.Equal(t, 1, last.Matched)
	assert.Equal(t, []string{"alfredchicken"}, read(t, db, systemdefs.SystemSNES).title(path, tags.TagTypeGameDatabase))
}

func TestScrapeMatchesAJapaneseTitleSpelledWithoutMacrons(t *testing.T) {
	t.Parallel()
	db := newLibrary(t)
	// Upstream titles this "Bishōjo Senshi Sailor Moon".
	path := rom("SNES", "Bishoujo Senshi Sailor Moon (Japan).sfc")
	scantest.IndexMediaPaths(t, db, systemdefs.SystemSNES, path)

	last := runScraper(t, db, nil, nil, scraper.ScrapeOptions{})
	assert.Equal(t, 1, last.Matched)
	assert.Equal(t, []string{"bishoujosenshisailormoon"},
		read(t, db, systemdefs.SystemSNES).title(path, tags.TagTypeGameDatabase))
}

func TestScrapeLeavesATitleTwoGamesShare(t *testing.T) {
	t.Parallel()
	db := newLibrary(t)
	// Two different NES games were released in the USA under this name, by
	// different publishers. The name alone does not say which one this is.
	path := rom("NES", "Indiana Jones and the Last Crusade (USA).nes")
	scantest.IndexMediaPaths(t, db, systemdefs.SystemNES, path)

	last := runScraper(t, db, nil, nil, scraper.ScrapeOptions{})
	assert.Equal(t, 0, last.Matched)
	assert.Equal(t, 1, last.Skipped)
	assert.False(t, read(t, db, systemdefs.SystemNES).scraped(path))
}

func TestScrapePassesOverAPrototypeThatSharesATitle(t *testing.T) {
	t.Parallel()
	db := newLibrary(t)
	// Upstream lists the retail game and, under another ID, an unfinished
	// sample of it. The sample does not make the title ambiguous.
	path := rom("Gameboy", "Hook (USA).gb")
	scantest.IndexMediaPaths(t, db, systemdefs.SystemGameboy, path)

	last := runScraper(t, db, nil, nil, scraper.ScrapeOptions{})
	assert.Equal(t, 1, last.Matched)
	assert.Equal(t, []string{"hook8bit"}, read(t, db, systemdefs.SystemGameboy).title(path, tags.TagTypeGameDatabase))
}

func TestScrapeRecordsWhatACompilationContains(t *testing.T) {
	t.Parallel()
	db := newLibrary(t)
	path := rom("SNES", "Caravan Shooting Collection (Japan).sfc")
	scantest.IndexMediaPaths(t, db, systemdefs.SystemSNES, path)

	runScraper(t, db, nil, nil, scraper.ScrapeOptions{})

	lib := read(t, db, systemdefs.SystemSNES)
	assert.Equal(t, []string{"hector87", "starforce", "starsoldier"}, lib.title(path, tags.TagTypeCompilation))
	assert.Equal(t, []string{"compilation"}, lib.title(path, tags.TagTypeMultigame))
	assert.Equal(t, []string{"caravanshootingcollection"}, lib.title(path, tags.TagTypeGameDatabase))
}

func TestScrapeSplitsJointDevelopersIntoOneDeveloperAndCredits(t *testing.T) {
	t.Parallel()
	db := newLibrary(t)
	path := rom("VirtualBoy", "Mario's Tennis (Japan, USA).vb")
	scantest.IndexMediaPaths(t, db, systemdefs.SystemVirtualBoy, path)

	runScraper(t, db, nil, nil, scraper.ScrapeOptions{})

	lib := read(t, db, systemdefs.SystemVirtualBoy)
	assert.Equal(t, []string{"nintendo"}, lib.title(path, tags.TagTypeDeveloper))
	assert.Equal(t, []string{"tose"}, lib.title(path, tags.TagTypeCredit))
	assert.Equal(t, []string{"nintendo"}, lib.title(path, tags.TagTypePublisher))
}

func TestScrapeMarksEveryFileOfATitleAndWritesTheTitleOnce(t *testing.T) {
	t.Parallel()
	db := newLibrary(t)
	first := rom("SNES", "Super Mario World (USA).sfc")
	second := rom("SNES", "Super Mario World (USA) (Rev 1).sfc")
	scantest.IndexMediaPaths(t, db, systemdefs.SystemSNES, first, second)

	last := runScraper(t, db, nil, nil, scraper.ScrapeOptions{})
	assert.Equal(t, 2, last.Matched)

	lib := read(t, db, systemdefs.SystemSNES)
	require.Equal(t, lib.media[first].MediaTitleDBID, lib.media[second].MediaTitleDBID, "one title, two files")
	assert.True(t, lib.scraped(first))
	assert.True(t, lib.scraped(second))
	assert.Equal(t, []string{"1991"}, lib.title(first, tags.TagTypeYear))

	again := runScraper(t, db, nil, nil, scraper.ScrapeOptions{})
	assert.Equal(t, 0, again.Processed, "an ordinary run leaves rows it has already written")
}

func TestScrapeUsesAnotherSystemsBundleOnlyForWhatItsOwnLacks(t *testing.T) {
	t.Parallel()
	db := newLibrary(t)
	// A Game Boy cartridge kept in the Game Boy Color folder.
	path := rom("GameboyColor", "Donkey Kong Land (USA, Europe).gb")
	scantest.IndexMediaPaths(t, db, systemdefs.SystemGameboyColor, path)

	last := runScraper(t, db, nil, nil, scraper.ScrapeOptions{})
	assert.Equal(t, 1, last.Matched)
	assert.Equal(t, []string{"donkeykongland"},
		read(t, db, systemdefs.SystemGameboyColor).title(path, tags.TagTypeGameDatabase))
}

func TestFillMissingKeepsWhatAnEarlierSourceWrote(t *testing.T) {
	t.Parallel()
	db := newLibrary(t)
	path := rom("SNES", "Super Mario World (USA).sfc")
	scantest.IndexMediaPaths(t, db, systemdefs.SystemSNES, path)
	lib := read(t, db, systemdefs.SystemSNES)
	require.NoError(t, db.UpsertMediaTitleTags(context.Background(), lib.media[path].MediaTitleDBID,
		[]database.TagInfo{
			{Type: string(tags.TagTypeYear), Tag: "1990"},
			{Type: string(tags.TagTypeGenre), Tag: string(tags.TagGameGenrePuzzle)},
		}))

	runScraper(t, db, nil, nil, scraper.ScrapeOptions{FillMissing: true})

	assert.Equal(t, []string{"1990"}, lib.title(path, tags.TagTypeYear), "a single-valued field is not replaced")
	assert.Equal(t, []string{"action", "action:platformer", "puzzle"}, lib.title(path, tags.TagTypeGenre),
		"an additive field gains values")
	assert.Equal(t, []string{"nintendo"}, lib.title(path, tags.TagTypePublisher), "an empty field is filled")
}

func TestFillMissingSkipsASystemUntilItsLibraryChanges(t *testing.T) {
	t.Parallel()
	db := newLibrary(t)
	first := rom("SNES", "Super Mario World (USA).sfc")
	scantest.IndexMediaPaths(t, db, systemdefs.SystemSNES, first)

	opts := scraper.ScrapeOptions{FillMissing: true}
	assert.Equal(t, 1, runScraper(t, db, nil, nil, opts).Matched)
	assert.Equal(t, 0, runScraper(t, db, nil, nil, opts).Processed, "nothing changed, so nothing is read")

	second := rom("SNES", "Super Mario Kart (USA).sfc")
	scantest.IndexMediaPaths(t, db, systemdefs.SystemSNES, first, second)
	last := runScraper(t, db, nil, nil, opts)
	assert.Equal(t, 2, last.Processed, "an index that changed the system is scraped again")
	assert.Equal(t, []string{"supermariokart"},
		read(t, db, systemdefs.SystemSNES).title(second, tags.TagTypeGameDatabase))
}

func TestScrapeMatchesArcadeArchivesBySetName(t *testing.T) {
	t.Parallel()
	db := newLibrary(t)
	parent := rom("arcade", "10yard.zip")
	clone := rom("arcade", "10yardj.zip")
	unlisted := rom("arcade", "notamameset.zip")
	scantest.IndexMediaPaths(t, db, systemdefs.SystemArcade, parent, clone, unlisted)

	last := runScraper(t, db, nil, nil, scraper.ScrapeOptions{})
	assert.Equal(t, 2, last.Matched)
	assert.Equal(t, 1, last.Skipped)

	lib := read(t, db, systemdefs.SystemArcade)
	assert.Equal(t, "10yard", lib.setName(parent))
	assert.Empty(t, lib.file(parent, tags.TagTypeMameParent), "a parent set has no parent")
	assert.Equal(t, "10yardj", lib.setName(clone))
	assert.Equal(t, []string{"10yard"}, lib.file(clone, tags.TagTypeMameParent))
	assert.Equal(t, []string{"1983"}, lib.title(clone, tags.TagTypeYear))
	assert.Equal(t, []string{"irem:m58"}, lib.title(clone, tags.TagTypeArcadeBoard), "one board, the most specific")
	assert.Equal(t, []string{"sports", "sports:football"}, lib.title(clone, tags.TagTypeGenre))
	assert.Equal(t, []string{"buttons:2", "joystick:8"}, lib.title(clone, tags.TagTypeInput))
	assert.False(t, lib.scraped(unlisted))
}

func TestScrapeReadsADescriptorsSetNameFromTheCacheThenTheFile(t *testing.T) {
	t.Parallel()
	db := newLibrary(t)
	fs := afero.NewMemMapFs()
	cached := filepath.Join(string(filepath.Separator), "media", "fat", "_Arcade", "Ten Yard Fight (Japan).mra")
	onDisk := filepath.Join(string(filepath.Separator), "media", "fat", "_Arcade", "Versus Ten Yard Fight.mra")
	require.NoError(t, fs.MkdirAll(filepath.Dir(onDisk), 0o750))
	require.NoError(t, afero.WriteFile(fs, onDisk, []byte(
		"<misterromdescription><name>Vs</name><setname>vs10yard</setname>"+
			"<rom index=\"0\"><part>AAAA</part></rom></misterromdescription>"), 0o600))
	scantest.IndexMediaPaths(t, db, systemdefs.SystemArcade, cached, onDisk)

	cache := func(path string) (string, bool) {
		if path == cached {
			return "10YardJ", true
		}
		return "", false
	}
	last := runScraper(t, db, fs, cache, scraper.ScrapeOptions{})
	assert.Equal(t, 2, last.Matched)

	lib := read(t, db, systemdefs.SystemArcade)
	assert.Equal(t, "10yardj", lib.setName(cached), "the platform's cache answers without a file read")
	assert.Equal(t, "vs10yard", lib.setName(onDisk), "a cache miss reads the descriptor")
	assert.Equal(t, []string{"2", "vs"}, lib.title(onDisk, tags.TagTypePlayers))
}

func TestScopedScrapeWritesOnlyTheSelectedFile(t *testing.T) {
	t.Parallel()
	db := newLibrary(t)
	selected := rom("SNES", "Super Mario World (USA).sfc")
	other := rom("SNES", "Super Mario Kart (USA).sfc")
	scantest.IndexMediaPaths(t, db, systemdefs.SystemSNES, selected, other)

	lib := read(t, db, systemdefs.SystemSNES)
	last := runScraper(t, db, nil, nil, scraper.ScrapeOptions{
		Scope: &database.ScrapeScope{
			SystemID: systemdefs.SystemSNES, Path: selected, MediaID: lib.media[selected].DBID,
		},
	})
	assert.Equal(t, 1, last.Matched)

	assert.True(t, lib.scraped(selected))
	assert.Equal(t, []string{"supermarioworld"}, lib.title(selected, tags.TagTypeGameDatabase))
	assert.False(t, lib.scraped(other))
	assert.Empty(t, lib.title(other, tags.TagTypeGameDatabase))
}

func TestScopedFillMissingKeepsWhatAnEarlierSourceWrote(t *testing.T) {
	t.Parallel()
	db := newLibrary(t)
	selected := rom("SNES", "Super Mario World (USA).sfc")
	other := rom("SNES", "Super Mario Kart (USA).sfc")
	scantest.IndexMediaPaths(t, db, systemdefs.SystemSNES, selected, other)

	lib := read(t, db, systemdefs.SystemSNES)
	require.NoError(t, db.UpsertMediaTitleTags(context.Background(), lib.media[selected].MediaTitleDBID,
		[]database.TagInfo{
			{Type: string(tags.TagTypeYear), Tag: "1990"},
			{Type: string(tags.TagTypeGenre), Tag: string(tags.TagGameGenrePuzzle)},
		}))

	last := runScraper(t, db, nil, nil, scraper.ScrapeOptions{
		FillMissing: true,
		Scope: &database.ScrapeScope{
			SystemID: systemdefs.SystemSNES, Path: selected, MediaID: lib.media[selected].DBID,
		},
	})
	assert.Equal(t, 1, last.Matched)
	assert.Equal(t, []string{"1990"}, lib.title(selected, tags.TagTypeYear), "a single-valued field is not replaced")
	assert.Equal(t, []string{"action", "action:platformer", "puzzle"}, lib.title(selected, tags.TagTypeGenre),
		"an additive field gains values")
	assert.Equal(t, []string{"nintendo"}, lib.title(selected, tags.TagTypePublisher), "an empty field is filled")
	assert.True(t, lib.scraped(selected))
	assert.False(t, lib.scraped(other))
	assert.Empty(t, lib.title(other, tags.TagTypePublisher))
}

func TestScrapeStopsWhenCancelled(t *testing.T) {
	t.Parallel()
	db := newLibrary(t)
	scantest.IndexMediaPaths(t, db, systemdefs.SystemSNES, rom("SNES", "Super Mario World (USA).sfc"))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s := NewPlatformScraper(nil)
	ch := make(chan scraper.ScrapeUpdate, 32)
	require.NoError(t, s.Scrape(ctx, nil, nil, nil, &database.Database{MediaDB: db}, scraper.ScrapeOptions{}, nil, ch))
	var last scraper.ScrapeUpdate
	for update := range ch {
		last = update
	}
	require.True(t, last.Done)
	require.ErrorIs(t, last.FatalErr, context.Canceled)
	assert.False(t, read(t, db, systemdefs.SystemSNES).scraped(rom("SNES", "Super Mario World (USA).sfc")))
}

func TestScrapeRefusesFillMissingWithForce(t *testing.T) {
	t.Parallel()
	s := NewPlatformScraper(nil)
	err := s.Scrape(context.Background(), nil, nil, nil, &database.Database{MediaDB: newLibrary(t)},
		scraper.ScrapeOptions{FillMissing: true, Force: true}, nil, make(chan scraper.ScrapeUpdate, 1))
	require.Error(t, err)
}
