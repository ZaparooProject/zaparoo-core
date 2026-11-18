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

package shared

import (
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/stretchr/testify/assert"
)

func TestLaunchExitReportsOncePerLaunch(t *testing.T) {
	t.Parallel()
	var e LaunchExit
	calls := 0
	e.Set(&platforms.LaunchOptions{MediaExited: func() { calls++ }})

	e.Capture()()
	e.Capture()()
	assert.Equal(t, 1, calls)

	e.Set(&platforms.LaunchOptions{MediaExited: func() { calls++ }})
	e.Capture()()
	assert.Equal(t, 2, calls)
}

func TestLaunchExitCaptureIgnoresNewerLaunch(t *testing.T) {
	t.Parallel()
	var e LaunchExit
	var first, second int
	e.Set(&platforms.LaunchOptions{MediaExited: func() { first++ }})
	report := e.Capture()
	e.Set(&platforms.LaunchOptions{MediaExited: func() { second++ }})

	report()
	assert.Equal(t, 1, first)
	assert.Zero(t, second, "a stop must not report a launch recorded after it captured")

	e.Capture()()
	assert.Equal(t, 1, second)
}

func TestLaunchExitWithoutCallback(t *testing.T) {
	t.Parallel()
	var e LaunchExit
	e.Capture()()
	e.Set(nil)
	e.Capture()()
}
