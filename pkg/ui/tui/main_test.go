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

package tui

import (
	"sync"
	"testing"

	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMainPageState_SetCancel(t *testing.T) {
	t.Parallel()

	state := &mainPageState{}
	cancelCalled := false

	// Set a cancel function
	state.SetCancel(func() {
		cancelCalled = true
	})

	// Cancel should work
	state.Cancel()
	assert.True(t, cancelCalled)

	// Cancel again should be safe (nil check)
	state.Cancel()
}

func TestMainPageState_SetCancelReplacesOld(t *testing.T) {
	t.Parallel()

	state := &mainPageState{}
	firstCancelCalled := false
	secondCancelCalled := false

	// Set first cancel
	state.SetCancel(func() {
		firstCancelCalled = true
	})

	// Set second cancel (should call first)
	state.SetCancel(func() {
		secondCancelCalled = true
	})

	// First should have been called when replaced
	assert.True(t, firstCancelCalled)
	assert.False(t, secondCancelCalled)

	// Now cancel should call second
	state.Cancel()
	assert.True(t, secondCancelCalled)
}

func TestMainPageState_Concurrent(t *testing.T) {
	t.Parallel()

	state := &mainPageState{}
	var wg sync.WaitGroup
	iterations := 100

	// Concurrent SetCancel and Cancel operations
	for range iterations {
		wg.Add(2)
		go func() {
			defer wg.Done()
			state.SetCancel(func() {})
		}()
		go func() {
			defer wg.Done()
			state.Cancel()
		}()
	}

	wg.Wait()
	// Just verify no race conditions - no assertion needed
}

func TestButtonGrid_NewButtonGrid(t *testing.T) {
	t.Parallel()

	app := tview.NewApplication()
	grid := NewButtonGrid(app, 3)

	require.NotNil(t, grid)
	assert.NotNil(t, grid.Box)
	assert.Equal(t, 3, grid.cols)
	assert.Len(t, grid.buttons, 2) // 2 rows
}

func TestButtonGrid_AddRow(t *testing.T) {
	t.Parallel()

	app := tview.NewApplication()
	grid := NewButtonGrid(app, 3)

	btn1 := &ButtonGridItem{Button: tview.NewButton("Btn1"), HelpText: "Help 1"}
	btn2 := &ButtonGridItem{Button: tview.NewButton("Btn2"), HelpText: "Help 2"}

	// Add first row
	grid.AddRow(btn1, btn2)
	assert.Len(t, grid.buttons[0], 2)
	assert.Nil(t, grid.buttons[1])

	// Add second row
	btn3 := &ButtonGridItem{Button: tview.NewButton("Btn3"), HelpText: "Help 3"}
	grid.AddRow(btn3)
	assert.Len(t, grid.buttons[1], 1)
}

func TestButtonGrid_SetOnHelp(t *testing.T) {
	t.Parallel()

	app := tview.NewApplication()
	grid := NewButtonGrid(app, 2)

	helpReceived := ""
	grid.SetOnHelp(func(help string) {
		helpReceived = help
	})

	// Trigger help callback manually
	if grid.onHelp != nil {
		grid.onHelp("Test help text")
	}

	assert.Equal(t, "Test help text", helpReceived)
}

func TestButtonGrid_SetOnEscape(t *testing.T) {
	t.Parallel()

	app := tview.NewApplication()
	grid := NewButtonGrid(app, 2)

	escapeCalled := false
	grid.SetOnEscape(func() {
		escapeCalled = true
	})

	// Trigger escape callback manually
	if grid.onEscape != nil {
		grid.onEscape()
	}

	assert.True(t, escapeCalled)
}

func TestButtonGridItem_Disabled(t *testing.T) {
	t.Parallel()

	item := &ButtonGridItem{
		Button:   tview.NewButton("Test"),
		HelpText: "Test help",
		Disabled: true,
	}

	assert.True(t, item.Disabled)
	assert.Equal(t, "Test help", item.HelpText)
}
