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
	"strings"
	"time"

	gozapscript "github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
)

var (
	// ErrExtendSourceNotReader is returned when an extension is attempted
	// from anywhere but a physical reader.
	ErrExtendSourceNotReader = errors.New("playtime extensions can only be granted by scanning a card")
	// ErrExtendNotAlone is returned when an extension shares a script with
	// other commands.
	ErrExtendNotAlone = errors.New("playtime.extend must be the only command on a token")
	// ErrExtendProfileMissing is returned when the authorizing profile
	// argument is absent.
	ErrExtendProfileMissing = errors.New("playtime.extend requires a profile argument")
)

// cmdPlaytimeExtend handles **playtime.extend:<amount>?profile=<switchId>.
//
// The amount is a Go duration, or "today" to waive the session limit for the
// rest of the local day. The profile argument is the switch ID of the
// authorizing profile — the same bearer credential the profile command takes
// — and names who permits the grant, never who receives it. The recipient is
// always whoever playtime is being enforced against when the card is
// scanned, so a card cannot be aimed at a particular person.
//
// This layer resolves nothing and grants nothing. It validates the shape of
// the request and hands the service layer an intent, which verifies the
// credential belongs to an administrator before applying it.
//
//nolint:gocritic // single-use parameter in command handler
func cmdPlaytimeExtend(pl platforms.Platform, env platforms.CmdEnv) (platforms.CmdResult, error) {
	// A grant weakens somebody's limits, so it has to come from physical
	// possession of a card. Allowing the API, hooks, playlists or remote
	// sources here would turn any path that can run ZapScript into a way
	// around a limit.
	if env.Source != tokens.SourceReader {
		return platforms.CmdResult{}, fmt.Errorf("%w: source %q", ErrExtendSourceNotReader, env.Source)
	}

	// Requiring the token to carry nothing else stops a combo card from
	// ordering an extension ahead of a launch to slip past the pre-launch
	// limit check.
	if env.TotalCommands != 1 {
		return platforms.CmdResult{}, ErrExtendNotAlone
	}

	if len(env.Cmd.Args) != 1 || env.Cmd.Args[0] == "" {
		return platforms.CmdResult{}, ErrArgCount
	}

	var args gozapscript.PlaytimeExtendArgs
	if err := ParseAdvArgs(pl, &env, &args); err != nil {
		return platforms.CmdResult{}, err
	}
	if args.Profile == "" {
		return platforms.CmdResult{}, ErrExtendProfileMissing
	}

	req := platforms.PlaytimeExtensionRequest{
		AuthorizerSwitchID: args.Profile,
	}

	amount := env.Cmd.Args[0]
	if strings.EqualFold(amount, gozapscript.PlaytimeExtendToday) {
		req.Mode = models.PlaytimeExtendModeToday
	} else {
		// A Go duration always ends in a unit, so it can never be confused
		// with the today keyword.
		parsed, err := time.ParseDuration(amount)
		if err != nil {
			return platforms.CmdResult{}, fmt.Errorf(
				"invalid extension amount %q, expected a duration like 15m or %q: %w",
				amount, gozapscript.PlaytimeExtendToday, err,
			)
		}
		req.Mode = models.PlaytimeExtendModeDuration
		req.Duration = parsed
	}

	return platforms.CmdResult{PlaytimeExtension: &req}, nil
}
