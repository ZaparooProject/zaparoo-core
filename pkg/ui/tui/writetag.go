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
	"fmt"
	"strings"

	"github.com/ZaparooProject/go-zapscript"
	libzapscript "github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// validateZapScript parses the input and returns a validation status message.
func validateZapScript(text string) (valid bool, message string) {
	if strings.TrimSpace(text) == "" {
		return false, ""
	}

	t := CurrentTheme()
	reader := zapscript.NewParser(text)
	script, err := reader.ParseScript()
	if err != nil {
		return false, fmt.Sprintf("[%s]Error: %s[-]", t.ErrorColorName, err.Error())
	}

	if len(script.Cmds) == 0 {
		return false, fmt.Sprintf("[%s]No commands found[-]", t.WarningColorName)
	}

	// Validate all command names are known
	for _, cmd := range script.Cmds {
		if !libzapscript.IsValidCommand(cmd.Name) {
			return false, fmt.Sprintf("[%s]Unknown command: %s[-]", t.ErrorColorName, cmd.Name)
		}
	}

	if len(script.Cmds) == 1 {
		return true, fmt.Sprintf("[%s]Valid: %s[-]", t.SuccessColorName, script.Cmds[0].Name)
	}

	return true, fmt.Sprintf("[%s]Valid: %d commands[-]", t.SuccessColorName, len(script.Cmds))
}

// BuildTagsWriteMenu creates the tag write menu.
func BuildTagsWriteMenu(svc SettingsService, pages *tview.Pages, app *tview.Application) {
	frame := NewPageFrame(app).
		SetTitle("Write Token").
		SetHelpText("Enter ZapScript and press Write button")

	goBack := func() {
		pages.SwitchToPage(PageMain)
	}
	frame.SetOnEscape(goBack)

	zapScriptLabel := NewLabel("ZapScript")
	zapScriptInput := tview.NewTextArea()
	zapScriptInput.SetBorder(true)
	zapScriptInput.SetBorderPadding(0, 0, 1, 1)

	validationStatus := tview.NewTextView().
		SetDynamicColors(true).
		SetText("")

	zapScriptInput.SetChangedFunc(func() {
		text := zapScriptInput.GetText()
		_, message := validateZapScript(text)
		validationStatus.SetText(message)
	})

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
		WriteTagWithModal(pages, app, svc, text, func(_ bool) {
			app.SetFocus(zapScriptInput)
		})
	}

	doClear := func() {
		zapScriptInput.SetText("", true)
		validationStatus.SetText("")
		app.SetFocus(zapScriptInput)
	}

	buttonBar := NewButtonBar(app)
	buttonBar.AddButton("Write", doWrite).
		AddButton("Clear", doClear).
		AddButton("Back", goBack).
		SetupNavigation(goBack)
	frame.SetButtonBar(buttonBar)

	buttonBar.SetOnUp(func() { app.SetFocus(zapScriptInput) })
	buttonBar.SetOnDown(func() { app.SetFocus(zapScriptInput) })
	buttonBar.SetOnLeft(func() { app.SetFocus(zapScriptInput) })
	buttonBar.SetOnRight(func() { app.SetFocus(zapScriptInput) })

	getLastLineIndex := func() int {
		text := zapScriptInput.GetText()
		if text == "" {
			return 0
		}
		return strings.Count(text, "\n")
	}

	zapScriptInput.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyUp:
			_, _, cursorRow, _ := zapScriptInput.GetCursor()
			if cursorRow == 0 {
				frame.FocusButtonBar()
				return nil
			}
			return event
		case tcell.KeyDown:
			_, _, cursorRow, _ := zapScriptInput.GetCursor()
			lastLine := getLastLineIndex()
			if cursorRow >= lastLine {
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

	contentFlex := tview.NewFlex().SetDirection(tview.FlexRow)
	contentFlex.AddItem(zapScriptLabel, 1, 0, false)
	contentFlex.AddItem(zapScriptInput, 0, 1, true)
	contentFlex.AddItem(validationStatus, 1, 0, false)

	frame.SetContent(contentFlex)
	pages.AddAndSwitchToPage(PageSettingsTagsWrite, frame, true)
}
