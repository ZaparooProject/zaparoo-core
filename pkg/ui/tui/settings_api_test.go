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

package tui

import (
	"context"
	"errors"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/client"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultSettingsService_GetSettings(t *testing.T) {
	t.Parallel()

	mockClient := mocks.NewMockAPIClient()
	mockClient.SetupSettingsResponse(&models.SettingsResponse{
		AudioScanFeedback:    true,
		DebugLogging:         false,
		ReadersAutoDetect:    true,
		ReadersScanMode:      "tap",
		ReadersScanExitDelay: 5,
	})

	svc := NewSettingsService(mockClient)
	settings, err := svc.GetSettings(context.Background())

	require.NoError(t, err)
	require.NotNil(t, settings)
	assert.True(t, settings.AudioScanFeedback)
	assert.False(t, settings.DebugLogging)
	assert.True(t, settings.ReadersAutoDetect)
	assert.Equal(t, "tap", settings.ReadersScanMode)
	assert.InEpsilon(t, float32(5), settings.ReadersScanExitDelay, 0.001)

	mockClient.AssertExpectations(t)
}

func TestDefaultSettingsService_GetSettings_Error(t *testing.T) {
	t.Parallel()

	mockClient := mocks.NewMockAPIClient()
	mockClient.SetupSettingsError(errors.New("connection failed"))

	svc := NewSettingsService(mockClient)
	settings, err := svc.GetSettings(context.Background())

	require.Error(t, err)
	assert.Nil(t, settings)
	assert.Contains(t, err.Error(), "failed to get settings")

	mockClient.AssertExpectations(t)
}

func TestDefaultSettingsService_UpdateSettings(t *testing.T) {
	t.Parallel()

	mockClient := mocks.NewMockAPIClient()
	mockClient.SetupUpdateSettingsSuccess()

	svc := NewSettingsService(mockClient)

	audioFeedback := true
	err := svc.UpdateSettings(context.Background(), models.UpdateSettingsParams{
		AudioScanFeedback: &audioFeedback,
	})

	require.NoError(t, err)
	mockClient.AssertExpectations(t)
}

func TestDefaultSettingsService_UpdateSettings_Error(t *testing.T) {
	t.Parallel()

	mockClient := mocks.NewMockAPIClient()
	mockClient.SetupUpdateSettingsError(errors.New("update failed"))

	svc := NewSettingsService(mockClient)

	audioFeedback := true
	err := svc.UpdateSettings(context.Background(), models.UpdateSettingsParams{
		AudioScanFeedback: &audioFeedback,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to update settings")

	mockClient.AssertExpectations(t)
}

func TestDefaultSettingsService_GetSystems(t *testing.T) {
	t.Parallel()

	mockClient := mocks.NewMockAPIClient()
	mockClient.SetupSystemsResponse([]models.System{
		{ID: "nes", Name: "Nintendo Entertainment System"},
		{ID: "snes", Name: "Super Nintendo"},
		{ID: "genesis", Name: "Sega Genesis"},
	})

	svc := NewSettingsService(mockClient)
	systems, err := svc.GetSystems(context.Background())

	require.NoError(t, err)
	require.Len(t, systems, 3)
	assert.Equal(t, "nes", systems[0].ID)
	assert.Equal(t, "Nintendo Entertainment System", systems[0].Name)

	mockClient.AssertExpectations(t)
}

func TestDefaultSettingsService_GetSystems_Error(t *testing.T) {
	t.Parallel()

	mockClient := mocks.NewMockAPIClient()
	mockClient.SetupSystemsError(errors.New("systems unavailable"))

	svc := NewSettingsService(mockClient)
	systems, err := svc.GetSystems(context.Background())

	require.Error(t, err)
	assert.Nil(t, systems)
	assert.Contains(t, err.Error(), "failed to get systems")

	mockClient.AssertExpectations(t)
}

func TestMockSettingsService_GetSettings(t *testing.T) {
	t.Parallel()

	mockSvc := NewMockSettingsService()
	mockSvc.SetupGetSettings(&models.SettingsResponse{
		AudioScanFeedback: true,
	})

	settings, err := mockSvc.GetSettings(context.Background())

	require.NoError(t, err)
	require.NotNil(t, settings)
	assert.True(t, settings.AudioScanFeedback)

	mockSvc.AssertExpectations(t)
}

func TestMockSettingsService_UpdateSettings(t *testing.T) {
	t.Parallel()

	mockSvc := NewMockSettingsService()
	mockSvc.SetupUpdateSettingsSuccess()

	audioFeedback := false
	err := mockSvc.UpdateSettings(context.Background(), models.UpdateSettingsParams{
		AudioScanFeedback: &audioFeedback,
	})

	require.NoError(t, err)
	mockSvc.AssertExpectations(t)
}

func TestMockSettingsService_GetSystems(t *testing.T) {
	t.Parallel()

	mockSvc := NewMockSettingsService()
	mockSvc.SetupGetSystems([]models.System{
		{ID: "psx", Name: "PlayStation"},
	})

	systems, err := mockSvc.GetSystems(context.Background())

	require.NoError(t, err)
	require.Len(t, systems, 1)
	assert.Equal(t, "psx", systems[0].ID)

	mockSvc.AssertExpectations(t)
}

func TestMockAPIClient_SetupTokenNotification(t *testing.T) {
	t.Parallel()

	mockClient := mocks.NewMockAPIClient()
	mockClient.SetupTokenNotification(&models.TokenResponse{
		UID:  "12345678",
		Data: "game_data",
		Text: "Super Mario Bros",
	})

	resp, err := mockClient.WaitNotification(context.Background(), 0, models.NotificationTokensAdded)

	require.NoError(t, err)
	assert.Contains(t, resp, "12345678")
	assert.Contains(t, resp, "Super Mario Bros")

	mockClient.AssertExpectations(t)
}

func TestLocalAPIClient_Creation(t *testing.T) {
	t.Parallel()

	// Just test that creation works - we can't test actual API calls without a running server
	apiClient := client.NewLocalAPIClient(nil)
	require.NotNil(t, apiClient)
}

func TestSettingsService_Integration_WithMockClient(t *testing.T) {
	t.Parallel()

	// Test the full flow: get settings -> update -> verify
	mockClient := mocks.NewMockAPIClient()

	// Setup initial settings
	mockClient.SetupSettingsResponse(&models.SettingsResponse{
		AudioScanFeedback: false,
		DebugLogging:      false,
	})
	mockClient.SetupUpdateSettingsSuccess()

	svc := NewSettingsService(mockClient)

	// Get initial settings
	settings, err := svc.GetSettings(context.Background())
	require.NoError(t, err)
	assert.False(t, settings.AudioScanFeedback)

	// Update settings
	audioFeedback := true
	err = svc.UpdateSettings(context.Background(), models.UpdateSettingsParams{
		AudioScanFeedback: &audioFeedback,
	})
	require.NoError(t, err)

	mockClient.AssertExpectations(t)
}

func TestSettingsService_UpdateMultipleFields(t *testing.T) {
	t.Parallel()

	mockClient := mocks.NewMockAPIClient()
	mockClient.SetupUpdateSettingsSuccess()

	svc := NewSettingsService(mockClient)

	audioFeedback := true
	debugLogging := true
	scanMode := "hold"
	exitDelay := float32(10)

	err := svc.UpdateSettings(context.Background(), models.UpdateSettingsParams{
		AudioScanFeedback:    &audioFeedback,
		DebugLogging:         &debugLogging,
		ReadersScanMode:      &scanMode,
		ReadersScanExitDelay: &exitDelay,
	})

	require.NoError(t, err)
	mockClient.AssertExpectations(t)
}

func TestSettingsService_EmptySystemsList(t *testing.T) {
	t.Parallel()

	mockClient := mocks.NewMockAPIClient()
	mockClient.SetupSystemsResponse([]models.System{})

	svc := NewSettingsService(mockClient)
	systems, err := svc.GetSystems(context.Background())

	require.NoError(t, err)
	assert.Empty(t, systems)

	mockClient.AssertExpectations(t)
}
