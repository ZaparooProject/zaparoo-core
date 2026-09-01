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

package helpers

import (
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func foldersFor(t *testing.T, cache *LauncherCache, systemID string) []string {
	t.Helper()
	all := cache.GetAllLaunchers()
	for i := range all {
		if all[i].SystemID == systemID {
			return all[i].Folders
		}
	}
	t.Fatalf("no launcher for system %q", systemID)
	return nil
}

func TestSystemIDFolderAddedWhenUndeclared(t *testing.T) {
	t.Parallel()

	// RetroArch launchers declare only the EmulationStation folder, so a
	// library organised by Zaparoo system ID would otherwise never be scanned.
	cache := &LauncherCache{}
	cache.InitializeFromSlice([]platforms.Launcher{
		{ID: "ra-genesis", SystemID: "Genesis", Folders: []string{"megadrive"}},
	})

	assert.Equal(t, []string{"megadrive", "Genesis"}, foldersFor(t, cache, "Genesis"))
}

func TestSystemIDFolderVisibleToBothLookups(t *testing.T) {
	t.Parallel()

	// The folder has to reach GetLaunchersBySystem too: path discovery uses
	// that, while LauncherMatcher uses GetAllLaunchers. If they disagree a
	// path is discovered but no launcher claims the files in it.
	cache := &LauncherCache{}
	cache.InitializeFromSlice([]platforms.Launcher{
		{ID: "ra-genesis", SystemID: "Genesis", Folders: []string{"megadrive"}},
	})

	bySystem := cache.GetLaunchersBySystem("Genesis")
	require.Len(t, bySystem, 1)
	assert.Contains(t, bySystem[0].Folders, "Genesis")
	assert.Contains(t, foldersFor(t, cache, "Genesis"), "Genesis")
}

func TestSystemIDFolderNotDuplicatedWhenDeclared(t *testing.T) {
	t.Parallel()

	// MiSTer's launchers already hand-list the system ID.
	cache := &LauncherCache{}
	cache.InitializeFromSlice([]platforms.Launcher{
		{ID: "mister-genesis", SystemID: "Genesis", Folders: []string{"MegaDrive", "Genesis"}},
	})
	assert.Equal(t, []string{"MegaDrive", "Genesis"}, foldersFor(t, cache, "Genesis"))

	cache.InitializeFromSlice([]platforms.Launcher{
		{ID: "lower", SystemID: "Genesis", Folders: []string{"genesis"}},
	})
	assert.Equal(t, []string{"genesis"}, foldersFor(t, cache, "Genesis"), "match is case-insensitive")
}

func TestSystemIDFolderYieldsToAnotherSystem(t *testing.T) {
	t.Parallel()

	// Folders named msx1 and msx2 are declared by system MSX, so systems MSX1
	// and MSX2 must not claim them. This is the only real collision today.
	cache := &LauncherCache{}
	cache.InitializeFromSlice([]platforms.Launcher{
		{ID: "msx", SystemID: "MSX", Folders: []string{"msx1", "msx2"}},
		{ID: "msx1", SystemID: "MSX1", Folders: []string{"msx1alt"}},
		{ID: "msx2", SystemID: "MSX2", Folders: []string{"msx2alt"}},
		{ID: "gen", SystemID: "Genesis", Folders: []string{"megadrive"}},
	})

	assert.Equal(t, []string{"msx1alt"}, foldersFor(t, cache, "MSX1"), "MSX owns the msx1 folder")
	assert.Equal(t, []string{"msx2alt"}, foldersFor(t, cache, "MSX2"), "MSX owns the msx2 folder")
	assert.Contains(t, foldersFor(t, cache, "Genesis"), "Genesis", "unrelated systems are unaffected")
}

func TestSystemIDFolderSkippedForNonScanningLaunchers(t *testing.T) {
	t.Parallel()

	// A launcher that never reads the filesystem gains nothing from a folder,
	// and must not reserve that name against another system.
	cache := &LauncherCache{}
	cache.InitializeFromSlice([]platforms.Launcher{
		{ID: "kodi", SystemID: "MSX", Folders: []string{"msx1"}, SkipFilesystemScan: true},
		{ID: "msx1", SystemID: "MSX1", Folders: []string{"msx1alt"}},
	})

	// Its own declared folder is left alone, but it gains no system-ID folder.
	assert.Equal(t, []string{"msx1"}, foldersFor(t, cache, "MSX"))
	// And it reserved nothing, so MSX1 is free to use its own ID.
	assert.Equal(t, []string{"msx1alt", "MSX1"}, foldersFor(t, cache, "MSX1"))
}

func TestSystemIDFolderSkippedForLaunchersWithoutRelativeFolders(t *testing.T) {
	t.Parallel()

	// A launcher with no root-relative folder matches by other means, such as
	// a Test function or an absolute path. Handing it a folder would widen
	// what it claims rather than just renaming where it looks.
	cache := &LauncherCache{}
	cache.InitializeFromSlice([]platforms.Launcher{
		{ID: "byTest", SystemID: "PS2"},
		{ID: "absOnly", SystemID: "PS3", Folders: []string{"/opt/roms/ps3"}},
	})

	assert.Empty(t, foldersFor(t, cache, "PS2"))
	assert.Equal(t, []string{"/opt/roms/ps3"}, foldersFor(t, cache, "PS3"))
}

func TestSystemIDFolderDoesNotMutateCallerSlice(t *testing.T) {
	t.Parallel()

	// Callers reuse launcher definitions across cache rebuilds; appending in
	// place would grow their Folders slice on every refresh.
	input := []platforms.Launcher{
		{ID: "ra-genesis", SystemID: "Genesis", Folders: []string{"megadrive"}},
	}
	cache := &LauncherCache{}
	cache.InitializeFromSlice(input)
	cache.InitializeFromSlice(input)

	assert.Equal(t, []string{"megadrive"}, input[0].Folders, "caller's slice is untouched")
	assert.Equal(t, []string{"megadrive", "Genesis"}, foldersFor(t, cache, "Genesis"))
}

func TestSystemIDFolderIgnoresAbsoluteAndEmptyFolders(t *testing.T) {
	t.Parallel()

	cache := &LauncherCache{}
	cache.InitializeFromSlice([]platforms.Launcher{
		{ID: "abs", SystemID: "Other", Folders: []string{"/opt/roms/Genesis", ""}},
		{ID: "gen", SystemID: "Genesis", Folders: []string{"megadrive"}},
	})

	// An absolute path does not reserve the bare name for another system.
	assert.Contains(t, foldersFor(t, cache, "Genesis"), "Genesis")
}

func TestSystemIDFolderSkippedWithoutSystemID(t *testing.T) {
	t.Parallel()

	cache := &LauncherCache{}
	cache.InitializeFromSlice([]platforms.Launcher{
		{ID: "generic", Folders: []string{"media"}},
	})

	launchers := cache.GetAllLaunchers()
	require.Len(t, launchers, 1)
	assert.Equal(t, []string{"media"}, launchers[0].Folders)
}
