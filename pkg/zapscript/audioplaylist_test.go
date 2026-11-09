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
	path := writePlaylistFile(t, dir, "mix.m3u", "\xef\xbb\xbf#EXTM3U\r\n"+
		"#EXTINF:-1,Groove Salad\r\n"+
		"http://ice1.somafm.com/groovesalad-128-mp3\r\n"+
		"\r\n"+
		"# a comment\r\n"+
		"album\\one.mp3\r\n"+
		"#EXTINF:120,Missing\r\n"+
		"missing.mp3\r\n"+
		"https://zpr.au/abc\r\n")

	items, err := readPlaylistFile(path)
	require.NoError(t, err)
	assert.Equal(t, []playlists.PlaylistItem{
		{Name: "Groove Salad", ZapScript: literalLaunch("http://ice1.somafm.com/groovesalad-128-mp3")},
		{Name: "", ZapScript: literalLaunch(filepath.Join(sub, "one.mp3"))},
		{Name: "Missing", ZapScript: "missing.mp3"},
		{Name: "", ZapScript: literalLaunch("https://zpr.au/abc")},
	}, items)
}

func TestReadM3UFile_NoEntries(t *testing.T) {
	t.Parallel()

	path := writePlaylistFile(t, t.TempDir(), "empty.m3u", "#EXTM3U\n#EXTINF:-1,Nothing\n")
	_, err := readPlaylistFile(path)
	require.ErrorContains(t, err, "no items found")
}

func TestReadPlaylistFile_RefusesOversizedFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	body := "[playlist]\nFile1=http://example.com/a\n" + strings.Repeat("#", maxPlaylistFileBytes)
	for _, name := range []string{"big.pls", "big.m3u"} {
		_, err := readPlaylistFile(writePlaylistFile(t, dir, name, body))
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

	items, err := readPlaylistFile(path)
	require.NoError(t, err)
	assert.Equal(t, []playlists.PlaylistItem{
		{Name: "SomaFM: Groove Salad (#1)", ZapScript: literalLaunch("https://ice1.somafm.com/groovesalad-128-mp3")},
		{Name: "SomaFM: Groove Salad (#2)", ZapScript: literalLaunch("https://ice2.somafm.com/groovesalad-128-mp3")},
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

func TestIsAudioPlaylistTarget(t *testing.T) {
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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			target := tt.target
			assert.Equal(t, tt.want, isAudioPlaylistTarget(pl, env, &target))
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

// A playlist file with no system named is a playlist when native audio is
// the launcher that would run it: found by folder, guessed from its
// extension, named in the launch, or the background slot's only player. A
// disc set .m3u in a console's folder stays that console's.
func TestIsAudioPlaylistTarget_ByLauncher(t *testing.T) {
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
			assert.Equal(t, tt.want, isAudioPlaylistTarget(mockPlatform, env, &target))
		})
	}
}
