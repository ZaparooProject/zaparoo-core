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

package state

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRemoteStatus(t *testing.T) {
	t.Parallel()

	st, notifications := NewState(nil, "")
	t.Cleanup(func() {
		for len(notifications) > 0 {
			<-notifications
		}
	})

	initial := st.RemoteStatus()
	assert.Empty(t, initial.State, "nothing reported yet")
	assert.True(t, initial.LastContactAt.IsZero())

	st.SetRemoteStatus(RemoteStateWaiting, "")
	waiting := st.RemoteStatus()
	assert.Equal(t, RemoteStateWaiting, waiting.State)
	assert.Empty(t, waiting.LastErrorCode)
	require.False(t, waiting.LastContactAt.IsZero(), "waiting counts as contact")

	st.SetRemoteStatus(RemoteStateError, "unreachable")
	failed := st.RemoteStatus()
	assert.Equal(t, RemoteStateError, failed.State)
	assert.Equal(t, "unreachable", failed.LastErrorCode)
	assert.Equal(t, waiting.LastContactAt, failed.LastContactAt, "a failure keeps the last successful contact")
	assert.False(t, failed.UpdatedAt.Before(waiting.UpdatedAt))
}
