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

package methods

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/profiles"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// newProfilesEnv builds a RequestEnv with a real profiles service over a
// mock user DB.
func newProfilesEnv(t *testing.T) (env requests.RequestEnv, mockUserDB *helpers.MockUserDBI, st *state.State) {
	t.Helper()
	mockUserDB = helpers.NewMockUserDBI()
	st, ns := state.NewState(nil, "boot")
	t.Cleanup(func() {
		for {
			select {
			case <-ns:
			default:
				return
			}
		}
	})
	db := &database.Database{UserDB: mockUserDB, MediaDB: nil}
	env = requests.RequestEnv{
		Context:  context.Background(),
		Database: db,
		State:    st,
		Profiles: profiles.NewService(db, st),
	}
	return env, mockUserDB, st
}

func testProfileRow(t *testing.T, pin string) *database.Profile {
	t.Helper()
	p := &database.Profile{
		ProfileID: "profile-1",
		Name:      "Kid A",
		SwitchID:  "corn-arm-truck",
		CreatedAt: 1700000000,
		UpdatedAt: 1700000000,
	}
	if pin != "" {
		hash, err := profiles.HashPIN(pin)
		require.NoError(t, err)
		p.PINHash = hash
	}
	return p
}

func TestHandleProfiles_List(t *testing.T) {
	t.Parallel()
	env, mockUserDB, _ := newProfilesEnv(t)

	mockUserDB.On("ListProfiles").Return([]database.Profile{*testProfileRow(t, "1234")}, nil)

	result, err := HandleProfiles(env)
	require.NoError(t, err)
	resp, ok := result.(models.ProfilesResponse)
	require.True(t, ok)
	require.Len(t, resp.Profiles, 1)
	assert.Equal(t, "profile-1", resp.Profiles[0].ProfileID)
	assert.Equal(t, "corn-arm-truck", resp.Profiles[0].SwitchID)
	assert.True(t, resp.Profiles[0].HasPIN)

	// The PIN hash must never appear in the serialized response.
	raw, err := json.Marshal(resp)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "pbkdf2")
}

func TestHandleProfilesNew(t *testing.T) {
	t.Parallel()
	env, mockUserDB, _ := newProfilesEnv(t)

	mockUserDB.On("CreateProfile", mock.Anything).Return(nil)

	env.Params = json.RawMessage(`{"name": "Kid A", "pin": "1234", "dailyLimit": "2h"}`)
	result, err := HandleProfilesNew(env)
	require.NoError(t, err)
	resp, ok := result.(models.ProfileResponse)
	require.True(t, ok)
	assert.Equal(t, "Kid A", resp.Name)
	assert.NotEmpty(t, resp.SwitchID)
	assert.True(t, resp.HasPIN)
	require.NotNil(t, resp.DailyLimit)
	assert.Equal(t, "2h", *resp.DailyLimit)
}

func TestHandleProfilesNew_InvalidParams(t *testing.T) {
	t.Parallel()
	env, _, _ := newProfilesEnv(t)

	// Missing required name.
	env.Params = json.RawMessage(`{"pin": "1234"}`)
	_, err := HandleProfilesNew(env)
	require.Error(t, err)

	// Non-numeric PIN.
	env.Params = json.RawMessage(`{"name": "Kid A", "pin": "abcd"}`)
	_, err = HandleProfilesNew(env)
	require.Error(t, err)

	// Bad duration.
	env.Params = json.RawMessage(`{"name": "Kid A", "dailyLimit": "2 hours"}`)
	_, err = HandleProfilesNew(env)
	require.Error(t, err)
}

func TestHandleProfilesSwitch_PINFlow(t *testing.T) {
	t.Parallel()
	env, mockUserDB, st := newProfilesEnv(t)

	mockUserDB.On("GetProfile", "profile-1").Return(testProfileRow(t, "1234"), nil)

	// Missing PIN.
	env.Params = json.RawMessage(`{"profileId": "profile-1"}`)
	_, err := HandleProfilesSwitch(env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "PIN")

	// Wrong PIN.
	env.Params = json.RawMessage(`{"profileId": "profile-1", "pin": "9999"}`)
	_, err = HandleProfilesSwitch(env)
	require.Error(t, err)

	assert.Nil(t, st.ActiveProfile())

	// Correct PIN.
	mockUserDB.On("SetDeviceState", database.DeviceStateKeyActiveProfile, "profile-1").Return(nil)
	env.Params = json.RawMessage(`{"profileId": "profile-1", "pin": "1234"}`)
	result, err := HandleProfilesSwitch(env)
	require.NoError(t, err)
	active, ok := result.(*models.ActiveProfile)
	require.True(t, ok)
	assert.Equal(t, "profile-1", active.ProfileID)
	require.NotNil(t, st.ActiveProfile())
}

func TestHandleProfilesSwitch_BySwitchIDStillRequiresPIN(t *testing.T) {
	t.Parallel()
	env, mockUserDB, _ := newProfilesEnv(t)

	mockUserDB.On("GetProfileBySwitchID", "corn-arm-truck").Return(testProfileRow(t, "1234"), nil)

	// Knowing the switch ID is not possession of the card: the API path
	// still enforces the PIN.
	env.Params = json.RawMessage(`{"switchId": "corn-arm-truck"}`)
	_, err := HandleProfilesSwitch(env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "PIN")
}

func TestHandleProfilesSwitch_DeactivateIsFree(t *testing.T) {
	t.Parallel()
	env, mockUserDB, st := newProfilesEnv(t)

	st.SetActiveProfile(&models.ActiveProfile{ProfileID: "profile-1", Name: "Kid A", HasPIN: true})
	mockUserDB.On("DeleteDeviceState", database.DeviceStateKeyActiveProfile).Return(nil)

	// No params at all = deactivate; PINs gate entry only.
	env.Params = nil
	result, err := HandleProfilesSwitch(env)
	require.NoError(t, err)
	active, ok := result.(*models.ActiveProfile)
	require.True(t, ok)
	assert.Nil(t, active)
	assert.Nil(t, st.ActiveProfile())
}

func TestHandleProfilesActive(t *testing.T) {
	t.Parallel()
	env, _, st := newProfilesEnv(t)

	result, err := HandleProfilesActive(env)
	require.NoError(t, err)
	assert.Nil(t, result.(*models.ActiveProfile))

	st.SetActiveProfile(&models.ActiveProfile{ProfileID: "profile-1", Name: "Kid A"})
	result, err = HandleProfilesActive(env)
	require.NoError(t, err)
	active, ok := result.(*models.ActiveProfile)
	require.True(t, ok)
	assert.Equal(t, "profile-1", active.ProfileID)
}

func TestHandleProfilesDelete(t *testing.T) {
	t.Parallel()
	env, mockUserDB, st := newProfilesEnv(t)

	st.SetActiveProfile(&models.ActiveProfile{ProfileID: "profile-1", Name: "Kid A"})
	mockUserDB.On("DeleteProfile", "profile-1").Return(nil)

	env.Params = json.RawMessage(`{"profileId": "profile-1"}`)
	result, err := HandleProfilesDelete(env)
	require.NoError(t, err)
	assert.Equal(t, NoContent{}, result)
	assert.Nil(t, st.ActiveProfile(), "deleting the active profile deactivates it")
}

func TestHandleProfilesUpdate_ClearPIN(t *testing.T) {
	t.Parallel()
	env, mockUserDB, _ := newProfilesEnv(t)

	mockUserDB.On("GetProfile", "profile-1").Return(testProfileRow(t, "1234"), nil)
	mockUserDB.On("UpdateProfile", mock.MatchedBy(func(p *database.Profile) bool {
		return p.PINHash == ""
	})).Return(nil)

	env.Params = json.RawMessage(`{"profileId": "profile-1", "clearPin": true}`)
	result, err := HandleProfilesUpdate(env)
	require.NoError(t, err)
	resp, ok := result.(models.ProfileResponse)
	require.True(t, ok)
	assert.False(t, resp.HasPIN)
	mockUserDB.AssertExpectations(t)
}
