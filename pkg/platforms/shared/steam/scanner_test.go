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
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/virtualpath"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// vdfEscapePath escapes backslashes in paths for VDF files.
// VDF format requires backslashes to be escaped as double backslashes.
func vdfEscapePath(path string) string {
	return strings.ReplaceAll(path, `\`, `\\`)
}

type testShortcut struct {
	AppName       string
	Exe           string
	StartDir      string
	LaunchOptions string
	AppID         uint32
	Optional      bool
}

func writeVDFString(buf *bytes.Buffer, key, value string) {
	_ = buf.WriteByte(0x01)
	_, _ = buf.WriteString(key)
	_ = buf.WriteByte(0x00)
	_, _ = buf.WriteString(value)
	_ = buf.WriteByte(0x00)
}

func writeVDFUint32(buf *bytes.Buffer, key string, value uint32) {
	_ = buf.WriteByte(0x02)
	_, _ = buf.WriteString(key)
	_ = buf.WriteByte(0x00)
	var raw [4]byte
	binary.LittleEndian.PutUint32(raw[:], value)
	_, _ = buf.Write(raw[:])
}

func writeEmptyVDFMap(buf *bytes.Buffer, key string) {
	_ = buf.WriteByte(0x00)
	_, _ = buf.WriteString(key)
	_ = buf.WriteByte(0x00)
	_ = buf.WriteByte(0x08)
}

func buildShortcutsVDF(shortcuts []testShortcut) []byte {
	var buf bytes.Buffer

	_ = buf.WriteByte(0x00)
	_, _ = buf.WriteString("shortcuts")
	_ = buf.WriteByte(0x00)

	for i, shortcut := range shortcuts {
		_ = buf.WriteByte(0x00)
		_, _ = buf.WriteString(strconv.Itoa(i))
		_ = buf.WriteByte(0x00)

		writeVDFUint32(&buf, "appid", shortcut.AppID)
		writeVDFString(&buf, "AppName", shortcut.AppName)
		writeVDFString(&buf, "Exe", shortcut.Exe)
		writeVDFString(&buf, "StartDir", shortcut.StartDir)
		writeVDFString(&buf, "LaunchOptions", shortcut.LaunchOptions)

		if shortcut.Optional {
			writeVDFString(&buf, "icon", "")
			writeVDFString(&buf, "ShortcutPath", "")
			writeVDFUint32(&buf, "IsHidden", 0)
			writeVDFUint32(&buf, "AllowDesktopConfig", 1)
			writeVDFUint32(&buf, "AllowOverlay", 1)
			writeEmptyVDFMap(&buf, "tags")
		}

		_ = buf.WriteByte(0x08)
	}

	_ = buf.WriteByte(0x08)
	_ = buf.WriteByte(0x08)

	return buf.Bytes()
}

func shortcutVirtualPath(appID uint32, appName string) string {
	bpid := (uint64(appID) << 32) | 0x02000000
	return virtualpath.CreateVirtualPath("steam", strconv.FormatUint(bpid, 10), appName)
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

		shortcuts := []testShortcut{
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
		err := os.WriteFile(filepath.Join(userdataDir, "shortcuts.vdf"), buildShortcutsVDF(shortcuts), 0o600)
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
