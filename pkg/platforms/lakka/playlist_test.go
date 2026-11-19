//go:build linux && !android

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package lakka

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const snesPlaylistName = "Nintendo - Super Nintendo Entertainment System.lpl"

var romsDir = filepath.Join(storageDir, "roms")

func TestParsePlaylistJSON(t *testing.T) {
	t.Parallel()
	data := `{
  "version": "1.5",
  "default_core_path": "/tmp/cores/snes9x_libretro.so",
  "items": [
    {
      "path": "/storage/roms/snes/Game (USA).sfc",
      "label": "Game (USA)",
      "core_path": "/tmp/cores/snes9x2010_libretro.so",
      "core_name": "Nintendo - SNES / SFC (bsnes)",
      "crc32": "12345678|crc",
      "db_name": "Nintendo - Super Nintendo Entertainment System.lpl"
    },
    {"path": "/storage/roms/snes/Other.sfc"},
    {"label": "no path"},
    {"path": 42, "label": "bad path type"},
    "not an object",
    {"path": "/storage/roms/snes/Typed.sfc", "label": 7, "crc32": null}
  ]
}`
	pl, err := parsePlaylist([]byte(data))
	require.NoError(t, err)
	assert.Equal(t, "/tmp/cores/snes9x_libretro.so", pl.DefaultCorePath)
	require.Len(t, pl.Entries, 3)
	assert.Equal(t, playlistEntry{
		Path:     "/storage/roms/snes/Game (USA).sfc",
		Label:    "Game (USA)",
		CorePath: "/tmp/cores/snes9x2010_libretro.so",
		CoreName: "Nintendo - SNES / SFC (bsnes)",
		CRC32:    "12345678|crc",
		DBName:   snesPlaylistName,
	}, pl.Entries[0])
	assert.Equal(t, playlistEntry{Path: "/storage/roms/snes/Other.sfc"}, pl.Entries[1])
	assert.Equal(t, playlistEntry{Path: "/storage/roms/snes/Typed.sfc"}, pl.Entries[2])
}

func TestParsePlaylistLegacy(t *testing.T) {
	t.Parallel()
	data := "/storage/roms/snes/Game.sfc\r\nGame\r\n/tmp/cores/snes9x_libretro.so\r\nSnes9x\r\nDETECT\r\n" +
		snesPlaylistName + "\r\n/storage/roms/snes/Two.sfc\nTwo\nDETECT\nDETECT\nDETECT\n" + snesPlaylistName + "\n"
	pl, err := parsePlaylist([]byte(data))
	require.NoError(t, err)
	require.Len(t, pl.Entries, 2)
	assert.Equal(t, "/storage/roms/snes/Game.sfc", pl.Entries[0].Path)
	assert.Equal(t, "/tmp/cores/snes9x_libretro.so", pl.Entries[0].CorePath)
	assert.Equal(t, snesPlaylistName, pl.Entries[0].DBName)
	assert.Equal(t, "DETECT", pl.Entries[1].CorePath)
}

func TestParsePlaylistRejectsMalformed(t *testing.T) {
	t.Parallel()
	for _, data := range []string{
		`{"items": [`,
		`{"items": {"path": "/x"}}`,
		`{"items": "nope"}`,
		"just some text",
		"\x00\x01\x02",
	} {
		_, err := parsePlaylist([]byte(data))
		require.Error(t, err, "%q", data)
	}

	pl, err := parsePlaylist(nil)
	require.NoError(t, err)
	assert.Empty(t, pl.Entries)

	pl, err = parsePlaylist([]byte(`{"version": "1.5"}`))
	require.NoError(t, err)
	assert.Empty(t, pl.Entries)
}

func TestParsePlaylistBounds(t *testing.T) {
	t.Parallel()
	_, err := parsePlaylist(make([]byte, maxPlaylistSize+1))
	require.Error(t, err)

	long := "/" + strings.Repeat("a", maxPlaylistField)
	data := `{"items": [{"path": "` + long + `"}, {"path": "/a\u0000b"}, {"path": "/ok.sfc"}]}`
	pl, err := parsePlaylist([]byte(data))
	require.NoError(t, err)
	require.Len(t, pl.Entries, 1)
	assert.Equal(t, "/ok.sfc", pl.Entries[0].Path)
}

func TestPlaylistTarget(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in, path, inner string
		ok              bool
	}{
		{in: "/storage/roms/snes/Game.sfc", path: "/storage/roms/snes/Game.sfc", ok: true},
		{in: "/storage/roms/snes/Game.zip#Game.sfc", path: "/storage/roms/snes/Game.zip", inner: "Game.sfc", ok: true},
		{
			in:   "/storage/roms/psx/Game.7Z#disc/Game.cue",
			path: "/storage/roms/psx/Game.7Z", inner: "disc/Game.cue", ok: true,
		},
		{in: "/storage/roms/snes/#1 Hits.sfc", path: "/storage/roms/snes/#1 Hits.sfc", ok: true},
		{in: "/storage/roms/snes/../snes/Game.sfc", path: "/storage/roms/snes/Game.sfc", ok: true},
		// Lower-casing these names changes their byte length, so the archive
		// suffix has to be found in the path as written.
		{
			in:   "/storage/roms/snes/\u0130\u0130\u0130\u0130.zip#\u0130.sfc",
			path: "/storage/roms/snes/\u0130\u0130\u0130\u0130.zip", inner: "\u0130.sfc", ok: true,
		},
		{in: "/\u023a\u023a\u023a\u023a.zip#", path: "/\u023a\u023a\u023a\u023a.zip", ok: true},
		{in: "/\u212a\u212a\u212a\u212a.ZIP#a.sfc", path: "/\u212a\u212a\u212a\u212a.ZIP", inner: "a.sfc", ok: true},
		{in: "roms/Game.sfc"},
		{in: ""},
		{in: "/"},
	}
	for _, tt := range tests {
		path, inner, ok := playlistTarget(tt.in)
		assert.Equal(t, tt.ok, ok, tt.in)
		assert.Equal(t, tt.path, path, tt.in)
		assert.Equal(t, tt.inner, inner, tt.in)
	}
}

func TestEntryCore(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "/tmp/cores/a_libretro.so", entryCore("/tmp/cores/a_libretro.so", "/tmp/cores/b_libretro.so"))
	assert.Equal(t, "/tmp/cores/b_libretro.so", entryCore("DETECT", "/tmp/cores/b_libretro.so"))
	assert.Equal(t, "/tmp/cores/b_libretro.so", entryCore("", "/tmp/cores/b_libretro.so"))
	assert.Empty(t, entryCore("DETECT", "DETECT"))
	assert.Empty(t, entryCore("", ""))
}

func writePlaylist(t *testing.T, fs afero.Fs, name, body string) {
	t.Helper()
	require.NoError(t, afero.WriteFile(fs, filepath.Join(playlistsDir, name), []byte(body), 0o644))
}

func writeFile(t *testing.T, fs afero.Fs, path string) {
	t.Helper()
	require.NoError(t, afero.WriteFile(fs, path, []byte("x"), 0o644))
}

func TestPlaylistStoreMapsSystems(t *testing.T) {
	t.Parallel()
	p, _, _ := testPlatform(t, closedAddr(t))
	snesGame := filepath.Join(romsDir, "snes", "Game.sfc")
	psxGame := filepath.Join(romsDir, "psx", "Game.cue")
	writePlaylist(t, p.fs, snesPlaylistName, `{"items": [
		{"path": "`+snesGame+`", "label": "SNES Game", "core_path": "DETECT"},
		{"path": "`+filepath.Join(romsDir, "snes", "Pack.zip")+`#Inner.sfc"}
	]}`)
	writePlaylist(t, p.fs, "content_history.lpl", `{"items": [
		{"path": "`+snesGame+`", "label": "History name", "core_path": "/tmp/cores/snes9x2010_libretro.so",
		 "db_name": "Sega - Mega Drive - Genesis.lpl"},
		{"path": "`+psxGame+`", "label": "PSX Game", "db_name": "Sony - PlayStation.lpl"},
		{"path": "`+filepath.Join(romsDir, "Movie.mkv")+`", "db_name": ""}
	]}`)
	writePlaylist(t, p.fs, "Images.lpl", `{"items": [{"path": "`+filepath.Join(romsDir, "a.png")+`"}]}`)
	writePlaylist(t, p.fs, "broken.lpl", `{"items": [`)
	require.NoError(t, p.fs.MkdirAll(filepath.Join(playlistsDir, "dir.lpl"), 0o755))

	index := p.playlists.load()
	snes := index.bySystem[systemdefs.SystemSNES]
	require.Len(t, snes, 2)
	// The system playlist wins over the history entry for the same file.
	assert.Equal(t, playlistItem{SystemID: systemdefs.SystemSNES, Path: snesGame, Name: "SNES Game"}, snes[0])
	assert.Equal(t, playlistItem{
		SystemID: systemdefs.SystemSNES,
		Path:     filepath.Join(romsDir, "snes", "Pack.zip"),
		Inner:    "Inner.sfc",
		Name:     "Inner",
	}, snes[1])
	assert.Empty(t, index.bySystem[systemdefs.SystemGenesis])

	psx := index.bySystem[systemdefs.SystemPSX]
	require.Len(t, psx, 1)
	assert.Equal(t, psxGame, psx[0].Path)
	assert.Len(t, index.byPath, 3)

	_, ok := index.lookup(psxGame, systemdefs.SystemPSX)
	assert.True(t, ok)
	_, ok = index.lookup(psxGame, systemdefs.SystemSNES)
	assert.False(t, ok)
}

func TestPlaylistStoreRereadsChangedPlaylists(t *testing.T) {
	t.Parallel()
	p, _, _ := testPlatform(t, closedAddr(t))
	assert.Empty(t, p.playlists.load().byPath, "missing directory")

	game := filepath.Join(romsDir, "snes", "Game.sfc")
	writePlaylist(t, p.fs, snesPlaylistName, `{"items": [{"path": "`+game+`"}]}`)
	first := p.playlists.load()
	require.Len(t, first.byPath, 1)
	assert.Same(t, first, p.playlists.load(), "unchanged playlists are not rebuilt")
	assert.Same(t, first, p.playlists.cached())

	path := filepath.Join(playlistsDir, snesPlaylistName)
	body := `{"items": [{"path": "` + game + `"}, {"path": "/b.sfc"}]}`
	require.NoError(t, afero.WriteFile(p.fs, path, []byte(body), 0o644))
	require.NoError(t, p.fs.Chtimes(path, time.Now(), time.Now().Add(time.Minute)))
	assert.Len(t, p.playlists.load().byPath, 2)

	require.NoError(t, p.fs.Remove(path))
	assert.Empty(t, p.playlists.load().byPath)
}

func TestPlaylistStoreSkipsOversizedPlaylist(t *testing.T) {
	t.Parallel()
	p, _, _ := testPlatform(t, closedAddr(t))
	body := `{"items": [{"path": "/storage/roms/snes/Game.sfc"}]}` + strings.Repeat(" ", maxPlaylistSize)
	writePlaylist(t, p.fs, snesPlaylistName, body)
	assert.Empty(t, p.playlists.load().byPath)
}

func primarySNESLauncher(t *testing.T, launchers []platforms.Launcher) platforms.Launcher {
	t.Helper()
	for i := range launchers {
		if launchers[i].SystemID == systemdefs.SystemSNES && launchers[i].Scanner != nil {
			return launchers[i]
		}
	}
	require.FailNow(t, "no SNES launcher with a playlist scanner")
	return platforms.Launcher{}
}

func TestScanPlaylistsMergesWithFolderResults(t *testing.T) {
	t.Parallel()
	p, _, _ := testPlatform(t, closedAddr(t))
	cfg := testConfig(t)

	inFolder := filepath.Join(romsDir, "snes", "Folder.sfc")
	elsewhere := filepath.Join(romsDir, "My Games", "Elsewhere.sfc")
	archive := filepath.Join(romsDir, "snes", "Pack.zip")
	zipDir := filepath.Join(romsDir, "snes", "Dir.zip")
	outside := filepath.Join(storageDir, "secret", "Outside.sfc")
	for _, f := range []string{inFolder, elsewhere, archive, outside} {
		writeFile(t, p.fs, f)
	}
	writePlaylist(t, p.fs, snesPlaylistName, `{"items": [
		{"path": "`+inFolder+`", "label": "Folder label"},
		{"path": "`+elsewhere+`", "label": "Elsewhere (USA)"},
		{"path": "`+archive+`#Game.sfc"},
		{"path": "`+zipDir+`#Inside.sfc"},
		{"path": "`+outside+`"},
		{"path": "`+filepath.Join(romsDir, "snes", "Missing.sfc")+`"}
	]}`)

	launcher := primarySNESLauncher(t, p.Launchers(cfg))
	folder := []platforms.ScanResult{
		{Path: inFolder},
		{Path: filepath.Join(zipDir, "Inside.sfc")},
	}
	results, err := launcher.Scanner(context.Background(), cfg, systemdefs.SystemSNES, folder)
	require.NoError(t, err)
	assert.Equal(t, []platforms.ScanResult{
		{Path: inFolder},
		{Path: filepath.Join(zipDir, "Inside.sfc")},
		{Path: elsewhere, Name: "Elsewhere (USA)"},
		{Path: archive, Name: "Game"},
	}, results)

	results, err = launcher.Scanner(context.Background(), cfg, systemdefs.SystemPSX, folder)
	require.NoError(t, err)
	assert.Equal(t, folder, results, "other systems are untouched")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = launcher.Scanner(ctx, cfg, systemdefs.SystemSNES, nil)
	require.ErrorIs(t, err, context.Canceled)
}

func TestOnlyOneLauncherPerSystemScansPlaylists(t *testing.T) {
	t.Parallel()
	p, _, _ := testPlatform(t, closedAddr(t))
	seen := make(map[string]string)
	for _, l := range p.Launchers(testConfig(t)) {
		if l.Scanner == nil {
			continue
		}
		prev, dup := seen[l.SystemID]
		assert.False(t, dup, "%s and %s both scan playlists", prev, l.ID)
		seen[l.SystemID] = l.ID
	}
	assert.Contains(t, seen, systemdefs.SystemSNES)
}

func TestPlaylistLauncherMatchesEntriesOutsideFolders(t *testing.T) {
	t.Parallel()
	p, _, _ := testPlatform(t, closedAddr(t))
	cfg := testConfig(t)
	elsewhere := filepath.Join(romsDir, "My Games", "Elsewhere.sfc")
	writePlaylist(t, p.fs, snesPlaylistName, `{"items": [{"path": "`+elsewhere+`"}]}`)

	var playlist *platforms.Launcher
	launchers := p.Launchers(cfg)
	for i := range launchers {
		if launchers[i].ID == playlistLauncherPrefix+systemdefs.SystemSNES {
			playlist = &launchers[i]
		}
	}
	require.NotNil(t, playlist)
	assert.True(t, playlist.ScanOnly)
	assert.True(t, playlist.SkipFilesystemScan)
	assert.Empty(t, playlist.Folders)
	assert.True(t, playlist.Test(cfg, elsewhere))
	assert.False(t, playlist.Test(cfg, filepath.Join(romsDir, "My Games", "Other.sfc")))
}

func launchArgs(t *testing.T, launcher *platforms.Launcher, cfg *config.Instance, path string) []string {
	t.Helper()
	cmd, err := launcher.BuildLaunchCommand(cfg, path, nil)
	require.NoError(t, err)
	return append([]string{cmd.Executable}, cmd.Args...)
}

func TestPlaylistEntryLaunchesWithRecordedCore(t *testing.T) {
	t.Parallel()
	p, _, _ := testPlatform(t, closedAddr(t))
	cfg := testConfig(t)
	require.NoError(t, afero.WriteFile(p.fs, retroArchExecutable, nil, 0o755))
	systemCore := filepath.Join(retroArchCoresDir, snesCore(t))
	recorded := filepath.Join(retroArchCoresDir, "snes9x2010_libretro.so")
	writeFile(t, p.fs, systemCore)
	writeFile(t, p.fs, recorded)

	withCore := filepath.Join(romsDir, "snes", "Core.sfc")
	detect := filepath.Join(romsDir, "snes", "Detect.sfc")
	missing := filepath.Join(romsDir, "snes", "Missing.sfc")
	elsewhere := filepath.Join(romsDir, "snes", "Elsewhere.sfc")
	notCore := filepath.Join(romsDir, "snes", "NotCore.sfc")
	writePlaylist(t, p.fs, snesPlaylistName, `{"items": [
		{"path": "`+withCore+`", "core_path": "/tmp/cores/snes9x2010_libretro.so"},
		{"path": "`+detect+`", "core_path": "DETECT"},
		{"path": "`+missing+`", "core_path": "/tmp/cores/missing_libretro.so"},
		{"path": "`+elsewhere+`", "core_path": "/usr/lib/libretro/snes9x2010_libretro.so"},
		{"path": "`+notCore+`", "core_path": "/storage/evil.so"}
	]}`)

	launcher := primarySNESLauncher(t, p.Launchers(cfg))
	want := func(core, path string) []string {
		return []string{retroArchExecutable, "-L", core, path}
	}
	assert.Equal(t, want(recorded, withCore), launchArgs(t, &launcher, cfg, withCore))
	assert.Equal(t, want(systemCore, detect), launchArgs(t, &launcher, cfg, detect))
	assert.Equal(t, want(systemCore, missing), launchArgs(t, &launcher, cfg, missing))
	assert.Equal(t, want(recorded, elsewhere), launchArgs(t, &launcher, cfg, elsewhere),
		"a core recorded elsewhere is found by name in the cores directory")
	assert.Equal(t, want(systemCore, notCore), launchArgs(t, &launcher, cfg, notCore))
	unlisted := filepath.Join(romsDir, "snes", "Unlisted.sfc")
	assert.Equal(t, want(systemCore, unlisted), launchArgs(t, &launcher, cfg, unlisted))
}

func TestPlaylistCoreMakesLauncherAvailableWithoutSystemCore(t *testing.T) {
	t.Parallel()
	p, _, _ := testPlatform(t, closedAddr(t))
	cfg := testConfig(t)
	require.NoError(t, afero.WriteFile(p.fs, retroArchExecutable, nil, 0o755))
	withCore := filepath.Join(romsDir, "snes", "Core.sfc")
	missingCore := filepath.Join(romsDir, "snes", "Missing.sfc")
	writePlaylist(t, p.fs, snesPlaylistName, `{"items": [
		{"path": "`+missingCore+`", "core_path": "/tmp/cores/missing_libretro.so"},
		{"path": "`+filepath.Join(romsDir, "snes", "Missing2.sfc")+`", "core_path": "/tmp/cores/missing_libretro.so"},
		{"path": "`+withCore+`", "core_path": "/tmp/cores/snes9x2010_libretro.so"}
	]}`)

	launcher := primarySNESLauncher(t, p.Launchers(cfg))
	require.NotNil(t, launcher.Availability)
	require.NotNil(t, launcher.Preflight)
	require.Error(t, launcher.Availability(cfg), "neither the system core nor a recorded core is installed")
	require.Error(t, launcher.Preflight(cfg, withCore, nil))

	writeFile(t, p.fs, filepath.Join(retroArchCoresDir, "snes9x2010_libretro.so"))
	require.NoError(t, launcher.Availability(cfg))
	require.NoError(t, launcher.Preflight(cfg, withCore, nil), "the entry's recorded core can run it")
	require.Error(t, launcher.Preflight(cfg, missingCore, nil), "this entry needs the missing system core")
	require.Error(t, launcher.Preflight(cfg, filepath.Join(romsDir, "snes", "Unlisted.sfc"), nil))

	writeFile(t, p.fs, filepath.Join(retroArchCoresDir, snesCore(t)))
	require.NoError(t, launcher.Preflight(cfg, missingCore, nil))
}

func TestPlaylistCoreFallsBackWhenRetroArchMissing(t *testing.T) {
	t.Parallel()
	p, _, _ := testPlatform(t, closedAddr(t))
	game := filepath.Join(romsDir, "snes", "Core.sfc")
	writeFile(t, p.fs, filepath.Join(retroArchCoresDir, "snes9x2010_libretro.so"))
	writePlaylist(t, p.fs, snesPlaylistName,
		`{"items": [{"path": "`+game+`", "core_path": "/tmp/cores/snes9x2010_libretro.so"}]}`)
	_, ok := p.playlistCore(game, systemdefs.SystemSNES)
	assert.False(t, ok)

	require.NoError(t, afero.WriteFile(p.fs, retroArchExecutable, nil, 0o755))
	core, ok := p.playlistCore(game, systemdefs.SystemSNES)
	assert.True(t, ok)
	assert.Equal(t, "snes9x2010_libretro.so", core)
	_, ok = p.playlistCore(game, systemdefs.SystemPSX)
	assert.False(t, ok, "entry belongs to another system")
}

func TestPlaylistEntryLoadsRecordedCoreInRunningRetroArch(t *testing.T) {
	t.Parallel()
	state := &retroArchState{status: contentless}
	ra := newFakeRetroArch(t, state.handle)
	p, _, _ := testPlatform(t, ra.addr())
	p.frontend.capability = loadSupported
	cfg := testConfig(t)
	require.NoError(t, afero.WriteFile(p.fs, retroArchExecutable, nil, 0o755))
	recorded := filepath.Join(retroArchCoresDir, "snes9x2010_libretro.so")
	writeFile(t, p.fs, recorded)
	writePlaylist(t, p.fs, snesPlaylistName,
		`{"items": [{"path": "`+testContent+`", "core_path": "`+recorded+`"}]}`)

	launcher := primarySNESLauncher(t, p.Launchers(cfg))
	_, err := launcher.Launch(cfg, testContent, &platforms.LaunchOptions{})
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		return ra.count(commandLoadContent+" "+recorded+"|"+testContent) == 1
	}, time.Second, time.Millisecond)
}

func FuzzParsePlaylist(f *testing.F) {
	f.Add([]byte(`{"version":"1.5","default_core_path":"DETECT","items":[{"path":"/storage/roms/a.zip#b.sfc",` +
		`"label":"A","core_path":"/tmp/cores/x_libretro.so","core_name":"X","crc32":"0|crc","db_name":"MAME.lpl"}]}`))
	f.Add([]byte("/storage/roms/a.sfc\nA\nDETECT\nDETECT\nDETECT\nSony - PlayStation.lpl\n"))
	f.Add([]byte(`{"items":[1,"x",null,{"path":7}]}`))
	f.Add([]byte("\xef\xbb\xbf{"))
	f.Add([]byte(`{"items":[{"path":"/\u023a\u023a\u023a\u023a.zip#"}]}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		pl, err := parsePlaylist(data)
		if err != nil {
			return
		}
		if len(pl.Entries) > maxPlaylistEntries {
			t.Fatalf("%d entries", len(pl.Entries))
		}
		for i := range pl.Entries {
			e := &pl.Entries[i]
			if e.Path == "" {
				t.Fatal("entry without a path")
			}
			for _, field := range []string{e.Path, e.Label, e.CorePath, e.CoreName, e.CRC32, e.DBName} {
				if len(field) > maxPlaylistField || strings.ContainsAny(field, "\x00\r\n") {
					t.Fatalf("unclean field %q", field)
				}
			}
			path, _, ok := playlistTarget(e.Path)
			if ok && (!filepath.IsAbs(path) || path != filepath.Clean(path)) {
				t.Fatalf("target %q of %q is not a clean absolute path", path, e.Path)
			}
			_ = entryName(e, path, "")
			_ = entryCore(e.CorePath, pl.DefaultCorePath)
		}
	})
}
