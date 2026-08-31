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
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestScanBehavior_RequireProfile_BlocksLaunchWithoutProfile(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, "tap", 0)

	env.cfg.SetProfilesRequireForLaunch(true)

	env.sendGameScan("card1", env.gamePath("game1.gba"))
	env.expectNoLaunch(t)
}

func TestScanBehavior_RequireProfile_AllowsLaunchWithActiveProfile(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, "tap", 0)

	env.cfg.SetProfilesRequireForLaunch(true)
	env.st.SetActiveProfile(&models.ActiveProfile{ProfileID: "profile-1", Name: "Dad"})

	env.sendGameScan("card1", env.gamePath("game1.gba"))
	env.waitForLaunch(t)
}

func TestScanBehavior_RequireProfile_OffByDefault(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, "tap", 0)

	// No profile active and require_for_launch unset: launches work exactly
	// as they did before profiles existed.
	env.sendGameScan("card1", env.gamePath("game1.gba"))
	env.waitForLaunch(t)
}

// TestScanBehavior_ProfileSwitchCard covers the signature card interaction:
// scanning a **profile token activates the profile with no PIN
// check, and **profile.clear deactivates it.
func TestScanBehavior_ProfileSwitchCard(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, "tap", 0)

	profile := &database.Profile{
		ProfileID: "profile-1",
		Name:      "Kid A",
		SwitchID:  "corn-arm-truck",
		PINHash:   "pbkdf2-sha256$1$AAAA$AAAA", // PIN set, but card scans bypass it
	}
	env.userDB.On("GetProfileBySwitchID", "corn-arm-truck").Return(profile, nil)
	env.userDB.On("ActivateProfile", "profile-1", mock.AnythingOfType("int64")).Return(nil)
	env.userDB.On("DeleteDeviceState", database.DeviceStateKeyActiveProfile).Return(nil)

	env.sendCommandScan("switch-card", "**profile:corn-arm-truck")
	env.waitForActiveProfile(t, "profile-1")

	env.sendCommandScan("clear-card", "**profile.clear")
	env.waitForNoActiveProfile(t)
}

// TestScanBehavior_RequireProfile_ComboCardSwitchThenLaunch covers a card
// carrying both a profile switch and a game launch: the gate must let it
// through because the switch activates a profile before the launch runs.
func TestScanBehavior_RequireProfile_ComboCardSwitchThenLaunch(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, "tap", 0)

	env.cfg.SetProfilesRequireForLaunch(true)

	profile := &database.Profile{
		ProfileID: "profile-1",
		Name:      "Kid A",
		SwitchID:  "corn-arm-truck",
	}
	env.userDB.On("GetProfileBySwitchID", "corn-arm-truck").Return(profile, nil)
	env.userDB.On("ActivateProfile", "profile-1", mock.AnythingOfType("int64")).Return(nil)

	env.sendCommandScan("combo-card",
		"**profile:corn-arm-truck||**launch:"+env.gamePath("game1.gba"))
	env.waitForLaunch(t)
	env.waitForActiveProfile(t, "profile-1")
}

// TestScanBehavior_RequireProfile_LaunchBeforeSwitchStillBlocked pins the
// gate's ordering rule: a profile switch AFTER the launch command does not
// satisfy require_for_launch.
func TestScanBehavior_RequireProfile_LaunchBeforeSwitchStillBlocked(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, "tap", 0)

	env.cfg.SetProfilesRequireForLaunch(true)

	env.sendCommandScan("backwards-combo-card",
		"**launch:"+env.gamePath("game1.gba")+"||**profile:corn-arm-truck")
	env.expectNoLaunch(t)
}

func (env *scanBehaviorEnv) waitForActiveProfile(t *testing.T, profileID string) {
	t.Helper()
	deadline := time.After(behaviorTimeout)
	for {
		if active := env.st.ActiveProfile(); active != nil && active.ProfileID == profileID {
			return
		}
		select {
		case <-deadline:
			require.FailNow(t, "timed out waiting for active profile", "profileID=%s", profileID)
		case <-time.After(time.Millisecond):
		}
	}
}

func (env *scanBehaviorEnv) waitForNoActiveProfile(t *testing.T) {
	t.Helper()
	deadline := time.After(behaviorTimeout)
	for {
		if env.st.ActiveProfile() == nil {
			return
		}
		select {
		case <-deadline:
			require.FailNow(t, "timed out waiting for profile deactivation")
		case <-time.After(time.Millisecond):
		}
	}
}
