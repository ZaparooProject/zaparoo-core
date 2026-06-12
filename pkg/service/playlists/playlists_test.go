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

package playlists_test

import (
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mediaslot"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	"github.com/stretchr/testify/assert"
)

func TestNewPlaylist_DefaultSlot(t *testing.T) {
	t.Parallel()
	p := playlists.NewPlaylist("id", "name", nil)
	assert.Equal(t, mediaslot.Primary, p.Slot)
}

func TestTransitions_PreserveSlot(t *testing.T) {
	t.Parallel()

	items := []playlists.PlaylistItem{
		{ZapScript: "a"},
		{ZapScript: "b"},
		{ZapScript: "c"},
	}

	// Use a non-default slot to prove it survives every transition.
	p := playlists.NewPlaylist("id", "name", items)
	p.Slot = mediaslot.Background

	t.Run("Next", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, mediaslot.Background, playlists.Next(*p).Slot)
	})
	t.Run("Previous", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, mediaslot.Background, playlists.Previous(*p).Slot)
	})
	t.Run("Goto", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, mediaslot.Background, playlists.Goto(*p, 1).Slot)
	})
	t.Run("Play", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, mediaslot.Background, playlists.Play(*p).Slot)
	})
	t.Run("Pause", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, mediaslot.Background, playlists.Pause(*p).Slot)
	})
}
