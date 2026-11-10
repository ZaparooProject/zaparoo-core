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
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadCoreDBFirstManifestRootWins(t *testing.T) {
	t.Parallel()
	fs := newTestFs(t)
	writeFile(t, fs, filepath.Join(manifestRoots[0], "Nintendo", "Nintendo Game Boy Advance.json"),
		`{"catalogue": "Custom GBA", "default": "gpsp", "libretro": {"gpsp": {"core": "gpsp_libretro.so"}}}`)
	writeFile(t, fs, filepath.Join(manifestRoots[0], "Nintendo", "broken.json"), `{not json`)

	db := loadCoreDB(fs, "aarch64")

	assert.Equal(t, "Custom GBA", db.catalogue("Nintendo Game Boy Advance"))
	assert.Equal(t, "Sega Mega Drive - Genesis", db.catalogue("Sega Mega Drive - Genesis"))
	assert.NotContains(t, db.systems, "broken")
}

func TestDefaultCoreHonoursDefaultAndRequirements(t *testing.T) {
	t.Parallel()
	fs := newTestFs(t)

	db := loadCoreDB(fs, "aarch64")
	choice, ok := db.defaultCore("Nintendo Game Boy Advance")
	require.True(t, ok)
	assert.Equal(t, "mgba", choice.ID)
	assert.Equal(t, "mgba_libretro.so", choice.Core)
	assert.Equal(t, "mu-mgba", choice.tag(), "systems without a runtime use the muOS player")
	assert.Equal(t, "performance", choice.Governor)

	_, ok = db.defaultCore("Missing System")
	assert.False(t, ok)
}

func TestDefaultCoreFallsBackToFirstUsableCore(t *testing.T) {
	t.Parallel()
	fs := newTestFs(t)
	writeFile(t, fs, filepath.Join(manifestRoots[0], "Nintendo", "Nintendo Game Boy Advance.json"),
		`{"default": "gone", "libretro": {
			"armgba": {"core": "armgba_libretro.so", "require": {"arch": ["aarch64"]}},
			"gpsp": {"core": "gpsp_libretro.so"}}}`)

	choice, ok := loadCoreDB(fs, "armhf").defaultCore("Nintendo Game Boy Advance")
	require.True(t, ok)
	assert.Equal(t, "gpsp", choice.ID, "armgba requires aarch64")

	choice, ok = loadCoreDB(fs, "aarch64").defaultCore("Nintendo Game Boy Advance")
	require.True(t, ok)
	assert.Equal(t, "armgba", choice.ID, "manifest order decides the fallback")
}

func TestDefaultCoreSkipsExtraCoresUnlessShown(t *testing.T) {
	t.Parallel()
	fs := newTestFs(t)

	choice, ok := loadCoreDB(fs, "aarch64").defaultCore("Sega Mega Drive - Genesis")
	require.True(t, ok)
	assert.Equal(t, "genesis plus gx", choice.ID)

	writeFile(t, fs, extraCoresFile, "1\n")
	choice, ok = loadCoreDB(fs, "aarch64").defaultCore("Sega Mega Drive - Genesis")
	require.True(t, ok)
	assert.Equal(t, "picodrive", choice.ID)
}

func TestDefaultCoreDeviceRequirement(t *testing.T) {
	t.Parallel()
	fs := newTestFs(t)
	writeFile(t, fs, filepath.Join(manifestRoots[0], "Sony", "Sony PlayStation.json"),
		`{"libretro": {
			"big": {"core": "big_libretro.so", "require": {"device": ["rk-g350-v"]}},
			"small": {"core": "small_libretro.so"}}}`)

	choice, ok := loadCoreDB(fs, "aarch64").defaultCore("Sony PlayStation")
	require.True(t, ok)
	assert.Equal(t, "small", choice.ID, "no board name means device requirements fail")

	writeFile(t, fs, boardNameFile, "rk-g350-v\n")
	choice, ok = loadCoreDB(fs, "aarch64").defaultCore("Sony PlayStation")
	require.True(t, ok)
	assert.Equal(t, "big", choice.ID)
}

func TestDefaultCorePrefersSystemRuntime(t *testing.T) {
	t.Parallel()
	fs := newTestFs(t)
	writeFile(t, fs, filepath.Join(manifestRoots[0], "Sony", "Sony PlayStation.json"),
		`{"runtime": "external", "default": "duck",
		  "libretro": {"duck": {"core": "duckstation_libretro.so"}},
		  "external": {"duck": {"core": "duckstation"}}}`)

	choice, ok := loadCoreDB(fs, "aarch64").defaultCore("Sony PlayStation")
	require.True(t, ok)
	assert.Equal(t, runtimeExternal, choice.Runtime)
	assert.Equal(t, "duckstation", choice.Core)
	assert.Equal(t, "ext-duck", choice.tag())
}

func TestRuntimeInOrder(t *testing.T) {
	t.Parallel()
	order := func(preferred int) []int {
		out := make([]int, 0, runtimeCount)
		for step := range runtimeCount {
			out = append(out, runtimeInOrder(preferred, step))
		}
		return out
	}
	assert.Equal(t, []int{runtimePickles, runtimeRetroArch, runtimeExternal}, order(runtimePickles))
	assert.Equal(t, []int{runtimeRetroArch, runtimePickles, runtimeExternal}, order(runtimeRetroArch))
	assert.Equal(t, []int{runtimeExternal, runtimePickles, runtimeRetroArch}, order(runtimeExternal))
}

func TestSystemForFolderAssignIndexBeatsManifests(t *testing.T) {
	t.Parallel()
	fs := newTestFs(t)

	db := loadCoreDB(fs, "aarch64")
	system, ok := db.systemForFolder("Game Boy-Advance")
	require.True(t, ok)
	assert.Equal(t, "Nintendo Game Boy Advance", system)
	_, ok = db.systemForFolder("Unknown Folder")
	assert.False(t, ok)
	_, ok = db.systemForFolder(" -_+")
	assert.False(t, ok)

	writeFile(t, fs, assignFile, `{"gameboyadvance": "Sega Mega Drive - Genesis"}`)
	system, ok = loadCoreDB(fs, "aarch64").systemForFolder("GameBoyAdvance")
	require.True(t, ok)
	assert.Equal(t, "Sega Mega Drive - Genesis", system)
}

func TestFlexIntAcceptsNumbersAndStrings(t *testing.T) {
	t.Parallel()
	var v struct {
		A flexInt `json:"a"`
		B flexInt `json:"b"`
		C flexInt `json:"c"`
		D flexInt `json:"d"`
	}
	require.NoError(t, json.Unmarshal([]byte(`{"a": 2, "b": "3", "c": "x", "d": null}`), &v))
	assert.Equal(t, flexInt(2), v.A)
	assert.Equal(t, flexInt(3), v.B)
	assert.Equal(t, flexInt(0), v.C)
	assert.Equal(t, flexInt(0), v.D)
	assert.Equal(t, 1, loadCoreDB(newTestFs(t), "aarch64").lookup("Sega Mega Drive - Genesis"))
}

func TestOrderedCoresRejectsNonObject(t *testing.T) {
	t.Parallel()
	var cores orderedCores
	require.Error(t, json.Unmarshal([]byte(`[]`), &cores))
}

func TestCPUProfileValue(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "default", cpuProfileValue(""))
	assert.Equal(t, "default", cpuProfileValue("Default"))
	assert.Equal(t, "extreme", cpuProfileValue("performance"))
	assert.Equal(t, "balanced", cpuProfileValue(" Balanced "))
	assert.Equal(t, "automatic", cpuProfileValue("something-else"))
}

func TestFactInListAliases(t *testing.T) {
	t.Parallel()
	assert.True(t, factInList("x86_64", []string{"x64"}))
	assert.True(t, factInList("armhf", []string{"ARM32"}))
	assert.False(t, factInList("", []string{""}))
	assert.False(t, factInList("aarch64", []string{"armhf"}))
}

// coreJSONFs returns a file system laid out like muOS 2606, which ships a
// single core.json instead of the per-system manifest tree.
func coreJSONFs(t *testing.T) afero.Fs {
	t.Helper()
	fs := afero.NewMemMapFs()
	writeFile(t, fs, coreJSONFiles[1], `{
  "Nintendo Game Boy Advance": `+gbaManifest+`,
  "Sega Mega Drive - Genesis": `+mdManifest+`
}`)
	require.NoError(t, fs.MkdirAll(testProc, 0o755))
	require.NoError(t, fs.MkdirAll(runDir, 0o755))
	require.NoError(t, fs.MkdirAll(testROMs, 0o755))
	return fs
}

func TestLoadCoreDBReadsBundledCoreJSON(t *testing.T) {
	t.Parallel()
	fs := coreJSONFs(t)

	db := loadCoreDB(fs, "aarch64")

	choice, ok := db.defaultCore("Nintendo Game Boy Advance")
	require.True(t, ok)
	assert.Equal(t, "mu-mgba", choice.tag())
	assert.Equal(t, 1, db.lookup("Sega Mega Drive - Genesis"))
	choice, ok = db.defaultCore("Sega Mega Drive - Genesis")
	require.True(t, ok)
	assert.Equal(t, "genesis plus gx", choice.ID, "extra cores are skipped in file order")
	system, ok := db.systemForFolder("Game Boy Advance")
	require.True(t, ok)
	assert.Equal(t, "Nintendo Game Boy Advance", system)
}

func TestLoadCoreDBUserCoreJSONReplacesBundled(t *testing.T) {
	t.Parallel()
	fs := coreJSONFs(t)
	writeFile(t, fs, coreJSONFiles[0], `{"Nintendo Game Boy Advance": {
		"catalogue": "User GBA", "default": "gpsp", "libretro": {"gpsp": {"core": "gpsp_libretro.so"}}}}`)

	db := loadCoreDB(fs, "aarch64")

	assert.Equal(t, "User GBA", db.catalogue("Nintendo Game Boy Advance"))
	choice, ok := db.defaultCore("Nintendo Game Boy Advance")
	require.True(t, ok)
	assert.Equal(t, "gpsp", choice.ID)
	assert.NotContains(t, db.systems, "Sega Mega Drive - Genesis", "the user file is not merged with the bundled one")
}

func TestLoadCoreDBSkipsUnusableUserCoreJSON(t *testing.T) {
	t.Parallel()
	for _, content := range []string{`{not json`, `null`, `["Nintendo Game Boy Advance"]`} {
		fs := coreJSONFs(t)
		writeFile(t, fs, coreJSONFiles[0], content)

		db := loadCoreDB(fs, "aarch64")

		assert.Contains(t, db.systems, "Sega Mega Drive - Genesis", content)
	}
}

func TestLoadCoreDBPrefersManifestTreeOverCoreJSON(t *testing.T) {
	t.Parallel()
	fs := newTestFs(t)
	writeFile(t, fs, coreJSONFiles[1], `{"Sony PlayStation": {"libretro": {"pcsx": {"core": "pcsx_libretro.so"}}}}`)

	db := loadCoreDB(fs, "aarch64")

	assert.Contains(t, db.systems, "Nintendo Game Boy Advance")
	assert.NotContains(t, db.systems, "Sony PlayStation")
}

func TestResolveContentWithCoreJSON(t *testing.T) {
	t.Parallel()
	fs := coreJSONFs(t)
	rom := filepath.Join(testROMs, "GBA", "Metroid Fusion.gba")
	writeFile(t, fs, rom, "rom")

	req, err := resolveContent(fs, loadCoreDB(fs, "aarch64"), rom)
	require.NoError(t, err)

	assert.Equal(t, "Metroid Fusion\nmgba_libretro.so\nNintendo Game Boy Advance\n"+
		"Nintendo Game Boy Advance\n0\nmu-mgba\n/mnt/mmc/ROMS/\nGBA\nMetroid Fusion.gba\n",
		req.contentLines())
}
