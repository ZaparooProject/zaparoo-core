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
package pinup

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeInstall lays out a PinUP System folder on fs with the given optional
// files present.
func writeInstall(t *testing.T, fs afero.Fs, dir string, withServer bool) {
	t.Helper()
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, menuExeName), []byte("exe"), 0o644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, databaseName), []byte("db"), 0o644))
	if withServer {
		require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, serverExeName), []byte("exe"), 0o644))
	}
}

func TestLocatorLocate(t *testing.T) {
	t.Parallel()

	t.Run("configured directory wins", func(t *testing.T) {
		t.Parallel()
		fs := afero.NewMemMapFs()
		cfgDir := filepath.FromSlash("/cfg/PinUPSystem")
		writeInstall(t, fs, cfgDir, true)
		writeInstall(t, fs, "/default/PinUPSystem", false)
		loc := Locator{FS: fs, Candidates: []string{"/default/PinUPSystem"}}

		inst, err := loc.Locate(cfgDir)
		require.NoError(t, err)
		assert.Equal(t, cfgDir, inst.Dir)
		assert.Equal(t, filepath.Join(cfgDir, databaseName), inst.DBPath)
		assert.Equal(t, filepath.Join(cfgDir, menuExeName), inst.MenuExe)
		assert.Equal(t, filepath.Join(cfgDir, serverExeName), inst.ServerExe)
		assert.Equal(t, filepath.Join(cfgDir, mediaDirName), inst.MediaDir)
	})

	t.Run("configured directory without popper is an error not a fallback", func(t *testing.T) {
		t.Parallel()
		fs := afero.NewMemMapFs()
		writeInstall(t, fs, "/default/PinUPSystem", false)
		loc := Locator{FS: fs, Candidates: []string{"/default/PinUPSystem"}}

		_, err := loc.Locate("/wrong")
		require.ErrorContains(t, err, "install_dir")
		require.ErrorContains(t, err, "/wrong")
	})

	t.Run("registry before candidates", func(t *testing.T) {
		t.Parallel()
		fs := afero.NewMemMapFs()
		writeInstall(t, fs, "/reg/PinUPSystem", false)
		writeInstall(t, fs, "/default/PinUPSystem", false)
		loc := Locator{
			FS:         fs,
			Registry:   func() (string, error) { return "/reg/PinUPSystem", nil },
			Candidates: []string{"/default/PinUPSystem"},
		}

		inst, err := loc.Locate("")
		require.NoError(t, err)
		assert.Equal(t, filepath.Clean("/reg/PinUPSystem"), inst.Dir)
		assert.Empty(t, inst.ServerExe)
	})

	t.Run("registry failure falls back to candidates", func(t *testing.T) {
		t.Parallel()
		fs := afero.NewMemMapFs()
		writeInstall(t, fs, "/default/PinUPSystem", false)
		loc := Locator{
			FS:         fs,
			Registry:   func() (string, error) { return "", errors.New("no key") },
			Candidates: []string{"/missing", "/default/PinUPSystem"},
		}

		inst, err := loc.Locate("")
		require.NoError(t, err)
		assert.Equal(t, filepath.Clean("/default/PinUPSystem"), inst.Dir)
	})

	t.Run("registry path without popper falls back to candidates", func(t *testing.T) {
		t.Parallel()
		fs := afero.NewMemMapFs()
		writeInstall(t, fs, "/default/PinUPSystem", false)
		loc := Locator{
			FS:         fs,
			Registry:   func() (string, error) { return "/stale", nil },
			Candidates: []string{"/default/PinUPSystem"},
		}

		inst, err := loc.Locate("")
		require.NoError(t, err)
		assert.Equal(t, filepath.Clean("/default/PinUPSystem"), inst.Dir)
	})

	t.Run("menu exe alone is not an install", func(t *testing.T) {
		t.Parallel()
		fs := afero.NewMemMapFs()
		half := filepath.FromSlash("/half")
		require.NoError(t, afero.WriteFile(fs, filepath.Join(half, menuExeName), []byte("exe"), 0o644))
		loc := Locator{FS: fs, Candidates: []string{half}}

		_, err := loc.Locate("")
		require.ErrorIs(t, err, ErrNotInstalled)
	})

	t.Run("nothing found", func(t *testing.T) {
		t.Parallel()
		loc := Locator{FS: afero.NewMemMapFs(), Candidates: DefaultInstallDirs()}

		_, err := loc.Locate("")
		require.ErrorIs(t, err, ErrNotInstalled)
	})
}

func TestDefaultInstallDirs(t *testing.T) {
	t.Parallel()

	dirs := DefaultInstallDirs()
	assert.Len(t, dirs, 12)
	assert.Equal(t, `C:\vPinball\PinUPSystem`, dirs[0])
	assert.Equal(t, `C:\PinUPSystem`, dirs[1])
	assert.Contains(t, dirs, `D:\vPinball\PinUPSystem`)
}

func TestParseLocalServer32(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		`"C:\vPinball\PinUPSystem\PinUpPlayer.exe" /automation`: `C:\vPinball\PinUPSystem\PinUpPlayer.exe`,
		`C:\PinUPSystem\PinUpPlayer.exe /Automation`:            `C:\PinUPSystem\PinUpPlayer.exe`,
		`C:\Program Files\PinUP\PinUpPlayer.EXE`:                `C:\Program Files\PinUP\PinUpPlayer.EXE`,
		`  "C:\x\PinUpPlayer.exe"  `:                            `C:\x\PinUpPlayer.exe`,
		`"C:\unterminated\PinUpPlayer.exe`:                      `C:\unterminated\PinUpPlayer.exe`,
		`no-extension-here`:                                     `no-extension-here`,
		``:                                                      ``,
	}
	for input, want := range tests {
		assert.Equal(t, want, ParseLocalServer32(input), "input %q", input)
	}
}

func TestInstallDirFromServerPath(t *testing.T) {
	t.Parallel()

	assert.Equal(t, `C:\vPinball\PinUPSystem`,
		InstallDirFromServerPath(`"C:\vPinball\PinUPSystem\PinUpPlayer.exe" /automation`))
	assert.Equal(t, `/opt/pinup`, InstallDirFromServerPath(`/opt/pinup/PinUpPlayer.exe`))
	assert.Empty(t, InstallDirFromServerPath(`PinUpPlayer.exe`))
	assert.Empty(t, InstallDirFromServerPath(``))
}
