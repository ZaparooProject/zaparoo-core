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

package audio

import (
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mediaslot"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewLongformPlaybackManager(t *testing.T) {
	t.Parallel()
	m := NewLongformPlaybackManager()
	require.NotNil(t, m)
	assert.NotNil(t, m.drainCallbacks)
	assert.Nil(t, m.primary)
	assert.Nil(t, m.background)
}

// TestLongformPlaybackManager_InvalidSlot verifies every mutating method returns
// an error when given a slot name that is neither primary nor background.
func TestLongformPlaybackManager_InvalidSlot(t *testing.T) {
	t.Parallel()
	m := NewLongformPlaybackManager()

	require.Error(t, m.Stop("badslot"), "Stop should error on invalid slot")
	require.Error(t, m.Pause("badslot"), "Pause should error on invalid slot")
	require.Error(t, m.Resume("badslot"), "Resume should error on invalid slot")
	require.Error(t, m.TogglePause("badslot"), "TogglePause should error on invalid slot")
	require.Error(t, m.Seek("badslot", 0), "Seek should error on invalid slot")
}

// TestLongformPlaybackManager_NoSourceOps verifies that Stop/Pause/Resume/TogglePause/Seek/State
// are all safe no-ops when no source has been registered for a slot.
func TestLongformPlaybackManager_NoSourceOps(t *testing.T) {
	t.Parallel()
	m := NewLongformPlaybackManager()

	slots := []string{"", mediaslot.Primary, mediaslot.Background}
	for _, slot := range slots {
		require.NoError(t, m.Stop(slot), "Stop should be no-op with no source (slot=%q)", slot)
		require.NoError(t, m.Pause(slot), "Pause should be no-op with no source (slot=%q)", slot)
		require.NoError(t, m.Resume(slot), "Resume should be no-op with no source (slot=%q)", slot)
		require.NoError(t, m.TogglePause(slot), "TogglePause should be no-op with no source (slot=%q)", slot)
		require.NoError(t, m.Seek(slot, 5*time.Second), "Seek should be no-op with no source (slot=%q)", slot)
		assert.Equal(t, PlaybackState{}, m.State(slot), "State should be empty with no source (slot=%q)", slot)
	}
}

// TestLongformPlaybackManager_SetDrainCallback verifies callbacks can be registered and invoked.
func TestLongformPlaybackManager_SetDrainCallback(t *testing.T) {
	t.Parallel()
	m := NewLongformPlaybackManager()

	var primaryCalled, backgroundCalled bool
	m.SetDrainCallback(mediaslot.Primary, func() { primaryCalled = true })
	m.SetDrainCallback(mediaslot.Background, func() { backgroundCalled = true })

	m.mu.Lock()
	pcb := m.drainCallbacks[mediaslot.Primary]
	bcb := m.drainCallbacks[mediaslot.Background]
	m.mu.Unlock()

	require.NotNil(t, pcb)
	require.NotNil(t, bcb)

	pcb()
	bcb()
	assert.True(t, primaryCalled)
	assert.True(t, backgroundCalled)
}
