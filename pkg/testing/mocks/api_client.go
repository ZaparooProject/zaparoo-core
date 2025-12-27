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

package mocks

import (
	"context"
	"encoding/json"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/stretchr/testify/mock"
)

// MockAPIClient is a mock implementation of client.APIClient for testing.
type MockAPIClient struct {
	mock.Mock
}

// NewMockAPIClient creates a new mock API client.
func NewMockAPIClient() *MockAPIClient {
	return &MockAPIClient{}
}

// Call mocks the API call method.
func (m *MockAPIClient) Call(ctx context.Context, method, params string) (string, error) {
	args := m.Called(ctx, method, params)
	return args.String(0), args.Error(1)
}

// WaitNotification mocks waiting for a notification.
func (m *MockAPIClient) WaitNotification(
	ctx context.Context,
	timeout time.Duration,
	notificationType string,
) (string, error) {
	args := m.Called(ctx, timeout, notificationType)
	return args.String(0), args.Error(1)
}

// SetupSettingsResponse configures the mock to return a settings response.
func (m *MockAPIClient) SetupSettingsResponse(settings *models.SettingsResponse) {
	data, _ := json.Marshal(settings)
	m.On("Call", mock.Anything, models.MethodSettings, "").Return(string(data), nil)
}

// SetupSettingsError configures the mock to return an error for settings.
func (m *MockAPIClient) SetupSettingsError(err error) {
	m.On("Call", mock.Anything, models.MethodSettings, "").Return("", err)
}

// SetupUpdateSettingsSuccess configures the mock to accept settings updates.
func (m *MockAPIClient) SetupUpdateSettingsSuccess() {
	m.On("Call", mock.Anything, models.MethodSettingsUpdate, mock.Anything).Return("{}", nil)
}

// SetupUpdateSettingsError configures the mock to return an error on update.
func (m *MockAPIClient) SetupUpdateSettingsError(err error) {
	m.On("Call", mock.Anything, models.MethodSettingsUpdate, mock.Anything).Return("", err)
}

// SetupSystemsResponse configures the mock to return a systems response.
func (m *MockAPIClient) SetupSystemsResponse(systems []models.System) {
	resp := models.SystemsResponse{Systems: systems}
	data, _ := json.Marshal(resp)
	m.On("Call", mock.Anything, models.MethodSystems, "").Return(string(data), nil)
}

// SetupSystemsError configures the mock to return an error for systems.
func (m *MockAPIClient) SetupSystemsError(err error) {
	m.On("Call", mock.Anything, models.MethodSystems, "").Return("", err)
}

// SetupTokenNotification configures the mock to return a token notification.
func (m *MockAPIClient) SetupTokenNotification(token *models.TokenResponse) {
	data, _ := json.Marshal(token)
	m.On("WaitNotification", mock.Anything, mock.Anything, models.NotificationTokensAdded).Return(string(data), nil)
}
