package tui

import (
	"fmt"
	"github.com/ZaparooProject/zaparoo-core/pkg/api/client"
	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/pkg/utils"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/rs/zerolog/log"
	"os/exec"
	"path"
	"strings"
	"time"
)

func setupLogExport(
	pl platforms.Platform,
	app *tview.Application,
	pages *tview.Pages,
	logDestPath string,
	logDestName string,
) *tview.List {
	logExport := tview.NewList()

	logExport.
		AddItem("Upload to termbin.com", "", 'a', func() {
			pages.RemovePage("export")
			outcome := uploadLog(pl, pages, app)
			modal := genericModal(outcome, "Log upload", func(buttonIndex int, buttonLabel string) {
				pages.RemovePage("upload")
			}, true)
			pages.AddPage("upload", modal, true, true)
		})

	if logDestPath != "" {
		logExport.AddItem("Copy to "+logDestName, "", 'b', func() {
			pages.RemovePage("export")
			outcome := copyLogToSd(pl, logDestPath, logDestName)
			modal := genericModal(outcome, "Log copy", func(buttonIndex int, buttonLabel string) {
				pages.RemovePage("copy")
			}, true)
			pages.AddPage("copy", modal, true, true)
		})
	}

	logExport.AddItem("Cancel", "", 'q', func() {
		pages.RemovePage("export")
	}).ShowSecondaryText(false)

	logExport.
		SetBorder(true).
		SetBorderPadding(1, 1, 1, 1).
		SetTitle("Log export")

	return logExport
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
	go func() {
		for {
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

			time.Sleep(1 * time.Second)
		}
	}()

	helpText := tview.NewTextView()
	helpText.SetBorder(true)

	displayCol := tview.NewFlex().SetDirection(tview.FlexRow)
	displayCol.AddItem(introText, 1, 1, false)
	displayCol.AddItem(tview.NewTextView(), 1, 1, false)
	displayCol.AddItem(statusText, 0, 1, false)
	displayCol.AddItem(tview.NewTextView(), 0, 1, false)
	displayCol.AddItem(helpText, 3, 1, false)

	pages := tview.NewPages().
		AddPage("main", main, true, true)

	// create the main modal
	main.SetTitle("Zaparoo Core v" + config.AppVersion + " (" + pl.ID() + ")").
		SetBorder(true).
		SetTitleAlign(tview.AlignCenter)

	main.AddItem(displayCol, 0, 1, false)

	searchButton := tview.NewButton("Search media").SetSelectedFunc(func() {
		app.Stop()
	})
	searchButton.SetFocusFunc(func() {
		helpText.SetText("Search for media and write to a tag.")
	})

	updateDBButton := tview.NewButton("Update media DB").SetSelectedFunc(func() {
		app.Stop()
	})
	updateDBButton.SetFocusFunc(func() {
		helpText.SetText("Update Core media database.")
	})

	settingsButton := tview.NewButton("Settings").SetSelectedFunc(func() {
		enableZapScript := client.DisableZapScript(cfg)
		_, err := ConfigUiBuilder(cfg, app, pages, func() {
			enableZapScript()
			pages.SwitchToPage("main")
		})
		if err != nil {
			log.Error().Err(err).Msg("error running config app")
			app.Stop()
		}
	})
	settingsButton.SetFocusFunc(func() {
		helpText.SetText("Manage settings for Core service.")
	})

	exportButton := tview.NewButton("Export log").SetSelectedFunc(func() {
		widget := modalBuilder(setupLogExport(pl, app, pages, logDestPath, logDestName), 42, 8)
		pages.AddPage("export", widget, true, true)
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

	searchButton.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		k := event.Key()
		if k == tcell.KeyUp || k == tcell.KeyLeft {
			app.SetFocus(exitButton)
			return event
		} else if k == tcell.KeyDown || k == tcell.KeyRight {
			app.SetFocus(updateDBButton)
			return event
		}
		return event
	})
	updateDBButton.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		k := event.Key()
		if k == tcell.KeyUp || k == tcell.KeyLeft {
			app.SetFocus(searchButton)
			return event
		} else if k == tcell.KeyDown || k == tcell.KeyRight {
			app.SetFocus(settingsButton)
			return event
		}
		return event
	})
	settingsButton.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		k := event.Key()
		if k == tcell.KeyUp || k == tcell.KeyLeft {
			app.SetFocus(updateDBButton)
			return event
		} else if k == tcell.KeyDown || k == tcell.KeyRight {
			app.SetFocus(exportButton)
			return event
		}
		return event
	})
	exportButton.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		k := event.Key()
		if k == tcell.KeyUp || k == tcell.KeyLeft {
			app.SetFocus(settingsButton)
			return event
		} else if k == tcell.KeyDown || k == tcell.KeyRight {
			app.SetFocus(exitButton)
			return event
		}
		return event
	})
	exitButton.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		k := event.Key()
		if k == tcell.KeyUp || k == tcell.KeyLeft {
			app.SetFocus(exportButton)
			return event
		} else if k == tcell.KeyDown || k == tcell.KeyRight {
			app.SetFocus(searchButton)
			return event
		}
		return event
	})

	main.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		k := event.Key()
		if k == tcell.KeyEscape {
			app.Stop()
		}
		return event
	})

	buttonRowTop := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(searchButton, 0, 1, true).
		AddItem(updateDBButton, 0, 1, false).
		AddItem(settingsButton, 0, 1, false).
		AddItem(exportButton, 0, 1, false).
		AddItem(exitButton, 0, 1, false)

	main.AddItem(buttonRowTop, 20, 1, true)

	centeredPages := centerWidget(70, 20, pages)
	return app.SetRoot(centeredPages, true).EnableMouse(true), nil
}

func copyLogToSd(pl platforms.Platform, logDestPath string, logDestName string) string {
	logPath := path.Join(pl.Settings().TempDir, config.LogFile)
	newPath := logDestPath
	err := utils.CopyFile(logPath, newPath)
	outcome := ""
	if err != nil {
		outcome = fmt.Sprintf("Unable to copy log file to %s.", logDestName)
		log.Error().Err(err).Msgf("error copying log file")
	} else {
		outcome = fmt.Sprintf("Copied %s to %s.", config.LogFile, logDestName)
	}
	return outcome
}

func uploadLog(pl platforms.Platform, pages *tview.Pages, app *tview.Application) string {
	logPath := path.Join(pl.Settings().TempDir, config.LogFile)
	modal := genericModal("Uploading log file...", "Log upload", func(buttonIndex int, buttonLabel string) {}, false)
	pages.RemovePage("export")
	// FIXME: this is not updating, too busy
	pages.AddPage("temp_upload", modal, true, true)
	app.ForceDraw()
	uploadCmd := "cat '" + logPath + "' | nc termbin.com 9999"
	out, err := exec.Command("bash", "-c", uploadCmd).Output()
	pages.RemovePage("temp_upload")
	if err != nil {
		log.Error().Err(err).Msgf("error uploading log file to termbin")
		return "Unable to upload log file."
	} else {
		return "Log file URL:\n" + strings.TrimSpace(string(out))
	}
}
