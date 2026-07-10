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

package platforms_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestResolveAction(t *testing.T) {
	t.Parallel()

	steamLauncher := &platforms.Launcher{ID: "Steam"}
	steamWithGroups := &platforms.Launcher{ID: "Steam", Groups: []string{"PC"}}
	emptyLauncher := &platforms.Launcher{ID: ""}

	t.Run("opts_action_takes_precedence", func(t *testing.T) {
		t.Parallel()

		fs := helpers.NewMemoryFS()
		cfg, err := helpers.NewTestConfig(fs, t.TempDir())
		require.NoError(t, err)

		// Configure launcher with action=details in config
		require.NoError(t, cfg.LoadTOML(`
[[launchers.default]]
launcher = "Steam"
action = "details"
`))

		// Opts specifies action=run, should override config
		opts := &platforms.LaunchOptions{Action: "run"}
		action := platforms.ResolveAction(opts, cfg, steamLauncher)

		assert.Equal(t, "run", action, "Options action should override config")
	})

	t.Run("uses_config_action_when_opts_action_empty", func(t *testing.T) {
		t.Parallel()

		fs := helpers.NewMemoryFS()
		cfg, err := helpers.NewTestConfig(fs, t.TempDir())
		require.NoError(t, err)

		// Configure launcher with action=details in config
		require.NoError(t, cfg.LoadTOML(`
[[launchers.default]]
launcher = "Steam"
action = "details"
`))

		// Opts has empty action
		opts := &platforms.LaunchOptions{Action: ""}
		action := platforms.ResolveAction(opts, cfg, steamLauncher)

		assert.Equal(t, "details", action, "Should use config action when opts.Action is empty")
	})

	t.Run("uses_config_action_when_opts_is_nil", func(t *testing.T) {
		t.Parallel()

		fs := helpers.NewMemoryFS()
		cfg, err := helpers.NewTestConfig(fs, t.TempDir())
		require.NoError(t, err)

		// Configure launcher with action=details in config
		require.NoError(t, cfg.LoadTOML(`
[[launchers.default]]
launcher = "Steam"
action = "details"
`))

		action := platforms.ResolveAction(nil, cfg, steamLauncher)

		assert.Equal(t, "details", action, "Should use config action when opts is nil")
	})

	t.Run("returns_empty_when_no_action_configured", func(t *testing.T) {
		t.Parallel()

		fs := helpers.NewMemoryFS()
		cfg, err := helpers.NewTestConfig(fs, t.TempDir())
		require.NoError(t, err)

		// No launcher defaults configured
		action := platforms.ResolveAction(nil, cfg, steamLauncher)

		assert.Empty(t, action, "Should return empty when no action configured")
	})

	t.Run("returns_empty_when_cfg_is_nil", func(t *testing.T) {
		t.Parallel()

		action := platforms.ResolveAction(nil, nil, steamLauncher)

		assert.Empty(t, action, "Should return empty when cfg is nil")
	})

	t.Run("returns_empty_when_launcher_id_empty", func(t *testing.T) {
		t.Parallel()

		fs := helpers.NewMemoryFS()
		cfg, err := helpers.NewTestConfig(fs, t.TempDir())
		require.NoError(t, err)

		require.NoError(t, cfg.LoadTOML(`
[[launchers.default]]
launcher = "Steam"
action = "details"
`))

		action := platforms.ResolveAction(nil, cfg, emptyLauncher)

		assert.Empty(t, action, "Should return empty when launcherID is empty")
	})

	t.Run("uses_group_based_config_lookup", func(t *testing.T) {
		t.Parallel()

		fs := helpers.NewMemoryFS()
		cfg, err := helpers.NewTestConfig(fs, t.TempDir())
		require.NoError(t, err)

		// Configure action for "PC" group, not specific launcher ID
		require.NoError(t, cfg.LoadTOML(`
[[launchers.default]]
launcher = "PC"
action = "browse"
`))

		action := platforms.ResolveAction(nil, cfg, steamWithGroups)

		assert.Equal(t, "browse", action, "Should match group in config")
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

func TestDoLaunch_DetailsActionSkipsActiveMedia(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("StopActiveLauncher", platforms.StopForPreemption).Return(nil).Once()

	launcher := &platforms.Launcher{
		ID:        "Steam",
		SystemID:  "pc",
		Lifecycle: platforms.LifecycleFireAndForget,
		Launch: func(*config.Instance, string, *platforms.LaunchOptions) (*os.Process, error) {
			// Fire-and-forget launcher returns no process handle
			var noProcess *os.Process
			return noProcess, nil
		},
	}

	var activeMedia *models.ActiveMedia
	setActiveMedia := func(media *models.ActiveMedia) {
		activeMedia = media
	}

	params := &platforms.LaunchParams{
		Platform:       mockPlatform,
		Config:         &config.Instance{},
		SetActiveMedia: setActiveMedia,
		Launcher:       launcher,
		DB:             nil,
		Path:           "steam://12345/Game",
		Options:        &platforms.LaunchOptions{Action: "details"},
	}

	err := platforms.DoLaunch(params, func(_ string) string { return "Game" })

	require.NoError(t, err)
	assert.Nil(t, activeMedia, "ActiveMedia should NOT be set for details action")
	mockPlatform.AssertExpectations(t)
}

func TestDoLaunch_LaunchError(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("StopActiveLauncher", platforms.StopForPreemption).Return(nil).Once()

	launcher := &platforms.Launcher{
		ID:        "test-launcher",
		SystemID:  "test-system",
		Lifecycle: platforms.LifecycleFireAndForget,
		Launch: func(*config.Instance, string, *platforms.LaunchOptions) (*os.Process, error) {
			return nil, assert.AnError
		},
	}

	params := &platforms.LaunchParams{
		Platform:       mockPlatform,
		Config:         &config.Instance{},
		SetActiveMedia: func(*models.ActiveMedia) {},
		Launcher:       launcher,
		DB:             nil,
		Path:           "/test/path.rom",
	}

	err := platforms.DoLaunch(params, func(_ string) string { return "path" })

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to launch")
	mockPlatform.AssertExpectations(t)
}

func TestDoLaunch_BlockingLaunchError(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("StopActiveLauncher", platforms.StopForPreemption).Return(nil).Once()

	launchCalled := false
	launcher := &platforms.Launcher{
		ID:        "blocking-launcher",
		SystemID:  "test-system",
		Lifecycle: platforms.LifecycleBlocking,
		Launch: func(*config.Instance, string, *platforms.LaunchOptions) (*os.Process, error) {
			launchCalled = true
			return nil, assert.AnError
		},
	}

	var activeMedia *models.ActiveMedia
	params := &platforms.LaunchParams{
		Platform:       mockPlatform,
		Config:         &config.Instance{},
		SetActiveMedia: func(media *models.ActiveMedia) { activeMedia = media },
		Launcher:       launcher,
		Path:           filepath.Join("test", "path.rom"),
	}

	err := platforms.DoLaunch(params, func(_ string) string { return "path" })

	require.Error(t, err)
	require.ErrorIs(t, err, assert.AnError)
	assert.True(t, launchCalled, "blocking launch must run before DoLaunch returns")
	assert.Nil(t, activeMedia)
	mockPlatform.AssertExpectations(t)
}

func TestDoLaunch_BlockingProcessCannotLeaveStaleActiveMedia(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("StopActiveLauncher", platforms.StopForPreemption).Return(nil).Once()
	mockPlatform.On("SetTrackedProcess", mock.AnythingOfType("*os.Process")).Return().Once()

	launcher := &platforms.Launcher{
		ID:        "blocking-launcher",
		SystemID:  "test-system",
		Lifecycle: platforms.LifecycleBlocking,
		Launch: func(*config.Instance, string, *platforms.LaunchOptions) (*os.Process, error) {
			cmd := exec.CommandContext(context.Background(), "true")
			require.NoError(t, cmd.Start())
			require.NoError(t, cmd.Wait())
			return cmd.Process, nil
		},
	}
	mediaUpdates := make(chan *models.ActiveMedia, 2)
	params := &platforms.LaunchParams{
		Platform:       mockPlatform,
		Config:         &config.Instance{},
		SetActiveMedia: func(media *models.ActiveMedia) { mediaUpdates <- media },
		Launcher:       launcher,
		Path:           filepath.Join("test", "path.rom"),
	}

	require.NoError(t, platforms.DoLaunch(params, func(_ string) string { return "path" }))
	select {
	case active := <-mediaUpdates:
		require.NotNil(t, active)
	case <-time.After(time.Second):
		t.Fatal("ActiveMedia was not published")
	}
	select {
	case cleared := <-mediaUpdates:
		assert.Nil(t, cleared)
	case <-time.After(time.Second):
		t.Fatal("completed process did not clear ActiveMedia")
	}
	mockPlatform.AssertExpectations(t)
}

func TestDoLaunch_NoSystemIDSkipsActiveMedia(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("StopActiveLauncher", platforms.StopForPreemption).Return(nil).Once()

	launcher := &platforms.Launcher{
		ID:        "test-launcher",
		SystemID:  "", // No SystemID
		Lifecycle: platforms.LifecycleFireAndForget,
		Launch: func(*config.Instance, string, *platforms.LaunchOptions) (*os.Process, error) {
			// Fire-and-forget launcher returns no process handle
			var noProcess *os.Process
			return noProcess, nil
		},
	}

	var activeMedia *models.ActiveMedia
	setActiveMedia := func(media *models.ActiveMedia) {
		activeMedia = media
	}

	params := &platforms.LaunchParams{
		Platform:       mockPlatform,
		Config:         &config.Instance{},
		SetActiveMedia: setActiveMedia,
		Launcher:       launcher,
		DB:             nil,
		Path:           "/test/path.rom",
	}

	err := platforms.DoLaunch(params, func(_ string) string { return "path" })

	require.NoError(t, err)
	assert.Nil(t, activeMedia, "ActiveMedia should NOT be set when no SystemID")
	mockPlatform.AssertExpectations(t)
}

func TestDoLaunch_ActiveMediaLookupUsesProvidedContext(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("StopActiveLauncher", platforms.StopForPreemption).Return(nil).Once()

	mockMediaDB := helpers.NewMockMediaDBI()
	mockMediaDB.On("SearchMediaPathExact",
		mock.MatchedBy(func(ctx context.Context) bool {
			return ctx.Err() != nil
		}),
		mock.Anything,
		"/test/path.rom",
	).Return([]database.SearchResult{}, context.Canceled).Once()

	launcher := &platforms.Launcher{
		ID:        "test-launcher",
		SystemID:  "test-system",
		Lifecycle: platforms.LifecycleFireAndForget,
		Launch: func(*config.Instance, string, *platforms.LaunchOptions) (*os.Process, error) {
			return &os.Process{}, nil
		},
	}

	var activeMedia *models.ActiveMedia
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	params := &platforms.LaunchParams{
		Context:        ctx,
		Platform:       mockPlatform,
		Config:         &config.Instance{},
		SetActiveMedia: func(media *models.ActiveMedia) { activeMedia = media },
		Launcher:       launcher,
		DB:             &database.Database{MediaDB: mockMediaDB},
		Path:           "/test/path.rom",
	}

	err := platforms.DoLaunch(params, func(_ string) string { return "path" })

	require.NoError(t, err)
	require.NotNil(t, activeMedia)
	assert.Equal(t, "path", activeMedia.Name)
	mockMediaDB.AssertExpectations(t)
	mockPlatform.AssertExpectations(t)
}

func TestDoLaunch_NilLaunchReturnsError(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()

	launcher := &platforms.Launcher{
		ID:       "no-launch-func",
		SystemID: "test-system",
		Launch:   nil,
	}

	params := &platforms.LaunchParams{
		Platform:       mockPlatform,
		Config:         &config.Instance{},
		SetActiveMedia: func(*models.ActiveMedia) {},
		Launcher:       launcher,
		DB:             nil,
		Path:           "/test/path.rom",
	}

	err := platforms.DoLaunch(params, func(_ string) string { return "path" })

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no-launch-func")
	assert.Contains(t, err.Error(), "no launch function configured")
	mockPlatform.AssertNotCalled(t, "StopActiveLauncher", platforms.StopForPreemption)
	mockPlatform.AssertExpectations(t)
}

func TestDoLaunch_TrackedLauncherError(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("StopActiveLauncher", platforms.StopForPreemption).Return(nil).Once()

	launcher := &platforms.Launcher{
		ID:        "test-launcher",
		SystemID:  "test-system",
		Lifecycle: platforms.LifecycleTracked,
		Launch: func(*config.Instance, string, *platforms.LaunchOptions) (*os.Process, error) {
			return nil, assert.AnError
		},
	}

	params := &platforms.LaunchParams{
		Platform:       mockPlatform,
		Config:         &config.Instance{},
		SetActiveMedia: func(*models.ActiveMedia) {},
		Launcher:       launcher,
		DB:             nil,
		Path:           "/test/path.rom",
	}

	err := platforms.DoLaunch(params, func(_ string) string { return "path" })

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to launch")
	mockPlatform.AssertExpectations(t)
}

func TestDoLaunch_NativeLaunchPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		inputPath         string
		expectedLaunchArg string
	}{
		{
			name:              "file_path_converted_to_native",
			inputPath:         "C:/Games/SNES/mario.sfc",
			expectedLaunchArg: filepath.FromSlash("C:/Games/SNES/mario.sfc"),
		},
		{
			name:              "uri_path_unchanged",
			inputPath:         "steam://run/12345",
			expectedLaunchArg: "steam://run/12345",
		},
		{
			name:              "kodi_uri_unchanged",
			inputPath:         "kodi://movies/12345",
			expectedLaunchArg: "kodi://movies/12345",
		},
		{
			name:              "unix_style_path_converted_to_native",
			inputPath:         "/home/user/roms/game.nes",
			expectedLaunchArg: filepath.FromSlash("/home/user/roms/game.nes"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mockPlatform := mocks.NewMockPlatform()
			mockPlatform.On("StopActiveLauncher", platforms.StopForPreemption).Return(nil).Once()

			var capturedPath string
			launcher := &platforms.Launcher{
				ID:        "test-launcher",
				SystemID:  "test-system",
				Lifecycle: platforms.LifecycleFireAndForget,
				Launch: func(_ *config.Instance, path string, _ *platforms.LaunchOptions) (*os.Process, error) {
					capturedPath = path
					var noProcess *os.Process
					return noProcess, nil
				},
			}

			params := &platforms.LaunchParams{
				Platform:       mockPlatform,
				Config:         &config.Instance{},
				SetActiveMedia: func(*models.ActiveMedia) {},
				Launcher:       launcher,
				DB:             nil,
				Path:           tt.inputPath,
			}

			err := platforms.DoLaunch(params, func(_ string) string { return "game" })
			require.NoError(t, err)

			assert.Equal(t, tt.expectedLaunchArg, capturedPath,
				"Launch should receive OS-native path")

			mockPlatform.AssertExpectations(t)
		})
	}
}

func TestDoLaunch_InvalidSlotReturnsError(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	launcher := &platforms.Launcher{
		ID:        platforms.NativeAudioLauncherID,
		Lifecycle: platforms.LifecycleFireAndForget,
		Launch: func(*config.Instance, string, *platforms.LaunchOptions) (*os.Process, error) {
			return &os.Process{Pid: os.Getpid()}, nil
		},
	}
	params := &platforms.LaunchParams{
		Platform:       mockPlatform,
		Config:         &config.Instance{},
		SetActiveMedia: func(*models.ActiveMedia) {},
		Launcher:       launcher,
		Path:           filepath.Join(string(os.PathSeparator), "song.mp3"),
		Options:        &platforms.LaunchOptions{Slot: "invalid-slot-value"},
	}

	err := platforms.DoLaunch(params, func(s string) string { return s })
	require.Error(t, err)
	assert.Contains(t, err.Error(), "normalize slot")
}

func TestDoLaunch_BackgroundSlotWrongLauncherReturnsError(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("StopActiveLauncher", platforms.StopForPreemption).Return(nil).Maybe()

	launcher := &platforms.Launcher{
		ID:        "some-other-launcher",
		SystemID:  "NES",
		Lifecycle: platforms.LifecycleFireAndForget,
		Launch: func(*config.Instance, string, *platforms.LaunchOptions) (*os.Process, error) {
			return &os.Process{Pid: os.Getpid()}, nil
		},
	}
	params := &platforms.LaunchParams{
		Platform:       mockPlatform,
		Config:         &config.Instance{},
		SetActiveMedia: func(*models.ActiveMedia) {},
		Launcher:       launcher,
		Path:           filepath.Join(string(os.PathSeparator), "game.nes"),
		Options:        &platforms.LaunchOptions{Slot: "background"},
	}

	err := platforms.DoLaunch(params, func(s string) string { return s })
	require.Error(t, err)
	assert.Contains(t, err.Error(), platforms.NativeAudioLauncherID)
}

func TestDoLaunch_BackgroundSlotReturnsEarlyWithoutSettingActiveMedia(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()

	launched := false
	launcher := &platforms.Launcher{
		ID:        platforms.NativeAudioLauncherID,
		Lifecycle: platforms.LifecycleFireAndForget,
		Launch: func(*config.Instance, string, *platforms.LaunchOptions) (*os.Process, error) {
			launched = true
			return &os.Process{Pid: os.Getpid()}, nil
		},
	}
	var activeMedia *models.ActiveMedia
	params := &platforms.LaunchParams{
		Platform:       mockPlatform,
		Config:         &config.Instance{},
		SetActiveMedia: func(m *models.ActiveMedia) { activeMedia = m },
		Launcher:       launcher,
		Path:           filepath.Join(string(os.PathSeparator), "song.mp3"),
		Options:        &platforms.LaunchOptions{Slot: "background"},
	}

	err := platforms.DoLaunch(params, func(s string) string { return s })
	require.NoError(t, err)
	assert.True(t, launched, "launcher must be called")
	assert.Nil(t, activeMedia, "background slot must not set active media")
}

func TestKeyboardControls_EmptyActions(t *testing.T) {
	t.Parallel()
	pl := mocks.NewMockPlatform()
	controls := platforms.KeyboardControls(pl, map[string]string{})
	assert.Empty(t, controls)
}

func TestKeyboardControls_BuildsMapWithCorrectKeys(t *testing.T) {
	t.Parallel()
	pl := mocks.NewMockPlatform()
	controls := platforms.KeyboardControls(pl, map[string]string{
		platforms.ControlSaveState: "{f2}",
		platforms.ControlLoadState: "{f4}",
	})
	require.Len(t, controls, 2)
	assert.Contains(t, controls, platforms.ControlSaveState)
	assert.Contains(t, controls, platforms.ControlLoadState)
	assert.NotNil(t, controls[platforms.ControlSaveState].Func)
	assert.NotNil(t, controls[platforms.ControlLoadState].Func)
}

func TestKeyboardControls_InvokesPlatformKeyboardPress(t *testing.T) {
	t.Parallel()
	pl := mocks.NewMockPlatform()
	pl.On("KeyboardPress", "{f2}").Return(nil)

	controls := platforms.KeyboardControls(pl, map[string]string{
		platforms.ControlSaveState: "{f2}",
	})
	ctrl, ok := controls[platforms.ControlSaveState]
	require.True(t, ok)
	require.NotNil(t, ctrl.Func)

	err := ctrl.Func(context.Background(), &config.Instance{}, platforms.ControlParams{})
	require.NoError(t, err)
	pl.AssertExpectations(t)
}

func TestKeyboardControls_PropagatesKeyboardPressError(t *testing.T) {
	t.Parallel()
	pl := mocks.NewMockPlatform()
	keyErr := errors.New("keyboard not supported")
	pl.On("KeyboardPress", "{ctrl+q}").Return(keyErr)

	controls := platforms.KeyboardControls(pl, map[string]string{
		platforms.ControlStop: "{ctrl+q}",
	})
	ctrl, ok := controls[platforms.ControlStop]
	require.True(t, ok)

	err := ctrl.Func(context.Background(), &config.Instance{}, platforms.ControlParams{})
	require.ErrorIs(t, err, keyErr)
	pl.AssertExpectations(t)
}
