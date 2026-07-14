//go:build linux

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package steamos

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	platformshared "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/launchers"
	sharedretroarch "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/retroarch"
	testinghelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSteamOSRetroArchOptions(t *testing.T) {
	t.Parallel()

	appendPath := filepath.Join("config", retroArchNativeConfig)
	opts := steamOSRetroArchOptions(appendPath)

	assert.Equal(t, []string{"flatpak", "run", RetroArchFlatpakID}, opts.Exec)
	assert.Equal(t, filepath.Join(
		launchers.FlatpakAppPath(RetroArchFlatpakID), "config", "retroarch", "cores",
	), opts.CoresDir)
	assert.Equal(t, appendPath, opts.AppendConfigPath)
	assert.Equal(t, retroArchNetworkAddr, opts.NetworkCmdAddr)
	assert.NotNil(t, opts.LaunchEnv)
	assert.NotNil(t, opts.Preflight)
}

func TestEnsureSharedRetroArchNetworkConfig(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	path := filepath.Join("config", "zaparoo", retroArchNativeConfig)

	require.NoError(t, sharedretroarch.EnsureNetworkCommandConfig(fs, path))
	data, err := afero.ReadFile(fs, path)
	require.NoError(t, err)
	assert.Equal(t, sharedretroarch.NetworkCommandConfig, string(data))
	info, err := fs.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestPlatformStartPreWritesRetroArchConfig(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	path := filepath.Join("config", retroArchNativeConfig)
	platform := NewPlatform()
	platform.fs = fs
	platform.retroArchAppendConfigPath = path

	require.NoError(t, platform.StartPre(nil))
	data, err := afero.ReadFile(fs, path)
	require.NoError(t, err)
	expected, err := sharedretroarch.ConfigForProfile(sharedretroarch.ConfigProfileLowLatency)
	require.NoError(t, err)
	assert.Equal(t, expected, string(data))
}

func TestNativeRetroArchLaunchers(t *testing.T) {
	t.Parallel()

	opts := sharedretroarch.Options{NetworkCmdAddr: retroArchNetworkAddr}
	nativeLaunchers := nativeRetroArchLaunchers(&opts)
	assert.Len(t, nativeLaunchers, len(sharedretroarch.CoreLaunches(sharedretroarch.ProfileDesktop)))
	require.NotEmpty(t, nativeLaunchers)
	assert.Equal(t, platforms.LifecycleBlocking, nativeLaunchers[0].Lifecycle)
	assert.Len(t, nativeLaunchers[0].Controls, 8)
	assert.Contains(t, nativeLaunchers[0].Groups, platformshared.LauncherGroupNative)
	assert.NotNil(t, nativeLaunchers[0].Kill)
}

func TestEmulatorMappingUsesSharedRetroArchCore(t *testing.T) {
	t.Parallel()

	shared, ok := sharedretroarch.CoreLaunchForFolder(sharedretroarch.ProfileDesktop, "snes")
	require.True(t, ok)
	emuDeck, ok := emulatorMapping["snes"]
	require.True(t, ok)
	assert.Equal(t, EmulatorRetroArch, emuDeck.Type)
	assert.Equal(t, strings.TrimSuffix(shared.Core, ".so"), emuDeck.Core)
}

func TestPlatformRootDirs(t *testing.T) {
	t.Parallel()

	fs := testinghelpers.NewMemoryFS()
	cfg, err := testinghelpers.NewTestConfig(fs, t.TempDir())
	require.NoError(t, err)
	platform := NewPlatform()
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	assert.Equal(t, []string{filepath.Join(home, "ROMs")}, platform.RootDirs(cfg))

	root := t.TempDir()
	require.NoError(t, cfg.LoadTOML(fmt.Sprintf("[launchers]\nindex_root = [%q]\n", root)))
	assert.Equal(t, []string{root}, platform.RootDirs(cfg))
}
