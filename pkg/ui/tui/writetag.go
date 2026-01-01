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

func BuildTagsWriteMenu(cfg *config.Instance, pages *tview.Pages, app *tview.Application) {
	statusText := tview.NewTextView().
		SetTextAlign(tview.AlignCenter).
		SetText("Enter your ZapScript and press the Write button. ESC to exit.")

	zapScriptInput := tview.NewInputField()
	zapScriptInput.SetLabel("ZapScript")
	zapScriptInput.SetLabelWidth(10)
	zapScriptInput.SetFieldWidth(50)

	writeButton := tview.NewButton("Write")

	// Main flex container - this is the page
	writeMenu := tview.NewFlex()
	writeMenu.SetTitle("Settings - NFC Tags - Write")
	writeMenu.SetDirection(tview.FlexRow)

	// Add UI elements to main container
	writeMenu.AddItem(statusText, 1, 0, false)
	writeMenu.AddItem(nil, 1, 0, false)
	writeMenu.AddItem(zapScriptInput, 1, 0, true)
	writeMenu.AddItem(nil, 1, 0, false)
	writeMenu.AddItem(writeButton, 1, 0, false)

	// Create pages for modals (this goes inside the main container)
	modalPages := tview.NewPages()

	// Create modals for feedback
	writeModal := tview.NewModal().
		SetText("Writing to tag...\nPlace your card on the reader.").
		AddButtons([]string{"Cancel"})

	successModal := tview.NewModal().
		SetText("Tag written successfully!").
		AddButtons([]string{"OK"}).
		SetDoneFunc(func(_ int, _ string) {
			modalPages.HidePage("success_modal")
			zapScriptInput.SetText("")
			app.SetFocus(zapScriptInput)
		})

	errorModal := tview.NewModal().
		SetText("Error writing to tag.").
		AddButtons([]string{"OK"}).
		SetDoneFunc(func(_ int, _ string) {
			modalPages.HidePage("error_modal")
			app.SetFocus(zapScriptInput)
		})

	modalPages.AddPage("write_modal", writeModal, true, false)
	modalPages.AddPage("success_modal", successModal, true, false)
	modalPages.AddPage("error_modal", errorModal, true, false)

	// Add modal pages to main container
	writeMenu.AddItem(modalPages, 0, 1, false)

	writeTag := func(text string) {
		ctx, cancel := context.WithCancel(context.Background())
		writeModal.SetDoneFunc(func(_ int, _ string) {
			cancel()
			_, _ = client.LocalClient(context.Background(), cfg, models.MethodReadersWriteCancel, "")
			modalPages.HidePage("write_modal")
			app.SetFocus(zapScriptInput)
		})

		modalPages.ShowPage("write_modal")
		app.SetFocus(writeModal)

		go func() {
			data, err := json.Marshal(&models.ReaderWriteParams{
				Text: text,
			})
			if err != nil {
				errorModal.SetText("Error preparing write request:\n" + err.Error())
				modalPages.HidePage("write_modal")
				modalPages.ShowPage("error_modal")
				app.SetFocus(errorModal).ForceDraw()
				return
			}

			_, err = client.LocalClient(ctx, cfg, models.MethodReadersWrite, string(data))
			if err != nil {
				errorModal.SetText("Error writing to tag:\n" + err.Error())
				modalPages.HidePage("write_modal")
				modalPages.ShowPage("error_modal")
				app.SetFocus(errorModal).ForceDraw()
				return
			}

			modalPages.HidePage("write_modal")
			modalPages.ShowPage("success_modal")
			app.SetFocus(successModal).ForceDraw()
		}()
	}

	// Input field handling
	zapScriptInput.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		k := event.Key()
		switch k { //nolint:exhaustive // only handling navigation keys
		case tcell.KeyTab, tcell.KeyDown:
			app.SetFocus(writeButton)
			return nil
		case tcell.KeyEnter:
			text := strings.TrimSpace(zapScriptInput.GetText())
			if text != "" {
				writeTag(text)
			}
			return nil
		}
		return event
	})

	// Button handling
	writeButton.SetSelectedFunc(func() {
		text := strings.TrimSpace(zapScriptInput.GetText())
		if text != "" {
			writeTag(text)
		}
	})

	writeButton.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		k := event.Key()
		switch k { //nolint:exhaustive // only handling navigation keys
		case tcell.KeyTab, tcell.KeyUp:
			app.SetFocus(zapScriptInput)
			return nil
		case tcell.KeyBacktab:
			app.SetFocus(zapScriptInput)
			return nil
		}
		return event
	})

	// Main page input handling
	writeMenu.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape {
			pages.SwitchToPage(PageMain)
			return nil
		}
		return event
	})

	pageDefaults(PageSettingsTagsWrite, pages, writeMenu)
}
