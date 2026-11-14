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

package standalone

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	testRoot  = fixtureRoot()
	testHome  = filepath.Join(testRoot, "Users", "player")
	testLocal = filepath.Join(testHome, "AppData", "Local")
	testDrive = filepath.Join(testRoot, "drive-c")
)

// fixtureRoot returns an absolute root for fixture paths. On Windows a path
// needs a drive to be absolute.
func fixtureRoot() string {
	if runtime.GOOS == "windows" {
		return `C:\`
	}
	return string(filepath.Separator)
}

func testEmulator() WindowsEmulator {
	return WindowsEmulator{
		Name:     "Dolphin",
		Exes:     []string{"dolphin.exe", "dolphin-emu*"},
		DirHints: []string{"dolphin-"},
		Scoop:    []string{"dolphin"},
		Systems:  []WindowsSystem{{ID: "DolphinGameCube"}, {ID: "DolphinWii"}},
	}
}

func testEnv(fs afero.Fs, vars map[string]string) *WindowsEnv {
	base := map[string]string{
		"USERPROFILE":  testHome,
		"LOCALAPPDATA": testLocal,
		"APPDATA":      filepath.Join(testHome, "AppData", "Roaming"),
		"ProgramFiles": filepath.Join(testDrive, "Program Files"),
		"SystemRoot":   filepath.Join(testDrive, "Windows"),
	}
	for key, value := range vars {
		base[key] = value
	}
	return &WindowsEnv{
		FS:          fs,
		Getenv:      func(key string) string { return base[key] },
		FixedDrives: []string{testDrive},
	}
}

func touch(t *testing.T, fs afero.Fs, path string) string {
	t.Helper()
	require.NoError(t, fs.MkdirAll(filepath.Dir(path), 0o750))
	require.NoError(t, afero.WriteFile(fs, path, []byte("MZ"), 0o600))
	return path
}

func discoverOne(env *WindowsEnv) (WindowsInstall, bool) {
	installs := DiscoverWindows(env, []WindowsEmulator{testEmulator()})
	install, ok := installs["Dolphin"]
	return install, ok
}

func TestDiscoverWindowsNothingInstalled(t *testing.T) {
	t.Parallel()

	_, ok := discoverOne(testEnv(afero.NewMemMapFs(), nil))
	assert.False(t, ok)
	assert.Empty(t, DiscoverWindows(&WindowsEnv{}, []WindowsEmulator{testEmulator()}))
}

func TestDiscoverWindowsConfiguredInstallDir(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	dir := filepath.Join(testRoot, "Portable", "Dolph")
	exe := touch(t, fs, filepath.Join(dir, "Dolphin.exe"))
	// A search hit elsewhere is ignored when install_dir is set.
	touch(t, fs, filepath.Join(testDrive, "Emulators", "Dolphin", "Dolphin.exe"))

	env := testEnv(fs, nil)
	env.Configured = func(id string) string {
		if id == "DolphinWii" {
			return dir
		}
		return ""
	}
	install, ok := discoverOne(env)
	require.True(t, ok)
	assert.Equal(t, WindowsInstall{Exe: exe, Dir: dir, Source: "install_dir"}, install)

	// A full path to the executable works too, keyed by emulator name.
	env.Configured = func(id string) string {
		if id == "Dolphin" {
			return exe
		}
		return ""
	}
	install, ok = discoverOne(env)
	require.True(t, ok)
	assert.Equal(t, "install_dir", install.Source)
	assert.Equal(t, exe, install.Exe)
}

func TestDiscoverWindowsAppPaths(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	exe := touch(t, fs, filepath.Join(testDrive, "Tools", "dolphin.exe"))
	env := testEnv(fs, nil)
	env.AppPath = func(name string) string {
		if name == "dolphin.exe" {
			return exe
		}
		return ""
	}
	install, ok := discoverOne(env)
	require.True(t, ok)
	assert.Equal(t, "app paths", install.Source)
	assert.Equal(t, exe, install.Exe)
}

func TestDiscoverWindowsUninstallEntries(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	iconExe := touch(t, fs, filepath.Join(testDrive, "Icon", "Dolphin.exe"))
	env := testEnv(fs, nil)
	env.UninstallEntries = []UninstallEntry{
		{DisplayName: "Something else", InstallLocation: filepath.Join(testDrive, "Other")},
		{DisplayName: "Dolphin Emulator", DisplayIcon: `"` + iconExe + `",0`},
	}
	install, ok := discoverOne(env)
	require.True(t, ok)
	assert.Equal(t, WindowsInstall{Exe: iconExe, Dir: filepath.Dir(iconExe), Source: "uninstall"}, install)

	fs = afero.NewMemMapFs()
	lowerIcon := touch(t, fs, filepath.Join(testDrive, "Icon", "dolphin-emu-32.exe"))
	preferred := touch(t, fs, filepath.Join(testDrive, "Icon", "Dolphin.exe"))
	env = testEnv(fs, nil)
	env.UninstallEntries = []UninstallEntry{{DisplayName: "Dolphin", DisplayIcon: lowerIcon}}
	install, ok = discoverOne(env)
	require.True(t, ok)
	assert.Equal(t, preferred, install.Exe, "the icon's folder is searched in the emulator's exe order")

	fs = afero.NewMemMapFs()
	location := filepath.Join(testDrive, "Apps", "dolphin-2509")
	exe := touch(t, fs, filepath.Join(location, "Dolphin.exe"))
	env = testEnv(fs, nil)
	env.UninstallEntries = []UninstallEntry{{DisplayName: "DE", InstallLocation: `"` + location + `"`}}
	install, ok = discoverOne(env)
	require.True(t, ok)
	assert.Equal(t, exe, install.Exe, "a location folder named like the emulator matches")
}

func TestDiscoverWindowsScoop(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	exe := touch(t, fs, filepath.Join(testHome, "scoop", "apps", "dolphin", "current", "Dolphin.exe"))
	install, ok := discoverOne(testEnv(fs, nil))
	require.True(t, ok)
	assert.Equal(t, WindowsInstall{Exe: exe, Dir: filepath.Dir(exe), Source: "scoop"}, install)

	fs = afero.NewMemMapFs()
	global := filepath.Join(testDrive, "ProgramData", "scoop")
	exe = touch(t, fs, filepath.Join(global, "apps", "dolphin", "current", "Dolphin.exe"))
	install, ok = discoverOne(testEnv(fs, map[string]string{"ProgramData": filepath.Dir(global)}))
	require.True(t, ok)
	assert.Equal(t, exe, install.Exe)
}

func TestDiscoverWindowsWinget(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	exe := touch(t, fs, filepath.Join(testLocal, "Microsoft", "WinGet", "Packages",
		"DolphinEmulator.Dolphin_Microsoft.Winget.Source_8wekyb3d8bbwe", "Dolphin-x64", "Dolphin.exe"))
	install, ok := discoverOne(testEnv(fs, nil))
	require.True(t, ok)
	assert.Equal(t, WindowsInstall{Exe: exe, Dir: filepath.Dir(exe), Source: "winget"}, install)

	fs = afero.NewMemMapFs()
	exe = touch(t, fs, filepath.Join(testLocal, "Microsoft", "WinGet", "Links", "dolphin.exe"))
	install, ok = discoverOne(testEnv(fs, nil))
	require.True(t, ok)
	assert.Equal(t, exe, install.Exe)
}

func TestDiscoverWindowsPath(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	system := filepath.Join(testDrive, "Windows", "System32")
	touch(t, fs, filepath.Join(system, "dolphin.exe"))
	bin := filepath.Join(testDrive, "bin")
	exe := touch(t, fs, filepath.Join(bin, "dolphin.exe"))
	path := strings.Join([]string{"relative", `"` + system + `"`, "", bin}, string(os.PathListSeparator))

	install, ok := discoverOne(testEnv(fs, map[string]string{"PATH": path}))
	require.True(t, ok)
	assert.Equal(t, WindowsInstall{Exe: exe, Dir: bin, Source: "path"}, install,
		"the Windows folder and relative entries are skipped")
}

func TestDiscoverWindowsCommonFolders(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"drive emulators":  filepath.Join(testDrive, "Emulators", "Dolphin", "Dolphin.exe"),
		"versioned folder": filepath.Join(testDrive, "Emulators", "dolphin-2509-x64", "Dolphin-x64", "Dolphin.exe"),
		"program files":    filepath.Join(testDrive, "Program Files", "Dolphin", "Dolphin.exe"),
		"home emulators":   filepath.Join(testHome, "Emulators", "Dolphin", "Dolphin.exe"),
		"emudeck": filepath.Join(testHome, "AppData", "Roaming", "EmuDeck", "Emulators",
			"Dolphin", "Dolphin.exe"),
		"downloads":            filepath.Join(testHome, "Downloads", "Dolphin-x64", "Dolphin.exe"),
		"drive root":           filepath.Join(testDrive, "dolphin", "Dolphin.exe"),
		"next to roms":         filepath.Join(testRoot, "Games", "Emulators", "Dolphin", "Dolphin.exe"),
		"prefix executable":    filepath.Join(testDrive, "Emulators", "Dolphin", "Dolphin-emu-nogui.exe"),
		"local programs":       filepath.Join(testLocal, "Programs", "Dolphin", "Dolphin.exe"),
		"es-de emulators":      filepath.Join(testDrive, "ES-DE", "Emulators", "Dolphin", "Dolphin.exe"),
		"emulation emulators":  filepath.Join(testDrive, "Emulation", "Emulators", "dolphin", "Dolphin.exe"),
		"local appdata direct": filepath.Join(testLocal, "Dolphin", "Dolphin.exe"),
		"bin folder":           filepath.Join(testDrive, "Emulators", "Dolphin", "bin", "Dolphin.exe"),
		"nested bin folder": filepath.Join(testDrive, "Emulators", "Dolphin", "GTK3Dolphin-3.10-win64",
			"bin", "Dolphin.exe"),
	}
	for name, exe := range cases {
		fs := afero.NewMemMapFs()
		touch(t, fs, exe)
		env := testEnv(fs, nil)
		env.IndexRoots = []string{filepath.Join(testRoot, "Games", "roms"), "relative"}
		install, ok := discoverOne(env)
		require.True(t, ok, name)
		assert.Equal(t, WindowsInstall{Exe: exe, Dir: filepath.Dir(exe), Source: "search"}, install, name)
	}
}

func TestDiscoverWindowsSearchIsBounded(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	// Too deep, in an unrelated folder, and a folder rather than a file.
	touch(t, fs, filepath.Join(testDrive, "Emulators", "Dolphin", "a", "b", "Dolphin.exe"))
	touch(t, fs, filepath.Join(testDrive, "Emulators", "Other", "Dolphin.exe"))
	require.NoError(t, fs.MkdirAll(filepath.Join(testDrive, "Emulators", "dolphin-x", "Dolphin.exe"), 0o750))
	_, ok := discoverOne(testEnv(fs, nil))
	assert.False(t, ok)
}

func TestDiscoverWindowsPrefersExeOrder(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	dir := filepath.Join(testDrive, "Emulators", "Dolphin")
	touch(t, fs, filepath.Join(dir, "Dolphin-emu-nogui.exe"))
	exe := touch(t, fs, filepath.Join(dir, "Dolphin.exe"))
	install, ok := discoverOne(testEnv(fs, nil))
	require.True(t, ok)
	assert.Equal(t, exe, install.Exe)
}

func TestIconPath(t *testing.T) {
	t.Parallel()

	assert.Equal(t, `C:\A B\app.exe`, iconPath(`"C:\A B\app.exe",0`))
	assert.Equal(t, `C:\A\app.exe`, iconPath(`C:\A\app.exe,1`))
	assert.Empty(t, iconPath(`C:\A\app.ico`))
	assert.Empty(t, iconPath(`"C:\A\app.exe`))
}
