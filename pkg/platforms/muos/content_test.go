//go:build linux && !android

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

package muos

import (
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveContentAutoAssignsByFolder(t *testing.T) {
	t.Parallel()
	fs := newTestFs(t)
	rom := filepath.Join(testROMs, "GBA", "Metroid Fusion.gba")
	writeFile(t, fs, rom, "rom")

	req, err := resolveContent(fs, loadCoreDB(fs, "aarch64"), rom)
	require.NoError(t, err)

	assert.Equal(t, "Metroid Fusion\nmgba_libretro.so\nNintendo Game Boy Advance\n"+
		"Nintendo Game Boy Advance\n0\nmu-mgba\n/mnt/mmc/ROMS/\nGBA\nMetroid Fusion.gba\n",
		req.contentLines())
	assert.Equal(t, map[string]string{
		"gov": "extreme", "con": "gba", "rac": "false", "flt": "none", "shd": "none", "ovl": "none",
	}, req.assets)
}

func TestResolveContentPrefersPerContentConfig(t *testing.T) {
	t.Parallel()
	fs := newTestFs(t)
	rom := filepath.Join(testROMs, "GBA", "Game.gba")
	writeFile(t, fs, rom, "rom")
	info := filepath.Join(infoContentDir, "GBA")
	writeFile(t, fs, filepath.Join(info, "Game.cfg"),
		"Game\ngpsp_libretro.so\nNintendo Game Boy Advance\nCat\n0\nmu-gpsp\n/mnt/mmc/ROMS/\nGBA\nGame.gba\n")
	writeFile(t, fs, filepath.Join(info, "core.cfg"),
		"picodrive_libretro.so\nSega Mega Drive - Genesis\nSega Mega Drive - Genesis\n1\npicodrive\n")
	writeFile(t, fs, filepath.Join(info, "Game.gov"), "powersave")
	writeFile(t, fs, filepath.Join(info, "core.gov"), "turbo")
	writeFile(t, fs, filepath.Join(info, "core.shd"), "crt")

	req, err := resolveContent(fs, loadCoreDB(fs, "aarch64"), rom)
	require.NoError(t, err)

	assert.Equal(t, "gpsp_libretro.so", req.core)
	assert.Equal(t, "Cat", req.catalogue)
	assert.Equal(t, "mu-gpsp", req.tag)
	assert.Equal(t, "powersave", req.assets["gov"], "per-content setting wins")
	assert.Equal(t, "crt", req.assets["shd"], "folder setting applies")
	assert.Equal(t, "system", req.assets["con"], "assigned content does not use manifest defaults")
}

func TestResolveContentFolderConfigAndIncompleteContentConfig(t *testing.T) {
	t.Parallel()
	fs := newTestFs(t)
	rom := filepath.Join(testROMs, "GBA", "Game.gba")
	writeFile(t, fs, rom, "rom")
	info := filepath.Join(infoContentDir, "GBA")
	writeFile(t, fs, filepath.Join(info, "Game.cfg"), "Game\n\n\n")
	writeFile(t, fs, filepath.Join(info, "core.cfg"),
		"picodrive_libretro.so\nSega Mega Drive - Genesis\n\n\npicodrive\n")

	req, err := resolveContent(fs, loadCoreDB(fs, "aarch64"), rom)
	require.NoError(t, err)

	assert.Equal(t, "picodrive_libretro.so", req.core)
	assert.Equal(t, "Sega Mega Drive - Genesis", req.system)
	assert.Equal(t, "Sega Mega Drive - Genesis", req.catalogue, "empty catalogue uses the system")
	assert.Equal(t, "0", req.lookup)
	assert.Equal(t, "default", req.assets["gov"])
}

func TestResolveContentRecursiveConfig(t *testing.T) {
	t.Parallel()
	fs := newTestFs(t)
	rom := filepath.Join(testROMs, "Collections", "Best", "Sonic.md")
	writeFile(t, fs, rom, "rom")
	writeFile(t, fs, filepath.Join(infoContentDir, "Collections", "core_recursive.cfg"),
		"genesis_plus_gx_libretro.so\nSega Mega Drive - Genesis\nSega Mega Drive - Genesis\n1\ngenesis plus gx\n")

	req, err := resolveContent(fs, loadCoreDB(fs, "aarch64"), rom)
	require.NoError(t, err)

	assert.Equal(t, "genesis_plus_gx_libretro.so", req.core)
	assert.Equal(t, "Collections/Best", req.relDir)
	assert.Equal(t, "1", req.lookup)
}

func TestResolveContentFallsBackToTopFolder(t *testing.T) {
	t.Parallel()
	fs := newTestFs(t)
	rom := filepath.Join(testROMs, "MD", "Hacks", "Sonic.md")
	writeFile(t, fs, rom, "rom")

	req, err := resolveContent(fs, loadCoreDB(fs, "aarch64"), rom)
	require.NoError(t, err)

	assert.Equal(t, "Sega Mega Drive - Genesis", req.system)
	assert.Equal(t, "mu-genesis plus gx", req.tag)
	assert.Equal(t, "MD/Hacks", req.relDir)
}

func TestResolveContentNestedAssignBeatsTopFolder(t *testing.T) {
	t.Parallel()
	fs := newTestFs(t)
	rom := filepath.Join(testROMs, "MD", "GBA", "Game.gba")
	writeFile(t, fs, rom, "rom")

	req, err := resolveContent(fs, loadCoreDB(fs, "aarch64"), rom)
	require.NoError(t, err)
	assert.Equal(t, "Nintendo Game Boy Advance", req.system)
}

func TestResolveContentRootAssets(t *testing.T) {
	t.Parallel()
	fs := newTestFs(t)
	rom := filepath.Join(testROMs, "game.gba")
	writeFile(t, fs, rom, "rom")
	writeFile(t, fs, filepath.Join(infoContentDir, "game.cfg"),
		"game\ngpsp_libretro.so\nNintendo Game Boy Advance\nNintendo Game Boy Advance\n0\nmu-gpsp\n")
	writeFile(t, fs, filepath.Join(infoContentDir, "game.flt"), "ignored")
	writeFile(t, fs, filepath.Join(infoContentDir, "core.flt"), "warm")

	req, err := resolveContent(fs, loadCoreDB(fs, "aarch64"), rom)
	require.NoError(t, err)
	assert.Empty(t, req.relDir)
	assert.Equal(t, "warm", req.assets["flt"])
}

func TestResolveContentErrors(t *testing.T) {
	t.Parallel()
	fs := newTestFs(t)
	writeFile(t, fs, filepath.Join(testROMs, "Unknown", "a.bin"), "rom")
	writeFile(t, fs, "/mnt/mmc/other/a.gba", "rom")
	writeFile(t, fs, testROMs+"/GBA/bad\nname.gba", "rom")
	require.NoError(t, fs.MkdirAll(filepath.Join(testROMs, "GBA", "dir.gba"), 0o755))
	db := loadCoreDB(fs, "aarch64")

	tests := map[string]string{
		"no core":      filepath.Join(testROMs, "Unknown", "a.bin"),
		"outside roms": "/mnt/mmc/other/a.gba",
		"line break":   testROMs + "/GBA/bad\nname.gba",
		"directory":    filepath.Join(testROMs, "GBA", "dir.gba"),
		"missing":      filepath.Join(testROMs, "GBA", "missing.gba"),
		"relative":     "ROMS/GBA/a.gba",
		"roms itself":  testROMs,
	}
	for name, path := range tests {
		_, err := resolveContent(fs, db, path)
		require.Error(t, err, name)
	}

	_, err := resolveContent(fs, db, filepath.Join(testROMs, "Unknown", "a.bin"))
	require.ErrorIs(t, err, errNoCore)
}

func TestResolveContentTrimsCarriageReturns(t *testing.T) {
	t.Parallel()
	fs := newTestFs(t)
	rom := filepath.Join(testROMs, "GBA", "Game.gba")
	writeFile(t, fs, rom, "rom")
	writeFile(t, fs, filepath.Join(infoContentDir, "GBA", "core.cfg"),
		"core.so\nSys\nCat\r\n0\ntag\n")

	req, err := resolveContent(fs, loadCoreDB(fs, "aarch64"), rom)
	require.NoError(t, err, "trailing carriage returns are trimmed")
	assert.Equal(t, "Cat", req.catalogue)
}

func TestResolveContentUsesConfiguredMounts(t *testing.T) {
	t.Parallel()
	fs := newTestFs(t)
	writeFile(t, fs, sdcardMountFile, "/media/card2\n")
	rom := "/media/card2/ROMS/GBA/Game.gba"
	writeFile(t, fs, rom, "rom")

	req, err := resolveContent(fs, loadCoreDB(fs, "aarch64"), rom)
	require.NoError(t, err)
	assert.Equal(t, "/media/card2/ROMS/", req.romsDir)

	assert.Equal(t, []string{defaultUSBMount, "/media/card2", defaultROMMount}, mountPoints(fs))
	writeFile(t, fs, usbMountFile, "relative/path\n")
	assert.Equal(t, defaultUSBMount, mountPoints(fs)[0], "relative mounts are ignored")
}

func TestWriteContentFileIsAtomicNineLines(t *testing.T) {
	t.Parallel()
	fs := newTestFs(t)
	req := &contentRequest{
		name: "Game", romsDir: "/mnt/mmc/ROMS/", relDir: "GBA", file: "Game.gba",
		coreAssignment: coreAssignment{core: "c.so", system: "Sys", catalogue: "Cat", lookup: "0", tag: "mu-c"},
	}

	require.NoError(t, writeContentFile(fs, req))

	assert.Equal(t, "Game\nc.so\nSys\nCat\n0\nmu-c\n/mnt/mmc/ROMS/\nGBA\nGame.gba\n", readFile(t, fs, contentFile))
	exists, err := afero.Exists(fs, contentFile+".tmp")
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestWriteAssets(t *testing.T) {
	t.Parallel()
	fs := newTestFs(t)
	req := &contentRequest{assets: map[string]string{
		"gov": "extreme", "con": "gba", "rac": "false", "flt": "none", "shd": "none", "ovl": "none",
	}}
	require.NoError(t, writeAssets(fs, req))
	assert.Equal(t, "extreme", readFile(t, fs, "/run/muos/governor"))
	assert.Equal(t, "gba", readFile(t, fs, "/run/muos/controller"))
	assert.Equal(t, "none", readFile(t, fs, "/run/muos/overlay_options"))
}

func TestContentName(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "Game (USA)", contentName("/a/Game (USA).zip"))
	assert.Equal(t, "Game.v1", contentName("/a/Game.v1.zip"))
	assert.Equal(t, "Game", contentName("/a/Game"))
	assert.Equal(t, ".hidden", contentName("/a/.hidden"))
}

func TestReadOverlayAndLastPlayed(t *testing.T) {
	t.Parallel()
	fs := newTestFs(t)
	_, ok := readOverlay(fs)
	assert.False(t, ok)

	writeFile(t, fs, overlayFile, "Game\nNintendo Game Boy Advance\nmgba_libretro.so")
	state, ok := readOverlay(fs)
	require.True(t, ok)
	assert.Equal(t, overlayState{name: "Game", system: "Nintendo Game Boy Advance", core: "mgba_libretro.so"}, state)

	cfg := filepath.Join(infoContentDir, "GBA", "Game.cfg")
	writeFile(t, fs, cfg, "Game\nc\ns\nc\n0\nt\n/mnt/mmc/ROMS/\nGBA\nGame.gba\n")
	writeFile(t, fs, lastPlayFile, cfg+"\n")
	path, ok := lastPlayedPath(fs, "Game")
	require.True(t, ok)
	assert.Equal(t, "/mnt/mmc/ROMS/GBA/Game.gba", path)

	_, ok = lastPlayedPath(fs, "Other")
	assert.False(t, ok, "a different game is running")

	writeFile(t, fs, cfg, "Game\nc\ns\nc\n0\nt\n/mnt/mmc/ROMS/\n\nGame.gba\n")
	path, ok = lastPlayedPath(fs, "Game")
	require.True(t, ok)
	assert.Equal(t, "/mnt/mmc/ROMS/Game.gba", path)
}
