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

package libretrothumbs

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

const nesPlaylist = "Nintendo - Nintendo Entertainment System"

var thumbDir = filepath.Join("data", "thumbs")

// fakeServer serves a directory listing and PNGs for the names it holds.
type fakeServer struct {
	images   map[string]bool // "<kind>/<name>"
	requests []string
	mu       syncutil.Mutex
}

func (f *fakeServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path, _ := url.PathUnescape(r.URL.EscapedPath())
	f.mu.Lock()
	f.requests = append(f.requests, path)
	f.mu.Unlock()
	prefix := "/" + nesPlaylist + "/"
	if !strings.HasPrefix(path, prefix) {
		http.NotFound(w, r)
		return
	}
	rest := strings.TrimPrefix(path, prefix)
	if rest == "Named_Boxarts/" {
		var b strings.Builder
		_, _ = b.WriteString(`<a href="?C=N;O=D">Name</a><a href="/">Parent</a>`)
		for key := range f.images {
			if name, ok := strings.CutPrefix(key, "Named_Boxarts/"); ok {
				_, _ = b.WriteString(`<a href="` + url.PathEscape(name+".png") + `">x</a>`)
			}
		}
		_, _ = w.Write([]byte(b.String())) //nolint:gosec // fixture listing of fixture names
		return
	}
	if f.images[strings.TrimSuffix(rest, ".png")] {
		_, _ = w.Write(append([]byte("\x89PNG\r\n\x1a\n"), rest...)) //nolint:gosec // fixture PNG body
		return
	}
	http.NotFound(w, r)
}

func (f *fakeServer) count(prefix string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, r := range f.requests {
		if strings.HasPrefix(r, prefix) {
			n++
		}
	}
	return n
}

type fakeDB struct {
	database.MediaDBI
	completed map[int64]struct{}
	media     []database.MediaWithFullPath
	writes    []database.ScrapeWriteTarget
	batches   []int
	mu        syncutil.Mutex
}

func (db *fakeDB) ApplyScrapeResults(_ context.Context, targets []database.ScrapeWriteTarget) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	db.batches = append(db.batches, len(targets))
	db.writes = append(db.writes, targets...)
	return nil
}

func (*fakeDB) IndexedSystems() ([]string, error) { return []string{"NES", "Android"}, nil }
func (*fakeDB) FindSystemBySystemID(string) (database.System, error) {
	return database.System{DBID: 7}, nil
}

func (db *fakeDB) GetMediaBySystemID(string) ([]database.MediaWithFullPath, error) {
	return slicesClone(db.media), nil
}

func slicesClone(in []database.MediaWithFullPath) []database.MediaWithFullPath {
	return append([]database.MediaWithFullPath(nil), in...)
}

func (db *fakeDB) GetScrapedMediaIDs(context.Context, string, int64) (map[int64]struct{}, error) {
	return db.completed, nil
}

func (db *fakeDB) GetScrapeRunMediaIDs(context.Context, string, string, int64) (map[int64]struct{}, error) {
	return db.completed, nil
}

func (db *fakeDB) ApplyScrapeResult(_ context.Context, mediaID, titleID int64, write *database.ScrapeWrite) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	db.writes = append(db.writes, database.ScrapeWriteTarget{MediaDBID: mediaID, MediaTitleDBID: titleID, Write: write})
	return nil
}

func runScrape(
	t *testing.T, server *fakeServer, fs afero.Fs, db *fakeDB, opts scraper.ScrapeOptions,
) scraper.ScrapeUpdate {
	t.Helper()
	ts := httptest.NewServer(server)
	t.Cleanup(ts.Close)
	s := newScraper(ts.URL, func(platforms.Platform) string { return thumbDir })
	updates := make(chan scraper.ScrapeUpdate, 64)
	require.NoError(t, s.Scrape(t.Context(), nil, nil, fs, &database.Database{MediaDB: db}, opts,
		platforms.ScraperCustomOptions{}, updates))
	var last scraper.ScrapeUpdate
	for update := range updates {
		last = update
	}
	return last
}

func TestScrapeDownloadsMatchedThumbnailsOnce(t *testing.T) {
	t.Parallel()
	server := &fakeServer{images: map[string]bool{
		"Named_Boxarts/3-D WorldRunner (USA)": true,
		"Named_Snaps/3-D WorldRunner (USA)":   true,
		"Named_Boxarts/8 Eyes (USA)":          true,
	}}
	db := &fakeDB{media: []database.MediaWithFullPath{
		{DBID: 1, MediaTitleDBID: 11, Path: "source://s/NES/3-D%20WorldRunner%20%28USA%29.nes"},
		{DBID: 2, MediaTitleDBID: 12, Path: "/roms/NES/8 Eyes (U).nes", TitleSlug: "8eyes"},
		{DBID: 3, MediaTitleDBID: 13, Path: "/roms/NES/Homebrew.nes", TitleSlug: "homebrew"},
		{DBID: 4, MediaTitleDBID: 14, Path: "/roms/NES/Gone.nes", IsMissing: true},
	}}
	fs := afero.NewMemMapFs()
	last := runScrape(t, server, fs, db, scraper.ScrapeOptions{RunID: "r1", FillMissing: true})
	require.True(t, last.Done)
	require.NoError(t, last.FatalErr)
	require.Equal(t, 3, last.Processed)
	require.Equal(t, 2, last.Matched)
	require.Equal(t, 1, last.Skipped)

	props := map[int64][]database.MediaProperty{}
	for _, w := range db.writes {
		require.Equal(t, scraper.SentinelTagInfo(scraperID), w.Write.Sentinel)
		require.Equal(t, []database.TagInfo{scraper.RunTagInfo(scraperID, "r1")}, w.Write.MediaTags)
		require.True(t, w.Write.FillMissing)
		props[w.MediaDBID] = w.Write.MediaProps
	}
	box := filepath.Join(thumbDir, nesPlaylist, "Named_Boxarts", "3-D WorldRunner (USA).png")
	require.Equal(t, []database.MediaProperty{
		{TypeTag: tags.PropertyTypeTag(tags.TagPropertyImageBoxart), Text: box},
		{
			TypeTag: tags.PropertyTypeTag(tags.TagPropertyImageScreenshot),
			Text:    filepath.Join(thumbDir, nesPlaylist, "Named_Snaps", "3-D WorldRunner (USA).png"),
		},
	}, props[1])
	require.Len(t, props[2], 1, "a No-Intro name found by title")
	data, err := afero.ReadFile(fs, box)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(string(data), "\x89PNG"))

	// A second run reuses the cached index and images.
	db.writes = nil
	indexRequests := server.count("/" + nesPlaylist + "/Named_Boxarts/")
	imageRequests := len(server.requests)
	runScrape(t, server, fs, db, scraper.ScrapeOptions{FillMissing: true, RunID: "r2"})
	require.Len(t, db.writes, 2)
	require.Equal(t, indexRequests, server.count("/"+nesPlaylist+"/Named_Boxarts/"),
		"the index and box art come from the cache")
	// Only images the server lacked are asked for again: 8 Eyes' snap and
	// title, and 3-D WorldRunner's title.
	require.Len(t, server.requests, imageRequests+3)
}

func TestScrapeSkipsCompletedAndUnsupported(t *testing.T) {
	t.Parallel()
	server := &fakeServer{images: map[string]bool{"Named_Boxarts/A (USA)": true}}
	db := &fakeDB{
		media:     []database.MediaWithFullPath{{DBID: 1, Path: "/roms/A (USA).nes"}},
		completed: map[int64]struct{}{1: {}},
	}
	last := runScrape(t, server, afero.NewMemMapFs(), db, scraper.ScrapeOptions{})
	require.True(t, last.Done)
	require.Zero(t, last.Processed)
	require.Empty(t, db.writes)
	require.Equal(t, 1, last.TotalSteps, "Android has no playlist and is not a step")
}

func TestScrapeReportsUnreachableServerWithoutFailingTheRun(t *testing.T) {
	t.Parallel()
	s := newScraper("http://127.0.0.1:1", func(platforms.Platform) string { return thumbDir })
	db := &fakeDB{media: []database.MediaWithFullPath{{DBID: 1, Path: "/roms/A (USA).nes"}}}
	updates := make(chan scraper.ScrapeUpdate, 16)
	require.NoError(t, s.Scrape(t.Context(), nil, nil, afero.NewMemMapFs(), &database.Database{MediaDB: db},
		scraper.ScrapeOptions{}, platforms.ScraperCustomOptions{}, updates))
	var sawErr bool
	var last scraper.ScrapeUpdate
	for update := range updates {
		sawErr = sawErr || update.Err != nil
		last = update
	}
	require.True(t, sawErr)
	require.NoError(t, last.FatalErr)
	require.Equal(t, 1, last.Skipped)
	require.Empty(t, db.writes)
}

func TestIndexCacheExpires(t *testing.T) {
	t.Parallel()
	server := &fakeServer{images: map[string]bool{"Named_Boxarts/A (USA)": true}}
	ts := httptest.NewServer(server)
	t.Cleanup(ts.Close)
	fs := afero.NewMemMapFs()
	c := newClient(fs, "/d", ts.URL)
	now := time.Now()
	c.now = func() time.Time { return now }
	_, err := c.index(t.Context(), nesPlaylist)
	require.NoError(t, err)
	_, err = c.index(t.Context(), nesPlaylist)
	require.NoError(t, err)
	require.Equal(t, 1, server.count("/"+nesPlaylist+"/Named_Boxarts/"))
	now = now.Add(indexMaxAge + time.Hour)
	ix, err := c.index(t.Context(), nesPlaylist)
	require.NoError(t, err)
	require.Equal(t, 2, server.count("/"+nesPlaylist+"/Named_Boxarts/"))
	_, ok := ix.match("A (USA)", "")
	require.True(t, ok)
}

// Arcade set archives are matched by the game MAME names, not the set.
func TestScrapeMatchesArcadeSetsByTitle(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, _ := url.PathUnescape(r.URL.EscapedPath())
		switch p {
		case "/MAME/Named_Boxarts/":
			_, _ = w.Write([]byte(`<a href="Donkey%20Kong%20(Japan%20set%201).png">x</a>` +
				`<a href="Donkey%20Kong%20(US%20set%201).png">x</a>`))
		case "/MAME/Named_Boxarts/Donkey Kong (US set 1).png":
			_, _ = w.Write([]byte("\x89PNG\r\n\x1a\nart"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(ts.Close)
	db := &arcadeDB{fakeDB: fakeDB{media: []database.MediaWithFullPath{
		{DBID: 1, Path: "/roms/arcade/dkong.zip", TitleSlug: "donkeykong"},
	}}}
	s := newScraper(ts.URL, func(platforms.Platform) string { return thumbDir })
	updates := make(chan scraper.ScrapeUpdate, 16)
	require.NoError(t, s.Scrape(t.Context(), nil, nil, afero.NewMemMapFs(), &database.Database{MediaDB: db},
		scraper.ScrapeOptions{}, platforms.ScraperCustomOptions{}, updates))
	for range updates { //nolint:revive // drain
	}
	require.Len(t, db.writes, 1)
	require.Equal(t,
		filepath.Join(thumbDir, "MAME", "Named_Boxarts", "Donkey Kong (US set 1).png"),
		db.writes[0].Write.MediaProps[0].Text)
}

type arcadeDB struct{ fakeDB }

func (*arcadeDB) IndexedSystems() ([]string, error) { return []string{"Arcade"}, nil }

// ScummVM launch files are matched by ScummVM's title, and a game ID two
// engines share goes to the one its folder names.
func TestScrapeMatchesScummVMTargetsByTitle(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, _ := url.PathUnescape(r.URL.EscapedPath())
		switch p {
		case "/ScummVM/Named_Boxarts/":
			_, _ = w.Write([]byte(`<a href="Atlantis_%20The%20Lost%20Tales.png">x</a>` +
				`<a href="Indiana%20Jones%20and%20the%20Fate%20of%20Atlantis.png">x</a>`))
		case "/ScummVM/Named_Boxarts/Indiana Jones and the Fate of Atlantis.png":
			_, _ = w.Write([]byte("\x89PNG\r\n\x1a\nart"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(ts.Close)
	db := &scummVMDB{fakeDB: fakeDB{media: []database.MediaWithFullPath{{
		DBID: 1,
		Path: "source://" + strings.Repeat("a", 64) +
			"/ScummVM/Indiana%20Jones%20and%20the%20Fate%20of%20Atlantis%20%28CD%20DOS%29/atlantis.scummvm",
	}}}}
	s := newScraper(ts.URL, func(platforms.Platform) string { return thumbDir })
	updates := make(chan scraper.ScrapeUpdate, 16)
	require.NoError(t, s.Scrape(t.Context(), nil, nil, afero.NewMemMapFs(), &database.Database{MediaDB: db},
		scraper.ScrapeOptions{}, platforms.ScraperCustomOptions{}, updates))
	for range updates { //nolint:revive // drain
	}
	require.Len(t, db.writes, 1)
	require.Equal(t,
		filepath.Join(thumbDir, "ScummVM", "Named_Boxarts", "Indiana Jones and the Fate of Atlantis.png"),
		db.writes[0].Write.MediaProps[0].Text)
}

type scummVMDB struct{ fakeDB }

func (*scummVMDB) IndexedSystems() ([]string, error) { return []string{"ScummVM"}, nil }

func batchTarget(id int64) database.ScrapeWriteTarget {
	return database.ScrapeWriteTarget{MediaDBID: id, MediaTitleDBID: id, Write: &database.ScrapeWrite{}}
}

// Matched rows are written batchSize at a time, in one transaction each.
func TestWriteBatchFlushesAtBatchSize(t *testing.T) {
	t.Parallel()
	db := &fakeDB{}
	current := time.Unix(1000, 0)
	batch := &writeBatch{db: db, now: func() time.Time { return current }}
	for i := range batchSize + 5 {
		require.NoError(t, batch.add(t.Context(), batchTarget(int64(i+1))))
	}
	require.Equal(t, []int{batchSize}, db.batches)
	require.NoError(t, batch.flush(t.Context()))
	require.Equal(t, []int{batchSize, 5}, db.batches, "the remainder is written at the end")
	require.Len(t, db.writes, batchSize+5)
	require.NoError(t, batch.flush(t.Context()))
	require.Equal(t, []int{batchSize, 5}, db.batches, "an empty flush writes nothing")
}

// A row that has waited batchInterval is written without a full batch.
func TestWriteBatchFlushesAfterInterval(t *testing.T) {
	t.Parallel()
	db := &fakeDB{}
	current := time.Unix(1000, 0)
	batch := &writeBatch{db: db, now: func() time.Time { return current }}
	require.NoError(t, batch.add(t.Context(), batchTarget(1)))
	require.False(t, batch.due())
	current = current.Add(batchInterval)
	require.True(t, batch.due(), "the ticker flushes a batch that has waited long enough")
	require.NoError(t, batch.add(t.Context(), batchTarget(2)))
	require.Equal(t, []int{2}, db.batches)
}

// Without batch support the rows are still written, one at a time.
func TestWriteBatchFallsBackToSingleWrites(t *testing.T) {
	t.Parallel()
	db := &singleWriteDB{}
	batch := &writeBatch{db: db}
	require.NoError(t, batch.add(t.Context(), batchTarget(1)))
	require.NoError(t, batch.add(t.Context(), batchTarget(2)))
	require.NoError(t, batch.flush(t.Context()))
	require.Equal(t, []int64{1, 2}, db.ids)
}

type singleWriteDB struct {
	database.MediaDBI
	ids []int64
}

func (db *singleWriteDB) ApplyScrapeResult(_ context.Context, mediaID, _ int64, _ *database.ScrapeWrite) error {
	db.ids = append(db.ids, mediaID)
	return nil
}

// A whole run writes its matches in batches rather than row by row.
func TestScrapeWritesMatchesInBatches(t *testing.T) {
	t.Parallel()
	images := map[string]bool{}
	media := make([]database.MediaWithFullPath, 0, batchSize+3)
	for i := range batchSize + 3 {
		name := fmt.Sprintf("Game %02d (USA)", i)
		images["Named_Boxarts/"+name] = true
		media = append(media, database.MediaWithFullPath{
			DBID: int64(i + 1), MediaTitleDBID: int64(i + 1), Path: "/roms/NES/" + name + ".nes",
		})
	}
	db := &fakeDB{media: media}
	last := runScrape(t, &fakeServer{images: images}, afero.NewMemMapFs(), db, scraper.ScrapeOptions{})
	require.True(t, last.Done)
	require.Equal(t, batchSize+3, last.Matched)
	require.Len(t, db.writes, batchSize+3)
	total := 0
	for _, n := range db.batches {
		require.LessOrEqual(t, n, batchSize)
		total += n
	}
	require.Equal(t, batchSize+3, total)
	require.Less(t, len(db.batches), batchSize+3, "rows are not written one transaction each")
}
