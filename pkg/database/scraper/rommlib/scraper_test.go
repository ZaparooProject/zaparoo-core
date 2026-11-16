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

package rommlib

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper/scrapertest"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/romm"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

var (
	pngBytes  = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")
	jpegBytes = []byte("\xff\xd8\xff\xe0\x00\x10JFIF\x00")
)

// server is a RomM server with one SNES platform.
type server struct {
	*httptest.Server
	assets      map[string][]byte
	roms        []romm.Rom
	assetCalls  atomic.Int32
	unavailable atomic.Bool
}

func newServer(t *testing.T, roms ...romm.Rom) *server {
	t.Helper()
	s := &server{roms: roms, assets: map[string][]byte{}}
	s.Server = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.Close)
	return s
}

func (s *server) serve(w http.ResponseWriter, r *http.Request) {
	if s.unavailable.Load() {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	switch p := r.URL.Path; {
	case p == "/api/platforms":
		writeJSON(w, []romm.Platform{{ID: 1, Slug: "snes", FSSlug: "snes", Name: "SNES"}})
	case p == "/api/roms":
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		page := map[string]any{"items": []romm.Rom{}, "total": len(s.roms), "offset": offset}
		if offset == 0 {
			page["items"] = s.roms
		}
		writeJSON(w, page)
	case strings.HasPrefix(p, "/assets/"):
		s.assetCalls.Add(1)
		body, ok := s.assets[p]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write(body)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func (s *server) config() *config.Instance {
	cfg := &config.Instance{}
	cfg.SetLauncherServerURL(romm.LauncherID, s.URL)
	return cfg
}

func rom(id int, file string) romm.Rom {
	ext := filepath.Ext(file)
	return romm.Rom{
		ID: id, PlatformID: 1, PlatformSlug: "snes", Name: strings.TrimSuffix(file, ext),
		FSName: file, FSNameNoExt: strings.TrimSuffix(file, ext), FSExtension: strings.TrimPrefix(ext, "."),
		Files:     []romm.RomFile{{ID: id * 10, FileName: file, FileSizeBytes: 3}},
		UpdatedAt: "2026-01-01T00:00:00",
	}
}

func richRom() romm.Rom {
	r := rom(1, "Super Game (USA).sfc")
	rating := 87.6
	released := int64(764553600000) // 1994-03-25 in milliseconds
	r.Metadatum = romm.Metadatum{
		AverageRating: &rating, FirstReleaseDate: &released, PlayerCount: "1-2",
		Genres:     []string{"Platform", "Zaparoo Nonsense Genre"},
		Developers: []string{"  Test   Developer ", "Test Developer", ""},
		Publishers: []string{"Test Publisher"},
	}
	r.Summary = "A test\n\tdescription.\x00"
	r.Regions = []string{"USA", "Atlantis"}
	r.Languages = []string{"English"}
	r.PathCoverLarge = "roms/1/cover/big.png?ts=1"
	r.PathCoverSmall = "roms/1/cover/small.png"
	r.MergedScreenshots = []string{"/assets/romm/resources/roms/1/screenshots/0.jpg"}
	return r
}

type harness struct {
	db     *testhelpers.MockMediaDBI
	fs     afero.Fs
	srv    *server
	writes *[]database.ScrapeWriteTarget
	free   uint64
}

const imageRoot = "/data/images/romm"

func newHarness(t *testing.T, roms ...romm.Rom) *harness {
	t.Helper()
	h := &harness{
		db: testhelpers.NewMockMediaDBI(), fs: afero.NewMemMapFs(), srv: newServer(t, roms...),
		free: 1 << 40,
	}
	h.srv.assets["/assets/romm/resources/roms/1/cover/big.png"] = pngBytes
	h.srv.assets["/assets/romm/resources/roms/1/screenshots/0.jpg"] = jpegBytes
	h.writes = &[]database.ScrapeWriteTarget{}
	h.db.On("ApplyScrapeResult", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			write, ok := args.Get(3).(*database.ScrapeWrite)
			require.True(t, ok)
			scrapertest.RequireValidWrite(t, write)
			mediaID, ok := args.Get(1).(int64)
			require.True(t, ok)
			titleID, ok := args.Get(2).(int64)
			require.True(t, ok)
			*h.writes = append(*h.writes, database.ScrapeWriteTarget{
				MediaDBID: mediaID, MediaTitleDBID: titleID, Write: write,
			})
		}).Return(nil)
	h.db.On("FindSystemBySystemID", systemdefs.SystemSNES).
		Return(database.System{DBID: 7, SystemID: systemdefs.SystemSNES}, nil).Maybe()
	h.db.On("LibraryRevision", mock.Anything, mock.Anything).Return(int64(3), nil).Maybe()
	return h
}

func (h *harness) scrape(t *testing.T, opts scraper.ScrapeOptions) []scraper.ScrapeUpdate {
	t.Helper()
	s := newScraper(h.srv.config(), func(*config.Instance, platforms.Platform) string {
		return filepath.FromSlash(imageRoot)
	}, func(string) (uint64, error) { return h.free, nil })
	ch := make(chan scraper.ScrapeUpdate, 64)
	require.NoError(t, s.Scrape(
		t.Context(), h.srv.config(), nil, h.fs, &database.Database{MediaDB: h.db}, opts, nil, ch,
	))
	var updates []scraper.ScrapeUpdate
	for update := range ch {
		updates = append(updates, update)
	}
	require.NotEmpty(t, updates)
	require.True(t, updates[len(updates)-1].Done)
	return updates
}

func (h *harness) byMedia() map[int64]*database.ScrapeWrite {
	out := make(map[int64]*database.ScrapeWrite)
	for _, w := range *h.writes {
		out[w.MediaDBID] = w.Write
	}
	return out
}

func row(dbID, titleID int64, p string) database.MediaWithFullPath {
	return database.MediaWithFullPath{DBID: dbID, MediaTitleDBID: titleID, Path: p, SystemID: systemdefs.SystemSNES}
}

func titleTags(write *database.ScrapeWrite, tagType tags.TagType) []string {
	var values []string
	for _, tag := range write.TitleTags {
		if tag.Type == string(tagType) {
			values = append(values, tag.Tag)
		}
	}
	return values
}

func mediaTags(write *database.ScrapeWrite, tagType tags.TagType) []string {
	var values []string
	for _, tag := range write.MediaTags {
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

func TestScraperDefinition(t *testing.T) {
	t.Parallel()
	srv := newServer(t)
	s := NewPlatformScraper(srv.config())
	assert.Equal(t, ScraperID, s.ID)
	assert.True(t, s.SupportsFillMissing)
	assert.True(t, s.AutoScrapeAllLaunchers, "runs after indexing once a server is configured")
	assert.Equal(t, scraper.AutoOrderLibraryServer, s.AutoScrapeOrder)
	assert.Empty(t, s.SupportedSystemIDs)
	assert.False(t, NewPlatformScraper(&config.Instance{}).AutoScrapeAllLaunchers)
	pl := mocks.NewMockPlatform()
	dataDir := t.TempDir()
	pl.On("Settings").Return(platforms.Settings{HostManagedPaths: true, DataDir: dataDir})
	assert.Equal(t, filepath.Join(dataDir, "images", config.ImageRomMDir), Dir(nil, pl))
	assert.Equal(t, filepath.Join(dataDir, "images", config.ImageRomMDir), Dir(&config.Instance{}, pl))
}

func TestScrapeWritesMetadataAndArtwork(t *testing.T) {
	t.Parallel()
	dupA, dupB := rom(3, "Dup.sfc"), rom(4, "Dup.sfc")
	h := newHarness(t, richRom(), rom(2, "Other.sfc"), dupA, dupB)
	h.db.On("GetMediaBySystemID", systemdefs.SystemSNES).Return([]database.MediaWithFullPath{
		row(100, 1, romm.RomPath(1, "Super Game (USA)")),
		row(101, 2, "/roms/SNES/other.SFC"),
		row(102, 3, "/roms/SNES/Super Game (USA).smc"),
		row(103, 4, "/roms/SNES/Dup.sfc"),
		row(104, 5, "/roms/SNES/Unknown.sfc"),
		row(105, 6, romm.RomPath(99, "Gone")),
		row(106, 7, "/roms/SNES/Done.sfc"),
		{DBID: 107, MediaTitleDBID: 8, SystemID: systemdefs.SystemSNES, IsMissing: true, Path: "/roms/SNES/Other.sfc"},
	}, nil).Once()
	h.db.On("GetScrapedMediaIDs", mock.Anything, ScraperID, int64(7)).
		Return(map[int64]struct{}{106: {}}, nil).Once()
	h.db.On("SetScrapeFingerprint", mock.Anything, ScraperID, systemdefs.SystemSNES, mock.Anything).
		Return(nil).Once()

	updates := h.scrape(t, scraper.ScrapeOptions{})
	final := updates[len(updates)-1]
	for _, update := range updates {
		require.NoError(t, update.FatalErr)
		require.NoError(t, update.Err)
	}
	assert.Equal(t, 6, final.Processed)
	assert.Equal(t, 3, final.Matched)
	assert.Equal(t, 3, final.Skipped, "the shared name, the unknown file and the ROM gone from the server")

	writes := h.byMedia()
	require.Len(t, writes, 3)
	rich := writes[100]
	require.NotNil(t, rich)
	assert.Equal(t, scraper.SentinelTagInfo(ScraperID), rich.Sentinel)
	assert.Equal(t, []string{"1994"}, titleTags(rich, tags.TagTypeYear))
	assert.Equal(t, []string{"2"}, titleTags(rich, tags.TagTypePlayers))
	assert.Equal(t, []string{"88"}, titleTags(rich, tags.TagTypeRating))
	assert.Equal(t, []string{tags.NormalizeTagValue(string(tags.TagTypeDeveloper), "Test Developer")},
		titleTags(rich, tags.TagTypeDeveloper), "repeated and blank companies collapse")
	assert.Len(t, titleTags(rich, tags.TagTypePublisher), 1)
	assert.NotEmpty(t, titleTags(rich, tags.TagTypeGenre))
	assert.Equal(t, []string{"us"}, mediaTags(rich, tags.TagTypeRegion), "an unknown region is dropped")
	assert.Equal(t, []string{"en"}, mediaTags(rich, tags.TagTypeLang))
	assert.Equal(t, "A test description.", propText(rich.TitleProps, tags.TagPropertyDescription))

	key := serverKey(h.srv.URL)
	assert.Equal(t, "images://romm/"+key+"/1/cover.png", propText(rich.MediaProps, tags.TagPropertyImageBoxart))
	assert.Equal(t, "images://romm/"+key+"/1/screenshot.jpg",
		propText(rich.MediaProps, tags.TagPropertyImageScreenshot))
	stored, err := afero.ReadFile(h.fs, filepath.Join(filepath.FromSlash(imageRoot), key, "1", "cover.png"))
	require.NoError(t, err)
	assert.Equal(t, pngBytes, stored)

	require.NotNil(t, writes[102], "a local copy matches by the name without its extension")
	assert.Equal(t, []string{"1994"}, titleTags(writes[102], tags.TagTypeYear))
	other := writes[101]
	require.NotNil(t, other, "a local file matches by name, ignoring case")
	assert.Empty(t, other.TitleTags)
	assert.Empty(t, other.MediaProps)
	h.db.AssertExpectations(t)
}

func TestScrapeReusesStoredArtwork(t *testing.T) {
	t.Parallel()
	h := newHarness(t, richRom())
	h.db.On("GetMediaBySystemID", systemdefs.SystemSNES).
		Return([]database.MediaWithFullPath{row(100, 1, romm.RomPath(1, "Super Game"))}, nil)
	h.db.On("GetScrapedMediaIDs", mock.Anything, ScraperID, int64(7)).Return(map[int64]struct{}{}, nil)
	h.db.On("SetScrapeFingerprint", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)

	h.scrape(t, scraper.ScrapeOptions{})
	require.Equal(t, int32(2), h.srv.assetCalls.Load())
	h.scrape(t, scraper.ScrapeOptions{})
	assert.Equal(t, int32(2), h.srv.assetCalls.Load(), "stored artwork is reused")

	// A forced run downloads again, and a changed format replaces the old file.
	h.srv.assets["/assets/romm/resources/roms/1/cover/big.png"] = jpegBytes
	h.db.On("GetScrapeRunMediaIDs", mock.Anything, ScraperID, "run-1", int64(7)).
		Return(map[int64]struct{}{}, nil).Once()
	h.scrape(t, scraper.ScrapeOptions{Force: true, RunID: "run-1"})
	assert.Equal(t, int32(4), h.srv.assetCalls.Load())
	last := (*h.writes)[len(*h.writes)-1].Write
	assert.Contains(t, last.MediaTags, scraper.RunTagInfo(ScraperID, "run-1"))
	assert.True(t, strings.HasSuffix(propText(last.MediaProps, tags.TagPropertyImageBoxart), "/cover.jpg"))
	dir := filepath.Join(filepath.FromSlash(imageRoot), serverKey(h.srv.URL), "1")
	_, err := h.fs.Stat(filepath.Join(dir, "cover.png"))
	assert.Error(t, err, "the old format is removed")
}

func TestScrapeKeepsTextWhenLowOnSpace(t *testing.T) {
	t.Parallel()
	h := newHarness(t, richRom())
	h.free = 0
	h.db.On("GetMediaBySystemID", systemdefs.SystemSNES).
		Return([]database.MediaWithFullPath{row(100, 1, romm.RomPath(1, "Super Game"))}, nil)
	h.db.On("GetScrapedMediaIDs", mock.Anything, ScraperID, int64(7)).Return(map[int64]struct{}{}, nil)
	h.db.On("SetScrapeFingerprint", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)

	h.scrape(t, scraper.ScrapeOptions{})
	require.Len(t, *h.writes, 1)
	write := (*h.writes)[0].Write
	assert.Empty(t, write.MediaProps)
	assert.NotEmpty(t, write.TitleProps)
	assert.Zero(t, h.srv.assetCalls.Load())
}

func TestScrapeRejectsUnsupportedImages(t *testing.T) {
	t.Parallel()
	h := newHarness(t, richRom())
	h.srv.assets["/assets/romm/resources/roms/1/cover/big.png"] = []byte("<html>not an image</html>")
	h.db.On("GetMediaBySystemID", systemdefs.SystemSNES).
		Return([]database.MediaWithFullPath{row(100, 1, romm.RomPath(1, "Super Game"))}, nil)
	h.db.On("GetScrapedMediaIDs", mock.Anything, ScraperID, int64(7)).Return(map[int64]struct{}{}, nil)
	h.db.On("SetScrapeFingerprint", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)

	h.scrape(t, scraper.ScrapeOptions{})
	require.Len(t, *h.writes, 1)
	write := (*h.writes)[0].Write
	assert.Empty(t, propText(write.MediaProps, tags.TagPropertyImageBoxart))
	assert.NotEmpty(t, propText(write.MediaProps, tags.TagPropertyImageScreenshot))
}

func TestScrapeFillMissingSkipsUnchangedSystem(t *testing.T) {
	t.Parallel()
	h := newHarness(t, richRom())
	h.db.On("GetMediaBySystemID", systemdefs.SystemSNES).
		Return([]database.MediaWithFullPath{row(100, 1, romm.RomPath(1, "Super Game"))}, nil).Once()
	h.db.On("GetScrapedMediaIDs", mock.Anything, ScraperID, int64(7)).Return(map[int64]struct{}{}, nil)
	var stored string
	h.db.On("SetScrapeFingerprint", mock.Anything, ScraperID, systemdefs.SystemSNES, mock.Anything).
		Run(func(args mock.Arguments) {
			var ok bool
			stored, ok = args.Get(3).(string)
			assert.True(t, ok)
		}).Return(nil).Once()
	h.scrape(t, scraper.ScrapeOptions{})
	require.NotEmpty(t, stored)

	h.db.On("GetScrapeFingerprint", mock.Anything, ScraperID, systemdefs.SystemSNES).
		Return(stored, nil).Once()
	updates := h.scrape(t, scraper.ScrapeOptions{FillMissing: true, Systems: []string{"snes"}})
	assert.Zero(t, updates[len(updates)-1].Processed)
	require.Len(t, *h.writes, 1, "the unchanged system is not loaded again")
	h.db.AssertExpectations(t)
}

func TestScrapeFillMissingResumesRun(t *testing.T) {
	t.Parallel()
	h := newHarness(t, richRom(), rom(2, "Other.sfc"))
	h.db.On("GetScrapeFingerprint", mock.Anything, ScraperID, systemdefs.SystemSNES).Return("", nil)
	h.db.On("GetMediaBySystemID", systemdefs.SystemSNES).Return([]database.MediaWithFullPath{
		row(100, 1, romm.RomPath(1, "Super Game")), row(101, 2, "/roms/SNES/Other.sfc"),
	}, nil)
	h.db.On("GetScrapeRunMediaIDs", mock.Anything, ScraperID, "resume", int64(7)).
		Return(map[int64]struct{}{100: {}}, nil).Once()
	h.db.On("SetScrapeFingerprint", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)

	h.scrape(t, scraper.ScrapeOptions{FillMissing: true, RunID: "resume"})
	require.Len(t, *h.writes, 1)
	assert.Equal(t, int64(101), (*h.writes)[0].MediaDBID)
	assert.True(t, (*h.writes)[0].Write.FillMissing)
}

func TestScrapeSkipsSystemsNotRequested(t *testing.T) {
	t.Parallel()
	h := newHarness(t, richRom())
	updates := h.scrape(t, scraper.ScrapeOptions{Systems: []string{systemdefs.SystemNES}})
	assert.Zero(t, updates[len(updates)-1].Processed)
	h.db.AssertNotCalled(t, "GetMediaBySystemID", mock.Anything)
}

func TestScrapeUnavailableServerIsNotFatal(t *testing.T) {
	t.Parallel()
	h := newHarness(t, richRom())
	h.srv.unavailable.Store(true)
	updates := h.scrape(t, scraper.ScrapeOptions{})
	final := updates[len(updates)-1]
	assert.True(t, final.Done)
	require.NoError(t, final.FatalErr)
	require.NoError(t, final.Err, "an unavailable server must not raise an inbox warning")
}

func TestScrapeDatabaseFailureIsFatal(t *testing.T) {
	t.Parallel()
	h := newHarness(t, richRom())
	h.db.On("GetMediaBySystemID", systemdefs.SystemSNES).Return(nil, errors.New("disk on fire"))
	updates := h.scrape(t, scraper.ScrapeOptions{})
	require.ErrorContains(t, updates[len(updates)-1].FatalErr, "disk on fire")
}

func TestScrapeScoped(t *testing.T) {
	t.Parallel()
	h := newHarness(t, richRom())
	scope := &database.ScrapeScope{SystemID: systemdefs.SystemSNES, Path: "/roms/SNES/Super Game (USA).sfc", MediaID: 1}
	h.db.On("GetScrapeMedia", mock.Anything, *scope).Return([]database.MediaFullRow{{
		Media:  database.Media{DBID: 1, MediaTitleDBID: 10, Path: scope.Path},
		Title:  database.MediaTitle{DBID: 10, Name: "Super Game", Slug: "supergame"},
		System: database.System{DBID: 7, SystemID: systemdefs.SystemSNES},
	}}, nil).Once()
	h.db.On("GetScopedScrapeMediaIDs", mock.Anything, *scope, ScraperID, "").
		Return(map[int64]struct{}{}, nil).Once()

	updates := h.scrape(t, scraper.ScrapeOptions{Scope: scope})
	final := updates[len(updates)-1]
	require.NoError(t, final.FatalErr)
	assert.Equal(t, 1, final.Matched)
	require.Len(t, *h.writes, 1)
	assert.Equal(t, int64(10), (*h.writes)[0].MediaTitleDBID)
}

func TestScrapeErrors(t *testing.T) {
	t.Parallel()
	srv := newServer(t)
	s := NewPlatformScraper(srv.config())
	db := &database.Database{MediaDB: testhelpers.NewMockMediaDBI()}
	ch := make(chan scraper.ScrapeUpdate, 1)
	require.Error(t, s.Scrape(t.Context(), srv.config(), nil, nil, db,
		scraper.ScrapeOptions{Force: true, FillMissing: true}, nil, ch))
	require.Error(t, s.Scrape(t.Context(), srv.config(), nil, nil, &database.Database{},
		scraper.ScrapeOptions{}, nil, ch))
	err := s.Scrape(t.Context(), &config.Instance{}, nil, nil, db, scraper.ScrapeOptions{}, nil, ch)
	require.ErrorIs(t, err, romm.ErrNotConfigured)
}

func TestIndexMatch(t *testing.T) {
	t.Parallel()
	idx := newIndex([]romm.Rom{rom(1, "Game.sfc"), rom(2, "Twin.sfc"), rom(3, "Twin.sfc")})
	cases := map[string]int{
		romm.RomPath(1, "Game"):    1,
		"romm://bad/Game":          0,
		`C:\roms\SNES\GAME.SFC`:    1,
		"/roms/SNES/Game.smc":      1,
		"/roms/SNES/Twin.sfc":      0,
		"/roms/SNES/":              0,
		"/roms/SNES/Unrelated.sfc": 0,
	}
	for p, want := range cases {
		got := idx.match(p)
		if want == 0 {
			assert.Nil(t, got, p)
			continue
		}
		require.NotNil(t, got, p)
		assert.Equal(t, want, got.ID, p)
	}
}

func TestFieldParsing(t *testing.T) {
	t.Parallel()
	ms, secs, zero := int64(764553600000), int64(764553600), int64(0)
	assert.Equal(t, "1994", releaseYear(&ms))
	assert.Equal(t, "1994", releaseYear(&secs))
	assert.Empty(t, releaseYear(&zero))
	assert.Empty(t, releaseYear(nil))

	assert.Equal(t, "4", maxPlayers("1-4"))
	assert.Equal(t, "1", maxPlayers("1"))
	assert.Empty(t, maxPlayers("single"))
	assert.Empty(t, maxPlayers(""))

	for in, want := range map[float64]string{87.6: "88", 100: "100", 0: "", -3: "", 101: ""} {
		assert.Equal(t, want, ratingValue(&in), in)
	}
	assert.Empty(t, ratingValue(nil))

	long := strings.Repeat("word ", 1000)
	cut := truncateText(long, maxDescriptionLength)
	assert.LessOrEqual(t, len([]rune(cut)), maxDescriptionLength)
	assert.False(t, strings.HasSuffix(cut, " "))
}
