//go:build linux && !android

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

package muos

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func launcherByID(t *testing.T, launchers []platforms.Launcher, id string) *platforms.Launcher {
	t.Helper()
	for i := range launchers {
		if launchers[i].ID == id {
			return &launchers[i]
		}
	}
	t.Fatalf("launcher %s not found", id)
	return nil
}

func TestPlatformLaunchersFromROMFolders(t *testing.T) {
	t.Parallel()
	fs := newTestFs(t)
	p, _, _ := newTestPlatform(t, fs)
	for _, dir := range []string{
		filepath.Join(testROMs, "GBA"),
		filepath.Join(testROMs, "Game Boy Advance"),
		filepath.Join(testSDROMs, "GBA"),
		filepath.Join(testSDROMs, "Megadrive"),
		filepath.Join(testROMs, "Homebrew"),
		filepath.Join(testROMs, "My Sega"),
		filepath.Join(testROMs, ".hidden"),
	} {
		require.NoError(t, fs.MkdirAll(dir, 0o755))
	}
	writeFile(t, fs, filepath.Join(testROMs, "notes.txt"), "x")
	writeFile(t, fs, filepath.Join(infoContentDir, "My Sega", "core.cfg"),
		"picodrive_libretro.so\nSega Mega Drive - Genesis\nSega Mega Drive - Genesis\n0\npicodrive\n")

	launchers := p.platformLaunchers()

	gba := launcherByID(t, launchers, systemdefs.SystemGBA)
	assert.Equal(t, systemdefs.SystemGBA, gba.SystemID)
	assert.Equal(t, []string{"GBA", "Game Boy Advance"}, gba.Folders)
	assert.Contains(t, gba.Extensions, ".gba")
	assert.Equal(t, runningInstance, gba.UsesRunningInstance)
	assert.NotNil(t, gba.Launch)
	assert.NotNil(t, gba.WaitForReady)
	assert.NotNil(t, gba.GameRunning)
	assert.Nil(t, gba.Test)

	md := launcherByID(t, launchers, systemdefs.SystemGenesis)
	assert.Equal(t, []string{"Megadrive", "My Sega"}, md.Folders)

	generic := launcherByID(t, launchers, "Generic")
	assert.True(t, generic.AllowListOnly)
	assert.Len(t, launchers, 3, "unassigned and hidden folders have no launcher")
}

func TestPlatformLaunchersCache(t *testing.T) {
	t.Parallel()
	fs := newTestFs(t)
	p, _, _ := newTestPlatform(t, fs)
	clock, ok := p.clock.(*clockwork.FakeClock)
	require.True(t, ok)
	require.NoError(t, fs.MkdirAll(filepath.Join(testROMs, "GBA"), 0o755))
	assert.Len(t, p.platformLaunchers(), 2)

	require.NoError(t, fs.MkdirAll(filepath.Join(testROMs, "MD"), 0o755))
	assert.Len(t, p.platformLaunchers(), 2, "cached")

	clock.Advance(cacheTTL + 1)
	assert.Len(t, p.platformLaunchers(), 3)
}

func TestLaunchersCombinesWithConfig(t *testing.T) {
	t.Parallel()
	cfg, err := helpers.NewTestConfig(helpers.NewMemoryFS(), t.TempDir())
	require.NoError(t, err)
	fs := newTestFs(t)
	p, _, _ := newTestPlatform(t, fs)
	require.NoError(t, fs.MkdirAll(filepath.Join(testROMs, "GBA"), 0o755))

	launchers := p.Launchers(cfg)
	launcherByID(t, launchers, systemdefs.SystemGBA)
	launcherByID(t, launchers, "Generic")
}

func TestGenericLauncherRunsScript(t *testing.T) {
	t.Parallel()
	fs := newTestFs(t)
	p, _, _ := newTestPlatform(t, fs)
	cmd := &mocks.MockCommandExecutor{}
	cmd.On("Start", mock.Anything, "/mnt/mmc/run.sh", []string(nil)).Return(nil)
	p.cmd = cmd

	generic := launcherByID(t, p.platformLaunchers(), "Generic")
	proc, err := generic.Launch(nil, "/mnt/mmc/run.sh", nil)
	require.NoError(t, err)
	assert.Nil(t, proc)
	cmd.AssertExpectations(t)
}

func TestSystemExtensionsWithoutESDEFolders(t *testing.T) {
	t.Parallel()
	assert.Empty(t, systemExtensions("NoSuchSystem"))
	assert.Contains(t, systemExtensions(systemdefs.SystemGenesis), ".md")
}

func TestIsPlainFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	file := filepath.Join(dir, "game.bin")
	require.NoError(t, os.WriteFile(file, []byte("x"), 0o600))
	hidden := filepath.Join(dir, ".game.bin")
	require.NoError(t, os.WriteFile(hidden, []byte("x"), 0o600))

	assert.True(t, isPlainFile(nil, file))
	assert.False(t, isPlainFile(nil, hidden))
	assert.False(t, isPlainFile(nil, dir))
	assert.False(t, isPlainFile(nil, filepath.Join(dir, "missing")))
}

func TestLaunchersWithoutExtensionsTestForFiles(t *testing.T) {
	t.Parallel()
	fs := newTestFs(t)
	require.NoError(t, fs.MkdirAll(filepath.Join(testROMs, "OpenBOR"), 0o755))
	require.NoError(t, fs.MkdirAll(filepath.Join(testROMs, "GBA"), 0o755))
	writeFile(t, fs, filepath.Join(infoContentDir, "OpenBOR", "core.cfg"), "openbor\nOpenBOR\n")
	p, _, _ := newTestPlatform(t, fs)

	openbor := launcherByID(t, p.platformLaunchers(), systemdefs.SystemOpenBOR)
	assert.Empty(t, openbor.Extensions)
	require.NotNil(t, openbor.Test)
	gba := launcherByID(t, p.platformLaunchers(), systemdefs.SystemGBA)
	assert.Nil(t, gba.Test)
}

func TestGenericLauncherReportsStartFailure(t *testing.T) {
	t.Parallel()
	p, _, _ := newTestPlatform(t, newTestFs(t))
	cmd := &mocks.MockCommandExecutor{}
	cmd.On("Start", mock.Anything, "/mnt/mmc/run.sh", []string(nil)).Return(os.ErrPermission)
	p.cmd = cmd

	generic := launcherByID(t, p.platformLaunchers(), "Generic")
	_, err := generic.Launch(nil, "/mnt/mmc/run.sh", nil)
	require.ErrorIs(t, err, os.ErrPermission)
}
