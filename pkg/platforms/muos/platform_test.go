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
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	corehelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	platformids "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/ids"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

var _ platforms.Platform = (*Platform)(nil)

func TestSettingsUseSDCard(t *testing.T) {
	t.Parallel()
	fs := newTestFs(t)
	p, _, _ := newTestPlatform(t, fs)

	s := p.Settings()
	assert.Equal(t, "/mnt/mmc/MUOS/zaparoo", s.DataDir)
	assert.Equal(t, "/mnt/mmc/MUOS/zaparoo", s.ConfigDir)
	assert.Equal(t, "/mnt/mmc/MUOS/zaparoo/"+config.LogsDir, s.LogDir)

	writeFile(t, fs, romMountFile, "/media/card\n")
	assert.Equal(t, "/media/card/MUOS/zaparoo", p.Settings().DataDir)
}

func TestRootDirs(t *testing.T) {
	t.Parallel()
	cfg, err := helpers.NewTestConfig(helpers.NewMemoryFS(), t.TempDir())
	require.NoError(t, err)
	fs := newTestFs(t)
	writeFile(t, fs, sdcardMountFile, "/mnt/mmc\n")
	p, _, _ := newTestPlatform(t, fs)

	assert.Equal(t, []string{"/mnt/usb/ROMS", "/mnt/mmc/ROMS"}, p.RootDirs(cfg))
}

func TestTrivialReturns(t *testing.T) {
	t.Parallel()
	p, _, _ := newTestPlatform(t, newTestFs(t))

	assert.Equal(t, platformids.MuOS, p.ID())
	require.ErrorIs(t, p.LaunchSystem(nil, "GBA"), platforms.ErrNotSupported)
	_, err := p.Screenshot()
	require.ErrorIs(t, err, platforms.ErrNotSupported)
	_, err = p.ForwardCmd(nil)
	require.ErrorIs(t, err, platforms.ErrNotSupported)
	_, ok := p.LookupMapping(nil)
	assert.False(t, ok)
	require.NoError(t, p.ScanHook(nil))
	assert.False(t, p.ManagedByPackageManager())
	assert.Equal(t, platforms.NoOpConsoleManager{}, p.ConsoleManager())
	assert.Len(t, p.Scrapers(nil), 4)
	p.SetTrackedProcess(nil)
}

func TestZeroValuePlatformDefaults(t *testing.T) {
	t.Parallel()
	p := &Platform{}
	assert.NotNil(t, p.filesystem())
	assert.NotNil(t, p.getClock())
	assert.NotNil(t, p.cmdExec())
	assert.Equal(t, "/proc", p.procDir())
	assert.Positive(t, p.selfPID())
	assert.NotEmpty(t, p.muArch())
	p.stopTracker()
}

func TestCoreDBReloadsAfterTTL(t *testing.T) {
	t.Parallel()
	fs := newTestFs(t)
	p, _, _ := newTestPlatform(t, fs)
	first := p.coreDB()
	assert.Same(t, first, p.coreDB())
	p.dbLoaded = p.dbLoaded.Add(-2 * cacheTTL)
	assert.NotSame(t, first, p.coreDB())
}

func TestRealSignalAndSleep(t *testing.T) {
	t.Parallel()
	p := &Platform{}
	require.NoError(t, p.sendSignal(os.Getpid(), 0), "signal 0 only checks the process exists")

	gone := exec.CommandContext(context.Background(), "true")
	require.NoError(t, gone.Run())
	require.ErrorIs(t, p.sendSignal(gone.Process.Pid, 0), syscall.ESRCH)

	p.pause(0)
}

func TestSupportedReaders(t *testing.T) {
	t.Parallel()
	cfg, err := helpers.NewTestConfig(helpers.NewMemoryFS(), t.TempDir())
	require.NoError(t, err)
	p, _, _ := newTestPlatform(t, newTestFs(t))
	assert.NotEmpty(t, p.SupportedReaders(cfg))
}

func TestStartPreSurvivesMissingInputDevices(t *testing.T) {
	t.Parallel()
	cfg, err := helpers.NewTestConfig(helpers.NewMemoryFS(), t.TempDir())
	require.NoError(t, err)
	p, _, _ := newTestPlatform(t, newTestFs(t))
	p.NewKeyboard = func() (shared.KeyboardDevice, error) { return nil, errInjected }

	require.NoError(t, p.StartPre(cfg))
}

func TestStartPostRunsTrackerUntilStop(t *testing.T) {
	t.Parallel()
	fs := newTestFs(t)
	p, _, _ := newTestPlatform(t, fs)
	media := &mediaRecorder{}

	require.NoError(t, p.StartPost(context.Background(), nil, nil, media.get, media.set, nil, nil))
	assert.NotNil(t, p.cancel)

	require.NoError(t, p.Stop())
	assert.Nil(t, p.cancel)
	assert.Nil(t, p.trackerDone)
}

func TestRootDirsDedupesIndexRoots(t *testing.T) {
	t.Parallel()
	cfg, err := helpers.NewTestConfig(helpers.NewMemoryFS(), t.TempDir())
	require.NoError(t, err)
	require.NoError(t, cfg.LoadTOML("[launchers]\nindex_root = [\"/mnt/mmc/ROMS/\", \"/media/extra\"]\n"))
	p, _, _ := newTestPlatform(t, newTestFs(t))

	assert.Equal(t, []string{"/mnt/mmc/ROMS", "/media/extra", "/mnt/usb/ROMS", "/mnt/sdcard/ROMS"}, p.RootDirs(cfg))
}

func TestStopActiveLauncherCannotWithdrawRequest(t *testing.T) {
	t.Parallel()
	base := newTestFs(t)
	writeFile(t, base, contentFile, "pending")
	p, _, media := newTestPlatform(t, &failFs{Fs: base, op: "remove", path: contentFile})
	media.set(&models.ActiveMedia{Path: "/g.gba"})

	require.NoError(t, p.StopActiveLauncher(platforms.StopForMenu))
	assert.Nil(t, media.get(), "a stuck request does not block the stop")
}

func TestLaunchMedia(t *testing.T) {
	t.Parallel()
	cfg, err := helpers.NewTestConfig(helpers.NewMemoryFS(), t.TempDir())
	require.NoError(t, err)
	fs := newTestFs(t)
	rom := filepath.Join(testROMs, "GBA", "Game.gba")
	writeFile(t, fs, rom, "rom")
	p, _, _ := newTestPlatform(t, fs)

	require.ErrorContains(t, p.LaunchMedia(cfg, "/nowhere/file.unknownext", nil, nil, nil), "error finding launcher")

	cmd := &mocks.MockCommandExecutor{}
	cmd.On("Start", mock.Anything, "/mnt/mmc/ok.sh", []string(nil)).Return(nil)
	cmd.On("Start", mock.Anything, "/mnt/mmc/bad.sh", []string(nil)).Return(os.ErrPermission)
	p.cmd = cmd
	generic := launcherByID(t, p.platformLaunchers(), "Generic")

	require.NoError(t, p.LaunchMedia(cfg, "/mnt/mmc/ok.sh", generic, nil, nil))
	err = p.LaunchMedia(cfg, "/mnt/mmc/bad.sh", generic, nil, nil)
	require.ErrorIs(t, err, os.ErrPermission)
	require.ErrorContains(t, err, "error launching")
	cmd.AssertExpectations(t)
}

func TestLaunchMediaFindsLauncherForPath(t *testing.T) {
	// Not parallel: mutates the shared GlobalLauncherCache.
	cfg, err := helpers.NewTestConfig(helpers.NewMemoryFS(), t.TempDir())
	require.NoError(t, err)
	fs := newTestFs(t)
	rom := filepath.Join(testROMs, "GBA", "Game.gba")
	writeFile(t, fs, rom, "rom")
	p, _, _ := newTestPlatform(t, fs)
	oldCache := corehelpers.GlobalLauncherCache
	corehelpers.GlobalLauncherCache = &corehelpers.LauncherCache{}
	t.Cleanup(func() { corehelpers.GlobalLauncherCache = oldCache })
	corehelpers.GlobalLauncherCache.InitializeFromSlice(p.Launchers(cfg))

	err = p.LaunchMedia(cfg, rom, nil, nil, nil)
	require.ErrorIs(t, err, errNotPickedUp, "the GBA launcher was picked and handed the ROM to muOS")
	require.ErrorContains(t, err, "error launching")
}
