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

package tui

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckList_Creation(t *testing.T) {
	t.Parallel()

	items := []string{"Item 1", "Item 2", "Item 3"}
	cl := NewCheckList(items, nil, nil)

	require.NotNil(t, cl)
	assert.Equal(t, 3, cl.GetItemCount())
	assert.Empty(t, cl.GetSelected())
	assert.Equal(t, 0, cl.GetSelectedCount())
}

func TestCheckList_InitialSelection(t *testing.T) {
	t.Parallel()

	items := []string{"A", "B", "C", "D"}
	selected := []string{"B", "D"}

	cl := NewCheckList(items, selected, nil)

	result := cl.GetSelected()
	assert.Len(t, result, 2)
	assert.Contains(t, result, "B")
	assert.Contains(t, result, "D")
	assert.NotContains(t, result, "A")
	assert.NotContains(t, result, "C")
	assert.Equal(t, 2, cl.GetSelectedCount())
}

func TestCheckList_Toggle(t *testing.T) {
	t.Parallel()

	items := []string{"A", "B", "C"}
	var lastSelection []string
	onChange := func(sel []string) {
		lastSelection = sel
	}

	cl := NewCheckList(items, nil, onChange)

	// Initially nothing selected
	assert.Empty(t, cl.GetSelected())

	// Toggle first item on
	cl.toggle(0)
	assert.Equal(t, []string{"A"}, lastSelection)
	assert.Equal(t, []string{"A"}, cl.GetSelected())

	// Toggle third item on
	cl.toggle(2)
	assert.Len(t, lastSelection, 2)
	assert.Contains(t, lastSelection, "A")
	assert.Contains(t, lastSelection, "C")

	// Toggle first item off
	cl.toggle(0)
	assert.Equal(t, []string{"C"}, lastSelection)
	assert.Equal(t, []string{"C"}, cl.GetSelected())

	// Toggle third item off
	cl.toggle(2)
	assert.Empty(t, lastSelection)
	assert.Empty(t, cl.GetSelected())
}

func TestCheckListWithValues_Creation(t *testing.T) {
	t.Parallel()

	items := []CheckListItem{
		{Label: "Nintendo Entertainment System", Value: "nes"},
		{Label: "Super Nintendo", Value: "snes"},
		{Label: "Sega Genesis", Value: "genesis"},
	}
	selected := []string{"snes", "genesis"}

	cl := NewCheckListWithValues(items, selected, nil)

	require.NotNil(t, cl)
	assert.Equal(t, 3, cl.GetItemCount())

	result := cl.GetSelected()
	assert.Len(t, result, 2)
	assert.Contains(t, result, "snes")
	assert.Contains(t, result, "genesis")
	assert.NotContains(t, result, "nes")
}

func TestCheckListWithValues_ToggleReturnsValues(t *testing.T) {
	t.Parallel()

	items := []CheckListItem{
		{Label: "Display Name", Value: "actual_value"},
		{Label: "Another Display", Value: "another_value"},
	}

	var lastSelection []string
	onChange := func(sel []string) {
		lastSelection = sel
	}

	cl := NewCheckListWithValues(items, nil, onChange)

	// Toggle first item - should get VALUE not label
	cl.toggle(0)
	require.Len(t, lastSelection, 1)
	assert.Equal(t, "actual_value", lastSelection[0])
	assert.NotContains(t, lastSelection, "Display Name")
}

func TestCheckList_GetSelectedCount(t *testing.T) {
	t.Parallel()

	items := []string{"A", "B", "C", "D", "E"}
	selected := []string{"A", "C", "E"}

	cl := NewCheckList(items, selected, nil)

	assert.Equal(t, 3, cl.GetSelectedCount())

	// Toggle one off
	cl.toggle(0) // A
	assert.Equal(t, 2, cl.GetSelectedCount())

	// Toggle one on
	cl.toggle(1) // B
	assert.Equal(t, 3, cl.GetSelectedCount())
}

func TestCheckList_SelectionSyncFunc(t *testing.T) {
	t.Parallel()

	items := []string{"A", "B", "C"}
	cl := NewCheckList(items, nil, nil)

	var syncedCount int
	cl.SetSelectionSyncFunc(func(count int) {
		syncedCount = count
	})

	// Toggle items and verify sync func is called
	cl.toggle(0)
	assert.Equal(t, 1, syncedCount)

	cl.toggle(1)
	assert.Equal(t, 2, syncedCount)

	cl.toggle(0)
	assert.Equal(t, 1, syncedCount)
}

func TestCheckList_NoOnChangeCallback(t *testing.T) {
	t.Parallel()

	items := []string{"A", "B", "C"}

	// Should not panic with nil onChange
	cl := NewCheckList(items, nil, nil)
	require.NotPanics(t, func() {
		cl.toggle(0)
		cl.toggle(1)
		cl.toggle(0)
	})

	// Verify toggling still works
	assert.Equal(t, []string{"B"}, cl.GetSelected())
}

func TestCheckList_FormatItem(t *testing.T) {
	t.Parallel()

	items := []string{"Test Item"}
	cl := NewCheckList(items, nil, nil)

	// Not selected
	formatted := cl.formatItem(0, "Test Item")
	assert.Equal(t, "[ ] Test Item", formatted)

	// Toggle on
	cl.toggle(0)
	formatted = cl.formatItem(0, "Test Item")
	assert.Equal(t, "[*] Test Item", formatted)
}

func TestCheckList_EmptyItems(t *testing.T) {
	t.Parallel()

	cl := NewCheckList(nil, nil, nil)

	assert.Equal(t, 0, cl.GetItemCount())
	assert.Empty(t, cl.GetSelected())
	assert.Equal(t, 0, cl.GetSelectedCount())
}

func TestCheckList_SelectAllItems(t *testing.T) {
	t.Parallel()

	items := []string{"A", "B", "C"}
	selected := []string{"A", "B", "C"} // All selected initially

	cl := NewCheckList(items, selected, nil)

	assert.Equal(t, 3, cl.GetSelectedCount())
	result := cl.GetSelected()
	assert.Len(t, result, 3)
}

func TestCheckListWithValues_MismatchedSelection(t *testing.T) {
	t.Parallel()

	items := []CheckListItem{
		{Label: "Item A", Value: "a"},
		{Label: "Item B", Value: "b"},
	}
	// Try to select values that don't exist
	selected := []string{"x", "y", "z"}

	cl := NewCheckListWithValues(items, selected, nil)

	// None should be selected since values don't match
	assert.Equal(t, 0, cl.GetSelectedCount())
	assert.Empty(t, cl.GetSelected())
}
