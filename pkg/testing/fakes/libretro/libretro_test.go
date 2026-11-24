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

package libretro_test

import (
	"bytes"
	"context"
	"encoding/json"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper/libretrothumbs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/fakes/fakectl"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/fakes/internal/fakehttp"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/fakes/libretro"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const nesPlaylist = "Nintendo - Nintendo Entertainment System"

func writeTree(t *testing.T, files ...string) string {
	t.Helper()
	root := t.TempDir()
	for _, rel := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o750))
		require.NoError(t, os.WriteFile(p, []byte("rom"), 0o600))
	}
	return root
}

func get(t *testing.T, base, p string) (status int, body string) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, base+p, http.NoBody)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, string(data)
}

func TestServesListingsAndImages(t *testing.T) {
	t.Parallel()
	root := writeTree(t,
		"nes/Mega Man: Rock (USA).nes",
		"nes/.hidden.nes",
		"nes/media/Skip.nes",
		"nes/readme.txt",
		"notasystem/Other.nes",
	)
	s := libretro.New(libretro.Options{
		LibraryRoot: root,
		Thumbnails:  []libretro.Thumbnail{{Playlist: "Custom", Kind: "Named_Snaps", Name: "A&B"}},
	})
	require.NoError(t, s.Err())
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	dir := "/" + url.PathEscape(nesPlaylist)

	tests := []struct {
		name     string
		path     string
		contains string
		status   int
		png      bool
	}{
		{
			name: "listing", path: dir + "/Named_Boxarts/", status: http.StatusOK,
			contains: `href="` + url.PathEscape("Mega Man_ Rock (USA).png") + `"`,
		},
		{
			name: "image", path: dir + "/Named_Titles/" + url.PathEscape("Mega Man_ Rock (USA).png"),
			status: http.StatusOK, png: true,
		},
		{name: "extra thumbnail", path: "/Custom/Named_Snaps/A_B.png", status: http.StatusOK, png: true},
		{name: "extra kind missing", path: "/Custom/Named_Boxarts/", status: http.StatusNotFound},
		{name: "unknown playlist", path: "/Nope/Named_Boxarts/", status: http.StatusNotFound},
		{name: "unknown image", path: dir + "/Named_Boxarts/Other.png", status: http.StatusNotFound},
		{name: "hidden skipped", path: dir + "/Named_Boxarts/.hidden.png", status: http.StatusNotFound},
		{name: "media skipped", path: dir + "/Named_Boxarts/Skip.png", status: http.StatusNotFound},
		{name: "root", path: "/", status: http.StatusNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			status, body := get(t, ts.URL, tt.path)
			assert.Equal(t, tt.status, status)
			if tt.contains != "" {
				assert.Contains(t, body, tt.contains)
			}
			if tt.png {
				_, err := png.Decode(strings.NewReader(body))
				assert.NoError(t, err)
			}
		})
	}
}

func TestNames(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		systemID string
		path     string
		want     []string
	}{
		{
			name: "plain", systemID: systemdefs.SystemNES, path: "/r/nes/A: B (USA).nes",
			want: []string{"A_ B (USA)"},
		},
		{
			name: "arcade set", systemID: systemdefs.SystemArcade, path: "/r/mame/dkong.zip",
			want: []string{"dkong", "Donkey Kong (US set 1)"},
		},
		{
			name: "arcade unknown set", systemID: systemdefs.SystemArcade, path: "/r/mame/zzznotaset.zip",
			want: []string{"zzznotaset"},
		},
		{
			name: "scummvm target", systemID: systemdefs.SystemScummVM, path: "/r/scummvm/sky.scummvm",
			want: []string{"sky.scummvm", "Beneath a Steel Sky"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, libretro.Names(tt.systemID, tt.path))
		})
	}
}

func TestLibraryKindsAndMissingRoot(t *testing.T) {
	t.Parallel()
	root := writeTree(t, "nes/A.nes")
	s := libretro.New(libretro.Options{LibraryRoot: root, Kinds: []string{"Named_Boxarts"}})
	assert.Equal(t, []libretro.Thumbnail{{
		Playlist: nesPlaylist, Kind: "Named_Boxarts", Name: "A",
		Path: "/" + nesPlaylist + "/Named_Boxarts/A.png",
	}}, s.Thumbnails())

	missing := libretro.New(libretro.Options{LibraryRoot: filepath.Join(root, "missing")})
	require.Error(t, missing.Err())
	assert.Empty(t, missing.Thumbnails())
}

func TestFaults(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		fault  libretro.Fault
		status int
	}{
		{
			name: "status", fault: libretro.Fault{Status: http.StatusServiceUnavailable},
			status: http.StatusServiceUnavailable,
		},
		{
			name: "other prefix", fault: libretro.Fault{Status: http.StatusBadGateway, PathPrefix: "/Other/"},
			status: http.StatusOK,
		},
		{name: "delay", fault: libretro.Fault{DelayMS: 50}, status: http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := libretro.New(libretro.Options{LibraryRoot: writeTree(t, "nes/A.nes")})
			ts := httptest.NewServer(s.Handler())
			t.Cleanup(ts.Close)
			require.NoError(t, s.SetFault(tt.fault))
			start := time.Now()
			status, _ := get(t, ts.URL, "/"+url.PathEscape(nesPlaylist)+"/Named_Boxarts/")
			assert.Equal(t, tt.status, status)
			if tt.fault.DelayMS > 0 {
				assert.GreaterOrEqual(t, time.Since(start), time.Duration(tt.fault.DelayMS)*time.Millisecond)
			}
			s.ClearFault()
			status, _ = get(t, ts.URL, "/"+url.PathEscape(nesPlaylist)+"/Named_Boxarts/")
			assert.Equal(t, http.StatusOK, status)
			require.Len(t, s.Requests(), 2)
		})
	}
}

func TestControlAndCLI(t *testing.T) {
	t.Parallel()
	root := writeTree(t, "nes/A.nes")
	s := libretro.New(libretro.Options{LibraryRoot: root, Kinds: []string{"Named_Boxarts"}})
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	call := fakehttp.HandlerCaller(s.ControlHandler())
	ctx := context.Background()
	get(t, ts.URL, "/x/y/")

	tests := []struct {
		check   func(t *testing.T, out string)
		before  func(t *testing.T)
		name    string
		wantErr string
		args    []string
	}{
		{name: "requests", args: []string{"requests"}, check: func(t *testing.T, out string) {
			var reqs []fakectl.Request
			require.NoError(t, json.Unmarshal([]byte(out), &reqs))
			require.Len(t, reqs, 1)
			assert.Equal(t, "/x/y/", reqs[0].Path)
		}},
		{name: "requests clear", args: []string{"requests", "clear"}, check: func(t *testing.T, out string) {
			assert.JSONEq(t, `{"ok":true}`, out)
			assert.Empty(t, s.Requests())
		}},
		{name: "fault set", args: []string{"fault", "set", "503"}, check: func(t *testing.T, out string) {
			assert.JSONEq(t, `{"fault":{"status":503}}`, out)
		}},
		{name: "fault clear", args: []string{"fault", "clear"}, check: func(t *testing.T, out string) {
			assert.JSONEq(t, `{"ok":true}`, out)
		}},
		{name: "thumbnails", args: []string{"thumbnails"}, check: func(t *testing.T, out string) {
			var thumbs []libretro.Thumbnail
			require.NoError(t, json.Unmarshal([]byte(out), &thumbs))
			require.Len(t, thumbs, 1)
			assert.Equal(t, "A", thumbs[0].Name)
		}},
		{
			name: "reload", args: []string{"reload"},
			before: func(t *testing.T) {
				require.NoError(t, os.WriteFile(filepath.Join(root, "nes", "B.nes"), []byte("b"), 0o600))
			},
			check: func(t *testing.T, out string) { assert.JSONEq(t, `{"thumbnails":2}`, out) },
		},
		{name: "thumbnails args", args: []string{"thumbnails", "x"}, wantErr: "usage: thumbnails"},
		{name: "unknown", args: []string{"nope"}, wantErr: "unknown command"},
		{name: "none", args: nil, wantErr: "no command given"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.before != nil {
				tt.before(t)
			}
			var out bytes.Buffer
			err := libretro.CLI(ctx, tt.args, call, &out)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			tt.check(t, strings.TrimSpace(out.String()))
		})
	}
}

// mediaDB is the slice of the media database the libretro scraper reads.
type mediaDB struct {
	database.MediaDBI
	media  []database.MediaWithFullPath
	writes []database.ScrapeWriteTarget
	mu     syncutil.Mutex
}

func (*mediaDB) IndexedSystems() ([]string, error) { return []string{systemdefs.SystemNES}, nil }

func (*mediaDB) FindSystemBySystemID(string) (database.System, error) {
	return database.System{DBID: 1, SystemID: systemdefs.SystemNES}, nil
}

func (db *mediaDB) GetMediaBySystemID(string) ([]database.MediaWithFullPath, error) {
	return append([]database.MediaWithFullPath(nil), db.media...), nil
}

func (*mediaDB) GetScrapedMediaIDs(context.Context, string, int64) (map[int64]struct{}, error) {
	return map[int64]struct{}{}, nil
}

func (*mediaDB) GetScrapeRunMediaIDs(context.Context, string, string, int64) (map[int64]struct{}, error) {
	return map[int64]struct{}{}, nil
}

func (db *mediaDB) ApplyScrapeResults(_ context.Context, targets []database.ScrapeWriteTarget) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	db.writes = append(db.writes, targets...)
	return nil
}

func (db *mediaDB) ApplyScrapeResult(_ context.Context, mediaID, titleID int64, w *database.ScrapeWrite) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	db.writes = append(db.writes, database.ScrapeWriteTarget{MediaDBID: mediaID, MediaTitleDBID: titleID, Write: w})
	return nil
}

// TestCoreScraperAgainstFake runs Core's libretro scraper, pointed at the fake
// through the base_url config key, over a library the fake serves.
func TestCoreScraperAgainstFake(t *testing.T) {
	t.Parallel()
	root := writeTree(t, "nes/Mega Man (USA).nes", "nes/Zelda (USA).nes")
	s := libretro.New(libretro.Options{LibraryRoot: root, Kinds: []string{"Named_Boxarts", "Named_Snaps"}})
	require.NoError(t, s.Err())
	base, err := s.Start("127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	cfg, err := config.NewConfig(t.TempDir(), config.BaseDefaults)
	require.NoError(t, err)
	cfg.SetScraperLibretroThumbnailsBaseURL(base)
	dataDir := t.TempDir()
	pl := mocks.NewMockPlatform()
	pl.On("Settings").Return(platforms.Settings{DataDir: dataDir, HostManagedPaths: true})

	db := &mediaDB{media: []database.MediaWithFullPath{
		{DBID: 1, MediaTitleDBID: 11, Path: filepath.Join(root, "nes", "Mega Man (USA).nes")},
		{DBID: 2, MediaTitleDBID: 12, Path: filepath.Join(root, "nes", "Zelda (USA).nes")},
		{DBID: 3, MediaTitleDBID: 13, Path: filepath.Join(root, "nes", "Not Served (USA).nes")},
	}}
	sc := libretrothumbs.NewPlatformScraper()
	updates := make(chan scraper.ScrapeUpdate, 64)
	require.NoError(t, sc.Scrape(t.Context(), cfg, pl, afero.NewOsFs(), &database.Database{MediaDB: db},
		scraper.ScrapeOptions{}, platforms.ScraperCustomOptions{}, updates))
	var last scraper.ScrapeUpdate
	for update := range updates {
		last = update
	}
	require.True(t, last.Done)
	require.NoError(t, last.FatalErr)
	assert.Equal(t, 2, last.Matched)

	db.mu.Lock()
	defer db.mu.Unlock()
	props := map[int64]int{}
	for _, w := range db.writes {
		props[w.MediaDBID] = len(w.Write.MediaProps)
	}
	assert.Equal(t, 2, props[1])
	assert.Equal(t, 2, props[2])
	assert.Zero(t, props[3])

	box := filepath.Join(libretrothumbs.Dir(cfg, pl), nesPlaylist, "Named_Boxarts", "Mega Man (USA).png")
	data, err := os.ReadFile(box) //nolint:gosec // Test path under a temp dir.
	require.NoError(t, err)
	assert.True(t, bytes.HasPrefix(data, []byte("\x89PNG")))

	var hits int
	for _, r := range s.Requests() {
		if strings.HasPrefix(r.Path, "/"+nesPlaylist+"/") {
			hits++
		}
	}
	assert.Positive(t, hits, "the scraper fetched from the fake, not the real server")
}
