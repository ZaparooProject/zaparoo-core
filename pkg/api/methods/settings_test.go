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

package methods

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	corehelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playtime"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// TestHandlePlaytimeLimitsUpdate_ReEnableWithActiveMedia tests that re-enabling
// playtime limits while a game is already running correctly triggers a session start.
// This is a regression test for the bug where disabling then re-enabling limits
// while a game was running would leave the session in "reset" state with 0m values.
func TestHandlePlaytimeLimitsUpdate_ReEnableWithActiveMedia(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("test-platform").Maybe()

	// Create a config using NewConfig with a temp directory so Save() works
	tmpDir := t.TempDir()
	cfg, err := config.NewConfig(tmpDir, config.Values{})
	require.NoError(t, err)
	cfg.SetPlaytimeLimitsEnabled(false) // Start with limits disabled

	appState, _ := state.NewState(mockPlatform, "test-boot-uuid")

	// Use a reliable time (2025) so clock checks pass
	baseTime := time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC)
	fakeClock := clockwork.NewFakeClockAt(baseTime)

	// Simulate a game already running
	appState.SetActiveMedia(&models.ActiveMedia{
		SystemID:   "NES",
		SystemName: "Nintendo Entertainment System",
		Name:       "Super Mario Bros",
		Path:       "/roms/nes/smb.nes",
		Started:    baseTime,
	})

	// Set up mock database - needed for checkLimits goroutine
	mockUserDB := helpers.NewMockUserDBI()
	mockUserDB.On("GetMediaHistory", mock.Anything, mock.Anything).Return([]database.MediaHistoryEntry{}, nil).Maybe()

	db := &database.Database{
		UserDB: mockUserDB,
	}

	// Create a LimitsManager with the mock database
	limitsManager := playtime.NewLimitsManager(db, mockPlatform, cfg, fakeClock)
	defer limitsManager.Stop() // Clean up goroutines

	// Prepare the request to enable limits
	enabled := true
	params := models.UpdatePlaytimeLimitsParams{
		Enabled: &enabled,
	}
	paramsJSON, err := json.Marshal(params)
	require.NoError(t, err)

	env := requests.RequestEnv{
		Platform:      mockPlatform,
		Config:        cfg,
		State:         appState,
		LimitsManager: limitsManager,
		Params:        paramsJSON,
	}

	// Call the handler
	result, err := HandlePlaytimeLimitsUpdate(env)
	require.NoError(t, err)
	assert.NotNil(t, result)

	// Verify that the limits are now enabled
	assert.True(t, cfg.PlaytimeLimitsEnabled(), "config should have limits enabled")
	assert.True(t, limitsManager.IsEnabled(), "limits manager should be enabled")

	// Verify that the session was started (state should be "active" not "reset")
	// This is the key assertion - the bug was that state remained "reset"
	status := limitsManager.GetStatus()
	assert.Equal(t, "active", status.State, "session should be active after re-enabling with running game")
	assert.True(t, status.SessionActive, "session should be marked as active")
}

// TestHandlePlaytimeLimitsUpdate_ReEnableWithNoActiveMedia tests that re-enabling
// playtime limits when no game is running leaves the session in "reset" state.
func TestHandlePlaytimeLimitsUpdate_ReEnableWithNoActiveMedia(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("test-platform").Maybe()

	// Create a config using NewConfig with a temp directory so Save() works
	tmpDir := t.TempDir()
	cfg, err := config.NewConfig(tmpDir, config.Values{})
	require.NoError(t, err)
	cfg.SetPlaytimeLimitsEnabled(false) // Start with limits disabled

	appState, _ := state.NewState(mockPlatform, "test-boot-uuid")

	// No active media - simulate no game running

	// Set up mock database
	mockUserDB := helpers.NewMockUserDBI()
	db := &database.Database{
		UserDB: mockUserDB,
	}

	// Create a LimitsManager
	baseTime := time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC)
	fakeClock := clockwork.NewFakeClockAt(baseTime)
	limitsManager := playtime.NewLimitsManager(db, mockPlatform, cfg, fakeClock)
	defer limitsManager.Stop() // Clean up goroutines

	// Prepare the request to enable limits
	enabled := true
	params := models.UpdatePlaytimeLimitsParams{
		Enabled: &enabled,
	}
	paramsJSON, err := json.Marshal(params)
	require.NoError(t, err)

	env := requests.RequestEnv{
		Platform:      mockPlatform,
		Config:        cfg,
		State:         appState,
		LimitsManager: limitsManager,
		Params:        paramsJSON,
	}

	// Call the handler
	result, err := HandlePlaytimeLimitsUpdate(env)
	require.NoError(t, err)
	assert.NotNil(t, result)

	// Verify that the limits are now enabled
	assert.True(t, cfg.PlaytimeLimitsEnabled(), "config should have limits enabled")
	assert.True(t, limitsManager.IsEnabled(), "limits manager should be enabled")

	// Verify that the session remains in reset state (no active game)
	status := limitsManager.GetStatus()
	assert.Equal(t, "reset", status.State, "session should be reset when no game is running")
	assert.False(t, status.SessionActive, "session should not be marked as active")
}

// TestHandleSettings_ReaderConnections tests that HandleSettings returns
// reader connection configuration in the response.
func TestHandleSettings_ReaderConnections(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("test-platform").Maybe()

	tmpDir := t.TempDir()
	cfg, err := config.NewConfig(tmpDir, config.Values{
		Readers: config.Readers{
			Connect: []config.ReadersConnect{
				{Driver: "pn532", Path: "/dev/ttyUSB0"},
				{Driver: "libnfc", Path: ""},
			},
		},
	})
	require.NoError(t, err)

	appState, _ := state.NewState(mockPlatform, "test-boot-uuid")

	env := requests.RequestEnv{
		Platform: mockPlatform,
		Config:   cfg,
		State:    appState,
		Params:   []byte(`{}`),
	}

	result, err := HandleSettings(env)
	require.NoError(t, err)

	resp, ok := result.(models.SettingsResponse)
	require.True(t, ok, "result should be SettingsResponse")

	assert.Len(t, resp.ReadersConnect, 2)
	assert.Equal(t, "pn532", resp.ReadersConnect[0].Driver)
	assert.Equal(t, "/dev/ttyUSB0", resp.ReadersConnect[0].Path)
	assert.Equal(t, "libnfc", resp.ReadersConnect[1].Driver)
	assert.Empty(t, resp.ReadersConnect[1].Path)
}

// TestHandleSettings_EmptyReaderConnections tests that HandleSettings returns
// an empty slice when no reader connections are configured.
func TestHandleSettings_EmptyReaderConnections(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("test-platform").Maybe()

	tmpDir := t.TempDir()
	cfg, err := config.NewConfig(tmpDir, config.Values{})
	require.NoError(t, err)

	appState, _ := state.NewState(mockPlatform, "test-boot-uuid")

	env := requests.RequestEnv{
		Platform: mockPlatform,
		Config:   cfg,
		State:    appState,
		Params:   []byte(`{}`),
	}

	result, err := HandleSettings(env)
	require.NoError(t, err)

	resp, ok := result.(models.SettingsResponse)
	require.True(t, ok, "result should be SettingsResponse")

	assert.Empty(t, resp.ReadersConnect)
}

// TestHandleSettingsUpdate_ReaderConnections tests that HandleSettingsUpdate
// properly updates reader connection configuration.
func TestHandleSettingsUpdate_ReaderConnections(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("test-platform").Maybe()

	tmpDir := t.TempDir()
	cfg, err := config.NewConfig(tmpDir, config.Values{})
	require.NoError(t, err)

	appState, _ := state.NewState(mockPlatform, "test-boot-uuid")

	params := models.UpdateSettingsParams{
		ReadersConnect: &[]models.ReaderConnection{
			{Driver: "pn532", Path: "/dev/ttyUSB0"},
			{Driver: "libnfc", Path: ""},
		},
	}
	paramsJSON, err := json.Marshal(params)
	require.NoError(t, err)

	env := requests.RequestEnv{
		Platform: mockPlatform,
		Config:   cfg,
		State:    appState,
		Params:   paramsJSON,
	}

	_, err = HandleSettingsUpdate(env)
	require.NoError(t, err)

	// Verify config was updated
	readers := cfg.Readers().Connect
	assert.Len(t, readers, 2)
	assert.Equal(t, "pn532", readers[0].Driver)
	assert.Equal(t, "/dev/ttyUSB0", readers[0].Path)
	assert.Equal(t, "libnfc", readers[1].Driver)
	assert.Empty(t, readers[1].Path)
}

// TestHandleSettings_ErrorReportingDefault tests that HandleSettings returns
// errorReporting as false by default.
func TestHandleSettings_ErrorReportingDefault(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("test-platform").Maybe()

	tmpDir := t.TempDir()
	cfg, err := config.NewConfig(tmpDir, config.Values{})
	require.NoError(t, err)

	appState, _ := state.NewState(mockPlatform, "test-boot-uuid")

	env := requests.RequestEnv{
		Platform: mockPlatform,
		Config:   cfg,
		State:    appState,
		Params:   []byte(`{}`),
	}

	result, err := HandleSettings(env)
	require.NoError(t, err)

	resp, ok := result.(models.SettingsResponse)
	require.True(t, ok, "result should be SettingsResponse")

	assert.False(t, resp.ErrorReporting, "errorReporting should be false by default")
}

// TestHandleSettings_ErrorReportingEnabled tests that HandleSettings returns
// errorReporting as true when it's enabled in config.
func TestHandleSettings_ErrorReportingEnabled(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("test-platform").Maybe()

	tmpDir := t.TempDir()
	cfg, err := config.NewConfig(tmpDir, config.Values{
		ErrorReporting: true,
	})
	require.NoError(t, err)

	appState, _ := state.NewState(mockPlatform, "test-boot-uuid")

	env := requests.RequestEnv{
		Platform: mockPlatform,
		Config:   cfg,
		State:    appState,
		Params:   []byte(`{}`),
	}

	result, err := HandleSettings(env)
	require.NoError(t, err)

	resp, ok := result.(models.SettingsResponse)
	require.True(t, ok, "result should be SettingsResponse")

	assert.True(t, resp.ErrorReporting, "errorReporting should be true when enabled")
}

// TestHandleSettingsUpdate_ErrorReportingEnable tests that HandleSettingsUpdate
// properly enables error reporting.
func TestHandleSettingsUpdate_ErrorReportingEnable(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("test-platform").Maybe()

	tmpDir := t.TempDir()
	cfg, err := config.NewConfig(tmpDir, config.Values{})
	require.NoError(t, err)
	assert.False(t, cfg.ErrorReporting(), "errorReporting should start as false")

	appState, _ := state.NewState(mockPlatform, "test-boot-uuid")

	enabled := true
	params := models.UpdateSettingsParams{
		ErrorReporting: &enabled,
	}
	paramsJSON, err := json.Marshal(params)
	require.NoError(t, err)

	env := requests.RequestEnv{
		Platform: mockPlatform,
		Config:   cfg,
		State:    appState,
		Params:   paramsJSON,
	}

	_, err = HandleSettingsUpdate(env)
	require.NoError(t, err)

	assert.True(t, cfg.ErrorReporting(), "errorReporting should be enabled after update")
}

// TestHandleSettingsUpdate_ErrorReportingDisable tests that HandleSettingsUpdate
// properly disables error reporting.
func TestHandleSettingsUpdate_ErrorReportingDisable(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("test-platform").Maybe()

	tmpDir := t.TempDir()
	cfg, err := config.NewConfig(tmpDir, config.Values{
		ErrorReporting: true,
	})
	require.NoError(t, err)
	assert.True(t, cfg.ErrorReporting(), "errorReporting should start as true")

	appState, _ := state.NewState(mockPlatform, "test-boot-uuid")

	disabled := false
	params := models.UpdateSettingsParams{
		ErrorReporting: &disabled,
	}
	paramsJSON, err := json.Marshal(params)
	require.NoError(t, err)

	env := requests.RequestEnv{
		Platform: mockPlatform,
		Config:   cfg,
		State:    appState,
		Params:   paramsJSON,
	}

	_, err = HandleSettingsUpdate(env)
	require.NoError(t, err)

	assert.False(t, cfg.ErrorReporting(), "errorReporting should be disabled after update")
}

// TestHandleSettingsUpdate_ReaderConnectionsWithIDSource tests that IDSource
// field is preserved when updating reader connections.
func TestHandleSettingsUpdate_ReaderConnectionsWithIDSource(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("test-platform").Maybe()

	tmpDir := t.TempDir()
	cfg, err := config.NewConfig(tmpDir, config.Values{})
	require.NoError(t, err)

	appState, _ := state.NewState(mockPlatform, "test-boot-uuid")

	params := models.UpdateSettingsParams{
		ReadersConnect: &[]models.ReaderConnection{
			{Driver: "pn532", Path: "/dev/ttyUSB0", IDSource: "uid"},
		},
	}
	paramsJSON, err := json.Marshal(params)
	require.NoError(t, err)

	env := requests.RequestEnv{
		Platform: mockPlatform,
		Config:   cfg,
		State:    appState,
		Params:   paramsJSON,
	}

	_, err = HandleSettingsUpdate(env)
	require.NoError(t, err)

	// Verify config was updated with IDSource
	readers := cfg.Readers().Connect
	assert.Len(t, readers, 1)
	assert.Equal(t, "pn532", readers[0].Driver)
	assert.Equal(t, "/dev/ttyUSB0", readers[0].Path)
	assert.Equal(t, "uid", readers[0].IDSource)
}

// TestHandleSettingsReload_RefreshesLauncherCache tests that HandleSettingsReload
// refreshes the launcher cache after reloading config and custom launcher files.
func TestHandleSettingsReload_RefreshesLauncherCache(t *testing.T) {
	t.Parallel()

	// Set up in-memory filesystem with required directories
	memFS := helpers.NewMemoryFS()
	dataDir := "/data"
	configDir := "/config"
	require.NoError(t, memFS.Fs.MkdirAll(configDir, 0o750))
	require.NoError(t, memFS.Fs.MkdirAll(dataDir+"/"+config.MappingsDir, 0o750))
	require.NoError(t, memFS.Fs.MkdirAll(dataDir+"/"+config.LaunchersDir, 0o750))

	cfg, err := helpers.NewTestConfig(memFS, configDir)
	require.NoError(t, err)

	expectedLaunchers := []platforms.Launcher{
		{ID: "test-launcher", SystemID: "NES"},
	}
	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("test-platform").Maybe()
	mockPlatform.On("Settings").Return(platforms.Settings{DataDir: dataDir}).Maybe()
	mockPlatform.On("Launchers", mock.AnythingOfType("*config.Instance")).Return(expectedLaunchers).Maybe()

	testCache := &corehelpers.LauncherCache{}
	assert.Empty(t, testCache.GetAllLaunchers())

	env := requests.RequestEnv{
		Platform:      mockPlatform,
		Config:        cfg,
		LauncherCache: testCache,
	}

	result, err := HandleSettingsReload(env)
	require.NoError(t, err)
	assert.Equal(t, NoContent{}, result)

	cached := testCache.GetAllLaunchers()
	require.Len(t, cached, 1)
	assert.Equal(t, "test-launcher", cached[0].ID)
	assert.Equal(t, "NES", cached[0].SystemID)

	mockPlatform.AssertExpectations(t)
}
