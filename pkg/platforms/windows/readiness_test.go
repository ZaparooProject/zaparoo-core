//go:build windows

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

package windows

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/esapi"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ platforms.MediaReadyPlatform = (*Platform)(nil)

func TestLaunchers_ReadinessProbes(t *testing.T) {
	t.Parallel()
	cfg, err := helpers.NewTestConfig(helpers.NewMemoryFS(), t.TempDir())
	require.NoError(t, err)

	probed := map[string]bool{}
	for _, l := range (&Platform{}).Launchers(cfg) {
		probed[l.ID] = l.WaitForReady != nil
	}

	assert.True(t, probed["Steam"], "Steam publishes before the game process exists")
	assert.True(t, probed["LaunchBox"], "LaunchBox reports a start before the game window exists")
	assert.True(t, probed["PinUPPopper"], "Popper publishes when the emulator process appears")
	assert.True(t, probed["KodiLocalVideo"], "Kodi polls for an active player")
	assert.True(t, probed["RetroBatNES"], "RetroBat games are ready once the emulator has a window")
	assert.False(t, probed["Flashpoint"], "Flashpoint gives no process or window to follow")
	assert.False(t, probed["GenericExecutable"], "a bare executable publishes no media")
}

// retroBatReadyRig stands in for EmulationStation and the process list.
type retroBatReadyRig struct {
	apiErr  error
	p       *Platform
	clock   *clockwork.FakeClock
	windows map[int]bool
	running esapi.RunningGameResponse
	procs   []windowsProcessInfo
	isUp    bool
}

func newRetroBatReadyRig(t *testing.T) *retroBatReadyRig {
	t.Helper()
	rig := &retroBatReadyRig{clock: clockwork.NewFakeClock(), windows: map[int]bool{}}
	rig.p = &Platform{
		readyClock:  rig.clock,
		windowCheck: func(pid int) bool { return rig.windows[pid] },
	}
	oldAPI, oldList, oldDir := retroBatAPIRunningGame, retroBatListProcesses, retroBatFindDir
	retroBatAPIRunningGame = func() (esapi.RunningGameResponse, bool, error) {
		return rig.running, rig.isUp, rig.apiErr
	}
	retroBatListProcesses = func() ([]windowsProcessInfo, error) { return rig.procs, nil }
	retroBatFindDir = func(*config.Instance) (string, error) { return `C:\RetroBat`, nil }
	t.Cleanup(func() {
		retroBatAPIRunningGame, retroBatListProcesses, retroBatFindDir = oldAPI, oldList, oldDir
	})
	return rig
}

func retroBatMedia() *models.ActiveMedia {
	return models.NewActiveMedia("ports", "Ports", `C:\RetroBat\roms\ports\2048.libretro`, "2048", "RetroBatPorts")
}

func (r *retroBatReadyRig) wait(ctx context.Context) <-chan error {
	done := make(chan error, 1)
	go func() { done <- r.p.waitForRetroBatGame(ctx, nil, retroBatMedia()) }()
	return done
}

// advanceUntil drives the fake clock until the wait returns.
func (r *retroBatReadyRig) advanceUntil(t *testing.T, done <-chan error) error {
	t.Helper()
	for {
		select {
		case err := <-done:
			return err
		case <-time.After(time.Millisecond):
			r.clock.Advance(retroBatReadyPoll)
		}
	}
}

func TestWaitForRetroBatGame_ReadyOnceTheEmulatorHasAWindow(t *testing.T) {
	t.Parallel()
	rig := newRetroBatReadyRig(t)
	rig.isUp = true
	rig.running = esapi.RunningGameResponse{Path: "C:/RetroBat/roms/ports/2048.libretro"}
	rig.procs = []windowsProcessInfo{{PID: 7, ExePath: `C:\RetroBat\emulators\retroarch\retroarch.exe`}}
	rig.windows[7] = true

	require.NoError(t, rig.advanceUntil(t, rig.wait(t.Context())))
}

func TestWaitForRetroBatGame_GameReportedButNoEmulatorWindowIsNotReady(t *testing.T) {
	t.Parallel()
	rig := newRetroBatReadyRig(t)
	rig.isUp = true
	rig.running = esapi.RunningGameResponse{Path: "C:/RetroBat/roms/ports/2048.libretro"}
	rig.procs = []windowsProcessInfo{
		{PID: 7, ExePath: `C:\RetroBat\emulators\retroarch\retroarch.exe`},
		{PID: 8, ExePath: `C:\RetroBat\emulationstation\emulationstation.exe`},
	}
	rig.windows[8] = true // the frontend's own window does not count
	ctx, cancel := context.WithCancel(t.Context())
	done := rig.wait(ctx)

	for range 20 {
		rig.clock.Advance(retroBatReadyPoll)
		time.Sleep(time.Millisecond)
	}
	cancel()

	require.ErrorIs(t, <-done, context.Canceled)
}

func TestWaitForRetroBatGame_OtherGameRunningIsNotReady(t *testing.T) {
	t.Parallel()
	rig := newRetroBatReadyRig(t)
	rig.isUp = true
	rig.running = esapi.RunningGameResponse{Path: "C:/RetroBat/roms/ports/other.libretro"}
	rig.procs = []windowsProcessInfo{{PID: 7, ExePath: `C:\RetroBat\emulators\retroarch\retroarch.exe`}}
	rig.windows[7] = true
	ctx, cancel := context.WithCancel(t.Context())
	done := rig.wait(ctx)

	for range 20 {
		rig.clock.Advance(retroBatReadyPoll)
		time.Sleep(time.Millisecond)
	}
	cancel()

	require.ErrorIs(t, <-done, context.Canceled)
}

func TestWaitForRetroBatGame_APIFailureIsUnavailable(t *testing.T) {
	t.Parallel()
	rig := newRetroBatReadyRig(t)
	rig.apiErr = errors.New("connection refused")
	ctx, cancel := context.WithCancel(t.Context())
	done := rig.wait(ctx)

	require.NoError(t, rig.clock.BlockUntilContext(t.Context(), 1))
	cancel()

	require.ErrorIs(t, <-done, platforms.ErrReadinessUnavailable)
}

func TestSameRetroBatPath(t *testing.T) {
	t.Parallel()

	assert.True(t, sameRetroBatPath("C:/RetroBat/roms/a.nes", `C:\RetroBat\roms\a.nes`))
	assert.True(t, sameRetroBatPath("c:/retrobat/ROMS/A.nes", `C:\RetroBat\roms\a.nes`))
	assert.False(t, sameRetroBatPath("C:/RetroBat/roms/a.nes", "C:/RetroBat/roms/b.nes"))
	assert.True(t, sameRetroBatPath("./nes/A.nes", `D:\RetroBat\roms\nes\a.nes`), "same file, different root")
	assert.False(t, sameRetroBatPath("", ""), "no name never matches")
	assert.False(t, sameRetroBatPath("C:/roms/dir/", `C:\roms\other\`), "no file name to compare")
}

// blockingFocuser never finds a window, so its attempt stays pending.
type blockingFocuser struct{}

func (blockingFocuser) Focus(ctx context.Context, _ uint32) error {
	<-ctx.Done()
	return ctx.Err()
}

func TestWaitForTrackedWindow_NoFocuserIsUnavailable(t *testing.T) {
	t.Parallel()

	err := (&Platform{}).waitForTrackedWindow(t.Context(), nil, nil)

	require.ErrorIs(t, err, platforms.ErrReadinessUnavailable)
}

func TestClearTrackedProcess_ForgetsTheWindowAttempt(t *testing.T) {
	t.Parallel()
	proc := &os.Process{Pid: 4242}
	p := &Platform{readyClock: clockwork.NewFakeClock(), trackedProcess: proc}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	p.windowFocus.Track(ctx, blockingFocuser{}, 4242)

	p.clearTrackedProcess(proc)

	// With the attempt forgotten nothing is tracked, so the wait is over at once.
	require.NoError(t, p.WaitForMediaReady(t.Context(), nil, nil))
}
