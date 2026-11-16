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

package installer

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type zipEntry struct {
	name string
	body string
	mode fs.FileMode
}

func buildZip(t testing.TB, entries []zipEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, e := range entries {
		h := &zip.FileHeader{Name: e.name, Method: zip.Deflate}
		if e.mode != 0 {
			h.SetMode(e.mode)
		}
		fw, err := w.CreateHeader(h)
		require.NoError(t, err)
		if e.body != "" {
			_, err = fw.Write([]byte(e.body))
			require.NoError(t, err)
		}
	}
	require.NoError(t, w.Close())
	return buf.Bytes()
}

func writeZip(t testing.TB, entries []zipEntry) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "game.zip")
	require.NoError(t, os.WriteFile(p, buildZip(t, entries), 0o600))
	return p
}

func platformWithFolders(
	t *testing.T, dataDir string, roots []string, launchers []platforms.Launcher,
) *mocks.MockPlatform {
	t.Helper()
	pl := mocks.NewMockPlatform()
	pl.On("ID").Return("mock-platform")
	pl.On("Settings").Return(platforms.Settings{DataDir: dataDir})
	pl.On("RootDirs", mock.AnythingOfType("*config.Instance")).Return(roots)
	pl.On("Launchers", mock.AnythingOfType("*config.Instance")).Return(launchers)
	return pl
}

func TestFindInstallDirUsesExistingSystemFolder(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	rootA := t.TempDir()
	rootB := t.TempDir()
	// Only the lower priority root has the folder: the download goes beside
	// the games already there.
	require.NoError(t, os.MkdirAll(filepath.Join(rootB, "SNES"), 0o750))
	pl := platformWithFolders(t, dataDir, []string{rootA, rootB}, []platforms.Launcher{
		{ID: "Other", SystemID: "NES", Folders: []string{"NES"}},
		{ID: "Skip", SystemID: "SNES", Folders: []string{"Skipped"}, SkipFilesystemScan: true},
		{ID: "SNES", SystemID: "SNES", Folders: []string{"SNES"}},
	})
	require.NoError(t, os.MkdirAll(filepath.Join(rootA, "Skipped"), 0o750))

	got, err := findInstallDir(&config.Instance{}, pl, "snes", mediaNames{filename: "Game.sfc"})
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(rootB, "SNES", "Game.sfc"), got)
}

func TestFindInstallDirUsesAbsoluteFolder(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	abs := filepath.Join(t.TempDir(), "roms", "snes")
	require.NoError(t, os.MkdirAll(abs, 0o750))
	pl := platformWithFolders(t, dataDir, []string{t.TempDir()}, []platforms.Launcher{
		{ID: "SNES", SystemID: "SNES", Folders: []string{"SNES", abs}},
	})

	got, err := findInstallDir(&config.Instance{}, pl, "snes", mediaNames{filename: "Game.sfc"})
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(abs, "Game.sfc"), got)
}

func TestFindInstallDirCreatesFolderUnderFirstExistingRoot(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	missing := filepath.Join(t.TempDir(), "unplugged")
	root := t.TempDir()
	pl := platformWithFolders(t, dataDir, []string{missing, root}, []platforms.Launcher{
		{ID: "SNES", SystemID: "SNES", Folders: []string{"SNES", "SFC"}},
	})

	got, err := findInstallDir(&config.Instance{}, pl, "snes", mediaNames{filename: "Game.sfc"})
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(root, "SNES", "Game.sfc"), got)
}

func TestFindInstallDirFallsBackToMediaFolder(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	pl := platformWithFolders(t, dataDir, []string{filepath.Join(t.TempDir(), "gone")},
		[]platforms.Launcher{{ID: "SNES", SystemID: "SNES", Folders: []string{"SNES"}}})

	got, err := findInstallDir(&config.Instance{}, pl, "snes", mediaNames{filename: "Game.sfc"})
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dataDir, config.MediaDir, "SNES", "Game.sfc"), got)
}

// A file downloaded before downloads moved next to the user's games is
// launched from where it is instead of being fetched again.
func TestFindInstallDirReusesLegacyDownload(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "SNES"), 0o750))
	legacy := filepath.Join(dataDir, config.MediaDir, "SNES", "Game.sfc")
	require.NoError(t, os.MkdirAll(filepath.Dir(legacy), 0o750))
	require.NoError(t, os.WriteFile(legacy, []byte("rom"), 0o600))
	pl := platformWithFolders(t, dataDir, []string{root},
		[]platforms.Launcher{{ID: "SNES", SystemID: "SNES", Folders: []string{"SNES"}}})

	got, err := findInstallDir(&config.Instance{}, pl, "snes", mediaNames{filename: "Game.sfc"})
	require.NoError(t, err)
	assert.Equal(t, legacy, got)

	other, err := findInstallDir(&config.Instance{}, pl, "snes", mediaNames{filename: "Other.sfc"})
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(root, "SNES", "Other.sfc"), other)
}

func fileDownloader(t *testing.T, body []byte, calls *int) func(DownloaderArgs) error {
	t.Helper()
	return func(args DownloaderArgs) error {
		*calls++
		if err := os.WriteFile(args.tempPath, body, 0o600); err != nil {
			return fmt.Errorf("write download: %w", err)
		}
		if err := os.Rename(args.tempPath, args.finalPath); err != nil {
			return fmt.Errorf("finish download: %w", err)
		}
		return nil
	}
}

func TestInstallRemoteMediaExtractsArchive(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "PSX"), 0o750))
	pl := platformWithFolders(t, dataDir, []string{root},
		[]platforms.Launcher{{ID: "PSX", SystemID: "PSX", Folders: []string{"PSX"}}})

	body := buildZip(t, []zipEntry{
		{name: "Game (Disc 1).cue", body: "cue1"},
		{name: "Game (Disc 1).bin", body: "bin1"},
		{name: "Game (Disc 2).cue", body: "cue2"},
		{name: "Game (Disc 2).bin", body: "bin2"},
		{name: "Game.m3u", body: "Game (Disc 1).cue\nGame (Disc 2).cue\n"},
	})
	calls := 0
	media := &RemoteMedia{
		URL:        "http://romm.local/api/roms/1/content/Game",
		SystemID:   "psx",
		FileName:   "Game",
		Archive:    true,
		LaunchExts: []string{".cue", ".m3u", ".chd"},
	}

	got, err := InstallRemoteMedia(context.Background(), &config.Instance{}, pl, nil, media,
		fileDownloader(t, body, &calls))
	require.NoError(t, err)
	gameDir := filepath.Join(root, "PSX", "Game")
	assert.Equal(t, filepath.Join(gameDir, "Game.m3u"), got)
	assert.FileExists(t, filepath.Join(gameDir, "Game (Disc 2).bin"))

	entries, err := os.ReadDir(filepath.Join(root, "PSX"))
	require.NoError(t, err)
	require.Len(t, entries, 1, "zip and staging folder are removed")

	// A second launch uses the extracted game without downloading again.
	again, err := InstallRemoteMedia(context.Background(), &config.Instance{}, pl, nil, media,
		fileDownloader(t, body, &calls))
	require.NoError(t, err)
	assert.Equal(t, got, again)
	assert.Equal(t, 1, calls)
}

func TestInstallRemoteMediaDropsSingleDiscPlaylist(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "PSX"), 0o750))
	pl := platformWithFolders(t, dataDir, []string{root},
		[]platforms.Launcher{{ID: "PSX", SystemID: "PSX", Folders: []string{"PSX"}}})

	body := buildZip(t, []zipEntry{
		{name: "Game.cue", body: "cue"},
		{name: "Game.bin", body: "bin"},
		{name: "Game.m3u", body: "Game.cue\n"},
	})
	calls := 0
	media := &RemoteMedia{
		URL:        "http://romm.local/api/roms/1/content/Game",
		SystemID:   "psx",
		FileName:   "Game",
		Archive:    true,
		LaunchExts: []string{".cue", ".m3u"},
	}

	got, err := InstallRemoteMedia(context.Background(), &config.Instance{}, pl, nil, media,
		fileDownloader(t, body, &calls))
	require.NoError(t, err)
	gameDir := filepath.Join(root, "PSX", "Game")
	assert.Equal(t, filepath.Join(gameDir, "Game.cue"), got)
	assert.NoFileExists(t, filepath.Join(gameDir, "Game.m3u"),
		"a playlist naming one disc would index the game twice")
}

func TestDropSingleDiscPlaylists(t *testing.T) {
	t.Parallel()

	write := func(t *testing.T, dir string, files map[string]string) {
		t.Helper()
		for name, body := range files {
			full := filepath.Join(dir, filepath.FromSlash(name))
			require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o750))
			require.NoError(t, os.WriteFile(full, []byte(body), 0o600))
		}
	}

	for _, tc := range []struct {
		files    map[string]string
		name     string
		exts     []string
		wantKept bool
	}{
		{
			name:  "single disc with comments and BOM",
			files: map[string]string{"g.cue": "c", "g.m3u": "\ufeff#EXTM3U\r\n\r\ng.cue\r\n"},
		},
		{
			name:  "single disc in a subfolder",
			files: map[string]string{"sub/g.cue": "c", "sub/g.m3u": "g.cue"},
		},
		{
			name:     "several discs",
			files:    map[string]string{"a.cue": "c", "b.cue": "c", "g.m3u": "a.cue\nb.cue\n"},
			wantKept: true,
		},
		{
			name:     "named disc missing",
			files:    map[string]string{"g.m3u": "g.cue"},
			wantKept: true,
		},
		{
			name:     "named disc outside the game",
			files:    map[string]string{"sub/g.m3u": "../../outside.cue"},
			wantKept: true,
		},
		{
			name:     "system opens only playlists",
			files:    map[string]string{"g.cue": "c", "g.m3u": "g.cue"},
			exts:     []string{".m3u"},
			wantKept: true,
		},
		{
			name:     "empty playlist",
			files:    map[string]string{"g.cue": "c", "g.m3u": "# nothing\n"},
			wantKept: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			write(t, dir, tc.files)
			dropSingleDiscPlaylists(dir, tc.exts)
			var playlist string
			for name := range tc.files {
				if strings.HasSuffix(name, ".m3u") {
					playlist = filepath.Join(dir, filepath.FromSlash(name))
				}
			}
			if tc.wantKept {
				assert.FileExists(t, playlist)
			} else {
				assert.NoFileExists(t, playlist)
			}
		})
	}
}

func TestInstallRemoteMediaRejectsUnsafeArchive(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "PSX"), 0o750))
	pl := platformWithFolders(t, dataDir, []string{root},
		[]platforms.Launcher{{ID: "PSX", SystemID: "PSX", Folders: []string{"PSX"}}})

	body := buildZip(t, []zipEntry{
		{name: "Game.cue", body: "cue"},
		{name: "../../escape.sh", body: "pwn"},
	})
	calls := 0
	_, err := InstallRemoteMedia(context.Background(), &config.Instance{}, pl, nil, &RemoteMedia{
		URL:      "http://romm.local/api/roms/1/content/Game",
		SystemID: "psx",
		FileName: "Game",
		Archive:  true,
	}, fileDownloader(t, body, &calls))
	require.ErrorIs(t, err, ErrUnsafeArchive)

	entries, err := os.ReadDir(filepath.Join(root, "PSX"))
	require.NoError(t, err)
	assert.Empty(t, entries, "nothing is left behind")
	assert.NoFileExists(t, filepath.Join(root, "escape.sh"))
}

func TestInstallRemoteMediaArchiveWithoutLaunchFile(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	pl := setupMockPlatformWithTempDir(t, dataDir)
	body := buildZip(t, []zipEntry{{name: "readme.txt", body: "hi"}})
	calls := 0
	_, err := InstallRemoteMedia(context.Background(), &config.Instance{}, pl, nil, &RemoteMedia{
		URL:        "http://romm.local/api/roms/1/content/Game",
		SystemID:   "psx",
		FileName:   "Game",
		Archive:    true,
		LaunchExts: []string{".cue"},
	}, fileDownloader(t, body, &calls))
	require.Error(t, err)
	assert.NoDirExists(t, filepath.Join(dataDir, config.MediaDir, "PSX", "Game"))
}

func TestInstallRemoteMediaUsesFileName(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	pl := setupMockPlatformWithTempDir(t, dataDir)
	calls := 0
	got, err := InstallRemoteMedia(context.Background(), &config.Instance{}, pl, nil, &RemoteMedia{
		URL:      "http://romm.local/api/roms/7/content/Some%20Game.nes?purpose=play",
		SystemID: "nes",
		FileName: "Some Game (USA).nes",
	}, fileDownloader(t, []byte("rom"), &calls))
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dataDir, config.MediaDir, "NES", "Some Game (USA).nes"), got)
}

func TestInstallRemoteMediaRefusesWhenDiskTooSmall(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	pl := setupMockPlatformWithTempDir(t, dataDir)
	calls := 0
	_, err := InstallRemoteMedia(context.Background(), &config.Instance{}, pl, nil, &RemoteMedia{
		URL:      "http://romm.local/api/roms/7/content/huge.iso",
		SystemID: "psx",
		Size:     1 << 62,
	}, fileDownloader(t, []byte("rom"), &calls))
	require.ErrorIs(t, err, ErrInsufficientSpace)
	assert.Zero(t, calls)
}

func TestSafeArchiveEntryName(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"Game.cue", "Disc 1/Track 01.bin", "folder/"} {
		assert.True(t, safeArchiveEntryName(name), name)
	}
	for _, name := range []string{
		"", "/", "/etc/passwd", "../x", "a/../../x", "a/./b", "a//b", `a\b`, "C:x",
		"a:stream", "x\x00y", "x\ny", ".", "..",
	} {
		assert.False(t, safeArchiveEntryName(name), "%q", name)
	}
}

func TestExtractZipRejectsSymlink(t *testing.T) {
	t.Parallel()

	zipPath := writeZip(t, []zipEntry{{name: "link", body: "/etc/passwd", mode: fs.ModeSymlink | 0o777}})
	err := extractZip(context.Background(), zipPath, filepath.Join(t.TempDir(), "out"), 1<<20)
	require.ErrorIs(t, err, ErrUnsafeArchive)
}

func TestExtractZipRejectsDuplicateEntry(t *testing.T) {
	t.Parallel()

	zipPath := writeZip(t, []zipEntry{{name: "a.bin", body: "1"}, {name: "a.bin", body: "2"}})
	err := extractZip(context.Background(), zipPath, filepath.Join(t.TempDir(), "out"), 1<<20)
	require.Error(t, err)
}

func TestExtractZipStopsAtBudget(t *testing.T) {
	t.Parallel()

	zipPath := writeZip(t, []zipEntry{
		{name: "a.bin", body: string(bytes.Repeat([]byte("x"), 600))},
		{name: "b.bin", body: string(bytes.Repeat([]byte("y"), 600))},
	})
	err := extractZip(context.Background(), zipPath, filepath.Join(t.TempDir(), "out"), 1000)
	require.ErrorIs(t, err, errArchiveTooLarge)
}

func TestExtractZipHonoursCancellation(t *testing.T) {
	t.Parallel()

	zipPath := writeZip(t, []zipEntry{{name: "a.bin", body: "1"}})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := extractZip(ctx, zipPath, filepath.Join(t.TempDir(), "out"), 1<<20)
	require.ErrorIs(t, err, context.Canceled)
}

func TestPickLaunchFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	for _, p := range []string{"z.bin", "a.cue", "sub/b.m3u", "sub/deeper/c.chd"} {
		full := filepath.Join(dir, filepath.FromSlash(p))
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o750))
		require.NoError(t, os.WriteFile(full, []byte("x"), 0o600))
	}

	got, err := pickLaunchFile(dir, nil)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, "sub", "b.m3u"), got, "a playlist wins at any depth")

	got, err = pickLaunchFile(dir, []string{".cue", ".chd"})
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, "a.cue"), got, "only extensions the system opens")

	got, err = pickLaunchFile(dir, []string{"CHD"})
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, "sub", "deeper", "c.chd"), got)

	_, err = pickLaunchFile(dir, []string{".iso"})
	require.Error(t, err)
}
