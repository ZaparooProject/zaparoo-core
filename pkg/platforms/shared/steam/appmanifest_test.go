// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
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
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLookupAppName(t *testing.T) {
	t.Parallel()

	t.Run("finds_existing_app", func(t *testing.T) {
		t.Parallel()

		steamAppsDir := t.TempDir()
		createMockManifest(t, steamAppsDir, 348550, "Batman: Arkham Knight")

		name, ok := LookupAppName(steamAppsDir, 348550)

		assert.True(t, ok)
		assert.Equal(t, "Batman: Arkham Knight", name)
	})

	t.Run("returns_false_for_missing_app", func(t *testing.T) {
		t.Parallel()

		steamAppsDir := t.TempDir()

		name, ok := LookupAppName(steamAppsDir, 999999)

		assert.False(t, ok)
		assert.Empty(t, name)
	})

	t.Run("handles_nonexistent_directory", func(t *testing.T) {
		t.Parallel()

		name, ok := LookupAppName("/nonexistent/path", 12345)

		assert.False(t, ok)
		assert.Empty(t, name)
	})
}

func TestReadAppManifest(t *testing.T) {
	t.Parallel()

	t.Run("reads_valid_manifest", func(t *testing.T) {
		t.Parallel()

		steamAppsDir := t.TempDir()
		createMockManifest(t, steamAppsDir, 250900, "The Binding of Isaac: Rebirth")

		info, ok := ReadAppManifest(steamAppsDir, 250900)

		assert.True(t, ok)
		assert.Equal(t, 250900, info.AppID)
		assert.Equal(t, "The Binding of Isaac: Rebirth", info.Name)
	})

	t.Run("handles_invalid_vdf", func(t *testing.T) {
		t.Parallel()

		steamAppsDir := t.TempDir()
		manifestPath := filepath.Join(steamAppsDir, "appmanifest_12345.acf")
		//nolint:gosec // G306: test file permissions are fine
		require.NoError(t, os.WriteFile(manifestPath, []byte("invalid vdf content {{{"), 0o644))

		_, ok := ReadAppManifest(steamAppsDir, 12345)

		assert.False(t, ok)
	})

	t.Run("handles_missing_name", func(t *testing.T) {
		t.Parallel()

		steamAppsDir := t.TempDir()
		manifestPath := filepath.Join(steamAppsDir, "appmanifest_12345.acf")
		content := `"AppState"
{
	"appid"		"12345"
}`
		//nolint:gosec // G306: test file permissions are fine
		require.NoError(t, os.WriteFile(manifestPath, []byte(content), 0o644))

		_, ok := ReadAppManifest(steamAppsDir, 12345)

		assert.False(t, ok)
	})

	t.Run("handles_missing_AppState", func(t *testing.T) {
		t.Parallel()

		steamAppsDir := t.TempDir()
		manifestPath := filepath.Join(steamAppsDir, "appmanifest_12345.acf")
		content := `"SomeOtherRoot"
{
	"appid"		"12345"
	"name"		"Test Game"
}`
		//nolint:gosec // G306: test file permissions are fine
		require.NoError(t, os.WriteFile(manifestPath, []byte(content), 0o644))

		_, ok := ReadAppManifest(steamAppsDir, 12345)

		assert.False(t, ok)
	})
}

func TestFindSteamAppsDir(t *testing.T) {
	t.Parallel()

	t.Run("finds_lowercase_steamapps", func(t *testing.T) {
		t.Parallel()

		steamDir := t.TempDir()
		//nolint:gosec // G301: test directory permissions are fine
		require.NoError(t, os.MkdirAll(filepath.Join(steamDir, "steamapps"), 0o755))

		result := FindSteamAppsDir(steamDir)

		assert.Equal(t, filepath.Join(steamDir, "steamapps"), result)
	})

	t.Run("finds_mixed_case_SteamApps", func(t *testing.T) {
		t.Parallel()

		steamDir := t.TempDir()
		//nolint:gosec // G301: test directory permissions are fine
		require.NoError(t, os.MkdirAll(filepath.Join(steamDir, "SteamApps"), 0o755))

		result := FindSteamAppsDir(steamDir)

		// Verify the directory is found and accessible
		info, err := os.Stat(result)
		require.NoError(t, err, "returned path should exist")
		assert.True(t, info.IsDir(), "returned path should be a directory")

		// On case-sensitive filesystems (Linux), exact match expected
		// On case-insensitive filesystems (macOS, Windows), path may differ in case
		assert.True(t, strings.EqualFold(filepath.Base(result), "steamapps"),
			"directory name should be steamapps (case-insensitive)")
	})

	t.Run("finds_nested_steam_steamapps", func(t *testing.T) {
		t.Parallel()

		steamDir := t.TempDir()
		//nolint:gosec // G301: test directory permissions are fine
		require.NoError(t, os.MkdirAll(filepath.Join(steamDir, "steam", "steamapps"), 0o755))

		result := FindSteamAppsDir(steamDir)

		assert.Equal(t, filepath.Join(steamDir, "steam", "steamapps"), result)
	})

	t.Run("returns_default_if_not_found", func(t *testing.T) {
		t.Parallel()

		steamDir := t.TempDir()

		result := FindSteamAppsDir(steamDir)

		assert.Equal(t, filepath.Join(steamDir, "steamapps"), result)
	})
}

func TestExtractAppIDFromPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		path   string
		wantID int
		wantOK bool
	}{
		{
			name:   "standard_virtual_path",
			path:   "steam://348550/Batman: Arkham Knight",
			wantID: 348550,
			wantOK: true,
		},
		{
			name:   "rungameid_format",
			path:   "steam://rungameid/250900",
			wantID: 250900,
			wantOK: true,
		},
		{
			name:   "id_only",
			path:   "steam://12345",
			wantID: 12345,
			wantOK: true,
		},
		{
			name:   "without_prefix",
			path:   "348550/SomeGame",
			wantID: 348550,
			wantOK: true,
		},
		{
			name:   "invalid_id",
			path:   "steam://notanumber/Game",
			wantID: 0,
			wantOK: false,
		},
		{
			name:   "empty_path",
			path:   "",
			wantID: 0,
			wantOK: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			appID, ok := ExtractAppIDFromPath(tc.path)

			assert.Equal(t, tc.wantOK, ok)
			if tc.wantOK {
				assert.Equal(t, tc.wantID, appID)
			}
		})
	}
}

func TestFormatGameName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		inName string
		want   string
		appID  int
	}{
		{
			name:   "with_name",
			appID:  348550,
			inName: "Batman: Arkham Knight",
			want:   "Batman: Arkham Knight",
		},
		{
			name:   "empty_name",
			appID:  12345,
			inName: "",
			want:   "Steam Game 12345",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result := FormatGameName(tc.appID, tc.inName)

			assert.Equal(t, tc.want, result)
		})
	}
}

func TestLookupAppNameInLibraries(t *testing.T) {
	t.Parallel()

	t.Run("finds_in_main_library", func(t *testing.T) {
		t.Parallel()

		steamAppsDir := t.TempDir()
		createMockManifest(t, steamAppsDir, 12345, "Test Game")

		name, ok := LookupAppNameInLibraries(steamAppsDir, 12345)

		assert.True(t, ok)
		assert.Equal(t, "Test Game", name)
	})

	t.Run("searches_library_folders", func(t *testing.T) {
		t.Parallel()

		// Create main steamapps dir with libraryfolders.vdf
		steamAppsDir := t.TempDir()

		// Create a secondary library
		secondLib := t.TempDir()
		secondLibSteamApps := filepath.Join(secondLib, "steamapps")
		//nolint:gosec // G301: test directory permissions are fine
		require.NoError(t, os.MkdirAll(secondLibSteamApps, 0o755))
		createMockManifest(t, secondLibSteamApps, 99999, "Game In Secondary Library")

		// Create libraryfolders.vdf pointing to both libraries
		// Escape backslashes for Windows paths in VDF content
		escapedSteamAppsDir := escapeVDFPath(steamAppsDir)
		escapedSecondLib := escapeVDFPath(secondLib)
		libFoldersContent := `"libraryfolders"
{
	"0"
	{
		"path"		"` + escapedSteamAppsDir + `"
		"apps"
		{
		}
	}
	"1"
	{
		"path"		"` + escapedSecondLib + `"
		"apps"
		{
			"99999"		"0"
		}
	}
}`
		//nolint:gosec // G306: test file permissions are fine
		require.NoError(t, os.WriteFile(
			filepath.Join(steamAppsDir, "libraryfolders.vdf"),
			[]byte(libFoldersContent),
			0o644,
		))

		name, ok := LookupAppNameInLibraries(steamAppsDir, 99999)

		assert.True(t, ok)
		assert.Equal(t, "Game In Secondary Library", name)
	})

	t.Run("returns_false_when_not_found", func(t *testing.T) {
		t.Parallel()

		steamAppsDir := t.TempDir()

		name, ok := LookupAppNameInLibraries(steamAppsDir, 999999)

		assert.False(t, ok)
		assert.Empty(t, name)
	})
}

// createMockManifest creates a mock appmanifest file for testing.
func createMockManifest(t *testing.T, steamAppsDir string, appID int, name string) {
	t.Helper()

	appIDStr := strconv.Itoa(appID)
	manifestPath := filepath.Join(steamAppsDir, "appmanifest_"+appIDStr+".acf")
	content := `"AppState"
{
	"appid"		"` + appIDStr + `"
	"name"		"` + name + `"
	"installdir"		"` + name + `"
	"StateFlags"		"4"
}`
	//nolint:gosec // G306: test file permissions are fine
	require.NoError(t, os.WriteFile(manifestPath, []byte(content), 0o644))
}

// escapeVDFPath escapes backslashes in paths for embedding in VDF content.
// This is needed on Windows where paths contain backslashes.
func escapeVDFPath(path string) string {
	return strings.ReplaceAll(path, `\`, `\\`)
}
