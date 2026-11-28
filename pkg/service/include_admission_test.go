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
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// writeIncludeScript writes a script file into the platform's scripts folder
// under the given name and removes it when the test ends. The scan behavior
// harness shares one data directory between tests, so names must be unique.
func (env *scanBehaviorEnv) writeIncludeScript(t *testing.T, name, text string) {
	t.Helper()
	dir := filepath.Join(helpers.DataDir(env.svc.Platform), config.ScriptsDir)
	require.NoError(t, os.MkdirAll(dir, 0o750))
	path := filepath.Join(dir, name+".txt")
	require.NoError(t, os.WriteFile(path, []byte(text), 0o600))
	t.Cleanup(func() { _ = os.Remove(path) })
}

func includeName(t *testing.T) string {
	t.Helper()
	return strings.NewReplacer("/", "-", " ", "-").Replace(t.Name())
}

// A launch inside an included file is held to require_for_launch exactly as
// one on the card is, though the card itself names no launch.
func TestScanBehavior_RequireProfile_BlocksIncludedLaunch(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, "tap", 0)
	env.cfg.SetProfilesRequireForLaunch(true)
	name := includeName(t)
	env.writeIncludeScript(t, name, "**launch:"+env.gamePath("game1.gba"))

	err := waitCompletion(t, env.scanWithCompletion(t, "card1", "**include:"+name))
	require.ErrorIs(t, err, state.ErrLaunchRequiresProfile)
	env.expectNoLaunch(t)
}

// An included file that switches profile before it launches satisfies
// require_for_launch, as the same commands on a card do.
func TestScanBehavior_RequireProfile_IncludedComboSwitchThenLaunch(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, "tap", 0)
	env.cfg.SetProfilesRequireForLaunch(true)
	profile := &database.Profile{ProfileID: "profile-1", Name: "Kid A", SwitchID: "corn-arm-truck"}
	env.userDB.On("GetProfileBySwitchID", "corn-arm-truck").Return(profile, nil)
	env.userDB.On("ActivateProfile", "profile-1", mock.AnythingOfType("int64")).Return(nil)
	name := includeName(t)
	env.writeIncludeScript(t, name, "**profile:corn-arm-truck||**launch:"+env.gamePath("game1.gba"))

	env.sendCommandScan("combo-card", "**include:"+name)
	env.waitForLaunch(t)
	env.waitForActiveProfile(t, "profile-1")
}

// A name that only an expression gives cannot be read before the run, so the
// launch the file adds is checked when the include runs.
func TestScanBehavior_RequireProfile_BlocksIncludedLaunchFromExpressionName(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, "tap", 0)
	env.cfg.SetProfilesRequireForLaunch(true)
	env.writeIncludeScript(t, env.svc.Platform.ID(), "**launch:"+env.gamePath("game1.gba"))

	err := waitCompletion(t, env.scanWithCompletion(t, "card1", "**include:[[platform]]"))
	require.ErrorIs(t, err, state.ErrLaunchRequiresProfile)
	env.expectNoLaunch(t)
}

// With the launch guard on, an include that would replace the running game
// waits for confirmation like a launch card does.
func TestLaunchGuard_StagesIncludedLaunch(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, "tap", 0)
	first := env.gamePath("first.rom")
	require.NoError(t, waitCompletion(t, env.sendAPIToken(t, first)))
	env.waitForLaunch(t)
	env.cfg.SetLaunchGuard(true)
	name := includeName(t)
	env.writeIncludeScript(t, name, "**launch:"+env.gamePath("other.rom"))

	env.sendCommandScan("other", "**include:"+name)
	env.waitForGuard(t)
	env.expectNoLaunch(t)
	require.Equal(t, first, env.st.ActiveMedia().Path)
}

// A profile switch that a nested include runs before the launch satisfies
// require_for_launch, as it does when the switch is in the included file.
func TestScanBehavior_RequireProfile_NestedIncludeSwitchThenLaunch(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, "tap", 0)
	env.cfg.SetProfilesRequireForLaunch(true)
	profile := &database.Profile{ProfileID: "profile-1", Name: "Kid A", SwitchID: "pear-lid-cloud"}
	env.userDB.On("GetProfileBySwitchID", "pear-lid-cloud").Return(profile, nil)
	env.userDB.On("ActivateProfile", "profile-1", mock.AnythingOfType("int64")).Return(nil)
	name := includeName(t)
	env.writeIncludeScript(t, name+"-profile", "**profile:pear-lid-cloud")
	env.writeIncludeScript(t, name, "**include:"+name+"-profile||**launch:"+env.gamePath("game1.gba"))

	require.NoError(t, waitCompletion(t, env.scanWithCompletion(t, "combo-card", "**include:"+name)))
	env.waitForLaunch(t)
	env.waitForActiveProfile(t, "profile-1")
}

// An on_remove hook that reaches a delay through an include runs off the
// reader loop, so putting the card back cancels it as it does a direct delay.
func TestScanBehavior_HoldDelayedOnRemove_IncludedDelayReinsertCancels(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, config.ScanModeHold, 0)
	name := includeName(t)
	env.writeIncludeScript(t, name, "**delay:10s||**input.keyboard:{escape}")
	require.NoError(t, env.cfg.LoadTOML(`[readers.scan]
mode = "hold"
on_remove = '**include:`+name+`'`))

	env.sendGameScan("game1", env.gamePath("game.rom"))
	env.waitForLaunch(t)
	env.waitForSoftwareToken(t)

	env.sendRemoval()
	env.sendGameScan("game1", env.gamePath("game.rom"))
	env.waitForActiveCard(t, "game1")

	env.expectNoLaunch(t)
	env.expectNoStop(t)
	select {
	case key := <-env.keyboardCh:
		t.Fatalf("unexpected delayed on_remove keyboard command: %s", key)
	case <-time.After(noEventWait):
	}
}

// An include with a when condition may not run, so a profile switch in its
// file does not let a launch after it past require_for_launch.
func TestScanBehavior_RequireProfile_ConditionalIncludeSwitchDoesNotSatisfy(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, "tap", 0)
	env.cfg.SetProfilesRequireForLaunch(true)
	name := includeName(t)
	env.writeIncludeScript(t, name, "**profile:corn-arm-truck")

	err := waitCompletion(t, env.scanWithCompletion(t, "combo-card",
		"**include:"+name+"?when=false||**launch:"+env.gamePath("game1.gba")))
	require.ErrorIs(t, err, state.ErrLaunchRequiresProfile)
	env.expectNoLaunch(t)
}

// With the launch guard on, an include whose file cannot be known before the
// run is held for confirmation, as it might launch.
func TestLaunchGuard_StagesIncludeFromExpressionName(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, "tap", 0)
	first := env.gamePath("first.rom")
	require.NoError(t, waitCompletion(t, env.sendAPIToken(t, first)))
	env.waitForLaunch(t)
	env.cfg.SetLaunchGuard(true)

	env.sendCommandScan("other", "**include:[[platform]]")
	env.waitForGuard(t)
	require.Equal(t, first, env.st.ActiveMedia().Path)
}
