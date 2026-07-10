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
	"encoding/json"
	"fmt"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/client"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
)

// SettingsService handles settings API operations.
type SettingsService interface {
	// GetSettings fetches current settings from the API.
	GetSettings(ctx context.Context) (*models.SettingsResponse, error)

	// UpdateSettings sends a settings update to the API.
	UpdateSettings(ctx context.Context, params *models.UpdateSettingsParams) error

	// CreateBackup creates a local backup ZIP.
	CreateBackup(ctx context.Context) (string, error)

	// ListBackups fetches local backup ZIP metadata.
	ListBackups(ctx context.Context) ([]map[string]any, error)

	// InspectBackup fetches local backup manifest details.
	InspectBackup(ctx context.Context, name string) (map[string]any, error)

	// DeleteBackup removes a local backup ZIP.
	DeleteBackup(ctx context.Context, name string) error

	// RestoreBackup restores a local backup ZIP.
	RestoreBackup(ctx context.Context, name string) error

	// GetBackupStatus fetches local/remote backup status.
	GetBackupStatus(ctx context.Context) (*models.BackupStatusResponse, error)

	// RunRemoteBackup uploads a backup to the configured remote provider.
	RunRemoteBackup(ctx context.Context) (int64, error)

	// ListRemoteBackups fetches remote backup snapshots.
	ListRemoteBackups(ctx context.Context) ([]map[string]any, error)

	// RestoreRemoteBackup restores a remote backup snapshot.
	RestoreRemoteBackup(ctx context.Context, id int64) error

	// StartAuthLink starts the reverse device link flow.
	StartAuthLink(ctx context.Context) (*models.AuthLinkStatusResponse, error)

	// GetAuthLinkStatus reports the active link flow's state.
	GetAuthLinkStatus(ctx context.Context) (*models.AuthLinkStatusResponse, error)

	// CancelAuthLink stops the active link flow.
	CancelAuthLink(ctx context.Context) error

	// GetSystems fetches available systems from the API.
	GetSystems(ctx context.Context) ([]models.System, error)

	// GetTokens fetches currently active tokens from the API.
	GetTokens(ctx context.Context) (*models.TokensResponse, error)

	// GetReaders fetches connected readers from the API.
	GetReaders(ctx context.Context) (*models.ReadersResponse, error)

	// WriteTag writes text to a tag via the reader.
	WriteTag(ctx context.Context, text string) error

	// CancelWriteTag cancels a pending write operation.
	CancelWriteTag(ctx context.Context) error

	// SearchMedia searches for media matching the given parameters.
	SearchMedia(ctx context.Context, params models.SearchParams) (*models.SearchResults, error)
}

// DefaultSettingsService implements SettingsService using an APIClient.
type DefaultSettingsService struct {
	apiClient client.APIClient
}

type remoteBackupRunRaw struct {
	Backup remoteBackupIDRaw `json:"backup"`
}

type remoteBackupIDRaw struct {
	ID int64 `json:"id"`
}

// NewSettingsService creates a SettingsService that uses the given APIClient.
func NewSettingsService(apiClient client.APIClient) *DefaultSettingsService {
	return &DefaultSettingsService{apiClient: apiClient}
}

// GetSettings fetches current settings from the API.
func (s *DefaultSettingsService) GetSettings(ctx context.Context) (*models.SettingsResponse, error) {
	resp, err := s.apiClient.Call(ctx, models.MethodSettings, "")
	if err != nil {
		return nil, fmt.Errorf("failed to get settings: %w", err)
	}
	var settings models.SettingsResponse
	if err := json.Unmarshal([]byte(resp), &settings); err != nil {
		return nil, fmt.Errorf("failed to parse settings: %w", err)
	}
	return &settings, nil
}

// UpdateSettings sends a settings update to the API.
func (s *DefaultSettingsService) UpdateSettings(ctx context.Context, params *models.UpdateSettingsParams) error {
	data, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("failed to marshal params: %w", err)
	}
	_, err = s.apiClient.Call(ctx, models.MethodSettingsUpdate, string(data))
	if err != nil {
		return fmt.Errorf("failed to update settings: %w", err)
	}
	return nil
}

func (s *DefaultSettingsService) CreateBackup(ctx context.Context) (string, error) {
	resp, err := s.apiClient.Call(ctx, models.MethodSettingsBackup, "")
	if err != nil {
		return "", fmt.Errorf("failed to create backup: %w", err)
	}
	var raw struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal([]byte(resp), &raw); err != nil {
		return "", fmt.Errorf("failed to parse backup result: %w", err)
	}
	return raw.Name, nil
}

func (s *DefaultSettingsService) ListBackups(ctx context.Context) ([]map[string]any, error) {
	resp, err := s.apiClient.Call(ctx, models.MethodSettingsBackupList, "")
	if err != nil {
		return nil, fmt.Errorf("failed to list backups: %w", err)
	}
	var backups []map[string]any
	if err := json.Unmarshal([]byte(resp), &backups); err != nil {
		return nil, fmt.Errorf("failed to parse backups: %w", err)
	}
	return backups, nil
}

func (s *DefaultSettingsService) InspectBackup(ctx context.Context, name string) (map[string]any, error) {
	params := models.BackupNameParams{Name: name}
	data, err := json.Marshal(&params)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal inspect params: %w", err)
	}
	resp, err := s.apiClient.Call(ctx, models.MethodSettingsBackupInspect, string(data))
	if err != nil {
		return nil, fmt.Errorf("failed to inspect backup: %w", err)
	}
	var backup map[string]any
	if err := json.Unmarshal([]byte(resp), &backup); err != nil {
		return nil, fmt.Errorf("failed to parse backup details: %w", err)
	}
	return backup, nil
}

func (s *DefaultSettingsService) DeleteBackup(ctx context.Context, name string) error {
	params := models.BackupNameParams{Name: name}
	data, err := json.Marshal(&params)
	if err != nil {
		return fmt.Errorf("failed to marshal delete params: %w", err)
	}
	_, err = s.apiClient.Call(ctx, models.MethodSettingsBackupDelete, string(data))
	if err != nil {
		return fmt.Errorf("failed to delete backup: %w", err)
	}
	return nil
}

func (s *DefaultSettingsService) RestoreBackup(ctx context.Context, name string) error {
	params := models.BackupRestoreParams{Name: name}
	data, err := json.Marshal(&params)
	if err != nil {
		return fmt.Errorf("failed to marshal restore params: %w", err)
	}
	_, err = s.apiClient.Call(ctx, models.MethodSettingsBackupRestore, string(data))
	if err != nil {
		return fmt.Errorf("failed to restore backup: %w", err)
	}
	return nil
}

func (s *DefaultSettingsService) GetBackupStatus(ctx context.Context) (*models.BackupStatusResponse, error) {
	resp, err := s.apiClient.Call(ctx, models.MethodSettingsBackupStatus, "")
	if err != nil {
		return nil, fmt.Errorf("failed to get backup status: %w", err)
	}
	var status models.BackupStatusResponse
	if err := json.Unmarshal([]byte(resp), &status); err != nil {
		return nil, fmt.Errorf("failed to parse backup status: %w", err)
	}
	return &status, nil
}

func (s *DefaultSettingsService) RunRemoteBackup(ctx context.Context) (int64, error) {
	resp, err := s.apiClient.Call(ctx, models.MethodSettingsBackupRemoteRun, "")
	if err != nil {
		return 0, fmt.Errorf("failed to run remote backup: %w", err)
	}
	var raw remoteBackupRunRaw
	if err := json.Unmarshal([]byte(resp), &raw); err != nil {
		return 0, fmt.Errorf("failed to parse remote backup result: %w", err)
	}
	return raw.Backup.ID, nil
}

func (s *DefaultSettingsService) ListRemoteBackups(ctx context.Context) ([]map[string]any, error) {
	resp, err := s.apiClient.Call(ctx, models.MethodSettingsBackupRemoteList, "")
	if err != nil {
		return nil, fmt.Errorf("failed to list remote backups: %w", err)
	}
	var raw struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal([]byte(resp), &raw); err != nil {
		return nil, fmt.Errorf("failed to parse remote backups: %w", err)
	}
	return raw.Items, nil
}

func (s *DefaultSettingsService) RestoreRemoteBackup(ctx context.Context, id int64) error {
	params := models.BackupRemoteRestoreParams{ID: id}
	data, err := json.Marshal(&params)
	if err != nil {
		return fmt.Errorf("failed to marshal remote restore params: %w", err)
	}
	_, err = s.apiClient.Call(ctx, models.MethodSettingsBackupRemoteRestore, string(data))
	if err != nil {
		return fmt.Errorf("failed to restore remote backup: %w", err)
	}
	return nil
}

// StartAuthLink starts the reverse device link flow.
func (s *DefaultSettingsService) StartAuthLink(ctx context.Context) (*models.AuthLinkStatusResponse, error) {
	resp, err := s.apiClient.Call(ctx, models.MethodSettingsAuthLink, "")
	if err != nil {
		return nil, fmt.Errorf("failed to start device link: %w", err)
	}
	var link models.AuthLinkStatusResponse
	if err := json.Unmarshal([]byte(resp), &link); err != nil {
		return nil, fmt.Errorf("failed to parse device link response: %w", err)
	}
	return &link, nil
}

// GetAuthLinkStatus reports the active link flow's state.
func (s *DefaultSettingsService) GetAuthLinkStatus(ctx context.Context) (*models.AuthLinkStatusResponse, error) {
	resp, err := s.apiClient.Call(ctx, models.MethodSettingsAuthLinkStatus, "")
	if err != nil {
		return nil, fmt.Errorf("failed to get device link status: %w", err)
	}
	var link models.AuthLinkStatusResponse
	if err := json.Unmarshal([]byte(resp), &link); err != nil {
		return nil, fmt.Errorf("failed to parse device link status: %w", err)
	}
	return &link, nil
}

// CancelAuthLink stops the active link flow.
func (s *DefaultSettingsService) CancelAuthLink(ctx context.Context) error {
	if _, err := s.apiClient.Call(ctx, models.MethodSettingsAuthLinkCancel, ""); err != nil {
		return fmt.Errorf("failed to cancel device link: %w", err)
	}
	return nil
}

// GetSystems fetches available systems from the API.
func (s *DefaultSettingsService) GetSystems(ctx context.Context) ([]models.System, error) {
	resp, err := s.apiClient.Call(ctx, models.MethodSystems, "")
	if err != nil {
		return nil, fmt.Errorf("failed to get systems: %w", err)
	}
	var systems models.SystemsResponse
	if err := json.Unmarshal([]byte(resp), &systems); err != nil {
		return nil, fmt.Errorf("failed to parse systems: %w", err)
	}
	return systems.Systems, nil
}

// GetTokens fetches currently active tokens from the API.
func (s *DefaultSettingsService) GetTokens(ctx context.Context) (*models.TokensResponse, error) {
	resp, err := s.apiClient.Call(ctx, models.MethodTokens, "")
	if err != nil {
		return nil, fmt.Errorf("failed to get tokens: %w", err)
	}
	var tokens models.TokensResponse
	if err := json.Unmarshal([]byte(resp), &tokens); err != nil {
		return nil, fmt.Errorf("failed to parse tokens: %w", err)
	}
	return &tokens, nil
}

// GetReaders fetches connected readers from the API.
func (s *DefaultSettingsService) GetReaders(ctx context.Context) (*models.ReadersResponse, error) {
	resp, err := s.apiClient.Call(ctx, models.MethodReaders, "")
	if err != nil {
		return nil, fmt.Errorf("failed to get readers: %w", err)
	}
	var readers models.ReadersResponse
	if err := json.Unmarshal([]byte(resp), &readers); err != nil {
		return nil, fmt.Errorf("failed to parse readers: %w", err)
	}
	return &readers, nil
}

// WriteTag writes text to a tag via the reader.
func (s *DefaultSettingsService) WriteTag(ctx context.Context, text string) error {
	params := models.ReaderWriteParams{Text: text}
	data, err := json.Marshal(&params)
	if err != nil {
		return fmt.Errorf("failed to marshal write params: %w", err)
	}
	_, err = s.apiClient.Call(ctx, models.MethodReadersWrite, string(data))
	if err != nil {
		return fmt.Errorf("failed to write tag: %w", err)
	}
	return nil
}

// CancelWriteTag cancels a pending write operation.
func (s *DefaultSettingsService) CancelWriteTag(ctx context.Context) error {
	_, err := s.apiClient.Call(ctx, models.MethodReadersWriteCancel, "")
	if err != nil {
		return fmt.Errorf("failed to cancel write: %w", err)
	}
	return nil
}

// SearchMedia searches for media matching the given parameters.
func (s *DefaultSettingsService) SearchMedia(
	ctx context.Context,
	params models.SearchParams,
) (*models.SearchResults, error) {
	data, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal search params: %w", err)
	}
	resp, err := s.apiClient.Call(ctx, models.MethodMediaSearch, string(data))
	if err != nil {
		return nil, fmt.Errorf("failed to search media: %w", err)
	}
	var results models.SearchResults
	if err := json.Unmarshal([]byte(resp), &results); err != nil {
		return nil, fmt.Errorf("failed to parse search results: %w", err)
	}
	return &results, nil
}
