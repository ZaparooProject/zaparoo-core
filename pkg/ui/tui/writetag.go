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
	"encoding/json"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/client"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// BuildTagsWriteMenu creates the tag write menu.
func BuildTagsWriteMenu(cfg *config.Instance, pages *tview.Pages, app *tview.Application) {
	// Create page frame
	frame := NewPageFrame(app).
		SetTitle("Write Tag").
		SetHelpText("Enter ZapScript and press Enter or Write button")

	goBack := func() {
		pages.SwitchToPage(PageMain)
	}
	frame.SetOnEscape(goBack)

	// Create ZapScript input
	zapScriptInput := tview.NewInputField()
	zapScriptInput.SetLabel("ZapScript: ")
	zapScriptInput.SetLabelWidth(11)
	setupInputFieldFocus(zapScriptInput)

	var writeCancel context.CancelFunc

	writeTag := func(text string) {
		writeModalPage := "write_modal"

		ctx, ctxCancel := tagReadContext()
		writeCancel = ctxCancel

		// Create waiting modal
		modal := tview.NewModal().
			SetText("Place tag on the reader...").
			AddButtons([]string{"Cancel"}).
			SetDoneFunc(func(_ int, _ string) {
				if writeCancel != nil {
					writeCancel()
					writeCancel = nil
				}
				_, _ = client.LocalClient(context.Background(), cfg, models.MethodReadersWriteCancel, "")
				pages.RemovePage(writeModalPage)
				app.SetFocus(zapScriptInput)
			})

		pages.AddPage(writeModalPage, modal, true, true)
		app.SetFocus(modal)

		go func() {
			defer func() {
				writeCancel = nil
			}()

			data, err := json.Marshal(&models.ReaderWriteParams{
				Text: text,
			})
			if err != nil {
				app.QueueUpdateDraw(func() {
					pages.RemovePage(writeModalPage)
					showErrorModal(pages, app, "Error: "+err.Error())
				})
				return
			}

			_, err = client.LocalClient(ctx, cfg, models.MethodReadersWrite, string(data))
			if err != nil {
				app.QueueUpdateDraw(func() {
					pages.RemovePage(writeModalPage)
					showErrorModal(pages, app, "Write failed: "+err.Error())
				})
				return
			}

			app.QueueUpdateDraw(func() {
				pages.RemovePage(writeModalPage)
				successModal := tview.NewModal().
					SetText("Tag written successfully!").
					AddButtons([]string{"OK"}).
					SetDoneFunc(func(_ int, _ string) {
						pages.RemovePage("success_modal")
						zapScriptInput.SetText("")
						app.SetFocus(zapScriptInput)
					})
				pages.AddPage("success_modal", successModal, true, true)
				app.SetFocus(successModal)
			})
		}()
	}

	doWrite := func() {
		text := strings.TrimSpace(zapScriptInput.GetText())
		if text == "" {
			showErrorModal(pages, app, "Please enter ZapScript before writing")
			return
		}
		writeTag(text)
	}

	// Button bar with Write and Back
	buttonBar := NewButtonBar(app)
	buttonBar.AddButton("Write", doWrite).
		AddButton("Back", goBack).
		SetupNavigation(goBack)
	frame.SetButtonBar(buttonBar)

	// Navigation from input
	zapScriptInput.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyTab, tcell.KeyDown:
			frame.FocusButtonBar()
			return nil
		case tcell.KeyEnter:
			doWrite()
			return nil
		case tcell.KeyEscape:
			goBack()
			return nil
		default:
			return event
		}
	})

	// Main content layout - just the input field
	contentFlex := tview.NewFlex().SetDirection(tview.FlexRow)
	contentFlex.AddItem(zapScriptInput, 1, 0, true)
	contentFlex.AddItem(tview.NewBox(), 0, 1, false) // spacer to push input to top

	frame.SetContent(contentFlex)
	pages.AddAndSwitchToPage(PageSettingsTagsWrite, frame, true)
}
