// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-only
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

	"github.com/ZaparooProject/zaparoo-core/pkg/api/client"
	"github.com/ZaparooProject/zaparoo-core/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func BuildTagsWriteMenu(cfg *config.Instance, pages *tview.Pages, _ *tview.Application) *tview.Form {
	topTextView := tview.NewTextView().
		SetLabel("").
		SetText("Place card on reader, input your ZapScript and press ENTER to write. ESC to exit.")
	zapScriptTextArea := tview.NewTextArea().
		SetLabel("ZapScript")

	tagsWriteMenu := tview.NewForm().
		AddFormItem(topTextView).
		AddFormItem(zapScriptTextArea)

	tagsWriteMenu.SetTitle("Settings - NFC Tags - Write")
	tagsWriteMenu.SetFocus(1)

	tagsWriteMenu.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		k := event.Key()
		switch k { //nolint:exhaustive // only handling specific keys
		case tcell.KeyEnter:
			text := zapScriptTextArea.GetText()
			text = strings.Trim(text, "\r\n ")
			data, _ := json.Marshal(&models.ReaderWriteParams{
				Text: text,
			})
			_, _ = client.LocalClient(context.Background(), cfg, models.MethodReadersWrite, string(data))
			zapScriptTextArea.SetText("", true)
		case tcell.KeyEscape:
			pages.SwitchToPage(PageMain)
		}
		return event
	})

	pageDefaults(PageSettingsTagsWrite, pages, tagsWriteMenu)

	return tagsWriteMenu
}
