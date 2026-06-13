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
	"errors"
	"fmt"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
)

// cmdProfileSwitch handles **profile.switch:<switchId> — the card-scan path
// for changing the device's active profile. The switch ID is resolved here
// so an unknown card fails the script (and plays the fail sound); the
// actual activation is applied by the service layer from the returned
// CmdResult. No PIN is checked on this path: possession of the card is the
// authorization.
//
//nolint:gocritic // single-use parameter in command handler
func cmdProfileSwitch(_ platforms.Platform, env platforms.CmdEnv) (platforms.CmdResult, error) {
	if len(env.Cmd.Args) != 1 || env.Cmd.Args[0] == "" {
		return platforms.CmdResult{}, ErrArgCount
	}
	switchID := env.Cmd.Args[0]

	if env.Database == nil || env.Database.UserDB == nil {
		return platforms.CmdResult{}, errors.New("user database not available")
	}
	if _, err := env.Database.UserDB.GetProfileBySwitchID(switchID); err != nil {
		return platforms.CmdResult{}, fmt.Errorf("unknown profile switch ID: %w", err)
	}

	return platforms.CmdResult{
		ProfileSwitch: &platforms.ProfileSwitchRequest{SwitchID: switchID},
	}, nil
}

// cmdProfileClear handles **profile.clear — deactivates the active profile.
//
//nolint:gocritic // single-use parameter in command handler
func cmdProfileClear(_ platforms.Platform, env platforms.CmdEnv) (platforms.CmdResult, error) {
	if len(env.Cmd.Args) > 0 {
		return platforms.CmdResult{}, ErrArgCount
	}
	return platforms.CmdResult{
		ProfileSwitch: &platforms.ProfileSwitchRequest{Clear: true},
	}, nil
}
