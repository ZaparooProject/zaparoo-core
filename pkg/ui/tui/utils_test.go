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

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCenterWidget(t *testing.T) {
	t.Parallel()

	textView := tview.NewTextView().SetText("Centered content")
	centered := CenterWidget(40, 10, textView)

	require.NotNil(t, centered)

	// Verify it's a Flex
	flex, ok := centered.(*tview.Flex)
	require.True(t, ok, "CenterWidget should return a Flex")
	assert.NotNil(t, flex)
}

func TestCenterWidget_DifferentSizes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		width  int
		height int
	}{
		{"small", 20, 5},
		{"medium", 60, 20},
		{"large", 100, 40},
		{"tall and narrow", 20, 50},
		{"wide and short", 80, 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			box := tview.NewBox()
			centered := CenterWidget(tt.width, tt.height, box)
			require.NotNil(t, centered)
		})
	}
}

func TestPageDefaults(t *testing.T) {
	t.Parallel()

	pages := tview.NewPages()
	textView := tview.NewTextView().SetText("Test content")

	result := pageDefaults("testPage", pages, textView)

	require.NotNil(t, result)

	// Verify page was added
	assert.True(t, pages.HasPage("testPage"))

	// Verify it's the front page
	name, _ := pages.GetFrontPage()
	assert.Equal(t, "testPage", name)
}

func TestPageDefaults_MultiplePAges(t *testing.T) {
	t.Parallel()

	pages := tview.NewPages()

	// Add first page
	tv1 := tview.NewTextView().SetText("Page 1")
	pageDefaults("page1", pages, tv1)

	// Add second page
	tv2 := tview.NewTextView().SetText("Page 2")
	pageDefaults("page2", pages, tv2)

	// Second page should be front
	name, _ := pages.GetFrontPage()
	assert.Equal(t, "page2", name)

	// Both pages should exist
	assert.True(t, pages.HasPage("page1"))
	assert.True(t, pages.HasPage("page2"))
}

func TestCurrentTheme(t *testing.T) {
	t.Parallel()

	theme := CurrentTheme()

	require.NotNil(t, theme)
	assert.NotEmpty(t, theme.Name)
	assert.NotEmpty(t, theme.DisplayName)
	assert.NotEmpty(t, theme.BgColorName)
	assert.NotEmpty(t, theme.AccentColorName)
	assert.NotEmpty(t, theme.TextColorName)
}

func TestSetCurrentTheme(t *testing.T) {
	// Not parallel - modifies global tview.Styles which races with widget creation in other tests

	// Setting a valid theme should return true
	ok := SetCurrentTheme("default")
	assert.True(t, ok)
	assert.Equal(t, "default", CurrentTheme().Name)

	// Setting an invalid theme should return false
	ok = SetCurrentTheme("nonexistent_theme")
	assert.False(t, ok)

	// Theme should still be the last valid one
	assert.Equal(t, "default", CurrentTheme().Name)
}

func TestAvailableThemes(t *testing.T) {
	t.Parallel()

	assert.NotEmpty(t, AvailableThemes)
	assert.NotEmpty(t, ThemeNames)

	// All names in ThemeNames should exist in AvailableThemes
	for _, name := range ThemeNames {
		theme, ok := AvailableThemes[name]
		assert.True(t, ok, "Theme %s should exist in AvailableThemes", name)
		assert.NotNil(t, theme)
		assert.Equal(t, name, theme.Name)
	}
}

func TestThemeDefaultColors(t *testing.T) {
	t.Parallel()

	theme := &ThemeDefault

	assert.Equal(t, tcell.ColorLightYellow, theme.BorderColor)
	assert.Equal(t, tcell.ColorWhite, theme.PrimaryTextColor)
	assert.Equal(t, tcell.ColorDarkBlue, theme.PrimitiveBackgroundColor)
	assert.Equal(t, tcell.ColorBlue, theme.ContrastBackgroundColor)
	assert.Equal(t, "darkblue", theme.BgColorName)
	assert.Equal(t, "yellow", theme.AccentColorName)
}

func TestExitDelayOptions(t *testing.T) {
	t.Parallel()

	// Verify the exit delay options are present
	assert.NotEmpty(t, ExitDelayOptions)

	// Verify first option is 0 seconds
	assert.Equal(t, "0 seconds", ExitDelayOptions[0].Label)
	assert.InDelta(t, 0, ExitDelayOptions[0].Value, 0.001)

	// Verify options contain expected values (label and value)
	expectedOptions := []struct {
		label string
		value float32
	}{
		{"0 seconds", 0},
		{"5 seconds", 5},
		{"10 seconds", 10},
	}
	for _, expected := range expectedOptions {
		found := false
		for _, opt := range ExitDelayOptions {
			if opt.Label == expected.label && opt.Value == expected.value {
				found = true
				break
			}
		}
		assert.True(t, found, "ExitDelayOptions should contain {%s, %f}", expected.label, expected.value)
	}
}

func TestResponsiveMaxWidget(t *testing.T) {
	t.Parallel()

	content := tview.NewTextView().SetText("Test content")
	wrapper := ResponsiveMaxWidget(100, 30, content)

	require.NotNil(t, wrapper)

	// Cast to responsiveWrapper to access internal state
	rw, ok := wrapper.(*responsiveWrapper)
	require.True(t, ok, "Should return a responsiveWrapper")
	assert.Equal(t, 100, rw.maxWidth)
	assert.Equal(t, 30, rw.maxHeight)
	assert.Equal(t, content, rw.child)
}

func TestResponsiveWrapper_Focus(t *testing.T) {
	t.Parallel()

	content := tview.NewTextView()
	w := ResponsiveMaxWidget(100, 30, content)
	wrapper, ok := w.(*responsiveWrapper)
	require.True(t, ok, "Should return a responsiveWrapper")

	// Focus should delegate to child
	var focusedPrimitive tview.Primitive
	wrapper.Focus(func(p tview.Primitive) {
		focusedPrimitive = p
	})

	assert.Equal(t, content, focusedPrimitive, "Focus should delegate to child")
}

func TestResponsiveWrapper_HasFocus(t *testing.T) {
	t.Parallel()

	content := tview.NewTextView()
	w := ResponsiveMaxWidget(100, 30, content)
	wrapper, ok := w.(*responsiveWrapper)
	require.True(t, ok, "Should return a responsiveWrapper")

	// HasFocus should delegate to child
	assert.False(t, wrapper.HasFocus(), "Content should not have focus initially")
}

func TestResponsiveWrapper_InputHandler(t *testing.T) {
	t.Parallel()

	content := tview.NewTextView()
	w := ResponsiveMaxWidget(100, 30, content)
	wrapper, ok := w.(*responsiveWrapper)
	require.True(t, ok, "Should return a responsiveWrapper")

	handler := wrapper.InputHandler()
	assert.NotNil(t, handler, "InputHandler should delegate to child")
}

func TestResponsiveWrapper_MouseHandler(t *testing.T) {
	t.Parallel()

	content := tview.NewTextView()
	w := ResponsiveMaxWidget(100, 30, content)
	wrapper, ok := w.(*responsiveWrapper)
	require.True(t, ok, "Should return a responsiveWrapper")

	handler := wrapper.MouseHandler()
	assert.NotNil(t, handler, "MouseHandler should delegate to child")
}

func TestColorToHex(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		expected string
		color    tcell.Color
	}{
		{
			name:     "black",
			color:    tcell.ColorBlack,
			expected: "#000000",
		},
		{
			name:     "white",
			color:    tcell.ColorWhite,
			expected: "#ffffff",
		},
		{
			name:     "red",
			color:    tcell.ColorRed,
			expected: "#ff0000",
		},
		{
			name:     "green",
			color:    tcell.ColorGreen,
			expected: "#008000",
		},
		{
			name:     "blue",
			color:    tcell.ColorBlue,
			expected: "#0000ff",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := colorToHex(tt.color)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestRgbToHex(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		expected string
		value    int32
	}{
		{name: "zero", value: 0, expected: "00"},
		{name: "max", value: 255, expected: "ff"},
		{name: "mid", value: 128, expected: "80"},
		{name: "low", value: 16, expected: "10"},
		{name: "single digit", value: 10, expected: "0a"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := rgbToHex(tt.value)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestNewLabel(t *testing.T) {
	t.Parallel()

	label := NewLabel("Test")

	require.NotNil(t, label)
	// The text should have a colon suffix
	assert.Equal(t, "Test:", label.GetText(true))
}

func TestSetInputLabel(t *testing.T) {
	t.Parallel()

	input := tview.NewInputField()
	result := SetInputLabel(input, "Name")

	require.NotNil(t, result)
	assert.Equal(t, input, result, "Should return same input for chaining")
	assert.Equal(t, "Name: ", input.GetLabel())
}

func TestFormatLabel(t *testing.T) {
	t.Parallel()

	result := FormatLabel("Status")

	// Should contain the label text with color markup
	assert.Contains(t, result, "Status:")
	assert.Contains(t, result, "[#")
	assert.Contains(t, result, "::b]") // bold markup
}

func TestSetBoxTitle(t *testing.T) {
	t.Parallel()

	box := tview.NewBox()
	SetBoxTitle(box, "Title")

	// The title should have padding
	assert.Equal(t, " Title ", box.GetTitle())
}

func TestTuiContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := tuiContext()
	defer cancel()

	require.NotNil(t, ctx)
	require.NotNil(t, cancel)

	// Context should have a deadline
	deadline, ok := ctx.Deadline()
	assert.True(t, ok, "tuiContext should have a deadline")
	assert.True(t, deadline.After(time.Now()), "Deadline should be in the future")
}

func TestTagReadContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := tagReadContext()
	defer cancel()

	require.NotNil(t, ctx)
	require.NotNil(t, cancel)

	// Context should have a deadline
	deadline, ok := ctx.Deadline()
	assert.True(t, ok, "tagReadContext should have a deadline")
	assert.True(t, deadline.After(time.Now()), "Deadline should be in the future")

	// Tag read context should have a longer timeout than TUI context
	tuiCtx, tuiCancel := tuiContext()
	defer tuiCancel()
	tuiDeadline, _ := tuiCtx.Deadline()

	assert.True(t, deadline.After(tuiDeadline), "Tag read timeout should be longer than TUI timeout")
}

func TestShowInfoModal_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()

	pages := tview.NewPages()
	mainPage := tview.NewTextView().SetText("Main Page")
	pages.AddPage("main", mainPage, true, true)

	runner.Start(pages)
	runner.Draw()

	// Show info modal
	runner.QueueUpdateDraw(func() {
		ShowInfoModal(pages, runner.App(), "Info", "This is information")
	})

	// Verify modal is shown
	require.True(t, runner.WaitForText("This is information", 500*time.Millisecond), "Modal message should appear")
	assert.True(t, runner.ContainsText("OK"), "OK button should be visible")
}

func TestShowErrorModal_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()

	pages := tview.NewPages()
	mainPage := tview.NewTextView().SetText("Main Page")
	pages.AddPage("main", mainPage, true, true)

	runner.Start(pages)
	runner.Draw()

	var dismissCalled atomic.Bool

	// Show error modal
	runner.QueueUpdateDraw(func() {
		ShowErrorModal(pages, runner.App(), "Something went wrong", func() {
			dismissCalled.Store(true)
		})
	})

	// Verify modal is shown
	require.True(t, runner.WaitForText("Something went wrong", 500*time.Millisecond), "Error message should appear")
	assert.True(t, runner.ContainsText("OK"), "OK button should be visible")

	// Dismiss the modal
	runner.Screen().InjectEnter()
	runner.Draw()
	time.Sleep(50 * time.Millisecond)

	assert.True(t, dismissCalled.Load(), "Dismiss callback should be called")
}

func TestShowConfirmModal_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()

	pages := tview.NewPages()
	mainPage := tview.NewTextView().SetText("Main Page")
	pages.AddPage("main", mainPage, true, true)

	runner.Start(pages)
	runner.Draw()

	var yesCalled, noCalled atomic.Bool

	// Show confirm modal
	runner.QueueUpdateDraw(func() {
		ShowConfirmModal(pages, runner.App(), "Are you sure?",
			func() { yesCalled.Store(true) },
			func() { noCalled.Store(true) },
		)
	})

	// Verify modal is shown
	require.True(t, runner.WaitForText("Are you sure?", 500*time.Millisecond), "Confirm message should appear")
	assert.True(t, runner.ContainsText("Yes"), "Yes button should be visible")
	assert.True(t, runner.ContainsText("No"), "No button should be visible")

	// Click Yes (first button, so just Enter)
	runner.Screen().InjectEnter()
	runner.Draw()
	time.Sleep(50 * time.Millisecond)

	assert.True(t, yesCalled.Load(), "Yes callback should be called")
	assert.False(t, noCalled.Load(), "No callback should not be called")
}

func TestShowConfirmModal_No_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()

	pages := tview.NewPages()
	mainPage := tview.NewTextView().SetText("Main Page")
	pages.AddPage("main", mainPage, true, true)

	runner.Start(pages)
	runner.Draw()

	var yesCalled, noCalled atomic.Bool

	// Show confirm modal
	runner.QueueUpdateDraw(func() {
		ShowConfirmModal(pages, runner.App(), "Are you sure?",
			func() { yesCalled.Store(true) },
			func() { noCalled.Store(true) },
		)
	})

	require.True(t, runner.WaitForText("Are you sure?", 500*time.Millisecond))

	// Navigate to No button and click
	runner.Screen().InjectTab()
	runner.Draw()
	runner.Screen().InjectEnter()
	runner.Draw()
	time.Sleep(50 * time.Millisecond)

	assert.False(t, yesCalled.Load(), "Yes callback should not be called")
	assert.True(t, noCalled.Load(), "No callback should be called")
}

func TestShowWaitingModal_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()

	pages := tview.NewPages()
	mainPage := tview.NewTextView().SetText("Main Page")
	pages.AddPage("main", mainPage, true, true)

	runner.Start(pages)
	runner.Draw()

	var cancelCalled atomic.Bool

	// Show waiting modal
	var cleanup func()
	runner.QueueUpdateDraw(func() {
		cleanup = ShowWaitingModal(pages, runner.App(), "Please wait...", func() {
			cancelCalled.Store(true)
		})
	})

	// Verify modal is shown
	require.True(t, runner.WaitForText("Please wait...", 500*time.Millisecond), "Waiting message should appear")
	assert.True(t, runner.ContainsText("Cancel"), "Cancel button should be visible")
	require.NotNil(t, cleanup)

	// Call cleanup to remove modal
	runner.QueueUpdateDraw(func() {
		cleanup()
	})
	time.Sleep(50 * time.Millisecond)

	// Cleanup should remove the modal without calling cancel
	assert.False(t, cancelCalled.Load(), "Cancel should not be called when cleanup is used")
}

func TestShowOSKModal_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()

	pages := tview.NewPages()
	mainPage := tview.NewTextView().SetText("Main Page")
	pages.AddPage("main", mainPage, true, true)

	runner.Start(pages)
	runner.Draw()

	var cancelCalled atomic.Bool

	// Show OSK modal
	runner.QueueUpdateDraw(func() {
		ShowOSKModal(pages, runner.App(), "initial",
			nil,
			func() { cancelCalled.Store(true) },
		)
	})

	// Verify OSK is shown - look for keyboard keys
	time.Sleep(100 * time.Millisecond)
	runner.Draw()

	// The OSK should be displayed (it has keys like "q", "w", etc.)
	// Press Escape to cancel
	runner.Screen().InjectEscape()
	runner.Draw()
	time.Sleep(50 * time.Millisecond)

	assert.True(t, cancelCalled.Load(), "Cancel callback should be called")
}

func TestTimeoutConstants(t *testing.T) {
	t.Parallel()

	// Verify timeout constants have sensible values
	assert.Equal(t, 5*time.Second, TUIRequestTimeout)
	assert.Equal(t, 30*time.Second, TagReadTimeout)

	// Tag read should be longer than TUI request (for user interaction)
	assert.Greater(t, TagReadTimeout, TUIRequestTimeout)
}

func TestDefaultDimensions(t *testing.T) {
	t.Parallel()

	// Verify default dimensions are reasonable
	assert.Equal(t, 100, DefaultMaxWidth)
	assert.Equal(t, 30, DefaultMaxHeight)

	// Width should be greater than height for typical terminal layouts
	assert.Greater(t, DefaultMaxWidth, DefaultMaxHeight)
}
