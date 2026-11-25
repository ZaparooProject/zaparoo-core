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

package romm

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func mockPlatform(t *testing.T, dataDir string, roots []string) *mocks.MockPlatform {
	t.Helper()
	pl := mocks.NewMockPlatform()
	pl.On("ID").Return("mock-platform").Maybe()
	pl.On("Settings").Return(platforms.Settings{DataDir: dataDir}).Maybe()
	pl.On("RootDirs", mock.AnythingOfType("*config.Instance")).Return(roots).Maybe()
	pl.On("Launchers", mock.AnythingOfType("*config.Instance")).Return([]platforms.Launcher{
		{ID: "NES", SystemID: systemdefs.SystemNES, Folders: []string{"NES"}, Extensions: []string{".nes", "ZIP"}},
		{ID: "PSX", SystemID: systemdefs.SystemPSX, Folders: []string{"PSX"}, Extensions: []string{".cue", ".chd"}},
		{ID: "Steamish", SystemID: systemdefs.SystemNES, Schemes: []string{"steam"}},
		{ID: "NamedOnly", SystemID: systemdefs.SystemNES, ExplicitOnly: true, Extensions: []string{".exe"}},
		{
			ID: "Player", SystemID: systemdefs.SystemAudio,
			Schemes: []string{"cdaudio"}, Extensions: []string{".mp3"},
		},
	}).Maybe()
	return pl
}

func nesFixture(t *testing.T) *fakeServer {
	t.Helper()
	bad := singleFileRom(4, 1, "nes", "Gone.nes")
	bad.MissingFromFS = true
	noFile := false
	offDisk := singleFileRom(5, 1, "nes", "Offline.nes")
	offDisk.HasFileOnDisk = &noFile
	return newFakeServer(t,
		[]Platform{
			{ID: 1, Slug: "nes", FSSlug: "nes", Name: "NES"},
			{ID: 2, Slug: "famicom", FSSlug: "fc", Name: "Famicom"},
			{ID: 3, Slug: "psx", FSSlug: "psx", Name: "PlayStation"},
			{ID: 9, Slug: "made-up", FSSlug: "made-up"},
		},
		[]Rom{
			singleFileRom(1, 1, "nes", "Super Game (USA).nes"),
			singleFileRom(2, 1, "nes", "Local Game (USA).nes"),
			singleFileRom(3, 1, "nes", "Wrong Format.fds"),
			bad,
			offDisk,
			singleFileRom(6, 2, "famicom", "Famicom Game (Japan).nes"),
			{
				ID: 7, PlatformID: 3, PlatformSlug: "psx", FSName: "Disc Game", FSNameNoExt: "Disc Game",
				HasMultipleFiles: true,
				Files: []RomFile{
					{FileName: "Disc Game (Disc 1).cue"},
					{FileName: "Disc Game (Disc 1).bin"},
					{FileName: "Disc Game (Disc 2).cue"},
					{FileName: "Disc Game (Disc 2).bin"},
				},
			},
		})
}

func TestScanListsServerRoms(t *testing.T) {
	t.Parallel()

	f := nesFixture(t)
	pl := mockPlatform(t, t.TempDir(), nil)
	local := []platforms.ScanResult{
		{Path: filepath.Join("roms", "NES", "local game (usa).NES"), Name: "Local Game (USA)"},
		{Path: "steam://1/Not A File.nes", Name: "Not A File", NoExt: true},
	}

	got, err := Scan(context.Background(), f.config(), pl, systemdefs.SystemNES, local)
	require.NoError(t, err)

	paths := make([]string, 0, len(got))
	for _, r := range got {
		paths = append(paths, r.Path)
	}
	assert.Equal(t, []string{
		local[0].Path,
		local[1].Path,
		RomPath(1, "Super Game (USA)"),
		RomPath(6, "Famicom Game (Japan)"),
	}, paths, "keeps the input, adds every listed ROM not already local, missing or unopenable")
	assert.True(t, got[2].NoExt)
	assert.Equal(t, "Super Game (USA)", got[2].Name)

	psx, err := Scan(context.Background(), f.config(), pl, systemdefs.SystemPSX, nil)
	require.NoError(t, err)
	require.Len(t, psx, 1)
	assert.Equal(t, RomPath(7, "Disc Game"), psx[0].Path)

	// A playlist on the device already stands for the whole multi-disc game.
	psx, err = Scan(context.Background(), f.config(), pl, systemdefs.SystemPSX,
		[]platforms.ScanResult{{Path: filepath.Join("PSX", "Disc Game.m3u")}})
	require.NoError(t, err)
	assert.Len(t, psx, 1)

	assert.Equal(t, int32(1), f.platCalls.Load(), "the platform list serves every system")
}

func TestScanWithoutServerReturnsInput(t *testing.T) {
	t.Parallel()

	pl := mocks.NewMockPlatform()
	in := []platforms.ScanResult{{Path: "/a.nes"}}
	got, err := Scan(context.Background(), &config.Instance{}, pl, systemdefs.SystemNES, in)
	require.NoError(t, err)
	assert.Equal(t, in, got)
}

// A launcher that also plays a scheme still opens its file extensions, but
// one that only runs when named does not take part in picking files.
func TestLaunchExtensions(t *testing.T) {
	t.Parallel()

	pl := mockPlatform(t, t.TempDir(), nil)
	assert.Equal(t, []string{".nes", ".zip"}, LaunchExtensions(nil, pl, systemdefs.SystemNES))
	assert.Equal(t, []string{".mp3"}, LaunchExtensions(nil, pl, systemdefs.SystemAudio))
}

func TestScanSkipsSystemWithoutFileLauncher(t *testing.T) {
	t.Parallel()

	f := nesFixture(t)
	pl := mockPlatform(t, t.TempDir(), nil)
	got, err := Scan(context.Background(), f.config(), pl, systemdefs.SystemSNES, nil)
	require.NoError(t, err)
	assert.Empty(t, got)
	assert.Zero(t, f.platCalls.Load())
}

func TestScanUnreachableServerIsSourceError(t *testing.T) {
	t.Parallel()

	f := nesFixture(t)
	f.failAll.Store(true)
	pl := mockPlatform(t, t.TempDir(), nil)
	in := []platforms.ScanResult{{Path: "/a.nes"}}
	got, err := Scan(context.Background(), f.config(), pl, systemdefs.SystemNES, in)
	require.ErrorIs(t, err, platforms.ErrScannerSourceUnreachable)
	assert.Equal(t, in, got)

	// The failure is remembered, so the next system does not wait on it again.
	_, err = Scan(context.Background(), f.config(), pl, systemdefs.SystemPSX, nil)
	require.ErrorIs(t, err, platforms.ErrScannerSourceUnreachable)
	assert.Equal(t, int32(0), f.platCalls.Load())
}

func TestScanRefusedTokenIsSourceError(t *testing.T) {
	t.Parallel()

	f := nesFixture(t)
	f.token = "right"
	t.Cleanup(config.AddAuthEntryForTesting(f.URL, config.CredentialEntry{Bearer: "wrong"}))
	pl := mockPlatform(t, t.TempDir(), nil)
	_, err := Scan(context.Background(), f.config(), pl, systemdefs.SystemNES, nil)
	require.ErrorIs(t, err, platforms.ErrScannerSourceUnreachable)
	require.ErrorIs(t, err, ErrUnauthorized)
}

func TestPlatformCacheExpires(t *testing.T) {
	// Replaces the package clock, so not parallel.
	f := nesFixture(t)
	c, err := NewClient(f.URL)
	require.NoError(t, err)

	clock := now()
	now = func() time.Time { return clock }
	t.Cleanup(func() { now = time.Now })

	_, err = platformsFor(context.Background(), c)
	require.NoError(t, err)
	_, err = platformsFor(context.Background(), c)
	require.NoError(t, err)
	assert.Equal(t, int32(1), f.platCalls.Load())

	clock = clock.Add(platformCacheTTL)
	_, err = platformsFor(context.Background(), c)
	require.NoError(t, err)
	assert.Equal(t, int32(2), f.platCalls.Load())
}

func TestLauncherNeverLaunchesItself(t *testing.T) {
	t.Parallel()

	f := nesFixture(t)
	l := NewLauncher(mocks.NewMockPlatform())
	cfg := f.config()

	assert.True(t, l.Test(cfg, "romm://1/Super Game (USA)"))
	assert.False(t, l.Test(cfg, "romm://abc/Game"))
	assert.False(t, l.Test(cfg, "romm://01/Game"))
	require.Error(t, l.Preflight(cfg, "romm://1/Super Game (USA)", nil))
	_, err := l.Launch(cfg, "romm://1/Super Game (USA)", nil)
	require.Error(t, err)

	require.NoError(t, l.Availability(cfg))
	require.ErrorIs(t, l.Availability(&config.Instance{}), ErrNotConfigured)

	assert.True(t, l.VirtualPathExists(cfg, "romm://1/Super Game (USA)"))
	assert.False(t, l.VirtualPathExists(cfg, "romm://4/Gone"), "missing on the server")
	assert.False(t, l.VirtualPathExists(cfg, "romm://404/Nope"))
}

func TestInstallSingleFile(t *testing.T) {
	t.Parallel()

	f := nesFixture(t)
	f.withToken(t, "secret")
	f.content["1/Super Game (USA).nes"] = []byte("rom")
	dataDir := t.TempDir()
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "NES"), 0o750))
	pl := mockPlatform(t, dataDir, []string{root})

	got, system, err := Install(context.Background(), f.config(), pl, nil, "romm://1/Super Game (USA)")
	require.NoError(t, err)
	assert.Equal(t, systemdefs.SystemNES, system)
	assert.Equal(t, filepath.Join(root, "NES", "Super Game (USA).nes"), got)
	data, err := os.ReadFile(got) //nolint:gosec // test path
	require.NoError(t, err)
	assert.Equal(t, "rom", string(data))
}

func TestInstallMultiDisc(t *testing.T) {
	t.Parallel()

	f := nesFixture(t)
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, name := range []string{"Disc Game (Disc 1).cue", "Disc Game (Disc 1).bin", "Disc Game (Disc 2).cue"} {
		w, err := zw.Create(name)
		require.NoError(t, err)
		_, err = w.Write([]byte(name))
		require.NoError(t, err)
	}
	require.NoError(t, zw.Close())
	f.content["7/Disc Game"] = buf.Bytes()
	dataDir := t.TempDir()
	pl := mockPlatform(t, dataDir, nil)

	got, system, err := Install(context.Background(), f.config(), pl, nil, "romm://7/Disc Game")
	require.NoError(t, err)
	assert.Equal(t, systemdefs.SystemPSX, system)
	// PSX here opens .cue and .chd but not .m3u, so the first disc is used.
	assert.Equal(t, filepath.Join(dataDir, config.MediaDir, systemdefs.SystemPSX, "Disc Game",
		"Disc Game (Disc 1).cue"), got)
}

func TestInstallRefusesUnavailableRom(t *testing.T) {
	t.Parallel()

	f := nesFixture(t)
	pl := mockPlatform(t, t.TempDir(), nil)
	_, _, err := Install(context.Background(), f.config(), pl, nil, "romm://4/Gone")
	require.Error(t, err)
	_, _, err = Install(context.Background(), f.config(), pl, nil, "romm://9999/Nope")
	require.ErrorIs(t, err, ErrNotFound)
	_, _, err = Install(context.Background(), &config.Instance{}, pl, nil, "romm://1/x")
	require.ErrorIs(t, err, ErrNotConfigured)
}
