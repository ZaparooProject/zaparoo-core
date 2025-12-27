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

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/stretchr/testify/mock"
)

// MockSettingsService is a mock implementation of SettingsService for testing.
type MockSettingsService struct {
	mock.Mock
}

// NewMockSettingsService creates a new mock settings service.
func NewMockSettingsService() *MockSettingsService {
	return &MockSettingsService{}
}

// GetSettings mocks fetching settings.
func (m *MockSettingsService) GetSettings(ctx context.Context) (*models.SettingsResponse, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1) //nolint:wrapcheck // mock returns test-provided errors
	}
	settings, ok := args.Get(0).(*models.SettingsResponse)
	if !ok {
		return nil, args.Error(1) //nolint:wrapcheck // mock returns test-provided errors
	}
	return settings, args.Error(1) //nolint:wrapcheck // mock returns test-provided errors
}

// UpdateSettings mocks updating settings.
func (m *MockSettingsService) UpdateSettings(ctx context.Context, params models.UpdateSettingsParams) error {
	args := m.Called(ctx, params)
	return args.Error(0) //nolint:wrapcheck // mock returns test-provided errors
}

// GetSystems mocks fetching systems.
func (m *MockSettingsService) GetSystems(ctx context.Context) ([]models.System, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1) //nolint:wrapcheck // mock returns test-provided errors
	}
	systems, ok := args.Get(0).([]models.System)
	if !ok {
		return nil, args.Error(1) //nolint:wrapcheck // mock returns test-provided errors
	}
	return systems, args.Error(1) //nolint:wrapcheck // mock returns test-provided errors
}

// SetupGetSettings configures the mock to return settings.
func (m *MockSettingsService) SetupGetSettings(settings *models.SettingsResponse) {
	m.On("GetSettings", mock.Anything).Return(settings, nil)
}

// SetupGetSettingsError configures the mock to return an error.
func (m *MockSettingsService) SetupGetSettingsError(err error) {
	m.On("GetSettings", mock.Anything).Return(nil, err)
}

// SetupUpdateSettingsSuccess configures the mock to accept updates.
func (m *MockSettingsService) SetupUpdateSettingsSuccess() {
	m.On("UpdateSettings", mock.Anything, mock.Anything).Return(nil)
}

// SetupUpdateSettingsError configures the mock to return an error on update.
func (m *MockSettingsService) SetupUpdateSettingsError(err error) {
	m.On("UpdateSettings", mock.Anything, mock.Anything).Return(err)
}

// SetupGetSystems configures the mock to return systems.
func (m *MockSettingsService) SetupGetSystems(systems []models.System) {
	m.On("GetSystems", mock.Anything).Return(systems, nil)
}

// SetupGetSystemsError configures the mock to return an error.
func (m *MockSettingsService) SetupGetSystemsError(err error) {
	m.On("GetSystems", mock.Anything).Return(nil, err)
}
