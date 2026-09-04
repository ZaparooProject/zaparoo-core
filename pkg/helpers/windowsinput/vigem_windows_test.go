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

//go:build windows

package windowsinput

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows"
)

// A machine without the driver is the ordinary case, and the message reaches
// the user through the API, so it must not trail SetupAPI's "No more data is
// available" behind an otherwise clear sentence.
func TestDriverMissingKeepsTheExpectedCaseClean(t *testing.T) {
	t.Parallel()

	err := driverMissing(windows.ERROR_NO_MORE_ITEMS)
	require.ErrorIs(t, err, ErrDriverMissing)
	assert.Equal(t, ErrDriverMissing.Error(), err.Error())
}

// Anything else is unexpected, and keeps its cause.
func TestDriverMissingKeepsAnUnexpectedCause(t *testing.T) {
	t.Parallel()

	err := driverMissing(windows.ERROR_ACCESS_DENIED)
	require.ErrorIs(t, err, ErrDriverMissing)
	require.ErrorIs(t, err, windows.ERROR_ACCESS_DENIED)
}
