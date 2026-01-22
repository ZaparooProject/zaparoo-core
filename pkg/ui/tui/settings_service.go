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
	UpdateSettings(ctx context.Context, params models.UpdateSettingsParams) error

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
func (s *DefaultSettingsService) UpdateSettings(ctx context.Context, params models.UpdateSettingsParams) error {
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
