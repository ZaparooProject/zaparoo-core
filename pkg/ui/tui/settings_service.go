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
	"errors"
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

	// GetProfiles fetches profiles without privileged switch IDs.
	GetProfiles(ctx context.Context) (*models.ProfilesResponse, error)

	// VerifyProfileManagement verifies one admin credential as a UI gate.
	VerifyProfileManagement(ctx context.Context, profileID, pin string) error

	// GetActiveProfile fetches the active profile, or nil when the device
	// is on the shared profile.
	GetActiveProfile(ctx context.Context) (*models.ActiveProfile, error)

	// NewProfile creates a profile and returns it (including its
	// generated switch ID).
	NewProfile(ctx context.Context, params *models.NewProfileParams) (*models.ProfileResponse, error)

	// UpdateProfile updates a profile.
	UpdateProfile(ctx context.Context, params *models.UpdateProfileParams) (*models.ProfileResponse, error)

	// DeleteProfile removes a profile.
	DeleteProfile(ctx context.Context, profileID string) error

	// SwitchProfile switches the active profile. Nil params deactivates
	// (switches to the shared profile).
	SwitchProfile(ctx context.Context, params *models.SwitchProfileParams) error

	// GetClients fetches paired clients.
	GetClients(ctx context.Context) (*models.ClientsResponse, error)

	// StartClientPairing starts a local pairing approval flow.
	StartClientPairing(ctx context.Context, role string) (*models.ClientsPairStartResponse, error)

	// CancelClientPairing cancels an active pairing flow.
	CancelClientPairing(ctx context.Context) error

	// DeleteClient revokes a paired client.
	DeleteClient(ctx context.Context, clientID string) error
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

// VerifyProfileManagement checks one administrator credential without
// retaining any client-side authorization state.
func (s *DefaultSettingsService) VerifyProfileManagement(
	ctx context.Context, profileID, pin string,
) error {
	data, err := json.Marshal(models.VerifyProfileParams{ProfileID: &profileID, PIN: &pin})
	if err != nil {
		return fmt.Errorf("failed to marshal management verification: %w", err)
	}
	resp, err := s.apiClient.Call(ctx, models.MethodProfilesVerify, string(data))
	if err != nil {
		return fmt.Errorf("failed to verify profile management: %w", err)
	}
	var verified models.ProfileVerifyResponse
	if err := json.Unmarshal([]byte(resp), &verified); err != nil {
		return fmt.Errorf("failed to parse management verification: %w", err)
	}
	if verified.Role != "admin" {
		return errors.New("administrator profile required")
	}
	return nil
}

// GetProfiles fetches profiles without privileged switch IDs.
func (s *DefaultSettingsService) GetProfiles(ctx context.Context) (*models.ProfilesResponse, error) {
	return s.callProfiles(ctx, "")
}

func (s *DefaultSettingsService) callProfiles(ctx context.Context, params string) (*models.ProfilesResponse, error) {
	resp, err := s.apiClient.Call(ctx, models.MethodProfiles, params)
	if err != nil {
		return nil, fmt.Errorf("failed to get profiles: %w", err)
	}
	var profiles models.ProfilesResponse
	if err := json.Unmarshal([]byte(resp), &profiles); err != nil {
		return nil, fmt.Errorf("failed to parse profiles: %w", err)
	}
	return &profiles, nil
}

// GetActiveProfile fetches the active profile, or nil when the device is
// on the shared profile.
func (s *DefaultSettingsService) GetActiveProfile(ctx context.Context) (*models.ActiveProfile, error) {
	resp, err := s.apiClient.Call(ctx, models.MethodProfilesActive, "")
	if err != nil {
		return nil, fmt.Errorf("failed to get active profile: %w", err)
	}
	var active *models.ActiveProfile
	if resp != "" {
		if err := json.Unmarshal([]byte(resp), &active); err != nil {
			return nil, fmt.Errorf("failed to parse active profile: %w", err)
		}
	}
	return active, nil
}

// NewProfile creates a profile.
func (s *DefaultSettingsService) NewProfile(
	ctx context.Context,
	params *models.NewProfileParams,
) (*models.ProfileResponse, error) {
	data, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal profile params: %w", err)
	}
	resp, err := s.apiClient.Call(ctx, models.MethodProfilesNew, string(data))
	if err != nil {
		return nil, fmt.Errorf("failed to create profile: %w", err)
	}
	var profile models.ProfileResponse
	if err := json.Unmarshal([]byte(resp), &profile); err != nil {
		return nil, fmt.Errorf("failed to parse profile: %w", err)
	}
	return &profile, nil
}

// UpdateProfile updates a profile.
func (s *DefaultSettingsService) UpdateProfile(
	ctx context.Context,
	params *models.UpdateProfileParams,
) (*models.ProfileResponse, error) {
	data, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal profile params: %w", err)
	}
	resp, err := s.apiClient.Call(ctx, models.MethodProfilesUpdate, string(data))
	if err != nil {
		return nil, fmt.Errorf("failed to update profile: %w", err)
	}
	var profile models.ProfileResponse
	if err := json.Unmarshal([]byte(resp), &profile); err != nil {
		return nil, fmt.Errorf("failed to parse profile: %w", err)
	}
	return &profile, nil
}

// DeleteProfile removes a profile.
func (s *DefaultSettingsService) DeleteProfile(ctx context.Context, profileID string) error {
	data, err := json.Marshal(models.DeleteProfileParams{ProfileID: profileID})
	if err != nil {
		return fmt.Errorf("failed to marshal profile params: %w", err)
	}
	_, err = s.apiClient.Call(ctx, models.MethodProfilesDelete, string(data))
	if err != nil {
		return fmt.Errorf("failed to delete profile: %w", err)
	}
	return nil
}

// SwitchProfile switches the active profile. Nil params deactivates.
func (s *DefaultSettingsService) SwitchProfile(ctx context.Context, params *models.SwitchProfileParams) error {
	paramsJSON := ""
	if params != nil {
		data, err := json.Marshal(params)
		if err != nil {
			return fmt.Errorf("failed to marshal switch params: %w", err)
		}
		paramsJSON = string(data)
	}
	_, err := s.apiClient.Call(ctx, models.MethodProfilesSwitch, paramsJSON)
	if err != nil {
		return fmt.Errorf("failed to switch profile: %w", err)
	}
	return nil
}

// GetClients fetches paired clients.
func (s *DefaultSettingsService) GetClients(ctx context.Context) (*models.ClientsResponse, error) {
	resp, err := s.apiClient.Call(ctx, models.MethodClients, "")
	if err != nil {
		return nil, fmt.Errorf("failed to get paired clients: %w", err)
	}
	var clients models.ClientsResponse
	if err := json.Unmarshal([]byte(resp), &clients); err != nil {
		return nil, fmt.Errorf("failed to parse paired clients: %w", err)
	}
	return &clients, nil
}

// StartClientPairing starts a local pairing approval flow.
func (s *DefaultSettingsService) StartClientPairing(
	ctx context.Context, role string,
) (*models.ClientsPairStartResponse, error) {
	data, err := json.Marshal(models.ClientsPairStartParams{Role: role})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal pairing params: %w", err)
	}
	resp, err := s.apiClient.Call(ctx, models.MethodClientsPairStart, string(data))
	if err != nil {
		return nil, fmt.Errorf("failed to start client pairing: %w", err)
	}
	var pairing models.ClientsPairStartResponse
	if err := json.Unmarshal([]byte(resp), &pairing); err != nil {
		return nil, fmt.Errorf("failed to parse pairing response: %w", err)
	}
	return &pairing, nil
}

// CancelClientPairing cancels an active pairing flow.
func (s *DefaultSettingsService) CancelClientPairing(ctx context.Context) error {
	if _, err := s.apiClient.Call(ctx, models.MethodClientsPairCancel, ""); err != nil {
		return fmt.Errorf("failed to cancel client pairing: %w", err)
	}
	return nil
}

// DeleteClient revokes a paired client.
func (s *DefaultSettingsService) DeleteClient(ctx context.Context, clientID string) error {
	data, err := json.Marshal(models.ClientsDeleteParams{ClientID: clientID})
	if err != nil {
		return fmt.Errorf("failed to marshal client delete params: %w", err)
	}
	if _, err := s.apiClient.Call(ctx, models.MethodClientsDelete, string(data)); err != nil {
		return fmt.Errorf("failed to delete paired client: %w", err)
	}
	return nil
}
