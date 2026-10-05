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

package playnite

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGamePathRoundTrip(t *testing.T) {
	t.Parallel()

	path := GamePath(idPC, "PC Game: Deluxe")
	id, err := ParseGamePath(path)
	require.NoError(t, err)
	assert.Equal(t, idPC, id)
}

func TestNormalizeGameID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		id      string
		want    string
		wantErr bool
	}{
		{name: "canonical", id: idPC, want: idPC},
		{name: "upper case folds", id: "67FEEE56-A90D-4022-9BE8-7BEC24E4FAC0", want: idPC},
		{name: "surrounding space", id: " " + idPC + " ", want: idPC},
		{name: "empty", id: "", wantErr: true},
		{name: "nil uuid", id: "00000000-0000-0000-0000-000000000000", wantErr: true},
		{name: "no hyphens", id: "67feee56a90d40229be87bec24e4fac0", wantErr: true},
		{name: "braced", id: "{" + idPC + "}", wantErr: true},
		{name: "urn form", id: "urn:uuid:" + idPC, wantErr: true},
		{name: "argument injection", id: idPC + " --shutdown", wantErr: true},
		{name: "not a uuid", id: "42", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := NormalizeGameID(tt.id)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParseGamePathRejectsOtherSchemes(t *testing.T) {
	t.Parallel()

	for _, path := range []string{
		"steam://" + idPC + "/Game",
		"playnite://not-a-guid/Game",
		"playnite://",
		`C:\Games\game.exe`,
	} {
		_, err := ParseGamePath(path)
		require.Error(t, err, path)
	}
}

func TestLocator(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	withInstall := func(t *testing.T, fs afero.Fs, dir string, fullscreen bool) {
		t.Helper()
		require.NoError(t, fs.MkdirAll(dir, 0o755))
		require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, desktopExeName), nil, 0o600))
		if fullscreen {
			require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, fullscreenExeName), nil, 0o600))
		}
	}
	configured := filepath.Join(root, "configured")
	registered := filepath.Join(root, "registered")
	candidate := filepath.Join(root, "candidate")

	t.Run("nothing installed", func(t *testing.T) {
		t.Parallel()
		locator := Locator{FS: afero.NewMemMapFs(), Candidates: []string{candidate}}
		_, err := locator.Locate("")
		require.ErrorIs(t, err, ErrNotInstalled)
	})

	t.Run("configured directory is authoritative", func(t *testing.T) {
		t.Parallel()
		fs := afero.NewMemMapFs()
		withInstall(t, fs, candidate, false)
		locator := Locator{FS: fs, Candidates: []string{candidate}}

		_, err := locator.Locate(configured)
		require.Error(t, err)
		require.NotErrorIs(t, err, ErrNotInstalled, "a wrong install_dir must not read as not installed")

		withInstall(t, fs, configured, true)
		inst, err := locator.Locate(configured)
		require.NoError(t, err)
		assert.Equal(t, configured, inst.Dir)
		assert.Equal(t, filepath.Join(configured, fullscreenExeName), inst.FullscreenExe)
	})

	t.Run("registry beats candidates", func(t *testing.T) {
		t.Parallel()
		fs := afero.NewMemMapFs()
		withInstall(t, fs, registered, false)
		withInstall(t, fs, candidate, false)
		locator := Locator{
			FS: fs, Candidates: []string{candidate},
			Registry: func() (string, error) { return registered, nil },
		}
		inst, err := locator.Locate("")
		require.NoError(t, err)
		assert.Equal(t, registered, inst.Dir)
		assert.Empty(t, inst.FullscreenExe)
	})

	t.Run("stale or failing registry falls back", func(t *testing.T) {
		t.Parallel()
		fs := afero.NewMemMapFs()
		withInstall(t, fs, candidate, false)
		for _, registry := range []func() (string, error){
			func() (string, error) { return registered, nil },
			func() (string, error) { return "", errors.New("no key") },
		} {
			locator := Locator{FS: fs, Candidates: []string{candidate}, Registry: registry}
			inst, err := locator.Locate("")
			require.NoError(t, err)
			assert.Equal(t, candidate, inst.Dir)
		}
	})

	t.Run("a directory named like the executable is not an install", func(t *testing.T) {
		t.Parallel()
		fs := afero.NewMemMapFs()
		require.NoError(t, fs.MkdirAll(filepath.Join(candidate, desktopExeName), 0o755))
		_, err := Locator{FS: fs, Candidates: []string{candidate}}.Locate("")
		require.ErrorIs(t, err, ErrNotInstalled)
	})
}

func TestDefaultInstallDirs(t *testing.T) {
	t.Parallel()

	assert.Empty(t, DefaultInstallDirs(""))
	local := filepath.Join(t.TempDir(), "Local")
	assert.Equal(t, []string{filepath.Join(local, "Playnite")}, DefaultInstallDirs(local))
}

func TestSystemForGame(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		want      string
		platforms []Platform
		wantOK    bool
	}{
		{name: "no platform is a PC program", want: systemdefs.SystemPC, wantOK: true},
		{
			name: "specification decides, not the name", want: systemdefs.SystemNES, wantOK: true,
			platforms: []Platform{{SpecificationID: "nintendo_nes", Name: "Whatever I Called It"}},
		},
		{
			name: "specification is case-insensitive", want: systemdefs.SystemPS2, wantOK: true,
			platforms: []Platform{{SpecificationID: " Sony_PlayStation2 "}},
		},
		{
			name: "first known platform wins", want: systemdefs.SystemSNES, wantOK: true,
			platforms: []Platform{
				{Name: "Custom"}, {SpecificationID: "nintendo_super_nes"}, {SpecificationID: "nintendo_nes"},
			},
		},
		{name: "unknown specification has no system", platforms: []Platform{{SpecificationID: "adobe_flash"}}},
		{name: "custom platform has no system", platforms: []Platform{{Name: "Homebrew Box"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := SystemForGame(&Game{Platforms: tt.platforms})
			assert.Equal(t, tt.wantOK, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestSpecificationSystemsAreRealSystems(t *testing.T) {
	t.Parallel()

	for spec, systemID := range specificationSystems {
		system, err := systemdefs.GetSystem(systemID)
		require.NoError(t, err, spec)
		assert.Equal(t, systemID, system.ID, spec)
	}

	systems := Systems()
	assert.True(t, slices.IsSorted(systems))
	assert.Contains(t, systems, systemdefs.SystemPC)
	assert.Contains(t, systems, systemdefs.SystemNES)
	assert.Len(t, slices.Compact(slices.Clone(systems)), len(systems), "no duplicates")
}

func TestScanResults(t *testing.T) {
	t.Parallel()

	games := fixtureGames()
	names := func(results []platforms.ScanResult) []string {
		out := make([]string, 0, len(results))
		for _, result := range results {
			out = append(out, result.Name)
		}
		return out
	}

	pc := ScanResults(games, systemdefs.SystemPC, false)
	assert.Equal(t, []string{"PC Game", "Steam Game", "Bare Program"}, names(pc),
		"hidden, uninstalled and unmapped games are left out")
	for _, result := range pc {
		assert.True(t, result.NoExt)
		assert.Nil(t, result.Source)
		id, err := ParseGamePath(result.Path)
		require.NoError(t, err)
		assert.NotEmpty(t, id)
	}
	assert.Equal(t, GamePath(idPC, "PC Game"), pc[0].Path)

	assert.Equal(t, []string{"PC Game", "Bare Program"},
		names(ScanResults(games, systemdefs.SystemPC, true)), "Steam games are left to the Steam launcher")
	assert.Equal(t, []string{"NES Game"}, names(ScanResults(games, systemdefs.SystemNES, true)))
	assert.Empty(t, ScanResults(games, systemdefs.SystemSNES, false))
}

func TestScanResultsRejectsUnusableEntries(t *testing.T) {
	t.Parallel()

	games := []Game{
		{ID: "not-a-guid", Name: "Bad ID", IsInstalled: true},
		{ID: idPC, Name: "   ", IsInstalled: true},
		{ID: idNES, Name: "Bad\x00Name", IsInstalled: true},
		{ID: idBare, Name: "  Trimmed  ", IsInstalled: true},
	}
	results := ScanResults(games, systemdefs.SystemPC, false)
	require.Len(t, results, 1)
	assert.Equal(t, "Trimmed", results[0].Name)
	assert.Equal(t, GamePath(idBare, "Trimmed"), results[0].Path)
}

func TestScanResultsRomSource(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	rom := filepath.Join(dir, "game.nes")
	require.NoError(t, os.WriteFile(rom, []byte("rom"), 0o600))

	tests := []struct {
		want    *platforms.MediaSource
		name    string
		romPath string
	}{
		{name: "existing file", romPath: rom, want: &platforms.MediaSource{
			Path: rom, Root: dir, Kind: platforms.MediaSourceFile,
		}},
		{name: "quoted path", romPath: `"` + rom + `"`, want: &platforms.MediaSource{
			Path: rom, Root: dir, Kind: platforms.MediaSourceFile,
		}},
		{name: "no rom"},
		{name: "missing file", romPath: filepath.Join(dir, "gone.nes")},
		{name: "directory", romPath: dir},
		{name: "relative path", romPath: "game.nes"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			games := []Game{{ID: idNES, Name: "NES Game", IsInstalled: true, RomPath: tt.romPath}}
			results := ScanResults(games, systemdefs.SystemPC, false)
			require.Len(t, results, 1)
			assert.Equal(t, tt.want, results[0].Source)
		})
	}
}

func TestLauncherDefinition(t *testing.T) {
	t.Parallel()

	h := newHarness(t, harnessOptions{})
	launcher := NewLauncher(h.integration)

	assert.Equal(t, LauncherID, launcher.ID)
	assert.Empty(t, launcher.SystemID, "Playnite holds games for many systems")
	assert.Equal(t, []string{shared.SchemePlaynite}, launcher.Schemes)
	assert.Equal(t, platforms.LifecycleExternal, launcher.Lifecycle)
	assert.NotNil(t, launcher.Kill, "hold mode needs a stop mechanism")
	assert.NotNil(t, launcher.Availability)
	assert.NotNil(t, launcher.Scanner)

	require.NotNil(t, launcher.Test)
	assert.True(t, launcher.Test(nil, GamePath(idPC, "PC Game")))
	assert.False(t, launcher.Test(nil, "playnite://not-a-guid/PC Game"),
		"a malformed path must not select the launcher and stop the running game")
	assert.False(t, launcher.Test(nil, "playnite://"))
}

func TestLauncherDrivesTheIntegration(t *testing.T) {
	t.Parallel()

	h := newHarness(t, harnessOptions{})
	ext := h.connect(fixtureGames())
	launcher := NewLauncher(h.integration)
	path := GamePath(idNES, "NES Game")

	existing := []platforms.ScanResult{{Path: "C:/roms/other.nes", Name: "Other"}}
	results, err := launcher.Scanner(t.Context(), nil, systemdefs.SystemNES, existing)
	require.NoError(t, err)
	require.Len(t, results, 2, "the scanner adds to what was already collected")
	assert.Equal(t, existing[0], results[0])
	assert.Equal(t, path, results[1].Path)

	require.NoError(t, launcher.Availability(nil))

	proc, err := launcher.Launch(nil, path, nil)
	require.NoError(t, err)
	assert.Nil(t, proc, "Playnite owns the game process")
	assert.Equal(t, 1, ext.count(CommandLaunch))
	_, err = launcher.Launch(nil, "playnite://not-a-guid/NES Game", nil)
	require.Error(t, err)

	require.ErrorIs(t, launcher.Kill(nil), ErrNoActiveGame)
	ext.write(started(game(t, idNES), 1, 4242))
	h.requireMediaPath(path)
	require.NoError(t, launcher.Kill(nil))
	assert.Equal(t, 1, ext.count(CommandStop))
}

func TestLauncherScannerKeepsResultsWhenPlayniteIsClosed(t *testing.T) {
	t.Parallel()

	h := newHarness(t, harnessOptions{})
	h.install()
	launcher := NewLauncher(h.integration)

	existing := []platforms.ScanResult{{Path: "C:/roms/other.nes", Name: "Other"}}
	results, err := launcher.Scanner(t.Context(), nil, systemdefs.SystemNES, existing)
	//nolint:errorlint,testifylint // Identity is the contract.
	assert.True(t, err == platforms.ErrScannerUnavailable, "got %v", err)
	assert.Equal(t, existing, results)
}
