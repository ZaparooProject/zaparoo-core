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
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/scantest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeSourceReader is a minimal platforms.SourceRootReader backed by a fixed
// listing tree, keyed by canonical source path.
type fakeSourceReader struct {
	tree map[string][]platforms.SourceEntry
}

func (*fakeSourceReader) SourceRoots(context.Context) ([]string, error) { return nil, nil }

func (r *fakeSourceReader) ReadSourceDir(_ context.Context, path string) ([]platforms.SourceEntry, error) {
	return r.tree[path], nil
}

func file(name string) platforms.SourceEntry { return platforms.SourceEntry{Name: name, Size: 1} }
func folder(name string) platforms.SourceEntry {
	return platforms.SourceEntry{Name: name, Size: -1, Dir: true}
}

func TestSourceRelativeSegments(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []string{"NES", "game.nes"},
		sourceRelativeSegments("source://root1/NES/game.nes", "source://root1"))
	assert.Nil(t, sourceRelativeSegments("source://root1", "source://root1"),
		"the root itself has no relative segments")
	assert.Nil(t, sourceRelativeSegments("source://other/NES/game.nes", "source://root1"),
		"a different root id never resolves")
	assert.Nil(t, sourceRelativeSegments("android://pkg/Name", "source://root1"), "a non-source path never resolves")
}

func TestSourceArtworkFallbackNames(t *testing.T) {
	t.Parallel()

	assert.Equal(t,
		[]string{"Game.png", "Game.jpg", "Game.jpeg", "Game.webp"},
		sourceArtworkFallbackNames("source://root1/Game.nes", "source://root1"),
		"a root-level file has no nested form, only flat")

	assert.Equal(t,
		[]string{
			"Sub/Other.png", "Sub/Other.jpg", "Sub/Other.jpeg", "Sub/Other.webp",
			"Other.png", "Other.jpg", "Other.jpeg", "Other.webp",
		},
		sourceArtworkFallbackNames("source://root1/Sub/Other.nes", "source://root1"),
		"a nested file tries the mirrored subfolder name before the flat one")

	assert.Nil(t, sourceArtworkFallbackNames("source://other/Game.nes", "source://root1"))
}

func TestSourceDirectoryArtworkFallbackNames(t *testing.T) {
	t.Parallel()

	assert.Equal(t,
		[]string{"Sub.png", "Sub.jpg", "Sub.jpeg", "Sub.webp"},
		sourceDirectoryArtworkFallbackNames("source://root1/Sub", "source://root1"))

	// A folder named after a disc image/playlist also answers to the
	// extensionless stem, matching the filesystem convention exactly.
	names := sourceDirectoryArtworkFallbackNames("source://root1/Game.cue", "source://root1")
	assert.Contains(t, names, "Game.cue.png")
	assert.Contains(t, names, "Game.png")
}

func TestStatSourceMediaDirs(t *testing.T) {
	t.Parallel()

	reader := &fakeSourceReader{tree: map[string][]platforms.SourceEntry{
		"source://root1/media": {folder("boxart"), file("notadir.txt"), folder("screenshot")},
	}}
	listings := newSourceDirListings(reader)
	dirs := statSourceMediaDirs(t.Context(), listings, "source://root1")
	assert.Equal(t, map[string]string{
		"boxart":     "source://root1/media/boxart",
		"screenshot": "source://root1/media/screenshot",
	}, dirs)
}

func TestFindSourceFile(t *testing.T) {
	t.Parallel()

	reader := &fakeSourceReader{tree: map[string][]platforms.SourceEntry{
		"source://root1/media/boxart":     {file("Game.png")},
		"source://root1/media/boxart/Sub": {file("Other.png")},
	}}
	listings := newSourceDirListings(reader)
	availableDirs := map[string]string{"boxart": "source://root1/media/boxart"}

	found := findSourceFile(t.Context(), listings, []string{"Game.png"}, []string{"boxart"}, availableDirs)
	require.NotNil(t, found)
	assert.Equal(t, "source://root1/media/boxart/Game.png", found.Path)
	assert.Equal(t, "image/png", found.ContentType)

	found = findSourceFile(
		t.Context(), listings, []string{"Sub/Other.png", "Other.png"}, []string{"boxart"}, availableDirs,
	)
	require.NotNil(t, found)
	assert.Equal(t, "source://root1/media/boxart/Sub/Other.png", found.Path)

	assert.Nil(t, findSourceFile(t.Context(), listings, []string{"Missing.png"}, []string{"boxart"}, availableDirs))
	assert.Nil(t, findSourceFile(t.Context(), listings, []string{"../escape.png"}, []string{"boxart"}, availableDirs),
		"a traversal segment in a candidate name is never looked up")
}

// End to end: scraping a fake SAF tree that follows the EmulationStation
// media/<subdir>/<name>.<ext> convention writes the same shape of
// MediaProperty/DirectoryProperty rows a real filesystem scrape would,
// keyed by source:// paths instead - the concrete repro of "folder covers
// don't exist for Android at all today."
func TestScrapeImportsArtworkForASourceRoot(t *testing.T) {
	t.Parallel()

	db, cleanup := testhelpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)

	const root = "source://root1"
	gamePath := root + "/Game.nes"
	otherPath := root + "/Sub/Other.nes"
	scantest.IndexScanResults(t, db, systemdefs.SystemNES, database.ScanReconcileOpts{},
		platforms.ScanResult{Path: gamePath}, platforms.ScanResult{Path: otherPath})

	reader := &fakeSourceReader{tree: map[string][]platforms.SourceEntry{
		root + "/media":            {folder("boxart")},
		root + "/media/boxart":     {file("Game.png"), file("Sub.png")},
		root + "/media/boxart/Sub": {file("Other.png")},
	}}
	system, err := db.FindSystemBySystemID(systemdefs.SystemNES)
	require.NoError(t, err)

	ch := make(chan scraper.ScrapeUpdate, 64)
	s := &scraperImpl{db: db, listings: newSourceDirListings(reader)}
	s.scrapeLoop(t.Context(), scraper.ScrapeOptions{}, []scraper.ScrapeSystem{{
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
	wantArt := map[string]string{
		gamePath:  root + "/media/boxart/Game.png",
		otherPath: root + "/media/boxart/Sub/Other.png",
	}
	for i := range rows {
		props, propErr := db.GetMediaPropertyMetadata(t.Context(), rows[i].DBID)
		require.NoError(t, propErr)
		texts := make([]string, 0, len(props))
		for _, prop := range props {
			texts = append(texts, prop.Text)
		}
		assert.Contains(t, texts, wantArt[rows[i].Path], rows[i].Path)
	}

	directoryProps, err := db.GetDirectoryProperties(t.Context(), system.DBID, root+"/Sub")
	require.NoError(t, err)
	require.NotEmpty(t, directoryProps, "the Sub folder itself gets a cover from media/boxart/Sub.png")
	assert.Equal(t, root+"/media/boxart/Sub.png", directoryProps[0].Text)
}
