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

package zapscript

import (
	"testing"

	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/userdb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func profileCmdEnv(mockDB *helpers.MockUserDBI, name string, args []string) platforms.CmdEnv {
	return platforms.CmdEnv{
		Cmd: zapscript.Command{
			Name: name,
			Args: args,
		},
		Database: &database.Database{UserDB: mockDB, MediaDB: nil},
	}
}

func TestCmdProfileSwitch_Success(t *testing.T) {
	t.Parallel()

	mockDB := helpers.NewMockUserDBI()
	mockDB.On("GetProfileBySwitchID", "corn-arm-truck").
		Return(&database.Profile{ProfileID: "p1", Name: "Kid A", SwitchID: "corn-arm-truck"}, nil)

	result, err := cmdProfile(nil, profileCmdEnv(mockDB, "profile", []string{"corn-arm-truck"}))
	require.NoError(t, err)
	require.NotNil(t, result.ProfileSwitch)
	assert.Equal(t, "corn-arm-truck", result.ProfileSwitch.SwitchID)
	assert.False(t, result.ProfileSwitch.Clear)
	assert.False(t, result.MediaChanged, "profile switch must not count as a media change")
}

func TestCmdProfileSwitch_UnknownSwitchID(t *testing.T) {
	t.Parallel()

	mockDB := helpers.NewMockUserDBI()
	mockDB.On("GetProfileBySwitchID", "no-such-card").Return(nil, userdb.ErrProfileNotFound)

	_, err := cmdProfile(nil, profileCmdEnv(mockDB, "profile", []string{"no-such-card"}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown profile switch ID")
}

func TestCmdProfileSwitch_ArgValidation(t *testing.T) {
	t.Parallel()

	mockDB := helpers.NewMockUserDBI()

	_, err := cmdProfile(nil, profileCmdEnv(mockDB, "profile", nil))
	require.ErrorIs(t, err, ErrArgCount)

	_, err = cmdProfile(nil, profileCmdEnv(mockDB, "profile", []string{""}))
	require.ErrorIs(t, err, ErrArgCount)

	_, err = cmdProfile(nil, profileCmdEnv(mockDB, "profile", []string{"a", "b"}))
	require.ErrorIs(t, err, ErrArgCount)
}

func TestCmdProfileClear(t *testing.T) {
	t.Parallel()

	mockDB := helpers.NewMockUserDBI()

	result, err := cmdProfileClear(nil, profileCmdEnv(mockDB, "profile.clear", nil))
	require.NoError(t, err)
	require.NotNil(t, result.ProfileSwitch)
	assert.True(t, result.ProfileSwitch.Clear)

	_, err = cmdProfileClear(nil, profileCmdEnv(mockDB, "profile.clear", []string{"extra"}))
	require.ErrorIs(t, err, ErrArgCount)
}

func TestProfileCommands_NotMediaLaunching(t *testing.T) {
	t.Parallel()

	// Profile switching must never be blocked by playtime limits — a kid
	// who has hit their limit can still hand the device to a parent card.
	assert.False(t, IsMediaLaunchingCommand(zapscript.ZapScriptCmdProfile))
	assert.False(t, IsMediaLaunchingCommand(zapscript.ZapScriptCmdProfileClear))
	assert.False(t, IsMediaDisruptingCommand(zapscript.ZapScriptCmdProfile))
	assert.False(t, IsMediaDisruptingCommand(zapscript.ZapScriptCmdProfileClear))
}
