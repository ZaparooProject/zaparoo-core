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
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fingerprintDB keeps step fingerprints and a settable library revision in
// memory on top of the batch-recording mock.
type fingerprintDB struct {
	*batchMockMediaDB
	stored   map[string]string
	revision int64
}

func (f *fingerprintDB) GetScrapeFingerprint(_ context.Context, scraperID, systemID string) (string, error) {
	return f.stored[scraperID+":"+systemID], nil
}

func (f *fingerprintDB) SetScrapeFingerprint(_ context.Context, scraperID, systemID, fingerprint string) error {
	f.stored[scraperID+":"+systemID] = fingerprint
	return nil
}

func (f *fingerprintDB) LibraryRevision(context.Context, string) (int64, error) {
	return f.revision, nil
}

// fingerprintFixture is one SNES box pack with one game, and a library that
// holds that game.
type fingerprintFixture struct {
	impl       *scraperImpl
	db         *fingerprintDB
	fs         afero.Fs
	sourcePath string
}

func newFingerprintFixture(t *testing.T) fingerprintFixture {
	t.Helper()
	fs := afero.NewMemMapFs()
	docsRoot := filepath.Join("media", "fat", "docs")
	sourcePath := filepath.Join(docsRoot, "SNES", artworkDirName)
	require.NoError(t, fs.MkdirAll(sourcePath, 0o750))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(sourcePath, indexFileName),
		[]byte("#name\tkey\nGame\tGame\n"), 0o600))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(sourcePath, "Game.jpg"), []byte("image"), 0o600))

	baseDB := newMockMediaDB(t)
	baseDB.On("GetTitlesBySystemID", systemdefs.SystemSNES).Return([]database.TitleWithSystem{
		{DBID: 10, Slug: "game", Name: "Game", SystemID: systemdefs.SystemSNES},
	}, nil)
	baseDB.On("GetMediaBySystemID", systemdefs.SystemSNES).Return([]database.MediaWithFullPath{
		{DBID: 100, MediaTitleDBID: 10, Path: "/games/SNES/Game.sfc"},
	}, nil)
	db := &fingerprintDB{
		batchMockMediaDB: &batchMockMediaDB{t: t, MockMediaDBI: baseDB},
		stored:           make(map[string]string),
		revision:         4,
	}
	impl := &scraperImpl{
		fs: fs, db: db, docsRoots: []string{docsRoot},
		sources: map[string][]sourceDir{systemdefs.SystemSNES: {{
			Path: sourcePath, SystemID: systemdefs.SystemSNES, Kind: sourceArtwork,
			Image: tags.TagPropertyImageBoxart, Metadata: true,
		}}},
	}
	return fingerprintFixture{impl: impl, db: db, fs: fs, sourcePath: sourcePath}
}

func runScrape(impl *scraperImpl, opts scraper.ScrapeOptions) []scraper.ScrapeUpdate {
	ch := make(chan scraper.ScrapeUpdate, 16)
	impl.scrapeLoop(context.Background(), opts, []string{systemdefs.SystemSNES}, ch)
	var updates []scraper.ScrapeUpdate
	for update := range ch {
		updates = append(updates, update)
	}
	return updates
}

func TestScrapeLoop_FillMissingSkipsASystemThatHasNotChanged(t *testing.T) {
	t.Parallel()
	f := newFingerprintFixture(t)
	impl, db := f.impl, f.db
	fill := scraper.ScrapeOptions{FillMissing: true, RunID: "run"}

	first := runScrape(impl, fill)
	require.Len(t, db.batches, 1, "the first run writes the game")
	assert.True(t, db.batches[0][0].Write.FillMissing)
	assert.Equal(t, 1, first[len(first)-1].Matched)
	require.NotEmpty(t, db.stored[scraperID+":"+systemdefs.SystemSNES])

	second := runScrape(impl, fill)
	require.Len(t, db.batches, 1, "nothing is read or written for an unchanged system")
	require.Len(t, second, 2)
	assert.Equal(t, scraper.ScrapeUpdate{
		SystemID: systemdefs.SystemSNES, TotalSteps: 1, CurrentStep: 1,
	}, second[0])
	assert.True(t, second[1].Done)
}

func TestScrapeLoop_FillMissingRunsAgainWhenThePackOrTheLibraryChanges(t *testing.T) {
	t.Parallel()
	fill := scraper.ScrapeOptions{FillMissing: true, RunID: "run"}

	t.Run("a new image in the pack", func(t *testing.T) {
		t.Parallel()
		f := newFingerprintFixture(t)
		impl, db, fs, sourcePath := f.impl, f.db, f.fs, f.sourcePath
		runScrape(impl, fill)
		require.NoError(t, afero.WriteFile(fs, filepath.Join(sourcePath, "Other.jpg"), []byte("image"), 0o600))
		runScrape(impl, fill)
		assert.Len(t, db.batches, 2)
	})
	t.Run("a rewritten index", func(t *testing.T) {
		t.Parallel()
		f := newFingerprintFixture(t)
		impl, db, fs, sourcePath := f.impl, f.db, f.fs, f.sourcePath
		runScrape(impl, fill)
		require.NoError(t, afero.WriteFile(fs, filepath.Join(sourcePath, indexFileName),
			[]byte("#name\tkey\nGame\tGame\nGame (Europe)\tGame\n"), 0o600))
		runScrape(impl, fill)
		assert.Len(t, db.batches, 2)
	})
	t.Run("a renamed image", func(t *testing.T) {
		t.Parallel()
		f := newFingerprintFixture(t)
		impl, db, fs, sourcePath := f.impl, f.db, f.fs, f.sourcePath
		runScrape(impl, fill)
		require.NoError(t, fs.Rename(filepath.Join(sourcePath, "Game.jpg"), filepath.Join(sourcePath, "Game2.jpg")))
		runScrape(impl, fill)
		assert.Len(t, db.batches, 1, "the renamed image matches nothing, but the pack was read again")
		assert.Len(t, db.stored, 1)
		runScrape(impl, fill)
	})
	t.Run("an index that changed the system's rows", func(t *testing.T) {
		t.Parallel()
		f := newFingerprintFixture(t)
		impl, db := f.impl, f.db
		runScrape(impl, fill)
		db.revision++
		runScrape(impl, fill)
		assert.Len(t, db.batches, 2)
	})
}

func TestScrapeLoop_ManualRunIgnoresTheFingerprintAndRefreshesIt(t *testing.T) {
	t.Parallel()
	f := newFingerprintFixture(t)
	impl, db, fs, sourcePath := f.impl, f.db, f.fs, f.sourcePath

	runScrape(impl, scraper.ScrapeOptions{})
	runScrape(impl, scraper.ScrapeOptions{})
	require.Len(t, db.batches, 2, "a manual run always does the work")

	// The manual run left a fingerprint behind, so the next index-triggered
	// run has nothing to do.
	runScrape(impl, scraper.ScrapeOptions{FillMissing: true, RunID: "run"})
	require.Len(t, db.batches, 2)

	// A step that failed must not be remembered as complete.
	require.NoError(t, afero.WriteFile(fs, filepath.Join(sourcePath, indexFileName), []byte("#name\n"), 0o600))
	before := db.stored[scraperID+":"+systemdefs.SystemSNES]
	updates := runScrape(impl, scraper.ScrapeOptions{})
	require.Error(t, updates[len(updates)-2].Err)
	assert.Equal(t, before, db.stored[scraperID+":"+systemdefs.SystemSNES])
}

func TestSourceReuse_ParsesASharedPackOnceAndReleasesIt(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	gameboy := filepath.Join("docs", "GAMEBOY", artworkDirName)
	color := filepath.Join("docs", "GBC", artworkDirName)
	for _, dir := range []string{gameboy, color} {
		require.NoError(t, fs.MkdirAll(dir, 0o750))
		require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "Game.jpg"), []byte("image"), 0o600))
	}
	impl := &scraperImpl{fs: fs, sources: map[string][]sourceDir{
		systemdefs.SystemGameboy:      {{Path: gameboy, SystemID: systemdefs.SystemGameboy, Kind: sourceArtwork}},
		systemdefs.SystemGameboyColor: {{Path: color, SystemID: systemdefs.SystemGameboyColor, Kind: sourceArtwork}},
	}}
	// Each system falls back to the other's pack, so both folders are read
	// by both steps.
	impl.planSourceReuse([]string{systemdefs.SystemGameboy, systemdefs.SystemGameboyColor})

	first, ok := impl.loadStepSources(context.Background(), scraper.ScrapeOptions{}, systemdefs.SystemGameboy, 0)
	require.True(t, ok)
	require.NoError(t, first.err)
	require.Len(t, first.records, 2)
	assert.Len(t, impl.loaded, 2, "the second step reads both packs again")
	impl.releaseSources(0)
	assert.Len(t, impl.loaded, 2)

	// Remove the files: the second step can only succeed from what was kept.
	require.NoError(t, fs.RemoveAll("docs"))
	second, ok := impl.loadStepSources(context.Background(), scraper.ScrapeOptions{}, systemdefs.SystemGameboyColor, 1)
	require.True(t, ok)
	require.NoError(t, second.err)
	require.Len(t, second.records, 2)
	impl.releaseSources(1)
	assert.Empty(t, impl.loaded, "nothing later reads them")
}

func TestArcadeSetName_PrefersTheCacheAndFallsBackToTheDescriptor(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	path := filepath.Join("_Arcade", "Game.mra")
	require.NoError(t, afero.WriteFile(fs, path, []byte("<misterromdescription><setname>fromfile</setname>"), 0o600))

	cached := &scraperImpl{fs: fs, setNames: func(string) (string, bool) { return "fromcache", true }}
	name, ok := cached.arcadeSetName(path)
	assert.True(t, ok)
	assert.Equal(t, "fromcache", name)

	// A cached empty name is an answer: that descriptor has none.
	none := &scraperImpl{fs: fs, setNames: func(string) (string, bool) { return "", true }}
	_, ok = none.arcadeSetName(path)
	assert.False(t, ok)

	for _, impl := range []*scraperImpl{
		{fs: fs, setNames: func(string) (string, bool) { return "", false }},
		{fs: fs},
	} {
		name, ok = impl.arcadeSetName(path)
		assert.True(t, ok)
		assert.Equal(t, "fromfile", name)
	}
}
