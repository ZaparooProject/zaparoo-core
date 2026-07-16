//go:build linux

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

package mister

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	misterconfig "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockLauncherManager is a minimal mock for testing
type mockLauncherManager struct{}

func (*mockLauncherManager) GetContext() context.Context {
	return context.Background()
}

func (*mockLauncherManager) NewContext() context.Context {
	return context.Background()
}

func TestConfigureTLSRootFallback_ConfiguresDefaultsAndCustomTransports(t *testing.T) {
	restoreTLSRootFallbackHooks(t)

	expectedPaths := []string{
		misterconfig.UpdateAllDownloaderCACert,
		misterconfig.SystemCACert,
	}
	var receivedPaths []string
	zapLinkConfigured := false
	installerConfigured := false
	configureTLSDefaults = func(paths []string) (string, error) {
		receivedPaths = append([]string(nil), paths...)
		return misterconfig.UpdateAllDownloaderCACert, nil
	}
	configureZapLinkHTTPTransport = func() { zapLinkConfigured = true }
	configureInstallerHTTPTransport = func() { installerConfigured = true }

	configureTLSRootFallback()

	assert.Equal(t, expectedPaths, receivedPaths)
	assert.True(t, zapLinkConfigured)
	assert.True(t, installerConfigured)
}

func TestConfigureTLSRootFallback_SkipsCustomTransportsOnDefaultError(t *testing.T) {
	restoreTLSRootFallbackHooks(t)

	zapLinkConfigured := false
	installerConfigured := false
	configureTLSDefaults = func(_ []string) (string, error) {
		return "", errors.New("load roots")
	}
	configureZapLinkHTTPTransport = func() { zapLinkConfigured = true }
	configureInstallerHTTPTransport = func() { installerConfigured = true }

	configureTLSRootFallback()

	assert.False(t, zapLinkConfigured)
	assert.False(t, installerConfigured)
}

func restoreTLSRootFallbackHooks(t *testing.T) {
	t.Helper()

	oldConfigureTLSDefaults := configureTLSDefaults
	oldConfigureZapLinkHTTPTransport := configureZapLinkHTTPTransport
	oldConfigureInstallerHTTPTransport := configureInstallerHTTPTransport
	t.Cleanup(func() {
		configureTLSDefaults = oldConfigureTLSDefaults
		configureZapLinkHTTPTransport = oldConfigureZapLinkHTTPTransport
		configureInstallerHTTPTransport = oldConfigureInstallerHTTPTransport
	})
}

func TestIsWidgetScript(t *testing.T) {
	t.Parallel()

	assert.False(t, isWidgetScript("/media/fat/Scripts/test.sh", ""))
	assert.False(t, isWidgetScript("/media/fat/Scripts/zaparoo.sh", "-show-text"))
	assert.True(t, isWidgetScript("/media/fat/Scripts/zaparoo.sh", "'-show-text'"))
}

func TestScriptRunMode(t *testing.T) {
	t.Parallel()

	runScript, widget := scriptRunMode("/media/fat/Scripts/test.sh", "")
	assert.Equal(t, misterScriptRunFlag, runScript)
	assert.False(t, widget)

	runScript, widget = scriptRunMode("/media/fat/Scripts/zaparoo.sh", "'-show-text'")
	assert.Equal(t, misterWidgetRunFlag, runScript)
	assert.True(t, widget)
}

func TestRunScript_HiddenSetsMiSTerEnvironment(t *testing.T) {
	t.Parallel()

	if scriptIsActive() {
		t.Skip("MiSTer script already active")
	}

	tmpDir := t.TempDir()
	flagPath := filepath.Join(tmpDir, "run_flag")
	argPath := filepath.Join(tmpDir, "arg")
	scriptPath := filepath.Join(tmpDir, "script.sh")
	script := "#!/bin/sh\n" +
		"printf '%s' \"$ZAPAROO_RUN_SCRIPT\" > run_flag\n" +
		"printf '%s' \"$1\" > arg\n"
	require.NoError(t, os.WriteFile(scriptPath, []byte(script), 0o700)) //nolint:gosec // test script must be executable

	err := runScript(nil, scriptPath, "hello", true)
	require.NoError(t, err)

	flag, err := os.ReadFile(flagPath) //nolint:gosec // test reads from its temp dir
	require.NoError(t, err)
	assert.Equal(t, misterScriptRunFlag, string(flag))

	arg, err := os.ReadFile(argPath) //nolint:gosec // test reads from its temp dir
	require.NoError(t, err)
	assert.Equal(t, "hello", string(arg))
}

func TestLaunchSystem_MenuUsesLaunchMenu(t *testing.T) {
	t.Parallel()

	p := &Platform{}
	err := p.LaunchSystem(&config.Instance{}, "Menu")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to launch menu")
}

func TestStopActiveLauncher_CustomKill(t *testing.T) {
	t.Parallel()

	tests := []struct {
		customKillErr     error
		customKillFunc    func(*config.Instance) error
		name              string
		customKillCalled  bool
		hasTrackedProcess bool
		expectSignalKill  bool
	}{
		{
			name: "custom Kill function is called when defined",
			customKillFunc: func(_ *config.Instance) error {
				return nil
			},
			customKillCalled:  true,
			hasTrackedProcess: true,
			expectSignalKill:  false,
		},
		{
			name: "custom Kill function error is logged but not fatal",
			customKillFunc: func(_ *config.Instance) error {
				return assert.AnError
			},
			customKillCalled:  true,
			customKillErr:     assert.AnError,
			hasTrackedProcess: true,
			expectSignalKill:  false,
		},
		{
			name:              "signal-based kill used when no custom Kill defined",
			customKillFunc:    nil,
			customKillCalled:  false,
			hasTrackedProcess: true,
			expectSignalKill:  true,
		},
		{
			name:              "no kill attempted when no tracked process",
			customKillFunc:    nil,
			customKillCalled:  false,
			hasTrackedProcess: false,
			expectSignalKill:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Create platform instance
			p := NewPlatform()
			p.setActiveMedia = func(_ *models.ActiveMedia) {}

			// Track if custom Kill was called
			killCalled := false
			var launcher platforms.Launcher
			if tt.customKillFunc != nil {
				launcher.Kill = func(cfg *config.Instance) error {
					killCalled = true
					err := tt.customKillFunc(cfg)
					if err == nil {
						p.processMu.Lock()
						proc := p.trackedProcess
						p.processMu.Unlock()
						if proc != nil {
							_ = proc.Signal(syscall.SIGTERM)
						}
					}
					return err
				}
			}
			p.setLastLauncher(&launcher)

			// Set up tracked process if needed
			if tt.hasTrackedProcess {
				// Create a dummy process (sleep) that we can kill
				ctx := context.Background()
				cmd := exec.CommandContext(ctx, "sleep", "10")
				err := cmd.Start()
				require.NoError(t, err)
				defer func() {
					// Clean up process if still running
					if cmd.Process != nil {
						_ = cmd.Process.Kill()
					}
				}()
				p.SetTrackedProcess(cmd.Process)
			}

			// Call StopActiveLauncher
			err := p.StopActiveLauncher(platforms.StopForPreemption)

			// Verify no error from StopActiveLauncher itself
			require.NoError(t, err)

			// Verify custom Kill was called if expected
			assert.Equal(t, tt.customKillCalled, killCalled, "custom Kill called mismatch")
		})
	}
}

func TestStopActiveLauncher_DoesNotReuseStaleKillForScript(t *testing.T) {
	t.Parallel()

	p := NewPlatform()
	p.setActiveMedia = func(_ *models.ActiveMedia) {}
	killCalls := 0
	launcher := platforms.Launcher{Kill: func(*config.Instance) error {
		killCalls++
		p.processMu.Lock()
		proc := p.trackedProcess
		p.processMu.Unlock()
		return proc.Signal(syscall.SIGTERM)
	}}
	p.setLastLauncher(&launcher)

	first := exec.CommandContext(context.Background(), "sleep", "10")
	require.NoError(t, first.Start())
	p.SetTrackedProcess(first.Process)
	require.NoError(t, p.StopActiveLauncher(platforms.StopForMenu))
	assert.Equal(t, 1, killCalls)

	second := exec.CommandContext(context.Background(), "sleep", "10")
	require.NoError(t, second.Start())
	p.SetTrackedProcess(second.Process)
	require.NoError(t, p.StopActiveLauncher(platforms.StopForMenu))
	assert.Equal(t, 1, killCalls, "script stop reused stale launcher Kill hook")
}

func TestScummVMLauncher_HasCustomKill(t *testing.T) {
	t.Parallel()

	p := NewPlatform()
	launcher := createScummVMLauncher(p)

	// Verify ScummVM launcher has a custom Kill function
	// This is important because ScummVM requires keyboard-based exit (Ctrl+q)
	// instead of signal-based termination to avoid VT lock issues on MiSTer
	assert.NotNil(t, launcher.Kill, "ScummVM launcher should have custom Kill function")

	// Note: We can't actually test keyboard input without initializing the
	// keyboard device, which requires uinput access. The function signature
	// and presence is what matters for the platform to use it correctly.
}

func TestStopActiveLauncher_WaitsForCleanup(t *testing.T) {
	t.Parallel()

	p := NewPlatform()
	p.launcherManager = &mockLauncherManager{}
	p.setActiveMedia = func(_ *models.ActiveMedia) {} // Required by StopActiveLauncher

	// Simulate a tracked process with cleanup channel
	done := make(chan struct{})
	p.processMu.Lock()
	p.trackedProcess = &os.Process{Pid: 99999} // Fake PID
	p.processDone = done
	p.processMu.Unlock()

	// Track when StopActiveLauncher returns
	stopReturned := make(chan struct{})

	// Call StopActiveLauncher in goroutine
	go func() {
		_ = p.StopActiveLauncher(platforms.StopForPreemption)
		close(stopReturned)
	}()

	// Give it a moment to start waiting
	time.Sleep(50 * time.Millisecond)

	// Verify StopActiveLauncher hasn't returned yet (should be blocked on channel)
	select {
	case <-stopReturned:
		t.Fatal("StopActiveLauncher returned before cleanup completed")
	default:
		// Good - still waiting
	}

	// Now signal cleanup completion
	close(done)

	// StopActiveLauncher should now return
	select {
	case <-stopReturned:
		// Good - returned after cleanup
	case <-time.After(500 * time.Millisecond):
		t.Fatal("StopActiveLauncher did not return after cleanup completed")
	}
}

func TestReturnToMenu_StopsTrackedConsoleProcess(t *testing.T) {
	t.Parallel()

	cmd := exec.CommandContext(context.Background(), "sleep", "10")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	p := NewPlatform()
	p.setActiveMedia = func(_ *models.ActiveMedia) {}
	restoreDone := make(chan struct{})
	proc, err := runTrackedProcess(p, cmd, func() { close(restoreDone) }, "return-to-menu-test")
	require.NoError(t, err)

	require.NoError(t, p.ReturnToMenu())
	select {
	case <-restoreDone:
	case <-time.After(time.Second):
		t.Fatal("console cleanup did not complete before ReturnToMenu returned")
	}
	require.Eventually(t, func() bool {
		return errors.Is(syscall.Kill(proc.Pid, 0), syscall.ESRCH)
	}, time.Second, 10*time.Millisecond, "tracked console process survived ReturnToMenu")
}

func TestStopActiveLauncher_KillsProcessGroupBeforeCleanup(t *testing.T) {
	pidPath := filepath.Join(t.TempDir(), "child.pid")
	cmd := exec.CommandContext( //nolint:gosec // Fixed test shell; only temp path is variable.
		context.Background(),
		"sh",
		"-c",
		`trap 'exit 0' TERM; sh -c 'trap "" TERM; while :; do sleep 1; done' & echo $! > "$1"; wait`,
		"sh",
		pidPath,
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	p := NewPlatform()
	p.setActiveMedia = func(_ *models.ActiveMedia) {}
	restoreDone := make(chan struct{})
	_, err := runTrackedProcess(p, cmd, func() { close(restoreDone) }, "group-test")
	require.NoError(t, err)

	var childPID int
	require.Eventually(t, func() bool {
		contents, readErr := os.ReadFile(pidPath) //nolint:gosec // Test-owned temporary file.
		if readErr != nil {
			return false
		}
		childPID, readErr = strconv.Atoi(strings.TrimSpace(string(contents)))
		return readErr == nil
	}, time.Second, 10*time.Millisecond)

	require.NoError(t, p.StopActiveLauncher(platforms.StopForMenu))
	select {
	case <-restoreDone:
	case <-time.After(time.Second):
		t.Fatal("console cleanup did not complete before stop returned")
	}

	require.Eventually(t, func() bool {
		return errors.Is(syscall.Kill(childPID, 0), syscall.ESRCH)
	}, time.Second, 10*time.Millisecond, "descendant process survived console cleanup")
}

func TestArcadeCardLaunchCache(t *testing.T) {
	t.Parallel()

	t.Run("SetArcadeCardLaunch stores setname and timestamp", func(t *testing.T) {
		t.Parallel()

		p := NewPlatform()
		p.SetArcadeCardLaunch("sf2")

		// Verify setname was stored (indirectly via CheckAndClearArcadeCardLaunch)
		result := p.CheckAndClearArcadeCardLaunch("sf2")
		assert.True(t, result, "should match freshly cached setname")
	})

	t.Run("CheckAndClearArcadeCardLaunch returns false for empty cache", func(t *testing.T) {
		t.Parallel()

		p := NewPlatform()
		result := p.CheckAndClearArcadeCardLaunch("pacman")

		assert.False(t, result, "should return false for empty cache")
	})

	t.Run("CheckAndClearArcadeCardLaunch returns false for mismatched setname", func(t *testing.T) {
		t.Parallel()

		p := NewPlatform()
		p.SetArcadeCardLaunch("sf2")

		result := p.CheckAndClearArcadeCardLaunch("pacman")
		assert.False(t, result, "should return false for mismatched setname")

		// Original setname should still be in cache
		result2 := p.CheckAndClearArcadeCardLaunch("sf2")
		assert.True(t, result2, "original setname should still be cached")
	})

	t.Run("CheckAndClearArcadeCardLaunch clears cache on match", func(t *testing.T) {
		t.Parallel()

		p := NewPlatform()
		p.SetArcadeCardLaunch("dkong")

		// First check should match and clear
		result1 := p.CheckAndClearArcadeCardLaunch("dkong")
		assert.True(t, result1, "first check should match")

		// Second check should return false (cache cleared)
		result2 := p.CheckAndClearArcadeCardLaunch("dkong")
		assert.False(t, result2, "second check should return false (cache cleared)")
	})

	t.Run("CheckAndClearArcadeCardLaunch handles case sensitivity", func(t *testing.T) {
		t.Parallel()

		p := NewPlatform()
		p.SetArcadeCardLaunch("StreetFighter2")

		// Should match exactly
		result := p.CheckAndClearArcadeCardLaunch("StreetFighter2")
		assert.True(t, result, "should match exact case")
	})

	t.Run("CheckAndClearArcadeCardLaunch returns false and clears stale cache", func(t *testing.T) {
		t.Parallel()

		p := NewPlatform()
		p.SetArcadeCardLaunch("mk2")

		// Manually set timestamp to 16 seconds ago (past the 15 second window)
		p.arcadeCardLaunch.mu.Lock()
		p.arcadeCardLaunch.timestamp = p.arcadeCardLaunch.timestamp.Add(-16e9) // -16 seconds in nanoseconds
		p.arcadeCardLaunch.mu.Unlock()

		result := p.CheckAndClearArcadeCardLaunch("mk2")
		assert.False(t, result, "should return false for stale cache (>15s)")

		// Cache should be cleared
		p.arcadeCardLaunch.mu.RLock()
		cached := p.arcadeCardLaunch.setname
		p.arcadeCardLaunch.mu.RUnlock()
		assert.Empty(t, cached, "cache should be cleared after stale check")
	})

	t.Run("concurrent SetArcadeCardLaunch and CheckAndClearArcadeCardLaunch", func(t *testing.T) {
		t.Parallel()

		p := NewPlatform()

		// Launch concurrent goroutines
		const goroutines = 10
		done := make(chan bool, goroutines)

		for i := range goroutines {
			go func(index int) {
				if index%2 == 0 {
					p.SetArcadeCardLaunch("concurrent")
				} else {
					_ = p.CheckAndClearArcadeCardLaunch("concurrent")
				}
				done <- true
			}(i)
		}

		// Wait for all goroutines to complete
		for range goroutines {
			<-done
		}

		// Test should complete without race conditions
		// (run with go test -race to verify)
	})
}

// TestGamepadPress_DisabledReturnsError tests that GamepadPress returns an error
// when the virtual gamepad is disabled (gpd.Device is nil).
func TestGamepadPress_DisabledReturnsError(t *testing.T) {
	t.Parallel()

	// Create platform with zero-value gamepad (Device will be nil)
	platform := NewPlatform()

	// Attempt to press a button
	err := platform.GamepadPress("a")

	// Should return error indicating gamepad is disabled
	require.Error(t, err)
	assert.Contains(t, err.Error(), "virtual gamepad is disabled")
}

// TestGamepadPress_ValidButtonsWhenDisabled tests various button names return the same disabled error
func TestGamepadPress_ValidButtonsWhenDisabled(t *testing.T) {
	t.Parallel()

	platform := NewPlatform()

	buttons := []string{"a", "b", "x", "y", "start", "select", "up", "down", "left", "right"}
	for _, button := range buttons {
		t.Run(button, func(t *testing.T) {
			t.Parallel()
			err := platform.GamepadPress(button)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "virtual gamepad is disabled")
		})
	}
}

// TestPresentUI_NoDeadlockWithActiveMedia guards against holding platformMu
// while renderer startup calls StopActiveLauncher, which also needs platformMu.
func TestPresentUI_NoDeadlockWithActiveMedia(t *testing.T) {
	t.Parallel()

	p := NewPlatform()
	p.launcherManager = &mockLauncherManager{}
	p.setActiveMedia = func(_ *models.ActiveMedia) {}

	// Set activeMedia function to return active media state
	// This triggers the StopActiveLauncher path in runScript
	p.activeMedia = func() *models.ActiveMedia {
		return &models.ActiveMedia{
			SystemID: "TestSystem",
			Path:     "/test/game.rom",
		}
	}

	// Run PresentUI in a goroutine with timeout detection.
	done := make(chan struct{})
	go func() {
		// This will fail (no actual script/console), but should NOT deadlock.
		_, _ = p.PresentUI(t.Context(), &models.UIEvent{
			Kind:    models.UIEventKindLoader,
			Message: "Test loader",
		})
		close(done)
	}()

	// If deadlock occurs, this will timeout
	select {
	case <-done:
		// Success - no deadlock
	case <-time.After(5 * time.Second):
		t.Fatal("PresentUI deadlocked while stopping active launcher")
	}
}
