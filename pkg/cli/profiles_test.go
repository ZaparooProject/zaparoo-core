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
	"encoding/json"
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListProfiles(t *testing.T) {
	t.Parallel()
	call := func(
		_ context.Context, _ *config.Instance, method, params string,
	) (string, error) {
		assert.Equal(t, models.MethodProfiles, method)
		assert.Empty(t, params)
		return `{"profiles":[{"profileId":"parent","name":"Parent","role":"admin"},` +
			`{"profileId":"kid","name":"Kid\nInjected","role":"member"}]}`, nil
	}

	var out strings.Builder
	err := listProfiles(context.Background(), nil, &out, call)
	require.NoError(t, err)
	assert.Contains(t, out.String(), "PROFILE ID")
	assert.Contains(t, out.String(), "parent      admin   Parent")
	assert.Contains(t, out.String(), "kid         member  KidInjected")
}

func TestResetProfilePIN_GeneratesAndUpdates(t *testing.T) {
	t.Parallel()
	call := func(
		_ context.Context, _ *config.Instance, method, params string,
	) (string, error) {
		assert.Equal(t, models.MethodProfilesUpdate, method)
		var update models.UpdateProfileParams
		require.NoError(t, json.Unmarshal([]byte(params), &update))
		assert.Equal(t, "profile-1", update.ProfileID)
		require.NotNil(t, update.PIN)
		assert.Equal(t, "00000000", *update.PIN)
		return `{"profileId":"profile-1"}`, nil
	}

	pin, err := resetProfilePIN(
		context.Background(), nil, "profile-1", strings.NewReader(strings.Repeat("\x00", 64)), call,
	)
	require.NoError(t, err)
	assert.Equal(t, "00000000", pin)
}

func TestResetProfileSwitchID_ReturnsReplacement(t *testing.T) {
	t.Parallel()
	call := func(
		_ context.Context, _ *config.Instance, method, params string,
	) (string, error) {
		assert.Equal(t, models.MethodProfilesUpdate, method)
		var update models.UpdateProfileParams
		require.NoError(t, json.Unmarshal([]byte(params), &update))
		assert.Equal(t, "profile-1", update.ProfileID)
		assert.True(t, update.RegenerateSwitchID)
		return `{"profileId":"profile-1","switchId":"new-switch-id"}`, nil
	}

	switchID, err := resetProfileSwitchID(context.Background(), nil, "profile-1", call)
	require.NoError(t, err)
	assert.Equal(t, "new-switch-id", switchID)
}
