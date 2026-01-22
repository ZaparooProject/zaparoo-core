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
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSettingsList_Navigation_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()

	pages := tview.NewPages()
	sl := NewSettingsList(pages, "main")

	toggleValue := false
	cycleIndex := 0

	sl.AddToggle("Test Toggle", "A test toggle", &toggleValue, func(bool) {})
	sl.AddCycle("Test Cycle", "A test cycle", []string{"A", "B"}, &cycleIndex, func(string, int) {})
	sl.AddAction("Test Action", "A test action", func() {})
	sl.AddBack()

	pages.AddPage("settings", sl.List, true, true)
	runner.Start(pages)
	runner.Draw()

	// Verify initial selection is first item (index 0)
	assert.Equal(t, 0, sl.GetCurrentItem())

	// Navigate down
	runner.SimulateArrowDown()
	assert.Equal(t, 1, sl.GetCurrentItem())

	// Navigate down again
	runner.SimulateArrowDown()
	assert.Equal(t, 2, sl.GetCurrentItem())

	// Navigate up
	runner.SimulateArrowUp()
	assert.Equal(t, 1, sl.GetCurrentItem())
}

func TestSettingsList_ToggleActivation_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()

	pages := tview.NewPages()
	sl := NewSettingsList(pages, "main")

	toggleValue := false
	toggleCalled := make(chan struct{}, 1)

	sl.AddToggle("Audio Feedback", "Play sound on scan", &toggleValue, func(_ bool) {
		select {
		case toggleCalled <- struct{}{}:
		default:
		}
	})

	pages.AddPage("settings", sl.List, true, true)
	runner.Start(pages)
	runner.Draw()

	// Press Enter to toggle
	runner.SimulateEnter()

	// Wait for the callback
	assert.True(t, runner.WaitForSignal(toggleCalled, 100*time.Millisecond), "toggle callback should be called")
	assert.True(t, toggleValue, "toggle value should be true after activation")
}

func TestSettingsList_EscapeGoesBack_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()

	pages := tview.NewPages()

	// Add a main page to go back to
	mainPage := tview.NewTextView().SetText("Main Page")
	pages.AddPage("main", mainPage, true, false)

	// Create settings list that goes back to main
	sl := NewSettingsList(pages, "main")
	sl.AddAction("Some Action", "Description", func() {})
	pages.AddPage("settings", sl.List, true, true)

	runner.Start(pages)
	runner.Draw()

	// Helper to read current page safely
	getFrontPage := func() string {
		var name string
		runner.QueueUpdateDraw(func() {
			name, _ = pages.GetFrontPage()
		})
		return name
	}

	// Verify we're on settings
	assert.Equal(t, "settings", getFrontPage())

	// Press Escape
	runner.SimulateEscape()

	// Verify we switched to main page
	assert.True(t, runner.WaitForCondition(func() bool {
		return getFrontPage() == "main"
	}, 100*time.Millisecond), "Should switch to main page")
}

func TestButtonBar_Navigation_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()

	bb := NewButtonBar(runner.App())

	button1Pressed := make(chan struct{}, 1)
	button2Pressed := make(chan struct{}, 1)

	bb.AddButton("Button 1", func() {
		select {
		case button1Pressed <- struct{}{}:
		default:
		}
	})
	bb.AddButton("Button 2", func() {
		select {
		case button2Pressed <- struct{}{}:
		default:
		}
	})
	bb.SetupNavigation(nil)

	runner.Start(bb)
	runner.SetFocus(bb)

	// Press Enter on first button
	runner.SimulateEnter()

	assert.True(t, runner.WaitForSignal(button1Pressed, 100*time.Millisecond), "first button should be pressed")

	// Navigate right to second button
	runner.SimulateArrowRight()

	// Press Enter on second button
	runner.SimulateEnter()

	assert.True(t, runner.WaitForSignal(button2Pressed, 100*time.Millisecond), "second button should be pressed")
}

func TestButtonBar_EscapeCallback_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()

	bb := NewButtonBar(runner.App())

	escapeCalled := make(chan struct{}, 1)
	bb.AddButton("Test", func() {})
	bb.SetupNavigation(func() {
		select {
		case escapeCalled <- struct{}{}:
		default:
		}
	})

	runner.Start(bb)
	runner.SetFocus(bb)

	// Press Escape
	runner.SimulateEscape()

	assert.True(t, runner.WaitForSignal(escapeCalled, 100*time.Millisecond), "escape callback should be called")
}

func TestCheckList_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()

	items := []string{"Item A", "Item B", "Item C"}
	var selections []string
	var selMu syncutil.Mutex
	selectionChanged := make(chan struct{}, 10)

	cl := NewCheckList(items, nil, func(sel []string) {
		selMu.Lock()
		selections = make([]string, len(sel))
		copy(selections, sel)
		selMu.Unlock()
		select {
		case selectionChanged <- struct{}{}:
		default:
		}
	})

	runner.Start(cl.List)
	runner.Draw()

	// Toggle first item
	runner.SimulateEnter()

	assert.True(t, runner.WaitForSignal(selectionChanged, 100*time.Millisecond), "selection callback should be called")

	selMu.Lock()
	require.Len(t, selections, 1)
	assert.Equal(t, "Item A", selections[0])
	selMu.Unlock()

	// Navigate down and toggle second
	runner.SimulateArrowDown()
	runner.SimulateEnter()

	assert.True(t, runner.WaitForSignal(selectionChanged, 100*time.Millisecond), "selection callback should be called")

	selMu.Lock()
	require.Len(t, selections, 2)
	assert.Contains(t, selections, "Item A")
	assert.Contains(t, selections, "Item B")
	selMu.Unlock()
}

func TestCheckList_EscapeNavigation_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()

	pages := tview.NewPages()

	// Add main page
	mainPage := tview.NewTextView().SetText("Main")
	pages.AddPage("main", mainPage, true, false)

	// Add checklist page
	cl := NewCheckList([]string{"A", "B"}, nil, nil)
	cl.SetupNavigation(pages, "main")
	pages.AddPage("checklist", cl.List, true, true)

	runner.Start(pages)
	runner.Draw()

	// Helper to read current page safely
	getFrontPage := func() string {
		var name string
		runner.QueueUpdateDraw(func() {
			name, _ = pages.GetFrontPage()
		})
		return name
	}

	// Verify we're on checklist
	assert.Equal(t, "checklist", getFrontPage())

	// Press Escape
	runner.SimulateEscape()

	// Verify we went back to main
	assert.True(t, runner.WaitForCondition(func() bool {
		return getFrontPage() == "main"
	}, 100*time.Millisecond), "Should go back to main")
}

func TestSettingsList_RefreshItems_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()

	pages := tview.NewPages()
	sl := NewSettingsList(pages, "main")

	toggleVal := false
	sl.AddToggle("Toggle A", "Description A", &toggleVal, func(bool) {})
	sl.AddToggle("Toggle B", "Description B", &toggleVal, func(bool) {})

	pages.AddPage("settings", sl.List, true, true)
	runner.Start(pages)
	runner.Draw()

	// First item should be selected
	assert.Equal(t, 0, sl.GetCurrentItem())

	// Navigate and verify refresh happens
	runner.SimulateArrowDown()

	assert.Equal(t, 1, sl.GetCurrentItem())

	// Navigate back up
	runner.SimulateArrowUp()

	assert.Equal(t, 0, sl.GetCurrentItem())
}

func TestScreenContainsText_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()

	textView := tview.NewTextView().SetText("Hello World")
	runner.Start(textView)
	runner.Draw()

	// Verify text is on screen
	assert.True(t, runner.ContainsText("Hello"))
	assert.True(t, runner.ContainsText("World"))
	assert.True(t, runner.ContainsText("Hello World"))
	assert.False(t, runner.ContainsText("Goodbye"))
}

func TestMultiplePages_Navigation_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()

	pages := tview.NewPages()

	// Create page 1
	page1 := tview.NewTextView().SetText("Page 1 Content")
	pages.AddPage("page1", page1, true, true)

	// Create page 2
	page2 := tview.NewTextView().SetText("Page 2 Content")
	pages.AddPage("page2", page2, true, false)

	runner.Start(pages)
	runner.Draw()

	// Helper to read current page safely
	getFrontPage := func() string {
		var name string
		runner.QueueUpdateDraw(func() {
			name, _ = pages.GetFrontPage()
		})
		return name
	}

	// Verify page 1 is showing
	assert.Equal(t, "page1", getFrontPage())

	// Switch to page 2
	runner.QueueUpdateDraw(func() {
		pages.SwitchToPage("page2")
	})

	// Verify page 2 is now showing
	assert.Equal(t, "page2", getFrontPage())
}
