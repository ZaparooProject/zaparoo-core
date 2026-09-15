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

package config

import (
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateCustomLauncher_MisterVirtualSystem(t *testing.T) {
	entry := LaunchersCustom{
		ID:       "MisterOtherArduboy",
		Kind:     CustomLauncherKindVirtualSystem,
		Backend:  CustomLauncherBackendMisterCore,
		Name:     "Arduboy",
		LoadPath: "_Other/Arduboy",
	}

	require.NoError(t, validateCustomLauncher(&entry))
	assert.Equal(t, "Other", entry.Category)
}

func TestValidateCustomLauncher_NormalizesImpliedKindAndBackend(t *testing.T) {
	entry := LaunchersCustom{
		ID:      "Winamp_Main",
		Kind:    CustomLauncherKindVirtualSystem,
		Name:    "Winamp",
		Execute: "winamp.exe",
	}

	require.NoError(t, validateCustomLauncher(&entry))
	assert.Equal(t, CustomLauncherKindVirtualSystem, entry.Kind)
	assert.Equal(t, CustomLauncherBackendCommand, entry.Backend)
	assert.Equal(t, "Other", entry.Category)
}

func TestValidateCustomLauncher_NormalizesOmittedKind(t *testing.T) {
	entry := LaunchersCustom{
		ID:       "WinampMedia",
		System:   "MusicTrack",
		FileExts: []string{".m3u"},
		Execute:  "winamp.exe \"[[media_path]]\"",
	}

	require.NoError(t, validateCustomLauncher(&entry))
	assert.Equal(t, CustomLauncherKindLauncher, entry.Kind)
	assert.Equal(t, CustomLauncherBackendCommand, entry.Backend)
}

func TestValidateCustomLauncher_MediaDirsOnlyEntry(t *testing.T) {
	t.Run("accepted with system and media dirs", func(t *testing.T) {
		entry := LaunchersCustom{
			ID:        "Famicom",
			System:    "NES",
			MediaDirs: []string{"/media/fat/cifs/games/famicom"},
			FileExts:  []string{".nes"},
		}

		require.NoError(t, validateCustomLauncher(&entry))
		assert.Equal(t, CustomLauncherKindLauncher, entry.Kind)
		assert.Empty(t, entry.Backend)
	})

	// Without media dirs it declares no folders, so it matches every path for
	// its system and indexes nothing. Loading it would do nothing at all.
	t.Run("rejected without media dirs", func(t *testing.T) {
		entry := LaunchersCustom{ID: "Famicom", System: "NES", FileExts: []string{".nes"}}
		require.ErrorContains(t, validateCustomLauncher(&entry),
			"a launcher without execute must set system and media_dirs")
	})

	t.Run("rejected without system", func(t *testing.T) {
		entry := LaunchersCustom{ID: "Famicom", MediaDirs: []string{"/games/famicom"}}
		require.ErrorContains(t, validateCustomLauncher(&entry),
			"a launcher without execute must set system and media_dirs")
	})
}

func TestValidateCustomLauncher_RejectsInvalidCommonFields(t *testing.T) {
	tests := []struct {
		name  string
		err   string
		entry LaunchersCustom
	}{
		{name: "missing id", entry: LaunchersCustom{Execute: "echo test"}, err: "id is required"},
		{
			name: "id whitespace", entry: LaunchersCustom{ID: " Bad", Execute: "echo test"},
			err: "surrounding whitespace",
		},
		{name: "unknown kind", entry: LaunchersCustom{ID: "Bad", Kind: "virtual-system"}, err: "unsupported kind"},
		{
			name: "unknown backend", entry: LaunchersCustom{ID: "Bad", Backend: "mister-core"},
			err: "unsupported backend",
		},
		{name: "invalid lifecycle", entry: LaunchersCustom{ID: "Bad", Lifecycle: "detached"}, err: "lifecycle"},
		{
			name:  "command missing execute",
			entry: LaunchersCustom{ID: "Bad", Backend: CustomLauncherBackendCommand},
			err:   "requires execute",
		},
		{
			name: "execute with native backend",
			entry: LaunchersCustom{
				ID: "Bad", Backend: CustomLauncherBackendMisterCore, System: "SNES",
				LoadPath: "_Console/SNES", Execute: "echo test",
			},
			err: "execute cannot be combined",
		},
		{
			name: "mister media launcher unsupported",
			entry: LaunchersCustom{
				ID: "Bad", Backend: CustomLauncherBackendMisterCore, System: "SNES",
				LoadPath: "_Console/SNES",
			},
			err: "currently requires kind \"virtual_system\"",
		},
		{
			name:  "load path without backend",
			entry: LaunchersCustom{ID: "Bad", LoadPath: "_Console/SNES"},
			err:   "load_path requires",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateCustomLauncher(&tt.entry)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.err)
		})
	}
}

func TestValidateCustomLauncher_CommandVirtualSystem(t *testing.T) {
	entry := LaunchersCustom{
		ID: "Tools", Kind: CustomLauncherKindVirtualSystem,
		Backend: CustomLauncherBackendCommand, Name: "Tools",
		Category: "Computer", Execute: "echo tools",
	}

	require.NoError(t, validateCustomLauncher(&entry))
}

func TestValidateCustomLauncher_KeepsCategoriesAsWritten(t *testing.T) {
	entry := LaunchersCustom{
		ID: "Tools", Kind: CustomLauncherKindVirtualSystem,
		Backend: CustomLauncherBackendCommand, Name: "Tools",
		Categories: []string{"KIDS", "Undeclared"}, Execute: "echo tools",
	}

	require.NoError(t, validateCustomLauncher(&entry))
	assert.Equal(t, "Other", entry.Category)
	assert.Equal(t, []string{"KIDS", "Undeclared"}, entry.Categories)
}

func TestValidateCustomLauncher_RejectsCategoriesOnMediaLauncher(t *testing.T) {
	entry := LaunchersCustom{
		ID: "Famicom", System: "NES", MediaDirs: []string{"Games"},
		Categories: []string{"Console"},
	}

	require.ErrorContains(t, validateCustomLauncher(&entry), "categories require")
}

func TestValidateCustomLauncher_RejectsInvalidMisterLoadPath(t *testing.T) {
	paths := []string{
		"/media/fat/_Other/Arduboy",
		`_Other\Arduboy`,
		"_Other/../Arduboy",
		"_Other//Arduboy",
		"_Other/Arduboy.rbf",
		" _Other/Arduboy",
	}
	for _, loadPath := range paths {
		t.Run(loadPath, func(t *testing.T) {
			entry := LaunchersCustom{
				ID: "Bad", Backend: CustomLauncherBackendMisterCore,
				System: "SNES", LoadPath: loadPath,
			}
			assert.Error(t, validateCustomLauncher(&entry))
		})
	}
}

func TestValidateCustomLauncher_RejectsInvalidVirtualSystem(t *testing.T) {
	valid := LaunchersCustom{
		ID: "MisterOtherArduboy", Kind: CustomLauncherKindVirtualSystem,
		Backend: CustomLauncherBackendMisterCore, Name: "Arduboy",
		Category: "Handheld", LoadPath: "_Other/Arduboy",
	}
	tests := []struct {
		name   string
		mutate func(*LaunchersCustom)
		err    string
	}{
		{name: "missing name", mutate: func(e *LaunchersCustom) { e.Name = "" }, err: "requires name"},
		{
			name:   "pattern load path",
			mutate: func(e *LaunchersCustom) { e.LoadPath = "_Other/Arduboy_<date>" },
			err:    "cannot use RBF patterns",
		},
		{name: "system field", mutate: func(e *LaunchersCustom) { e.System = "SNES" }, err: "media launcher fields"},
		{
			name:   "media directories",
			mutate: func(e *LaunchersCustom) { e.MediaDirs = []string{"Games"} },
			err:    "media launcher fields",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry := cloneCustomLauncher(&valid)
			tt.mutate(&entry)
			err := validateCustomLauncher(&entry)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.err)
		})
	}
}

func TestLoadCustomLaunchers_StrictAndAtomic(t *testing.T) {
	fs := afero.NewMemMapFs()
	launchersDir := filepath.Join("data", "launchers")
	require.NoError(t, fs.MkdirAll(launchersDir, 0o750))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(launchersDir, "valid.toml"), []byte(`
[[launchers.custom]]
id = "MisterOtherArduboy"
kind = "virtual_system"
backend = "mister_core"
name = "Arduboy"
load_path = "_Other/Arduboy"
`), 0o600))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(launchersDir, "unknown.toml"), []byte(`
[[launchers.custom]]
id = "Typo"
execute = "echo test"
excute = "misspelled"
`), 0o600))

	cfg := &Instance{fs: fs}
	require.NoError(t, cfg.LoadCustomLaunchers(launchersDir))
	require.Len(t, cfg.CustomLaunchers(), 1)

	// Reload replaces the external snapshot instead of appending duplicates.
	require.NoError(t, cfg.LoadCustomLaunchers(launchersDir))
	require.Len(t, cfg.CustomLaunchers(), 1)
}

func TestLoadCustomLaunchers_RejectsCategoryDeclarations(t *testing.T) {
	fs := afero.NewMemMapFs()
	launchersDir := filepath.Join("data", "launchers")
	require.NoError(t, fs.MkdirAll(launchersDir, 0o750))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(launchersDir, "invalid.toml"), []byte(`
[[systems.category]]
name = "Favorites"
systems = ["SNES"]

[[launchers.custom]]
id = "Tools"
execute = "echo tools"
`), 0o600))

	cfg := &Instance{fs: fs}
	require.Error(t, cfg.LoadCustomLaunchers(launchersDir))
	assert.Empty(t, cfg.CustomLaunchers())
}

func TestLoadCustomLaunchers_RetainsSnapshotWhenAllFilesFail(t *testing.T) {
	fs := afero.NewMemMapFs()
	launchersDir := filepath.Join("data", "launchers")
	launcherPath := filepath.Join(launchersDir, "launcher.toml")
	require.NoError(t, fs.MkdirAll(launchersDir, 0o750))
	require.NoError(t, afero.WriteFile(fs, launcherPath, []byte(`
[[launchers.custom]]
id = "WorkingLauncher"
execute = "echo test"
`), 0o600))
	cfg := &Instance{fs: fs}
	require.NoError(t, cfg.LoadCustomLaunchers(launchersDir))
	require.Len(t, cfg.CustomLaunchers(), 1)

	require.NoError(t, afero.WriteFile(fs, launcherPath, []byte(`invalid {{{`), 0o600))
	require.Error(t, cfg.LoadCustomLaunchers(launchersDir))

	entries := cfg.CustomLaunchers()
	require.Len(t, entries, 1)
	assert.Equal(t, "WorkingLauncher", entries[0].ID)
}

func TestLoadCustomLaunchers_InlineEntryWinsCaseInsensitiveDuplicateID(t *testing.T) {
	fs := afero.NewMemMapFs()
	launchersDir := filepath.Join("data", "launchers")
	require.NoError(t, fs.MkdirAll(launchersDir, 0o750))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(launchersDir, "duplicate.toml"), []byte(`
[[launchers.custom]]
id = "inlinelauncher"
execute = "echo external"
`), 0o600))

	cfg := &Instance{fs: fs}
	require.NoError(t, cfg.LoadTOML(`
[[launchers.custom]]
id = "InlineLauncher"
execute = "echo inline"
`))
	require.NoError(t, cfg.LoadCustomLaunchers(launchersDir))

	entries := cfg.CustomLaunchers()
	require.Len(t, entries, 1)
	assert.Equal(t, "echo inline", entries[0].Execute)
}

func TestLoadCustomLaunchers_ResolvesMainConfigCategories(t *testing.T) {
	fs := afero.NewMemMapFs()
	launchersDir := filepath.Join("data", "launchers")
	require.NoError(t, fs.MkdirAll(launchersDir, 0o750))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(launchersDir, "virtual.toml"), []byte(`
[[launchers.custom]]
id = "Tools"
kind = "virtual_system"
name = "Tools"
category = "favorite systems"
categories = ["COMPUTER"]
execute = "echo tools"
`), 0o600))

	cfg := &Instance{fs: fs}
	require.NoError(t, cfg.LoadTOML(`
[[systems.category]]
name = "Favorite Systems"
`))
	require.NoError(t, cfg.LoadCustomLaunchers(launchersDir))

	entries := cfg.CustomLaunchers()
	require.Len(t, entries, 1)
	primary, categories := cfg.SystemCategoryResolver().VirtualSystem(entries[0].Category, entries[0].Categories)
	assert.Equal(t, "Favorite Systems", primary)
	assert.Equal(t, []string{"Favorite Systems", "Computer"}, categories)
}

func TestLoadCustomLaunchers_CategoriesFollowLaterConfigReloads(t *testing.T) {
	fs := afero.NewMemMapFs()
	launchersDir := filepath.Join("data", "launchers")
	require.NoError(t, fs.MkdirAll(launchersDir, 0o750))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(launchersDir, "virtual.toml"), []byte(`
[[launchers.custom]]
id = "Tools"
kind = "virtual_system"
name = "Tools"
category = "Kids"
categories = ["Favorites"]
execute = "echo tools"
`), 0o600))

	cfg := &Instance{fs: fs}
	require.NoError(t, cfg.LoadCustomLaunchers(launchersDir))
	require.NoError(t, cfg.LoadTOML(`
[[systems.category]]
name = "Kids"

[[systems.category]]
name = "Favorites"
`))

	entries := cfg.CustomLaunchers()
	require.Len(t, entries, 1)
	primary, categories := cfg.SystemCategoryResolver().VirtualSystem(entries[0].Category, entries[0].Categories)
	assert.Equal(t, "Kids", primary, "categories declared after launchers load still resolve")
	assert.Equal(t, []string{"Kids", "Favorites"}, categories)
}

func TestCustomLaunchers_ReturnsDeepCopy(t *testing.T) {
	cfg := &Instance{}
	require.NoError(t, cfg.LoadTOML(`
[[launchers.custom]]
id = "CommandLauncher"
execute = "echo test"

[launchers.custom.controls]
menu = "**input.keyboard:{f1}"
`))

	first := cfg.CustomLaunchers()
	require.Len(t, first, 1)
	first[0].Controls["menu"] = "mutated"

	second := cfg.CustomLaunchers()
	assert.Equal(t, "**input.keyboard:{f1}", second[0].Controls["menu"])
}

func TestCustomLaunchers_ReturnsCategoryCopy(t *testing.T) {
	cfg := &Instance{}
	require.NoError(t, cfg.LoadTOML(`
[[systems.category]]
name = "Favorites"

[[launchers.custom]]
id = "Tools"
kind = "virtual_system"
name = "Tools"
categories = ["Favorites"]
execute = "echo tools"
`))

	first := cfg.CustomLaunchers()
	require.Len(t, first, 1)
	first[0].Categories[0] = "mutated"

	second := cfg.CustomLaunchers()
	assert.Equal(t, []string{"Favorites"}, second[0].Categories)
}

func TestLoadCustomLaunchers_KeepsVirtualSystemWithUndeclaredCategories(t *testing.T) {
	fs := afero.NewMemMapFs()
	launchersDir := filepath.Join("data", "launchers")
	require.NoError(t, fs.MkdirAll(launchersDir, 0o750))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(launchersDir, "virtual.toml"), []byte(`
[[launchers.custom]]
id = "Tools"
kind = "virtual_system"
name = "Tools"
category = "Removed Primary"
categories = ["Removed Extra", "Computer"]
execute = "echo tools"
`), 0o600))

	cfg := &Instance{fs: fs}
	require.NoError(t, cfg.LoadCustomLaunchers(launchersDir))

	entries := cfg.CustomLaunchers()
	require.Len(t, entries, 1, "an undeclared category must not remove the virtual system")
	assert.Equal(t, "Tools", entries[0].ID)
}
