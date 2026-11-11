//go:build linux && !android

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package lakka

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	platformids "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/ids"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/retroarch"
	testinghelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/jonboulle/clockwork"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testPlatform(t *testing.T, addr string) (*Platform, *mocks.MockCommandExecutor, *clockwork.FakeClock) {
	t.Helper()
	exec := &mocks.MockCommandExecutor{}
	t.Cleanup(func() { exec.AssertExpectations(t) })
	clock := clockwork.NewFakeClock()
	p := newTestablePlatform(afero.NewMemMapFs(), exec, clock, addr)
	p.frontend.queryTimeout = 500 * time.Millisecond
	return p, exec, clock
}

func testConfig(t *testing.T) *config.Instance {
	t.Helper()
	cfg, err := testinghelpers.NewTestConfig(testinghelpers.NewMemoryFS(), t.TempDir())
	require.NoError(t, err)
	return cfg
}

func TestPlatformIdentity(t *testing.T) {
	t.Parallel()
	p := NewPlatform()
	assert.Equal(t, platformids.Lakka, p.ID())

	settings := p.Settings()
	assert.Equal(t, "/storage/.local/share/zaparoo", settings.DataDir)
	assert.Equal(t, "/storage/.config/zaparoo", settings.ConfigDir)
	assert.Equal(t, "/storage/.local/share/zaparoo/logs", settings.LogDir)
	assert.Equal(t, filepath.Join(os.TempDir(), "zaparoo"), settings.TempDir)
}

func TestRootDirs(t *testing.T) {
	t.Parallel()
	cfg := testConfig(t)
	p := NewPlatform()
	assert.Equal(t, []string{"/storage/roms"}, p.RootDirs(cfg))

	extra := t.TempDir()
	require.NoError(t, cfg.LoadTOML(fmt.Sprintf("[launchers]\nindex_root = [%q, \"/storage/roms/\"]\n", extra)))
	assert.Equal(t, []string{extra, "/storage/roms/"}, p.RootDirs(cfg))
}

func TestCoreProfile(t *testing.T) {
	t.Parallel()
	assert.Equal(t, retroarch.ProfileDesktop, coreProfile("amd64"))
	assert.Equal(t, retroarch.ProfileApplianceARM, coreProfile("arm64"))
	assert.Equal(t, retroarch.ProfileApplianceARM, coreProfile("arm"))
}

func snesLauncher(t *testing.T, launchers []platforms.Launcher) platforms.Launcher {
	t.Helper()
	for i := range launchers {
		if launchers[i].SystemID == systemdefs.SystemSNES && launchers[i].Folders != nil {
			return launchers[i]
		}
	}
	require.FailNow(t, "no SNES launcher")
	return platforms.Launcher{}
}

func snesCore(t *testing.T) string {
	t.Helper()
	for _, c := range retroarch.CoreLaunches(coreProfile(runtime.GOARCH)) {
		if c.SystemID == systemdefs.SystemSNES && c.Scan {
			return c.Core
		}
	}
	require.FailNow(t, "no SNES core")
	return ""
}

func TestLaunchersUseRunningRetroArch(t *testing.T) {
	t.Parallel()
	state := &retroArchState{status: contentless}
	ra := newFakeRetroArch(t, state.handle)
	p, _, _ := testPlatform(t, ra.addr())
	p.frontend.capability = loadSupported
	cfg := testConfig(t)

	launcher := snesLauncher(t, p.Launchers(cfg))
	assert.Equal(t, platforms.LifecycleFireAndForget, launcher.Lifecycle)
	assert.Nil(t, launcher.Kill)
	assert.NotNil(t, launcher.GameRunning)
	assert.NotNil(t, launcher.WaitForReady)
	require.Contains(t, launcher.Controls, platforms.ControlStop)
	require.Contains(t, launcher.Controls, platforms.ControlSaveState)

	require.Error(t, launcher.Availability(cfg), "core missing")
	core := filepath.Join(retroArchCoresDir, snesCore(t))
	require.NoError(t, afero.WriteFile(p.fs, retroArchExecutable, nil, 0o755))
	require.NoError(t, afero.WriteFile(p.fs, core, nil, 0o644))
	require.NoError(t, launcher.Availability(cfg))

	proc, err := launcher.Launch(cfg, testContent, &platforms.LaunchOptions{})
	require.NoError(t, err)
	assert.Nil(t, proc)
	require.Eventually(t, func() bool {
		return ra.count(commandLoadContent+" "+core+"|"+testContent) == 1
	}, time.Second, time.Millisecond)
	assert.Equal(t, testContent, p.frontend.currentLaunch())
}

func TestStopControlClosesContent(t *testing.T) {
	t.Parallel()
	state := &retroArchState{status: contentless}
	ra := newFakeRetroArch(t, state.handle)
	p, exec, _ := testPlatform(t, ra.addr())
	expectSystemctl(exec, errInactive, "is-active", "--quiet", transientUnit)

	stop := p.controls()[platforms.ControlStop]
	require.NoError(t, stop.Func(context.Background(), testConfig(t), platforms.ControlParams{}))
	assert.Zero(t, ra.count("QUIT"))
}

func TestStopActiveLauncherKeepsMediaWhenStopFails(t *testing.T) {
	t.Parallel()
	state := &retroArchState{status: playingGame}
	ra := newFakeRetroArch(t, state.handle)
	p, exec, clock := testPlatform(t, ra.addr())
	expectSystemctl(exec, errInactive, "is-active", "--quiet", transientUnit).Twice()
	expectSystemctl(exec, nil, "is-active", "--quiet", retroArchService)

	holder := &mediaHolder{}
	media := &models.ActiveMedia{Path: testContent}
	holder.set(media)
	require.NoError(t, p.StartPost(context.Background(), testConfig(t), nil, holder.get, holder.set, nil, nil))
	t.Cleanup(func() { _ = p.Stop() })

	var err error
	drive(t, clock, pollInterval, func() { err = p.StopActiveLauncher(platforms.StopForMenu) })
	require.ErrorIs(t, err, platforms.ErrStopFailed)
	assert.Same(t, media, holder.get())
}

func TestResolveCoresUsesInstalledCoreNames(t *testing.T) {
	t.Parallel()
	p, _, _ := testPlatform(t, closedAddr(t))
	cores := []string{"FreeIntv_libretro.so", "citra_libretro.so", "panda3ds_libretro.so", "snes9x_libretro.so"}
	for _, core := range cores {
		require.NoError(t, afero.WriteFile(p.fs, filepath.Join(retroArchCoresDir, core), nil, 0o644))
	}

	launches := p.resolveCores([]retroarch.CoreLaunch{
		{SystemID: systemdefs.SystemIntellivision, Core: "freeintv_libretro.so"},
		{SystemID: systemdefs.System3DS, Core: "azahar_libretro.so"},
		{SystemID: systemdefs.SystemPS2, Core: "pcsx2_libretro.so"},
		{SystemID: systemdefs.SystemSNES, Core: "snes9x_libretro.so"},
	})
	assert.Equal(t, "FreeIntv_libretro.so", launches[0].Core, "case differs on disk")
	assert.Equal(t, "citra_libretro.so", launches[1].Core, "first installed alternate")
	assert.Equal(t, "pcsx2_libretro.so", launches[2].Core, "missing core with no installed alternate is kept")
	assert.Equal(t, "snes9x_libretro.so", launches[3].Core)
}

func TestResolveCoresWithoutCoresDir(t *testing.T) {
	t.Parallel()
	p, _, _ := testPlatform(t, closedAddr(t))
	launches := []retroarch.CoreLaunch{{SystemID: systemdefs.System3DS, Core: "azahar_libretro.so"}}
	assert.Equal(t, "azahar_libretro.so", p.resolveCores(launches)[0].Core)
}
