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

// showTextViewer shows read-only text on its own page. Up/Down and
// PgUp/PgDn scroll it; Tab, or Down once the end is on screen, moves to the
// Back button. The text is shown as is, without color tags, so brackets in
// license texts display literally.
func showTextViewer(
	pages *tview.Pages,
	app *tview.Application,
	pageName string,
	titles []string,
	text string,
	goBack func(),
) {
	frame := NewPageFrame(app).SetTitle(titles...)
	frame.SetOnEscape(goBack)

	view := tview.NewTextView().
		SetDynamicColors(false).
		SetScrollable(true).
		SetWrap(true).
		SetWordWrap(true)
	view.SetText(text).ScrollToBeginning()

	focusView := func() { app.SetFocus(view) }
	buttonBar := NewButtonBar(app).
		AddButton("Back", goBack).
		SetupNavigation(goBack).
		SetOnUp(focusView).
		SetOnDown(focusView)

	view.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyTab, tcell.KeyBacktab:
			frame.FocusButtonBar()
			return nil
		case tcell.KeyEscape:
			goBack()
			return nil
		case tcell.KeyDown:
			if textViewAtEnd(view) {
				frame.FocusButtonBar()
				return nil
			}
		default:
		}
		return event
	})

	frame.SetButtonBar(buttonBar)
	frame.SetHelpText("Up/Down and PgUp/PgDn to scroll")
	frame.SetContent(view)
	pages.AddAndSwitchToPage(pageName, frame, true)
	app.SetFocus(view)
}

// textViewAtEnd reports whether the last line of a scrollable text view is
// on screen.
func textViewAtEnd(view *tview.TextView) bool {
	row, _ := view.GetScrollOffset()
	_, _, _, height := view.GetInnerRect()
	return row+height >= view.GetWrappedLineCount()
}
