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
	"context"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// TUIRequestTimeout is the timeout for API requests from the TUI.
// This is shorter than the default API timeout since TUI calls are to localhost.
const TUIRequestTimeout = 5 * time.Second

// TagReadTimeout is the timeout for tag read operations.
// This is longer than TUIRequestTimeout to give users time to physically tap a tag.
const TagReadTimeout = 30 * time.Second

// tuiContext creates a context with the TUI request timeout.
// Use this for API calls from the TUI to avoid long hangs.
func tuiContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), TUIRequestTimeout)
}

// tagReadContext creates a context with the tag read timeout.
// Use this for operations where the user needs to physically interact with a tag.
func tagReadContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), TagReadTimeout)
}

// DefaultMaxWidth is the default maximum width for the TUI in non-CRT mode.
const DefaultMaxWidth = 100

// DefaultMaxHeight is the default maximum height for the TUI in non-CRT mode.
const DefaultMaxHeight = 30

// CenterWidget creates a fixed-size centered layout for CRT displays.
func CenterWidget(width, height int, p tview.Primitive) tview.Primitive {
	return tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().
			SetDirection(tview.FlexRow).
			AddItem(nil, 0, 1, false).
			AddItem(p, height, 1, true).
			AddItem(nil, 0, 1, false), width, 1, true).
		AddItem(nil, 0, 1, false)
}

// responsiveWrapper wraps a primitive with max width/height constraints.
type responsiveWrapper struct {
	*tview.Box
	child     tview.Primitive
	maxWidth  int
	maxHeight int
}

// ResponsiveMaxWidget creates a centered layout that respects max width/height
// but clamps to terminal size when smaller. This prevents clipping on small terminals.
func ResponsiveMaxWidget(maxWidth, maxHeight int, p tview.Primitive) tview.Primitive {
	return &responsiveWrapper{
		Box:       tview.NewBox(),
		child:     p,
		maxWidth:  maxWidth,
		maxHeight: maxHeight,
	}
}

// Draw implements tview.Primitive.
func (r *responsiveWrapper) Draw(screen tcell.Screen) {
	r.DrawForSubclass(screen, r)
	x, y, width, height := r.GetInnerRect()

	// Calculate actual dimensions (clamped to available space)
	actualWidth := r.maxWidth
	if width < r.maxWidth {
		actualWidth = width
	}
	actualHeight := r.maxHeight
	if height < r.maxHeight {
		actualHeight = height
	}

	// Calculate centered position
	offsetX := (width - actualWidth) / 2
	offsetY := (height - actualHeight) / 2

	// Set the child's position and draw it
	r.child.SetRect(x+offsetX, y+offsetY, actualWidth, actualHeight)
	r.child.Draw(screen)
}

// Focus implements tview.Primitive.
func (r *responsiveWrapper) Focus(delegate func(p tview.Primitive)) {
	delegate(r.child)
}

// HasFocus implements tview.Primitive.
func (r *responsiveWrapper) HasFocus() bool {
	return r.child.HasFocus()
}

// MouseHandler implements tview.Primitive.
func (r *responsiveWrapper) MouseHandler() func(
	action tview.MouseAction, event *tcell.EventMouse, setFocus func(p tview.Primitive),
) (consumed bool, capture tview.Primitive) {
	return r.child.MouseHandler()
}

// InputHandler implements tview.Primitive.
func (r *responsiveWrapper) InputHandler() func(event *tcell.EventKey, setFocus func(p tview.Primitive)) {
	return r.child.InputHandler()
}

func pageDefaults[S PrimitiveWithSetBorder](name string, pages *tview.Pages, widget S) tview.Primitive {
	widget.SetBorder(true)
	pages.AddAndSwitchToPage(name, widget, true)
	return widget
}

// Modal page name constants for consistent overlay management.
const (
	infoModalPage    = "info_modal"
	errorModalPage   = "error_modal"
	confirmModalPage = "confirm_modal"
	waitingModalPage = "waiting_modal"
)

// ShowInfoModal displays an informational modal with a title and OK button.
func ShowInfoModal(pages *tview.Pages, app *tview.Application, title, message string) {
	modal := tview.NewModal().
		SetText(message).
		AddButtons([]string{"OK"}).
		SetDoneFunc(func(_ int, _ string) {
			pages.HidePage(infoModalPage)
			pages.RemovePage(infoModalPage)
		})
	modal.SetTitle(" " + title + " ").
		SetBorder(true).
		SetTitleAlign(tview.AlignCenter)
	pages.AddPage(infoModalPage, modal, false, true)
	app.SetFocus(modal)
}

// ShowErrorModal displays an error message modal to the user.
func ShowErrorModal(pages *tview.Pages, app *tview.Application, message string) {
	modal := tview.NewModal().
		SetText(message).
		AddButtons([]string{"OK"}).
		SetDoneFunc(func(_ int, _ string) {
			pages.HidePage(errorModalPage)
			pages.RemovePage(errorModalPage)
		})
	pages.AddPage(errorModalPage, modal, false, true)
	app.SetFocus(modal)
}

// ShowConfirmModal displays a confirmation dialog with Yes/No buttons.
// onYes is called when the user clicks "Yes", onNo is called for "No" or Escape.
func ShowConfirmModal(pages *tview.Pages, app *tview.Application, message string, onYes, onNo func()) {
	modal := tview.NewModal().
		SetText(message).
		AddButtons([]string{"Yes", "No"}).
		SetDoneFunc(func(buttonIndex int, _ string) {
			pages.HidePage(confirmModalPage)
			pages.RemovePage(confirmModalPage)
			if buttonIndex == 0 {
				if onYes != nil {
					onYes()
				}
			} else {
				if onNo != nil {
					onNo()
				}
			}
		})
	pages.AddPage(confirmModalPage, modal, false, true)
	app.SetFocus(modal)
}

// ShowWaitingModal displays a modal while waiting for user action (like placing a tag).
// Returns a cleanup function that removes the modal.
func ShowWaitingModal(pages *tview.Pages, app *tview.Application, message string, onCancel func()) func() {
	modal := tview.NewModal().
		SetText(message).
		AddButtons([]string{"Cancel"}).
		SetDoneFunc(func(_ int, _ string) {
			pages.HidePage(waitingModalPage)
			pages.RemovePage(waitingModalPage)
			if onCancel != nil {
				onCancel()
			}
		})
	pages.AddPage(waitingModalPage, modal, false, true)
	app.SetFocus(modal)

	return func() {
		pages.HidePage(waitingModalPage)
		pages.RemovePage(waitingModalPage)
	}
}

// SetBoxTitle sets a box title with consistent padding.
func SetBoxTitle(box interface{ SetTitle(string) *tview.Box }, title string) {
	box.SetTitle(" " + title + " ")
}

// genericModal is deprecated. Use ShowInfoModal, ShowErrorModal, ShowConfirmModal instead.
func genericModal(
	message string,
	title string,
	action func(buttonIndex int, buttonLabel string),
	withButton bool,
) *tview.Modal {
	modal := tview.NewModal()
	modal.SetTitle(" " + title + " ").
		SetBorder(true).
		SetTitleAlign(tview.AlignCenter)
	modal.SetText(message)
	if withButton {
		modal.AddButtons([]string{"OK"}).
			SetDoneFunc(action)
	}
	return modal
}

type PrimitiveWithSetBorder interface {
	tview.Primitive
	SetBorder(arg bool) *tview.Box
}

// BuildAndRetry attempts to build and display a TUI dialog, retrying with
// alternate settings on error.
// It's used to work around issues on MiSTer, which has an unusual setup for
// showing TUI applications.
func BuildAndRetry(
	builder func() (*tview.Application, error),
) error {
	app, err := builder()
	if err != nil {
		return err
	}
	return tryRunApp(app, builder)
}
