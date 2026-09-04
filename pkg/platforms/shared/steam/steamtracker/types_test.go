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

package steamtracker

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLaunchOwnershipRejectsStaleLifecycle(t *testing.T) {
	t.Parallel()

	var ownership launchOwnership
	ownership.set(123, 10)
	ownership.set(123, 20)

	assert.False(t, ownership.clearIfMatches(123, 10))
	assert.False(t, ownership.clearIfMatches(456, 20))
	assert.True(t, ownership.clearIfMatches(123, 20))
	assert.False(t, ownership.clearIfMatches(123, 20))
}

func TestLaunchOwnershipMatches(t *testing.T) {
	t.Parallel()

	var ownership launchOwnership
	// Real launches never use zero: AppIDs are non-zero and lifecycle IDs
	// start at 1, so the zero value cannot collide with a live launch.
	assert.False(t, ownership.matches(123, 10), "an unset ownership owns no real launch")

	ownership.set(123, 10)
	assert.True(t, ownership.matches(123, 10))
	assert.False(t, ownership.matches(123, 9), "an earlier lifecycle no longer owns the launch")
	assert.False(t, ownership.matches(456, 10), "a different game does not own the launch")

	// Replacing the launch means the previous one stops matching, which is
	// what stops a slow process lookup acting on the game it replaced.
	ownership.set(123, 11)
	assert.False(t, ownership.matches(123, 10))
	assert.True(t, ownership.matches(123, 11))

	require.True(t, ownership.clearIfMatches(123, 11))
	assert.False(t, ownership.matches(123, 11), "a cleared ownership owns nothing")
}
