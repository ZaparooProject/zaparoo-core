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
	"fmt"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/virtualpath"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playtime"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript/titles"
	"github.com/stretchr/testify/assert"
)

// TestIsExpectedLaunchError verifies that expected user/operational launch
// failures are classified for Warn-level logging (kept out of Sentry) while
// genuine errors are not, including when the sentinels are wrapped.
func TestIsExpectedLaunchError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		err      error
		name     string
		expected bool
	}{
		{name: "file not found", err: zapscript.ErrFileNotFound, expected: true},
		{
			name: "wrapped no title match",
			err:  fmt.Errorf("error resolving title SNES/Zelda: %w", titles.ErrNoMatch), expected: true,
		},
		{name: "low confidence title match", err: titles.ErrLowConfidence, expected: true},
		{name: "no playlist active", err: zapscript.ErrNoPlaylistActive, expected: true},
		{name: "launch in progress", err: state.ErrLaunchInProgress, expected: true},
		{name: "script busy", err: platforms.ErrScriptAlreadyRunning, expected: true},
		{
			name: "wrapped script busy", err: fmt.Errorf("forward: %w", platforms.ErrScriptAlreadyRunning),
			expected: true,
		},
		{name: "untyped similar error", err: errors.New("a script is already running"), expected: false},
		{name: "unknown system", err: systemdefs.ErrUnknownSystem, expected: true},
		{name: "run zapscript disabled", err: state.ErrRunZapScriptDisabled, expected: true},
		{name: "invalid script", err: zapscript.ErrInvalidScript, expected: true},
		{name: "invalid advanced arguments", err: zapscript.ErrInvalidArguments, expected: true},
		{name: "unknown command", err: zapscript.ErrUnknownCommand, expected: true},
		{
			name: "wrapped unsupported control action",
			err:  fmt.Errorf("wrapped: %w", zapscript.ErrUnsupportedControlAction), expected: true,
		},
		{name: "untyped action error", err: errors.New("not supported by launcher"), expected: false},
		{name: "command blocked", err: zapscript.ErrCommandBlocked, expected: true},
		{
			name: "wrapped command outside token bound",
			err:  fmt.Errorf("%w: execute", zapscript.ErrCommandNotPermitted), expected: true,
		},
		{name: "hook blocked launch", err: state.ErrLaunchBlockedByHook, expected: true},
		{name: "launch panicked", err: errLaunchPanicked, expected: false},
		{
			name:     "wrapped no playlist active",
			err:      fmt.Errorf("failed to run zapscript command: %w", zapscript.ErrNoPlaylistActive),
			expected: true,
		},
		{
			name:     "wrapped unknown system",
			err:      fmt.Errorf("%w: neo", systemdefs.ErrUnknownSystem),
			expected: true,
		},
		{
			name:     "wrong argument count",
			err:      fmt.Errorf("failed to run zapscript command: %w", zapscript.ErrArgCount),
			expected: true,
		},
		{
			name:     "missing required argument",
			err:      fmt.Errorf("failed to run zapscript command: %w", zapscript.ErrRequiredArgs),
			expected: true,
		},
		{
			name: "invalid launch format",
			err:  fmt.Errorf("%w: sfiii2n", zapscript.ErrInvalidLaunchFormat), expected: true,
		},
		{
			name: "no launcher for the file",
			err:  fmt.Errorf("%w for: game.xyz", helpers.ErrNoLauncher), expected: true,
		},
		{
			name: "launcher unavailable",
			err:  fmt.Errorf("failed to send request: %w", platforms.ErrLauncherUnavailable), expected: true,
		},
		{
			name: "scheme mismatch",
			err:  fmt.Errorf("%w: expected launchbox, got steam", virtualpath.ErrSchemeMismatch), expected: true,
		},
		{name: "execute timeout", err: zapscript.ErrExecuteTimeout, expected: true},
		{name: "execute non-zero exit", err: zapscript.ErrExecuteExitStatus, expected: true},
		{name: "no control capabilities", err: zapscript.ErrNoControlCapabilities, expected: true},
		{name: "no history", err: zapscript.ErrNoHistory, expected: true},
		{name: "playtime limit", err: playtime.ErrLimitReached, expected: true},
		{name: "generic error", err: errors.New("database disk image is malformed"), expected: false},
		{name: "nil error", err: nil, expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, zapscript.IsExpectedRunError(tt.err))
		})
	}
}
