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
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const installedManifest = `"AppState" { "appid" "730" "StateFlags" "4" "installdir" "Game" }`

func TestIsAppInstalled(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		manifest    string
		secondary   string
		libraries   string
		steamApps   string
		missingGame bool
		gameIsFile  bool
		want        bool
	}{
		{name: "main without library metadata", manifest: installedManifest, want: true},
		{name: "main despite corrupt library metadata", manifest: installedManifest, libraries: `broken`, want: true},
		{name: "no metadata"},
		{
			name: "secondary", secondary: installedManifest,
			libraries: `"libraryfolders" { "1" { "path" SECONDARY "apps" { "730" "123" } } }`, want: true,
		},
		{
			name: "secondary with stale index", secondary: installedManifest,
			libraries: `"libraryfolders" { "1" { "path" SECONDARY "apps" { "999" "123" } } }`, want: true,
		},
		{
			name: "secondary without index", secondary: installedManifest,
			libraries: `"libraryfolders" { "1" { "path" SECONDARY } }`, want: true,
		},
		{
			name: "legacy secondary", secondary: installedManifest,
			libraries: `"LibraryFolders" { "1" SECONDARY }`, want: true,
		},
		{
			name: "unavailable library does not hide installed game", secondary: installedManifest,
			libraries: `"libraryfolders" { "1" { "path" MISSING } "2" { "path" SECONDARY } }`, want: true,
		},
		{
			name: "corrupt main does not hide secondary", manifest: `broken`, secondary: installedManifest,
			libraries: `"libraryfolders" { "1" { "path" SECONDARY } }`, want: true,
		},
		{name: "unavailable library", libraries: `"libraryfolders" { "1" { "path" MISSING } }`},
		{
			name:      "index alone is not installation",
			libraries: `"libraryfolders" { "1" { "path" SECONDARY "apps" { "730" "123" } } }`,
		},
		{
			name: "corrupt libraries", secondary: installedManifest,
			libraries: `"libraryfolders" { "1" { "path" SECONDARY }`,
		},
		{name: "invalid library type", libraries: `"libraryfolders" "invalid"`},
		{name: "invalid library path", libraries: `"libraryfolders" { "1" { "path" { "bad" "type" } } }`},
		{name: "corrupt manifest", manifest: `garbage`},
		{name: "truncated manifest", manifest: strings.TrimSuffix(installedManifest, "}")},
		{name: "mismatched appid", manifest: strings.Replace(installedManifest, `"730"`, `"999"`, 1)},
		{name: "missing state flags", manifest: `"AppState" { "appid" "730" "installdir" "Game" }`},
		{name: "invalid state flags", manifest: strings.Replace(installedManifest, `"4"`, `"invalid"`, 1)},
		{name: "partial install", manifest: strings.Replace(installedManifest, `"4"`, `"1024"`, 1)},
		{
			name:     "installed with update required",
			manifest: strings.Replace(installedManifest, `"4"`, `"6"`, 1), want: true,
		},
		{name: "missing game directory", manifest: installedManifest, missingGame: true},
		{name: "game path is file", manifest: installedManifest, gameIsFile: true},
		{name: "missing installdir", manifest: `"AppState" { "appid" "730" "StateFlags" "4" }`},
		{name: "installdir traversal", manifest: strings.Replace(installedManifest, `"Game"`, `".."`, 1)},
		{name: "installdir is common root", manifest: strings.Replace(installedManifest, `"Game"`, `"."`, 1)},
		{name: "case insensitive metadata", manifest: strings.NewReplacer(
			"AppState", "APPSTATE", "appid", "APPID", "StateFlags", "STATEFLAGS", "installdir", "INSTALLDIR",
		).Replace(installedManifest), want: true},
		{name: "mixed case steamapps", steamApps: "SteamApps", manifest: installedManifest, want: true},
		{name: "trailing comment", manifest: installedManifest + ` // no final newline`, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			fs := testhelpers.NewMemoryFS()
			base := t.TempDir()
			root := filepath.Join(base, "Steam")
			secondary := filepath.Join(base, "Secondary")
			steamApps := tt.steamApps
			if steamApps == "" {
				steamApps = "steamapps"
			}
			mainApps := filepath.Join(root, steamApps)
			require.NoError(t, fs.Fs.MkdirAll(mainApps, 0o750))
			if tt.manifest != "" {
				require.NoError(t, fs.WriteFile(filepath.Join(mainApps, "appmanifest_730.acf"),
					[]byte(tt.manifest), 0o600))
			}
			if tt.secondary != "" {
				require.NoError(t, fs.WriteFile(filepath.Join(secondary, "steamapps", "appmanifest_730.acf"),
					[]byte(tt.secondary), 0o600))
			}
			if !tt.missingGame {
				gameDir := filepath.Join(mainApps, "common", "Game")
				if tt.gameIsFile {
					require.NoError(t, fs.WriteFile(gameDir, []byte("not a directory"), 0o600))
				} else {
					require.NoError(t, fs.Fs.MkdirAll(gameDir, 0o750))
				}
				require.NoError(t, fs.Fs.MkdirAll(filepath.Join(secondary, "steamapps", "common", "Game"), 0o750))
			}
			if tt.libraries != "" {
				content := strings.NewReplacer("SECONDARY", strconv.Quote(secondary),
					"MISSING", strconv.Quote(filepath.Join(base, "Missing"))).Replace(tt.libraries)
				require.NoError(t, fs.WriteFile(filepath.Join(mainApps, "libraryfolders.vdf"), []byte(content), 0o600))
			}
			client := NewClient(Options{})
			client.fs = fs.Fs

			assert.Equal(t, tt.want, client.isAppInstalled(root, "730"))
		})
	}
}

func TestInstallationLookupDoesNotUseWorkingDirectory(t *testing.T) {
	t.Parallel()

	fs := testhelpers.NewMemoryFS()
	require.NoError(t, fs.WriteFile(filepath.Join("steamapps", "appmanifest_730.acf"),
		[]byte(installedManifest), 0o600))
	require.NoError(t, fs.Fs.MkdirAll(filepath.Join("steamapps", "common", "Game"), 0o750))
	client := NewClient(Options{})
	client.fs = fs.Fs
	assert.False(t, client.isAppInstalled("", "730"))
	assert.False(t, client.isAppInstalled(".", "730"))
}

type unreadableSteamMetadataFS struct {
	afero.Fs
}

func (unreadableSteamMetadataFS) Open(string) (afero.File, error) {
	return nil, os.ErrPermission
}

func TestInstallationMetadataAccessFailures(t *testing.T) {
	t.Parallel()

	fs := testhelpers.NewMemoryFS()
	path := filepath.Join(t.TempDir(), "appmanifest_730.acf")
	require.NoError(t, fs.WriteFile(path, []byte(installedManifest), 0o600))
	client := NewClient(Options{})
	client.fs = unreadableSteamMetadataFS{Fs: fs.Fs}
	_, ok := client.readInstallMetadata(path)
	assert.False(t, ok)

	client.fs = fs.Fs
	require.NoError(t, fs.WriteFile(path, []byte(strings.Repeat(" ", maxInstallMetadataSize+1)), 0o600))
	_, ok = client.readInstallMetadata(path)
	assert.False(t, ok, "oversized metadata must not be parsed")
	_, ok = client.readInstallMetadata(filepath.Dir(path))
	assert.False(t, ok, "metadata must be a regular file")
}

func TestValidInstallVDF(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		valid bool
	}{
		{installedManifest, true},
		{`AppState { appid 730 StateFlags 4 installdir Game }`, true},
		{"// heading\n" + installedManifest + " // tail", true},
		{`"AppState" { "name" "A \\\"quoted\\\" name // { }" }`, true},
		{`"AppState" { "installdir" "C:\\Games\\Game" }`, true},
		{"", false},
		{`// unfinished file`, false},
		{`"AppState" { "appid" "730"`, false},
		{`"AppState" { "appid" }`, false},
		{`"AppState" { "appid" "730 }`, false},
		{`"AppState" { "appid" "730" } }`, false},
		{installedManifest + ` "extra" {}`, false},
		{`"AppState" { "appid" "730" ! }`, false},
		{`"AppState" { "appid" "730" ` + "\x00" + ` }`, false},
		{strings.Repeat(`"nested" {`, 65) + strings.Repeat("}", 65), false},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.valid, validInstallVDF([]byte(tt.input)))
		})
	}
}

func FuzzSteamInstallationMetadata(f *testing.F) {
	f.Add(installedManifest)
	f.Add(installedManifest + ` // tail`)
	f.Add(`"libraryfolders" { "0" { "path" "/Steam" "apps" { "730" "1" } } }`)
	f.Add(`"AppState" { "appid" "730"`)
	f.Add(`// tail`)
	f.Fuzz(func(t *testing.T, data string) {
		if len(data) > maxInstallMetadataSize {
			t.Skip("metadata size limit covered by unit test")
		}
		fs := testhelpers.NewMemoryFS()
		path := filepath.Join("steamapps", "appmanifest_730.acf")
		require.NoError(t, fs.WriteFile(path, []byte(data), 0o600))
		client := NewClient(Options{})
		client.fs = fs.Fs
		metadata, ok := client.readInstallMetadata(path)
		if ok {
			require.True(t, validInstallVDF([]byte(data)))
			require.NotEmpty(t, metadata)
		}
	})
}
