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

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
)

// callKiosk calls a kiosk method and parses the lock state it returns.
func (s *DefaultSettingsService) callKiosk(
	ctx context.Context, method string, params any,
) (*models.KioskResponse, error) {
	payload := ""
	if params != nil {
		data, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal %s params: %w", method, err)
		}
		payload = string(data)
	}
	resp, err := s.apiClient.Call(ctx, method, payload)
	if err != nil {
		return nil, fmt.Errorf("failed to call %s: %w", method, err)
	}
	var state models.KioskResponse
	if err := json.Unmarshal([]byte(resp), &state); err != nil {
		return nil, fmt.Errorf("failed to parse %s response: %w", method, err)
	}
	return &state, nil
}

// GetKiosk returns the kiosk lock's state.
func (s *DefaultSettingsService) GetKiosk(ctx context.Context) (*models.KioskResponse, error) {
	return s.callKiosk(ctx, models.MethodKiosk, nil)
}

// EnableKiosk turns kiosk mode on from a preset.
func (s *DefaultSettingsService) EnableKiosk(
	ctx context.Context, preset string, confirmEmpty bool,
) (*models.KioskResponse, error) {
	return s.callKiosk(ctx, models.MethodKioskEnable,
		models.KioskEnableParams{Preset: preset, ConfirmEmpty: confirmEmpty})
}

// DisableKiosk turns kiosk mode off.
func (s *DefaultSettingsService) DisableKiosk(ctx context.Context) (*models.KioskResponse, error) {
	return s.callKiosk(ctx, models.MethodKioskDisable, nil)
}

// UnlockKiosk lifts the lock with an admin profile's PIN.
func (s *DefaultSettingsService) UnlockKiosk(
	ctx context.Context, profileID, pin string,
) (*models.KioskResponse, error) {
	return s.callKiosk(ctx, models.MethodKioskUnlock,
		models.KioskUnlockParams{ProfileID: &profileID, PIN: &pin})
}

// LockKiosk ends a temporary unlock.
func (s *DefaultSettingsService) LockKiosk(ctx context.Context) (*models.KioskResponse, error) {
	return s.callKiosk(ctx, models.MethodKioskLock, nil)
}

// GetProfileContent returns a profile's content rule. An empty profile ID
// is the shared profile.
func (s *DefaultSettingsService) GetProfileContent(
	ctx context.Context, profileID string,
) (*models.ProfilesContentResponse, error) {
	data, err := json.Marshal(models.ProfilesContentParams{ProfileID: profileID})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal content params: %w", err)
	}
	resp, err := s.apiClient.Call(ctx, models.MethodProfilesContent, string(data))
	if err != nil {
		return nil, fmt.Errorf("failed to get profile content: %w", err)
	}
	var content models.ProfilesContentResponse
	if err := json.Unmarshal([]byte(resp), &content); err != nil {
		return nil, fmt.Errorf("failed to parse profile content: %w", err)
	}
	return &content, nil
}

// UpdateProfileContent replaces a profile's content rule.
func (s *DefaultSettingsService) UpdateProfileContent(
	ctx context.Context, params *models.ProfilesContentUpdateParams,
) error {
	data, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("failed to marshal content update: %w", err)
	}
	if _, err := s.apiClient.Call(ctx, models.MethodProfilesContentUpdate, string(data)); err != nil {
		return fmt.Errorf("failed to update profile content: %w", err)
	}
	return nil
}
