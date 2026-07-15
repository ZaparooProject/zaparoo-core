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

package cli

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"text/tabwriter"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
)

const generatedProfilePINLength = 8

type profileAPICaller func(
	ctx context.Context,
	cfg *config.Instance,
	method string,
	params string,
) (string, error)

func listProfiles(
	ctx context.Context,
	cfg *config.Instance,
	out io.Writer,
	call profileAPICaller,
) error {
	resp, err := call(ctx, cfg, models.MethodProfiles, "")
	if err != nil {
		return fmt.Errorf("failed to list profiles: %w", err)
	}

	var profiles models.ProfilesResponse
	if err := json.Unmarshal([]byte(resp), &profiles); err != nil {
		return fmt.Errorf("failed to parse profiles response: %w", err)
	}

	w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(w, "PROFILE ID\tROLE\tNAME"); err != nil {
		return fmt.Errorf("failed to write profiles: %w", err)
	}
	for i := range profiles.Profiles {
		profile := &profiles.Profiles[i]
		if _, err := fmt.Fprintf(w, "%s\t%s\t%s\n",
			sanitizeForOutput(profile.ProfileID),
			sanitizeForOutput(profile.Role),
			sanitizeForOutput(profile.Name),
		); err != nil {
			return fmt.Errorf("failed to write profiles: %w", err)
		}
	}
	if err := w.Flush(); err != nil {
		return fmt.Errorf("failed to flush profiles: %w", err)
	}
	return nil
}

func resetProfilePIN(
	ctx context.Context,
	cfg *config.Instance,
	profileID string,
	random io.Reader,
	call profileAPICaller,
) (string, error) {
	pin, err := generateProfilePIN(random)
	if err != nil {
		return "", err
	}
	params, err := json.Marshal(models.UpdateProfileParams{
		ProfileID: profileID,
		PIN:       &pin,
	})
	if err != nil {
		return "", fmt.Errorf("failed to encode profile PIN reset: %w", err)
	}
	if _, err := call(ctx, cfg, models.MethodProfilesUpdate, string(params)); err != nil {
		return "", fmt.Errorf("failed to reset profile PIN: %w", err)
	}
	return pin, nil
}

func resetProfileSwitchID(
	ctx context.Context,
	cfg *config.Instance,
	profileID string,
	call profileAPICaller,
) (string, error) {
	params, err := json.Marshal(models.UpdateProfileParams{
		ProfileID:          profileID,
		RegenerateSwitchID: true,
	})
	if err != nil {
		return "", fmt.Errorf("failed to encode switch ID reset: %w", err)
	}
	resp, err := call(ctx, cfg, models.MethodProfilesUpdate, string(params))
	if err != nil {
		return "", fmt.Errorf("failed to reset profile switch ID: %w", err)
	}
	var profile models.ProfileResponse
	if err := json.Unmarshal([]byte(resp), &profile); err != nil {
		return "", fmt.Errorf("failed to parse profile response: %w", err)
	}
	if profile.SwitchID == "" {
		return "", errors.New("profile response did not include a switch ID")
	}
	return profile.SwitchID, nil
}

func generateProfilePIN(random io.Reader) (string, error) {
	limit := new(big.Int).Exp(big.NewInt(10), big.NewInt(generatedProfilePINLength), nil)
	value, err := rand.Int(random, limit)
	if err != nil {
		return "", fmt.Errorf("failed to generate profile PIN: %w", err)
	}
	return fmt.Sprintf("%0*d", generatedProfilePINLength, value.Int64()), nil
}
