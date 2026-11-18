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

package zapscript

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mediaslot"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writePlaylistFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func TestReadM3UFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	sub := filepath.Join(dir, "album")
	require.NoError(t, os.Mkdir(sub, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(sub, "one.mp3"), nil, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "game.nsf"), nil, 0o600))
	path := writePlaylistFile(t, dir, "mix.m3u", "\xef\xbb\xbf#EXTM3U\r\n"+
		"#EXTINF:-1,Groove Salad\r\n"+
		"http://ice1.somafm.com/groovesalad-128-mp3\r\n"+
		"\r\n"+
		"# a comment\r\n"+
		"album\\one.mp3\r\n"+
		"#EXTINF:120,Missing\r\n"+
		"missing.mp3\r\n"+
		"https://zpr.au/abc\r\n"+
		"game.nsf/5\r\n")

	items, err := readPlaylistFile(nil, nil, path)
	require.NoError(t, err)
	assert.Equal(t, []playlists.PlaylistItem{
		{Name: "Groove Salad", ZapScript: literalLaunch("http://ice1.somafm.com/groovesalad-128-mp3")},
		{Name: "", ZapScript: literalLaunch(filepath.Join(sub, "one.mp3"))},
		{Name: "Missing", ZapScript: "missing.mp3"},
		{Name: "", ZapScript: literalLaunch("https://zpr.au/abc")},
		{Name: "", ZapScript: literalLaunch(filepath.Join(dir, "game.nsf", "5"))},
	}, items)
}

func TestReadM3UFile_NoEntries(t *testing.T) {
	t.Parallel()

	path := writePlaylistFile(t, t.TempDir(), "empty.m3u", "#EXTM3U\n#EXTINF:-1,Nothing\n")
	_, err := readPlaylistFile(nil, nil, path)
	require.ErrorContains(t, err, "no items found")
}

func TestReadPlaylistFile_RefusesOversizedFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	body := "[playlist]\nFile1=http://example.com/a\n" + strings.Repeat("#", maxPlaylistFileBytes)
	for _, name := range []string{"big.pls", "big.m3u"} {
		_, err := readPlaylistFile(nil, nil, writePlaylistFile(t, dir, name, body))
		require.ErrorIs(t, err, errPlaylistFileTooLarge, name)
	}
}

// A station's pls carries keys a playlist has no use for; they are read
// past rather than logged as unrecognized.
func TestReadPlsFile_StationPlaylist(t *testing.T) {
	t.Parallel()

	path := writePlaylistFile(t, t.TempDir(), "groovesalad.pls", "[playlist]\n"+
		"numberofentries=2\n"+
		"File1=https://ice1.somafm.com/groovesalad-128-mp3\n"+
		"Title1=SomaFM: Groove Salad (#1)\n"+
		"Length1=-1\n"+
		"File2=https://ice2.somafm.com/groovesalad-128-mp3\n"+
		"Title2=SomaFM: Groove Salad (#2)\n"+
		"Length2=-1\n"+
		"Version=2\n")

	items, err := readPlaylistFile(nil, nil, path)
	require.NoError(t, err)
	assert.Equal(t, []playlists.PlaylistItem{
		{Name: "SomaFM: Groove Salad (#1)", ZapScript: literalLaunch("https://ice1.somafm.com/groovesalad-128-mp3")},
		{Name: "SomaFM: Groove Salad (#2)", ZapScript: literalLaunch("https://ice2.somafm.com/groovesalad-128-mp3")},
	}, items)
}

// A pls saved with a byte order mark, a capitalised header or lowercase
// keys still reads.
func TestReadPlsFile_HeaderAndKeyVariants(t *testing.T) {
	t.Parallel()

	path := writePlaylistFile(t, t.TempDir(), "station.pls", "\xef\xbb\xbf[Playlist]\r\n"+
		"file1=https://radio.example/stream\r\n"+
		"title1=Example Radio\r\n")

	items, err := readPlaylistFile(nil, nil, path)
	require.NoError(t, err)
	assert.Equal(t, []playlists.PlaylistItem{
		{Name: "Example Radio", ZapScript: literalLaunch("https://radio.example/stream")},
	}, items)
}

// A playlist entry runs as ZapScript, so a URL entry must parse as a launch
// of the whole URL, query included, to reach the stream and ZapLink checks.
func TestLiteralLaunchParsesAsLaunchOfTheLocation(t *testing.T) {
	t.Parallel()

	for _, entry := range []string{
		"http://ice1.somafm.com/groovesalad-128-mp3",
		"https://example.com/listen.pls?sid=1&type=http",
		"http://example.com:8000/;stream.mp3?icy=http",
		"https://zpr.au/abc",
		"/music/a ^weird| [[name]] {x} @y #z, 'q' \"d\" *s.mp3",
		"**launch.random:snes",
		"@SNES/Super Mario World",
		"{\"x\":1}",
		"#trait",
	} {
		script, err := zapscript.NewParser(literalLaunch(entry)).ParseScript()
		require.NoError(t, err, entry)
		require.Len(t, script.Cmds, 1, entry)
		assert.Equal(t, zapscript.ZapScriptCmdLaunch, script.Cmds[0].Name, entry)
		require.NotEmpty(t, script.Cmds[0].Args, entry)
		assert.Equal(t, entry, script.Cmds[0].Args[0], entry)
	}
}

func TestIsPlaylistFileTarget(t *testing.T) {
	t.Parallel()

	pl := newPlaylistTestPlatform()
	env := &platforms.CmdEnv{Cfg: &config.Instance{}}
	tests := []struct {
		name   string
		target launchTarget
		want   bool
	}{
		{
			name:   "pls in audio",
			target: launchTarget{path: "/music/a.pls", systemID: systemdefs.SystemAudio},
			want:   true,
		},
		{
			name:   "m3u in audio",
			target: launchTarget{path: "/music/a.M3U", systemID: systemdefs.SystemAudio},
			want:   true,
		},
		{name: "disc set m3u", target: launchTarget{path: "/games/PSX/a.m3u", systemID: systemdefs.SystemPSX}},
		{name: "a track", target: launchTarget{path: "/music/a.mp3", systemID: systemdefs.SystemAudio}},
		{
			name:   "playlist url",
			target: launchTarget{path: "https://radio.example/listen.pls", systemID: systemdefs.SystemAudio},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			target := tt.target
			assert.Equal(t, tt.want, isPlaylistFileTarget(pl, env, &target))
		})
	}
}

// Launching a playlist file kept with audio plays it as a playlist through
// the real command path: it is queued with the launch guard already let go,
// since its first item takes the guard itself, and it runs untrusted.
func TestRunCommand_LaunchingAnAudioPlaylistFilePlaysItAsAPlaylist(t *testing.T) {
	t.Parallel()

	for _, slot := range []string{"", mediaslot.Background} {
		name := slot
		if name == "" {
			name = "primary"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			path := writePlaylistFile(t, t.TempDir(), "Groove Salad.m3u",
				"#EXTINF:-1,Groove Salad\nhttp://ice1.somafm.com/groovesalad-128-mp3\n")
			text := "**launch:" + path + "?system=audio"
			if slot != "" {
				text += "&slot=" + slot
			}
			script, err := zapscript.NewParser(text).ParseScript()
			require.NoError(t, err)

			lm := state.NewLauncherManager()
			queue := make(chan *playlists.Playlist, 1)
			var guardHeldAtQueue bool
			result, err := RunCommand(
				t.Context(), newPlaylistTestPlatform(), &config.Instance{},
				playlists.PlaylistController{Queue: queue},
				tokens.Token{Source: tokens.SourceAPI}, script.Cmds[0], 1, 0, nil,
				&RunCommandOptions{LauncherManager: lm}, &zapscript.ArgExprEnv{},
			)
			require.NoError(t, err)
			pls := <-queue
			guardHeldAtQueue = lm.Launching()

			assert.True(t, result.PlaylistChanged)
			assert.False(t, result.MediaChanged)
			assert.False(t, guardHeldAtQueue, "the launch guard is let go before the playlist is queued")
			assert.Equal(t, "Groove Salad", pls.Name)
			assert.True(t, pls.Playing)
			assert.True(t, pls.Unsafe, "a playlist file's entries run untrusted")
			wantSlot := mediaslot.Primary
			if slot != "" {
				wantSlot = slot
			}
			assert.Equal(t, wantSlot, pls.Slot)
			assert.Equal(t, []playlists.PlaylistItem{
				{Name: "Groove Salad", ZapScript: literalLaunch("http://ice1.somafm.com/groovesalad-128-mp3")},
			}, pls.Items)
		})
	}
}

// A playlist file launched by an item of a background playlist plays in the
// background too, as any other item of that playlist would.
func TestRunCommand_AudioPlaylistFileFollowsItsPlaylistSlot(t *testing.T) {
	t.Parallel()

	path := writePlaylistFile(t, t.TempDir(), "Station.pls",
		"[playlist]\nFile1=http://radio.example/stream\nNumberOfEntries=1\n")
	script, err := zapscript.NewParser("**launch:" + path + "?system=audio").ParseScript()
	require.NoError(t, err)

	background := playlists.NewPlaylist("bg", "Background", []playlists.PlaylistItem{{ZapScript: path}})
	background.Slot = mediaslot.Background
	queue := make(chan *playlists.Playlist, 1)
	_, err = RunCommand(
		t.Context(), newPlaylistTestPlatform(), &config.Instance{},
		playlists.PlaylistController{Queue: queue, Background: background, Current: background},
		tokens.Token{Source: tokens.SourcePlaylist}, script.Cmds[0], 1, 0, nil,
		&RunCommandOptions{LauncherManager: state.NewLauncherManager()}, &zapscript.ArgExprEnv{},
	)
	require.NoError(t, err)
	pls := <-queue
	assert.Equal(t, mediaslot.Background, pls.Slot)
}

// A playlist file with no system named is a playlist unless another launcher
// takes it or is named in the launch. A disc set .m3u in a console's folder
// stays that console's.
func TestIsPlaylistFileTarget_ByLauncher(t *testing.T) {
	// Cannot use t.Parallel() - modifies shared GlobalLauncherCache
	dir := t.TempDir()
	psxDir := filepath.Join(dir, "PSX")
	cfg := &config.Instance{}
	native := platforms.Launcher{
		ID: platforms.NativeAudioLauncherID, SystemID: systemdefs.SystemAudio,
		Extensions: []string{".mp3", ".pls", ".m3u"},
	}
	psx := platforms.Launcher{
		ID: "PSX", SystemID: systemdefs.SystemPSX,
		Folders: []string{psxDir}, Extensions: []string{".cue", ".m3u"},
	}
	helpers.GlobalLauncherCache.InitializeFromSlice([]platforms.Launcher{native, psx})
	t.Cleanup(func() { helpers.GlobalLauncherCache.InitializeFromSlice(nil) })
	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("Launchers", cfg).Return([]platforms.Launcher{native, psx}).Maybe()
	mockPlatform.On("RootDirs", cfg).Return([]string{}).Maybe()
	mockPlatform.On("Settings").Return(platforms.Settings{}).Maybe()

	tests := []struct {
		args map[string]string
		name string
		path string
		want bool
	}{
		{name: "guessed pls", path: filepath.Join(dir, "Soma.pls"), want: true},
		{name: "m3u outside a console folder", path: filepath.Join(dir, "Mix.m3u"), want: true},
		{name: "disc set in a console folder", path: filepath.Join(psxDir, "Game.m3u")},
		{
			name: "background slot",
			path: filepath.Join(dir, "Mix.m3u"),
			args: map[string]string{string(zapscript.KeySlot): mediaslot.Background},
			want: true,
		},
		{
			name: "native audio named",
			path: filepath.Join(psxDir, "Mix.m3u"),
			args: map[string]string{string(zapscript.KeyLauncher): platforms.NativeAudioLauncherID},
			want: true,
		},
		{
			name: "another launcher named",
			path: filepath.Join(dir, "Soma.pls"),
			args: map[string]string{string(zapscript.KeyLauncher): "PSX"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := &platforms.CmdEnv{
				Cfg: cfg,
				Cmd: zapscript.Command{Name: zapscript.ZapScriptCmdLaunch, AdvArgs: zapscript.NewAdvArgs(tt.args)},
			}
			target := launchTarget{path: tt.path}
			assert.Equal(t, tt.want, isPlaylistFileTarget(mockPlatform, env, &target))
		})
	}
}

// claimingLaunchers sets up native audio in a Music folder, a console that
// lists .m3u for its disc sets, and a frontend that takes any file under its
// folder through Test, and returns a platform that knows them.
func claimingLaunchers(t *testing.T, cfg *config.Instance, psxDir, frontendDir string) *mocks.MockPlatform {
	t.Helper()
	native := platforms.Launcher{
		ID: platforms.NativeAudioLauncherID, SystemID: systemdefs.SystemAudio,
		Folders: []string{"Music"}, Extensions: []string{".mp3", ".pls", ".m3u", ".m3u8"},
	}
	psx := platforms.Launcher{
		ID: "PSX", SystemID: systemdefs.SystemPSX,
		Folders: []string{psxDir}, Extensions: []string{".cue", ".m3u"},
	}
	frontend := platforms.Launcher{
		ID: "Frontend", SystemID: systemdefs.SystemSaturn,
		Test: func(_ *config.Instance, path string) bool {
			return strings.HasPrefix(path, frontendDir+string(filepath.Separator))
		},
	}
	launchers := []platforms.Launcher{native, psx, frontend}
	helpers.GlobalLauncherCache.InitializeFromSlice(launchers)
	t.Cleanup(func() { helpers.GlobalLauncherCache.InitializeFromSlice(nil) })
	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("Launchers", cfg).Return(launchers).Maybe()
	mockPlatform.On("RootDirs", cfg).Return([]string{}).Maybe()
	mockPlatform.On("Settings").Return(platforms.Settings{}).Maybe()
	mockPlatform.On("ID").Return("test").Maybe()
	return mockPlatform
}

// A playlist file anywhere is a playlist unless a launcher other than native
// audio takes it, whether it lists the extension or matches it by Test.
func TestIsPlaylistFileTarget_DefersToClaimingLaunchers(t *testing.T) {
	// Cannot use t.Parallel() - modifies shared GlobalLauncherCache
	dir := t.TempDir()
	psxDir := filepath.Join(dir, "PSX")
	frontendDir := filepath.Join(dir, "roms")
	cfg := &config.Instance{}
	pl := claimingLaunchers(t, cfg, psxDir, frontendDir)

	tests := []struct {
		name string
		path string
		want bool
	}{
		{name: "unclaimed m3u", path: filepath.Join(dir, "Playlists", "Mix.m3u"), want: true},
		{name: "unclaimed pls", path: filepath.Join(dir, "Playlists", "Games.pls"), want: true},
		{name: "unclaimed m3u8", path: filepath.Join(dir, "Playlists", "Mix.m3u8"), want: true},
		{name: "console disc set", path: filepath.Join(psxDir, "Game.m3u")},
		{name: "frontend disc set", path: filepath.Join(frontendDir, "saturn", "Game.m3u")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := &platforms.CmdEnv{
				Cfg: cfg,
				Cmd: zapscript.Command{Name: zapscript.ZapScriptCmdLaunch, AdvArgs: zapscript.NewAdvArgs(nil)},
			}
			target := launchTarget{path: tt.path}
			assert.Equal(t, tt.want, isPlaylistFileTarget(pl, env, &target))
		})
	}
}

// A bare launch of a playlist file no launcher takes plays it as a playlist
// through the real command path, outside any audio folder.
func TestRunCommand_LaunchingAnUnclaimedPlaylistFilePlaysIt(t *testing.T) {
	// Cannot use t.Parallel() - modifies shared GlobalLauncherCache
	helpers.GlobalLauncherCache.InitializeFromSlice(nil)
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.sfc"), nil, 0o600))
	path := writePlaylistFile(t, dir, "Games.m3u", "a.sfc\n")
	script, err := zapscript.NewParser("**launch:" + path).ParseScript()
	require.NoError(t, err)

	queue := make(chan *playlists.Playlist, 1)
	result, err := RunCommand(
		t.Context(), newPlaylistTestPlatform(), &config.Instance{},
		playlists.PlaylistController{Queue: queue},
		tokens.Token{Source: tokens.SourceAPI}, script.Cmds[0], 1, 0, nil,
		&RunCommandOptions{LauncherManager: state.NewLauncherManager()}, &zapscript.ArgExprEnv{},
	)
	require.NoError(t, err)
	assert.True(t, result.PlaylistChanged)
	pls := <-queue
	assert.Equal(t, "Games", pls.Name)
	assert.Equal(t, []playlists.PlaylistItem{{ZapScript: literalLaunch(filepath.Join(dir, "a.sfc"))}}, pls.Items)
}

// Asking for a playlist reads the file as one even when a launcher takes it.
func TestLoadPlaylist_ReadsAClaimedPlaylistFile(t *testing.T) {
	// Cannot use t.Parallel() - modifies shared GlobalLauncherCache
	dir := t.TempDir()
	psxDir := filepath.Join(dir, "PSX")
	require.NoError(t, os.MkdirAll(psxDir, 0o750))
	cfg := &config.Instance{}
	pl := claimingLaunchers(t, cfg, psxDir, filepath.Join(dir, "roms"))
	require.NoError(t, os.WriteFile(filepath.Join(psxDir, "Game (Disc 1).cue"), nil, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(psxDir, "Game (Disc 2).cue"), nil, 0o600))
	path := writePlaylistFile(t, psxDir, "Game.m3u", "Game (Disc 1).cue\nGame (Disc 2).cue\n")

	pls, err := loadPlaylist(pl, platforms.CmdEnv{
		ServiceCtx: t.Context(),
		Cfg:        cfg,
		Cmd:        zapscript.Command{Name: zapscript.ZapScriptCmdPlaylistOpen, Args: []string{path}},
	})
	require.NoError(t, err)
	assert.Equal(t, []playlists.PlaylistItem{
		{ZapScript: literalLaunch(filepath.Join(psxDir, "Game (Disc 1).cue"))},
		{ZapScript: literalLaunch(filepath.Join(psxDir, "Game (Disc 2).cue"))},
	}, pls.Items)
}

// A listed playlist file is read in place of its entry, in order. A console's
// disc set stays an entry, and a file that lists itself or loops back is
// read once.
func TestReadPlaylistFile_Nesting(t *testing.T) {
	// Cannot use t.Parallel() - modifies shared GlobalLauncherCache
	dir := t.TempDir()
	psxDir := filepath.Join(dir, "PSX")
	require.NoError(t, os.MkdirAll(psxDir, 0o750))
	cfg := &config.Instance{}
	pl := claimingLaunchers(t, cfg, psxDir, filepath.Join(dir, "roms"))
	disc := writePlaylistFile(t, psxDir, "Game.m3u", "Game.cue\n")
	for _, name := range []string{"one.mp3", "two.mp3", "three.mp3"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), nil, 0o600))
	}
	writePlaylistFile(t, dir, "inner.pls", "[playlist]\nFile1=two.mp3\nFile2=outer.m3u\nFile3=inner.pls\n")
	outer := writePlaylistFile(t, dir, "outer.m3u",
		"one.mp3\n#EXTINF:0,Inner\ninner.pls\nouter.m3u\nthree.mp3\n"+disc+"\n")

	items, err := readPlaylistFile(pl, &platforms.CmdEnv{Cfg: cfg}, outer)
	require.NoError(t, err)
	assert.Equal(t, []playlists.PlaylistItem{
		{ZapScript: literalLaunch(filepath.Join(dir, "one.mp3"))},
		{ZapScript: filepath.Join(dir, "two.mp3")},
		{ZapScript: literalLaunch(filepath.Join(dir, "three.mp3"))},
		{ZapScript: literalLaunch(disc)},
	}, items)
}

// A pls entry with launch arguments that names a playlist file being read
// would replace the playlist with a fresh read of itself when it runs.
func TestReadPlaylistFile_SkipsSelfReferenceWithArgs(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.mp3"), nil, 0o600))
	writePlaylistFile(t, dir, "inner.pls", "[playlist]\nFile1=outer.m3u?launcher=native-audio\nFile2=a.mp3\n")
	outer := writePlaylistFile(t, dir, "outer.m3u", "inner.pls\n")

	items, err := readPlaylistFile(nil, nil, outer)
	require.NoError(t, err)
	assert.Equal(t, []playlists.PlaylistItem{
		{ZapScript: filepath.Join(dir, "a.mp3")},
	}, items)
}

// A symlinked folder that leads back to the playlist's own folder does not
// let the playlist read itself again under a longer path.
func TestReadPlaylistFile_DetectsCyclesThroughSymlinks(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "song.mp3"), nil, 0o600))
	if err := os.Symlink(dir, filepath.Join(dir, "again")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	path := writePlaylistFile(t, dir, "list.m3u", "song.mp3\n"+filepath.Join("again", "list.m3u")+"\n")

	items, err := readPlaylistFile(nil, nil, path)
	require.NoError(t, err)
	assert.Equal(t, []playlists.PlaylistItem{
		{ZapScript: literalLaunch(filepath.Join(dir, "song.mp3"))},
	}, items)
}

func TestReadPlaylistFile_NestedReadsAreBounded(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.mp3"), nil, 0o600))
	writePlaylistFile(t, dir, "leaf.m3u", "a.mp3\n")
	path := writePlaylistFile(t, dir, "many.m3u", strings.Repeat("leaf.m3u\n", maxPlaylistFileReads+10))

	items, err := readPlaylistFile(nil, nil, path)
	require.NoError(t, err)
	assert.Len(t, items, maxPlaylistFileReads-1, "the outer file counts as one read")
}

func TestReadPlaylistFile_ItemCount(t *testing.T) {
	t.Parallel()

	path := writePlaylistFile(t, t.TempDir(), "big.m3u", strings.Repeat("/music/a.mp3\n", maxPlaylistItems+5))
	items, err := readPlaylistFile(nil, nil, path)
	require.NoError(t, err)
	assert.Len(t, items, maxPlaylistItems)
}

func TestReadPlaylistFile_RefusesHLSManifest(t *testing.T) {
	t.Parallel()

	path := writePlaylistFile(t, t.TempDir(), "live.m3u8",
		"#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-TARGETDURATION:6\n#EXTINF:6.0,\nseg0.ts\n")
	_, err := readPlaylistFile(nil, nil, path)
	require.ErrorIs(t, err, errHLSManifest)
}

// Entries with control characters or malformed URLs are skipped, and the
// rest of a mixed playlist is kept as written.
func TestReadPlaylistFile_SkipsBadEntries(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.mp3"), nil, 0o600))
	for _, name := range []string{"mix.m3u8", "mix.pls"} {
		var body string
		if strings.HasSuffix(name, ".pls") {
			body = "[playlist]\nFile1=a.mp3\nFile2=bad\x00name.mp3\nFile3=http:///nohost\n" +
				"File4=://nothing\nFile5=https://radio.example/live?sid=1\nFile6=zaparoo://launch/abc\n"
		} else {
			body = "a.mp3\r\nbad\x00name.mp3\r\nhttp:///nohost\r\n://nothing\r\n" +
				"https://radio.example/live?sid=1\r\nzaparoo://launch/abc\r\n"
		}
		items, err := readPlaylistFile(nil, nil, writePlaylistFile(t, dir, name, body))
		require.NoError(t, err, name)
		local := filepath.Join(dir, "a.mp3")
		if strings.HasSuffix(name, ".m3u8") {
			local = literalLaunch(local)
		}
		assert.Equal(t, []playlists.PlaylistItem{
			{ZapScript: local},
			{ZapScript: literalLaunch("https://radio.example/live?sid=1")},
			{ZapScript: literalLaunch("zaparoo://launch/abc")},
		}, items, name)
	}
}

// Relative entries are found next to the playlist, in subfolders, and then
// under the token's path root. A pls entry keeps its launch arguments.
func TestReadPlaylistFile_ResolvesRelativeEntries(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	lists := filepath.Join(root, "Playlists")
	album := filepath.Join(lists, "album")
	music := filepath.Join(root, "Music")
	require.NoError(t, os.MkdirAll(album, 0o750))
	require.NoError(t, os.MkdirAll(music, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(album, "song.mp3"), nil, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(lists, "song.mp3"), nil, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(music, "track.mp3"), nil, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(lists, "game.sfc"), nil, 0o600))

	path := writePlaylistFile(t, lists, "mix.pls", "[playlist]\n"+
		"File1=album/song.mp3\nFile2=Music/track.mp3\nFile3=game.sfc?launcher=snes9x\nFile4=missing.mp3\n")
	items, err := readPlaylistFile(nil, &platforms.CmdEnv{PathRoot: root}, path)
	require.NoError(t, err)
	assert.Equal(t, []playlists.PlaylistItem{
		{ZapScript: filepath.Join(album, "song.mp3")},
		{ZapScript: filepath.Join(music, "track.mp3")},
		{ZapScript: filepath.Join(lists, "game.sfc") + "?launcher=snes9x"},
		{ZapScript: "missing.mp3"},
	}, items)

	// Without a path root the entry is left for the launch to find.
	items, err = readPlaylistFile(nil, nil, path)
	require.NoError(t, err)
	assert.Equal(t, "Music/track.mp3", items[1].ZapScript)
}
