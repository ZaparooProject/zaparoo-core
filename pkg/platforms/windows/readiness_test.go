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
	assert.True(t, probed["Flashpoint"], "Flashpoint starts its player some seconds after the link is handed over")
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
	rig := newRetroBatReadyRig(t)
	rig.isUp = true
	rig.running = esapi.RunningGameResponse{Path: "C:/RetroBat/roms/ports/2048.libretro"}
	rig.procs = []windowsProcessInfo{{PID: 7, ExePath: `C:\RetroBat\emulators\retroarch\retroarch.exe`}}
	rig.windows[7] = true

	require.NoError(t, rig.advanceUntil(t, rig.wait(t.Context())))
}

func TestWaitForRetroBatGame_GameReportedButNoEmulatorWindowIsNotReady(t *testing.T) {
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

func TestFlashpointDirFromCommand(t *testing.T) {
	t.Parallel()

	dir, err := flashpointDirFromCommand(`"C:\Flashpoint\Launcher\Flashpoint.exe" "%1"`)
	require.NoError(t, err)
	assert.Equal(t, `C:\Flashpoint`, dir)

	dir, err = flashpointDirFromCommand(`D:\fp\launcher\Flashpoint.exe %1`)
	require.NoError(t, err)
	assert.Equal(t, `D:\fp`, dir)

	_, err = flashpointDirFromCommand(`"C:\Tools\Other.exe" "%1"`)
	require.ErrorIs(t, err, errFlashpointBadLayout)
}

// flashpointRig stands in for the process list while a game starts.
type flashpointRig struct {
	clock   *clockwork.FakeClock
	windows map[int]bool
	procs   []windowsProcessInfo
}

func (r *flashpointRig) wait(ctx context.Context, findDir func() (string, error)) <-chan error {
	done := make(chan error, 1)
	go func() {
		done <- waitForFlashpointPlayer(ctx, r.clock, findDir,
			func() ([]windowsProcessInfo, error) { return r.procs, nil },
			func(pid int) bool { return r.windows[pid] })
	}()
	return done
}

func flashpointRoot() (string, error) { return `C:\Flashpoint`, nil }

func TestWaitForFlashpointPlayer_ReadyOnceANewPlayerHasAWindow(t *testing.T) {
	t.Parallel()
	rig := &flashpointRig{clock: clockwork.NewFakeClock(), windows: map[int]bool{}}
	rig.procs = []windowsProcessInfo{
		{PID: 1, ExePath: `C:\Flashpoint\Launcher\Flashpoint.exe`},
		{PID: 2, ExePath: `C:\Flashpoint\FPSoftware\Flash\flashplayer_32_sa.exe`}, // the previous game
	}
	rig.windows[1], rig.windows[2] = true, true
	done := rig.wait(t.Context(), flashpointRoot)

	require.NoError(t, rig.clock.BlockUntilContext(t.Context(), 1))
	rig.procs = append(rig.procs,
		windowsProcessInfo{PID: 3, ExePath: `C:\Flashpoint\FPSoftware\Flash\flashplayer_32_sa.exe`})
	rig.clock.Advance(flashpointReadyPoll)
	require.NoError(t, rig.clock.BlockUntilContext(t.Context(), 1))
	select {
	case err := <-done:
		t.Fatalf("ready before the new player had a window: %v", err)
	default:
	}
	rig.windows[3] = true
	for {
		select {
		case err := <-done:
			require.NoError(t, err)
			return
		case <-time.After(time.Millisecond):
			rig.clock.Advance(flashpointReadyPoll)
		}
	}
}

func TestWaitForFlashpointPlayer_LauncherOnlyIsNotReady(t *testing.T) {
	t.Parallel()
	rig := &flashpointRig{clock: clockwork.NewFakeClock(), windows: map[int]bool{1: true}}
	rig.procs = []windowsProcessInfo{{PID: 1, ExePath: `C:\Flashpoint\Launcher\Flashpoint.exe`}}
	ctx, cancel := context.WithCancel(t.Context())
	done := rig.wait(ctx, flashpointRoot)

	for range 20 {
		rig.clock.Advance(flashpointReadyPoll)
		time.Sleep(time.Millisecond)
	}
	cancel()

	require.ErrorIs(t, <-done, context.Canceled)
}

func TestWaitForFlashpointPlayer_NotInstalledIsUnavailable(t *testing.T) {
	t.Parallel()
	rig := &flashpointRig{clock: clockwork.NewFakeClock(), windows: map[int]bool{}}

	err := <-rig.wait(t.Context(), func() (string, error) { return "", errFlashpointNotFound })

	require.ErrorIs(t, err, platforms.ErrReadinessUnavailable)
}

func (r *flashpointRig) start(ctx context.Context) <-chan error {
	done := make(chan error, 1)
	go func() {
		done <- waitForFlashpointStart(ctx, r.clock, `C:\Flashpoint`, flashpointStartTimeout,
			func() ([]windowsProcessInfo, error) { return r.procs, nil },
			func(pid int) bool { return r.windows[pid] })
	}()
	return done
}

// Windows, 2026-10-26: a cold Flashpoint was handed the game's link 5 s after
// its window opened, while its backend was still starting. The backend
// stalled, no game started and the launcher exited a minute later.
func TestWaitForFlashpointStart_WindowAloneIsNotEnough(t *testing.T) {
	t.Parallel()
	rig := &flashpointRig{clock: clockwork.NewFakeClock(), windows: map[int]bool{1: true}}
	rig.procs = []windowsProcessInfo{{PID: 1, ExePath: `C:\Flashpoint\Launcher\Flashpoint.exe`}}
	done := rig.start(t.Context())

	for range 40 { // 10 s of polls, twice the old settle
		blockCtx, cancel := context.WithTimeout(t.Context(), time.Second)
		blockErr := rig.clock.BlockUntilContext(blockCtx, 1)
		cancel()
		select {
		case err := <-done:
			t.Fatalf("handed over before the game server started: %v", err)
		default:
		}
		require.NoError(t, blockErr)
		rig.clock.Advance(flashpointReadyPoll)
	}

	require.NoError(t, rig.clock.BlockUntilContext(t.Context(), 1))
	rig.procs = append(rig.procs,
		windowsProcessInfo{PID: 2, ExePath: `C:\Flashpoint\Server\Flashpoint Game Server.exe`})
	for {
		select {
		case err := <-done:
			require.NoError(t, err)
			return
		case <-time.After(time.Millisecond):
			rig.clock.Advance(flashpointReadyPoll)
		}
	}
}

func TestWaitForFlashpointStart_ServerWithoutWindowIsNotReady(t *testing.T) {
	t.Parallel()
	rig := &flashpointRig{clock: clockwork.NewFakeClock(), windows: map[int]bool{}}
	rig.procs = []windowsProcessInfo{
		{PID: 1, ExePath: `C:\Flashpoint\Launcher\Flashpoint.exe`},
		{PID: 2, ExePath: `C:\Flashpoint\Server\Flashpoint Game Server.exe`},
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := rig.start(ctx)

	for range 20 {
		rig.clock.Advance(flashpointReadyPoll)
		time.Sleep(time.Millisecond)
	}
	cancel()

	require.ErrorIs(t, <-done, context.Canceled)
}

// A Flashpoint without the game server is still handed the game once the wait
// runs out with its window up, rather than the launch failing.
func TestWaitForFlashpointStart_NoServerHandsOverAfterTimeout(t *testing.T) {
	t.Parallel()
	rig := &flashpointRig{clock: clockwork.NewFakeClock(), windows: map[int]bool{1: true}}
	rig.procs = []windowsProcessInfo{{PID: 1, ExePath: `C:\Flashpoint\Launcher\Flashpoint.exe`}}
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	done := rig.start(ctx)

	for {
		select {
		case err := <-done:
			require.NoError(t, err)
			return
		case <-time.After(time.Millisecond):
			rig.clock.Advance(flashpointReadyPoll)
		}
	}
}

// Flashpoint that never opens its window fails the launch.
func TestWaitForFlashpointStart_NoWindowFails(t *testing.T) {
	t.Parallel()
	rig := &flashpointRig{clock: clockwork.NewFakeClock(), windows: map[int]bool{}}
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	done := rig.start(ctx)

	for {
		select {
		case err := <-done:
			require.ErrorIs(t, err, context.DeadlineExceeded)
			return
		case <-time.After(time.Millisecond):
			rig.clock.Advance(flashpointReadyPoll)
		}
	}
}

// A Flashpoint that comes up just before the start timeout still gets its
// settle; the timeout bounds the wait for it, not the settle after.
func TestWaitForFlashpointStart_ReadyAtTheTimeoutStillSettles(t *testing.T) {
	t.Parallel()
	rig := &flashpointRig{clock: clockwork.NewFakeClock(), windows: map[int]bool{1: true}}
	rig.procs = []windowsProcessInfo{
		{PID: 1, ExePath: `C:\Flashpoint\Launcher\Flashpoint.exe`},
		{PID: 2, ExePath: `C:\Flashpoint\Server\Flashpoint Game Server.exe`},
	}
	const timeout = 20 * time.Millisecond
	done := make(chan error, 1)
	go func() {
		done <- waitForFlashpointStart(t.Context(), rig.clock, `C:\Flashpoint`, timeout,
			func() ([]windowsProcessInfo, error) { return rig.procs, nil },
			func(pid int) bool { return rig.windows[pid] })
	}()
	require.NoError(t, rig.clock.BlockUntilContext(t.Context(), 1))
	time.Sleep(5 * timeout) // the start timeout runs out during the settle
	rig.clock.Advance(flashpointStartSettle)

	require.NoError(t, <-done)
}

// One player that will not die is a failed stop, even when another was ended.
func TestKillFlashpointPlayers_PartialFailureIsAnError(t *testing.T) {
	t.Parallel()

	players := `C:\Flashpoint\FPSoftware`
	procs := []windowsProcessInfo{
		{PID: 10, ExePath: `C:\Flashpoint\FPSoftware\Flash\flashplayer_32_sa.exe`},
		{PID: 11, ExePath: `C:\Flashpoint\FPSoftware\Basilisk-Portable\Basilisk-Portable.exe`},
	}
	n, err := killFlashpointPlayers(players,
		func() ([]windowsProcessInfo, error) { return procs, nil },
		func(pid uint32) (string, error) {
			for i := range procs {
				if procs[i].PID == pid {
					return procs[i].ExePath, nil
				}
			}
			return "", errors.New("gone")
		},
		func(_ context.Context, pid uint32) error {
			if pid == 11 {
				return errors.New("access denied")
			}
			return nil
		},
	)

	require.Error(t, err)
	assert.Equal(t, 1, n, "the player that was ended is still counted")
}

// Windows, 2026-10-26: stopping a Flashpoint game cleared the media and left
// the game's player running, since Core holds no handle to it.
func TestKillFlashpointPlayers(t *testing.T) {
	t.Parallel()

	players := `C:\Flashpoint\FPSoftware`
	procs := []windowsProcessInfo{
		{PID: 10, ExePath: `C:\Flashpoint\FPSoftware\Flash\flashplayer_32_sa.exe`},
		{PID: 11, ExePath: `C:\Flashpoint\Launcher\Flashpoint.exe`},
		{PID: 12, ExePath: `C:\Windows\explorer.exe`},
		{PID: 13, ExePath: `C:\Flashpoint\FPSoftware\Basilisk-Portable\Basilisk-Portable.exe`},
	}
	var killed []uint32
	n, err := killFlashpointPlayers(players,
		func() ([]windowsProcessInfo, error) { return procs, nil },
		func(pid uint32) (string, error) {
			for i := range procs {
				if procs[i].PID == pid {
					if pid == 13 {
						return `C:\Other\reused.exe`, nil // PID reused since the listing
					}
					return procs[i].ExePath, nil
				}
			}
			return "", errors.New("gone")
		},
		func(_ context.Context, pid uint32) error {
			killed = append(killed, pid)
			return nil
		},
	)

	require.NoError(t, err)
	assert.Equal(t, 1, n)
	assert.Equal(t, []uint32{10}, killed, "only a player still under FPSoftware is ended")
}
