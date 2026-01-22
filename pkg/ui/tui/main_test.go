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
	"sync"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
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

func TestButtonGrid_FocusFirst(t *testing.T) {
	t.Parallel()

	app := tview.NewApplication()
	grid := NewButtonGrid(app, 3)

	btn1 := &ButtonGridItem{Button: tview.NewButton("Btn1"), HelpText: "Help 1"}
	btn2 := &ButtonGridItem{Button: tview.NewButton("Btn2"), HelpText: "Help 2"}
	btn3 := &ButtonGridItem{Button: tview.NewButton("Btn3"), HelpText: "Help 3"}

	grid.AddRow(btn1, btn2, btn3)

	grid.FocusFirst()

	row, col := grid.GetFocus()
	assert.Equal(t, 0, row, "Should focus first row")
	assert.Equal(t, 0, col, "Should focus first column")
}

func TestButtonGrid_FocusFirst_SkipsDisabled(t *testing.T) {
	t.Parallel()

	app := tview.NewApplication()
	grid := NewButtonGrid(app, 3)

	// First button is disabled
	btn1 := &ButtonGridItem{Button: tview.NewButton("Btn1"), HelpText: "Help 1", Disabled: true}
	btn2 := &ButtonGridItem{Button: tview.NewButton("Btn2"), HelpText: "Help 2"}
	btn3 := &ButtonGridItem{Button: tview.NewButton("Btn3"), HelpText: "Help 3"}

	grid.AddRow(btn1, btn2, btn3)

	grid.FocusFirst()

	row, col := grid.GetFocus()
	assert.Equal(t, 0, row, "Should focus first row")
	assert.Equal(t, 1, col, "Should skip disabled and focus second column")
}

func TestButtonGrid_SetFocus(t *testing.T) {
	t.Parallel()

	app := tview.NewApplication()
	grid := NewButtonGrid(app, 3)

	btn1 := &ButtonGridItem{Button: tview.NewButton("Btn1"), HelpText: "Help 1"}
	btn2 := &ButtonGridItem{Button: tview.NewButton("Btn2"), HelpText: "Help 2"}
	btn3 := &ButtonGridItem{Button: tview.NewButton("Btn3"), HelpText: "Help 3"}

	grid.AddRow(btn1, btn2, btn3)

	grid.SetFocus(0, 2)

	row, col := grid.GetFocus()
	assert.Equal(t, 0, row)
	assert.Equal(t, 2, col)
}

func TestButtonGrid_SetFocus_FallsBackOnDisabled(t *testing.T) {
	t.Parallel()

	app := tview.NewApplication()
	grid := NewButtonGrid(app, 3)

	btn1 := &ButtonGridItem{Button: tview.NewButton("Btn1"), HelpText: "Help 1"}
	btn2 := &ButtonGridItem{Button: tview.NewButton("Btn2"), HelpText: "Help 2", Disabled: true}
	btn3 := &ButtonGridItem{Button: tview.NewButton("Btn3"), HelpText: "Help 3"}

	grid.AddRow(btn1, btn2, btn3)

	// Try to set focus on disabled button
	grid.SetFocus(0, 1)

	// Should fall back to first enabled
	row, col := grid.GetFocus()
	assert.Equal(t, 0, row)
	assert.Equal(t, 0, col, "Should fall back to first enabled button")
}

func TestButtonGrid_isCurrentEnabled(t *testing.T) {
	t.Parallel()

	app := tview.NewApplication()
	grid := NewButtonGrid(app, 3)

	btn1 := &ButtonGridItem{Button: tview.NewButton("Btn1"), HelpText: "Help 1"}
	btn2 := &ButtonGridItem{Button: tview.NewButton("Btn2"), HelpText: "Help 2", Disabled: true}

	grid.AddRow(btn1, btn2)

	grid.focusedRow = 0
	grid.focusedCol = 0
	assert.True(t, grid.isCurrentEnabled(), "First button should be enabled")

	grid.focusedCol = 1
	assert.False(t, grid.isCurrentEnabled(), "Second button should be disabled")
}

func TestButtonGrid_HelpCallback_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()

	app := runner.App()
	grid := NewButtonGrid(app, 3)

	var helpMu syncutil.Mutex
	var helpTexts []string
	grid.SetOnHelp(func(text string) {
		helpMu.Lock()
		helpTexts = append(helpTexts, text)
		helpMu.Unlock()
	})

	btn1 := &ButtonGridItem{Button: tview.NewButton("Btn1"), HelpText: "Help for button 1"}
	btn2 := &ButtonGridItem{Button: tview.NewButton("Btn2"), HelpText: "Help for button 2"}
	btn3 := &ButtonGridItem{Button: tview.NewButton("Btn3"), HelpText: "Help for button 3"}

	grid.AddRow(btn1, btn2, btn3)

	runner.Start(grid)
	runner.SetFocus(grid)

	// Helper to safely get helpTexts
	getHelpTexts := func() []string {
		helpMu.Lock()
		defer helpMu.Unlock()
		return append([]string(nil), helpTexts...)
	}

	// Helper to check if text is in help texts
	containsHelpText := func(text string) bool {
		for _, h := range getHelpTexts() {
			if h == text {
				return true
			}
		}
		return false
	}

	// Should trigger help for first button on focus
	assert.True(t, runner.WaitForCondition(func() bool {
		return containsHelpText("Help for button 1")
	}, 100*time.Millisecond), "Should receive help for first button")

	// Navigate right
	runner.Screen().InjectArrowRight()
	runner.Draw()

	assert.True(t, runner.WaitForCondition(func() bool {
		return containsHelpText("Help for button 2")
	}, 100*time.Millisecond), "Should receive help for second button")
}

func TestButtonGrid_Navigation_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()

	app := runner.App()
	grid := NewButtonGrid(app, 3)

	btn1 := &ButtonGridItem{Button: tview.NewButton("Btn1"), HelpText: "Help 1"}
	btn2 := &ButtonGridItem{Button: tview.NewButton("Btn2"), HelpText: "Help 2"}
	btn3 := &ButtonGridItem{Button: tview.NewButton("Btn3"), HelpText: "Help 3"}
	btn4 := &ButtonGridItem{Button: tview.NewButton("Btn4"), HelpText: "Help 4"}
	btn5 := &ButtonGridItem{Button: tview.NewButton("Btn5"), HelpText: "Help 5"}

	grid.AddRow(btn1, btn2, btn3)
	grid.AddRow(btn4, btn5, nil)

	runner.Start(grid)
	runner.SetFocus(grid)

	// Get current focus helper
	getFocus := func() (int, int) {
		var row, col int
		runner.QueueUpdateDraw(func() {
			row, col = grid.GetFocus()
		})
		return row, col
	}

	// Wait for focus to match expected position
	waitForFocus := func(expectedRow, expectedCol int) bool {
		return runner.WaitForCondition(func() bool {
			row, col := getFocus()
			return row == expectedRow && col == expectedCol
		}, 100*time.Millisecond)
	}

	// Initial focus should be (0, 0)
	assert.True(t, waitForFocus(0, 0), "Initial focus should be (0, 0)")

	// Navigate right
	runner.Screen().InjectArrowRight()
	runner.Draw()
	assert.True(t, waitForFocus(0, 1), "Focus should be (0, 1) after right")

	// Navigate down
	runner.Screen().InjectArrowDown()
	runner.Draw()
	assert.True(t, waitForFocus(1, 1), "Focus should be (1, 1) after down")

	// Navigate left
	runner.Screen().InjectArrowLeft()
	runner.Draw()
	assert.True(t, waitForFocus(1, 0), "Focus should be (1, 0) after left")

	// Navigate up
	runner.Screen().InjectArrowUp()
	runner.Draw()
	assert.True(t, waitForFocus(0, 0), "Focus should be (0, 0) after up")
}

func TestButtonGrid_TabNavigation_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()

	app := runner.App()
	grid := NewButtonGrid(app, 3)

	btn1 := &ButtonGridItem{Button: tview.NewButton("Btn1"), HelpText: "Help 1"}
	btn2 := &ButtonGridItem{Button: tview.NewButton("Btn2"), HelpText: "Help 2"}
	btn3 := &ButtonGridItem{Button: tview.NewButton("Btn3"), HelpText: "Help 3"}

	grid.AddRow(btn1, btn2, btn3)

	runner.Start(grid)
	runner.SetFocus(grid)

	getFocus := func() (int, int) {
		var row, col int
		runner.QueueUpdateDraw(func() {
			row, col = grid.GetFocus()
		})
		return row, col
	}

	// Tab should navigate right
	runner.Screen().InjectTab()
	runner.Draw()
	row, col := getFocus()
	assert.Equal(t, 0, row)
	assert.Equal(t, 1, col)

	// Backtab should navigate left
	runner.Screen().InjectBacktab()
	runner.Draw()
	row, col = getFocus()
	assert.Equal(t, 0, row)
	assert.Equal(t, 0, col)
}

func TestButtonGrid_EscapeCallback_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()

	app := runner.App()
	grid := NewButtonGrid(app, 3)

	escapeCalled := make(chan struct{}, 1)
	grid.SetOnEscape(func() {
		select {
		case escapeCalled <- struct{}{}:
		default:
		}
	})

	btn1 := &ButtonGridItem{Button: tview.NewButton("Btn1"), HelpText: "Help 1"}
	grid.AddRow(btn1)

	runner.Start(grid)
	runner.SetFocus(grid)

	runner.Screen().InjectEscape()
	runner.Draw()

	assert.True(t, runner.WaitForSignal(escapeCalled, 100*time.Millisecond), "Escape callback should be called")
}

func TestButtonGrid_EnterActivatesButton_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()

	app := runner.App()
	grid := NewButtonGrid(app, 3)

	buttonPressed := make(chan struct{}, 1)
	btn1 := &ButtonGridItem{
		Button: tview.NewButton("Btn1").SetSelectedFunc(func() {
			select {
			case buttonPressed <- struct{}{}:
			default:
			}
		}),
		HelpText: "Help 1",
	}
	grid.AddRow(btn1)

	runner.Start(grid)
	runner.SetFocus(grid)

	runner.Screen().InjectEnter()
	runner.Draw()

	assert.True(t, runner.WaitForSignal(buttonPressed, 100*time.Millisecond), "Button should be activated on Enter")
}

func TestButtonGrid_DisabledButtonsSkipped_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()

	app := runner.App()
	grid := NewButtonGrid(app, 3)

	btn1 := &ButtonGridItem{Button: tview.NewButton("Btn1"), HelpText: "Help 1"}
	btn2 := &ButtonGridItem{Button: tview.NewButton("Btn2"), HelpText: "Help 2", Disabled: true}
	btn3 := &ButtonGridItem{Button: tview.NewButton("Btn3"), HelpText: "Help 3"}

	grid.AddRow(btn1, btn2, btn3)

	runner.Start(grid)
	runner.SetFocus(grid)

	getFocus := func() (int, int) {
		var row, col int
		runner.QueueUpdateDraw(func() {
			row, col = grid.GetFocus()
		})
		return row, col
	}

	// Initial focus (0, 0)
	_, col := getFocus()
	assert.Equal(t, 0, col)

	// Navigate right - should skip disabled btn2 and go to btn3
	runner.Screen().InjectArrowRight()
	runner.Draw()
	row, col := getFocus()
	assert.Equal(t, 0, row)
	assert.Equal(t, 2, col, "Should skip disabled button and go to third")
}

func TestMainFrame_Delegates(t *testing.T) {
	t.Parallel()

	content := tview.NewTextView().SetText("Test content")
	frame := NewMainFrame(content)

	assert.NotNil(t, frame.content)

	// InputHandler should delegate to content
	handler := frame.InputHandler()
	assert.NotNil(t, handler)
}

func TestMainFrame_HasFocus(t *testing.T) {
	t.Parallel()

	content := tview.NewTextView()
	frame := NewMainFrame(content)

	// HasFocus should delegate to content
	hasFocus := frame.HasFocus()
	assert.False(t, hasFocus, "Content should not have focus initially")
}

func TestMainFrame_NilContent(t *testing.T) {
	t.Parallel()

	frame := NewMainFrame(nil)

	// Should handle nil content gracefully
	handler := frame.InputHandler()
	assert.Nil(t, handler)

	hasFocus := frame.HasFocus()
	assert.False(t, hasFocus)
}

func TestMainFrame_MouseHandler(t *testing.T) {
	t.Parallel()

	content := tview.NewTextView()
	frame := NewMainFrame(content)

	handler := frame.MouseHandler()
	assert.NotNil(t, handler)
}

func TestButtonGrid_WrapAround_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()

	app := runner.App()
	grid := NewButtonGrid(app, 3)

	btn1 := &ButtonGridItem{Button: tview.NewButton("Btn1"), HelpText: "Help 1"}
	btn2 := &ButtonGridItem{Button: tview.NewButton("Btn2"), HelpText: "Help 2"}
	btn3 := &ButtonGridItem{Button: tview.NewButton("Btn3"), HelpText: "Help 3"}

	grid.AddRow(btn1, btn2, btn3)

	runner.Start(grid)
	runner.SetFocus(grid)

	getFocus := func() (int, int) {
		var row, col int
		runner.QueueUpdateDraw(func() {
			row, col = grid.GetFocus()
		})
		return row, col
	}

	// Wait for focus to match expected position
	waitForFocus := func(expectedRow, expectedCol int, msg string) {
		ok := runner.WaitForCondition(func() bool {
			row, col := getFocus()
			return row == expectedRow && col == expectedCol
		}, 100*time.Millisecond)
		assert.True(t, ok, msg)
	}

	// Navigate to the last column
	runner.Screen().InjectArrowRight()
	runner.Draw()
	waitForFocus(0, 1, "Should be at column 1 after first right")

	runner.Screen().InjectArrowRight()
	runner.Draw()
	waitForFocus(0, 2, "Should be at last column")

	// Navigate right again - should wrap to first
	runner.Screen().InjectArrowRight()
	runner.Draw()
	waitForFocus(0, 0, "Should wrap to first column")
}
