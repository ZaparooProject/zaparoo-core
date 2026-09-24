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

	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// columnsFixture builds a page frame holding two settings columns and a
// one-button bar.
type columnsFixture struct {
	frame   *PageFrame
	columns *SettingsColumns
	toggle  *bool
	cycle   *int
	pressed chan string
}

func newColumnsFixture(runner *TestAppRunner) *columnsFixture {
	app := runner.App()
	pages := tview.NewPages()
	fixture := &columnsFixture{toggle: new(bool), cycle: new(int), pressed: make(chan string, 4)}
	fixture.frame = NewPageFrame(app).SetTitle("Columns")
	bar := NewButtonBar(app).AddButton("Back", func() { fixture.pressed <- "Back" }).SetupNavigation(nil)
	fixture.frame.SetButtonBar(bar)

	newColumn := func() *SettingsList {
		return NewSettingsList(pages, "main").SetDynamicHelpMode(true).
			SetHelpCallback(func(desc string) { fixture.frame.SetHelpText(desc) })
	}
	left := newColumn()
	left.AddHeader("Left").
		AddNavAction("Left one", "left one help", func() { fixture.pressed <- "Left one" }).
		AddToggle("Left toggle", "left toggle help", fixture.toggle, func(bool) {}).
		AddNavAction("Left three", "left three help", func() { fixture.pressed <- "Left three" })
	right := newColumn()
	right.AddHeader("Right").
		AddNavAction("Right one", "right one help", func() { fixture.pressed <- "Right one" }).
		AddCycle("Right cycle", "right cycle help", []string{"A", "B", "C"}, fixture.cycle, nil)

	fixture.columns = NewSettingsColumns(app, left, right)
	fixture.frame.SetContent(fixture.columns)
	fixture.frame.SetupContentToButtonNavigation()
	pages.AddPage("columns", fixture.frame, true, true)
	runner.Start(pages)
	runner.Draw()
	return fixture
}

func (f *columnsFixture) waitPressed(t *testing.T, runner *TestAppRunner, want string) {
	t.Helper()
	select {
	case got := <-f.pressed:
		assert.Equal(t, want, got)
	default:
		require.True(t, runner.WaitForCondition(func() bool { return len(f.pressed) > 0 }, uiSettleTimeout))
		assert.Equal(t, want, <-f.pressed)
	}
}

// sharesLine reports whether a and b are drawn on the same screen line.
func sharesLine(runner *TestAppRunner, a, b string) bool {
	for _, line := range strings.Split(runner.GetScreenText(), "\n") {
		if strings.Contains(line, a) && strings.Contains(line, b) {
			return true
		}
	}
	return false
}

func TestSettingsColumns_SideBySide_Integration(t *testing.T) {
	t.Parallel()

	for _, size := range [][2]int{{80, 25}, {75, 15}} {
		runner := NewTestAppRunner(t, size[0], size[1])
		newColumnsFixture(runner)
		require.True(t, runner.WaitForText("left one help", uiSettleTimeout))
		assert.True(t, sharesLine(runner, "Left one", "Right one"),
			"size %v: first rows of both columns share a line", size)
		runner.Stop()
	}
}

func TestSettingsColumns_LeftRightSwitchColumns_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()
	fixture := newColumnsFixture(runner)

	// On a toggle, Right switches columns and leaves the toggle alone.
	runner.SimulateArrowDown()
	require.True(t, runner.WaitForText("left toggle help", uiSettleTimeout))
	runner.SimulateArrowRight()
	require.True(t, runner.WaitForText("right cycle help", uiSettleTimeout), "lands on the same row")
	assert.False(t, *fixture.toggle)

	// On a cycle, Left/Right step the value instead of switching.
	runner.SimulateArrowRight()
	assert.Equal(t, 1, *fixture.cycle)
	runner.SimulateArrowLeft()
	assert.Equal(t, 0, *fixture.cycle)
	assert.True(t, runner.ContainsText("right cycle help"))

	// From a nav row, Left goes back; Right in the right column stays.
	runner.SimulateArrowUp()
	runner.SimulateArrowRight()
	runner.SimulateEnter()
	fixture.waitPressed(t, runner, "Right one")
	runner.SimulateArrowLeft()
	runner.SimulateEnter()
	fixture.waitPressed(t, runner, "Left one")
}

func TestSettingsColumns_ButtonBarReturnsToSameColumn_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()
	fixture := newColumnsFixture(runner)

	runner.SimulateArrowRight()
	runner.SimulateArrowDown()
	runner.SimulateArrowDown()
	require.True(t, runner.WaitForCondition(func() bool {
		var focused bool
		runner.QueueUpdateDraw(func() { focused = fixture.frame.GetButtonBar().HasFocus() })
		return focused
	}, uiSettleTimeout), "Down past the last row moves to the button bar")

	// Up from the bar comes back to the bottom of the right column.
	runner.SimulateArrowUp()
	require.True(t, runner.WaitForText("right cycle help", uiSettleTimeout))

	// Tab also leaves the columns; Down from the bar wraps to the top of
	// the column that had focus.
	runner.SimulateTab()
	runner.SimulateArrowDown()
	runner.SimulateEnter()
	fixture.waitPressed(t, runner, "Right one")
}

func TestSettingsColumns_StackWhenNarrow_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 50, 25)
	defer runner.Stop()
	fixture := newColumnsFixture(runner)

	require.True(t, runner.WaitForText("Right one", uiSettleTimeout))
	assert.False(t, sharesLine(runner, "Left one", "Right one"), "narrow layouts stack the columns")

	// Left/Right do nothing extra; Down runs from the left column into the
	// right one.
	runner.SimulateArrowRight()
	require.True(t, runner.WaitForText("left one help", uiSettleTimeout))
	runner.SimulateArrowDown()
	runner.SimulateArrowDown()
	runner.SimulateArrowDown()
	runner.SimulateEnter()
	fixture.waitPressed(t, runner, "Right one")
	runner.SimulateArrowUp()
	runner.SimulateEnter()
	fixture.waitPressed(t, runner, "Left three")
}
