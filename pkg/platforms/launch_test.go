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

package platforms_test

import (
	"os"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestResolveAction(t *testing.T) {
	t.Parallel()

	t.Run("opts_action_takes_precedence", func(t *testing.T) {
		t.Parallel()

		fs := helpers.NewMemoryFS()
		cfg, err := helpers.NewTestConfig(fs, t.TempDir())
		require.NoError(t, err)

		// Configure launcher with action=details in config
		cfg.SetLauncherDefaultsForTesting([]config.LaunchersDefault{
			{Launcher: "Steam", Action: "details"},
		})

		// Opts specifies action=run, should override config
		opts := &platforms.LaunchOptions{Action: "run"}
		action := platforms.ResolveAction(opts, cfg, "Steam")

		assert.Equal(t, "run", action, "Options action should override config")
	})

	t.Run("uses_config_action_when_opts_action_empty", func(t *testing.T) {
		t.Parallel()

		fs := helpers.NewMemoryFS()
		cfg, err := helpers.NewTestConfig(fs, t.TempDir())
		require.NoError(t, err)

		// Configure launcher with action=details in config
		cfg.SetLauncherDefaultsForTesting([]config.LaunchersDefault{
			{Launcher: "Steam", Action: "details"},
		})

		// Opts has empty action
		opts := &platforms.LaunchOptions{Action: ""}
		action := platforms.ResolveAction(opts, cfg, "Steam")

		assert.Equal(t, "details", action, "Should use config action when opts.Action is empty")
	})

	t.Run("uses_config_action_when_opts_is_nil", func(t *testing.T) {
		t.Parallel()

		fs := helpers.NewMemoryFS()
		cfg, err := helpers.NewTestConfig(fs, t.TempDir())
		require.NoError(t, err)

		// Configure launcher with action=details in config
		cfg.SetLauncherDefaultsForTesting([]config.LaunchersDefault{
			{Launcher: "Steam", Action: "details"},
		})

		action := platforms.ResolveAction(nil, cfg, "Steam")

		assert.Equal(t, "details", action, "Should use config action when opts is nil")
	})

	t.Run("returns_empty_when_no_action_configured", func(t *testing.T) {
		t.Parallel()

		fs := helpers.NewMemoryFS()
		cfg, err := helpers.NewTestConfig(fs, t.TempDir())
		require.NoError(t, err)

		// No launcher defaults configured
		action := platforms.ResolveAction(nil, cfg, "Steam")

		assert.Empty(t, action, "Should return empty when no action configured")
	})

	t.Run("returns_empty_when_cfg_is_nil", func(t *testing.T) {
		t.Parallel()

		action := platforms.ResolveAction(nil, nil, "Steam")

		assert.Empty(t, action, "Should return empty when cfg is nil")
	})

	t.Run("returns_empty_when_launcher_id_empty", func(t *testing.T) {
		t.Parallel()

		fs := helpers.NewMemoryFS()
		cfg, err := helpers.NewTestConfig(fs, t.TempDir())
		require.NoError(t, err)

		cfg.SetLauncherDefaultsForTesting([]config.LaunchersDefault{
			{Launcher: "Steam", Action: "details"},
		})

		action := platforms.ResolveAction(nil, cfg, "")

		assert.Empty(t, action, "Should return empty when launcherID is empty")
	})
}

func TestIsActionDetails(t *testing.T) {
	t.Parallel()

	tests := []struct {
		action   string
		expected bool
	}{
		{"details", true},
		{"Details", true},
		{"DETAILS", true},
		{"run", false},
		{"", false},
		{"detail", false},
	}

	for _, tt := range tests {
		t.Run(tt.action, func(t *testing.T) {
			t.Parallel()
			result := platforms.IsActionDetails(tt.action)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestDoLaunch_RunningInstanceLauncher(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                string
		usesRunningInstance string
		expectStopCalled    bool
	}{
		{
			name:                "running_instance_launcher_skips_stop",
			usesRunningInstance: platforms.InstanceKodi,
			expectStopCalled:    false,
		},
		{
			name:                "regular_launcher_calls_stop",
			usesRunningInstance: "",
			expectStopCalled:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Create mock platform
			mockPlatform := mocks.NewMockPlatform()

			// Track if StopActiveLauncher was called
			if tt.expectStopCalled {
				mockPlatform.On("StopActiveLauncher", platforms.StopForPreemption).Return(nil).Once()
			}

			// Track SetTrackedProcess call (for LifecycleTracked launchers)
			mockPlatform.On("SetTrackedProcess", mock.AnythingOfType("*os.Process")).Return().Maybe()

			// Create a simple launcher with the specified UsesRunningInstance value
			launcher := &platforms.Launcher{
				ID:                  "test-launcher",
				SystemID:            "test-system",
				UsesRunningInstance: tt.usesRunningInstance,
				Lifecycle:           platforms.LifecycleTracked,
				Launch: func(*config.Instance, string, *platforms.LaunchOptions) (*os.Process, error) {
					// Return a mock process (use current process for testing)
					return &os.Process{Pid: os.Getpid()}, nil
				},
			}

			// Track active media
			var activeMedia *models.ActiveMedia
			setActiveMedia := func(media *models.ActiveMedia) {
				activeMedia = media
			}

			// Create launch params
			params := &platforms.LaunchParams{
				Platform:       mockPlatform,
				Config:         &config.Instance{},
				SetActiveMedia: setActiveMedia,
				Launcher:       launcher,
				DB:             nil,
				Path:           "/test/path.rom",
			}

			// Execute DoLaunch with a simple getDisplayName function
			getDisplayName := func(_ string) string {
				return "path" // Simple extraction for test
			}
			err := platforms.DoLaunch(params, getDisplayName)

			// Verify results
			require.NoError(t, err)
			assert.NotNil(t, activeMedia, "Active media should be set")

			// Verify mock expectations
			mockPlatform.AssertExpectations(t)
		})
	}
}

func TestDoLaunch_LifecycleModes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                string
		usesRunningInstance string
		lifecycle           platforms.LauncherLifecycle
		expectStopCalled    bool
		launchReturnsProc   bool
	}{
		{
			name:                "tracked_launcher_without_running_instance",
			lifecycle:           platforms.LifecycleTracked,
			usesRunningInstance: "",
			expectStopCalled:    true,
			launchReturnsProc:   true,
		},
		{
			name:                "tracked_launcher_with_running_instance",
			lifecycle:           platforms.LifecycleTracked,
			usesRunningInstance: platforms.InstanceKodi,
			expectStopCalled:    false,
			launchReturnsProc:   true,
		},
		{
			name:                "fire_and_forget_without_running_instance",
			lifecycle:           platforms.LifecycleFireAndForget,
			usesRunningInstance: "",
			expectStopCalled:    true,
			launchReturnsProc:   false,
		},
		{
			name:                "fire_and_forget_with_running_instance",
			lifecycle:           platforms.LifecycleFireAndForget,
			usesRunningInstance: platforms.InstanceKodi,
			expectStopCalled:    false,
			launchReturnsProc:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Create mock platform
			mockPlatform := mocks.NewMockPlatform()

			// Setup expectations
			if tt.expectStopCalled {
				mockPlatform.On("StopActiveLauncher", platforms.StopForPreemption).Return(nil).Once()
			}

			if tt.launchReturnsProc {
				mockPlatform.On("SetTrackedProcess", mock.AnythingOfType("*os.Process")).Return().Once()
			}

			// Create launcher
			launcher := &platforms.Launcher{
				ID:                  "test-launcher",
				SystemID:            "test-system",
				UsesRunningInstance: tt.usesRunningInstance,
				Lifecycle:           tt.lifecycle,
				Launch: func(*config.Instance, string, *platforms.LaunchOptions) (*os.Process, error) {
					if tt.launchReturnsProc {
						return &os.Process{Pid: os.Getpid()}, nil
					}
					// Fire-and-forget launcher returns no process handle
					var noProcess *os.Process
					return noProcess, nil
				},
			}

			// Track active media
			var activeMedia *models.ActiveMedia
			setActiveMedia := func(media *models.ActiveMedia) {
				activeMedia = media
			}

			// Create launch params
			params := &platforms.LaunchParams{
				Platform:       mockPlatform,
				Config:         &config.Instance{},
				SetActiveMedia: setActiveMedia,
				Launcher:       launcher,
				DB:             nil,
				Path:           "/test/path.rom",
			}

			// Execute DoLaunch with a simple getDisplayName function
			getDisplayName := func(_ string) string {
				return "path" // Simple extraction for test
			}
			err := platforms.DoLaunch(params, getDisplayName)

			// Verify results
			require.NoError(t, err)
			assert.NotNil(t, activeMedia, "Active media should be set")

			// Verify mock expectations
			mockPlatform.AssertExpectations(t)
		})
	}
}
