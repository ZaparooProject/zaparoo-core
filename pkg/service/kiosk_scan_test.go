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

package service

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/kiosk"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/profiles"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript"
	"github.com/jonboulle/clockwork"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// kioskRule is the content rule the scan tests give every profile.
type kioskRule struct {
	mode    string
	entries []database.ContentRuleEntry
}

// lockScanEnv attaches a kiosk lock to a scan behavior environment and
// locks it. The lock sits on the environment's own mocked user database, so
// what it enforces is decided by the rule passed here.
func lockScanEnv(t *testing.T, env *scanBehaviorEnv, rule kioskRule, mutate func(*kiosk.Settings)) *kiosk.Service {
	t.Helper()
	env.userDB.On("SetDeviceState", database.DeviceStateKeyKiosk, mock.Anything).Return(nil).Maybe()
	env.userDB.On("ListProfiles").Return([]database.Profile{
		{ProfileID: "admin-1", Name: "Parent", Role: profiles.ProfileRoleAdmin, SwitchID: "parent-card", PINHash: "x"},
	}, nil).Maybe()
	env.userDB.On("GetContentRule", mock.Anything).Return(
		database.ContentRule{Mode: rule.mode, Entries: rule.entries}, nil).Maybe()
	env.userDB.On("GetMediaUserData", mock.Anything, mock.Anything).
		Return(database.MediaUserData{}, false, nil).Maybe()
	env.userDB.On("FindDeckItemsByAnchor", mock.Anything, mock.Anything).
		Return(map[string][]int64{}, nil).Maybe()
	// A launch that names its system looks the path up in the index, which
	// here holds nothing.
	env.mediaDB.On("FindSystemBySystemID", mock.Anything).
		Return(database.System{}, errors.New("system is not indexed")).Maybe()

	lock := kiosk.NewService(env.svc.DB, clockwork.NewFakeClock())
	env.st.SetKiosk(lock)
	settings, ok := kiosk.PresetSettings(kiosk.PresetFamily)
	require.True(t, ok)
	if mutate != nil {
		mutate(&settings)
	}
	require.NoError(t, lock.Enable(&settings, kiosk.EnableOptions{ConfirmEmpty: true}))
	return lock
}

var openRule = kioskRule{mode: database.ContentModeAll}

// gbaSystem is the system the scan tests launch under. The harness platform
// has no launchers to resolve a path with, so the cards name it themselves.
const gbaSystem = "GBA"

func (env *scanBehaviorEnv) gbaLaunch(name string) string {
	return "**launch:" + env.gamePath(name) + "?system=" + gbaSystem
}

func allowGBA() kioskRule {
	return kioskRule{mode: database.ContentModeAllowed, entries: []database.ContentRuleEntry{
		{Effect: database.ContentEffectAllow, Kind: database.ContentKindSystem, SystemID: gbaSystem},
	}}
}

func (env *scanBehaviorEnv) expectNoKeyboard(t *testing.T) {
	t.Helper()
	select {
	case key := <-env.keyboardCh:
		t.Fatalf("unexpected KeyboardPress: %s", key)
	case <-time.After(noEventWait):
	}
}

// waitForRefusal waits for a kiosk.launch.refused notification.
func (env *scanBehaviorEnv) waitForRefusal(t *testing.T) {
	t.Helper()
	deadline := time.After(behaviorTimeout)
	for {
		select {
		case notif := <-env.notifCh:
			if notif.Method == models.NotificationKioskLaunchRefused {
				return
			}
		case <-deadline:
			t.Fatal("timed out waiting for kiosk.launch.refused")
		}
	}
}

func TestKioskScan_AllowedGameLaunches(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, "tap", 0)
	lockScanEnv(t, env, allowGBA(), nil)

	env.sendCommandScan("card1", env.gbaLaunch("game1.gba"))
	env.waitForLaunch(t)
}

func TestKioskScan_DisallowedGameIsRefused(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, "tap", 0)
	// Allowed-only with nothing allowed: every launch is outside the rule.
	lockScanEnv(t, env, kioskRule{mode: database.ContentModeAllowed}, nil)

	completion := env.scanWithCompletion(t, "card1", "**launch:"+env.gamePath("game1.gba"))
	require.ErrorIs(t, waitCompletion(t, completion), kiosk.ErrLaunchRefused)
	env.expectNoLaunch(t)
}

func TestKioskScan_RefusalIsNotified(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, "tap", 0)
	lockScanEnv(t, env, kioskRule{mode: database.ContentModeAllowed}, nil)

	env.sendGameScan("card1", env.gamePath("game1.gba"))
	env.waitForRefusal(t)
}

func TestKioskScan_DeniedSystemIsRefusedUnderAnOpenRule(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, "tap", 0)
	lockScanEnv(t, env, kioskRule{mode: database.ContentModeAll, entries: []database.ContentRuleEntry{
		{Effect: database.ContentEffectDeny, Kind: database.ContentKindSystem, SystemID: gbaSystem},
	}}, nil)

	completion := env.scanWithCompletion(t, "card1", env.gbaLaunch("game1.gba"))
	require.ErrorIs(t, waitCompletion(t, completion), kiosk.ErrLaunchRefused)
	env.expectNoLaunch(t)
}

func TestKioskScan_UnlockLiftsTheContentRule(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, "tap", 0)
	lock := lockScanEnv(t, env, kioskRule{mode: database.ContentModeAllowed}, nil)
	require.NoError(t, lock.Unlock(time.Minute))

	env.sendGameScan("card1", env.gamePath("game1.gba"))
	env.waitForLaunch(t)
}

func TestKioskScan_APILaunchAnswersToTheSameRule(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, "tap", 0)
	lockScanEnv(t, env, kioskRule{mode: database.ContentModeAllowed}, nil)

	// An unbounded API token is what a paired admin's run produces. The
	// content rule is about what launches, so it is refused all the same.
	completion := env.sendAPIToken(t, "**launch:"+env.gamePath("game1.gba"))
	require.ErrorIs(t, waitCompletion(t, completion), kiosk.ErrLaunchRefused)
	env.expectNoLaunch(t)
}

func TestKioskScan_CardCommandsOutsideThePolicyAreRefused(t *testing.T) {
	t.Parallel()
	for _, script := range []string{
		"**input.keyboard:a",
		"**execute:echo hi",
		"**mister.script:update_all.sh",
		"**profile.clear",
	} {
		t.Run(script, func(t *testing.T) {
			t.Parallel()
			env := setupScanBehavior(t, "tap", 0)
			lockScanEnv(t, env, openRule, nil)

			completion := env.scanWithCompletion(t, "card1", script)
			require.ErrorIs(t, waitCompletion(t, completion), zapscript.ErrCommandNotPermitted)
			env.expectNoKeyboard(t)
		})
	}
}

func TestKioskScan_SameCardWorksWhenNotLocked(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, "tap", 0)
	lock := lockScanEnv(t, env, openRule, nil)
	require.NoError(t, lock.Unlock(time.Minute))

	env.sendCommandScan("card1", "**input.keyboard:a")
	assert.Equal(t, "a", env.waitForKeyboard(t))
}

func TestKioskScan_RefusedCommandStopsTheRestOfTheCard(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, "tap", 0)
	lockScanEnv(t, env, openRule, nil)

	// A card cannot smuggle an allowed launch in behind a refused command,
	// or a refused command in behind an allowed launch.
	completion := env.scanWithCompletion(t, "card1",
		"**input.keyboard:a||**launch:"+env.gamePath("game1.gba"))
	require.ErrorIs(t, waitCompletion(t, completion), zapscript.ErrCommandNotPermitted)
	env.expectNoLaunch(t)
	env.expectNoKeyboard(t)
}

func TestKioskScan_MappedCardRunsItsOwnersScript(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, "tap", 0)

	// The mapping is stored on the device, where only its owner could have
	// put it, so the script runs as written whatever the card says.
	mappingsDir := filepath.Join(t.TempDir(), "mappings")
	require.NoError(t, env.fs.MkdirAll(mappingsDir, 0o750))
	require.NoError(t, afero.WriteFile(env.fs, filepath.Join(mappingsDir, "kiosk.toml"), []byte(`
[[mappings.entry]]
token_key = "id"
match_pattern = "ownercard"
zapscript = "**input.keyboard:b"
`), 0o600))
	require.NoError(t, env.cfg.LoadMappings(mappingsDir))
	require.Len(t, env.cfg.Mappings(), 1)

	lockScanEnv(t, env, openRule, nil)

	env.sendCommandScan("ownercard", "**launch:anything")
	assert.Equal(t, "b", env.waitForKeyboard(t))
}

func TestKioskScan_AdminSwitchCardIsNotAKey(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, "tap", 0)
	env.userDB.On("GetProfileBySwitchID", "parent-card").Return(&database.Profile{
		ProfileID: "admin-1", Name: "Parent", Role: profiles.ProfileRoleAdmin, SwitchID: "parent-card",
	}, nil)
	env.userDB.On("GetProfileBySwitchID", "kid-card").Return(&database.Profile{
		ProfileID: "kid-1", Name: "Kid", Role: profiles.ProfileRoleMember, SwitchID: "kid-card",
	}, nil)
	env.userDB.On("ActivateProfile", "kid-1", mock.AnythingOfType("int64")).Return(nil)
	lockScanEnv(t, env, openRule, nil)

	completion := env.scanWithCompletion(t, "parent", "**profile:parent-card")
	require.ErrorIs(t, waitCompletion(t, completion), profiles.ErrKioskLocked)
	assert.Nil(t, env.st.ActiveProfile())

	// A member's card still switches.
	env.sendCommandScan("kid", "**profile:kid-card")
	env.waitForActiveProfile(t, "kid-1")
}

func TestKioskScan_FixedProfileRefusesEverySwitchCard(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, "tap", 0)
	lockScanEnv(t, env, openRule, func(s *kiosk.Settings) { s.ProfileSwitching = false })

	completion := env.scanWithCompletion(t, "kid", "**profile:kid-card")
	require.ErrorIs(t, waitCompletion(t, completion), zapscript.ErrCommandNotPermitted)
	assert.Nil(t, env.st.ActiveProfile())
}

func TestBoundScannedToken(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, "tap", 0)

	bound := func(source string, hasMapping bool) bool {
		token := tokens.Token{Source: source}
		boundScannedToken(env.svc, &token, hasMapping)
		return !token.AllowedCommands.Unrestricted()
	}

	assert.False(t, bound(tokens.SourceReader, false), "no lock attached")
	lock := lockScanEnv(t, env, openRule, nil)

	assert.True(t, bound(tokens.SourceReader, false))
	assert.True(t, bound(tokens.SourceGMC, false))
	assert.True(t, bound(tokens.SourceRemote, false))
	assert.False(t, bound(tokens.SourceReader, true), "a mapped scan runs the owner's script")
	assert.False(t, bound(tokens.SourceHook, false), "hooks are configured on the device")
	assert.False(t, bound(tokens.SourceControl, false))
	assert.False(t, bound(tokens.SourcePlaylist, false), "playlist items inherit their bound instead")

	require.NoError(t, lock.Unlock(time.Minute))
	assert.False(t, bound(tokens.SourceReader, false), "temporarily unlocked")
}

func TestAdmitKioskMediaInstall(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, "tap", 0)
	card := tokens.Token{Source: tokens.SourceReader}
	owner := tokens.Token{Source: tokens.SourceAPI}

	require.NoError(t, admitKioskMediaInstall(env.svc, &card), "no lock attached")

	lock := lockScanEnv(t, env, openRule, nil)
	lock.BoundToken(&card)

	err := admitKioskMediaInstall(env.svc, &card)
	require.ErrorIs(t, err, kiosk.ErrLaunchRefused)
	var refused *kiosk.RefusedError
	require.ErrorAs(t, err, &refused)
	assert.Equal(t, kiosk.ReasonDownload, refused.Reason)

	require.NoError(t, admitKioskMediaInstall(env.svc, &owner), "an unbounded token is the owner's")

	require.NoError(t, lock.Unlock(time.Minute))
	require.NoError(t, admitKioskMediaInstall(env.svc, &card), "temporarily unlocked")
}

func TestKioskSystemPermitted(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, "tap", 0)
	assert.Nil(t, kioskSystemPermitted(env.svc), "no lock means no filter")

	lockScanEnv(t, env, allowGBA(), nil)
	permitted := kioskSystemPermitted(env.svc)
	require.NotNil(t, permitted)
	assert.True(t, permitted(gbaSystem))
	assert.True(t, permitted("gba"))
	assert.False(t, permitted("NES"))
}

func TestKioskLaunchSystem(t *testing.T) {
	t.Parallel()
	genesis := func(string) (platforms.Launcher, error) {
		return platforms.Launcher{ID: "Genesis", SystemID: "Genesis"}, nil
	}
	none := func(string) (platforms.Launcher, error) {
		return platforms.Launcher{}, helpers.ErrNoLauncher
	}
	sonic := "/games/Genesis/Sonic.md"

	// A card that calls a Genesis game an NES game is judged as Genesis,
	// because the Genesis launcher is what would run it.
	assert.Equal(t, "Genesis", kioskLaunchSystem(
		platforms.ResolvedLaunch{Path: sonic, SystemID: "NES"}, genesis))

	// A launcher already chosen is what runs.
	assert.Equal(t, "SNES", kioskLaunchSystem(platforms.ResolvedLaunch{
		Path: sonic, SystemID: "NES", Launcher: &platforms.Launcher{ID: "SNES", SystemID: "SNES"},
	}, genesis))

	// Nothing can launch a path no launcher takes, so the card's own
	// system is all there is to report.
	assert.Equal(t, "NES", kioskLaunchSystem(platforms.ResolvedLaunch{Path: sonic, SystemID: "NES"}, none))
	assert.Empty(t, kioskLaunchSystem(platforms.ResolvedLaunch{Path: sonic}, none))

	// A system launch has no path to look up.
	assert.Equal(t, "NES", kioskLaunchSystem(platforms.ResolvedLaunch{SystemID: "NES"}, genesis))
}
