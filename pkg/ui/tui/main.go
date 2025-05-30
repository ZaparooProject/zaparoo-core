package tui

import (
	"fmt"

	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/pkg/utils"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

const (
	PageMain              = "main"
	PageSettingsMain      = "settings_main"
	PageSettingsTags      = "settings_tags"
	PageSettingsTagsRead  = "settings_tags_read"
	PageSettingsTagsWrite = "settings_tags_write"
	PageSettingsAudio     = "settings_audio"
	PageSettingsReaders   = "settings_readers"
	PageSettingsScanMode  = "settings_readers_scanMode"
	PageSearchMedia       = "search_media"
	PageExportLog         = "export_log"
)

func setupButtonNavigation(app *tview.Application, buttons ...*tview.Button) {
	for i, button := range buttons {
		prevIndex := (i - 1 + len(buttons)) % len(buttons)
		nextIndex := (i + 1) % len(buttons)

		button.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
			k := event.Key()
			if k == tcell.KeyUp || k == tcell.KeyLeft {
				app.SetFocus(buttons[prevIndex])
				return event
			} else if k == tcell.KeyDown || k == tcell.KeyRight {
				app.SetFocus(buttons[nextIndex])
				return event
			}
			return event
		})
	}
}

func BuildMain(
	cfg *config.Instance,
	pl platforms.Platform,
	isRunning func() bool,
	logDestPath string,
	logDestName string,
) (*tview.Application, error) {
	app := tview.NewApplication()
	SetTheme(&tview.Styles)

	main := tview.NewFlex()

	introText := tview.NewTextView().SetText("Visit zaparoo.org for guides and support.")
	statusText := tview.NewTextView()

	var svcStatus string
	if isRunning() {
		svcStatus = "RUNNING"
	} else {
		svcStatus = "NOT RUNNING"
	}

	ip := utils.GetLocalIP()
	var ipDisplay string
	if ip == "" {
		ipDisplay = "Unknown"
	} else {
		ipDisplay = ip
	}

	statusText.SetText(
		fmt.Sprintf(
			"Service status: %s\nDevice address: %s",
			svcStatus,
			ipDisplay,
		),
	)

	helpText := tview.NewTextView()
	helpText.SetBorder(true)

	displayCol := tview.NewFlex().SetDirection(tview.FlexRow)
	displayCol.AddItem(introText, 1, 1, false)
	displayCol.AddItem(tview.NewTextView(), 1, 1, false)
	displayCol.AddItem(statusText, 0, 1, false)
	displayCol.AddItem(tview.NewTextView(), 0, 1, false)
	displayCol.AddItem(helpText, 3, 1, false)

	pages := tview.NewPages().
		AddPage(PageMain, main, true, true)

	// create the main modal
	main.SetTitle("Zaparoo Core v" + config.AppVersion + " (" + pl.ID() + ")").
		SetBorder(true).
		SetTitleAlign(tview.AlignCenter)

	main.AddItem(displayCol, 0, 1, false)

	searchButton := tview.NewButton("Search media").SetSelectedFunc(func() {
		pages.SwitchToPage(PageSearchMedia)
	})
	searchButton.SetFocusFunc(func() {
		helpText.SetText("Search for media and write to an NFC tag.")
	})

	writeButton := tview.NewButton("Custom write").SetSelectedFunc(func() {
		pages.SwitchToPage(PageSettingsTagsWrite)
	})
	writeButton.SetFocusFunc(func() {
		helpText.SetText("Write custom ZapScript to an NFC tag.")
	})

	updateDBButton := tview.NewButton("Update media DB").SetSelectedFunc(func() {
		app.Stop()
	})
	updateDBButton.SetFocusFunc(func() {
		helpText.SetText("Update Core media database.")
	})

	settingsButton := tview.NewButton("Settings").SetSelectedFunc(func() {
		pages.SwitchToPage(PageSettingsMain)
	})
	settingsButton.SetFocusFunc(func() {
		helpText.SetText("Manage settings for Core service.")
	})

	exportButton := tview.NewButton("Export log").SetSelectedFunc(func() {
		pages.SwitchToPage(PageExportLog)
	})
	exportButton.SetFocusFunc(func() {
		helpText.SetText("Export Core log file for support.")
	})

	exitButton := tview.NewButton("Exit").SetSelectedFunc(func() {
		app.Stop()
	})
	exitButton.SetFocusFunc(func() {
		helpText.SetText("Exit app. (Service will keep running)")
	})

	setupButtonNavigation(
		app,
		searchButton,
		writeButton,
		updateDBButton,
		settingsButton,
		exportButton,
		exitButton,
	)

	main.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		k := event.Key()
		if k == tcell.KeyEscape {
			app.Stop()
		}
		return event
	})

	buttonNav := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(searchButton, 0, 1, true).
		AddItem(writeButton, 0, 1, false).
		AddItem(updateDBButton, 0, 1, false).
		AddItem(settingsButton, 0, 1, false).
		AddItem(exportButton, 0, 1, false).
		AddItem(exitButton, 0, 1, false)
	main.AddItem(buttonNav, 20, 1, true)

	BuildExportLogModal(pl, app, pages, logDestPath, logDestName)
	BuildSettingsMainMenu(cfg, pages, app)
	BuildTagsMenu(cfg, pages, app)
	BuildTagsReadMenu(cfg, pages, app)
	BuildSearchMedia(cfg, pages, app)
	BuildTagsWriteMenu(cfg, pages, app)
	BuildAudioMenu(cfg, pages, app)
	BuildReadersMenu(cfg, pages, app)
	BuildScanModeMenu(cfg, pages, app)

	pages.SwitchToPage(PageMain)

	centeredPages := centerWidget(70, 20, pages)
	return app.SetRoot(centeredPages, true).
		EnableMouse(true), nil
}
