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

package updater

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Windows fails at the replacement, which is after the download and after the
// database snapshot, so the guard has to answer before any of that is spent.
func TestPreflightPlatform_RefusesWindows(t *testing.T) {
	t.Parallel()

	err := preflightPlatform("windows")
	require.ErrorIs(t, err, ErrPlatformUnsupported)
	assert.Contains(t, err.Error(), "Windows installer",
		"the message has to say what to do instead")
}

func TestPreflightPlatform_AllowsPlatformsThatReplaceTheirOwnBinary(t *testing.T) {
	t.Parallel()

	for _, goos := range []string{"linux", "darwin", "freebsd"} {
		assert.NoError(t, preflightPlatform(goos), goos)
	}
}
