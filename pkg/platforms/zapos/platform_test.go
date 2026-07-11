//go:build linux

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package zapos

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	platformids "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/ids"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/retroarch"
	testinghelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewPlatform(t *testing.T) {
	t.Parallel()

	platform := NewPlatform()

	require.NotNil(t, platform.Base)
	assert.Equal(t, platformids.ZapOS, platform.ID())
}

func TestSettings(t *testing.T) {
	t.Parallel()

	settings := NewPlatform().Settings()
	dataDir := userdataPath("data", "zaparoo")
	assert.Equal(t, userdataPath("config", "zaparoo"), settings.ConfigDir)
	assert.Equal(t, dataDir, settings.DataDir)
	assert.Equal(t, filepath.Join(dataDir, "logs"), settings.LogDir)
	assert.Equal(t, filepath.Join(os.TempDir(), "zaparoo"), settings.TempDir)
}

func TestRootDirs(t *testing.T) {
	t.Parallel()

	fs := testinghelpers.NewMemoryFS()
	cfg, err := testinghelpers.NewTestConfig(fs, t.TempDir())
	require.NoError(t, err)
	platform := NewPlatform()
	assert.Equal(t, []string{userdataPath("media")}, platform.RootDirs(cfg))

	configuredRoot := t.TempDir()
	require.NoError(t, cfg.LoadTOML(fmt.Sprintf("[launchers]\nindex_root = [%q]\n", configuredRoot)))
	assert.Equal(t, []string{configuredRoot}, platform.RootDirs(cfg))
}

func TestLaunchersCustomFirst(t *testing.T) {
	t.Parallel()

	fs := testinghelpers.NewMemoryFS()
	cfg, err := testinghelpers.NewTestConfig(fs, t.TempDir())
	require.NoError(t, err)
	require.NoError(t, cfg.LoadTOML(`
[[launchers.custom]]
id = "CustomPSX"
system = "PSX"
media_dirs = ["psx"]
file_exts = [".chd"]
execute = "echo [[media_path]]"
`))

	launchers := NewPlatform().Launchers(cfg)

	require.Len(t, launchers, len(retroarch.CoreLaunches(retroarch.ProfileApplianceARM))+1)
	assert.Equal(t, "CustomPSX", launchers[0].ID)
	assert.Equal(t, "RetroArchOpera", launchers[1].ID)
}

func TestApplianceRetroArchOptions(t *testing.T) {
	t.Parallel()

	opts := applianceRetroArchOptions()
	pack := userdataPath("runtime", "retroarch")
	assert.Equal(t, []string{filepath.Join(pack, "retroarch")}, opts.Exec)
	assert.Equal(t, []string{"openvt", "-c", "2", "-s", "-w", "--"}, opts.VTWrap)
	assert.Equal(t, filepath.Join(pack, "cores"), opts.CoresDir)
	assert.Equal(t, filepath.Join(pack, "retroarch.cfg"), opts.ConfigPath)
	assert.Equal(t, filepath.Join(pack, "lib"), opts.LibDir)
	assert.Equal(t, userdataPath("home"), opts.Home)
	logPath := filepath.Join(userdataPath("data", "zaparoo"), "logs", "retroarch.log")
	assert.Equal(t, []string{"-v", "--log-file", logPath}, opts.ExtraArgs)
	assert.Equal(t, retroArchNetworkAddr, opts.NetworkCmdAddr)
}

func TestAppliancePSXCommand(t *testing.T) {
	t.Parallel()

	psx, ok := retroarch.CoreLaunchForFolder(retroarch.ProfileApplianceARM, "psx")
	require.True(t, ok)
	mediaPath := userdataPath("media", "psx", "game.chd")
	spec := retroarch.BuildCommand(applianceRetroArchOptions(), psx, mediaPath)
	pack := userdataPath("runtime", "retroarch")

	assert.Equal(t, "openvt", spec.Name)
	assert.Equal(t, []string{
		"-c", "2", "-s", "-w", "--",
		filepath.Join(pack, "retroarch"),
		"-v", "--log-file", filepath.Join(userdataPath("data", "zaparoo"), "logs", "retroarch.log"),
		"--config", filepath.Join(pack, "retroarch.cfg"),
		"-L", filepath.Join(pack, "cores", "pcsx_rearmed_libretro.so"),
		mediaPath,
	}, spec.Args)
	assert.Equal(t, []string{
		"HOME=" + userdataPath("home"),
		"LD_LIBRARY_PATH=" + filepath.Join(pack, "lib"),
	}, spec.Env)
}
