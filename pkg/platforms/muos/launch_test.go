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
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/jonboulle/clockwork"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var runtimeFiles = []string{
	"/run/muos/controller", "/run/muos/filter", overlayFile, "/run/muos/overlay_options",
	"/run/muos/retroarch", "/run/muos/shader",
}

// frontendLoader stands in for the muOS frontend loop: when the given menu
// module is closed it reads and consumes the content request.
func frontendLoader(t *testing.T, fs afero.Fs, menuPID int, picked *string, safeQuit *bool) func(int, syscall.Signal) {
	t.Helper()
	return func(pid int, sig syscall.Signal) {
		if pid != menuPID || sig != syscall.SIGTERM {
			return
		}
		removeProc(fs, pid)
		data, err := afero.ReadFile(fs, contentFile)
		if err != nil {
			return
		}
		*picked = string(data)
		*safeQuit, _ = afero.Exists(fs, safeQuitFile)
		_ = fs.Remove(contentFile)
	}
}

func TestLaunchContentFromMenu(t *testing.T) {
	t.Parallel()
	fs := newTestFs(t)
	p, sigs, _ := newTestPlatform(t, fs)
	rom := filepath.Join(testROMs, "GBA", "Game.gba")
	writeFile(t, fs, rom, "rom")
	writeFile(t, fs, foregroundFile, "muxfrontend\n")
	addProc(t, fs, 30, "muxfrontend", "muxfrontend")
	var picked string
	var safeQuit bool
	sigs.onSignal = frontendLoader(t, fs, 30, &picked, &safeQuit)

	proc, err := p.launchContent(nil, rom, nil)
	require.NoError(t, err)
	assert.Nil(t, proc)

	assert.Equal(t, "Game\nmgba_libretro.so\nNintendo Game Boy Advance\nNintendo Game Boy Advance\n0\n"+
		"mu-mgba\n/mnt/mmc/ROMS/\nGBA\nGame.gba\n", picked)
	assert.True(t, safeQuit)
	assert.Equal(t, "extreme", readFile(t, fs, "/run/muos/governor"))
	assert.Equal(t, "Game", p.pending)
	assert.False(t, p.launching)
}

func TestLaunchContentReplacesRunningGame(t *testing.T) {
	t.Parallel()
	fs := newTestFs(t)
	p, sigs, media := newTestPlatform(t, fs)
	media.set(models.NewActiveMedia("GBA", "Game Boy Advance", "/old.gba", "Old", "GBA"))
	rom := filepath.Join(testROMs, "GBA", "Game.gba")
	writeFile(t, fs, rom, "rom")
	writeFile(t, fs, overlayFile, "Old\nNintendo Game Boy Advance\nmgba_libretro.so")
	writeFile(t, fs, foregroundFile, "retroarch\n")
	addProc(t, fs, 40, "retroarch", "retroarch")
	p.lastOverlay = "Old\nNintendo Game Boy Advance\nmgba_libretro.so"

	var picked string
	var safeQuit bool
	loader := frontendLoader(t, fs, 41, &picked, &safeQuit)
	sigs.onSignal = func(pid int, sig syscall.Signal) {
		if pid == 40 && sig == syscall.SIGTERM {
			// The game exits, launch.sh cleans up and the frontend
			// returns to the explorer.
			removeProc(fs, 40)
			for _, f := range runtimeFiles {
				_ = fs.Remove(f)
			}
			writeFile(t, fs, foregroundFile, "muxfrontend\n")
			addProc(t, fs, 41, "muxfrontend", "muxfrontend")
			return
		}
		loader(pid, sig)
	}

	_, err := p.launchContent(nil, rom, nil)
	require.NoError(t, err)

	assert.Contains(t, picked, "Game\nmgba_libretro.so\n")
	assert.Equal(t, "gba", readFile(t, fs, "/run/muos/controller"), "settings are written after cleanup")
	assert.Nil(t, media.get(), "the replaced game is no longer active")
	assert.Empty(t, p.lastOverlay)
	assert.Equal(t, []sentSignal{
		{40, syscall.SIGCONT}, {40, syscall.SIGTERM}, {41, syscall.SIGCONT}, {41, syscall.SIGTERM},
	}, sigs.signals())
}

func TestLaunchContentTimesOut(t *testing.T) {
	t.Parallel()
	fs := newTestFs(t)
	p, _, _ := newTestPlatform(t, fs)
	rom := filepath.Join(testROMs, "GBA", "Game.gba")
	writeFile(t, fs, rom, "rom")

	_, err := p.launchContent(nil, rom, nil)
	require.ErrorIs(t, err, errNotPickedUp)

	exists, err := afero.Exists(fs, contentFile)
	require.NoError(t, err)
	assert.False(t, exists, "an unclaimed request is withdrawn")
	assert.Empty(t, p.pending)
}

func TestLaunchContentStuckGame(t *testing.T) {
	t.Parallel()
	fs := newTestFs(t)
	p, _, _ := newTestPlatform(t, fs)
	rom := filepath.Join(testROMs, "GBA", "Game.gba")
	writeFile(t, fs, rom, "rom")
	writeFile(t, fs, overlayFile, "Old\nsys\ncore")
	writeFile(t, fs, foregroundFile, "retroarch\n")
	addProc(t, fs, 40, "retroarch", "retroarch")

	_, err := p.launchContent(nil, rom, nil)
	require.ErrorIs(t, err, platforms.ErrStopFailed)

	exists, err := afero.Exists(fs, contentFile)
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestLaunchContentUnresolvable(t *testing.T) {
	t.Parallel()
	fs := newTestFs(t)
	p, sigs, _ := newTestPlatform(t, fs)
	rom := filepath.Join(testROMs, "Unknown", "a.bin")
	writeFile(t, fs, rom, "rom")

	_, err := p.launchContent(nil, rom, nil)
	require.ErrorIs(t, err, errNoCore)
	assert.Empty(t, sigs.signals())
}

func TestStopActiveLauncherClosesGame(t *testing.T) {
	t.Parallel()
	fs := newTestFs(t)
	p, sigs, media := newTestPlatform(t, fs)
	media.set(models.NewActiveMedia("GBA", "Game Boy Advance", "/g.gba", "g", "GBA"))
	writeFile(t, fs, overlayFile, "g\nsys\ncore")
	writeFile(t, fs, foregroundFile, "retroarch\n")
	writeFile(t, fs, contentFile, "stale")
	addProc(t, fs, 40, "retroarch", "retroarch")
	sigs.onSignal = func(pid int, sig syscall.Signal) {
		if sig == syscall.SIGTERM {
			removeProc(fs, pid)
		}
	}

	require.NoError(t, p.StopActiveLauncher(platforms.StopForMenu))
	assert.Nil(t, media.get())
	assert.Len(t, sigs.signals(), 2)
	exists, err := afero.Exists(fs, contentFile)
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestStopActiveLauncherLeavesMenuAlone(t *testing.T) {
	t.Parallel()
	fs := newTestFs(t)
	p, sigs, _ := newTestPlatform(t, fs)
	writeFile(t, fs, overlayFile, "g\nsys\ncore")
	writeFile(t, fs, foregroundFile, "muxfrontend\n")
	addProc(t, fs, 30, "muxfrontend", "muxfrontend")

	require.NoError(t, p.ReturnToMenu())
	assert.Empty(t, sigs.signals())
}

func TestStopActiveLauncherReportsStuckGame(t *testing.T) {
	t.Parallel()
	fs := newTestFs(t)
	p, _, media := newTestPlatform(t, fs)
	active := models.NewActiveMedia("GBA", "Game Boy Advance", "/g.gba", "g", "GBA")
	media.set(active)
	writeFile(t, fs, overlayFile, "g\nsys\ncore")
	writeFile(t, fs, foregroundFile, "retroarch\n")
	addProc(t, fs, 40, "retroarch", "retroarch")

	require.ErrorIs(t, p.StopActiveLauncher(platforms.StopForPreemption), platforms.ErrStopFailed)
	assert.Same(t, active, media.get())
}

func TestWaitForReady(t *testing.T) {
	t.Parallel()
	fs := newTestFs(t)
	p, _, _ := newTestPlatform(t, fs)
	clock, ok := p.clock.(*clockwork.FakeClock)
	require.True(t, ok)
	media := models.NewActiveMedia("GBA", "Game Boy Advance", "/mnt/mmc/ROMS/GBA/Game.gba", "Game", "GBA")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- p.waitForReady(ctx, nil, media) }()

	require.NoError(t, clock.BlockUntilContext(ctx, 1))
	writeFile(t, fs, overlayFile, "Game\nNintendo Game Boy Advance\nmgba_libretro.so")
	clock.Advance(readyPollInterval)
	require.NoError(t, <-done)

	cancelled, stop := context.WithCancel(context.Background())
	stop()
	_ = fs.Remove(overlayFile)
	require.ErrorIs(t, p.waitForReady(cancelled, nil, media), context.Canceled)
	require.NoError(t, p.waitForReady(ctx, nil, nil))
	assert.Equal(t, readyTimeout, readyTimeoutFor(media))
}

func TestGameRunning(t *testing.T) {
	t.Parallel()
	fs := newTestFs(t)
	p, _, _ := newTestPlatform(t, fs)
	media := models.NewActiveMedia("GBA", "Game Boy Advance", "/mnt/mmc/ROMS/GBA/Game.gba", "Game", "GBA")
	p.pending = "Game"

	running, err := p.gameRunning(context.Background(), nil, media)
	require.NoError(t, err)
	assert.False(t, running)
	assert.Empty(t, p.pending, "a launch that never started is forgotten")

	writeFile(t, fs, overlayFile, "Game\nsys\ncore")
	running, err = p.gameRunning(context.Background(), nil, media)
	require.NoError(t, err)
	assert.True(t, running)

	running, err = p.gameRunning(context.Background(), nil, nil)
	require.NoError(t, err)
	assert.False(t, running)
}

func TestLaunchContentWriteFailures(t *testing.T) {
	t.Parallel()
	rom := filepath.Join(testROMs, "GBA", "Game.gba")
	tests := map[string]*failFs{
		"assets":  {op: "mkdir", path: runDir},
		"request": {op: "open", path: contentFile + ".tmp"},
	}
	for name, fs := range tests {
		fs.Fs = newTestFs(t)
		writeFile(t, fs.Fs, rom, "rom")
		p, _, _ := newTestPlatform(t, fs)

		_, err := p.launchContent(nil, rom, nil)
		require.ErrorIs(t, err, errInjected, name)
		assert.Empty(t, p.pending, name)
		assert.False(t, p.launching, name)
	}
}

// The game exits but launch.sh never clears its overlay, and the frontend
// never picks up the request, which Core then cannot withdraw.
func TestLaunchContentFrontendNeverResponds(t *testing.T) {
	t.Parallel()
	base := newTestFs(t)
	rom := filepath.Join(testROMs, "GBA", "Game.gba")
	writeFile(t, base, rom, "rom")
	writeFile(t, base, overlayFile, "Old\nsys\ncore")
	writeFile(t, base, foregroundFile, "retroarch\n")
	addProc(t, base, 40, "retroarch", "retroarch")
	p, sigs, media := newTestPlatform(t, &failFs{Fs: base, op: "remove", path: contentFile})
	media.set(models.NewActiveMedia("GBA", "Game Boy Advance", "/old.gba", "Old", "GBA"))
	p.lastOverlay = "Old\nsys\ncore"
	sigs.onSignal = func(pid int, sig syscall.Signal) {
		if sig == syscall.SIGTERM {
			removeProc(base, pid)
		}
	}
	pauses := 0
	p.sleep = func(time.Duration) { pauses++ }

	_, err := p.launchContent(nil, rom, nil)
	require.ErrorIs(t, err, errNotPickedUp)
	assert.Nil(t, media.get())
	assert.Empty(t, p.lastOverlay, "cleanup is assumed done after the wait")
	assert.Empty(t, p.pending)
	assert.GreaterOrEqual(t, pauses, overlayGonePolls+pickupPolls, "both waits run to the end")
	exists, err := afero.Exists(base, contentFile)
	require.NoError(t, err)
	assert.True(t, exists, "the request could not be withdrawn")
}

func TestLaunchContentMenuWontClose(t *testing.T) {
	t.Parallel()
	fs := newTestFs(t)
	p, sigs, _ := newTestPlatform(t, fs)
	rom := filepath.Join(testROMs, "GBA", "Game.gba")
	writeFile(t, fs, rom, "rom")
	writeFile(t, fs, overlayFile, "Old\nsys\ncore")
	writeFile(t, fs, foregroundFile, "muxfrontend\n")
	addProc(t, fs, 30, "muxfrontend", "muxfrontend")

	_, err := p.launchContent(nil, rom, nil)
	require.ErrorIs(t, err, errNotPickedUp)
	terms := 0
	for _, s := range sigs.signals() {
		if s.sig == syscall.SIGTERM {
			terms++
		}
	}
	assert.Equal(t, 1, terms, "a running menu is not treated as content, and is asked to close once")
}

func TestLaunchContentMenuAssetsFail(t *testing.T) {
	t.Parallel()
	base := newTestFs(t)
	rom := filepath.Join(testROMs, "GBA", "Game.gba")
	writeFile(t, base, rom, "rom")
	writeFile(t, base, foregroundFile, "muxfrontend\n")
	addProc(t, base, 30, "muxfrontend", "muxfrontend")
	p, sigs, _ := newTestPlatform(t, &failFs{Fs: base, op: "open", path: runDir + "/governor", skip: 1})

	_, err := p.launchContent(nil, rom, nil)
	require.ErrorIs(t, err, errInjected)
	assert.Empty(t, sigs.signals(), "the menu is left open")
	exists, err := afero.Exists(base, contentFile)
	require.NoError(t, err)
	assert.False(t, exists, "the request is withdrawn")
}

func TestLaunchContentWithoutSafeQuitMarker(t *testing.T) {
	t.Parallel()
	base := newTestFs(t)
	rom := filepath.Join(testROMs, "GBA", "Game.gba")
	writeFile(t, base, rom, "rom")
	writeFile(t, base, foregroundFile, "muxfrontend\n")
	addProc(t, base, 30, "muxfrontend", "muxfrontend")
	p, sigs, _ := newTestPlatform(t, &failFs{Fs: base, op: "open", path: safeQuitFile})
	var picked string
	var safeQuit bool
	sigs.onSignal = frontendLoader(t, base, 30, &picked, &safeQuit)

	_, err := p.launchContent(nil, rom, nil)
	require.NoError(t, err)
	assert.NotEmpty(t, picked)
	assert.False(t, safeQuit)
}

func TestLaunchContentPickedUpDuringLastWait(t *testing.T) {
	t.Parallel()
	fs := newTestFs(t)
	p, _, _ := newTestPlatform(t, fs)
	rom := filepath.Join(testROMs, "GBA", "Game.gba")
	writeFile(t, fs, rom, "rom")
	pauses := 0
	p.sleep = func(time.Duration) {
		pauses++
		if pauses == pickupPolls {
			_ = fs.Remove(contentFile)
		}
	}

	_, err := p.launchContent(nil, rom, nil)
	require.NoError(t, err)
	assert.Equal(t, "Game", p.pending)
}
