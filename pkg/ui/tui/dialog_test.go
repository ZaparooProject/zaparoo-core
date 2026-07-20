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
	"strings"
	"testing"
	"time"

	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// screenBounds is the bounding box of all non-blank cells on screen.
type screenBounds struct {
	minX, minY, maxX, maxY int
}

// drawnBounds returns the bounding box of all non-blank cells on screen,
// synchronized with the app's draw cycle. ok is false when nothing is drawn.
func drawnBounds(runner *TestAppRunner) (bounds screenBounds, ok bool) {
	runner.QueueUpdateDraw(func() {
		cells, width, height := runner.Screen().GetContents()
		bounds = screenBounds{minX: width, minY: height, maxX: -1, maxY: -1}
		for y := range height {
			for x := range width {
				cell := cells[y*width+x]
				if len(cell.Runes) == 0 || cell.Runes[0] == ' ' {
					continue
				}
				bounds.minX = min(bounds.minX, x)
				bounds.minY = min(bounds.minY, y)
				bounds.maxX = max(bounds.maxX, x)
				bounds.maxY = max(bounds.maxY, y)
			}
		}
	})
	return bounds, bounds.maxX >= 0
}

func TestDialog_SizesToContent(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()

	pages := tview.NewPages()
	pages.AddPage("main", tview.NewBox(), true, true)
	runner.Start(pages)
	runner.Draw()

	runner.QueueUpdateDraw(func() {
		dialog := NewDialog().
			SetText("Hi").
			AddButtons([]string{"OK"}).
			SetDoneFunc(func(_ int) {})
		pages.AddPage("dialog", dialog, true, true)
		runner.App().SetFocus(dialog)
	})
	require.True(t, runner.WaitForText("Hi", 500*time.Millisecond))

	bounds, ok := drawnBounds(runner)
	require.True(t, ok, "dialog should be drawn")
	width := bounds.maxX - bounds.minX + 1
	height := bounds.maxY - bounds.minY + 1
	// Content is tiny, so the dialog must be far narrower than the
	// tview.Modal minimum of a third of the screen plus chrome.
	assert.LessOrEqual(t, width, 14, "dialog width should fit content")
	assert.LessOrEqual(t, height, 6, "dialog height should fit content")
}

func TestDialog_ClampedToParentWindow(t *testing.T) {
	t.Parallel()

	// The simulation screen is always 80x25: tview re-inits the screen on
	// Run, which resets a SimulationScreen to its default size.
	const (
		screenWidth  = 80
		screenHeight = 25
		windowWidth  = 30
		windowHeight = 10
	)

	runner := NewTestAppRunner(t, screenWidth, screenHeight)
	defer runner.Stop()

	pages := tview.NewPages()
	pages.AddPage("main", tview.NewBox(), true, true)
	// Same shape as CRT mode: the whole TUI lives in a small centered window.
	runner.Start(CenterWidget(windowWidth, windowHeight, pages))
	runner.Draw()

	runner.QueueUpdateDraw(func() {
		dialog := NewDialog().
			SetText("This message is far too long to fit in the parent window " +
				"on a single line so it has to word-wrap instead of overflowing.").
			SetTitle("Clamp").
			AddButtons([]string{"OK"}).
			SetDoneFunc(func(_ int) {})
		pages.AddPage("dialog", dialog, true, true)
		runner.App().SetFocus(dialog)
	})
	require.True(t, runner.WaitForText("word-wrap", 500*time.Millisecond))

	bounds, ok := drawnBounds(runner)
	require.True(t, ok, "dialog should be drawn")

	windowX := (screenWidth - windowWidth) / 2
	windowY := (screenHeight - windowHeight) / 2
	assert.GreaterOrEqual(t, bounds.minX, windowX, "dialog must not draw left of the window")
	assert.GreaterOrEqual(t, bounds.minY, windowY, "dialog must not draw above the window")
	assert.Less(t, bounds.maxX, windowX+windowWidth, "dialog must not draw right of the window")
	assert.Less(t, bounds.maxY, windowY+windowHeight, "dialog must not draw below the window")
}

func TestDialog_ButtonSelectionAndEscape(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()

	pages := tview.NewPages()
	pages.AddPage("main", tview.NewBox(), true, true)
	runner.Start(pages)
	runner.Draw()

	show := func() chan int {
		pressed := make(chan int, 1)
		runner.QueueUpdateDraw(func() {
			dialog := NewDialog().
				SetText("Pick one").
				AddButtons([]string{"First", "Second"}).
				SetDoneFunc(func(buttonIndex int) {
					pages.RemovePage("dialog")
					pressed <- buttonIndex
				})
			pages.AddPage("dialog", dialog, true, true)
			runner.App().SetFocus(dialog)
		})
		require.True(t, runner.WaitForText("Pick one", 500*time.Millisecond))
		return pressed
	}

	pressed := show()
	runner.Screen().InjectEnter()
	runner.Draw()
	select {
	case index := <-pressed:
		assert.Equal(t, 0, index, "Enter should select the first button")
	case <-time.After(500 * time.Millisecond):
		t.Fatal("done handler was not called for the first button")
	}

	pressed = show()
	runner.Screen().InjectArrowRight()
	runner.Screen().InjectEnter()
	runner.Draw()
	select {
	case index := <-pressed:
		assert.Equal(t, 1, index, "right arrow then Enter should select the second button")
	case <-time.After(500 * time.Millisecond):
		t.Fatal("done handler was not called for the second button")
	}

	pressed = show()
	runner.Screen().InjectEscape()
	runner.Draw()
	select {
	case index := <-pressed:
		assert.Equal(t, -1, index, "Escape should report index -1")
	case <-time.After(500 * time.Millisecond):
		t.Fatal("done handler was not called for Escape")
	}
}

func TestDialog_LongTextWordWraps(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()

	pages := tview.NewPages()
	pages.AddPage("main", tview.NewBox(), true, true)
	runner.Start(pages)
	runner.Draw()

	longText := strings.Repeat("word ", 30) + "FINAL-MARKER"
	runner.QueueUpdateDraw(func() {
		dialog := NewDialog().
			SetText(longText).
			AddButtons([]string{"OK"}).
			SetDoneFunc(func(_ int) {})
		pages.AddPage("dialog", dialog, true, true)
		runner.App().SetFocus(dialog)
	})

	assert.True(t, runner.WaitForText("FINAL-MARKER", 500*time.Millisecond),
		"wrapped text should remain fully visible")
}

func TestDialog_SetTextUpdatesContent(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()

	pages := tview.NewPages()
	pages.AddPage("main", tview.NewBox(), true, true)
	runner.Start(pages)
	runner.Draw()

	var dialog *Dialog
	runner.QueueUpdateDraw(func() {
		dialog = NewDialog().
			SetText("Before").
			AddButtons([]string{"Cancel"}).
			SetDoneFunc(func(_ int) {})
		pages.AddPage("dialog", dialog, true, true)
		runner.App().SetFocus(dialog)
	})
	require.True(t, runner.WaitForText("Before", 500*time.Millisecond))

	runner.QueueUpdateDraw(func() {
		dialog.SetText("After the update")
	})
	assert.True(t, runner.WaitForText("After the update", 500*time.Millisecond))
	assert.False(t, runner.ContainsText("Before"), "old text should be gone")
}

func TestAuthLinkMessage(t *testing.T) {
	t.Parallel()

	message := authLinkMessage("https://zaparoo.com/link", "ABCD-1234")
	assert.Contains(t, message, "zaparoo.com/link")
	assert.NotContains(t, message, "https://", "URL scheme should be stripped for display")
	assert.Contains(t, message, "ABCD-1234")
	assert.Contains(t, message, "Waiting for approval")
}
