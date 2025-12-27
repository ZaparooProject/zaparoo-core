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

func pageDefaults[S PrimitiveWithSetBorder](name string, pages *tview.Pages, widget S) tview.Primitive {
	widget.SetBorder(true)
	pages.AddAndSwitchToPage(name, widget, true)
	return widget
}

// ThemeBgColor is the background color name for use in tview color tags.
// Must match the PrimitiveBackgroundColor set in SetTheme.
const ThemeBgColor = "darkblue"

func SetTheme(theme *tview.Theme) {
	theme.BorderColor = tcell.ColorLightYellow
	theme.PrimaryTextColor = tcell.ColorWhite
	theme.ContrastSecondaryTextColor = tcell.ColorFuchsia
	theme.PrimitiveBackgroundColor = tcell.ColorDarkBlue // matches ThemeBgColor
	theme.ContrastBackgroundColor = tcell.ColorBlue
	theme.InverseTextColor = tcell.ColorDarkBlue
}

func genericModal(
	message string,
	title string,
	action func(buttonIndex int, buttonLabel string),
	withButton bool,
) *tview.Modal {
	modal := tview.NewModal()
	modal.SetTitle(title).
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
