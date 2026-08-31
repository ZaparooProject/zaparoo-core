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

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

const (
	// dialogChromeWidth is the border plus one column of padding per side.
	dialogChromeWidth = 4
	// dialogWindowMargin keeps at least one column visible around a dialog
	// on each side of the parent window.
	dialogWindowMargin = 2
)

// Dialog is a modal dialog box sized to fit its content and clamped to the
// window it is drawn in. tview.Modal is deliberately not used for dialogs:
// it sizes and centers itself against the physical terminal, so in CRT mode
// (or any capped window) it draws wider than the TUI window itself.
type Dialog struct {
	*tview.Box
	frame    *tview.Box
	textView *tview.TextView
	form     *tview.Form
	done     func(buttonIndex int)
	text     string
	buttons  []string
}

// NewDialog returns an empty dialog. Add a message with SetText, buttons
// with AddButtons, and a close handler with SetDoneFunc. Add it to pages
// with resize enabled so it can center itself within the parent window.
func NewDialog() *Dialog {
	d := &Dialog{
		Box: tview.NewBox(),
		frame: tview.NewBox().
			SetBackgroundColor(tview.Styles.ContrastBackgroundColor),
		textView: tview.NewTextView().
			SetDynamicColors(true).
			SetWrap(true).
			SetWordWrap(true).
			SetTextAlign(tview.AlignCenter),
		form: tview.NewForm().
			SetButtonsAlign(tview.AlignCenter).
			SetButtonBackgroundColor(tview.Styles.PrimitiveBackgroundColor).
			SetButtonTextColor(tview.Styles.PrimaryTextColor),
	}
	d.frame.SetBorder(true).SetTitleAlign(tview.AlignCenter)
	d.textView.SetBackgroundColor(tview.Styles.ContrastBackgroundColor)
	d.form.SetBackgroundColor(tview.Styles.ContrastBackgroundColor)
	d.form.SetBorderPadding(0, 0, 0, 0)
	d.form.SetCancelFunc(func() {
		if d.done != nil {
			d.done(-1)
		}
	})
	return d
}

// SetText sets the message text. It may contain line breaks and tview color
// tags; long lines word-wrap to the available width.
func (d *Dialog) SetText(text string) *Dialog {
	d.text = text
	d.textView.SetText(text)
	return d
}

// SetTextAlign sets the horizontal alignment of the message text.
func (d *Dialog) SetTextAlign(align int) *Dialog {
	d.textView.SetTextAlign(align)
	return d
}

// SetTitle sets the dialog title, padded consistently with SetBoxTitle.
func (d *Dialog) SetTitle(title string) *Dialog {
	d.frame.SetTitle(" " + title + " ")
	return d
}

// AddButtons adds buttons to the dialog. Left/right and up/down move
// between buttons.
func (d *Dialog) AddButtons(labels []string) *Dialog {
	for index, label := range labels {
		i := index
		d.buttons = append(d.buttons, label)
		d.form.AddButton(label, func() {
			if d.done != nil {
				d.done(i)
			}
		})
		button := d.form.GetButton(d.form.GetButtonCount() - 1)
		button.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
			switch event.Key() {
			case tcell.KeyDown, tcell.KeyRight:
				return tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone)
			case tcell.KeyUp, tcell.KeyLeft:
				return tcell.NewEventKey(tcell.KeyBacktab, 0, tcell.ModNone)
			default:
				return event
			}
		})
	}
	return d
}

// SetDoneFunc sets the handler called with the pressed button's index. It
// is also called with index -1 when the user presses Escape.
func (d *Dialog) SetDoneFunc(handler func(buttonIndex int)) *Dialog {
	d.done = handler
	return d
}

// contentWidth returns the widest element the dialog wants to display
// without wrapping: text line, button row, or title.
func (d *Dialog) contentWidth() int {
	width := 0
	for _, label := range d.buttons {
		width += tview.TaggedStringWidth(label) + 6
	}
	if width > 0 {
		width -= 2
	}
	for _, line := range strings.Split(d.text, "\n") {
		if lineWidth := tview.TaggedStringWidth(line); lineWidth > width {
			width = lineWidth
		}
	}
	if titleWidth := tview.TaggedStringWidth(d.frame.GetTitle()); titleWidth > width {
		width = titleWidth
	}
	return width
}

// Draw implements tview.Primitive. The dialog rect covers the whole parent
// window; only the centered frame is drawn so the page behind stays visible.
func (d *Dialog) Draw(screen tcell.Screen) {
	x, y, w, h := d.GetInnerRect()
	if w < 1 || h < 1 {
		return
	}

	width := d.contentWidth() + dialogChromeWidth
	if maxWidth := w - dialogWindowMargin; width > maxWidth {
		width = maxWidth
	}
	textWidth := width - dialogChromeWidth
	if textWidth < 1 {
		textWidth = 1
		width = textWidth + dialogChromeWidth
	}

	buttonRows := 0
	if len(d.buttons) > 0 {
		buttonRows = 2 // blank spacer + button row
	}
	height := len(tview.WordWrap(d.text, textWidth)) + buttonRows + 2
	if height > h {
		height = h
	}

	d.frame.SetRect(x+(w-width)/2, y+(h-height)/2, width, height)
	d.frame.Draw(screen)

	innerX, innerY, innerWidth, innerHeight := d.frame.GetInnerRect()
	if textHeight := innerHeight - buttonRows; textHeight > 0 {
		d.textView.SetRect(innerX+1, innerY, innerWidth-2, textHeight)
		d.textView.Draw(screen)
	}
	if buttonRows > 0 && innerHeight > 0 {
		d.form.SetRect(innerX, innerY+innerHeight-1, innerWidth, 1)
		d.form.Draw(screen)
	}
}

// Focus implements tview.Primitive.
func (d *Dialog) Focus(delegate func(p tview.Primitive)) {
	if len(d.buttons) > 0 {
		delegate(d.form)
		return
	}
	d.Box.Focus(delegate)
}

// HasFocus implements tview.Primitive.
func (d *Dialog) HasFocus() bool {
	return d.form.HasFocus() || d.Box.HasFocus()
}

// InputHandler implements tview.Primitive.
func (d *Dialog) InputHandler() func(event *tcell.EventKey, setFocus func(p tview.Primitive)) {
	return d.WrapInputHandler(func(event *tcell.EventKey, setFocus func(p tview.Primitive)) {
		if d.form.HasFocus() {
			if handler := d.form.InputHandler(); handler != nil {
				handler(event, setFocus)
			}
		}
	})
}

// MouseHandler implements tview.Primitive.
func (d *Dialog) MouseHandler() func(
	action tview.MouseAction, event *tcell.EventMouse, setFocus func(p tview.Primitive),
) (consumed bool, capture tview.Primitive) {
	return d.WrapMouseHandler(func(
		action tview.MouseAction, event *tcell.EventMouse, setFocus func(p tview.Primitive),
	) (consumed bool, capture tview.Primitive) {
		consumed, capture = d.form.MouseHandler()(action, event, setFocus)
		if !consumed && action == tview.MouseLeftDown && d.InRect(event.Position()) {
			setFocus(d)
			consumed = true
		}
		return consumed, capture
	})
}
