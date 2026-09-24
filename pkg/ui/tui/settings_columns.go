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
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// settingsColumnsMinWidth is the narrowest content width that still gives
// each column room for a label and its value. Narrower, the columns stack.
const settingsColumnsMinWidth = 60

// settingsColumnGap is the blank space kept on each side of the divider.
const settingsColumnGap = 1

// SettingsColumns lays out two settings lists side by side. Up/Down move
// within a column, and Left/Right switch columns unless the selected item
// uses them itself (a cycle). When the content area is too narrow the
// columns stack, left above right, and Up/Down run from one into the other.
type SettingsColumns struct {
	*tview.Box
	app           *tview.Application
	left          *SettingsList
	right         *SettingsList
	active        *SettingsList
	divider       *VerticalDivider
	onNavigateOut func()
	stacked       bool
}

// NewSettingsColumns wraps two fully built settings lists. The left column
// holds the initial selection and supplies the initial help text.
func NewSettingsColumns(app *tview.Application, left, right *SettingsList) *SettingsColumns {
	sc := &SettingsColumns{
		Box:     tview.NewBox(),
		app:     app,
		left:    left,
		right:   right,
		active:  left,
		divider: NewVerticalDivider(),
	}
	right.hasFocus = false
	right.refreshAllItems(-1)
	if current := right.GetCurrentItem(); !right.isSelectable(current) {
		if target := right.scanSelectable(current+1, 1); target != -1 {
			right.SetCurrentItem(target)
		}
	}

	for _, sl := range []*SettingsList{left, right} {
		list := sl
		list.onHorizontal = func(dir int) { sc.switchColumn(list, dir) }
		list.onEdge = func(dir int) bool { return sc.handleEdge(list, dir) }
		original := list.GetInputCapture()
		list.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
			if event.Key() == tcell.KeyTab && sc.onNavigateOut != nil {
				sc.onNavigateOut()
				return nil
			}
			if original != nil {
				return original(event)
			}
			return event
		})
	}
	left.TriggerInitialHelp()
	return sc
}

// SetOnNavigateOut sets the callback fired when Up/Down run past the top or
// bottom of a column, or Tab is pressed; pages point it at the button bar.
func (sc *SettingsColumns) SetOnNavigateOut(fn func()) *SettingsColumns {
	sc.onNavigateOut = fn
	return sc
}

// Left returns the left column's list.
func (sc *SettingsColumns) Left() *SettingsList { return sc.left }

// Right returns the right column's list.
func (sc *SettingsColumns) Right() *SettingsList { return sc.right }

// switchColumn moves the selection from one column to the other, landing on
// the item nearest the same screen row.
func (sc *SettingsColumns) switchColumn(from *SettingsList, dir int) {
	if sc.stacked {
		return
	}
	target := sc.right
	if dir < 0 {
		target = sc.left
	}
	if target == from {
		return
	}
	fromOffset, _ := from.GetOffset()
	targetOffset, _ := target.GetOffset()
	index := target.nearestSelectable(targetOffset + from.GetCurrentItem() - fromOffset)
	if index == -1 {
		return
	}
	target.SetCurrentItem(index)
	sc.focusList(target)
}

// handleEdge runs when Up/Down pass the first or last selectable item of a
// column. Stacked columns continue into each other; otherwise navigation
// leaves the columns.
func (sc *SettingsColumns) handleEdge(from *SettingsList, dir int) bool {
	if sc.stacked {
		if from == sc.left && dir > 0 {
			if index := sc.right.scanSelectable(0, 1); index != -1 {
				sc.right.SetCurrentItem(index)
				sc.focusList(sc.right)
				return true
			}
		}
		if from == sc.right && dir < 0 {
			if index := sc.left.scanSelectable(len(sc.left.items)-1, -1); index != -1 {
				sc.left.SetCurrentItem(index)
				sc.focusList(sc.left)
				return true
			}
		}
	}
	if sc.onNavigateOut != nil {
		sc.onNavigateOut()
		return true
	}
	return false
}

func (sc *SettingsColumns) focusList(list *SettingsList) {
	sc.active = list
	if sc.app != nil {
		sc.app.SetFocus(list.List)
	}
}

// FocusFirst selects the first item of the column that last had focus (the
// top column when stacked) and focuses it.
func (sc *SettingsColumns) FocusFirst() {
	list := sc.active
	if sc.stacked {
		list = sc.left
	}
	if index := list.scanSelectable(0, 1); index != -1 {
		list.SetCurrentItem(index)
	}
	sc.focusList(list)
}

// FocusLast selects the last item of the column that last had focus (the
// bottom column when stacked) and focuses it.
func (sc *SettingsColumns) FocusLast() {
	list := sc.active
	if sc.stacked {
		list = sc.right
	}
	if index := list.scanSelectable(len(list.items)-1, -1); index != -1 {
		list.SetCurrentItem(index)
	}
	sc.focusList(list)
}

// Draw lays the columns out for the current width and draws them.
func (sc *SettingsColumns) Draw(screen tcell.Screen) {
	sc.DrawForSubclass(screen, sc)
	x, y, width, height := sc.GetInnerRect()
	if width <= 0 || height <= 0 {
		return
	}
	sc.stacked = width < settingsColumnsMinWidth

	if sc.stacked {
		leftHeight := min(sc.left.GetItemCount(), height)
		if sc.active == sc.right {
			// Keep the focused column on screen when both do not fit.
			leftHeight = min(leftHeight, height-min(sc.right.GetItemCount(), height))
		}
		sc.left.SetRect(x, y, width, leftHeight)
		sc.right.SetRect(x, y+leftHeight, width, height-leftHeight)
		if leftHeight > 0 {
			sc.left.Draw(screen)
		}
		if height-leftHeight > 0 {
			sc.right.Draw(screen)
		}
		return
	}

	leftWidth := (width - 1 - 2*settingsColumnGap) / 2
	dividerX := x + leftWidth + settingsColumnGap
	rightX := dividerX + 1 + settingsColumnGap
	sc.left.SetRect(x, y, leftWidth, height)
	sc.divider.SetRect(dividerX, y, 1, height)
	sc.right.SetRect(rightX, y, x+width-rightX, height)
	sc.left.Draw(screen)
	sc.divider.Draw(screen)
	sc.right.Draw(screen)
}

// Focus implements tview.Primitive.
func (sc *SettingsColumns) Focus(delegate func(p tview.Primitive)) {
	delegate(sc.active.List)
}

// HasFocus implements tview.Primitive.
func (sc *SettingsColumns) HasFocus() bool {
	return sc.left.HasFocus() || sc.right.HasFocus()
}

// InputHandler implements tview.Primitive.
func (sc *SettingsColumns) InputHandler() func(event *tcell.EventKey, setFocus func(p tview.Primitive)) {
	return sc.WrapInputHandler(func(event *tcell.EventKey, setFocus func(p tview.Primitive)) {
		if sc.right.HasFocus() {
			sc.active = sc.right
		} else if sc.left.HasFocus() {
			sc.active = sc.left
		}
		if handler := sc.active.InputHandler(); handler != nil {
			handler(event, setFocus)
		}
	})
}

// MouseHandler implements tview.Primitive.
func (sc *SettingsColumns) MouseHandler() func(
	action tview.MouseAction,
	event *tcell.EventMouse,
	setFocus func(p tview.Primitive),
) (consumed bool, capture tview.Primitive) {
	return sc.WrapMouseHandler(func(
		action tview.MouseAction,
		event *tcell.EventMouse,
		setFocus func(p tview.Primitive),
	) (consumed bool, capture tview.Primitive) {
		for _, list := range []*SettingsList{sc.left, sc.right} {
			if !list.InRect(event.Position()) {
				continue
			}
			consumed, capture = list.MouseHandler()(action, event, setFocus)
			if consumed && action == tview.MouseLeftClick {
				sc.active = list
			}
			return consumed, capture
		}
		return false, nil
	})
}
