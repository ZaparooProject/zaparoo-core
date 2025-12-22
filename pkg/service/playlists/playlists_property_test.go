// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
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
	"testing"

	"pgregory.net/rapid"
)

// playlistItemGen generates random PlaylistItem values.
func playlistItemGen() *rapid.Generator[PlaylistItem] {
	return rapid.Custom(func(t *rapid.T) PlaylistItem {
		return PlaylistItem{
			ZapScript: rapid.String().Draw(t, "zapscript"),
			Name:      rapid.String().Draw(t, "name"),
		}
	})
}

// playlistGen generates random Playlist values with valid state.
func playlistGen() *rapid.Generator[Playlist] {
	return rapid.Custom(func(t *rapid.T) Playlist {
		items := rapid.SliceOf(playlistItemGen()).Draw(t, "items")
		idx := 0
		if len(items) > 0 {
			idx = rapid.IntRange(0, len(items)-1).Draw(t, "index")
		}
		return Playlist{
			ID:      rapid.String().Draw(t, "id"),
			Name:    rapid.String().Draw(t, "name"),
			Items:   items,
			Index:   idx,
			Playing: rapid.Bool().Draw(t, "playing"),
		}
	})
}

// nonEmptyPlaylistGen generates playlists with at least one item.
func nonEmptyPlaylistGen() *rapid.Generator[Playlist] {
	return rapid.Custom(func(t *rapid.T) Playlist {
		items := rapid.SliceOfN(playlistItemGen(), 1, 100).Draw(t, "items")
		idx := rapid.IntRange(0, len(items)-1).Draw(t, "index")
		return Playlist{
			ID:      rapid.String().Draw(t, "id"),
			Name:    rapid.String().Draw(t, "name"),
			Items:   items,
			Index:   idx,
			Playing: rapid.Bool().Draw(t, "playing"),
		}
	})
}

// TestPropertyNextIndexBounds verifies Next() maintains valid index bounds.
func TestPropertyNextIndexBounds(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		p := nonEmptyPlaylistGen().Draw(t, "playlist")
		result := Next(p)

		if result.Index < 0 || result.Index >= len(result.Items) {
			t.Fatalf("Next() produced invalid index %d for %d items",
				result.Index, len(result.Items))
		}
	})
}

// TestPropertyPreviousIndexBounds verifies Previous() maintains valid index bounds.
func TestPropertyPreviousIndexBounds(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		p := nonEmptyPlaylistGen().Draw(t, "playlist")
		result := Previous(p)

		if result.Index < 0 || result.Index >= len(result.Items) {
			t.Fatalf("Previous() produced invalid index %d for %d items",
				result.Index, len(result.Items))
		}
	})
}

// TestPropertyGotoIndexBounds verifies Goto() clamps index to valid bounds.
func TestPropertyGotoIndexBounds(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		p := playlistGen().Draw(t, "playlist")
		targetIdx := rapid.IntRange(-100, 200).Draw(t, "targetIndex")
		result := Goto(p, targetIdx)

		// Empty playlist: index should be 0
		if len(result.Items) == 0 {
			if result.Index != 0 {
				t.Fatalf("Goto() on empty playlist should set index to 0, got %d", result.Index)
			}
			return
		}

		// Non-empty playlist: index must be in bounds
		if result.Index < 0 || result.Index >= len(result.Items) {
			t.Fatalf("Goto(%d) produced invalid index %d for %d items",
				targetIdx, result.Index, len(result.Items))
		}
	})
}

// TestPropertyNavigationReversibility verifies Next() then Previous() returns to original.
func TestPropertyNavigationReversibility(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		p := nonEmptyPlaylistGen().Draw(t, "playlist")
		originalIdx := p.Index

		afterNext := Next(p)
		afterPrev := Previous(*afterNext)

		if afterPrev.Index != originalIdx {
			t.Fatalf("Next then Previous did not return to original index: %d -> %d -> %d",
				originalIdx, afterNext.Index, afterPrev.Index)
		}
	})
}

// TestPropertyItemsPreserved verifies operations never lose items.
func TestPropertyItemsPreserved(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		p := playlistGen().Draw(t, "playlist")
		originalLen := len(p.Items)

		// Apply random sequence of operations
		ops := rapid.SliceOfN(rapid.IntRange(0, 4), 1, 10).Draw(t, "ops")
		current := &p
		for _, op := range ops {
			switch op {
			case 0:
				current = Next(*current)
			case 1:
				current = Previous(*current)
			case 2:
				current = Goto(*current, rapid.IntRange(-10, 20).Draw(t, "gotoIdx"))
			case 3:
				current = Play(*current)
			case 4:
				current = Pause(*current)
			}
		}

		if len(current.Items) != originalLen {
			t.Fatalf("Items lost during operations: started with %d, ended with %d",
				originalLen, len(current.Items))
		}
	})
}

// TestPropertyNextWrapsAtEnd verifies Next() wraps from last to first.
func TestPropertyNextWrapsAtEnd(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		items := rapid.SliceOfN(playlistItemGen(), 2, 50).Draw(t, "items")
		p := Playlist{
			ID:    "test",
			Items: items,
			Index: len(items) - 1, // Start at last item
		}

		result := Next(p)
		if result.Index != 0 {
			t.Fatalf("Next() at last index should wrap to 0, got %d", result.Index)
		}
	})
}

// TestPropertyPreviousWrapsAtStart verifies Previous() wraps from first to last.
func TestPropertyPreviousWrapsAtStart(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		items := rapid.SliceOfN(playlistItemGen(), 2, 50).Draw(t, "items")
		p := Playlist{
			ID:    "test",
			Items: items,
			Index: 0, // Start at first item
		}

		result := Previous(p)
		expectedIdx := len(items) - 1
		if result.Index != expectedIdx {
			t.Fatalf("Previous() at index 0 should wrap to %d, got %d",
				expectedIdx, result.Index)
		}
	})
}

// TestPropertyPlayPauseToggle verifies Play/Pause toggle behavior.
func TestPropertyPlayPauseToggle(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		p := playlistGen().Draw(t, "playlist")

		played := Play(p)
		if !played.Playing {
			t.Fatal("Play() should set Playing to true")
		}

		paused := Pause(*played)
		if paused.Playing {
			t.Fatal("Pause() should set Playing to false")
		}
	})
}

// TestPropertyCurrentNeverPanics verifies Current() never panics regardless of state.
func TestPropertyCurrentNeverPanics(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		// Generate potentially invalid playlist states
		items := rapid.SliceOf(playlistItemGen()).Draw(t, "items")
		idx := rapid.IntRange(-10, 110).Draw(t, "index")
		p := &Playlist{
			ID:    "test",
			Items: items,
			Index: idx,
		}

		// This should not panic
		_ = p.Current()
	})
}

// TestPropertyFullCycleNavigation verifies navigating through all items returns to start.
func TestPropertyFullCycleNavigation(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		items := rapid.SliceOfN(playlistItemGen(), 1, 50).Draw(t, "items")
		p := Playlist{
			ID:    "test",
			Items: items,
			Index: 0,
		}

		// Navigate forward through all items
		current := &p
		for range items {
			current = Next(*current)
		}

		// Should be back at start
		if current.Index != 0 {
			t.Fatalf("Full cycle navigation should return to index 0, got %d", current.Index)
		}
	})
}

// TestPropertyIDPreserved verifies ID is preserved across all operations.
func TestPropertyIDPreserved(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		p := playlistGen().Draw(t, "playlist")
		originalID := p.ID

		// Apply random operations
		ops := rapid.SliceOfN(rapid.IntRange(0, 4), 1, 10).Draw(t, "ops")
		current := &p
		for _, op := range ops {
			switch op {
			case 0:
				current = Next(*current)
			case 1:
				current = Previous(*current)
			case 2:
				current = Goto(*current, rapid.IntRange(0, 10).Draw(t, "idx"))
			case 3:
				current = Play(*current)
			case 4:
				current = Pause(*current)
			}
		}

		if current.ID != originalID {
			t.Fatalf("ID changed during operations: %q -> %q", originalID, current.ID)
		}
	})
}
