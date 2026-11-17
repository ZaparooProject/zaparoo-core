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
	"time"

	gozapscript "github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	platformids "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/ids"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/kiosk"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/profiles"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/jonboulle/clockwork"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	kioskAdminID  = "admin-1"
	kioskKidID    = "kid-1"
	kioskAdminPIN = "4321"
	kioskKidPIN   = "1111"
)

// kioskFixture is a device with an admin profile, a member profile, a real
// user database and a kiosk lock, with nothing mocked between the handlers
// and the lock they enforce.
type kioskFixture struct {
	lock  *kiosk.Service
	db    *database.Database
	st    *state.State
	profs *profiles.Service
	clock *clockwork.FakeClock
}

func newKioskFixture(t *testing.T) *kioskFixture {
	t.Helper()
	userDB, cleanup := helpers.NewInMemoryUserDB(t)
	t.Cleanup(cleanup)
	db := &database.Database{UserDB: userDB}
	adminHash, err := profiles.HashPIN(kioskAdminPIN)
	require.NoError(t, err)
	kidHash, err := profiles.HashPIN(kioskKidPIN)
	require.NoError(t, err)
	require.NoError(t, userDB.CreateProfile(&database.Profile{
		ProfileID: kioskAdminID, Name: "Parent", Role: profiles.ProfileRoleAdmin,
		SwitchID: "a-b-c", PINHash: adminHash,
	}))
	require.NoError(t, userDB.CreateProfile(&database.Profile{
		ProfileID: kioskKidID, Name: "Kid", Role: profiles.ProfileRoleMember,
		SwitchID: "d-e-f", PINHash: kidHash,
	}))

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
	clock := clockwork.NewFakeClock()
	lock := kiosk.NewService(db, clock)
	st.SetKiosk(lock)
	return &kioskFixture{lock: lock, db: db, st: st, profs: profiles.NewService(db, st), clock: clock}
}

func (f *kioskFixture) env(mutate func(*requests.RequestEnv)) requests.RequestEnv {
	env := requests.RequestEnv{
		Context:    context.Background(),
		Database:   f.db,
		State:      f.st,
		Profiles:   f.profs,
		Kiosk:      f.lock,
		PlatformID: platformids.Linux,
	}
	if mutate != nil {
		mutate(&env)
	}
	return env
}

func (f *kioskFixture) local() requests.RequestEnv {
	return f.env(func(e *requests.RequestEnv) { e.IsLocal = true; e.PlatformID = "" })
}

func (f *kioskFixture) paired(role string) requests.RequestEnv {
	return f.env(func(e *requests.RequestEnv) { e.ClientRole = role })
}

func (f *kioskFixture) lockOpen(t *testing.T) {
	t.Helper()
	settings, ok := kiosk.PresetSettings(kiosk.PresetFamily)
	require.True(t, ok)
	settings.DefaultContentMode = database.ContentModeAll
	require.NoError(t, f.lock.Enable(&settings, kiosk.EnableOptions{}))
}

//nolint:gocritic // test helper takes the environment by value, as handlers do
func withKioskParams(t *testing.T, env requests.RequestEnv, params any) requests.RequestEnv {
	t.Helper()
	raw, err := json.Marshal(params)
	require.NoError(t, err)
	env.Params = raw
	return env
}

func kioskStr(s string) *string { return &s }

func unlockParams(profileID, pin string) models.KioskUnlockParams {
	return models.KioskUnlockParams{ProfileID: kioskStr(profileID), PIN: kioskStr(pin)}
}

func TestKioskEnableNeedsAdminAuthority(t *testing.T) {
	t.Parallel()
	params := models.KioskEnableParams{Preset: kiosk.PresetFamily, ConfirmEmpty: true}

	refused := map[string]func(*kioskFixture) requests.RequestEnv{
		"paired member": func(f *kioskFixture) requests.RequestEnv { return f.paired("member") },
		"unpaired":      func(f *kioskFixture) requests.RequestEnv { return f.env(nil) },
		"unpaired on MiSTer": func(f *kioskFixture) requests.RequestEnv {
			return f.env(func(e *requests.RequestEnv) { e.PlatformID = platformids.Mister })
		},
		"online remote": func(f *kioskFixture) requests.RequestEnv { return f.paired("remote") },
	}
	for name, build := range refused {
		f := newKioskFixture(t)
		_, err := HandleKioskEnable(withKioskParams(t, build(f), params))
		require.ErrorIs(t, err, ErrForbidden, name)
		assert.False(t, f.lock.Active(), name)
	}

	allowed := map[string]func(*kioskFixture) requests.RequestEnv{
		"localhost":    func(f *kioskFixture) requests.RequestEnv { return f.local() },
		"paired admin": func(f *kioskFixture) requests.RequestEnv { return f.paired("admin") },
		"API key": func(f *kioskFixture) requests.RequestEnv {
			return f.env(func(e *requests.RequestEnv) { e.APIKeyAuthenticated = true })
		},
	}
	for name, build := range allowed {
		f := newKioskFixture(t)
		result, err := HandleKioskEnable(withKioskParams(t, build(f), params))
		require.NoError(t, err, name)
		resp, ok := result.(models.KioskResponse)
		require.True(t, ok)
		assert.Equal(t, "locked", resp.State, name)
		require.NotNil(t, resp.Settings)
		assert.Equal(t, kiosk.PresetFamily, resp.Settings.Preset)
		assert.True(t, f.lock.Locked(), name)
	}
}

func TestKioskEnableAppliesSettingsOverPreset(t *testing.T) {
	t.Parallel()
	f := newKioskFixture(t)
	off := false
	mode := database.ContentModeAll
	result, err := HandleKioskEnable(withKioskParams(t, f.local(), models.KioskEnableParams{
		Preset:   kiosk.PresetFamily,
		Settings: &models.KioskSettingsPatch{ProfileSwitching: &off, DefaultContentMode: &mode},
	}))
	require.NoError(t, err)
	resp, ok := result.(models.KioskResponse)
	require.True(t, ok)
	assert.False(t, resp.Settings.ProfileSwitching)
	assert.Equal(t, database.ContentModeAll, resp.Settings.DefaultContentMode)
	assert.True(t, resp.Settings.DeviceUnlock, "untouched settings keep the preset's value")
}

func TestKioskEnableReportsNothingAllowed(t *testing.T) {
	t.Parallel()
	f := newKioskFixture(t)
	_, err := HandleKioskEnable(withKioskParams(t, f.local(), models.KioskEnableParams{Preset: kiosk.PresetFamily}))
	require.ErrorIs(t, err, kiosk.ErrNothingAllowed)
	assert.False(t, f.lock.Active())
}

func TestKioskAdminMethodsRefuseALockedDevicesOwnCallers(t *testing.T) {
	t.Parallel()
	f := newKioskFixture(t)
	f.lockOpen(t)
	on := true

	// The device itself is capped while locked, so nothing on it can turn
	// the lock off, change it, or read and rewrite the content rules.
	local := f.local()
	_, err := HandleKioskDisable(local)
	require.ErrorIs(t, err, ErrForbidden)
	_, err = HandleKioskUpdate(withKioskParams(t, local, models.KioskUpdateParams{
		Settings: models.KioskSettingsPatch{IdleReset: &on},
	}))
	require.ErrorIs(t, err, ErrForbidden)
	_, err = HandleKioskAllow(withKioskParams(t, local, models.KioskAllowParams{SystemID: "NES", Path: "/a.nes"}))
	require.ErrorIs(t, err, ErrForbidden)
	_, err = HandleProfilesContent(local)
	require.ErrorIs(t, err, ErrForbidden)
	_, err = HandleProfilesContentUpdate(withKioskParams(t, local, models.ProfilesContentUpdateParams{Mode: "all"}))
	require.ErrorIs(t, err, ErrForbidden)
	assert.True(t, f.lock.Locked())

	// A paired admin is not capped and can do all of it without unlocking.
	admin := f.paired("admin")
	_, err = HandleKioskAllow(withKioskParams(t, admin, models.KioskAllowParams{SystemID: "NES", Path: "/a.nes"}))
	require.NoError(t, err)
	_, err = HandleProfilesContent(admin)
	require.NoError(t, err)
	assert.True(t, f.lock.Locked(), "an override leaves the device locked")
	_, err = HandleKioskDisable(admin)
	require.NoError(t, err)
	assert.False(t, f.lock.Active())
}

func TestKioskUnlockAuthorization(t *testing.T) {
	t.Parallel()

	t.Run("paired admin needs no credential", func(t *testing.T) {
		t.Parallel()
		f := newKioskFixture(t)
		f.lockOpen(t)
		result, err := HandleKioskUnlock(f.paired("admin"))
		require.NoError(t, err)
		resp, ok := result.(models.KioskResponse)
		require.True(t, ok)
		assert.Equal(t, "unlocked", resp.State)
		require.NotNil(t, resp.UnlockUntil)
	})

	t.Run("admin PIN on the device", func(t *testing.T) {
		t.Parallel()
		f := newKioskFixture(t)
		f.lockOpen(t)
		_, err := HandleKioskUnlock(withKioskParams(t, f.local(), unlockParams(kioskAdminID, kioskAdminPIN)))
		require.NoError(t, err)
		assert.False(t, f.lock.Locked())
	})

	t.Run("wrong PIN on the device", func(t *testing.T) {
		t.Parallel()
		f := newKioskFixture(t)
		f.lockOpen(t)
		_, err := HandleKioskUnlock(withKioskParams(t, f.local(), unlockParams(kioskAdminID, "0000")))
		require.ErrorIs(t, err, profiles.ErrPINIncorrect)
		assert.True(t, f.lock.Locked())
	})

	t.Run("wrong PINs run into the rate limit", func(t *testing.T) {
		t.Parallel()
		f := newKioskFixture(t)
		f.lockOpen(t)
		for range 5 {
			_, err := HandleKioskUnlock(withKioskParams(t, f.local(), unlockParams(kioskAdminID, "0000")))
			require.ErrorIs(t, err, profiles.ErrPINIncorrect)
		}
		// The right PIN is refused too once the limit is hit.
		_, err := HandleKioskUnlock(withKioskParams(t, f.local(), unlockParams(kioskAdminID, kioskAdminPIN)))
		require.ErrorIs(t, err, profiles.ErrPINRateLimited)
		assert.True(t, f.lock.Locked())
	})

	t.Run("a member profile's PIN opens nothing", func(t *testing.T) {
		t.Parallel()
		f := newKioskFixture(t)
		f.lockOpen(t)
		_, err := HandleKioskUnlock(withKioskParams(t, f.local(), unlockParams(kioskKidID, kioskKidPIN)))
		require.ErrorIs(t, err, ErrForbidden)
		assert.True(t, f.lock.Locked())
	})

	t.Run("no PIN is taken from the network", func(t *testing.T) {
		t.Parallel()
		f := newKioskFixture(t)
		f.lockOpen(t)
		// Even the right PIN: a paired member could otherwise guess at it
		// from a script.
		for name, env := range map[string]requests.RequestEnv{
			"paired member": f.paired("member"),
			"API key":       f.env(func(e *requests.RequestEnv) { e.APIKeyAuthenticated = true }),
			"unpaired":      f.env(nil),
		} {
			_, err := HandleKioskUnlock(withKioskParams(t, env, unlockParams(kioskAdminID, kioskAdminPIN)))
			require.ErrorIs(t, err, ErrForbidden, name)
		}
		assert.True(t, f.lock.Locked())
	})

	t.Run("on-device unlock turned off", func(t *testing.T) {
		t.Parallel()
		f := newKioskFixture(t)
		require.NoError(t, f.db.UserDB.CreateClient(&database.Client{
			ClientID: "c1", ClientName: "parent phone", AuthToken: "t1", Role: "admin",
			PairingKey: make([]byte, 32),
		}))
		settings, _ := kiosk.PresetSettings(kiosk.PresetPublic)
		require.NoError(t, f.lock.Enable(&settings, kiosk.EnableOptions{}))

		_, err := HandleKioskUnlock(withKioskParams(t, f.local(), unlockParams(kioskAdminID, kioskAdminPIN)))
		require.ErrorIs(t, err, ErrKioskLocked)
		assert.True(t, f.lock.Locked())

		_, err = HandleKioskUnlock(f.paired("admin"))
		require.NoError(t, err)
		assert.False(t, f.lock.Locked())
	})

	t.Run("recovery token on the device", func(t *testing.T) {
		t.Parallel()
		f := newKioskFixture(t)
		fs := afero.NewMemMapFs()
		require.NoError(t, f.lock.InitRecoveryToken(fs, "/data"))
		raw, err := afero.ReadFile(fs, kiosk.RecoveryTokenPath("/data"))
		require.NoError(t, err)
		token := string(raw[:len(raw)-1])
		f.lockOpen(t)

		_, err = HandleKioskUnlock(withKioskParams(t, f.local(),
			models.KioskUnlockParams{RecoveryToken: kioskStr("not-the-token")}))
		require.ErrorIs(t, err, ErrForbidden)

		// The token proves access to the device's files, so it is only
		// honoured from the device.
		_, err = HandleKioskUnlock(withKioskParams(t, f.paired("member"),
			models.KioskUnlockParams{RecoveryToken: &token}))
		require.ErrorIs(t, err, ErrForbidden)
		assert.True(t, f.lock.Locked())

		_, err = HandleKioskUnlock(withKioskParams(t, f.local(), models.KioskUnlockParams{RecoveryToken: &token}))
		require.NoError(t, err)
		assert.False(t, f.lock.Locked())
	})

	t.Run("unlock takes a duration", func(t *testing.T) {
		t.Parallel()
		f := newKioskFixture(t)
		f.lockOpen(t)
		seconds := 60
		_, err := HandleKioskUnlock(withKioskParams(t, f.paired("admin"), models.KioskUnlockParams{Duration: &seconds}))
		require.NoError(t, err)
		assert.Equal(t, f.clock.Now().Add(time.Minute), f.lock.Status().UnlockUntil)
	})
}

func TestKioskLockIsOpenToAnyAuthenticatedCaller(t *testing.T) {
	t.Parallel()
	f := newKioskFixture(t)
	f.lockOpen(t)
	require.NoError(t, f.lock.Unlock(time.Minute))

	_, err := HandleKioskLock(f.env(nil))
	require.ErrorIs(t, err, ErrForbidden, "an unpaired client is not a caller at all")
	assert.False(t, f.lock.Locked())

	_, err = HandleKioskLock(f.paired("member"))
	require.NoError(t, err)
	assert.True(t, f.lock.Locked())
}

func TestKioskStatusReportsSeal(t *testing.T) {
	t.Parallel()
	f := newKioskFixture(t)

	result, err := HandleKiosk(f.local())
	require.NoError(t, err)
	off, ok := result.(models.KioskResponse)
	require.True(t, ok)
	assert.Equal(t, "off", off.State)
	assert.Nil(t, off.Settings)
	assert.False(t, off.Seal.Sealed)

	f.lockOpen(t)
	result, err = HandleKiosk(f.local())
	require.NoError(t, err)
	locked, ok := result.(models.KioskResponse)
	require.True(t, ok)
	assert.Equal(t, "locked", locked.State)
	assert.Nil(t, locked.UnlockUntil)
	// No platform is attached here, so there is no host to be unsealed.
	assert.True(t, locked.Seal.Sealed)
	assert.Empty(t, locked.Seal.Unsealed)
}

func TestProfilesContentUpdateRoundTrip(t *testing.T) {
	t.Parallel()
	f := newKioskFixture(t)
	env := f.local()

	result, err := HandleProfilesContentUpdate(withKioskParams(t, env, models.ProfilesContentUpdateParams{
		ProfileID: kioskKidID,
		Mode:      database.ContentModeAllowed,
		Entries: []models.ContentRuleEntry{
			{Effect: "allow", Kind: "system", SystemID: "nes"},
			{Effect: "deny", Kind: "media", SystemID: "NES", Path: "/games/NES/Scary.nes"},
			{Effect: "allow", Kind: "deck", Value: " kids-deck "},
			{Effect: "deny", Kind: "tags", Value: "rating:adult"},
		},
	}))
	require.NoError(t, err)
	resp, ok := result.(models.ProfilesContentResponse)
	require.True(t, ok)
	assert.Equal(t, database.ContentModeAllowed, resp.Mode)
	require.Len(t, resp.Entries, 4)
	assert.Equal(t, "NES", resp.Entries[0].SystemID, "system IDs are stored in their canonical spelling")
	assert.Equal(t, "kids-deck", resp.Entries[2].Value)

	// What was stored is what the lock enforces.
	f.lockOpen(t)
	ctx := context.Background()
	require.NoError(t, f.lock.CheckLaunch(ctx, kioskKidID, "NES", "/games/NES/Mario.nes", true))
	require.ErrorIs(t, f.lock.CheckLaunch(ctx, kioskKidID, "NES", "/games/NES/Scary.nes", true), kiosk.ErrLaunchRefused)
	require.ErrorIs(t,
		f.lock.CheckLaunch(ctx, kioskKidID, "SNES", "/games/SNES/Zelda.sfc", true), kiosk.ErrLaunchRefused)

	// The shared profile has no rule of its own and reports the default.
	result, err = HandleProfilesContent(f.paired("admin"))
	require.NoError(t, err)
	shared, ok := result.(models.ProfilesContentResponse)
	require.True(t, ok)
	assert.Empty(t, shared.Mode)
	assert.Equal(t, database.ContentModeAll, shared.EffectiveMode)
	assert.Empty(t, shared.Entries)
}

func TestProfilesContentUpdateRejectsBadEntries(t *testing.T) {
	t.Parallel()
	f := newKioskFixture(t)
	bad := map[string]models.ProfilesContentUpdateParams{
		"unknown profile": {ProfileID: "nobody"},
		"unknown system": {Entries: []models.ContentRuleEntry{
			{Effect: "allow", Kind: "system", SystemID: "NotASystem"},
		}},
		"media without a path": {Entries: []models.ContentRuleEntry{
			{Effect: "allow", Kind: "media", SystemID: "NES"},
		}},
		"deck without an ID": {Entries: []models.ContentRuleEntry{{Effect: "allow", Kind: "deck"}}},
		"tags without a filter": {Entries: []models.ContentRuleEntry{
			{Effect: "allow", Kind: "tags", Value: " , "},
		}},
		"unknown kind":   {Entries: []models.ContentRuleEntry{{Effect: "allow", Kind: "rating", Value: "e"}}},
		"unknown effect": {Entries: []models.ContentRuleEntry{{Effect: "maybe", Kind: "system", SystemID: "NES"}}},
		"unknown mode":   {Mode: "some"},
	}
	for name, params := range bad {
		_, err := HandleProfilesContentUpdate(withKioskParams(t, f.local(), params))
		require.Error(t, err, name)
	}
	rule, err := f.db.UserDB.GetContentRule("")
	require.NoError(t, err)
	assert.Empty(t, rule.Entries, "a refused update stores nothing")
}

func TestCappedCallersNeverSeeSwitchIDs(t *testing.T) {
	t.Parallel()
	f := newKioskFixture(t)

	switchIDs := func(env requests.RequestEnv) []string { //nolint:gocritic // test helper
		result, err := HandleProfiles(env)
		require.NoError(t, err)
		resp, ok := result.(models.ProfilesResponse)
		require.True(t, ok)
		ids := make([]string, 0, len(resp.Profiles))
		for i := range resp.Profiles {
			ids = append(ids, resp.Profiles[i].SwitchID)
		}
		return ids
	}

	assert.ElementsMatch(t, []string{"a-b-c", "d-e-f"}, switchIDs(f.local()), "kiosk off: the device sees them")
	f.lockOpen(t)
	assert.ElementsMatch(t, []string{"", ""}, switchIDs(f.local()), "locked: a switch ID is a way in")
	assert.ElementsMatch(t, []string{"a-b-c", "d-e-f"}, switchIDs(f.paired("admin")))
}

func TestProfilesSwitchUnderKiosk(t *testing.T) {
	t.Parallel()
	f := newKioskFixture(t)
	f.lockOpen(t)
	env := f.local()

	// To a member profile, with its PIN: allowed.
	_, err := HandleProfilesSwitch(withKioskParams(t, env, models.SwitchProfileParams{
		ProfileID: kioskStr(kioskKidID), PIN: kioskStr(kioskKidPIN),
	}))
	require.NoError(t, err)
	require.NotNil(t, f.st.ActiveProfile())

	// To the admin profile, even with the right PIN or its card: refused.
	_, err = HandleProfilesSwitch(withKioskParams(t, env, models.SwitchProfileParams{
		ProfileID: kioskStr(kioskAdminID), PIN: kioskStr(kioskAdminPIN),
	}))
	require.ErrorIs(t, err, profiles.ErrKioskLocked)
	_, err = HandleProfilesSwitch(withKioskParams(t, env, models.SwitchProfileParams{SwitchID: kioskStr("a-b-c")}))
	require.ErrorIs(t, err, profiles.ErrKioskLocked)

	// Out to the shared profile: refused, it cannot carry a PIN.
	_, err = HandleProfilesSwitch(env)
	require.ErrorIs(t, err, profiles.ErrKioskLocked)
	assert.Equal(t, kioskKidID, f.st.ActiveProfile().ProfileID)

	// With switching turned off, nothing moves.
	settings := f.lock.Status().Settings
	settings.ProfileSwitching = false
	require.NoError(t, f.lock.UpdateSettings(&settings))
	_, err = HandleProfilesSwitch(withKioskParams(t, env, models.SwitchProfileParams{
		ProfileID: kioskStr(kioskKidID), PIN: kioskStr(kioskKidPIN),
	}))
	require.ErrorIs(t, err, profiles.ErrKioskLocked)
}

// runAndCapture calls HandleRun and returns the token it queued.
func runAndCapture(t *testing.T, env requests.RequestEnv, script string) tokens.Token { //nolint:gocritic // test helper
	t.Helper()
	queue := make(chan tokens.Token, 1)
	env.TokenQueue = queue
	raw, err := json.Marshal(script)
	require.NoError(t, err)
	env.Params = raw

	done := make(chan struct{})
	var captured tokens.Token
	go func() {
		defer close(done)
		captured = <-queue
		captured.Completion.Complete(nil)
	}()
	_, err = HandleRun(env)
	require.NoError(t, err)
	<-done
	return captured
}

func TestRunFromACappedCallerIsBoundLikeACard(t *testing.T) {
	t.Parallel()
	f := newKioskFixture(t)

	free := runAndCapture(t, f.local(), "**launch.system:NES")
	assert.True(t, free.AllowedCommands.Unrestricted(), "kiosk off bounds nothing")

	f.lockOpen(t)
	for name, env := range map[string]requests.RequestEnv{
		"localhost":     f.local(),
		"paired member": f.paired("member"),
		"API key":       f.env(func(e *requests.RequestEnv) { e.APIKeyAuthenticated = true }),
	} {
		bound := runAndCapture(t, env, "**launch.system:NES")
		assert.False(t, bound.AllowedCommands.Unrestricted(), name)
		assert.True(t, bound.AllowedCommands.Allows(gozapscript.ZapScriptCmdLaunchSystem), name)
		assert.False(t, bound.AllowedCommands.Allows(gozapscript.ZapScriptCmdExecute), name)
		assert.False(t, bound.AllowedCommands.Allows(gozapscript.ZapScriptCmdInputKeyboard), name)
	}

	admin := runAndCapture(t, f.paired("admin"), "**launch.system:NES")
	assert.True(t, admin.AllowedCommands.Unrestricted(), "a paired admin's script is not card text")
}

func TestKioskNarrowSystems(t *testing.T) {
	t.Parallel()
	f := newKioskFixture(t)
	require.NoError(t, f.db.UserDB.ReplaceContentRule("", &database.ContentRule{
		Mode: database.ContentModeAllowed,
		Entries: []database.ContentRuleEntry{
			{Effect: database.ContentEffectAllow, Kind: database.ContentKindSystem, SystemID: "NES"},
			{Effect: database.ContentEffectAllow, Kind: database.ContentKindSystem, SystemID: "SNES"},
		},
	}))
	local := f.local()
	requested := []string{"Genesis", "NES"}

	got, err := kioskNarrowSystems(&local, &requested)
	require.NoError(t, err)
	assert.Equal(t, &requested, got, "kiosk off narrows nothing")

	f.lockOpen(t)

	got, err = kioskNarrowSystems(&local, &requested)
	require.NoError(t, err)
	assert.Equal(t, []string{"NES"}, *got)

	// Asking for nothing means everything, which becomes the allowed list
	// instead of staying an empty one that reads as everything.
	got, err = kioskNarrowSystems(&local, nil)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"NES", "SNES"}, *got)

	// Asking only for systems that are closed is refused outright.
	closed := []string{"Genesis"}
	_, err = kioskNarrowSystems(&local, &closed)
	require.ErrorIs(t, err, ErrKioskLocked)

	// A paired admin browses everything.
	admin := f.paired("admin")
	got, err = kioskNarrowSystems(&admin, &requested)
	require.NoError(t, err)
	assert.Equal(t, &requested, got)
}
