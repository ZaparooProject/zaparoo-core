//go:build linux

// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
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
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
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
					return tt.customKillFunc(cfg)
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
