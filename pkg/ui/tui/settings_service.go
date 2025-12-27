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
