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
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript"
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
		{name: "no playlist active", err: zapscript.ErrNoPlaylistActive, expected: true},
		{name: "launch in progress", err: state.ErrLaunchInProgress, expected: true},
		{name: "unknown system", err: systemdefs.ErrUnknownSystem, expected: true},
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
		{name: "generic error", err: errors.New("database disk image is malformed"), expected: false},
		{name: "nil error", err: nil, expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, isExpectedLaunchError(tt.err))
		})
	}
}
