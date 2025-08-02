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
		switch k { //nolint:exhaustive
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
