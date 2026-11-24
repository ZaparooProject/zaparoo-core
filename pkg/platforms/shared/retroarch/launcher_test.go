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

package retroarch

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	testinghelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewLauncher(t *testing.T) {
	t.Parallel()

	c := CoreLaunch{
		ID:         "RetroArchPCSXReARMed",
		SystemID:   "PSX",
		Core:       "pcsx_rearmed_libretro.so",
		Folders:    []string{"psx"},
		Extensions: []string{".chd"},
		Scan:       true,
	}
	fs := afero.NewMemMapFs()
	root := filepath.Join(t.TempDir(), "runtime")
	executable := filepath.Join(root, "retroarch")
	coresDir := filepath.Join(root, "cores")
	configPath := filepath.Join(root, "retroarch.cfg")
	appendConfigPath := filepath.Join(root, "zaparoo.cfg")
	corePath := filepath.Join(coresDir, c.Core)
	mediaPath := filepath.Join(root, "games", "game.chd")
	require.NoError(t, fs.MkdirAll(coresDir, 0o750))
	require.NoError(t, afero.WriteFile(fs, executable, []byte("binary"), 0o750))
	require.NoError(t, afero.WriteFile(fs, corePath, []byte("core"), 0o600))
	launcher := NewLauncher(Options{
		FS: fs, NetworkCmdAddr: "127.0.0.1:55355", Exec: []string{executable}, CoresDir: coresDir,
		ConfigPath: configPath, AppendConfigPath: appendConfigPath, ExtraArgs: []string{"--verbose"},
		ExtraEnv: []string{"BASE=1"}, LaunchEnv: func() []string { return []string{"DISPLAY=:1", "BASE=2"} },
	}, c)

	assert.Equal(t, c.ID, launcher.ID)
	assert.Equal(t, c.SystemID, launcher.SystemID)
	assert.Equal(t, c.Folders, launcher.Folders)
	assert.Equal(t, c.Extensions, launcher.Extensions)
	assert.Equal(t, platforms.LifecycleBlocking, launcher.Lifecycle)
	assert.Len(t, launcher.Controls, 13)
	assert.Contains(t, launcher.Controls, platforms.ControlToggleTray)
	assert.Contains(t, launcher.Controls, platforms.ControlNext)
	assert.Contains(t, launcher.Controls, platforms.ControlPrevious)
	assert.NotNil(t, launcher.Kill)
	command, err := launcher.BuildLaunchCommand(nil, mediaPath, nil)
	require.NoError(t, err)
	assert.Equal(t, executable, command.Executable)
	assert.Equal(t, []string{
		"--verbose",
		"--config", configPath,
		"--appendconfig", appendConfigPath,
		"-L", corePath,
		mediaPath,
	}, command.Args)
	assert.Empty(t, command.Dir)
	assert.Equal(t, []string{"BASE=2", "DISPLAY=:1"}, command.Env)

	c.Folders[0] = "changed"
	c.Extensions[0] = ".changed"
	assert.Equal(t, []string{"psx"}, launcher.Folders)
	assert.Equal(t, []string{".chd"}, launcher.Extensions)
}

func TestNewLaunchersPreservesOrder(t *testing.T) {
	t.Parallel()

	cores := []CoreLaunch{
		{ID: "first", Core: "first_libretro.so"},
		{ID: "second", Core: "second_libretro.so"},
	}

	launchers := NewLaunchers(Options{}, cores)

	require.Len(t, launchers, 2)
	assert.Equal(t, "first", launchers[0].ID)
	assert.Equal(t, "second", launchers[1].ID)
}

func TestResolveCoreUsesLoadPathOverride(t *testing.T) {
	t.Parallel()

	fs := testinghelpers.NewMemoryFS()
	cfg, err := testinghelpers.NewTestConfig(fs, t.TempDir())
	require.NoError(t, err)
	require.NoError(t, cfg.LoadTOML(`
[[launchers.default]]
launcher = "RetroArchBSNES"
load_path = "snes9x"
`))

	coreLaunch := CoreLaunch{ID: "RetroArchBSNES", Core: "bsnes_libretro.so"}
	core, err := resolveCore(cfg, &coreLaunch, false)

	require.NoError(t, err)
	assert.Equal(t, "snes9x_libretro.so", core)
}

func TestResolveCoreUsesDLLsOnWindows(t *testing.T) {
	t.Parallel()

	fs := testinghelpers.NewMemoryFS()
	cfg, err := testinghelpers.NewTestConfig(fs, t.TempDir())
	require.NoError(t, err)

	coreLaunch := CoreLaunch{ID: "RetroArchSNES9x", Core: "snes9x_libretro.so"}
	core, err := resolveCore(cfg, &coreLaunch, true)
	require.NoError(t, err)
	assert.Equal(t, "snes9x_libretro.dll", core)

	require.NoError(t, cfg.LoadTOML(`
[[launchers.default]]
launcher = "RetroArchSNES9x"
load_path = "bsnes_libretro.dll"
`))
	core, err = resolveCore(cfg, &coreLaunch, true)
	require.NoError(t, err)
	assert.Equal(t, "bsnes_libretro.dll", core)

	_, err = resolveCore(cfg, &coreLaunch, false)
	require.Error(t, err, "DLL cores are only accepted on Windows")
}

func TestWindowsLauncherSkipsExecuteBitAndBuildsDLLCommand(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	exe := filepath.Join(string(filepath.Separator), "RetroArch", "retroarch.exe")
	cores := filepath.Join(string(filepath.Separator), "RetroArch", "cores")
	require.NoError(t, afero.WriteFile(fs, exe, []byte("MZ"), 0o600))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(cores, "snes9x_libretro.dll"), []byte("MZ"), 0o600))

	launcher := NewLauncher(Options{
		FS: fs, Exec: []string{exe}, CoresDir: cores, ExtraArgs: []string{"--fullscreen"}, Windows: true,
	}, CoreLaunch{ID: "RetroArchSNES9x", Core: "snes9x_libretro.so"})
	require.NoError(t, launcher.Availability(nil))
	command, err := launcher.BuildLaunchCommand(nil, filepath.Join(string(filepath.Separator), "game.sfc"), nil)
	require.NoError(t, err)
	assert.Equal(t, exe, command.Executable)
	assert.Equal(t, []string{
		"--fullscreen", "-L", filepath.Join(cores, "snes9x_libretro.dll"),
		filepath.Join(string(filepath.Separator), "game.sfc"),
	}, command.Args)
}

func TestNormalizeCoreFilename(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "stem", input: "mgba", want: "mgba_libretro.so"},
		{name: "libretro stem", input: "mgba_libretro", want: "mgba_libretro.so"},
		{name: "filename", input: "mgba_libretro.so", want: "mgba_libretro.so"},
		{name: "empty", input: "", wantErr: true},
		{name: "absolute", input: filepath.Join(string(filepath.Separator), "tmp", "mgba_libretro.so"), wantErr: true},
		{name: "traversal", input: filepath.Join("..", "mgba_libretro.so"), wantErr: true},
		{name: "wrong extension", input: "mgba.dll", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := normalizeCoreFilename(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestLauncherPreflightErrorsBeforeExec(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	root := filepath.Join(t.TempDir(), "runtime")
	binary := filepath.Join(root, "retroarch")
	coresDir := filepath.Join(root, "cores")
	require.NoError(t, fs.MkdirAll(coresDir, 0o750))
	require.NoError(t, afero.WriteFile(fs, binary, []byte("binary"), 0o750))

	preflightErr := errors.New("flatpak missing")
	launcher := NewLauncher(Options{
		FS:       fs,
		Exec:     []string{binary},
		CoresDir: coresDir,
		Preflight: func(_ string) error {
			return preflightErr
		},
	}, CoreLaunch{ID: "RetroArchMesen", Core: "mesen_libretro.so"})

	proc, err := launcher.Launch(&config.Instance{}, "game.nes", nil)

	assert.Nil(t, proc)
	require.Error(t, err)
	assert.ErrorIs(t, err, preflightErr)
}

func TestLauncherReportsMissingCore(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	root := filepath.Join(t.TempDir(), "runtime")
	binary := filepath.Join(root, "retroarch")
	require.NoError(t, fs.MkdirAll(root, 0o750))
	require.NoError(t, afero.WriteFile(fs, binary, []byte("binary"), 0o750))

	launcher := NewLauncher(Options{
		FS:       fs,
		Exec:     []string{binary},
		CoresDir: filepath.Join(root, "cores"),
	}, CoreLaunch{ID: "RetroArchMesen", Core: "mesen_libretro.so"})

	proc, err := launcher.Launch(&config.Instance{}, "game.nes", nil)

	assert.Nil(t, proc)
	var repair *platforms.LaunchRepairError
	require.ErrorAs(t, err, &repair)
	assert.Equal(t, platforms.LaunchRepairLauncherComponentMissing, repair.Reason())
	assert.Equal(t, "RetroArch", repair.Params()[platforms.LaunchRepairParamLauncher])
	assert.Equal(t, "the RetroArch core for this system is not installed", repair.Error())
	assert.NotContains(t, repair.Error(), root)
}

func TestLauncherReportsMissingExecutable(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	root := filepath.Join(t.TempDir(), "runtime")
	launcher := NewLauncher(Options{
		FS:       fs,
		Exec:     []string{filepath.Join(root, "retroarch")},
		CoresDir: filepath.Join(root, "cores"),
	}, CoreLaunch{ID: "RetroArchMesen", Core: "mesen_libretro.so"})

	proc, err := launcher.Launch(&config.Instance{}, "game.nes", nil)

	assert.Nil(t, proc)
	var repair *platforms.LaunchRepairError
	require.ErrorAs(t, err, &repair)
	assert.Equal(t, platforms.LaunchRepairLauncherNotInstalled, repair.Reason())
	assert.Equal(t, "RetroArch", repair.Params()[platforms.LaunchRepairParamLauncher])
	assert.Equal(t, "RetroArch is not installed", repair.Error())
}

// statFailFs fails every stat, as an unreadable mount would.
type statFailFs struct {
	afero.Fs
}

var errStatFault = errors.New("injected stat fault")

func (statFailFs) Stat(string) (os.FileInfo, error) { return nil, errStatFault }

// A stat failure other than a missing file is not reported as "not
// installed": the cause is kept for the log and errors.Is.
func TestLauncherKeepsStatFailuresThatAreNotMissingFiles(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "runtime")
	executable := NewLauncher(Options{
		FS:       statFailFs{Fs: afero.NewMemMapFs()},
		Exec:     []string{filepath.Join(root, "retroarch")},
		CoresDir: filepath.Join(root, "cores"),
	}, CoreLaunch{ID: "RetroArchMesen", Core: "mesen_libretro.so"})
	_, err := executable.Launch(&config.Instance{}, "game.nes", nil)
	require.ErrorIs(t, err, errStatFault)
	assert.Contains(t, err.Error(), "failed to stat retroarch executable")

	core := NewLauncher(Options{
		FS:       statFailFs{Fs: afero.NewMemMapFs()},
		Exec:     []string{"retroarch"},
		CoresDir: filepath.Join(root, "cores"),
	}, CoreLaunch{ID: "RetroArchMesen", Core: "mesen_libretro.so"})
	_, err = core.Launch(&config.Instance{}, "game.nes", nil)
	require.ErrorIs(t, err, errStatFault)
	assert.Contains(t, err.Error(), "failed to stat retroarch core")
}
