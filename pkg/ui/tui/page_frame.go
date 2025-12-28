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
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// PageFrame provides a consistent page structure with:
// - Breadcrumb title in top border
// - Main content area
// - Dynamic help text line
// - ButtonBar footer
// - Keyboard hints in bottom border
type PageFrame struct {
	content tview.Primitive
	*tview.Box
	helpText  *tview.TextView
	buttonBar *ButtonBar
	app       *tview.Application
	onEscape  func()
}

// defaultHintsRunes returns the standard keyboard hints as runes for drawing.
// Uses tcell arrow runes for terminal compatibility.
func defaultHintsRunes() []rune {
	return []rune{
		tcell.RuneLArrow, tcell.RuneUArrow, tcell.RuneDArrow, tcell.RuneRArrow,
		':', ' ', 'N', 'a', 'v', 'i', 'g', 'a', 't', 'e', ' ',
		tcell.RuneVLine, ' ',
		'E', 'n', 't', 'e', 'r', ':', ' ', 'S', 'e', 'l', 'e', 'c', 't', ' ',
		tcell.RuneVLine, ' ',
		'E', 'S', 'C', ':', ' ', 'B', 'a', 'c', 'k',
	}
}

// NewPageFrame creates a new page frame with the given application reference.
func NewPageFrame(app *tview.Application) *PageFrame {
	pf := &PageFrame{
		Box: tview.NewBox(),
		app: app,
	}
	pf.SetBorder(true)

	// Create help text view
	pf.helpText = tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignCenter)

	return pf
}

// SetTitle sets the page title using breadcrumb-style path segments.
// Example: SetTitle("Settings", "Readers", "Manage") displays " Settings > Readers > Manage "
func (pf *PageFrame) SetTitle(path ...string) *PageFrame {
	title := " " + strings.Join(path, " > ") + " "
	pf.Box.SetTitle(title)
	return pf
}

// SetContent sets the main content primitive.
func (pf *PageFrame) SetContent(content tview.Primitive) *PageFrame {
	pf.content = content
	return pf
}

// SetHelpText sets the dynamic help text displayed above the button bar.
func (pf *PageFrame) SetHelpText(text string) *PageFrame {
	pf.helpText.SetText(text)
	return pf
}

// SetButtonBar sets the button bar at the bottom of the frame.
// Automatically sets up Up/Down navigation to return focus to content.
func (pf *PageFrame) SetButtonBar(bar *ButtonBar) *PageFrame {
	pf.buttonBar = bar
	bar.SetOnUp(pf.FocusContent)
	return pf
}

// SetOnEscape sets the callback when ESC is pressed.
func (pf *PageFrame) SetOnEscape(fn func()) *PageFrame {
	pf.onEscape = fn
	return pf
}

// Draw renders the page frame with bottom border hints.
func (pf *PageFrame) Draw(screen tcell.Screen) {
	pf.DrawForSubclass(screen, pf)

	x, y, width, height := pf.GetInnerRect()
	if width <= 0 || height <= 0 {
		return
	}

	// Calculate layout heights
	helpHeight := 1
	buttonHeight := 0
	if pf.buttonBar != nil {
		buttonHeight = 1
	}

	contentHeight := height - helpHeight - buttonHeight
	if contentHeight < 1 {
		contentHeight = 1
	}

	// Draw content
	if pf.content != nil {
		pf.content.SetRect(x, y, width, contentHeight)
		pf.content.Draw(screen)
	}

	// Draw help text
	pf.helpText.SetRect(x, y+contentHeight, width, helpHeight)
	pf.helpText.Draw(screen)

	// Draw button bar
	if pf.buttonBar != nil {
		pf.buttonBar.SetRect(x, y+contentHeight+helpHeight, width, buttonHeight)
		pf.buttonBar.Draw(screen)
	}

	// Draw bottom border hints
	pf.drawBottomHints(screen)
}

// drawBottomHints renders the hints text in the bottom border.
func (pf *PageFrame) drawBottomHints(screen tcell.Screen) {
	outerX, outerY, outerWidth, outerHeight := pf.GetRect()
	if outerWidth <= 4 || outerHeight <= 2 {
		return
	}

	// Bottom border is at outerY + outerHeight - 1
	bottomY := outerY + outerHeight - 1

	// Get hints runes and calculate centering
	hints := defaultHintsRunes()
	availableWidth := outerWidth - 4 // Leave space for corners and padding
	if len(hints) > availableWidth {
		hints = hints[:availableWidth]
	}

	// Center the hints
	startX := outerX + (outerWidth-len(hints))/2

	// Get theme colors - use border color (same as title) on primitive background
	t := CurrentTheme()
	style := tcell.StyleDefault.
		Foreground(t.BorderColor).
		Background(t.PrimitiveBackgroundColor)

	// Clear just the text area with padding
	clearStart := startX - 1
	clearEnd := startX + len(hints) + 1
	for i := clearStart; i < clearEnd; i++ {
		screen.SetContent(i, bottomY, ' ', nil, style)
	}

	// Draw the hints
	for i, r := range hints {
		screen.SetContent(startX+i, bottomY, r, nil, style)
	}
}

// Focus implements tview.Primitive.
func (pf *PageFrame) Focus(delegate func(p tview.Primitive)) {
	// Focus the content first, or button bar if no content
	if pf.content != nil {
		delegate(pf.content)
	} else if pf.buttonBar != nil {
		delegate(pf.buttonBar.GetFirstButton())
	}
}

// HasFocus implements tview.Primitive.
func (pf *PageFrame) HasFocus() bool {
	if pf.content != nil && pf.content.HasFocus() {
		return true
	}
	if pf.buttonBar != nil && pf.buttonBar.HasFocus() {
		return true
	}
	return false
}

// InputHandler implements tview.Primitive.
func (pf *PageFrame) InputHandler() func(event *tcell.EventKey, setFocus func(p tview.Primitive)) {
	return pf.WrapInputHandler(func(event *tcell.EventKey, setFocus func(p tview.Primitive)) {
		if event.Key() == tcell.KeyEscape && pf.onEscape != nil {
			pf.onEscape()
			return
		}

		// Delegate to focused child
		if pf.content != nil && pf.content.HasFocus() {
			if handler := pf.content.InputHandler(); handler != nil {
				handler(event, setFocus)
			}
			return
		}

		if pf.buttonBar != nil && pf.buttonBar.HasFocus() {
			if handler := pf.buttonBar.InputHandler(); handler != nil {
				handler(event, setFocus)
			}
			return
		}
	})
}

// MouseHandler implements tview.Primitive.
func (pf *PageFrame) MouseHandler() func(
	action tview.MouseAction,
	event *tcell.EventMouse,
	setFocus func(p tview.Primitive),
) (consumed bool, capture tview.Primitive) {
	return pf.WrapMouseHandler(func(
		action tview.MouseAction,
		event *tcell.EventMouse,
		setFocus func(p tview.Primitive),
	) (consumed bool, capture tview.Primitive) {
		// Check button bar first
		if pf.buttonBar != nil {
			bx, by, bw, bh := pf.buttonBar.GetRect()
			mx, my := event.Position()
			if mx >= bx && mx < bx+bw && my >= by && my < by+bh {
				return pf.buttonBar.MouseHandler()(action, event, setFocus)
			}
		}

		// Then content
		if pf.content != nil {
			if handler := pf.content.MouseHandler(); handler != nil {
				return handler(action, event, setFocus)
			}
		}

		return false, nil
	})
}

// GetContent returns the content primitive.
func (pf *PageFrame) GetContent() tview.Primitive {
	return pf.content
}

// GetButtonBar returns the button bar.
func (pf *PageFrame) GetButtonBar() *ButtonBar {
	return pf.buttonBar
}

// FocusContent sets focus to the content primitive.
func (pf *PageFrame) FocusContent() {
	if pf.content != nil && pf.app != nil {
		pf.app.SetFocus(pf.content)
	}
}

// FocusButtonBar sets focus to the button bar.
func (pf *PageFrame) FocusButtonBar() {
	if pf.buttonBar != nil && pf.app != nil {
		pf.app.SetFocus(pf.buttonBar)
	}
}

// SetupContentToButtonNavigation sets up full wrap navigation between content and button bar.
// This should be called after setting content and button bar.
func (pf *PageFrame) SetupContentToButtonNavigation() {
	if pf.content == nil || pf.buttonBar == nil || pf.app == nil {
		return
	}

	// Get the last focusable item in content if it's a list
	if list, ok := pf.content.(*tview.List); ok {
		originalCapture := list.GetInputCapture()
		list.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
			key := event.Key()
			if key == tcell.KeyTab {
				pf.FocusButtonBar()
				return nil
			}
			// Down on last item → button bar
			if key == tcell.KeyDown && list.GetCurrentItem() == list.GetItemCount()-1 {
				pf.FocusButtonBar()
				return nil
			}
			// Up on first item → button bar (wrap)
			if key == tcell.KeyUp && list.GetCurrentItem() == 0 {
				pf.FocusButtonBar()
				return nil
			}
			if key == tcell.KeyEscape && pf.onEscape != nil {
				pf.onEscape()
				return nil
			}
			if originalCapture != nil {
				return originalCapture(event)
			}
			return event
		})
	}

	// Set up button bar to navigate back to content
	pf.buttonBar.SetOnUp(pf.FocusContent)
	pf.buttonBar.SetOnDown(pf.FocusContent)
}
