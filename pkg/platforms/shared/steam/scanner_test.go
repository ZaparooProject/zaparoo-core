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

package steam

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/virtualpath"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/fixtures"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errTestFSAccess = errors.New("test filesystem access failure")

type failOpenFS struct {
	afero.Fs
	failPaths map[string]error
}

func (f failOpenFS) Open(name string) (afero.File, error) {
	if err := f.failPaths[filepath.Clean(name)]; err != nil {
		return nil, err
	}
	file, err := f.Fs.Open(name)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", name, err)
	}
	return file, nil
}

// vdfEscapePath escapes backslashes in paths for VDF files.
// VDF format requires backslashes to be escaped as double backslashes.
func vdfEscapePath(path string) string {
	return strings.ReplaceAll(path, `\`, `\\`)
}

func shortcutVirtualPath(appID uint32, appName string) string {
	bpid := (uint64(appID) << 32) | 0x02000000
	return virtualpath.CreateVirtualPath("steam", strconv.FormatUint(bpid, 10), appName)
}

func writeMockManifestFS(t *testing.T, fs afero.Fs, steamAppsDir string, appID int, name string) string {
	t.Helper()
	appIDString := strconv.Itoa(appID)
	path := filepath.Join(steamAppsDir, "appmanifest_"+appIDString+".acf")
	content := `"AppState"
{
	"appid"		"` + appIDString + `"
	"name"		"` + name + `"
}`
	require.NoError(t, afero.WriteFile(fs, path, []byte(content), 0o600))
	return path
}

func TestScanSteamApps(t *testing.T) {
	t.Parallel()

	t.Run("returns_empty_when_libraryfolders_not_found", func(t *testing.T) {
		t.Parallel()

		tempDir := t.TempDir()
		results, err := ScanSteamApps(tempDir)

		require.NoError(t, err)
		assert.Empty(t, results)
	})

	t.Run("returns_empty_when_libraryfolders_invalid", func(t *testing.T) {
		t.Parallel()

		tempDir := t.TempDir()
		err := os.WriteFile(filepath.Join(tempDir, "libraryfolders.vdf"), []byte("not valid vdf"), 0o600)
		require.NoError(t, err)

		results, scanErr := ScanSteamApps(tempDir)

		require.NoError(t, scanErr)
		assert.Empty(t, results)
	})

	t.Run("returns_empty_when_libraryfolders_missing_key", func(t *testing.T) {
		t.Parallel()

		tempDir := t.TempDir()
		vdfContent := `"notlibraryfolders"
{
}`
		err := os.WriteFile(filepath.Join(tempDir, "libraryfolders.vdf"), []byte(vdfContent), 0o600)
		require.NoError(t, err)

		results, scanErr := ScanSteamApps(tempDir)

		require.NoError(t, scanErr)
		assert.Empty(t, results)
	})

	t.Run("scans_valid_library_with_apps", func(t *testing.T) {
		t.Parallel()

		tempDir := t.TempDir()

		// Create library structure
		steamAppsDir := filepath.Join(tempDir, "steamapps")
		require.NoError(t, os.MkdirAll(steamAppsDir, 0o750))

		// Create libraryfolders.vdf pointing to our temp dir
		vdfContent := `"libraryfolders"
{
	"0"
	{
		"path"		"` + vdfEscapePath(tempDir) + `"
		"label"		""
		"contentid"		"123456"
		"totalsize"		"0"
		"update_clean_bytes_tally"		"0"
		"time_last_update_corruption"		"0"
		"apps"
		{
			"730"		"123456"
		}
	}
}`
		err := os.WriteFile(filepath.Join(tempDir, "steamapps", "libraryfolders.vdf"), []byte(vdfContent), 0o600)
		require.NoError(t, err)

		// Create an app manifest
		manifestContent := `"AppState"
{
	"appid"		"730"
	"Universe"		"1"
	"name"		"Counter-Strike 2"
	"StateFlags"		"4"
	"installdir"		"Counter-Strike Global Offensive"
}`
		err = os.WriteFile(filepath.Join(steamAppsDir, "appmanifest_730.acf"), []byte(manifestContent), 0o600)
		require.NoError(t, err)

		results, scanErr := ScanSteamApps(filepath.Join(tempDir, "steamapps"))

		require.NoError(t, scanErr)
		require.Len(t, results, 1)
		assert.Equal(t, "Counter-Strike 2", results[0].Name)
		assert.Contains(t, results[0].Path, "steam://730/")
		assert.True(t, results[0].NoExt)
	})

	t.Run("handles_invalid_manifest_gracefully", func(t *testing.T) {
		t.Parallel()

		tempDir := t.TempDir()

		// Create library structure
		steamAppsDir := filepath.Join(tempDir, "steamapps")
		require.NoError(t, os.MkdirAll(steamAppsDir, 0o750))

		// Create libraryfolders.vdf
		vdfContent := `"libraryfolders"
{
	"0"
	{
		"path"		"` + vdfEscapePath(tempDir) + `"
	}
}`
		err := os.WriteFile(filepath.Join(steamAppsDir, "libraryfolders.vdf"), []byte(vdfContent), 0o600)
		require.NoError(t, err)

		// Create an invalid app manifest
		err = os.WriteFile(filepath.Join(steamAppsDir, "appmanifest_730.acf"), []byte("invalid content"), 0o600)
		require.NoError(t, err)

		results, scanErr := ScanSteamApps(steamAppsDir)

		require.NoError(t, scanErr)
		assert.Empty(t, results)
	})

	t.Run("continues_after_invalid_manifest", func(t *testing.T) {
		t.Parallel()

		fs := afero.NewMemMapFs()
		libraryPath := filepath.Join(string(filepath.Separator), "library")
		steamAppsDir := filepath.Join(libraryPath, "steamapps")
		require.NoError(t, fs.MkdirAll(steamAppsDir, 0o750))

		vdfContent := `"libraryfolders"
{
	"0"
	{
		"path"		"` + vdfEscapePath(libraryPath) + `"
	}
}`
		require.NoError(t, afero.WriteFile(
			fs, filepath.Join(steamAppsDir, "libraryfolders.vdf"), []byte(vdfContent), 0o600,
		))
		require.NoError(t, afero.WriteFile(
			fs, filepath.Join(steamAppsDir, "appmanifest_100.acf"), []byte("invalid content"), 0o600,
		))
		writeMockManifestFS(t, fs, steamAppsDir, 200, "Valid Game")

		results, scanErr := scanSteamAppsFS(fs, steamAppsDir)

		require.NoError(t, scanErr)
		require.Len(t, results, 1)
		assert.Equal(t, "Valid Game", results[0].Name)
		assert.Contains(t, results[0].Path, "steam://200/")
	})

	t.Run("continues_after_library_directory_read_failure", func(t *testing.T) {
		t.Parallel()

		fs := afero.NewMemMapFs()
		steamDir := filepath.Join(string(filepath.Separator), "steam", "steamapps")
		failingLibraryPath := filepath.Join(string(filepath.Separator), "failing-library")
		failingAppsPath := filepath.Join(failingLibraryPath, "steamapps")
		validLibraryPath := filepath.Join(string(filepath.Separator), "valid-library")
		validAppsPath := filepath.Join(validLibraryPath, "steamapps")
		require.NoError(t, fs.MkdirAll(steamDir, 0o750))
		require.NoError(t, fs.MkdirAll(failingAppsPath, 0o750))
		require.NoError(t, fs.MkdirAll(validAppsPath, 0o750))
		content := `"libraryfolders"
{
	"0"
	{
		"path"		"` + vdfEscapePath(failingLibraryPath) + `"
	}
	"1"
	{
		"path"		"` + vdfEscapePath(validLibraryPath) + `"
	}
}`
		require.NoError(t, afero.WriteFile(fs, filepath.Join(steamDir, "libraryfolders.vdf"), []byte(content), 0o600))
		writeMockManifestFS(t, fs, validAppsPath, 200, "Valid Game")
		failingFS := failOpenFS{Fs: fs, failPaths: map[string]error{failingAppsPath: errTestFSAccess}}

		results, err := scanSteamAppsFS(failingFS, steamDir)

		require.NoError(t, err)
		require.Len(t, results, 1)
		assert.Equal(t, "Valid Game", results[0].Name)
		assert.Contains(t, results[0].Path, "steam://200/")
	})

	t.Run("continues_after_manifest_open_failure", func(t *testing.T) {
		t.Parallel()

		fs := afero.NewMemMapFs()
		libraryPath := filepath.Join(string(filepath.Separator), "library")
		steamAppsDir := filepath.Join(libraryPath, "steamapps")
		require.NoError(t, fs.MkdirAll(steamAppsDir, 0o750))
		content := `"libraryfolders"
{
	"0"
	{
		"path"		"` + vdfEscapePath(libraryPath) + `"
	}
}`
		require.NoError(t, afero.WriteFile(
			fs, filepath.Join(steamAppsDir, "libraryfolders.vdf"), []byte(content), 0o600,
		))
		failedManifest := writeMockManifestFS(t, fs, steamAppsDir, 100, "Unreadable Game")
		writeMockManifestFS(t, fs, steamAppsDir, 200, "Valid Game")
		failingFS := failOpenFS{Fs: fs, failPaths: map[string]error{failedManifest: errTestFSAccess}}

		results, err := scanSteamAppsFS(failingFS, steamAppsDir)

		require.NoError(t, err)
		require.Len(t, results, 1)
		assert.Equal(t, "Valid Game", results[0].Name)
	})

	t.Run("skips_malformed_library_entries_and_manifests", func(t *testing.T) {
		t.Parallel()

		fs := afero.NewMemMapFs()
		steamDir := filepath.Join(string(filepath.Separator), "steam", "steamapps")
		libraryPath := filepath.Join(string(filepath.Separator), "library")
		unavailableLibraryPath := filepath.Join(string(filepath.Separator), "missing-library")
		steamAppsDir := filepath.Join(libraryPath, "steamapps")
		require.NoError(t, fs.MkdirAll(steamDir, 0o750))
		require.NoError(t, fs.MkdirAll(steamAppsDir, 0o750))
		libraries := `"libraryfolders"
{
	"bad-entry"	"not-a-map"
	"missing-path"
	{
		"label"	"No path"
	}
	"unavailable"
	{
		"path"	"` + vdfEscapePath(unavailableLibraryPath) + `"
	}
	"valid"
	{
		"path"	"` + vdfEscapePath(libraryPath) + `"
	}
}`
		require.NoError(t, afero.WriteFile(
			fs, filepath.Join(steamDir, "libraryfolders.vdf"), []byte(libraries), 0o600,
		))
		manifests := map[string]string{
			"appmanifest_100.acf": `"Other" {}`,
			"appmanifest_200.acf": `"AppState" { "name" "Missing ID" }`,
			"appmanifest_300.acf": `"AppState" { "appid" "300" }`,
			"appmanifest_400.acf": `"AppState" { "appid" "400" "name" "Valid Game" }`,
		}
		for name, content := range manifests {
			require.NoError(t, afero.WriteFile(fs, filepath.Join(steamAppsDir, name), []byte(content), 0o600))
		}

		results, err := scanSteamAppsFS(fs, steamDir)

		require.NoError(t, err)
		require.Len(t, results, 1)
		assert.Equal(t, "Valid Game", results[0].Name)
		assert.Contains(t, results[0].Path, "steam://400/")
	})
}

func TestScanSteamShortcuts(t *testing.T) {
	t.Parallel()

	t.Run("returns_empty_when_userdata_not_found", func(t *testing.T) {
		t.Parallel()

		tempDir := t.TempDir()
		results, err := ScanSteamShortcuts(tempDir)

		require.NoError(t, err)
		assert.Empty(t, results)
	})

	t.Run("returns_empty_when_no_shortcuts_file", func(t *testing.T) {
		t.Parallel()

		tempDir := t.TempDir()
		userdataDir := filepath.Join(tempDir, "userdata", "12345678", "config")
		require.NoError(t, os.MkdirAll(userdataDir, 0o750))

		results, err := ScanSteamShortcuts(tempDir)

		require.NoError(t, err)
		assert.Empty(t, results)
	})

	t.Run("handles_invalid_shortcuts_file_gracefully", func(t *testing.T) {
		t.Parallel()

		tempDir := t.TempDir()
		userdataDir := filepath.Join(tempDir, "userdata", "12345678", "config")
		require.NoError(t, os.MkdirAll(userdataDir, 0o750))

		// Write invalid shortcuts.vdf
		err := os.WriteFile(filepath.Join(userdataDir, "shortcuts.vdf"), []byte("invalid binary"), 0o600)
		require.NoError(t, err)

		results, scanErr := ScanSteamShortcuts(tempDir)

		require.NoError(t, scanErr)
		assert.Empty(t, results)
	})

	t.Run("scans_non_steam_shortcuts_from_user_config", func(t *testing.T) {
		t.Parallel()

		tempDir := t.TempDir()
		userdataDir := filepath.Join(tempDir, "userdata", "12345678", "config")
		require.NoError(t, os.MkdirAll(userdataDir, 0o750))

		shortcuts := []fixtures.TestShortcut{
			{
				AppID:   624353111,
				AppName: "Capcom vs. SNK 2 Mark of the Millennium 2001",
				Exe: `"C:\Games\RetroArch\retroarch.exe" ` +
					`-L "cores\flycast_libretro.dll" "roms\Capcom vs SNK 2.chd"`,
				StartDir:      `"C:\Games\RetroArch"`,
				LaunchOptions: "",
				Optional:      true,
			},
			{
				AppID:         3545518019,
				AppName:       "Hyper Duel",
				Exe:           `"C:\Games\RetroArch\retroarch.exe"`,
				StartDir:      `"C:\Games\RetroArch"`,
				LaunchOptions: `-L "cores\mednafen_saturn_libretro.dll" "roms\Hyper Duel.chd"`,
				Optional:      false,
			},
		}
		err := os.WriteFile(filepath.Join(userdataDir, "shortcuts.vdf"), fixtures.BuildShortcutsVDF(shortcuts), 0o600)
		require.NoError(t, err)

		results, scanErr := ScanSteamShortcuts(tempDir)

		require.NoError(t, scanErr)
		require.Len(t, results, 2)
		assert.Equal(t, shortcuts[0].AppName, results[0].Name)
		assert.Equal(t, shortcutVirtualPath(shortcuts[0].AppID, shortcuts[0].AppName), results[0].Path)
		assert.True(t, results[0].NoExt)
		assert.Equal(t, shortcuts[1].AppName, results[1].Name)
		assert.Equal(t, shortcutVirtualPath(shortcuts[1].AppID, shortcuts[1].AppName), results[1].Path)
		assert.True(t, results[1].NoExt)
	})

	t.Run("excludes_only_exact_shortcut_executable", func(t *testing.T) {
		t.Parallel()

		runtimePath := filepath.Join(string(filepath.Separator), "runtime", "bin", "zaparoo-steam-runtime")
		tests := []struct {
			name        string
			excluded    string
			nonMatching string
		}{
			{
				name: "bare quoted path", excluded: `"` + runtimePath + `"`,
				nonMatching: `"` + runtimePath + `-copy"`,
			},
			{
				name: "quoted path with arguments", excluded: `"` + runtimePath + `" --runtime`,
				nonMatching: `"` + runtimePath + `-copy" --runtime`,
			},
			{
				name: "unquoted path with arguments", excluded: runtimePath + ` --runtime`,
				nonMatching: runtimePath + `-copy --runtime`,
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()

				fs := afero.NewMemMapFs()
				steamDir := filepath.Join(string(filepath.Separator), "steam")
				userdataDir := filepath.Join(steamDir, "userdata", "12345678", "config")
				require.NoError(t, fs.MkdirAll(userdataDir, 0o750))
				shortcuts := []fixtures.TestShortcut{
					{AppID: 42, AppName: "Zaparoo Runtime", Exe: tt.excluded},
					{AppID: 43, AppName: "Runtime Copy", Exe: tt.nonMatching},
				}
				require.NoError(t, afero.WriteFile(
					fs, filepath.Join(userdataDir, "shortcuts.vdf"), fixtures.BuildShortcutsVDF(shortcuts), 0o600,
				))
				client := NewClient(Options{ExcludedShortcutExecutables: []string{runtimePath}})
				client.fs = fs

				results, scanErr := client.ScanShortcuts(steamDir)

				require.NoError(t, scanErr)
				require.Len(t, results, 1)
				assert.Equal(t, shortcutVirtualPath(shortcuts[1].AppID, shortcuts[1].AppName), results[0].Path)
			})
		}
	})

	t.Run("skips_non_directory_entries", func(t *testing.T) {
		t.Parallel()

		tempDir := t.TempDir()
		userdataDir := filepath.Join(tempDir, "userdata")
		require.NoError(t, os.MkdirAll(userdataDir, 0o750))

		// Create a file instead of a directory
		err := os.WriteFile(filepath.Join(userdataDir, "somefile.txt"), []byte("not a dir"), 0o600)
		require.NoError(t, err)

		results, scanErr := ScanSteamShortcuts(tempDir)

		require.NoError(t, scanErr)
		assert.Empty(t, results)
	})
}

func TestClientScanMethods(t *testing.T) {
	t.Parallel()

	t.Run("ScanApps_delegates_to_ScanSteamApps", func(t *testing.T) {
		t.Parallel()

		client := NewClient(Options{})
		tempDir := t.TempDir()

		results, err := client.ScanApps(tempDir)

		require.NoError(t, err)
		assert.Empty(t, results)
	})

	t.Run("ScanShortcuts_delegates_to_ScanSteamShortcuts", func(t *testing.T) {
		t.Parallel()

		client := NewClient(Options{})
		tempDir := t.TempDir()

		results, err := client.ScanShortcuts(tempDir)

		require.NoError(t, err)
		assert.Empty(t, results)
	})
}
