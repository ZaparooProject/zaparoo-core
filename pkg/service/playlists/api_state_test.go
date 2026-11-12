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

package playlists

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mediaslot"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAPIState_RepeatNone(t *testing.T) {
	t.Parallel()
	p := &Playlist{
		ID:   "pl-1",
		Name: "Rock Classics",
		Slot: mediaslot.Background,
		Items: []PlaylistItem{
			{Name: "Track A", ZapScript: "**launch:a.mp3"},
			{Name: "Track B", ZapScript: "**launch:b.mp3"},
		},
		Index: 1,
	}
	got := p.APIState()
	assert.Equal(t, "pl-1", got.ID)
	assert.Equal(t, "Rock Classics", got.Name)
	assert.Equal(t, mediaslot.Background, got.Slot)
	assert.Equal(t, 1, got.Index)
	assert.Equal(t, 2, got.Total)
	assert.Equal(t, "none", got.Repeat)
	assert.False(t, got.Playing)
	assert.Len(t, got.Items, 2)
	assert.Equal(t, "Track A", got.Items[0].Name)
}

func TestAPIState_RepeatAll(t *testing.T) {
	t.Parallel()
	p := &Playlist{Slot: mediaslot.Primary, Loop: true}
	assert.Equal(t, "all", p.APIState().Repeat)
}

func TestAPIState_RepeatOne(t *testing.T) {
	t.Parallel()
	// LoopOne takes priority over Loop if both are set (defensive)
	p := &Playlist{Slot: mediaslot.Primary, Loop: true, LoopOne: true}
	assert.Equal(t, "one", p.APIState().Repeat)
}

func TestAPIState_Playing(t *testing.T) {
	t.Parallel()
	p := &Playlist{Slot: mediaslot.Primary, Playing: true}
	assert.True(t, p.APIState().Playing)
}

func TestAPIState_Unsafe(t *testing.T) {
	t.Parallel()
	untrusted := &Playlist{Slot: mediaslot.Primary, Unsafe: true}
	assert.True(t, untrusted.APIState().Unsafe)
	trusted := &Playlist{Slot: mediaslot.Primary}
	assert.False(t, trusted.APIState().Unsafe)
}

func TestAPIState_Rotation(t *testing.T) {
	t.Parallel()
	none := (&Playlist{Slot: mediaslot.Primary}).APIState()
	assert.Nil(t, none.AdvanceMs)
	assert.Nil(t, none.IdleMs)
	assert.Nil(t, none.CountdownMs)
	assert.False(t, none.Armed)
	encoded, err := json.Marshal(none)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "advanceMs")
	assert.NotContains(t, string(encoded), "countdownMs")

	advancing := (&Playlist{
		Slot: mediaslot.Primary, Advance: 90 * time.Second, Countdown: 0, CountdownSet: true,
	}).APIState()
	require.NotNil(t, advancing.AdvanceMs)
	assert.Equal(t, int64(90000), *advancing.AdvanceMs)
	assert.Nil(t, advancing.IdleMs)
	require.NotNil(t, advancing.CountdownMs, "an explicit zero countdown is reported")
	assert.Equal(t, int64(0), *advancing.CountdownMs)

	idle := (&Playlist{Slot: mediaslot.Primary, Idle: 5 * time.Minute}).APIState()
	require.NotNil(t, idle.IdleMs)
	assert.Equal(t, int64(300000), *idle.IdleMs)
	assert.Nil(t, idle.AdvanceMs)
	assert.Nil(t, idle.CountdownMs, "an unset countdown is omitted")
	assert.False(t, idle.Armed)

	armed := (&Playlist{
		Slot: mediaslot.Primary, Idle: 5 * time.Minute, Armed: true,
	}).APIState()
	assert.True(t, armed.Armed, "a loaded idle playlist reports that it is armed")
	encoded, err = json.Marshal(armed)
	require.NoError(t, err)
	assert.Contains(t, string(encoded), `"armed":true`)
}

func TestAPIState_EmptyItems(t *testing.T) {
	t.Parallel()
	p := &Playlist{Slot: mediaslot.Primary}
	state := p.APIState()
	assert.Empty(t, state.Items)
	assert.Equal(t, 0, state.Total)
}

func TestAPIState_Shuffle(t *testing.T) {
	t.Parallel()
	plain := (&Playlist{Slot: mediaslot.Primary}).APIState()
	assert.False(t, plain.Shuffle)
	encoded, err := json.Marshal(plain)
	require.NoError(t, err)
	assert.Contains(t, string(encoded), `"shuffle":false`, "shuffle is always present")

	shuffled := (&Playlist{Slot: mediaslot.Primary, Shuffled: true}).APIState()
	assert.True(t, shuffled.Shuffle)
}
