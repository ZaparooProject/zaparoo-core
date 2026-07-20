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

package tui

import (
	"context"
	"errors"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/client"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	testifymock "github.com/stretchr/testify/mock"
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
	err := svc.UpdateSettings(context.Background(), &models.UpdateSettingsParams{
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
	err := svc.UpdateSettings(context.Background(), &models.UpdateSettingsParams{
		AudioScanFeedback: &audioFeedback,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to update settings")

	mockClient.AssertExpectations(t)
}

func TestDefaultSettingsService_LocalBackupAPIContracts(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mockClient := mocks.NewMockAPIClient()
	mockClient.On("Call", testifymock.Anything, models.MethodSettingsBackup, "").
		Return(`{"name":"backup-1.zip"}`, nil).Once()
	mockClient.On("Call", testifymock.Anything, models.MethodSettingsBackupList, "").
		Return(`[{"name":"backup-1.zip","size":42}]`, nil).Once()
	mockClient.On(
		"Call", testifymock.Anything, models.MethodSettingsBackupInspect, `{"name":"backup-1.zip"}`,
	).Return(`{"name":"backup-1.zip","integrity":"unchecked"}`, nil).Once()
	mockClient.On(
		"Call", testifymock.Anything, models.MethodSettingsBackupDelete, `{"name":"backup-1.zip"}`,
	).Return(`{}`, nil).Once()
	mockClient.On(
		"Call", testifymock.Anything, models.MethodSettingsBackupRestore, `{"name":"backup-1.zip"}`,
	).Return(`{}`, nil).Once()
	mockClient.On("Call", testifymock.Anything, models.MethodSettingsBackupStatus, "").
		Return(`{"local":{"lastStatus":"success"},"remote":{"lastStatus":"never"}}`, nil).Once()

	svc := NewSettingsService(mockClient)
	name, err := svc.CreateBackup(ctx)
	require.NoError(t, err)
	assert.Equal(t, "backup-1.zip", name)

	backups, err := svc.ListBackups(ctx)
	require.NoError(t, err)
	require.Len(t, backups, 1)
	assert.Equal(t, "backup-1.zip", backups[0]["name"])
	assert.InDelta(t, 42, backups[0]["size"], 0)

	backup, err := svc.InspectBackup(ctx, name)
	require.NoError(t, err)
	assert.Equal(t, "unchecked", backup["integrity"])
	require.NoError(t, svc.DeleteBackup(ctx, name))
	require.NoError(t, svc.RestoreBackup(ctx, name))

	status, err := svc.GetBackupStatus(ctx)
	require.NoError(t, err)
	assert.Equal(t, "success", status.Local.LastStatus)
	assert.Equal(t, "never", status.Remote.LastStatus)
	mockClient.AssertExpectations(t)
}

func TestDefaultSettingsService_RemoteBackupAPIContracts(t *testing.T) {
	t.Parallel()
	const backupID = "save/a%b ?\u96ea"
	ctx := context.Background()
	mockClient := mocks.NewMockAPIClient()
	mockClient.On("Call", testifymock.Anything, models.MethodSettingsBackupRemoteRun, "").Return(
		`{"backup":{"id":"save/a%b ?\u96ea"}}`, nil,
	).Once()
	mockClient.On("Call", testifymock.Anything, models.MethodSettingsBackupRemoteList, "").Return(
		`{"items":[{"id":"save/a%b ?\u96ea","createdAt":"2026-07-10T12:00:00Z",`+
			`"backupType":"manual","sizeBytes":42,"categories":{"saves":{"files":1,"bytes":42}},`+
			`"sourceDevice":{"id":"dev-2","name":"Bedroom","platform":"mister",`+
			`"linked":true,"current":false}}]}`, nil,
	).Once()
	mockClient.On(
		"Call", testifymock.Anything, models.MethodSettingsBackupRemoteRestore,
		"{\"id\":\"save/a%b ?\u96ea\"}",
	).Return(`{}`, nil).Once()

	svc := NewSettingsService(mockClient)
	id, err := svc.RunRemoteBackup(ctx)
	require.NoError(t, err)
	assert.Equal(t, backupID, id)

	backups, err := svc.ListRemoteBackups(ctx)
	require.NoError(t, err)
	require.Len(t, backups, 1)
	assert.Equal(t, backupID, backups[0].ID)
	assert.Equal(t, int64(42), backups[0].SizeBytes)
	require.NotNil(t, backups[0].SourceDevice)
	assert.Equal(t, "dev-2", backups[0].SourceDevice.ID)
	assert.Equal(t, "Bedroom", backups[0].SourceDevice.Name)
	assert.Equal(t, int64(1), backups[0].Categories["saves"].Files)
	require.NoError(t, svc.RestoreRemoteBackup(ctx, backupID))
	mockClient.AssertExpectations(t)
}

func TestDefaultSettingsService_AuthLinkAPIContracts(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mockClient := mocks.NewMockAPIClient()
	mockClient.On("Call", testifymock.Anything, models.MethodSettingsAuthLink, "").
		Return(`{"status":"pending","userCode":"ABCD-EFGH"}`, nil).Once()
	mockClient.On("Call", testifymock.Anything, models.MethodSettingsAuthLinkStatus, "").
		Return(`{"status":"approved"}`, nil).Once()
	mockClient.On("Call", testifymock.Anything, models.MethodSettingsAuthLinkCancel, "").
		Return(`{}`, nil).Once()
	mockClient.On("Call", testifymock.Anything, models.MethodSettingsAuthUnlink, "").
		Return(`{}`, nil).Once()

	svc := NewSettingsService(mockClient)
	started, err := svc.StartAuthLink(ctx)
	require.NoError(t, err)
	assert.Equal(t, models.AuthLinkStatusPending, started.Status)
	assert.Equal(t, "ABCD-EFGH", started.UserCode)

	status, err := svc.GetAuthLinkStatus(ctx)
	require.NoError(t, err)
	assert.Equal(t, models.AuthLinkStatusApproved, status.Status)
	require.NoError(t, svc.CancelAuthLink(ctx))
	require.NoError(t, svc.Unlink(ctx))
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
	err := mockSvc.UpdateSettings(context.Background(), &models.UpdateSettingsParams{
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
	err = svc.UpdateSettings(context.Background(), &models.UpdateSettingsParams{
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

	err := svc.UpdateSettings(context.Background(), &models.UpdateSettingsParams{
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

func TestDefaultSettingsService_GetTokens(t *testing.T) {
	t.Parallel()

	mockClient := mocks.NewMockAPIClient()
	mockClient.SetupTokensResponse(&models.TokensResponse{
		Active: []models.TokenResponse{
			{UID: "12345678", Text: "Super Mario Bros"},
		},
		Last: &models.TokenResponse{
			UID: "87654321", Text: "Sonic the Hedgehog",
		},
	})

	svc := NewSettingsService(mockClient)
	tokens, err := svc.GetTokens(context.Background())

	require.NoError(t, err)
	require.NotNil(t, tokens)
	require.Len(t, tokens.Active, 1)
	assert.Equal(t, "12345678", tokens.Active[0].UID)
	assert.Equal(t, "Super Mario Bros", tokens.Active[0].Text)
	require.NotNil(t, tokens.Last)
	assert.Equal(t, "87654321", tokens.Last.UID)

	mockClient.AssertExpectations(t)
}

func TestDefaultSettingsService_GetTokens_Error(t *testing.T) {
	t.Parallel()

	mockClient := mocks.NewMockAPIClient()
	mockClient.SetupTokensError(errors.New("tokens unavailable"))

	svc := NewSettingsService(mockClient)
	tokens, err := svc.GetTokens(context.Background())

	require.Error(t, err)
	assert.Nil(t, tokens)
	assert.Contains(t, err.Error(), "failed to get tokens")

	mockClient.AssertExpectations(t)
}

func TestDefaultSettingsService_GetReaders(t *testing.T) {
	t.Parallel()

	mockClient := mocks.NewMockAPIClient()
	mockClient.SetupReadersResponse(&models.ReadersResponse{
		Readers: []models.ReaderInfo{
			{Driver: "libnfc", ID: "pn532-1", Connected: true},
			{Driver: "acr122pcsc", ID: "acr122-1", Connected: true},
		},
	})

	svc := NewSettingsService(mockClient)
	readers, err := svc.GetReaders(context.Background())

	require.NoError(t, err)
	require.NotNil(t, readers)
	require.Len(t, readers.Readers, 2)
	assert.Equal(t, "libnfc", readers.Readers[0].Driver)
	assert.True(t, readers.Readers[0].Connected)

	mockClient.AssertExpectations(t)
}

func TestDefaultSettingsService_GetReaders_Error(t *testing.T) {
	t.Parallel()

	mockClient := mocks.NewMockAPIClient()
	mockClient.SetupReadersError(errors.New("readers unavailable"))

	svc := NewSettingsService(mockClient)
	readers, err := svc.GetReaders(context.Background())

	require.Error(t, err)
	assert.Nil(t, readers)
	assert.Contains(t, err.Error(), "failed to get readers")

	mockClient.AssertExpectations(t)
}

func TestDefaultSettingsService_GetTokens_Empty(t *testing.T) {
	t.Parallel()

	mockClient := mocks.NewMockAPIClient()
	mockClient.SetupTokensResponse(&models.TokensResponse{
		Active: []models.TokenResponse{},
	})

	svc := NewSettingsService(mockClient)
	tokens, err := svc.GetTokens(context.Background())

	require.NoError(t, err)
	require.NotNil(t, tokens)
	assert.Empty(t, tokens.Active)

	mockClient.AssertExpectations(t)
}

func TestDefaultSettingsService_GetReaders_Empty(t *testing.T) {
	t.Parallel()

	mockClient := mocks.NewMockAPIClient()
	mockClient.SetupReadersResponse(&models.ReadersResponse{
		Readers: []models.ReaderInfo{},
	})

	svc := NewSettingsService(mockClient)
	readers, err := svc.GetReaders(context.Background())

	require.NoError(t, err)
	require.NotNil(t, readers)
	assert.Empty(t, readers.Readers)

	mockClient.AssertExpectations(t)
}

func TestDefaultSettingsService_WriteTag(t *testing.T) {
	t.Parallel()

	mockClient := mocks.NewMockAPIClient()
	mockClient.SetupWriteTagSuccess()

	svc := NewSettingsService(mockClient)
	err := svc.WriteTag(context.Background(), "**launch.system:nes")

	require.NoError(t, err)
	mockClient.AssertExpectations(t)
}

func TestDefaultSettingsService_WriteTag_Error(t *testing.T) {
	t.Parallel()

	mockClient := mocks.NewMockAPIClient()
	mockClient.SetupWriteTagError(errors.New("no reader connected"))

	svc := NewSettingsService(mockClient)
	err := svc.WriteTag(context.Background(), "test")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to write tag")

	mockClient.AssertExpectations(t)
}

func TestDefaultSettingsService_CancelWriteTag(t *testing.T) {
	t.Parallel()

	mockClient := mocks.NewMockAPIClient()
	mockClient.SetupCancelWriteTagSuccess()

	svc := NewSettingsService(mockClient)
	err := svc.CancelWriteTag(context.Background())

	require.NoError(t, err)
	mockClient.AssertExpectations(t)
}

func TestDefaultSettingsService_CancelWriteTag_Error(t *testing.T) {
	t.Parallel()

	mockClient := mocks.NewMockAPIClient()
	mockClient.SetupCancelWriteTagError(errors.New("no pending write"))

	svc := NewSettingsService(mockClient)
	err := svc.CancelWriteTag(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to cancel write")

	mockClient.AssertExpectations(t)
}

func TestDefaultSettingsService_SearchMedia(t *testing.T) {
	t.Parallel()

	mockClient := mocks.NewMockAPIClient()
	mockClient.SetupSearchMediaResponse(&models.SearchResults{
		Results: []models.SearchResultMedia{
			{Name: "Super Mario Bros", Path: "/games/nes/smb.nes"},
			{Name: "Super Mario Bros 3", Path: "/games/nes/smb3.nes"},
		},
		Total: 2,
	})

	svc := NewSettingsService(mockClient)
	query := "mario"
	results, err := svc.SearchMedia(context.Background(), models.SearchParams{
		Query: &query,
	})

	require.NoError(t, err)
	require.NotNil(t, results)
	assert.Len(t, results.Results, 2)
	assert.Equal(t, "Super Mario Bros", results.Results[0].Name)
	assert.Equal(t, 2, results.Total)

	mockClient.AssertExpectations(t)
}

func TestDefaultSettingsService_SearchMedia_Error(t *testing.T) {
	t.Parallel()

	mockClient := mocks.NewMockAPIClient()
	mockClient.SetupSearchMediaError(errors.New("database unavailable"))

	svc := NewSettingsService(mockClient)
	query := "test"
	results, err := svc.SearchMedia(context.Background(), models.SearchParams{
		Query: &query,
	})

	require.Error(t, err)
	assert.Nil(t, results)
	assert.Contains(t, err.Error(), "failed to search media")

	mockClient.AssertExpectations(t)
}

func TestDefaultSettingsService_SearchMedia_Empty(t *testing.T) {
	t.Parallel()

	mockClient := mocks.NewMockAPIClient()
	mockClient.SetupSearchMediaResponse(&models.SearchResults{
		Results: []models.SearchResultMedia{},
		Total:   0,
	})

	svc := NewSettingsService(mockClient)
	query := "nonexistent"
	results, err := svc.SearchMedia(context.Background(), models.SearchParams{
		Query: &query,
	})

	require.NoError(t, err)
	require.NotNil(t, results)
	assert.Empty(t, results.Results)
	assert.Equal(t, 0, results.Total)

	mockClient.AssertExpectations(t)
}
