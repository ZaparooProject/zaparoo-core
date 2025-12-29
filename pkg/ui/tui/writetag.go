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
	"fmt"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/client"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript/parser"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// validateZapScript parses the input and returns a validation status message.
func validateZapScript(text string) (valid bool, message string) {
	if strings.TrimSpace(text) == "" {
		return false, ""
	}

	t := CurrentTheme()
	reader := parser.NewParser(text)
	script, err := reader.ParseScript()
	if err != nil {
		return false, fmt.Sprintf("[%s]Error: %s[-]", t.ErrorColorName, err.Error())
	}

	if len(script.Cmds) == 0 {
		return false, fmt.Sprintf("[%s]No commands found[-]", t.WarningColorName)
	}

	// Validate all command names are known
	for _, cmd := range script.Cmds {
		if !zapscript.IsValidCommand(cmd.Name) {
			return false, fmt.Sprintf("[%s]Unknown command: %s[-]", t.ErrorColorName, cmd.Name)
		}
	}

	if len(script.Cmds) == 1 {
		return true, fmt.Sprintf("[%s]Valid: %s[-]", t.SuccessColorName, script.Cmds[0].Name)
	}

	return true, fmt.Sprintf("[%s]Valid: %d commands[-]", t.SuccessColorName, len(script.Cmds))
}

// BuildTagsWriteMenu creates the tag write menu.
func BuildTagsWriteMenu(cfg *config.Instance, pages *tview.Pages, app *tview.Application) {
	// Create page frame
	frame := NewPageFrame(app).
		SetTitle("Write Tag").
		SetHelpText("Enter ZapScript and press Write button")

	goBack := func() {
		pages.SwitchToPage(PageMain)
	}
	frame.SetOnEscape(goBack)

	// Create label
	zapScriptLabel := NewLabel("ZapScript")

	// Create multiline text area for ZapScript input
	zapScriptInput := tview.NewTextArea()
	zapScriptInput.SetBorder(true)
	zapScriptInput.SetBorderPadding(0, 0, 1, 1)

	// Create validation status display
	validationStatus := tview.NewTextView().
		SetDynamicColors(true).
		SetText("")

	// Update validation on text change
	zapScriptInput.SetChangedFunc(func() {
		text := zapScriptInput.GetText()
		_, message := validateZapScript(text)
		validationStatus.SetText(message)
	})

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
					ShowErrorModal(pages, app, "Error: "+err.Error())
				})
				return
			}

			_, err = client.LocalClient(ctx, cfg, models.MethodReadersWrite, string(data))
			if err != nil {
				app.QueueUpdateDraw(func() {
					pages.RemovePage(writeModalPage)
					ShowErrorModal(pages, app, "Write failed: "+err.Error())
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
			ShowErrorModal(pages, app, "Please enter ZapScript before writing")
			return
		}
		valid, _ := validateZapScript(text)
		if !valid {
			ShowErrorModal(pages, app, "Please fix ZapScript errors before writing")
			return
		}
		writeTag(text)
	}

	doClear := func() {
		zapScriptInput.SetText("", true)
		validationStatus.SetText("")
		app.SetFocus(zapScriptInput)
	}

	// Button bar with Write, Clear, and Back
	buttonBar := NewButtonBar(app)
	buttonBar.AddButton("Write", doWrite).
		AddButton("Clear", doClear).
		AddButton("Back", goBack).
		SetupNavigation(goBack)
	frame.SetButtonBar(buttonBar)

	// Up from button bar goes to text area
	buttonBar.SetOnUp(func() {
		app.SetFocus(zapScriptInput)
	})
	// Down from button bar wraps to text area
	buttonBar.SetOnDown(func() {
		app.SetFocus(zapScriptInput)
	})
	// Left from button bar goes to text area
	buttonBar.SetOnLeft(func() {
		app.SetFocus(zapScriptInput)
	})
	// Right from last button wraps to text area
	buttonBar.SetOnRight(func() {
		app.SetFocus(zapScriptInput)
	})

	// Helper to count lines in text (0-indexed last line)
	getLastLineIndex := func() int {
		text := zapScriptInput.GetText()
		if text == "" {
			return 0
		}
		return strings.Count(text, "\n")
	}

	// Navigation from text area - intercept Up/Down at boundaries
	zapScriptInput.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyUp:
			// Get cursor position (fromRow, fromCol, toRow, toCol)
			_, _, cursorRow, _ := zapScriptInput.GetCursor()
			if cursorRow == 0 {
				// On first line, navigate away
				frame.FocusButtonBar()
				return nil
			}
			return event
		case tcell.KeyDown:
			_, _, cursorRow, _ := zapScriptInput.GetCursor()
			lastLine := getLastLineIndex()
			if cursorRow >= lastLine {
				// On last line, navigate away
				frame.FocusButtonBar()
				return nil
			}
			return event
		case tcell.KeyTab:
			frame.FocusButtonBar()
			return nil
		case tcell.KeyEscape:
			goBack()
			return nil
		default:
			return event
		}
	})

	// Main content layout
	contentFlex := tview.NewFlex().SetDirection(tview.FlexRow)
	contentFlex.AddItem(zapScriptLabel, 1, 0, false)
	contentFlex.AddItem(zapScriptInput, 0, 1, true)
	contentFlex.AddItem(validationStatus, 1, 0, false)

	frame.SetContent(contentFlex)
	pages.AddAndSwitchToPage(PageSettingsTagsWrite, frame, true)
}
