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
	"os/exec"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stopTestPlatform returns a Platform wired to in-memory active media, and a
// reader for whatever that media currently is.
func stopTestPlatform(
	media *models.ActiveMedia,
) (platform *Platform, currentMedia func() *models.ActiveMedia) {
	current := media
	p := &Platform{}
	p.activeMedia = func() *models.ActiveMedia { return current }
	p.setActiveMedia = func(m *models.ActiveMedia) { current = m }
	return p, func() *models.ActiveMedia { return current }
}

func testActiveMedia() *models.ActiveMedia {
	return models.NewActiveMedia("PC", "PC", "steam://212680", "FTL", "Steam")
}

// shortenStopTimeouts keeps the escalation timings small so tests that expect
// a failure do not wait out the real close and kill windows.
func shortenStopTimeouts(t *testing.T) {
	t.Helper()
	graceful, forced, poll := windowsGracefulStopTimeout, windowsForcedStopTimeout, windowsStopPollInterval
	windowsGracefulStopTimeout = 150 * time.Millisecond
	windowsForcedStopTimeout = 150 * time.Millisecond
	windowsStopPollInterval = 10 * time.Millisecond
	t.Cleanup(func() {
		windowsGracefulStopTimeout, windowsForcedStopTimeout, windowsStopPollInterval = graceful, forced, poll
	})
}

func TestStopActiveLauncher_NothingActiveSucceeds(t *testing.T) {
	t.Parallel()

	p, currentMedia := stopTestPlatform(nil)

	require.NoError(t, p.StopActiveLauncher(platforms.StopForMenu))
	assert.Nil(t, currentMedia())
}

// stubMediaRunning forces the "is the media still running" probe, which
// otherwise needs a live Steam registry value.
func stubMediaRunning(t *testing.T, running bool) {
	t.Helper()
	original := windowsMediaStillRunning
	windowsMediaStillRunning = func(*Platform) bool { return running }
	t.Cleanup(func() { windowsMediaStillRunning = original })
}

func TestStopActiveLauncher_ReportsFailureWhenMediaConfirmedRunning(t *testing.T) {
	// Not parallel: replaces the package-level running probe.
	stubMediaRunning(t, true)

	// An externally detected Steam game whose process was never resolved, but
	// Steam still reports it running: claiming success would tell Core the
	// game stopped while it plainly has not. This is the core of issue 819.
	media := testActiveMedia()
	p, currentMedia := stopTestPlatform(media)

	err := p.StopActiveLauncher(platforms.StopForMenu)

	require.ErrorIs(t, err, platforms.ErrStopFailed)
	assert.Same(t, media, currentMedia(), "active media must survive a failed stop")
}

func TestStopActiveLauncher_ClearsUnconfirmedMediaWithoutMechanism(t *testing.T) {
	// Not parallel: replaces the package-level running probe.
	stubMediaRunning(t, false)

	// No handle, no Kill, and no evidence the media is still running -- for
	// example a tracked launcher whose process already exited. Reporting
	// failure here left Core unable to preempt, so it could not launch at all.
	p, currentMedia := stopTestPlatform(testActiveMedia())

	require.NoError(t, p.StopActiveLauncher(platforms.StopForMenu))
	assert.Nil(t, currentMedia())
}

func TestStopActiveLauncher_ClearsMediaForFireAndForgetLauncher(t *testing.T) {
	// Not parallel: replaces the package-level running probe. Even a running
	// browser tab is cleared, because Core never had a way to stop it.
	stubMediaRunning(t, true)

	// A browser tab or detached script never hands back a handle by design,
	// so there is genuinely nothing to stop and clearing media is honest.
	p, currentMedia := stopTestPlatform(testActiveMedia())
	p.setLastLauncher(&platforms.Launcher{
		ID:        "WebBrowser",
		Lifecycle: platforms.LifecycleFireAndForget,
	})

	require.NoError(t, p.StopActiveLauncher(platforms.StopForMenu))
	assert.Nil(t, currentMedia())
}

func TestStopActiveLauncher_ExternalLifecycleWithoutProcessFails(t *testing.T) {
	// Not parallel: replaces the package-level running probe.
	stubMediaRunning(t, true)

	// LifecycleExternal means Core expected the platform tracker to supply a
	// process. Not having one, while the game is confirmed still running, is a
	// tracking failure rather than "nothing to stop".
	media := testActiveMedia()
	p, currentMedia := stopTestPlatform(media)
	p.setLastLauncher(&platforms.Launcher{
		ID:        "Steam",
		Lifecycle: platforms.LifecycleExternal,
	})

	err := p.StopActiveLauncher(platforms.StopForMenu)

	require.ErrorIs(t, err, platforms.ErrStopFailed)
	assert.Same(t, media, currentMedia())
}

func TestStopActiveLauncher_StopsProcessTree(t *testing.T) {
	t.Parallel()

	p, currentMedia := stopTestPlatform(testActiveMedia())

	//nolint:gosec // Fixed command; ComSpec resolves the shell by absolute path.
	cmd := exec.CommandContext(context.Background(), helpers.ComSpec(), "/C", "timeout", "/T", "30")
	require.NoError(t, cmd.Start())
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			// Wait releases the goroutine CommandContext starts to watch the
			// context; without it that goroutine outlives the test.
			_ = cmd.Wait()
		}
	})
	p.SetTrackedProcess(cmd.Process)

	require.NoError(t, p.StopActiveLauncher(platforms.StopForMenu))
	assert.False(t, helpers.IsProcessRunning(cmd.Process), "tracked process should be gone")
	assert.Nil(t, currentMedia(), "media cleared once the stop is confirmed")

	p.processMu.RLock()
	tracked := p.trackedProcess
	p.processMu.RUnlock()
	assert.Nil(t, tracked)
}

// recordTaskKills replaces the tree-kill hooks and returns the ordered list of
// phases they were called in.
func recordTaskKills(t *testing.T, graceful, forced func(uint32) error) *[]string {
	t.Helper()
	calls := make([]string, 0, 2)
	tree, force := windowsTaskKillTree, windowsForceTaskKillTree
	windowsTaskKillTree = func(_ context.Context, pid uint32) error {
		calls = append(calls, "graceful")
		return graceful(pid)
	}
	windowsForceTaskKillTree = func(_ context.Context, pid uint32) error {
		calls = append(calls, "forced")
		return forced(pid)
	}
	t.Cleanup(func() { windowsTaskKillTree, windowsForceTaskKillTree = tree, force })
	return &calls
}

func TestStopActiveLauncher_EscalatesGracefulThenForced(t *testing.T) {
	// Not parallel: replaces package-level kill hooks.
	shortenStopTimeouts(t)
	// Neither hook ends the process, so the stop must try the polite close
	// first and only then escalate to a forced kill.
	calls := recordTaskKills(t,
		func(uint32) error { return nil },
		func(uint32) error { return nil },
	)
	stubMediaRunning(t, false)

	p, _ := stopTestPlatform(testActiveMedia())
	//nolint:gosec // Fixed command; ComSpec resolves the shell by absolute path.
	cmd := exec.CommandContext(context.Background(), helpers.ComSpec(), "/C", "timeout", "/T", "30")
	require.NoError(t, cmd.Start())
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			// Wait releases the goroutine CommandContext starts to watch the
			// context; without it that goroutine outlives the test.
			_ = cmd.Wait()
		}
	})
	p.SetTrackedProcess(cmd.Process)

	_ = p.StopActiveLauncher(platforms.StopForMenu)
	assert.Equal(t, []string{"graceful", "forced"}, *calls)
}

func TestStopActiveLauncher_SkipsForcedKillWhenCloseWorks(t *testing.T) {
	// Not parallel: replaces package-level kill hooks.
	shortenStopTimeouts(t)
	//nolint:gosec // Fixed command; ComSpec resolves the shell by absolute path.
	cmd := exec.CommandContext(context.Background(), helpers.ComSpec(), "/C", "timeout", "/T", "30")
	require.NoError(t, cmd.Start())
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			// Wait releases the goroutine CommandContext starts to watch the
			// context; without it that goroutine outlives the test.
			_ = cmd.Wait()
		}
	})

	// The polite close ends the process, so escalating would be gratuitous.
	calls := recordTaskKills(t,
		func(uint32) error { return cmd.Process.Kill() },
		func(uint32) error { return nil },
	)

	p, currentMedia := stopTestPlatform(testActiveMedia())
	p.SetTrackedProcess(cmd.Process)

	require.NoError(t, p.StopActiveLauncher(platforms.StopForMenu))
	assert.Equal(t, []string{"graceful"}, *calls, "forced kill must not run after a clean close")
	assert.Nil(t, currentMedia())
}

func TestStopActiveLauncher_KeepsStateWhenKillFails(t *testing.T) {
	// Not parallel: replaces package-level kill hooks.
	shortenStopTimeouts(t)
	stubMediaRunning(t, true)
	tree, force := windowsTaskKillTree, windowsForceTaskKillTree
	windowsTaskKillTree = func(_ context.Context, _ uint32) error { return nil }
	windowsForceTaskKillTree = func(_ context.Context, _ uint32) error { return nil }
	t.Cleanup(func() { windowsTaskKillTree, windowsForceTaskKillTree = tree, force })

	media := testActiveMedia()
	p, currentMedia := stopTestPlatform(media)

	//nolint:gosec // Fixed command; ComSpec resolves the shell by absolute path.
	cmd := exec.CommandContext(context.Background(), helpers.ComSpec(), "/C", "timeout", "/T", "30")
	require.NoError(t, cmd.Start())
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			// Wait releases the goroutine CommandContext starts to watch the
			// context; without it that goroutine outlives the test.
			_ = cmd.Wait()
		}
	})
	p.SetTrackedProcess(cmd.Process)

	err := p.StopActiveLauncher(platforms.StopForMenu)

	require.ErrorIs(t, err, platforms.ErrStopFailed)
	assert.Same(t, media, currentMedia(), "media must survive a failed stop")

	// The handle is kept so a retry still has something to act on.
	p.processMu.RLock()
	tracked := p.trackedProcess
	p.processMu.RUnlock()
	assert.Equal(t, cmd.Process, tracked)
}

func TestClearTrackedProcessPID(t *testing.T) {
	t.Parallel()

	p := &Platform{}
	p.setActiveMedia = func(_ *models.ActiveMedia) {}

	//nolint:gosec // Fixed command; ComSpec resolves the shell by absolute path.
	cmd := exec.CommandContext(context.Background(), helpers.ComSpec(), "/C", "timeout", "/T", "30")
	require.NoError(t, cmd.Start())
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			// Wait releases the goroutine CommandContext starts to watch the
			// context; without it that goroutine outlives the test.
			_ = cmd.Wait()
		}
	})
	p.SetTrackedProcess(cmd.Process)

	assert.False(t, p.ClearTrackedProcessPID(cmd.Process.Pid+100000),
		"a stale PID must not clear the tracked process")

	p.processMu.RLock()
	stillTracked := p.trackedProcess
	p.processMu.RUnlock()
	assert.Equal(t, cmd.Process, stillTracked)

	assert.True(t, p.ClearTrackedProcessPID(cmd.Process.Pid))
	p.processMu.RLock()
	cleared := p.trackedProcess
	p.processMu.RUnlock()
	assert.Nil(t, cleared)

	// Clearing must not signal the process; it is only forgotten.
	assert.True(t, helpers.IsProcessRunning(cmd.Process))
}

func TestStopActiveLauncher_CustomKillFailureWithNoActiveMediaSucceeds(t *testing.T) {
	t.Parallel()

	// A launcher Kill that reports "no active game" is the expected answer
	// when Core is idle. Treating it as a stop failure blocked the next
	// launch, because preemption refuses to start over running media.
	p, currentMedia := stopTestPlatform(nil)
	p.setLastLauncher(&platforms.Launcher{
		ID:        "LaunchBox",
		Lifecycle: platforms.LifecycleExternal,
		Kill: func(*config.Instance) error {
			return errors.New("no active LaunchBox game")
		},
	})

	require.NoError(t, p.StopActiveLauncher(platforms.StopForPreemption))
	assert.Nil(t, currentMedia())
}

func TestStopActiveLauncher_DoesNotClearProcessTrackedDuringStop(t *testing.T) {
	t.Parallel()

	// The stop snapshotted no process, but the Steam tracker publishes one
	// while the custom Kill runs. Clearing on a nil snapshot would discard the
	// only handle to a game that is genuinely running.
	p, _ := stopTestPlatform(testActiveMedia())

	//nolint:gosec // Fixed command; ComSpec resolves the shell by absolute path.
	cmd := exec.CommandContext(context.Background(), helpers.ComSpec(), "/C", "timeout", "/T", "30")
	require.NoError(t, cmd.Start())
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			// Wait releases the goroutine CommandContext starts to watch the
			// context; without it that goroutine outlives the test.
			_ = cmd.Wait()
		}
	})

	p.setLastLauncher(&platforms.Launcher{
		ID:        "LaunchBox",
		Lifecycle: platforms.LifecycleExternal,
		Kill: func(*config.Instance) error {
			p.SetTrackedProcess(cmd.Process)
			return nil
		},
	})

	require.NoError(t, p.StopActiveLauncher(platforms.StopForMenu))

	p.processMu.RLock()
	tracked := p.trackedProcess
	p.processMu.RUnlock()
	assert.Equal(t, cmd.Process, tracked, "handle published during the stop must survive")
}

func TestStopActiveLauncher_FailedStopKeepsLauncherForRetry(t *testing.T) {
	t.Parallel()

	// A stop that fails must leave the launcher in place. Discarding it made
	// every retry fall through to "no mechanism" and report failure forever,
	// which also blocked launching anything else.
	p, currentMedia := stopTestPlatform(testActiveMedia())

	calls := 0
	p.setLastLauncher(&platforms.Launcher{
		ID:        "LaunchBox",
		Lifecycle: platforms.LifecycleExternal,
		Kill: func(*config.Instance) error {
			calls++
			if calls == 1 {
				return errors.New("plugin not connected")
			}
			return nil
		},
	})

	require.ErrorIs(t, p.StopActiveLauncher(platforms.StopForMenu), platforms.ErrStopFailed)
	assert.Equal(t, 1, calls)
	assert.NotNil(t, currentMedia(), "media stays while the game is still running")

	// The retry must reach the same Kill function and succeed.
	require.NoError(t, p.StopActiveLauncher(platforms.StopForMenu))
	assert.Equal(t, 2, calls, "retry must attempt the launcher Kill again")
	assert.Nil(t, currentMedia())
}
