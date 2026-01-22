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
	"sync/atomic"
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

	// Helper to read current item safely from tview's goroutine
	getCurrentItem := func() int {
		var result int
		runner.QueueUpdateDraw(func() {
			result = sl.GetCurrentItem()
		})
		return result
	}

	// Verify initial selection is first item (index 0)
	assert.Equal(t, 0, getCurrentItem())

	// Navigate down
	runner.Screen().InjectArrowDown()
	runner.Draw()
	assert.Equal(t, 1, getCurrentItem())

	// Navigate down again
	runner.Screen().InjectArrowDown()
	runner.Draw()
	assert.Equal(t, 2, getCurrentItem())

	// Navigate up
	runner.Screen().InjectArrowUp()
	runner.Draw()
	assert.Equal(t, 1, getCurrentItem())
}

func TestSettingsList_ToggleActivation_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()

	pages := tview.NewPages()
	sl := NewSettingsList(pages, "main")

	toggleValue := false
	var toggleCalled atomic.Bool

	sl.AddToggle("Audio Feedback", "Play sound on scan", &toggleValue, func(_ bool) {
		toggleCalled.Store(true)
	})

	pages.AddPage("settings", sl.List, true, true)
	runner.Start(pages)
	runner.Draw()

	// Press Enter to toggle
	runner.Screen().InjectEnter()
	runner.Draw()

	// Give time for the callback
	time.Sleep(20 * time.Millisecond)

	assert.True(t, toggleCalled.Load(), "toggle callback should be called")
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
	runner.Screen().InjectEscape()
	runner.Draw()
	time.Sleep(20 * time.Millisecond)

	// Verify we switched to main page
	assert.Equal(t, "main", getFrontPage())
}

func TestButtonBar_Navigation_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()

	bb := NewButtonBar(runner.App())

	var button1Pressed atomic.Bool
	var button2Pressed atomic.Bool

	bb.AddButton("Button 1", func() { button1Pressed.Store(true) })
	bb.AddButton("Button 2", func() { button2Pressed.Store(true) })
	bb.SetupNavigation(nil)

	runner.Start(bb)
	runner.SetFocus(bb)

	// Press Enter on first button
	runner.Screen().InjectEnter()
	runner.Draw()
	time.Sleep(20 * time.Millisecond)

	assert.True(t, button1Pressed.Load(), "first button should be pressed")
	assert.False(t, button2Pressed.Load(), "second button should not be pressed yet")

	// Navigate right to second button
	runner.Screen().InjectArrowRight()
	runner.Draw()

	// Press Enter on second button
	runner.Screen().InjectEnter()
	runner.Draw()
	time.Sleep(20 * time.Millisecond)

	assert.True(t, button2Pressed.Load(), "second button should be pressed")
}

func TestButtonBar_EscapeCallback_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()

	bb := NewButtonBar(runner.App())

	var escapeCalled atomic.Bool
	bb.AddButton("Test", func() {})
	bb.SetupNavigation(func() {
		escapeCalled.Store(true)
	})

	runner.Start(bb)
	runner.SetFocus(bb)

	// Press Escape
	runner.Screen().InjectEscape()
	runner.Draw()
	time.Sleep(20 * time.Millisecond)

	assert.True(t, escapeCalled.Load(), "escape callback should be called")
}

func TestCheckList_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()

	items := []string{"Item A", "Item B", "Item C"}
	var selections []string
	var selMu syncutil.Mutex

	cl := NewCheckList(items, nil, func(sel []string) {
		selMu.Lock()
		selections = make([]string, len(sel))
		copy(selections, sel)
		selMu.Unlock()
	})

	runner.Start(cl.List)
	runner.Draw()

	// Toggle first item
	runner.Screen().InjectEnter()
	runner.Draw()
	time.Sleep(20 * time.Millisecond)

	selMu.Lock()
	require.Len(t, selections, 1)
	assert.Equal(t, "Item A", selections[0])
	selMu.Unlock()

	// Navigate down and toggle second
	runner.Screen().InjectArrowDown()
	runner.Draw()
	runner.Screen().InjectEnter()
	runner.Draw()
	time.Sleep(20 * time.Millisecond)

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
	runner.Screen().InjectEscape()
	runner.Draw()
	time.Sleep(20 * time.Millisecond)

	// Verify we went back to main
	assert.Equal(t, "main", getFrontPage())
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

	// Helper to read current item safely
	getCurrentItem := func() int {
		var result int
		runner.QueueUpdateDraw(func() {
			result = sl.GetCurrentItem()
		})
		return result
	}

	// First item should be selected
	assert.Equal(t, 0, getCurrentItem())

	// Navigate and verify refresh happens
	runner.Screen().InjectArrowDown()
	runner.Draw()

	assert.Equal(t, 1, getCurrentItem())

	// Navigate back up
	runner.Screen().InjectArrowUp()
	runner.Draw()

	assert.Equal(t, 0, getCurrentItem())
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
