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
	"errors"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/permissions"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/userdb"
	platformids "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/ids"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/profiles"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
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
		Role:      profiles.ProfileRoleMember,
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

func TestHandleProfiles_ListLocalIncludesSwitchIDButNeverPINHash(t *testing.T) {
	t.Parallel()
	env, mockUserDB, _ := newProfilesEnv(t)
	env.IsLocal = true

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

func TestHandleProfiles_LocalSeesSwitchIDs(t *testing.T) {
	t.Parallel()
	env, mockUserDB, _ := newProfilesEnv(t)
	env.IsLocal = true
	admin := testProfileRow(t, "1234")
	admin.Role = profiles.ProfileRoleAdmin
	lastUsedAt := int64(1784079000)
	admin.LastUsedAt = &lastUsedAt
	mockUserDB.On("ListProfiles").Return([]database.Profile{*admin}, nil)

	result, err := HandleProfiles(env)
	require.NoError(t, err)
	resp, ok := result.(models.ProfilesResponse)
	require.True(t, ok)
	require.Len(t, resp.Profiles, 1)
	assert.Equal(t, admin.SwitchID, resp.Profiles[0].SwitchID)
	assert.Equal(t, profiles.ProfileRoleAdmin, resp.Profiles[0].Role)
	require.NotNil(t, resp.Profiles[0].LastUsedAt)
	assert.Equal(t, lastUsedAt, *resp.Profiles[0].LastUsedAt)
}

func TestHandleProfiles_List_MemberOmitsSwitchIDs(t *testing.T) {
	t.Parallel()
	env, mockUserDB, _ := newProfilesEnv(t)
	env.IsLocal = false
	env.ClientRole = string(permissions.RoleMember)

	mockUserDB.On("ListProfiles").Return([]database.Profile{*testProfileRow(t, "1234")}, nil)

	result, err := HandleProfiles(env)
	require.NoError(t, err)
	resp, ok := result.(models.ProfilesResponse)
	require.True(t, ok)
	require.Len(t, resp.Profiles, 1)
	assert.Equal(t, "profile-1", resp.Profiles[0].ProfileID)

	// Switch IDs are bearer credentials: a member client must never see
	// them, in the struct or on the wire.
	assert.Empty(t, resp.Profiles[0].SwitchID)
	raw, err := json.Marshal(resp)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "switchId")
	assert.NotContains(t, string(raw), "corn-arm-truck")
}

func TestHandleProfiles_List_UnpairedRemoteSeesSwitchIDs(t *testing.T) {
	t.Parallel()
	env, mockUserDB, _ := newProfilesEnv(t)
	env.IsLocal = false
	env.ClientRole = "" // no paired identity
	env.PlatformID = platformids.Mister

	mockUserDB.On("ListProfiles").Return([]database.Profile{*testProfileRow(t, "1234")}, nil)

	// MiSTer preserves switch-ID visibility for its grandfathered legacy API.
	result, err := HandleProfiles(env)
	require.NoError(t, err)
	resp, ok := result.(models.ProfilesResponse)
	require.True(t, ok)
	require.Len(t, resp.Profiles, 1)
	assert.Equal(t, "corn-arm-truck", resp.Profiles[0].SwitchID)
}

func TestHandleProfilesUpdate_MemberForbidden(t *testing.T) {
	t.Parallel()
	env, _, _ := newProfilesEnv(t)
	env.IsLocal = false
	env.ClientRole = string(permissions.RoleMember)

	// A member client cannot mutate profiles — profiles.update could
	// clear PINs and remove limits.
	env.Params = json.RawMessage(`{"profileId": "profile-1", "clearPin": true}`)
	_, err := HandleProfilesUpdate(env)
	require.ErrorIs(t, err, ErrForbidden)

	_, err = HandleProfilesDelete(env)
	require.Error(t, err)

	env.Params = json.RawMessage(`{"name": "Me"}`)
	_, err = HandleProfilesNew(env)
	require.ErrorIs(t, err, ErrForbidden)
}

func TestHandleProfilesNew(t *testing.T) {
	t.Parallel()
	env, mockUserDB, _ := newProfilesEnv(t)
	env.IsLocal = true

	mockUserDB.On("ListProfiles").Return([]database.Profile{}, nil).Once()
	mockUserDB.On("CreateProfile", mock.Anything).Run(func(args mock.Arguments) {
		args.Get(0).(*database.Profile).Role = profiles.ProfileRoleAdmin
	}).Return(nil)

	env.Params = json.RawMessage(`{"name": "Kid A", "pin": "1234", "dailyLimit": "2h"}`)
	result, err := HandleProfilesNew(env)
	require.NoError(t, err)
	resp, ok := result.(models.ProfileResponse)
	require.True(t, ok)
	assert.Equal(t, "Kid A", resp.Name)
	assert.NotEmpty(t, resp.SwitchID)
	assert.Equal(t, profiles.ProfileRoleAdmin, resp.Role)
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
	mockUserDB.On("ActivateProfile", "profile-1", mock.AnythingOfType("int64")).Return(nil)
	env.Params = json.RawMessage(`{"profileId": "profile-1", "pin": "1234"}`)
	result, err := HandleProfilesSwitch(env)
	require.NoError(t, err)
	active, ok := result.(*models.ActiveProfile)
	require.True(t, ok)
	assert.Equal(t, "profile-1", active.ProfileID)
	require.NotNil(t, st.ActiveProfile())
}

func TestHandleProfilesSwitch_BySwitchIDNeedsNoPIN(t *testing.T) {
	t.Parallel()
	env, mockUserDB, st := newProfilesEnv(t)

	mockUserDB.On("GetProfileBySwitchID", "corn-arm-truck").Return(testProfileRow(t, "1234"), nil)
	mockUserDB.On("ActivateProfile", "profile-1", mock.AnythingOfType("int64")).Return(nil)

	// The switch ID is a bearer credential: presenting it authorizes the
	// switch with no PIN, identically to scanning the card it's written on.
	env.Params = json.RawMessage(`{"switchId": "corn-arm-truck"}`)
	result, err := HandleProfilesSwitch(env)
	require.NoError(t, err)
	active, ok := result.(*models.ActiveProfile)
	require.True(t, ok)
	assert.Equal(t, "profile-1", active.ProfileID)
	require.NotNil(t, st.ActiveProfile())
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
	env.IsLocal = true

	st.SetActiveProfile(&models.ActiveProfile{ProfileID: "profile-1", Name: "Kid A"})
	mockUserDB.On("DeleteProfile", "profile-1").Return(nil)

	env.Params = json.RawMessage(`{"profileId": "profile-1"}`)
	result, err := HandleProfilesDelete(env)
	require.NoError(t, err)
	assert.Equal(t, NoContent{}, result)
	assert.Nil(t, st.ActiveProfile(), "deleting the active profile deactivates it")
}

func TestHandleProfilesUpdate_LocalRecoveryPromotesAdmin(t *testing.T) {
	t.Parallel()
	env, mockUserDB, _ := newProfilesEnv(t)
	env.IsLocal = true
	member := testProfileRow(t, "")
	member.Role = profiles.ProfileRoleMember
	mockUserDB.On("ListProfiles").Return([]database.Profile{*member}, nil)
	mockUserDB.On("GetProfile", member.ProfileID).Return(member, nil)
	mockUserDB.On("UpdateProfile", mock.MatchedBy(func(p *database.Profile) bool {
		return p.Role == profiles.ProfileRoleAdmin && p.PINHash != ""
	})).Return(nil)
	env.Params = json.RawMessage(`{"profileId":"profile-1","role":"admin","pin":"1234"}`)

	result, err := HandleProfilesUpdate(env)
	require.NoError(t, err)
	resp, ok := result.(models.ProfileResponse)
	require.True(t, ok)
	assert.Equal(t, profiles.ProfileRoleAdmin, resp.Role)
	assert.True(t, resp.HasPIN)
}

func TestHandleProfilesUpdate_ClearPIN(t *testing.T) {
	t.Parallel()
	env, mockUserDB, _ := newProfilesEnv(t)
	env.IsLocal = true

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

func TestHandleProfilesVerify(t *testing.T) {
	t.Parallel()
	env, mockUserDB, st := newProfilesEnv(t)

	mockUserDB.On("GetProfile", "profile-1").Return(testProfileRow(t, "1234"), nil)
	mockUserDB.On("GetProfileBySwitchID", "corn-arm-truck").Return(testProfileRow(t, "1234"), nil)

	// Wrong PIN fails.
	env.Params = json.RawMessage(`{"profileId": "profile-1", "pin": "9999"}`)
	_, err := HandleProfilesVerify(env)
	require.Error(t, err)

	// Missing PIN fails when the profile has one.
	env.Params = json.RawMessage(`{"profileId": "profile-1"}`)
	_, err = HandleProfilesVerify(env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "PIN")

	// Correct PIN verifies and returns the identity.
	env.Params = json.RawMessage(`{"profileId": "profile-1", "pin": "1234"}`)
	result, err := HandleProfilesVerify(env)
	require.NoError(t, err)
	resp, ok := result.(models.ProfileVerifyResponse)
	require.True(t, ok)
	assert.Equal(t, "profile-1", resp.ProfileID)
	assert.Equal(t, "Kid A", resp.Name)
	assert.True(t, resp.HasPIN)

	// Switch ID is a bearer credential: resolving it is the verification.
	env.Params = json.RawMessage(`{"switchId": "corn-arm-truck"}`)
	result, err = HandleProfilesVerify(env)
	require.NoError(t, err)
	resp, ok = result.(models.ProfileVerifyResponse)
	require.True(t, ok)
	assert.Equal(t, "profile-1", resp.ProfileID)

	// Verification grants nothing: no profile was activated by any of the
	// above, and no device state was written (the mock would have failed
	// on an unexpected SetDeviceState call).
	assert.Nil(t, st.ActiveProfile())

	// Neither selector is an invalid request.
	env.Params = json.RawMessage(`{}`)
	_, err = HandleProfilesVerify(env)
	require.Error(t, err)

	// Both selectors is an ambiguous request, rejected rather than
	// silently preferring one.
	env.Params = json.RawMessage(`{"profileId": "profile-1", "switchId": "corn-arm-truck", "pin": "1234"}`)
	_, err = HandleProfilesVerify(env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exactly one")
}

func TestHandleProfilesSwitch_RejectsBothSelectors(t *testing.T) {
	t.Parallel()
	env, _, st := newProfilesEnv(t)

	env.Params = json.RawMessage(`{"profileId": "profile-1", "switchId": "corn-arm-truck", "pin": "1234"}`)
	_, err := HandleProfilesSwitch(env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exactly one")
	assert.Nil(t, st.ActiveProfile())
}

func TestHandleProfilesVerify_SharesRateLimiterWithSwitch(t *testing.T) {
	t.Parallel()
	env, mockUserDB, _ := newProfilesEnv(t)

	mockUserDB.On("GetProfile", "profile-1").Return(testProfileRow(t, "1234"), nil)

	// Exhaust the PIN attempt limit through verify...
	env.Params = json.RawMessage(`{"profileId": "profile-1", "pin": "9999"}`)
	for range 5 {
		_, err := HandleProfilesVerify(env)
		require.Error(t, err)
	}

	// ...and switching is now rate limited too, even with the correct PIN:
	// verify cannot be used as a brute-force oracle that sidesteps the
	// switch path's protection.
	env.Params = json.RawMessage(`{"profileId": "profile-1", "pin": "1234"}`)
	_, err := HandleProfilesSwitch(env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "too many PIN attempts")
}

// verifyByScan runs profiles.verify with scan in the background and hands
// it the token once the claim is open.
func verifyByScan(t *testing.T, env *requests.RequestEnv, st *state.State, scanned *tokens.Token) (any, error) {
	t.Helper()
	type result struct {
		value any
		err   error
	}
	done := make(chan result, 1)
	go func() {
		v, err := HandleProfilesVerify(*env)
		done <- result{v, err}
	}()
	require.Eventually(t, st.HasScanClaims, time.Second, time.Millisecond)
	if scanned != nil {
		require.True(t, st.DeliverScanToClaims(*scanned))
	}
	select {
	case r := <-done:
		return r.value, r.err
	case <-time.After(time.Second):
		t.Fatal("profiles.verify did not return")
		return nil, errors.New("profiles.verify did not return")
	}
}

func TestHandleProfilesVerify_Scan(t *testing.T) {
	t.Parallel()
	env, mockUserDB, st := newProfilesEnv(t)
	env.ClientID = "client-1"
	mockUserDB.On("GetEnabledMappings").Return([]database.Mapping{}, nil)
	mockUserDB.On("GetProfileBySwitchID", "corn-arm-truck").Return(testProfileRow(t, "1234"), nil)
	env.Params = json.RawMessage(`{"scan": true}`)

	result, err := verifyByScan(t, &env, st, &tokens.Token{UID: "04a1", Text: "**profile:corn-arm-truck"})
	require.NoError(t, err)
	resp, ok := result.(models.ProfileVerifyResponse)
	require.True(t, ok)
	assert.Equal(t, "profile-1", resp.ProfileID)
	assert.True(t, resp.HasPIN)
	assert.Nil(t, st.ActiveProfile(), "verifying by scan switches nothing")
	assert.False(t, st.HasScanClaims(), "the claim is released")
}

func TestHandleProfilesVerify_ScanMappedCard(t *testing.T) {
	t.Parallel()
	env, mockUserDB, st := newProfilesEnv(t)
	mockUserDB.On("GetEnabledMappings").Return([]database.Mapping{{
		Type: userdb.MappingTypeID, Match: userdb.MatchTypeExact,
		Pattern: "04a1", Override: "**profile:corn-arm-truck", Enabled: true,
	}}, nil)
	mockUserDB.On("GetProfileBySwitchID", "corn-arm-truck").Return(testProfileRow(t, ""), nil)
	env.Params = json.RawMessage(`{"scan": true}`)

	result, err := verifyByScan(t, &env, st, &tokens.Token{UID: "04A1"})
	require.NoError(t, err)
	resp, ok := result.(models.ProfileVerifyResponse)
	require.True(t, ok)
	assert.Equal(t, "profile-1", resp.ProfileID)
}

func TestHandleProfilesVerify_ScanNotASwitchCard(t *testing.T) {
	t.Parallel()
	env, mockUserDB, st := newProfilesEnv(t)
	mockUserDB.On("GetEnabledMappings").Return([]database.Mapping{}, nil)
	env.Params = json.RawMessage(`{"scan": true}`)

	_, err := verifyByScan(t, &env, st, &tokens.Token{UID: "04a1", Text: "snes/smw.sfc"})
	require.ErrorIs(t, err, errNotSwitchCard)
	var clientErr *models.ClientError
	assert.ErrorAs(t, err, &clientErr)
}

func TestHandleProfilesVerify_ScanUnknownSwitchID(t *testing.T) {
	t.Parallel()
	env, mockUserDB, st := newProfilesEnv(t)
	mockUserDB.On("GetEnabledMappings").Return([]database.Mapping{}, nil)
	mockUserDB.On("GetProfileBySwitchID", "no-such-card").Return(nil, userdb.ErrProfileNotFound)
	env.Params = json.RawMessage(`{"scan": true}`)

	_, err := verifyByScan(t, &env, st, &tokens.Token{UID: "04a1", Text: "**profile:no-such-card"})
	require.ErrorIs(t, err, profiles.ErrNotFound)
}

func TestHandleProfilesVerify_ScanCancelled(t *testing.T) {
	t.Parallel()
	env, _, st := newProfilesEnv(t)
	env.ClientID = "client-1"
	env.Params = json.RawMessage(`{"scan": true}`)

	type result struct{ err error }
	done := make(chan result, 1)
	go func() {
		_, err := HandleProfilesVerify(env)
		done <- result{err}
	}()
	require.Eventually(t, st.HasScanClaims, time.Second, time.Millisecond)

	other := env
	other.ClientID = "client-2"
	_, err := HandleProfilesVerifyCancel(other)
	require.Error(t, err, "another connection cannot cancel the scan")
	assert.True(t, st.HasScanClaims())

	result2, err := HandleProfilesVerifyCancel(env)
	require.NoError(t, err)
	assert.Equal(t, NoContent{}, result2)

	select {
	case r := <-done:
		require.Error(t, r.err)
		assert.Contains(t, r.err.Error(), "cancelled")
	case <-time.After(time.Second):
		t.Fatal("profiles.verify did not return after cancel")
	}
	assert.False(t, st.HasScanClaims())
}

func TestHandleProfilesVerify_ScanTimesOut(t *testing.T) {
	t.Parallel()
	env, _, st := newProfilesEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	env.Context = ctx
	env.Params = json.RawMessage(`{"scan": true}`)

	done := make(chan error, 1)
	go func() {
		_, err := HandleProfilesVerify(env)
		done <- err
	}()
	require.Eventually(t, st.HasScanClaims, time.Second, time.Millisecond)
	cancel()

	select {
	case err := <-done:
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no card scanned")
	case <-time.After(time.Second):
		t.Fatal("profiles.verify did not return after the request ended")
	}
	assert.False(t, st.HasScanClaims(), "an ended request leaves no claim behind")
}

func TestHandleProfilesVerify_ScanRejectsOtherSelectors(t *testing.T) {
	t.Parallel()
	env, _, st := newProfilesEnv(t)
	env.Params = json.RawMessage(`{"scan": true, "switchId": "corn-arm-truck"}`)

	_, err := HandleProfilesVerify(env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exactly one")
	assert.False(t, st.HasScanClaims())
}
