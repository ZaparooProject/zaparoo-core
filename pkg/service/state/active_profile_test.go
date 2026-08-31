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

package state_test

import (
	"encoding/json"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetActiveProfile_StoresCopy(t *testing.T) {
	t.Parallel()

	st, _ := state.NewState(nil, "boot")
	assert.Nil(t, st.ActiveProfile())

	profile := &models.ActiveProfile{
		ProfileID: "profile-1",
		Name:      "Kid A",
		HasPIN:    true,
	}
	st.SetActiveProfile(profile)

	// Mutating the caller's struct must not affect stored state.
	profile.Name = "mutated"

	got := st.ActiveProfile()
	require.NotNil(t, got)
	assert.Equal(t, "Kid A", got.Name)
	assert.Equal(t, "profile-1", got.ProfileID)
	assert.True(t, got.HasPIN)

	// Mutating the returned copy must not affect stored state either.
	got.Name = "also mutated"
	assert.Equal(t, "Kid A", st.ActiveProfile().Name)
}

func TestSetActiveProfile_Notifications(t *testing.T) {
	t.Parallel()

	st, ns := state.NewState(nil, "boot")

	st.SetActiveProfile(&models.ActiveProfile{ProfileID: "profile-1", Name: "Kid A"})
	activated := <-ns
	assert.Equal(t, models.NotificationProfilesActive, activated.Method)
	var payload models.ProfilesActiveNotification
	require.NoError(t, json.Unmarshal(activated.Params, &payload))
	require.NotNil(t, payload.Profile)
	assert.Equal(t, "profile-1", payload.Profile.ProfileID)

	st.SetActiveProfile(nil)
	deactivated := <-ns
	assert.Equal(t, models.NotificationProfilesActive, deactivated.Method)
	var nilPayload models.ProfilesActiveNotification
	require.NoError(t, json.Unmarshal(deactivated.Params, &nilPayload))
	assert.Nil(t, nilPayload.Profile)
	assert.Nil(t, st.ActiveProfile())

	// Duplicate deactivation emits no second notification.
	st.SetActiveProfile(nil)
	select {
	case n := <-ns:
		t.Fatalf("unexpected notification: %s", n.Method)
	default:
	}
}
